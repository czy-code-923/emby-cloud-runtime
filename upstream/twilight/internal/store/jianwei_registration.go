package store

import "strings"

// EmbyRegistrationSummary is an aggregate, read-only view of the same pool
// used by EmbyOccupancy. Web-only profiles are never registered Emby users.
type EmbyRegistrationSummary struct {
	Limit      int  `json:"limit"`
	Unlimited  bool `json:"unlimited"`
	Registered int  `json:"registered"`
	Reserved   int  `json:"reserved"`
	Occupied   int  `json:"occupied"`
	Remaining  *int `json:"remaining"`
}

func (s *Store) EmbyRegistrationSummary(limit int, countActive bool) EmbyRegistrationSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v := EmbyRegistrationSummary{Limit: limit, Unlimited: limit <= 0}
	for _, u := range s.state.Users {
		if strings.TrimSpace(u.EmbyID) != "" {
			v.Registered++
		}
	}
	v.Occupied = s.embyOccupancyLocked(EmbyOccupancyOptions{CountActiveUsers: countActive})
	v.Reserved = max(0, v.Occupied-v.Registered)
	if !v.Unlimited {
		remaining := max(0, limit-v.Occupied)
		v.Remaining = &remaining
	}
	return v
}
