package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/prejudice-studio/twilight/internal/config"
	"github.com/prejudice-studio/twilight/internal/retention"
	"github.com/prejudice-studio/twilight/internal/store"
)

func TestJianweiNativeMenuHonorsIndependentFeatureSwitches(t *testing.T) {
	a := newTestApp(t)
	a.cfg().TelegramMode = true
	a.cfg().TelegramBotToken = "fixture"
	a.cfg().SigninEnabled = true
	a.cfg().SigninRenewalEnabled = false
	a.cfg().InviteEnabled = false
	a.cfg().MediaRequestEnabled = false
	var messages []map[string]any
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		messages = append(messages, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer remote.Close()
	a.cfg().TelegramAPIURL = remote.URL
	u, err := a.store().CreateUser(store.User{Username: "panel-fixture", TelegramID: 4321, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	a.telegramMemberPanel(context.Background(), u.TelegramID, u)
	if len(messages) != 1 {
		t.Fatal("panel did not reach Bot transport")
	}
	text := asString(messages[0]["text"]) + asString(messages[0]["caption"])
	if strings.Contains(text, "4321") || strings.Contains(text, "panel-fixture") {
		t.Fatal("home leaked account information before query")
	}
	panel, _ := json.Marshal(messages[0]["reply_markup"])
	if !strings.Contains(string(panel), "jw:/retention") || strings.Contains(string(panel), "jw:/renew") || strings.Contains(string(panel), "jw:/invite") || strings.Contains(string(panel), "jw:/request") {
		t.Fatal("panel advertised a disabled feature", string(panel))
	}
	if err := a.publishTelegramMemberCommands(context.Background()); err != nil {
		t.Fatal(err)
	}
	menu, _ := json.Marshal(messages[len(messages)-1]["commands"])
	if !strings.Contains(string(menu), `"command":"signin"`) || strings.Contains(string(menu), `"command":"renew"`) || strings.Contains(string(menu), `"command":"retention_confirm"`) || strings.Contains(string(menu), `"command":"invite"`) {
		t.Fatal("slash menu advertised a hidden or disabled feature", string(menu))
	}
}

func TestJianweiNativeRetentionSummaryShowsFutureAndCollectionGap(t *testing.T) {
	now := time.Now().Unix()
	v := store.RetentionView{Policy: retention.DefaultPolicy(), Start: now + 3600, End: now + 31*86400, Quality: "stale"}
	v.Policy.Enabled = true
	v.Member.Member = retention.Member{PlaybackEnd: v.End, Mode: "B", Participation: true, NextSwitchAt: now + 86400}
	text := retentionNativeText(v)
	for _, expected := range []string{"下一窗尚未开始", "采样已过期", "窗口起止", "已锁定，仅管理员可修改"} {
		if !strings.Contains(text, expected) {
			t.Fatal("missing member-facing state:", expected)
		}
	}
	if strings.Contains(text, "可更换于") {
		t.Fatal("locked selection advertised a future member switch")
	}
	v.Policy.Enabled = false
	if !strings.Contains(retentionNativeText(v), "不会自动扣分或按观影续期") {
		t.Fatal("inactive policy was presented as active")
	}
}

func TestJianweiSigninRetryKeepsOriginalDailyReceipt(t *testing.T) {
	date := time.Now().In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02")
	si := store.Signin{Points: 3600, Records: []store.SigninRecord{{Date: date, RawAward: 20, Points: 5, Total: 5, CapWithheld: 15}}}
	first := signinActionPayload(config.Config{}, si, true, 5, 0)
	retry := signinActionPayload(config.Config{}, si, false, 0, 0)
	for _, payload := range []map[string]any{first, retry} {
		award, ok := payload["today_award"].(map[string]any)
		if !ok || award["raw_points"] != 20 || award["credited_points"] != 5 || award["withheld_points"] != 15 {
			t.Fatal("daily receipt lost original result", payload)
		}
	}
	if retry["total_today"] != 0 {
		t.Fatal("retry appears to have granted a second award")
	}
}

func TestJianweiCaptionBoundsAndCompactProtectedRetention(t *testing.T) {
	for _, rows := range []int{1, 3, 6, 10} {
		caption := memberPanelCaption("<b>见微</b>\n"+strings.Repeat("🍒测试", 800), rows, true)
		if len(utf16.Encode([]rune(caption))) > 1024 || strings.Contains(caption, "<b>") {
			t.Fatal("invalid Telegram caption", rows)
		}
	}
	primary := memberPanelCaption("见微\n欢迎回来", 6, false)
	secondary := memberPanelCaption("见微\n账户设置", 2, false)
	if strings.Count(primary, "\n") >= strings.Count(secondary, "\n") {
		t.Fatal("secondary layout lacks information space")
	}
	v := store.RetentionView{}
	v.Member.Member.Permanent = true
	text := retentionNativeText(v)
	if !strings.Contains(text, "无需保号") || strings.Contains(text, "窗口起止") {
		t.Fatal("protected account has irrelevant retention controls", text)
	}
}
