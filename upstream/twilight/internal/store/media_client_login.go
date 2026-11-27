package store

import (
	"time"
)

type MediaClientLogin struct {
	EmbyID    string `json:"emby_id"`
	DeviceID  string `json:"device_id"`
	ExpiresAt int64  `json:"expires_at"`
}

func (s *Store) RememberMediaClientLogin(hash, id, device string) error {
	if len(hash) != 64 || id == "" || len(id) > 128 || len(device) > 200 {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutateAndSaveLocked(func() error {
		now := time.Now().Unix()
		if s.state.MediaClientLogins == nil {
			s.state.MediaClientLogins = map[string]MediaClientLogin{}
		}
		for k, v := range s.state.MediaClientLogins {
			if v.ExpiresAt <= now {
				delete(s.state.MediaClientLogins, k)
			}
		}
		if len(s.state.MediaClientLogins) >= 4096 {
			return ErrConflict
		}
		s.state.MediaClientLogins[hash] = MediaClientLogin{EmbyID: id, DeviceID: device, ExpiresAt: now + 90*86400}
		return nil
	})
}
func (s *Store) MediaClientLogin(hash string) (MediaClientLogin, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.state.MediaClientLogins[hash]
	return v, ok && v.ExpiresAt > time.Now().Unix()
}
