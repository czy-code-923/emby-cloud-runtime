package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestJianweiCopyOutputKeepsOriginalRowsAcrossTextAndPhoto(t *testing.T) {
	a := newTestApp(t)
	a.cfg().TelegramMode = true
	a.cfg().TelegramBotToken = "fixture-copy-layout"
	requests := []map[string]any{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		requests = append(requests, payload)
		w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer server.Close()
	a.cfg().TelegramAPIURL = server.URL
	for _, photo := range []bool{false, true} {
		rows := [][]map[string]any{{{"text": "复制账号", "copy_text": map[string]string{"text": "member_<见微>&"}}, memberButton("修改密码", "/password")}, {{"text": "复制线路", "copy_text": map[string]string{"text": "https://play.example.test"}}}}
		original, _ := json.Marshal(append(rows, []map[string]any{memberButton("‹ 返回主面板", "/panel")}))
		ctx := context.WithValue(context.Background(), memberPanelContextKey{}, memberPanelTarget{ChatID: 42, MessageID: 9, Photo: photo})
		a.memberSendButtons(ctx, 42, "🎬 我的线路\n账号 member_<见微>&\nhttps://play.example.test", rows)
		payload := requests[len(requests)-1]
		keyboard, _ := json.Marshal(payload["reply_markup"].(map[string]any)["inline_keyboard"])
		if string(keyboard) != string(original) {
			t.Fatal("original keyboard changed", string(keyboard))
		}
		entityField := "entities"
		if photo {
			entityField = "caption_entities"
		}
		if len(payload[entityField].([]any)) != 2 || payload["message_id"] != float64(9) {
			t.Fatal("copy entities or same-message navigation lost")
		}
	}
	if len(requests) != 2 {
		t.Fatal("copy formatting sent an additional message", len(requests))
	}
}

func TestJianweiCopyEntitiesPreserveExactValuesAndOriginalKeyboard(t *testing.T) {
	values := []string{"member_<见微>&", "https://play.example.test:443/path?a=1&b=2", "P<&`密🔑码", "LOGIN-CODE"}
	rows := [][]map[string]any{}
	for _, value := range values {
		rows = append(rows, []map[string]any{{"text": "复制", "copy_text": map[string]string{"text": value}}})
	}
	original, _ := json.Marshal(rows)
	text := "🎬 见微\n账号：" + values[0] + "\n线路：" + values[1] + "\n密码：" + values[2]
	text = memberCopyText(text, rows)
	entities := memberCopyEntities(text, rows)
	if len(entities) != len(values) {
		t.Fatal("a copyable value lost its entity", entities)
	}
	units := utf16.Encode([]rune(text))
	found := map[string]bool{}
	lastEnd := 0
	for _, entity := range entities {
		end := entity.Offset + entity.Length
		if entity.Type != "code" || entity.Offset < lastEnd || end > len(units) {
			t.Fatal("invalid Telegram UTF-16 entity", entity)
		}
		found[string(utf16.Decode(units[entity.Offset:end]))] = true
		lastEnd = end
	}
	for _, value := range values {
		if !found[value] || strings.Count(text, value) != 1 {
			t.Fatal("wrong or duplicated clipboard value", value)
		}
	}
	after, _ := json.Marshal(rows)
	if string(original) != string(after) || strings.Contains(text, "复制帮助") || strings.Contains(text, "第三方客户端") {
		t.Fatal("copy formatting changed the original keyboard or added help")
	}
	wire, err := json.Marshal(telegramSendMessageRequest{ChatID: 42, Text: text, Entities: entities, ReplyMarkup: map[string]any{"inline_keyboard": rows}})
	if err != nil || !strings.Contains(string(wire), `"entities"`) || strings.Contains(string(wire), `"parse_mode"`) {
		t.Fatal("copyable literal values were not transported as entities", err)
	}
}

func TestJianweiCopyEntitiesDoNotOverlapOrExceedPhotoCaption(t *testing.T) {
	url := "https://cc.example.test/play"
	rows := [][]map[string]any{{{"text": "复制账号", "copy_text": map[string]string{"text": "cc"}}}, {{"text": "复制线路", "copy_text": map[string]string{"text": url}}}}
	text := "🎬 账号 cc\n线路 " + url + "\n" + strings.Repeat("额外说明\n", 30)
	for _, photo := range []bool{false, true} {
		ctx := context.WithValue(context.Background(), memberPanelContextKey{}, memberPanelTarget{ChatID: 42, MessageID: 9, Photo: photo})
		request := telegramSendMessageRequest{ChatID: int64(42), Text: text, Entities: memberCopyEntities(text, rows), ReplyMarkup: map[string]any{"inline_keyboard": rows}}
		method, raw := (&App{}).memberPanelEdit(ctx, "sendMessage", request)
		body := raw.(map[string]any)
		if !reflect.DeepEqual(body["reply_markup"], request.ReplyMarkup) {
			t.Fatal("editing changed button count, order or action")
		}
		field, entityField, expectedMethod := "text", "entities", "editMessageText"
		if photo {
			field, entityField, expectedMethod = "caption", "caption_entities", "editMessageCaption"
			if _, exists := body["entities"]; exists {
				t.Fatal("photo edit used text entities")
			}
		}
		if method != expectedMethod || body["message_id"] != int64(9) {
			t.Fatal("copy panel stopped editing the original message")
		}
		actualText := body[field].(string)
		units := utf16.Encode([]rune(actualText))
		lastEnd := 0
		for _, entity := range body[entityField].([]telegramMessageEntity) {
			end := entity.Offset + entity.Length
			if entity.Offset < lastEnd || end > len(units) {
				t.Fatal("caption entity was not recomputed after truncation", entity)
			}
			lastEnd = end
		}
	}
}
