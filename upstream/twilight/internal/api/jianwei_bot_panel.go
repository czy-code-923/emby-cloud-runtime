package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"github.com/prejudice-studio/twilight/internal/store"
)

func init() {
	for index, item := range []struct{ name, description, usage string }{
		{"panel", "打开见微 Emby 主面板", "/panel"},
		{"signin", "每日签到领取积分", "/signin"},
		{"renew", "使用积分续期 Emby", "/renew confirm"},
		{"redeem", "使用注册或续期兑换码", "/redeem <兑换码>"},
		{"register", "使用已有资格开通 Emby", "/register <Emby用户名>"},
		{"devices", "查看我的 Emby 在线设备", "/devices"},
		{"lines", "查看我的播放线路", "/lines"},
		{"request", "提交求片并查看进度", "/request <片名> 或 /request"},
		{"invite", "查看邀请资格", "/invite"},
		{"security", "设置或修改安全码", "/security <新安全码> [当前安全码]"},
		{"password", "安全码验证后修改 Emby 密码", "/password <安全码> <新密码>"},
		{"device_list", "查看登记设备与播放配额", "/device_list"},
		{"device_release", "停用设备并释放名额", "/device_release <安全码> <设备标识>"},
	} {
		command := "/" + item.name
		telegramCommandRegistry[command] = telegramCommandSpec{name: item.name, label: command, description: item.description, usage: item.usage, category: "user", order: 31 + index, private: true, handler: func(a *App, ctx context.Context, c telegramCommandCtx) { a.telegramMemberCommand(ctx, c) }}
	}
}

func (a *App) telegramMemberPanel(ctx context.Context, chatID int64, user store.User) {
	cfg := a.cfg()
	escape := html.EscapeString
	text := ""
	if cfg.TelegramBannerEnabled && strings.TrimSpace(cfg.TelegramBannerText) != "" {
		text = "<b>推广 · 推荐内容</b>\n" + escape(truncateString(cfg.TelegramBannerText, 500)) + "\n\n"
	}
	text += "<b>见微 Emby · 微光影院</b>\n小见世界，细看生活。\n\n"
	text += "你好，" + escape(user.Username) + "\n"
	if user.EmbyID == "" {
		if user.PendingEmby {
			text += "开通资格已就绪，点击「开通 Emby」设置用户名。\n\n"
		} else {
			text += "已建立 Telegram 身份，尚未开通 Emby。请先兑换注册码，再开通媒体账户。\n\n"
		}
	} else {
		text += "欢迎回来，你的 Telegram 与 Emby 已关联。\n\n"
	}
	status := "等待开通"
	if user.EmbyID != "" {
		status = "已开通"
		if user.EmbyDisabled {
			status = "播放已停用"
		}
	}
	text += "🎞  媒体账户  ·  " + status + "\n"
	if user.EmbyUsername != "" {
		text += "👤  Emby  ·  " + escape(user.EmbyUsername) + "\n"
	}
	expiry := "尚未开通"
	if user.EmbyID != "" {
		expiry = "长期有效"
		if t := publicExpiryUnix(user.ExpiredAt); t > 0 {
			expiry = time.Unix(t, 0).Format("2006-01-02")
		}
	}
	text += fmt.Sprintf("🗓  有效期  ·  %s\n✦  积分  ·  %d\n\n选择下方功能；输入 / 可查看快捷命令。", expiry, a.store().Signin(user.UID).Points)
	button := func(text, command string) map[string]any {
		return map[string]any{"text": text, "callback_data": "jw:" + command}
	}
	rows := [][]map[string]any{{button("👤 我的账户", "/me"), button("🎞 服务状态", "/emby")}, {button("🎟 兑换卡码", "/redeem"), button("✨ 开通 Emby", "/register")}}
	if cfg.SigninEnabled {
		rows = append(rows, []map[string]any{button("✦ 每日签到", "/signin"), button("🗓 积分续期", "/renew")})
	}
	rows = append(rows, []map[string]any{button("📱 我的设备", "/devices"), button("🧭 播放线路", "/lines")})
	rows = append(rows, []map[string]any{button("🛡 保号方式与进度", "/retention")})
	rows = append(rows, []map[string]any{button("📌 见微公告", "/announcements"), button("♡ 我的收藏", "/favorites")}, []map[string]any{button("🔎 媒体库", "/library"), button("🏆 播放排行", "/rank")})
	rows = append(rows, []map[string]any{button("🔐 安全码", "/security"), button("🔑 修改密码", "/password")}, []map[string]any{button("设备配额", "/device_list"), button("停用旧设备", "/device_release")})
	if cfg.MediaRequestEnabled {
		rows = append(rows, []map[string]any{button("🎬 求片与进度", "/request")})
	}
	if cfg.InviteEnabled {
		rows = append(rows, []map[string]any{button("🤝 邀请中心", "/invite")})
	}
	if target := a.jianweiMiniURL(); target != "" {
		rows = append(rows, []map[string]any{{"text": "打开见微面板 ↗", "web_app": map[string]string{"url": target}}})
	}
	if cfg.TelegramShopEnabled {
		if target := publicHTTPSURL(cfg.CommerceStoreURL); target != "" {
			rows = append(rows, []map[string]any{{"text": "🛍 独立发卡商店", "url": target}, button("商店登录码", "/shopcode")})
		}
	}
	if cfg.TelegramBannerEnabled {
		if target := publicHTTPSURL(cfg.TelegramBannerURL); target != "" {
			rows = append(rows, []map[string]any{{"text": "推广 · 了解更多", "url": target}})
		}
	}
	rows = append(rows, []map[string]any{button("📖 使用帮助", "/help"), button("刷新主面板", "/panel")})
	_, _ = telegramPostResult[telegramDiscardResult](a, ctx, "sendMessage", telegramSendMessageRequest{ChatID: chatID, Text: text, ParseMode: "HTML", DisableWebPagePreview: true, ReplyMarkup: map[string]any{"inline_keyboard": rows}}, 20*time.Second)
}

