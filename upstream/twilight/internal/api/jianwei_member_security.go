package api

import (
	"net/http"
	"regexp"
	"time"

	"github.com/prejudice-studio/twilight/internal/security"
	"github.com/prejudice-studio/twilight/internal/store"
)

var memberSecurityCodePattern = regexp.MustCompile(`^[0-9]{6,12}$`)

func (a *App) verifyMemberSecurityCode(w http.ResponseWriter, r *http.Request, u store.User, code string) bool {
	if !a.allowRate(r.Context(), rateKey("member-security:", u.UID), 5, 5*time.Minute) {
		fail(w, 429, "安全码验证过于频繁，请5分钟后重试")
		return false
	}
	if u.SecurityCodeHash == "" {
		fail(w, 409, "请先设置安全码")
		return false
	}
	if !memberSecurityCodePattern.MatchString(code) || !security.VerifyPassword(code, u.SecurityCodeHash) {
		fail(w, 403, "安全码不正确")
		return false
	}
	return true
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
		return nil
	})
	if statusFromError(w, err) {
		return
	}
	a.audit(r, "member_security_code_changed", "user", u.UID, nil)
	ok(w, "安全码已保存，请勿与 Emby 密码相同", nil)
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
	if !a.consumePasswordChangeEmailCode(w, body, u, emailPurposeChangeEmby) {
		return
	}
	if err := a.embySetPassword(r.Context(), u.EmbyID, password); err != nil {
		fail(w, 502, "更新 Emby 密码失败，请稍后重试")
		return
	}
	a.audit(r, "change_emby_password", "user", u.UID, map[string]any{"method": "telegram_security_code"})
	ok(w, "Emby 密码已更新", nil)
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
	_, err = a.store().UpdateUser(uid, func(u *store.User) error { u.SecurityCodeHash = ""; return nil })
	if statusFromError(w, err) {
		return
	}
	a.audit(r, "member_security_code_reset", "admin", uid, nil)
	ok(w, "安全码已清除，用户需重新设置", nil)
}

func (a *App) handleMemberPasswordEmail(w http.ResponseWriter, r *http.Request, _ Params) {
	user := current(r).User
	if !a.passwordChangeEmailRequired(user, emailPurposeChangeEmby) {
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
