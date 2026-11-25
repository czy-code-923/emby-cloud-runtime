package store

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func shopFixture(t *testing.T, stock int) (*Store, User, ShopSettings) {
	t.Helper()
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	u, _, err := s.EnsureTelegramProfile(783001, "buyer", "fixture", -1)
	if err != nil {
		t.Fatal(err)
	}
	cfg := ShopSettings{Enabled: true, Instructions: "fixture payment instructions", ReceivingAccount: "fixture merchant", Channel: "fixture channel", Products: []ShopProduct{{ID: "registration", Name: "30 days", Kind: 1, Days: 30, PriceCents: 1000, Stock: stock, Enabled: true}}}
	if err = s.SaveShopConfiguration(cfg, 0); err != nil {
		t.Fatal(err)
	}
	return s, u, s.ShopConfiguration()
}
func TestJianweiCommerceConcurrentInventoryAndCapacity(t *testing.T) {
	s, u, cfg := shopFixture(t, 1)
	var wg sync.WaitGroup
	var won atomic.Int64
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.CreateShopOrder(u.UID, fmt.Sprintf("order-%020d", i), "registration", fmt.Sprintf("request-%020d", i), cfg.Revision, 1, false)
			if err == nil {
				won.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if won.Load() != 1 || s.EmbyOccupancy(EmbyOccupancyOptions{}) != 1 {
		t.Fatalf("oversold: %d", won.Load())
	}
	original := s.ShopOrders(u.UID, false)[0]
	replay, err := s.CreateShopOrder(u.UID, "different-order-id", "registration", original.RequestKey, cfg.Revision, 1, false)
	if err != nil || replay.ID != original.ID {
		t.Fatal("order retry not idempotent", err)
	}
	review, err := s.SubmitShopPayment(u.UID, original.ID, "fixture receipt", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SubmitShopPayment(u.UID+1, original.ID, "", false); err == nil {
		t.Fatal("foreign buyer changed order")
	}
	receipt, key := strings.Repeat("A", 64), strings.Repeat("B", 64)
	issued, err := s.ConfirmShopPayment(review.ID, review.Revision, 1, 1000, time.Now().Unix(), receipt, key, "encrypted-fixture", "", 1, false)
	if err != nil || issued.Status != "issued" {
		t.Fatal("own capacity reservation blocked fulfillment", err, issued.Status)
	}
	rc,_:=s.RegCode(key)
	if RegCodeExpired(rc,rc.CreatedAt+30*86400-1)||!RegCodeExpired(rc,rc.CreatedAt+30*86400) {t.Fatal("shop code must expire at exactly 30 days; legacy validity uses hours")}
	replay, err = s.ConfirmShopPayment(review.ID, review.Revision, 1, 1000, time.Now().Unix(), receipt, strings.Repeat("C", 64), "second-cipher", "", 1, false)
	if err != nil || replay.CodeKey != key || s.CountRegCodes() != 1 {
		t.Fatal("confirmation replay minted another code", err)
	}
	if s.EmbyOccupancy(EmbyOccupancyOptions{}) != 1 {
		t.Fatal("order and delivered code double-counted capacity")
	}
	frozen, err := s.ReviewShopOrder(issued.ID, "refund_pending", "fixture refund", "", issued.Revision, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ConsumeRegCode(key, u.UID, u.TelegramID); err == nil {
		t.Fatal("refunding code redeemed")
	}
	refunded, err := s.ReviewShopOrder(frozen.ID, "refund_complete", "fixture refund", "transfer-fixture", frozen.Revision, 1)
	if err != nil || refunded.Status != "refunded" {
		t.Fatal(err)
	}
	if _, err = s.ReviewShopOrder(frozen.ID, "refund_complete", "fixture refund", "transfer-fixture", frozen.Revision, 1); err != nil {
		t.Fatal("refund completion replay failed", err)
	}
}

func TestJianweiCommerceLatePaymentReceiptAndRedemptionRace(t *testing.T) {
	s, u, cfg := shopFixture(t, 1)
	late, err := s.CreateShopOrder(u.UID, "late-order-000000001", "registration", "late-request-000000001", cfg.Revision, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	err = s.mutateAndSaveLocked(func() error {
		o := s.state.ShopOrders[late.ID]
		o.PayBefore = time.Now().Unix() - 1
		s.state.ShopOrders[o.ID] = o
		return nil
	})
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	late, err = s.SubmitShopPayment(u.UID, late.ID, "late actual payment", false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateShopOrder(u.UID, "second-order-00000001", "registration", "second-request-000001", cfg.Revision, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	second, err = s.SubmitShopPayment(u.UID, second.ID, "", false)
	if err != nil {
		t.Fatal(err)
	}
	key := strings.Repeat("D", 64)
	receipt := strings.Repeat("E", 64)
	second, err = s.ConfirmShopPayment(second.ID, second.Revision, 1, 1000, time.Now().Unix(), receipt, key, "cipher", "", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ConfirmShopPayment(late.ID, late.Revision, 1, 1000, time.Now().Unix(), receipt, strings.Repeat("F", 64), "cipher", "", 0, false); err == nil {
		t.Fatal("one receipt funded two orders")
	}
	late, err = s.ConfirmShopPayment(late.ID, late.Revision, 1, 1000, time.Now().Unix(), strings.Repeat("G", 64), strings.Repeat("H", 64), "cipher", "", 0, false)
	if err != nil || late.Status != "paid_hold" || late.CodeKey != "" || late.ReceivedCents != 1000 {
		t.Fatal("late payment oversold or lost receipt", err, late)
	}
	var wg sync.WaitGroup
	var consumed, refunding bool
	wg.Add(2)
	go func() { defer wg.Done(); _, e := s.ConsumeRegCode(key, u.UID, u.TelegramID); consumed = e == nil }()
	go func() {
		defer wg.Done()
		_, e := s.ReviewShopOrder(second.ID, "refund_pending", "fixture", "", second.Revision, 1)
		refunding = e == nil
	}()
	wg.Wait()
	if consumed == refunding {
		t.Fatal("redemption/refund must have exactly one winner", consumed, refunding)
	}
}

func TestJianweiShopLoginPurposeReplayExpiryAndLogout(t *testing.T) {
	s, u, _ := shopFixture(t, -1)
	ctx := context.Background()
	now := time.Now().Unix()
	code := strings.Repeat("A", 20)
	if err := s.CreateShopLoginCode(ctx, u, code, "bot", now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConsumeShopLoginCode(ctx, code, "other-bot", now); err == nil {
		t.Fatal("cross-bot login accepted")
	}
	if _, err := s.ConsumeShopLoginCode(ctx, code, "bot", now+301); err == nil {
		t.Fatal("expired login accepted")
	}
	if _, err := s.ConsumeShopLoginCode(ctx, code, "bot", now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConsumeShopLoginCode(ctx, code, "bot", now); err == nil {
		t.Fatal("code replay accepted")
	}
	token := strings.Repeat("c", 64)
	if err := s.CreateMiniSession(ctx, u.UID, u.TelegramID, "shop-proof", "nonce", "shop-session|bot", token, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MiniSessionUser(ctx, token, "bot", now); err == nil {
		t.Fatal("shop session crossed into mini app")
	}
	if err := s.RevokeShopSession(ctx, token, "bot"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MiniSessionUser(ctx, token, "shop-session|bot", now); err == nil {
		t.Fatal("logout left valid session")
	}
}
