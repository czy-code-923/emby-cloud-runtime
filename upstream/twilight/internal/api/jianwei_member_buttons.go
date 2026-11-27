package api

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/prejudice-studio/twilight/internal/store"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type memberInputFlow struct {
	Command   string
	Device    string
	Code      string
	At        int64
	MessageID int64
	Photo     bool
}

func memberButton(text, command string) map[string]any {
	return map[string]any{"text": text, "callback_data": "jw:" + command}
}
func (a *App) memberSendButtons(ctx context.Context, chatID int64, text string, rows [][]map[string]any) {
	rows = append(rows, []map[string]any{memberButton("‹ 返回主面板", "/panel")})
	_, _ = telegramPostResult[telegramDiscardResult](a, ctx, "sendMessage", telegramSendMessageRequest{ChatID: chatID, Text: text, ReplyMarkup: map[string]any{"inline_keyboard": rows}}, 20*time.Second)
}
func (a *App) beginMemberInput(ctx context.Context, c telegramCommandCtx, command, device, prompt string) {
	target, _ := ctx.Value(memberPanelContextKey{}).(memberPanelTarget)
	flow := memberInputFlow{Command: command, Device: device, At: time.Now().Unix(), MessageID: target.MessageID, Photo: target.Photo}
	a.memberInputMu.Lock()
	if a.memberInputs == nil {
		a.memberInputs = map[int64]memberInputFlow{}
	}
	for id, v := range a.memberInputs {
		if v.At < time.Now().Unix()-300 {
			delete(a.memberInputs, id)
		}
	}
	if len(a.memberInputs) >= 512 {
		oldID := int64(0)
		oldAt := time.Now().Unix()
		for id, v := range a.memberInputs {
			if v.At <= oldAt {
				oldAt = v.At
				oldID = id
			}
		}
		delete(a.memberInputs, oldID)
	}
	a.memberInputs[c.FromID] = flow
	a.memberInputMu.Unlock()
	a.memberSendButtons(ctx, c.ChatID, prompt+"\n\n请仅在此私聊输入，5分钟内有效；返回主面板可取消。", nil)
}
func (a *App) consumeMemberInput(ctx context.Context, message *telegramMessage) bool {
	if message.From.ID <= 0 || message.Chat.ID != message.From.ID || strings.HasPrefix(message.Text, "/") {
		return false
	}
	a.memberInputMu.Lock()
	flow, ok := a.memberInputs[message.From.ID]
	delete(a.memberInputs, message.From.ID)
	a.memberInputMu.Unlock()
	if !ok || flow.At < time.Now().Unix()-300 {
		return false
	}
	user, ok := a.store().FindUserByTelegramID(message.From.ID)
	if !ok || !user.Active {
		return true
	}
	if flow.MessageID > 0 {
		ctx = context.WithValue(ctx, memberPanelContextKey{}, memberPanelTarget{ChatID: message.From.ID, MessageID: flow.MessageID, Photo: flow.Photo})
	}
	text := strings.TrimSpace(message.Text)
	if len(text) > 256 {
		a.memberSendButtons(ctx, message.Chat.ID, "输入过长，请重新选择操作。", nil)
		return true
	}
	c := telegramCommandCtx{FromID: message.From.ID, ChatID: message.Chat.ID, MessageID: message.MessageID, Username: message.From.Username, Command: flow.Command}
	switch flow.Command {
	case "/redeem", "/register":
		c.Args = []string{text}
	case "/security":
		c.Args = strings.Fields(text)
	case "/password":
		c.Args = strings.Fields(text)
	case "/device_release":
		c.Args = []string{text, flow.Device}
	case "/request":
		c.Args = []string{text}
	default:
		return false
	}
	a.telegramMemberCommand(ctx, c)
	return true
}
func (a *App) handleMemberStatus(w http.ResponseWriter, r *http.Request, _ Params) {
	sessions, e := a.embySessionsSnapshot(r.Context(), false)
	if e != nil {
		fail(w, 502, "媒体服务暂不可达，未将故障显示为零人在线")
		return
	}
	online := map[string]bool{}
	watching := map[string]bool{}
	playing := 0
	now := time.Now().Unix()
	for _, s := range sessions {
		uid := asString(s["UserId"])
		if uid == "" {
			continue
		}
		at, e := time.Parse(time.RFC3339Nano, asString(s["LastActivityDate"]))
		if e == nil && now-at.Unix() <= 300 {
			online[uid] = true
		}
		if _, ok := embySessionNowPlaying(s); ok {
			state, _ := s["PlayState"].(map[string]any)
			paused, _ := state["IsPaused"].(bool)
			online[uid] = true
			if !paused {
				watching[uid] = true
				playing++
			}
		}
	}
	ok(w, "OK", map[string]any{"online_users": len(online), "watching_users": len(watching), "playing_sessions": playing, "updated_at": now, "online_window_seconds": 300})
}
func (a *App) telegramMemberStatus(ctx context.Context, c telegramCommandCtx) {
	u, found := a.store().FindUserByTelegramID(c.FromID)
	if !found || !u.Active {
		return
	}
	success, message, data := a.invokeMember(ctx, u, a.handleMemberStatus, nil)
	if success {
		d, _ := data.(map[string]any)
		message = fmt.Sprintf("见微 Emby · 服务状态\n\n在线人数：%v（近5分钟活动）\n观看人数：%v\n正在播放：%v 路\n\n只展示聚合人数，不展示其他用户信息。", d["online_users"], d["watching_users"], d["playing_sessions"])
	}
	a.memberSendButtons(ctx, c.ChatID, message, [][]map[string]any{{memberButton("刷新状态", "/service")}})
}
func (a *App) telegramPoints(ctx context.Context, c telegramCommandCtx) {
	u, found := a.store().FindUserByTelegramID(c.FromID)
	if !found || !u.Active {
		return
	}
	w := a.store().Wallet(u.UID)
	p := a.store().RetentionPolicy()
	rows := [][]map[string]any{}
	text := fmt.Sprintf("见微 Emby · 积分商店\n\n可用：%d · 冻结：%d · 总额：%d\n月卡 %d积分 / 30天\n季卡 %d积分 / 90天\n年卡 %d积分 / 365天\n\n扣分后生成本人专属续期码，核销后保留剩余时长并增加免保号时长。", w.Available, w.Frozen, w.Balance, p.MonthPoints, p.QuarterPoints, p.YearPoints)
	if len(c.Args) == 2 && c.Args[0] == "confirm" {
		sku := strings.Split(c.Args[1], ":")
		if len(sku) != 3 || len(sku[1]) != 16 {
			return
		}
		success, message, data := a.invokeMember(ctx, u, a.handlePointsPurchase, map[string]any{"sku": sku[0], "request_id": sku[1], "confirm": true, "expected_cost": financeUID(sku[2])})
		text = message
		if success {
			d, _ := data.(map[string]any)
			code := asString(d["code"])
			text += "\n\n" + code
			rows = append(rows, []map[string]any{memberButton("立即使用此码", "/points_redeem "+asString(d["id"]))})
		}
	} else if len(c.Args) == 1 {
		sku := c.Args[0]
		if sku != "month" && sku != "quarter" && sku != "year" {
			return
		}
		cost := map[string]int{"month": p.MonthPoints, "quarter": p.QuarterPoints, "year": p.YearPoints}[sku]
		if !p.Enabled {
			a.memberSendButtons(ctx, c.ChatID, "积分商店尚未开启，没有扣除积分。", nil)
			return
		}
		if u.EmbyID == "" || u.ExpiredAt < 0 || u.Role == store.RoleAdmin {
			a.memberSendButtons(ctx, c.ChatID, "当前账户尚未开通或已长期有效，无需兑换普通续期卡；没有扣除积分。", [][]map[string]any{{memberButton("返回积分商店", "/points")}})
			return
		}
		if w.Available < cost {
			a.memberSendButtons(ctx, c.ChatID, fmt.Sprintf("可用积分%d，本卡需要%d积分，还差%d积分；没有扣分。", w.Available, cost, cost-w.Available), [][]map[string]any{{memberButton("每日签到", "/signin"), memberButton("返回积分商店", "/points")}})
			return
		}
		nonce := fmt.Sprintf("%016x", time.Now().UnixNano())

		text += "\n\n确认兑换？本次将扣除对应可用积分。"
		rows = append(rows, []map[string]any{memberButton("确认扣分并发码", "/points confirm "+sku+":"+nonce+":"+fmt.Sprint(map[string]int{"month": p.MonthPoints, "quarter": p.QuarterPoints, "year": p.YearPoints}[sku])), memberButton("取消", "/points")})
	} else {
		rows = append(rows, []map[string]any{memberButton("兑换月卡", "/points month"), memberButton("兑换季卡", "/points quarter"), memberButton("兑换年卡", "/points year")})
	}
	a.memberSendButtons(ctx, c.ChatID, text, rows)
}
func (a *App) telegramPointsRedeem(ctx context.Context, c telegramCommandCtx) {
	if len(c.Args) != 1 {
		return
	}
	u, ok := a.store().FindUserByTelegramID(c.FromID)
	o, found := a.store().ShopOrder(c.Args[0])
	if !ok || !found || o.UID != u.UID || o.Status != "issued" || o.RedeemedAt > 0 {
		a.memberSendButtons(ctx, c.ChatID, "卡码已使用或不属于当前账号，请刷新。", nil)
		return
	}
	code, e := a.shopCipher(o.ID, o.CodeCipher, true)
	if e != nil {
		return
	}
	c.Command = "/redeem"
	c.Args = []string{code}
	a.telegramMemberCommand(ctx, c)
}
func (a *App) telegramDeviceButtons(ctx context.Context, c telegramCommandCtx) {
	u, ok := a.store().FindUserByTelegramID(c.FromID)
	if !ok || !u.Active {
		return
	}
	success, message, data := a.invokeMember(ctx, u, a.handleMemberDeviceRegistry, nil)
	if !success {
		a.memberSendButtons(ctx, c.ChatID, message, nil)
		return
	}
	d, _ := data.(map[string]any)
	items, _ := d["items"].([]any)
	rows := [][]map[string]any{{memberButton("同步登录设备", "/device_sync")}}
	playing := fmt.Sprint(d["playing_streams"])
	if available, _ := d["sessions_available"].(bool); !available {
		playing = "暂不可用"
	}
	message = fmt.Sprintf("见微 · 我的设备\n\n登录登记：%v / %v 台\n正在播放：%v / %v 路\n\n0表示不限。每条记录包含设备与软件。\n选择下方设备可验证安全码后停用。", len(items), d["max_devices"], playing, d["max_streams"])
	page := 1
	if len(c.Args) == 1 && strings.HasPrefix(c.Args[0], "page:") {
		value, _ := strconv.Atoi(strings.TrimPrefix(c.Args[0], "page:"))
		page = clamp(value, 1, 250)
	}
	start := (page - 1) * 4
	if start >= len(items) {
		page = 1
		start = 0
	}
	for i := start; i < len(items) && i < start+4; i++ {
		item, _ := items[i].(map[string]any)
		client := firstNonEmpty(asString(item["client"]), "软件待同步")
		message += fmt.Sprintf("\n%d. %s · %s %s", i+1, truncateString(asString(item["name"]), 18), client, asString(item["client_version"]))
		rows = append(rows, []map[string]any{memberButton(fmt.Sprintf("%d · 停用 %s", i+1, truncateString(client, 15)), "/device_stop "+memberDeviceKey(asString(item["id"])))})
	}
	paging := []map[string]any{}
	if page > 1 {
		paging = append(paging, memberButton("‹ 上一页", fmt.Sprintf("/devices page:%d", page-1)))
	}
	if start+4 < len(items) {
		paging = append(paging, memberButton("下一页 ›", fmt.Sprintf("/devices page:%d", page+1)))
	}
	if len(paging) > 0 {
		rows = append(rows, paging)
	}

	a.memberSendButtons(ctx, c.ChatID, message, rows)
}
func init() {
	telegramCommandRegistry["/request_new"] = telegramCommandSpec{name: "request_new", label: "/request_new", description: "填写求片", category: "user", private: true, handler: func(a *App, ctx context.Context, c telegramCommandCtx) {
		if a.cfg().MediaRequestEnabled {
			a.beginMemberInput(ctx, c, "/request", "", "请输入片名，可附年份或季数。")
		}
	}}
	telegramCommandRegistry["/account_security"] = telegramCommandSpec{name: "account_security", label: "/account_security", description: "账户安全", category: "user", private: true, handler: func(a *App, ctx context.Context, c telegramCommandCtx) {
		u, ok := a.store().FindUserByTelegramID(c.FromID)
		if !ok || !u.Active {
			return
		}
		status := "尚未设置安全码"
		if u.SecurityCodeHash != "" {
			status = "安全码已设置"
		}
		a.memberSendButtons(ctx, c.ChatID, "账户安全\n\n"+status+"。密码无法反查；可通过安全码验证后修改。", [][]map[string]any{{memberButton("设置 / 修改安全码", "/security"), memberButton("修改Emby密码", "/password")}})
	}}

	for _, item := range []struct {
		name, description string
		handler           func(*App, context.Context, telegramCommandCtx)
	}{{"device_sync", "同步本人登录设备", func(a *App, ctx context.Context, c telegramCommandCtx) {
		u, found := a.store().FindUserByTelegramID(c.FromID)
		if !found || !u.Active {
			return
		}
		success, message, _ := a.invokeMember(ctx, u, a.handleMemberDeviceSync, nil)
		if !success {
			a.memberSendButtons(ctx, c.ChatID, message, [][]map[string]any{{memberButton("返回设备", "/devices")}})
			return
		}
		a.telegramDeviceButtons(ctx, c)
	}}, {"device_stop", "选择本人设备", func(a *App, ctx context.Context, c telegramCommandCtx) { a.telegramMemberCommand(ctx, c) }}, {"service", "在线人数与观看人数", (*App).telegramMemberStatus}, {"points", "积分商店月卡季卡", (*App).telegramPoints}, {"points_redeem", "使用积分商店专属码", (*App).telegramPointsRedeem}} {
		handler := item.handler
		telegramCommandRegistry["/"+item.name] = telegramCommandSpec{name: item.name, label: "/" + item.name, description: item.description, usage: "/" + item.name, category: "user", order: 46, private: true, handler: handler}
	}
}

