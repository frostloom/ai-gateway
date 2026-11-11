package supportconfig

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"strings"
)

var ErrEncryptionUnavailable = errors.New("客服密钥加密服务未配置或不可用")

type Cipher struct{ aead cipher.AEAD }

func NewCipher(encodedKey string) (*Cipher, error) {
	key, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil || len(key) != 32 {
		return nil, ErrEncryptionUnavailable
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrEncryptionUnavailable
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrEncryptionUnavailable
	}
	return &Cipher{aead: aead}, nil
}

func (c *Cipher) Seal(plaintext, purpose string) (string, error) {
	if c == nil || c.aead == nil {
		return "", ErrEncryptionUnavailable
	}
	if plaintext == "" {
		return "", nil
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", ErrEncryptionUnavailable
	}
	encrypted := c.aead.Seal(nonce, nonce, []byte(plaintext), []byte("customer-service:"+purpose))
	return "v1:" + base64.StdEncoding.EncodeToString(encrypted), nil
}

func (c *Cipher) Open(encrypted, purpose string) (string, error) {
	if c == nil || c.aead == nil {
		return "", ErrEncryptionUnavailable
	}
	if encrypted == "" {
		return "", nil
	}
	if !strings.HasPrefix(encrypted, "v1:") {
		return "", ErrEncryptionUnavailable
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encrypted, "v1:"))
	if err != nil || len(raw) < c.aead.NonceSize()+c.aead.Overhead() {
		return "", ErrEncryptionUnavailable
	}
	nonce := raw[:c.aead.NonceSize()]
	plain, err := c.aead.Open(nil, nonce, raw[c.aead.NonceSize():], []byte("customer-service:"+purpose))
	if err != nil {
		return "", ErrEncryptionUnavailable
	}
	return string(plain), nil
}
