package domain

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"cozy/internal/panel/audit"
	"cozy/internal/panel/settings"
	"cozy/internal/panel/store/db"
)

// ReissueRules is how many new links the subscriber may issue on their own, over a
// sliding window.
type ReissueRules struct {
	Off   bool `json:"off,omitempty" doc:"Подписчик не меняет ссылку сам, только админ"`
	Limit int  `json:"limit" doc:"Сколько раз подписчик может сменить ссылку за days; 0 — без ограничения"`
	Days  int  `json:"days" doc:"За сколько дней считается лимит (скользящее окно)"`
}

// DefaultReissueRules: one new link a day.
var DefaultReissueRules = ReissueRules{Limit: 1, Days: 1}

// Bounds of the rules editor.
const (
	MaxReissueLimit = 100
	MaxReissueDays  = 365
)

var (
	ErrReissueOff      = errors.New("reissue_off")      // the subscriber may not change the link
	ErrReissueCooldown = errors.New("reissue_cooldown") // the subscriber used the limit for this window
)

// Validate refuses rules the editor cannot mean.
func (r ReissueRules) Validate() error {
	if r.Limit < 0 || r.Limit > MaxReissueLimit {
		return fieldErr("limit", "bad_reissue_limit")
	}
	if r.Days < 1 || r.Days > MaxReissueDays {
		return fieldErr("days", "bad_reissue_days")
	}
	return nil
}

func (r ReissueRules) window() time.Duration { return time.Duration(r.Days) * 24 * time.Hour }

// LoadReissueRules reads the admin's rules; a missing or broken value falls back to
// the default. Only a database read failure is an error.
func LoadReissueRules(ctx context.Context, q *db.Queries) (ReissueRules, error) {
	raw, err := q.GetSetting(ctx, settings.KeyReissueRules)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DefaultReissueRules, nil
		}
		return DefaultReissueRules, err
	}
	var r ReissueRules
	if err := json.Unmarshal([]byte(raw), &r); err != nil || r.Validate() != nil {
		return DefaultReissueRules, nil
	}
	return r, nil
}

// ReissueState is where a user stands against the rules: how many new links they took
// in the window, and when the next one frees up (zero: now).
type ReissueState struct {
	Rules ReissueRules
	Used  int
	Next  time.Time
}

// Left is how many new links the subscriber has: 0 when off or spent, -1 unlimited.
func (s ReissueState) Left() int {
	if s.Rules.Off {
		return 0
	}
	if s.Rules.Limit == 0 {
		return -1
	}
	if l := s.Rules.Limit - s.Used; l > 0 {
		return l
	}
	return 0
}

func reissueState(ctx context.Context, q *db.Queries, rules ReissueRules, userID int64, now time.Time) (ReissueState, error) {
	st := ReissueState{Rules: rules}
	if rules.Off || rules.Limit == 0 {
		return st, nil
	}
	ats, err := q.ListLinkReissuesSince(ctx, db.ListLinkReissuesSinceParams{UserID: userID, At: now.Add(-rules.window()).Unix()})
	if err != nil {
		return st, err
	}
	st.Used = len(ats)
	if st.Used >= rules.Limit {
		st.Next = time.Unix(ats[st.Used-rules.Limit], 0).Add(rules.window()).UTC()
	}
	return st, nil
}

// ReissueState of a user now.
func (s *Users) ReissueState(ctx context.Context, userID int64) (ReissueState, error) {
	rules, err := LoadReissueRules(ctx, s.st.Q)
	if err != nil {
		return ReissueState{}, err
	}
	return reissueState(ctx, s.st.Q, rules, userID, s.now())
}

// ReissueByOwner gives the user a new link at their own request (bot or Mini App),
// within the admin's rules. The old link stops working at once and the bound devices
// are forgotten. The admin's own reissue is outside the rules.
func (s *Users) ReissueByOwner(ctx context.Context, id int64, via string) (db.User, error) {
	rules, err := LoadReissueRules(ctx, s.st.Q)
	if err != nil {
		return db.User{}, err
	}
	if rules.Off {
		return db.User{}, ErrReissueOff
	}
	err = s.reissueByOwner(ctx, rules, id, via)
	if errors.Is(err, ErrNoSlots) {
		if err := s.pool.Refill(ctx, RefillBatch); err != nil {
			return db.User{}, err
		}
		s.changes.SlotsChanged()
		err = s.reissueByOwner(ctx, rules, id, via)
	}
	if err != nil {
		return db.User{}, err
	}
	s.changes.PoliciesChanged()
	return s.Get(ctx, id)
}

func (s *Users) reissueByOwner(ctx context.Context, rules ReissueRules, id int64, via string) error {
	now := s.now()
	return s.st.Tx(ctx, func(q *db.Queries) error {
		if _, err := q.LockUser(ctx, id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		st, err := reissueState(ctx, q, rules, id, now)
		if err != nil {
			return err
		}
		if !st.Next.IsZero() {
			return ErrReissueCooldown
		}
		if err := s.reissueOn(ctx, q, id, now.Unix()); err != nil {
			return err
		}
		if err := q.AddLinkReissue(ctx, db.AddLinkReissueParams{UserID: id, At: now.Unix()}); err != nil {
			return err
		}
		return audit.Write(ctx, q, now, audit.Entry{Action: "user.reissue", TargetType: "user", TargetID: strconv.FormatInt(id, 10),
			Details: map[string]any{"actor": "subscriber", "via": via}})
	})
}
