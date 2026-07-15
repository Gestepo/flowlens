package webhook

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"
)

type SecretCipher struct{ aead cipher.AEAD }

func NewSecretCipher(key []byte) (*SecretCipher, error) {
	if len(key) != 32 {
		return nil, errors.New("FLOWLENS_SECRET_KEY must contain exactly 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &SecretCipher{aead: aead}, nil
}

func (cipher *SecretCipher) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, cipher.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return cipher.aead.Seal(nonce, nonce, plaintext, []byte("flowlens-webhook-secret-v1")), nil
}

func (cipher *SecretCipher) Decrypt(value []byte) ([]byte, error) {
	nonceSize := cipher.aead.NonceSize()
	if len(value) < nonceSize {
		return nil, errors.New("encrypted webhook secret is invalid")
	}
	return cipher.aead.Open(nil, value[:nonceSize], value[nonceSize:], []byte("flowlens-webhook-secret-v1"))
}
