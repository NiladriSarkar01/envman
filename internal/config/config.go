package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	// StorageDirectoryEnv overrides the default EnvMan storage directory.
	StorageDirectoryEnv = "ENVMAN_HOME"
	// MarkerName identifies a directory registered as an EnvMan project.
	MarkerName = ".envman"
)

// Config stores settings shared by EnvMan projects.
type Config struct {
	// CurrentEnvironment is retained for compatibility with earlier config files.
	CurrentEnvironment string `json:"current_environment"`
	// Vault contains global encryption and value-display preferences.
	Vault VaultSettings `json:"vault"`
}

// VaultSettings controls the encrypted store and how likely credentials are displayed.
type VaultSettings struct {
	// KeyFile points to the password-wrapped vault key; key material is never stored here.
	KeyFile string `json:"key_file"`
	// PublicKeyFile is used to encrypt new values without unlocking the vault.
	PublicKeyFile string `json:"public_key_file"`
	// PrivateKeyFile stores the private key encrypted by the vault key.
	PrivateKeyFile string `json:"private_key_file"`
	// RevealSecretsByDefault disables name-based masking when true.
	RevealSecretsByDefault bool `json:"reveal_secrets_by_default"`
}

// ProjectMarker links a project directory to its persistent storage ID.
type ProjectMarker struct {
	// ID is also the directory name used for this project's shared storage.
	ID string `json:"id"`
}

// ProjectMetadata describes a registered project and its active environment.
type ProjectMetadata struct {
	// ID binds this metadata to the marker in the source project directory.
	ID string `json:"id"`
	// Name is the directory basename captured when the project is initialized.
	Name string `json:"name"`
	// Path records the source project location, not the EnvMan storage location.
	Path string `json:"path"`
	// ActiveEnvironment is empty until the user selects an environment.
	ActiveEnvironment string `json:"active_environment"`
}

// Paths contains locations in shared EnvMan storage.
type Paths struct {
	// Root is the storage directory selected by ENVMAN_HOME or the default.
	Root string
	// Config contains storage-wide settings.
	Config string
	// Environments contains one subdirectory for each registered project.
	Environments string
}

// ProjectPaths contains locations associated with one registered project.
type ProjectPaths struct {
	// Root is the project's source directory.
	Root string
	// Marker is the project-local file that identifies its registration.
	Marker string
	// ID is the stable identifier shared by the marker and metadata.
	ID string
	// Name is the source directory's basename.
	Name string
	// Storage contains this project's metadata and environment data.
	Storage string
	// Metadata is the JSON file holding project details and active selection.
	Metadata string
	// Environments contains one directory per environment name.
	Environments string
}

// StorageRoot returns the configured storage directory or the platform default.
// An ENVMAN_HOME override is normalized to an absolute path so that later
// commands resolve the same storage location regardless of their working
// directory. On systems without an override, storage lives under the user's
// configuration directory.
func StorageRoot() (string, error) {
	if root := os.Getenv(StorageDirectoryEnv); root != "" {
		return filepath.Abs(root)
	}
	if runtime.GOOS == "windows" {
		return filepath.Abs("D:/work_space/projects/personal/envman/.env")
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("determine user configuration directory: %w", err)
	}
	if configDir == "" {
		return "", errors.New("determine user configuration directory: empty path")
	}
	return filepath.Join(configDir, "envman"), nil
}

// InitializeStorage creates shared storage directories and their config file
// if needed. Existing regular config files are left untouched so repeated
// setup calls do not erase settings.
func InitializeStorage() (Paths, error) {
	paths, err := StoragePaths()
	if err != nil {
		return Paths{}, err
	}
	return initializeAt(paths.Root)
}

// StoragePaths resolves the paths under the configured storage root without
// creating directories or files. Callers can use it for read-only setup checks.
func StoragePaths() (Paths, error) {
	root, err := StorageRoot()
	if err != nil {
		return Paths{}, fmt.Errorf("determine EnvMan storage directory: %w", err)
	}
	return pathsAt(root), nil
}

