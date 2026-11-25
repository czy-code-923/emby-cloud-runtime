package api

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func (a *App) handleMemberAnnouncements(w http.ResponseWriter, r *http.Request, _ Params) {
	all := a.store().ListAnnouncements(false)
	page := clamp(queryInt(r, "page", 1), 1, 100000)
	size := clamp(queryInt(r, "page_size", 20), 1, 20)
	start := min((page-1)*size, len(all))
	end := min(start+size, len(all))
	items := []map[string]any{}
	for _, item := range all[start:end] {
		items = append(items, map[string]any{"id": item.ID, "title": item.Title, "content": truncateString(item.Content, 8000), "pinned": item.Pinned, "created_at": item.CreatedAt})
	}
	ok(w, "OK", map[string]any{"items": items, "page": page, "has_next": end < len(all)})
}
func (a *App) handleMemberRank(w http.ResponseWriter, r *http.Request, _ Params) {
	cfg := a.cfg()
	if !cfg.PlayRankEnabled || !cfg.PlayRankUserVisible {
		fail(w, 403, "播放排行尚未对会员开放")
		return
	}
	// A small, cached public projection never includes full names, tokens,
	// internal URLs or administrator-only refresh controls.
	q := playRankRequest{rangeKey: "week", since: playRankWindow(time.Now(), "week"), limit: 20, groupBy: playRankGroupSeries, sortBy: playRankSortPlays}
	data := a.playRankData(r.Context(), q, false, false)
	items := []map[string]any{}
	if media, ok := data["media"].([]map[string]any); ok {
		for _, m := range media {
			items = append(items, map[string]any{"title": m["title"], "plays": m["plays"], "duration": m["duration"], "viewers": m["viewers"]})
		}
	}
	ok(w, "OK", map[string]any{"items": items, "range": "week", "updated_at": data["updated_at"]})
}

type memberLibraryItem struct {
	ID             string `json:"Id"`
	Name           string `json:"Name"`
	Type           string `json:"Type"`
	ProductionYear int    `json:"ProductionYear"`
	UserData       struct {
		IsFavorite bool `json:"IsFavorite"`
	} `json:"UserData"`
}

