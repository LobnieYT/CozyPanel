package backup

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
)

// users imports users with their slots: matched by sub_token and slot uuid.
// Devices, traffic history and grants are operational rows and stay behind;
// counters travel on replace, reset on fresh.
func (a *applier) applyUsers(ctx context.Context) error {
	list, err := rows(ctx, a.src, `SELECT * FROM users ORDER BY id`)
	if err != nil {
		return err
	}
	for _, u := range list {
		if a.strategy == StrategyFresh {
			// Fresh mints a new identity every time, even for known tokens.
			if err := a.insertUser(ctx, u, true); err != nil {
				return err
			}
			a.count(SectionUsers, "insert")
			continue
		}
		token, _ := u["sub_token"].(string)
		var id int64
		err := a.tx.QueryRowContext(ctx, `SELECT id FROM users WHERE sub_token = ?`, token).Scan(&id)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		switch {
		case err == nil && a.strategy == StrategyReplace:
			if err := a.replaceUser(ctx, id, u); err != nil {
				return err
			}
			a.count(SectionUsers, "update")
		case err == nil:
			a.count(SectionUsers, "skip")
		case a.strategy == StrategySkip:
			a.count(SectionUsers, "skip")
		default:
			// Unmatched under replace: take the row as-is, links survive.
			if err := a.insertUser(ctx, u, false); err != nil {
				return err
			}
			a.count(SectionUsers, "insert")
		}
	}
	// Fresh slots must not collide with the pool's future names: push the
	// counter past every slot id in use.
	var maxID int64
	if err := a.tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(id), 0) FROM slots`).Scan(&maxID); err != nil {
		return err
	}
	var last int64
	if err := a.tx.QueryRowContext(ctx, `SELECT last FROM slot_counter WHERE id = 1`).Scan(&last); err != nil && err != sql.ErrNoRows {
		return err
	}
	if maxID > last {
		if _, err := a.tx.ExecContext(ctx, `INSERT INTO slot_counter (id, last) VALUES (1, ?) ON CONFLICT (id) DO UPDATE SET last = excluded.last`, maxID); err != nil {
			return err
		}
	}
	// Bump the panel's slot epoch? No: quotas re-base from the panel on the
	// next push, like with any fresh node.
	return nil
}

// slotFor resolves the target slot for an incoming user row: the slot with
// the same uuid (secret refreshed on replace), or a fresh insert of it. Fresh
// always mints a new uuid: new credentials, never someone else's.
func (a *applier) slotFor(ctx context.Context, u map[string]any, fresh bool) (int64, error) {
	var uuid string
	srows, err := rows(ctx, a.src, `SELECT * FROM slots WHERE id = ?`, toInt(u["slot_id"]))
	if err != nil {
		return 0, err
	}
	if len(srows) == 0 {
		return 0, nil
	}
	s := srows[0]
	uuid, _ = s["uuid"].(string)
	if fresh {
		s["uuid"] = newUUID()
		s["name"] = freshSlotName(ctx, a.tx, s)
		return insertRow(ctx, a.tx, "slots", without(s, "id"), nil)
	}
	var id int64
	err = a.tx.QueryRowContext(ctx, `SELECT id FROM slots WHERE uuid = ?`, uuid).Scan(&id)
	if err != nil && err != sql.ErrNoRows {
		return 0, err
	}
	if err == nil {
		if a.strategy == StrategyReplace {
			if err := updateRow(ctx, a.tx, "slots", id, without(s, "id")); err != nil {
				return 0, err
			}
		}
		return id, nil
	}
	return insertRow(ctx, a.tx, "slots", without(s, "id"), nil)
}

func freshSlotName(ctx context.Context, tx *sql.Tx, s map[string]any) string {
	base, _ := s["name"].(string)
	if base == "" {
		base = "imported"
	}
	candidate := base
	for i := 2; ; i++ {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM slots WHERE name = ?`, candidate).Scan(&n); err != nil || n == 0 {
			return candidate
		}
		candidate = fmt.Sprintf("%s (%d)", base, i)
	}
}

func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func newToken() string {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}

// replaceUser overwrites a matched user row (counters included: the backup is
// the truth at its time) and rebuilds its slot link and pool limits.
func (a *applier) replaceUser(ctx context.Context, id int64, u map[string]any) error {
	slot, err := a.slotFor(ctx, u, false)
	if err != nil {
		a.warnf("user kept without its slot: %v", err)
		slot = 0
	}
	row := without(u, "id", "slot_id", "tariff_id", "inbounds")
	row["slot_id"] = nullIfZero(slot)
	if tid, ok := a.tariffs[toInt(u["tariff_id"])]; ok {
		row["tariff_id"] = tid
	} else {
		row["tariff_id"] = nil
	}
	row["inbounds"] = remapInbounds(u["inbounds"], a.inbounds)
	if err := updateRow(ctx, a.tx, "users", id, row); err != nil {
		return err
	}
	if _, err := a.tx.ExecContext(ctx, `DELETE FROM user_pools WHERE user_id = ?`, id); err != nil {
		return err
	}
	return a.userPoolsFor(ctx, id, toInt(u["id"]))
}