func (a *App) telegramHandleMemberCallback(ctx context.Context, callback *telegramCallbackQuery) bool {
	if !strings.HasPrefix(callback.Data, "jw:") {
		return false
	}
	if callback.Message == nil || callback.From.IsBot || callback.From.ID <= 0 || callback.Message.Chat.ID != callback.From.ID {
		_ = a.telegramAnswerCallbackQuery(ctx, callback.ID, "请在与机器人的私聊中使用此面板。", true)
		return true
	}
	if !a.allowRate(ctx, rateKey("member-callback:", callback.From.ID), 30, time.Minute) {
		return true
	}
	_ = a.telegramAnswerCallbackQuery(ctx, callback.ID, "", false)
	ctx = context.WithValue(ctx, memberPanelContextKey{}, memberPanelTarget{ChatID: callback.From.ID, MessageID: callback.Message.MessageID})
	command := strings.TrimPrefix(callback.Data, "jw:")
	c := telegramCommandCtx{ChatID: callback.From.ID, FromID: callback.From.ID, Username: callback.From.Username, Command: command}
	// Only bounded read-only discovery commands may carry paging arguments.
	parts := strings.Fields(command)
	if len(parts) > 1 {
		paging := (parts[0] == "/announcements" || parts[0] == "/favorites" || parts[0] == "/library") && strings.HasPrefix(parts[1], "page:")
		selection := len(parts) == 2 && parts[0] == "/retention" && (parts[1] == "A" || parts[1] == "B" || parts[1] == "off")
		confirmation := len(parts) == 2 && parts[0] == "/retention_confirm" && len(parts[1]) == 24 && memberRedeemPayload.MatchString(parts[1])
		if len(callback.Data) > 64 || (!paging && !selection && !confirmation) {
			return true
		}
		command = parts[0]
		c.Command = command
		c.Args = parts[1:]
	}
	// Callbacks cannot dispatch admin commands, custom commands or arguments.
	if spec, found := telegramCommandRegistry[command]; found && !spec.admin && spec.category == "user" {
		a.telegramDispatchRegistry(ctx, command, c, true)
	} else if command == "/help" {
		_ = a.telegramSendMessage(ctx, c.ChatID, a.telegramHelpText(false))
	}
	return true
}

