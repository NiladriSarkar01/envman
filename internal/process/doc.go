// Package process runs external commands with variables loaded from an EnvMan
// environment. The environment supplied by EnvMan overlays the caller's
// inherited process environment, so unrelated system variables remain
// available to the child command.
package process
