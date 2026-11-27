package api

import (
	"context"
	"fmt"
	"html"
	"regexp"
	"strings"
	"unicode/utf16"
)

var memberHTMLTag = regexp.MustCompile(`<[^>]*>`)

// Telegram owns pixel sizing. Keep a bounded caption and balance fewer
// secondary rows with more information space instead of sending new panels.
func memberPanelCaption(text string, rows int, htmlText bool) string {
	if htmlText {
		text = html.UnescapeString(memberHTMLTag.ReplaceAllString(text, ""))
	}
	text = strings.TrimSpace(text)
	lines := strings.Split(text, "\n")
	if len(lines) > 16 {
		lines = append(lines[:14], "…更多内容可在Mini App查看")
	}
	text = strings.Join(lines, "\n")
	var out strings.Builder
	units := 0
	for _, r := range text {
		n := utf16.RuneLen(r)
		if n < 1 {
			n = 1
		}
		if units+n > 920 {
			out.WriteString("…")
			break
		}
		units += n
		out.WriteRune(r)
	}
	text = out.String()
	target := 20 - rows*2
	if target < 6 {
		target = 6
	}
	if target > 14 {
		target = 14
	}
	for strings.Count(text, "\n")+1 < target {
		text += "\n\u2063"
	}
	return text
}

func (a *App) telegramMemberAccountInfo(ctx context.Context, c telegramCommandCtx) {
	user, found := a.store().FindUserByTelegramID(c.FromID)
	if !found || !user.Active {
		return
	}
	user = effectiveMember(user)
	status := "尚未开通"
	if user.EmbyID != "" {
		status = "已开通"
		if user.EmbyDisabled || !a.embyShouldEnableUser(user) {
			status = "播放已停用"
		}
	}
	data := a.memberAccount(user)
	text := fmt.Sprintf("见微 · 我的账户\n\n用户：%s\nTelegram ID：%d\n账户类别：%s\n当前状态：%s\n可用积分：%d\nEmby账号：%s", firstNonEmpty(user.EmbyUsername, user.TelegramUsername, user.Username), user.TelegramID, memberRoleName(user.Role), status, a.store().Wallet(user.UID).Available, firstNonEmpty(user.EmbyUsername, "尚未开通"))
	if user.EmbyID != "" {
		text += "\n有效期：" + telegramExpiryTimeLabel(user.ExpiredAt)
	}
	if data["security_code_set"] == false {
		text += "\n安全码：尚未设置"
	}
	rows := [][]map[string]any{{memberButton("账号与线路", "/lines"), memberButton("设备管理", "/devices")}, {memberButton("签到与保号", "/retention"), memberButton("账户安全", "/account_security")}}
	a.memberSendButtons(ctx, c.ChatID, text, rows)
}

func (a *App) telegramMemberMore(ctx context.Context, c telegramCommandCtx) {
	rows := [][]map[string]any{}
	for _, v := range a.store().Services().Items {
		if v.Enabled {
			rows = append(rows, []map[string]any{memberButton("音乐、小说与专属影视", "/services")})
			break
		}
	}
	if a.cfg().MediaRequestEnabled {
		rows = append(rows, []map[string]any{memberButton("求片与进度", "/request")})
	}
	if a.cfg().InviteEnabled {
		rows = append(rows, []map[string]any{memberButton("邀请中心", "/invite")})
	}
	if a.cfg().TelegramShopEnabled {
		rows = append(rows, []map[string]any{memberButton("商店登录码", "/shopcode")})
	}
	rows = append(rows, []map[string]any{memberButton("使用帮助", "/member_help")})
	a.memberSendButtons(ctx, c.ChatID, "见微 · 更多服务\n\n观影之外的小事，也在这里。\n公告、收藏、媒体库与播放排行\n可在Mini App的「发现」里查看。\n\n不支持Mini App的客户端\n仍可通过 / 快捷命令操作。", rows)
}

func (a *App) telegramMemberHelp(ctx context.Context, c telegramCommandCtx) {
	text := "见微 · 使用指引\n\n/account 查询账户 · /lines 播放线路\n/devices 设备与软件 · /service 在线状态\n/retention 签到保号 · /points 积分商店\n/redeem 使用卡码 · /security 安全码\n/password 修改密码 · /shopcode 商店登录\n/request 求片进度\n\n普通播放器使用已开通的媒体账号。\nEmby原生管理员仅通过安全入口管理。\n新密码只在创建时显示，请及时备份。"
	a.memberSendButtons(ctx, c.ChatID, text, [][]map[string]any{{memberButton("查询我的账户", "/account"), memberButton("账户安全", "/account_security")}})
}

func init() {
	for _, item := range []struct {
		name, description string
		handler           func(*App, context.Context, telegramCommandCtx)
	}{{"account", "查询我的账户信息", (*App).telegramMemberAccountInfo}, {"more", "更多服务", (*App).telegramMemberMore}, {"member_help", "会员使用指引", (*App).telegramMemberHelp}} {
		handler := item.handler
		telegramCommandRegistry["/"+item.name] = telegramCommandSpec{name: item.name, label: "/" + item.name, description: item.description, category: "user", private: true, order: 47, handler: handler}
	}
}
