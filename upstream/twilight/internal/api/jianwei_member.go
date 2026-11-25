package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/prejudice-studio/twilight/internal/store"
)

// Mini App routes are an explicit member-only allowlist. They never accept a
// target UID, arbitrary endpoint, administrator session or API key as identity.
func (a *App) miniMember(next HandlerFunc) HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, p Params) {
		user, valid := a.miniUser(w, r)
		if !valid {
			return
		}
		if r.Method != http.MethodGet {
			if !requireWebUIIntent(w, r, "miniapp-action") {
				return
			}
			// SameSite=None is necessary inside Telegram's iframe. Require the
			// exact deployed app origin as well as the non-simple intent header.
			u, err := url.Parse(a.jianweiMiniURL())
			if err != nil || u.Host == "" || r.Header.Get("Origin") != u.Scheme+"://"+u.Host {
				fail(w, 403, "操作来源不匹配，请从机器人重新打开面板")
				return
			}
		}
		if !a.allowRate(r.Context(), rateKey("mini-member:", user.UID), 90, time.Minute) {
			fail(w, 429, "操作过于频繁，请稍后再试")
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), principalKey, principal{User: user}))
		next(w, r, p)
	}
}

func publicHTTPSURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return ""
	}
	return u.String()
}

func (a *App) memberAccount(user store.User) map[string]any {
	cfg := a.cfg()
	si := a.store().Signin(user.UID)
	shop := ""
	if cfg.TelegramShopEnabled {
		shop = publicHTTPSURL(cfg.CommerceStoreURL)
	}
	banner := ""
	bannerURL := ""
	if cfg.TelegramBannerEnabled {
		banner = truncateString(strings.TrimSpace(cfg.TelegramBannerText), 500)
		bannerURL = publicHTTPSURL(cfg.TelegramBannerURL)
	}
	return map[string]any{
		"icon": a.publicServerIconURL(), "name": cfg.AppName,
		"uid": user.UID, "username": user.Username, "emby_username": user.EmbyUsername,
		"media_enabled": user.EmbyID != "" && !user.EmbyDisabled, "has_emby": user.EmbyID != "",
		"expired_at": publicExpiryUnix(user.ExpiredAt), "points": si.Points,
		"signin_enabled": cfg.SigninEnabled, "last_signin": si.LastSignin,
		"can_register":   user.EmbyID == "" && (user.PendingEmby || cfg.EmbyDirectRegisterEnabled),
		"invite_enabled": cfg.InviteEnabled, "requests_enabled": cfg.MediaRequestEnabled,
		"shop_url": shop, "banner": banner, "banner_url": bannerURL,
		"signin": a.signinPagePayload(user, 12), "role": user.Role, "security_code_set": user.SecurityCodeHash != "", "password_email_required": a.passwordChangeEmailRequired(user, emailPurposeChangeEmby),
	}
}

func (a *App) handleMemberMediaRequests(w http.ResponseWriter, r *http.Request, _ Params) {
	if a.requireMediaRequestEnabled(w) {
		return
	}
	requests := a.store().ListMediaRequests(current(r).User.UID, false)
	items := make([]map[string]any, 0, 30)
	for _, item := range requests {
		if len(items) >= 30 {
			break
		}
		items = append(items, mediaRequestUserDTO(item))
	}
	ok(w, "OK", map[string]any{"items": items, "total": len(requests)})
}

func (a *App) handleMemberInvite(w http.ResponseWriter, r *http.Request, _ Params) {
	if a.requireInviteEnabled(w) {
		return
	}
	user := current(r).User
	can, reason := a.canInvite(user)
	codes := a.store().ListInviteCodes(user.UID)
	items := make([]map[string]any, 0, 20)
	for _, code := range codes {
		if len(items) >= 20 {
			break
		}
		items = append(items, a.inviteCodeDTO(code))
	}
	ok(w, "OK", map[string]any{"can_invite": can, "reason": reason, "codes": items, "total": len(codes), "children": len(a.store().ChildrenOf(user.UID))})
}

func (a *App) handleMemberCreateRequest(w http.ResponseWriter, r *http.Request, p Params) {
	// This narrow transport cannot request administrator inventory bypass.
	if a.requireMediaRequestEnabled(w) {
		return
	}
	if !a.allowRate(r.Context(), rateKey("member-request:", strconv.FormatInt(current(r).User.UID, 10)), 5, time.Minute) {
		fail(w, 429, "求片提交过于频繁，请稍后再试")
		return
	}
	payload := decodeMap(r)
	title := truncateString(strings.TrimSpace(stringValue(payload, "title")), 120)
	if title == "" {
		fail(w, 400, "请填写片名")
		return
	}
	clean := map[string]any{"title": title, "note": truncateString(stringValue(payload, "note"), 1000)}
	raw, _ := json.Marshal(clean)
	r.Body = io.NopCloser(bytes.NewReader(raw))
	a.handleCreateMediaRequest(w, r, p)
}
