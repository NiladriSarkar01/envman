package store

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/envman/internal/secrets"
)

const encryptedFileHeader = "ENVMAN-ENCRYPTED-V1\n"
const journalHeader = "ENVMAN-JOURNAL-V1\n"

var variableNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type encryptedVariables struct {
	Version    int    `json:"version"`
	Ciphertext string `json:"ciphertext"`
}

type journalEntry struct {
	Operation string          `json:"op"`
	Name      string          `json:"name"`
	Value     json.RawMessage `json:"value,omitempty"`
}

// Load reads variables from path; a missing file represents an empty
// environment. Legacy whole-file ciphertext uses vaultKey, while journal
// values use privateKey. Other read and decoding errors are surfaced rather
// than treated as an empty environment.
func Load(path string, vaultKey, privateKey []byte) (map[string]string, error) {
	base, journal, err := readEnvironmentFiles(path)
	if err != nil {
		return nil, err
	}
	defer clear(base)
	defer clear(journal)
	variables, err := decodeBaseVariables(path, base, vaultKey)
	if err != nil {
		return nil, err
	}
	if len(journal) > 0 {
		if err := applyJournal(path, variables, journal, privateKey); err != nil {
			return nil, err
		}
	}
	return variables, nil
}

func decodeBaseVariables(path string, data, key []byte) (map[string]string, error) {
	if len(data) == 0 {
		return map[string]string{}, nil
	}
	if bytes.HasPrefix(data, []byte(encryptedFileHeader)) {
		var document encryptedVariables
		if err := json.Unmarshal(data[len(encryptedFileHeader):], &document); err != nil {
			return nil, fmt.Errorf("decode encrypted variables file %q: %w", path, err)
		}
		if document.Version != 1 {
			return nil, fmt.Errorf("encrypted variables file %q has unsupported version %d", path, document.Version)
		}
		ciphertext, err := base64.StdEncoding.DecodeString(document.Ciphertext)
		if err != nil {
			return nil, fmt.Errorf("decode encrypted variables in %q: %w", path, err)
		}
		data, err = secrets.Decrypt(key, ciphertext)
		if err != nil {
			return nil, fmt.Errorf("decrypt variables file %q: %w", path, err)
		}
	}
	var variables map[string]string
	if err := json.Unmarshal(data, &variables); err != nil {
		return nil, fmt.Errorf("decode variables file %q: %w", path, err)
	}
	if variables == nil {
		variables = map[string]string{}
	}
	return variables, nil
}

// AppendValue encrypts and appends one value without reading the current
// environment file or requiring its private decryption key.
func AppendValue(path, name, value, publicKeyPath string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	encrypted, err := secrets.EncryptValue(publicKeyPath, name, value)
	if err != nil {
		return err
	}
	return appendJournalEntry(path, []journalEntry{{Operation: "set", Name: name, Value: encrypted}})
}

// AppendValues encrypts every value before appending any journal data, so an
// invalid key or unencodable record cannot leave a partially imported file.
func AppendValues(path string, values map[string]string, publicKeyPath string) error {
	names := make([]string, 0, len(values))
	for name := range values {
		if err := ValidateName(name); err != nil {
			return err
		}
		names = append(names, name)
	}
	sort.Strings(names)
	entries := make([]journalEntry, 0, len(names))
	for _, name := range names {
		encrypted, err := secrets.EncryptValue(publicKeyPath, name, values[name])
		if err != nil {
			return err
		}
		entries = append(entries, journalEntry{Operation: "set", Name: name, Value: encrypted})
	}
	return appendJournalEntry(path, entries)
}

// AppendDelete records a tombstone without reading or decrypting existing values.
func AppendDelete(path, name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	return appendJournalEntry(path, []journalEntry{{Operation: "delete", Name: name}})
}

func marshalJournalEntries(entries []journalEntry) ([]byte, error) {
	var data bytes.Buffer
	for _, entry := range entries {
		encoded, err := json.Marshal(entry)
		if err != nil {
			return nil, fmt.Errorf("encode variable journal entry: %w", err)
		}
		data.Write(encoded)
		data.WriteByte('\n')
	}
	return data.Bytes(), nil
}

