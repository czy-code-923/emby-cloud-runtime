package api

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prejudice-studio/twilight/internal/store"
)

func TestJianweiTMDBMissingCredentialIsConfigurationConflictAcrossAliases(t *testing.T) {
	for _, route := range []struct {
		path   string
		params Params
	}{
		{"/api/v2/media/search?q=example&source=tmdb", nil},
		{"/api/v1/media/search/tmdb?q=example&source=bangumi", nil},
		{"/api/v2/media/search/tmdb?q=example&source=bangumi", Params{"source": "tmdb"}},
	} {
		t.Run(route.path, func(t *testing.T) {
			a := newTestApp(t)
			a.cfg().TMDBAPIKey = ""
			r := httptest.NewRequest("GET", route.path, nil)
			r = r.WithContext(context.WithValue(r.Context(), principalKey, principal{User: store.User{UID: 77, Role: store.RoleNormal, Active: true}}))
			w := httptest.NewRecorder()
			if strings.HasPrefix(route.path, "/api/v2/") {
				a.handleV2MediaSearch(w, r, route.params)
			} else {
				a.handleMediaSearch(w, r, route.params)
			}
			if w.Code != 409 || !strings.Contains(w.Body.String(), "TMDB") || !strings.Contains(w.Body.String(), "MEDIA_SEARCH_SOURCE_FAILED") {
				t.Fatal(w.Code, w.Body)
			}
		})
	}
}
