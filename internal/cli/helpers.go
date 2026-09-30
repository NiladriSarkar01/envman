package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// confirmationFlag parses the optional --yes switch used to skip a prompt.
func confirmationFlag(args []string) (bool, error) {
	// Destructive commands are non-interactive only when --yes is explicit.
	switch len(args) {
	case 0:
		return false, nil
	case 1:
		if args[0] == "--yes" {
			return true, nil
		}
	}
	return false, errors.New("usage: envman project remove [--yes]")
}

// confirm writes a prompt and accepts only "y" or "yes" as affirmative input.
// Any other answer, including empty input, is a refusal.
func confirm(ctx Context, prompt string) (bool, error) {
	if _, err := io.WriteString(ctx.Out, paint(ctx.Out, colorYellow, prompt)); err != nil {
		return false, fmt.Errorf("write confirmation prompt: %w", err)
	}
	reader := bufio.NewReader(ctx.In)
	answer, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("read confirmation: %w", err)
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	// EOF without an affirmative answer is deliberately treated as a refusal,
	// which makes piped or closed input safe for destructive operations.
	return answer == "y" || answer == "yes", nil
}

// requireNoArgs gives no-argument commands a consistent usage error.
func requireNoArgs(args []string, command string) error {
	if len(args) != 0 {
		return fmt.Errorf("%s does not accept arguments", command)
	}
	return nil
}

// samePath compares normalized absolute paths using the host filesystem's
// casing rules; unresolved paths are treated as non-matches.
func samePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	if os.PathSeparator == '\\' {
		// Windows path identity is case-insensitive after cleaning and
		// absolute-path resolution.
		return strings.EqualFold(filepath.Clean(leftAbs), filepath.Clean(rightAbs))
	}
	return filepath.Clean(leftAbs) == filepath.Clean(rightAbs)
}
