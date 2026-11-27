package api

import (
	"bytes"
	"github.com/prejudice-studio/twilight/internal/security"
	"github.com/prejudice-studio/twilight/internal/store"
	"image"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func (a *App) handleShopProofUpload(w http.ResponseWriter, r *http.Request, p Params) {
	uid := current(r).User.UID
	o, found := a.store().ShopOrder(p["order_id"])
	if !found || o.UID != uid {
		fail(w, 404, "订单不存在")
		return
	}
	if o.ProofUploads >= 3 {
		fail(w, 409, "本订单付款截图已达3次上限，请联系管理员处理")
		return
	}
	if !a.allowRate(r.Context(), rateKey("shop-proof:", uid), 5, time.Minute) || !a.allowRate(r.Context(), rateKey("shop-proof-day:", uid), 12, 24*time.Hour) {
		fail(w, 429, "上传过于频繁，请稍后再试")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 3<<20)
	if r.ParseMultipartForm(3<<20) != nil {
		fail(w, 400, "请选择不超过2MB的PNG或JPEG付款截图")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		fail(w, 400, "请选择付款截图")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (2<<20)+1))
	if err != nil || len(data) > 2<<20 {
		fail(w, 400, "图片过大")
		return
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width < 32 || cfg.Height < 32 || cfg.Width > 4096 || cfg.Height > 4096 || (format != "png" && format != "jpeg") {
		fail(w, 400, "付款截图必须是有效的PNG或JPEG图片")
		return
	}
	ext := "png"
	if format == "jpeg" {
		ext = "jpg"
	}
	name, err := security.RandomHex(8)
	if err != nil {
		fail(w, 503, "暂不可上传")
		return
	}
	name += "." + ext
	dir := filepath.Join(a.cfg().UploadDir, "commerce-proof")
	if os.MkdirAll(dir, 0700) != nil {
		fail(w, 503, "图片保存失败")
		return
	}
	path := filepath.Join(dir, name)
	if store.WriteFileAtomicSync(path, data, 0600) != nil {
		fail(w, 503, "图片保存失败")
		return
	}
	if err = a.store().AttachShopProof(uid, o.ID, name); err != nil {
		_ = os.Remove(path)
		statusFromError(w, err)
		return
	}
	a.audit(r, "shop_payment_proof_uploaded", "commerce", uid, map[string]any{"order_id": o.ID})
	updated, _ := a.store().ShopOrder(o.ID)
	okResponse := a.shopOrderView(updated, false)
	ok(w, "付款截图已保存，仅本人和管理员可查看", okResponse)
}

func (a *App) handleShopProof(w http.ResponseWriter, r *http.Request, p Params) {
	a.serveShopProof(w, r, p, false)
}
func (a *App) handleAdminShopProof(w http.ResponseWriter, r *http.Request, p Params) {
	if requireAdmin(w, r) {
		return
	}
	a.serveShopProof(w, r, p, true)
}
func (a *App) serveShopProof(w http.ResponseWriter, r *http.Request, p Params, admin bool) {
	o, found := a.store().ShopOrder(p["order_id"])
	if !found || (!admin && o.UID != current(r).User.UID) || !uploadFilenamePattern.MatchString(o.PaymentProof) {
		http.NotFound(w, r)
		return
	}
	path, err := ResolveLeafFile(filepath.Join(a.cfg().UploadDir, "commerce-proof"), o.PaymentProof, "")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Vary", "Cookie")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, path)
}
