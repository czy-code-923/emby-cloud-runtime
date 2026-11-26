package store

import "time"

func (s *Store) ClaimShopDelivery() (ShopOrder, int64, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result ShopOrder
	var telegramID int64
	found := false
	err := s.mutateAndSaveLocked(func() error {
		result = ShopOrder{}
		found = false
		telegramID = 0
		now := time.Now().Unix()
		for id, o := range s.state.ShopOrders {
			u, ok := s.state.Users[o.UID]
			if !ok || u.TelegramID <= 0 || o.Status != "issued" || o.NotifiedAt > 0 || o.NotifyLeaseUntil > now || o.CodeCipher == "" {
				continue
			}
			o.NotifyLeaseUntil = now + 120
			s.state.ShopOrders[id] = o
			result = o
			telegramID = u.TelegramID
			found = true
			return nil
		}
		return errNoChange
	})
	return result, telegramID, found, err
}
func (s *Store) MarkShopDelivered(id string, uid int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutateAndSaveLocked(func() error {
		o, ok := s.state.ShopOrders[id]
		if !ok || o.UID != uid || o.Status != "issued" {
			return ErrConflict
		}
		if o.NotifiedAt > 0 {
			return errNoChange
		}
		o.NotifiedAt = time.Now().Unix()
		o.NotifyLeaseUntil = 0
		s.state.ShopOrders[id] = o
		return nil
	})
}
