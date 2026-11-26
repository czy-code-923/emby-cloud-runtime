package api

import (
	"context"
	"encoding/hex"
	"github.com/prejudice-studio/twilight/internal/security"
	"regexp"
	"strings"
	"time"
)

var memberRedeemPayload = regexp.MustCompile(`^[A-Za-z0-9_-]{1,57}$`)

// A deep link is only a transport for the existing redemption operation. It
// keeps its membership, expiry, capacity, recipient and single-use checks.
func (a *App) telegramRedeemLink(ctx context.Context, c telegramCommandCtx, code string) {
	if !memberRedeemPayload.MatchString(code) || !a.allowRate(ctx, rateKey("redeem-link:", c.FromID), 5, time.Minute) {
		_ = a.telegramSendMessage(ctx, c.ChatID, "兑换链接格式不正确或操作过于频繁，请稍后重试。")
		return
	}
	u, found := a.store().FindUserByTelegramID(c.FromID)
	if !found {
		if !a.cfg().TelegramAutoProvision {
			_ = a.telegramSendMessage(ctx, c.ChatID, "请先联系管理员建立账户，再使用兑换链接。")
			return
		}
		random, err := security.RandomHex(32)
		if err != nil {
			return
		}
		hash, err := security.HashPassword(hex.EncodeToString([]byte(random)))
		if err != nil {
			return
		}
		u, _, err = a.store().EnsureTelegramProfile(c.FromID, c.Username, hash, a.cfg().UserLimit)
		if err != nil {
			_ = a.telegramSendMessage(ctx, c.ChatID, "暂时无法建立身份，请联系管理员检查注册名额。")
			return
		}
	}
	if !u.Active {
		_ = a.telegramSendMessage(ctx, c.ChatID, "账户已停用，请联系管理员。")
		return
	}
	lookup := code
	if strings.HasPrefix(strings.ToUpper(code), "JW-P-") {
		if digest, err := a.shopDigest("code", strings.ToUpper(code)); err == nil {
			lookup = digest
		}
	}
	if u.EmbyID != "" {
		if rc, ok := a.store().RegCode(lookup); ok && (rc.Type == 1 || rc.Type == 3) {
			_ = a.telegramSendMessage(ctx, c.ChatID, "你已经注册 Emby，账号："+u.EmbyUsername+"。此链接为开通资格，无需重复注册；续期请使用续期码。\n/panel 返回用户面板。")
			return
		}
	}
	// Reuse the command dispatcher rather than skipping normal command gating.
	c.Command = "/redeem"
	c.Args = []string{code}
	a.telegramDispatchRegistry(ctx, c.Command, c, true)
}
