package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prejudice-studio/twilight/internal/config"
	"github.com/prejudice-studio/twilight/internal/store"
)

func TestJianweiMediaLimitsAndClientHeaders(t *testing.T) {
	cfg := &config.Config{MediaLimitsEnabled: true, MediaNormalDevices: 3, MediaNormalStreams: 2, MediaWhitelistDevices: 8, MediaWhitelistStreams: 4}
	for _, tc := range []struct{ role, devices, streams int }{{store.RoleNormal, 3, 2}, {store.RoleWhitelist, 8, 4}, {store.RoleAdmin, 0, 0}} {
		d, s := mediaLimits(cfg, store.User{Role: tc.role})
		if d != tc.devices || s != tc.streams {
			t.Fatalf("role %d: %d/%d", tc.role, d, s)
		}
	}
	one := 1
	d, s := mediaLimits(cfg, store.User{Role: store.RoleWhitelist, MediaMaxDevices: &one})
	if d != 1 || s != 4 {
		t.Fatal("override lost")
	}
	cfg.MediaLimitsEnabled = false
	d, s = mediaLimits(cfg, store.User{MediaMaxDevices: &one})
	if d != 0 || s != 0 {
		t.Fatal("disabled limit applied")
	}
	for _, header := range []string{`MediaBrowser Token="fixture", DeviceId="phone", Device="Test phone"`, `Emby Token="fixture", DeviceId="phone", Device="Test phone"`} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Authorization", header)
		uri, _ := url.Parse("/Items")
		token, id, name := mediaAuthFields(r, uri)
		if token != "fixture" || id != "phone" || name != "Test phone" {
			t.Fatal("client auth parsing")
		}
	}
	for _, value := range []any{float64(-1), float64(101), float64(1.5), "2", true} {
		if _, _, err := parseMediaLimit(map[string]any{"limit": value}, "limit"); err == nil {
			t.Fatal("invalid quota accepted")
		}
	}
}

