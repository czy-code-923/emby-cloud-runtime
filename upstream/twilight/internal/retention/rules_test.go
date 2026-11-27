package retention

import (
	"errors"
	"testing"
)

func fixture() (Member, Wallet, Progress, Policy, int64) {
	now := int64(1800000000)
	p := DefaultPolicy()
	p.Enabled = true
	m := Member{UID: 7, Version: 1, PlaybackEnd: now + 30*Day}
	return m, Wallet{Balance: 300, Version: 1}, Progress{WindowID: WindowID(m), Version: "0"}, p, now
}

func TestRetentionAHasNoSigninCountGateAndRespectsFrozen(t *testing.T) {
	m, w, g, p, now := fixture()
	m.Mode = ModeA
	m.Participation = true
	m.Consent = true
	m.ConsentCap = 300
	for _, tc := range []struct {
		balance, frozen int
		want            bool
	}{{299, 0, false}, {300, 0, true}, {300, 1, false}, {1000, 700, true}} {
		w.Balance, w.Frozen = tc.balance, tc.frozen
		d, err := Evaluate(m, w, g, p, false, now)
		if (err == nil) != tc.want {
			t.Fatalf("balance=%d frozen=%d err=%v", tc.balance, tc.frozen, err)
		}
		if err == nil && (d.NewEnd != m.PlaybackEnd+30*Day || d.Cost != 300) {
			t.Fatal("wrong renewal decision")
		}
	}
	m.ConsentCap = 299
	if _, err := Evaluate(m, w, g, p, false, now); !errors.Is(err, ErrConsent) {
		t.Fatal("authorization ceiling bypassed")
	}
}

