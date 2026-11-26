package store

import (
	"reflect"
	"sort"
	"strings"
)

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
		for index, device := range u.MediaDevices {
			if device.ID == id {
				if limit <= 0 || index < limit {
					return errNoChange
				}
				return ErrConflict
			}
		}
		if (limit > 0 && len(u.MediaDevices) >= limit) || len(u.MediaDevices) >= 1000 {
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

// Normalize only mappings verified against the current Emby account's device
// list. Preserve revocations, collapse duplicates and never grant a new slot.
func (s *Store) NormalizeMediaDeviceIDs(uid int64, mappings map[string]string) error {
	if len(mappings) > 1000 {
		return ErrInvalid
	}
	for old, next := range mappings {
		if old == "" || next == "" || len(old) > 200 || len(next) > 200 {
			return ErrInvalid
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutateAndSaveLocked(func() error {
		u, found := s.state.Users[uid]
		if !found {
			return ErrNotFound
		}
		beforeDevices := append([]MediaDevice{}, u.MediaDevices...)
		beforeRevoked := append([]string{}, u.MediaRevokedDevices...)
		revoked := map[string]bool{}
		for _, id := range u.MediaRevokedDevices {
			revoked[id] = true
			if next := mappings[id]; next != "" {
				revoked[next] = true
			}
		}
		if len(revoked) > 1000 {
			return ErrConflict
		}
		u.MediaRevokedDevices = []string{}
		for id := range revoked {
			u.MediaRevokedDevices = append(u.MediaRevokedDevices, id)
		}
		sort.Strings(u.MediaRevokedDevices)
		u.MediaDevices = []MediaDevice{}
		seen := map[string]bool{}
		for _, d := range beforeDevices {
			if next := mappings[d.ID]; next != "" {
				d.ID = next
			}
			if !seen[d.ID] && !revoked[d.ID] {
				u.MediaDevices = append(u.MediaDevices, d)
				seen[d.ID] = true
			}
		}
		if reflect.DeepEqual(beforeDevices, u.MediaDevices) && reflect.DeepEqual(beforeRevoked, u.MediaRevokedDevices) {
			return errNoChange
		}
		s.state.Users[uid] = u
		return nil
	})
}
