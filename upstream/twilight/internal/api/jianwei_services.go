package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/prejudice-studio/twilight/internal/security"
	"github.com/prejudice-studio/twilight/internal/store"
)

func init() {
	telegramCommandRegistry["/services"] = telegramCommandSpec{name: "services", label: "/services", description: "音乐、小说与专属影视权益", usage: "/services", category: "user", order: 48, private: true, handler: func(a *App, ctx context.Context, c telegramCommandCtx) {
		u, ok := a.store().FindUserByTelegramID(c.FromID)
		if !ok || !u.Active {
			return
		}
		u = effectiveMember(u)
		grants := a.store().ServiceGrants(u.UID)
		text := "见微 · 分区服务\n\n统一Emby账号，按分区核验权限。\n"
		count := 0
		for _, v := range a.store().Services().Items {
			if !v.Enabled {
				continue
			}
			count++
			text += "\n" + v.Name
			if store.ServiceAccess(u, v, grants[v.ID], time.Now().Unix()) {
				text += " · 可用\n"
				if grants[v.ID] > 0 && u.Role == store.RoleNormal {
					text += time.Unix(grants[v.ID], 0).In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02") + " 到期\n"
				}
				for _, line := range v.Lines {
					text += line + "\n"
				}
			} else {
				text += " · 需本服务注册码/续期码或积分权益\n"
			}
		}
		if count == 0 {
			text += "\n扩展服务尚未开放，现有影视服务不变。"
		}
		a.memberSendButtons(ctx, c.ChatID, text, [][]map[string]any{{memberButton("使用服务卡码", "/redeem"), memberButton("积分商店", "/points")}, {memberButton("账号与播放密码", "/lines")}})
	}}
}

