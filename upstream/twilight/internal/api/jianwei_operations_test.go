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
	for _, key := range []string{"admin_uids", "repo_url", "cors_origins", "databases_dir"} {
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
