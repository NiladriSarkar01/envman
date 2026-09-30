package cli

import (
	"fmt"
	"os"

	"github.com/envman/internal/config"
)

// projectInfo displays stable project identity and the current environment
// selection without exposing variable values.
func projectInfo(ctx Context) error {
	if err := requireNoArgs(ctx.Args, "project info"); err != nil {
		return err
	}
	_, metadata, err := config.LoadCurrentProject()
	if err != nil {
		return err
	}
	active := metadata.ActiveEnvironment
	if active == "" {
		// Keep the display useful before the user creates or selects an environment.
		active = "(none)"
	}
	if _, err := fmt.Fprintf(ctx.Out, "%s\n  %s %s\n  %s %s\n\n%s %s\n",
		heading(ctx.Out, "Project"),
		label(ctx.Out, "Name:"), paint(ctx.Out, colorBold+colorCyan, metadata.Name),
		label(ctx.Out, "ID:"), paint(ctx.Out, colorDim, metadata.ID),
		label(ctx.Out, "Active environment:"), paint(ctx.Out, colorBold+colorGreen, active)); err != nil {
		return fmt.Errorf("write project info: %w", err)
	}
	return nil
}

// projectList displays every registered project and highlights the current
// working directory when it is among them.
func projectList(ctx Context) error {
	if err := requireNoArgs(ctx.Args, "project list"); err != nil {
		return err
	}
	projects, err := config.ListProjects()
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("determine current directory: %w", err)
	}
	if _, err := fmt.Fprintln(ctx.Out, heading(ctx.Out, "Projects")); err != nil {
		return fmt.Errorf("write project list: %w", err)
	}
	for _, project := range projects {
		// Highlight the project matching the current working directory.
		prefix := "  "
		if samePath(cwd, project.Path) {
			prefix = paint(ctx.Out, colorBold+colorGreen, "* ")
		}
		if _, err := fmt.Fprintf(ctx.Out, "%s%s\n", prefix, paint(ctx.Out, colorCyan, project.Name)); err != nil {
			return fmt.Errorf("write project list: %w", err)
		}
	}
	return nil
}

// projectRemove unregisters this project and deletes its EnvMan data after
// confirmation; the source-code directory itself is never removed.
func projectRemove(ctx Context) error {
	yes, err := confirmationFlag(ctx.Args)
	if err != nil {
		return err
	}
	paths, metadata, err := config.LoadCurrentProject()
	if err != nil {
		return err
	}
	if !yes {
		// Removal affects EnvMan's registration and stored data, not source files.
		confirmed, err := confirm(ctx, fmt.Sprintf("Remove EnvMan project %q and its environment data? The source-code project will not be deleted. [y/N] ", metadata.Name))
		if err != nil {
			return err
		}
		if !confirmed {
			_, err := fmt.Fprintln(ctx.Out, warning(ctx.Out, "Project removal cancelled."))
			return err
		}
	}
	if err := config.RemoveCurrentProject(true); err != nil {
		return err
	}
	_, err = fmt.Fprintf(ctx.Out, "%s %s %s\n", success(ctx.Out, "Removed EnvMan data for project"), paint(ctx.Out, colorCyan, metadata.Name), paint(ctx.Out, colorDim, "("+paths.ID+")"))
	return err
}
