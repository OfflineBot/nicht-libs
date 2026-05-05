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
	"os"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/hkdf"
)

// PepperPurposeArgonSalt is the HKDF "info" tag for deriving per-user Argon2 salts.
const PepperPurposeArgonSalt = "argon2-salt"

// PepperPurposeCryptoKey is the HKDF "info" tag for deriving the AES key (second_hash) from first_hash.
const PepperPurposeCryptoKey = "crypto_key"

// pepper returns the server-side PEPPER as raw bytes. Returns nil if not configured.
func pepper() []byte {
	v := os.Getenv("PASSWORD_HASH_PEPPER")
	if v == "" {
		return nil
	}
	return []byte(v)
}

// DeriveArgonSalt computes a deterministic 16-byte Argon2 salt from email + PEPPER.
// Used both client-side (computed in the browser) and server-side (during v1→v2 migration).
func DeriveArgonSalt(email string) ([]byte, error) {
	p := pepper()
	if len(p) < 32 {
		return nil, fmt.Errorf("PASSWORD_HASH_PEPPER not configured or too short")
	}
	r := hkdf.New(sha256.New, []byte(strings.ToLower(strings.TrimSpace(email))), p, []byte(PepperPurposeArgonSalt))
	salt := make([]byte, 16)
	if _, err := io.ReadFull(r, salt); err != nil {
		return nil, fmt.Errorf("hkdf read failed: %w", err)
	}
	return salt, nil
}

// ServerArgon2id computes the Argon2id "first_hash" server-side. Used ONLY for
// the one-time v1→v2 migration: when a legacy user logs in with their plaintext
// password, the server reproduces what the client would have computed.
//
// Parameters MUST match the client (mem=64MB, iter=3, par=1, output=32 byte).
func ServerArgon2id(password, email string) ([]byte, error) {
	salt, err := DeriveArgonSalt(email)
	if err != nil {
		return nil, err
	}
	return argon2.IDKey([]byte(password), salt, 3, 64*1024, 1, 32), nil
}

// DeriveSecondHash computes second_hash = HKDF(first_hash, info="crypto_key", salt=random_string).
// Server uses this during change-password to compute the NEW second_hash without help from the client.
func DeriveSecondHash(firstHash []byte, randomString string) ([]byte, error) {
	if len(firstHash) == 0 {
		return nil, fmt.Errorf("first_hash empty")
	}
	if len(randomString) == 0 {
		return nil, fmt.Errorf("random_string empty")
	}
	r := hkdf.New(sha256.New, firstHash, []byte(randomString), []byte(PepperPurposeCryptoKey))
	out := make([]byte, 32)
	if _, err := io.ReadFull(r, out); err != nil {
		return nil, fmt.Errorf("hkdf read failed: %w", err)
	}
	return out, nil
}

// GenerateRandomString returns a 32-byte random hex string (64 chars). Used as the
// per-user crypto_salt that produces second_hash on the client.
func GenerateRandomString() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("random gen failed: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// EncryptWithKey encrypts plaintext using AES-256-GCM with an explicit 32-byte key.
// Returns base64-encoded ciphertext (nonce || ciphertext_with_tag).
func EncryptWithKey(plaintext string, key []byte) (string, error) {
	if len(key) != 32 {
		return "", fmt.Errorf("key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("cipher init failed: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("gcm init failed: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("nonce gen failed: %w", err)
	}
	ct := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ct), nil
}

// DecryptWithKey decrypts a base64-encoded AES-256-GCM ciphertext with an explicit key.
// Returns an error if the GCM tag does not validate (wrong key or corrupted data).
func DecryptWithKey(encoded string, key []byte) (string, error) {
	if len(key) != 32 {
		return "", fmt.Errorf("key must be 32 bytes, got %d", len(key))
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("base64 decode failed: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("cipher init failed: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("gcm init failed: %w", err)
	}
	if len(data) < gcm.NonceSize() {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ct := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("decryption failed (wrong key or corrupted)")
	}
	return string(pt), nil
}

// ZeroBytes overwrites every byte of b with 0. Use to wipe sensitive material
// (first_hash, second_hash, decrypted credentials) from RAM after use.
//
// Caveats: Go's GC may have copied the slice; this only wipes the addressable backing array.
// For stronger guarantees use a memguard-style enclave.
func ZeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// ZeroString attempts to wipe the backing bytes of a string by reinterpreting it.
// NOTE: in Go, strings are immutable — this is best-effort. Prefer []byte for sensitive data.
func ZeroString(_ string) {
	// no-op stub to make caller intent visible. Switch sensitive data to []byte where possible.
}

// DecodeHex32 parses a 64-character hex string into a 32-byte slice.
// Used to decode first_hash / second_hash sent by the client.
func DecodeHex32(s string) ([]byte, error) {
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("invalid hex: %w", err)
	}
	if len(b) != 32 {
		return nil, fmt.Errorf("expected 32 bytes, got %d", len(b))
	}
	return b, nil
}
