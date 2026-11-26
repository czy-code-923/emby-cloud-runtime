package api

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var metadataConfigFields = map[string]map[string]bool{
	"Global":      {"tmdb_api_key": true, "tmdb_api_url": true, "tmdb_image_url": true, "bangumi_token": true, "bangumi_api_url": true},
	"BangumiSync": {"enabled": true, "manage_enabled": true, "auto_add_collection": true, "private_collection": true, "min_progress_percent": true, "block_keywords": true, "webhook_secret": true, "webhook_allow_legacy_token": true},
}

func (a *App) handleMetadataSave(w http.ResponseWriter, r *http.Request, _ Params) {
	body := decodeMap(r)
	if requireAdmin(w, r) || !requireWebUIIntent(w, r, "metadata-admin") || !a.twoFactorPassword(w, r, body) {
		return
	}
	sections, valid := body["sections"].(map[string]any)
	if !valid || len(sections) == 0 {
		fail(w, 400, "没有资料源配置变更")
		return
	}
	for section, raw := range sections {
		fields, ok := raw.(map[string]any)
		if !ok || metadataConfigFields[section] == nil {
			fail(w, 400, "不支持的资料源配置")
			return
		}
		for field := range fields {
			if !metadataConfigFields[section][field] {
				fail(w, 400, "不支持的资料源配置字段")
				return
			}
		}
	}
	revision := stringValue(body, "expected_revision")
	if revision == "" {
		fail(w, 409, "请先刷新资料源配置")
		return
	}
	info, status, message := a.patchConfigSections(revision, sections)
	if status != 200 {
		failConfigEdit(w, status, message)
		return
	}
	a.audit(r, "metadata_config_saved", "admin", 0, nil)
	ok(w, "资料源配置已保存并热重载", info)
}
func (a *App) handleMetadataProbe(w http.ResponseWriter, r *http.Request, _ Params) {
	if !a.allowRate(r.Context(), "metadata-probe", 6, time.Minute) {
		fail(w, 429, "检查过于频繁，请稍后再试")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	results := map[string]string{"tmdb": "unconfigured", "bangumi": "unavailable"}
	if a.cfg().TMDBAPIKey != "" {
		var payload map[string]any
		if e := a.tmdbJSON(ctx, "/configuration", url.Values{}, &payload); e == nil {
			results["tmdb"] = "available"
		} else {
			results["tmdb"] = "unavailable"
		}
	}
	endpoint, e := bangumiEndpoint(a.cfg().BangumiAPIURL, "/subjects/1", nil)
	if e == nil {
		var payload map[string]any
		if e = getJSON(ctx, endpoint, a.bangumiSearchHeaders(), &payload); e == nil {
			results["bangumi"] = "available"
		}
	}
	ok(w, "资料源连接检查完成", results)
}

// TMDB supports either a v3 key or an application read-access bearer token.
func (a *App) tmdbJSON(ctx context.Context, path string, q url.Values, out any) error {
	base, e := a.tmdbBase()
	if e != nil {
		return e
	}
	key := strings.TrimSpace(a.cfg().TMDBAPIKey)
	if key == "" {
		return fmt.Errorf("TMDB key not configured")
	}
	headers := map[string]string{"Accept": "application/json"}
	q.Del("api_key")
	if strings.HasPrefix(strings.ToLower(key), "bearer ") {
		headers["Authorization"] = "Bearer " + strings.TrimSpace(key[7:])
	} else if strings.Count(key, ".") == 2 {
		headers["Authorization"] = "Bearer " + key
	} else {
		q.Set("api_key", key)
	}
	return getJSON(ctx, base+path+"?"+q.Encode(), headers, out)
}
func (a *App) tmdbCacheScope() string {
	c := a.cfg()
	sum := sha256.Sum256([]byte(c.TMDBAPIKey + "|" + c.TMDBAPIURL + "|" + c.TMDBImageURL))
	return fmt.Sprintf("%x", sum[:8])
}
