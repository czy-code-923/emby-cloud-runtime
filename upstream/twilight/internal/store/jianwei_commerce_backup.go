package store

import (
	"fmt"
	"github.com/prejudice-studio/twilight/internal/security"
)

// Paid-code delivery must survive an import with the original encryption key.
// Reject an incompatible key before replacing any business data.
func ValidateShopBackup(state State, configuredKey ...string) error {
	if len(state.Shop.Products) > 24 || len(state.ShopOrders) > 100000 {
		return fmt.Errorf("invalid commerce backup size")
	}
	receipts := map[string]bool{}
	codes := map[string]bool{}
	for id, o := range state.ShopOrders {
		if id != o.ID || o.UID <= 0 || o.Revision < 1 || o.Product.Days < 1 || o.Product.PriceCents < 1 || (o.Product.Kind != 1 && o.Product.Kind != 2) {
			return fmt.Errorf("invalid commerce order backup")
		}
		switch o.Status {
		case "pending", "review", "hold", "paid_hold", "issued", "cancelled", "closed_unpaid", "refund_pending", "refunded":
		default:
			return fmt.Errorf("invalid commerce status backup")
		}
		if o.ReceiptHash != "" {
			if receipts[o.ReceiptHash] {
				return fmt.Errorf("duplicate receipt in commerce backup")
			}
			receipts[o.ReceiptHash] = true
		}
		if o.CodeKey != "" {
			if codes[o.CodeKey] {
				return fmt.Errorf("duplicate delivery in commerce backup")
			}
			codes[o.CodeKey] = true
		}
		if o.CodeCipher != "" {
			key, err := security.TwoFactorKey(configuredKey...)
			if err != nil {
				return err
			}
			code, err := security.ShopCipher(key, id, o.CodeCipher, true)
			if err != nil {
				return fmt.Errorf("commerce delivery encryption key does not match backup")
			}
			digest, err := security.ShopDigest(key, "code", code)
			if err != nil || digest != o.CodeKey {
				return fmt.Errorf("invalid encrypted commerce delivery")
			}
		}
	}
	return nil
}