func TestRetentionSettlementAdvancesOldEndAndWaitsForNextWindow(t *testing.T) {
	m, w, g, p, now := fixture()
	m.Mode = ModeB
	m.Participation = true
	g.Seconds = 1800
	d, err := Evaluate(m, w, g, p, false, now+Day)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := ApplySettlement(m, d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ApplySettlement(updated, d); !errors.Is(err, ErrStale) {
		t.Fatal("same window applied twice")
	}
	if _, err = Evaluate(updated, w, g, p, false, now+2*Day); err == nil {
		t.Fatal("old watch proof reused for another window")
	}
	g.WindowID = WindowID(updated)
	g.Seconds = 0
	if _, err = Evaluate(updated, w, g, p, false, now+2*Day); err == nil {
		t.Fatal("future window settled before it starts")
	}
}

func TestRetentionOneChoicePerWindowAndFreshChoiceAfterRenewal(t *testing.T) {
	m, w, g, p, now := fixture()
	q, err := Preview(m, w, g, p, ModeA, now)
	if err != nil {
		t.Fatal(err)
	}
	m, err = Confirm(m, w, g, p, q, false, true, 300, now)
	if err != nil {
		t.Fatal(err)
	}
	if m.SelectedWindow != WindowID(m) {
		t.Fatal("choice not scoped to current window")
	}
	for _, target := range []string{ModeA, ModeB} {
		if _, err = Preview(m, w, g, p, target, now+20*Day); !errors.Is(err, ErrSelected) {
			t.Fatal("second choice accepted", target, err)
		}
	}
	decision, err := Evaluate(m, w, g, p, false, now+1)
	if err != nil {
		t.Fatal(err)
	}
	m, err = ApplySettlement(m, decision)
	if err != nil {
		t.Fatal(err)
	}
	if m.Participation || m.Consent || m.Mode != "" {
		t.Fatal("old debit authorization carried into next window")
	}
	if Selectable(m, p, now+2) {
		t.Fatal("future window opened early")
	}
	g = Progress{WindowID: WindowID(m)}
	q, err = Preview(m, w, g, p, ModeB, decision.OldEnd+1)
	if err != nil {
		t.Fatal("next window did not reopen", err)
	}
	m, err = Confirm(m, w, g, p, q, false, false, 0, decision.OldEnd+1)
	if err != nil || m.Mode != ModeB {
		t.Fatal("next window could not select B", err)
	}
	if _, err = Preview(m, w, g, p, ModeA, decision.OldEnd+2); !errors.Is(err, ErrSelected) {
		t.Fatal("new window was not locked", err)
	}
}

func TestRetentionRiskPreviewCancelExpiryAndVersionChanges(t *testing.T) {
	m, w, g, p, now := fixture()
	m.PlaybackEnd = now + 2*Day
	g.WindowID = WindowID(m)
	m.Mode = ModeA
	m.Participation = true
	g.Seconds = 720
	quote, err := Preview(m, w, g, p, ModeB, now)
	if err != nil || !quote.Risk || quote.WatchShortfall != 1080 {
		t.Fatal("missing real target shortfall")
	}
	if _, err = Confirm(m, w, g, p, quote, false, false, 0, now); !errors.Is(err, ErrRisk) {
		t.Fatal("risk acknowledgement missing")
	}
	if _, err = Confirm(m, w, g, p, quote, true, false, 0, now+120); !errors.Is(err, ErrStale) {
		t.Fatal("expired ticket accepted")
	}
	w.Version++
	if _, err = Confirm(m, w, g, p, quote, true, false, 0, now); !errors.Is(err, ErrStale) {
		t.Fatal("wallet change did not invalidate preview")
	}
	w.Version--
	g.Version = "new-proof"
	if _, err = Confirm(m, w, g, p, quote, true, false, 0, now); !errors.Is(err, ErrStale) {
		t.Fatal("progress change did not invalidate preview")
	}
	g.Version = "0"
	updated, err := Confirm(m, w, g, p, quote, true, false, 0, now+1)
	if err != nil || updated.Mode != ModeB || updated.PlaybackEnd != m.PlaybackEnd {
		t.Fatal("confirmed switch changed playback time")
	}
}

func TestRetentionCrossingRiskBoundaryRequiresNewPreview(t *testing.T) {
	m, w, g, p, now := fixture()
	m.PlaybackEnd = now + 3*Day + 1
	g.WindowID = WindowID(m)
	quote, err := Preview(m, w, g, p, ModeB, now)
	if err != nil || quote.Risk {
		t.Fatal("unexpected pre-boundary risk")
	}
	if _, err = Confirm(m, w, g, p, quote, true, false, 0, now+2); !errors.Is(err, ErrStale) {
		t.Fatal("ordinary ticket crossed into risk period")
	}
}

func TestRetentionExemptionTailIsStrictlyLessThanThirtyDaysAndOptIn(t *testing.T) {
	m, w, g, p, now := fixture()
	m.ExemptEnd = m.PlaybackEnd
	if Selectable(m, p, now) {
		t.Fatal("exactly thirty-day exemption offered tail participation")
	}
	if !Selectable(m, p, now+1) {
		t.Fatal("tail unavailable below thirty days")
	}
	if _, err := Evaluate(m, w, g, p, false, now+1); err == nil {
		t.Fatal("exemption silently enabled activity")
	}
	m.Permanent = true
	if Selectable(m, p, now+1) {
		t.Fatal("permanent exemption has a tail")
	}
}

func TestRetentionLateARequiresProofOrExplicitRescueAndUsesOldEnd(t *testing.T) {
	m, w, g, p, now := fixture()
	m.Mode = ModeA
	m.Participation = true
	m.Consent = true
	m.ConsentCap = 300
	if _, err := Evaluate(m, w, g, p, false, m.PlaybackEnd+1); err == nil {
		t.Fatal("late wallet silently charged")
	}
	d, err := Evaluate(m, w, g, p, true, m.PlaybackEnd+1)
	if err != nil || d.NewEnd != m.PlaybackEnd+30*Day || !d.Rescue {
		t.Fatal("rescue did not preserve original deadline")
	}
	g.QualifiedAt = m.PlaybackEnd - 1
	if _, err = Evaluate(m, w, g, p, false, m.PlaybackEnd+Day-1); err != nil {
		t.Fatal("server-accepted proof rejected during closing")
	}
	if _, err = Evaluate(m, w, g, p, false, m.PlaybackEnd+Day); err == nil {
		t.Fatal("automatic closing exceeded 24 hours")
	}
	if _, err = Evaluate(m, w, g, p, true, m.PlaybackEnd+7*Day); err == nil {
		t.Fatal("rescue exceeded original seven-day boundary")
	}
	if AccessUntil(m, p, true) != m.PlaybackEnd+7*Day || AccessUntil(m, p, false) != m.PlaybackEnd {
		t.Fatal("closing and rescue stacked to eight days")
	}
	m.HoldUntil = now + 2*Day
	if _, err = Evaluate(m, w, g, p, false, now+1); err == nil {
		t.Fatal("incident hold ignored")
	}
}

func TestRetentionCardPausesParticipationWithoutLosingTimeOrCooldown(t *testing.T) {
	m, _, _, _, now := fixture()
	m.PlaybackEnd = now + 10*Day
	m.NextSwitchAt = now + 5*Day
	m.Mode = ModeA
	m.Participation = true
	m.Consent = true
	m.ConsentCap = 300
	updated, err := AddExemption(m, 30*Day, now)
	if err != nil || updated.PlaybackEnd != now+40*Day || updated.ExemptEnd != updated.PlaybackEnd || updated.Participation || updated.Consent || updated.Mode != "" || updated.SelectedWindow != "" {
		t.Fatal("card corrupted retained time or retained old authorization")
	}
}

func TestRetentionOrganicCapDoesNotDeleteBalanceOrRerollZero(t *testing.T) {
	for _, tc := range []struct{ balance, raw, want int }{{3595, 16, 5}, {3600, 16, 0}, {4000, 16, 0}, {0, 10, 10}} {
		if got := OrganicAward(tc.balance, tc.raw, 3600); got != tc.want {
			t.Fatalf("award=%d want=%d", got, tc.want)
		}
	}
	p := DefaultPolicy()
	p.OrganicCap = 900
	if p.Validate() == nil {
		t.Fatal("unreachable year-card price accepted")
	}
}
