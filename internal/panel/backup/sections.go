package backup

import (
	"context"
	"database/sql"
	"fmt"
)

// byNameOrInsert resolves a named row (tariffs, pools, packages): existing id
// by name, or a fresh insert under replace/fresh. Skip leaves the map empty.
func (a *applier) byNameOrInsert(ctx context.Context, table string, row map[string]any, extra map[string]any) (int64, bool, error) {
	name, _ := row["name"].(string)
	if name == "" {
		return 0, false, nil
	}
	names, err := a.byName(ctx, a.tx, table)
	if err != nil {
		return 0, false, err
	}
	if id, ok := names[name]; ok {
		if a.strategy == StrategyReplace {
			merged := map[string]any{}
			for k, v := range row {
				merged[k] = v
			}
			for k, v := range extra {
				merged[k] = v
			}
			if err := updateRow(ctx, a.tx, table, id, merged); err != nil {
				return 0, false, err
			}
			return id, true, nil
		}
		return id, false, nil
	}
	if a.strategy == StrategySkip {
		return 0, false, nil
	}
	merged := map[string]any{}
	for k, v := range row {
		merged[k] = v
	}
	for k, v := range extra {
		merged[k] = v
	}
	id, err := insertRow(ctx, a.tx, table, merged, nil)
	return id, err == nil, err
}

