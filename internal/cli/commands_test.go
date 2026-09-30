package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/envman/internal/config"
)

func TestEnvironmentCreateRequiresVaultSetup(t *testing.T) {
	projectRoot := t.TempDir()
	storageRoot := t.TempDir()
	t.Setenv(config.StorageDirectoryEnv, storageRoot)
	setWorkingDirectory(t, projectRoot)

	app, err := New()
	if err != nil {
		t.Fatalf("create CLI: %v", err)
	}
	var output bytes.Buffer
	run := func(args ...string) error {
		t.Helper()
		return app.ExecuteWithIO(args, strings.NewReader(""), &output, &output)
	}
	if err := run("init"); err != nil {
		t.Fatalf("initialize project: %v", err)
	}
	if err := run("env", "create", "dev"); err == nil || !strings.Contains(err.Error(), "envman setup") {
		t.Fatalf("create without setup error = %v, want setup-required error", err)
	}
	paths, _, err := config.LoadCurrentProject()
	if err != nil {
		t.Fatalf("load project: %v", err)
	}
	if _, err := os.Stat(filepath.Join(paths.Environments, "dev")); !os.IsNotExist(err) {
		t.Fatalf("environment was created before vault setup: err %v", err)
	}

	const password = "correct horse battery staple"
	if err := run("setup"); err == nil {
		t.Fatal("setup unexpectedly succeeded without password input")
	}
	output.Reset()
	if err := app.ExecuteWithIO([]string{"setup"}, strings.NewReader(password+"\n"+password+"\n"), &output, &output); err != nil {
		t.Fatalf("setup vault: %v", err)
	}
	output.Reset()
	if err := run("env", "create", "dev"); err != nil {
		t.Fatalf("create after setup: %v", err)
	}
}

func TestEnvironmentCreateWithoutSetupHasNoFilesystemSideEffects(t *testing.T) {
	projectRoot := t.TempDir()
	storageRoot := filepath.Join(t.TempDir(), "not-created")
	t.Setenv(config.StorageDirectoryEnv, storageRoot)
	setWorkingDirectory(t, projectRoot)

	app, err := New()
	if err != nil {
		t.Fatalf("create CLI: %v", err)
	}
	var output bytes.Buffer
	err = app.ExecuteWithIO([]string{"env", "create", "dev"}, strings.NewReader(""), &output, &output)
	if err == nil || !strings.Contains(err.Error(), "envman setup") {
		t.Fatalf("create without setup error = %v, want setup-required error", err)
	}
	if _, err := os.Stat(storageRoot); !os.IsNotExist(err) {
		t.Fatalf("failed create initialized shared storage: err %v", err)
	}
	projectEntries, err := os.ReadDir(projectRoot)
	if err != nil {
		t.Fatalf("read project directory: %v", err)
	}
	if len(projectEntries) != 0 {
		t.Fatalf("failed create wrote project files: %v", projectEntries)
	}
}

