package api

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/prejudice-studio/twilight/internal/security"
	"github.com/prejudice-studio/twilight/internal/store"
)

func miniAppIdentity(raw, token string, now int64) (telegramUser, error) {
	invalid := errors.New("Mini App 身份验证失败，请从机器人重新打开")
	if len(raw) == 0 || len(raw) > 8192 || token == "" {
		return telegramUser{}, invalid
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return telegramUser{}, invalid
	}
	parts := make([]string, 0, len(values))
	for key, list := range values {
		if len(list) != 1 {
			return telegramUser{}, invalid
		}
		if key != "hash" {
			parts = append(parts, key+"="+list[0])
		}
	}
	sort.Strings(parts)
	key := hmac.New(sha256.New, []byte("WebAppData"))
	key.Write([]byte(token))
	signature := hmac.New(sha256.New, key.Sum(nil))
	signature.Write([]byte(strings.Join(parts, "\n")))
	expected, err := hex.DecodeString(values.Get("hash"))
	if err != nil || !hmac.Equal(expected, signature.Sum(nil)) {
		return telegramUser{}, invalid
	}
	date, err := strconv.ParseInt(values.Get("auth_date"), 10, 64)
	if err != nil || date > now+30 || date < now-300 {
		return telegramUser{}, invalid
	}
	var user telegramUser
	if json.Unmarshal([]byte(values.Get("user")), &user) != nil || user.ID <= 0 || user.IsBot {
		return telegramUser{}, invalid
	}
	return user, nil
}

func (a *App) jianweiMiniURL() string {
	value := strings.TrimSpace(a.cfg().TelegramMiniAppURL)
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/miniapp" {
		return ""
	}
	return u.String()
}

func (a *App) telegramStartProfile(ctx context.Context, chatID, fromID int64, username string) {
	if !a.allowRate(ctx, rateKey("jw-tg-start:", fromID), 6, time.Minute) {
		return
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return
	}
	hash, err := security.HashPassword(hex.EncodeToString(random))
	if err != nil {
		return
	}
	user, created, err := a.store().EnsureTelegramProfile(fromID, username, hash, a.cfg().UserLimit)
	if err != nil {
		_ = a.telegramSendMessage(ctx, chatID, "暂时无法建立身份，请联系管理员检查注册容量。")
		return
	}
	if !user.Active {
		_ = a.telegramSendMessage(ctx, chatID, "账号已停用，请联系管理员。")
		return
	}
	if created {
		a.auditTelegramAction(fromID, "telegram_profile_created", "user", user.UID, map[string]any{"source": "start"})
	}
	a.telegramMemberPanel(ctx, chatID, user)
}

func (a *App) handleJianweiTelegramIdentity(w http.ResponseWriter, r *http.Request, _ Params) {
	w.Header().Set("Cache-Control", "private, no-store")
	ok(w, "OK", map[string]any{"bot": a.telegramBotInfo(r.Context(), 8*time.Second, true), "runtime": a.telegramRuntimeStatus(), "auto_provision": a.cfg().TelegramAutoProvision, "mini_app_url": a.jianweiMiniURL()})
}

func (a *App) handlePublishMiniApp(w http.ResponseWriter, r *http.Request, _ Params) {
	if requireAdmin(w, r) || !requireWebUIIntent(w, r, "publish-miniapp") {
		return
	}
	payload := decodeMap(r)
	if !a.twoFactorPassword(w, r, payload) {
		return
	}
	state, status, err := deploymentRequest(r.Context(), deploymentSocket, http.MethodGet, "/network/status", nil)
	if err != nil || status != 200 {
		fail(w, 503, "请先发布账户域名的HTTPS入口")
		return
	}
	applied, _ := state["applied"].(map[string]any)
	origin, _ := applied["origin"].(string)
	if origin == "" || origin != a.cfg().PublicBaseURL {
		fail(w, 409, "当前域名尚未发布，或设置已更改，请先核对证书入口")
		return
	}
	target := origin + "/miniapp"
	previousMenu, err := telegramPostResult[map[string]any](a, r.Context(), "getChatMenuButton", struct{}{}, 10*time.Second)
	if err != nil {
		fail(w, 502, "暂时无法读取现有机器人菜单，请重试")
		return
	}
	snapshot, err := a.configEditSnapshot()
	if err != nil {
		fail(w, 503, "暂时无法读取配置版本")
		return
	}
	_, err = telegramPostResult[telegramDiscardResult](a, r.Context(), "setChatMenuButton", map[string]any{"menu_button": map[string]any{"type": "web_app", "text": "见微面板", "web_app": map[string]string{"url": target}}}, 20*time.Second)
	if err != nil {
		fail(w, 502, "机器人菜单未发布，请检查Token和网络后重试")
		return
	}
	_, code, message := a.patchConfigSections(snapshot.revision, map[string]any{"Telegram": map[string]any{"mini_app_url": target}})
	if code != http.StatusOK {
		rollbackContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, rollbackError := telegramPostResult[telegramDiscardResult](a, rollbackContext, "setChatMenuButton", map[string]any{"menu_button": previousMenu}, 10*time.Second)
		a.audit(r, "telegram_miniapp_publish_failed", "admin", current(r).User.UID, map[string]any{"origin": origin, "menu_restored": rollbackError == nil})
		if rollbackError != nil {
			fail(w, 503, "配置版本发生冲突，且菜单恢复未获回执；请刷新配置后重新发布菜单")
			return
		}
		fail(w, code, message)
		return
	}
	a.audit(r, "telegram_miniapp_publish", "admin", current(r).User.UID, map[string]any{"origin": origin})
	ok(w, "Mini App菜单已发布", map[string]any{"url": target})
}

