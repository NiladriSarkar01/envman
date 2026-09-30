package cli

import (
	"fmt"

	"github.com/envman/internal/config"
)

// runInit registers the current working directory and prints the generated or
// reused project ID and marker location.
func runInit(ctx Context) error {
	// Initialization binds the current directory to its stable EnvMan project
	// ID and prepares the separate storage area for its environments.
	if len(ctx.Args) != 0 {
		return fmt.Errorf("init does not accept arguments")
	}
	paths, err := config.InitializeProject()
	if err != nil {
		return fmt.Errorf("initialize project: %w", err)
	}
	if _, err := fmt.Fprintf(ctx.Out, "%s %s %s\n%s %s\n",
		success(ctx.Out, "Project"),
		paint(ctx.Out, colorBold+colorCyan, paths.Name),
		success(ctx.Out, "initialized."),
		label(ctx.Out, "Marker:"), paint(ctx.Out, colorDim, paths.Marker)); err != nil {
		return fmt.Errorf("write initialization result: %w", err)
	}
	return nil
}
