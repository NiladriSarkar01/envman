package cli

import (
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/envman/internal/config"
	"github.com/envman/internal/environment"
	"github.com/envman/internal/store"
)

// setVariable encrypts one value with the public key and appends it without
// loading or decrypting the rest of the environment.
func setVariable(ctx Context) error {
	if len(ctx.Args) != 2 {
		return errors.New("usage: envman set <name> <value>")
	}
	if err := store.ValidateName(ctx.Args[0]); err != nil {
		return err
	}
	_, cfg, err := loadVaultConfig()
	if err != nil {
		return err
	}
	path, err := activeVariablesPath()
	if err != nil {
		return err
	}
	if err := store.AppendValue(path, ctx.Args[0], ctx.Args[1], cfg.Vault.PublicKeyFile); err != nil {
		return err
	}
	_, err = fmt.Fprintf(ctx.Out, "%s %s.\n", success(ctx.Out, "Set"), paint(ctx.Out, colorCyan, ctx.Args[0]))
	return err
}

// getVariable prints one value and follows the configured secret-masking
// default; --show explicitly reveals a likely credential for this invocation.
func getVariable(ctx Context) error {
	if len(ctx.Args) < 1 || len(ctx.Args) > 2 {
		return errors.New("usage: envman get <name> [--show]")
	}
	show := len(ctx.Args) == 2 && ctx.Args[1] == "--show"
	if len(ctx.Args) == 2 && !show {
		return errors.New("usage: envman get <name> [--show]")
	}
	if err := store.ValidateName(ctx.Args[0]); err != nil {
		return err
	}
	vault, cfg, err := unlockVault(ctx)
	if err != nil {
		return err
	}
	defer vault.clear()
	variables, _, err := activeVariables(vault)
	if err != nil {
		return err
	}
	value, exists := variables[ctx.Args[0]]
	if !exists {
		return fmt.Errorf("variable %q is not set", ctx.Args[0])
	}
	if store.IsSecret(ctx.Args[0]) && !show && !cfg.Vault.RevealSecretsByDefault {
		value = "********"
	} else if store.IsSecret(ctx.Args[0]) {
		value = paint(ctx.Out, colorYellow, value)
	}
	_, err = fmt.Fprintln(ctx.Out, value)
	return err
}

// unsetVariable appends a tombstone for a variable without decrypting the
// environment file.
func unsetVariable(ctx Context) error {
	if len(ctx.Args) != 1 {
		return errors.New("usage: envman unset <name>")
	}
	if err := store.ValidateName(ctx.Args[0]); err != nil {
		return err
	}
	path, err := activeVariablesPath()
	if err != nil {
		return err
	}
	names, err := store.Keys(path)
	if err != nil {
		return err
	}
	found := false
	for _, name := range names {
		if name == ctx.Args[0] {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("variable %q is not set", ctx.Args[0])
	}
	if err := store.AppendDelete(path, ctx.Args[0]); err != nil {
		return err
	}
	_, err = fmt.Fprintf(ctx.Out, "%s %s.\n", success(ctx.Out, "Unset"), paint(ctx.Out, colorCyan, ctx.Args[0]))
	return err
}

// listVariables lists names without unlocking by default. Values are loaded
// only when --show or the configured reveal-by-default preference requests them.
func listVariables(ctx Context) error {
	show := false
	if len(ctx.Args) == 1 && ctx.Args[0] == "--show" {
		show = true
	} else if len(ctx.Args) != 0 {
		return errors.New("usage: envman list [--show]")
	}
	path, err := activeVariablesPath()
	if err != nil {
		return err
	}
	_, metadata, err := config.LoadCurrentProject()
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(ctx.Out, "%s %s   %s %s\n\n",
		label(ctx.Out, "Project:"), heading(ctx.Out, metadata.Name),
		label(ctx.Out, "Environment:"), heading(ctx.Out, metadata.ActiveEnvironment)); err != nil {
		return err
	}
	_, cfg, err := loadVaultConfig()
	if err != nil {
		return err
	}
	if show || cfg.Vault.RevealSecretsByDefault {
		vault, _, err := unlockVault(ctx)
		if err != nil {
			return err
		}
		defer vault.clear()
		variables, err := store.Load(path, vault.symmetricKey, vault.privateKey)
		if err != nil {
			return err
		}
		names := sortedVariableNames(variables)
		for _, name := range names {
			value := variables[name]
			if store.IsSecret(name) && !show {
				value = "********"
			}
			if _, err := fmt.Fprintf(ctx.Out, "  %-24s %s\n", paint(ctx.Out, colorCyan, name), paintSecretValue(ctx.Out, name, show || cfg.Vault.RevealSecretsByDefault, value)); err != nil {
				return fmt.Errorf("write variable list: %w", err)
			}
		}
		return nil
	}
	names, err := store.Keys(path)
	if err != nil {
		return err
	}
	for _, name := range names {
		if _, err := fmt.Fprintf(ctx.Out, "  %s\n", paint(ctx.Out, colorCyan, name)); err != nil {
			return fmt.Errorf("write variable list: %w", err)
		}
	}
	return nil
}

func paintSecretValue(out io.Writer, name string, reveal bool, value string) string {
	if store.IsSecret(name) && reveal {
		return paint(out, colorYellow, value)
	}
	return value
}

func sortedVariableNames(variables map[string]string) []string {
	names := make([]string, 0, len(variables))
	for name := range variables {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func activeVariablesPath() (string, error) {
	paths, metadata, err := config.LoadCurrentProject()
	if err != nil {
		return "", err
	}
	active, err := environment.Current(metadata)
	if err != nil {
		return "", err
	}
	return environment.VariableFile(paths, active)
}

func activeVariables(vault *unlockedVault) (map[string]string, string, error) {
	path, err := activeVariablesPath()
	if err != nil {
		return nil, "", err
	}
	variables, err := store.Load(path, vault.symmetricKey, vault.privateKey)
	if err != nil {
		return nil, "", err
	}
	return variables, path, nil
}
