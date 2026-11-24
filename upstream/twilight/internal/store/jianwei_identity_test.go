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
	if err = s.CreateMiniSession(ctx, user.UID, user.TelegramID, "proof", "nonce", "bot-key", token, now); err != nil {
		t.Fatal(err)
	}
	if err = s.CreateMiniSession(ctx, user.UID, user.TelegramID, "proof", "nonce", "bot-key", token, now); err != nil {
		t.Fatal("same browser retry must be idempotent")
	}
	if err = s.CreateMiniSession(ctx, user.UID, user.TelegramID, "proof", "another-browser", "bot-key", strings.Repeat("b", 64), now); err == nil {
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

func TestJianweiMiniSessionRebindAndRestoreRevokeOldAudience(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	u, _, err := s.EnsureTelegramProfile(9123, "member", "fixture", 2)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now().Unix()
	token := strings.Repeat("a", 64)
	if err = s.CreateMiniSession(ctx, u.UID, 9123, "proof", "nonce", "bot", token, now); err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateUser(u.UID, func(user *User) error { user.TelegramID = 9124; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err = s.MiniSessionUser(ctx, token, "bot", now); err == nil {
		t.Fatal("Telegram rebind preserved another identity's session")
	}
	token = strings.Repeat("b", 64)
	if err = s.CreateMiniSession(ctx, u.UID, 9124, "new-proof", "new-nonce", "bot", token, now); err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if err = s.LoadSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err = s.MiniSessionUser(ctx, token, "bot", now); err == nil {
		t.Fatal("restore preserved short-lived session credentials")
	}
	var count int
	if err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM twilight_mini_sessions`).Scan(&count); err != nil || count != 0 {
		t.Fatal("mini sessions were restored or left behind")
	}
}
