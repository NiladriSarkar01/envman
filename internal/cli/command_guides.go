package cli

func commandGuide(name string) (usage, details string, examples []string) {
	switch name {
	case "setup":
		return "envman setup",
			"Creates shared EnvMan storage and initializes the vault. Enter and confirm a password of at least 12 bytes. The password is not stored; losing it means encrypted values cannot be recovered. Setup is a one-time operation.",
			[]string{"envman setup"}
	case "init":
		return "envman init",
			"Registers the current directory as an EnvMan project. A small .envman marker is created in the project, while project metadata and environment data live in shared storage.",
			[]string{"cd path/to/project", "envman init"}
	case "project info":
		return "envman project info",
			"Shows the current registered project's name, stable ID, and active environment.",
			[]string{"envman project info"}
	case "project list":
		return "envman project list",
			"Lists registered projects and marks the current working directory with an asterisk.",
			[]string{"envman project list"}
	case "project remove":
		return "envman project remove [--yes]",
			"Removes EnvMan registration and stored environment data for the current project. It never deletes the source-code directory. Without --yes, removal requires confirmation.",
			[]string{"envman project remove", "envman project remove --yes"}
	case "env create":
		return "envman env create <name>",
			"Creates a named environment after shared vault setup and project initialization. Names are single path components: whitespace and path separators are not allowed. Creating an environment does not select it.",
			[]string{"envman env create dev", "envman env create production"}
	case "env list":
		return "envman env list",
			"Lists the current project's environments and marks the active environment with an asterisk.",
			[]string{"envman env list"}
	case "env use":
		return "envman env use <name>",
			"Selects an existing environment as the project's active environment. This selection is used by commands that do not accept an explicit --env option.",
			[]string{"envman env use dev"}
	case "env current":
		return "envman env current",
			"Shows the current project and active environment.",
			[]string{"envman env current"}
	case "env delete":
		return "envman env delete <name> [--yes]",
			"Permanently deletes an environment and its variables. The active environment cannot be deleted. Without --yes, EnvMan displays a confirmation prompt.",
			[]string{"envman env delete test", "envman env delete test --yes"}
	case "env rename":
		return "envman env rename <old-name> <new-name>",
			"Renames an existing environment. If it is active, the active selection is updated too. The destination name must not already exist.",
			[]string{"envman env rename dev development"}
	case "set":
		return "envman set <name> <value>",
			"Encrypts the value with the vault's public key and appends it to the environment journal. It does not read or decrypt existing values, so no password is required. Values containing spaces should be quoted by the shell.",
			[]string{`envman set API_URL "https://localhost:8443"`, `envman set DB_PASSWORD "correct horse battery staple"`}
	case "get":
		return "envman get <name> [--show]",
			"Unlocks the vault and prints one value. Likely-secret variable names are masked unless --show is supplied or reveal_secrets_by_default is enabled in config.json.",
			[]string{"envman get API_URL", "envman get DB_PASSWORD --show"}
	case "unset":
		return "envman unset <name>",
			"Appends a deletion record for a variable without decrypting other values or asking for the vault password. It reports an error if the name is not currently set.",
			[]string{"envman unset OLD_TOKEN"}
	case "list":
		return "envman list [--show]",
			"Without --show, prints variable names only and does not unlock the vault. With --show, prompts for the vault password and prints names and values; likely secrets remain masked unless reveal_secrets_by_default is enabled.",
			[]string{"envman list", "envman list --show"}
	case "run":
		return "envman run [--env <name>] -- <command> [arguments...]",
			"Prompts for the vault password, loads the selected environment, and runs a child process with its variables added to the inherited process environment. Use --env to choose another environment for this invocation without changing the project's active selection. The -- separator is required.",
			[]string{"envman run -- go test ./...", "envman run --env production -- ./server --port 8080"}
	case "import":
		return "envman import <.env-file> [--env <name>]",
			"Parses KEY=VALUE entries and appends encrypted values using the vault's public key. Existing keys not present in the import are untouched; duplicate names in the input resolve to the last value. No password is required.",
			[]string{"envman import .env", "envman import .env --env production"}
	case "export":
		return "envman export [--example] <.env-file> [--env <name>]",
			"Exports the active environment by default or selects another with --env. Full value export requires the vault password. --example writes variable names with empty values and does not unlock the vault. The destination must not already exist.",
			[]string{"envman export backup.env", "envman export --env production backup.env", "envman export --example .env.example"}
	default:
		return "", "", nil
	}
}
