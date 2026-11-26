package api

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prejudice-studio/twilight/internal/security"
	"github.com/prejudice-studio/twilight/internal/store"
)

func TestJianweiMemberSecurityCooldownPersistsAndOnlyFailuresCount(t *testing.T) {
	a := newTestApp(t)
	hash, e := security.HashPassword("183927")
	if e != nil {
		t.Fatal(e)
	}
	u, e := a.store().CreateUser(store.User{Username: "proof-member", Active: true, TelegramID: 12347, SecurityCodeHash: hash})
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now().Unix()
	for i := 0; i < 4; i++ {
		if retry, e := a.store().CheckMemberSecurityResult(u.UID, hash, false, now); e != nil || retry != 0 {
			t.Fatal(retry, e)
		}
	}
	if retry, e := a.store().CheckMemberSecurityResult(u.UID, hash, true, now); e != nil || retry != 0 {
		t.Fatal("valid proof consumed attempts", retry, e)
	}
	for i := 0; i < 5; i++ {
		retry, e := a.store().CheckMemberSecurityResult(u.UID, hash, false, now)
		if e != nil || (i < 4 && retry != 0) || (i == 4 && retry != 300) {
			t.Fatal(i, retry, e)
		}
	}
	other, e := store.OpenPostgres(context.Background(), testDSN)
	if e != nil {
		t.Fatal(e)
	}
	defer other.Close()
	locked, found := other.User(u.UID)
	if !found || locked.MemberSecurityAttempts.LockedUntil != now+300 {
		t.Fatal("cooldown was not shared across processes")
	}
	if retry, e := other.CheckMemberSecurityResult(u.UID, hash, true, now+1); e != nil || retry != 299 {
		t.Fatal("correct proof bypassed an active lock", retry, e)
	}
	if retry, e := other.CheckMemberSecurityResult(u.UID, hash, true, now+301); e != nil || retry != 0 {
		t.Fatal("cooldown did not expire", retry, e)
	}
	if _, e := other.CheckMemberSecurityResult(u.UID, "stale-hash", true, now+302); e == nil {
		t.Fatal("stale secret authorized mutation")
	}
}

func TestJianweiMemberEmailProofOnlyForBoundVerifiedEmail(t *testing.T) {
	a := newTestApp(t)
	u := store.User{RequireEmailForEmbyPasswordChange: true}
	if a.memberPasswordEmailRequired(u) {
		t.Fatal("unbound Telegram member required nonexistent email")
	}
	if !a.passwordChangeEmailRequired(u, emailPurposeChangeEmby) {
		t.Fatal("Web policy was weakened")
	}
	u.Email = "fixture@example.test"
	u.EmailVerified = true
	if !a.memberPasswordEmailRequired(u) {
		t.Fatal("bound email protection lost")
	}
}

func TestJianweiTelegramAvatarUsesOwnIdentityWithoutLeakingBotToken(t *testing.T) {
	a := newTestApp(t)
	var pngData bytes.Buffer
	_ = png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 32, 32)))
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/botavatar-fixture/getUserProfilePhotos":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["user_id"] != float64(89123) {
				t.Error("avatar identity changed", body)
			}
			_, _ = w.Write([]byte(`{"ok":true,"result":{"photos":[[{"file_id":"own-photo","width":32}]]}}`))
		case "/botavatar-fixture/getFile":
			_, _ = w.Write([]byte(`{"ok":true,"result":{"file_path":"photos/own.png"}}`))
		case "/file/botavatar-fixture/photos/own.png":
			_, _ = w.Write(pngData.Bytes())
		default:
			w.WriteHeader(404)
		}
	}))
	defer remote.Close()
	a.cfg().TelegramMode = true
	a.cfg().TelegramBotToken = "avatar-fixture"
	a.cfg().TelegramAPIURL = remote.URL
	r := httptest.NewRequest("GET", "/api/v2/miniapp/avatar", nil)
	r = r.WithContext(context.WithValue(r.Context(), principalKey, principal{User: store.User{TelegramID: 89123}}))
	w := httptest.NewRecorder()
	a.handleMiniTelegramAvatar(w, r)
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), pngData.Bytes()) || !strings.Contains(w.Header().Get("Cache-Control"), "private") || w.Header().Get("Location") != "" {
		t.Fatal("private avatar delivery failed", w.Code, w.Header())
	}
	if strings.Contains(w.Body.String(), "avatar-fixture") {
		t.Fatal("Bot secret leaked")
	}
}
