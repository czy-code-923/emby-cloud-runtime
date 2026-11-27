package store

import (
	"encoding/json"
	"fmt"
	"github.com/prejudice-studio/twilight/internal/retention"
	"sync"
	"testing"
	"time"
)

func retentionFixture(t *testing.T) (*Store, User, int64) {
	t.Helper()
	s, e := Open("")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = s.Close() })
	now := time.Now().Unix()
	u, e := s.CreateUser(User{Username: "retention-fixture", Role: RoleNormal, Active: true, EmbyID: "retention-emby", TelegramID: 886621, ExpiredAt: now + 86400})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.SetRetentionEnabled(true, 1); e != nil {
		t.Fatal(e)
	}
	return s, u, now
}
func TestJianweiRetentionAtomicSigninSettlementAndFutureWindow(t *testing.T) {
	s, u, now := retentionFixture(t)
	s.mu.Lock()
	e := s.mutateAndSaveLocked(func() error { s.state.Signin[u.UID] = Signin{UID: u.UID, Points: 299}; return nil })
	s.mu.Unlock()
	if e != nil {
		t.Fatal(e)
	}
	q, e := s.RetentionPreview(u.UID, "A", "quote-a", now)
	if e != nil || !q.Selection.Risk {
		t.Fatal("near-expiry risk preview missing", e)
	}
	if e = s.RetentionConfirm(u.UID, "quote-a", false, true, 300, now); e != retention.ErrRisk {
		t.Fatal("risk bypass", e)
	}
	if e = s.RetentionConfirm(u.UID, "quote-a", true, true, 300, now); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, e := s.AddSignin(u.UID, 1); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	v, e := s.RetentionView(u.UID, now+1)
	if e != nil || v.End != u.ExpiredAt+30*86400 || v.Balance != 0 || len(v.Ledger) != 1 {
		t.Fatalf("atomic renewal: %+v %v", v, e)
	}
	if e = s.EvaluateRetention(u.UID, now+2, false); e != nil {
		t.Fatal(e)
	}
	if len(s.state.Retention.Settlements) != 1 {
		t.Fatal("same/future window settled twice")
	}
	if _, e = s.RetentionPreview(u.UID, "B", "quote-b", now+3); e == nil {
		t.Fatal("cooldown bypass", e)
	}
	if e = s.RetentionWithdraw(u.UID); e != ErrRetentionModeLocked {
		t.Fatal(e)
	}
	if _, e = s.RetentionPreview(u.UID, "B", "quote-b", now+4); e == nil {
		t.Fatal("withdraw reset cooldown", e)
	}
	oldNext := v.Member.Member.NextSwitchAt
	if _, e = s.UpdateUser(u.UID, func(user *User) error { user.ExpiredAt += 30 * 86400; return nil }); e != nil {
		t.Fatal(e)
	}
	v, _ = s.RetentionView(u.UID, now+5)
	if v.Member.Member.Participation || v.Member.Member.Consent || v.Member.Member.NextSwitchAt != oldNext || v.Member.Member.ExemptEnd != v.End {
		t.Fatal("external card revived authorization or reset cooldown")
	}
}
func TestJianweiRetentionTrustedSamplingUnionSeeksAndWindows(t *testing.T) {
	s, u, now := retentionFixture(t)
	if _, e := s.RetentionPreview(u.UID, "B", "quote-b", now); e != nil {
		t.Fatal(e)
	}
	if e := s.RetentionConfirm(u.UID, "quote-b", true, false, 0, now); e != nil {
		t.Fatal(e)
	}
	for seconds := int64(0); seconds <= 1800; seconds += 30 {
		observations := []RetentionObservation{}
		for device := 0; device < 2; device++ {
			observations = append(observations, RetentionObservation{UID: u.UID, Session: fmt.Sprint(device), Item: "movie", Playing: true, Position: seconds})
		}
		if e := s.ObserveRetention(observations, now+seconds); e != nil {
			t.Fatal(e)
		}
		if seconds == 90 {
			v, _ := s.RetentionView(u.UID, now+seconds)
			if v.Seconds != 90 {
				t.Fatal("verified short segments were not accumulated")
			}
		}
		if seconds == 900 {
			v, _ := s.RetentionView(u.UID, now+seconds)
			if v.Seconds != 900 {
				t.Fatalf("overlap doubled: %d", v.Seconds)
			}
		}
	}
	v, _ := s.RetentionView(u.UID, now+1801)
	if len(v.Ledger) != 1 || v.End != u.ExpiredAt+30*86400 || v.Balance != 0 || v.Seconds != 0 {
		t.Fatalf("B window reused or billed: %+v", v)
	}
	intervals := s.state.Retention.Watches[u.UID]
	_, seconds := unionRetentionIntervals(intervals, now, now+1801)
	if seconds != 1800 {
		t.Fatalf("trusted union %d", seconds)
	}
	if e := s.ObserveRetention([]RetentionObservation{{UID: u.UID, Session: "0", Item: "movie", Playing: true, Position: 999999}}, now+1830); e != nil {
		t.Fatal(e)
	}
	_, after := unionRetentionIntervals(s.state.Retention.Watches[u.UID], now, now+1831)
	if after != 1800 {
		t.Fatal("seek credited")
	}
}

