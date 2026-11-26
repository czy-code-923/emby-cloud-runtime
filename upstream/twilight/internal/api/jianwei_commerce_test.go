package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prejudice-studio/twilight/internal/store"
)

func TestJianweiShopCookieIsolationOwnershipAndCipher(t *testing.T) {
	a := newTestApp(t)
	initial, err := json.Marshal(a.store().ShopConfiguration())
	if err != nil || !strings.Contains(string(initial), `"products":[]`) {
		t.Fatal("empty catalogue must be an array for the first-run editor", string(initial), err)
	}
	a.cfg().TelegramMode = true
	a.cfg().TelegramBotToken = "shop-test-bot"
	a.cfg().CommerceStoreURL = "https://shop.example.test"
	a.cfg().CORSOrigins = append(a.cfg().CORSOrigins, a.cfg().CommerceStoreURL)
	t.Setenv("TWILIGHT_TWO_FACTOR_KEY", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
	u, _, err := a.store().EnsureTelegramProfile(812345, "buyer", "fixture", -1)
	if err != nil {
		t.Fatal(err)
	}
	v, _, err := a.store().EnsureTelegramProfile(812346, "other", "fixture", -1)
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("a", 64)
	if err = a.store().CreateMiniSession(context.Background(), u.UID, u.TelegramID, "shop-proof", token, "shop-session|shop-test-bot", token, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	request := func(method, path, origin, intent, cookie, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Origin", origin)
		r.Header.Set("X-Twilight-Client", "webui")
		r.Header.Set("X-Twilight-Intent", intent)
		r.Header.Set("Content-Type", "application/json")
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: cookie, Value: token})
		}
		w := httptest.NewRecorder()
		a.ServeHTTP(w, r)
		return w
	}
	for _, cookie := range []string{"", miniCookie} {
		if w := request("GET", "/api/v2/commerce/orders", "", "", cookie, ""); w.Code < 400 {
			t.Fatal("wrong cookie audience accepted")
		}
	}
	if w := request("POST", "/api/v2/commerce/orders", "https://foreign.test", "shop-action", shopCookie, `{}`); w.Code < 400 {
		t.Fatal("foreign origin accepted")
	}
	settings := store.ShopSettings{Enabled: true, Instructions: "test instructions", Channel: "test", ReceivingAccount: "fixture recipient", Products: []store.ShopProduct{{ID: "new", Name: "fixture product", Kind: 1, Days: 30, PriceCents: 1000, Stock: -1, Enabled: true}}}
	if err = a.store().SaveShopConfiguration(settings, 0); err != nil {
		t.Fatal(err)
	}
	other, err := a.store().CreateShopOrder(v.UID, "foreign-order-0000001", "new", "foreign-request-00001", 1, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if w := request("POST", "/api/v2/commerce/orders/"+other.ID+"/payment", a.cfg().CommerceStoreURL, "shop-action", shopCookie, `{"note":"forged","uid":999}`); w.Code < 400 {
		t.Fatal("foreign payment state mutated")
	}
	code := "JW-P-" + strings.Repeat("B", 32)
	digest, err := a.shopDigest("code", code)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := a.shopCipher("order", code, false)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := a.shopCipher("order", sealed, true)
	if err != nil || opened != code || strings.Contains(sealed, code) {
		t.Fatal("cipher roundtrip failed", err)
	}
	if _, err = a.shopCipher("other-order", sealed, true); err == nil {
		t.Fatal("delivery moved between orders")
	}
	if err = a.store().UpsertRegCode(store.RegCode{Code: digest, Source: "commerce", Type: 2, Days: 30, Active: true, UseCountLimit: 1, TargetUID: u.UID, CreatedAt: time.Now().Unix()}); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(a.shopOrderView(store.ShopOrder{ID: "order", CodeKey: digest, CodeCipher: sealed, Status: "issued"}, true))
	if strings.Contains(string(raw), sealed) || strings.Contains(string(raw), digest) || strings.Contains(string(raw), code) {
		t.Fatal("admin list leaked delivered code")
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/redeem", strings.NewReader(`{"code":"`+digest+`"}`))
	req = req.WithContext(context.WithValue(req.Context(), principalKey, principal{User: u}))
	a.handleUseCode(rr, req, nil)
	if rr.Code < 400 {
		t.Fatal("internal digest accepted as payment code")
	}
	if w := request("POST", "/api/v2/commerce/logout", a.cfg().CommerceStoreURL, "shop-action", shopCookie, `{}`); w.Code != 200 {
		t.Fatal(w.Body)
	}
	if w := request("GET", "/api/v2/commerce/orders", "", "", shopCookie, ""); w.Code != 401 {
		t.Fatal("logout session still valid")
	}
}

func TestJianweiPanelCallbacksEditOriginatingMessage(t *testing.T) {
	a := newTestApp(t)
	a.cfg().TelegramShopEnabled = true
	a.cfg().CommerceStoreURL = "https://shop.example.test"
	ctx := context.WithValue(context.Background(), memberPanelContextKey{}, memberPanelTarget{ChatID: 4321, MessageID: 777})
	method, body := a.memberPanelEdit(ctx, "sendMessage", telegramSendMessageRequest{ChatID: int64(4321), Text: "native result"})
	if method != "editMessageText" {
		t.Fatal("callback sent a new message")
	}
	m := body.(map[string]any)
	if m["message_id"] != int64(777) {
		t.Fatal("wrong message edited")
	}
	raw, _ := json.Marshal(m)
	if !strings.Contains(string(raw), "jw:/panel") || !strings.Contains(string(raw), "https://shop.example.test") {
		t.Fatal("result lost return or shop navigation")
	}
	method, _ = a.memberPanelEdit(ctx, "sendMessage", telegramSendMessageRequest{ChatID: int64(999), Text: "admin notification"})
	if method != "sendMessage" {
		t.Fatal("callback redirected another recipient")
	}
	method, _ = a.memberPanelEdit(context.Background(), "sendMessage", telegramSendMessageRequest{ChatID: int64(4321), Text: "typed command"})
	if method != "sendMessage" {
		t.Fatal("typed command edited an unrelated panel")
	}
}
