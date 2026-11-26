package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/prejudice-studio/twilight/internal/config"
	"github.com/prejudice-studio/twilight/internal/store"
)

func TestJianweiControlRegistryHas24UniqueModulesAndExcludesSecrets(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range controlModules() {
		if seen[m.ID] || m.Name == "" || !strings.HasPrefix(m.URL, "/admin") {
			t.Fatal("invalid control module", m)
		}
		seen[m.ID] = true
	}
	if len(seen) != 24 {
		t.Fatal("module omission", len(seen))
	}
	for _, section := range configSectionDefs() {
		for _, f := range section.Fields {
			if f.Type == "secret" && consoleFieldAllowed(section.Key, f) {
				t.Fatal("secret entered revision journal", section.Key, f.Key)
			}
		}
	}
	for _, key := range []string{"admin_ids", "admin_uids", "repo_url", "cors_origins", "databases_dir"} {
		if consoleFieldAllowed("Global", configFieldDef{Key: key, Type: "string"}) {
			t.Fatal("protected access field editable", key)
		}
	}
}
func TestJianweiFinanceControlAndExportRejectOrdinaryAndUnauthenticated(t *testing.T) {
	a := newTestApp(t)
	cookies := registerAndLogin(t, a, "ordinary-finance", "FinanceReader123")
	for _, path := range []string{"/api/v2/admin/finance", "/api/v2/admin/control", "/api/v2/admin/play-rank/export"} {
		if w := doJSON(a, http.MethodGet, path, "", nil); w.Code != 401 {
			t.Fatal(path, "anonymous", w.Code)
		}
		if w := doJSON(a, http.MethodGet, path, "", cookies); w.Code != 403 {
			t.Fatal(path, "ordinary", w.Code)
		}
	}
}
func TestJianweiPhotoCallbacksStayOnOriginatingCaption(t *testing.T) {
	a := newTestApp(t)
	ctx := context.WithValue(context.Background(), memberPanelContextKey{}, memberPanelTarget{ChatID: 4321, MessageID: 91, Photo: true})
	method, body := a.memberPanelEdit(ctx, "sendMessage", telegramSendMessageRequest{ChatID: int64(4321), Text: "账号与线路"})
	if method != "editMessageCaption" {
		t.Fatal("photo callback sent another message", method)
	}
	raw, _ := json.Marshal(body)
	if !strings.Contains(string(raw), `"caption":"账号与线路"`) || !strings.Contains(string(raw), `"message_id":91`) {
		t.Fatal("caption target mismatch")
	}
}
func TestJianweiRegistrationAndExpiredWhitelistCannotReadAllRoutes(t *testing.T) {
	a := newTestApp(t)
	a.cfg().EmbyPublicURL = "https://normal.example.test"
	a.cfg().EmbyWhitelistURL = "https://white.example.test"
	for _, u := range []store.User{{UID: 1, Role: store.RoleNormal, Active: true, PendingEmby: true}, {UID: 2, Role: store.RoleWhitelist, Active: true}, {UID: 3, Role: store.RoleWhitelist, Active: true, EmbyID: "member", WhitelistUntil: time.Now().Unix() - 1, ExpiredAt: time.Now().Unix() + 86400}} {
		req := httptest.NewRequest(http.MethodGet, "/lines", nil)
		req = req.WithContext(context.WithValue(req.Context(), principalKey, principal{User: u}))
		w := httptest.NewRecorder()
		a.handleEmbyURLs(w, req, nil)
		if strings.Contains(w.Body.String(), "white.example.test") || u.EmbyID == "" && strings.Contains(w.Body.String(), "normal.example.test") {
			t.Fatal("unearned route access", w.Body.String())
		}
	}
}

func TestJianweiPlaybackHostNormalizesOnlyDefaultPort(t *testing.T) {
	for raw, expected := range map[string]string{"https://WHITE.Example.test:443/": "white.example.test", "https://white.example.test:8443": "white.example.test:8443", "http://normal.example.test:80": "normal.example.test"} {
		v, e := url.Parse(raw)
		if e != nil || normalizedPlaybackHost(v) != expected {
			t.Fatal(raw, normalizedPlaybackHost(v), e)
		}
	}
}

func TestJianweiListOnlyPlaybackDoesNotDuplicatePrimaryCertificate(t *testing.T) {
	a := newTestApp(t)
	a.cfg().EmbyPublicURL = ""
	a.cfg().EmbyURLList = []config.Line{{URL: "https://normal.example.test"}, {URL: "https://second.example.test:443"}}
	a.cfg().EmbyWhitelistURL = "https://white.example.test"
	origins := a.configuredIngressOrigins()
	if origins["playback"] != "https://normal.example.test" || len(origins) != 5 {
		t.Fatal(origins)
	}
	for key, value := range origins {
		if strings.HasPrefix(key, "playback-") && value == origins["playback"] {
			t.Fatal("duplicate primary publication", origins)
		}
	}
}

