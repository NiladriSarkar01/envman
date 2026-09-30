package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/envman/internal/config"
	"github.com/envman/internal/environment"
	"github.com/envman/internal/process"
	"github.com/envman/internal/store"
)

// runWithEnvironment loads either the selected or explicitly named environment
// and starts the requested child program with its variables.
func runWithEnvironment(ctx Context) error {
	separator := -1
	for i, arg := range ctx.Args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator < 0 || separator == len(ctx.Args)-1 {
		return errors.New("usage: envman run [--env <name>] -- <command> [arguments...]")
	}
	environmentName := ""
	flags := ctx.Args[:separator]
	if len(flags) != 0 {
		if len(flags) != 2 || flags[0] != "--env" {
			return errors.New("usage: envman run [--env <name>] -- <command> [arguments...]")
		}
		environmentName = flags[1]
	}
	vault, _, err := unlockVault(ctx)
	if err != nil {
		return err
	}
	defer vault.clear()
	paths, metadata, err := config.LoadCurrentProject()
	if err != nil {
		return err
	}
	if environmentName == "" {
		environmentName, err = environment.Current(metadata)
		if err != nil {
			return err
		}
	}
	variablesPath, err := environment.VariableFile(paths, environmentName)
	if err != nil {
		return err
	}
	variables, err := store.Load(variablesPath, vault.symmetricKey, vault.privateKey)
	if err != nil {
		return err
	}
	return process.Run(ctx.Args[separator+1], ctx.Args[separator+2:], variables, ctx.In, ctx.Out, ctx.Err)
}

// importEnvironment parses a .env file and appends encrypted values without
// loading or decrypting the current environment.
func importEnvironment(ctx Context) error {
	path, environmentName, _, err := parseFileEnvironmentArgs(ctx.Args, false)
	if err != nil {
		return errors.New("usage: envman import <.env-file> [--env <name>]")
	}
	_, cfg, err := loadVaultConfig()
	if err != nil {
		return err
	}
	variablesPath, err := resolveVariablesFile(environmentName)
	if err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open .env file %q: %w", path, err)
	}
	imported, parseErr := store.ParseDotEnv(file)
	closeErr := file.Close()
	if parseErr != nil {
		return parseErr
	}
	if closeErr != nil {
		return fmt.Errorf("close .env file %q: %w", path, closeErr)
	}
	if err := store.AppendValues(variablesPath, imported, cfg.Vault.PublicKeyFile); err != nil {
		return err
	}
	_, err = fmt.Fprintf(ctx.Out, "%s from %s.\n", success(ctx.Out, fmt.Sprintf("Imported %d variables", len(imported))), paint(ctx.Out, colorCyan, path))
	return err
}

// exportEnvironment writes either a key-only example or all values to a new
// .env file. Only the full-value form unlocks the vault.
func exportEnvironment(ctx Context) error {
	path, environmentName, example, err := parseFileEnvironmentArgs(ctx.Args, true)
	if err != nil {
		return errors.New("usage: envman export [--example] <.env-file> [--env <name>]")
	}
	variablesPath, err := resolveVariablesFile(environmentName)
	if err != nil {
		return err
	}
	variables := make(map[string]string)
	if example {
		names, err := store.Keys(variablesPath)
		if err != nil {
			return err
		}
		for _, name := range names {
			variables[name] = ""
		}
	} else {
		vault, _, err := unlockVault(ctx)
		if err != nil {
			return err
		}
		defer vault.clear()
		variables, err = store.Load(variablesPath, vault.symmetricKey, vault.privateKey)
		if err != nil {
			return err
		}
	}
	if err := store.WriteDotEnv(path, variables); err != nil {
		return err
	}
	mode := "variables"
	if example {
		mode = "variable names"
	}
	_, err = fmt.Fprintf(ctx.Out, "%s to %s.\n", success(ctx.Out, fmt.Sprintf("Exported %d %s", len(variables), mode)), paint(ctx.Out, colorCyan, path))
	return err
}

func resolveVariablesFile(environmentName string) (string, error) {
	paths, metadata, err := config.LoadCurrentProject()
	if err != nil {
		return "", err
	}
	if environmentName == "" {
		environmentName, err = environment.Current(metadata)
		if err != nil {
			return "", err
		}
	}
	return environment.VariableFile(paths, environmentName)
}

func parseFileEnvironmentArgs(args []string, allowExample bool) (string, string, bool, error) {
	var path, environmentName string
	example := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--env":
			if i+1 >= len(args) || environmentName != "" {
				return "", "", false, errors.New("invalid file/environment arguments")
			}
			i++
			environmentName = args[i]
		case "--example":
			if !allowExample || example {
				return "", "", false, errors.New("invalid file/environment arguments")
			}
			example = true
		default:
			if strings.HasPrefix(args[i], "-") || path != "" {
				return "", "", false, errors.New("invalid file/environment arguments")
			}
			path = args[i]
		}
	}
	if path == "" {
		return "", "", false, errors.New("file path is required")
	}
	return path, environmentName, example, nil
}