type memberPanelContextKey struct{}
type memberPanelTarget struct{ ChatID, MessageID int64 }

// Inline button results replace their originating private message. Commands
// typed by the member have no target and continue to produce a normal reply.
func (a *App) memberPanelEdit(ctx context.Context, method string, body any) (string, any) {
	target, ok := ctx.Value(memberPanelContextKey{}).(memberPanelTarget)
	if !ok || method != "sendMessage" || target.MessageID <= 0 {
		return method, body
	}
	message, ok := body.(telegramSendMessageRequest)
	if !ok || fmt.Sprint(message.ChatID) != fmt.Sprint(target.ChatID) {
		return method, body
	}
	markup := message.ReplyMarkup
	if markup == nil {
		rows := [][]map[string]any{{{"text": "‹ 返回主面板", "callback_data": "jw:/panel"}}}
		if a.cfg().TelegramShopEnabled {
			if url := publicHTTPSURL(a.cfg().CommerceStoreURL); url != "" {
				rows = append(rows, []map[string]any{{"text": "🛍 发卡商店 ↗", "url": url}})
			}
		}
		markup = map[string]any{"inline_keyboard": rows}
	}
	return "editMessageText", map[string]any{"chat_id": target.ChatID, "message_id": target.MessageID, "text": message.Text, "parse_mode": message.ParseMode, "disable_web_page_preview": true, "reply_markup": markup}
}

// A bounded in-process transport reuses the existing business handlers and
// their atomic Store mutations. It makes no network request and accepts no URL.
type memberReply struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (w *memberReply) Header() http.Header { return w.header }
func (w *memberReply) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
}
func (w *memberReply) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	n := len(p)
	if w.body.Len()+n <= 65536 {
		_, _ = w.body.Write(p)
	}
	return n, nil
}
func (a *App) invokeMember(ctx context.Context, user store.User, next HandlerFunc, body map[string]any) (bool, string, any) {
	raw, _ := json.Marshal(body)
	r, _ := http.NewRequestWithContext(context.WithValue(ctx, principalKey, principal{User: user}), http.MethodPost, "http://member.invalid/member", bytes.NewReader(raw))
	r.RemoteAddr = "127.0.0.1:0"
	r.Header.Set("Content-Type", "application/json")
	w := &memberReply{header: make(http.Header)}
	next(w, r, nil)
	var envelope struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    any    `json:"data"`
	}
	if json.Unmarshal(w.body.Bytes(), &envelope) != nil {
		return false, "操作暂未完成，请稍后重试。", nil
	}
	return envelope.Success, envelope.Message, envelope.Data
}

