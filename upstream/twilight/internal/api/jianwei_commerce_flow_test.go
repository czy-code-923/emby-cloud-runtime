package api

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/prejudice-studio/twilight/internal/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestJianweiShopPublishOrderReviewDeliveryEndToEnd(t *testing.T) {
	a := newTestApp(t)
	admin, _, recovery, _ := factorEnroll(t, a)
	a.cfg().AdminUIDs = append(a.cfg().AdminUIDs, admin.UID)
	a.applyConfiguredAdmins()
	admin, _ = a.store().User(admin.UID)
	challenge := factorChallenge(t, a, admin)
	login := factorRequest(a, "POST", "/api/v2/auth/two-factor", map[string]any{"request": challenge, "code": recovery[0], "recovery": true}, nil)
	factorData(t, login)
	cookies := login.Result().Cookies()
	a.cfg().TelegramMode = true
	a.cfg().TelegramBotToken = "fixture-shop-bot"
	a.cfg().CommerceStoreURL = "https://shop.example.test/shop"
	a.cfg().CORSOrigins = append(a.cfg().CORSOrigins, "https://shop.example.test")
	buyer, _, e := a.store().EnsureTelegramProfile(451234, "buyer-flow", "fixture", -1)
	if e != nil {
		t.Fatal(e)
	}
	call := func(method, path string, body any, member bool) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Twilight-Client", "webui")
		if member {
			r.Header.Set("Origin", "https://shop.example.test")
			r.Header.Set("X-Twilight-Intent", "shop-action")
			r.AddCookie(&http.Cookie{Name: shopCookie, Value: strings.Repeat("c", 64)})
		} else {
			r.Header.Set("X-Twilight-Intent", "shop-admin")
			for _, c := range cookies {
				r.AddCookie(c)
			}
		}
		w := httptest.NewRecorder()
		a.ServeHTTP(w, r)
		return w
	}
	settings := store.ShopSettings{Enabled: true, DefaultChannel: "static", Channels: []store.PaymentChannel{{ID: "static", Name: "测试静态码", Kind: "alipay_qr", Enabled: true, Account: "isolated-fixture", Instructions: "仅隔离测试，不收款", QRURL: "https://shop.example.test/api/v2/commerce/assets/fixture.png", Version: 1}}, Products: []store.ShopProduct{{ID: "reg-month", Name: "隔离测试月卡", Kind: 1, Days: 30, PriceCents: 1999, Stock: 2, Enabled: true}}}
	w := call("PUT", "/api/v2/admin/commerce", map[string]any{"settings": settings, "revision": 0, "password": factorTestPassword}, false)
	if w.Code != 200 {
		t.Fatal("publish", w.Code, w.Body)
	}
	if e = a.store().CreateMiniSession(context.Background(), buyer.UID, buyer.TelegramID, "shop-proof", "nonce", "shop-session|fixture-shop-bot", strings.Repeat("c", 64), time.Now().Unix()); e != nil {
		t.Fatal(e)
	}
	w = call("POST", "/api/v2/commerce/orders", map[string]any{"product_id": "reg-month", "revision": 1, "request_key": "fixture-order-request-123", "channel_id": "static"}, true)
	d := factorData(t, w)
	id := asString(d["id"])
	if d["payment_qr"] == nil || id == "" {
		t.Fatal("order missing payment snapshot", d)
	}
	w = call("POST", "/api/v2/commerce/orders/"+id+"/payment", map[string]any{"note": "隔离模拟付款，无真实资金"}, true)
	factorData(t, w)
	order, _ := a.store().ShopOrder(id)
	review := map[string]any{"action": "confirm_payment", "revision": order.Revision, "password": factorTestPassword, "verified_actual_receipt": true, "payment_reference": "isolated-fixture-receipt-123", "received_cents": 1999, "paid_at": time.Now().Unix(), "note": "隔离数据库模拟核验"}
	w = call("POST", "/api/v2/admin/commerce/orders/"+id+"/review", review, false)
	factorData(t, w)
	order, _ = a.store().ShopOrder(id)
	if order.Status != "issued" || order.ReceivedCents != 1999 {
		t.Fatal("review failed", order.Status)
	}
	view := a.shopOrderView(order, false)
	code := asString(view["code"])
	if !strings.HasPrefix(code, "JW-P-") {
		t.Fatal("buyer did not receive code")
	}
	req := httptest.NewRequest("POST", "/redeem", strings.NewReader(fmt.Sprintf(`{"code":%q}`, code)))
	req = req.WithContext(context.WithValue(req.Context(), principalKey, principal{User: buyer}))
	rr := httptest.NewRecorder()
	a.handleUseCode(rr, req, nil)
	if rr.Code != 200 {
		t.Fatal("redeem", rr.Code, rr.Body)
	}
	buyer, _ = a.store().User(buyer.UID)
	if !buyer.PendingEmby {
		t.Fatal("registration entitlement missing")
	}
	// Replaying the identical receipt cannot create another delivery or code.
	w = call("POST", "/api/v2/admin/commerce/orders/"+id+"/review", review, false)
	if w.Code < 400 {
		t.Fatal("stale review was replayed")
	}
}
