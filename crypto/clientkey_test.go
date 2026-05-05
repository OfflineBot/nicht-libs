package crypto

import (
	"bytes"
	"encoding/hex"
	"os"
	"testing"
)

func setPepper(t *testing.T) {
	t.Helper()
	const pepper = "test-pepper-must-be-32-bytes-long-x"
	t.Setenv("PASSWORD_HASH_PEPPER", pepper)
}

func TestDeriveArgonSalt_IsDeterministic(t *testing.T) {
	setPepper(t)
	a, err := DeriveArgonSalt("user@dhbw.de")
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	b, err := DeriveArgonSalt("user@dhbw.de")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Fatalf("salt should be deterministic, got %x and %x", a, b)
	}
	if len(a) != 16 {
		t.Fatalf("salt should be 16 bytes, got %d", len(a))
	}
}

func TestDeriveArgonSalt_DifferentEmails_DifferentSalts(t *testing.T) {
	setPepper(t)
	a, _ := DeriveArgonSalt("alice@dhbw.de")
	b, _ := DeriveArgonSalt("bob@dhbw.de")
	if bytes.Equal(a, b) {
		t.Fatal("different emails should produce different salts")
	}
}

func TestDeriveArgonSalt_CaseInsensitive(t *testing.T) {
	setPepper(t)
	a, _ := DeriveArgonSalt("User@DHBW.DE")
	b, _ := DeriveArgonSalt("user@dhbw.de")
	if !bytes.Equal(a, b) {
		t.Fatal("salt should normalize email to lowercase")
	}
}

func TestDeriveArgonSalt_RequiresPepper(t *testing.T) {
	os.Unsetenv("PASSWORD_HASH_PEPPER")
	if _, err := DeriveArgonSalt("user@dhbw.de"); err == nil {
		t.Fatal("missing PEPPER should error, got nil")
	}
}

func TestServerArgon2id_RoundtripDeterministic(t *testing.T) {
	setPepper(t)
	h1, err := ServerArgon2id("hunter2", "user@dhbw.de")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	h2, _ := ServerArgon2id("hunter2", "user@dhbw.de")
	if !bytes.Equal(h1, h2) {
		t.Fatal("Argon2id should be deterministic for same input")
	}
	if len(h1) != 32 {
		t.Fatalf("first_hash should be 32 bytes, got %d", len(h1))
	}
}

func TestServerArgon2id_DifferentPasswords(t *testing.T) {
	setPepper(t)
	h1, _ := ServerArgon2id("password1", "user@dhbw.de")
	h2, _ := ServerArgon2id("password2", "user@dhbw.de")
	if bytes.Equal(h1, h2) {
		t.Fatal("different passwords should produce different hashes")
	}
}

func TestDeriveSecondHash_Deterministic(t *testing.T) {
	first := bytes.Repeat([]byte{0x42}, 32)
	salt := "my-random-string"
	a, err := DeriveSecondHash(first, salt)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	b, _ := DeriveSecondHash(first, salt)
	if !bytes.Equal(a, b) {
		t.Fatal("HKDF should be deterministic")
	}
	if len(a) != 32 {
		t.Fatalf("second_hash should be 32 bytes, got %d", len(a))
	}
}

func TestDeriveSecondHash_DifferentSalts(t *testing.T) {
	first := bytes.Repeat([]byte{0x42}, 32)
	a, _ := DeriveSecondHash(first, "salt1")
	b, _ := DeriveSecondHash(first, "salt2")
	if bytes.Equal(a, b) {
		t.Fatal("different random_strings should produce different keys")
	}
}

func TestEncryptDecryptWithKey_Roundtrip(t *testing.T) {
	key := bytes.Repeat([]byte{0x99}, 32)
	plaintext := "moodle-password-secret-stuff"

	ct, err := EncryptWithKey(plaintext, key)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	pt, err := DecryptWithKey(ct, key)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if pt != plaintext {
		t.Fatalf("roundtrip mismatch: got %q want %q", pt, plaintext)
	}
}

func TestDecryptWithKey_WrongKeyFails(t *testing.T) {
	key1 := bytes.Repeat([]byte{0x01}, 32)
	key2 := bytes.Repeat([]byte{0x02}, 32)
	ct, _ := EncryptWithKey("secret", key1)
	if _, err := DecryptWithKey(ct, key2); err == nil {
		t.Fatal("decrypt with wrong key should fail (AES-GCM tag check)")
	}
}

func TestEncryptWithKey_RequiresCorrectLength(t *testing.T) {
	if _, err := EncryptWithKey("x", []byte{1, 2, 3}); err == nil {
		t.Fatal("non-32-byte key should be rejected")
	}
}

func TestEncryptWithKey_RandomNonce(t *testing.T) {
	key := bytes.Repeat([]byte{0x77}, 32)
	a, _ := EncryptWithKey("same", key)
	b, _ := EncryptWithKey("same", key)
	if a == b {
		t.Fatal("encrypting the same plaintext twice should yield different ciphertexts (random nonce)")
	}
}

func TestZeroBytes(t *testing.T) {
	b := []byte{1, 2, 3, 4, 5}
	ZeroBytes(b)
	for i, v := range b {
		if v != 0 {
			t.Fatalf("byte %d not zeroed: %d", i, v)
		}
	}
}

func TestDecodeHex32(t *testing.T) {
	src := bytes.Repeat([]byte{0xab}, 32)
	hexStr := hex.EncodeToString(src)
	got, err := DecodeHex32(hexStr)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !bytes.Equal(got, src) {
		t.Fatal("roundtrip mismatch")
	}

	if _, err := DecodeHex32("notvalid"); err == nil {
		t.Fatal("invalid hex should error")
	}
	if _, err := DecodeHex32("abcd"); err == nil {
		t.Fatal("wrong length should error")
	}
}

func TestGenerateRandomString(t *testing.T) {
	a, _ := GenerateRandomString()
	b, _ := GenerateRandomString()
	if a == b {
		t.Fatal("two consecutive randoms should differ")
	}
	if len(a) != 64 {
		t.Fatalf("expected 64 hex chars, got %d", len(a))
	}
}

// Integration-style: simulate the full register/login flow purely with the crypto layer.
func TestEndToEndKeyDerivation(t *testing.T) {
	setPepper(t)
	const email = "alice@dhbw.de"
	const password = "correct horse battery staple"

	// Client-side at register
	firstHashClient, err := ServerArgon2id(password, email)
	if err != nil {
		t.Fatalf("first_hash: %v", err)
	}
	salt, _ := GenerateRandomString()
	secondHashClient, _ := DeriveSecondHash(firstHashClient, salt)

	// Client encrypts a service password
	ciphertext, _ := EncryptWithKey("real-moodle-pw", secondHashClient)

	// Later: client logs in again (different session, same password+email)
	firstHashAgain, _ := ServerArgon2id(password, email)
	secondHashAgain, _ := DeriveSecondHash(firstHashAgain, salt)
	if !bytes.Equal(secondHashClient, secondHashAgain) {
		t.Fatal("second_hash should reproduce identically across logins")
	}

	// Client decrypts with the re-derived key
	plain, err := DecryptWithKey(ciphertext, secondHashAgain)
	if err != nil {
		t.Fatalf("decrypt across sessions failed: %v", err)
	}
	if plain != "real-moodle-pw" {
		t.Fatalf("plaintext mismatch: %q", plain)
	}
}
