package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestPaintLeavesNonTerminalOutputPlain(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	const message = "Vault is ready."
	if got := paint(&bytes.Buffer{}, colorGreen, message); got != message {
		t.Fatalf("painted non-terminal output = %q, want plain text", got)
	}
	if strings.Contains(paint(&bytes.Buffer{}, colorBold+colorCyan, message), "\x1b[") {
		t.Fatal("non-terminal output contains ANSI escape sequences")
	}
}

func TestColorCanBeDisabled(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if got := paint(&bytes.Buffer{}, colorGreen, "success"); got != "success" {
		t.Fatalf("paint with NO_COLOR = %q, want plain text", got)
	}
}
