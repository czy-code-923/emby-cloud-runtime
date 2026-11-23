package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"time"
)

// The application can request fixed release operations through a Unix socket.
// It has neither a Docker socket nor a general command execution interface.
const deploymentSocket = "/run/emby-cloud-updater/updater.sock"

func deploymentRequest(ctx context.Context, socketPath, method, path string, payload map[string]any) (map[string]any, int, error) {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 60 * time.Second}
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil { return nil, 0, err }
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, "http://updater"+path, body)
	if err != nil { return nil, 0, err }
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil { return nil, 0, err }
	defer response.Body.Close()
	var data map[string]any
	err = json.NewDecoder(io.LimitReader(response.Body, 1024*1024)).Decode(&data)
	return data, response.StatusCode, err
}

func (a *App) handleDeploymentUpdate(w http.ResponseWriter, r *http.Request, params Params) {
	if requireAdmin(w, r) { return }
	operation := params["operation"]
	if operation == "" { operation = "status" }
	if (r.Method == http.MethodGet && operation != "status") ||
		(r.Method == http.MethodPost && operation != "check" && operation != "apply") {
		failWithCode(w, http.StatusNotFound, ErrNotFound, "不支持的版本操作")
		return
	}
	var payload map[string]any
	if r.Method == http.MethodPost {
		if !requireWebUIIntent(w, r, "deploy-release") { return }
		payload = decodeMap(r)
		if operation == "apply" {
			if !a.twoFactorPassword(w, r, payload) { return }
			payload = map[string]any{"tag": stringValue(payload, "tag"), "expected_version": stringValue(payload, "expected_version")}
		} else { payload = map[string]any{} }
	}
	data, status, err := deploymentRequest(r.Context(), deploymentSocket, r.Method, "/"+operation, payload)
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "升级执行器暂不可用，请检查主机服务")
		return
	}
	if status >= 400 {
		failWithCodeData(w, status, ErrInvalidPayload, firstNonEmpty(asString(data["error"]), "版本操作失败"), data)
		return
	}
	if operation == "apply" {
		a.audit(r, "deployment_release_apply", "system", current(r).User.UID, map[string]any{"tag": payload["tag"], "expected_version": payload["expected_version"]})
	}
	w.Header().Set("Cache-Control", "private, no-store")
	ok(w, "OK", data)
}
