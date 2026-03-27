package config

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"

	"github.com/zalando/go-keyring"
	"golang.org/x/crypto/chacha20poly1305"
)

// keyringServiceName returns a profile-scoped keyring service name.
// The default profile uses "llmail" for backward compatibility.
func keyringServiceName() string {
	if activeProfile != DefaultProfile {
		return "llmail:" + activeProfile
	}
	return "llmail"
}

func StorePassword(account, password string, storage PasswordStorage) (string, error) {
	switch storage {
	case PasswordStorageKeyring:
		return "", storeKeyring(account, password)
	case PasswordStorageEncrypted:
		return encryptPassword(account, password)
	default:
		return "", fmt.Errorf("unknown password storage: %s", storage)
	}
}

func RetrievePassword(account string, storage PasswordStorage, encrypted string) (string, error) {
	switch storage {
	case PasswordStorageKeyring:
		return retrieveKeyring(account)
	case PasswordStorageEncrypted:
		return decryptPassword(account, encrypted)
	default:
		return "", fmt.Errorf("unknown password storage: %s", storage)
	}
}

func KeyringAvailable() bool {
	testKey := "llmail-keyring-test"
	err := keyring.Set(keyringServiceName(), testKey, "test")
	if err != nil {
		return false
	}
	_ = keyring.Delete(keyringServiceName(), testKey)
	return true
}

func storeKeyring(account, password string) error {
	return keyring.Set(keyringServiceName(), account, password)
}

func retrieveKeyring(account string) (string, error) {
	return keyring.Get(keyringServiceName(), account)
}

// encryptPassword encrypts using XChaCha20-Poly1305 with a key derived from the account name.
// This is a simple fallback for headless servers - not meant to be highly secure,
// but better than plaintext. The account name is used as a deterministic key seed.
func encryptPassword(account, password string) (string, error) {
	key := deriveKey(account)

	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return "", fmt.Errorf("creating cipher: %w", err)
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generating nonce: %w", err)
	}

	ciphertext := aead.Seal(nonce, nonce, []byte(password), []byte(account))
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func decryptPassword(account, encoded string) (string, error) {
	key := deriveKey(account)

	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return "", fmt.Errorf("creating cipher: %w", err)
	}

	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decoding: %w", err)
	}

	nonceSize := aead.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := aead.Open(nil, nonce, ciphertext, []byte(account))
	if err != nil {
		return "", fmt.Errorf("decrypting: %w", err)
	}

	return string(plaintext), nil
}

const llmKeyringPrefix = "llm-api-key:"

// StoreAPIKey stores an LLM API key using the same mechanism as account passwords.
// The keyring key is prefixed to avoid collision with IMAP account passwords.
func StoreAPIKey(provider, apiKey string, storage PasswordStorage) (string, error) {
	return StorePassword(llmKeyringPrefix+provider, apiKey, storage)
}

// RetrieveAPIKey retrieves an LLM API key.
func RetrieveAPIKey(provider string, storage PasswordStorage, encrypted string) (string, error) {
	return RetrievePassword(llmKeyringPrefix+provider, storage, encrypted)
}

func deriveKey(account string) []byte {
	// Simple deterministic key derivation from account name.
	// Pad or hash to 32 bytes for XChaCha20-Poly1305.
	key := make([]byte, chacha20poly1305.KeySize)
	seed := []byte("llmail-encryption-key:" + account)
	for i := range key {
		key[i] = seed[i%len(seed)]
	}
	return key
}
