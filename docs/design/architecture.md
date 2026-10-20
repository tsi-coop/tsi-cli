# TSI CLI: Architecture

Functional and technical design of `tsi-cli`, in one document. It describes what is built, why it is shaped this way, and where it stops. For how to *use* it, see the [guides](../guides/README.md).

**Status:** the framework and four modules (`tsi-dpdp-cms`, `tsi-privacy-vault`, `tsi-compass`, `tsi-dx-node`) are implemented and verified against the real containers. Everything in this document describes that build unless it says "not built".

| | |
|---|---|
| Repository | `github.com/tsi-coop/tsi-cli` (Go module `github.com/tsi-coop/tsi-cli`) |
| Binary | `tsi`; the first command group is `tsi compose` |
| Language | Go 1.22, about 5,600 lines plus 1,600 lines of tests |
| Runtime dependencies | Docker with the `compose` plugin; internet on first use of each module version |

## Contents

**Part A: Functional design** — [1 Purpose](#1-purpose-and-positioning) · [2 Concepts](#2-concepts) · [3 Use cases](#3-use-cases) · [4 Modules](#4-the-modules) · [5 Behaviour](#5-functional-behaviour) · [6 Guarantees](#6-guarantees-and-quality-targets) · [7 Scope](#7-scope-and-limits)

**Part B: Technical design** — [8 System context](#8-system-context) · [9 Layers and packages](#9-layers-and-packages) · [10 Catalog](#10-the-catalog) · [11 Data and state](#11-data-model-and-state) · [12 Mechanisms](#12-key-mechanisms) · [13 Interfaces](#13-interfaces) · [14 Security](#14-security-model) · [15 Testing](#15-testing-architecture) · [16 Build and dependencies](#16-build-dependencies-and-versioning) · [17 Extending](#17-extending-tsi-cli) · [18 Decisions](#18-design-decisions) · [19 Limitations and roadmap](#19-limitations-and-roadmap)

---

# Part A: Functional design

## 1. Purpose and positioning

### The problem

TSI Coop offers its products as separate services. Each is a Java web application with its own PostgreSQL database and an HTTP API, and none depends on another. That independence is the point: an organisation takes only what it needs (the idea of *composable software*), keeps its existing gateways and identity systems, and adds parts as it grows.

The cost of composability is assembly. For every module, someone must write an environment file with the right secrets, prepare the database from scripts that are not in the image, start the containers, open a browser console, create the first administrator, create an app, generate an API key, and copy a secret that the module shows only once. Each module differs in variable names, setup tokens, response shapes and password rules. Done by hand this takes hours, for every module and every developer, and nothing proves at the end that it works.

### What `tsi` does

`tsi compose` does that assembly from one command, using each product's own API, and then proves the result with smoke tests:

```
tsi compose init       # pick your modules
tsi compose up -d --bootstrap   # start, prepare databases, create admin/app/key
tsi compose test       # check they really work
tsi compose down       # clean up
```

It is also a Go library, so an application's own `go test` can start real modules instead of mocking them.

### Positioning

| | |
|---|---|
| Users | Lead architects, DevOps engineers, backend developers, ISV product teams |
| Nature | A headless, developer-first launcher. It does not proxy or inspect traffic and does not replace gateways or identity systems |
| Value | Zero-friction scaffolding: a working, provisioned, tested module in about a minute |
| **Not** | A production installer. It binds to loopback, generates development-grade settings, and has no TLS, backup, upgrade or high-availability story. Production follows each module's own deployment guide |

## 2. Concepts

| Term | Meaning |
|---|---|
| **Module** | One TSI product, described by a catalog entry (for example `tsi-privacy-vault`) |
| **Instance** | A module run under a name, written `module:instance` (`tsi-dx-node:node-a`). Only some modules support more than one |
| **Key** | The name an entry goes by: the instance name, or the module name. It is the compose service, the state key and the env-file name |
| **Stack** | The set of modules in one project directory, described by `tsi.yaml` |
| **Project directory** | Where `tsi.yaml`, the generated compose file and `.tsi/` live. One directory is one compose project, named after the directory unless `project:` says otherwise |
| **Catalog** | The embedded, declarative description of every module: image, ports, environment, migrations, readiness probe, bootstrap steps, smoke test list |
| **Bootstrap** | The ordered, idempotent list of API calls that takes a started module to a usable one: administrator, tenant app, API key, entities, policy, partner nodes |
| **Smoke test** | A Go check that exercises a bootstrapped module over HTTP with the credentials `tsi` created |
| **State** | `.tsi/state.yaml`: the generated secrets and the one-time credentials the modules cannot return again |
| **Permanent secret** | A secret that protects data at rest; it cannot change once the database holds data |
| **Offset** | A number added to every module's upstream host port (default 300) so a `tsi` stack does not collide with modules run from their repositories |

## 3. Use cases

**An architect or developer evaluating TSI.**
- A working module in minutes: `init`, `up --bootstrap`, `test`. Secrets are generated, the database is prepared, and an administrator login or API key is waiting at the end.
- Take only what you need. The modules do not depend on each other; run one or all.
- Try, then discard: `down --volumes` removes containers, data, credentials and generated files together.

**ISVs and in-house teams building on TSI.**
- Integration tests against the real module instead of mocks. `testkit.Start(t, ...)` starts the module, provisions it, hands back the URL and credentials, and cleans up. Examples: consent recorded, checked and withdrawn; a vault record stored, fetched, searched and erased; a package sent between two exchange nodes and arriving governed.
- The same stack runs in CI, with JUnit or JSON reports.
- Reproducible environments: pinned image tags and checksummed database scripts mean the same stack on every machine.

**DevOps.**
- Generated, reviewable files: a compose file you can read, and per-container env files.
- One place for status and logs across modules.
- Choose where PostgreSQL runs: one shared server, one per module, or an existing one.
- Secrets stay local, with safety rules built in (file modes, permanent keys never silently regenerated, redaction).

**TSI Coop itself.**
- One onboarding path for all products instead of one README each.
- A smoke-test suite per module that can run against every release.
- Integration work exposes gaps early (no health endpoints, schema outside the image, inconsistent names).
- A new module is a new catalog entry, not new Go code, for the common shapes.

**Things this could grow into** (not built): a demo environment that resets with one command; preview stacks per pull request in an ISV's CI; a conformance check an ISV runs to show their product works with TSI modules; an agent interface (see [19](#19-limitations-and-roadmap)).

## 4. The modules

| | `tsi-dpdp-cms` | `tsi-privacy-vault` | `tsi-compass` | `tsi-dx-node` |
|---|---|---|---|---|
| Purpose | Consent management under the DPDP Act | PII vault: tokenization, blind-index search, erasure | IT GRC and compliance operations | Peer-to-peer governed data exchange |
| Version | v0.5.3 | v0.3.0 | v0.4.3 (commit `ccabfff`; the repo has no tags) | v0.3.0 |
| Upstream app / DB port | 8080 / 5432 | 8081 / 5433 | 8085 / 5437 | 8082 / 5434 (a second instance takes +1) |
| Database / role | `tsi_cms` | `tsi_privacy_vault` / `tsi_vault` | `tsi_compass` | `tsi_dx_node_<instance>` |
| Migration files | 13, plus the `app.enc_key` setting | 5 | 22 | 5 |
| Setup call | `bootstrap/setup` `initial_setup`; 201, 409 when done | `admin/setup` `create_user` as `SYSTEM_ADMIN`; 403 when done | `bootstrap/setup` `complete_setup`; 409 when done | `admin/registeradmin`; 201, 409 when done |
| Setup token variable | `TSI_BOOTSTRAP_TOKEN` | `TSI_PRIVACY_VAULT_BOOTSTRAP_TOKEN` | `TSI_BOOTSTRAP_TOKEN` | `TSI_DX_NODE_BOOTSTRAP_TOKEN` |
| Administrator password rule | at least 12 | at least 12 (set by `tsi`) | at least 10 | upper, lower, digit, one of `!@#$%^&*()_+` |
| Session | Bearer | Bearer | Bearer | Bearer |
| Readiness probe | `list_active_fiduciaries` returns 200 | a setup call with a wrong token returns 401 | `check_setup` returns 200 | a setup call with a wrong token returns 401 |
| Credential `tsi` hands back | admin login and an API key/secret | admin login and an API key/secret | admin login (its API keys cannot authenticate) | admin login per node, plus an API key on the sender |
| Permanent secrets | `DB_ENCRYPTION_KEY`, `TSI_LOOKUP_SALT` | master key, index secret | `DB_ENCRYPTION_KEY`, `TSI_LOOKUP_SALT` | the node's private key |
| Smoke tests | 7 | 10 | 6 | 6 per node (2 sender-only) |

All four share a shape: a Java 17 WAR on Jetty (container port 8080), PostgreSQL 15 with its own database, all API calls JSON `POST` with a `_func` field choosing the action, and a first-run flow of one-time administrator setup, login, then tenant credentials. They are pulled from Docker Hub as `tsicoop/<name>:<tag>`. Per-module bootstrap contents and settings are in the [product guides](../guides/products/).

`tsi-ledger` and `tsi-sign` (double-entry ledger; document sealing and eSign) are TSI products but **not part of the soft launch**. The design does not preclude them: ledger uses a cookie session, which the runner already supports, and sign builds from source, which would need a build fallback in the engine.

## 5. Functional behaviour

### 5.1 Commands

| Command | Behaviour |
|---|---|
| `init` | Choose modules and database mode (interactively, or by flags), generate secrets, write `tsi.yaml`, the compose file and `.tsi/`. Never overwrites `tsi.yaml` without `--force`, and never regenerates a secret that exists |
| `up [--bootstrap]` | Preflight, fetch and verify database scripts, pull images, start PostgreSQL, create databases, apply migrations, start modules, wait until ready, optionally bootstrap |
| `bootstrap` | Provisioning only; safe to repeat |
| `status` | Container state, readiness, whether provisioned |
| `creds` | Show saved credentials, masked unless `--show-secret` |
| `test` | Run smoke tests; JUnit, JSON or text; exit 6 on failure |
| `logs` | Container logs with secrets redacted |
| `down [--volumes]` | Stop; with `--volumes` also delete data, state and generated files |
| `modules`, `version` | List the catalog; print the version |

Every command takes `--dir`, `--project`, `--port-offset`, `--non-interactive`, `-o json` and (where meaningful) `--plan`, a dry run that changes nothing. Errors are typed: they name the module and step, give a fix, and map to stable exit codes ([command reference](../guides/commands.md#exit-codes)).

### 5.2 Automatic and manual setup

`up --bootstrap` makes `tsi` do each module's first-run setup, so the module's own setup page reports "already complete". `up` alone starts the modules and leaves setup to the person, using the setup token in the module's env file. A person who sets a module up by hand can still let `tsi` test it by telling `tsi` the administrator password. See [Bootstrap and signing in](../guides/bootstrap-and-signing-in.md).

### 5.3 Idempotence and recovery

Re-running is always safe and never silently changes anything:

- completed bootstrap steps are skipped, and credentials do not change;
- a module that already has an object a previous, interrupted run created is found again instead of being duplicated (by an "already exists" status, by looking first, or by listing and matching);
- migrations are recorded with checksums, so only new files are applied and a changed one is refused;
- permanent secrets cannot change after data exists;
- if `.tsi/state.yaml` is missing but containers exist, `up` refuses rather than generate new keys over existing data.

### 5.4 The exchange scenario (`tsi-dx-node`)

Two nodes are provisioned into a working exchange, in phases across both: each node is initialised and given a generated RSA identity, registers the other as a partner with its public key; the first node starts the handshake and both show the partner `Active`; the second node (the receiver) defines an asynchronous contract with personal-data rules and invites the first (the sender); the sender accepts and registers an application with an API key. The smoke test then sends a real package from sender to receiver and checks it arrives with the contract's governance applied (identifiers hashed, mobile numbers masked, other fields unchanged).

### 5.5 Sign-in and credentials

The credential a user is handed depends on how the module is driven. Modules whose tenant API works with a key return an API key and secret (consent, vault, and the exchange sender). Modules driven by an administrator session return the administrator login. Secrets the module shows once are saved before the next call is made.

## 6. Guarantees and quality targets

| Guarantee | How it is met |
|---|---|
| Never lose a one-time secret | Saved to `.tsi/state.yaml` (atomic write, mode 0600) immediately, before the next API call |
| Never regenerate a permanent secret | Locked after the first migration; overrides refused; node identities never replaced; the consent module's key step is checksummed |
| Fail early, change nothing | Preflight checks, port checks and database-script download and verification all run before any container is created |
| Secrets stay on the machine | No telemetry; files 0600 in a 0700 directory; `.tsi/` added to `.gitignore`; output redacted |
| Nothing exposed beyond the host | Ports published on `127.0.0.1` by default |
| Reproducible | Image tags pinned in the catalog and recorded as digests in `tsi.lock`; database scripts pinned to a ref and a SHA-256 per file |
| Clear failures | Errors name module and step and suggest a fix; stable exit codes |

| Target | Status |
|---|---|
| `init` under a minute with defaults | Met (non-interactive, secrets generated) |
| Up, provisioned and tested in about a minute with images present | Met: one module in about 17 s, two in about 22 s, a dx-node pair in about 27 s, all four modules in about 70 s including tests |
| Re-run changes nothing | Met and tested |

## 7. Scope and limits

**In scope:** a catalog of modules; config and secret generation; manifest rendering; lifecycle; readiness; provisioning; smoke tests; the CLI; the Go library and `testkit`.

**Out of scope:** Kubernetes output; remote or production deployment; telemetry; changing the modules; upgrading a database that holds data; `tsi-ledger` and `tsi-sign`; the dx-node synchronous lane; natural-language or agent provisioning. Limits specific to a module are in its [product guide](../guides/products/).

### How the original concept changed

The first concept (a Word specification) was written before the module repositories were read. Building against the real code changed these things:

| Original idea | What is true and what was built |
|---|---|
| Five modules | Six TSI products exist; four are in the soft launch |
| One port per module (8081 to 8085) and a shared `.env.tsi` | The container port is always 8080; host ports differ per repo (and are offset). Modules reuse variable names, so there is one env file per container under `.tsi/env/` |
| Images on `ghcr.io/tsicoop/...`, tag `latest` | Docker Hub `tsicoop/<name>`, pinned tags |
| Vault needs Redis; vault gates on consent; sign is stateless | PostgreSQL only; no cross-module dependencies; sign has its own database |
| A `/health` endpoint on every module | None exists; readiness is a cheap unauthenticated call per module |
| "Resolve inter-service dependencies" | There are none, so there is nothing to resolve |
| dx-node needs input mTLS certificates | The image ships a dev transport certificate; `tsi` generates the node identities and registers them through the admin API |
| Schema ships in the image | It does not; it is in each repo's `db/` folder. `tsi` fetches it ([12.3](#123-database-scripts)) |
| Compose `version: '3.8'` | Obsolete in the compose specification; omitted |
| `init` writes the files; `up` just starts | `up` also prepares databases, waits for readiness and (optionally) provisions; the value is the provisioning and the proof |

---

# Part B: Technical design

## 8. System context

```
                         ┌──────────────────────────────────┐
  Developer / CI /  ─────►  tsi (CLI)   or   Go library user  │
  go test                └────────────────┬─────────────────┘
                                          │ in-process calls
                         ┌────────────────▼─────────────────┐
                         │          tsi-cli (this repo)       │
                         └───┬────────────┬─────────────┬────┘
            docker compose   │   pgx      │  HTTPS GET  │  HTTP/JSON POST
                         ┌───▼──┐     ┌───▼────┐   ┌────▼─────┐   ┌──────────────────────────┐
                         │Docker│     │Postgres│   │ GitHub   │   │ TSI modules (Jetty :8080) │
                         │engine│     │ shared,│   │ raw files│   │ cms, vault, compass,      │
                         │      │     │per-mod,│   │ (db      │   │ dx-node ×2                │
                         │      │     │external│   │ scripts) │   │                           │
                         └──────┘     └────────┘   └──────────┘   └──────────────────────────┘
   Local disk: tsi.yaml · docker-compose.tsi.yml · tsi.lock · .tsi/{state.yaml, env/, certs/}
   Cache: ~/.cache/tsi-cli/migrations
```

External dependencies: a Docker-compatible engine (v1 shells out to `docker compose`), Docker Hub images, GitHub raw file hosting for database scripts, and a PostgreSQL 15 server (a container, or the user's own).

## 9. Layers and packages

```
┌────────────────────────────────────────────────────────────────────┐
│ L5  Interface     cmd/tsi (cobra)                                    │
├────────────────────────────────────────────────────────────────────┤
│ L4  Orchestration compose/   Stack: Plan, Write, Up, Bootstrap, Test │
│                              Status, Logs, Down;  testkit/           │
├────────────────────────────────────────────────────────────────────┤
│ L3  Domain        bootstrap/  tsitest/  health/  migrate/  render/   │
│                   certs/                                             │
├────────────────────────────────────────────────────────────────────┤
│ L2  Model         catalog/  config/  secrets/  tsierr/               │
├────────────────────────────────────────────────────────────────────┤
│ L1  Infrastructure engine/ (docker compose | fake)  ·  pgx  ·        │
│                   net/http + cookiejar  ·  filesystem  · internal/redact │
└────────────────────────────────────────────────────────────────────┘
Dependencies point downward. cmd/tsi holds no logic beyond flag parsing and printing.
```

| Package | Responsibility |
|---|---|
| `catalog` | Load and validate the embedded per-module YAML (strict: unknown keys are errors) |
| `config` | `tsi.yaml`: typed config, defaults (port offset), validation, `module:instance` parsing |
| `secrets` | Generators (`random32`, `hex16`, `token`, `password`, `strong`), the state store (atomic, locked, 0600), immutable-secret rules |
| `render` | Resolve each module against config and state (ports, hostnames, databases, environment); build the compose file and env files. Deterministic; golden-tested |
| `engine` | `Engine` interface and the `docker compose` implementation; a fake for tests |
| `migrate` | Create databases and roles; fetch, verify and apply SQL over pgx; the migration ledger |
| `health` | Readiness probing with backoff |
| `bootstrap` | The step runner: templating, sessions, idempotence, phases, peers |
| `tsitest` | Smoke suites per module; reporters (text, JSON, JUnit); skip support |
| `certs` | PKCS#12 keystores; node identities (PEM) |
| `compose` | The public façade tying the above into `Stack` |
| `testkit` | `Start(t, ...)` for an application's `go test` |
| `tsierr` | Typed errors and exit codes |
| `internal/redact` | Masks known secrets in every output path |

Library packages are at the top level, not under `internal/`, so an application can import `compose`, `testkit` and `tsitest`.

## 10. The catalog

A module is a YAML file in `catalog/modules/`, validated at load. Adding a module is adding a file, not Go code, for the common shapes. An abridged entry:

```yaml
name: tsi-privacy-vault
image: {repo: tsicoop/tsi-privacy-vault, tag: v0.3.0}      # pinned, never "latest"
container_port: 8080
ports: {app: 8081, db: 5433}                                # upstream host ports
db: {name: tsi_privacy_vault, user: tsi_vault}
session: bearer                                             # bearer | cookie
credentials: apikey                                         # apikey (default) | admin
admin_password: strong                                      # generator, if the module has a policy
identity: {p2p_port: 8443, max_instances: 2, hostnames: [...]}   # modules whose instances talk
env:
  - {name: TSI_PRIVACY_VAULT_MASTER_KEY, generator: random32, secret: true, immutable: true}
  - {name: POSTGRES_HOST, value: "jdbc:postgresql://{{.DBHost}}:{{.DBPort}}"}
  - {name: TSI_PRIVACY_VAULT_ENV, default: local}
migrations:
  source: {repo: tsi-coop/tsi-privacy-vault, ref: v0.3.0, dir: db}
  steps:
    - {file: 00_init.sql, sha256: c6fb1618...}
    - {action: set_database_setting, setting: app.enc_key, from_env: DB_ENCRYPTION_KEY}   # cms only
ready: {path: ..., body: {...}, expect: [401]}
spec_defaults: {...}          # per-module bootstrap settings, overridable in tsi.yaml
spec_replace: [policy.content]   # maps a user replaces wholesale instead of merging
bootstrap: [ ...steps... ]
smoke: [ {id: ..., description: ...} ]
```

Validation rules include: a pinned (non-`latest`) tag; every migration file has a 64-hex SHA-256 and a plain file name; a migration source needs `owner/repo`, a ref that is not a branch name, and a directory; `session` and `credentials` take only listed values; bootstrap step ids are unique.

### Bootstrap step vocabulary

A step is one API call. Its fields are the vocabulary that lets YAML express every recipe so far:

| Field | Meaning |
|---|---|
| `path`, `body`, `auth` | The `POST`, a body whose string leaves are templates, and `bearer` to send the admin token |
| `expect`, `already_done`, `already_done_body` | Success statuses; statuses (and response text, such as `duplicate key`) that mean "done before" |
| `capture`, `secret` | Variables taken from the response by dotted path; which of them are secrets (redacted, saved) |
| `volatile` | Run every time and do not save (login; look-up steps) |
| `when`, `unless` | Skip unless / if a template path is truthy (an empty list is false) |
| `foreach` | Repeat for every item of a list (`Spec.entities`, `Peers`); each item has its own saved state id |
| `select` (`match`, `capture`, `from`, `optional`) | Take fields from the item of a list response that matches |
| `recover` | On an "already done" status, look the existing object up and capture its ids |
| `phase` | Run after every instance has finished earlier phases |
| `poll` | Repeat a request every two seconds, up to N seconds, until a `select` finds its item |

Template context: `.Admin` (name, email, password), `.Env` (the module's resolved environment), `.Spec` (the merged settings), `.Vars` (captured so far), `.Item` (current `foreach` item), and for modules with an identity `.Identity`, `.Node` and `.Peers`. A string that is exactly one `{{.Path}}` keeps the value's type (list, map, number, boolean); anything else is text-templated and a missing key is an error.

## 11. Data model and state

### Files in a project directory

```
<project>/
├── tsi.yaml                  intent; no secrets (commit)
├── docker-compose.tsi.yml    generated; no secrets
├── tsi.lock                  image digests after pulling
├── .gitignore                ".tsi/" appended
└── .tsi/                     0700, git-ignored
    ├── state.yaml            0600: the only copy of what modules cannot return
    ├── env/<key>.env         0600: one env file per container
    ├── env/postgres.env      superuser password (shared mode)
    ├── certs/<key>.p12       keystore (consent module)
    ├── certs/<key>.pem|.key  node identity (exchange module); key is 0600
    └── lock                  advisory file lock
```

### `tsi.yaml`

Version, project name, modules (with instances), database mode and port, port offset, bind address, administrator identity, environment overrides, bootstrap settings, local migration directories. Decoding is strict, and defaults are applied before the file is read, so a missing `port_offset` means the default offset while an explicit `0` is honoured and always written. Full reference: [Configuration](../guides/configuration.md).

### `state.yaml`

```
version: 1
db_admin_password: ...                       shared mode
modules:
  <key>:
    secrets: {NAME: value}                   generated; permanent ones flagged in the catalog
    admin_password: ...
    db_password: ...
    data_initialized: true                   set after migrations; locks permanent secrets
    steps: {<step id>: {done, vars}}         bootstrap progress and what each step returned
    credentials: {base_url, api_key, api_secret, admin_email, admin_password, extra}
```

Writes go to a temporary file in the same directory, are synced, and renamed over the old file under an exclusive `flock`, so a crash cannot leave half a file and two `tsi` processes cannot interleave. Registering the file's secrets with the redactor happens on every load and save.

### Secret classes

| Class | Examples | Rule |
|---|---|---|
| Generated, replaceable | JWT secrets, setup tokens, database passwords | Edit in `state.yaml` and `up` to rotate |
| **Permanent** | database encryption key, lookup salt, vault master key and index secret, node private key | Locked after the first migration; refused afterwards |
| One-time upstream | tenant API secrets, administrator passwords | Saved on receipt; unrecoverable from the module |

### Database scripts and the migration ledger

Each applied script (and the `app.enc_key` setting) is recorded in `_tsi_cli_migrations (id, checksum, applied_at)` in the module's own database. A rerun skips recorded ids; a recorded id whose content hash differs is an error.

## 12. Key mechanisms

### 12.1 Lifecycle

```mermaid
sequenceDiagram
    participant U as User / CI
    participant S as compose.Stack
    participant R as render
    participant F as migrate.Fetcher
    participant E as Engine (docker compose)
    participant M as migrate (pgx)
    participant H as health
    participant B as bootstrap
    participant API as TSI module APIs
    U->>S: Up(WithBootstrap)
    S->>E: Preflight (docker, compose plugin); PS (state-lost guard)
    S->>R: Resolve (generate missing secrets) and write files
    S->>S: save state; check host ports free
    S->>F: Prefetch every migration file (download, verify SHA-256, cache)
    S->>E: pull images; up PostgreSQL (wait healthy)
    S->>M: create role + database; apply migrations
    S->>E: up modules
    S->>H: wait ready (backoff, per-module probe)
    S->>B: phases across all instances
    B->>API: setup, login, create tenant objects
    B->>S: save credentials after each step
```

Order matters. PostgreSQL starts first and migrations run before the application starts, because the schema is not in the images. The state is saved before containers use any generated secret.

### 12.2 Rendering

`render.Resolve` turns config, catalog and state into a layout: for each module its key and index, host ports (upstream port plus index plus offset), hostname, database name and role (instance names are suffixed), database host and port as seen from the container, and the ordered environment. For each environment variable it uses, in order, a generator (value kept in state), a template over `DBHost`, `DBPort`, `DBName`, `DBUser`, `DBPass`, `Origin`, or a default; then applies the user's override (refusing permanent ones after data exists). The compose file is built from typed structs and `yaml.Marshal`, which cannot produce invalid YAML; its content never includes a secret, only paths to env files. `$` in env values is doubled because compose interpolates env files.

### 12.3 Database scripts

The module images do not carry their schema; the repositories mount `db/` into PostgreSQL's `initdb.d`, which only runs on an empty data directory and cannot serve an external server. `tsi` therefore applies the scripts itself over pgx (simple query protocol, so multi-statement files and dollar-quoting work), and it does so without copying them into this repository:

1. The catalog pins `source` (repo, ref, dir) and a SHA-256 per file; branch names are refused as refs.
2. `migrate.Fetcher` downloads `https://raw.githubusercontent.com/<repo>/<ref>/<dir>/<file>`, retrying server errors but not 404, verifies the hash, and caches the verified bytes (atomic write; a cache entry that no longer matches is fetched again). A mismatch is an error and nothing is cached or run.
3. `Up` prefetches every file before it pulls an image, so network and checksum problems leave nothing behind.
4. `migration_dirs` in `tsi.yaml` reads a local directory instead (unverified, with a warning when it differs), for offline work and module development.
5. A role and database per module are created with the administrator connection; SQL files run as the module's own role. The one action needing a superuser, `ALTER DATABASE ... SET app.enc_key` (the consent module's migrations read it), runs over the administrator connection, and the connection is reopened so later files see it.

`make network-test`, run in CI, downloads every pinned file and checks it. `hack/sums.sh` prints the catalog lines for a new ref.

### 12.4 Database modes and ports

| Mode | Servers | Notes |
|---|---|---|
| `shared` (default) | one container | a database and role per module; one failure domain |
| `per-module` | one container per module/instance | like the upstream compose files |
| `external` | the user's server | administrator DSN must be a superuser; containers reach it as `host.docker.internal` (or `module_host`); the server must listen beyond loopback |

The migration administrator connection is the shared server's superuser, the module's own role in per-module mode (it created its database at container start and is a superuser there), or the user's DSN. Ports are published on `bind` (loopback by default) as `upstream + instance index + offset`; `Up` tries to listen on each before creating anything and names the module when one is taken.

### 12.5 Readiness

No module has a health endpoint. Each catalog entry names a cheap unauthenticated call that proves the application is serving: a public status call that reads the database (consent, Compass), or a setup call with a wrong token that is refused with 401 before any database access and so writes nothing to an audit log (vault, exchange node; the vault validates the body first, so the probe must be well formed). Polling uses exponential backoff up to a limit; the database is already proven by the migrations that ran earlier. Readiness is separate from "setup done".

### 12.6 The bootstrap runner

`compose.bootstrap` builds one `Runner` per module or instance, gives modules with an identity their `Extra` context, then runs phase 1 on every runner, then phase 2, and so on. For each step the runner:

1. evaluates `when`/`unless`; skips a step already recorded as done (restoring its captured variables) unless it is `volatile`;
2. expands `foreach` into per-item instances, each with its own saved state id (`create_partner#0`);
3. renders the body, makes the call, and takes the **effective status**: the HTTP status, unless a 2xx reply carries a higher `status` in its JSON body (the vault does this);
4. on an "already done" status runs `recover`, if any, to learn the existing object's ids;
5. on an unexpected status stops with a typed error (a login failure carries a hint about the saved password);
6. captures variables, registers secrets with the redactor, records the step and **saves state before continuing**.

Sessions: Bearer (the token captured by the login step) for all four modules; the runner also supports a cookie jar for modules that use one. Smoke tests that need administrator fixtures call `Runner.AdminCall`, which logs in once and reuses the token (some modules rate-limit sign-ins) and retries once on 401.

### 12.7 Multiple instances and node identity

`module:instance` entries make several runs of one module: the instance name is the key; ports are `base + index`; each instance has its own database, role and secrets; and a catalog `identity` block (`p2p_port`, `max_instances`, `hostnames`) enables the peer features.

For the exchange node, `certs.LoadOrCreateIdentity` creates an RSA-2048 key (PKCS#8) and a self-signed certificate per instance and never replaces them, because peers pin the public key. The runner's context then provides `.Identity` (this node's certificate and key), `.Node` (id, hostname, port, index, `sender`, `receiver`) and `.Peers` (the other instances: id, address, certificate, and the variables their runners have captured). Roles follow the order: the first instance is the sender and the second the receiver.

Two constraints from the image shape this. The image's TLS certificate is valid only for `node-a-server-1`, `node-b-server-1`, `localhost` and `tsi-dx-node`, and a node verifies its peer's name when it connects (the code that would disable this is applied too late to take effect). So the catalog lists those two names as `hostnames`, and the compose file gives each instance the matching network alias. And the node's create calls are not idempotent (re-registering a partner resets it to `Pending`; a second contract of the same name is created), so the recipe looks first with volatile look-up steps and creates only `unless` the object exists.

### 12.8 Smoke tests

Suites are Go code registered per module in `tsitest`, each a list of ordered checks sharing state. A check returns nil, an error, or `Skipf(...)` for "not applicable to this instance" (a sender-only check on the receiver), which counts separately from passes and failures. `Env` gives a check the base URL, the saved credentials, the rendered bootstrap settings, an admin call, a readiness call, its index and its `Peers`, so a sender-side check can reach the receiver. The catalog's `smoke` list is documentation, and a unit test fails if it disagrees with the registered suite. Checks clean up what they create and use the HTTP status the module really sends (for example a refused scope is 401, not 403, on the consent module).

### 12.9 Error model

`tsierr.Error{Code, Module, Step, Msg, Hint, Cause}` renders as `[step] module: message: cause` plus a `fix:` line. Codes map to exit statuses 1 to 10 (internal, usage, preflight, not ready, bootstrap, test failed, permanent secret, state lost, not found, migration). All output passes through the redactor, which holds every secret loaded from or saved to state.

## 13. Interfaces

### Library

```go
s, err := compose.New(compose.WithDir("."), compose.WithModules("tsi-dpdp-cms"), compose.WithPortOffset(0),
                      compose.WithEngine(e), compose.WithMigrationSource(src), compose.WithOutput(w))
plan, _ := s.Plan()                              // no side effects
err = s.Up(ctx, compose.WithBootstrap())         // also: Write, WaitReady, Bootstrap
creds, ok := s.Credentials("tsi-dpdp-cms")       // by key
results, err := s.Test(ctx, compose.OnlyModules("node-a"))
rep, err := s.Status(ctx)
err = s.Logs(ctx, w, engine.LogOptions{Tail: 50})
err = s.Down(ctx, compose.WithVolumes())
```

Every method takes a `context.Context` and honours cancellation (child `docker` processes are killed). Mutating methods take the state lock, and are safe to call twice. `testkit.Start(t, opts...)` wraps this for tests: temporary directory, unique project, a free port range, `Up` with bootstrap, and a cleanup that deletes everything.

### Engine

```go
type Engine interface {
    Preflight(ctx) error
    Pull(ctx, Project, services...) error
    Up(ctx, Project, wait bool, services...) error
    Down(ctx, Project, removeVolumes bool) error
    PS(ctx, Project) ([]Container, error)
    Logs(ctx, Project, w, LogOptions, services...) error
    Digest(ctx, image) (string, error)
}
```

`Docker` shells out to `docker compose -p <project> [-f file]` (omitting `-f` before the first file exists, so `ps` can still tell whether containers exist). `ProjectDirs` reads the working directory Docker recorded on a project's containers; `Up` uses it to refuse a project name that already belongs to another directory, before anything is written or started. `Fake` records calls for unit tests. A Docker SDK engine could implement the same interface without changing any upper layer.

### CLI

A thin cobra tree: `tsi version` and `tsi compose {modules, init, up, bootstrap, status, creds, test, logs, down}`. Output has a human form and `-o json`; progress goes to standard error and results to standard output.

## 14. Security model

| Concern | Measure |
|---|---|
| Trust boundary | The machine running `tsi`. No telemetry; the only outbound calls are image pulls and the database-script download |
| At rest | `.tsi/` is 0700; state, env files and private keys 0600; `.gitignore` updated. The compose file holds no secret |
| In output | Central redactor over logs, errors, Docker output and `creds` (masked without `--show-secret`) |
| In transit | Loopback only by default; no TLS between the CLI and local modules |
| Supply chain | Image tags pinned and digests recorded; database scripts pinned to a ref and verified by SHA-256 before use; branch refs refused |
| Destructive actions | `down --volumes` needs the project name typed or `--yes`; permanent secrets are never replaced; a missing state file stops `up` |
| Open first-run | Some modules accept a first administrator from whoever reaches the port; ports are on loopback and every module in the catalog also requires a setup token that `tsi` generates |
| Limits | The generated administrator passwords and development settings are not suitable for production; `tsi` is not a hardening tool |

## 15. Testing architecture

| Level | What | How |
|---|---|---|
| Unit | catalog validation, config rules and port defaults, secret generators and immutability, state store (modes, locking, corrupt file), renderer output, env escaping, bootstrap runner (idempotence, phases, foreach, recover, poll, select-from, conditions, effective status), fetcher (cache, checksum, retry, local override), certs, reporters, smoke-list consistency | `go test -race`; an `httptest` fake module; a fake engine; fake migration source; golden files for compose output (`go test ./render -update`) |
| Network | every pinned database script matches upstream | `make network-test` (build tag `network`), in CI |
| Integration | all four modules, dx-node as a pair, on one PostgreSQL: bring up, bootstrap, run every smoke suite, bootstrap again and expect identical credentials | `make integration` (build tag `integration`, real Docker, about 70 s) |

Not done: validating the generated compose file with `compose-go` (golden files and real `docker compose` runs cover it), and contract checks of the catalog's calls against each module's request schemas in CI.

## 16. Build, dependencies and versioning

| Dependency | Use |
|---|---|
| `spf13/cobra` | CLI |
| `jackc/pgx/v5` | create databases, apply migrations |
| `gopkg.in/yaml.v3` | catalog, config, state, compose output |
| `software.sslmate.com/src/go-pkcs12` | writing PKCS#12 keystores (the standard library cannot) |
| standard library | `crypto/x509`, `embed`, `net/http`, `net/http/cookiejar`, `text/template`, `syscall.Flock` |

The Makefile builds with `-ldflags "-X main.version=$(git describe --tags --always --dirty)"`, so an untagged build reports its commit hash. CI runs format, vet, unit tests, build and the network test, then the integration test. Distribution: build from source and `go install` today; Homebrew and release binaries are planned.

Reproducibility: image tags and migration refs are fixed in the catalog, so a given `tsi` build always sets up the same module versions. A module upgrade is a catalog change (new tag, new checksums), not a user setting.

## 17. Extending `tsi-cli`

### Adding a module

1. Read the module's repository and image: routes (the routing table inside the WAR), setup and login calls and their status codes, credential response fields, password rules, environment variables, the migration files.
2. Write `catalog/modules/<name>.yaml`: image, ports, database, environment (mark permanent secrets), migration source with checksums (`hack/sums.sh <checkout> <ref> db`), readiness probe, settings, bootstrap steps.
3. Add the smoke suite in `tsitest/<name>.go` and the matching `smoke:` list.
4. Run it for real. The integration findings so far (a response shape, a status code the module sends, an order that matters, a call that is not idempotent) were all found this way, not from reading code.
5. Add a render golden, extend the integration test, write the product guide.

When a flow does not fit the step vocabulary, extend the vocabulary (a step field with a unit test) rather than adding a module-specific Go branch; every field in [section 10](#bootstrap-step-vocabulary) arrived that way.

### Other extension points

| To add | Touch |
|---|---|
| Another container runtime | A new `Engine` implementation |
| Another output (for example Kubernetes manifests) | A new renderer over the same layout; catalog and bootstrap unchanged |
| Another command group | A new package and cobra subcommand over the library |
| An agent interface | A server over the `compose` façade with an operation allow-list, plan-then-confirm and secrets returned as handles |

## 18. Design decisions

| Decision | Why | Alternatives rejected |
|---|---|---|
| Library first, thin CLI | Everything the CLI does is available to `go test` and CI | Logic in the CLI |
| Catalog as data | Modules differ in names and shapes; YAML absorbs that without code | A Go type per module |
| Drive modules through their own APIs | No changes to the modules, no direct writes to their data | Seeding the database |
| Apply database scripts with `tsi` over pgx | The schema is not in the images; one code path for container, shared and external servers; no wait for container restarts | `initdb.d` mounts (empty data directory only, no external server) |
| Fetch database scripts, verify by SHA-256, cache | No redundant SQL in this repository; a moved tag or tampered download cannot run | Vendoring copies (drift, maintenance); trusting the network |
| Shell out to `docker compose` | Minimal and faithful to how the modules are run by hand | The Docker SDK (heavier, same behaviour) |
| Compose file from typed structs | Cannot emit invalid YAML; deterministic for golden tests | `text/template` over YAML |
| One shared PostgreSQL by default | Fewer containers, one place for the data | One per module (still available) |
| Per-container env files, no shared `.env` | Modules reuse variable names with different values | One `.env.tsi` |
| State file is the single source of truth for unrecoverable data | One-time secrets exist nowhere else | Re-deriving or regenerating |
| Save before the next call | A crash cannot lose a secret the module shows once | Save at the end |
| Look-up-then-create where creates are not idempotent | Avoids duplicates and resets after lost state | Trusting a 409 |
| Port offset 300 by default; `0` writes explicitly | A `tsi` stack never collides with a module run from its repository | Upstream ports |
| Loopback binding | Several modules accept a first caller during setup | Publishing on all interfaces |
| Compose project named after the directory, and refused if another directory owns it | The container, network and volume names come from the project, so a shared name silently shares data and PostgreSQL passwords | One fixed default name (it was `tsi` for everyone, and a second directory recreated the first one's database container) |
| Soft launch of four modules | Verify the framework against the harder cases (cookie-free admin sessions, two-node exchange) first | Six modules at once |

## 19. Limitations and roadmap

**Known limitations**
- Local, development and CI use only.
- `tsi-dx-node`: at most two nodes, under fixed names; asynchronous lane only (the synchronous lane needs a responder service).
- `tsi-compass`: only the first administrator and a smoke check; no business data. Its generated API keys do not authenticate in v0.4.3.
- Consent module: fiduciary domain validation needs real DNS, so it stays pending locally; the keystore is generated but the module reads it only in production mode.
- Vault: AWS KMS and S3 storage are not set up; the key lives in the container environment.
- `tsi` is not an upgrade tool: it applies new database scripts, but does not migrate a database that holds data across module versions.
- First use of each module version needs internet access (or `migration_dirs`).
- An external PostgreSQL must be a superuser connection and reachable from containers.

**Risks**

| Risk | Mitigation |
|---|---|
| A module's API changes between versions | Pinned tags; the network test; the integration test catches a broken recipe |
| Upstream database scripts change under a tag | Checksums refuse them; the network test reports it |
| Losing one-time credentials or permanent keys | State file, refusal to regenerate, clear recovery messages |
| Port clashes on a developer machine | Default offset, preflight port check, `--plan` |
| Secrets leaking into logs or git | File modes, `.gitignore`, redaction in every output path |

**Roadmap** (no fixed order): release packaging (goreleaser, Homebrew); the dx-node synchronous lane; the remaining modules (`tsi-ledger` has a cookie session, which the runner supports; `tsi-sign` builds from source and needs an engine build fallback); contract checks against module request schemas in CI; a command that prints a setup token; an agent interface over the library with the safety rules above; and, if the module maintainers agree, module changes that would simplify `tsi` (the schema inside the image, health endpoints).
