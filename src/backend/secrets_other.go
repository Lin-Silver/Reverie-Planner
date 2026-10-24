//go:build !windows

package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
)

func protectSecret(rootPath, secret string) (string, error) {
	if secret == "" {
		return "", nil
	}
	key, err := loadOrCreateSecretKey(rootPath)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(secret), nil)
	return "aesgcm:v1:" + base64.StdEncoding.EncodeToString(sealed), nil
}

func unprotectSecret(rootPath, encoded string) (string, error) {
	const prefix = "aesgcm:v1:"
	if len(encoded) < len(prefix) || encoded[:len(prefix)] != prefix {
		return "", errors.New("unsupported encrypted credential format")
	}
	sealed, err := base64.StdEncoding.DecodeString(encoded[len(prefix):])
	if err != nil {
		return "", errors.New("invalid encrypted credential")
	}
	key, err := loadOrCreateSecretKey(rootPath)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(sealed) < gcm.NonceSize() {
		return "", errors.New("invalid encrypted credential")
	}
	nonce, ciphertext := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", errors.New("credential could not be decrypted")
	}
	return string(plaintext), nil
}

func loadOrCreateSecretKey(rootPath string) ([]byte, error) {
	path := filepath.Join(rootPath, "data", "provider.key")
	if key, err := os.ReadFile(path); err == nil {
		if len(key) != 32 {
			return nil, errors.New("invalid provider encryption key")
		}
		return key, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		existing, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, readErr
		}
		if len(existing) != 32 {
			return nil, errors.New("invalid provider encryption key")
		}
		return existing, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err := file.Write(key); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	return key, nil
}