// tariffs imports pools, packages, tariffs and their links, remapping ids.
func (a *applier) applyTariffs(ctx context.Context) error {
	a.pools, a.packages, a.tariffs = map[int64]int64{}, map[int64]int64{}, map[int64]int64{}
	pools, err := rows(ctx, a.src, `SELECT * FROM traffic_pools`)
	if err != nil {
		return err
	}
	for _, p := range pools {
		id, kept, err := a.byNameOrInsert(ctx, "traffic_pools", p, nil)
		if err != nil {
			return err
		}
		old := toInt(p["id"])
		if id != 0 {
			a.pools[old] = id
			a.count(SectionTariffs, boolStr(kept, "update", "insert"))
		} else {
			a.count(SectionTariffs, "skip")
		}
	}
	pkgs, err := rows(ctx, a.src, `SELECT * FROM traffic_packages`)
	if err != nil {
		return err
	}
	for _, p := range pkgs {
		remapped := remapID(p, "pool_id", a.pools)
		id, kept, err := a.byNameOrInsert(ctx, "traffic_packages", p, remapped)
		if err != nil {
			return err
		}
		old := toInt(p["id"])
		if id != 0 {
			a.packages[old] = id
			a.count(SectionTariffs, boolStr(kept, "update", "insert"))
		} else {
			a.count(SectionTariffs, "skip")
		}
	}
	tariffs, err := rows(ctx, a.src, `SELECT * FROM tariffs`)
	if err != nil {
		return err
	}
	for _, t := range tariffs {
		id, kept, err := a.byNameOrInsert(ctx, "tariffs", t, nil)
		if err != nil {
			return err
		}
		old := toInt(t["id"])
		if id != 0 {
			a.tariffs[old] = id
			a.count(SectionTariffs, boolStr(kept, "update", "insert"))
		} else {
			a.count(SectionTariffs, "skip")
		}
	}
	links, err := rows(ctx, a.src, `SELECT * FROM tariff_pools`)
	if err != nil {
		return err
	}
	for _, l := range links {
		tid, ok1 := a.tariffs[toInt(l["tariff_id"])]
		pid, ok2 := a.pools[toInt(l["pool_id"])]
		if !ok1 || !ok2 {
			continue
		}
		var n int
		if err := a.tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM tariff_pools WHERE tariff_id = ? AND pool_id = ?`, tid, pid).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			if _, err := a.tx.ExecContext(ctx, `INSERT INTO tariff_pools (tariff_id, pool_id, traffic_limit) VALUES (?, ?, ?)`, tid, pid, l["traffic_limit"]); err != nil {
				return err
			}
		}
	}
	return nil
}

func boolStr(b bool, t, f string) string {
	if b {
		return t
	}
	return f
}

// remapID rewrites one foreign key through a map; missing entries become NULL.
func remapID(row map[string]any, col string, m map[int64]int64) map[string]any {
	out := map[string]any{}
	if id, ok := m[toInt(row[col])]; ok {
		out[col] = id
	} else {
		out[col] = nil
	}
	return out
}

// nodes imports nodes (id 1 is always the local one), warp rows, relays and
// the relay mesh, remapping node ids.
func (a *applier) applyNodes(ctx context.Context) error {
	a.nodes = map[int64]int64{}
	list, err := rows(ctx, a.src, `SELECT * FROM nodes ORDER BY id`)
	if err != nil {
		return err
	}
	for _, n := range list {
		old := toInt(n["id"])
		if old == 1 {
			a.nodes[1] = 1
			if a.strategy == StrategyReplace {
				if err := updateRow(ctx, a.tx, "nodes", 1, without(n, "id")); err != nil {
					return err
				}
				a.count(SectionNodes, "update")
			} else {
				a.count(SectionNodes, "skip")
			}
			continue
		}
		addr, _ := n["address"].(string)
		var id int64
		var matched bool
		if addr != "" {
			err := a.tx.QueryRowContext(ctx, `SELECT id FROM nodes WHERE address = ? AND id <> 1`, addr).Scan(&id)
			if err != nil && err != sql.ErrNoRows {
				return err
			}
			matched = err == nil
		}
		switch {
		case matched && a.strategy == StrategyReplace:
			if err := updateRow(ctx, a.tx, "nodes", id, without(n, "id")); err != nil {
				return err
			}
			a.nodes[old] = id
			a.count(SectionNodes, "update")
		case matched:
			a.nodes[old] = id
			a.count(SectionNodes, "skip")
		case a.strategy == StrategySkip:
			a.count(SectionNodes, "skip")
		default:
			id, err := insertRow(ctx, a.tx, "nodes", without(n, "id"), nil)
			if err != nil {
				return err
			}
			a.nodes[old] = id
			a.count(SectionNodes, "insert")
		}
	}
	warps, err := rows(ctx, a.src, `SELECT * FROM node_warp`)
	if err != nil {
		return err
	}
	for _, w := range warps {
		id, ok := a.nodes[toInt(w["node_id"])]
		if !ok {
			continue
		}
		if _, err := a.tx.ExecContext(ctx, `DELETE FROM node_warp WHERE node_id = ?`, id); err != nil {
			return err
		}
		row := without(w, "node_id")
		row["node_id"] = id
		if _, err := insertRow(ctx, a.tx, "node_warp", row, []string{"node_id"}); err != nil {
			return err
		}
	}
	relays, err := rows(ctx, a.src, `SELECT * FROM node_relays`)
	if err != nil {
		return err
	}
	for _, r := range relays {
		id, ok := a.nodes[toInt(r["node_id"])]
		if !ok {
			continue
		}
		var exit any
		if x, ok := a.nodes[toInt(r["exit_node_id"])]; ok {
			exit = x
		}
		if _, err := a.tx.ExecContext(ctx, `DELETE FROM node_relays WHERE node_id = ?`, id); err != nil {
			return err
		}
		row := without(r, "node_id", "exit_node_id")
		row["node_id"] = id
		row["exit_node_id"] = exit
		if _, err := insertRow(ctx, a.tx, "node_relays", row, []string{"node_id"}); err != nil {
			return err
		}
	}
	mesh, err := rows(ctx, a.src, `SELECT * FROM relay_users`)
	if err != nil {
		return err
	}
	for _, m := range mesh {
		id, ok := a.nodes[toInt(m["exit_node_id"])]
		if !ok {
			a.warnf("relay mesh of missing node skipped")
			continue
		}
		row := without(m, "exit_node_id")
		row["exit_node_id"] = id
		if _, err := insertRow(ctx, a.tx, "relay_users", row, nil); err != nil {
			return err
		}
	}
	return nil
}

func without(row map[string]any, drop ...string) map[string]any {
	out := map[string]any{}
	dropped := map[string]bool{}
	for _, k := range drop {
		dropped[k] = true
	}
	for k, v := range row {
		if !dropped[k] {
			out[k] = v
		}
	}
	return out
}

// inbounds imports inbounds matched by (node, name); the node comes from the
// nodes section when selected, else from the live database by address.
func (a *applier) applyInbounds(ctx context.Context) error {
	a.inbounds = map[int64]int64{}
	list, err := rows(ctx, a.src, `SELECT * FROM inbounds ORDER BY id`)
	if err != nil {
		return err
	}
	for _, in := range list {
		old := toInt(in["id"])
		node, ok := a.nodes[toInt(in["node_id"])]
		if !ok {
			// Nodes section not selected: attach by address to a live node.
			node, ok = a.nodeByAddress(ctx, in)
			if !ok {
				a.warnf("inbound %q skipped: its node is not here", in["name"])
				a.count(SectionInbounds, "skip")
				continue
			}
		}
		name, _ := in["name"].(string)
		var id int64
		err := a.tx.QueryRowContext(ctx, `SELECT id FROM inbounds WHERE node_id = ? AND name = ?`, node, name).Scan(&id)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		row := without(in, "id", "node_id", "pool_id")
		row["node_id"] = node
		if pid, ok := a.pools[toInt(in["pool_id"])]; ok {
			row["pool_id"] = pid
		} else if a.pools != nil {
			row["pool_id"] = nil
		}
		switch {
		case err == nil && a.strategy == StrategyReplace:
			if err := updateRow(ctx, a.tx, "inbounds", id, row); err != nil {
				return err
			}
			a.inbounds[old] = id
			a.count(SectionInbounds, "update")
		case err == nil:
			var cur int64
			_ = a.tx.QueryRowContext(ctx, `SELECT id FROM inbounds WHERE node_id = ? AND name = ?`, node, name).Scan(&cur)
			a.inbounds[old] = id
			a.count(SectionInbounds, "skip")
		case a.strategy == StrategySkip:
			a.count(SectionInbounds, "skip")
		default:
			if a.strategy == StrategyFresh {
				row["name"] = freshName(ctx, a.tx, "inbounds", "node_id", node, name)
			}
			id, err := insertRow(ctx, a.tx, "inbounds", row, nil)
			if err != nil {
				return err
			}
			a.inbounds[old] = id
			a.count(SectionInbounds, "insert")
		}
	}
	return nil
}

// nodeByAddress finds a live node by the source row's address, for imports
// without the nodes section.
func (a *applier) nodeByAddress(ctx context.Context, in map[string]any) (int64, bool) {
	var addr string
	err := a.src.QueryRowContext(ctx, `SELECT address FROM nodes WHERE id = ?`, toInt(in["node_id"])).Scan(&addr)
	if err != nil || addr == "" {
		return 0, false
	}
	var id int64
	if err := a.tx.QueryRowContext(ctx, `SELECT id FROM nodes WHERE address = ?`, addr).Scan(&id); err != nil {
		return 0, false
	}
	return id, true
}

func freshName(ctx context.Context, tx *sql.Tx, table, scopeCol string, scope int64, base any) string {
	name, _ := base.(string)
	if name == "" {
		name = "imported"
	}
	// Fresh rows keep their name unless taken, then take "name (2)", "name (3)", ...
	candidate := name
	for i := 2; ; i++ {
		var n int
		if err := tx.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s = ? AND name = ?", table, scopeCol), scope, candidate).Scan(&n); err != nil || n == 0 {
			return candidate
		}
		candidate = fmt.Sprintf("%s (%d)", name, i)
	}
}
