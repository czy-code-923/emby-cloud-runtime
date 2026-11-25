package store

import "strings"

type MediaDevice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ClaimMediaDevice serializes enrollment across API workers in the same state
// CAS transaction as user/role changes. Known devices do not cause writes.
func (s *Store) ClaimMediaDevice(uid int64, id, name string, limitFor func(User) int) error {
	if id == "" || len(id) > 200 || len(name) > 200 {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutateAndSaveLocked(func() error {
		u, found := s.state.Users[uid]
		if !found || !u.Active {
			return ErrNotFound
		}
		for _, revoked := range u.MediaRevokedDevices {
			if revoked == id {
				return ErrConflict
			}
		}
		limit := limitFor(u)
		if limit <= 0 {
			return errNoChange
		}
		for index, device := range u.MediaDevices {
			if device.ID == id {
				if index < limit {
					return errNoChange
				}
				return ErrConflict
			}
		}
		if len(u.MediaDevices) >= limit {
			return ErrConflict
		}
		u.MediaDevices = append(append([]MediaDevice{}, u.MediaDevices...), MediaDevice{ID: id, Name: strings.TrimSpace(name)})
		s.state.Users[uid] = u
		return nil
	})
}

func (s *Store) RemoveMediaDevice(uid int64, id string) error {
	_, err := s.UpdateUser(uid, func(u *User) error {
		if len(u.MediaRevokedDevices) >= 1000 {
			return ErrConflict
		}
		u.MediaRevokedDevices = append(append([]string{}, u.MediaRevokedDevices...), id)
		items := make([]MediaDevice, 0, len(u.MediaDevices))
		for _, device := range u.MediaDevices {
			if device.ID != id {
				items = append(items, device)
			}
		}
		u.MediaDevices = items
		return nil
	})
	return err
}
