package store

import "github.com/prejudice-studio/twilight/internal/retention"

func retentionCard(p retention.Policy, sku string) (int, int64) {
	switch sku {
	case "month":
		return p.MonthPoints, 30 * retention.Day
	case "quarter":
		return p.QuarterPoints, 90 * retention.Day
	case "year":
		return p.YearPoints, 365 * retention.Day
	}
	return 0, 0
}
func (s *Store) retentionSpecialPreviewLocked(u User, r RetentionMember, p retention.Policy, action string, now int64) (RetentionQuote, error) {
	if !p.Enabled || r.Member.Permanent || u.EmbyDisabled && !u.EmbyAutoDisabled {
		return RetentionQuote{}, retention.ErrUnavailable
	}
	balance := s.availablePointsLocked(u.UID)
	progress := s.retentionProgressLocked(r, now)
	cost, days := retentionCard(p, action)
	if action == "rescue" {
		if !r.Member.Participation || now < r.Member.PlaybackEnd || now >= r.Member.PlaybackEnd+p.RescueSeconds {
			return RetentionQuote{}, retention.ErrUnavailable
		}
		_, progress.Seconds = unionRetentionIntervals(s.state.Retention.Watches[u.UID], r.Member.PlaybackEnd-p.CycleSeconds, now)
		if r.Member.Mode == retention.ModeA {
			cost = p.Cost
		}
	} else if cost <= 0 || days <= 0 {
		return RetentionQuote{}, ErrInvalid
	}
	q := RetentionQuote{UID: u.UID, Action: action, Version: r.Member.Version, Balance: balance, Watch: progress.Seconds, End: u.ExpiredAt, PolicyRevision: s.state.Retention.Policy.Revision}
	q.Selection = retention.Selection{WindowID: retention.WindowID(r.Member), WindowStart: r.Member.PlaybackEnd - p.CycleSeconds, WindowEnd: r.Member.PlaybackEnd, Target: action, Cost: cost, Available: balance, WatchSeconds: progress.Seconds, PointsShortfall: max(0, cost-balance), WatchShortfall: max(int64(0), p.WatchSeconds-progress.Seconds), ExpiresAt: now + 120, NextSwitchAt: r.Member.NextSwitchAt, Risk: action == "rescue"}
	return q, nil
}
func (s *Store) retentionSpecialConfirmLocked(u User, r RetentionMember, p retention.Policy, q RetentionQuote, token string, risk, consent bool, cap int, purchase bool, now int64) error {
	si := s.state.Signin[u.UID]
	if !p.Enabled || q.Selection.ExpiresAt <= now || r.Member.Version != q.Version || u.ExpiredAt != q.End || s.availablePointsLocked(u.UID) != q.Balance || s.state.Retention.Policy.Revision != q.PolicyRevision {
		return retention.ErrStale
	}
	var decision retention.Settlement
	var m retention.Member
	if q.Action == "rescue" {
		if !risk {
			return retention.ErrRisk
		}
		progress := s.retentionProgressLocked(r, now)
		_, progress.Seconds = unionRetentionIntervals(s.state.Retention.Watches[u.UID], r.Member.PlaybackEnd-p.CycleSeconds, now)
		if progress.Seconds != q.Watch {
			return retention.ErrStale
		}
		if r.Member.Mode == retention.ModeA && (!consent || cap < p.Cost) {
			return retention.ErrConsent
		}
		var err error
		decision, err = retention.Evaluate(r.Member, s.walletLocked(u.UID), progress, p, true, now)
		if err != nil {
			return err
		}
		if _, exists := s.state.Retention.Settlements[decision.WindowID]; exists {
			return retention.ErrStale
		}
		m, err = retention.ApplySettlement(r.Member, decision)
		if err != nil {
			return err
		}
		if decision.Mode == retention.ModeB {
			// Post-deadline rescue seconds belong to the failed window only. They
			// must not also qualify the new window which starts at the original P.
			before := []RetentionInterval{}
			for _, v := range s.state.Retention.Watches[u.UID] {
				if v.Start < decision.OldEnd {
					v.End = min(v.End, decision.OldEnd)
					if v.End > v.Start {
						before = append(before, v)
					}
				}
			}
			s.state.Retention.Watches[u.UID] = before
		}
	} else {
		if !purchase {
			return retention.ErrConsent
		}
		cost, days := retentionCard(p, q.Action)
		if cost <= 0 || cost != q.Selection.Cost || s.availablePointsLocked(u.UID) < cost {
			return ErrInsufficientPoints
		}
		var err error
		m, err = retention.AddExemption(r.Member, days, now)
		if err != nil {
			return err
		}
		decision = retention.Settlement{WindowID: "card:" + token, OldEnd: r.Member.PlaybackEnd, NewEnd: m.PlaybackEnd, Mode: "EXEMPT_" + q.Action, Cost: cost, At: now}
		m.LastSettlement = &decision
	}
	oldUser := u
	before := si.Points
	si.Points -= decision.Cost
	s.state.Signin[u.UID] = si
	u.ExpiredAt = m.PlaybackEnd
	s.state.Users[u.UID] = u
	s.recordEntitlementLocked(decision.WindowID, "points_direct", oldUser, u, now)
	s.walletEntryLocked(u.UID, u.UID, "retention:"+decision.WindowID, "points_direct", "免保号或补救", -decision.Cost)
	r.Member = m
	r.QualifiedAt = 0
	r.Policy = s.state.Retention.Policy
	s.state.Retention.Members[u.UID] = r
	s.state.Retention.Settlements[decision.WindowID] = RetentionLedger{Settlement: decision, UID: u.UID, BalanceBefore: before, BalanceAfter: si.Points}
	source := "exempt_playback_" + q.Action
	if q.Action == "rescue" {
		source = "activity_rescue_" + decision.Mode
	}
	s.state.Retention.Grants[decision.WindowID] = RetentionGrant{ID: decision.WindowID, UID: u.UID, Source: source, Start: decision.OldEnd, End: decision.NewEnd, At: now}
	return nil
}