func (a *App) sendMemberHero(ctx context.Context, chatID int64, text string, rows [][]map[string]any) {
	cfg := a.cfg()
	photo := publicHTTPSURL(cfg.TelegramPanelImageURL)
	// The previous built-in URL is migrated without replacing custom artwork.
	if photo != "" && (photo == strings.TrimRight(cfg.PublicBaseURL, "/")+"/brand/jianwei-panel.png" || photo == strings.TrimRight(cfg.PublicBaseURL, "/")+"/brand/jianwei-panel-v2.png" || photo == strings.TrimRight(cfg.PublicBaseURL, "/")+"/brand/jianwei-panel-v3.png") {
		photo = strings.TrimRight(cfg.PublicBaseURL, "/") + "/brand/jianwei-panel-v4.png"
	}
	if cfg.TelegramBannerEnabled && publicHTTPSURL(cfg.TelegramBannerImageURL) != "" {
		photo = publicHTTPSURL(cfg.TelegramBannerImageURL)
	}
	if photo == "" && publicHTTPSURL(cfg.PublicBaseURL) != "" {
		photo = strings.TrimRight(cfg.PublicBaseURL, "/") + "/brand/jianwei-panel-v4.png"
	}
	markup := map[string]any{"inline_keyboard": rows}
	if cfg.TelegramPanelImageEnabled && photo != "" {
		target, editing := ctx.Value(memberPanelContextKey{}).(memberPanelTarget)
		method := "sendPhoto"
		body := map[string]any{"chat_id": chatID, "photo": photo, "caption": memberPanelCaption(text, len(rows), false), "reply_markup": markup}
		if editing {
			method = "editMessageMedia"
			body = map[string]any{"chat_id": chatID, "message_id": target.MessageID, "media": map[string]any{"type": "photo", "media": photo, "caption": memberPanelCaption(text, len(rows), false)}, "reply_markup": markup}
		}
		if _, e := telegramPostResult[telegramDiscardResult](a, ctx, method, body, 20*time.Second); e == nil {
			return
		}
	}
	_, _ = telegramPostResult[telegramDiscardResult](a, ctx, "sendMessage", telegramSendMessageRequest{ChatID: chatID, Text: memberPanelCaption(text, len(rows), false), DisableWebPagePreview: true, ReplyMarkup: markup}, 20*time.Second)
}

func memberDeviceKey(id string) string {
	sum := sha256.Sum256([]byte(id))
	return fmt.Sprintf("%x", sum[:8])
}
