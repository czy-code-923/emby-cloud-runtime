package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prejudice-studio/twilight/internal/store"
)

func TestJianweiRegistrationDisplayUsesEmbyLimitAndOnlyUnregisteredMembers(t *testing.T) {
	a := newTestApp(t)
	a.cfg().EmbyUserLimit, a.cfg().UserLimit = 37, 999
	web, err := a.store().CreateUser(store.User{Username: "private-web-name", TelegramUsername: "private-telegram-name", TelegramID: 536781, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	media, err := a.store().CreateUser(store.User{Username: "private-web-media", TelegramID: 536782, Active: true, EmbyID: "media-id", EmbyUsername: "media-only", ExpiredAt: -1})
	if err != nil {
		t.Fatal(err)
	}
	v := a.memberAccount(web)["registration"].(*store.EmbyRegistrationSummary)
	if v.Limit != 37 || v.Registered != 1 || v.Remaining == nil || *v.Remaining != 36 {
		t.Fatalf("wrong capacity source: %+v", v)
	}
	for _, role := range []int{store.RoleNormal, store.RoleAdmin, store.RoleWhitelist} {
		bound := media
		bound.Role, bound.EmbyDisabled, bound.ExpiredAt, bound.WhitelistUntil = role, true, 1, 1
		if _, exists := a.memberAccount(bound)["registration"]; exists {
			t.Fatal("bound, disabled or expired Emby member received registration capacity")
		}
	}
	web.PendingEmby = true
	if a.memberRegistrationFor(web) == nil {
		t.Fatal("pending user without Emby should still see capacity")
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`[]`)) }))
	defer upstream.Close()
	a.cfg().EmbyURL, a.cfg().EmbyToken = upstream.URL, "fixture-only"
	for _, u := range []store.User{web, media} {
		success, message, data := a.invokeMember(context.Background(), u, a.handleMemberStatus, nil)
		if !success {
			t.Fatal(message)
		}
		_, exists := data.(map[string]any)["registration"]
		if exists != (u.EmbyID == "") {
			t.Fatal("service summary did not enforce registration visibility")
		}
	}
}

func TestJianweiRegistrationBotProfileNeverFallsBackToWebIdentity(t *testing.T) {
	a := newTestApp(t)
	a.cfg().TelegramMode, a.cfg().TelegramBotToken = true, "fixture-only"
	var sent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		sent, _ = body["text"].(string)
		w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer server.Close()
	a.cfg().TelegramAPIURL = server.URL
	u, err := a.store().CreateUser(store.User{Username: "private-web-profile", TelegramUsername: "private-tg-profile", TelegramID: 536783, Active: true, EmbyID: "media-profile", EmbyUsername: "film-user", ExpiredAt: time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	a.telegramMemberAccountInfo(context.Background(), telegramCommandCtx{FromID: u.TelegramID, ChatID: u.TelegramID})
	if !strings.Contains(sent, "· 💠 Emby账号 | film-user") || !strings.Contains(sent, "· 📊 当前状态 | 已开通") {
		t.Fatal(sent)
	}
	for _, secret := range []string{u.Username, u.TelegramUsername, "536783", "Telegram ID", "总注册席位"} {
		if strings.Contains(sent, secret) {
			t.Fatalf("profile exposed non-media identity or registration data: %s", secret)
		}
	}
	a.telegramMemberPanel(context.Background(), u.TelegramID, u)
	if strings.Contains(sent, "总注册席位") {
		t.Fatal("registered Bot home exposed capacity")
	}
	u, err = a.store().UpdateUser(u.UID, func(v *store.User) error { v.EmbyID, v.EmbyUsername = "", ""; return nil })
	if err != nil {
		t.Fatal(err)
	}
	a.telegramMemberAccountInfo(context.Background(), telegramCommandCtx{FromID: u.TelegramID, ChatID: u.TelegramID})
	if !strings.Contains(sent, "· 📊 当前状态 | 未注册") || strings.Contains(sent, "private-web-profile") {
		t.Fatal(sent)
	}
	a.telegramMemberPanel(context.Background(), u.TelegramID, u)
	if !strings.Contains(sent, "总注册席位") || !strings.Contains(sent, "已注册Emby") {
		t.Fatal(sent)
	}
}
