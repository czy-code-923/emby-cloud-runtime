package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/prejudice-studio/twilight/internal/retention"
)

// Finance uses the same PostgreSQL state CAS as codes, balances and grants.
// No action rewrites a historical entry or performs an external transfer.
type FinanceState struct {
	Revision        int64                        `json:"revision"`
	Holds           map[int64]int                `json:"holds"`
	Ledger          []WalletEntry                `json:"ledger"`
	Operations      map[string]string            `json:"operations"`
	Incidents       map[string]ServiceIncident   `json:"incidents"`
	Sources         map[string]EntitlementSource `json:"sources"`
	Reconciliations map[string]Reconciliation    `json:"reconciliations"`
}
type WalletEntry struct {
	ID      string `json:"id"`
	UID     int64  `json:"uid"`
	Actor   int64  `json:"actor"`
	Kind    string `json:"kind"`
	Delta   int    `json:"delta"`
	Balance int    `json:"balance"`
	Frozen  int    `json:"frozen"`
	At      int64  `json:"at"`
	Reason  string `json:"reason"`
}
type WalletView struct {
	UID       int64  `json:"uid"`
	Username  string `json:"username"`
	Balance   int    `json:"balance"`
	Frozen    int    `json:"frozen"`
	Available int    `json:"available"`
	Revision  int64  `json:"revision"`
}
type ServiceIncident struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	Reason    string  `json:"reason"`
	Status    string  `json:"status"`
	UIDs      []int64 `json:"uids"`
	Start     int64   `json:"start"`
	End       int64   `json:"end"`
	HoldUntil int64   `json:"hold_until"`
	Days      int     `json:"days"`
	Points    int     `json:"points"`
	Revision  int64   `json:"revision"`
	CreatedAt int64   `json:"created_at"`
	AppliedAt int64   `json:"applied_at"`
	Actor     int64   `json:"actor"`
}
type EntitlementSource struct {
	ServiceID     string `json:"service_id,omitempty"`
	Offset        int64  `json:"offset"` // projection; original Before/After remain unchanged
	Sequence      int64  `json:"sequence"`
	ID            string `json:"id"`
	UID           int64  `json:"uid"`
	Kind          string `json:"kind"`
	Before        int64  `json:"before"`
	After         int64  `json:"after"`
	RoleBefore    int    `json:"role_before"`
	RoleAfter     int    `json:"role_after"`
	At            int64  `json:"at"`
	RevokedAt     int64  `json:"revoked_at"`
	RefundSeconds int64  `json:"refund_seconds"`
}
type Reconciliation struct {
	ID               string   `json:"id"`
	Date             string   `json:"date"`
	Channel          string   `json:"channel"`
	ExpectedReceived int64    `json:"expected_received"`
	ExpectedRefund   int64    `json:"expected_refund"`
	ActualReceived   int64    `json:"actual_received"`
	ActualRefund     int64    `json:"actual_refund"`
	Difference       int64    `json:"difference"`
	OrderIDs         []string `json:"order_ids"`
	Note             string   `json:"note"`
	At               int64    `json:"at"`
	Actor            int64    `json:"actor"`
}

