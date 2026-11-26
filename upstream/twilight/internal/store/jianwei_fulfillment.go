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
			statusAllowed := o.Status == "issued" || o.Status == "paid_hold" || o.Status == "refund_pending" || o.Status == "refunded"
			alreadyNotified := o.NotifiedStatus == o.Status || o.NotifiedStatus == "" && o.NotifiedAt > 0 && o.Status == "issued"
			if !ok || u.TelegramID <= 0 || !statusAllowed || alreadyNotified || o.NotifyLeaseUntil > now || o.Status == "issued" && (o.CodeCipher == "" || o.RedeemedAt > 0) {
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
func (s *Store) MarkShopDelivered(id string, uid int64, expectedStatus ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutateAndSaveLocked(func() error {
		o, ok := s.state.ShopOrders[id]
		if !ok || o.UID != uid || len(expectedStatus) > 0 && o.Status != expectedStatus[0] {
			return ErrConflict
		}
		if o.NotifiedStatus == o.Status {
			return errNoChange
		}
		o.NotifiedStatus = o.Status
		o.NotifiedAt = time.Now().Unix()
		o.NotifyLeaseUntil = 0
		s.state.ShopOrders[id] = o
		return nil
	})
}
