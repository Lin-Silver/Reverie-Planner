//go:build windows

package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

type dataBlob struct {
	size uint32
	data *byte
}

var (
	crypt32                = syscall.NewLazyDLL("crypt32.dll")
	procCryptProtectData   = crypt32.NewProc("CryptProtectData")
	procCryptUnprotectData = crypt32.NewProc("CryptUnprotectData")
	procLocalFree          = syscall.NewLazyDLL("kernel32.dll").NewProc("LocalFree")
)

func protectSecret(_ string, secret string) (string, error) {
	if secret == "" {
		return "", nil
	}
	input := []byte(secret)
	in := dataBlob{size: uint32(len(input)), data: &input[0]}
	var out dataBlob
	ok, _, err := procCryptProtectData.Call(
		uintptr(unsafe.Pointer(&in)), 0, 0, 0, 0, 1,
		uintptr(unsafe.Pointer(&out)),
	)
	if ok == 0 {
		return "", fmt.Errorf("Windows credential protection failed: %w", err)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.data)))
	protected := unsafe.Slice(out.data, out.size)
	return "dpapi:v1:" + base64.StdEncoding.EncodeToString(protected), nil
}

func unprotectSecret(_ string, encoded string) (string, error) {
	const prefix = "dpapi:v1:"
	if len(encoded) < len(prefix) || encoded[:len(prefix)] != prefix {
		return "", errors.New("unsupported encrypted credential format")
	}
	ciphertext, err := base64.StdEncoding.DecodeString(encoded[len(prefix):])
	if err != nil || len(ciphertext) == 0 {
		return "", errors.New("invalid encrypted credential")
	}
	in := dataBlob{size: uint32(len(ciphertext)), data: &ciphertext[0]}
	var out dataBlob
	ok, _, callErr := procCryptUnprotectData.Call(
		uintptr(unsafe.Pointer(&in)), 0, 0, 0, 0, 1,
		uintptr(unsafe.Pointer(&out)),
	)
	if ok == 0 {
		return "", fmt.Errorf("Windows credential decryption failed: %w", callErr)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.data)))
	plaintext := unsafe.Slice(out.data, out.size)
	return string(plaintext), nil
}
