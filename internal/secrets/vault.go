// Package secrets manages the password-protected vault key and authenticated
// encryption used for environment variable files.
package secrets

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/crypto/argon2"
)

const (
	keyFileVersion = 1
	kdfName        = "argon2id"
	keySize        = 32
	saltSize       = 16
	kdfMemoryKiB   = 64 * 1024
	kdfIterations  = 3
	kdfParallelism = 2
	journalVersion = 1
)

// keyFile stores the parameters needed to derive a wrapping key and the
// authenticated ciphertext of the random vault key, never the key itself.
type keyFile struct {
	Version     int    `json:"version"`
	KDF         string `json:"kdf"`
	Salt        string `json:"salt"`
	MemoryKiB   uint32 `json:"memory_kib"`
	Iterations  uint32 `json:"iterations"`
	Parallelism uint8  `json:"parallelism"`
	Nonce       string `json:"nonce"`
	WrappedKey  string `json:"wrapped_key"`
}

type publicEnvelope struct {
	Version    int    `json:"version"`
	WrappedKey string `json:"wrapped_key"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

// ValidatePassword ensures a new vault password is not empty or trivially short.
func ValidatePassword(password string) error {
	if len(password) < 12 {
		return errors.New("vault password must be at least 12 bytes")
	}
	return nil
}

// Initialize generates a random vault key and stores only its password-encrypted
// form at keyPath. The returned key can be used for encryption during setup.
func Initialize(keyPath, password string) ([]byte, error) {
	if err := ValidatePassword(password); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(keyPath); err == nil {
		return nil, fmt.Errorf("vault key file %q already exists", keyPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect vault key file %q: %w", keyPath, err)
	}

	vaultKey := make([]byte, keySize)
	if _, err := rand.Read(vaultKey); err != nil {
		return nil, fmt.Errorf("generate vault key: %w", err)
	}
	salt := make([]byte, saltSize)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generate vault salt: %w", err)
	}
	passwordData := []byte(password)
	wrappingKey := argon2.IDKey(passwordData, salt, kdfIterations, kdfMemoryKiB, kdfParallelism, keySize)
	clear(passwordData)
	defer clear(wrappingKey)
	block, err := aes.NewCipher(wrappingKey)
	if err != nil {
		return nil, fmt.Errorf("create vault key cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create vault key authenticator: %w", err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate vault key nonce: %w", err)
	}
	serialized := keyFile{
		Version:     keyFileVersion,
		KDF:         kdfName,
		Salt:        base64.StdEncoding.EncodeToString(salt),
		MemoryKiB:   kdfMemoryKiB,
		Iterations:  kdfIterations,
		Parallelism: kdfParallelism,
		Nonce:       base64.StdEncoding.EncodeToString(nonce),
		WrappedKey:  base64.StdEncoding.EncodeToString(aead.Seal(nil, nonce, vaultKey, []byte("envman:vault-key:v1"))),
	}
	data, err := json.MarshalIndent(serialized, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode vault key file: %w", err)
	}
	data = append(data, '\n')
	if err := writePrivateFile(keyPath, data); err != nil {
		clear(vaultKey)
		return nil, err
	}
	return vaultKey, nil
}

// Unlock derives the wrapping key from password and authenticates the wrapped
// vault key. An incorrect password and a modified key file both fail closed.
func Unlock(keyPath, password string) ([]byte, error) {
	data, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read vault key file %q: %w", keyPath, err)
	}
	var serialized keyFile
	if err := json.Unmarshal(data, &serialized); err != nil {
		return nil, fmt.Errorf("decode vault key file %q: %w", keyPath, err)
	}
	if serialized.Version != keyFileVersion || serialized.KDF != kdfName ||
		serialized.MemoryKiB < 8*1024 || serialized.MemoryKiB > 256*1024 ||
		serialized.Iterations == 0 || serialized.Iterations > 10 ||
		serialized.Parallelism == 0 || serialized.Parallelism > 16 {
		return nil, fmt.Errorf("vault key file %q has unsupported or unsafe parameters", keyPath)
	}
	if err := ValidatePassword(password); err != nil {
		return nil, errors.New("incorrect vault password or damaged vault key file")
	}
	salt, err := base64.StdEncoding.DecodeString(serialized.Salt)
	if err != nil || len(salt) != saltSize {
		return nil, fmt.Errorf("vault key file %q has an invalid salt", keyPath)
	}
	nonce, err := base64.StdEncoding.DecodeString(serialized.Nonce)
	if err != nil {
		return nil, fmt.Errorf("vault key file %q has an invalid nonce", keyPath)
	}
	wrapped, err := base64.StdEncoding.DecodeString(serialized.WrappedKey)
	if err != nil {
		return nil, fmt.Errorf("vault key file %q has an invalid wrapped key", keyPath)
	}
	passwordData := []byte(password)
	wrappingKey := argon2.IDKey(passwordData, salt, serialized.Iterations, serialized.MemoryKiB, serialized.Parallelism, keySize)
	clear(passwordData)
	defer clear(wrappingKey)
	block, err := aes.NewCipher(wrappingKey)
	if err != nil {
		return nil, fmt.Errorf("create vault key cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create vault key authenticator: %w", err)
	}
	if len(nonce) != aead.NonceSize() {
		return nil, fmt.Errorf("vault key file %q has an invalid nonce size", keyPath)
	}
	vaultKey, err := aead.Open(nil, nonce, wrapped, []byte("envman:vault-key:v1"))
	if err != nil {
		return nil, errors.New("incorrect vault password or damaged vault key file")
	}
	if len(vaultKey) != keySize {
		return nil, fmt.Errorf("vault key file %q contains an invalid key", keyPath)
	}
	return vaultKey, nil
}

// Encrypt authenticates and encrypts plaintext with the unlocked vault key.
func Encrypt(key, plaintext []byte) ([]byte, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate data nonce: %w", err)
	}
	encrypted := aead.Seal(nil, nonce, plaintext, []byte("envman:variables:v1"))
	return append(nonce, encrypted...), nil
}

// Decrypt authenticates and decrypts an encrypted variable payload.
func Decrypt(key, ciphertext []byte) ([]byte, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < aead.NonceSize()+aead.Overhead() {
		return nil, errors.New("encrypted variables file is truncated")
	}
	nonce := ciphertext[:aead.NonceSize()]
	plaintext, err := aead.Open(nil, nonce, ciphertext[aead.NonceSize():], []byte("envman:variables:v1"))
	if err != nil {
		return nil, errors.New("cannot authenticate encrypted variables file")
	}
	return plaintext, nil
}

// CreateAsymmetricKeys creates a public key for password-free writes and an
// encrypted private key for reads. Existing destinations are never replaced.
func CreateAsymmetricKeys(publicPath, privatePath string, vaultKey []byte) error {
	if len(vaultKey) != keySize {
		return fmt.Errorf("invalid vault key size: got %d bytes", len(vaultKey))
	}
	pair, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		return fmt.Errorf("generate vault encryption keypair: %w", err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&pair.PublicKey)
	if err != nil {
		return fmt.Errorf("encode vault public key: %w", err)
	}
	publicBytes := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})
	privateDER := x509.MarshalPKCS1PrivateKey(pair)
	defer clear(privateDER)
	privateCiphertext, err := encryptWithAAD(vaultKey, privateDER, []byte("envman:private-key:v1"))
	if err != nil {
		return fmt.Errorf("encrypt vault private key: %w", err)
	}
	privateBytes := append([]byte("ENVMAN-PRIVATE-V1\n"), privateCiphertext...)
	if err := writePrivateFile(privatePath, privateBytes); err != nil {
		return err
	}
	if err := writePublicFile(publicPath, publicBytes); err != nil {
		if removeErr := os.Remove(privatePath); removeErr != nil {
			return errors.Join(err, fmt.Errorf("remove incomplete private key file: %w", removeErr))
		}
		return err
	}
	return nil
}

// EncryptValue encrypts one value using the vault's public key. It does not
// require the password or load any existing variable data.
func EncryptValue(publicPath, name, value string) ([]byte, error) {
	data, err := os.ReadFile(publicPath)
	if err != nil {
		return nil, fmt.Errorf("read vault public key %q: %w", publicPath, err)
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "PUBLIC KEY" {
		return nil, fmt.Errorf("vault public key %q is invalid", publicPath)
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("decode vault public key %q: %w", publicPath, err)
	}
	publicKey, ok := parsed.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("vault public key %q has an unsupported type", publicPath)
	}
	dataKey := make([]byte, keySize)
	if _, err := rand.Read(dataKey); err != nil {
		return nil, fmt.Errorf("generate value encryption key: %w", err)
	}
	defer clear(dataKey)
	blockCipher, err := aes.NewCipher(dataKey)
	if err != nil {
		return nil, fmt.Errorf("create value cipher: %w", err)
	}
	aead, err := cipher.NewGCM(blockCipher)
	if err != nil {
		return nil, fmt.Errorf("create value authenticator: %w", err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate value nonce: %w", err)
	}
	wrappedKey, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, publicKey, dataKey, []byte("envman:value-key:v1"))
	if err != nil {
		return nil, fmt.Errorf("wrap value encryption key: %w", err)
	}
	plaintext := []byte(value)
	defer clear(plaintext)
	document, err := json.Marshal(publicEnvelope{
		Version:    journalVersion,
		WrappedKey: base64.StdEncoding.EncodeToString(wrappedKey),
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(aead.Seal(nil, nonce, plaintext, []byte("envman:value:v1:"+name))),
	})
	if err != nil {
		return nil, fmt.Errorf("encode encrypted value: %w", err)
	}
	return document, nil
}

// DecryptValue decrypts one public-key-encrypted value using a password-unlocked
// private key in PKCS#1 DER form.
func DecryptValue(privateDER []byte, name string, encrypted []byte) ([]byte, error) {
	privateKey, err := x509.ParsePKCS1PrivateKey(privateDER)
	if err != nil {
		return nil, fmt.Errorf("decode vault private key: %w", err)
	}
	var document publicEnvelope
	if err := json.Unmarshal(encrypted, &document); err != nil {
		return nil, fmt.Errorf("decode encrypted value: %w", err)
	}
	if document.Version != journalVersion {
		return nil, fmt.Errorf("unsupported encrypted value version %d", document.Version)
	}
	wrappedKey, err := base64.StdEncoding.DecodeString(document.WrappedKey)
	if err != nil {
		return nil, fmt.Errorf("decode wrapped value key: %w", err)
	}
	nonce, err := base64.StdEncoding.DecodeString(document.Nonce)
	if err != nil {
		return nil, fmt.Errorf("decode value nonce: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(document.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decode encrypted value: %w", err)
	}
	dataKey, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, privateKey, wrappedKey, []byte("envman:value-key:v1"))
	if err != nil {
		return nil, errors.New("cannot unwrap encrypted value key")
	}
	defer clear(dataKey)
	blockCipher, err := aes.NewCipher(dataKey)
	if err != nil {
		return nil, fmt.Errorf("create value cipher: %w", err)
	}
	aead, err := cipher.NewGCM(blockCipher)
	if err != nil {
		return nil, fmt.Errorf("create value authenticator: %w", err)
	}
	if len(nonce) != aead.NonceSize() {
		return nil, errors.New("encrypted value has an invalid nonce")
	}
	value, err := aead.Open(nil, nonce, ciphertext, []byte("envman:value:v1:"+name))
	if err != nil {
		return nil, errors.New("cannot authenticate encrypted value")
	}
	return value, nil
}

// UnlockPrivateKey decrypts the vault private key using the unlocked vault key.
func UnlockPrivateKey(path string, vaultKey []byte) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read encrypted vault private key %q: %w", path, err)
	}
	const header = "ENVMAN-PRIVATE-V1\n"
	if !bytes.HasPrefix(data, []byte(header)) {
		return nil, fmt.Errorf("encrypted vault private key %q has an unsupported format", path)
	}
	return decryptWithAAD(vaultKey, data[len(header):], []byte("envman:private-key:v1"))
}

func encryptWithAAD(key, plaintext, aad []byte) ([]byte, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate private key nonce: %w", err)
	}
	return append(nonce, aead.Seal(nil, nonce, plaintext, aad)...), nil
}

func decryptWithAAD(key, ciphertext, aad []byte) ([]byte, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < aead.NonceSize()+aead.Overhead() {
		return nil, errors.New("encrypted private key is truncated")
	}
	plaintext, err := aead.Open(nil, ciphertext[:aead.NonceSize()], ciphertext[aead.NonceSize():], aad)
	if err != nil {
		return nil, errors.New("cannot authenticate encrypted private key")
	}
	return plaintext, nil
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != keySize {
		return nil, fmt.Errorf("invalid vault key size: got %d bytes", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create variables cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create variables authenticator: %w", err)
	}
	return aead, nil
}

func writePrivateFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create vault key directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create vault key file %q: %w", path, err)
	}
	n, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil {
		return errors.Join(fmt.Errorf("write vault key file: %w", writeErr), removeKeyFile(path))
	}
	if n != len(data) {
		return errors.Join(fmt.Errorf("write vault key file: %w", io.ErrShortWrite), removeKeyFile(path))
	}
	if closeErr != nil {
		return errors.Join(fmt.Errorf("close vault key file: %w", closeErr), removeKeyFile(path))
	}
	return nil
}

func writePublicFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create vault public-key directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("create vault public key %q: %w", path, err)
	}
	n, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil {
		return errors.Join(fmt.Errorf("write vault public key: %w", writeErr), os.Remove(path))
	}
	if n != len(data) {
		return errors.Join(fmt.Errorf("write vault public key: %w", io.ErrShortWrite), os.Remove(path))
	}
	if closeErr != nil {
		return errors.Join(fmt.Errorf("close vault public key: %w", closeErr), os.Remove(path))
	}
	return nil
}

func removeKeyFile(path string) error {
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove incomplete vault key file %q: %w", path, err)
	}
	return nil
}