func pathsAt(root string) Paths {
	return Paths{
		Root:         root,
		Config:       filepath.Join(root, "config.json"),
		Environments: filepath.Join(root, "environments"),
	}
}

func initializeAt(root string) (Paths, error) {
	paths := pathsAt(root)
	// Restrictive directory permissions help keep locally stored environment
	// values private to the current user where the platform supports them.
	if err := os.MkdirAll(paths.Environments, 0o700); err != nil {
		return Paths{}, fmt.Errorf("create environments directory %q: %w", paths.Environments, err)
	}
	if err := createJSONFileIfMissing(paths.Config, defaultConfig(paths), "storage config"); err != nil {
		return Paths{}, err
	}
	return paths, nil
}

// LoadConfig reads global settings and fills defaults for keys absent from
// configurations created by older versions.
func LoadConfig(paths Paths) (Config, error) {
	data, err := os.ReadFile(paths.Config)
	if err != nil {
		return Config{}, fmt.Errorf("read EnvMan config %q: %w", paths.Config, err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("decode EnvMan config %q: %w", paths.Config, err)
	}
	defaults := defaultConfig(paths)
	if cfg.Vault.KeyFile == "" {
		cfg.Vault.KeyFile = defaults.Vault.KeyFile
	} else if !filepath.IsAbs(cfg.Vault.KeyFile) {
		cfg.Vault.KeyFile = filepath.Join(paths.Root, cfg.Vault.KeyFile)
	}
	if cfg.Vault.PublicKeyFile == "" {
		cfg.Vault.PublicKeyFile = defaults.Vault.PublicKeyFile
	} else if !filepath.IsAbs(cfg.Vault.PublicKeyFile) {
		cfg.Vault.PublicKeyFile = filepath.Join(paths.Root, cfg.Vault.PublicKeyFile)
	}
	if cfg.Vault.PrivateKeyFile == "" {
		cfg.Vault.PrivateKeyFile = defaults.Vault.PrivateKeyFile
	} else if !filepath.IsAbs(cfg.Vault.PrivateKeyFile) {
		cfg.Vault.PrivateKeyFile = filepath.Join(paths.Root, cfg.Vault.PrivateKeyFile)
	}
	return cfg, nil
}

// SaveConfig persists global EnvMan settings without placing password or key
// material in the config file.
func SaveConfig(paths Paths, cfg Config) error {
	if cfg.Vault.KeyFile == "" {
		return fmt.Errorf("vault key-file path cannot be empty")
	}
	return writeJSON(paths.Config, cfg)
}

func defaultConfig(paths Paths) Config {
	return Config{
		Vault: VaultSettings{
			KeyFile:        filepath.Join(paths.Root, "vault.key"),
			PublicKeyFile:  filepath.Join(paths.Root, "vault.pub"),
			PrivateKeyFile: filepath.Join(paths.Root, "vault.private"),
		},
	}
}

// InitializeProject registers the current working directory with EnvMan. The
// project's marker stays beside its source files, while metadata and secrets
// are stored in the shared EnvMan directory.
func InitializeProject() (ProjectPaths, error) {
	root, err := os.Getwd()
	if err != nil {
		return ProjectPaths{}, fmt.Errorf("determine current project directory: %w", err)
	}
	storage, err := InitializeStorage()
	if err != nil {
		return ProjectPaths{}, fmt.Errorf("initialize EnvMan storage: %w", err)
	}
	return initializeProjectAt(root, storage.Environments)
}

func initializeProjectAt(root, projectsDir string) (ProjectPaths, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return ProjectPaths{}, fmt.Errorf("resolve project directory: %w", err)
	}
	paths := ProjectPaths{Root: root, Marker: filepath.Join(root, MarkerName), Name: filepath.Base(root)}
	if data, err := os.ReadFile(paths.Marker); err == nil {
		// An existing marker is authoritative; malformed or invalid IDs should
		// fail rather than silently registering the directory as a new project.
		var marker ProjectMarker
		if err := json.Unmarshal(data, &marker); err != nil {
			return ProjectPaths{}, fmt.Errorf("decode project marker %q: %w", paths.Marker, err)
		}
		if !validID(marker.ID) {
			return ProjectPaths{}, fmt.Errorf("project marker %q contains an invalid project ID", paths.Marker)
		}
		paths.ID = marker.ID
	} else if errors.Is(err, os.ErrNotExist) {
		// Preserve an ID from the legacy project config before assigning a new one.
		paths.ID, err = legacyProjectID(root)
		if err != nil {
			return ProjectPaths{}, err
		}
		if paths.ID == "" {
			paths.ID, err = newID()
			if err != nil {
				return ProjectPaths{}, err
			}
		}
		markerData, marshalErr := json.MarshalIndent(ProjectMarker{ID: paths.ID}, "", "  ")
		if marshalErr != nil {
			return ProjectPaths{}, fmt.Errorf("encode project marker: %w", marshalErr)
		}
		markerData = append(markerData, '\n')
		// O_EXCL in writeNewFile avoids replacing a marker created concurrently.
		if err := writeNewFile(paths.Marker, markerData); err != nil {
			return ProjectPaths{}, fmt.Errorf("create project marker %q: %w", paths.Marker, err)
		}
	} else {
		return ProjectPaths{}, fmt.Errorf("read project marker %q: %w", paths.Marker, err)
	}

	paths.Storage = filepath.Join(projectsDir, paths.ID)
	paths.Metadata = filepath.Join(paths.Storage, "project.json")
	paths.Environments = filepath.Join(paths.Storage, "environments")
	if err := os.MkdirAll(paths.Environments, 0o700); err != nil {
		return ProjectPaths{}, fmt.Errorf("create project environments directory %q: %w", paths.Environments, err)
	}

	metadata := ProjectMetadata{ID: paths.ID, Name: paths.Name, Path: paths.Root}
	// Reinitializing a project must not discard its currently selected environment.
	if existing, err := readProjectMetadata(paths.Metadata); err == nil {
		metadata.ActiveEnvironment = existing.ActiveEnvironment
	} else if !errors.Is(err, os.ErrNotExist) {
		return ProjectPaths{}, err
	}
	if err := writeJSON(paths.Metadata, metadata); err != nil {
		return ProjectPaths{}, fmt.Errorf("save project metadata %q: %w", paths.Metadata, err)
	}
	return paths, nil
}

