package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"image"
	"io"
	"net/http"
	"path"
	"strings"
	"time"
)

type miniTelegramAvatar struct {
	Data []byte
	MIME string
}

var miniTelegramAvatars = newTTLCache(64, 5*time.Minute)

// The browser sees only its own private resource URL, never Telegram's file
// URL containing the Bot token. Privacy-hidden avatars fall back to branding.
func (a *App) handleMiniTelegramAvatar(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Vary", "Cookie")
	user := current(r).User
	key := fmt.Sprintf("%x:%d", sha256.Sum256([]byte(a.cfg().TelegramBotToken)), user.TelegramID)
	avatar, hit := miniTelegramAvatars.get(key)
	if !hit {
		ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
		defer cancel()
		picture := a.readTelegramAvatar(ctx, user.TelegramID)
		miniTelegramAvatars.set(key, picture)
		avatar = picture
	}
	picture, _ := avatar.(miniTelegramAvatar)
	if len(picture.Data) == 0 {
		http.Redirect(w, r, "/api/v2/system/server-icon", http.StatusTemporaryRedirect)
		return
	}
	w.Header().Set("Content-Type", picture.MIME)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(picture.Data)
}

func (a *App) readTelegramAvatar(ctx context.Context, telegramID int64) miniTelegramAvatar {
	var empty miniTelegramAvatar
	if telegramID <= 0 {
		return empty
	}
	photos, err := telegramPostResult[struct {
		Photos [][]struct {
			ID    string `json:"file_id"`
			Width int    `json:"width"`
		} `json:"photos"`
	}](a, ctx, "getUserProfilePhotos", map[string]any{"user_id": telegramID, "limit": 1}, 4*time.Second)
	if err != nil || len(photos.Photos) == 0 || len(photos.Photos[0]) == 0 {
		return empty
	}
	sizes := photos.Photos[0]
	chosen := sizes[0]
	for _, size := range sizes {
		if size.Width <= 512 && size.Width > chosen.Width {
			chosen = size
		}
	}
	file, err := telegramPostResult[struct {
		Path string `json:"file_path"`
	}](a, ctx, "getFile", map[string]string{"file_id": chosen.ID}, 4*time.Second)
	if err != nil || len(file.Path) > 256 || file.Path == "" || path.Clean(file.Path) != file.Path || strings.HasPrefix(file.Path, "/") || strings.ContainsAny(file.Path, "?\\#") || strings.HasPrefix(file.Path, "..") {
		return empty
	}
	endpoint, err := a.telegramEndpoint("getFile")
	if err != nil {
		return empty
	}
	token := strings.TrimSpace(a.cfg().TelegramBotToken)
	suffix := "/bot" + token + "/getFile"
	if !strings.HasSuffix(endpoint, suffix) {
		return empty
	}
	address := strings.TrimSuffix(endpoint, suffix) + "/file/bot" + token + "/" + file.Path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return empty
	}
	response, err := sharedHTTPClient.Do(req)
	if err != nil {
		return empty
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return empty
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (256<<10)+1))
	if err != nil || len(data) > 256<<10 {
		return empty
	}
	info, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || info.Width < 1 || info.Height < 1 || info.Width > 1024 || info.Height > 1024 || (format != "jpeg" && format != "png") {
		return empty
	}
	mime := "image/jpeg"
	if format == "png" {
		mime = "image/png"
	}
	return miniTelegramAvatar{Data: data, MIME: mime}
}