const miniCookie = "jw_miniapp"

func (a *App) handleMiniSession(w http.ResponseWriter, r *http.Request, _ Params) {
	w.Header().Set("Cache-Control", "private, no-store")
	if !a.allowRate(r.Context(), rateKey("jw-mini-auth:", a.clientIP(r)), 12, time.Minute) {
		fail(w, 429, "请求过于频繁")
		return
	}
	if !a.telegramAvailable() {
		fail(w, 403, "机器人服务未启用")
		return
	}
	payload := decodeMap(r)
	actor, err := miniAppIdentity(stringValue(payload, "init_data"), a.cfg().TelegramBotToken, time.Now().Unix())
	if err != nil {
		fail(w, 401, "Mini App 身份验证失败，请从机器人重新打开")
		return
	}
	// Signature-equivalent encodings must share the same one-use proof identity.
	// URL ordering and percent-encoding cannot create a second session.
	proofValues, _ := url.ParseQuery(stringValue(payload, "init_data"))
	proofValues.Set("hash", strings.ToLower(proofValues.Get("hash")))
	proof := proofValues.Encode()
	user, found := a.store().FindUserByTelegramID(actor.ID)
	if !found || !user.Active {
		fail(w, 403, "请先在机器人私聊中点击启动，或联系管理员检查账号状态")
		return
	}
	nonce := stringValue(payload, "request_id")
	if len(nonce) != 64 {
		fail(w, 400, "请重新打开面板")
		return
	}
	if _, err = hex.DecodeString(nonce); err != nil {
		fail(w, 400, "请重新打开面板")
		return
	}
	mac := hmac.New(sha256.New, []byte(a.cfg().TelegramBotToken))
	mac.Write([]byte("jw-mini-session|" + proof + "|" + nonce))
	token := hex.EncodeToString(mac.Sum(nil))
	if err = a.store().CreateMiniSession(r.Context(), user.UID, actor.ID, proof, nonce, a.cfg().TelegramBotToken, token, time.Now().Unix()); err != nil {
		fail(w, 409, "启动凭据已使用，请从机器人重新打开面板")
		return
	}

	http.SetCookie(w, &http.Cookie{Name: miniCookie, Value: token, Path: "/api/v2/miniapp", Secure: true, HttpOnly: true, SameSite: http.SameSiteNoneMode, MaxAge: 900})
	ok(w, "OK", map[string]any{"authenticated": true})
}

func (a *App) miniUser(w http.ResponseWriter, r *http.Request) (store.User, bool) {
	w.Header().Set("Cache-Control", "private, no-store")
	if !a.telegramAvailable() {
		fail(w, 403, "机器人服务未启用")
		return store.User{}, false
	}
	cookie, err := r.Cookie(miniCookie)
	if err != nil {
		fail(w, 401, "请从机器人重新打开面板")
		return store.User{}, false
	}
	u, err := a.store().MiniSessionUser(r.Context(), cookie.Value, a.cfg().TelegramBotToken, time.Now().Unix())
	if err != nil {
		fail(w, 401, "会话已过期，请从机器人重新打开面板")
		return store.User{}, false
	}
	return u, true
}
func (a *App) handleMiniAccount(w http.ResponseWriter, r *http.Request, _ Params) {
	user, okUser := a.miniUser(w, r)
	if !okUser {
		return
	}
	ok(w, "OK", a.memberAccount(user))
}
