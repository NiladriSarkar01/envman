package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/envman/internal/config"
	"github.com/envman/internal/secrets"
	"golang.org/x/term"
)

type unlockedVault struct {
	symmetricKey []byte
	privateKey   []byte
}

func (vault *unlockedVault) clear() {
	clear(vault.symmetricKey)
	clear(vault.privateKey)
}

func unlockVault(ctx Context) (*unlockedVault, config.Config, error) {
	_, cfg, err := loadVaultConfig()
	if err != nil {
		return nil, config.Config{}, err
	}
	password, err := readPassword(ctx, "Vault password: ")
	if err != nil {
		return nil, config.Config{}, err
	}
	defer clear(password)
	key, err := secrets.Unlock(cfg.Vault.KeyFile, string(password))
	if err != nil {
		return nil, config.Config{}, err
	}
	privateKey, err := secrets.UnlockPrivateKey(cfg.Vault.PrivateKeyFile, key)
	if err != nil {
		clear(key)
		return nil, config.Config{}, err
	}
	return &unlockedVault{symmetricKey: key, privateKey: privateKey}, cfg, nil
}

func loadVaultConfig() (config.Paths, config.Config, error) {
	paths, err := config.StoragePaths()
	if err != nil {
		return config.Paths{}, config.Config{}, err
	}
	if _, err := os.Stat(paths.Config); errors.Is(err, os.ErrNotExist) {
		return config.Paths{}, config.Config{}, errors.New("vault is not set up; run 'envman setup' first")
	} else if err != nil {
		return config.Paths{}, config.Config{}, fmt.Errorf("inspect EnvMan config: %w", err)
	}
	cfg, err := config.LoadConfig(paths)
	if err != nil {
		return config.Paths{}, config.Config{}, err
	}
	if _, err := os.Stat(cfg.Vault.KeyFile); errors.Is(err, os.ErrNotExist) {
		return config.Paths{}, config.Config{}, errors.New("vault is not set up; run 'envman setup' first")
	} else if err != nil {
		return config.Paths{}, config.Config{}, fmt.Errorf("inspect vault key file: %w", err)
	}
	for _, keyPath := range []string{cfg.Vault.PublicKeyFile, cfg.Vault.PrivateKeyFile} {
		if _, err := os.Stat(keyPath); errors.Is(err, os.ErrNotExist) {
			return config.Paths{}, config.Config{}, fmt.Errorf("vault key file %q is missing; restore it from backup", keyPath)
		} else if err != nil {
			return config.Paths{}, config.Config{}, fmt.Errorf("inspect vault key file %q: %w", keyPath, err)
		}
	}
	return paths, cfg, nil
}

func requireVaultSetup() error {
	_, _, err := loadVaultConfig()
	return err
}

// readPassword disables terminal echo for interactive use. When input is
// piped, it consumes only the password line so a following command can still
// receive the remainder of stdin.
func readPassword(ctx Context, prompt string) ([]byte, error) {
	if _, err := io.WriteString(ctx.Out, paint(ctx.Out, colorBold+colorCyan, prompt)); err != nil {
		return nil, fmt.Errorf("write password prompt: %w", err)
	}
	if file, ok := ctx.In.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		password, err := term.ReadPassword(int(file.Fd()))
		if _, writeErr := io.WriteString(ctx.Out, "\n"); writeErr != nil {
			clear(password)
			return nil, fmt.Errorf("write password prompt newline: %w", writeErr)
		}
		if err != nil {
			clear(password)
			return nil, fmt.Errorf("read vault password: %w", err)
		}
		if len(password) == 0 {
			return nil, errors.New("vault password cannot be empty")
		}
		return password, nil
	}

	password := make([]byte, 0, 64)
	var one [1]byte
	for {
		n, err := ctx.In.Read(one[:])
		if n > 0 {
			if one[0] == '\n' {
				break
			}
			if len(password) >= 4096 {
				clear(password)
				return nil, errors.New("vault password exceeds 4096 bytes")
			}
			password = append(password, one[0])
		}
		if err != nil {
			if errors.Is(err, io.EOF) && len(password) > 0 {
				break
			}
			clear(password)
			if errors.Is(err, io.EOF) {
				return nil, errors.New("no vault password provided")
			}
			return nil, fmt.Errorf("read vault password: %w", err)
		}
	}
	if len(password) > 0 && password[len(password)-1] == '\r' {
		password = password[:len(password)-1]
	}
	if len(password) == 0 {
		return nil, errors.New("vault password cannot be empty")
	}
	return password, nil
}
