package api

import (
	"context"
	"github.com/prejudice-studio/twilight/internal/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestJianweiPlaybackLoginSupportedAPIAndIdentityPersistence(t *testing.T) {
	a := newTestApp(t)
	u, e := a.store().CreateUser(store.User{Username: "ordinary-play", Active: true, Role: store.RoleAdmin, EmbyID: "play-id", ExpiredAt: -1})
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/Users/AuthenticateByName":
			if strings.Contains(r.Header.Get("X-Emby-Authorization"), "native") {
				w.Write([]byte(`{"AccessToken":"native-token","User":{"Id":"native-id","Policy":{"IsAdministrator":true}}}`))
				return
			}
			w.Write([]byte(`{"AccessToken":"user-token","User":{"Id":"play-id","Policy":{"IsAdministrator":false}},"SessionInfo":{"DeviceId":"d1"}}`))
		case "/Sessions":
			if r.Header.Get("X-Emby-Token") != "user-token" {
				w.WriteHeader(401)
				return
			}
			w.Write([]byte(`[]`))
		case "/Users/play-id":
			w.Write([]byte(`{"Id":"play-id","Policy":{"IsAdministrator":false}}`))
		case "/Users/Me":
			t.Error("unsupported API called")
			w.WriteHeader(500)
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	a.cfg().EmbyURL = server.URL
	a.cfg().EmbyToken = "server-token"
	a.cfg().BotInternalSecret = strings.Repeat("s", 32)
	login := func(secret, header string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/v2/internal/emby-login", strings.NewReader(`{"Username":"ordinary-play","Pw":"fixture"}`))
		r.Header.Set("X-JW-Gateway", secret)
		r.Header.Set("X-Emby-Authorization", header)
		w := httptest.NewRecorder()
		a.handleMediaClientLogin(w, r, nil)
		return w
	}
	if w := login("bad", ""); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w := login(a.cfg().BotInternalSecret, "native"); w.Code != 403 || strings.Contains(w.Body.String(), "native-token") {
		t.Fatal("native administrator escaped", w.Body)
	}
	if w := login(a.cfg().BotInternalSecret, ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	verified, ok := a.store().MediaClientLogin(a.mediaTokenHash("user-token"))
	if !ok || verified.EmbyID != u.EmbyID {
		t.Fatal("identity was not persisted")
	}
	a.mediaIdentity.items = nil
	r := httptest.NewRequest("GET", "/api/v2/internal/emby-access", nil)
	r.Header.Set("X-JW-Gateway", a.cfg().BotInternalSecret)
	r.Header.Set("X-JW-URI", "/Users/play-id/Views")
	r.Header.Set("X-JW-Method", "GET")
	r.Header.Set("X-Emby-Token", "user-token")
	r.Header.Set("X-Emby-Device-Id", "d1")
	w := httptest.NewRecorder()
	a.handleMediaGatewayAccess(w, r, nil)
	if w.Code != 204 {
		t.Fatal("administrator ordinary player blocked", w.Code, w.Body)
	}
	r.Header.Set("X-JW-URI", "/Users/forged/Views")
	w = httptest.NewRecorder()
	a.handleMediaGatewayAccess(w, r, nil)
	if w.Code != 403 {
		t.Fatal("cross-account library request accepted", w.Code)
	}
	r.Header.Set("X-JW-URI", "/Users/play-id/Views")
	// The bound subject remains the authenticated ID even if the URL names another user.
	if got, _ := a.store().MediaClientLogin(a.mediaTokenHash("user-token")); got.EmbyID == "forged" {
		t.Fatal("client controlled identity")
	}
	_, _ = a.store().UpdateUser(u.UID, func(v *store.User) error { v.Active = false; return nil })
	w = httptest.NewRecorder()
	a.handleMediaGatewayAccess(w, r, nil)
	if w.Code != 403 {
		t.Fatal("disabled subject bypassed cache", w.Code)
	}
	_ = context.Background()
	_ = time.Now()
}
