package cli

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// Context carries the arguments and streams provided to a command handler.
// Args contains only the arguments after the matched command path; the streams
// make interactive prompts and output independently replaceable in tests.
type Context struct {
	Args []string
	In   io.Reader
	Out  io.Writer
	Err  io.Writer
}

// Handler executes a command using its parsed arguments and I/O streams.
type Handler func(Context) error

// Command describes a command path, its help text, and its handler. A path is
// represented as separate words (for example, []string{"env", "create"}) so
// the dispatcher can match nested commands without reparsing a string.
type Command struct {
	Path        []string
	Description string
	Handler     Handler
}

// CLI stores registered commands and dispatches invocations to their handlers.
type CLI struct {
	commands []Command
}

// New creates a CLI with the built-in commands and any additional commands.
// Registration validates all paths up front, so ambiguities are reported while
// constructing the CLI rather than depending on registration order at runtime.
func New(commands ...Command) (*CLI, error) {
	app := &CLI{}
	for _, command := range append(builtInCommands(), commands...) {
		if err := app.Register(command); err != nil {
			return nil, err
		}
	}
	if err := app.Register(Command{
		Path:        []string{"help"},
		Description: "Show help for EnvMan commands",
		Handler: func(ctx Context) error {
			return app.writeHelp(ctx.Out, ctx.Args)
		},
	}); err != nil {
		return nil, err
	}
	return app, nil
}

// Register adds a command unless its path conflicts with an existing path.
// Prefix conflicts are rejected as well as exact duplicates: a command named
// "env" cannot coexist with a command named "env create", because dispatch
// could not consistently decide whether the first path is complete.
func (app *CLI) Register(command Command) error {
	if len(command.Path) == 0 || command.Handler == nil || strings.TrimSpace(command.Description) == "" {
		return fmt.Errorf("command requires a path, description, and handler")
	}
	path := append([]string(nil), command.Path...)
	for _, part := range path {
		if strings.TrimSpace(part) != part || part == "" || strings.ContainsAny(part, " \t\r\n") {
			return fmt.Errorf("invalid command path %q", strings.Join(path, " "))
		}
	}
	for _, registered := range app.commands {
		common := len(path)
		if len(registered.Path) < common {
			common = len(registered.Path)
		}
		if (common == len(path) || common == len(registered.Path)) && equalPath(path[:common], registered.Path[:common]) {
			return fmt.Errorf("command path %q conflicts with registered command %q", strings.Join(path, " "), strings.Join(registered.Path, " "))
		}
	}
	command.Path = path
	app.commands = append(app.commands, command)
	return nil
}

// Execute runs args using the process's standard input and error streams.
func (app *CLI) Execute(args []string, stdout io.Writer) error {
	return app.ExecuteWithIO(args, os.Stdin, stdout, os.Stderr)
}

// ExecuteWithIO dispatches args while using the supplied streams for all
// command I/O. Help flags are handled before normal dispatch; an incomplete
// command prefix displays its available children, while an unknown leaf is an
// error. This keeps help discoverable without treating misspellings as success.
func (app *CLI) ExecuteWithIO(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return app.writeHelp(stdout, nil)
	}
	if isHelp(args[0]) {
		return app.writeHelp(stdout, args[1:])
	}
	if len(args) > 1 && isHelp(args[len(args)-1]) {
		return app.writeHelp(stdout, args[:len(args)-1])
	}

	var match *Command
	for i := range app.commands {
		command := &app.commands[i]
		if len(command.Path) > len(args) || !equalPath(command.Path, args[:len(command.Path)]) {
			continue
		}
		// Prefer the longest matching path so nested commands win over their parents.
		if match == nil || len(command.Path) > len(match.Path) {
			match = command
		}
	}
	if match != nil {
		return match.Handler(Context{
			Args: args[len(match.Path):],
			In:   stdin,
			Out:  stdout,
			Err:  stderr,
		})
	}

	if children := app.children(args); len(children) != 0 {
		return app.writeHelp(stdout, args)
	}
	return fmt.Errorf("unknown command %q; run 'envman help' to see available commands", strings.Join(args, " "))
}

// Execute creates the built-in CLI and runs args, writing help and command
// output to stdout. Command errors are returned to the executable entry point,
// which can report them on stderr and set an appropriate process exit status.
func Execute(args []string, stdout io.Writer) error {
	app, err := New()
	if err != nil {
		return fmt.Errorf("create CLI: %w", err)
	}
	return app.Execute(args, stdout)
}

func (app *CLI) children(prefix []string) []Command {
	var children []Command
	for _, command := range app.commands {
		// Only return strict descendants; an exact match is a complete command,
		// not another item to show beneath the requested prefix.
		if len(command.Path) > len(prefix) && equalPath(prefix, command.Path[:len(prefix)]) {
			children = append(children, command)
		}
	}
	return children
}

