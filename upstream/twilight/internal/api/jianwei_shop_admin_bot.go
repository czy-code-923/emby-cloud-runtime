package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/prejudice-studio/twilight/internal/store"
)

func init() {
	telegramCommandRegistry["/orders"] = telegramCommandSpec{name: "orders", label: "/orders", description: "管理员待核款订单", usage: "/orders", category: "admin", order: 74, private: true, admin: true, handler: func(a *App, ctx context.Context, c telegramCommandCtx) {
		u, exists := a.store().FindUserByTelegramID(c.FromID)
		if !exists || !u.Active || u.Role != store.RoleAdmin {
			return
		}
		rows := [][]map[string]any{}
		for _, o := range a.store().ShopOrders(0, true) {
			if o.Status == "review" || o.Status == "hold" || o.Status == "paid_hold" {
				rows = append(rows, []map[string]any{{"text": fmt.Sprintf("%s · ¥%.2f", o.Product.Name, float64(o.Product.PriceCents)/100), "callback_data": shopAdminCallback("view", o)}})
				if len(rows) >= 10 {
					break
				}
			}
		}
		a.memberSendButtons(ctx, c.ChatID, "见微 · 管理员核款\n\n买家提交付款不代表已经到账。请核实实际收款后通过；通过后按订单快照自动发专属码。最近待处理订单：", rows)
	}}
}