// insertUser stores an incoming user; fresh regenerates the token, the slot
// uuid and zeroes the counters (handed-out links die, as promised).
func (a *applier) insertUser(ctx context.Context, u map[string]any, fresh bool) error {
	slot, err := a.slotFor(ctx, u, fresh)
	if err != nil {
		return err
	}
	row := without(u, "id", "slot_id", "tariff_id", "inbounds", "sub_token")
	if fresh {
		row["sub_token"] = freshToken(a.tx, ctx)
		for _, c := range []string{"used_up", "used_down", "total_up", "total_down"} {
			row[c] = 0
		}
	} else {
		row["sub_token"] = u["sub_token"]
	}
	row["slot_id"] = nullIfZero(slot)
	if tid, ok := a.tariffs[toInt(u["tariff_id"])]; ok {
		row["tariff_id"] = tid
	} else {
		row["tariff_id"] = nil
	}
	row["inbounds"] = remapInbounds(u["inbounds"], a.inbounds)
	id, err := insertRow(ctx, a.tx, "users", row, nil)
	if err != nil {
		return err
	}
	return a.userPoolsFor(ctx, id, toInt(u["id"]))
}

func freshToken(tx *sql.Tx, ctx context.Context) string {
	for i := 0; i < 5; i++ {
		t := newToken()
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE sub_token = ?`, t).Scan(&n); err == nil && n == 0 {
			return t
		}
	}
	return newToken()
}

func nullIfZero(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// remapInbounds rewrites the allowed-inbound id list through the inbound map,
// dropping ids with no counterpart.
func remapInbounds(raw any, m map[int64]int64) any {
	s, _ := raw.(string)
	if s == "" {
		return raw
	}
	var ids []int64
	if err := json.Unmarshal([]byte(s), &ids); err != nil {
		return raw
	}
	out := []int64{}
	for _, id := range ids {
		if nid, ok := m[id]; ok {
			out = append(out, nid)
		}
	}
	if len(out) == 0 {
		return nil
	}
	raw2, _ := json.Marshal(out)
	return string(raw2)
}

// userPoolsFor copies per-user pool limits from the source user to the target
// one, through the pool map.
func (a *applier) userPoolsFor(ctx context.Context, targetID, sourceID int64) error {
	list, err := rows(ctx, a.src, `SELECT * FROM user_pools WHERE user_id = ?`, sourceID)
	if err != nil {
		return err
	}
	for _, p := range list {
		pid, ok := a.pools[toInt(p["pool_id"])]
		if !ok {
			continue
		}
		if _, err := a.tx.ExecContext(ctx, `INSERT INTO user_pools (user_id, pool_id, traffic_limit, used_up, used_down) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (user_id, pool_id) DO UPDATE SET traffic_limit = excluded.traffic_limit`,
			targetID, pid, p["traffic_limit"], p["used_up"], p["used_down"]); err != nil {
			return err
		}
	}
	return nil
}

// settings imports setting keys: replace overwrites, skip leaves, fresh acts
// like replace (there is nothing fresh about a key). Path keys never travel.
func (a *applier) applySettings(ctx context.Context, net bool) error {
	list, err := rows(ctx, a.src, `SELECT key, value FROM settings`)
	if err != nil {
		return err
	}
	sec := SectionSettings
	if net {
		sec = SectionNetwork
	}
	for _, r := range list {
		k, _ := r["key"].(string)
		if pathKeys[k] {
			continue
		}
		isNet := false
		for _, nk := range networkKeys {
			if nk == k {
				isNet = true
			}
		}
		if isNet != net {
			continue
		}
		var n int
		if err := a.tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM settings WHERE key = ?`, k).Scan(&n); err != nil {
			return err
		}
		if n > 0 && a.strategy == StrategySkip {
			a.count(sec, "skip")
			continue
		}
		if _, err := a.tx.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?)
			ON CONFLICT (key) DO UPDATE SET value = excluded.value`, k, r["value"]); err != nil {
			return err
		}
		if n > 0 {
			a.count(sec, "update")
		} else {
			a.count(sec, "insert")
		}
	}
	return nil
}
