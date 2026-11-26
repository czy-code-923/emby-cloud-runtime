package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prejudice-studio/twilight/internal/store"
)

func TestJianweiRegistrationEligibilityKeepsCodeUntilMembershipVerified(t *testing.T) {
	a := newTestApp(t)
	a.cfg().TelegramMode = true
	a.cfg().TelegramBotToken = "fixture"
	a.cfg().TelegramForceBindGroup = true
	a.cfg().TelegramGroupIDs = []string{"-1001122334"}
	status := "left"
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if status == "unavailable" {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"status":"` + status + `","user":{"id":99887766,"is_bot":false}}}`))
	}))
	defer remote.Close()
	a.cfg().TelegramAPIURL = remote.URL
	u, err := a.store().CreateUser(store.User{Username: "eligibility-fixture", Role: store.RoleNormal, Active: true, TelegramID: 99887766})
	if err != nil {
		t.Fatal(err)
	}
	const code = "TW-REG-ELIGIBILITYFIXTURE"
	if err = a.store().UpsertRegCode(store.RegCode{Code: code, Type: 1, Active: true, Days: 30, UseCountLimit: 1, ValidityTime: 24, CreatedAt: time.Now().Unix()}); err != nil {
		t.Fatal(err)
	}
	request := func(handler HandlerFunc, body string) *httptest.ResponseRecorder {
		currentUser, _ := a.store().User(u.UID)
		r := httptest.NewRequest(http.MethodPost, "/member-action", strings.NewReader(body))
		r = r.WithContext(context.WithValue(r.Context(), principalKey, principal{User: currentUser}))
		w := httptest.NewRecorder()
		handler(w, r, nil)
		return w
	}
	for _, expected := range []struct {
		status string
		code   int
	}{{"left", 403}, {"unavailable", 503}} {
		status = expected.status
		w := request(a.handleUseCode, `{"code":"`+code+`"}`)
		saved, _ := a.store().RegCode(code)
		if w.Code != expected.code || saved.UseCount != 0 {
			t.Fatal("unverified membership consumed code", w.Code, w.Body.String())
		}
	}
	status = "member"
	w := request(a.handleUseCode, `{"code":"`+code+`"}`)
	if w.Code != 200 {
		t.Fatal("verified member could not claim eligibility", w.Code, w.Body.String())
	}
	saved, _ := a.store().RegCode(code)
	if saved.UseCount != 1 {
		t.Fatal("verified claim did not consume exactly once")
	}
	status = "left"
	w = request(a.handleRegisterEmby, `{"emby_username":"fixture","emby_password":"Fixture12345!"}`)
	if w.Code != 403 {
		t.Fatal("Emby creation skipped a fresh membership check", w.Code, w.Body.String())
	}
}
