package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/envman/internal/config"
	"github.com/envman/internal/secrets"
)

// runSetup creates shared storage and initializes its password-protected vault.
// The password is confirmed before a random vault key is created and saved in
// password-wrapped form; only the key-file path is written to config.json.
func runSetup(ctx Context) error {
	if len(ctx.Args) != 0 {
		return fmt.Errorf("setup does not accept arguments")
	}
	paths, err := config.InitializeStorage()
	if err != nil {
		return fmt.Errorf("set up EnvMan storage: %w", err)
	}
	cfg, err := config.LoadConfig(paths)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(cfg.Vault.KeyFile); err == nil {
		return fmt.Errorf("vault is already set up at %q", cfg.Vault.KeyFile)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect vault key file: %w", err)
	}
	password, err := readPassword(ctx, "Create vault password (minimum 12 bytes): ")
	if err != nil {
		return err
	}
	defer clear(password)
	confirmation, err := readPassword(ctx, "Confirm vault password: ")
	if err != nil {
		return err
	}
	defer clear(confirmation)
	if string(password) != string(confirmation) {
		return errors.New("vault passwords do not match")
	}
	key, err := secrets.Initialize(cfg.Vault.KeyFile, string(password))
	if err != nil {
		return fmt.Errorf("initialize vault: %w", err)
	}
	if err := secrets.CreateAsymmetricKeys(cfg.Vault.PublicKeyFile, cfg.Vault.PrivateKeyFile, key); err != nil {
		clear(key)
		if removeErr := os.Remove(cfg.Vault.KeyFile); removeErr != nil {
			return errors.Join(fmt.Errorf("create vault encryption keys: %w", err), fmt.Errorf("remove incomplete vault key file: %w", removeErr))
		}
		return fmt.Errorf("create vault encryption keys: %w", err)
	}
	clear(key)
	if err := config.SaveConfig(paths, cfg); err != nil {
		removeErr := errors.Join(os.Remove(cfg.Vault.KeyFile), os.Remove(cfg.Vault.PublicKeyFile), os.Remove(cfg.Vault.PrivateKeyFile))
		if removeErr != nil {
			return errors.Join(fmt.Errorf("save EnvMan config: %w", err), fmt.Errorf("remove incomplete vault key files: %w", removeErr))
		}
		return fmt.Errorf("save EnvMan config: %w", err)
	}
	if _, err := fmt.Fprintf(ctx.Out, "%s\n  %s %s\n  %s %s\n  %s %s\n  %s %s\n\n%s\n",
		success(ctx.Out, "Vault is ready."),
		label(ctx.Out, "Root:"), paint(ctx.Out, colorDim, paths.Root),
		label(ctx.Out, "Config:"), paint(ctx.Out, colorCyan, paths.Config),
		label(ctx.Out, "Projects:"), paint(ctx.Out, colorDim, paths.Environments),
		label(ctx.Out, "Key file:"), paint(ctx.Out, colorDim, cfg.Vault.KeyFile),
		warning(ctx.Out, "Keep your password safe; it cannot be reset to recover encrypted values.")); err != nil {
		return fmt.Errorf("write setup result: %w", err)
	}
	return nil
}
