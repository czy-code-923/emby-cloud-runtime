package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/prejudice-studio/twilight/internal/store"
)

func TestJianweiDiscoveryUsesBoundEmbyUserAndSafeProjection(t *testing.T) {
	a := newTestApp(t)
	var favoriteWrites atomic.Int64
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/Users/bound-user/Items":
			if r.URL.Query().Get("Limit") != "20" || r.URL.Query().Get("IsFavorite") != "true" {
				t.Error("unbounded favorites query")
			}
			_, _ = w.Write([]byte(`{"Items":[{"Id":"42","Name":"Fixture film","Type":"Movie","Path":"private-path","UserData":{"IsFavorite":true}}],"TotalRecordCount":1}`))
		case "/Users/bound-user/Items/42":
			_, _ = w.Write([]byte(`{"Id":"42","Name":"Fixture film"}`))
		case "/Users/bound-user/FavoriteItems/42":
			if r.Method != "DELETE" {
				t.Error("unexpected mutation")
			}
			favoriteWrites.Add(1)
			w.WriteHeader(204)
		default:
			t.Error("foreign user or unexpected upstream path", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer remote.Close()
	a.cfg().EmbyURL = remote.URL
	a.cfg().EmbyToken = "fixture-admin-token"
	u, err := a.store().CreateUser(store.User{Username: "bound", EmbyID: "bound-user", Active: true})
	if err != nil {
		t.Fatal(err)
	}
	request := func(path, body string, handler HandlerFunc) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r = r.WithContext(context.WithValue(r.Context(), principalKey, principal{User: u}))
		w := httptest.NewRecorder()
		handler(w, r, nil)
		return w
	}
	w := request("/api/v2/miniapp/favorites?user_id=other", "", a.handleMemberLibrary)
	if w.Code != 200 || strings.Contains(w.Body.String(), "private-path") || strings.Contains(w.Body.String(), "fixture-admin-token") {
		t.Fatal("unsafe favorites result", w.Body)
	}
	w = request("/api/v2/miniapp/favorite", `{"uid":999,"emby_id":"other","item_id":"42","favorite":false}`, a.handleMemberFavorite)
	if w.Code != 200 || favoriteWrites.Load() != 1 {
		t.Fatal("own favorite update failed", w.Body)
	}
	w = request("/api/v2/miniapp/favorite", `{"item_id":"../Users/other","favorite":true}`, a.handleMemberFavorite)
	if w.Code < 400 || favoriteWrites.Load() != 1 {
		t.Fatal("path traversal accepted")
	}
	a.cfg().PlayRankEnabled = false
	if w = request("/api/v2/miniapp/rank", "", a.handleMemberRank); w.Code != 403 {
		t.Fatal("rank feature gate bypassed")
	}
}

func TestJianweiCallbackWireTransportUsesEditAndNoNewMessage(t *testing.T) {
	a := newTestApp(t)
	a.cfg().TelegramMode = true
	a.cfg().TelegramBotToken = "fixture"
	var edits, sends atomic.Int64
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sendMessage") {
			sends.Add(1)
		}
		if strings.HasSuffix(r.URL.Path, "/editMessageText") {
			edits.Add(1)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["message_id"] != float64(777) {
			t.Error("lost callback message identifier")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":777}}`))
	}))
	defer remote.Close()
	a.cfg().TelegramAPIURL = remote.URL
	ctx := context.WithValue(context.Background(), memberPanelContextKey{}, memberPanelTarget{ChatID: 4321, MessageID: 777})
	if err := a.telegramSendMessage(ctx, int64(4321), "fixture button result"); err != nil {
		t.Fatal(err)
	}
	if edits.Load() != 1 || sends.Load() != 0 {
		t.Fatal("callback response created a new message")
	}
}
