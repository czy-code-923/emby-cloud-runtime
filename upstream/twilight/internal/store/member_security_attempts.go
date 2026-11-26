package store

import "time"

type MemberSecurityAttempts struct {
	Failures    int   `json:"failures,omitempty"`
	WindowStart int64 `json:"window_start,omitempty"`
	LockedUntil int64 `json:"locked_until,omitempty"`
}

// A single persisted UID boundary covers Bot and Mini App processes. Correct
// proofs do not consume attempts; a stale hash cannot authorize a new secret.
func (s *Store) CheckMemberSecurityResult(uid int64, observedHash string, success bool, now int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var retry int64
	err := s.mutateAndSaveLocked(func() error {
		retry = 0
		u, ok := s.state.Users[uid]
		if !ok {
			return ErrNotFound
		}
		if u.SecurityCodeHash != observedHash {
			return ErrConflict
		}
		v := u.MemberSecurityAttempts
		if v.LockedUntil > now {
			retry = v.LockedUntil - now
			return errNoChange
		}
		if success {
			if v.Failures == 0 && v.LockedUntil == 0 {
				return errNoChange
			}
			u.MemberSecurityAttempts = MemberSecurityAttempts{}
		} else {
			if v.WindowStart <= 0 || now-v.WindowStart >= int64(5*time.Minute/time.Second) {
				v = MemberSecurityAttempts{WindowStart: now}
			}
			v.Failures++
			if v.Failures >= 5 {
				v.LockedUntil = now + 300
				retry = 300
			}
			u.MemberSecurityAttempts = v
		}
		s.state.Users[uid] = u
		return nil
	})
	return retry, err
}
