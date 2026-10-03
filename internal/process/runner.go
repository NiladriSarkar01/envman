package process

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"sort"
	"strings"

	"golang.org/x/sys/windows"
)

type environmentEntry struct {
	// Preserve the spelling used in the resulting KEY=value entry.
	name  string
	value string
}

// consoleSnapshot stores the original console mode of a Windows console handle.
//
// Windows console modes control how the console behaves, including aspects of
// input processing and output processing.
//
// A child process may change these modes while running. If it exits abnormally
// or is interrupted, the terminal can sometimes be left in an unexpected state.
//
// We capture the original modes before launching the child and attempt to
// restore them after the child has completely exited.
type consoleSnapshot struct {
	handle windows.Handle
	mode   uint32
}

// captureConsoleModes records the current console modes of the standard
// input, output, and error handles.
//
// Important details:
//
//   - Only actual *os.File values referring to the standard streams are used.
//   - Redirected streams, such as files or pipes, are not console handles.
//   - GetConsoleMode may fail for redirected handles; those are simply skipped.
//   - Duplicate handles are captured only once.
//
// This function does not modify the console. It only records its current state.
func captureConsoleModes(
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
) []consoleSnapshot {
	var snapshots []consoleSnapshot

	// Track handles already captured so that we do not restore the same
	// console handle multiple times.
	seen := make(map[windows.Handle]bool)

	// capture accepts a standard stream only when it is the corresponding
	// operating-system file handle.
	capture := func(file *os.File) {
		if file == nil {
			return
		}

		handle := windows.Handle(file.Fd())

		// Avoid duplicate snapshots when multiple streams refer to the
		// same underlying console handle.
		if seen[handle] {
			return
		}

		var mode uint32

		// GetConsoleMode succeeds for console handles.
		// It normally fails for redirected files and pipes, which is expected.
		if err := windows.GetConsoleMode(handle, &mode); err != nil {
			return
		}

		seen[handle] = true

		snapshots = append(snapshots, consoleSnapshot{
			handle: handle,
			mode:   mode,
		})
	}

	// Only capture the actual standard streams. Do not assume that arbitrary
	// readers or writers expose Windows console handles.
	if file, ok := stdin.(*os.File); ok && file == os.Stdin {
		capture(file)
	}

	if file, ok := stdout.(*os.File); ok && file == os.Stdout {
		capture(file)
	}

	if file, ok := stderr.(*os.File); ok && file == os.Stderr {
		capture(file)
	}

	return snapshots
}

// restoreConsoleModes attempts to restore every console mode captured earlier.
//
// We attempt all restorations even if one fails. This is important because
// failure to restore one handle should not prevent attempts on the others.
//
// The returned error contains any restoration failures.
func restoreConsoleModes(snapshots []consoleSnapshot) error {
	var restoreErrors []error

	for _, snapshot := range snapshots {
		if err := windows.SetConsoleMode(
			snapshot.handle,
			snapshot.mode,
		); err != nil {
			restoreErrors = append(
				restoreErrors,
				fmt.Errorf(
					"restore console mode for handle %v: %w",
					snapshot.handle,
					err,
				),
			)
		}
	}

	return errors.Join(restoreErrors...)
}

// Run launches an external process and manages its lifecycle.
//
// Behaviour:
//
//  1. Capture the current console modes.
//  2. Register for Ctrl+C notifications.
//  3. Start the child process.
//  4. Wait for either the child to exit or Ctrl+C to arrive.
//  5. If Ctrl+C arrives, terminate the direct child process.
//  6. Wait for the child to finish before returning.
//  7. Restore the captured console modes.
//
// The function deliberately waits for the child before returning. This avoids
// leaving a direct child process running after the runner has finished.
//
// Limitations:
//
//   - Process.Kill terminates the direct child, not necessarily its descendants.
//   - A child using ANSI terminal sequences may require additional terminal
//     cleanup beyond restoring Windows console modes.
//   - Console restoration is best-effort; Windows may reject a restoration.
//
// The caller remains responsible for deciding whether the application itself
// should exit after Run returns.
func Run(
	name string,
	args []string,
	variables map[string]string,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
) (retErr error) {

	// Capture console state before launching the child.
	//
	// We use a named return value so that a restoration error can be combined
	// with the process execution error in the deferred cleanup.
	consoleState := captureConsoleModes(stdin, stdout, stderr)

	// Deferred cleanup executes on every normal return path, including:
	//   - child startup failure
	//   - successful child completion
	//   - Ctrl+C interruption
	//
	// It does not execute if the program terminates through os.Exit or a
	// forced process termination.
	defer func() {
		if err := restoreConsoleModes(consoleState); err != nil {
			retErr = errors.Join(retErr, err)
		}
	}()

	// Construct the command without invoking a shell.
	//
	// This means name and args are passed directly to the operating system.
	// Shell features such as pipes, redirects, and variable expansion are not
	// interpreted automatically.
	command := exec.Command(name, args...)

	// Connect the child process to the supplied streams.
	//
	// Passing os.Stdin, os.Stdout, and os.Stderr allows the child to interact
	// directly with the user's terminal.
	command.Stdin = stdin
	command.Stdout = stdout
	command.Stderr = stderr

	// Build the child's environment from the current environment plus the
	// supplied overrides.
	//
	// This assumes mergedEnvironment preserves the existing environment and
	// applies the values in variables according to your implementation.
	command.Env = mergedEnvironment(os.Environ(), variables)

	// Subscribe to Ctrl+C before starting the child.
	//
	// This gives the parent an opportunity to handle an interrupt rather than
	// immediately terminating before cleanup can run.
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)

	// Always unregister the signal handler when Run returns.
	defer signal.Stop(interrupts)

	// Start the child without blocking until it exits.
	if err := command.Start(); err != nil {
		return fmt.Errorf("start %q: %w", name, err)
	}

	// Wait must be called exactly once for this process.
	//
	// Running Wait in a goroutine allows the main function to react to Ctrl+C
	// while the child is still running.
	waitResult := make(chan error, 1)

	go func() {
		waitResult <- command.Wait()
	}()

	select {
	case err := <-waitResult:
		// The child exited normally or returned an execution error.
		//
		// The deferred console restoration still runs before Run returns.
		if err != nil {
			return fmt.Errorf("run %q: %w", name, err)
		}

		return nil

	case sig := <-interrupts:
		// Ctrl+C was received by the parent.
		//
		// We terminate the direct child and then wait for it. Do not return
		// immediately after Kill: the process may still be shutting down.
		killErr := command.Process.Kill()

		// Wait for the child to finish and release its process resources.
		waitErr := <-waitResult

		// A child may exit between the interrupt and the Kill call.
		// In that case, an already-finished process is not a cleanup failure.
		if errors.Is(killErr, os.ErrProcessDone) {
			killErr = nil
		}

		// The interrupt was handled and the direct child has finished.
		// Return an interruption error so the caller can decide whether
		// to exit, retry, or continue.
		interruptErr := fmt.Errorf("process %q interrupted by %v", name, sig)

		return errors.Join(
			interruptErr,
			wrapIfNotNil("terminate child process", killErr),
			wrapIfNotNil("wait for child process", waitErr),
		)
	}
}

// wrapIfNotNil adds context to an error without producing a nil error.
//
// This keeps the cleanup error messages understandable when several errors
// are combined with errors.Join.
func wrapIfNotNil(context string, err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("%s: %w", context, err)
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
