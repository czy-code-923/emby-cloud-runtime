package api

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prejudice-studio/twilight/internal/security"
	"github.com/prejudice-studio/twilight/internal/store"
)

func (a *App) handleFinance(w http.ResponseWriter, r *http.Request, _ Params) {
	w.Header().Set("Cache-Control", "private, no-store")
	revision, wallets, ledger, incidents, reconciliations := a.store().FinanceOverview()
	if r.URL.Query().Get("export") == "ledger" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=jianwei-wallet-ledger.csv")
		writer := csv.NewWriter(w)
		_ = writer.Write([]string{"ID", "UID", "操作", "变动", "余额", "冻结", "时间", "理由"})
		for _, v := range ledger {
			_ = writer.Write([]string{v.ID, fmt.Sprint(v.UID), v.Kind, fmt.Sprint(v.Delta), fmt.Sprint(v.Balance), fmt.Sprint(v.Frozen), time.Unix(v.At, 0).UTC().Format(time.RFC3339), csvSafe(v.Reason)})
		}
		writer.Flush()
		return
	}
	preview, _ := a.store().ReconciliationPreview(firstNonEmpty(r.URL.Query().Get("date"), time.Now().In(time.FixedZone("Asia/Shanghai", 28800)).Format("2006-01-02")), r.URL.Query().Get("channel"))
	eligible := []map[string]any{}
	for _, u := range a.store().ListUsers() {
		if len(eligible) >= 500 {
			break
		}
		if u.Active && u.Role == store.RoleNormal && u.ExpiredAt >= 0 && u.EmbyID != "" && (!u.EmbyDisabled || u.EmbyAutoDisabled) {
			eligible = append(eligible, map[string]any{"uid": u.UID, "username": u.Username})
		}
	}
	ok(w, "OK", map[string]any{"eligible_incident_users": eligible, "revision": revision, "wallets": wallets, "ledger": ledger, "incidents": incidents, "reconciliations": reconciliations, "reconciliation_preview": preview})
}
func (a *App) handleFinanceAction(w http.ResponseWriter, r *http.Request, _ Params) {
	body := decodeMap(r)
	if !a.adminShopIntent(w, r, body) {
		return
	}
	revision, valid := shopInteger(w, body, "revision")
	if !valid {
		return
	}
	actor := current(r).User.UID
	action := stringValue(body, "action")
	var err error
	uid := int64(0)
	switch action {
	case "wallet":
		uid, valid = shopInteger(w, body, "uid")
		if !valid {
			return
		}
		amount, good := shopInteger(w, body, "amount")
		if !good {
			return
		}
		err = a.store().AdjustWallet(uid, actor, revision, stringValue(body, "request_id"), stringValue(body, "kind"), stringValue(body, "reason"), int(amount))
	case "incident_create":
		raw, _ := json.Marshal(body["incident"])
		var v store.ServiceIncident
		if json.Unmarshal(raw, &v) != nil {
			fail(w, 400, "事故参数无效")
			return
		}
		v.Actor = actor
		err = a.store().CreateIncident(v, revision)
	case "incident_compensate", "incident_close":
		op := "compensate"
		if action == "incident_close" {
			op = "close"
		}
		amounts := []int{}
		if _, set := body["days"]; set {
			days, good := shopInteger(w, body, "days")
			if !good {
				return
			}
			points, good := shopInteger(w, body, "points")
			if !good {
				return
			}
			amounts = []int{int(days), int(points)}
		}
		err = a.store().ResolveIncident(stringValue(body, "id"), op, revision, actor, amounts...)
	case "reconcile":
		raw, _ := json.Marshal(body["reconciliation"])
		var v store.Reconciliation
		if json.Unmarshal(raw, &v) != nil {
			fail(w, 400, "对账参数无效")
			return
		}
		v.Actor = actor
		err = a.store().ConfirmReconciliation(v, revision)
	default:
		fail(w, 400, "请选择有效操作")
		return
	}
	if statusFromError(w, err) {
		return
	}
	a.audit(r, "finance_"+action, "finance", uid, map[string]any{"request_id": stringValue(body, "request_id"), "reason": stringValue(body, "reason")})
	ok(w, "操作已记录；账本、冻结与权益状态已原子更新", nil)
}
func (a *App) handlePointsCatalog(w http.ResponseWriter, r *http.Request, _ Params) {
	u := current(r).User
	p := a.store().RetentionPolicy()
	items := []map[string]any{}
	for _, sku := range []string{"month", "quarter", "year"} {
		points, days := p.MonthPoints, 30
		if sku == "quarter" {
			points = p.QuarterPoints
			days = 90
		}
		if sku == "year" {
			points = p.YearPoints
			days = 365
		}
		items = append(items, map[string]any{"id": sku, "name": map[string]string{"month": "月卡", "quarter": "季卡", "year": "年卡"}[sku], "points": points, "days": days})
	}
	ok(w, "OK", map[string]any{"enabled": p.Enabled, "can_purchase": u.EmbyID != "" && u.Active && u.ExpiredAt >= 0 && u.Role != store.RoleAdmin, "wallet": a.store().Wallet(u.UID), "items": items})
}
func (a *App) handlePointsPurchase(w http.ResponseWriter, r *http.Request, _ Params) {
	b := decodeMap(r)
	if !boolValue(b, "confirm", false) {
		fail(w, 400, "请确认积分价格及单次定向发码")
		return
	}
	uid := current(r).User.UID
	if !a.allowRate(r.Context(), rateKey("points-purchase:", uid), 6, time.Minute) {
		fail(w, 429, "请稍后重试")
		return
	}
	id, e := security.RandomHex(12)
	if e != nil {
		fail(w, 503, "暂不可用")
		return
	}
	random, e := security.RandomHex(16)
	if e != nil {
		fail(w, 503, "暂不可用")
		return
	}
	code := "JW-P-" + strings.ToUpper(random)
	key, e := a.shopDigest("code", code)
	if e != nil {
		fail(w, 503, "交付密钥不可用")
		return
	}
	cipher, e := a.shopCipher(id, code, false)
	if e != nil {
		fail(w, 503, "交付密钥不可用")
		return
	}
	order, e := a.store().CreatePointsOrder(uid, id, stringValue(b, "sku"), stringValue(b, "request_id"), key, cipher, time.Now().Unix(), int(intValue(b, "expected_cost", -1)))
	if statusFromError(w, e) {
		return
	}
	a.audit(r, "points_card_issued", "commerce", uid, map[string]any{"order_id": order.ID, "points": order.PointsCost})
	ok(w, "积分已扣除，专属单次续期码已生成；兑换后生效", a.shopOrderView(order, false))
}
func (a *App) handleMemberShopCode(w http.ResponseWriter, r *http.Request, _ Params) {
	u := current(r).User
	if !a.allowRate(r.Context(), rateKey("shop-code:", u.UID), 3, 5*time.Minute) {
		fail(w, 429, "请5分钟后重试")
		return
	}
	code, e := security.RandomHex(10)
	code = strings.ToUpper(code)
	if e == nil {
		e = a.store().CreateShopLoginCode(r.Context(), u, code, a.cfg().TelegramBotToken, time.Now().Unix())
	}
	if statusFromError(w, e) {
		return
	}
	ok(w, "5分钟内有效，仅限见微独立商店", map[string]any{"code": code, "expires_in": 300})
}
func financeUID(raw string) int64 { v, _ := strconv.ParseInt(raw, 10, 64); return v }
