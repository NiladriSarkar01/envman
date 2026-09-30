package process

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
)

type environmentEntry struct {
	// Preserve the spelling used in the resulting KEY=value entry.
	name  string
	value string
}

// Run executes a program with the current process environment overridden by variables.
// Standard streams are passed through so interactive child programs behave as
// expected and their output remains visible to the caller.
func Run(name string, args []string, variables map[string]string, stdin io.Reader, stdout, stderr io.Writer) error {
	command := exec.Command(name, args...)
	command.Stdin = stdin
	command.Stdout = stdout
	command.Stderr = stderr
	command.Env = mergedEnvironment(os.Environ(), variables)
	if err := command.Run(); err != nil {
		return fmt.Errorf("run %q: %w", name, err)
	}
	return nil
}

func mergedEnvironment(current []string, overrides map[string]string) []string {
	// Use a map to coalesce duplicate keys while retaining the final spelling
	// and value selected for each platform-specific environment key.
	merged := make(map[string]environmentEntry, len(current)+len(overrides))
	for _, item := range current {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			merged[environmentKey(key)] = environmentEntry{name: key, value: value}
		}
	}
	for key, value := range overrides {
		// Overrides replace inherited values, including case variants on Windows.
		merged[environmentKey(key)] = environmentEntry{name: key, value: value}
	}
	entries := make([]environmentEntry, 0, len(merged))
	for _, entry := range merged {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		return strings.ToUpper(entries[i].name) < strings.ToUpper(entries[j].name)
	})
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry.name+"="+entry.value)
	}
	return result
}

func environmentKey(key string) string {
	// Windows environment variable names are case-insensitive; other supported
	// platforms distinguish casing and must retain the original key.
	if runtime.GOOS == "windows" {
		return strings.ToUpper(key)
	}
	return key
}
