package api

import (
	"context"
	"errors"
	"fmt"
	"github.com/prejudice-studio/twilight/internal/retention"
	"github.com/prejudice-studio/twilight/internal/security"
	"github.com/prejudice-studio/twilight/internal/store"
	"net/http"
	"strings"
	"time"
)

func retentionError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	message := "保号操作未完成，请刷新后重试"
	switch {
	case errors.Is(err, retention.ErrUnavailable), errors.Is(err, store.ErrEmbyRequired):
		message = "当前尚不能选择保号方式，请先开通 Emby；免保号账号需剩余不足30天且管理员已开启功能"
	case errors.Is(err, store.ErrRetentionModeLocked):
		message = store.ErrRetentionModeLocked.Error()
	case errors.Is(err, retention.ErrCooldown):
		message = "距离上次实际选择或更换尚未满20天"
	case errors.Is(err, retention.ErrStale), errors.Is(err, store.ErrConflict):
		message = "确认已过期或账户进度已改变，请重新预览"
	case errors.Is(err, retention.ErrNotReady), errors.Is(err, store.ErrInsufficientPoints):
		message = "积分或可信观影尚未达标，本次没有扣分或续期"
	case errors.Is(err, retention.ErrConsent):
		message = "请选择明确同意自动扣分及费用上限"
	case errors.Is(err, retention.ErrRisk):
		message = "临近到期且目标方式未达标，请明确确认缺口风险"
	}
	fail(w, 409, message)
	return true
}
func (a *App) handleMemberRetention(w http.ResponseWriter, r *http.Request, _ Params) {
	v, err := a.store().RetentionView(current(r).User.UID, time.Now().Unix())
	if retentionError(w, err) {
		return
	}
	ok(w, "OK", v)
}
func (a *App) handleMemberRetentionPreview(w http.ResponseWriter, r *http.Request, _ Params) {
	if !a.allowRate(r.Context(), rateKey("retention-preview:", current(r).User.UID), 10, time.Minute) {
		fail(w, 429, "预览过于频繁")
		return
	}
	token, err := security.RandomHex(12)
	if err != nil {
		fail(w, 503, "暂时无法生成确认")
		return
	}
	q, err := a.store().RetentionPreview(current(r).User.UID, stringValue(decodeMap(r), "mode"), token, time.Now().Unix())
	if retentionError(w, err) {
		return
	}
	ok(w, "请核对保号方式与费用", map[string]any{"token": token, "quote": q.Selection, "purchase": q.Action != "" && q.Action != "rescue", "rescue": q.Action == "rescue"})
}
func (a *App) handleMemberRetentionConfirm(w http.ResponseWriter, r *http.Request, _ Params) {
	b := decodeMap(r)
	risk, _, valid := requireStrictBoolValue(w, b, "acknowledge_risk")
	if !valid {
		return
	}
	consent, _, valid := requireStrictBoolValue(w, b, "authorize_a")
	if !valid {
		return
	}
	cap, valid := shopInteger(w, b, "cost_cap")
	if !valid {
		return
	}
	purchase, _, valid := requireStrictBoolValue(w, b, "confirm_purchase")
	if !valid {
		return
	}
	uid := current(r).User.UID
	if retentionError(w, a.store().RetentionConfirm(uid, stringValue(b, "token"), risk, consent, int(cap), time.Now().Unix(), purchase)) {
		return
	}
	a.audit(r, "retention_mode_confirmed", "retention", uid, nil)
	ok(w, "保号方式已保存，符合条件时自动结算", nil)
}
func (a *App) handleMemberRetentionWithdraw(w http.ResponseWriter, r *http.Request, _ Params) {
	uid := current(r).User.UID
	if retentionError(w, a.store().RetentionWithdraw(uid)) {
		return
	}
	a.audit(r, "retention_authorization_withdrawn", "retention", uid, nil)
	ok(w, "已撤回活动保号参与与未提交的自动扣分授权，切换冷却保留", nil)
}
func (a *App) handleAdminRetention(w http.ResponseWriter, r *http.Request, _ Params) {
	p := a.store().RetentionPolicy()
	if r.Method == http.MethodPut {
		b := decodeMap(r)
		if requireAdmin(w, r) || !requireWebUIIntent(w, r, "retention-admin") || !a.twoFactorPassword(w, r, b) {
			return
		}
		enabled, present, valid := requireStrictBoolValue(w, b, "enabled")
		if !valid {
			return
		}
		if !present {
			fail(w, 400, "请选择功能开关")
			return
		}
		revision, valid := shopInteger(w, b, "revision")
		if !valid {
			return
		}
		if enabled {
			snapshot, e := a.configEditSnapshot()
			if e != nil {
				fail(w, 503, "暂时无法读取签到配置")
				return
			}
			_, code, message := a.patchConfigSections(snapshot.revision, map[string]any{"SAR": map[string]any{"signin_enabled": true, "daily_min": 10, "daily_max": 20, "streak_bonus_enabled": false, "signin_auto_renewal_enabled": false, "signin_renewal_cost": 300, "signin_renewal_days": 30}})
			if code != 200 {
				fail(w, code, message)
				return
			}
		}
		if retentionError(w, a.store().SetRetentionEnabled(enabled, revision)) {
			return
		}
		a.audit(r, "retention_policy_enabled", "retention", 0, map[string]any{"enabled": enabled})
		ok(w, "保号总控已保存", nil)
		return
	}
	items := []store.RetentionView{}
	for _, u := range a.store().ListUsers() {
		if len(items) >= 200 {
			break
		}
		if v, err := a.store().RetentionView(u.UID, time.Now().Unix()); err == nil && v.Member.Member.Participation {
			items = append(items, v)
		}
	}
	ok(w, "OK", map[string]any{"policy": p, "members": items})
}