func shopAdminCallback(action string, o store.ShopOrder) string {
	return "jws:" + action + ":" + o.ID + ":" + strconv.FormatInt(o.Revision, 10)
}
func shopAdminRows(o store.ShopOrder, confirm bool) [][]map[string]any {
	rows := [][]map[string]any{}
	if o.Status == "review" || o.Status == "hold" || o.Status == "paid_hold" {
		if confirm {
			rows = append(rows, []map[string]any{{"text": "确认已到账，通过并发码", "callback_data": shopAdminCallback("accept", o)}})
		} else {
			rows = append(rows, []map[string]any{{"text": "核实到账后通过", "callback_data": shopAdminCallback("approve", o)}})
		}
		if o.PaymentSecretCipher != "" {
			rows = append(rows, []map[string]any{{"text": "查看付款口令（私聊）", "callback_data": shopAdminCallback("secret", o)}})
		}
		if o.PaymentProof != "" {
			rows = append(rows, []map[string]any{{"text": "查看付款截图", "callback_data": shopAdminCallback("proof", o)}})
		}
	}
	rows = append(rows, []map[string]any{{"text": "刷新订单", "callback_data": shopAdminCallback("view", o)}})
	return rows
}
func shopAdminText(o store.ShopOrder) string {
	hint := "买家已提交付款信息；请检查本方实际收款后通过。"
	if o.Status == "issued" || o.Status == "redeemed" {
		hint = "此订单已处理，无需再次通过。"
	}
	return fmt.Sprintf("见微 · 管理员订单核验\n\n订单：%s\n商品：%s · %d天\n金额：¥%.2f\n渠道：%s\n买家UID：%d\n状态：%s\n\n%s", o.ID, o.Product.Name, o.Product.Days, float64(o.Product.PriceCents)/100, o.Channel, o.UID, o.Status, hint)
}
func (a *App) runShopAdminNotices(ctx context.Context) {
	if !a.telegramAvailable() {
		return
	}
	for i := 0; i < 5; i++ {
		o, tg, found, err := a.store().ClaimShopAdminNotice()
		if err != nil || !found {
			return
		}
		_, err = telegramPostResult[telegramDiscardResult](a, ctx, "sendMessage", telegramSendMessageRequest{ChatID: tg, Text: shopAdminText(o), ReplyMarkup: map[string]any{"inline_keyboard": shopAdminRows(o, false)}}, 5*time.Second)
		if err != nil {
			return
		}
		_ = a.store().MarkShopAdminNotified(o.ID, o.Revision, tg)
	}
}
func (a *App) telegramHandleShopAdminCallback(ctx context.Context, c *telegramCallbackQuery) bool {
	if !strings.HasPrefix(c.Data, "jws:") {
		return false
	}
	u, exists := a.store().FindUserByTelegramID(c.From.ID)
	if c.Message == nil || c.From.IsBot || c.Message.Chat.ID != c.From.ID || !exists || !u.Active || u.Role != store.RoleAdmin {
		_ = a.telegramAnswerCallbackQuery(ctx, c.ID, "仅限已绑定且有效的管理员私聊核验。", true)
		return true
	}
	if !a.allowRate(ctx, rateKey("shop-admin-bot:", u.UID), 15, time.Minute) {
		_ = a.telegramAnswerCallbackQuery(ctx, c.ID, "操作频繁，请稍后再试。", true)
		return true
	}
	parts := strings.Split(c.Data, ":")
	if len(parts) != 4 || len(parts[2]) != 24 || len(c.Data) > 64 {
		return true
	}
	o, found := a.store().ShopOrder(parts[2])
	rev, err := strconv.ParseInt(parts[3], 10, 64)
	if !found || err != nil {
		_ = a.telegramAnswerCallbackQuery(ctx, c.ID, "订单不存在。", true)
		return true
	}
	_ = a.telegramAnswerCallbackQuery(ctx, c.ID, "", false)
	ctx = context.WithValue(ctx, memberPanelContextKey{}, memberPanelTarget{ChatID: c.From.ID, MessageID: c.Message.MessageID, Photo: len(c.Message.Photo) > 0})
	text := shopAdminText(o)
	confirm := false
	if rev != o.Revision && parts[1] != "view" {
		text = "订单已变化，本次未操作。请重新核对最新状态。\n\n" + text
	} else {
		switch parts[1] {
		case "approve":
			confirm = true
			text += "\n\n确认：我已核实本方实际收到上述金额。下一步将发放专属单次码。"
		case "accept":
			body, _ := json.Marshal(map[string]any{"action": "approve", "revision": o.Revision})
			r, _ := http.NewRequestWithContext(context.WithValue(ctx, principalKey, principal{User: u}), http.MethodPost, "http://bot.invalid/review", bytes.NewReader(body))
			r.RemoteAddr = "127.0.0.1:0"
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-Twilight-Client", "webui")
			r.Header.Set("X-Twilight-Intent", "shop-admin")
			w := &memberReply{header: make(http.Header)}
			a.handleAdminShopReview(w, r, Params{"order_id": o.ID})
			var reply struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
			}
			_ = json.Unmarshal(w.body.Bytes(), &reply)
			text = reply.Message
			if fresh, ok := a.store().ShopOrder(o.ID); ok {
				o = fresh
			}
			text += "\n\n" + shopAdminText(o)
		case "secret":
			secret, e := a.shopCipher("payment-secret:"+o.ID, o.PaymentSecretCipher, true)
			if e != nil || o.PaymentSecretCipher == "" {
				text += "\n没有可读取的付款口令。"
			} else {
				text += "\n\n付款口令（仅供本方领取并核实）：\n" + secret + "\n请勿转发。"
				a.auditShopBotSecret(u, o)
			}
		case "proof":
			if e := a.telegramShopProof(ctx, c.From.ID, o); e != nil {
				text += "\n付款截图暂不可读取，请在私有管理后台查看。"
			}
		case "view":
		default:
			return true
		}
	}
	if o.Status == "issued" {
		deliveryContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		a.runShopDeliveries(deliveryContext)
		cancel()
	}
	a.memberSendButtons(ctx, c.From.ID, text, shopAdminRows(o, confirm))
	return true
}
func (a *App) auditShopBotSecret(u store.User, o store.ShopOrder) {
	r, _ := http.NewRequestWithContext(context.WithValue(context.Background(), principalKey, principal{User: u}), http.MethodPost, "http://bot.invalid/payment", nil)
	a.audit(r, "payment_secret_revealed", "finance", o.UID, map[string]any{"order_id": o.ID, "via": "telegram_private"})
}
func (a *App) telegramShopProof(ctx context.Context, chatID int64, o store.ShopOrder) error {
	if !uploadFilenamePattern.MatchString(o.PaymentProof) {
		return fmt.Errorf("proof unavailable")
	}
	path, e := ResolveLeafFile(filepath.Join(a.cfg().UploadDir, "commerce-proof"), o.PaymentProof, "")
	if e != nil {
		return fmt.Errorf("proof unavailable")
	}
	content, e := os.ReadFile(path)
	if e != nil || len(content) > 2<<20 {
		return fmt.Errorf("proof unavailable")
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("chat_id", fmt.Sprint(chatID))
	_ = writer.WriteField("caption", "订单 "+o.ID+" · 买家提供的截图，实际到账须另行核实。")
	part, e := writer.CreateFormFile("photo", o.PaymentProof)
	if e != nil {
		return e
	}
	_, _ = part.Write(content)
	_ = writer.Close()
	endpoint, e := a.telegramEndpoint("sendPhoto")
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	r, e := http.NewRequestWithContext(ctx, "POST", endpoint, &body)
	if e != nil {
		return e
	}
	r.Header.Set("Content-Type", writer.FormDataContentType())
	response, e := sharedHTTPClient.Do(r)
	if e != nil {
		return fmt.Errorf("proof delivery failed")
	}
	defer response.Body.Close()
	result, e := decodeTelegramEnvelope[telegramDiscardResult](response.Body)
	if e != nil || !result.OK {
		return fmt.Errorf("proof delivery failed")
	}
	return nil
}
