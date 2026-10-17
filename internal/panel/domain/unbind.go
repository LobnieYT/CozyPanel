package domain

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"cozy/internal/panel/settings"
	"cozy/internal/panel/store/db"
)

// UnbindRules is how the subscriber unbinds devices on their own: how many per period,
// and how soon an unbound device may take a place again.
type UnbindRules struct {
	Off         bool `json:"off,omitempty" doc:"Подписчик не отвязывает устройства сам, только админ"`
	Limit       int  `json:"limit" doc:"Сколько устройств подписчик может отвязать за days; 0 — без ограничения"`
	Days        int  `json:"days" doc:"За сколько дней считается лимит (скользящее окно)"`
	ReturnHours int  `json:"return_hours" doc:"Сколько часов отвязанное подписчиком устройство не может привязаться снова; 0 — сразу, -1 — пока админ не разрешит"`
}

// ReturnNever: an unbound device waits for the admin, however long.
const ReturnNever = -1

// foreverUnix is the pause of ReturnNever, far past any prune.
const foreverUnix = 253402300799

// Forever is whether a pause moment means "until the admin allows".
func Forever(t time.Time) bool { return !t.IsZero() && t.Unix() >= foreverUnix }

// DefaultUnbindRules: one unbind a day, back in a day.
var DefaultUnbindRules = UnbindRules{Limit: 1, Days: 1, ReturnHours: 24}

// Bounds of the rules editor.
const (
	MaxUnbindLimit  = 100
	MaxUnbindDays   = 365
	MaxReturnHours  = 720
	unbindKeepHours = (365 + 1) * 24
)

var (
	ErrUnbindOff     = errors.New("unbind_off")     // the subscriber may not unbind on their own
	ErrDeviceUnbound = errors.New("device_unbound") // the subscriber unbound it a moment ago: it waits out the pause
)

// UnboundError is a fetch by a device waiting out its pause.
type UnboundError struct{ Until time.Time }

func (e *UnboundError) Error() string { return ErrDeviceUnbound.Error() }

// Is matches the pause against its sentinel, so errors.Is sees through.
func (e *UnboundError) Is(target error) bool { return target == ErrDeviceUnbound }

// Validate refuses rules the editor cannot mean.
func (r UnbindRules) Validate() error {
	if r.Limit < 0 || r.Limit > MaxUnbindLimit {
		return fieldErr("limit", "bad_unbind_limit")
	}
	if r.Days < 1 || r.Days > MaxUnbindDays {
		return fieldErr("days", "bad_unbind_days")
	}
	if r.ReturnHours < ReturnNever || r.ReturnHours > MaxReturnHours {
		return fieldErr("return_hours", "bad_return_hours")
	}
	return nil
}

// LoadUnbindRules reads the admin's rules; anything missing or broken falls back to
// the default. A partial save keeps the default for the fields it lacks.
func LoadUnbindRules(ctx context.Context, q *db.Queries) UnbindRules {
	r, _, err := settings.GetOver(ctx, settings.New(q), settings.KeyUnbindRules, DefaultUnbindRules)
	if err != nil || r.Validate() != nil {
		return DefaultUnbindRules
	}
	return r
}

// UnbindState is where a user stands against the rules: how many unbinds they used in
// the window, and when the next one frees up (zero: now).
type UnbindState struct {
	Rules UnbindRules
	Used  int
	Next  time.Time
}

// Left is how many unbinds the subscriber has: 0 when off or spent, -1 unlimited.
func (s UnbindState) Left() int {
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

// UnbindStateOf counts the user's unbinds in the sliding window.
func UnbindStateOf(ctx context.Context, q *db.Queries, userID int64, now time.Time) (UnbindState, error) {
	rules := LoadUnbindRules(ctx, q)
	st := UnbindState{Rules: rules}
	if rules.Off || rules.Limit == 0 {
		return st, nil
	}
	window := time.Duration(rules.Days) * 24 * time.Hour
	ats, err := q.ListUnbindsSince(ctx, db.ListUnbindsSinceParams{UserID: userID, At: now.Add(-window).Unix()})
	if err != nil {
		return st, err
	}
	st.Used = len(ats)
	if st.Used >= rules.Limit {
		st.Next = time.Unix(ats[st.Used-rules.Limit], 0).Add(window).UTC()
	}
	return st, nil
}

// holdUnbound pauses a device its subscriber unbound: its app, still open, would fetch
// the subscription and bind again at once. The admin's ban (no until) stays untouched.
func holdUnbound(ctx context.Context, q *db.Queries, userID int64, hwid, label string, rules UnbindRules, now time.Time) error {
	if hwid == "" || rules.ReturnHours == 0 {
		return nil
	}
	until := now.Add(time.Duration(rules.ReturnHours) * time.Hour)
	if rules.ReturnHours == ReturnNever {
		until = time.Unix(foreverUnix, 0)
	}
	return q.HoldUnbound(ctx, db.HoldUnboundParams{UserID: userID, Hwid: hwid, Label: label, BannedAt: now.Unix(),
		Until: sql.NullInt64{Int64: until.Unix(), Valid: true}})
}

// PruneDeviceData drops pauses that ran out and unbind history older than any window.
func PruneDeviceData(ctx context.Context, q *db.Queries, now time.Time) error {
	if err := q.DeleteExpiredBans(ctx, sql.NullInt64{Int64: now.Unix(), Valid: true}); err != nil {
		return err
	}
	return q.DeleteUnbindsBefore(ctx, now.Add(-unbindKeepHours*time.Hour).Unix())
}
