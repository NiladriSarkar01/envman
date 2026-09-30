package cli

func builtInCommands() []Command {
	// Keep command registration declarative so help output and dispatch use the
	// same source of truth for names, descriptions, and handlers.
	return []Command{
		{Path: []string{"setup"}, Description: "Initialize shared storage and create the password-protected vault", Handler: runSetup},
		{Path: []string{"init"}, Description: "Register the current project", Handler: runInit},
		{Path: []string{"project", "info"}, Description: "Show current project information", Handler: projectInfo},
		{Path: []string{"project", "list"}, Description: "List registered projects", Handler: projectList},
		{Path: []string{"project", "remove"}, Description: "Remove current project's EnvMan data", Handler: projectRemove},
		{Path: []string{"env", "create"}, Description: "Create an environment", Handler: envCreate},
		{Path: []string{"env", "list"}, Description: "List current project's environments", Handler: envList},
		{Path: []string{"env", "use"}, Description: "Select the active environment", Handler: envUse},
		{Path: []string{"env", "current"}, Description: "Show the active environment", Handler: envCurrent},
		{Path: []string{"env", "delete"}, Description: "Delete an environment", Handler: envDelete},
		{Path: []string{"env", "rename"}, Description: "Rename an environment", Handler: envRename},
		{Path: []string{"set"}, Description: "Encrypt and set a variable in the active environment", Handler: setVariable},
		{Path: []string{"get"}, Description: "Get a variable value (vault password required)", Handler: getVariable},
		{Path: []string{"unset"}, Description: "Remove a variable from the active environment", Handler: unsetVariable},
		{Path: []string{"list"}, Description: "List variable names; --show displays values (vault password required)", Handler: listVariables},
		{Path: []string{"run"}, Description: "Run a command with unlocked active-environment variables", Handler: runWithEnvironment},
		{Path: []string{"import"}, Description: "Encrypt and import a .env file without unlocking existing values", Handler: importEnvironment},
		{Path: []string{"export"}, Description: "Export values (password required) or names only with --example", Handler: exportEnvironment},
	}
}
