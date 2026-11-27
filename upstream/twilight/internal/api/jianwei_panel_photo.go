package api

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"sync"
	"time"
)

// Keep the built-in hero available when Telegram cannot fetch the Cloudflare
// public URL. Custom advertisements continue to use their configured artwork.
//
//go:embed assets/jianwei-panel-v4.png
var memberHeroPNG []byte
var memberHeroIDs sync.Map

func (a *App) sendBuiltinMemberHero(ctx context.Context, chatID int64, caption string, markup any) error {
	endpoint, e := a.telegramEndpoint("sendPhoto")
	if e != nil {
		return e
	}
	// The URL identifies the bot without keeping the raw token as a map key.
	key := a.mediaTokenHash(endpoint)
	if file, ok := memberHeroIDs.Load(key); ok {
		_, e = telegramPostResult[telegramDiscardResult](a, ctx, "sendPhoto", map[string]any{"chat_id": chatID, "photo": file, "caption": caption, "reply_markup": markup}, 8*time.Second)
		if e == nil {
			return nil
		}
		memberHeroIDs.Delete(key)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("chat_id", fmt.Sprint(chatID))
	_ = writer.WriteField("caption", caption)
	raw, _ := json.Marshal(markup)
	_ = writer.WriteField("reply_markup", string(raw))
	part, e := writer.CreateFormFile("photo", "jianwei-panel-v4.png")
	if e != nil {
		return e
	}
	_, _ = part.Write(memberHeroPNG)
	_ = writer.Close()
	photoCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	request, e := http.NewRequestWithContext(photoCtx, "POST", endpoint, &body)
	if e != nil {
		return e
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, e := sharedHTTPClient.Do(request)
	if e != nil {
		return e
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		return telegramDecodeErrorResponse(response, "sendPhoto")
	}
	result, e := decodeTelegramEnvelope[struct {
		Photo []struct {
			FileID string `json:"file_id"`
		} `json:"photo"`
	}](response.Body)
	if e != nil {
		return e
	}
	if !result.OK {
		return fmt.Errorf("Telegram photo upload rejected")
	}
	if len(result.Result.Photo) > 0 {
		memberHeroIDs.Store(key, result.Result.Photo[len(result.Result.Photo)-1].FileID)
	}
	return nil
}
