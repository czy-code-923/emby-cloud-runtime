package store

import (
	"fmt"
	"github.com/prejudice-studio/twilight/internal/retention"
	"sort"
	"time"
)

// All wallet debits, grant facts, window decisions and delivery intentions
// share the state CAS transaction. Delivery retries never repeat a debit.
type RetentionState struct {
	Policy      retention.Policy              `json:"policy"`
	Members     map[int64]RetentionMember     `json:"members"`
	Quotes      map[string]RetentionQuote     `json:"quotes"`
	Settlements map[string]RetentionLedger    `json:"settlements"`
	Grants      map[string]RetentionGrant     `json:"grants"`
	Samples     map[string]RetentionSample    `json:"samples"`
	Watches     map[int64][]RetentionInterval `json:"watches"`
	LastPoll    int64                         `json:"last_poll"`
	Quality     string                        `json:"quality"`
}
type RetentionMember struct {
	Member          retention.Member `json:"member"`
	Policy          retention.Policy `json:"policy"`
	AppliedVersion  int64            `json:"applied_version"`
	NotifiedVersion int64            `json:"notified_version"`
	QualifiedAt     int64            `json:"qualified_at"`
}
type RetentionQuote struct {
	UID            int64               `json:"uid"`
	Selection      retention.Selection `json:"selection"`
	Action         string              `json:"action,omitempty"`
	Version        int64               `json:"version,omitempty"`
	Balance        int                 `json:"balance,omitempty"`
	Watch          int64               `json:"watch,omitempty"`
	End            int64               `json:"end,omitempty"`
	PolicyRevision int64               `json:"policy_revision,omitempty"`
}
type RetentionLedger struct {
	retention.Settlement
	UID           int64 `json:"uid"`
	BalanceBefore int   `json:"balance_before"`
	BalanceAfter  int   `json:"balance_after"`
}
type RetentionGrant struct {
	ID     string `json:"id"`
	UID    int64  `json:"uid"`
	Source string `json:"source"`
	Start  int64  `json:"start"`
	End    int64  `json:"end"`
	At     int64  `json:"at"`
}
type RetentionInterval struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
}
type RetentionSample struct {
	UID      int64  `json:"uid"`
	Item     string `json:"item"`
	At       int64  `json:"at"`
	Position int64  `json:"position"`
	Start    int64  `json:"start"`
}
type RetentionObservation struct {
	UID      int64
	Session  string
	Item     string
	Position int64
	Playing  bool
}
type RetentionView struct {
	Policy      retention.Policy  `json:"policy"`
	Member      RetentionMember   `json:"account"`
	WindowID    string            `json:"window_id"`
	Start       int64             `json:"window_start"`
	End         int64             `json:"window_end"`
	Seconds     int64             `json:"watch_seconds"`
	SigninDays  int               `json:"signin_days"`
	Balance     int               `json:"balance"`
	Selectable  bool              `json:"selectable"`
	AccessUntil int64             `json:"access_until"`
	LastPoll    int64             `json:"last_poll"`
	Quality     string            `json:"quality"`
	Username    string            `json:"username"`
	EmbyName    string            `json:"emby_username"`
	Ledger      []RetentionLedger `json:"ledger"`
}

