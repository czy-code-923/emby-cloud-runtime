package store

import (
	"fmt"
	"github.com/prejudice-studio/twilight/internal/retention"
	"testing"
)

func TestJianweiRetentionPaidWindowOpeningAndNextChoice(t *testing.T) {
	s, u, now := retentionFixture(t)
	end := now + 60*retention.Day
	if _, e := s.UpdateUser(u.UID, func(v *User) error { v.ExpiredAt = end; return nil }); e != nil {
		t.Fatal(e)
	}
	v, _ := s.RetentionView(u.UID, now)
	if v.Selectable || v.SelectableAt != end-30*retention.Day+1 {
		t.Fatal("paid opening time is wrong", v.SelectableAt)
	}
	if v, _ = s.RetentionView(u.UID, v.SelectableAt-1); v.Selectable {
		t.Fatal("paid exemption opened early")
	}
	openAt := end - 30*retention.Day + 1
	if v, _ = s.RetentionView(u.UID, openAt); !v.Selectable {
		t.Fatal("window did not open")
	}
	if _, e := s.RetentionPreview(u.UID, "B", "first-window", openAt); e != nil {
		t.Fatal(e)
	}
	if e := s.RetentionConfirm(u.UID, "first-window", true, false, 0, openAt); e != nil {
		t.Fatal(e)
	}
	if v, _ = s.RetentionView(u.UID, openAt+1); !v.ChoiceLocked || v.Selectable {
		t.Fatal("choice not locked")
	}
}

func TestJianweiRetentionShortSegmentsAccumulateWithoutDoubleCounting(t *testing.T) {
	s, u, now := retentionFixture(t)
	if _, e := s.RetentionPreview(u.UID, "B", "shorts", now); e != nil {
		t.Fatal(e)
	}
	if e := s.RetentionConfirm(u.UID, "shorts", true, false, 0, now); e != nil {
		t.Fatal(e)
	}
	for part := int64(0); part < 30; part++ {
		for _, second := range []int64{0, 30, 60} {
			// New sessions with pauses between them: each verified minute counts.
			observation := []RetentionObservation{{UID: u.UID, Session: fmt.Sprint(part), Item: "episode", Playing: true, Position: second}}
			if e := s.ObserveRetention(observation, now+part*120+second); e != nil {
				t.Fatal(e)
			}
		}
	}
	v, _ := s.RetentionView(u.UID, now+3601)
	if len(v.Ledger) != 1 || v.End != u.ExpiredAt+30*retention.Day || v.Balance != 0 {
		t.Fatal("cumulative B renewal failed", v.End, len(v.Ledger))
	}
	if v.Member.Member.Participation || v.Member.Member.Consent || v.Selectable {
		t.Fatal("next window authorized or opened early")
	}
	if _, e := s.RetentionPreview(u.UID, "A", "next-window", u.ExpiredAt+1); e != nil {
		t.Fatal("next window could not choose A", e)
	}
}

func TestJianweiShopExplicitEmptyCatalogueDoesNotReseed(t *testing.T) {
	s, _, _ := retentionFixture(t)
	if e := s.SaveShopConfiguration(ShopSettings{Products: []ShopProduct{}}, 0); e != nil {
		t.Fatal(e)
	}
	v := s.ShopConfiguration()
	if len(v.Products) != 0 {
		t.Fatal("deleted products reappeared as templates")
	}
}
