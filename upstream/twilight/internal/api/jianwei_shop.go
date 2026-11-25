package api

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prejudice-studio/twilight/internal/security"
	"github.com/prejudice-studio/twilight/internal/store"
)

const shopCookie = "jw_shop"

func shopInteger(w http.ResponseWriter, body map[string]any, key string) (int64, bool) {
	v, ok := body[key].(float64)
	if !ok || v != math.Trunc(v) || v < 0 || v > 9007199254740991 {
		fail(w, 400, "订单金额、时间与版本必须为非负整数")
		return 0, false
	}
	return int64(v), true
}

func (a *App) shopDigest(purpose, value string) (string, error) {
	key, err := security.TwoFactorKey(a.cfg().TwoFactorKey)
	if err != nil {
		return "", err
	}
	return security.ShopDigest(key, purpose, value)
}
func (a *App) shopCipher(orderID, value string, open bool) (string, error) {
	key, err := security.TwoFactorKey(a.cfg().TwoFactorKey)
	if err != nil {
		return "", err
	}
	return security.ShopCipher(key, orderID, value, open)
}

func (a *App) shopIntent(w http.ResponseWriter, r *http.Request) bool {
	if !requireWebUIIntent(w, r, "shop-action") {
		return false
	}
	if a.cfg().CommerceStoreURL == "" || r.Header.Get("Origin") != a.cfg().CommerceStoreURL {
		fail(w, 403, "请从独立商店页面操作")
		return false
	}
	return true
}
func (a *App) shopMember(next HandlerFunc) HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, p Params) {
		w.Header().Set("Cache-Control", "private, no-store")
		if r.Method != "GET" && !a.shopIntent(w, r) {
			return
		}
		cookie, err := r.Cookie(shopCookie)
		if err != nil {
			fail(w, 401, "请使用机器人签发的一次性商店登录码")
			return
		}
		u, err := a.store().MiniSessionUser(r.Context(), cookie.Value, "shop-session|"+a.cfg().TelegramBotToken, time.Now().Unix())
		if err != nil {
			fail(w, 401, "商店登录已过期，请重新获取登录码")
			return
		}
		if !a.allowRate(r.Context(), rateKey("shop-member:", u.UID), 60, time.Minute) {
			fail(w, 429, "操作过于频繁")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), principalKey, principal{User: u})), p)
	}
}
func (a *App) handleShopCatalog(w http.ResponseWriter, r *http.Request, _ Params) {
	w.Header().Set("Cache-Control", "no-store")
	cfg := a.store().ShopConfiguration()
	products := []store.ShopProduct{}
	if cfg.Enabled {
		for _, p := range cfg.Products {
			if p.Enabled {
				products = append(products, p)
			}
		}
	}
	ok(w, "OK", map[string]any{"enabled": cfg.Enabled, "revision": cfg.Revision, "products": products, "brand": "见微 Emby", "currency": "CNY"})
}
func (a *App) handleShopLogin(w http.ResponseWriter, r *http.Request, _ Params) {
	if !a.shopIntent(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if !a.telegramAvailable() || !a.allowRate(r.Context(), rateKey("shop-login:", a.clientIP(r)), 5, 5*time.Minute) {
		fail(w, 429, "请稍后再试或联系管理员")
		return
	}
	code := strings.ToUpper(strings.TrimSpace(stringValue(decodeMap(r), "code")))
	if len(code) != 20 {
		fail(w, 400, "请输入20位商店登录码，不是 Telegram 验证码")
		return
	}
	u, err := a.store().ConsumeShopLoginCode(r.Context(), code, a.cfg().TelegramBotToken, time.Now().Unix())
	if err != nil {
		fail(w, 401, "登录码已过期或已使用")
		return
	}
	token, err := security.RandomHex(32)
	if err != nil {
		fail(w, 503, "登录暂不可用")
		return
	}
	err = a.store().CreateMiniSession(r.Context(), u.UID, u.TelegramID, "shop-session|"+token, token, "shop-session|"+a.cfg().TelegramBotToken, token, time.Now().Unix())
	if err != nil {
		fail(w, 503, "登录暂不可用，请重新取码")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: shopCookie, Value: token, Path: "/api/v2/commerce", HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode, MaxAge: 900})
	ok(w, "登录成功", map[string]any{"username": u.Username})
}
func (a *App) handleShopLogout(w http.ResponseWriter, r *http.Request, _ Params) {
	if !a.shopIntent(w, r) {
		return
	}
	if cookie, err := r.Cookie(shopCookie); err == nil {
		if err = a.store().RevokeShopSession(r.Context(), cookie.Value, a.cfg().TelegramBotToken); err != nil {
			fail(w, 503, "退出暂不可用，请重试")
			return
		}
	}
	http.SetCookie(w, &http.Cookie{Name: shopCookie, Path: "/api/v2/commerce", HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	ok(w, "已退出商店", nil)
}
func (a *App) shopOrderView(o store.ShopOrder, admin bool) map[string]any {
	out := map[string]any{"id": o.ID, "revision": o.Revision, "product": o.Product, "channel": o.Channel, "receiving_account": o.ReceivingAccount, "instructions": o.Instructions, "status": o.Status, "created_at": o.CreatedAt, "updated_at": o.UpdatedAt, "pay_before": o.PayBefore, "note": o.Note, "admin_note": o.AdminNote}
	if o.CodeKey != "" {
		if rc, ok := a.store().RegCode(o.CodeKey); ok {
			out["redeemed"] = rc.UseCount > 0 || o.RedeemedAt > 0
			out["code_expires_at"] = rc.CreatedAt + rc.ValidityTime*3600
			if !admin && o.Status == "issued" && rc.Active && rc.UseCount == 0 && o.RedeemedAt == 0 {
				if code, err := a.shopCipher(o.ID, o.CodeCipher, true); err == nil {
					out["code"] = code
				}
			}
		}
	}
	if admin {
		out["uid"] = o.UID
		out["received_cents"] = o.ReceivedCents
		out["paid_at"] = o.PaidAt
		out["checked_by"] = o.CheckedBy
		out["refund_at"] = o.RefundAt
	}
	return out
}
func (a *App) handleShopOrders(w http.ResponseWriter, r *http.Request, _ Params) {
	u := current(r).User
	orders := a.store().ShopOrders(u.UID, false)
	items := []map[string]any{}
	page, start, end := shopPage(r, len(orders))
	for _, o := range orders[start:end] {
		items = append(items, a.shopOrderView(o, false))
	}
	ok(w, "OK", map[string]any{"username": u.Username, "items": items, "page": page, "total": len(orders), "has_next": end < len(orders)})
}
func shopPage(r *http.Request, total int) (page, start, end int) {
	page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	if page > 100000 {
		page = 100000
	}
	start = (page - 1) * 20
	if start > total {
		start = total
	}
	end = start + 20
	if end > total {
		end = total
	}
	return
}
func (a *App) handleShopOrderCreate(w http.ResponseWriter, r *http.Request, _ Params) {
	u := current(r).User
	body := decodeMap(r)
	revision, valid := shopInteger(w, body, "revision")
	if !valid {
		return
	}
	if !a.allowRate(r.Context(), rateKey("shop-create:", u.UID), 5, 10*time.Minute) {
		fail(w, 429, "请先处理现有订单")
		return
	}
	id, err := security.RandomHex(12)
	if err != nil {
		fail(w, 503, "下单暂不可用")
		return
	}
	order, err := a.store().CreateShopOrder(u.UID, id, stringValue(body, "product_id"), stringValue(body, "request_key"), revision, a.cfg().EmbyUserLimit, a.cfg().EmbyDirectRegisterEnabled)
	if statusFromError(w, err) {
		return
	}
	a.audit(r, "shop_order_created", "commerce", u.UID, map[string]any{"order_id": id, "amount_cents": order.Product.PriceCents})
	ok(w, "订单已创建，请按订单说明付款", a.shopOrderView(order, false))
}
func (a *App) handleShopPaymentSubmit(w http.ResponseWriter, r *http.Request, p Params) {
	body := decodeMap(r)
	order, err := a.store().SubmitShopPayment(current(r).User.UID, p["order_id"], stringValue(body, "note"), boolValue(body, "cancel", false))
	if statusFromError(w, err) {
		return
	}
	a.audit(r, "shop_payment_reported", "commerce", current(r).User.UID, map[string]any{"order_id": order.ID, "status": order.Status})
	ok(w, "已记录，请等待管理员核实到账", a.shopOrderView(order, false))
}
func (a *App) handleAdminShop(w http.ResponseWriter, r *http.Request, _ Params) {
	w.Header().Set("Cache-Control", "private, no-store")
	orders := a.store().ShopOrders(0, true)
	items := []map[string]any{}
	page, start, end := shopPage(r, len(orders))
	for _, o := range orders[start:end] {
		items = append(items, a.shopOrderView(o, true))
	}
	ok(w, "OK", map[string]any{"settings": a.store().ShopConfiguration(), "orders": items, "total": len(orders), "page": page, "has_next": end < len(orders)})
}
func (a *App) adminShopIntent(w http.ResponseWriter, r *http.Request, body map[string]any) bool {
	if requireAdmin(w, r) || !requireWebUIIntent(w, r, "shop-admin") {
		return false
	}
	factor, err := a.store().TwoFactorAccount(r.Context(), current(r).User.UID)
	if err != nil || factor.EnabledAt <= 0 {
		fail(w, 403, "请先为管理员启用双重验证，再管理收款与发卡")
		return false
	}
	return a.twoFactorPassword(w, r, body)
}
func (a *App) handleAdminShopConfig(w http.ResponseWriter, r *http.Request, _ Params) {
	body := decodeMap(r)
	if !a.adminShopIntent(w, r, body) {
		return
	}
	raw, _ := json.Marshal(body["settings"])
	var settings store.ShopSettings
	if json.Unmarshal(raw, &settings) != nil {
		fail(w, 400, "商品设置格式不正确")
		return
	}
	revision, valid := shopInteger(w, body, "revision")
	if !valid {
		return
	}
	if err := a.store().SaveShopConfiguration(settings, revision); statusFromError(w, err) {
		return
	}
	a.audit(r, "shop_settings_updated", "commerce", current(r).User.UID, nil)
	ok(w, "商店设置已保存", nil)
}
func (a *App) handleAdminShopReview(w http.ResponseWriter, r *http.Request, p Params) {
	body := decodeMap(r)
	if !a.adminShopIntent(w, r, body) {
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	id := p["order_id"]
	o, found := a.store().ShopOrder(id)
	if !found {
		fail(w, 404, "订单不存在")
		return
	}
	action := stringValue(body, "action")
	revision, valid := shopInteger(w, body, "revision")
	if !valid {
		return
	}
	var result store.ShopOrder
	var err error
	if action == "confirm_payment" {
		received, valid := shopInteger(w, body, "received_cents")
		if !valid {
			return
		}
		paidAt, valid := shopInteger(w, body, "paid_at")
		if !valid {
			return
		}
		reference := strings.TrimSpace(stringValue(body, "payment_reference"))
		if len(reference) < 4 || len(reference) > 200 || !boolValue(body, "verified_actual_receipt", false) {
			fail(w, 400, "请先核对实际到账并填写收款方账单的唯一交易编号")
			return
		}
		receipt, hashErr := a.shopDigest("receipt", o.Channel+"\x00"+o.ReceivingAccount+"\x00"+strings.ToLower(reference))
		if hashErr != nil {
			fail(w, 503, "商店交付密钥不可用")
			return
		}
		random, randomErr := security.RandomHex(16)
		if randomErr != nil {
			fail(w, 503, "发码暂不可用")
			return
		}
		code := "JW-P-" + strings.ToUpper(random)
		codeKey, keyErr := a.shopDigest("code", code)
		if keyErr != nil {
			fail(w, 503, "发码暂不可用")
			return
		}
		sealed, sealErr := a.shopCipher(id, code, false)
		if sealErr != nil {
			fail(w, 503, "发码暂不可用")
			return
		}
		result, err = a.store().ConfirmShopPayment(id, revision, current(r).User.UID, received, paidAt, receipt, codeKey, sealed, stringValue(body, "note"), a.cfg().EmbyUserLimit, a.cfg().EmbyDirectRegisterEnabled)
	} else {
		result, err = a.store().ReviewShopOrder(id, action, stringValue(body, "note"), stringValue(body, "refund_reference"), revision, current(r).User.UID)
	}
	if statusFromError(w, err) {
		return
	}
	a.audit(r, "shop_order_"+action, "commerce", result.UID, map[string]any{"order_id": result.ID, "status": result.Status, "amount_cents": result.Product.PriceCents})
	ok(w, "核查结果已保存", a.shopOrderView(result, true))
}

func (a *App) telegramShopCode(ctx context.Context, c telegramCommandCtx) {
	u, found := a.store().FindUserByTelegramID(c.FromID)
	if !found || !u.Active {
		_ = a.telegramSendMessage(ctx, c.ChatID, "请先发送 /start 建立账户")
		return
	}
	if !a.allowRate(ctx, rateKey("shop-code:", u.UID), 3, 5*time.Minute) {
		_ = a.telegramSendMessage(ctx, c.ChatID, "登录码请求过于频繁，请5分钟后再试")
		return
	}
	code, err := security.RandomHex(10)
	if err != nil {
		return
	}
	code = strings.ToUpper(code)
	if err = a.store().CreateShopLoginCode(ctx, u, code, a.cfg().TelegramBotToken, time.Now().Unix()); err != nil {
		_ = a.telegramSendMessage(ctx, c.ChatID, "暂时无法签发登录码")
		return
	}
	_ = a.telegramSendMessage(ctx, c.ChatID, "见微独立商店登录码\n"+code+"\n5分钟内有效，仅能使用一次。仅输入到你确认的见微商店页面，不要转交他人。此码不是 Telegram 登录验证码。")
}

func init() {
	telegramCommandRegistry["/shopcode"] = telegramCommandSpec{name: "shopcode", label: "/shopcode", description: "获取独立商店一次性登录码", usage: "/shopcode", category: "user", order: 48, private: true, handler: func(a *App, ctx context.Context, c telegramCommandCtx) { a.telegramShopCode(ctx, c) }}
}
