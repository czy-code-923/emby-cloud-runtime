package api

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"

	"github.com/prejudice-studio/twilight/internal/security"
	"github.com/prejudice-studio/twilight/internal/store"
)

type controlModule struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	URL         string   `json:"url"`
	Sections    []string `json:"sections"`
	Description string   `json:"description"`
}

func controlModules() []controlModule {
	return []controlModule{
		{"overview", "总览", "/admin", []string{"Global"}, "健康、资源和业务待办"},
		{"users_roles", "用户与角色", "/admin/users", []string{"Global", "Emby"}, "身份、权限、账户及单用户配额"},
		{"registration", "注册与邀请", "/admin/invite", []string{"Global", "SAR", "Telegram"}, "人数上限、邀请和已售资格"},
		{"entitlements", "权益与事故补偿", "/admin/finance", []string{"SAR"}, "活动保号、事故保护与预览补偿"},
		{"points_codes", "钱包与积分码", "/admin/finance", []string{"SAR"}, "冻结、释放、调整、不可改写流水和积分商店"},
		{"catalog_promotions", "商品与活动", "/admin/commerce", []string{}, "现金卡、白名单卡及积分卡"},
		{"payments_fulfillment", "收款退款对账", "/admin/finance", []string{}, "人工核款、来源退款、日对账"},
		{"telegram_links", "Telegram", "/admin/telegram", []string{"Telegram"}, "机器人、横图、菜单及Mini App"},
		{"content_clients", "内容与客户端", "/admin/announcements", []string{"Client", "Help"}, "公告、帮助与客户端策略"},
		{"media", "媒体与求片", "/admin/requests", []string{"Global"}, "人工求片；自动下载追新后续独立部署"},
		{"storage_routes", "网盘与线路", "/admin/config", []string{"Emby", "OpenStrm"}, "普通与白名单线路；凭据在专用配置页仅写入"},
		{"devices_sessions", "设备与会话", "/admin/device-audit", []string{"DeviceLimit"}, "登录设备、播放会话、配额和停用"},
		{"abuse_rules", "防滥用策略", "/admin/security", []string{"Security", "RateLimit"}, "鉴权保持强制，准入与频率策略"},
		{"abuse_events", "安全事件", "/admin/violations", []string{}, "审计、异常与设备检查"},
		{"capacity", "资源与容量", "/admin/status", []string{"Global", "DeviceLimit"}, "资源上限、注册和同播容量"},
		{"network_tls", "域名与证书", "/admin/entries", []string{"Emby", "Global"}, "多播放入口、证书与发布回执"},
		{"backup_migration", "备份与迁移", "/admin/migration", []string{"Database"}, "加密备份、预检和迁移；主备切换为外部运维"},
		{"external_points", "外部积分", "/admin/finance", []string{}, "仅管理内账；外部游戏奖励适配器未接入"},
		{"system_releases", "配置发布与审计", "/admin/control", []string{"Global", "API"}, "草稿、影响、版本绑定发布及配置回滚"},
		{"notifications", "通知与投递", "/admin/tickets", []string{"Notification", "Email", "Telegram"}, "通知配置、站内待办与订单状态"},
		{"update_assistant", "程序更新助手", "/admin/updates", []string{}, "正式Release、摘要检查、备份和健康回退"},
		{"jobs_observability", "任务与监控", "/admin/scheduler", []string{"Scheduler"}, "定时任务、执行记录和排行发布"},
		{"privacy_exports", "工单与导出", "/admin/tickets", []string{"Ticket"}, "工单、内部备注、账务和排行导出"},
		{"secrets_adapters", "凭据与适配器", "/admin/config", []string{}, "密钥仅在专用表单写入，不进入配置草稿和导出"},
	}
}
func moduleByID(id string) (controlModule, bool) {
	for _, m := range controlModules() {
		if m.ID == id {
			return m, true
		}
	}
	return controlModule{}, false
}
func moduleSection(m controlModule, key string) bool {
	for _, s := range m.Sections {
		if s == key {
			return true
		}
	}
	return false
}
func consoleFieldAllowed(section string, f configFieldDef) bool {
	if f.Type == "secret" {
		return false
	}
	key := strings.ToLower(f.Key)
	for _, bad := range []string{"admin_uids", "admin_usernames", "repo_url", "databases_dir", "driver", "database_url", "host", "port", "cors_origins", "trusted_proxies", "state_file", "upload_dir"} {
		if key == bad {
			return false
		}
	}
	return true
}
func (a *App) handleControl(w http.ResponseWriter, r *http.Request, _ Params) {
	snapshot, e := a.configEditSnapshot()
	if e != nil {
		fail(w, 503, "配置读取失败")
		return
	}
	values := configValues(snapshot.file)
	sections := []map[string]any{}
	for _, def := range configSectionDefs() {
		fields := []map[string]any{}
		for _, field := range def.Fields {
			if consoleFieldAllowed(def.Key, field) {
				fields = append(fields, map[string]any{"key": field.Key, "label": field.Label, "type": field.Type, "description": field.Description, "value": values[def.Key][field.Key], "options": field.Options})
			}
		}
		sections = append(sections, map[string]any{"key": def.Key, "title": def.Title, "fields": fields})
	}
	w.Header().Set("Cache-Control", "private, no-store")
	ok(w, "OK", map[string]any{"modules": controlModules(), "sections": sections, "revision": snapshot.revision, "drafts": a.store().ConsoleDrafts(), "scope": "配置回滚不回滚订单、核销、钱包和业务数据库"})
}
func (a *App) handleControlPreview(w http.ResponseWriter, r *http.Request, _ Params) {
	if requireAdmin(w, r) || !requireWebUIIntent(w, r, "control-admin") {
		return
	}
	body := decodeMap(r)
	m, okModule := moduleByID(stringValue(body, "module"))
	snapshot, e := a.configEditSnapshot()
	if !okModule || e != nil {
		fail(w, 400, "模块无效")
		return
	}
	if stringValue(body, "expected_revision") != snapshot.revision {
		fail(w, 409, "配置已改变，请刷新")
		return
	}
	sections, okSections := body["sections"].(map[string]any)
	if !okSections || len(sections) == 0 || len(sections) > 12 {
		fail(w, 400, "请选择要修改的设置")
		return
	}
	values := configValues(snapshot.file)
	before := map[string]any{}
	desired := map[string]any{}
	count := 0
	for section, raw := range sections {
		if !moduleSection(m, section) {
			fail(w, 403, "字段不属于当前模块")
			return
		}
		fields, valid := raw.(map[string]any)
		if !valid {
			fail(w, 400, "字段格式无效")
			return
		}
		old := map[string]any{}
		next := map[string]any{}
		for key, value := range fields {
			found := false
			for _, def := range configSectionDefs() {
				if def.Key != section {
					continue
				}
				for _, field := range def.Fields {
					if field.Key == key && consoleFieldAllowed(section, field) {
						normalized := normalizeConfigField(field, value)
						old[key] = values[section][key]
						next[key] = normalized
						values[section][key] = normalized
						found = true
						count++
					}
				}
			}
			if !found {
				fail(w, 403, "不允许修改凭据、管理访问或未知字段")
				return
			}
		}
		before[section] = old
		desired[section] = next
	}
	if count == 0 || count > 200 {
		fail(w, 400, "变更数量无效")
		return
	}
	content, e := mergeConfigTOML(snapshot.content, values)
	if e != nil || validateConfigContent(a.configFilePath(), []byte(content)) != nil {
		fail(w, 400, "配置未通过服务端校验")
		return
	}
	id, e := security.RandomHex(12)
	if e != nil {
		fail(w, 503, "预览暂不可用")
		return
	}
	reason := strings.TrimSpace(stringValue(body, "reason"))
	if reason == "" || len(reason) > 500 {
		fail(w, 400, "请填写变更理由")
		return
	}
	d := store.ConsoleDraft{ID: id, Module: m.ID, Expected: snapshot.revision, Sections: desired, Before: before, Reason: reason, Actor: current(r).User.UID}
	if statusFromError(w, a.store().SaveConsoleDraft(d)) {
		return
	}
	a.audit(r, "control_draft_created", "system", 0, map[string]any{"draft_id": id, "module": m.ID, "fields": count})
	ok(w, "草稿已校验；请核对影响后确认发布", map[string]any{"id": id, "revision": 1, "before": before, "desired": desired, "impact": "影响新操作和准入；既有订单价格、核销事实、余额及原始权益不会回滚。域名/证书仍须专用执行器发布。"})
}
func (a *App) handleControlApply(w http.ResponseWriter, r *http.Request, _ Params) {
	body := decodeMap(r)
	if requireAdmin(w, r) || !requireWebUIIntent(w, r, "control-admin") || !a.twoFactorPassword(w, r, body) {
		return
	}
	d, found := a.store().ConsoleDraft(stringValue(body, "id"))
	if !found {
		fail(w, 404, "草稿不存在")
		return
	}
	rev, valid := shopInteger(w, body, "revision")
	if !valid {
		return
	}
	action := stringValue(body, "action")
	if action == "discard" {
		if statusFromError(w, a.store().TransitionConsoleDraft(d.ID, rev, "draft", "discarded", "", "")) {
			return
		}
		ok(w, "草稿已撤回", nil)
		return
	}
	snapshot, e := a.configEditSnapshot()
	if e != nil {
		fail(w, 503, "配置不可用")
		return
	}
	expected := d.Expected
	sections := d.Sections
	from := "draft"
	to := "applied"
	if action == "rollback" {
		expected = d.Applied
		sections = d.Before
		from = "applied"
		to = "rolled_back"
	} else if action != "publish" {
		fail(w, 400, "操作无效")
		return
	}
	if snapshot.revision != expected {
		fail(w, 409, "当前配置与预览版本不符，不能覆盖后续修改")
		return
	}
	if statusFromError(w, a.store().TransitionConsoleDraft(d.ID, rev, from, "applying", "", "")) {
		return
	}
	_, status, message := a.patchConfigSections(expected, sections)
	next, e := a.configEditSnapshot()
	applied := ""
	if e == nil {
		applied = next.revision
	}
	if status != 200 {
		to = "failed"
	}
	effective := true
	if status == 200 {
		actual := configValues(next.effective)
		for section, raw := range sections {
			fields, _ := raw.(map[string]any)
			for key, value := range fields {
				left, _ := json.Marshal(value)
				right, _ := json.Marshal(actual[section][key])
				if !reflect.DeepEqual(left, right) {
					effective = false
				}
			}
		}
		if !effective {
			to = "overridden"
		}
	}
	if err := a.store().TransitionConsoleDraft(d.ID, rev+1, "applying", to, applied, message); err != nil {
		fail(w, 503, "配置已提交但回执记录待恢复，请先核对当前配置，勿重复发布")
		return
	}
	a.audit(r, "control_"+action, "system", 0, map[string]any{"id": d.ID, "module": d.Module, "status": to})
	if status != 200 {
		fail(w, status, message)
		return
	}
	ok(w, "配置操作完成，请查看应用状态", map[string]any{"status": to, "revision": applied, "effective": effective})
}
