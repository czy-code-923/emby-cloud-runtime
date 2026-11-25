// Package retention implements the v8.1 rules without clocks, HTTP or I/O.
// The caller must persist the decision, wallet, grant and outbox atomically.
package retention

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

const Day int64 = 86400
const (
	ModeA = "A"
	ModeB = "B"
)

var (
	ErrUnavailable = errors.New("retention unavailable")
	ErrCooldown    = errors.New("mode change cooling down")
	ErrConsent     = errors.New("explicit authorization required")
	ErrRisk        = errors.New("explicit shortfall acknowledgement required")
	ErrStale       = errors.New("preview changed or expired")
	ErrNotReady    = errors.New("window does not qualify")
)

type Policy struct {
	Revision        int64 `json:"revision"`
	Enabled         bool  `json:"enabled"`
	Cost            int   `json:"a_cost"`
	CycleSeconds    int64 `json:"cycle_seconds"`
	WatchSeconds    int64 `json:"watch_seconds"`
	CooldownSeconds int64 `json:"cooldown_seconds"`
	RiskSeconds     int64 `json:"risk_seconds"`
	ClosingSeconds  int64 `json:"closing_seconds"`
	RescueSeconds   int64 `json:"rescue_seconds"`
	OrganicCap      int   `json:"organic_cap"`
	DailyMin        int   `json:"daily_min"`
	DailyMax        int   `json:"daily_max"`
	MonthPoints     int   `json:"month_points"`
	QuarterPoints   int   `json:"quarter_points"`
	YearPoints      int   `json:"year_points"`
}

func DefaultPolicy() Policy {
	return Policy{Revision: 1, Cost: 300, CycleSeconds: 30 * Day, WatchSeconds: 1800, CooldownSeconds: 20 * Day, RiskSeconds: 3 * Day, ClosingSeconds: Day, RescueSeconds: 7 * Day, OrganicCap: 3600, DailyMin: 10, DailyMax: 20, MonthPoints: 300, QuarterPoints: 850, YearPoints: 3000}
}

func (p Policy) Validate() error {
	if p.Revision < 1 || p.Cost < 1 || p.Cost > 1000000 || p.CycleSeconds != 30*Day || p.WatchSeconds < 120 || p.WatchSeconds > 86400 || p.CooldownSeconds != 20*Day || p.RiskSeconds != 3*Day || p.ClosingSeconds != Day || p.RescueSeconds != 7*Day || p.DailyMin < 0 || p.DailyMax < p.DailyMin || p.DailyMax > 10000 {
		return ErrUnavailable
	}
	if p.MonthPoints != p.Cost || p.QuarterPoints < 1 || p.YearPoints < p.QuarterPoints || p.OrganicCap < p.YearPoints || p.OrganicCap > 100000000 {
		return ErrUnavailable
	}
	return nil
}

type Member struct {
	UID            int64       `json:"uid"`
	Version        int64       `json:"version"`
	PlaybackEnd    int64       `json:"playback_end"`
	ExemptEnd      int64       `json:"exempt_end"`
	Permanent      bool        `json:"permanent"`
	Mode           string      `json:"selected_mode"`
	Participation  bool        `json:"participation_enabled"`
	Consent        bool        `json:"a_consent"`
	ConsentCap     int         `json:"a_consent_cap"`
	Epoch          int64       `json:"authorization_epoch"`
	LastChangedAt  int64       `json:"last_mode_changed_at"`
	NextSwitchAt   int64       `json:"next_switch_at"`
	HoldUntil      int64       `json:"incident_hold_until"`
	LastSettlement *Settlement `json:"last_settlement,omitempty"`
}

type Wallet struct {
	Balance int   `json:"balance"`
	Frozen  int   `json:"frozen"`
	Version int64 `json:"version"`
}

func (w Wallet) Available() int { return max(0, w.Balance-w.Frozen) }

type Progress struct {
	WindowID string `json:"window_id"`
	Seconds  int64  `json:"seconds"`
	Version  string `json:"version"`
	// AcceptedAt is a persisted server receipt, never a client timestamp.
	QualifiedAt int64 `json:"qualified_at"`
}

