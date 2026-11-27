package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/prejudice-studio/twilight/internal/retention"
	"github.com/prejudice-studio/twilight/internal/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestJianweiCardsDuringSelectedWindowResetAuthorizationAndApplyPrivileges(t *testing.T) {
	for _, mode := range []string{"A", "B"} {
		for _, kind := range []int{2, 3} {
			t.Run(fmt.Sprintf("%s-card-%d", mode, kind), func(t *testing.T) {
				a := newTestApp(t)
				now := time.Now().Unix()
				t.Setenv("TWILIGHT_TWO_FACTOR_KEY", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("w", 32))))
				u, e := a.store().CreateUser(store.User{Username: "window-card", Role: store.RoleNormal, Active: true, EmbyID: "player", TelegramID: 451234, ExpiredAt: now + 10*retention.Day})
				if e != nil {
					t.Fatal(e)
				}
				streams := 2
				remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					if r.Method == "POST" {
						var policy map[string]any
						_ = json.NewDecoder(r.Body).Decode(&policy)
						if value, ok := policy["SimultaneousStreamLimit"].(float64); ok {
							streams = int(value)
						}
						w.WriteHeader(204)
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"Id": "player", "Policy": map[string]any{"IsAdministrator": false, "IsDisabled": false, "SimultaneousStreamLimit": streams}})
				}))
				defer remote.Close()
				a.cfg().EmbyURL = remote.URL
				a.cfg().EmbyToken = "fixture-key"
				a.cfg().MediaLimitsEnabled = true
				a.cfg().MediaNormalStreams = 2
				a.cfg().MediaWhitelistStreams = 3
				if e = a.store().SetRetentionEnabled(true, 1); e != nil {
					t.Fatal(e)
				}
				if _, e = a.store().RetentionPreview(u.UID, mode, "choice", now); e != nil {
					t.Fatal(e)
				}
				if e = a.store().RetentionConfirm(u.UID, "choice", false, mode == "A", 300, now); e != nil {
					t.Fatal(e)
				}
				originalEnd := u.ExpiredAt
				code := "JW-P-" + strings.Repeat("B", 32)
				digest, e := a.shopDigest("code", code)
				if e != nil {
					t.Fatal(e)
				}
				if e = a.store().UpsertRegCode(store.RegCode{Code: digest, Type: kind, Days: 30, ValidityTime: 24, UseCountLimit: 1, Active: true, Source: "commerce", TargetUID: u.UID, TargetTelegramID: u.TelegramID, CreatedAt: now}); e != nil {
					t.Fatal(e)
				}
				request := httptest.NewRequest("POST", "/redeem", strings.NewReader(fmt.Sprintf(`{"code":%q}`, code)))
				request = request.WithContext(context.WithValue(request.Context(), principalKey, principal{User: u}))
				w := httptest.NewRecorder()
				a.handleUseCode(w, request, nil)
				if w.Code != 200 {
					t.Fatal("redeem", w.Code, w.Body)
				}
				u, _ = a.store().User(u.UID)
				view, _ := a.store().RetentionView(u.UID, time.Now().Unix())
				if u.ExpiredAt != originalEnd+30*retention.Day || view.Member.Member.Participation || view.Member.Member.Consent || view.ChoiceLocked || len(view.Ledger) != 0 {
					t.Fatal("old window or debit authorization survived card", view.Member)
				}
				if kind == 2 && (u.Role != store.RoleNormal || view.Selectable || view.SelectableAt != originalEnd+1) {
					t.Fatal("renewal exemption not applied", view.SelectableAt, u.Role)
				}
				if kind == 3 {
					if u.Role != store.RoleWhitelist || !view.Member.Member.Permanent || u.WhitelistUntil < now+30*retention.Day || u.WhitelistUntil > time.Now().Unix()+30*retention.Day || streams != 3 {
						t.Fatal("whitelist privileges or duration wrong", u.Role, u.WhitelistUntil, streams)
					}
				}

				// Ordinary renewal while temporary whitelist is active extends viewing,
				// never the more privileged whitelist deadline.
				if kind == 3 {
					whiteUntil, before := u.WhitelistUntil, u.ExpiredAt
					renewal := "TW-REN-" + strings.Repeat("C", 20)
					if e = a.store().UpsertRegCode(store.RegCode{Code: renewal, Type: 2, Days: 30, ValidityTime: 24, UseCountLimit: 1, Active: true, CreatedAt: now}); e != nil {
						t.Fatal(e)
					}
					r := httptest.NewRequest("POST", "/redeem", strings.NewReader(fmt.Sprintf(`{"code":%q}`, renewal)))
					r = r.WithContext(context.WithValue(r.Context(), principalKey, principal{User: u}))
					response := httptest.NewRecorder()
					a.handleUseCode(response, r, nil)
					after, _ := a.store().User(u.UID)
					if response.Code != 200 || after.Role != store.RoleWhitelist || after.WhitelistUntil != whiteUntil || after.ExpiredAt != before+30*retention.Day {
						t.Fatal("ordinary renewal extended whitelist", response.Code, after.WhitelistUntil)
					}
				}
			})
		}
	}
}
