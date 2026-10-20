# tsi-cli

A CLI to pick, configure, and launch the data infrastructure primitives your stack needs.

> **Status: soft launch.** Four modules are implemented and tested end to end against the real containers: `tsi-dpdp-cms`, `tsi-privacy-vault`, `tsi-compass` and `tsi-dx-node`. See [Modules](#modules) and [Roadmap](#roadmap).

## Why tsi-cli

Each TSI module (consent, privacy vault, compliance, data exchange) is set up by hand today: write an env file, mount its `db/` folder into Postgres, start it, open a browser console, create the first administrator, create an app, generate an API key, and copy a secret that is shown only once. Every module has slightly different steps, setup tokens and variable names.

`tsi` does all of that from one command, and then proves it works.

## Who this is for

`tsi` is for **local development, evaluation and CI**: trying a module in minutes, and giving an ISV's `go test` runs a real module to talk to. It binds ports to `127.0.0.1`, generates secrets for you, and has no TLS, backup, upgrade or high-availability story. **It is not a production installer.** For production, follow the deployment guide in each module's own repository.

## Key Features

- **Pick only what you need.** The modules are independent. Run just the vault, just the consent service, or both on one shared PostgreSQL.
- **Secrets generated for you.** JWT secrets, setup tokens, encryption keys and keystores are created with real values, never the insecure defaults the compose files ship with.
- **Provisioned through the modules' own APIs.** Administrator, tenant app and API key are created for you and saved locally (file mode 0600).
- **Smoke tests included.** `tsi compose test` checks each module with the credentials it just created.
- **Safe to re-run.** Bootstrap is idempotent. Permanent keys (database encryption key, vault master key) are never silently regenerated.
- **Also a Go library.** Use `tsitest.Start(t, ...)` in your own `go test` runs to test against real modules instead of mocks.
- **Local first.** No telemetry. Everything lives in `.tsi/` next to your project.
- **Nothing vendored.** The database scripts are downloaded from each module's own repository at a pinned release and checked against a SHA-256 before they run.

## Quick Start

You need Docker and a terminal.

1. Install `tsi` (see [Installation](#installation)).
2. Create a working directory and choose your modules:
   ```bash
   mkdir my-tsi-stack && cd my-tsi-stack
   tsi compose init
   ```
   This asks which modules you want and where the database should live, generates the secrets, and writes `tsi.yaml`, `docker-compose.tsi.yml` and `.tsi/` (secrets, env files, keystores; git-ignored). Non-interactive: `tsi compose init --modules tsi-dpdp-cms --non-interactive`.
3. Start the stack and provision it:
   ```bash
   tsi compose up -d --bootstrap
   ```
4. Check that everything works:
   ```bash
   tsi compose test
   ```
5. See what was created, including the API key for each module:
   ```bash
   tsi compose status
   tsi compose creds --show-secret
   ```
6. When you are done:
   ```bash
   tsi compose down --volumes
   ```
   `--volumes` also removes the data and the saved credentials. Leave it off to keep them.

## Installation

### Prerequisites

| Requirement | Why |
|---|---|
| Docker 24+ with the `docker compose` plugin | Runs the modules and PostgreSQL |
| Free ports | Defaults are 8081 to 8088 for the modules and 5432 to 5440 for PostgreSQL. Use `PortOffset` or `tsi.yaml` overrides if they clash |
| Go 1.22+ | Only to install with `go install` or build from source |

### Option 1: Build from source

```bash
git clone https://github.com/tsi-coop/tsi-cli.git
cd tsi-cli
make build            # writes bin/tsi
sudo mv bin/tsi /usr/local/bin/
tsi version
```

### Option 2: go install

```bash
go install github.com/tsicoop/tsi-cli/cmd/tsi@latest
tsi version
```

This works once the repository is published at that path.

### Option 3: Homebrew (planned)

```bash
brew install tsicoop/tap/tsi
```

### Option 4: Release binary (planned)

Download the archive for your OS from the GitHub releases page, unpack it and place `tsi` on your `PATH`.

## Database scripts

The module images do not contain their schema, so `tsi` applies the SQL files from the module's repository. Each module in the catalog pins a repository, a tag (or commit) and a SHA-256 for every file. On first use `tsi` downloads the files from `raw.githubusercontent.com`, verifies them, and caches them under your user cache directory (`~/.cache/tsi-cli`, or `$TSI_CACHE_DIR`). A file that does not match its checksum is never cached or run. Downloading happens before any container starts, so a network problem fails early and changes nothing.

Applied files are recorded in a `_tsi_cli_migrations` table in the module's database, so a re-run applies only new files, and a file that changed after it was applied is refused.

Offline, or developing a module? Point `tsi` at a local checkout in `tsi.yaml`; those files are used as they are (with a warning if they differ from the pinned release):

```yaml
migration_dirs:
  tsi-privacy-vault: /home/me/tsi-privacy-vault/db
```

## Commands

| Command | What it does |
|---|---|
| `tsi compose modules` | List the available modules |
| `tsi compose init` | Choose modules, generate secrets, write the config and compose files. Never overwrites secrets without `--force` |
| `tsi compose up [-d] [--bootstrap]` | Start the stack, optionally create the admin, app and API key |
| `tsi compose bootstrap` | Provisioning only. Re-running changes nothing |
| `tsi compose status` | Container health, ports and readiness |
| `tsi compose creds [--module m] [--show-secret]` | Show saved tenant credentials (masked by default) |
| `tsi compose test [--module m] [--format json\|junit]` | Run smoke tests; exits 6 if any fail |
| `tsi compose logs [-f] [module...]` | Combined logs |
| `tsi compose down [--volumes]` | Stop the stack |

Global flags: `--dir`, `--project`, `--port-offset N` (shift every host port, useful when 8080 or 5432 are taken), `--non-interactive`, `--output json` and `--plan` (a dry run that changes nothing). Errors name the module and step and suggest a fix; exit codes are stable (see `tsierr`).

### Database modes

| Mode | What runs |
|---|---|
| `shared` (default) | One Postgres container, one database and role per module |
| `per-module` | One Postgres container per module, like the upstream compose files |
| `external` | Your own Postgres: `--database external --dsn postgres://admin:pw@host:5432/postgres`. The admin role must be a superuser, and the server must be reachable from containers as `host.docker.internal` (or set `database.module_host`) |

Set the mode in `tsi.yaml` (`database.mode`) or with `tsi compose init --database`.

## Modules

| Module | Purpose | Version | Smoke tests |
|---|---|---|---|
| `tsi-dpdp-cms` | Consent management | v0.5.3 | 7 |
| `tsi-privacy-vault` | PII vault, tokenization and erasure | v0.3.0 | 10 |
| `tsi-compass` | IT GRC and compliance operations | v0.4.3 | 6 |
| `tsi-dx-node` | Peer-to-peer governed data exchange (run as a pair) | v0.3.0 | 6 per node, 2 only on the sender |

### What `tsi compose up --bootstrap` does for `tsi-dpdp-cms`

1. Generates the JWT secret, database encryption key, lookup salt, setup token, database password and a PKCS#12 keystore.
2. Starts PostgreSQL and applies the 13 migrations (plus the `app.enc_key` database setting upstream sets in `03z_enc_key.sh`).
3. Starts the module and waits for it to answer a readiness call.
4. Creates the administrator, a data fiduciary, an app, an API key (READ and WRITE) and a default consent policy, and activates the policy.
5. Saves the API key and secret to `.tsi/state.yaml` (the module shows the secret only once).

The fiduciary's domain validation needs real DNS, so on localhost it stays `PENDING`. Edit names and contact details under `bootstrap:` in `tsi.yaml`.

### What `tsi compose up --bootstrap` does for `tsi-privacy-vault`

1. Generates the master key, blind-index secret, JWT secret, setup token and database password. The **master key** and **index secret** are permanent: they are locked once the database holds data, and `tsi` refuses to change them.
2. Applies the 5 migrations, starts the module and waits for it.
3. Creates the `SYSTEM_ADMIN` account, three entities (`TSI_ID_NUMBER` as an ID, `TSI_NOTE` as DATA, `TSI_DOCUMENT` as a FILE), a client API key, and read-write permission on each entity.
4. Saves the key and secret to `.tsi/state.yaml`.

Change the entities under `bootstrap: tsi-privacy-vault: entities:` in `tsi.yaml`. The vault runs with `TSI_PRIVACY_VAULT_ENV=local` here (upstream defaults to `production`, which marks the console cookie `Secure` and breaks plain-http use).

### What `tsi compose up --bootstrap` does for `tsi-compass`

1. Generates the JWT secret, the database encryption key and lookup salt (both permanent once data exists), the setup token and the database password.
2. Applies the 22 migrations from the pinned commit, starts the module and waits for it.
3. Creates the administrator. Compass has no usable machine credential in v0.4.3 (see [Notes for the module maintainers](#notes-for-the-module-maintainers)), so **the credential is the administrator login**: `tsi compose creds --show-secret` prints it, and the console is at the module's URL.

### What `tsi compose up --bootstrap` does for `tsi-dx-node`

A node is only useful with a peer, so run two. Name each instance after a colon:

```yaml
# tsi.yaml
modules:
  - tsi-dx-node:node-a
  - tsi-dx-node:node-b
```

```bash
tsi compose init --modules tsi-dx-node:node-a,tsi-dx-node:node-b --non-interactive
tsi compose up -d --bootstrap
```

`tsi` then walks both nodes through the whole exchange, in phases:

1. **Each node:** initialises (node identity, administrator), gets a generated RSA key and certificate, and registers the other node as a partner using that public key.
2. **Handshake:** node-a starts it, and both nodes show the partner as `Active`.
3. **Contract:** node-b (the receiver) defines an asynchronous data contract with PII rules (`applicant_id` hashed, `mobile` masked) and invites node-a.
4. **Accept:** node-a accepts, and the contract is `Active` on both.
5. **Sender:** node-a registers an application authorised for the contract and receives an API key.

`tsi compose test` then sends a real package from node-a and checks that node-b received it with the PII governed as the contract says. Node-a is the sender and node-b the receiver; the checks that only apply to the sender show as skipped on node-b.

Limits specific to dx-node:
- **Two nodes at most**, named `node-a-server-1` and `node-b-server-1` on the Docker network. The image's built-in TLS certificate is valid for exactly those names, and a node checks its peer's certificate against the name it connects to.
- **Asynchronous lane only.** The synchronous request/response lane needs a responder service; it is not set up.
- Every node's administrator password is generated to meet the node's complexity rule and saved in `.tsi/state.yaml`.

## Notes for the module maintainers

Found while building the integrations (none block `tsi`; each has a workaround in the catalog):

- **Schemas are not in the images.** They live only in each repo's `db/` folder, so a database has to be prepared outside the image. `tsi` downloads the pinned files and verifies them.
- **No health endpoint.** `tsi` uses a cheap unauthenticated call per module as its readiness probe.
- **`tsi-compass` API keys cannot authenticate.** Key generation writes `api_key_hash` and `api_secret_hash`, while request authentication reads `key_value` and expects a UUID key id (leftovers from the consent module). The documented `X-API-Key`/`X-API-Secret` access to the admin API does not work in v0.4.3.
- **`tsi-compass` has no git tags.** The v0.4.3 image is the commit `ccabfff`, which is what `tsi` pins.
- **`tsi-dx-node` re-registration is destructive.** Creating a partner that already exists resets it to `Pending`, and creating a contract with an existing name creates a second one. `tsi` looks first and creates only if absent.
- **`tsi-dx-node` TLS certificate names are baked in.** The image's certificate is valid only for `node-a-server-1`, `node-b-server-1`, `localhost` and `tsi-dx-node`, and the hostname-verification override (`jdk.internal.httpclient.disableHostnameVerification`) is set after the HTTP client is created, so it has no effect.
- **`tsi-privacy-vault` returns some errors as HTTP 200** with the real status in the body.
- **Email domains:** `tsi-dx-node` accepts `.local` at setup but rejects it at login; `tsi` defaults to `admin@example.org`.

## Use it from Go

```go
import "github.com/tsicoop/tsi-cli/compose"

stack, err := compose.New(compose.WithDir("."), compose.WithModules("tsi-dpdp-cms"))
if err != nil { /* ... */ }
err = stack.Up(ctx, compose.WithBootstrap()) // render, pull, migrate, start, wait ready, provision
creds, _ := stack.Credentials("tsi-dpdp-cms")
results, _ := stack.Test(ctx)
defer stack.Down(ctx, compose.WithVolumes())
```

In a test (needs Docker; picks free ports and removes everything afterwards):

```go
import "github.com/tsicoop/tsi-cli/testkit"

func TestMyApp(t *testing.T) {
    s := testkit.Start(t, compose.WithModules("tsi-dpdp-cms"))
    c, _ := s.Credentials("tsi-dpdp-cms")
    // point the app under test at c.BaseURL with c.APIKey / c.APISecret
}
```

## Development

```bash
make test          # unit tests with the race detector
make build         # bin/tsi
make network-test  # downloads every pinned migration from GitHub and checks its checksum
make integration   # real Docker: brings up both modules on one Postgres, provisions and tests them (under a minute)
```

## Documentation

| Document | Contents |
|---|---|
| [Technical architecture](prep/TECHNICAL_ARCHITECTURE.md) | Components, flows, state, security model |
| [Implementation plan](prep/IMPLEMENTATION_PLAN.md) | Per-module recipes, milestones, risks, open questions |
| [Use cases](prep/USE_CASES.md) | Who this is for and what they can do with it |

## Limits

- Local and development use first. It is not a production orchestrator, and there is no Kubernetes output in v1.
- `tsi-dpdp-cms` fiduciary domain validation needs real DNS, so it stays pending on localhost.
- `tsi-compass` has a very large surface (tickets, risks, controls, DPIA, vendor portal). `tsi` provisions the first administrator and smoke-tests the basics; it does not seed business data.

## Roadmap

Done: the framework (catalog, secrets, rendering, migrations, readiness, bootstrap, multi-node phases, smoke tests, CLI, `testkit`) and the four soft-launch modules. Next, in no fixed order: release packaging (goreleaser, Homebrew), the dx-node synchronous lane, and the remaining modules. Milestones are in the [implementation plan](prep/IMPLEMENTATION_PLAN.md#8-milestones).

## License & Contributions

This project is distributed under the Apache 2.0 License. See [LICENSE](LICENSE).

To contribute, email a short summary of your change and a link to your branch to admin@tsicoop.org. Direct commits to `main` are restricted, and every contribution is reviewed for fit, readability and long-term maintenance before it is merged.
