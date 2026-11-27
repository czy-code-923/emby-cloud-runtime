package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/prejudice-studio/twilight/internal/config"
	"github.com/prejudice-studio/twilight/internal/store"
	"net/http"
	"net/url"
	"time"
)

// Offline owner maintenance, executed inside the trusted backend container.
// Credentials are never command arguments. The native administrator is kept.
func runAdminPlaybackBind(args []string) error {
	fs := flag.NewFlagSet("bind-admin-playback", flag.ContinueOnError)
	uid := fs.Int64("uid", 0, "platform administrator UID")
	old := fs.String("expected-id", "", "expected native Emby administrator ID")
	id := fs.String("emby-id", "", "new ordinary Emby ID")
	confirm := fs.String("confirm", "", "BIND_ADMIN_PLAYBACK")
	if e := fs.Parse(args); e != nil {
		return e
	}
	if *uid <= 0 || *old == "" || *id == "" || *id == *old || *confirm != "BIND_ADMIN_PLAYBACK" {
		return errors.New("UID, expected native ID, ordinary ID and confirmation required")
	}
	path, e := runtimeConfigPath("")
	if e != nil {
		return e
	}
	cfg, e := config.NewReader(path).ReadLocked()
	if e != nil {
		return e
	}
	st, e := openStore(context.Background(), cfg)
	if e != nil {
		return e
	}
	defer st.Close()
	user, found := st.User(*uid)
	if !found || user.Role != store.RoleAdmin || user.EmbyID != *old {
		return errors.New("administrator binding changed")
	}
	client := http.Client{Timeout: 8 * time.Second}
	read := func(id string) (map[string]any, error) {
		req, e := http.NewRequest("GET", cfg.EmbyURL+"/Users/"+url.PathEscape(id), nil)
		if e != nil {
			return nil, e
		}
		req.Header.Set("X-Emby-Token", cfg.EmbyToken)
		r, e := client.Do(req)
		if e != nil {
			return nil, e
		}
		defer r.Body.Close()
		if r.StatusCode != 200 {
			return nil, errors.New("Emby user check failed")
		}
		var v map[string]any
		e = json.NewDecoder(r.Body).Decode(&v)
		return v, e
	}
	native, e := read(*old)
	if e != nil {
		return e
	}
	play, e := read(*id)
	if e != nil {
		return e
	}
	nativePolicy, _ := native["Policy"].(map[string]any)
	policy, _ := play["Policy"].(map[string]any)
	if nativePolicy["IsAdministrator"] != true || policy["IsAdministrator"] != false || play["Id"] != *id {
		return errors.New("native administrator and ordinary player boundary invalid")
	}
	if other, ok := st.FindUserByEmbyID(*id); ok && other.UID != *uid {
		return errors.New("playback account already bound")
	}
	name, _ := play["Name"].(string)
	_, e = st.UpdateUser(*uid, func(u *store.User) error {
		if u.EmbyID != *old || u.Role != store.RoleAdmin {
			return store.ErrConflict
		}
		u.EmbyID = *id
		u.EmbyUsername = name
		u.EmbyDisabled = false
		u.MediaDevices = nil
		u.MediaRevokedDevices = nil
		u.MediaPolicyManaged = false
		return nil
	})
	if e != nil {
		return e
	}
	_ = st.AddAuditLog(store.AuditLog{UID: *uid, Username: user.Username, Action: "admin_playback_bound", Category: "security", Source: "offline_cli", TargetUID: *uid, Detail: map[string]any{"native_owner_preserved": true, "playback_name": name}, CreatedAt: time.Now().Unix()}, 10000)
	fmt.Println("Bound administrator playback:", name, "; native owner preserved")
	return nil
}
