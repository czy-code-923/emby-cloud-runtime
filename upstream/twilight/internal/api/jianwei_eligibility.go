package api

import (
	"context"
	"net/http"
	"time"

	"github.com/prejudice-studio/twilight/internal/store"
)

// Auto-created Telegram identity is not proof of group/channel eligibility.
// Run this shared guard before consuming registration codes or creating Emby
// users, including the in-process Bot and Mini App transports.
func (a *App) requireMemberTelegramEligibility(w http.ResponseWriter, r *http.Request, user store.User) bool {
	if a.userIsProtected(user) {
		return false
	}
	cfg := a.cfg()
	chats := []string{}
	if cfg.TelegramForceBindGroup || cfg.TelegramRequireMembership {
		chats = append(chats, cfg.TelegramGroupIDs...)
	}
	if cfg.TelegramForceBindChannel {
		chats = append(chats, cfg.TelegramChannelIDs...)
	}
	chats = telegramChatIDs(chats)
	if user.TelegramID <= 0 && (cfg.ForceBindTelegram || len(chats) > 0) {
		failWithCode(w, http.StatusForbidden, ErrTGBindRequired, "请先通过 Telegram 机器人绑定本人身份，再兑换开通资格")
		return true
	}
	if len(chats) == 0 {
		return false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	missing, err := a.telegramMembershipMissingForChats(ctx, user.TelegramID, chats, true)
	if err != nil {
		failWithCode(w, http.StatusServiceUnavailable, ErrTGBindGroupCheckFailed, "Telegram 群组/频道资格暂时无法核验，请稍后重试或联系管理员；卡码未消耗，未创建 Emby 账户")
		return true
	}
	if len(missing) > 0 {
		failWithCode(w, http.StatusForbidden, ErrTGBindGroupMembershipRequired, "请先加入要求的 Telegram 群组/频道，再重新兑换或开通；卡码未消耗，未创建 Emby 账户")
		return true
	}
	return false
}
