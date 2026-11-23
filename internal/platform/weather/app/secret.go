package app

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"

	"github.com/google/uuid"
)

// Mã hóa khóa API (FN-TT-03/BR-03): AES-256-GCM, nonce 12 byte ngẫu nhiên, AAD = tenant_id — bản mã của tenant này
// không mở được dưới tenant khác. Khóa API thô chỉ nằm trong bộ nhớ của request hoặc lượt job.

func seal(key []byte, tid uuid.UUID, plain string) (cipherText, nonce []byte, err error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, nil, err
	}
	nonce = make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	return gcm.Seal(nil, nonce, []byte(plain), tid[:]), nonce, nil
}

func open(key []byte, tid uuid.UUID, cipherText, nonce []byte) (string, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	plain, err := gcm.Open(nil, nonce, cipherText, tid[:])
	if err != nil {
		return "", errors.New("weather: không mở được khóa API (sai khóa mã hóa hoặc dữ liệu bị sửa)")
	}
	return string(plain), nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, ErrSecretMissing
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
