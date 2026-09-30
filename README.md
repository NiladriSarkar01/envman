# EnvMan

EnvMan is a command-line tool for managing per-project environment variables.
It keeps each project's environments separate, encrypts newly written values,
and can run programs with the selected environment.

## Requirements

- Go 1.27 or newer to build from source.
- A terminal for hidden password entry. Piped input is also supported.

## Build and install

Build the command from the repository root:

```powershell
go build -o envman.exe .\cmd\envman
```

Or install it into your Go binary directory:

```powershell
go install .\cmd\envman
```

Run `envman help` for a quick-start overview or `envman <command> --help` for
details and examples for an individual command.

## First-time setup

Initialize EnvMan's shared storage and create its vault once:

```text
envman setup
```

Setup asks you to enter and confirm a password of at least 12 bytes. Terminal
input is hidden while you type. **Keep this password safe:** it cannot be reset
to recover encrypted values.

Next, initialize a project from its root directory and create an environment:

```text
cd path/to/my-project
envman init
envman env create dev
envman env use dev
```

Run `envman setup` before creating environments. Run `envman init` separately
for every source project you want EnvMan to manage.

## Managing variables

Set, list, retrieve, and remove variables in the active environment:

```text
envman set API_URL https://localhost:8443
envman set DB_PASSWORD "my local database password"
envman list
envman get API_URL
envman get DB_PASSWORD --show
envman unset API_URL
```

`set` encrypts each value using the vault's public key and appends it without
reading or decrypting existing values. `unset` appends a removal record.
Neither command prompts for the password.

`list` prints variable **names only** and does not unlock the vault. To print
values, use:

```text
envman list --show
```

That command prompts for the vault password. Likely-secret names (for example,
`PASSWORD`, `TOKEN`, and `API_KEY`) are masked by default in `get` and
`list --show`; use `get <name> --show` to reveal an individual value. The
`reveal_secrets_by_default` setting in the shared `config.json` can change this
masking default.

## Running a program

Use `run` to pass environment variables to a child process. It prompts for the
vault password because it decrypts values:

```text
envman run -- go test ./...
envman run -- npm test
envman run --env production -- ./server --port 8080
```

The `--` separator is required. Options before it belong to EnvMan; the command
and arguments after it are passed to the child process. `--env` selects an
environment for that invocation only and does not change the active selection.
The child's environment inherits the current process environment, with values
from EnvMan taking precedence for matching variable names.

## Environments and projects

```text
envman env list
envman env current
envman env use production
envman env rename dev development
envman env delete test

envman project info
envman project list
envman project remove
```

Environment names cannot contain path separators or whitespace. The active
environment cannot be deleted; select a different one first. Deletion and
project removal prompt for confirmation. Supply `--yes` for non-interactive
use:

```text
envman env delete test --yes
envman project remove --yes
```

Project removal deletes EnvMan's registration and stored data, not the source
project directory.

## Import and export

Import merges entries from a `.env` file into the selected environment. Values
are encrypted and appended without unlocking existing data:

```text
envman import .env
envman import .env --env production
```

Export all values to a **new** file; this prompts for the vault password:

```text
envman export backup.env
envman export --env production backup.env
```

For a template containing names but no values, use `--example`. This does not
prompt for the password:

```text
envman export --example .env.example
```

Export refuses to overwrite an existing destination. Store exported files
securely: full exports contain plaintext values.

## Storage and encryption

Shared storage contains `config.json`, the password-wrapped vault key, the
vault public key, and the password-protected private key. Project markers live
in the source project directories; project metadata and variable journals live
in shared storage, grouped by project and environment.

Set `ENVMAN_HOME` to select another shared storage directory:

```powershell
$env:ENVMAN_HOME = "D:\secure\envman"
```

New variable values are encrypted individually with a randomly generated data
key, wrapped with the vault's RSA public key, and authenticated with AES-GCM.
The private key is encrypted with the vault key, which is unlocked from the
password using Argon2id. Variable updates and removals are append-only journal
entries; old entries remain encrypted in the journal after being superseded or
removed.

Older plaintext variable JSON remains readable. Older whole-file-encrypted
files can still be read with the vault password, but their keys cannot be
listed or exported as an example without migration because their names are
encrypted together with their values.

## Command reference

| Command | Purpose | Password needed |
| --- | --- | --- |
| `setup` | Create shared storage and the vault | To create and confirm |
| `init` | Register the current directory as a project | No |
| `env create/list/use/current/rename` | Manage environments | No |
| `env delete` | Delete an inactive environment | No |
| `project info/list` | Show project information | No |
| `project remove` | Remove EnvMan project data | No |
| `set <name> <value>` | Append an encrypted value | No |
| `unset <name>` | Append a deletion entry | No |
| `list` | Print variable names only | No |
| `get <name>` | Print one value (secret masking applies) | Yes |
| `get <name> --show` | Reveal one value | Yes |
| `list --show` | Print names and values | Yes |
| `run ...` | Run a child process with environment values | Yes |
| `import <file>` | Append values from a `.env` file | No |
| `export --example <file>` | Export names with empty values | No |
| `export <file>` | Export names and values | Yes |

## Terminal colors

EnvMan uses color in interactive terminals. Set `NO_COLOR` to disable ANSI
colors. Output redirected to a file or pipe remains uncolored.
