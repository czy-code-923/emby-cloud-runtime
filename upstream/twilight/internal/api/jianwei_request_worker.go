package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prejudice-studio/twilight/internal/security"
	"github.com/prejudice-studio/twilight/internal/store"
)

const apiKeyPermissionMediaSync = "media:sync"

func mediaRequestTrackingDTO(tracking store.MediaRequestTracking) map[string]any {
	events := make([]map[string]any, 0, len(tracking.Events))
	for _, e := range tracking.Events {
		events = append(events, map[string]any{"stage": e.Stage, "progress": e.Progress, "note": e.Note, "at": e.At})
	}
	return map[string]any{"stage": tracking.Stage, "progress": tracking.Progress, "events": events}
}

func (a *App) mediaWorkerAuthorized(w http.ResponseWriter, r *http.Request) bool {
	if requireAdmin(w, r) {
		return false
	}
	if a.requireMediaRequestEnabled(w) {
		return false
	}
	// A normal API key, a legacy implicit full key or a browser session cannot
	// acquire this scope. The active owner must also still be an administrator.
	if current(r).APIKey.ID <= 0 {
		fail(w, 403, "请使用仅授予media:sync的工作器API Key")
		return false
	}
	return true
}

func (a *App) handleMediaWorkerQueue(w http.ResponseWriter, r *http.Request, _ Params) {
	if !a.mediaWorkerAuthorized(w, r) {
		return
	}
	result := a.store().ListMediaRequestsPageWithOptions(store.MediaRequestListOptions{All: true, StatusFilter: "active", Page: clamp(queryInt(r, "page", 1), 1, 1000), PerPage: 20})
	items := []map[string]any{}
	for _, request := range result.Requests {
		if request.Status == store.MediaRequestStatusUnhandled {
			continue
		}
		items = append(items, map[string]any{"require_key": request.RequireKey, "revision": request.Revision, "title": request.Title, "source": request.Source, "media_id": request.MediaID, "media_type": request.MediaType, "season": request.Season, "year": request.Year, "note": request.Note, "metadata_confirmed": request.MediaID < 1_000_000_000_000 && !boolish(request.MediaInfo["manual_title_only"]), "tracking": request.Tracking})
	}
	_ = a.store().TouchMediaWorker(time.Now().Unix())
	ok(w, "OK", map[string]any{"protocol": 1, "items": items, "page": result.Page, "has_next": result.HasNext})
}

