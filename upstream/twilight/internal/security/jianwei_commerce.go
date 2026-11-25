package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

func ShopDigest(key []byte, purpose, value string) (string, error) {
	if len(key) != 32 {
		return "", ErrTwoFactorKey
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("jianwei:shop:" + purpose + "\x00" + value))
	return strings.ToUpper(hex.EncodeToString(mac.Sum(nil))), nil
}
func ShopCipher(key []byte, orderID, value string, open bool) (string, error) {
	if len(key) != 32 {
		return "", ErrTwoFactorKey
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("jianwei:shop:delivery-encryption:v1"))
	block, err := aes.NewCipher(mac.Sum(nil))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	aad := []byte("jianwei:shop:order:" + orderID)
	if open {
		raw, err := base64.RawURLEncoding.DecodeString(value)
		if err != nil || len(raw) < gcm.NonceSize() {
			return "", fmt.Errorf("invalid delivery")
		}
		clear, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], aad)
		return string(clear), err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	raw := gcm.Seal(nonce, nonce, []byte(value), aad)
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
