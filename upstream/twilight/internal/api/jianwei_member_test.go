package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prejudice-studio/twilight/internal/store"
)

func TestJianweiMiniMemberIdentityOriginAndFeatureGates(t *testing.T) {
	a := newTestApp(t)
	a.cfg().TelegramMode = true
	a.cfg().TelegramBotToken = "fixture-token"
	a.cfg().TelegramMiniAppURL = "https://account.example.test/miniapp"
	a.cfg().CORSOrigins = append(a.cfg().CORSOrigins, "https://account.example.test")
	u, _, err := a.store().EnsureTelegramProfile(9123, "member", "unusable-fixture", -1)
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("c", 64)
	if err = a.store().CreateMiniSession(context.Background(), u.UID, 9123, "member-proof", "nonce", "fixture-token", token, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	request := func(path, body, origin, intent string, authenticated bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		r.Header.Set("X-Twilight-Client", "webui")
		r.Header.Set("X-Twilight-Intent", intent)
		if authenticated {
			r.AddCookie(&http.Cookie{Name: miniCookie, Value: token})
		}
		w := httptest.NewRecorder()
		a.ServeHTTP(w, r)
		return w
	}
	for _, tc := range []struct {
		origin, intent string
		auth           bool
	}{{"https://account.example.test", "miniapp-action", false}, {"https://foreign.invalid", "miniapp-action", true}, {"https://account.example.test", "", true}, {"", "miniapp-action", true}} {
		w := request("/api/v2/miniapp/signin", "{}", tc.origin, tc.intent, tc.auth)
		if w.Code < 400 {
			t.Fatalf("untrusted mutation accepted: %+v", tc)
		}
	}
	a.cfg().SigninEnabled = true
	w := request("/api/v2/miniapp/signin", `{"uid":999}`, "https://account.example.test", "miniapp-action", true)
	if w.Code != 200 {
		t.Fatalf("member signin failed: %d %s", w.Code, w.Body)
	}
	points := a.store().Signin(u.UID).Points
	w = request("/api/v2/miniapp/signin", "{}", "https://account.example.test", "miniapp-action", true)
	if w.Code != 200 || a.store().Signin(u.UID).Points != points || a.store().Signin(999).Points != 0 {
		t.Fatal("signin changed another user or awarded twice")
	}
	a.cfg().InviteEnabled = false
	w = request("/api/v2/miniapp/invite", "{}", "https://account.example.test", "miniapp-action", true)
	if w.Code < 400 {
		t.Fatal("disabled invitation created a code")
	}
	a.cfg().MediaRequestEnabled = false
	w = request("/api/v2/miniapp/requests", `{"title":"Fixture","skip_inventory_check":true}`, "https://account.example.test", "miniapp-action", true)
	if w.Code < 400 {
		t.Fatal("disabled media requests accepted")
	}
	a.cfg().EmbyDirectRegisterEnabled = false
	w = request("/api/v2/miniapp/register", `{"emby_username":"fixture","emby_password":"Fixture12345!"}`, "https://account.example.test", "miniapp-action", true)
	if w.Code < 400 {
		t.Fatal("Telegram identity granted free playback")
	}
}

func TestJianweiAdminOnlyWebRejectsExistingUserSession(t *testing.T) {
	a := newTestApp(t)
	cookies := registerAndLogin(t, a, "ordinary-reader", "Reader123456")
	a.cfg().WebAdminOnly = true
	if w := doJSON(a, http.MethodGet, "/api/v2/settings/identity", "", cookies); w.Code != http.StatusUnauthorized {
		t.Fatalf("ordinary Web session survived: %d", w.Code)
	}
	u, found := a.store().FindUserByUsername("ordinary-reader")
	if !found {
		t.Fatal("user missing")
	}
	r := httptest.NewRequest(http.MethodPost, "/api/v2/auth/login", nil)
	if _, err := a.completeVerifiedLogin(r, loginInput{}, u, "", ""); err == nil {
		t.Fatal("verified login bypassed admin-only gate")
	}
}

func TestJianweiMemberProjectionDoesNotLeakSecrets(t *testing.T) {
	a := newTestApp(t)
	u := store.User{UID: 42, Username: "fixture", PasswordHash: "private-hash", LegacyAPIKeyHash: "private-key", EmbyID: "private-id"}
	a.cfg().TelegramBannerEnabled = false
	a.cfg().TelegramBannerText = "disabled-ad"
	a.cfg().TelegramShopEnabled = false
	a.cfg().CommerceStoreURL = "https://private-shop.example"
	raw, err := json.Marshal(a.memberAccount(u))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private-hash", "private-key", "private-id", "disabled-ad", "private-shop.example"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatalf("member projection leaked %s", secret)
		}
	}
}

func TestJianweiMemberCommandsArePrivate(t *testing.T) {
	for _, command := range []string{"/panel", "/signin", "/renew", "/register", "/redeem", "/devices", "/lines", "/request", "/invite"} {
		spec, found := telegramCommandRegistry[command]
		if !found || !spec.private || spec.admin || spec.handler == nil {
			t.Fatalf("invalid member command %s", command)
		}
	}
	for _, raw := range []string{"javascript:alert(1)", "http://insecure.example", "https://u:p@example.com"} {
		if publicHTTPSURL(raw) != "" {
			t.Fatal("unsafe external link accepted")
		}
	}
}