func (a *App) handleMediaWorkerEvent(w http.ResponseWriter, r *http.Request, p Params) {
	if !a.mediaWorkerAuthorized(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	body := decodeMap(r)
	raw, _ := json.Marshal(body)
	var event store.MediaRequestWorkerUpdate
	if json.Unmarshal(raw, &event) != nil {
		fail(w, 400, "状态事件格式不正确")
		return
	}
	revision, valid := shopInteger(w, body, "revision")
	if !valid {
		return
	}
	key := p["require_key"]
	request, exists := a.store().FindMediaRequestByKey(key)
	if !exists {
		fail(w, 404, "求片不存在")
		return
	}
	replayed := false
	for _, previous := range request.Tracking.Events {
		if previous.ID == event.EventID && previous.Digest == store.MediaRequestWorkerDigest(event) {
			replayed = true
		}
	}
	if event.Stage == "completed" && !replayed {
		if !memberItemID.MatchString(event.MediaItemID) {
			fail(w, 400, "完成事件必须提供已入库的Emby媒体ID")
			return
		}
		var item map[string]any
		if a.embyGet(r.Context(), "/Items/"+urlPathEscape(event.MediaItemID), &item) != nil || asString(item["Id"]) != event.MediaItemID {
			fail(w, 409, "尚未在Emby核实到入库媒体，不能标记完成")
			return
		}
		kind := asString(item["Type"])
		if kind != "Movie" && kind != "Series" && kind != "Episode" {
			fail(w, 409, "入库对象不是电影或剧集")
			return
		}
		if request.MediaID < 1_000_000_000_000 && !boolish(request.MediaInfo["manual_title_only"]) {
			providers, _ := item["ProviderIds"].(map[string]any)
			matched := false
			for name, id := range providers {
				if strings.EqualFold(name, request.Source) && asString(id) == strconv.FormatInt(request.MediaID, 10) {
					matched = true
				}
			}
			if !matched {
				fail(w, 409, "入库媒体资料源编号与本次求片不匹配")
				return
			}
		}
	}
	updated, err := a.store().UpdateMediaRequestFromWorker(key, event, revision)
	if failMediaRequestMutation(w, err) {
		return
	}
	_ = a.store().TouchMediaWorker(time.Now().Unix())
	a.audit(r, "media_worker_event", "system", updated.UID, map[string]any{"request_id": updated.ID, "stage": event.Stage, "sequence": event.Sequence})
	writeMediaRequestETag(w, updated.Revision)
	ok(w, "进度已同步", map[string]any{"require_key": updated.RequireKey, "revision": updated.Revision, "status": updated.Status, "tracking": mediaRequestTrackingDTO(updated.Tracking)})
}

func (a *App) handleAdminMediaWorker(w http.ResponseWriter, r *http.Request, _ Params) {
	last := a.store().MediaWorkerHealth()
	ok(w, "OK", map[string]any{"protocol": 1, "last_seen": last, "connected": last > 0 && time.Now().Unix()-last < 120, "enabled": a.cfg().MediaRequestEnabled, "scope": apiKeyPermissionMediaSync, "queue_path": "/api/v2/integrations/media-requests", "event_path": "/api/v2/integrations/media-requests/{require_key}/events", "public": false})
}

func (a *App) handleAdminMediaWorkerKey(w http.ResponseWriter, r *http.Request, _ Params) {
	body := decodeMap(r)
	if requireAdmin(w, r) || !requireWebUIIntent(w, r, "media-worker-key") || !a.twoFactorPassword(w, r, body) {
		return
	}
	value, err := security.RandomHex(32)
	if err != nil {
		fail(w, 503, "暂时无法创建工作器密钥")
		return
	}
	plain := "jw-media-" + value
	prefix, suffix, _ := maskAPIKey(plain)
	key, err := a.store().CreateAPIKey(store.APIKey{UID: current(r).User.UID, Name: "媒体状态工作器", Hash: hashAPIKey(plain), Prefix: prefix, Suffix: suffix, AllowQuery: false, RateLimit: 60, Permissions: []string{apiKeyPermissionMediaSync}, ExpiredAt: time.Now().Add(90 * 24 * time.Hour).Unix()})
	if statusFromError(w, err) {
		return
	}
	a.audit(r, "media_worker_key_created", "security", key.UID, map[string]any{"key_id": key.ID})
	ok(w, "此密钥只显示一次，请安全保存；仅允许求片队列与进度接口，90天后过期", publicAPIKey(key, plain))
}

func (a *App) runMediaRequestNotices(ctx context.Context) {
	if !a.telegramAvailable() {
		return
	}
	for i := 0; i < 5; i++ {
		request, tg, found, err := a.store().ClaimMediaRequestNotice()
		if err != nil || !found {
			return
		}
		message := fmt.Sprintf("见微 · 求片更新\n\n%s\n状态：%s", request.Title, store.MediaRequestStatusText(request.Status))
		if request.AdminNote != "" {
			message += "\n" + truncateString(request.AdminNote, 250)
		}
		rows := [][]map[string]any{{memberButton("查看我的求片", "/request")}}
		if link := a.jianweiMiniURL(); link != "" {
			rows = append(rows, []map[string]any{{"text": "在Mini App查看进度", "web_app": map[string]string{"url": link}}})
		}
		_, err = telegramPostResult[telegramDiscardResult](a, ctx, "sendMessage", telegramSendMessageRequest{ChatID: tg, Text: message, ReplyMarkup: map[string]any{"inline_keyboard": rows}}, 15*time.Second)
		if err != nil {
			return
		}
		_ = a.store().MarkMediaRequestNotice(request.ID, request.Revision)
	}
}

func (a *App) handleMemberRequestSearch(w http.ResponseWriter, r *http.Request, p Params) {
	if a.requireMediaRequestEnabled(w) {
		return
	}
	if !a.allowRate(r.Context(), rateKey("member-request-search:", current(r).User.UID), 15, time.Minute) {
		fail(w, 429, "搜索过于频繁，请稍后再试")
		return
	}
	if len(strings.TrimSpace(r.URL.Query().Get("q"))) < 2 {
		fail(w, 400, "请输入至少两个字符搜索资料")
		return
	}
	query := r.URL.Query()
	query.Set("limit", "12")
	r.URL.RawQuery = query.Encode()
	a.handleV2MediaSearch(w, r, p)
}
