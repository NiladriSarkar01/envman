package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInitializeStorageCreatesStorageWithoutOverwritingConfig(t *testing.T) {
	root := t.TempDir()
	paths, err := initializeAt(root)
	if err != nil {
		t.Fatalf("initialize storage: %v", err)
	}
	if paths.Root != root || paths.Environments != filepath.Join(root, "environments") {
		t.Fatalf("unexpected storage paths: %+v", paths)
	}
	configBytes, err := os.ReadFile(paths.Config)
	if err != nil {
		t.Fatalf("read storage config: %v", err)
	}
	var got Config
	if err := json.Unmarshal(configBytes, &got); err != nil {
		t.Fatalf("decode storage config: %v", err)
	}
	if got.CurrentEnvironment != "" {
		t.Errorf("initial current environment = %q, want empty", got.CurrentEnvironment)
	}
	if got.Vault.KeyFile != filepath.Join(root, "vault.key") {
		t.Errorf("vault key file = %q, want %q", got.Vault.KeyFile, filepath.Join(root, "vault.key"))
	}
	if got.Vault.PublicKeyFile != filepath.Join(root, "vault.pub") {
		t.Errorf("vault public key file = %q, want %q", got.Vault.PublicKeyFile, filepath.Join(root, "vault.pub"))
	}
	if got.Vault.PrivateKeyFile != filepath.Join(root, "vault.private") {
		t.Errorf("vault private key file = %q, want %q", got.Vault.PrivateKeyFile, filepath.Join(root, "vault.private"))
	}
	if got.Vault.RevealSecretsByDefault {
		t.Error("secret values are revealed by default; want masking enabled")
	}

	const existing = "{\n  \"current_environment\": \"preserve\"\n}\n"
	if err := os.WriteFile(paths.Config, []byte(existing), 0o600); err != nil {
		t.Fatalf("write existing storage config: %v", err)
	}
	if _, err := initializeAt(root); err != nil {
		t.Fatalf("initialize storage again: %v", err)
	}
	configBytes, err = os.ReadFile(paths.Config)
	if err != nil {
		t.Fatalf("read storage config after repeat: %v", err)
	}
	if string(configBytes) != existing {
		t.Errorf("storage config overwritten: got %q, want %q", configBytes, existing)
	}
}

func TestInitializeProjectCreatesMarkerAndRegistersProject(t *testing.T) {
	projectRoot := t.TempDir()
	storageRoot := t.TempDir()
	t.Setenv(StorageDirectoryEnv, storageRoot)
	storage, err := initializeAt(storageRoot)
	if err != nil {
		t.Fatalf("initialize storage: %v", err)
	}
	paths, err := initializeProjectAt(projectRoot, storage.Environments)
	if err != nil {
		t.Fatalf("initialize project: %v", err)
	}
	if paths.ID == "" || paths.Marker != filepath.Join(projectRoot, MarkerName) {
		t.Fatalf("unexpected initialized project paths: %+v", paths)
	}
	if paths.Metadata != filepath.Join(storage.Environments, paths.ID, "project.json") {
		t.Fatalf("unexpected metadata path: %q", paths.Metadata)
	}
	if paths.Environments != filepath.Join(storage.Environments, paths.ID, "environments") {
		t.Fatalf("unexpected environment path: %q", paths.Environments)
	}

	markerBytes, err := os.ReadFile(paths.Marker)
	if err != nil {
		t.Fatalf("read project marker: %v", err)
	}
	var marker ProjectMarker
	if err := json.Unmarshal(markerBytes, &marker); err != nil {
		t.Fatalf("decode project marker: %v", err)
	}
	if marker.ID != paths.ID {
		t.Errorf("marker ID = %q, want %q", marker.ID, paths.ID)
	}

	const activeEnvironment = "dev"
	metadata := ProjectMetadata{ID: paths.ID, Name: paths.Name, Path: paths.Root, ActiveEnvironment: activeEnvironment}
	if err := SaveProjectMetadata(paths, metadata); err != nil {
		t.Fatalf("save project active environment: %v", err)
	}
	repeated, err := initializeProjectAt(projectRoot, storage.Environments)
	if err != nil {
		t.Fatalf("initialize project again: %v", err)
	}
	if repeated.ID != paths.ID {
		t.Errorf("project ID changed: got %q, want %q", repeated.ID, paths.ID)
	}
	_, metadata, err = ProjectPathsByID(paths.ID)
	if err != nil {
		t.Fatalf("read project metadata: %v", err)
	}
	if metadata.ActiveEnvironment != activeEnvironment {
		t.Errorf("active environment lost on reinitialization: got %q", metadata.ActiveEnvironment)
	}
}

func TestLoadCurrentProjectRequiresMarkerToMatchRegistration(t *testing.T) {
	projectRoot := t.TempDir()
	storageRoot := t.TempDir()
	t.Setenv(StorageDirectoryEnv, storageRoot)
	storage, err := initializeAt(storageRoot)
	if err != nil {
		t.Fatalf("initialize storage: %v", err)
	}
	paths, err := initializeProjectAt(projectRoot, storage.Environments)
	if err != nil {
		t.Fatalf("initialize project: %v", err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatalf("change to project directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWD); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})

	// LoadCurrentProject reads the configured storage root from ENVMAN_HOME.
	t.Setenv(StorageDirectoryEnv, storageRoot)
	loaded, metadata, err := LoadCurrentProject()
	if err != nil {
		t.Fatalf("load current project: %v", err)
	}
	if loaded.ID != paths.ID || metadata.Name != paths.Name {
		t.Errorf("loaded project = %+v, %+v; want ID %q and name %q", loaded, metadata, paths.ID, paths.Name)
	}
}

func TestEnvironmentNameRejectsTraversal(t *testing.T) {
	for _, name := range []string{"", ".", "..", "../other", `..\other`, "with space"} {
		if err := ValidateEnvironmentName(name); err == nil {
			t.Errorf("ValidateEnvironmentName(%q) succeeded", name)
		}
	}
	if err := ValidateEnvironmentName("production-1"); err != nil {
		t.Errorf("valid environment name rejected: %v", err)
	}
}
