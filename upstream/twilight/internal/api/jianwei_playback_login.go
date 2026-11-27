package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

func (a *App) mediaTokenHash(token string) string {
	h := sha256.Sum256([]byte(a.cfg().EmbyURL + "\x00" + token))
	return hex.EncodeToString(h[:])
}

// The authentication response is the authority for the token subject. Emby
// 4.10 has no /Users/Me resource; never infer a subject from client UserId.
func (a *App) handleMediaClientLogin(w http.ResponseWriter, r *http.Request, _ Params) {
	secret := a.cfg().BotInternalSecret
	if len(secret) < 16 || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-JW-Gateway")), []byte(secret)) != 1 {
		fail(w, 403, "Forbidden gateway")
		return
	}
	if !a.allowRate(r.Context(), rateKey("media-login:", r.Header.Get("X-JW-Client-IP")), 10, time.Minute) {
		w.Header().Set("Retry-After", "60")
		fail(w, 429, "登录过于频繁，请稍后再试")
		return
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, 16385))
	if e != nil || len(raw) > 16384 {
		fail(w, 400, "登录参数过大")
		return
	}
	var credentials map[string]any
	if json.Unmarshal(raw, &credentials) != nil {
		fail(w, 400, "登录参数不正确")
		return
	}
	endpoint, e := a.validatedEmbyEndpoint("/Users/AuthenticateByName")
	if e != nil {
		fail(w, 503, "媒体服务暂不可用")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(string(raw)))
	if e != nil {
		fail(w, 503, "媒体服务暂不可用")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	for _, key := range []string{"X-Emby-Authorization", "Authorization", "X-Emby-Client", "X-Emby-Device-Id", "X-Emby-Device-Name", "X-Emby-Client-Version"} {
		if v := r.Header.Get(key); v != "" {
			req.Header.Set(key, v)
		}
	}
	resp, e := sharedHTTPClient.Do(req)
	if e != nil {
		fail(w, 502, "媒体登录暂不可用")
		return
	}
	defer resp.Body.Close()
	data, e := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if e != nil {
		fail(w, 502, "媒体登录响应错误")
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Type", "application/json")
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		w.WriteHeader(resp.StatusCode)
		w.Write(data)
		return
	}
	var result struct {
		User        map[string]any
		AccessToken string
		SessionInfo struct {
			DeviceID string `json:"DeviceId"`
		}
	}
	if json.Unmarshal(data, &result) != nil || result.AccessToken == "" || asString(result.User["Id"]) == "" {
		fail(w, 502, "媒体登录响应无效")
		return
	}
	if embyRemoteIsAdministrator(result.User) {
		fail(w, 403, "原生owner用于后台管理，请使用管理员的独立播放账号")
		return
	}
	u, found := a.store().FindUserByEmbyID(asString(result.User["Id"]))
	if !found || !u.Active || u.EmbyDisabled || !a.embyShouldEnableUser(u) {
		fail(w, 403, "播放账号尚未绑定或已停用，请联系管理员")
		return
	}
	if e = a.store().RememberMediaClientLogin(a.mediaTokenHash(result.AccessToken), u.EmbyID, result.SessionInfo.DeviceID); e != nil {
		fail(w, 503, "播放会话暂不可保存，请重试")
		return
	}
	w.WriteHeader(resp.StatusCode)
	w.Write(data)
}