func (a *App) handleAdminServices(w http.ResponseWriter, r *http.Request, _ Params) {
	if requireAdmin(w, r) {
		return
	}
	if r.Method == "PUT" {
		if !requireWebUIIntent(w, r, "member-services") {
			return
		}
		var body struct {
			Revision int64                 `json:"revision"`
			Items    []store.MemberService `json:"items"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body) != nil {
			fail(w, 400, "服务配置无效")
			return
		}
		// Reserve different domains; reusing the ordinary host would either leak a
		// protected service or lock ordinary movie clients out of their existing line.
		known := map[string]bool{}
		for _, raw := range append(append([]string{a.cfg().EmbyPublicURL, a.cfg().EmbyWhitelistURL}, lineURLs(a.cfg().EmbyURLList)...), lineURLs(a.cfg().EmbyWhitelistURLList)...) {
			v, _ := url.Parse(raw)
			if v != nil {
				known[normalizedPlaybackHost(v)] = true
			}
		}
		var folders []map[string]any
		needsLibraries := false
		for _, v := range body.Items {
			needsLibraries = needsLibraries || len(v.LibraryIDs) > 0
		}
		if needsLibraries {
			ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
			defer cancel()
			if a.embyGet(ctx, "/Library/VirtualFolders", &folders) != nil {
				fail(w, 503, "请先确认Emby可读取媒体库，再保存库权限")
				return
			}
		}
		valid := map[string]bool{}
		for _, f := range folders {
			valid[asString(f["ItemId"])] = true
		}
		for _, v := range body.Items {
			for _, id := range v.LibraryIDs {
				if !valid[id] {
					fail(w, 400, "媒体库已变化，请刷新后选择真实媒体库")
					return
				}
			}
			for _, raw := range v.Lines {
				u, _ := url.Parse(raw)
				if u != nil && known[normalizedPlaybackHost(u)] {
					fail(w, 400, "独立服务请使用单独域名，保留现有普通和白名单线路")
					return
				}
			}
		}
		if statusFromError(w, a.store().SaveServices(body.Items, body.Revision)) {
			return
		}
		a.audit(r, "member_services_saved", "emby", 0, nil)
		pending := 0
		for _, u := range a.store().ListUsers() {
			if u.EmbyID != "" {
				ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
				e := a.applyMediaStreamLimit(ctx, u)
				cancel()
				if e != nil {
					pending++
				}
			}
		}
		ok(w, "服务配置已保存；启用后的独立域名请到站点与入口申请证书并发布", map[string]any{"policy_sync_pending": pending})
		return
	}
	cfg := a.store().Services()
	var folders []map[string]any
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	e := a.embyGet(ctx, "/Library/VirtualFolders", &folders)
	libraries := []map[string]string{}
	for _, f := range folders {
		if id := asString(f["ItemId"]); id != "" {
			libraries = append(libraries, map[string]string{"id": id, "name": asString(f["Name"])})
		}
	}
	ok(w, "OK", map[string]any{"revision": cfg.Revision, "items": cfg.Items, "libraries": libraries, "libraries_available": e == nil, "emby_backend": "current"})
}
func (a *App) handleServiceCards(w http.ResponseWriter, r *http.Request, _ Params) {
	if requireAdmin(w, r) || !requireWebUIIntent(w, r, "member-services") {
		return
	}
	b := decodeMap(r)
	id := stringValue(b, "service_id")
	kind := int(intValue(b, "type", 0))
	days := int(intValue(b, "days", 0))
	count := int(intValue(b, "count", 1))
	var service store.MemberService
	for _, v := range a.store().Services().Items {
		if v.ID == id {
			service = v
		}
	}
	if !service.Enabled || (kind != 1 && kind != 2) || kind == 1 && !service.RegisterEnabled || kind == 2 && !service.RenewEnabled || days < 1 || days > 3650 || count < 1 || count > 20 {
		fail(w, 400, "请启用服务及对应注册/续期入口，天数1至3650，数量1至20")
		return
	}
	if !a.allowRate(r.Context(), rateKey("service-card-admin:", current(r).User.UID), 5, time.Minute) {
		fail(w, 429, "请稍后再生成")
		return
	}
	codes := []string{}
	now := time.Now().Unix()
	for i := 0; i < count; i++ {
		random, e := security.RandomHex(10)
		if e != nil {
			fail(w, 503, "暂不可生成")
			return
		}
		code := "JW-S-" + strings.ToUpper(random)
		e = a.store().UpsertRegCode(store.RegCode{Code: code, ServiceID: id, Type: kind, Days: days, UseCountLimit: 1, Active: true, CreatedAt: now, CreatedTime: now, ValidityTime: 30 * 24, Source: "admin", CreatorUID: current(r).User.UID, Note: service.Name + "专属权益"})
		if statusFromError(w, e) {
			return
		}
		codes = append(codes, code)
	}
	a.audit(r, "service_cards_created", "regcode", 0, map[string]any{"service_id": id, "type": kind, "count": count})
	ok(w, "专属单次码已生成，30天内兑换", map[string]any{"codes": codes})
}
func (a *App) handleServiceCode(w http.ResponseWriter, r *http.Request, u store.User, code store.RegCode, b map[string]any) {
	_, _, valid := a.previewCode(r.Context(), code.Code, u)
	if !valid {
		fail(w, 400, "服务卡码无效、已使用或不属于当前用户")
		return
	}
	var service store.MemberService
	for _, v := range a.store().Services().Items {
		if v.ID == code.ServiceID {
			service = v
		}
	}
	if !service.Enabled || u.EmbyID == "" {
		fail(w, 409, "请先开通统一Emby账号，且服务需要已启用；未核销此码")
		return
	}
	if boolValue(b, "check_only", false) {
		data := codePreview("regcode", code.Type, code.Days, "")
		data["service_id"] = code.ServiceID
		data["description"] = service.Name + "独立权益，不改变影视会员和白名单身份"
		data["requires_emby_credentials"] = false
		ok(w, "服务卡码有效", data)
		return
	}
	updated, e := a.store().ConsumeServiceCode(u.UID, u.TelegramID, code.Code, time.Now().Unix())
	if statusFromError(w, e) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	pending := a.applyMediaStreamLimit(ctx, updated) != nil
	if updated.EmbyAutoDisabled {
		if a.embyApplyEnabledState(ctx, updated.UID, updated.EmbyID, true) != nil {
			pending = true
		}
	}
	a.audit(r, "service_code_redeemed", "regcode", u.UID, map[string]any{"service_id": code.ServiceID})
	message := service.Name + "权益已生效"
	if pending {
		message += "，播放策略同步中，无需重复兑换"
	}
	ok(w, message, map[string]any{"service_id": code.ServiceID, "service_until": a.store().ServiceGrants(u.UID)[code.ServiceID], "media_sync_pending": pending})
}
func (a *App) handleMemberServices(w http.ResponseWriter, r *http.Request, _ Params) {
	u := effectiveMember(current(r).User)
	grants := a.store().ServiceGrants(u.UID)
	items := []map[string]any{}
	now := time.Now().Unix()
	for _, v := range a.store().Services().Items {
		if !v.Enabled {
			continue
		}
		access := store.ServiceAccess(u, v, grants[v.ID], now)
		lines := []string{}
		if access {
			lines = v.Lines
		}
		items = append(items, map[string]any{"id": v.ID, "name": v.Name, "access": access, "until": grants[v.ID], "included": u.Role == store.RoleAdmin || u.Role == store.RoleWhitelist, "lines": lines, "points_enabled": v.PointsEnabled})
	}
	ok(w, "OK", map[string]any{"items": items})
}
func (a *App) servicePolicyStamp(u store.User) string {
	cfg := a.store().Services()
	ids := []string{}
	grants := a.store().ServiceGrants(u.UID)
	for _, v := range cfg.Items {
		if store.ServiceAccess(u, v, grants[v.ID], time.Now().Unix()) {
			ids = append(ids, v.ID)
		}
	}
	sort.Strings(ids)
	return fmt.Sprintf("%d:%d:%s", cfg.Revision, u.Role, strings.Join(ids, ","))
}
func (a *App) applyServiceLibraryPolicy(ctx context.Context, u store.User, policy map[string]any) error {
	cfg := a.store().Services()
	restricted := map[string]bool{}
	allowed := map[string]bool{}
	grants := a.store().ServiceGrants(u.UID)
	now := time.Now().Unix()
	for _, v := range cfg.Items {
		for _, id := range v.LibraryIDs {
			restricted[id] = true
			if store.ServiceAccess(u, v, grants[v.ID], now) {
				allowed[id] = true
			}
		}
	}
	baseline, managed := a.store().ServiceBaseline(u.UID)
	if len(restricted) == 0 && !managed {
		return nil
	}
	// Preserve existing manual folder restrictions before managing subservices.
	// Removing the service configuration restores that baseline, never a wider
	// all-libraries permission. The native owner is never bound to this path.
	if !managed {
		ids := []string{}
		switch existing := policy["EnabledFolders"].(type) {
		case []string:
			ids = append(ids, existing...)
		case []any:
			for _, id := range existing {
				if text, ok := id.(string); ok {
					ids = append(ids, text)
				}
			}
		}
		if e := a.store().RememberServiceBaseline(u.UID, boolish(policy["EnableAllFolders"]), ids); e != nil {
			return e
		}
		baseline, _ = a.store().ServiceBaseline(u.UID)
	}
	baseIDs := map[string]bool{}
	for _, id := range baseline.IDs {
		baseIDs[id] = true
	}
	var folders []map[string]any
	if e := a.embyGet(ctx, "/Library/VirtualFolders", &folders); e != nil {
		return e
	}
	baseAccess := !embyAccessExpired(u)
	ids := []string{}
	for _, f := range folders {
		id := asString(f["ItemId"])
		if id != "" && ((baseAccess && !restricted[id] && (baseline.All || baseIDs[id])) || allowed[id]) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	policy["EnableAllFolders"] = len(restricted) == 0 && baseline.All && baseAccess
	policy["EnabledFolders"] = ids
	policy["EnableAllChannels"] = false
	policy["EnabledChannels"] = []string{}
	return nil
}
func (a *App) serviceHostAccess(u store.User, host string) (bool, bool) {
	grants := a.store().ServiceGrants(u.UID)
	for _, v := range a.store().Services().Items {
		for _, raw := range v.Lines {
			line, e := url.Parse(raw)
			if e == nil && strings.EqualFold(normalizedPlaybackHost(line), host) {
				return true, store.ServiceAccess(u, v, grants[v.ID], time.Now().Unix())
			}
		}
	}
	return false, false
}

func (a *App) memberShouldDisableForWebState(u store.User) bool {
	return !a.embyShouldEnableUser(effectiveMember(u))
}