func TestJianweiPlaybackHostAuthorizationReadsCurrentRole(t *testing.T) {
	a := newTestApp(t)
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("X-Emby-Token") == "native-admin" {
			_, _ = w.Write([]byte(`{"Id":"member-host","Policy":{"IsAdministrator":true}}`))
			return
		}
		_, _ = w.Write([]byte(`{"Id":"member-host","Policy":{"IsAdministrator":false}}`))
	}))
	defer remote.Close()
	a.cfg().EmbyURL = remote.URL
	a.cfg().EmbyToken = "fixture-admin"
	a.cfg().BotInternalSecret = strings.Repeat("x", 32)
	a.cfg().MediaLimitsEnabled = false
	a.cfg().EmbyPublicURL = "https://normal.example.test"
	a.cfg().EmbyWhitelistURL = "https://white.example.test"
	u, e := a.store().CreateUser(store.User{Username: "host-member", Role: store.RoleNormal, Active: true, EmbyID: "member-host", ExpiredAt: time.Now().Unix() + 86400})
	if e != nil {
		t.Fatal(e)
	}
	call := func(host, token string) int {
		r := httptest.NewRequest("GET", "/api/v2/internal/emby-access", nil)
		r.Header.Set("X-JW-Gateway", a.cfg().BotInternalSecret)
		r.Header.Set("X-JW-URI", "/Items")
		r.Header.Set("X-JW-Method", "GET")
		r.Header.Set("X-JW-Host", host)
		r.Header.Set("X-Emby-Token", token)
		r.Header.Set("X-Emby-Device-Id", "phone")
		w := httptest.NewRecorder()
		a.handleMediaGatewayAccess(w, r, nil)
		return w.Code
	}
	if call("normal.example.test:443", "member-token") != 204 || call("white.example.test", "member-token") != 403 {
		t.Fatal("ordinary role boundary")
	}
	_, e = a.store().UpdateUser(u.UID, func(next *store.User) error { next.Role = store.RoleWhitelist; return nil })
	if e != nil {
		t.Fatal(e)
	}
	if call("white.example.test", "member-token") != 204 {
		t.Fatal("whitelist route denied")
	}
	_, e = a.store().UpdateUser(u.UID, func(next *store.User) error { next.WhitelistUntil = time.Now().Unix() - 1; return nil })
	if e != nil {
		t.Fatal(e)
	}
	if call("white.example.test", "member-token") != 403 {
		t.Fatal("cached identity retained expired white privileges")
	}
	_, e = a.store().UpdateUser(u.UID, func(next *store.User) error { next.Role = store.RoleAdmin; return nil })
	if e != nil {
		t.Fatal(e)
	}
	if call("white.example.test", "member-token") != 204 || call("unknown.example.test", "member-token") != 403 {
		t.Fatal("admin still must use configured route")
	}
	if call("normal.example.test", "native-admin") != 403 {
		t.Fatal("native server administrator exposed")
	}
}

func TestJianweiEmbyDeviceImportUsesReportedIDAndOwnAccount(t *testing.T) {
	a := newTestApp(t)
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Items":[{"Id":"7","ReportedDeviceId":"reported-own","Name":"Phone","AppName":"Yamby","AppVersion":"2.1","LastUserId":"mine"},{"Id":"8","ReportedDeviceId":"reported-other","Name":"Other","LastUserId":"other"}]}`))
	}))
	defer remote.Close()
	a.cfg().EmbyURL = remote.URL
	a.cfg().EmbyToken = "fixture"
	u, e := a.store().CreateUser(store.User{Username: "device-import", Role: store.RoleNormal, Active: true, EmbyID: "mine", ExpiredAt: time.Now().Unix() + 86400})
	if e != nil {
		t.Fatal(e)
	}
	if e = a.store().ClaimMediaDevice(u.UID, "7", "Phone", func(store.User) int { return 5 }); e != nil {
		t.Fatal(e)
	}
	success, message, _ := a.invokeMember(context.Background(), u, a.handleMemberDeviceSync, nil)
	if !success {
		t.Fatal(message)
	}
	after, _ := a.store().User(u.UID)
	if len(after.MediaDevices) != 1 || after.MediaDevices[0].ID != "reported-own" || after.MediaDevices[0].Client != "Yamby" || after.MediaDevices[0].ClientVersion != "2.1" {
		t.Fatal(after.MediaDevices)
	}
	if e = a.store().RemoveMediaDevice(u.UID, "reported-own"); e != nil {
		t.Fatal(e)
	}
	success, message, _ = a.invokeMember(context.Background(), after, a.handleMemberDeviceSync, nil)
	if !success {
		t.Fatal(message)
	}
	after, _ = a.store().User(u.UID)
	if len(after.MediaDevices) != 0 {
		t.Fatal("sync restored revoked device", after.MediaDevices)
	}
}
