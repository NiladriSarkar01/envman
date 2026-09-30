// Package cli defines EnvMan's command-line interface, built-in commands, and
// the handlers that connect user requests to the configuration and storage
// layers. Handlers receive their arguments and I/O streams through Context,
// which keeps command behavior testable without invoking the process directly.
package cli
