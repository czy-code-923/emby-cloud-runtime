package store

import (
	"strings"
	"testing"
	"time"
)

func TestJianweiServicesAreIndependentAndPointsAreAtomic(t *testing.T) {
	s, u, now := retentionFixture(t)
	items := s.Services().Items
	items[0].Enabled = true
	items[0].LibraryIDs = []string{"music-library"}
	items[0].Lines = []string{"https://music.example.test"}
	items[0].RegisterEnabled = true
	items[0].RenewEnabled = true
	items[0].PointsEnabled = true
	items[0].Points = 150
	if e := s.SaveServices(items, 0); e != nil {
		t.Fatal(e)
	}
	if ServiceAccess(u, items[0], 0, now) {
		t.Fatal("ordinary movie membership leaked service")
	}
	white := u
	white.Role = RoleWhitelist
	white.WhitelistUntil = now + 100
	if !ServiceAccess(white, items[0], 0, now) || ServiceAccess(white, items[0], 0, now+101) {
		t.Fatal("temporary whitelist boundary")
	}
	if e := s.AdjustWallet(u.UID, 1, 0, "service-credit-fixture", "credit", "fixture", 500); e != nil {
		t.Fatal(e)
	}
	if e := s.AdjustWallet(u.UID, 1, s.Wallet(u.UID).Revision, "service-freeze-fixture", "freeze", "fixture", 400); e != nil {
		t.Fatal(e)
	}
	if _, e := s.CreatePointsOrder(u.UID, "service-frozen-order", "svc-music", "service-frozen-request", strings.Repeat("a", 64), "sealed", now, 150); e != ErrInsufficientPoints {
		t.Fatal("service spent frozen points", e)
	}
	if e := s.AdjustWallet(u.UID, 1, s.Wallet(u.UID).Revision, "service-release-fixture", "release", "fixture", 400); e != nil {
		t.Fatal(e)
	}
	if _, e := s.RetentionPreview(u.UID, "B", "retain-service-window", now); e != nil {
		t.Fatal(e)
	}
	if e := s.RetentionConfirm(u.UID, "retain-service-window", true, false, 0, now); e != nil {
		t.Fatal(e)
	}
	key := strings.Repeat("f", 64)
	order, e := s.CreatePointsOrder(u.UID, "service-points-order-01", "svc-music", "service-points-request", key, "sealed-fixture", now, 150)
	if e != nil {
		t.Fatal(e)
	}
	duplicate, e := s.CreatePointsOrder(u.UID, "service-points-order-02", "svc-music", "service-points-request", strings.Repeat("e", 64), "sealed-fixture", now, 150)
	if e != nil || duplicate.ID != order.ID || s.Wallet(u.UID).Available != 350 {
		t.Fatal("duplicate points debit", e)
	}
	if v, _ := s.RetentionView(u.UID, now); !v.Member.Member.Participation || !v.ChoiceLocked {
		t.Fatal("issuing unrelated card cancelled current A/B window")
	}
	after, e := s.ConsumeServiceCode(u.UID, u.TelegramID, key, now)
	if e != nil {
		t.Fatal(e)
	}
	if after.Role != RoleNormal || after.ExpiredAt != u.ExpiredAt || s.ServiceGrants(u.UID)["music"] != now+30*86400 {
		t.Fatal("service card changed movie expiry or role")
	}
	if v, _ := s.RetentionView(u.UID, now); !v.ChoiceLocked || !v.Member.Member.Participation {
		t.Fatal("service redemption changed A/B")
	}
	if _, e = s.ConsumeServiceCode(u.UID, u.TelegramID, key, now); e == nil {
		t.Fatal("double redemption")
	}
	product, cost, e := s.ServicePointsProduct(u, "svc-music", now)
	if e != nil || product.Kind != 2 || cost != 150 {
		t.Fatal("renewal kind or price")
	}
	renewal := RegCode{Code: "service-renewal", ServiceID: "music", Type: 2, Days: 30, UseCountLimit: 1, Active: true, CreatedAt: now, ValidityTime: 720}
	if e = s.UpsertRegCode(renewal); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ConsumeServiceCode(u.UID, u.TelegramID, renewal.Code, now+1); e != nil {
		t.Fatal(e)
	}
	if s.ServiceGrants(u.UID)["music"] != now+60*86400 {
		t.Fatal("renewal discarded remaining time")
	}
	s.mu.Lock()
	e = s.mutateAndSaveLocked(func() error {
		source := s.state.Finance.Sources["code:"+key]
		o := s.state.ShopOrders[order.ID]
		return s.refundServiceSourceLocked(&o, source, now+10)
	})
	s.mu.Unlock()
	if e != nil {
		t.Fatal(e)
	}
	if end := s.ServiceGrants(u.UID)["music"]; end != now+30*86400+10 {
		t.Fatal("refund removed later independent service renewal", end)
	}
	s.mu.Lock()
	e = s.mutateAndSaveLocked(func() error {
		source := s.state.Finance.Sources["code:"+renewal.Code]
		return s.refundServiceSourceLocked(&ShopOrder{ID: "later-independent-order", UID: u.UID, Product: ShopProduct{Days: 30}}, source, now+20)
	})
	s.mu.Unlock()
	if e != nil || s.ServiceGrants(u.UID)["music"] != now+20 {
		t.Fatal("later source refund lost lineage after earlier refund", e)
	}
	after, _ = s.User(u.UID)
	if after.ExpiredAt != u.ExpiredAt {
		t.Fatal("service refund touched movie membership")
	}
	snapshot, e := s.Snapshot()
	if e != nil {
		t.Fatal(e)
	}
	if e = s.LoadSnapshot(snapshot); e != nil {
		t.Fatal("service backup round trip", e)
	}
}

func TestJianweiServiceConfigurationRejectsAmbiguousLibrariesAndDoesNotReserveAccounts(t *testing.T) {
	s, _, now := retentionFixture(t)
	items := s.Services().Items
	items[0].LibraryIDs = []string{"same"}
	items[1].LibraryIDs = []string{"same"}
	if s.SaveServices(items, 0) == nil {
		t.Fatal("duplicate library ownership")
	}
	items = s.Services().Items
	items[0].Lines = []string{"https://same.example.test"}
	items[1].Lines = []string{"https://same.example.test:443"}
	if s.SaveServices(items, 0) == nil {
		t.Fatal("default TLS port bypassed duplicate service host check")
	}
	items = s.Services().Items
	items[0].Enabled = true
	if s.SaveServices(items, 0) == nil {
		t.Fatal("enabled empty library service")
	}
	before := s.EmbyOccupancy(EmbyOccupancyOptions{})
	if e := s.UpsertRegCode(RegCode{Code: "reserved-service-code", ServiceID: "music", Type: 1, Days: 30, Active: true, UseCountLimit: 100, CreatedAt: now, ValidityTime: 720}); e != nil {
		t.Fatal(e)
	}
	if s.EmbyOccupancy(EmbyOccupancyOptions{}) != before {
		t.Fatal("service codes consumed main registration slots")
	}
	if validateServiceState(ServiceState{Grants: map[int64]map[string]int64{1: {"unknown": time.Now().Unix()}}}) == nil {
		t.Fatal("invalid imported grant accepted")
	}
}
