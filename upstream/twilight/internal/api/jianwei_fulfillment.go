package api

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Persistent delivery intentions retry the same code. A transport retry never
// creates another order, another code or a second charge.
func (a *App) runShopDeliveries(ctx context.Context) {
	if !a.telegramAvailable() {
		return
	}
	for i := 0; i < 5; i++ {
		o, telegramID, found, e := a.store().ClaimShopDelivery()
		if e != nil || !found {
			return
		}
		code, e := a.shopCipher(o.ID, o.CodeCipher, true)
		if e != nil {
			return
		}
		rows := [][]map[string]any{{{"text": "复制专属卡码", "copy_text": map[string]string{"text": code}}}}
		identity, identityErr := a.telegramGetMe(ctx)
		if bot := strings.TrimPrefix(identity.Username, "@"); identityErr == nil && bot != "" {
			rows = append(rows, []map[string]any{{"text": "验证并使用卡码", "url": "https://t.me/" + bot + "?start=redeem_" + code}})
		}
		text := fmt.Sprintf("见微 Emby · 卡码已交付\n\n%s · %d天\n订单：%s\n专属单次码：%s\n\n仅限本人使用，核销后生效。", o.Product.Name, o.Product.Days, o.ID, code)
		_, e = telegramPostResult[telegramDiscardResult](a, ctx, "sendMessage", telegramSendMessageRequest{ChatID: telegramID, Text: text, ReplyMarkup: map[string]any{"inline_keyboard": rows}}, 15*time.Second)
		if e != nil {
			return
		}
		_ = a.store().MarkShopDelivered(o.ID, o.UID)
	}
}
