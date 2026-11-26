package store

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestJianweiWalletFreezeBlocksEveryDebitAndIdempotentRetry(t *testing.T) {
	s, u, now := retentionFixture(t)
	if e := s.AdjustWallet(u.UID, 1, 0, "fixture-credit-0001", "credit", "fixture", 1000); e != nil {
		t.Fatal(e)
	}
	revision := s.Wallet(u.UID).Revision
	if e := s.AdjustWallet(u.UID, 1, revision, "fixture-freeze-0001", "freeze", "investigate", 800); e != nil {
		t.Fatal(e)
	}
	if e := s.AdjustWallet(u.UID, 1, revision, "fixture-freeze-0001", "freeze", "investigate", 800); e != nil {
		t.Fatal("same operation not idempotent", e)
	}
	if e := s.AdjustWallet(u.UID, 1, revision, "fixture-freeze-0001", "freeze", "investigate", 1); e == nil {
		t.Fatal("request id reused for another amount")
	}
	if _, _, e := s.SpendSigninPointsAndUpdateUser(u.UID, 300, nil); e != ErrInsufficientPoints {
		t.Fatal("legacy renewal spent frozen points", e)
	}
	if _, e := s.RetentionPreview(u.UID, "A", "frozen-preview", now); e != nil {
		t.Fatal(e)
	}
	if e := s.RetentionConfirm(u.UID, "frozen-preview", true, true, 300, now); e != nil {
		t.Fatal(e)
	}
	if v, _ := s.RetentionView(u.UID, now); v.End != u.ExpiredAt || v.Balance != 200 || v.Frozen != 800 {
		t.Fatal("automatic renewal spent a hold", v)
	}
	if _, e := s.CreatePointsOrder(u.UID, "points-fixture-0001", "month", "points-request-0001", strings.Repeat("a", 64), "fixture", now, 300); e != ErrInsufficientPoints {
		t.Fatal("points shop spent a hold", e)
	}
	if e := s.AdjustWallet(u.UID, 1, s.Wallet(u.UID).Revision, "fixture-release-001", "release", "resolved", 100); e != nil {
		t.Fatal(e)
	}
	v, _ := s.RetentionView(u.UID, now)
	if v.End != u.ExpiredAt+30*86400 || v.Balance != 0 || v.Frozen != 700 {
		t.Fatal("released funds failed authorized settlement", v)
	}
}
func TestJianweiPointsOrderConcurrentRequestsMintOneCode(t *testing.T) {
	s, u, now := retentionFixture(t)
	if e := s.AdjustWallet(u.UID, 1, 0, "fixture-credit-0001", "credit", "fixture", 1000); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			o, e := s.CreatePointsOrder(u.UID, fmt.Sprintf("points-order-%020d", i), "month", "same-points-request-01", fmt.Sprintf("%064x", i+1), "fixture", now, 300)
			if e != nil || o.PointsCost != 300 {
				t.Error(o, e)
			}
		}(i)
	}
	wg.Wait()
	if len(s.ShopOrders(u.UID, false)) != 1 || s.Wallet(u.UID).Balance != 700 {
		t.Fatal("double issuance or debit")
	}
	if _, e := s.CreatePointsOrder(u.UID, "points-bad-cost-00001", "year", "different-request-0001", strings.Repeat("b", 64), "fixture", now, 300); e != ErrConflict {
		t.Fatal("stale price accepted", e)
	}
}
func TestJianweiIncidentProtectsWithoutDoubleCompensationOrAdminUnban(t *testing.T) {
	s, u, now := retentionFixture(t)
	v := ServiceIncident{ID: "incident-fixture-0001", Title: "fixture outage", Reason: "server sampling gap", UIDs: []int64{u.UID}, Start: now - 600, End: now, HoldUntil: now + 86400*3, Days: 2, Points: 20, Actor: 1}
	if e := s.CreateIncident(v, 0); e != nil {
		t.Fatal(e)
	}
	view, _ := s.RetentionView(u.UID, now)
	if view.AccessUntil < now+86400*3 || view.Member.Member.HoldUntil == 0 {
		t.Fatal("incident not protecting access")
	}
	if e := s.ResolveIncident(v.ID, "compensate", 1, 1); e != nil {
		t.Fatal(e)
	}
	if e := s.ResolveIncident(v.ID, "compensate", 1, 1); e != nil {
		t.Fatal("retry was not idempotent", e)
	}
	user, _ := s.User(u.UID)
	if user.ExpiredAt != u.ExpiredAt+2*86400 || s.Wallet(u.UID).Balance != 20 {
		t.Fatal("compensation not exact")
	}
	v.ID = "incident-admin-ban-01"
	if e := s.CreateIncident(v, s.Wallet(u.UID).Revision); e != nil {
		t.Fatal(e)
	}
	_, _ = s.UpdateUser(u.UID, func(next *User) error { next.EmbyDisabled = true; next.EmbyAutoDisabled = false; return nil })
	if e := s.ResolveIncident(v.ID, "compensate", 1, 1); e == nil {
		t.Fatal("incident bypassed manual ban")
	}
}
func TestJianweiRedeemedRefundProtectsIndependentLaterGrant(t *testing.T) {
	s, u, now := retentionFixture(t)
	key := strings.Repeat("d", 64)
	s.mu.Lock()
	e := s.mutateAndSaveLocked(func() error {
		first := u
		first.ExpiredAt += 30 * 86400
		s.recordEntitlementLocked("code:"+key, "code", u, first, now)
		later := first
		later.ExpiredAt += 90 * 86400
		s.recordEntitlementLocked("independent-later", "points", first, later, now+1)
		s.state.Users[u.UID] = later
		s.state.RegCodes[key] = RegCode{Code: key, Type: 2, Days: 30, UseCount: 1}
		s.state.ShopOrders["refund-fixture-0001"] = ShopOrder{ID: "refund-fixture-0001", UID: u.UID, Revision: 1, Status: "issued", CodeKey: key, RedeemedAt: now, Product: ShopProduct{Kind: 2, Days: 30, PriceCents: 1000}}
		return nil
	})
	s.mu.Unlock()
	if e != nil {
		t.Fatal(e)
	}
	result, e := s.ReviewShopOrder("refund-fixture-0001", "refund_pending", "confirmed refund", "", 1, 1)
	if e != nil {
		t.Fatal(e)
	}
	user, _ := s.User(u.UID)
	if user.ExpiredAt != u.ExpiredAt+90*86400 {
		t.Fatal("independent grant lost", user.ExpiredAt)
	}
	if _, e = s.ReviewShopOrder(result.ID, "refund_complete", "actual external refund", "bank-ref-unique", result.Revision, 1); e != nil {
		t.Fatal(e)
	}
	// A later unknown manual edit must not be silently rewritten by a refund.
	s.mu.Lock()
	e = s.mutateAndSaveLocked(func() error {
		o := s.state.ShopOrders[result.ID]
		o.ID = "unknown-source-0001"
		o.Status = "issued"
		o.Revision = 1
		o.CodeKey = strings.Repeat("e", 64)
		s.state.ShopOrders[o.ID] = o
		return nil
	})
	s.mu.Unlock()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ReviewShopOrder("unknown-source-0001", "refund_pending", "unknown lineage", "", 1, 1); e == nil {
		t.Fatal("unproven historical entitlement changed")
	}
}
func TestJianweiReconciliationPersistsRealDifferenceAndNoDuplicate(t *testing.T) {
	s, u, now := retentionFixture(t)
	s.mu.Lock()
	e := s.mutateAndSaveLocked(func() error {
		s.state.ShopOrders["receipt-fixture-0001"] = ShopOrder{ID: "receipt-fixture-0001", UID: u.UID, PaidAt: now, ReceivedCents: 1000, Channel: "alipay_qr"}
		return nil
	})
	s.mu.Unlock()
	if e != nil {
		t.Fatal(e)
	}
	day := time.Unix(now, 0).In(time.FixedZone("Asia/Shanghai", 28800)).Format("2006-01-02")
	v, e := s.ReconciliationPreview(day, "")
	if e != nil || v.ExpectedReceived != 1000 {
		t.Fatal(v, e)
	}
	v.ID = "reconciliation-fixture-01"
	v.ActualReceived = 900
	v.Actor = 1
	if e = s.ConfirmReconciliation(v, 0); e == nil {
		t.Fatal("difference accepted without explanation")
	}
	v.Note = "10 yuan unmatched receipt under review"
	if e = s.ConfirmReconciliation(v, 0); e != nil {
		t.Fatal(e)
	}
	if e = s.ConfirmReconciliation(v, 0); e != nil {
		t.Fatal("duplicate statement", e)
	}
}

