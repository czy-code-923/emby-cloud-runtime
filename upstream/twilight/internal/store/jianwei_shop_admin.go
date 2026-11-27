package store

import "time"

// Claim one administrator delivery at a time. Telegram acceptance is tracked
// per recipient and order revision; retries never approve payment or mint codes.
func (s *Store) ClaimShopAdminNotice() (ShopOrder, int64, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var order ShopOrder
	var tg int64
	found := false
	err := s.mutateAndSaveLocked(func() error {
		order = ShopOrder{}
		tg = 0
		found = false
		now := time.Now().Unix()
		for id, o := range s.state.ShopOrders {
			if o.Status != "review" && o.Status != "hold" && o.Status != "paid_hold" || o.AdminNotifyLeaseUntil > now {
				continue
			}
			for _, admin := range s.state.Users {
				if !admin.Active || admin.Role != RoleAdmin || admin.TelegramID <= 0 || o.AdminNotified[admin.TelegramID] >= o.Revision {
					continue
				}
				o.AdminNotifyLeaseUntil = now + 90
				s.state.ShopOrders[id] = o
				order, tg, found = o, admin.TelegramID, true
				return nil
			}
		}
		return errNoChange
	})
	return order, tg, found, err
}

func (s *Store) MarkShopAdminNotified(id string, revision, tg int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutateAndSaveLocked(func() error {
		o, exists := s.state.ShopOrders[id]
		if !exists || o.Revision != revision {
			return ErrConflict
		}
		if o.AdminNotified == nil {
			o.AdminNotified = map[int64]int64{}
		}
		o.AdminNotified[tg] = revision
		o.AdminNotifyLeaseUntil = 0
		s.state.ShopOrders[id] = o
		return nil
	})
}

func (s *Store) ShopEligibleProductIDs(uid int64) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, exists := s.state.Users[uid]
	ids := []string{}
	if !exists || !u.Active {
		return ids
	}
	for _, p := range s.state.Shop.Products {
		if p.ServiceID != "" && p.Enabled && s.serviceShopEligibleLocked(u, p, time.Now().Unix()) {
			ids = append(ids, p.ID)
		}
	}
	return ids
}
