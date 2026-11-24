package store

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestJianweiTelegramProfileIsIdempotentAndNeverGrantsMedia(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	first, created, err := s.EnsureTelegramProfile(98231, "actor", "unusable-fixture-hash", 2)
	if err != nil || !created || first.Role != RoleNormal || first.EmbyID != "" || first.PendingEmby {
		t.Fatal("profile creation granted unexpected access")
	}
	again, created, err := s.EnsureTelegramProfile(98231, "another-name", "ignored", 2)
	if err != nil || created || again.UID != first.UID {
		t.Fatal("start replay created a second identity")
	}
	if _, _, err = s.EnsureTelegramProfile(98232, "second", "fixture", 1); err == nil {
		t.Fatal("capacity limit bypassed")
	}
}

func TestJianweiMiniSessionReplayRotationAndDisabledUser(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	user, _, err := s.EnsureTelegramProfile(98231, "actor", "fixture", 2)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now().Unix()
	token := strings.Repeat("a", 64)
	if err = s.CreateMiniSession(ctx, user.UID, "proof", "nonce", "bot-key", token, now); err != nil {
		t.Fatal(err)
	}
	if err = s.CreateMiniSession(ctx, user.UID, "proof", "nonce", "bot-key", token, now); err != nil {
		t.Fatal("same browser retry must be idempotent")
	}
	if err = s.CreateMiniSession(ctx, user.UID, "proof", "another-browser", "bot-key", strings.Repeat("b", 64), now); err == nil {
		t.Fatal("proof replay from another browser accepted")
	}
	if _, err = s.MiniSessionUser(ctx, token, "rotated-key", now); err == nil {
		t.Fatal("token rotation did not revoke session")
	}
	if _, err = s.MiniSessionUser(ctx, token, "bot-key", now+901); err == nil {
		t.Fatal("expired session accepted")
	}
	if _, err = s.MiniSessionUser(ctx, token, "bot-key", now); err != nil {
		t.Fatal("valid session rejected")
	}
	if _, err = s.UpdateUser(user.UID, func(u *User) error { u.Active = false; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err = s.MiniSessionUser(ctx, token, "bot-key", now); err == nil {
		t.Fatal("disabled user accepted")
	}
}