func TestJianweiFrozenAndUndeliveredObligationsPreventDeletion(t *testing.T) {
	s, u, now := retentionFixture(t)
	if e := s.AdjustWallet(u.UID, 1, 0, "deletion-credit-0001", "credit", "fixture", 1000); e != nil {
		t.Fatal(e)
	}
	if e := s.AdjustWallet(u.UID, 1, s.Wallet(u.UID).Revision, "deletion-freeze-0001", "freeze", "fixture", 100); e != nil {
		t.Fatal(e)
	}
	if !s.FinancialLiability(u.UID) || s.DeleteUser(u.UID) == nil {
		t.Fatal("frozen account deleted")
	}
	if e := s.AdjustWallet(u.UID, 1, s.Wallet(u.UID).Revision, "deletion-release-001", "release", "fixture", 100); e != nil {
		t.Fatal(e)
	}
	o, e := s.CreatePointsOrder(u.UID, "delivery-order-00001", "month", "delivery-request-001", strings.Repeat("f", 64), "encrypted-code", now, 300)
	if e != nil {
		t.Fatal(e)
	}
	if !s.FinancialLiability(u.UID) || s.DeleteUser(u.UID) == nil {
		t.Fatal("undelivered code lost with account")
	}
	claimed, tg, found, e := s.ClaimShopDelivery()
	if e != nil || !found || claimed.ID != o.ID || tg != u.TelegramID {
		t.Fatal(claimed, tg, found, e)
	}
	_, _, found, e = s.ClaimShopDelivery()
	if e != nil || found {
		t.Fatal("delivery lease allowed duplicate", found, e)
	}
	if e = s.MarkShopDelivered(o.ID, u.UID); e != nil {
		t.Fatal(e)
	}
	if e = s.MarkShopDelivered(o.ID, u.UID); e != nil {
		t.Fatal(e)
	}
	if s.Wallet(u.UID).Balance != 700 || len(s.ShopOrders(u.UID, false)) != 1 {
		t.Fatal("notification debited or reminted")
	}
}
func TestJianweiFinanceBackupRejectsTamperedIncidentAndStatement(t *testing.T) {
	state := State{Finance: FinanceState{Incidents: map[string]ServiceIncident{"incident-tamper-0001": {ID: "incident-tamper-0001", Revision: 1, Status: "open", UIDs: []int64{1}, Start: 1, End: 2, HoldUntil: 3, Points: -1}}}}
	if ValidateFinanceBackup(&state) == nil {
		t.Fatal("negative compensation imported")
	}
	state.Finance.Incidents = nil
	state.Finance.Reconciliations = map[string]Reconciliation{"statement-tamper-001": {ID: "statement-tamper-001", Date: "2026-11-26", At: 1, ActualReceived: 100, Difference: 0}}
	if ValidateFinanceBackup(&state) == nil {
		t.Fatal("incorrect money difference imported")
	}
}
func TestJianweiDisabledZeroPriceTemplatesCannotOpenShop(t *testing.T) {
	s, _, _ := retentionFixture(t)
	cfg := s.ShopConfiguration()
	if len(cfg.Products) != 9 || cfg.Enabled {
		t.Fatal("default catalogue", cfg)
	}
	cfg.Enabled = true
	cfg.DefaultChannel = "alipay_phrase"
	cfg.Channels[0].Enabled = true
	cfg.Channels[0].Account = "fixture"
	cfg.Channels[0].Instructions = "fixture"
	if s.SaveShopConfiguration(cfg, cfg.Revision) == nil {
		t.Fatal("shop opened without priced products")
	}
}