func legacyProjectID(root string) (string, error) {
	// Older versions kept the ID in the project directory. Reuse it where
	// possible so upgrading does not strand the project's existing data.
	path := filepath.Join(root, "envman.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read legacy project config %q: %w", path, err)
	}
	var legacy struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		return "", fmt.Errorf("decode legacy project config %q: %w", path, err)
	}
	if legacy.ID == "" {
		return "", nil
	}
	if !validID(legacy.ID) {
		return "", fmt.Errorf("legacy project config %q contains an invalid project ID", path)
	}
	return legacy.ID, nil
}

// ProjectPathsByID loads a registered project's paths and metadata by ID.
// The ID is validated before it is joined to the storage root, preventing
// arbitrary path fragments from being interpreted as a project directory.
func ProjectPathsByID(id string) (ProjectPaths, ProjectMetadata, error) {
	if !validID(id) {
		return ProjectPaths{}, ProjectMetadata{}, fmt.Errorf("invalid project ID %q", id)
	}
	storage, err := InitializeStorage()
	if err != nil {
		return ProjectPaths{}, ProjectMetadata{}, err
	}
	root := filepath.Join(storage.Environments, id)
	metadataPath := filepath.Join(root, "project.json")
	metadata, err := readProjectMetadata(metadataPath)
	if err != nil {
		return ProjectPaths{}, ProjectMetadata{}, err
	}
	return ProjectPaths{
		ID: id, Name: metadata.Name, Root: metadata.Path, Marker: filepath.Join(metadata.Path, MarkerName), Storage: root, Metadata: metadataPath,
		Environments: filepath.Join(root, "environments"),
	}, metadata, nil
}

