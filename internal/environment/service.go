package environment

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/envman/internal/config"
)

// Create adds an environment directory to the project. Environment directories
// are created with private permissions because their variable files may hold
// credentials.
func Create(paths config.ProjectPaths, name string) error {
	path, err := config.EnvironmentPath(paths, name)
	if err != nil {
		return err
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("environment %q already exists", name)
		}
		return fmt.Errorf("create environment %q: %w", name, err)
	}
	return nil
}

// List returns the project's environment names in sorted order. Sorting here
// makes the service result stable for both CLI output and callers.
func List(paths config.ProjectPaths) ([]string, error) {
	entries, err := os.ReadDir(paths.Environments)
	if err != nil {
		return nil, fmt.Errorf("read project environments: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// Use selects an existing environment and returns the previously active name.
// It verifies the target exists before saving metadata; an empty previous name
// indicates the project had not selected an environment yet.
func Use(paths config.ProjectPaths, metadata config.ProjectMetadata, name string) (string, error) {
	if _, err := config.EnvironmentPath(paths, name); err != nil {
		return "", err
	}
	environments, err := List(paths)
	if err != nil {
		return "", err
	}
	if !contains(environments, name) {
		return "", fmt.Errorf("environment %q does not exist", name)
	}
	previous := metadata.ActiveEnvironment
	metadata.ActiveEnvironment = name
	if err := config.SaveProjectMetadata(paths, metadata); err != nil {
		return "", fmt.Errorf("save active environment: %w", err)
	}
	return previous, nil
}

// Delete removes an environment directory unless it is currently active. The
// active environment is protected because removing it would leave project
// metadata pointing at a directory that no longer exists.
func Delete(paths config.ProjectPaths, metadata config.ProjectMetadata, name string) error {
	path, err := config.EnvironmentPath(paths, name)
	if err != nil {
		return err
	}
	if metadata.ActiveEnvironment == name {
		return fmt.Errorf("cannot delete active environment %q; switch to another environment first", name)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("find environment %q: %w", name, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("environment %q is not a directory", name)
	}
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("delete environment %q: %w", name, err)
	}
	return nil
}

// Rename moves an environment and updates project metadata when it was active.
// Existing destinations are rejected instead of merged or replaced, avoiding
// accidental loss of variables already stored under the new name.
func Rename(paths config.ProjectPaths, metadata config.ProjectMetadata, oldName, newName string) error {
	oldPath, err := config.EnvironmentPath(paths, oldName)
	if err != nil {
		return err
	}
	newPath, err := config.EnvironmentPath(paths, newName)
	if err != nil {
		return err
	}
	if oldName == newName {
		return fmt.Errorf("old and new environment names are the same")
	}
	oldInfo, err := os.Stat(oldPath)
	if err != nil {
		return fmt.Errorf("find environment %q: %w", oldName, err)
	}
	if !oldInfo.IsDir() {
		return fmt.Errorf("environment %q is not a directory", oldName)
	}
	if _, err := os.Lstat(newPath); err == nil {
		return fmt.Errorf("environment %q already exists", newName)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect environment path %q: %w", newName, err)
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("rename environment %q to %q: %w", oldName, newName, err)
	}
	if metadata.ActiveEnvironment == oldName {
		metadata.ActiveEnvironment = newName
		if err := config.SaveProjectMetadata(paths, metadata); err != nil {
			// Keep the directory and metadata in sync if saving the active name fails.
			if rollbackErr := os.Rename(newPath, oldPath); rollbackErr != nil {
				return errors.Join(fmt.Errorf("save renamed active environment: %w", err), fmt.Errorf("restore environment directory: %w", rollbackErr))
			}
			return fmt.Errorf("save renamed active environment: %w", err)
		}
	}
	return nil
}

// VariableFile resolves the variables file for an existing environment
// directory. It does not create a missing variables.json; reads treat a
// missing base file as empty, and mutations append an encrypted journal beside it.
func VariableFile(paths config.ProjectPaths, environmentName string) (string, error) {
	dir, err := config.EnvironmentPath(paths, environmentName)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("find environment %q: %w", environmentName, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("environment %q is not a directory", environmentName)
	}
	return filepath.Join(dir, "variables.json"), nil
}

// Current returns the active environment or an actionable error when none is
// selected. Trimming is used only to detect an unset/blank selection; the
// stored value itself is returned unchanged.
func Current(metadata config.ProjectMetadata) (string, error) {
	if strings.TrimSpace(metadata.ActiveEnvironment) == "" {
		return "", errors.New("no active environment; create and use an environment first")
	}
	return metadata.ActiveEnvironment, nil
}

func contains(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}
