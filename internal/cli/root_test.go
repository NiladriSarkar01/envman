package cli

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestCLIRegistersAndExecutesAdditionalSubcommands(t *testing.T) {
	app, err := New(Command{
		Path:        []string{"status", "short"},
		Description: "Show short status",
		Handler: func(ctx Context) error {
			if len(ctx.Args) != 1 || ctx.Args[0] != "--json" {
				t.Errorf("handler args = %v, want [--json]", ctx.Args)
			}
			_, err := io.WriteString(ctx.Out, "ready\n")
			return err
		},
	})
	if err != nil {
		t.Fatalf("create CLI: %v", err)
	}

	var output bytes.Buffer
	if err := app.ExecuteWithIO([]string{"status", "short", "--json"}, strings.NewReader(""), &output, &output); err != nil {
		t.Fatalf("execute registered command: %v", err)
	}
	if output.String() != "ready\n" {
		t.Errorf("output = %q, want %q", output.String(), "ready\n")
	}
}

func TestCLIHelpListsBuiltInCommandTree(t *testing.T) {
	app, err := New()
	if err != nil {
		t.Fatalf("create CLI: %v", err)
	}
	var output bytes.Buffer
	if err := app.ExecuteWithIO([]string{"project"}, strings.NewReader(""), &output, &output); err != nil {
		t.Fatalf("execute project help: %v", err)
	}
	for _, command := range []string{"info", "list", "remove"} {
		if !strings.Contains(output.String(), command) {
			t.Errorf("help output %q does not contain command %q", output.String(), command)
		}
	}
	output.Reset()
	if err := app.ExecuteWithIO([]string{"env", "help"}, strings.NewReader(""), &output, &output); err != nil {
		t.Fatalf("execute environment help: %v", err)
	}
	if !strings.Contains(output.String(), "rename") {
		t.Errorf("environment help output %q does not contain rename", output.String())
	}
	output.Reset()
	if err := app.ExecuteWithIO([]string{"help", "init"}, strings.NewReader(""), &output, &output); err != nil {
		t.Fatalf("execute help init: %v", err)
	}
	if !strings.Contains(output.String(), "Register the current project") {
		t.Errorf("init help output %q does not describe init", output.String())
	}
	output.Reset()
	if err := app.ExecuteWithIO([]string{"init", "--help"}, strings.NewReader(""), &output, &output); err != nil {
		t.Fatalf("execute init --help: %v", err)
	}
	if !strings.Contains(output.String(), "Register the current project") {
		t.Errorf("init --help output %q does not describe init", output.String())
	}
	output.Reset()
	if err := app.ExecuteWithIO([]string{"help"}, strings.NewReader(""), &output, &output); err != nil {
		t.Fatalf("execute help: %v", err)
	}
	if !strings.Contains(output.String(), "help") || !strings.Contains(output.String(), "project info") {
		t.Errorf("root help output %q is missing commands", output.String())
	}
	if !strings.Contains(output.String(), "Quick start") ||
		!strings.Contains(output.String(), "list --show") ||
		!strings.Contains(output.String(), "export --example") {
		t.Errorf("root help output %q is missing its usage guide", output.String())
	}
	output.Reset()
	if err := app.ExecuteWithIO([]string{"help", "run"}, strings.NewReader(""), &output, &output); err != nil {
		t.Fatalf("execute detailed run help: %v", err)
	}
	if !strings.Contains(output.String(), "Usage") ||
		!strings.Contains(output.String(), "vault password") ||
		!strings.Contains(output.String(), "envman run -- go test ./...") {
		t.Errorf("run help output %q is missing command details", output.String())
	}
}

func TestCLIRejectsDuplicateAndInvalidCommands(t *testing.T) {
	handler := func(Context) error { return nil }
	if _, err := New(Command{Path: []string{"init"}, Description: "duplicate", Handler: handler}); err == nil {
		t.Fatal("creating CLI with duplicate built-in command succeeded")
	}
	if _, err := New(Command{Path: []string{"bad command"}, Description: "invalid", Handler: handler}); err == nil {
		t.Fatal("creating CLI with invalid command name succeeded")
	}
	if _, err := New(Command{Path: []string{"missing"}, Description: "missing handler"}); err == nil {
		t.Fatal("creating CLI with missing handler succeeded")
	}
}

func TestCLIPropagatesHandlerError(t *testing.T) {
	wantErr := errors.New("command failed")
	app, err := New(Command{
		Path:        []string{"fail"},
		Description: "Fail",
		Handler:     func(Context) error { return wantErr },
	})
	if err != nil {
		t.Fatalf("create CLI: %v", err)
	}
	if err := app.ExecuteWithIO([]string{"fail"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}); !errors.Is(err, wantErr) {
		t.Errorf("execute error = %v, want %v", err, wantErr)
	}
}