func (a *App) handleMemberLibrary(w http.ResponseWriter, r *http.Request, _ Params) {
	u := current(r).User
	if u.EmbyID == "" {
		fail(w, 409, "请先开通 Emby")
		return
	}
	if !a.allowRate(r.Context(), rateKey("member-library:", u.UID), 20, time.Minute) {
		fail(w, 429, "查询过于频繁，请稍后再试")
		return
	}
	favorites := !strings.HasSuffix(r.URL.Path, "/library")
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if !favorites && (len([]rune(query)) < 2 || len(query) > 120) {
		fail(w, 400, "请输入2至120个字符的片名")
		return
	}
	page := clamp(queryInt(r, "page", 1), 1, 5000)
	size := clamp(queryInt(r, "page_size", 20), 1, 20)
	values := url.Values{"Recursive": {"true"}, "Limit": {strconv.Itoa(size)}, "StartIndex": {strconv.Itoa((page - 1) * size)}, "EnableImages": {"false"}, "EnableUserData": {"true"}, "IncludeItemTypes": {"Movie,Series"}, "SortBy": {"SortName"}}
	if favorites {
		values.Set("IsFavorite", "true")
	} else {
		values.Set("SearchTerm", query)
	}
	var result struct {
		Items            []memberLibraryItem `json:"Items"`
		TotalRecordCount int                 `json:"TotalRecordCount"`
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := a.embyGet(ctx, "/Users/"+url.PathEscape(u.EmbyID)+"/Items?"+values.Encode(), &result); err != nil {
		fail(w, 502, "暂时无法读取 Emby 收藏与媒体库，请稍后重试")
		return
	}
	items := []map[string]any{}
	for i, item := range result.Items {
		if i >= size {
			break
		}
		items = append(items, map[string]any{"id": item.ID, "title": item.Name, "type": item.Type, "year": item.ProductionYear, "favorite": item.UserData.IsFavorite})
	}
	ok(w, "OK", map[string]any{"items": items, "page": page, "total": result.TotalRecordCount, "has_next": page*size < result.TotalRecordCount})
}

var memberItemID = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

func (a *App) handleMemberFavorite(w http.ResponseWriter, r *http.Request, _ Params) {
	u := current(r).User
	if u.EmbyID == "" {
		fail(w, 409, "请先开通 Emby")
		return
	}
	body := decodeMap(r)
	id := stringValue(body, "item_id")
	favorite, present, valid := requireStrictBoolValue(w, body, "favorite")
	if !valid {
		return
	}
	if !present || !memberItemID.MatchString(id) {
		fail(w, 400, "收藏参数不正确")
		return
	}
	if !a.allowRate(r.Context(), rateKey("member-favorite:", u.UID), 15, time.Minute) {
		fail(w, 429, "操作过于频繁")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	prefix := "/Users/" + url.PathEscape(u.EmbyID)
	var item memberLibraryItem
	if err := a.embyGet(ctx, prefix+"/Items/"+id, &item); err != nil || item.ID != id {
		fail(w, 404, "未找到你可访问的媒体")
		return
	}
	var err error
	if favorite {
		err = a.embyPost(ctx, prefix+"/FavoriteItems/"+id, nil, nil)
	} else {
		err = a.embyDelete(ctx, prefix+"/FavoriteItems/"+id)
	}
	if err != nil {
		fail(w, 502, "收藏操作未完成，请刷新核对后重试")
		return
	}
	a.audit(r, "member_favorite_updated", "emby", u.UID, map[string]any{"item_id": id, "favorite": favorite})
	ok(w, "收藏已同步到 Emby", nil)
}

func (a *App) telegramDiscovery(ctx context.Context, c telegramCommandCtx) {
	u, found := a.store().FindUserByTelegramID(c.FromID)
	if !found || !u.Active {
		_ = a.telegramSendMessage(ctx, c.ChatID, "请先 /start 建立账户")
		return
	}
	var handler HandlerFunc
	title := ""
	body := map[string]any{}
	page := 1
	if len(c.Args) > 0 && strings.HasPrefix(c.Args[0], "page:") {
		page, _ = strconv.Atoi(strings.TrimPrefix(c.Args[0], "page:"))
		page = clamp(page, 1, 5000)
		c.Args = c.Args[1:]
	}
	switch c.Command {
	case "/announcements":
		handler = func(w http.ResponseWriter, r *http.Request, p Params) {
			r.URL.RawQuery = fmt.Sprintf("page=%d&page_size=5", page)
			a.handleMemberAnnouncements(w, r, p)
		}
		title = "见微公告"
	case "/rank":
		handler = a.handleMemberRank
		title = "本周播放排行"
	case "/favorites", "/library":
		title = "我的收藏"
		if c.Command == "/library" {
			title = "媒体库搜索"
			if len(c.Args) == 0 {
				_ = a.telegramSendMessage(ctx, c.ChatID, "输入 /library 片名 搜索媒体库；/favorites 查看 Emby 收藏。")
				return
			}
		}
		handler = func(w http.ResponseWriter, r *http.Request, p Params) {
			r.URL.Path = "/" + strings.TrimPrefix(c.Command, "/")
			q := url.Values{}
			q.Set("page", strconv.Itoa(page))
			q.Set("page_size", "10")
			if c.Command == "/library" {
				q.Set("q", strings.Join(c.Args, " "))
			}
			r.URL.RawQuery = q.Encode()
			a.handleMemberLibrary(w, r, p)
		}
	case "/favorite", "/unfavorite":
		if len(c.Args) != 1 {
			_ = a.telegramSendMessage(ctx, c.ChatID, "先用 /library 搜索，再发送 /favorite 媒体编号 收藏；/unfavorite 媒体编号 取消收藏。")
			return
		}
		handler = a.handleMemberFavorite
		body["item_id"] = c.Args[0]
		body["favorite"] = c.Command == "/favorite"
	}
	success, message, data := a.invokeMember(ctx, u, handler, body)
	if !success {
		_ = a.telegramSendMessage(ctx, c.ChatID, message)
		return
	}
	text := "<b>" + title + "</b>\n"
	rows := [][]map[string]any{}
	if title == "" {
		text = message
	} else {
		payload, _ := data.(map[string]any)
		items, _ := payload["items"].([]any)
		if len(items) == 0 {
			text += "暂无记录。"
		}
		for i, raw := range items {
			item, _ := raw.(map[string]any)
			text += fmt.Sprintf("\n%d. %s", i+1, html.EscapeString(truncateString(fmt.Sprint(item["title"]), 80)))
			if content, ok := item["content"].(string); ok {
				text += "\n" + html.EscapeString(truncateString(content, 220))
			}
			if id, ok := item["id"].(string); ok {
				text += " · " + html.EscapeString(id)
			}
			if plays, ok := item["plays"]; ok {
				text += fmt.Sprintf(" · %v 次播放", plays)
			}
			text += "\n"
		}
		if c.Command != "/rank" {
			nav := []map[string]any{}
			add := func(label string, next int) {
				cmd := fmt.Sprintf("%s page:%d", c.Command, next)
				if c.Command == "/library" {
					cmd += " " + strings.Join(c.Args, " ")
				}
				if len("jw:"+cmd) <= 64 {
					nav = append(nav, map[string]any{"text": label, "callback_data": "jw:" + cmd})
				} else {
					text += "\n" + html.EscapeString(cmd)
				}
			}
			if page > 1 {
				add("‹ 上一页", page-1)
			}
			if hasNext, _ := payload["has_next"].(bool); hasNext {
				add("下一页 ›", page+1)
			}
			if len(nav) > 0 {
				rows = append(rows, nav)
			}
			text += fmt.Sprintf("\n第 %d 页", page)
		}
		if c.Command == "/favorites" || c.Command == "/library" {
			text += "\n/favorite 编号 · /unfavorite 编号"
		}
	}
	rows = append(rows, []map[string]any{{"text": "‹ 返回主面板", "callback_data": "jw:/panel"}})
	if a.cfg().TelegramShopEnabled {
		if target := publicHTTPSURL(a.cfg().CommerceStoreURL); target != "" {
			rows = append(rows, []map[string]any{{"text": "🛍 发卡商店 ↗", "url": target}})
		}
	}
	_, _ = telegramPostResult[telegramDiscardResult](a, ctx, "sendMessage", telegramSendMessageRequest{ChatID: c.ChatID, Text: text, ParseMode: "HTML", DisableWebPagePreview: true, ReplyMarkup: map[string]any{"inline_keyboard": rows}}, 20*time.Second)
}
func init() {
	for i, item := range []struct{ name, description string }{{"announcements", "查看见微公告"}, {"rank", "本周播放排行"}, {"favorites", "我的 Emby 收藏"}, {"library", "搜索媒体库"}, {"favorite", "收藏媒体"}, {"unfavorite", "取消收藏"}} {
		name := "/" + item.name
		telegramCommandRegistry[name] = telegramCommandSpec{name: item.name, label: name, description: item.description, usage: name, category: "user", order: 52 + i, private: true, handler: func(a *App, ctx context.Context, c telegramCommandCtx) { a.telegramDiscovery(ctx, c) }}
	}
}