// Keys returns the active variable names without decrypting journal values.
func Keys(path string) ([]string, error) {
	base, journal, err := readEnvironmentFiles(path)
	if err != nil {
		return nil, err
	}
	defer clear(base)
	defer clear(journal)
	if bytes.HasPrefix(base, []byte(encryptedFileHeader)) {
		return nil, errors.New("this environment uses legacy whole-file encryption; key-only listing is unavailable until its values are migrated")
	}
	var rawVariables map[string]json.RawMessage
	if len(base) == 0 {
		rawVariables = make(map[string]json.RawMessage)
	} else if err := json.Unmarshal(base, &rawVariables); err != nil {
		return nil, fmt.Errorf("decode variable names in %q: %w", path, err)
	}
	names := make(map[string]struct{}, len(rawVariables))
	for name := range rawVariables {
		if err := ValidateName(name); err != nil {
			return nil, fmt.Errorf("invalid variable name in %q: %w", path, err)
		}
		names[name] = struct{}{}
	}
	if len(journal) > 0 {
		scanner := bufio.NewScanner(bytes.NewReader(journal))
		scanner.Buffer(make([]byte, 4096), 4*1024*1024)
		for scanner.Scan() {
			var entry journalEntry
			if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
				return nil, fmt.Errorf("decode variable journal %q: %w", path, err)
			}
			if err := ValidateName(entry.Name); err != nil {
				return nil, fmt.Errorf("invalid variable name in %q: %w", path, err)
			}
			switch entry.Operation {
			case "set":
				names[entry.Name] = struct{}{}
			case "delete":
				delete(names, entry.Name)
			default:
				return nil, fmt.Errorf("unknown operation %q in variable journal %q", entry.Operation, path)
			}
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("read variable journal %q: %w", path, err)
		}
	}
	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	sort.Strings(result)
	return result, nil
}

func splitJournal(data []byte) ([]byte, []byte, bool) {
	marker := []byte("\n" + journalHeader)
	if bytes.HasPrefix(data, []byte(journalHeader)) {
		return nil, data[len(journalHeader):], true
	}
	index := bytes.Index(data, marker)
	if index < 0 {
		return nil, nil, false
	}
	return data[:index], data[index+len(marker):], true
}

func readEnvironmentFiles(path string) ([]byte, []byte, error) {
	base, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		base = nil
	} else if err != nil {
		return nil, nil, fmt.Errorf("read variables file %q: %w", path, err)
	}
	rawBase := base
	base, inlineJournal, hasInlineJournal := splitJournal(rawBase)
	if !hasInlineJournal {
		base = rawBase
		inlineJournal = nil
	}
	journal, err := os.ReadFile(path + ".journal")
	if errors.Is(err, os.ErrNotExist) {
		journal = nil
	} else if err != nil {
		clear(base)
		clear(inlineJournal)
		return nil, nil, fmt.Errorf("read variables journal %q: %w", path+".journal", err)
	} else {
		if !bytes.HasPrefix(journal, []byte(journalHeader)) {
			clear(base)
			clear(inlineJournal)
			clear(journal)
			return nil, nil, fmt.Errorf("variables journal %q has an unsupported format", path+".journal")
		}
		journal = journal[len(journalHeader):]
	}
	if len(inlineJournal) > 0 {
		journal = append(inlineJournal, journal...)
	} else {
		clear(inlineJournal)
	}
	return base, journal, nil
}

func applyJournal(path string, variables map[string]string, journal, privateKey []byte) error {
	scanner := bufio.NewScanner(bytes.NewReader(journal))
	scanner.Buffer(make([]byte, 4096), 4*1024*1024)
	for scanner.Scan() {
		var entry journalEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			return fmt.Errorf("decode variable journal %q: %w", path, err)
		}
		if err := ValidateName(entry.Name); err != nil {
			return fmt.Errorf("invalid variable name in %q: %w", path, err)
		}
		switch entry.Operation {
		case "set":
			if len(privateKey) == 0 {
				return errors.New("vault private key is required to read environment values")
			}
			value, err := secrets.DecryptValue(privateKey, entry.Name, entry.Value)
			if err != nil {
				return fmt.Errorf("decrypt variable %q: %w", entry.Name, err)
			}
			variables[entry.Name] = string(value)
			clear(value)
		case "delete":
			delete(variables, entry.Name)
		default:
			return fmt.Errorf("unknown operation %q in variable journal %q", entry.Operation, path)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read variable journal %q: %w", path, err)
	}
	return nil
}

