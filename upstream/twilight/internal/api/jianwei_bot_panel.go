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
	"net/url"
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
		{"devices", "登录设备、在线播放与配额", "/devices"},
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
	user = effectiveMember(user)
	cfg := a.cfg()
	escape := html.EscapeString
	text := ""
	if cfg.TelegramBannerEnabled && strings.TrimSpace(cfg.TelegramBannerText) != "" {
		text = "<b>推广 · 推荐内容</b>\n" + escape(truncateString(cfg.TelegramBannerText, 500)) + "\n\n"
	}
	text += "<b>见微 Emby · 微光影院</b>\n小见世界，细看生活。\n\n"
	displayName := firstNonEmpty(user.EmbyUsername, user.TelegramUsername, user.Username)
	text += "▎欢迎进入用户面板，" + escape(displayName) + "\n\n"
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
		if user.EmbyDisabled || !a.embyShouldEnableUser(user) {
			status = "播放已停用"
		}
	}
	role := "用户"
	if user.Role == store.RoleAdmin {
		role = "管理员"
	} else if user.Role == store.RoleWhitelist {
		role = "白名单"
	}
	text += fmt.Sprintf("· 🆔 Telegram ID  |  <code>%d</code>\n· 📊 当前状态  |  %s\n· 👥 账户类别  |  %s\n· ✦ 可用积分  |  %d\n· 👤 Emby 账号  |  %s\n", user.TelegramID, status, role, a.store().Wallet(user.UID).Available, escape(firstNonEmpty(user.EmbyUsername, "尚未开通")))
	mode := "尚未参与"
	if user.Role != store.RoleNormal {
		mode = "受保护账户 · 免活动保号"
	} else if v, err := a.store().RetentionView(user.UID, time.Now().Unix()); err == nil {
		if !v.Policy.Enabled {
			mode = "活动保号尚未开启"
		} else if v.Member.Member.Participation {
			if v.Member.Member.Mode == "A" {
				mode = fmt.Sprintf("签到积分 · 每窗扣%d积分续30天", v.Policy.Cost)
			} else {
				mode = fmt.Sprintf("观影 · %d / 30 分钟", v.Seconds/60)
			}
		} else if v.Member.Member.ExemptEnd > time.Now().Unix() {
			mode = "免保号有效期内"
		}
	}
	text += "· 🛡 保号方式  |  " + mode + "\n"
	expiry := "尚未开通"
	if user.EmbyID != "" {
		expiry = "长期有效"
		if t := publicExpiryUnix(user.ExpiredAt); t > 0 {
			expiry = time.Unix(t, 0).In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02 15:04:05")
		}
	}
	text += "· 🗓 有效期至  |  " + expiry + "\n\n选择下方功能；输入 / 可查看快捷命令。"
	button := func(text, command string) map[string]any {
		return map[string]any{"text": text, "callback_data": "jw:" + command}
	}
	rows := [][]map[string]any{{button("👤 我的账户", "/me"), button("🎞 在线状态", "/service")}}
	if user.EmbyID == "" {
		rows = append(rows, []map[string]any{button("✨ 开通 Emby", "/register"), button("🎟 使用卡码", "/redeem")})
	} else {
		rows = append(rows, []map[string]any{button("🧭 账号与线路", "/lines"), button("📱 设备管理", "/devices")})
	}
	rows = append(rows, []map[string]any{button("✦ 签到与保号", "/retention"), button("🛍 积分商店", "/points")}, []map[string]any{button("🎟 使用卡码", "/redeem"), button("🔐 账户安全", "/account_security")})
	if cfg.MediaRequestEnabled {
		rows = append(rows, []map[string]any{button("🎬 求片与进度", "/request")})
	}
	if cfg.InviteEnabled {
		rows = append(rows, []map[string]any{button("🤝 邀请中心", "/invite")})
	}
	if target := a.jianweiMiniURL(); target != "" {
		rows = append(rows, []map[string]any{{"text": "见微 Mini App · 公告 / 收藏 / 媒体 / 排行 ↗", "web_app": map[string]string{"url": target}}})
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
	a.sendMemberHero(ctx, chatID, text, rows)
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
	ctx = context.WithValue(ctx, memberPanelContextKey{}, memberPanelTarget{ChatID: callback.From.ID, MessageID: callback.Message.MessageID, Photo: len(callback.Message.Photo) > 0})
	command := strings.TrimPrefix(callback.Data, "jw:")
	c := telegramCommandCtx{ChatID: callback.From.ID, FromID: callback.From.ID, Username: callback.From.Username, Command: command}
	// Only bounded read-only discovery commands may carry paging arguments.
	parts := strings.Fields(command)
	if len(parts) > 1 {
		paging := (parts[0] == "/announcements" || parts[0] == "/favorites" || parts[0] == "/library") && strings.HasPrefix(parts[1], "page:")
		selection := len(parts) == 2 && parts[0] == "/retention" && (parts[1] == "A" || parts[1] == "B" || parts[1] == "off" || parts[1] == "month" || parts[1] == "quarter" || parts[1] == "year" || parts[1] == "rescue")
		confirmation := len(parts) == 2 && parts[0] == "/retention_confirm" && len(parts[1]) == 24 && memberRedeemPayload.MatchString(parts[1])
		memberChoice := (parts[0] == "/points" && (len(parts) == 2 || len(parts) == 3)) || ((parts[0] == "/device_stop" || parts[0] == "/points_redeem") && len(parts) == 2)
		favorite := len(parts) == 2 && (parts[0] == "/favorite" || parts[0] == "/unfavorite") && memberItemID.MatchString(parts[1])
		if len(callback.Data) > 64 || (!paging && !selection && !confirmation && !favorite && !memberChoice && !(len(parts) == 2 && parts[0] == "/invite" && parts[1] == "create")) {
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
type memberPanelTarget struct {
	ChatID, MessageID int64
	Photo             bool
}

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
	method = "editMessageText"
	if target.Photo {
		method = "editMessageCaption"
	}
	result := map[string]any{"chat_id": target.ChatID, "message_id": target.MessageID, "text": message.Text, "parse_mode": message.ParseMode, "disable_web_page_preview": true, "reply_markup": markup}
	if target.Photo {
		delete(result, "text")
		result["caption"] = truncateString(message.Text, 1020)
	}
	return method, result
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
		a.memberInputMu.Lock()
		delete(a.memberInputs, c.FromID)
		a.memberInputMu.Unlock()
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
		if !a.cfg().SigninEnabled || !a.cfg().SigninRenewalEnabled {
			_ = a.telegramSendMessage(ctx, c.ChatID, "管理员尚未开启手动积分续期。使用 /retention 查看活动保号与免保号积分卡；没有扣除积分。")
			return
		}
		if len(c.Args) != 1 || c.Args[0] != "confirm" {
			_ = a.telegramSendMessage(ctx, c.ChatID, fmt.Sprintf("积分续期：%d 积分 / %d 天。确认扣除积分并续期，请输入 /renew confirm", a.cfg().SigninRenewalCost, a.cfg().SigninRenewalDays))
			return
		}
		handler = a.handleSigninRenew
	case "/redeem":
		if len(c.Args) != 1 {
			a.beginMemberInput(ctx, c, "/redeem", "", "请输入注册、续期或白名单兑换码。")
			return
		}
		handler = a.handleUseCode
		body["code"] = c.Args[0]
	case "/register":
		if user.EmbyID != "" {
			_ = a.telegramSendMessage(ctx, c.ChatID, "你已经注册 Emby，账号："+user.EmbyUsername+"。\n可在「播放线路」「设备」「修改密码」管理现有账户，无需再次开通。")
			return
		}
		if len(c.Args) != 1 {
			if !user.PendingEmby {
				a.memberSendButtons(ctx, c.ChatID, "请先取得开通资格，再开通 Emby。", [][]map[string]any{{memberButton("使用注册码", "/redeem")}})
			} else {
				a.beginMemberInput(ctx, c, "/register", "", "资格已就绪，请输入Emby用户名；系统将生成初始密码。")
			}
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
		a.telegramDeviceButtons(ctx, c)
		return
	case "/device_stop":
		if len(c.Args) != 1 {
			return
		}
		chosen := ""
		name := ""
		for _, device := range user.MediaDevices {
			if memberDeviceKey(device.ID) == c.Args[0] {
				chosen = device.ID
				name = device.Name
			}
		}
		if chosen == "" {
			a.memberSendButtons(ctx, c.ChatID, "设备已变化或已停用，请刷新设备列表。", nil)
			return
		}

		if user.SecurityCodeHash == "" {
			a.memberSendButtons(ctx, c.ChatID, "请先设置安全码，再停用设备。", [][]map[string]any{{memberButton("设置安全码", "/security")}})
			return
		}
		a.beginMemberInput(ctx, c, "/device_release", chosen, "将停用设备："+name+"。请输入安全码确认。")
		return
	case "/security":
		if len(c.Args) < 1 || len(c.Args) > 2 {
			prompt := "请设置6–12位数字安全码。"
			if user.SecurityCodeHash != "" {
				prompt = "请输入：新安全码 当前安全码（用空格分隔）。"
			}
			a.beginMemberInput(ctx, c, "/security", "", prompt)
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
		if user.SecurityCodeHash == "" {
			a.memberSendButtons(ctx, c.ChatID, "尚未设置安全码，请先设置；安全码用于保护修改密码和设备停用。", [][]map[string]any{{memberButton("设置安全码", "/security")}})
			return
		}
		if len(c.Args) != 2 {
			a.beginMemberInput(ctx, c, "/password", "", "请输入：安全码 新Emby密码（用空格分隔）。\n密码至少8位并包含大小写字母和数字；若要求邮箱验证，请在Mini App操作。")
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
			message = "我的账号与播放线路\n账号：" + user.EmbyUsername + "\n密码：不可反查，请使用修改密码按钮设置\n"
			count := 0
			if d, ok := data.(map[string]any); ok {
				for _, key := range []string{"lines", "whitelist_lines"} {
					if list, ok := d[key].([]any); ok {
						for _, v := range list {
							if item, ok := v.(map[string]any); ok {
								count++
								target, _ := url.Parse(asString(item["url"]))
								port := target.Port()
								if port == "" {
									port = "443"
									if target.Scheme == "http" {
										port = "80"
									}
								}
								message += "• " + asString(item["name"]) + "\n" + asString(item["url"]) + " · 端口 " + port + "\n"
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
								message += "• " + asString(item["title"]) + " · " + store.MediaRequestStatusText(asString(item["status"])) + "\n"
								if note := asString(item["admin_note"]); note != "" {
									message += "  管理员：" + truncateString(note, 150) + "\n"
								}
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
			if d, ok := data.(map[string]any); ok {
				if award, ok := d["today_award"].(map[string]any); ok {
					message += fmt.Sprintf("\n\n· 今日奖励  |  %v %s\n· 实际入账  |  %v\n· 封顶暂未发放  |  %v\n· 当前可用余额  |  %v\n· 连续签到  |  %v天", award["raw_points"], a.cfg().SigninCurrencyName, award["credited_points"], award["withheld_points"], d["current_points"], d["current_streak"])
				}
			}
			message += "\n\n同日重试不会重复入账；返回主面板可查看保号进度。"
		case "/redeem":
			if user.EmbyID == "" {
				message += "\n开通资格已就绪，请继续 /register 用户名；/panel 查看账户。"
			} else {
				message += "\n已同步现有 Emby 账户，无需再次注册；返回主面板查看有效期与线路。"
			}
		}
	}
	rows := [][]map[string]any{}
	if c.Command == "/request" && len(c.Args) == 0 && a.cfg().MediaRequestEnabled {
		rows = append(rows, []map[string]any{memberButton("提交新的求片", "/request_new")})
	}
	if c.Command == "/invite" && len(c.Args) == 0 && a.cfg().InviteEnabled {
		rows = append(rows, []map[string]any{memberButton("创建邀请码", "/invite create")})
	}
	if success && c.Command == "/security" {
		rows = append(rows, []map[string]any{memberButton("继续修改密码", "/password"), memberButton("我的设备", "/devices")})
	}
	if success && c.Command == "/redeem" {
		fresh, _ := a.store().User(user.UID)
		if fresh.EmbyID == "" && fresh.PendingEmby {
			rows = append(rows, []map[string]any{memberButton("继续开通Emby", "/register")})
		} else {
			rows = append(rows, []map[string]any{memberButton("查看账号与线路", "/lines"), memberButton("查看保号", "/retention")})
		}
	}
	if c.Command == "/lines" {
		if user.EmbyUsername != "" {
			rows = append(rows, []map[string]any{{"text": "复制Emby账号", "copy_text": map[string]string{"text": user.EmbyUsername}}, memberButton("修改密码", "/password")})
		}
		if d, ok := data.(map[string]any); ok {
			for _, key := range []string{"lines", "whitelist_lines"} {
				if list, ok := d[key].([]any); ok {
					for _, raw := range list {
						item, _ := raw.(map[string]any)
						target := asString(item["url"])
						if len([]rune(target)) > 0 && len([]rune(target)) <= 256 {
							rows = append(rows, []map[string]any{{"text": "复制 · " + asString(item["name"]), "copy_text": map[string]string{"text": target}}})
						}
					}
				}
			}
		}
	}
	a.memberSendButtons(ctx, c.ChatID, truncateString(message, 3500), rows)
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

func (a *App) telegramMemberCommands() []map[string]string {
	commands := []map[string]string{{"command": "start", "description": "打开见微 Emby 主面板"}, {"command": "help", "description": "使用帮助与快捷命令"}}
	for _, item := range a.telegramCommandCatalog() {
		if !item.Admin && !item.Disabled && a.telegramMemberCommandVisible(item.Name) && item.Name != "start" && item.Name != "help" && len(commands) < 100 {
			commands = append(commands, map[string]string{"command": strings.ToLower(item.Name), "description": truncateString(item.Description, 100)})
		}
	}
	return commands
}

func (a *App) publishTelegramMemberCommands(ctx context.Context) error {
	_, err := telegramPostResult[telegramDiscardResult](a, ctx, "setMyCommands", map[string]any{"commands": a.telegramMemberCommands(), "scope": map[string]string{"type": "all_private_chats"}}, 15*time.Second)
	return err
}

// Keep confirmation transports and disabled features out of the slash menu.
// Their handlers independently enforce authorization and feature gates.
func (a *App) telegramMemberCommandVisible(name string) bool {
	cfg := a.cfg()
	switch name {
	case "retention_confirm", "points_redeem", "device_sync", "device_stop", "request_new", "account_security":
		return false
	case "signin":
		return cfg.SigninEnabled
	case "renew":
		return cfg.SigninEnabled && cfg.SigninRenewalEnabled
	case "invite":
		return cfg.InviteEnabled
	case "request":
		return cfg.MediaRequestEnabled
	case "rank":
		return cfg.PlayRankEnabled && cfg.PlayRankUserVisible
	case "shopcode":
		return cfg.TelegramShopEnabled
	}
	return true
}
