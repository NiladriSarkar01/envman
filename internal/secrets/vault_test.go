package secrets

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitializeUnlockAndEncrypt(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "vault.key")
	password := "a sufficiently long test password"
	key, err := Initialize(keyPath, password)
	if err != nil {
		t.Fatalf("initialize vault: %v", err)
	}
	data, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read key file: %v", err)
	}
	if bytes.Contains(data, []byte(password)) || bytes.Contains(data, key) {
		t.Fatal("key file contains plaintext password or vault key")
	}
	unlocked, err := Unlock(keyPath, password)
	if err != nil {
		t.Fatalf("unlock vault: %v", err)
	}
	if !bytes.Equal(unlocked, key) {
		t.Fatal("unlocked key does not match initialized key")
	}
	if _, err := Unlock(keyPath, "an incorrect password"); err == nil {
		t.Fatal("unlock with incorrect password succeeded")
	}

	plaintext := []byte("sensitive values")
	ciphertext, err := Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("encrypt data: %v", err)
	}
	if bytes.Contains(ciphertext, plaintext) {
		t.Fatal("ciphertext contains plaintext")
	}
	decoded, err := Decrypt(unlocked, ciphertext)
	if err != nil || !bytes.Equal(decoded, plaintext) {
		t.Fatalf("decrypt data = %q, err %v", decoded, err)
	}
	ciphertext[len(ciphertext)-1] ^= 1
	if _, err := Decrypt(unlocked, ciphertext); err == nil {
		t.Fatal("decrypting tampered data succeeded")
	}
}

func TestInitializeRejectsShortPasswordAndExistingKey(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "vault.key")
	if _, err := Initialize(keyPath, "short"); err == nil {
		t.Fatal("initialize with short password succeeded")
	}
	if _, err := Initialize(keyPath, strings.Repeat("p", 12)); err != nil {
		t.Fatalf("initialize with valid password: %v", err)
	}
	if _, err := Initialize(keyPath, strings.Repeat("q", 12)); err == nil {
		t.Fatal("initialize overwrote existing key file")
	}
}

func TestPublicKeyWritesWithoutPasswordAndPrivateKeyReadsAfterUnlock(t *testing.T) {
	dir := t.TempDir()
	password := "a sufficiently long test password"
	vaultKey, err := Initialize(filepath.Join(dir, "vault.key"), password)
	if err != nil {
		t.Fatalf("initialize vault: %v", err)
	}
	publicPath := filepath.Join(dir, "vault.pub")
	privatePath := filepath.Join(dir, "vault.private")
	if err := CreateAsymmetricKeys(publicPath, privatePath, vaultKey); err != nil {
		t.Fatalf("create asymmetric keys: %v", err)
	}
	encrypted, err := EncryptValue(publicPath, "TOKEN", "no-password-to-encrypt")
	if err != nil {
		t.Fatalf("encrypt value with public key: %v", err)
	}
	unlockedKey, err := Unlock(filepath.Join(dir, "vault.key"), password)
	if err != nil {
		t.Fatalf("unlock vault: %v", err)
	}
	privateDER, err := UnlockPrivateKey(privatePath, unlockedKey)
	if err != nil {
		t.Fatalf("unlock private key: %v", err)
	}
	value, err := DecryptValue(privateDER, "TOKEN", encrypted)
	if err != nil || string(value) != "no-password-to-encrypt" {
		t.Fatalf("decrypt public-key value = %q, err %v", value, err)
	}
	if _, err := DecryptValue(privateDER, "RENAMED_TOKEN", encrypted); err == nil {
		t.Fatal("value decrypted after its journal key was changed")
	}
}
