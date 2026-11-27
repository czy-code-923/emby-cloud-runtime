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

func memberRoleName(role int) string {
	switch role {
	case store.RoleAdmin:
		return "管理员"
	case store.RoleWhitelist:
		return "白名单"
	default:
		return "用户"
	}
}

func effectiveMember(u store.User) store.User {
	if u.Role == store.RoleWhitelist && u.WhitelistUntil > 0 && u.WhitelistUntil <= time.Now().Unix() && !u.PendingEmby {
		u.Role = store.RoleNormal
	}
	return u
}

func publicHTTPSURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return ""
	}
	return u.String()
}

func (a *App) memberAccount(user store.User) map[string]any {
	user = effectiveMember(user)
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
	result := map[string]any{
		"icon": a.publicServerIconURL(), "name": cfg.AppName,
		"uid": user.UID, "username": user.Username, "emby_username": user.EmbyUsername,
		"media_enabled": user.EmbyID != "" && !user.EmbyDisabled && a.embyShouldEnableUser(user), "has_emby": user.EmbyID != "",
		"expired_at": publicExpiryUnix(user.ExpiredAt), "points": a.store().Wallet(user.UID).Available, "wallet": a.store().Wallet(user.UID), "role_label": memberRoleName(user.Role), "avatar": miniAvatarURL(user),
		"email_enabled": emailConfigured(cfg), "email_verified": user.EmailVerified, "email_masked": maskEmail(user.Email),
		"signin_enabled": cfg.SigninEnabled, "last_signin": si.LastSignin,
		"can_register":   user.EmbyID == "" && (user.PendingEmby || cfg.EmbyDirectRegisterEnabled),
		"invite_enabled": cfg.InviteEnabled, "requests_enabled": cfg.MediaRequestEnabled, "request_daily_limit": cfg.MediaRequestDailyLimit,
		"banner_image": publicHTTPSURL(cfg.TelegramBannerImageURL), "shop_url": shop, "banner": banner, "banner_url": bannerURL,
		"signin": a.signinPagePayload(user, 30), "role": user.Role, "security_code_set": user.SecurityCodeHash != "", "password_email_required": a.memberPasswordEmailRequired(user),
		"viewing": a.memberViewing(user.UID),
	}
	if registration := a.memberRegistrationFor(user); registration != nil {
		result["registration"] = registration
	}
	return result
}

func (a *App) memberViewing(uid int64) store.MemberViewing {
	v, err := a.store().MemberViewing(uid, time.Now().Unix())
	if err != nil {
		return store.MemberViewing{Quality: "unavailable"}
	}
	return v
}

func (a *App) handleMemberMediaRequests(w http.ResponseWriter, r *http.Request, _ Params) {
	if a.requireMediaRequestEnabled(w) {
		return
	}
	result := a.store().ListMediaRequestsPageWithOptions(store.MediaRequestListOptions{UID: current(r).User.UID, StatusFilter: "all", Page: clamp(queryInt(r, "page", 1), 1, 1000), PerPage: 12})
	items := make([]map[string]any, 0, len(result.Requests))
	for _, item := range result.Requests {
		items = append(items, mediaRequestUserDTO(item))
	}
	ok(w, "OK", map[string]any{"items": items, "total": result.Total, "page": result.Page, "has_next": result.HasNext})
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
	for _, key := range []string{"source", "media_id", "media_type", "season", "year", "poster", "poster_url", "overview", "original_title"} {
		if value, ok := payload[key]; ok {
			clean[key] = value
		}
	}

	raw, _ := json.Marshal(clean)
	r.Body = io.NopCloser(bytes.NewReader(raw))
	a.handleCreateMediaRequest(w, r, p)
}

func miniAvatarURL(u store.User) string {
	if u.Avatar != "" || u.TelegramID > 0 {
		return "/api/v2/miniapp/avatar"
	}
	return ""
}
