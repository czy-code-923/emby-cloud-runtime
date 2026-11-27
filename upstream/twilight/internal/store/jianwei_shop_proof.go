package store

import (
	"encoding/hex"
	"strings"
	"time"
)

// A receipt image is private order evidence, never a public commerce asset.
func (s *Store) AttachShopProof(uid int64, id, name string) error {
	leaf := strings.TrimSuffix(strings.TrimSuffix(name, ".png"), ".jpg")
	if len(leaf) != 16 || (name != leaf+".png" && name != leaf+".jpg") {
		return ErrInvalid
	}
	if _, err := hex.DecodeString(leaf); err != nil {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutateAndSaveLocked(func() error {
		o, ok := s.state.ShopOrders[id]
		if !ok || o.UID != uid {
			return ErrNotFound
		}
		if o.Status != "pending" && o.Status != "review" && o.Status != "hold" {
			return ErrConflict
		}
		if o.ProofUploads >= 3 {
			return ErrConflict
		}
		o.PaymentProof = name
		o.ProofUploads++
		o.Revision++
		o.UpdatedAt = time.Now().Unix()
		s.state.ShopOrders[id] = o
		return nil
	})
}
