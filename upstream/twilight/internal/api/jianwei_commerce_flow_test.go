package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/prejudice-studio/twilight/internal/store"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func shopFixtureImage(t *testing.T, a *App, path, intent, origin string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var content bytes.Buffer
	writer := multipart.NewWriter(&content)
	part, e := writer.CreateFormFile("file", "fixture.png")
	if e != nil {
		t.Fatal(e)
	}
	if e = png.Encode(part, image.NewRGBA(image.Rect(0, 0, 32, 32))); e != nil {
		t.Fatal(e)
	}
	if e = writer.Close(); e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("POST", path, &content)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	r.Header.Set("X-Twilight-Client", "webui")
	r.Header.Set("X-Twilight-Intent", intent)
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	for _, cookie := range cookies {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	return w
}

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
	qr := factorData(t, shopFixtureImage(t, a, "/api/v2/admin/commerce/image", "shop-admin", "", cookies))
	settings.Channels[0].QRURL = asString(qr["url"])
	w := call("PUT", "/api/v2/admin/commerce", map[string]any{"settings": settings, "revision": 0}, false)
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
	memberCookies := []*http.Cookie{{Name: shopCookie, Value: strings.Repeat("c", 64)}}
	for i := 0; i < 3; i++ {
		factorData(t, shopFixtureImage(t, a, "/api/v2/commerce/orders/"+id+"/proof", "shop-action", "https://shop.example.test", memberCookies))
	}
	if limited := shopFixtureImage(t, a, "/api/v2/commerce/orders/"+id+"/proof", "shop-action", "https://shop.example.test", memberCookies); limited.Code != 409 {
		t.Fatal("proof replacements unbounded", limited.Code)
	}
	if own := call("GET", "/api/v2/commerce/orders/"+id+"/proof", nil, true); own.Code != 200 || !strings.Contains(own.Header().Get("Cache-Control"), "no-store") {
		t.Fatal("owner cannot read private proof", own.Code)
	}
	if review := call("GET", "/api/v2/admin/commerce/orders/"+id+"/proof", nil, false); review.Code != 200 {
		t.Fatal("administrator cannot review proof", review.Code)
	}
	if anonymous := doJSON(a, "GET", "/api/v2/commerce/orders/"+id+"/proof", "", nil); anonymous.Code != 401 {
		t.Fatal("proof exposed anonymously", anonymous.Code)
	}
	wrong := httptest.NewRequest("GET", "/api/v2/commerce/orders/"+id+"/proof", nil)
	wrong = wrong.WithContext(context.WithValue(wrong.Context(), principalKey, principal{User: store.User{UID: buyer.UID + 100, Role: store.RoleAdmin}}))
	denied := httptest.NewRecorder()
	a.handleShopProof(denied, wrong, Params{"order_id": id})
	if denied.Code != 404 {
		t.Fatal("public shop audience crossed owner boundary", denied.Code)
	}
	// Removing a product does not change an existing order's price or delivery.
	settings.Enabled = false
	settings.Products = []store.ShopProduct{}
	factorData(t, call("PUT", "/api/v2/admin/commerce", map[string]any{"settings": settings, "revision": 1}, false))
	w = call("POST", "/api/v2/commerce/orders/"+id+"/payment", map[string]any{"note": "隔离模拟付款，无真实资金"}, true)
	factorData(t, w)
	order, _ := a.store().ShopOrder(id)
	review := map[string]any{"action": "approve", "revision": order.Revision, "note": "隔离数据库模拟核验"}
	w = call("POST", "/api/v2/admin/commerce/orders/"+id+"/review", review, false)
	factorData(t, w)
	order, _ = a.store().ShopOrder(id)
	if order.Status != "issued" || order.ReceivedCents != 1999 || order.ReceiptKind != "manual_order_confirmation" || order.PaidAt <= 0 || order.PaymentProof == "" {
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
	if w.Code == 200 {
		after, _ := a.store().ShopOrder(id)
		if after.CodeKey != order.CodeKey || after.ReceiptHash != order.ReceiptHash || len(a.store().ShopOrders(buyer.UID, false)) != 1 {
			t.Fatal("idempotent review created another delivery")
		}
	} else if w.Code != 409 {
		t.Fatal("unexpected review replay failure", w.Code, w.Body)
	}
}
