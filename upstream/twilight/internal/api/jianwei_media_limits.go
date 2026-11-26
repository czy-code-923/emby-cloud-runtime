package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/prejudice-studio/twilight/internal/config"
	"github.com/prejudice-studio/twilight/internal/store"
)

func mediaLimits(cfg *config.Config, u store.User) (devices, streams int) {
	u = effectiveMember(u)
	if !cfg.MediaLimitsEnabled {
		return 0, 0
	}
	devices, streams = cfg.MediaNormalDevices, cfg.MediaNormalStreams
	if u.Role == store.RoleAdmin {
		devices, streams = cfg.MediaAdminDevices, cfg.MediaAdminStreams
	}
	if u.Role == store.RoleWhitelist {
		devices, streams = cfg.MediaWhitelistDevices, cfg.MediaWhitelistStreams
	}
	if u.MediaMaxDevices != nil {
		devices = *u.MediaMaxDevices
	}
	if u.MediaMaxStreams != nil {
		streams = *u.MediaMaxStreams
	}
	return clamp(devices, 0, 100), clamp(streams, 0, 100)
}

func (a *App) applyMediaStreamLimit(ctx context.Context, u store.User) error {
	if u.EmbyID == "" {
		return nil
	}
	_, streams := mediaLimits(a.cfg(), u)
	remote, found, err := a.embyUserByID(ctx, u.EmbyID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("Emby user not found")
	}
	policy, ok := remote["Policy"].(map[string]any)
	if !ok {
		return fmt.Errorf("Emby policy missing")
	}
	// Preserve unrelated remote policies, including native administrator privileges.
	policy["SimultaneousStreamLimit"] = streams
	managed := a.cfg().MediaLimitsEnabled
	if managed && !u.MediaPolicyManaged {
		if _, err = a.store().UpdateUser(u.UID, func(next *store.User) error { next.MediaPolicyManaged = true; return nil }); err != nil {
			return err
		}
	}
	var ignored map[string]any
	if err = a.embyPost(ctx, "/Users/"+urlPathEscape(u.EmbyID)+"/Policy", policy, &ignored); err != nil {
		return err
	}
	if !managed && u.MediaPolicyManaged {
		_, err = a.store().UpdateUser(u.UID, func(next *store.User) error { next.MediaPolicyManaged = false; return nil })
	}
	return err
}

// Cache only the verified identity, never a plaintext Emby token. Account
// activity, expiry and current device enrollment are checked on every request.
type mediaIdentityCache struct {
	mu    sync.Mutex
	items map[[32]byte]mediaIdentityEntry
}
type mediaIdentityEntry struct {
	id          string
	device      string
	until       time.Time
	policyStamp string
}

func mediaAuthFields(r *http.Request, uri *url.URL) (token, device, name string) {
	token = firstNonEmpty(r.Header.Get("X-Emby-Token"), r.Header.Get("X-MediaBrowser-Token"), uri.Query().Get("api_key"), uri.Query().Get("X-Emby-Token"))
	device = firstNonEmpty(r.Header.Get("X-Emby-Device-Id"), uri.Query().Get("DeviceId"), uri.Query().Get("deviceId"), uri.Query().Get("X-Emby-Device-Id"))
	for _, header := range []string{r.Header.Get("X-Emby-Authorization"), r.Header.Get("Authorization")} {
		header = strings.TrimSpace(header)
		if strings.HasPrefix(strings.ToLower(header), "bearer ") {
			if token == "" {
				token = strings.TrimSpace(header[7:])
			}
			continue
		}
		for _, prefix := range []string{"mediabrowser ", "emby "} {
			if strings.HasPrefix(strings.ToLower(header), prefix) {
				header = header[len(prefix):]
				break
			}
		}
		for _, field := range strings.Split(header, ",") {
			key, value, found := strings.Cut(strings.TrimSpace(field), "=")
			if !found {
				continue
			}
			value = strings.Trim(value, "\"")
			switch strings.ToLower(key) {
			case "token":
				if token == "" {
					token = value
				}
			case "deviceid":
				if device == "" {
					device = value
				}
			case "device":
				name = value
			}
		}
	}
	return
}

