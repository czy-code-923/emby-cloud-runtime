package api

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/prejudice-studio/twilight/internal/security"
	"github.com/prejudice-studio/twilight/internal/store"
)

var memberSecurityCodePattern = regexp.MustCompile(`^[0-9]{6,12}$`)

func (a *App) verifyMemberSecurityCode(w http.ResponseWriter, r *http.Request, u store.User, code string) bool {
	if u.SecurityCodeHash == "" {
		fail(w, 409, "请先设置安全码")
		return false
	}
	now := time.Now().Unix()
	if u.MemberSecurityAttempts.LockedUntil > now {
		memberSecurityCooldown(w, u.MemberSecurityAttempts.LockedUntil-now)
		return false
	}
	valid := memberSecurityCodePattern.MatchString(code) && security.VerifyPassword(code, u.SecurityCodeHash)
	retry, err := a.store().CheckMemberSecurityResult(u.UID, u.SecurityCodeHash, valid, now)
	if err != nil {
		fail(w, 409, "安全设置已变化，请刷新后重新验证")
		return false
	}
	if retry > 0 {
		memberSecurityCooldown(w, retry)
		return false
	}
	if !valid {
		failWithCode(w, 403, ErrForbidden, "安全码不正确；连续错误5次将冷却5分钟")
		return false
	}
	return true
}

func memberSecurityCooldown(w http.ResponseWriter, seconds int64) {
	w.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
	failWithCodeData(w, 429, ErrRateLimited, fmt.Sprintf("安全码连续验证错误，请%d秒后重试", seconds), map[string]any{"retry_after_seconds": seconds})
}

func (a *App) handleMemberSecurityCode(w http.ResponseWriter, r *http.Request, _ Params) {
	u := current(r).User
	body := decodeMap(r)
	if u.TelegramID <= 0 {
		fail(w, 403, "请先绑定 Telegram")
		return
	}
	if u.SecurityCodeHash != "" && !a.verifyMemberSecurityCode(w, r, u, stringValue(body, "old_code")) {
		return
	}
	if !a.allowRate(r.Context(), rateKey("member-security-set:", u.UID), 3, 10*time.Minute) {
		fail(w, 429, "修改过于频繁，请稍后再试")
		return
	}
	code := stringValue(body, "new_code")
	if !memberSecurityCodePattern.MatchString(code) {
		fail(w, 400, "安全码需为6至12位数字")
		return
	}
	hash, err := security.HashPassword(code)
	if err != nil {
		fail(w, 500, "安全码保存失败")
		return
	}
	_, err = a.store().UpdateUser(u.UID, func(next *store.User) error {
		if next.SecurityCodeHash != u.SecurityCodeHash {
			return store.ErrConflict
		}
		next.SecurityCodeHash = hash
		next.MemberSecurityAttempts = store.MemberSecurityAttempts{}
		return nil
	})
	if statusFromError(w, err) {
		return
	}
	a.audit(r, "member_security_code_changed", "user", u.UID, nil)
	ok(w, "安全码已保存，请立即备份到安全位置；系统不会显示原安全码，请勿与Emby密码相同", nil)
}

func (a *App) handleMemberPassword(w http.ResponseWriter, r *http.Request, _ Params) {
	u := current(r).User
	body := decodeMap(r)
	if !a.verifyMemberSecurityCode(w, r, u, stringValue(body, "security_code")) {
		return
	}
	if a.requireNonEmbyAdmin(w, r, u) {
		return
	}
	if u.EmbyID == "" {
		fail(w, 409, "尚未绑定 Emby")
		return
	}
	password := stringValue(body, "new_password")
	if valid, message := validateStrongPassword(password, "Emby password"); !valid {
		fail(w, 400, message)
		return
	}
	// This member channel uses Telegram identity + the security code. Any
	// separately enabled email verification remains mandatory.
	if a.memberPasswordEmailRequired(u) && !a.consumePasswordChangeEmailCode(w, body, u, emailPurposeChangeEmby) {
		return
	}
	if err := a.embySetPassword(r.Context(), u.EmbyID, password); err != nil {
		fail(w, 502, "更新 Emby 密码失败，请稍后重试")
		return
	}
	a.audit(r, "change_emby_password", "user", u.UID, map[string]any{"method": "telegram_security_code"})
	ok(w, "Emby密码已更新，请立即保存到密码管理器；系统无法再次显示原密码", nil)
}

func (a *App) handleAdminResetMemberSecurity(w http.ResponseWriter, r *http.Request, p Params) {
	if requireAdmin(w, r) || !requireWebUIIntent(w, r, "member-security-reset") {
		return
	}
	body := decodeMap(r)
	if !a.twoFactorPassword(w, r, body) {
		return
	}
	uid, err := int64Param(p, "uid")
	if err != nil {
		fail(w, 400, "无效用户")
		return
	}
	_, err = a.store().UpdateUser(uid, func(u *store.User) error {
		u.SecurityCodeHash = ""
		u.MemberSecurityAttempts = store.MemberSecurityAttempts{}
		return nil
	})
	if statusFromError(w, err) {
		return
	}
	a.audit(r, "member_security_code_reset", "admin", uid, nil)
	ok(w, "安全码已清除，用户需重新设置", nil)
}

func (a *App) handleMemberPasswordEmail(w http.ResponseWriter, r *http.Request, _ Params) {
	user := current(r).User
	if !a.memberPasswordEmailRequired(user) {
		fail(w, 409, "此账户无需邮箱验证码")
		return
	}
	if !user.EmailVerified || user.Email == "" {
		fail(w, 403, "请联系管理员确认已验证邮箱")
		return
	}
	id, status, code, message := a.issueEmailCode(r.Context(), a.clientIP(r), emailPurposeChangeEmby, user.Email, user.UID)
	if code != "" {
		failWithCode(w, status, code, message)
		return
	}
	ok(w, "验证码已发送", map[string]any{"verification_id": id})
}

func (a *App) handleAdminResetMediaDevices(w http.ResponseWriter, r *http.Request, p Params) {
	if requireAdmin(w, r) || !requireWebUIIntent(w, r, "member-security-reset") {
		return
	}
	if !a.twoFactorPassword(w, r, decodeMap(r)) {
		return
	}
	uid, err := int64Param(p, "uid")
	if err != nil {
		fail(w, 400, "无效用户")
		return
	}
	_, err = a.store().UpdateUser(uid, func(u *store.User) error { u.MediaDevices = nil; u.MediaRevokedDevices = nil; return nil })
	if statusFromError(w, err) {
		return
	}
	a.mediaIdentity.mu.Lock()
	a.mediaIdentity.items = nil
	a.mediaIdentity.mu.Unlock()
	a.audit(r, "admin_reset_media_devices", "admin", uid, nil)
	ok(w, "登记与停用记录已清除，设备将在下次访问时重新登记", nil)
}

// Telegram identity and the security code remain mandatory. Email proof is
// added only when the member actually has a verified email address.
func (a *App) memberPasswordEmailRequired(user store.User) bool {
	return user.EmailVerified && user.Email != "" && a.passwordChangeEmailRequired(user, emailPurposeChangeEmby)
}
