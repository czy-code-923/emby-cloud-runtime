package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prejudice-studio/twilight/internal/store"
)

func TestJianweiViewingDurationAndShorterPanelPreserveInformation(t *testing.T) {
	for seconds, expected := range map[int64]string{0: "0秒", 59: "59秒", 60: "1分0秒", 3661: "1小时1分1秒"} {
		if actual := memberViewingDuration(seconds); actual != expected {
			t.Fatal(seconds, actual)
		}
	}
	if lines := strings.Count(memberPanelCaption("见微\n欢迎", 6, false), "\n") + 1; lines != 6 {
		t.Fatal("primary panel did not become two lines shorter", lines)
	}
	text := "见微 · 我的 Emby\n\n· 📊 当前状态 | 已开通\n· 💠 Emby账号 | fixture\n· 🏷 账户类别 | 管理员\n· 🍒 可用积分 | 31\n· 🗓 有效期至 | 永久\n· 🔐 安全码 | 尚未设置\n· 🎬 近30天观影 | 1小时1分1秒\n· 🕒 最后观影 | 2026-11-28 01:00:00（北京时间）\n采集暂未更新，仅显示已确认记录"
	caption := memberPanelCaption(text, 3, false)
	if !strings.Contains(caption, "近30天观影 | 1小时1分1秒") || !strings.Contains(caption, "最后观影 | 2026-11-28 01:00:00") {
		t.Fatal("new account facts were truncated", caption)
	}
}

func TestJianweiViewingAccountAndBotUseSameVerifiedOwnRecords(t *testing.T) {
	a := newTestApp(t)
	a.cfg().TelegramMode = true
	a.cfg().TelegramBotToken = "fixture-viewing"
	u, err := a.store().CreateUser(store.User{Username: "viewing-admin", TelegramID: 53457, Role: store.RoleAdmin, Active: true, EmbyID: "fixture-viewing", EmbyUsername: "fixture", ExpiredAt: -1})
	if err != nil {
		t.Fatal(err)
	}
	if err = a.store().SetRetentionEnabled(true, a.store().RetentionPolicy().Revision); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	for _, at := range []int64{now - 60, now - 30} {
		if err = a.store().ObserveRetention([]store.RetentionObservation{{UID: u.UID, Session: "fixture", Item: "movie", Playing: true, Position: at - now + 60}}, at); err != nil {
			t.Fatal(err)
		}
	}
	account := a.memberAccount(u)
	v := account["viewing"].(store.MemberViewing)
	if v.Seconds != 30 || v.LastWatchedAt != now-30 || v.Source != "emby_server_session_samples" || !v.HasWindowRecords {
		t.Fatal("administrator's real viewing disappeared or was fabricated", v)
	}
	var sent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		sent, _ = payload["text"].(string)
		w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer server.Close()
	a.cfg().TelegramAPIURL = server.URL
	a.telegramMemberAccountInfo(context.Background(), telegramCommandCtx{FromID: u.TelegramID, ChatID: u.TelegramID})
	expectedLast := time.Unix(now-30, 0).In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02 15:04:05")
	if !strings.Contains(sent, "近30天观影 | 30秒") || !strings.Contains(sent, "最后观影 | "+expectedLast) {
		t.Fatal("Bot and Mini App viewing facts differ", sent)
	}
}
