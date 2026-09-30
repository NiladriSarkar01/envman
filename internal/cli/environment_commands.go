package cli

import (
	"errors"
	"fmt"

	"github.com/envman/internal/config"
	"github.com/envman/internal/environment"
	"github.com/envman/internal/store"
)

// envCreate validates the single requested name and creates the environment
// without implicitly changing the project's active selection.
func envCreate(ctx Context) error {
	if len(ctx.Args) != 1 {
		return errors.New("usage: envman env create <name>")
	}
	if err := requireVaultSetup(); err != nil {
		return err
	}
	paths, _, err := config.LoadCurrentProject()
	if err != nil {
		return err
	}
	if err := environment.Create(paths, ctx.Args[0]); err != nil {
		return err
	}
	_, err = fmt.Fprintf(ctx.Out, "%s %s.\n", success(ctx.Out, "Created environment"), paint(ctx.Out, colorCyan, ctx.Args[0]))
	return err
}

// envList prints the project's environments and marks the active one.
func envList(ctx Context) error {
	if err := requireNoArgs(ctx.Args, "env list"); err != nil {
		return err
	}
	paths, metadata, err := config.LoadCurrentProject()
	if err != nil {
		return err
	}
	names, err := environment.List(paths)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(ctx.Out, "%s %s\n\n", label(ctx.Out, "Project:"), heading(ctx.Out, metadata.Name)); err != nil {
		return err
	}
	for _, name := range names {
		// Mark the active environment consistently with the project listing.
		prefix := "  "
		if name == metadata.ActiveEnvironment {
			prefix = paint(ctx.Out, colorBold+colorGreen, "* ")
		}
		if _, err := fmt.Fprintf(ctx.Out, "%s%s\n", prefix, paint(ctx.Out, colorCyan, name)); err != nil {
			return fmt.Errorf("write environment list: %w", err)
		}
	}
	return nil
}

// envUse persists a new active selection and reports whether it was the first
// selection or a switch from an existing environment.
func envUse(ctx Context) error {
	if len(ctx.Args) != 1 {
		return errors.New("usage: envman env use <name>")
	}
	paths, metadata, err := config.LoadCurrentProject()
	if err != nil {
		return err
	}
	previous, err := environment.Use(paths, metadata, ctx.Args[0])
	if err != nil {
		return err
	}
	if previous == "" {
		_, err = fmt.Fprintf(ctx.Out, "%s %s\n", success(ctx.Out, "Environment selected:"), heading(ctx.Out, ctx.Args[0]))
	} else {
		_, err = fmt.Fprintf(ctx.Out, "%s %s → %s\n", success(ctx.Out, "Environment switched:"), paint(ctx.Out, colorDim, previous), heading(ctx.Out, ctx.Args[0]))
	}
	return err
}

// envCurrent displays both the project and its selected environment.
func envCurrent(ctx Context) error {
	if err := requireNoArgs(ctx.Args, "env current"); err != nil {
		return err
	}
	_, metadata, err := config.LoadCurrentProject()
	if err != nil {
		return err
	}
	active, err := environment.Current(metadata)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(ctx.Out, "%s %s\n%s %s\n", label(ctx.Out, "Project:"), heading(ctx.Out, metadata.Name), label(ctx.Out, "Environment:"), heading(ctx.Out, active))
	return err
}

// envDelete protects the active environment and confirms permanent removal
// unless the caller explicitly supplies --yes.
func envDelete(ctx Context) error {
	if len(ctx.Args) < 1 || len(ctx.Args) > 2 {
		return errors.New("usage: envman env delete <name> [--yes]")
	}
	yes := false
	if len(ctx.Args) == 2 {
		if ctx.Args[1] != "--yes" {
			return errors.New("usage: envman env delete <name> [--yes]")
		}
		yes = true
	}
	paths, metadata, err := config.LoadCurrentProject()
	if err != nil {
		return err
	}
	if metadata.ActiveEnvironment == ctx.Args[0] {
		return fmt.Errorf("cannot delete active environment %q; switch to another environment first", ctx.Args[0])
	}
	if !yes {
		variablesPath, err := environment.VariableFile(paths, ctx.Args[0])
		if err != nil {
			return err
		}
		names, err := store.Keys(variablesPath)
		if err != nil {
			return err
		}
		// Require explicit confirmation because this operation permanently
		// removes the environment directory and its stored values.
		confirmed, err := confirm(ctx, fmt.Sprintf("Delete environment %q? This will permanently delete %d variables. [y/N] ", ctx.Args[0], len(names)))
		if err != nil {
			return err
		}
		if !confirmed {
			_, err := fmt.Fprintln(ctx.Out, warning(ctx.Out, "Environment deletion cancelled."))
			return err
		}
	}
	if err := environment.Delete(paths, metadata, ctx.Args[0]); err != nil {
		return err
	}
	_, err = fmt.Fprintf(ctx.Out, "%s %s.\n", success(ctx.Out, "Deleted environment"), paint(ctx.Out, colorCyan, ctx.Args[0]))
	return err
}

// envRename changes an environment's name and preserves active selection when
// the renamed environment was selected.
func envRename(ctx Context) error {
	if len(ctx.Args) != 2 {
		return errors.New("usage: envman env rename <old-name> <new-name>")
	}
	paths, metadata, err := config.LoadCurrentProject()
	if err != nil {
		return err
	}
	if err := environment.Rename(paths, metadata, ctx.Args[0], ctx.Args[1]); err != nil {
		return err
	}
	_, err = fmt.Fprintf(ctx.Out, "%s %s %s %s.\n", success(ctx.Out, "Renamed environment"), paint(ctx.Out, colorDim, ctx.Args[0]), label(ctx.Out, "to"), paint(ctx.Out, colorCyan, ctx.Args[1]))
	return err
}
