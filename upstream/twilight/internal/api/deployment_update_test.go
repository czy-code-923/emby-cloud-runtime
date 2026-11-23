package api

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"testing"
)

func TestDeploymentUpdateRequiresAdminAndExplicitIntent(t *testing.T) {
	app := newTestApp(t)
	user := registerAndLogin(t, app, "reader", "Reader123456")
	response := doJSON(app, http.MethodGet, "/api/v2/admin/deployment/status", "", user)
	if response.Code != http.StatusForbidden {
		t.Fatalf("non-admin status=%d", response.Code)
	}
	admin := registerAdmin(t, app, "admin", "Admin123456")
	response = doJSON(app, http.MethodPost, "/api/v2/admin/deployment/check", `{}`, admin)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("missing intent status=%d", response.Code)
	}
	response = doJSONWithHeaders(app, http.MethodPost, "/api/v2/admin/deployment/apply", `{"tag":"v0.2.0","expected_version":"0.1.0","password":"wrong"}`, admin, map[string]string{"X-Twilight-Client": "webui", "X-Twilight-Intent": "deploy-release"})
	if response.Code != http.StatusForbidden {
		t.Fatalf("wrong password status=%d", response.Code)
	}
}

func TestDeploymentUpdateUnixProxy(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "updater.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/check" || r.Method != http.MethodPost {
			t.Error("unexpected updater operation")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"installed_version": "0.2.0"})
	})}
	defer server.Close()
	go func() { _ = server.Serve(listener) }()
	result, status, err := deploymentRequest(context.Background(), socketPath, http.MethodPost, "/check", map[string]any{})
	if err != nil || status != http.StatusOK || result["installed_version"] != "0.2.0" {
		t.Fatalf("proxy result=%v status=%d err=%v", result, status, err)
	}
}