func appendJournalEntry(path string, entries []journalEntry) error {
	encodedEntries, err := marshalJournalEntries(entries)
	if err != nil {
		return err
	}
	journalPath := path + ".journal"
	file, err := os.OpenFile(journalPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open variables journal %q: %w", journalPath, err)
	}
	info, err := file.Stat()
	if err != nil {
		return errors.Join(fmt.Errorf("inspect variables journal %q: %w", journalPath, err), file.Close())
	}
	if info.Size() == 0 {
		if _, err := io.WriteString(file, journalHeader); err != nil {
			return errors.Join(fmt.Errorf("initialize variables journal %q: %w", journalPath, err), file.Close())
		}
	}
	n, err := file.Write(encodedEntries)
	if err != nil {
		return errors.Join(fmt.Errorf("append variables journal %q: %w", journalPath, err), file.Close())
	}
	if n != len(encodedEntries) {
		return errors.Join(fmt.Errorf("append variables journal %q: %w", journalPath, io.ErrShortWrite), file.Close())
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close variables journal %q: %w", journalPath, err)
	}
	return nil
}

// ValidateName reports whether name is a portable shell-style environment
// variable. The accepted form starts with a letter or underscore and contains
// only letters, digits, or underscores thereafter.
func ValidateName(name string) error {
	if !variableNamePattern.MatchString(name) {
		return fmt.Errorf("invalid environment variable name %q", name)
	}
	return nil
}

// IsSecret reports whether a variable name matches a common credential naming
// hint. This is a name-based display safeguard, not a guarantee that all
// sensitive values are detected or that the stored values are encrypted.
func IsSecret(name string) bool {
	upper := strings.ToUpper(name)
	for _, hint := range []string{
		"SECRET", "PASSWORD", "PASSWD", "TOKEN", "API_KEY", "PRIVATE_KEY",
		"ACCESS_KEY", "CLIENT_SECRET", "CREDENTIAL", "DATABASE_URL", "DB_URL",
		"REDIS_URL", "MONGO_URL", "CONNECTION_STRING",
	} {
		if strings.Contains(upper, hint) {
			return true
		}
	}
	return false
}

// ParseDotEnv parses KEY=VALUE lines, optional export prefixes, and quoted
// values. Blank lines and whole-line comments are skipped; malformed names,
// missing equals signs, invalid quoted values, and scanner errors are reported
// with context instead of being silently ignored.
func ParseDotEnv(reader io.Reader) (map[string]string, error) {
	values := make(map[string]string)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		name, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("invalid .env line %d: expected KEY=VALUE", lineNumber)
		}
		name = strings.TrimSpace(name)
		if err := ValidateName(name); err != nil {
			return nil, fmt.Errorf("invalid .env line %d: %w", lineNumber, err)
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '\'' && value[len(value)-1] == '\'' || value[0] == '"' && value[len(value)-1] == '"') {
			// Double quotes use Go-compatible unquoting, while single quotes
			// are treated literally to avoid interpreting backslash escapes.
			unquoted, err := strconv.Unquote(value)
			if value[0] == '\'' {
				unquoted = value[1 : len(value)-1]
				err = nil
			}
			if err != nil {
				return nil, fmt.Errorf("invalid quoted value on .env line %d: %w", lineNumber, err)
			}
			value = unquoted
		}
		values[name] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read .env file: %w", err)
	}
	return values, nil
}

// WriteDotEnv writes sorted, quoted variables without overwriting an existing
// file. Exclusive creation protects user data at the destination, and the
// restrictive mode limits access to the exported values.
func WriteDotEnv(path string, values map[string]string) error {
	names := make([]string, 0, len(values))
	for name := range values {
		if err := ValidateName(name); err != nil {
			return err
		}
		names = append(names, name)
	}
	sort.Strings(names)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("refusing to overwrite existing file %q", path)
		}
		return fmt.Errorf("create export file %q: %w", path, err)
	}
	for _, name := range names {
		if _, err := fmt.Fprintf(file, "%s=%s\n", name, strconv.Quote(values[name])); err != nil {
			return fmt.Errorf("write export file %q: %w", path, errors.Join(err, removeIncompleteFile(path, file)))
		}
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close export file %q: %w", path, errors.Join(err, removeExportFile(path)))
	}
	return nil
}

// removeIncompleteFile closes and removes a partially written export.
func removeIncompleteFile(path string, file *os.File) error {
	if err := file.Close(); err != nil {
		return errors.Join(fmt.Errorf("close incomplete export file: %w", err), removeExportFile(path))
	}
	return removeExportFile(path)
}

func removeExportFile(path string) error {
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove incomplete export file %q: %w", path, err)
	}
	return nil
}
