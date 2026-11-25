package store

import (
	"encoding/base64"
	"github.com/prejudice-studio/twilight/internal/security"
	"strings"
	"testing"
)

func TestJianweiCommerceBackupEncryptionBinding(t *testing.T) {
	t.Setenv("TWILIGHT_TWO_FACTOR_KEY", "")
	key := []byte(strings.Repeat("k", 32))
	secret := "JW-P-" + strings.Repeat("A", 32)
	cipher, err := security.ShopCipher(key, "order-1", secret, false)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := security.ShopDigest(key, "code", secret)
	if err != nil {
		t.Fatal(err)
	}
	o := ShopOrder{ID: "order-1", UID: 1, Revision: 2, Status: "issued", Product: ShopProduct{Kind: 1, Days: 30, PriceCents: 1000}, CodeKey: digest, CodeCipher: cipher}
	state := State{ShopOrders: map[string]ShopOrder{o.ID: o}}
	validKey := base64.StdEncoding.EncodeToString(key)
	if err := ValidateShopBackup(state, validKey); err != nil {
		t.Fatal(err)
	}
	if err := ValidateShopBackup(state, base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 32)))); err == nil {
		t.Fatal("wrong import key accepted")
	}
	o.ID = "order-2"
	state.ShopOrders = map[string]ShopOrder{o.ID: o}
	if err := ValidateShopBackup(state, validKey); err == nil {
		t.Fatal("code copied to different order")
	}
}