func WindowID(m Member) string { return fmt.Sprintf("%d:%d", m.UID, m.PlaybackEnd) }
func Selectable(m Member, p Policy, now int64) bool {
	if !p.Enabled || m.Permanent || m.PlaybackEnd <= now || m.HoldUntil > now {
		return false
	}
	return !(m.ExemptEnd > now && m.PlaybackEnd-now >= p.CycleSeconds)
}
func ready(mode string, w Wallet, progress Progress, p Policy) bool {
	if mode == ModeA {
		return w.Available() >= p.Cost
	}
	return mode == ModeB && progress.Seconds >= p.WatchSeconds
}

type Selection struct {
	WindowID        string `json:"window_id"`
	WindowStart     int64  `json:"window_start"`
	WindowEnd       int64  `json:"window_end"`
	Source          string `json:"source_mode"`
	Target          string `json:"target_mode"`
	Cost            int    `json:"cost"`
	Available       int    `json:"available"`
	WatchSeconds    int64  `json:"watch_seconds"`
	PointsShortfall int    `json:"points_shortfall"`
	WatchShortfall  int64  `json:"watch_shortfall"`
	Risk            bool   `json:"risk"`
	NoChange        bool   `json:"no_change"`
	NextSwitchAt    int64  `json:"next_switch_at"`
	ExpiresAt       int64  `json:"expires_at"`
	Fingerprint     string `json:"fingerprint"`
}

func Preview(m Member, w Wallet, progress Progress, p Policy, target string, now int64) (Selection, error) {
	if p.Validate() != nil || !Selectable(m, p, now) || (target != ModeA && target != ModeB) {
		return Selection{}, ErrUnavailable
	}
	if progress.WindowID != WindowID(m) {
		return Selection{}, ErrStale
	}
	changed := m.Mode != target
	if changed && m.Mode != "" && now < m.NextSwitchAt {
		return Selection{}, ErrCooldown
	}
	risk := (!m.Participation || changed) && now >= m.PlaybackEnd-p.RiskSeconds && !ready(target, w, progress, p)
	next := m.NextSwitchAt
	if changed {
		next = now + p.CooldownSeconds
	}
	snapshot := struct {
		Member   Member
		Wallet   Wallet
		Progress Progress
		Policy   Policy
		Target   string
		Risk     bool
	}{m, w, progress, p, target, risk}
	raw, _ := json.Marshal(snapshot)
	digest := sha256.Sum256(raw)
	return Selection{WindowID: WindowID(m), WindowStart: m.PlaybackEnd - p.CycleSeconds, WindowEnd: m.PlaybackEnd, Source: m.Mode, Target: target, Cost: p.Cost, Available: w.Available(), WatchSeconds: progress.Seconds, PointsShortfall: max(0, p.Cost-w.Available()), WatchShortfall: max(int64(0), p.WatchSeconds-progress.Seconds), Risk: risk, NoChange: m.Participation && !changed, NextSwitchAt: next, ExpiresAt: now + 120, Fingerprint: hex.EncodeToString(digest[:])}, nil
}

func Confirm(m Member, w Wallet, progress Progress, p Policy, quote Selection, acknowledgeRisk, authorizeA bool, cap int, now int64) (Member, error) {
	if now >= quote.ExpiresAt || quote.ExpiresAt > now+120 {
		return m, ErrStale
	}
	current, err := Preview(m, w, progress, p, quote.Target, now)
	if err != nil {
		return m, err
	}
	if current.Fingerprint != quote.Fingerprint {
		return m, ErrStale
	}
	if current.Risk && !acknowledgeRisk {
		return m, ErrRisk
	}
	if quote.Target == ModeA && (!authorizeA || cap < p.Cost || cap > 1000000) {
		return m, ErrConsent
	}
	if current.NoChange && (quote.Target != ModeA || (m.Consent && m.ConsentCap == cap)) {
		return m, nil
	}
	if m.Mode != quote.Target {
		m.LastChangedAt = now
		m.NextSwitchAt = now + p.CooldownSeconds
	}
	m.Mode = quote.Target
	m.Participation = true
	m.Version++
	m.Epoch++
	m.Consent = quote.Target == ModeA && authorizeA
	m.ConsentCap = 0
	if m.Consent {
		m.ConsentCap = cap
	}
	return m, nil
}

