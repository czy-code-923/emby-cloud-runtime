package api

import (
	"fmt"
	"strings"

	"github.com/prejudice-studio/twilight/internal/store"
)

func (a *App) memberRegistration() store.EmbyRegistrationSummary {
	cfg := a.cfg()
	return a.store().EmbyRegistrationSummary(cfg.EmbyUserLimit, cfg.EmbyDirectRegisterEnabled)
}

func (a *App) memberRegistrationFor(user store.User) *store.EmbyRegistrationSummary {
	if strings.TrimSpace(user.EmbyID) != "" {
		return nil
	}
	v := a.memberRegistration()
	return &v
}

func memberRegistrationText(v store.EmbyRegistrationSummary) string {
	total, left := "不限", "不限"
	if !v.Unlimited && v.Remaining != nil {
		total, left = fmt.Sprint(v.Limit), fmt.Sprint(*v.Remaining)
	}
	text := fmt.Sprintf("· 🎟 总注册席位 | %s · 剩余 %s\n· 👥 已注册Emby | %d", total, left, v.Registered)
	if v.Reserved > 0 {
		text += fmt.Sprintf(" · 预留 %d", v.Reserved)
	}
	return text
}
