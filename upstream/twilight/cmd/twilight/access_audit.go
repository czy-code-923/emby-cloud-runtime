package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/prejudice-studio/twilight/internal/config"
	"github.com/prejudice-studio/twilight/internal/store"
)

// Both legacy single URLs and current multi-line configuration are supported.
// Keep only configured HTTPS origins; never infer a public address from EmbyURL.
func auditPlaybackLines(single string, lines []config.Line) []string {
	values := []string{single}
	for _, line := range lines {
		values = append(values, line.URL)
	}
	var result []string
	seen := map[string]bool{}
	for _, value := range values {
		u, err := url.Parse(strings.TrimSpace(value))
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" {
			continue
		}
		u.Host = strings.ToLower(u.Host)
		if u.Port() == "443" {
			u.Host = u.Hostname()
			if strings.Contains(u.Host, ":") {
				u.Host = "[" + u.Host + "]"
			}
		}
		u.Path = ""
		value = u.String()
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

// Root-only maintenance via the backend container. Creates a disposable native
// player and matching platform user, checks real HTTPS, then removes only IDs
// created by this invocation. No existing user or financial data is modified.
func runWhitelistAccessAudit(args []string) (resultError error) {
	flags := flag.NewFlagSet("audit-whitelist", flag.ContinueOnError)
	confirm := flags.String("confirm", "", "AUDIT_WHITELIST")
	if e := flags.Parse(args); e != nil {
		return e
	}
	if *confirm != "AUDIT_WHITELIST" {
		return errors.New("explicit audit confirmation required")
	}
	path, e := runtimeConfigPath("")
	if e != nil {
		return e
	}
	cfg, e := config.NewReader(path).ReadLocked()
	if e != nil {
		return errors.New("cannot read runtime configuration for whitelist audit")
	}
	st, e := openStore(context.Background(), cfg)
	if e != nil {
		return errors.New("cannot open runtime database for whitelist audit")
	}
	defer st.Close()
	ordinary := auditPlaybackLines(cfg.EmbyPublicURL, cfg.EmbyURLList)
	whitelist := auditPlaybackLines(cfg.EmbyWhitelistURL, cfg.EmbyWhitelistURLList)
	if len(ordinary) == 0 || len(whitelist) == 0 {
		return errors.New("ordinary and whitelist HTTPS lines must be configured")
	}
	random := make([]byte, 12)
	if _, e = rand.Read(random); e != nil {
		return e
	}
	suffix := hex.EncodeToString(random)
	name := "jw-audit-" + suffix[:12]
	password := "Jw!" + suffix
	client := http.Client{Timeout: 12 * time.Second}
	request := func(method, target string, body any, token string) (int, map[string]any, error) {
		var raw []byte
		if body != nil {
			raw, _ = json.Marshal(body)
		}
		r, e := http.NewRequest(method, target, bytes.NewReader(raw))
		if e != nil {
			return 0, nil, e
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Emby-Authorization", `MediaBrowser Client="Jianwei Audit",Device="Disposable",DeviceId="jw-audit-`+suffix+`",Version="1"`)
		if token != "" {
			r.Header.Set("X-Emby-Token", token)
		}
		resp, e := client.Do(r)
		if e != nil {
			return 0, nil, errors.New("audit network request failed")
		}
		defer resp.Body.Close()
		var data map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&data)
		return resp.StatusCode, data, nil
	}
	code, native, e := request("POST", cfg.EmbyURL+"/Users/New", map[string]any{"Name": name}, cfg.EmbyToken)
	if e != nil || code != 200 {
		return errors.New("cannot create disposable native audit player")
	}
	id, _ := native["Id"].(string)
	if id == "" {
		return errors.New("audit player ID missing")
	}
	var uid int64
	defer func() {
		// Before deleting, verify the ID still names exactly our random account.
		status, user, err := request("GET", cfg.EmbyURL+"/Users/"+url.PathEscape(id), nil, cfg.EmbyToken)
		if err != nil || status != 200 || user["Name"] != name {
			resultError = errors.New("audit cleanup requires manual check: native identity changed")
			return
		}
		deleted, _, err := request("DELETE", cfg.EmbyURL+"/Users/"+url.PathEscape(id), nil, cfg.EmbyToken)
		if err != nil || deleted != 204 && deleted != 200 {
			resultError = errors.New("native audit account cleanup failed")
			return
		}
		if uid > 0 {
			user, found := st.User(uid)
			if !found || user.Username != name || user.EmbyID != id || st.DeleteUser(uid) != nil {
				resultError = errors.New("platform audit account cleanup failed")
				return
			}
		}
		fmt.Println(`{"audit_accounts_cleaned":true}`)
	}()
	if status, _, err := request("POST", cfg.EmbyURL+"/Users/"+id+"/Password", map[string]any{"NewPw": password, "ResetPassword": false}, cfg.EmbyToken); err != nil || status != 204 && status != 200 {
		return errors.New("audit password setup failed")
	}
	policy := map[string]any{"IsAdministrator": false, "IsDisabled": false, "EnableRemoteAccess": true, "EnableAllFolders": true, "SimultaneousStreamLimit": 3}
	if status, _, err := request("POST", cfg.EmbyURL+"/Users/"+id+"/Policy", policy, cfg.EmbyToken); err != nil || status != 204 && status != 200 {
		return errors.New("audit policy setup failed")
	}
	u, e := st.CreateUser(store.User{Username: name, EmbyUsername: name, EmbyID: id, Role: store.RoleWhitelist, WhitelistUntil: time.Now().Add(time.Hour).Unix(), ExpiredAt: time.Now().Add(24 * time.Hour).Unix(), Active: true})
	if e != nil {
		return e
	}
	uid = u.UID
	report := map[string]any{"audit": "disposable_whitelist", "existing_users_unchanged": true}
	loginStatus, login, e := request("POST", ordinary[0]+"/Users/AuthenticateByName", map[string]any{"Username": name, "Pw": password}, "")
	if e != nil || loginStatus != 200 {
		return errors.New("audit client authentication failed")
	}
	token, _ := login["AccessToken"].(string)
	if token == "" {
		return errors.New("audit authentication token missing")
	}
	for label, hosts := range map[string][]string{"ordinary": ordinary, "whitelist": whitelist} {
		for index, host := range hosts {
			status, _, err := request("GET", host+"/Users/"+id+"/Views", nil, token)
			report[fmt.Sprintf("%s_%d_as_whitelist", label, index+1)] = status
			if err != nil || status != 200 {
				return fmt.Errorf("%s line %d denied valid whitelist", label, index+1)
			}
		}
	}
	_, e = st.UpdateUser(uid, func(next *store.User) error { next.WhitelistUntil = time.Now().Unix() - 1; return nil })
	if e != nil {
		return e
	}
	for label, hosts := range map[string][]string{"ordinary": ordinary, "whitelist": whitelist} {
		for index, host := range hosts {
			status, _, err := request("GET", host+"/Users/"+id+"/Views", nil, token)
			report[fmt.Sprintf("%s_%d_after_expiry", label, index+1)] = status
			expected := 200
			if label == "whitelist" {
				expected = 403
			}
			if err != nil || status != expected {
				return fmt.Errorf("%s line %d returned unexpected permissions after whitelist expiry", label, index+1)
			}
		}
	}
	report["passed"] = true
	raw, _ := json.Marshal(report)
	fmt.Println(string(raw))
	return nil
}