func (f *FinanceState) ensure() {
	if f.Holds == nil {
		f.Holds = map[int64]int{}
	}
	if f.Operations == nil {
		f.Operations = map[string]string{}
	}
	if f.Incidents == nil {
		f.Incidents = map[string]ServiceIncident{}
	}
	if f.Sources == nil {
		f.Sources = map[string]EntitlementSource{}
	}
	if f.Reconciliations == nil {
		f.Reconciliations = map[string]Reconciliation{}
	}
}
func financeDigest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func (s *Store) availablePointsLocked(uid int64) int {
	return max(0, s.state.Signin[uid].Points-s.state.Finance.Holds[uid])
}
func (s *Store) walletLocked(uid int64) retention.Wallet {
	return retention.Wallet{Balance: s.state.Signin[uid].Points, Frozen: s.state.Finance.Holds[uid], Version: s.state.Finance.Revision}
}
func (s *Store) walletEntryLocked(uid, actor int64, id, kind, reason string, delta int) {
	s.state.Finance.ensure()
	found := false
	for _, entry := range s.state.Finance.Ledger {
		if entry.UID == uid {
			found = true
			break
		}
	}
	if !found && s.state.Signin[uid].Points-delta > 0 {
		s.state.Finance.Ledger = append(s.state.Finance.Ledger, WalletEntry{ID: fmt.Sprintf("opening:%d", uid), UID: uid, Kind: "opening", Delta: s.state.Signin[uid].Points - delta, Balance: s.state.Signin[uid].Points - delta, At: time.Now().Unix(), Reason: "升级前既有余额结转"})
	}
	s.state.Finance.Revision++
	s.state.Finance.Ledger = append(s.state.Finance.Ledger, WalletEntry{ID: id, UID: uid, Actor: actor, Kind: kind, Delta: delta, Balance: s.state.Signin[uid].Points, Frozen: s.state.Finance.Holds[uid], At: time.Now().Unix(), Reason: reason})
}
func (s *Store) Wallet(uid int64) WalletView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u := s.state.Users[uid]
	p := s.state.Signin[uid].Points
	f := s.state.Finance.Holds[uid]
	return WalletView{uid, u.Username, p, f, max(0, p-f), s.state.Finance.Revision}
}
func (s *Store) FinanceOverview() (int64, []WalletView, []WalletEntry, []ServiceIncident, []Reconciliation) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	wallets := []WalletView{}
	for _, uid := range s.userUIDs {
		if len(wallets) >= 500 {
			break
		}
		u := s.state.Users[uid]
		p := s.state.Signin[uid].Points
		f := s.state.Finance.Holds[uid]
		wallets = append(wallets, WalletView{uid, u.Username, p, f, max(0, p-f), s.state.Finance.Revision})
	}
	ledger := []WalletEntry{}
	for i := len(s.state.Finance.Ledger) - 1; i >= 0 && len(ledger) < 200; i-- {
		ledger = append(ledger, s.state.Finance.Ledger[i])
	}
	incidents := []ServiceIncident{}
	for _, v := range s.state.Finance.Incidents {
		v.UIDs = cloneSlice(v.UIDs)
		incidents = append(incidents, v)
	}
	sort.Slice(incidents, func(i, j int) bool { return incidents[i].CreatedAt > incidents[j].CreatedAt })
	if len(incidents) > 100 {
		incidents = incidents[:100]
	}
	recs := []Reconciliation{}
	for _, v := range s.state.Finance.Reconciliations {
		v.OrderIDs = cloneSlice(v.OrderIDs)
		recs = append(recs, v)
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].At > recs[j].At })
	if len(recs) > 100 {
		recs = recs[:100]
	}
	return s.state.Finance.Revision, wallets, ledger, incidents, recs
}
func (s *Store) AdjustWallet(uid, actor, revision int64, id, kind, reason string, amount int) error {
	if len(id) < 16 || len(id) > 80 || strings.TrimSpace(reason) == "" || len(reason) > 500 || amount <= 0 || amount > 10000000 {
		return ErrInvalid
	}
	hash := financeDigest([]any{uid, kind, reason, amount})
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutateAndSaveLocked(func() error {
		s.state.Finance.ensure()
		f := &s.state.Finance
		if old, ok := f.Operations[id]; ok {
			if old != hash {
				return ErrConflict
			}
			return errNoChange
		}
		if f.Revision != revision {
			return ErrConflict
		}
		if _, ok := s.state.Users[uid]; !ok {
			return ErrNotFound
		}
		si := s.state.Signin[uid]
		si.UID = uid
		delta := 0
		switch kind {
		case "credit":
			delta = amount
		case "debit":
			if s.availablePointsLocked(uid) < amount {
				return ErrInsufficientPoints
			}
			delta = -amount
		case "freeze":
			if amount > s.availablePointsLocked(uid) {
				return ErrInsufficientPoints
			}
			f.Holds[uid] += amount
		case "release":
			if amount > f.Holds[uid] {
				return ErrConflict
			}
			f.Holds[uid] -= amount
		default:
			return ErrInvalid
		}
		if si.Points+delta < 0 || si.Points+delta > 100000000 {
			return ErrInvalid
		}
		si.Points += delta
		s.state.Signin[uid] = si
		f.Operations[id] = hash
		s.walletEntryLocked(uid, actor, id, kind, reason, delta)
		return nil
	})
}
func (s *Store) CreateIncident(v ServiceIncident, expected int64) error {
	if len(v.ID) < 16 || len(v.ID) > 80 || strings.TrimSpace(v.Title) == "" || len(v.Title) > 120 || strings.TrimSpace(v.Reason) == "" || len(v.Reason) > 1000 || len(v.UIDs) == 0 || len(v.UIDs) > 500 || v.Start <= 0 || v.End < v.Start || v.HoldUntil < time.Now().Unix() || v.HoldUntil > time.Now().Unix()+30*86400 || v.Days < 0 || v.Days > 30 || v.Points < 0 || v.Points > 10000 {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutateAndSaveLocked(func() error {
		s.state.Finance.ensure()
		digest := financeDigest(map[string]any{"id": v.ID, "title": v.Title, "reason": v.Reason, "uids": v.UIDs, "start": v.Start, "end": v.End, "hold_until": v.HoldUntil, "days": v.Days, "points": v.Points, "actor": v.Actor})
		if _, ok := s.state.Finance.Incidents[v.ID]; ok {
			if s.state.Finance.Operations["incident-create:"+v.ID] != digest {
				return ErrConflict
			}
			return errNoChange
		}
		if expected != s.state.Finance.Revision {
			return ErrConflict
		}
		seen := map[int64]bool{}
		for _, uid := range v.UIDs {
			u, ok := s.state.Users[uid]
			if !ok || seen[uid] || u.Role != RoleNormal || u.ExpiredAt < 0 || u.EmbyID == "" || !u.Active || u.EmbyDisabled && !u.EmbyAutoDisabled {
				return ErrInvalid
			}
			seen[uid] = true
		}
		v.Status = "open"
		v.Revision = 1
		v.CreatedAt = time.Now().Unix()
		v.UIDs = cloneSlice(v.UIDs)
		s.state.Retention.ensure()
		for _, uid := range v.UIDs {
			u := s.state.Users[uid]
			r := s.retentionMemberLocked(u)
			r.Member.HoldUntil = max(r.Member.HoldUntil, v.HoldUntil)
			r.Member.Version++
			s.state.Retention.Members[uid] = r
			u.RetentionManaged = true
			u.RetentionAccessUntil = max(retention.AccessUntil(r.Member, r.Policy, r.Member.Participation), v.HoldUntil)
			s.state.Users[uid] = u
		}
		s.state.Finance.Incidents[v.ID] = v
		s.state.Finance.Operations["incident-create:"+v.ID] = digest
		s.state.Finance.Revision++
		return nil
	})
}
func (s *Store) ResolveIncident(id, action string, revision, actor int64, amounts ...int) error {
	if len(amounts) > 0 && (len(amounts) != 2 || amounts[0] < 0 || amounts[0] > 30 || amounts[1] < 0 || amounts[1] > 10000) {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutateAndSaveLocked(func() error {
		s.state.Finance.ensure()
		v, ok := s.state.Finance.Incidents[id]
		if !ok {
			return ErrNotFound
		}
		if action == "compensate" && v.Status == "compensated" && len(amounts) == 2 && (v.Days != amounts[0] || v.Points != amounts[1]) {
			return ErrConflict
		}
		if (action == "compensate" && v.Status == "compensated") || (action == "close" && v.Status == "closed") {
			return errNoChange
		}
		if v.Revision != revision || v.Status != "open" || (action != "compensate" && action != "close") {
			return ErrConflict
		}
		if action == "compensate" && len(amounts) == 2 {
			v.Days = amounts[0]
			v.Points = amounts[1]
		}
		now := time.Now().Unix()
		for _, uid := range v.UIDs {
			u, ok := s.state.Users[uid]
			if !ok {
				continue
			}
			if action == "compensate" {
				if u.Role != RoleNormal || u.ExpiredAt < 0 || !u.Active || u.EmbyDisabled && !u.EmbyAutoDisabled {
					return ErrConflict
				}
				old := u
				if v.Days > 0 {
					u.ExpiredAt = max(u.ExpiredAt, now) + int64(v.Days)*86400
				}
				s.recordEntitlementLocked("incident:"+id+fmt.Sprint(":", uid), "incident", old, u, now)
				si := s.state.Signin[uid]
				si.UID = uid
				si.Points += v.Points
				if si.Points > 100000000 {
					return ErrInvalid
				}
				s.state.Signin[uid] = si
				s.walletEntryLocked(uid, actor, "incident:"+id+fmt.Sprint(":", uid), "compensation", v.Reason, v.Points)
			}
			// Revoke only this incident's hold; another open incident still protects it.
			hold := int64(0)
			for otherID, other := range s.state.Finance.Incidents {
				if otherID != id && other.Status == "open" {
					for _, target := range other.UIDs {
						if target == uid {
							hold = max(hold, other.HoldUntil)
						}
					}
				}
			}
			r := s.retentionMemberLocked(u)
			r.Member.HoldUntil = hold
			r.Member.Version++
			s.state.Retention.Members[uid] = r
			u.RetentionManaged = true
			u.RetentionAccessUntil = max(retention.AccessUntil(r.Member, r.Policy, r.Member.Participation), hold)
			s.state.Users[uid] = u
		}
		v.Status = "closed"
		if action == "compensate" {
			v.Status = "compensated"
		}
		v.Revision++
		v.AppliedAt = now
		v.Actor = actor
		s.state.Finance.Incidents[id] = v
		s.state.Finance.Revision++
		return nil
	})
}
func (s *Store) recordEntitlementLocked(id, kind string, before, after User, now int64) {
	s.state.Finance.ensure()
	if _, ok := s.state.Finance.Sources[id]; ok {
		return
	}
	s.state.Finance.Revision++
	s.state.Finance.Sources[id] = EntitlementSource{ID: id, UID: after.UID, Kind: kind, Before: before.ExpiredAt, After: after.ExpiredAt, RoleBefore: before.Role, RoleAfter: after.Role, At: now, Sequence: s.state.Finance.Revision}
}

// Refund only the unconsumed duration belonging to this exact source. A newer
// unknown administrative edit is a conflict, never an excuse to subtract days.
func (s *Store) refundSourceLocked(o *ShopOrder, now int64) error {
	id := o.EntitlementSourceID
	if id == "" {
		id = "code:" + o.CodeKey
	}
	source, ok := s.state.Finance.Sources[id]
	u, exists := s.state.Users[o.UID]
	if !ok || !exists || source.UID != o.UID || source.RevokedAt > 0 || (source.ServiceID == "" && u.ExpiredAt < 0) {
		return ErrConflict
	}
	if source.ServiceID != "" {
		return s.refundServiceSourceLocked(o, source, now)
	}
	if u.EmbyID == "" && u.PendingEmby && u.RegistrationCode == o.CodeKey {
		u.PendingEmby = false
		u.PendingEmbyDays = nil
		u.ExpiredAt = source.Before
		u.EmbyGrantLocked = false
		u.RegistrationSource = ""
		u.RegistrationCode = ""
		if source.RoleBefore != RoleWhitelist && u.Role == RoleWhitelist {
			u.Role = source.RoleBefore
			u.WhitelistUntil = 0
		}
		source.RevokedAt = now
		s.state.Finance.Sources[id] = source
		s.state.Users[u.UID] = u
		return nil
	}
	latest := source
	for _, v := range s.state.Finance.Sources {
		if v.UID == o.UID && v.ServiceID == "" && v.RevokedAt == 0 && v.Sequence > latest.Sequence {
			latest = v
		}
	}
	if latest.After != u.ExpiredAt {
		return ErrConflict
	}
	start := max(source.Before-source.Offset, source.At)
	remaining := max(int64(0), source.After-source.Offset-max(start, now))
	if remaining > int64(o.Product.Days)*86400 {
		return ErrConflict
	}
	before := u
	removedStart := max(start, now)
	// Rebase later independent grants, preserving their original facts.
	// A subsequent refund must not count their already consumed time again.
	for otherID, other := range s.state.Finance.Sources {
		if other.UID == u.UID && other.ServiceID == "" && other.RevokedAt == 0 && other.Sequence > source.Sequence && other.After > other.Before && other.Kind != "refund" {
			other.Offset += remaining
			s.state.Finance.Sources[otherID] = other
		}
	}
	if u.WhitelistUntil > removedStart {
		u.WhitelistUntil -= min(remaining, u.WhitelistUntil-removedStart)
	}
	u.ExpiredAt = max(now, u.ExpiredAt-remaining)
	if source.RoleAfter == RoleWhitelist && source.RoleBefore != RoleWhitelist && u.Role == RoleWhitelist {
		laterWhite := false
		for _, v := range s.state.Finance.Sources {
			if v.UID == u.UID && v.ID != source.ID && v.RevokedAt == 0 && v.RoleAfter == RoleWhitelist && v.Kind == "code_whitelist" && v.Sequence > source.Sequence {
				laterWhite = true
			}
		}
		if !laterWhite {
			u.Role = RoleNormal
			u.WhitelistUntil = 0
		}
	}
	source.RevokedAt = now
	source.RefundSeconds = remaining
	s.state.Finance.Sources[id] = source
	s.recordEntitlementLocked("refund:"+o.ID, "refund", before, u, now)
	s.state.Users[u.UID] = u
	s.state.Finance.Revision++
	return nil
}

func (s *Store) CreatePointsOrder(uid int64, id, sku, requestKey, codeKey, cipher string, now int64, expectedCost ...int) (ShopOrder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result ShopOrder
	err := s.mutateAndSaveLocked(func() error {
		s.state.Finance.ensure()
		s.state.Retention.ensure()
		for _, o := range s.state.ShopOrders {
			if o.UID == uid && o.RequestKey == requestKey {
				if o.Product.ID != "points-"+sku {
					return ErrConflict
				}
				result = o
				return errNoChange
			}
		}
		u, ok := s.state.Users[uid]
		if !ok || !u.Active || u.EmbyID == "" || !strings.HasPrefix(sku, "svc-") && (u.ExpiredAt < 0 || u.Role == RoleAdmin) || u.EmbyDisabled && !u.EmbyAutoDisabled {
			return ErrEmbyRequired
		}
		if len(requestKey) < 16 || len(requestKey) > 80 || len(id) < 16 || len(codeKey) != 64 || cipher == "" {
			return ErrInvalid
		}
		p := s.state.Retention.Policy
		cost, seconds := retentionCard(p, sku)
		product := ShopProduct{ID: "points-" + sku, Name: map[string]string{"month": "积分月卡", "quarter": "积分季卡", "year": "积分年卡"}[sku], Kind: 2, Days: int(seconds / 86400), Stock: -1, Enabled: true}
		isService := strings.HasPrefix(sku, "svc-")
		if isService {
			var err error
			product, cost, err = s.servicePointsProductLocked(u, sku, now)
			if err != nil {
				return err
			}
		}
		if len(expectedCost) < 1 || len(expectedCost) > 2 || expectedCost[0] != cost || (isService && (len(expectedCost) != 2 || expectedCost[1] != product.Days)) {
			return ErrConflict
		}
		if (!isService && !p.Enabled) || cost <= 0 {
			return ErrInvalid
		}
		if s.availablePointsLocked(uid) < cost {
			return ErrInsufficientPoints
		}
		if _, exists := s.state.ShopOrders[id]; exists {
			return ErrConflict
		}
		if _, exists := s.state.RegCodes[codeKey]; exists {
			return ErrConflict
		}
		result = ShopOrder{ID: id, UID: uid, Revision: 1, RequestKey: requestKey, Product: product, Channel: "points", Status: "issued", CreatedAt: now, UpdatedAt: now, CodeKey: codeKey, CodeCipher: cipher, PointsCost: cost}
		s.state.RegCodes[codeKey] = RegCode{Code: codeKey, ServiceID: product.ServiceID, Type: product.Kind, Days: product.Days, UseCountLimit: 1, Active: true, TargetUID: uid, TargetTelegramID: u.TelegramID, Source: "commerce", CreatedAt: now, CreatedTime: now, ValidityTime: 30 * 24, Note: "shop:" + id}
		si := s.state.Signin[uid]
		si.UID = uid
		si.Points -= cost
		s.state.Signin[uid] = si
		// Issuing a card must not cancel the current window before it is redeemed.
		s.state.ShopOrders[id] = result
		s.walletEntryLocked(uid, uid, "points:"+id, "points_card", "积分卡兑换 "+sku, -cost)
		return nil
	})
	return result, err
}
func (s *Store) ReconciliationPreview(day, channel string) (Reconciliation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.reconciliationLocked(day, channel)
}
func (s *Store) reconciliationLocked(day, channel string) (Reconciliation, error) {
	loc := time.FixedZone("Asia/Shanghai", 8*3600)
	start, err := time.ParseInLocation("2006-01-02", day, loc)
	if err != nil || len(channel) > 60 {
		return Reconciliation{}, ErrInvalid
	}
	end := start.Add(24 * time.Hour).Unix()
	v := Reconciliation{Date: day, Channel: channel, OrderIDs: []string{}}
	for _, o := range s.state.ShopOrders {
		if channel != "" && o.Channel != channel {
			continue
		}
		match := false
		if o.PaidAt >= start.Unix() && o.PaidAt < end {
			v.ExpectedReceived += o.ReceivedCents
			match = true
		}
		if o.RefundAt >= start.Unix() && o.RefundAt < end {
			v.ExpectedRefund += o.ReceivedCents
			match = true
		}
		if match {
			v.OrderIDs = append(v.OrderIDs, o.ID)
		}
	}
	sort.Strings(v.OrderIDs)
	return v, nil
}
func (s *Store) ConfirmReconciliation(v Reconciliation, expected int64) error {
	if len(v.ID) < 16 || len(v.ID) > 80 || v.ActualReceived < 0 || v.ActualRefund < 0 || len(v.Note) > 1000 {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutateAndSaveLocked(func() error {
		s.state.Finance.ensure()
		if old, ok := s.state.Finance.Reconciliations[v.ID]; ok {
			if old.Date != v.Date || old.ActualReceived != v.ActualReceived || old.ActualRefund != v.ActualRefund {
				return ErrConflict
			}
			return errNoChange
		}
		if expected != s.state.Finance.Revision {
			return ErrConflict
		}
		computed, err := s.reconciliationLocked(v.Date, v.Channel)
		if err != nil {
			return err
		}
		if v.ExpectedReceived != computed.ExpectedReceived || v.ExpectedRefund != computed.ExpectedRefund || financeDigest(v.OrderIDs) != financeDigest(computed.OrderIDs) {
			return ErrConflict
		}
		v.Difference = (v.ActualReceived - v.ActualRefund) - (v.ExpectedReceived - v.ExpectedRefund)
		if v.Difference != 0 && strings.TrimSpace(v.Note) == "" {
			return ErrInvalid
		}
		v.At = time.Now().Unix()
		s.state.Finance.Reconciliations[v.ID] = v
		s.state.Finance.Revision++
		return nil
	})
}
func ValidateFinanceBackup(state *State) error {
	f := &state.Finance
	f.ensure()
	if len(f.Ledger) > 1000000 || len(f.Incidents) > 10000 || len(f.Sources) > 1000000 || len(f.Operations) > 1000000 || len(f.Reconciliations) > 100000 || f.Revision < 0 {
		return fmt.Errorf("finance backup exceeds resource budget")
	}
	for uid, frozen := range f.Holds {
		if _, ok := state.Users[uid]; !ok || frozen < 0 || frozen > state.Signin[uid].Points {
			return fmt.Errorf("invalid frozen balance")
		}
	}
	seen := map[string]bool{}
	for _, v := range f.Ledger {
		if v.ID == "" || v.UID <= 0 || v.Balance < 0 || v.Frozen < 0 || v.Frozen > v.Balance || seen[v.ID] {
			return fmt.Errorf("invalid wallet ledger")
		}
		seen[v.ID] = true
	}
	for id, v := range f.Sources {
		if id != v.ID || v.UID <= 0 || v.At <= 0 || v.Offset < 0 {
			return fmt.Errorf("invalid entitlement lineage")
		}
	}
	for id, v := range f.Incidents {
		if id != v.ID || len(id) < 16 || len(id) > 80 || v.Revision < 1 || len(v.UIDs) == 0 || len(v.UIDs) > 500 || v.Start <= 0 || v.End < v.Start || v.HoldUntil < v.Start || v.Days < 0 || v.Days > 30 || v.Points < 0 || v.Points > 10000 || len(v.Title) > 120 || len(v.Reason) > 1000 || (v.Status != "open" && v.Status != "compensated" && v.Status != "closed") {
			return fmt.Errorf("invalid service incident")
		}
		seenUID := map[int64]bool{}
		for _, uid := range v.UIDs {
			if uid <= 0 || seenUID[uid] {
				return fmt.Errorf("invalid incident target")
			}
			seenUID[uid] = true
		}
	}
	for id, v := range f.Reconciliations {
		if id != v.ID || len(id) < 16 || len(id) > 80 || len(v.Channel) > 80 || len(v.Note) > 1000 || len(v.OrderIDs) > 100000 || v.At <= 0 || v.ActualReceived < 0 || v.ActualRefund < 0 || v.ExpectedReceived < 0 || v.ExpectedRefund < 0 || v.Difference != (v.ActualReceived-v.ActualRefund)-(v.ExpectedReceived-v.ExpectedRefund) {
			return fmt.Errorf("invalid reconciliation")
		}
		if _, err := time.Parse("2006-01-02", v.Date); err != nil {
			return fmt.Errorf("invalid reconciliation date")
		}
	}

	return nil
}

func (s *Store) expireTemporaryRolesLocked() {
	now := time.Now().Unix()
	for uid, u := range s.state.Users {
		if u.Role == RoleWhitelist && u.WhitelistUntil > 0 && u.WhitelistUntil <= now && !u.PendingEmby {
			u.Role = RoleNormal
			u.WhitelistUntil = 0
			s.state.Users[uid] = u
		}
	}
}
