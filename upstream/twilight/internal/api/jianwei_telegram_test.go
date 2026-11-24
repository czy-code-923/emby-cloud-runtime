package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"
)

func miniTestProof(token string, now int64) string {
	values := url.Values{"auth_date": {fmt.Sprint(now)}, "query_id": {"test-query"}, "user": {`{"id":9123,"username":"member"}`}}
	parts := []string{}
	for key := range values {
		parts = append(parts, key+"="+values.Get(key))
	}
	sort.Strings(parts)
	key := hmac.New(sha256.New, []byte("WebAppData"))
	key.Write([]byte(token))
	mac := hmac.New(sha256.New, key.Sum(nil))
	mac.Write([]byte(strings.Join(parts, "\n")))
	values.Set("hash", hex.EncodeToString(mac.Sum(nil)))
	return values.Encode()
}

func TestMiniAppSessionAudienceCanonicalReplayAndRotation(t *testing.T) {
	app := newTestApp(t)
	app.cfg().TelegramMode = true
	app.cfg().TelegramBotToken = "fixture-token"
	u, _, err := app.store().EnsureTelegramProfile(9123, "member", "unusable-fixture", -1)
	if err != nil {
		t.Fatal(err)
	}
	proof := miniTestProof("fixture-token", time.Now().Unix())
	nonce := strings.Repeat("a", 64)
	login := func(raw, requestID string) *http.Response {
		body, _ := json.Marshal(map[string]string{"init_data": raw, "request_id": requestID})
		return doJSON(app, http.MethodPost, "/api/v2/miniapp/session", string(body), nil).Result()
	}
	first := login(proof, nonce)
	defer first.Body.Close()
	if first.StatusCode != http.StatusOK || len(first.Cookies()) != 1 {
		t.Fatal("valid proof did not establish a session")
	}
	cookie := first.Cookies()[0]
	if !cookie.Secure || !cookie.HttpOnly || cookie.Path != "/api/v2/miniapp" || cookie.SameSite != http.SameSiteNoneMode {
		t.Fatal("unsafe mini session cookie")
	}
	parts := strings.Split(proof, "&")
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	reordered := strings.Join(parts, "&")
	retry := login(reordered, nonce)
	defer retry.Body.Close()
	if retry.StatusCode != http.StatusOK || retry.Cookies()[0].Value != cookie.Value {
		t.Fatal("same browser retry must canonicalize the proof")
	}
	replay := login(reordered, strings.Repeat("b", 64))
	defer replay.Body.Close()
	if replay.StatusCode != http.StatusConflict {
		t.Fatal("signature-equivalent replay created another session")
	}
	account := doJSON(app, http.MethodGet, "/api/v2/miniapp/account", "", []*http.Cookie{cookie})
	if account.Code != http.StatusOK || !strings.Contains(account.Body.String(), fmt.Sprintf(`"uid":%d`, u.UID)) {
		t.Fatal("valid mini session did not identify its owner")
	}
	admin := doJSON(app, http.MethodGet, "/api/v2/admin/stats", "", []*http.Cookie{cookie})
	if admin.Code != http.StatusUnauthorized {
		t.Fatal("mini session acquired Web privileges")
	}
	webCookies := registerAndLogin(t, app, "web-reader", "Reader123456")
	account = doJSON(app, http.MethodGet, "/api/v2/miniapp/account", "", webCookies)
	if account.Code != http.StatusUnauthorized {
		t.Fatal("Web session crossed the Mini App audience")
	}
	app.cfg().TelegramBotToken = "rotated-fixture-token"
	account = doJSON(app, http.MethodGet, "/api/v2/miniapp/account", "", []*http.Cookie{cookie})
	if account.Code != http.StatusUnauthorized {
		t.Fatal("old mini session survived Bot rotation")
	}
}

func TestMiniAppSignatureExpiryAndTampering(t *testing.T) {
	now := int64(1800000000)
	proof := miniTestProof("fixture-token", now)
	user, err := miniAppIdentity(proof, "fixture-token", now)
	if err != nil || user.ID != 9123 {
		t.Fatal("valid Telegram proof rejected")
	}
	for _, tc := range []struct {
		raw, token string
		at         int64
	}{
		{proof, "wrong-token", now}, {proof, "fixture-token", now + 301}, {proof, "fixture-token", now - 31},
		{proof + "&user=attacker", "fixture-token", now}, {strings.Replace(proof, "member", "attacker", 1), "fixture-token", now},
		{"", "fixture-token", now}, {strings.Repeat("x", 8193), "fixture-token", now},
	} {
		if _, err := miniAppIdentity(tc.raw, tc.token, tc.at); err == nil {
			t.Fatal("invalid/replayed input accepted")
		}
	}
}