func (a *App) telegramMemberCommand(ctx context.Context, c telegramCommandCtx) {
	user, found := a.store().FindUserByTelegramID(c.FromID)
	if !found || !user.Active {
		_ = a.telegramSendMessage(ctx, c.ChatID, "请先 /start 建立身份；账户停用请联系管理员。")
		return
	}
	if !a.allowRate(ctx, rateKey("member-command:", user.UID), 30, time.Minute) {
		return
	}
	if c.Command == "/panel" {
		a.telegramMemberPanel(ctx, c.ChatID, user)
		return
	}
	var handler HandlerFunc
	body := map[string]any{}
	password := ""
	switch c.Command {
	case "/signin":
		handler = a.handleSignin
	case "/renew":
		if len(c.Args) != 1 || c.Args[0] != "confirm" {
			_ = a.telegramSendMessage(ctx, c.ChatID, fmt.Sprintf("积分续期：%d 积分 / %d 天。确认扣除积分并续期，请输入 /renew confirm", a.cfg().SigninRenewalCost, a.cfg().SigninRenewalDays))
			return
		}
		handler = a.handleSigninRenew
	case "/redeem":
		if len(c.Args) != 1 {
			_ = a.telegramSendMessage(ctx, c.ChatID, "发送 /redeem 兑换码\n支持注册码、续期码及已开启的邀请资格；只在此私聊发送。")
			return
		}
		handler = a.handleUseCode
		body["code"] = c.Args[0]
	case "/register":
		if len(c.Args) != 1 {
			_ = a.telegramSendMessage(ctx, c.ChatID, "先使用 /redeem 兑换开通资格，然后发送 /register 用户名。\n成功后会生成 Emby 密码；已有 Emby 账号请联系管理员绑定，避免重复注册。")
			return
		}
		buf := make([]byte, 16)
		if _, err := rand.Read(buf); err != nil {
			return
		}
		password = "Jw!" + hex.EncodeToString(buf)
		handler = a.handleRegisterEmby
		body["emby_username"] = c.Args[0]
		body["emby_password"] = password
	case "/devices":
		handler = a.handleV2EmbyUserSessions
	case "/security":
		if len(c.Args) < 1 || len(c.Args) > 2 {
			_ = a.telegramSendMessage(ctx, c.ChatID, "发送 /security 新安全码 [当前安全码]\n使用6–12位数字；已有安全码时必须提供当前码。仅在此私聊输入，完成后删除含码的消息。")
			return
		}
		handler = a.handleMemberSecurityCode
		body["new_code"] = c.Args[0]
		if len(c.Args) == 2 {
			body["old_code"] = c.Args[1]
		}
		if c.MessageID > 0 {
			_ = a.telegramDeleteMessage(ctx, c.ChatID, c.MessageID)
		}
	case "/password":
		if len(c.Args) != 2 {
			_ = a.telegramSendMessage(ctx, c.ChatID, "发送 /password 安全码 新Emby密码\n密码至少8位，包含大小写字母和数字。若账户要求邮箱验证，请使用 Mini App 完成验证。仅在此私聊输入。")
			return
		}
		handler = a.handleMemberPassword
		body["security_code"] = c.Args[0]
		body["new_password"] = c.Args[1]
		if c.MessageID > 0 {
			_ = a.telegramDeleteMessage(ctx, c.ChatID, c.MessageID)
		}
	case "/device_list":
		handler = a.handleMemberDeviceRegistry
	case "/device_release":
		if len(c.Args) != 2 {
			_ = a.telegramSendMessage(ctx, c.ChatID, "先发送 /device_list 查看标识，再发送 /device_release 安全码 设备标识\n此操作将禁止该设备继续访问并释放名额；需要重新使用时请联系管理员。")
			return
		}
		handler = a.handleMemberReleaseDevice
		body["security_code"] = c.Args[0]
		body["device_id"] = c.Args[1]
		if c.MessageID > 0 {
			_ = a.telegramDeleteMessage(ctx, c.ChatID, c.MessageID)
		}
	case "/lines":
		handler = a.handleEmbyURLs
	case "/invite":
		handler = a.handleMemberInvite
		if len(c.Args) > 0 {
			if c.Args[0] != "create" {
				_ = a.telegramSendMessage(ctx, c.ChatID, "/invite 查看资格；/invite create 创建邀请码（默认有效期由管理员设置）。")
				return
			}
			handler = a.handleCreateInviteCode
		}
	case "/request":
		if len(c.Args) == 0 {
			handler = a.handleMemberMediaRequests
		} else {
			handler = a.handleMemberCreateRequest
			body["title"] = truncateString(c.argString(), 120)
		}
	default:
		return
	}
	success, message, data := a.invokeMember(ctx, user, handler, body)
	if success {
		switch c.Command {
		case "/register":
			message = "Emby 已开通\n用户名：" + c.Args[0] + "\n初始密码：" + password + "\n请妥善保存，并在客户端修改密码。"
		case "/devices":
			message = "我的 Emby 在线设备\n"
			if items, ok := data.([]any); ok {
				for _, v := range items {
					if item, ok := v.(map[string]any); ok {
						message += "• " + asString(item["device_name"]) + " · " + asString(item["client"]) + "\n"
					}
				}
				if len(items) == 0 {
					message += "当前没有在线设备。"
				}
			}
		case "/device_list":
			message = "登记的播放设备\n"
			if d, ok := data.(map[string]any); ok {
				message += fmt.Sprintf("设备上限：%v · 同时播放流：%v（0为不限）\n", d["max_devices"], d["max_streams"])
				if list, ok := d["items"].([]any); ok {
					for _, v := range list {
						if device, ok := v.(map[string]any); ok {
							message += "• " + asString(device["name"]) + "\n标识：" + asString(device["id"]) + "\n"
						}
					}
				}
			}
		case "/lines":
			message = "我的播放线路\n"
			count := 0
			if d, ok := data.(map[string]any); ok {
				for _, key := range []string{"lines", "whitelist_lines"} {
					if list, ok := d[key].([]any); ok {
						for _, v := range list {
							if item, ok := v.(map[string]any); ok {
								count++
								message += "• " + asString(item["name"]) + "\n" + asString(item["url"]) + "\n"
							}
						}
					}
				}
			}
			if count == 0 {
				message += "暂无可用线路，请检查开通状态或联系管理员。"
			}
		case "/request":
			if len(c.Args) == 0 {
				message = "求片与进度\n发送 /request 片名 提交申请。\n"
				if d, ok := data.(map[string]any); ok {
					if items, ok := d["items"].([]any); ok {
						for _, v := range items {
							if item, ok := v.(map[string]any); ok {
								message += "• " + asString(item["title"]) + " · " + asString(item["status"]) + "\n"
							}
						}
					}
				}
			} else {
				message = "求片已提交，管理员审核后安排资源；/request 查看进度。"
			}
		case "/invite":
			message = memberInviteText(data)
		case "/signin":
			message += "\n返回 /panel 查看积分余额。"
		case "/redeem":
			message += "\n未开通用户可继续 /register 用户名；/panel 查看账户。"
		}
	}
	_ = a.telegramSendMessage(ctx, c.ChatID, truncateString(message, 3500))
}