func readProjectMetadata(path string) (ProjectMetadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ProjectMetadata{}, fmt.Errorf("read project metadata %q: %w", path, err)
	}
	var metadata ProjectMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return ProjectMetadata{}, fmt.Errorf("decode project metadata %q: %w", path, err)
	}
	if !validID(metadata.ID) {
		return ProjectMetadata{}, fmt.Errorf("project metadata %q contains an invalid project ID", path)
	}
	return metadata, nil
}

// SaveProjectMetadata persists metadata only when it belongs to paths' project.
// This ID check prevents callers from accidentally writing one project's
// metadata into another project's storage path.
func SaveProjectMetadata(paths ProjectPaths, metadata ProjectMetadata) error {
	if !validID(metadata.ID) || metadata.ID != paths.ID {
		return fmt.Errorf("invalid project ID in metadata")
	}
	return writeJSON(paths.Metadata, metadata)
}

// LoadCurrentProject resolves and validates the project registered in the
// working directory. In addition to loading the marker's ID, it checks that
// the stored project path matches the current directory, so copied or stale
// marker files cannot silently select a different registration.
func LoadCurrentProject() (ProjectPaths, ProjectMetadata, error) {
	root, err := os.Getwd()
	if err != nil {
		return ProjectPaths{}, ProjectMetadata{}, fmt.Errorf("determine current project directory: %w", err)
	}
	markerPath := filepath.Join(root, MarkerName)
	data, err := os.ReadFile(markerPath)
	if err != nil {
		return ProjectPaths{}, ProjectMetadata{}, fmt.Errorf("read project marker %q (run envman init first): %w", markerPath, err)
	}
	var marker ProjectMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		return ProjectPaths{}, ProjectMetadata{}, fmt.Errorf("decode project marker %q: %w", markerPath, err)
	}
	paths, metadata, err := ProjectPathsByID(marker.ID)
	if err != nil {
		return ProjectPaths{}, ProjectMetadata{}, err
	}
	if filepath.Clean(paths.Root) != filepath.Clean(root) {
		return ProjectPaths{}, ProjectMetadata{}, fmt.Errorf("project marker ID %q is registered to %q, not %q", marker.ID, paths.Root, root)
	}
	return paths, metadata, nil
}

// ListProjects returns metadata for all registered projects in shared storage.
// Non-directory entries and directories without valid ID-shaped names are
// ignored, while malformed metadata for a recognized project is reported.
func ListProjects() ([]ProjectMetadata, error) {
	storage, err := InitializeStorage()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(storage.Environments)
	if err != nil {
		return nil, fmt.Errorf("list registered projects: %w", err)
	}
	projects := make([]ProjectMetadata, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || !validID(entry.Name()) {
			continue
		}
		metadata, err := readProjectMetadata(filepath.Join(storage.Environments, entry.Name(), "project.json"))
		if err != nil {
			return nil, err
		}
		if metadata.Name == "" {
			metadata.Name = metadata.ID
		}
		projects = append(projects, metadata)
	}
	return projects, nil
}

// RemoveCurrentProject unregisters the current project and optionally deletes
// its stored data. With removeData false, only the registration metadata and
// local marker are removed; environment data remains in shared storage.
func RemoveCurrentProject(removeData bool) error {
	paths, _, err := LoadCurrentProject()
	if err != nil {
		return err
	}
	if removeData {
		if err := os.RemoveAll(paths.Storage); err != nil {
			return fmt.Errorf("remove project storage %q: %w", paths.Storage, err)
		}
	} else {
		if err := os.Remove(paths.Metadata); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove project registration %q: %w", paths.Metadata, err)
		}
	}
	if err := os.Remove(paths.Marker); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove project marker %q: %w", paths.Marker, err)
	}
	return nil
}

