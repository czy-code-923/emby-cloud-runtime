package store

import (
	"encoding/json"
	"testing"

	"github.com/prejudice-studio/twilight/internal/retention"
)

func TestJianweiViewingRollingUnionDoesNotUsePollLoginOrOtherUser(t *testing.T) {
	now := int64(1800000000)
	start := now - 30*retention.Day
	state := RetentionState{Policy: retention.DefaultPolicy(), LastPoll: now, Quality: "server_session_samples", Watches: map[int64][]RetentionInterval{1: {{Start: start - 30, End: start + 30}, {Start: start, End: start + 60}, {Start: now - 90, End: now - 30}}, 2: {{Start: now - 10000, End: now}}}}
	state.Policy.Enabled = true
	v := memberViewing(state, 1, now)
	if v.Seconds != 120 || v.LastWatchedAt != now-30 || v.LastPoll != now || v.WindowStart != start || v.WindowEnd != now || !v.HasWindowRecords {
		t.Fatal("rolling union, owner or last-viewing evidence is wrong", v)
	}
	v = memberViewing(state, 3, now)
	if v.HasWindowRecords || v.Seconds != 0 || v.LastWatchedAt != 0 {
		t.Fatal("empty account inherited viewing or last poll time", v)
	}
	state.LastWatchAt = map[int64]int64{1: now + 100}
	state.Watches[1] = []RetentionInterval{{Start: now - 10, End: now + 10}, {Start: -100, End: now}, {Start: now, End: now}}
	v = memberViewing(state, 1, now)
	if v.HasWindowRecords || v.LastWatchedAt != 0 || v.Seconds != 0 {
		t.Fatal("future or invalid evidence fabricated viewing", v)
	}
}

func TestJianweiViewingSamplesExcludeBufferPauseSeekAndStaleGap(t *testing.T) {
	s, u, now := retentionFixture(t)
	observe := func(at, position int64, playing bool) {
		t.Helper()
		if err := s.ObserveRetention([]RetentionObservation{{UID: u.UID, Session: "fixture", Item: "movie", Position: position, Playing: playing}}, now+at); err != nil {
			t.Fatal(err)
		}
	}
	observe(0, 0, true)
	v, _ := s.MemberViewing(u.UID, now)
	if v.LastWatchedAt != 0 || v.HasWindowRecords {
		t.Fatal("first unconfirmed sample was reported as viewing")
	}
	observe(30, 20, true)
	observe(60, 20, false)
	observe(90, 50, true)
	observe(120, 80, true)
	observe(150, 100000, true)
	observe(300, 100150, true)
	v, _ = s.MemberViewing(u.UID, now+300)
	if v.Seconds != 50 || v.LastWatchedAt != now+120 || v.LastPoll != now+300 {
		t.Fatal("buffer, pause, seek or missing reports inflated viewing", v)
	}
	// Eligibility settlement may consume a rescue interval. That must not
	// erase the same person's factual viewing history from their account.
	s.mu.Lock()
	err := s.mutateAndSaveLocked(func() error {
		s.state.Retention.Watches[u.UID] = nil
		return nil
	})
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	v, _ = s.MemberViewing(u.UID, now+300)
	if v.Seconds != 50 || v.LastWatchedAt != now+120 {
		t.Fatal("retention settlement erased factual viewing", v)
	}
	if _, err := s.MemberViewing(u.UID+10000, now); err != ErrNotFound {
		t.Fatal("unknown user summary was not rejected", err)
	}
	if err := s.ObserveRetention(nil, now+38*retention.Day); err != nil {
		t.Fatal(err)
	}
	v, _ = s.MemberViewing(u.UID, now+38*retention.Day)
	if v.LastWatchedAt != now+120 || v.HasWindowRecords || v.Seconds != 0 {
		t.Fatal("last actual viewing time was replaced by an empty poll or trimmed", v)
	}
	raw, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	var state State
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRetentionBackup(&state); err != nil {
		t.Fatal(err)
	}
	if state.Retention.LastWatchAt[u.UID] != now+120 || state.Retention.LastPoll != 0 {
		t.Fatal("backup confused viewing history with transient collection state")
	}
}

func TestJianweiViewingCollectionQualityDoesNotInventMissingRecords(t *testing.T) {
	now := int64(1800000000)
	state := RetentionState{Policy: retention.DefaultPolicy()}
	state.Policy.Enabled = true
	if v := memberViewing(state, 1, now); v.Quality != "waiting" || v.LastWatchedAt != 0 {
		t.Fatal(v)
	}
	state.LastPoll = now - 91
	if v := memberViewing(state, 1, now); v.Quality != "stale" || v.LastWatchedAt != 0 {
		t.Fatal(v)
	}
	state.Policy.Enabled = false
	if v := memberViewing(state, 1, now); v.Quality != "disabled" || v.LastWatchedAt != 0 {
		t.Fatal(v)
	}
}