func TestJianweiDeviceQuotaAtomicEnrollment(t *testing.T) {
	a := newTestApp(t)
	u, err := a.store().CreateUser(store.User{Username: "device-owner", Role: store.RoleNormal})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var accepted atomic.Int32
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if a.store().ClaimMediaDevice(u.UID, string(rune('a'+i)), "fixture", func(store.User) int { return 2 }) == nil {
				accepted.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if accepted.Load() != 2 {
		t.Fatalf("quota race: %d", accepted.Load())
	}
	u, _ = a.store().User(u.UID)
	id := u.MediaDevices[0].ID
	if err = a.store().RemoveMediaDevice(u.UID, id); err != nil {
		t.Fatal(err)
	}
	if err = a.store().ClaimMediaDevice(u.UID, id, "fixture", func(store.User) int { return 2 }); err == nil {
		t.Fatal("revoked device re-enrolled")
	}
	if err = a.store().ClaimMediaDevice(u.UID, "new", "new", func(store.User) int { return 2 }); err != nil {
		t.Fatal(err)
	}
}

func TestJianweiMemberSecurityCodeOwnership(t *testing.T) {
	a := newTestApp(t)
	u, _, err := a.store().EnsureTelegramProfile(5678, "secure-member", "fixture", -1)
	if err != nil {
		t.Fatal(err)
	}
	call := func(body map[string]any) bool {
		u, _ = a.store().User(u.UID)
		ok, _, _ := a.invokeMember(context.Background(), u, a.handleMemberSecurityCode, body)
		return ok
	}
	if !call(map[string]any{"new_code": "127836"}) {
		t.Fatal("initial security code failed")
	}
	if call(map[string]any{"new_code": "987654", "old_code": "999999"}) {
		t.Fatal("wrong old code accepted")
	}
	if !call(map[string]any{"new_code": "987654", "old_code": "127836"}) {
		t.Fatal("valid change rejected")
	}
	u, _ = a.store().User(u.UID)
	raw, _ := json.Marshal(publicUser(u))
	if strings.Contains(string(raw), u.SecurityCodeHash) || strings.Contains(string(raw), "987654") {
		t.Fatal("security secret leaked")
	}
}

func TestJianweiGatewayAuthAndQuota(t *testing.T) {
	a := newTestApp(t)
	var streamLimit atomic.Int32
	emby := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/Sessions":
			if r.Header.Get("X-Emby-Token") != "fixture-token" {
				w.WriteHeader(401)
				return
			}
			_, _ = w.Write([]byte(`[]`))
		case "/Users/media-fixture":
			_, _ = w.Write([]byte(`{"Id":"media-fixture","Policy":{"IsAdministrator":false,"SimultaneousStreamLimit":0}}`))
		case "/Users/media-fixture/Policy":
			var policy map[string]any
			_ = json.NewDecoder(r.Body).Decode(&policy)
			streamLimit.Store(int32(policy["SimultaneousStreamLimit"].(float64)))
			w.WriteHeader(204)
		default:
			w.WriteHeader(404)
		}
	}))
	defer emby.Close()
	a.cfg().EmbyURL = emby.URL
	a.cfg().EmbyToken = "admin-fixture"
	a.cfg().BotInternalSecret = strings.Repeat("x", 32)
	a.cfg().MediaLimitsEnabled = true
	a.cfg().MediaNormalDevices = 1
	a.cfg().MediaNormalStreams = 2
	u, err := a.store().CreateUser(store.User{Username: "media-owner", Role: store.RoleNormal, EmbyID: "media-fixture", ExpiredAt: time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store().RememberMediaClientLogin(a.mediaTokenHash("fixture-token"), "media-fixture", "one"); err != nil {
		t.Fatal(err)
	}
	request := func(secret, token, device, path, method string) int {
		r := httptest.NewRequest("GET", "/api/v2/internal/emby-access", nil)
		r.Header.Set("X-JW-Gateway", secret)
		r.Header.Set("X-JW-URI", path)
		r.Header.Set("X-JW-Method", method)
		r.Header.Set("X-JW-Client-IP", "192.0.2.5")
		r.Header.Set("X-Emby-Token", token)
		r.Header.Set("X-Emby-Device-Id", device)
		w := httptest.NewRecorder()
		a.handleMediaGatewayAccess(w, r, nil)
		return w.Code
	}
	secret := a.cfg().BotInternalSecret
	if request("bad", "fixture-token", "one", "/Items", "GET") != 403 {
		t.Fatal("untrusted proxy accepted")
	}
	if request(secret, "", "", "/System/Info/Public", "GET") != 204 {
		t.Fatal("discovery rejected")
	}
	if request(secret, "", "", "/Users/AuthenticateByName", "GET") != 405 {
		t.Fatal("invalid login method")
	}
	if request(secret, "bad", "one", "/Items", "GET") != 401 {
		t.Fatal("invalid user token")
	}
	if code := request(secret, "fixture-token", "one", "/Items", "GET"); code != 204 {
		t.Fatalf("valid client: %d", code)
	}
	if streamLimit.Load() != 2 {
		t.Fatal("native stream policy not applied")
	}
	if request(secret, "fixture-token", "", "/Items/poster/Images/Primary?api_key=fixture-token", "GET") != 204 {
		t.Fatal("admitted client image request lost its device association")
	}
	if request(secret, "fixture-token", "two", "/Items", "GET") != 403 {
		t.Fatal("second device exceeded quota")
	}
	if request(secret, "fixture-token", "", "/Items/poster/Images/Primary?api_key=fixture-token", "GET") != 204 {
		t.Fatal("rejected second device corrupted admitted device association")
	}
	a.cfg().MediaLimitsEnabled = false
	if request(secret, "fixture-token", "one", "/Items", "GET") != 204 || streamLimit.Load() != 0 {
		t.Fatal("disabling managed limits did not clear native policy")
	}
	_, _ = a.store().UpdateUser(u.UID, func(next *store.User) error { next.Active = false; return nil })
	if request(secret, "fixture-token", "one", "/Items", "GET") != 403 {
		t.Fatal("cached token bypassed user disable")
	}
}