func (a *App) handleMediaGatewayAccess(w http.ResponseWriter, r *http.Request, _ Params) {
	w.Header().Set("Cache-Control", "no-store")
	secret := a.cfg().BotInternalSecret
	if len(secret) < 16 || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-JW-Gateway")), []byte(secret)) != 1 {
		fail(w, 403, "入口不可用")
		return
	}
	uri, err := url.ParseRequestURI(r.Header.Get("X-JW-URI"))
	if err != nil || uri.IsAbs() {
		fail(w, 400, "无效请求")
		return
	}
	path := strings.ToLower(uri.Path)
	if strings.HasPrefix(path, "/emby/") {
		path = strings.TrimPrefix(path, "/emby")
	}
	if a.cfg().BlockEmbyOfficialClients {
		client := r.Header.Get("X-Emby-Client")
		for _, header := range []string{r.Header.Get("Authorization"), r.Header.Get("X-Emby-Authorization")} {
			for _, part := range strings.Split(header, ",") {
				key, value, found := strings.Cut(strings.TrimSpace(part), "=")
				key = strings.TrimPrefix(strings.ToLower(key), "mediabrowser ")
				key = strings.TrimPrefix(key, "emby ")
				if found && key == "client" {
					client = strings.Trim(value, "\"")
				}
			}
		}
		if strings.HasPrefix(strings.ToLower(client), "emby") {
			fail(w, 403, "请使用支持见微线路的第三方客户端")
			return
		}
	}
	// Anonymous clients need server discovery and password authentication only.
	if path == "/system/info/public" || path == "/users/authenticatebyname" {
		method := r.Header.Get("X-JW-Method")
		if (path == "/system/info/public" && method != http.MethodGet) || (path == "/users/authenticatebyname" && method != http.MethodPost) {
			fail(w, 405, "请求方法不支持")
			return
		}
		budget := 30
		if path == "/users/authenticatebyname" {
			budget = 10
		}
		if !a.allowRate(r.Context(), rateKey("media-entry:", r.Header.Get("X-JW-Client-IP"), path), budget, time.Minute) {
			fail(w, 429, "请求过于频繁")
			return
		}
		w.WriteHeader(204)
		return
	}
	token, device, name := mediaAuthFields(r, uri)
	if token == "" || len(token) > 256 || len(device) > 200 {
		fail(w, 401, "请在 Emby 客户端登录")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	key := sha256.Sum256([]byte(a.cfg().EmbyURL + "\x00" + token))
	cache := &a.mediaIdentity
	cache.mu.Lock()
	entry := cache.items[key]
	cache.mu.Unlock()
	if time.Now().After(entry.until) {
		endpoint, err := a.validatedEmbyEndpoint("/Users/Me")
		if err != nil {
			fail(w, 503, "媒体服务暂不可用")
			return
		}
		var remote map[string]any
		if err = getJSON(ctx, endpoint, map[string]string{"X-Emby-Token": token}, &remote); err != nil {
			fail(w, 401, "请重新登录 Emby")
			return
		}
		if embyRemoteIsAdministrator(remote) {
			fail(w, 403, "Emby 服务器管理员请使用安全入口；播放请使用普通媒体账户")
			return
		}
		entry = mediaIdentityEntry{id: asString(remote["Id"]), until: time.Now().Add(15 * time.Second), policyStamp: entry.policyStamp, device: entry.device}
		if entry.id == "" {
			fail(w, 401, "无法验证媒体账户")
			return
		}
		cache.mu.Lock()
		if cache.items == nil || len(cache.items) >= 2048 {
			cache.items = map[[32]byte]mediaIdentityEntry{}
		}
		cache.items[key] = entry
		cache.mu.Unlock()
	}
	u, found := a.store().FindUserByEmbyID(entry.id)
	u = effectiveMember(u)
	if !found || !u.Active || u.EmbyDisabled || !a.embyShouldEnableUser(u) {
		fail(w, 403, "媒体账户未开通或已停用")
		return
	}
	// Image/subtitle requests from native clients often carry only api_key.
	// Reuse only a device already admitted with this verified token; a fresh
	// token without device enrollment still cannot claim a device slot.
	if device == "" {
		device = entry.device
	}
	host := normalizedPlaybackHost(&url.URL{Scheme: "https", Host: r.Header.Get("X-JW-Host")})
	if host != "" {
		allowed := false
		for _, raw := range append([]string{a.cfg().EmbyPublicURL}, lineURLs(a.cfg().EmbyURLList)...) {
			v, e := url.Parse(raw)
			if e == nil && strings.EqualFold(normalizedPlaybackHost(v), host) {
				allowed = true
			}
		}
		if u.Role == store.RoleWhitelist || u.Role == store.RoleAdmin {
			for _, raw := range append([]string{a.cfg().EmbyWhitelistURL}, lineURLs(a.cfg().EmbyWhitelistURLList)...) {
				v, e := url.Parse(raw)
				if e == nil && strings.EqualFold(normalizedPlaybackHost(v), host) {
					allowed = true
				}
			}
		}
		if !allowed {
			fail(w, 403, "当前账号无权使用此线路")
			return
		}
	}
	devices, streams := mediaLimits(a.cfg(), u)
	stamp := fmt.Sprintf("%d:%d:%v", u.UID, streams, a.cfg().MediaLimitsEnabled)
	if (a.cfg().MediaLimitsEnabled || u.MediaPolicyManaged) && entry.policyStamp != stamp {
		if err := a.applyMediaStreamLimit(ctx, u); err != nil {
			fail(w, 503, "播放策略正在同步，请稍后重试")
			return
		}
		entry.policyStamp = stamp
		cache.mu.Lock()
		if cache.items == nil {
			cache.items = map[[32]byte]mediaIdentityEntry{}
		}
		cache.items[key] = entry
		cache.mu.Unlock()
	}
	for _, revoked := range u.MediaRevokedDevices {
		if device == revoked {
			fail(w, 403, "此设备已被停用")
			return
		}
	}
	if devices > 0 && device == "" {
		fail(w, 400, "客户端未提供设备标识，请升级客户端")
		return
	}
	if device != "" {
		client, version := mediaClientSoftware(r, uri)
		err = a.store().ClaimMediaDevice(u.UID, device, name, func(user store.User) int { n, _ := mediaLimits(a.cfg(), user); return n }, client, version)
		if err != nil {
			fail(w, 403, "已达到设备上限或此设备已停用，请在见微面板核对设备")
			return
		}
	}

	if device != "" && entry.device != device {
		entry.device = device
		cache.mu.Lock()
		cache.items[key] = entry
		cache.mu.Unlock()
	}
	w.WriteHeader(204)
}