// EnvironmentPath validates a name before resolving its directory in the
// project. Keeping name validation here ensures every caller shares the same
// path-traversal and naming rules.
func EnvironmentPath(paths ProjectPaths, name string) (string, error) {
	if err := ValidateEnvironmentName(name); err != nil {
		return "", err
	}
	return filepath.Join(paths.Environments, name), nil
}

// ValidateEnvironmentName rejects empty names, path components, whitespace,
// and separators. Environment names are single directory names rather than
// arbitrary paths, so callers cannot escape the project's environment root.
func ValidateEnvironmentName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\ \t\r\n") || strings.TrimSpace(name) != name {
		return fmt.Errorf("invalid environment name %q", name)
	}
	return nil
}

func validID(id string) bool {
	// Check the canonical UUID layout before decoding its hexadecimal payload.
	if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		return false
	}
	decoded, err := hex.DecodeString(strings.ReplaceAll(id, "-", ""))
	return err == nil && len(decoded) == 16
}

func newID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", fmt.Errorf("generate project ID: %w", err)
	}
	// Set the RFC 4122 version-4 and variant bits on the random identifier.
	id[6] = id[6]&0x0f | 0x40
	id[8] = id[8]&0x3f | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", id[0:4], id[4:6], id[6:8], id[8:10], id[10:16]), nil
}

func createJSONFileIfMissing(path string, value any, description string) error {
	if err := writeJSONNew(path, value); err != nil {
		if errors.Is(err, os.ErrExist) {
			// An existing path is acceptable only if it is a regular file; do
			// not mistake a directory or special file for initialized config.
			info, statErr := os.Stat(path)
			if statErr != nil {
				return fmt.Errorf("inspect existing %s file %q: %w", description, path, statErr)
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("existing %s path %q is not a regular file", description, path)
			}
			return nil
		}
		return fmt.Errorf("create %s file %q: %w", description, path, err)
	}
	return nil
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode JSON for %q: %w", path, err)
	}
	data = append(data, '\n')
	temp, err := os.CreateTemp(filepath.Dir(path), ".envman-*")
	if err != nil {
		return fmt.Errorf("create temporary file for %q: %w", path, err)
	}
	tempName := temp.Name()
	// Complete and close the replacement before touching the current file.
	// That way an encoding or write failure leaves the last good JSON intact.
	n, writeErr := temp.Write(data)
	closeErr := temp.Close()
	if writeErr != nil {
		_ = os.Remove(tempName)
		return fmt.Errorf("write temporary file for %q: %w", path, writeErr)
	}
	if n != len(data) {
		_ = os.Remove(tempName)
		return fmt.Errorf("write temporary file for %q: %w", path, io.ErrShortWrite)
	}
	if closeErr != nil {
		_ = os.Remove(tempName)
		return fmt.Errorf("close temporary file for %q: %w", path, closeErr)
	}
	// Keep the temporary file beside the destination to avoid a cross-filesystem rename.
	if err := os.Rename(tempName, path); err != nil {
		_ = os.Remove(tempName)
		return fmt.Errorf("replace %q: %w", path, err)
	}
	return nil
}

func writeJSONNew(path string, value any) error {
	// Unlike writeJSON, this path is intended for first-time initialization and
	// must not replace a file that already exists.
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode JSON: %w", err)
	}
	data = append(data, '\n')
	return writeNewFile(path, data)
}

func writeNewFile(path string, data []byte) error {
	// O_EXCL makes creation fail rather than overwrite a file that appeared
	// after the caller checked for its absence.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	n, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil {
		return errors.Join(writeErr, removeIncompleteFile(path))
	}
	if n != len(data) {
		return errors.Join(io.ErrShortWrite, removeIncompleteFile(path))
	}
	if closeErr != nil {
		return errors.Join(closeErr, removeIncompleteFile(path))
	}
	return nil
}

func removeIncompleteFile(path string) error {
	// Cleanup errors are returned alongside the original write failure so
	// callers can distinguish an incomplete artifact that could not be removed.
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove incomplete file %q: %w", path, err)
	}
	return nil
}
