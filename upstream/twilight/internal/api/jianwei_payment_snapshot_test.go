package api

import (
	"github.com/prejudice-studio/twilight/internal/store"
	"testing"
)

func TestJianweiShopPaymentSnapshotIncludesQRAndExactCents(t *testing.T) {
	a := &App{}
	order := store.ShopOrder{ID: "fixture-order", Product: store.ShopProduct{PriceCents: 1999}, PaymentKind: "alipay_qr", PaymentQR: "https://shop.example.test/api/v2/commerce/assets/abc.png", PaymentSecretCipher: "must-not-leak"}
	view := a.shopOrderView(order, false)
	if view["payment_qr"] != order.PaymentQR || view["payment_kind"] != "alipay_qr" || view["product"].(store.ShopProduct).PriceCents != 1999 {
		t.Fatal("order payment snapshot was dropped", view)
	}
	if _, exists := view["payment_secret_cipher"]; exists {
		t.Fatal("encrypted payment secret exposed")
	}
}
