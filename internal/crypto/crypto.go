package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
)

var encryptKey []byte

// Init sets the encryption key (32 bytes, hex or raw)
func Init(key string) error {
	if key == "" {
		// Generate a random key
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			return err
		}
		encryptKey = raw
		return nil
	}
	// Try hex decode first (64 hex chars = 32 bytes)
	if len(key) == 64 {
		if decoded, err := hex.DecodeString(key); err == nil {
			encryptKey = decoded
			return nil
		}
	}
	// Use SHA-256 to derive 32 bytes from the key
	hash := sha256.Sum256([]byte(key))
	encryptKey = hash[:]
	return nil
}

// MustEncrypt encrypts and returns the original on error (never fails silently with empty)
func MustEncrypt(plaintext string) string {
	if plaintext == "" || plaintext == "******" {
		return plaintext
	}
	enc, err := Encrypt(plaintext)
	if err != nil {
		return plaintext
	}
	return enc
}

// GetKey returns the current key as hex (for saving to config)
func GetKey() string {
	return hex.EncodeToString(encryptKey)
}

// Encrypt encrypts plaintext with AES-256-GCM, returns base64
func Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	block, err := aes.NewCipher(encryptKey)
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
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts a base64-encoded AES-256-GCM ciphertext
func Decrypt(encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}
	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("解密失败: %w", err)
	}
	block, err := aes.NewCipher(encryptKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("密文太短")
	}
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("解密失败: %w", err)
	}
	return string(plaintext), nil
}
