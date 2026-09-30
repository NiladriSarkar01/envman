package store

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/envman/internal/secrets"
)

func TestJournalRoundTripAndDotEnvExport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "variables.json")
	publicKey, privateKey, _ := testKeyPair(t)
	want := map[string]string{
		"PORT":         "8080",
		"DATABASE_URL": "postgres://localhost/dev?sslmode=disable",
		"JWT_SECRET":   "secret-value",
	}
	if err := AppendValues(path, want, publicKey); err != nil {
		t.Fatalf("append variables: %v", err)
	}
	got, err := Load(path, nil, privateKey)
	if err != nil {
		t.Fatalf("load variables: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("loaded values = %#v, want %#v", got, want)
	}
	fileBytes, err := os.ReadFile(path + ".journal")
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	for _, value := range want {
		if bytes.Contains(fileBytes, []byte(value)) {
			t.Errorf("journal contains plaintext value %q", value)
		}
	}

	dotEnvPath := filepath.Join(t.TempDir(), ".env")
	if err := WriteDotEnv(dotEnvPath, got); err != nil {
		t.Fatalf("export .env: %v", err)
	}
	file, err := os.Open(dotEnvPath)
	if err != nil {
		t.Fatalf("open exported .env: %v", err)
	}
	roundTrip, parseErr := ParseDotEnv(file)
	closeErr := file.Close()
	if parseErr != nil {
		t.Fatalf("parse exported .env: %v", parseErr)
	}
	if closeErr != nil {
		t.Fatalf("close exported .env: %v", closeErr)
	}
	if !reflect.DeepEqual(roundTrip, want) {
		t.Errorf(".env round trip = %#v, want %#v", roundTrip, want)
	}
}

func TestAppendDoesNotReadOrDecryptExistingValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "variables.json")
	legacy, err := json.Marshal(map[string]string{"OLD": "plain legacy value"})
	if err != nil {
		t.Fatalf("encode legacy variables: %v", err)
	}
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}
	publicKey, privateKey, _ := testKeyPair(t)
	if err := AppendValue(path, "NEW", "new secret", publicKey); err != nil {
		t.Fatalf("append without decrypting existing file: %v", err)
	}
	values, err := Load(path, nil, privateKey)
	if err != nil {
		t.Fatalf("load combined values: %v", err)
	}
	want := map[string]string{"OLD": "plain legacy value", "NEW": "new secret"}
	if !reflect.DeepEqual(values, want) {
		t.Fatalf("combined values = %#v, want %#v", values, want)
	}
}

func TestAppendDoesNotDecodeExistingEnvironmentFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "variables.json")
	if err := os.WriteFile(path, []byte("existing data that is not JSON"), 0o600); err != nil {
		t.Fatalf("write existing file: %v", err)
	}
	publicKey, _, _ := testKeyPair(t)
	if err := AppendValue(path, "NEW", "value", publicKey); err != nil {
		t.Fatalf("append without decoding existing file: %v", err)
	}
	if _, err := os.Stat(path + ".journal"); err != nil {
		t.Fatalf("append-only journal was not created: %v", err)
	}
	if _, err := Load(path, nil, nil); err == nil {
		t.Fatal("load unexpectedly accepted corrupt legacy base file")
	}
}

func TestKeysDoNotDecodeBaseVariableValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "variables.json")
	if err := os.WriteFile(path, []byte(`{"TOKEN":{"value":"not-a-string"},"PORT":8080}`), 0o600); err != nil {
		t.Fatalf("write base variables: %v", err)
	}
	names, err := Keys(path)
	if err != nil {
		t.Fatalf("list keys without decoding values: %v", err)
	}
	if want := []string{"PORT", "TOKEN"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("keys = %v, want %v", names, want)
	}
}

func TestKeysAndDeleteDoNotDecryptValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "variables.json")
	publicKey, privateKey, _ := testKeyPair(t)
	if err := AppendValues(path, map[string]string{"TOKEN": "secret", "PORT": "8080"}, publicKey); err != nil {
		t.Fatalf("append variables: %v", err)
	}
	names, err := Keys(path)
	if err != nil {
		t.Fatalf("list names without private key: %v", err)
	}
	if want := []string{"PORT", "TOKEN"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("keys = %v, want %v", names, want)
	}
	if err := AppendDelete(path, "TOKEN"); err != nil {
		t.Fatalf("append delete without private key: %v", err)
	}
	names, err = Keys(path)
	if err != nil || !reflect.DeepEqual(names, []string{"PORT"}) {
		t.Fatalf("keys after delete = %v, err %v", names, err)
	}
	values, err := Load(path, nil, privateKey)
	if err != nil || !reflect.DeepEqual(values, map[string]string{"PORT": "8080"}) {
		t.Fatalf("values after delete = %#v, err %v", values, err)
	}
}

func TestLoadSupportsLegacyPlaintext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "variables.json")
	want := map[string]string{"TOKEN": "legacy-secret"}
	plain, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("encode legacy variables: %v", err)
	}
	if err := os.WriteFile(path, plain, 0o600); err != nil {
		t.Fatalf("write legacy variables: %v", err)
	}
	got, err := Load(path, nil, nil)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("load legacy variables = %#v, err %v", got, err)
	}
	names, err := Keys(path)
	if err != nil || !reflect.DeepEqual(names, []string{"TOKEN"}) {
		t.Fatalf("legacy keys = %v, err %v", names, err)
	}
}

func TestWriteDotEnvNeverOverwritesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	const existing = "DO_NOT_TOUCH=yes\n"
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatalf("write existing file: %v", err)
	}
	if err := WriteDotEnv(path, map[string]string{"NEW": "value"}); err == nil {
		t.Fatal("export overwrote an existing file")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read existing file: %v", err)
	}
	if string(got) != existing {
		t.Errorf("existing file changed: got %q, want %q", got, existing)
	}
}

func TestParseDotEnvSupportsCommentsExportAndQuotedValues(t *testing.T) {
	input := "# comment\nexport PORT=8080\nNAME='local dev'\nURL=\"https://example.test?a=b\"\nEMPTY=\n"
	got, err := ParseDotEnv(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parse .env: %v", err)
	}
	want := map[string]string{"PORT": "8080", "NAME": "local dev", "URL": "https://example.test?a=b", "EMPTY": ""}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parsed values = %#v, want %#v", got, want)
	}
}

func TestSecretNameDetection(t *testing.T) {
	for _, name := range []string{"JWT_SECRET", "DB_PASSWORD", "SERVICE_TOKEN", "API_KEY", "DATABASE_URL"} {
		if !IsSecret(name) {
			t.Errorf("%q not detected as secret", name)
		}
	}
	for _, name := range []string{"PORT", "APP_NAME"} {
		if IsSecret(name) {
			t.Errorf("%q unexpectedly detected as secret", name)
		}
	}
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	variables, err := Load(filepath.Join(t.TempDir(), "missing.json"), nil, nil)
	if err != nil || len(variables) != 0 {
		t.Fatalf("load missing file = %#v, err %v", variables, err)
	}
}

func testKeyPair(t *testing.T) (string, []byte, []byte) {
	t.Helper()
	dir := t.TempDir()
	symmetricKey := bytes.Repeat([]byte{0x32}, 32)
	publicPath := filepath.Join(dir, "vault.pub")
	privatePath := filepath.Join(dir, "vault.private")
	if err := secrets.CreateAsymmetricKeys(publicPath, privatePath, symmetricKey); err != nil {
		t.Fatalf("create asymmetric keys: %v", err)
	}
	privateKey, err := secrets.UnlockPrivateKey(privatePath, symmetricKey)
	if err != nil {
		t.Fatalf("unlock private key: %v", err)
	}
	return publicPath, privateKey, symmetricKey
}