func TestJianweiSequentialRefundRebasesLaterGrantWithoutTakingThirdGrant(t *testing.T) {
	s, u, now := retentionFixture(t)
	s.mu.Lock()
	e := s.mutateAndSaveLocked(func() error {
		u.ExpiredAt = now
		s.state.Users[u.UID] = u
		first := u
		first.ExpiredAt += 30 * 86400
		s.recordEntitlementLocked("code:first", "code", u, first, now)
		second := first
		second.ExpiredAt += 90 * 86400
		s.recordEntitlementLocked("code:second", "code", first, second, now)
		third := second
		third.ExpiredAt += 30 * 86400
		s.recordEntitlementLocked("third", "code", second, third, now)
		s.state.Users[u.UID] = third
		one := ShopOrder{ID: "one", UID: u.UID, CodeKey: "first", Product: ShopProduct{Days: 30}}
		if e := s.refundSourceLocked(&one, now); e != nil {
			return e
		}
		two := ShopOrder{ID: "two", UID: u.UID, CodeKey: "second", Product: ShopProduct{Days: 90}}
		return s.refundSourceLocked(&two, now+20*86400)
	})
	s.mu.Unlock()
	if e != nil {
		t.Fatal(e)
	}
	after, _ := s.User(u.UID)
	if after.ExpiredAt != now+50*86400 {
		t.Fatal("third independent 30 days consumed by wrong refund", after.ExpiredAt-now)
	}
}
