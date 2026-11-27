package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/prejudice-studio/twilight/internal/store"
)

func TestJianweiServiceLibrariesAndWhitelistHostsEnforceBothLayers(t *testing.T) {
	a := newTestApp(t)
	var posted map[string]any
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/Library/VirtualFolders":
			w.Write([]byte(`[{"ItemId":"movie","Name":"电影"},{"ItemId":"music","Name":"音乐"},{"ItemId":"books","Name":"小说"}]`))
		case r.Method == "POST":
			json.NewDecoder(r.Body).Decode(&posted)
			w.WriteHeader(204)
		default:
			w.Write([]byte(`{"Id":"member","Policy":{"IsAdministrator":false,"IsDisabled":false,"EnableAllFolders":true}}`))
		}
	}))
	defer remote.Close()
	a.cfg().EmbyURL = remote.URL
	a.cfg().EmbyToken = "fixture"
	a.cfg().MediaLimitsEnabled = false
	services := a.store().Services().Items
	services[0].Enabled = true
	services[0].LibraryIDs = []string{"music"}
	services[0].Lines = []string{"https://music.example.test"}
	services[1].Enabled = true
	services[1].LibraryIDs = []string{"books"}
	services[1].Lines = []string{"https://books.example.test"}
	if e := a.store().SaveServices(services, 0); e != nil {
		t.Fatal(e)
	}
	now := time.Now().Unix()
	u, e := a.store().CreateUser(store.User{Username: "service-member", EmbyID: "member", Active: true, Role: store.RoleNormal, ExpiredAt: now + 86400})
	if e != nil {
		t.Fatal(e)
	}
	check := func(want []string) {
		t.Helper()
		if e := a.applyMediaStreamLimit(context.Background(), u); e != nil {
			t.Fatal(e)
		}
		bytes, _ := json.Marshal(posted["EnabledFolders"])
		var got []string
		json.Unmarshal(bytes, &got)
		if posted["EnableAllFolders"] != false || !reflect.DeepEqual(got, want) || posted["IsAdministrator"] != false {
			t.Fatal("library ACL", posted)
		}
	}
	check([]string{"movie"})
	if matched, allowed := a.serviceHostAccess(u, "music.example.test"); !matched || allowed {
		t.Fatal("ordinary account service host allowed")
	}
	u.Role = store.RoleWhitelist
	u.WhitelistUntil = now + 100
	check([]string{"books", "movie", "music"})
	if _, allowed := a.serviceHostAccess(u, "music.example.test"); !allowed {
		t.Fatal("white denied music")
	}
	u.WhitelistUntil = now - 1
	check([]string{"movie"})
	if _, allowed := a.serviceHostAccess(u, "books.example.test"); allowed {
		t.Fatal("expired white retained access")
	}
	u.Role = store.RoleAdmin
	check([]string{"books", "movie", "music"})
	u.Role = store.RoleNormal
	u.WhitelistUntil = 0
	if e = a.store().UpsertRegCode(store.RegCode{Code: "music-service-001", ServiceID: "music", Type: 1, Days: 30, Active: true, UseCountLimit: 1, CreatedAt: now, ValidityTime: 720}); e != nil {
		t.Fatal(e)
	}
	// The store reads the canonical role; persist the test account's ordinary state.
	u, e = a.store().ConsumeServiceCode(u.UID, 0, "music-service-001", now)
	if e != nil {
		t.Fatal(e)
	}
	check([]string{"movie", "music"})
	u.ExpiredAt = now - 1
	u.RetentionAccessUntil = 0
	check([]string{"music"})
	if !a.embyShouldEnableUser(u) || a.memberShouldDisableForWebState(u) {
		t.Fatal("movie expiry disabled an active independent service")
	}
}

func TestJianweiTelegramRequestBudgetReservesFallbackTime(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
		w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	start := time.Now()
	_, e := telegramPostResultToEndpoint[telegramDiscardResult](ctx, server.URL, "sendPhoto", map[string]string{}, 30*time.Millisecond)
	if e == nil || time.Since(start) > 500*time.Millisecond || ctx.Err() != nil {
		t.Fatal("optional photo consumed parent deadline", e)
	}
}

func TestJianweiBuiltinBannerUploadsBytesAndCachesTelegramFileID(t *testing.T) {
	a := newTestApp(t)
	a.cfg().TelegramMode = true
	a.cfg().TelegramBotToken = "fixture-photo"
	a.cfg().PublicBaseURL = "https://mini.example.test"
	a.cfg().TelegramPanelImageEnabled = true
	uploads, cached := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
			uploads++
			if e := r.ParseMultipartForm(4 << 20); e != nil {
				t.Error(e)
			}
			f, _, e := r.FormFile("photo")
			if e != nil {
				t.Error(e)
			} else {
				f.Close()
			}
			if !strings.Contains(r.FormValue("reply_markup"), "jw:/panel") {
				t.Error("lost panel buttons")
			}
			w.Write([]byte(`{"ok":true,"result":{"photo":[{"file_id":"cached-hero-id"}]}}`))
		} else {
			cached++
			var v map[string]any
			json.NewDecoder(r.Body).Decode(&v)
			if v["photo"] != "cached-hero-id" {
				t.Error("did not reuse uploaded file")
			}
			w.Write([]byte(`{"ok":true,"result":true}`))
		}
	}))
	defer server.Close()
	a.cfg().TelegramAPIURL = server.URL
	key := a.mediaTokenHash(server.URL + "/botfixture-photo/sendPhoto")
	memberHeroIDs.Delete(key)
	markup := map[string]any{"inline_keyboard": [][]map[string]any{{memberButton("面板", "/panel")}}}
	if e := a.sendBuiltinMemberHero(context.Background(), 4321, "fixture", markup); e != nil {
		t.Fatal(e)
	}
	if e := a.sendBuiltinMemberHero(context.Background(), 4321, "fixture", markup); e != nil {
		t.Fatal(e)
	}
	if uploads != 1 || cached != 1 {
		t.Fatal("photo cache", uploads, cached)
	}
}
