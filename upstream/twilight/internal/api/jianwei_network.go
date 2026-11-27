package api

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

func (a *App) handleNetworkStatus(w http.ResponseWriter, r *http.Request, _ Params) {
	data, status, err := deploymentRequest(r.Context(), deploymentSocket, http.MethodGet, "/network/status", nil)
	if err != nil || status >= 400 {
		fail(w, 503, "证书执行器暂不可用，请检查主机服务")
		return
	}
	data["desired_origin"] = a.cfg().PublicBaseURL
	data["desired_origins"] = a.configuredIngressOrigins()
	w.Header().Set("Cache-Control", "private, no-store")
	ok(w, "OK", data)
}

func (a *App) handleNetworkAction(w http.ResponseWriter, r *http.Request, _ Params) {
	if requireAdmin(w, r) {
		return
	}
	if !requireWebUIIntent(w, r, "network-certificate") {
		return
	}
	payload := decodeMap(r)
	if !a.twoFactorPassword(w, r, payload) {
		return
	}
	operation := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	profile := stringValue(payload, "profile")
	configuredOrigins := a.configuredIngressOrigins()
	if _, valid := configuredOrigins[profile]; !valid {
		fail(w, 400, "请选择 Mini App、商店或播放入口")
		return
	}
	revision := intValue(payload, "expected_revision", -1)
	if revision < 0 {
		fail(w, 409, "请刷新证书状态后重试")
		return
	}
	origin := configuredOrigins[profile]
	var body map[string]any
	switch operation {
	case "configure":
		domain, err := url.Parse(origin)
		if err != nil || domain.Scheme != "https" || domain.Hostname() == "" || (domain.Port() != "" && domain.Port() != "443") {
			fail(w, 400, "请先保存标准HTTPS网站域名；内置证书入口使用443端口")
			return
		}
		accepted, _, valid := requireStrictBoolValue(w, payload, "accept_terms")
		if !valid {
			return
		}
		staging, _, valid := requireStrictBoolValue(w, payload, "staging")
		if !valid {
			return
		}
		body = map[string]any{"domain": domain.Hostname(), "email": stringValue(payload, "email"), "cloudflare_token": stringValue(payload, "cloudflare_token"), "accept_terms": accepted, "staging": staging, "profile": profile, "expected_revision": revision}
	case "issue", "renew", "publish":
		if operation == "publish" && !strings.HasPrefix(profile, "playback") {
			snapshot, err := a.configEditSnapshot()
			if err != nil {
				fail(w, 503, "暂时无法读取配置版本")
				return
			}
			origins := append([]string{}, a.cfg().CORSOrigins...)
			found := false
			for _, allowed := range origins {
				if allowed == origin {
					found = true
				}
			}
			if !found {
				origins = append(origins, origin)
				_, code, message := a.patchConfigSections(snapshot.revision, map[string]any{"API": map[string]any{"cors_origins": origins}})
				if code != http.StatusOK {
					fail(w, code, message)
					return
				}
			}
		}
		body = map[string]any{"profile": profile, "expected_revision": revision}
	default:
		fail(w, 404, "不支持的证书操作")
		return
	}
	data, status, err := deploymentRequest(r.Context(), deploymentSocket, http.MethodPost, "/network/"+operation, body)
	if err != nil {
		fail(w, 503, "证书执行器暂不可用，请检查主机服务")
		return
	}
	if status >= 400 {
		fail(w, status, "证书操作未提交，请检查域名、凭据与任务状态")
		return
	}
	data["desired_origins"] = a.configuredIngressOrigins()
	a.audit(r, "network_"+operation, "system", current(r).User.UID, map[string]any{"origin": a.cfg().PublicBaseURL})
	w.Header().Set("Cache-Control", "private, no-store")
	ok(w, "OK", data)
}

func (a *App) configuredIngressOrigins() map[string]string {
	cfg := a.cfg()
	out := map[string]string{"account": cfg.PublicBaseURL, "shop": cfg.CommerceStoreURL, "playback": cfg.EmbyPublicURL}
	ordinary := append([]string{cfg.EmbyPublicURL}, lineURLs(cfg.EmbyURLList)...)
	// A list-only configuration uses its first standard HTTPS line as primary.
	primary := ""
	for _, raw := range ordinary {
		u, e := url.Parse(raw)
		if e == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && (u.Port() == "" || u.Port() == "443") {
			primary = strings.ToLower(u.Hostname())
			out["playback"] = "https://" + primary
			break
		}
	}
	all := append(ordinary, append([]string{cfg.EmbyWhitelistURL}, lineURLs(cfg.EmbyWhitelistURLList)...)...)
	for _,service:=range a.store().Services().Items{if service.Enabled{all=append(all,service.Lines...)}}
	for _, raw := range all {
		u, e := url.Parse(raw)
		if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Port() != "" && u.Port() != "443" {
			continue
		}
		host := strings.ToLower(u.Hostname())
		if host == primary {
			continue
		}
		sum := sha256.Sum256([]byte(host))
		out[fmt.Sprintf("playback-%x", sum[:6])] = "https://" + host
	}
	return out
}