func (a *App) mediaGatewayClientIP(r *http.Request) string {
	if r.Method != http.MethodGet || r.URL.Path != "/api/v2/internal/emby-access" {
		return ""
	}
	secret := a.cfg().BotInternalSecret
	if len(secret) < 16 || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-JW-Gateway")), []byte(secret)) != 1 {
		return ""
	}
	ip := net.ParseIP(r.Header.Get("X-JW-Client-IP"))
	if ip == nil {
		return ""
	}
	return ip.String()
}

func (a *App) handleMemberDeviceRegistry(w http.ResponseWriter, r *http.Request, _ Params) {
	u := current(r).User
	devices, streams := mediaLimits(a.cfg(), u)
	items := u.MediaDevices
	if items == nil {
		items = []store.MediaDevice{}
	}
	playing := 0
	online := []map[string]any{}
	sessions, e := a.embySessionsSnapshot(r.Context(), false)
	if e == nil {
		for _, session := range sessions {
			if u.EmbyID == "" || asString(session["UserId"]) != u.EmbyID {
				continue
			}
			_, watching := embySessionNowPlaying(session)
			state, _ := session["PlayState"].(map[string]any)
			paused, _ := state["IsPaused"].(bool)
			if watching && !paused {
				playing++
			}
			online = append(online, map[string]any{"device_id": asString(session["DeviceId"]), "name": asString(session["DeviceName"]), "client": asString(session["Client"]), "client_version": asString(session["ApplicationVersion"]), "playing": watching && !paused})
		}
	}
	ok(w, "OK", map[string]any{"items": items, "online": online, "playing_streams": playing, "sessions_available": e == nil, "used_devices": len(items), "max_devices": devices, "max_streams": streams, "enabled": a.cfg().MediaLimitsEnabled})
}

func (a *App) handleMemberDeviceSync(w http.ResponseWriter, r *http.Request, _ Params) {
	u := current(r).User
	if u.EmbyID == "" {
		fail(w, 409, "请先开通Emby")
		return
	}
	if !a.allowRate(r.Context(), rateKey("device-sync:", u.UID), 3, time.Minute) {
		fail(w, 429, "请稍后同步")
		return
	}
	var all struct {
		Items []struct {
			ID         string `json:"Id"`
			ReportedID string `json:"ReportedDeviceId"`
			AppName    string `json:"AppName"`
			AppVersion string `json:"AppVersion"`
			Name       string `json:"Name"`
			LastUserID string `json:"LastUserId"`
		} `json:"Items"`
	}
	if e := a.embyGet(r.Context(), "/Devices", &all); e != nil {
		fail(w, 502, "无法读取媒体服务器设备")
		return
	}
	mappings := map[string]string{}
	for _, device := range all.Items {
		if device.LastUserID == u.EmbyID && device.ID != "" && device.ReportedID != "" && len(device.ReportedID) <= 200 {
			mappings[device.ID] = device.ReportedID
		}
	}
	if err := a.store().NormalizeMediaDeviceIDs(u.UID, mappings); err != nil {
		fail(w, 409, "旧设备记录核对失败，请联系管理员")
		return
	}
	added, skipped := 0, 0
	for _, device := range all.Items {
		if device.LastUserID != u.EmbyID || device.ID == "" {
			continue
		}
		id := firstNonEmpty(device.ReportedID, device.ID)
		if e := a.store().ClaimMediaDevice(u.UID, id, device.Name, func(v store.User) int { d, _ := mediaLimits(a.cfg(), v); return d }, device.AppName, device.AppVersion); e != nil {
			skipped++
		} else {
			added++
		}
	}
	a.audit(r, "member_devices_synced", "user", u.UID, map[string]any{"matched": added, "skipped": skipped})
	ok(w, "已核对本人登录设备；超额或已停用设备不会自动恢复", map[string]any{"matched": added, "skipped": skipped})
}

