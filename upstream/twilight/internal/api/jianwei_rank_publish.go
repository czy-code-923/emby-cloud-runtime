package api

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func (a *App) memberRankPublication(ctx context.Context) map[string]any {
	q := playRankRequest{rangeKey: "week", since: playRankWindow(time.Now(), "week"), limit: 20, groupBy: playRankGroupSeries, sortBy: playRankSortPlays}
	return a.playRankData(ctx, q, false, false)
}
func (a *App) handleRankExport(w http.ResponseWriter, r *http.Request, _ Params) {
	data := a.memberRankPublication(r.Context())
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=jianwei-weekly-playrank.csv")
	_, _ = w.Write([]byte{0xef, 0xbb, 0xbf})
	out := csv.NewWriter(w)
	_ = out.Write([]string{"排名", "作品", "播放次数", "观看秒数", "观看人数"})
	if media, ok := data["media"].([]map[string]any); ok {
		for i, item := range media {
			_ = out.Write([]string{fmt.Sprint(i + 1), csvSafe(asString(item["title"])), fmt.Sprint(item["plays"]), fmt.Sprint(item["duration"]), fmt.Sprint(item["viewers"])})
		}
	}
	out.Flush()
}
func (a *App) runRankPublish(ctx context.Context) (map[string]any, []string, error) {
	cfg := a.cfg()
	if !cfg.PlayRankPublishEnabled || !cfg.PlayRankEnabled {
		return map[string]any{"success": true, "skipped": true, "reason": "publication disabled"}, nil, nil
	}
	target := strings.TrimSpace(cfg.PlayRankPublishTarget)
	approved := false
	for _, id := range append(append([]string{}, cfg.TelegramGroupIDs...), cfg.TelegramChannelIDs...) {
		if target == id && target != "" {
			approved = true
		}
	}
	if !approved {
		return nil, nil, fmt.Errorf("rank target must be an explicitly configured group or channel")
	}
	data := a.memberRankPublication(ctx)
	text := "见微 Emby · 本周观影榜\n\n"
	count := 0
	if media, ok := data["media"].([]map[string]any); ok {
		for i, item := range media {
			if i >= 10 {
				break
			}
			text += fmt.Sprintf("%02d  %s · %v次\n", i+1, truncateString(asString(item["title"]), 60), item["plays"])
			count++
		}
	}
	if count == 0 {
		return map[string]any{"success": true, "skipped": true, "reason": "no playback records"}, nil, nil
	}
	text += "\n按服务器已记录数据统计；不公布观看者身份。"
	_, err := telegramPostResult[telegramDiscardResult](a, ctx, "sendMessage", telegramSendMessageRequest{ChatID: target, Text: text, DisableWebPagePreview: true}, 15*time.Second)
	return map[string]any{"success": err == nil, "items": count}, nil, err
}