func TestProjectEnvironmentAndVariableCommands(t *testing.T) {
	if os.Getenv("ENVMAN_CLI_HELPER") == "1" {
		if got := os.Getenv("ENVMAN_CLI_VALUE"); got != "injected" {
			t.Fatalf("child environment value = %q, want injected", got)
		}
		return
	}

	projectRoot := t.TempDir()
	storageRoot := t.TempDir()
	t.Setenv(config.StorageDirectoryEnv, storageRoot)
	setWorkingDirectory(t, projectRoot)

	app, err := New()
	if err != nil {
		t.Fatalf("create CLI: %v", err)
	}
	run := func(args []string, input string) (string, error) {
		t.Helper()
		var output bytes.Buffer
		err := app.ExecuteWithIO(args, strings.NewReader(input), &output, &output)
		return output.String(), err
	}

	const password = "correct horse battery staple"
	if _, err := run([]string{"setup"}, password+"\n"+password+"\n"); err != nil {
		t.Fatalf("setup vault: %v", err)
	}
	if _, err := run([]string{"init"}, ""); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, config.MarkerName)); err != nil {
		t.Fatalf("project marker was not created: %v", err)
	}
	if _, err := run([]string{"project", "info"}, ""); err != nil {
		t.Fatalf("project info: %v", err)
	}
	if _, err := run([]string{"project", "list"}, ""); err != nil {
		t.Fatalf("project list: %v", err)
	}
	if _, err := run([]string{"env", "create", "dev"}, ""); err != nil {
		t.Fatalf("create dev: %v", err)
	}
	if _, err := run([]string{"env", "create", "test"}, ""); err != nil {
		t.Fatalf("create test: %v", err)
	}
	if _, err := run([]string{"env", "use", "dev"}, ""); err != nil {
		t.Fatalf("use dev: %v", err)
	}
	if _, err := run([]string{"env", "rename", "dev", "development"}, ""); err != nil {
		t.Fatalf("rename active environment: %v", err)
	}
	if _, err := run([]string{"env", "current"}, ""); err != nil {
		t.Fatalf("env current after rename: %v", err)
	}
	if _, err := run([]string{"env", "rename", "development", "dev"}, ""); err != nil {
		t.Fatalf("rename environment back: %v", err)
	}
	if _, err := run([]string{"set", "PORT", "8080"}, ""); err != nil {
		t.Fatalf("set PORT: %v", err)
	}
	if _, err := run([]string{"set", "JWT_SECRET", "secret-value"}, ""); err != nil {
		t.Fatalf("set JWT_SECRET: %v", err)
	}
	if _, err := run([]string{"set", "ENVMAN_CLI_HELPER", "1"}, ""); err != nil {
		t.Fatalf("set run helper: %v", err)
	}
	if _, err := run([]string{"set", "ENVMAN_CLI_VALUE", "injected"}, ""); err != nil {
		t.Fatalf("set run value: %v", err)
	}
	if _, err := run([]string{"env", "use", "test"}, ""); err != nil {
		t.Fatalf("use test to prepare run environment: %v", err)
	}
	if _, err := run([]string{"set", "ENVMAN_CLI_HELPER", "1"}, ""); err != nil {
		t.Fatalf("set test run helper: %v", err)
	}
	if _, err := run([]string{"set", "ENVMAN_CLI_VALUE", "injected"}, ""); err != nil {
		t.Fatalf("set test run value: %v", err)
	}
	if _, err := run([]string{"env", "use", "dev"}, ""); err != nil {
		t.Fatalf("switch back to dev: %v", err)
	}
	if output, err := run([]string{"get", "JWT_SECRET"}, password+"\n"); err != nil || strings.TrimSpace(strings.TrimPrefix(output, "Vault password: ")) != "********" {
		t.Fatalf("masked get = %q, err %v", output, err)
	}
	if output, err := run([]string{"get", "JWT_SECRET", "--show"}, password+"\n"); err != nil || strings.TrimSpace(strings.TrimPrefix(output, "Vault password: ")) != "secret-value" {
		t.Fatalf("show get = %q, err %v", output, err)
	}
	storage, err := config.InitializeStorage()
	if err != nil {
		t.Fatalf("load shared storage: %v", err)
	}
	cfg, err := config.LoadConfig(storage)
	if err != nil {
		t.Fatalf("load EnvMan config: %v", err)
	}
	cfg.Vault.RevealSecretsByDefault = true
	if err := config.SaveConfig(storage, cfg); err != nil {
		t.Fatalf("enable reveal-by-default: %v", err)
	}
	if output, err := run([]string{"get", "JWT_SECRET"}, password+"\n"); err != nil || strings.TrimSpace(strings.TrimPrefix(output, "Vault password: ")) != "secret-value" {
		t.Fatalf("configured default get = %q, err %v", output, err)
	}
	cfg.Vault.RevealSecretsByDefault = false
	if err := config.SaveConfig(storage, cfg); err != nil {
		t.Fatalf("restore masked default: %v", err)
	}
	if output, err := run([]string{"list"}, ""); err != nil || !strings.Contains(output, "PORT") || !strings.Contains(output, "JWT_SECRET") || strings.Contains(output, "********") {
		t.Fatalf("list output = %q, err %v", output, err)
	}
	if output, err := run([]string{"list", "--show"}, password+"\n"); err != nil || !strings.Contains(output, "secret-value") {
		t.Fatalf("list --show output = %q, err %v", output, err)
	}
	if _, err := run([]string{"env", "current"}, ""); err != nil {
		t.Fatalf("env current: %v", err)
	}
	if _, err := run([]string{"run", "--env", "test", "--", os.Args[0], "-test.run=^TestProjectEnvironmentAndVariableCommands$"}, password+"\n"); err != nil {
		t.Fatalf("run command with temporary environment: %v", err)
	}
	if output, err := run([]string{"env", "current"}, ""); err != nil || !strings.Contains(output, "Environment: dev") {
		t.Fatalf("temporary run changed active environment: output %q, err %v", output, err)
	}
	if _, err := run([]string{"run", "--", os.Args[0], "-test.run=^TestProjectEnvironmentAndVariableCommands$"}, password+"\n"); err != nil {
		t.Fatalf("run command with active environment: %v", err)
	}

	importFile := filepath.Join(projectRoot, "source.env")
	if err := os.WriteFile(importFile, []byte("IMPORTED=value\n"), 0o600); err != nil {
		t.Fatalf("write import fixture: %v", err)
	}
	if _, err := run([]string{"import", importFile}, ""); err != nil {
		t.Fatalf("import .env: %v", err)
	}
	exportFile := filepath.Join(projectRoot, "exported.env")
	if _, err := run([]string{"export", exportFile}, password+"\n"); err != nil {
		t.Fatalf("export .env: %v", err)
	}
	if _, err := os.Stat(exportFile); err != nil {
		t.Fatalf("export file not created: %v", err)
	}
	exampleFile := filepath.Join(projectRoot, "example.env")
	if _, err := run([]string{"export", "--example", exampleFile}, ""); err != nil {
		t.Fatalf("export example without password: %v", err)
	}
	exampleData, err := os.ReadFile(exampleFile)
	if err != nil {
		t.Fatalf("read example export: %v", err)
	}
	if bytes.Contains(exampleData, []byte("secret-value")) || !bytes.Contains(exampleData, []byte("JWT_SECRET=")) {
		t.Fatalf("example export contains values or is missing keys: %q", exampleData)
	}

	if _, err := run([]string{"env", "use", "test"}, ""); err != nil {
		t.Fatalf("switch to test: %v", err)
	}
	if _, err := run([]string{"env", "delete", "test", "--yes"}, ""); err == nil {
		t.Fatal("deleting the active environment unexpectedly succeeded")
	}
	if _, err := run([]string{"env", "delete", "dev"}, "y\n"); err != nil {
		t.Fatalf("delete dev: %v", err)
	}
	if _, err := run([]string{"unset", "ENVMAN_CLI_HELPER"}, ""); err != nil {
		t.Fatalf("unset ENVMAN_CLI_HELPER: %v", err)
	}
	if _, err := run([]string{"unset", "PORT"}, ""); err == nil {
		t.Fatal("unset missing variable unexpectedly succeeded")
	}
	if _, err := run([]string{"project", "remove"}, "n\n"); err != nil {
		t.Fatalf("cancel project removal: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, config.MarkerName)); err != nil {
		t.Fatalf("cancelled removal deleted project marker: %v", err)
	}
	if _, err := run([]string{"project", "remove", "--yes"}, ""); err != nil {
		t.Fatalf("remove project: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, config.MarkerName)); !os.IsNotExist(err) {
		t.Errorf("project marker still exists after removal: err = %v", err)
	}
	if _, err := os.Stat(projectRoot); err != nil {
		t.Errorf("project source directory was removed: %v", err)
	}
}

func TestSetupStoresOnlyKeyFileLocationAndRejectsWrongPassword(t *testing.T) {
	projectRoot := t.TempDir()
	storageRoot := t.TempDir()
	t.Setenv(config.StorageDirectoryEnv, storageRoot)
	setWorkingDirectory(t, projectRoot)
	app, err := New()
	if err != nil {
		t.Fatalf("create CLI: %v", err)
	}
	run := func(args []string, input string) error {
		t.Helper()
		var output bytes.Buffer
		return app.ExecuteWithIO(args, strings.NewReader(input), &output, &output)
	}

	const password = "test vault password with length"
	if err := run([]string{"setup"}, "short\nshort\n"); err == nil {
		t.Fatal("setup accepted a short password")
	}
	if err := run([]string{"setup"}, password+"\n"+password+"\n"); err != nil {
		t.Fatalf("setup vault: %v", err)
	}
	storage, err := config.InitializeStorage()
	if err != nil {
		t.Fatalf("initialize storage: %v", err)
	}
	cfgBytes, err := os.ReadFile(storage.Config)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if bytes.Contains(cfgBytes, []byte(password)) || !bytes.Contains(cfgBytes, []byte("vault.key")) {
		t.Fatalf("config should contain the key-file location but not the password: %s", cfgBytes)
	}

	if err := run([]string{"init"}, ""); err != nil {
		t.Fatalf("initialize project: %v", err)
	}
	if err := run([]string{"env", "create", "dev"}, ""); err != nil {
		t.Fatalf("create environment: %v", err)
	}
	if err := run([]string{"env", "use", "dev"}, ""); err != nil {
		t.Fatalf("select environment: %v", err)
	}
	if err := run([]string{"set", "API_TOKEN", "plaintext-must-not-be-saved"}, ""); err != nil {
		t.Fatalf("set without unlocking vault: %v", err)
	}
	paths, _, err := config.LoadCurrentProject()
	if err != nil {
		t.Fatalf("load current project: %v", err)
	}
	variablesPath := filepath.Join(paths.Environments, "dev", "variables.json")
	variableData, err := os.ReadFile(variablesPath + ".journal")
	if err != nil {
		t.Fatalf("read appended variable file: %v", err)
	}
	if bytes.Contains(variableData, []byte("plaintext-must-not-be-saved")) {
		t.Fatal("password-free set wrote the variable value in plaintext")
	}
}

func setWorkingDirectory(t *testing.T, path string) {
	t.Helper()
	current, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	if err := os.Chdir(path); err != nil {
		t.Fatalf("change working directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(current); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
}
