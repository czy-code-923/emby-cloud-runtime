package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/prejudice-studio/twilight/internal/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJianweiBotAdministratorReceiptRequiresRolePrivateAndConfirmation(t *testing.T) {
	t.Setenv("TWILIGHT_TWO_FACTOR_KEY", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("b", 32))))
	a := newTestApp(t)
	a.cfg().TelegramMode = true
	a.cfg().TelegramBotToken = "fixture-admin-shop"
	requests := []map[string]any{}
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		requests = append(requests, b)
		w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer remote.Close()
	a.cfg().TelegramAPIURL = remote.URL
	admin, e := a.store().CreateUser(store.User{Username: "receipt-bot-admin", Role: store.RoleAdmin, Active: true, TelegramID: 123451})
	if e != nil {
		t.Fatal(e)
	}
	buyer, _, e := a.store().EnsureTelegramProfile(123452, "receipt-bot-buyer", "fixture", -1)
	if e != nil {
		t.Fatal(e)
	}
	cfg := store.ShopSettings{Enabled: true, Channel: "fixture", ReceivingAccount: "fixture", Instructions: "fixture", Products: []store.ShopProduct{{ID: "open", Name: "fixture", Kind: 1, Days: 30, PriceCents: 1999, Enabled: true, Stock: -1}}}
	if e = a.store().SaveShopConfiguration(cfg, 0); e != nil {
		t.Fatal(e)
	}
	o, e := a.store().CreateShopOrder(buyer.UID, strings.Repeat("a", 24), "open", strings.Repeat("b", 24), 1, 10, false)
	if e != nil {
		t.Fatal(e)
	}
	o, e = a.store().SubmitShopPayment(buyer.UID, o.ID, "fixture", false)
	if e != nil {
		t.Fatal(e)
	}
	a.runShopAdminNotices(context.Background())
	before := len(requests)
	a.runShopAdminNotices(context.Background())
	if before != 1 || len(requests) != 1 {
		t.Fatal("notice delivery not idempotent", len(requests))
	}
	call := func(from, chat int64, action string) {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"id": "fixture-callback", "data": shopAdminCallback(action, o), "from": map[string]any{"id": from}, "message": map[string]any{"message_id": 7, "chat": map[string]any{"id": chat, "type": "private"}}})
		var callback telegramCallbackQuery
		_ = json.Unmarshal(raw, &callback)
		if !a.telegramHandleShopAdminCallback(context.Background(), &callback) {
			t.Fatal("unhandled callback")
		}
	}
	call(buyer.TelegramID, buyer.TelegramID, "accept")
	call(admin.TelegramID, -100, "accept")
	fresh, _ := a.store().ShopOrder(o.ID)
	if fresh.Status != "review" {
		t.Fatal("non-admin or group approved")
	}
	call(admin.TelegramID, admin.TelegramID, "approve")
	fresh, _ = a.store().ShopOrder(o.ID)
	if fresh.Status != "review" {
		t.Fatal("preview mutated payment")
	}
	call(admin.TelegramID, admin.TelegramID, "accept")
	fresh, _ = a.store().ShopOrder(o.ID)
	if fresh.Status != "issued" || fresh.ReceivedCents != 1999 || fresh.CheckedBy != admin.UID {
		t.Fatal("approval failed", fresh.Status, requests[len(requests)-1]["text"])
	}
	key := fresh.CodeKey
	call(admin.TelegramID, admin.TelegramID, "accept")
	fresh, _ = a.store().ShopOrder(o.ID)
	if fresh.CodeKey != key {
		t.Fatal("duplicate callback minted code")
	}
	if a.store().Wallet(buyer.UID).Balance != 0 {
		t.Fatal("cash order changed wallet")
	}
}

func TestJianweiLegacyCopyRefreshesPrivatePanelWithoutHelperButtons(t *testing.T) {
	a := newTestApp(t)
	a.cfg().TelegramMode = true
	a.cfg().TelegramBotToken = "fixture-copy"
	_, _, e := a.store().EnsureTelegramProfile(53455, "copy-member", "fixture", -1)
	if e != nil {
		t.Fatal(e)
	}
	texts := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		if text, ok := b["text"].(string); ok {
			texts = append(texts, text)
		}
		w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer server.Close()
	a.cfg().TelegramAPIURL = server.URL
	raw := `{"id":"copy-fixture","data":"jwc:value:0","from":{"id":53455},"message":{"message_id":9,"chat":{"id":53455,"type":"private"},"reply_markup":{"inline_keyboard":[[{"text":"复制账号","copy_text":{"text":"fixture-copy-value"}}]]}}}`
	var callback telegramCallbackQuery
	_ = json.Unmarshal([]byte(raw), &callback)
	if !a.telegramHandleMemberCallback(context.Background(), &callback) {
		t.Fatal("copy unhandled")
	}
	if len(texts) != 2 || !strings.Contains(texts[1], "当前状态 | 未注册") || strings.Contains(texts[1], "fixture-copy-value") {
		t.Fatal("obsolete copy panel was not replaced safely", texts)
	}
	before := len(texts)
	callback.Message.Chat.ID = -100
	a.telegramHandleMemberCallback(context.Background(), &callback)
	if len(texts) != before {
		t.Fatal("copy leaked to group")
	}
	callback.Message.Chat.ID = callback.From.ID
	for _, data := range []string{"jwc:value:", "jwc:value:0:extra", "jwc:value:-1", "jwc:value:01", "jwc:value:21", "jwc:copyhelp:extra"} {
		callback.Data = data
		a.telegramHandleMemberCallback(context.Background(), &callback)
	}
	if len(texts) != before {
		t.Fatal("malformed legacy callbacks triggered panel actions")
	}
}
