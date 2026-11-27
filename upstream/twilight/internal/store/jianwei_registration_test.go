package store

import (
	"testing"
	"time"
)

func TestJianweiRegistrationSummaryMatchesCapacityWithoutCountingWebProfiles(t *testing.T) {
	s, buyer, cfg := shopFixture(t, 5)
	if _, err := s.CreateShopOrder(buyer.UID, "capacity-order-00000000001", "registration", "capacity-request-00000001", cfg.Revision, 20, false); err != nil {
		t.Fatal(err)
	}
	for _, u := range []User{
		{Username: "bound-admin", Active: true, Role: RoleAdmin, EmbyID: "media-admin"},
		{Username: "bound-disabled", Active: false, EmbyID: "media-disabled", ExpiredAt: 1},
		{Username: "pending-media", Active: true, PendingEmby: true},
		{Username: "web-only", Active: true},
	} {
		if _, err := s.CreateUser(u); err != nil {
			t.Fatal(err)
		}
	}
	for _, code := range []RegCode{
		{Code: "MAIN-CAPACITY", Type: 1, Active: true, ValidityTime: -1, UseCount: 1, UseCountLimit: 3},
		{Code: "SCOPE-ONLY", Type: 1, ServiceID: "music", Active: true, ValidityTime: -1, UseCountLimit: 20},
		{Code: "RENEW-ONLY", Type: 2, Active: true, ValidityTime: -1, UseCountLimit: 20},
		{Code: "EXPIRED", Type: 1, Active: true, ValidityTime: time.Now().Unix() - 1, UseCountLimit: 20},
	} {
		if err := s.UpsertRegCode(code); err != nil {
			t.Fatal(err)
		}
	}
	v := s.EmbyRegistrationSummary(20, false)
	if v.Registered != 2 || v.Reserved != 4 || v.Occupied != 6 || v.Remaining == nil || *v.Remaining != 14 || v.Unlimited {
		t.Fatalf("wrong registered/reserved split: %+v", v)
	}
	for _, active := range []bool{false, true} {
		v = s.EmbyRegistrationSummary(20, active)
		if v.Registered != 2 || v.Occupied != s.EmbyOccupancy(EmbyOccupancyOptions{CountActiveUsers: active}) || v.Registered+v.Reserved != v.Occupied {
			t.Fatalf("display diverged from registration enforcement: %+v", v)
		}
	}
	v = s.EmbyRegistrationSummary(1, false)
	if v.Remaining == nil || *v.Remaining != 0 || v.Registered != 2 {
		t.Fatalf("full capacity must not wrap negative or hide existing users: %+v", v)
	}
	for _, limit := range []int{-1, 0} {
		v = s.EmbyRegistrationSummary(limit, false)
		if !v.Unlimited || v.Remaining != nil || v.Registered != 2 {
			t.Fatalf("unlimited must not pretend there are zero remaining slots: %+v", v)
		}
	}
}
