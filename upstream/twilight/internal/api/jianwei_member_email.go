package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

func (a *App) handleMemberBindEmailSend(w http.ResponseWriter, r *http.Request, p Params) {
	body := decodeMap(r)
	// Reuse the canonical email service, but the member transport can issue
	// only a binding challenge, never a Web-password recovery challenge.
	raw, _ := json.Marshal(map[string]any{"purpose": emailPurposeBind, "email": stringValue(body, "email")})
	r.Body = io.NopCloser(bytes.NewReader(raw))
	a.handleSendEmailCode(w, r, p)
}
