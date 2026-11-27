package store

import "github.com/prejudice-studio/twilight/internal/retention"

// MemberViewing is a read-only projection of authenticated Emby session
// samples. It never extrapolates to the present or uses login/poll timestamps
// as viewing evidence. It is independent of the member's entitlement window.
type MemberViewing struct {
	Available        bool   `json:"available"`
	WindowStart      int64  `json:"window_start"`
	WindowEnd        int64  `json:"window_end"`
	Seconds          int64  `json:"seconds"`
	HasWindowRecords bool   `json:"has_window_records"`
	LastWatchedAt    int64  `json:"last_watched_at"`
	LastPoll         int64  `json:"last_poll"`
	Quality          string `json:"quality"`
	Source           string `json:"source"`
}

func (s *Store) MemberViewing(uid, now int64) (MemberViewing, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, exists := s.state.Users[uid]; !exists || now <= 0 {
		return MemberViewing{}, ErrNotFound
	}
	return memberViewing(s.state.Retention, uid, now), nil
}

func memberViewing(state RetentionState, uid, now int64) MemberViewing {
	start := max(int64(0), now-30*retention.Day)
	last := state.LastWatchAt[uid]
	if last < 0 || last > now {
		last = 0
	}
	items, present := state.ViewingWatches[uid]
	if !present {
		// Older snapshots have only the already verified eligibility intervals.
		items = state.Watches[uid]
	}
	valid := make([]RetentionInterval, 0, len(items))
	for _, item := range items {
		if item.Start < 0 || item.End <= item.Start || item.End > now {
			continue
		}
		last = max(last, item.End)
		valid = append(valid, item)
	}
	merged, seconds := unionRetentionIntervals(valid, start, now)
	quality := state.Quality
	if !state.Policy.Enabled {
		quality = "disabled"
	} else if state.LastPoll <= 0 || state.LastPoll > now {
		quality = "waiting"
	} else if now-state.LastPoll > 90 {
		quality = "stale"
	}
	poll := state.LastPoll
	if poll < 0 || poll > now {
		poll = 0
	}
	return MemberViewing{Available: true, WindowStart: start, WindowEnd: now, Seconds: seconds, HasWindowRecords: len(merged) > 0, LastWatchedAt: last, LastPoll: poll, Quality: quality, Source: "emby_server_session_samples"}
}