func (app *CLI) writeHelp(stdout io.Writer, prefix []string) error {
	if len(prefix) != 0 {
		for _, command := range app.commands {
			if equalPath(command.Path, prefix) {
				return app.writeCommandHelp(stdout, command)
			}
		}
		children := app.children(prefix)
		if len(children) == 0 {
			return fmt.Errorf("no help available for %q", strings.Join(prefix, " "))
		}
	}

	if _, err := fmt.Fprintf(stdout, "\n  %s  %s\n\n", paint(stdout, colorBold+colorCyan, "ENVMAN"), paint(stdout, colorDim, "Environment variables, kept organized by project.")); err != nil {
		return fmt.Errorf("write CLI help: %w", err)
	}
	usage := fmt.Sprintf("Usage: %s %s", paint(stdout, colorDim, "envman"), paint(stdout, colorBold+colorBlue, "<command> [arguments]"))
	if _, err := fmt.Fprintf(stdout, "%s\n\n%s\n", usage, heading(stdout, "Available commands")); err != nil {
		return fmt.Errorf("write CLI help: %w", err)
	}
	commands := append([]Command(nil), app.commands...)
	if len(prefix) != 0 {
		commands = app.children(prefix)
	}
	sort.Slice(commands, func(i, j int) bool {
		return strings.Join(commands[i].Path, " ") < strings.Join(commands[j].Path, " ")
	})
	for _, command := range commands {
		name := strings.Join(command.Path, " ")
		if len(prefix) != 0 {
			name = strings.TrimPrefix(name, strings.Join(prefix, " ")+" ")
		}
		paddedName := fmt.Sprintf("%-24s", name)
		if _, err := fmt.Fprintf(stdout, "  %s %s\n", paint(stdout, colorBold+colorCyan, paddedName), paint(stdout, colorDim, command.Description)); err != nil {
			return fmt.Errorf("write CLI help: %w", err)
		}
	}
	if len(prefix) == 0 {
		if err := writeQuickGuide(stdout); err != nil {
			return fmt.Errorf("write CLI help: %w", err)
		}
	}
	return nil
}

func (app *CLI) writeCommandHelp(stdout io.Writer, command Command) error {
	name := strings.Join(command.Path, " ")
	usage, details, examples := commandGuide(name)
	if usage == "" {
		usage = "envman " + name + " [arguments]"
	}
	if _, err := fmt.Fprintf(stdout, "\n%s\n\n%s\n\n", heading(stdout, "Usage"), paint(stdout, colorBold+colorCyan, usage)); err != nil {
		return fmt.Errorf("write CLI help: %w", err)
	}
	if _, err := fmt.Fprintf(stdout, "%s\n%s\n", heading(stdout, "About"), command.Description); err != nil {
		return fmt.Errorf("write CLI help: %w", err)
	}
	if details != "" {
		if _, err := fmt.Fprintf(stdout, "\n%s\n%s\n", heading(stdout, "Details"), details); err != nil {
			return fmt.Errorf("write CLI help: %w", err)
		}
	}
	if len(examples) != 0 {
		if _, err := fmt.Fprintf(stdout, "\n%s\n", heading(stdout, "Examples")); err != nil {
			return fmt.Errorf("write CLI help: %w", err)
		}
		for _, example := range examples {
			if _, err := fmt.Fprintf(stdout, "  %s\n", paint(stdout, colorCyan, example)); err != nil {
				return fmt.Errorf("write CLI help: %w", err)
			}
		}
	}
	return nil
}

func writeQuickGuide(stdout io.Writer) error {
	sections := []struct {
		title string
		lines []string
	}{
		{
			title: "Quick start",
			lines: []string{
				"1. Run `envman setup` once to create the password-protected vault.",
				"2. In a project directory, run `envman init` to register the project.",
				"3. Create and select an environment: `envman env create dev`, then `envman env use dev`.",
				"4. Add variables with `envman set API_URL https://localhost`.",
				"5. Run a program with `envman run -- <command> [arguments...]`.",
			},
		},
		{
			title: "Password prompts",
			lines: []string{
				"`get`, `list --show`, full `export`, and `run` ask for the vault password.",
				"`list` shows names only. `set`, `unset`, `import`, and `export --example` do not unlock values.",
				"Secret-like names are masked by default; use `--show` to reveal values for one command.",
				"Interactive password input is hidden. The password cannot be reset to recover encrypted values.",
			},
		},
		{
			title: "Import and export",
			lines: []string{
				"`envman import .env` appends encrypted values to the active environment.",
				"`envman export backup.env` exports names and values to a new file (password required).",
				"`envman export --example .env.example` exports names with empty values (no password).",
				"Export refuses to overwrite a file that already exists.",
			},
		},
		{
			title: "Storage and color",
			lines: []string{
				"Set `ENVMAN_HOME` to change the shared storage directory.",
				"ANSI colors appear in interactive terminals; set `NO_COLOR` to disable them.",
				"Run `envman <command> --help` for command-specific usage and examples.",
			},
		},
	}
	for _, section := range sections {
		if _, err := fmt.Fprintf(stdout, "\n%s\n", heading(stdout, section.title)); err != nil {
			return err
		}
		for _, line := range section.lines {
			if _, err := fmt.Fprintf(stdout, "  %s\n", line); err != nil {
				return err
			}
		}
	}
	return nil
}

func isHelp(arg string) bool {
	// Accept the conventional short and long forms as well as "help".
	return arg == "help" || arg == "--help" || arg == "-h"
}

func equalPath(left, right []string) bool {
	// Compare words rather than joined strings to preserve command boundaries.
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