func (a *App) runRetention(ctx context.Context) (map[string]any, []string, error) {
	a.runShopDeliveries(ctx)
	a.runMediaRequestNotices(ctx)
	if !a.store().RetentionPolicy().Enabled {
		return map[string]any{"success": true, "enabled": false}, nil, nil
	}
	now := time.Now().Unix()
	// An independent server-side sample runs even if activity-log collection
	// or optional reporting plugins are unavailable.
	sessions, err := a.embySessionsSnapshot(ctx, true)
	if err != nil {
		_ = a.store().ProtectSamplingIncident(now)
		return map[string]any{"success": false}, nil, err
	}
	observations := []store.RetentionObservation{}
	for _, s := range sessions {
		u, found := a.store().FindUserByEmbyID(asString(s["UserId"]))
		if !found {
			continue
		}
		item, _ := s["NowPlayingItem"].(map[string]any)
		state, _ := s["PlayState"].(map[string]any)
		kind := strings.ToLower(asString(item["Type"]))
		paused, _ := state["IsPaused"].(bool)
		observations = append(observations, store.RetentionObservation{UID: u.UID, Session: asString(s["Id"]), Item: asString(item["Id"]), Position: int64(numeric(state["PositionTicks"])) / 10000000, Playing: !paused && (kind == "movie" || kind == "episode")})
	}
	if err = a.store().ObserveRetention(observations, now); err != nil {
		return map[string]any{"success": false}, nil, err
	}
	synced, failed := 0, 0
	for _, u := range a.store().ListUsers() {
		v, e := a.store().RetentionView(u.UID, now)
		if e != nil || v.Member.Member.LastSettlement == nil {
			continue
		}
		member := v.Member
		version := member.Member.LastSettlement.NewEnd
		if member.AppliedVersion < version {
			if u.EmbyDisabled && u.EmbyAutoDisabled {
				if e = a.embyApplyEnabledState(ctx, u.UID, u.EmbyID, true); e != nil {
					failed++
					continue
				}
			}
			if e = a.store().RetentionDelivery(u.UID, version, false); e != nil {
				failed++
				continue
			}
			synced++
		}
		if member.NotifiedVersion < version && u.TelegramID > 0 && a.telegramAvailable() {
			text := fmt.Sprintf("见微 Emby · 保号结算成功\n方式：%s\n扣除积分：%d\n播放有效期：%s\n/retention 查看窗口与进度。", member.Member.LastSettlement.Mode, member.Member.LastSettlement.Cost, time.Unix(member.Member.PlaybackEnd, 0).In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02 15:04"))
			if e = a.telegramSendMessage(ctx, u.TelegramID, text); e == nil {
				_ = a.store().RetentionDelivery(u.UID, version, true)
			}
		}
	}
	return map[string]any{"success": failed == 0, "observed": len(observations), "synchronized": synced, "failed": failed}, nil, nil
}

func retentionNativeText(v store.RetentionView) string {
	m := v.Member.Member
	if m.Permanent {
		return "见微 · 签到与保号\n\n无需保号\n\n管理员、白名单或长期有效账户\n无需选择活动保号方式。\n每日签到仍可正常领取积分。"
	}
	if m.ExemptEnd > time.Now().Unix() && !v.Selectable {
		return "见微 · 签到与保号\n\n无需保号\n当前购买权益有效，免活动保号。\n\n临近到期时可选择活动方式或购买续期。\n每日签到仍可正常使用。"
	}
	mode := m.Mode
	if mode == "" {
		mode = "尚未选择"
	}
	status := "未参与"
	if m.Participation {
		status = "已参与"
	}
	end := "尚未开通"
	if m.PlaybackEnd < 0 {
		end = "长期有效"
	} else if m.PlaybackEnd > 0 {
		end = time.Unix(m.PlaybackEnd, 0).In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02 15:04")
	}
	now := time.Now().Unix()
	format := func(value int64) string {
		if value <= 0 {
			return "—"
		}
		return time.Unix(value, 0).In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02 15:04")
	}
	text := fmt.Sprintf("见微 Emby · 保号与签到\n\n· 当前方式  |  %s · %s\n· 播放截止  |  %s\n· 可用积分  |  %d · A需%d\n· 本窗签到  |  %d天\n· 可信观影  |  %d / %d分钟", mode, status, end, v.Balance, v.Policy.Cost, v.SigninDays, v.Seconds/60, v.Policy.WatchSeconds/60)
	if !v.Policy.Enabled {
		text += "\n\n活动保号尚未开启，当前不会自动扣分或按观影续期。"
	}
	if m.Permanent {
		return text + "\n\n受保护或长期有效账户，免活动保号要求。"
	}
	if m.PlaybackEnd > 0 {
		text += "\n· 窗口起止  |  " + format(v.Start) + " → " + format(v.End)
	}
	if m.ExemptEnd > now {
		text += "\n· 免保号至  |  " + format(m.ExemptEnd)
	}
	if v.Start > now {
		text += "\n本窗已结，下一窗尚未开始；不会重复扣款或复用旧观影。"
	}
	if m.HoldUntil > now {
		text += "\n· 事故保护至 | " + format(m.HoldUntil) + "；已暂停活动结算，等待管理员核实补偿。"
	}
	if m.NextSwitchAt > now {
		text += fmt.Sprintf("\n· 可更换于  |  %s（剩余%d天%d小时）", format(m.NextSwitchAt), (m.NextSwitchAt-now)/86400, ((m.NextSwitchAt-now)%86400)/3600)
	} else {
		text += "\n· 方式切换  |  可选择；选择后锁定，仅管理员可修改"
	}
	quality := map[string]string{"disabled": "功能未开启", "waiting": "等待首次服务器采样", "stale": "采样已过期，缺报不会记为有效观影", "server_session_samples": "服务器采样正常"}[v.Quality]
	if quality == "" {
		quality = "等待首次服务器采样"
	}
	text += "\n· 采集状态  |  " + quality + "\n· 最近采样  |  " + format(v.LastPoll)
	if v.AccessUntil > v.End && now >= v.End {
		text += "\n· 救济播放至  |  " + format(v.AccessUntil) + "；补救须单独确认，不静默追扣。"
	}
	text += fmt.Sprintf("\n\nA：明确授权后，每个30天窗口扣%d积分续30天，无签到次数硬门槛。\nB：本窗有效观影满30分钟，免费续30天。\n确认选择后用户不能撤回或更换，只能联系管理员调整。\n签到 /signin · 选择 /retention A 或 /retention B\n需要修改请联系管理员", v.Policy.Cost)
	return text
}
func (a *App) telegramRetention(ctx context.Context, c telegramCommandCtx) {
	u, found := a.store().FindUserByTelegramID(c.FromID)
	if !found || !u.Active {
		_ = a.telegramSendMessage(ctx, c.ChatID, "请先 /start 建立账户")
		return
	}
	handler := a.handleMemberRetention
	body := map[string]any{}
	if c.Command == "/retention_confirm" {
		if len(c.Args) != 1 {
			return
		}
		handler = a.handleMemberRetentionConfirm
		body = map[string]any{"token": c.Args[0], "acknowledge_risk": true, "authorize_a": true, "cost_cap": 300, "confirm_purchase": true}
	} else if len(c.Args) > 0 {
		if c.Args[0] == "off" {
			handler = a.handleMemberRetentionWithdraw
		} else {
			handler = a.handleMemberRetentionPreview
			mode := c.Args[0]
			if mode == "a" || mode == "b" {
				mode = strings.ToUpper(mode)
			}
			body["mode"] = mode
		}
	}
	success, message, data := a.invokeMember(ctx, u, handler, body)
	rows := [][]map[string]any{{memberButton("✦ 每日签到", "/signin"), memberButton("🛍 积分商店", "/points")}, {memberButton("‹ 返回主面板", "/panel")}}
	if success && handlerNamePreview(c.Command, c.Args) {
		d, _ := data.(map[string]any)
		q, _ := d["quote"].(map[string]any)
		token := asString(d["token"])
		message = fmt.Sprintf("选择保号方式 %s\n当前积分 %v · 观影 %v秒\nA授权：每个符合条件的窗口最多扣%v积分，顺延30天，可随时撤回。B不扣积分。\n下一次可更换时间：%s\n确认120秒内有效；选择后不能自行撤回或更换。", q["target_mode"], q["available"], q["watch_seconds"], q["cost"], time.Unix(int64(numeric(q["next_switch_at"])), 0).In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02 15:04"))
		if purchase, _ := d["purchase"].(bool); purchase {
			message = fmt.Sprintf("积分免保号卡 · %s\n本次扣除%v积分，保留当前剩余时长，追加卡券天数并暂停活动自动扣分。\n这是独立的单次交易，不会开启自动购买订阅。确认120秒内有效；选择后不能自行撤回或更换。", q["target_mode"], q["cost"])
		}
		if rescue, _ := d["rescue"].(bool); rescue {
			message = fmt.Sprintf("明确补救确认\n本次需%v积分，已有可信观影%v秒。成功按原截止顺延30天，已使用的救济时间包含在内。A需本次明确授权，不静默追扣。", q["cost"], q["watch_seconds"])
		}
		if risk, _ := q["risk"].(bool); risk {
			message += fmt.Sprintf("\n临近截止，目标尚有缺口：积分%v / 观影%v秒。切换立即生效，原方式不兜底；到期仍不足则进入收尾与救济。", q["points_shortfall"], q["watch_shortfall"])
		}
		rows = append([][]map[string]any{{{"text": "我已了解，确认选择与授权", "callback_data": "jw:/retention_confirm " + token}}}, rows...)
	} else if success && len(c.Args) == 0 && c.Command == "/retention" {
		if v, e := a.store().RetentionView(u.UID, time.Now().Unix()); e == nil {
			message = retentionNativeText(v)
			if v.Policy.Enabled && !v.Member.Member.Permanent && v.Member.Member.PlaybackEnd > 0 {

				if v.Member.Member.Participation && time.Now().Unix() >= v.End && time.Now().Unix() < v.AccessUntil {
					rows = append([][]map[string]any{{{"text": "查看本窗补救确认", "callback_data": "jw:/retention rescue"}}}, rows...)
				}
			}

			if v.Selectable {
				rows = append([][]map[string]any{{{"text": "A · 签到积分", "callback_data": "jw:/retention A"}, {"text": "B · 观影保号", "callback_data": "jw:/retention B"}}}, rows...)
			}
			if false && v.Member.Member.Participation {
				rows = append([][]map[string]any{{{"text": "撤回自动保号参与", "callback_data": "jw:/retention off"}}}, rows...)
			}
		}
	}
	_, _ = telegramPostResult[telegramDiscardResult](a, ctx, "sendMessage", telegramSendMessageRequest{ChatID: c.ChatID, Text: message, ReplyMarkup: map[string]any{"inline_keyboard": rows}}, 20*time.Second)
}
func handlerNamePreview(command string, args []string) bool {
	return command == "/retention" && len(args) > 0 && args[0] != "off"
}
func init() {
	for _, name := range []string{"retention", "retention_confirm"} {
		telegramCommandRegistry["/"+name] = telegramCommandSpec{name: name, label: "/" + name, description: "查看与选择签到或观影保号", usage: "/retention [A|B|off|month|quarter|year|rescue]", category: "user", order: 45, private: true, handler: func(a *App, ctx context.Context, c telegramCommandCtx) { a.telegramRetention(ctx, c) }}
	}
}

func (a *App) handleAdminRetentionMode(w http.ResponseWriter, r *http.Request, p Params) {
	b := decodeMap(r)
	if requireAdmin(w, r) || !requireWebUIIntent(w, r, "retention-admin") || !a.twoFactorPassword(w, r, b) {
		return
	}
	uid, e := int64Param(p, "uid")
	if e != nil {
		fail(w, 400, "无效用户")
		return
	}
	mode := stringValue(b, "mode")
	note := strings.TrimSpace(stringValue(b, "note"))
	if len(note) < 3 || len(note) > 500 {
		fail(w, 400, "请填写调整原因（3–500字）")
		return
	}
	if retentionError(w, a.store().AdminRetentionMode(uid, mode, time.Now().Unix())) {
		return
	}
	a.audit(r, "admin_retention_mode_changed", "retention", uid, map[string]any{"mode": mode, "note": note})
	ok(w, "已调整保号方式", nil)
}