// Withdraw never changes the mode cooldown or a committed settlement.
func Withdraw(m Member) Member {
	if !m.Participation && !m.Consent {
		return m
	}
	m.Participation = false
	m.Consent = false
	m.ConsentCap = 0
	m.Epoch++
	m.Version++
	return m
}

type Settlement struct {
	WindowID string `json:"window_id"`
	OldEnd   int64  `json:"old_end"`
	NewEnd   int64  `json:"new_end"`
	Mode     string `json:"mode"`
	Cost     int    `json:"spent_points"`
	At       int64  `json:"settled_at"`
	Rescue   bool   `json:"rescue"`
}

func Evaluate(m Member, w Wallet, progress Progress, p Policy, explicitRescue bool, now int64) (Settlement, error) {
	if p.Validate() != nil || !p.Enabled || m.Permanent || !m.Participation || m.HoldUntil > now || m.PlaybackEnd <= 0 || progress.WindowID != WindowID(m) || now < m.PlaybackEnd-p.CycleSeconds || !ready(m.Mode, w, progress, p) {
		return Settlement{}, ErrNotReady
	}
	if m.Mode == ModeA && (!m.Consent || m.ConsentCap < p.Cost) {
		return Settlement{}, ErrConsent
	}
	if now >= m.PlaybackEnd {
		if explicitRescue {
			if now >= m.PlaybackEnd+p.RescueSeconds {
				return Settlement{}, ErrNotReady
			}
		} else if now >= m.PlaybackEnd+p.ClosingSeconds || progress.QualifiedAt < m.PlaybackEnd-p.CycleSeconds || progress.QualifiedAt >= m.PlaybackEnd {
			return Settlement{}, ErrNotReady
		}
	}
	cost := 0
	if m.Mode == ModeA {
		cost = p.Cost
	}
	return Settlement{WindowID: WindowID(m), OldEnd: m.PlaybackEnd, NewEnd: m.PlaybackEnd + p.CycleSeconds, Mode: m.Mode, Cost: cost, At: now, Rescue: explicitRescue && now >= m.PlaybackEnd}, nil
}

func ApplySettlement(m Member, decision Settlement) (Member, error) {
	if decision.WindowID != WindowID(m) || decision.OldEnd != m.PlaybackEnd || decision.NewEnd <= decision.OldEnd {
		return m, ErrStale
	}
	m.PlaybackEnd = decision.NewEnd
	m.LastSettlement = &decision
	m.Version++
	return m, nil
}

// A paid/points exemption preserves unused time, pauses participation and
// invalidates uncommitted candidates; historical preference/cooldown remains.
func AddExemption(m Member, seconds, now int64) (Member, error) {
	if m.Permanent || seconds <= 0 || seconds > 10*365*Day {
		return m, ErrUnavailable
	}
	m.PlaybackEnd = max(m.PlaybackEnd, now) + seconds
	m.ExemptEnd = m.PlaybackEnd
	m.Participation = false
	m.Consent = false
	m.ConsentCap = 0
	m.Epoch++
	m.Version++
	return m, nil
}

func OrganicAward(balance, raw, cap int) int {
	if raw <= 0 || cap < 1 || balance >= cap {
		return 0
	}
	return min(raw, max(0, cap-balance))
}

// Temporary rescue includes the closing day; it never changes the business P.
func AccessUntil(m Member, p Policy, participatedAtDeadline bool) int64 {
	if m.Permanent {
		return -1
	}
	end := m.PlaybackEnd
	if participatedAtDeadline {
		end = max(end, m.PlaybackEnd+p.RescueSeconds)
	}
	return max(end, m.HoldUntil)
}