func (r *RetentionState) ensure() {
	if r.Policy.Revision == 0 {
		r.Policy = retention.DefaultPolicy()
	}
	if r.Members == nil {
		r.Members = map[int64]RetentionMember{}
	}
	if r.Quotes == nil {
		r.Quotes = map[string]RetentionQuote{}
	}
	if r.Settlements == nil {
		r.Settlements = map[string]RetentionLedger{}
	}
	if r.Grants == nil {
		r.Grants = map[string]RetentionGrant{}
	}
	if r.Samples == nil {
		r.Samples = map[string]RetentionSample{}
	}
	if r.Watches == nil {
		r.Watches = map[int64][]RetentionInterval{}
	}
}
func (s *Store) RetentionPolicy() retention.Policy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p := s.state.Retention.Policy
	if p.Revision == 0 {
		p = retention.DefaultPolicy()
	}
	return p
}
func (s *Store) SetRetentionEnabled(enabled bool, revision int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutateAndSaveLocked(func() error {
		s.state.Retention.ensure()
		r := &s.state.Retention
		if r.Policy.Revision != revision {
			return ErrConflict
		}
		r.Policy.Enabled = enabled
		r.Policy.Revision++
		return nil
	})
}
func (s *Store) retentionMemberLocked(u User) RetentionMember {
	r := s.state.Retention.Members[u.UID]
	if r.Member.UID == 0 {
		p := s.state.Retention.Policy
		if p.Revision == 0 {
			p = retention.DefaultPolicy()
		}
		r = RetentionMember{Member: retention.Member{UID: u.UID, Version: 1, PlaybackEnd: u.ExpiredAt, ExemptEnd: u.ExpiredAt, Permanent: u.Role != RoleNormal || u.ExpiredAt < 0}, Policy: p}
	}
	// External card/admin changes revoke old candidates and preserve cooldown.
	if r.Member.PlaybackEnd != u.ExpiredAt {
		r.Member = retention.Withdraw(r.Member)
		r.Member.PlaybackEnd = u.ExpiredAt
		r.Member.ExemptEnd = u.ExpiredAt
		r.Member.Version++
		r.Member.LastSettlement = nil
		r.QualifiedAt = 0
	}
	r.Member.Permanent = u.Role != RoleNormal || u.ExpiredAt < 0
	return r
}
func (s *Store) reconcileRetentionEntitlementsLocked() {
	if len(s.state.Retention.Members) == 0 {
		return
	}
	s.state.Retention.ensure()
	for uid, old := range s.state.Retention.Members {
		u, ok := s.state.Users[uid]
		if !ok {
			delete(s.state.Retention.Members, uid)
			continue
		}
		r := s.retentionMemberLocked(u)
		if old.Member.PlaybackEnd != u.ExpiredAt {
			id := fmt.Sprintf("external:%d:%d", uid, r.Member.Version)
			s.state.Retention.Grants[id] = RetentionGrant{ID: id, UID: uid, Source: "external_entitlement_change", Start: old.Member.PlaybackEnd, End: u.ExpiredAt, At: time.Now().Unix()}
		}
		s.state.Retention.Members[uid] = r
		if s.state.Retention.Policy.Enabled {
			s.evaluateRetentionLocked(uid, false, time.Now().Unix())
		}
		u = s.state.Users[uid]
		latest := s.state.Retention.Members[uid]
		u.RetentionManaged = true
		u.RetentionAccessUntil = retention.AccessUntil(latest.Member, latest.Policy, latest.Member.Participation)
		s.state.Users[uid] = u
	}
}
func unionRetentionIntervals(all []RetentionInterval, start, end int64) ([]RetentionInterval, int64) {
	items := make([]RetentionInterval, 0, len(all))
	for _, v := range all {
		v.Start = max(v.Start, start)
		v.End = min(v.End, end)
		if v.End > v.Start {
			items = append(items, v)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Start < items[j].Start })
	merged := []RetentionInterval{}
	var seconds int64
	for _, v := range items {
		n := len(merged)
		if n > 0 && v.Start <= merged[n-1].End {
			merged[n-1].End = max(merged[n-1].End, v.End)
		} else {
			merged = append(merged, v)
		}
	}
	for _, v := range merged {
		seconds += v.End - v.Start
	}
	return merged, seconds
}
func (s *Store) retentionProgressLocked(r RetentionMember, now int64) retention.Progress {
	m := r.Member
	_, seconds := unionRetentionIntervals(s.state.Retention.Watches[m.UID], m.PlaybackEnd-30*retention.Day, min(m.PlaybackEnd, now))
	return retention.Progress{WindowID: retention.WindowID(m), Seconds: seconds, Version: fmt.Sprintf("%d", seconds), QualifiedAt: r.QualifiedAt}
}
func (s *Store) RetentionView(uid, now int64) (RetentionView, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.retentionViewLocked(uid, now)
}
func (s *Store) retentionViewLocked(uid, now int64) (RetentionView, error) {
	u, ok := s.state.Users[uid]
	if !ok {
		return RetentionView{}, ErrNotFound
	}
	r := s.retentionMemberLocked(u)
	p := r.Policy
	p.Enabled = s.state.Retention.Policy.Enabled
	progress := s.retentionProgressLocked(r, now)
	days := 0
	for _, v := range s.state.Signin[uid].Records {
		if v.CreatedAt >= r.Member.PlaybackEnd-30*retention.Day && v.CreatedAt < r.Member.PlaybackEnd {
			days++
		}
	}
	ledger := []RetentionLedger{}
	for _, v := range s.state.Retention.Settlements {
		if v.UID == uid {
			ledger = append(ledger, v)
		}
	}
	sort.Slice(ledger, func(i, j int) bool { return ledger[i].At > ledger[j].At })
	if len(ledger) > 12 {
		ledger = ledger[:12]
	}
	quality := s.state.Retention.Quality
	if !p.Enabled {
		quality = "disabled"
	} else if s.state.Retention.LastPoll == 0 {
		quality = "waiting"
	} else if now-s.state.Retention.LastPoll > 90 {
		quality = "stale"
	}
	return RetentionView{Policy: p, Member: r, WindowID: progress.WindowID, Start: r.Member.PlaybackEnd - 30*retention.Day, End: r.Member.PlaybackEnd, Seconds: progress.Seconds, SigninDays: days, Balance: s.state.Signin[uid].Points, Selectable: u.EmbyID != "" && u.Active && retention.Selectable(r.Member, p, now), AccessUntil: retention.AccessUntil(r.Member, p, r.Member.Participation), LastPoll: s.state.Retention.LastPoll, Quality: quality, Username: u.Username, EmbyName: u.EmbyUsername, Ledger: ledger}, nil
}
func (s *Store) RetentionPreview(uid int64, target, token string, now int64) (RetentionQuote, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var quote RetentionQuote
	err := s.mutateAndSaveLocked(func() error {
		s.state.Retention.ensure()
		u, ok := s.state.Users[uid]
		if !ok || !u.Active || u.EmbyID == "" {
			return ErrEmbyRequired
		}
		r := s.retentionMemberLocked(u)
		p := r.Policy
		p.Enabled = s.state.Retention.Policy.Enabled
		if target == "month" || target == "quarter" || target == "year" || target == "rescue" {
			var err error
			quote, err = s.retentionSpecialPreviewLocked(u, r, p, target, now)
			if err != nil {
				return err
			}
			for id, q := range s.state.Retention.Quotes {
				if q.UID == uid || q.Selection.ExpiresAt <= now {
					delete(s.state.Retention.Quotes, id)
				}
			}
			s.state.Retention.Quotes[token] = quote
			s.state.Retention.Members[uid] = r
			return nil
		}
		selection, err := retention.Preview(r.Member, retention.Wallet{Balance: s.state.Signin[uid].Points}, s.retentionProgressLocked(r, now), p, target, now)
		if err != nil {
			return err
		}
		for id, q := range s.state.Retention.Quotes {
			if q.UID == uid || q.Selection.ExpiresAt <= now {
				delete(s.state.Retention.Quotes, id)
			}
		}
		quote = RetentionQuote{UID: uid, Selection: selection}
		s.state.Retention.Quotes[token] = quote
		s.state.Retention.Members[uid] = r
		return nil
	})
	return quote, err
}
func (s *Store) RetentionConfirm(uid int64, token string, risk, consent bool, cap int, now int64, purchase ...bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutateAndSaveLocked(func() error {
		s.state.Retention.ensure()
		q, ok := s.state.Retention.Quotes[token]
		if !ok || q.UID != uid {
			if ledger, ok := s.state.Retention.Settlements["card:"+token]; ok && ledger.UID == uid {
				return errNoChange
			}
			return retention.ErrStale
		}
		u, ok := s.state.Users[uid]
		if !ok || !u.Active || u.EmbyID == "" {
			return ErrEmbyRequired
		}
		r := s.retentionMemberLocked(u)
		p := r.Policy
		p.Enabled = s.state.Retention.Policy.Enabled
		if q.Action != "" {
			confirmed := len(purchase) > 0 && purchase[0]
			if err := s.retentionSpecialConfirmLocked(u, r, p, q, token, risk, consent, cap, confirmed, now); err != nil {
				return err
			}
			delete(s.state.Retention.Quotes, token)
			return nil
		}
		m, err := retention.Confirm(r.Member, retention.Wallet{Balance: s.state.Signin[uid].Points}, s.retentionProgressLocked(r, now), p, q.Selection, risk, consent, cap, now)
		if err != nil {
			return err
		}
		r.Member = m
		s.state.Retention.Members[uid] = r
		delete(s.state.Retention.Quotes, token)
		s.evaluateRetentionLocked(uid, false, now)
		return nil
	})
}
func (s *Store) RetentionWithdraw(uid int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutateAndSaveLocked(func() error {
		s.state.Retention.ensure()
		u, ok := s.state.Users[uid]
		if !ok {
			return ErrNotFound
		}
		r := s.retentionMemberLocked(u)
		r.Member = retention.Withdraw(r.Member)
		s.state.Retention.Members[uid] = r
		return nil
	})
}
func (s *Store) evaluateRetentionLocked(uid int64, rescue bool, now int64) bool {
	r, ok := s.state.Retention.Members[uid]
	if !ok {
		return false
	}
	u, ok := s.state.Users[uid]
	if !ok || !u.Active || u.EmbyID == "" || u.Role != RoleNormal {
		return false
	}
	r = s.retentionMemberLocked(u)
	p := r.Policy
	p.Enabled = s.state.Retention.Policy.Enabled
	if !p.Enabled {
		return false
	}
	si := s.state.Signin[uid]
	progress := s.retentionProgressLocked(r, now)
	ready := (r.Member.Mode == retention.ModeA && si.Points >= p.Cost) || (r.Member.Mode == retention.ModeB && progress.Seconds >= p.WatchSeconds)
	if ready && r.QualifiedAt == 0 && (now < r.Member.PlaybackEnd || (r.Member.Mode == retention.ModeB && now < r.Member.PlaybackEnd+retention.Day)) {
		r.QualifiedAt = min(now, r.Member.PlaybackEnd-1)
		progress.QualifiedAt = r.QualifiedAt
	}
	s.state.Retention.Members[uid] = r
	decision, err := retention.Evaluate(r.Member, retention.Wallet{Balance: si.Points}, progress, p, rescue, now)
	if err != nil {
		return false
	}
	if _, exists := s.state.Retention.Settlements[decision.WindowID]; exists {
		return false
	}
	m, err := retention.ApplySettlement(r.Member, decision)
	if err != nil {
		return false
	}
	before := si.Points
	si.Points -= decision.Cost
	s.state.Signin[uid] = si
	u.ExpiredAt = decision.NewEnd
	s.state.Users[uid] = u
	r.Member = m
	r.QualifiedAt = 0
	r.Policy = s.state.Retention.Policy
	s.state.Retention.Members[uid] = r
	s.state.Retention.Settlements[decision.WindowID] = RetentionLedger{Settlement: decision, UID: uid, BalanceBefore: before, BalanceAfter: si.Points}
	s.state.Retention.Grants[decision.WindowID] = RetentionGrant{ID: decision.WindowID, UID: uid, Source: "activity_renewal_" + decision.Mode, Start: decision.OldEnd, End: decision.NewEnd, At: now}
	return true
}
func (s *Store) EvaluateRetention(uid, now int64, rescue bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutateAndSaveLocked(func() error {
		s.state.Retention.ensure()
		if !s.evaluateRetentionLocked(uid, rescue, now) {
			return errNoChange
		}
		return nil
	})
}

