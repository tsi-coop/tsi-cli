# Command reference

All commands are `tsi compose <command>`, run in the project directory (the one holding `tsi.yaml`), or anywhere with `--dir`.

## Global flags

| Flag | Meaning |
|---|---|
| `--dir <path>` | Project directory (default: the current directory) |
| `--project <name>` | Compose project name; overrides `project:` in `tsi.yaml` |
| `--port-offset <n>` | Add `n` to every upstream host port; overrides `port_offset` |
| `--non-interactive` | Never prompt; use flags and defaults |
| `-o, --output json` | Machine-readable output where a command prints results |
| `--plan` | Show what would happen and change nothing (`init`, `up`, `bootstrap`, `down`) |

Progress messages go to standard error and results to standard output, so `tsi compose creds -o json | jq` works.

## Commands

### `tsi compose modules`

Lists the modules `tsi` can run, with the host port each uses by default (the upstream port plus the default offset), image and purpose.

### `tsi compose init`

Chooses modules, generates secrets and writes `tsi.yaml`, `docker-compose.tsi.yml` and `.tsi/`.

| Flag | Meaning |
|---|---|
| `--modules a,b` | Modules to run; `module:instance` for a named instance |
| `--database shared\|per-module\|external` | Database mode |
| `--dsn <url>` | Administrator connection string for `--database external` |
| `--admin-email`, `--admin-name` | The administrator created in each module |
| `--force` | Overwrite an existing `tsi.yaml`. Secrets are never overwritten |

Without `--modules` it asks, unless `--non-interactive` (then it fails and tells you to pass `--modules`).

### `tsi compose up`

Starts the stack, waits until every module is ready, then prints the status.

| Flag | Meaning |
|---|---|
| `-d` | Accepted for familiarity with `docker compose`. `up` always runs in the background |
| `--bootstrap` | Also provision the modules ([Bootstrap and signing in](bootstrap-and-signing-in.md)) |
| `--plan` | Show files, services, ports, migrations and bootstrap steps; change nothing |

Safe to run again: it regenerates the files from `tsi.yaml` and `.tsi/state.yaml`, recreates containers whose settings changed, applies only new migrations, and skips completed bootstrap steps. It stops with exit code 8 if containers exist but `.tsi/state.yaml` is missing.

### `tsi compose bootstrap`

Provisioning only, for a stack that is already running. Safe to repeat; completed steps are skipped.

### `tsi compose status`

Container state, whether each module answers its readiness call, and whether it is provisioned. `-o json` for scripts.

### `tsi compose creds`

Prints the saved sign-in details.

| Flag | Meaning |
|---|---|
| `--module <name>` | One module or instance |
| `--show-secret` | Print secrets in full (masked by default) |
| `-o json` | JSON |

### `tsi compose test`

Runs each module's smoke tests with the saved credentials. See [Testing and CI](testing-and-ci.md).

| Flag | Meaning |
|---|---|
| `--module <name>` | One module or instance |
| `--format text\|json\|junit` | Report format (`-o json` also works) |

Exits with code 6 if any check fails.

### `tsi compose logs [module...]`

Container logs, optionally for named modules. `-f` follows, `--tail <n>` limits the lines. Known secrets are redacted.

### `tsi compose down`

Stops and removes the containers and network; keeps the data and `.tsi/`.

| Flag | Meaning |
|---|---|
| `--volumes` | **Also delete the data volumes and `.tsi/`, the compose file and `tsi.lock`.** Asks you to type the project name |
| `--yes` | Skip the confirmation (required when not interactive) |

### `tsi version`

Prints the version: the git tag, or the commit hash for an untagged build. `-dirty` marks uncommitted changes.

## Exit codes

Errors name the module and step and suggest a fix. Scripts can rely on these codes:

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Internal or unexpected error |
| 2 | Usage error: bad flag, bad `tsi.yaml`, a module listed wrongly |
| 3 | Preflight failed: Docker unreachable, a port in use, a Docker command failed |
| 4 | A module was not ready in time |
| 5 | A bootstrap step failed |
| 6 | One or more smoke tests failed |
| 7 | A permanent secret would have changed |
| 8 | State lost: containers exist but `.tsi/state.yaml` is missing |
| 9 | Not found: no `tsi.yaml`, unknown module, no saved credentials |
| 10 | Database or migration problem, including a download or checksum failure |
