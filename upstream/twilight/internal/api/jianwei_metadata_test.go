package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestJianweiTMDBReadTokenAndV3Key(t *testing.T) {
	a := newTestApp(t)
	var auth, key string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		key = r.URL.Query().Get("api_key")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":1}`))
	}))
	defer server.Close()
	a.cfg().TMDBAPIURL = server.URL
	for _, value := range []string{"raw-v3-key", "Bearer read-token", "aaa.bbb.ccc"} {
		a.cfg().TMDBAPIKey = value
		var out map[string]any
		if e := a.tmdbJSON(context.Background(), "/configuration", url.Values{}, &out); e != nil {
			t.Fatal(e)
		}
		if value == "raw-v3-key" {
			if key != value || auth != "" {
				t.Fatal("invalid v3 auth")
			}
		} else if key != "" || auth == "" {
			t.Fatal("bearer credential leaked into query")
		}
	}
}
func TestJianweiMiniMediaImageRequiresOwnLibraryAccess(t *testing.T) {
	a := newTestApp(t)
	cookies := registerAndLogin(t, a, "image-owner", "ImageOwner123!")
	// A web administrator session does not authenticate the Mini App resource.
	w := doJSON(a, "GET", "/api/v2/miniapp/media/12/image", "", cookies)
	if w.Code < 400 {
		t.Fatal("web session authenticated mini image")
	}
	var payload map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &payload)
}