func TestJianweiRetentionCollectionQualityExpiresWithoutFabricatingProgress(t *testing.T) {
	s, u, now := retentionFixture(t)
	v, err := s.RetentionView(u.UID, now)
	if err != nil || v.Quality != "waiting" {
		t.Fatal("unsampled account must wait", err)
	}
	if err = s.ObserveRetention(nil, now); err != nil {
		t.Fatal(err)
	}
	v, _ = s.RetentionView(u.UID, now+30)
	if v.Quality != "server_session_samples" || v.Seconds != 0 {
		t.Fatal("an empty healthy poll must not fabricate viewing")
	}
	v, _ = s.RetentionView(u.UID, now+91)
	if v.Quality != "stale" || v.Seconds != 0 {
		t.Fatal("missing reports did not become stale")
	}
	if err = s.SetRetentionEnabled(false, s.RetentionPolicy().Revision); err != nil {
		t.Fatal(err)
	}
	v, _ = s.RetentionView(u.UID, now+92)
	if v.Quality != "disabled" {
		t.Fatal("disabled collection shown as current")
	}
}
func TestJianweiRetentionOrganicCapIdempotency(t *testing.T) {
	s, u, _ := retentionFixture(t)
	s.mu.Lock()
	e := s.mutateAndSaveLocked(func() error { s.state.Signin[u.UID] = Signin{UID: u.UID, Points: 3595}; return nil })
	s.mu.Unlock()
	if e != nil {
		t.Fatal(e)
	}
	first, created, e := s.AddSignin(u.UID, 20)
	if e != nil || !created || first.Points != 3600 {
		t.Fatal("reward cap", e)
	}
	if first.Records[0].RawAward != 20 || first.Records[0].CapWithheld != 15 {
		t.Fatal("raw/actual award lost")
	}
	again, created, e := s.AddSignin(u.UID, 20)
	if e != nil || created || again.Points != 3600 {
		t.Fatal("daily replay re-awarded", e)
	}
}

func TestJianweiRetentionCardsAndRescueConsent(t *testing.T) {
	s, u, now := retentionFixture(t)
	s.mu.Lock()
	e := s.mutateAndSaveLocked(func() error { s.state.Signin[u.UID] = Signin{UID: u.UID, Points: 1000}; return nil })
	s.mu.Unlock()
	if e != nil {
		t.Fatal(e)
	}
	q, e := s.RetentionPreview(u.UID, "month", "card-one", now)
	if e != nil || q.Selection.Cost != 300 {
		t.Fatal(e)
	}
	if e = s.RetentionConfirm(u.UID, "card-one", false, false, 0, now, false); e != retention.ErrConsent {
		t.Fatal("purchase confirmation bypass", e)
	}
	if e = s.RetentionConfirm(u.UID, "card-one", false, false, 0, now, true); e != nil {
		t.Fatal(e)
	}
	if e = s.RetentionConfirm(u.UID, "card-one", false, false, 0, now+1, true); e != nil {
		t.Fatal("purchase retry", e)
	}
	v, _ := s.RetentionView(u.UID, now+1)
	if v.Balance != 700 || v.End != u.ExpiredAt+30*86400 || v.Member.Member.Participation {
		t.Fatal("card reset or repeated spend")
	}
	// Exactly thirty days of exemption is not an eligible tail window.
	if v.Selectable {
		t.Fatal("card silently opted in")
	}
	if _, e = s.RetentionPreview(u.UID, "A", "tail", now+86402); e != nil {
		t.Fatal(e)
	}
	if e = s.RetentionConfirm(u.UID, "tail", true, true, 300, now+86402); e != nil {
		t.Fatal(e)
	}
}

func TestJianweiRetentionBackupRejectsCorruptionAndDropsPendingConfirmations(t *testing.T) {
	s, u, now := retentionFixture(t)
	if _, e := s.RetentionPreview(u.UID, "B", "before-import", now); e != nil {
		t.Fatal(e)
	}
	s.mu.RLock()
	snapshot, e := s.snapshotStateLocked()
	s.mu.RUnlock()
	if e != nil {
		t.Fatal(e)
	}
	var state State
	if e = json.Unmarshal(snapshot.data, &state); e != nil {
		t.Fatal(e)
	}
	if e = ValidateRetentionBackup(&state); e != nil {
		t.Fatal(e)
	}
	if len(state.Retention.Quotes) != 0 || len(state.Retention.Samples) != 0 {
		t.Fatal("transient confirmation survived import")
	}
	r := state.Retention.Members[u.UID]
	r.Member.PlaybackEnd++
	state.Retention.Members[u.UID] = r
	if e = ValidateRetentionBackup(&state); e == nil {
		t.Fatal("mismatched entitlement imported")
	}
}
