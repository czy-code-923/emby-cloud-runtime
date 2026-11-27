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
		rows := [][]map[string]any{{memberButton("打开我的面板", "/panel")}}
		text := fmt.Sprintf("见微 Emby · 订单状态更新\n\n%s\n订单：%s\n", o.Product.Name, o.ID)
		if o.Status == "issued" {
			code, decryptErr := a.shopCipher(o.ID, o.CodeCipher, true)
			if decryptErr != nil {
				return
			}
			rows = append([][]map[string]any{{{"text": "复制专属卡码", "copy_text": map[string]string{"text": code}}}}, rows...)
			identity, identityErr := a.telegramGetMe(ctx)
			if bot := strings.TrimPrefix(identity.Username, "@"); identityErr == nil && bot != "" {
				rows = append(rows, []map[string]any{{"text": "验证并使用卡码", "url": "https://t.me/" + bot + "?start=redeem_" + code}})
			}
			text += fmt.Sprintf("卡码已交付 · %d天\n专属单次码：%s\n\n仅限本人使用，核销后生效。", o.Product.Days, code)
		} else {
			text += map[string]string{"paid_hold": "已核实收款，交付暂挂；请联系管理员处理名额或资格。", "refund_pending": "已进入退款处理，相关卡码已冻结；原订单可证明的未消费权益已撤回。实际资金退款请等待管理员核实。", "refunded": "退款完成已登记；现金以本方实际到账为准，积分订单已退回原钱包。"}[o.Status]
		}
		text = memberCopyText(text, rows)
		_, e = telegramPostResult[telegramDiscardResult](a, ctx, "sendMessage", telegramSendMessageRequest{ChatID: telegramID, Text: text, Entities: memberCopyEntities(text, rows), ReplyMarkup: map[string]any{"inline_keyboard": rows}}, 15*time.Second)
		if e != nil {
			return
		}
		_ = a.store().MarkShopDelivered(o.ID, o.UID, o.Status)
	}
}
