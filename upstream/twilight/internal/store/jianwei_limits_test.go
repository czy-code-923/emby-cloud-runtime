package store

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestJianweiDailyRequestLimitSurvivesDeletion(t *testing.T) {
	s, u, now := retentionFixture(t)
	for i := 0; i < 2; i++ {
		r, e := s.CreateMediaRequestWithOptions(MediaRequest{UID: u.UID, Title: fmt.Sprint("movie", i), Source: "tmdb", MediaID: int64(i + 100)}, MediaRequestCreateOptions{DailyLimit: 2})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.DeleteMediaRequestByKey(r.RequireKey, nil); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := s.CreateMediaRequestWithOptions(MediaRequest{UID: u.UID, Title: "third", Source: "tmdb", MediaID: 200}, MediaRequestCreateOptions{DailyLimit: 2}); !errors.Is(e, ErrMediaRequestDailyLimit) {
		t.Fatal("deleted requests bypassed quota", e)
	}
	_ = now
}
func TestJianweiRetentionBLocksAndAdminCanModify(t *testing.T) {
	s, u, now := retentionFixture(t)
	if _, e := s.RetentionPreview(u.UID, "B", "choose-b", now); e != nil {
		t.Fatal(e)
	}
	if e := s.RetentionConfirm(u.UID, "choose-b", true, false, 0, now); e != nil {
		t.Fatal("B cannot be selected", e)
	}
	if e := s.RetentionWithdraw(u.UID); !errors.Is(e, ErrRetentionModeLocked) {
		t.Fatal("withdraw bypass", e)
	}
	if _, e := s.RetentionPreview(u.UID, "A", "switch", now+25*86400); !errors.Is(e, ErrRetentionModeLocked) {
		t.Fatal("time bypassed mode lock", e)
	}
	if e := s.AdminRetentionMode(u.UID, "A", now); e != nil {
		t.Fatal(e)
	}
	v, _ := s.RetentionView(u.UID, now)
	if v.Member.Member.Mode != "A" || !v.Member.Member.Consent {
		t.Fatal("administrator adjustment failed")
	}
	if e := s.AdminRetentionMode(u.UID, "off", now); e != nil {
		t.Fatal(e)
	}
	v, _ = s.RetentionView(u.UID, now)
	if v.Member.Member.Mode != "" {
		t.Fatal("administrator unlock failed")
	}
	_ = time.Now()
}
