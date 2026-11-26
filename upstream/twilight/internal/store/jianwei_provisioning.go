package store

import "time"

// Provisioning starts the sold duration when the account is actually created,
// and records that transition separately from the earlier code redemption.
func (s *Store) completeCommerceProvisioningLocked(before, after User) {
	if !before.PendingEmby || before.EmbyID != "" || after.EmbyID == "" || after.PendingEmby || before.RegistrationCode == "" {
		return
	}
	for id, o := range s.state.ShopOrders {
		if o.UID != after.UID || o.CodeKey != before.RegistrationCode || o.RedeemedAt <= 0 || o.Status != "issued" {
			continue
		}
		origin, found := s.state.Finance.Sources["code:"+o.CodeKey]
		if !found {
			return
		}
		base := before
		base.ExpiredAt = time.Now().Unix()
		base.Role = origin.RoleBefore
		source := "provision:" + id
		s.recordEntitlementLocked(source, origin.Kind, base, after, time.Now().Unix())
		o.EntitlementSourceID = source
		s.state.ShopOrders[id] = o
		return
	}
}
