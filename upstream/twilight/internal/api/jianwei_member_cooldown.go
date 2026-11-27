package api

import (
	"net/http"
	"strconv"
	"time"
)

func (a *App) memberCooldown(w http.ResponseWriter, r *http.Request, key string, seconds int) bool {
	if a.allowRate(r.Context(), rateKey("member-cooldown:"+key+":", current(r).User.UID), 1, time.Duration(seconds)*time.Second) {
		return false
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	failWithCodeData(w, 429, ErrRateLimited, "操作过于频繁，请等待"+strconv.Itoa(seconds)+"秒再试", map[string]any{"retry_after_seconds": seconds})
	return true
}