func (a *App) handleMemberReleaseDevice(w http.ResponseWriter, r *http.Request, _ Params) {
	u := current(r).User
	payload := decodeMap(r)
	if !a.verifyMemberSecurityCode(w, r, u, stringValue(payload, "security_code")) {
		return
	}
	id := stringValue(payload, "device_id")
	owned := false
	for _, device := range u.MediaDevices {
		if device.ID == id {
			owned = true
		}
	}
	if !owned {
		fail(w, 404, "设备不存在")
		return
	}
	// Revoke only this member/device at the gateway; deleting an Emby device globally could log out another user on a shared installation.
	if err := a.store().RemoveMediaDevice(u.UID, id); err != nil {
		fail(w, 503, "设备名额释放失败，请重试")
		return
	}
	a.mediaIdentity.mu.Lock()
	a.mediaIdentity.items = nil
	a.mediaIdentity.mu.Unlock()
	if sessions, e := a.embySessionsSnapshot(r.Context(), true); e == nil {
		for _, session := range sessions {
			if asString(session["UserId"]) == u.EmbyID && asString(session["DeviceId"]) == id {
				sid := asString(session["Id"])
				if memberItemID.MatchString(sid) {
					_ = a.embyPost(r.Context(), "/Sessions/"+sid+"/Playing/Stop", map[string]any{}, nil)
				}
			}
		}
	}
	a.audit(r, "release_media_device", "user", u.UID, map[string]any{"device": id})
	ok(w, "已禁止此设备访问并释放名额", nil)
}

func parseMediaLimit(payload map[string]any, key string) (*int, bool, error) {
	raw, found := payload[key]
	if !found {
		return nil, false, nil
	}
	if raw == nil {
		return nil, true, nil
	}
	n, ok := raw.(float64)
	if !ok || n < 0 || n > 100 || n != float64(int(n)) {
		return nil, true, fmt.Errorf("%s 必须为0至100的整数或null", key)
	}
	v := int(n)
	return &v, true, nil
}

func lineURLs(lines []config.Line) []string {
	out := []string{}
	for _, line := range lines {
		out = append(out, line.URL)
	}
	return out
}

func normalizedPlaybackHost(v *url.URL) string {
	if v == nil || v.Host == "" {
		return ""
	}
	host := strings.ToLower(strings.TrimSuffix(v.Hostname(), "."))
	port := v.Port()
	if port == "" || (v.Scheme == "https" && port == "443") || (v.Scheme == "http" && port == "80") {
		return host
	}
	return host + ":" + port
}

func mediaClientSoftware(r *http.Request, uri *url.URL) (string, string) {
	client := firstNonEmpty(r.Header.Get("X-Emby-Client"), uri.Query().Get("X-Emby-Client"), uri.Query().Get("Client"))
	version := firstNonEmpty(r.Header.Get("X-Emby-Client-Version"), uri.Query().Get("Version"))
	for _, header := range []string{r.Header.Get("X-Emby-Authorization"), r.Header.Get("Authorization")} {
		header = strings.TrimSpace(header)
		lower := strings.ToLower(header)
		for _, prefix := range []string{"mediabrowser ", "emby "} {
			if strings.HasPrefix(lower, prefix) {
				header = header[len(prefix):]
				break
			}
		}
		for _, part := range strings.Split(header, ",") {
			key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
			if !ok {
				continue
			}
			value = strings.Trim(value, "\"")
			switch strings.ToLower(key) {
			case "client":
				client = firstNonEmpty(client, value)
			case "version":
				version = firstNonEmpty(version, value)
			}
		}
	}
	return truncateString(client, 100), truncateString(version, 50)
}
