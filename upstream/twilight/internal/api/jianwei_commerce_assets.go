package api

import (
	"bytes"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/prejudice-studio/twilight/internal/security"
	"github.com/prejudice-studio/twilight/internal/store"
)

func (a *App) handleCommerceImageUpload(w http.ResponseWriter, r *http.Request, _ Params) {
	if requireAdmin(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 3<<20)
	if r.ParseMultipartForm(3<<20) != nil {
		fail(w, 400, "请选择不超过2MB的PNG或JPEG收款码")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	if !a.adminShopIntent(w, r, map[string]any{"password": r.FormValue("password")}) {
		return
	}
	origin := publicHTTPSURL(a.cfg().CommerceStoreURL)
	if origin == "" {
		fail(w, 409, "请先配置商店HTTPS域名")
		return
	}
	file, _, e := r.FormFile("file")
	if e != nil {
		fail(w, 400, "缺少收款码图片")
		return
	}
	defer file.Close()
	data, e := io.ReadAll(io.LimitReader(file, (2<<20)+1))
	if e != nil || len(data) > 2<<20 {
		fail(w, 400, "图片过大")
		return
	}
	cfg, format, e := image.DecodeConfig(bytes.NewReader(data))
	if e != nil || cfg.Width < 32 || cfg.Height < 32 || cfg.Width > 4096 || cfg.Height > 4096 || (format != "png" && format != "jpeg") {
		fail(w, 400, "请选择有效的PNG或JPEG图片")
		return
	}
	extension := "png"
	if format == "jpeg" {
		extension = "jpg"
	}
	name, e := security.RandomHex(8)
	if e != nil {
		fail(w, 503, "暂不可用")
		return
	}
	name += "." + extension
	dir := filepath.Join(a.cfg().UploadDir, "commerce")
	if os.MkdirAll(dir, 0700) != nil {
		fail(w, 503, "图片保存失败")
		return
	}
	if e = store.WriteFileAtomicSync(filepath.Join(dir, name), data, 0600); e != nil {
		fail(w, 503, "图片保存失败")
		return
	}
	a.audit(r, "commerce_qr_uploaded", "finance", 0, nil)
	ok(w, "图片已保存，仍需保存渠道配置", map[string]any{"url": origin + "/api/v2/commerce/assets/" + name})
}
func (a *App) handleCommerceImage(w http.ResponseWriter, r *http.Request, p Params) {
	name := p["filename"]
	if !uploadFilenamePattern.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	root := filepath.Join(a.cfg().UploadDir, "commerce")
	file, e := ResolveLeafFile(root, name, "")
	if e != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, file)
}

func (a *App) handleMiniAvatar(w http.ResponseWriter, r *http.Request, _ Params) {
	u := current(r).User
	name := filepath.Base(u.Avatar)
	if name == "." || name == "" {
		http.NotFound(w, r)
		return
	}
	a.handleAsset(w, r, Params{"kind": "avatar", "filename": name})
}
