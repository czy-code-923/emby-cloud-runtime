package store

import (
	"strings"
	"testing"
	"time"
)

func TestJianweiCashServiceOrderPreservesMainRightsAndRefund(t *testing.T) {
	s, u, cfg := shopFixture(t, -1)
	now := time.Now().Unix()
	u, e := s.UpdateUser(u.UID, func(v *User) error { v.EmbyID = "cash-service-player"; v.ExpiredAt = now + 86400; return nil })
	if e != nil {
		t.Fatal(e)
	}
	items := s.Services().Items
	items[0].Enabled = true
	items[0].LibraryIDs = []string{"music"}
	items[0].Lines = []string{"https://music.example.test"}
	items[0].RegisterEnabled = true
	items[0].RenewEnabled = true
	if e = s.SaveServices(items, 0); e != nil {
		t.Fatal(e)
	}
	cfg.Products = append(cfg.Products, ShopProduct{ID: "music-open", ServiceID: "music", Name: "音乐开通", Kind: 1, Days: 30, PriceCents: 1999, Enabled: true, Stock: -1}, ShopProduct{ID: "music-renew", ServiceID: "music", Name: "音乐续期", Kind: 2, Days: 90, PriceCents: 3999, Enabled: true, Stock: -1})
	if e = s.SaveShopConfiguration(cfg, cfg.Revision); e != nil {
		t.Fatal(e)
	}
	cfg = s.ShopConfiguration()
	if _, e = s.CreateShopOrder(u.UID, "service-renew-reject-01", "music-renew", "service-request-renew-0", cfg.Revision, 1, false); e == nil {
		t.Fatal("renewed unopened service")
	}
	o, e := s.CreateShopOrder(u.UID, "service-cash-order-0001", "music-open", "service-request-open-01", cfg.Revision, 1, false)
	if e != nil {
		t.Fatal(e)
	}
	if s.EmbyOccupancy(EmbyOccupancyOptions{}) != 1 {
		t.Fatal("service consumed main registration quota")
	}
	o, e = s.SubmitShopPayment(u.UID, o.ID, "fixture", false)
	if e != nil {
		t.Fatal(e)
	}
	key := strings.Repeat("c", 64)
	o, e = s.ConfirmShopPayment(o.ID, o.Revision, 1, 1999, now, strings.Repeat("d", 64), key, "fixture-sealed", "fixture", 1, false, "manual_order_confirmation")
	if e != nil || o.Status != "issued" {
		t.Fatal(o.Status, e)
	}
	rc, _ := s.RegCode(key)
	if rc.ServiceID != "music" || rc.Type != 1 || rc.TargetUID != u.UID {
		t.Fatal("lost service scope", rc)
	}
	if _, e = s.ConsumeServiceCode(u.UID, u.TelegramID, key, now); e != nil {
		t.Fatal(e)
	}
	fresh, _ := s.User(u.UID)
	if fresh.Role != u.Role || fresh.ExpiredAt != u.ExpiredAt || s.ServiceGrants(u.UID)["music"] != now+30*86400 {
		t.Fatal("service changed main rights", fresh)
	}
	order, _ := s.ShopOrder(o.ID)
	if order.RedeemedAt == 0 || order.EntitlementSourceID == "" {
		t.Fatal("service source missing", order)
	}
	if _, e = s.CreateShopOrder(u.UID, "service-cash-duplicate01", "music-open", "service-request-open-02", cfg.Revision, 1, false); e == nil {
		t.Fatal("duplicate opening")
	}
	if ids := s.ShopEligibleProductIDs(u.UID); len(ids) != 1 || ids[0] != "music-renew" {
		t.Fatal(ids)
	}
	preview := s.ShopRefundPreview(o.ID)
	if preview["proven"] != true {
		t.Fatal("service refund not proven", preview)
	}
	if _, e = s.ReviewShopOrder(o.ID, "refund_pending", "fixture refund", "", order.Revision, 1); e != nil {
		t.Fatal(e)
	}
	fresh, _ = s.User(u.UID)
	if fresh.ExpiredAt != u.ExpiredAt || s.ServiceGrants(u.UID)["music"] > time.Now().Unix()+1 {
		t.Fatal("refund escaped service")
	}
	white, _ := s.UpdateUser(u.UID, func(v *User) error { v.Role = RoleWhitelist; v.WhitelistUntil = now + 3600; return nil })
	if ids := s.ShopEligibleProductIDs(white.UID); len(ids) != 0 {
		t.Fatal("white charged twice", ids)
	}
}

func TestJianweiAdminShopNoticeLeasesAndRecipients(t *testing.T) {
	s, u, cfg := shopFixture(t, -1)
	admin, e := s.CreateUser(User{Username: "shop-notice-admin", Role: RoleAdmin, Active: true, TelegramID: 93455})
	if e != nil {
		t.Fatal(e)
	}
	o, e := s.CreateShopOrder(u.UID, strings.Repeat("a", 24), "registration", strings.Repeat("b", 24), cfg.Revision, 10, false)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, found, e := s.ClaimShopAdminNotice(); e != nil || found {
		t.Fatal("pending is not payment", e)
	}
	o, e = s.SubmitShopPayment(u.UID, o.ID, "fixture", false)
	if e != nil {
		t.Fatal(e)
	}
	claimed, tg, found, e := s.ClaimShopAdminNotice()
	if e != nil || !found || tg != admin.TelegramID || claimed.ID != o.ID {
		t.Fatal("missing admin notice", e)
	}
	if _, _, found, e = s.ClaimShopAdminNotice(); e != nil || found {
		t.Fatal("duplicate lease", e)
	}
	if e = s.MarkShopAdminNotified(o.ID, o.Revision, tg); e != nil {
		t.Fatal(e)
	}
	if _, _, found, e = s.ClaimShopAdminNotice(); e != nil || found {
		t.Fatal("duplicate delivery", e)
	}
	_, e = s.UpdateUser(admin.UID, func(v *User) error { v.Active = false; return nil })
	if e != nil {
		t.Fatal(e)
	}
}