func memberInviteText(data any) string {
	text := "邀请中心\n"
	if d, ok := data.(map[string]any); ok {
		if code := asString(d["code"]); code != "" {
			return "邀请码已生成：" + code + "\n邀请人使用 /redeem 兑换；分享后请勿重复使用。"
		}
		if can, ok := d["can_invite"].(bool); ok && can {
			text += "可发送 /invite create 创建邀请码。\n"
		} else {
			text += "暂不能创建：" + asString(d["reason"]) + "\n"
		}
		if items, ok := d["codes"].([]any); ok {
			for _, v := range items {
				if item, ok := v.(map[string]any); ok {
					text += "• " + asString(item["code"]) + "\n"
				}
			}
		}
	}
	return text
}

func (a *App) publishTelegramMemberCommands(ctx context.Context) error {
	commands := []map[string]string{{"command": "start", "description": "打开见微 Emby 主面板"}, {"command": "help", "description": "使用帮助与快捷命令"}}
	for _, item := range a.telegramCommandCatalog() {
		if !item.Admin && !item.Disabled && item.Name != "start" && item.Name != "help" && len(commands) < 100 {
			commands = append(commands, map[string]string{"command": strings.ToLower(item.Name), "description": truncateString(item.Description, 100)})
		}
	}
	_, err := telegramPostResult[telegramDiscardResult](a, ctx, "setMyCommands", map[string]any{"commands": commands, "scope": map[string]string{"type": "all_private_chats"}}, 15*time.Second)
	return err
}