// Poll observations are made by the authenticated server, never accepted from
// Mini App request bodies. Position jumps, pause and stale gaps close a run.
func (s *Store) ObserveRetention(observations []RetentionObservation, now int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutateAndSaveLocked(func() error {
		s.state.Retention.ensure()
		r := &s.state.Retention
		if !r.Policy.Enabled || now <= r.LastPoll {
			return errNoChange
		}
		next := map[string]RetentionSample{}
		for i, v := range observations {
			if i >= 2000 {
				break
			}
			u, ok := s.state.Users[v.UID]
			if !ok || !u.Active || u.EmbyID == "" || !v.Playing || v.Session == "" || v.Item == "" {
				continue
			}
			prev, found := r.Samples[v.Session]
			sample := RetentionSample{UID: v.UID, Item: v.Item, At: now, Position: v.Position, Start: now}
			dt, dp := now-prev.At, v.Position-prev.Position
			if found && prev.UID == v.UID && prev.Item == v.Item && dt > 0 && dt <= 90 && dp > 0 && dp <= dt+5 && dp >= max(int64(1), dt-10) {
				sample.Start = prev.Start
				if now-sample.Start >= 120 {
					r.Watches[v.UID] = append(r.Watches[v.UID], RetentionInterval{Start: sample.Start, End: now})
				}
			}
			next[v.Session] = sample
		}
		r.Samples = next
		r.LastPoll = now
		r.Quality = "server_session_samples"
		for uid, items := range r.Watches {
			merged, _ := unionRetentionIntervals(items, now-37*retention.Day, now)
			r.Watches[uid] = merged
		}
		for uid := range r.Members {
			s.evaluateRetentionLocked(uid, false, now)
		}
		return nil
	})
}
func (s *Store) RetentionDelivery(uid, version int64, notified bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutateAndSaveLocked(func() error {
		r, ok := s.state.Retention.Members[uid]
		if !ok || r.Member.LastSettlement == nil || r.Member.LastSettlement.NewEnd != version {
			return ErrConflict
		}
		if notified {
			r.NotifiedVersion = version
		} else {
			r.AppliedVersion = version
		}
		s.state.Retention.Members[uid] = r
		return nil
	})
}
