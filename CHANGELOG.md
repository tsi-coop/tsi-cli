# Changelog

## Unreleased

### Changed
- **A port conflict now names what holds the port.** When a host port is already in use by a Docker container, the error shows the container, its compose project and the directory it was started from, and suggests `docker stop` or `tsi compose down` there, instead of only saying the port is in use.

## v0.1.1

### Fixed
- **Two project directories no longer share a compose project.** The default project name was `tsi` for every directory, so a second directory reused the first one's containers and PostgreSQL volume and failed with a password error (it also recreated the first stack's PostgreSQL container). `init` now derives the project from the directory name, `up` refuses a project that belongs to another directory (before changing anything), and a rejected database password fails at once with an explanation, instead of retrying for a minute, and stops what that attempt started.

## v0.1.0 (2026-10-20): soft launch

First release. `tsi compose` takes a developer from nothing to a running, provisioned and tested stack of TSI modules with one command, for local development, evaluation and CI. It is not a production installer. Design: [architecture](docs/design/architecture.md). Usage: [guides](docs/guides/README.md).

### Modules

Four modules, each pinned to a version and verified against the real container:

| Module | Version | What `tsi` sets up | Smoke tests |
|---|---|---|---|
| `tsi-dpdp-cms` | v0.5.3 | administrator, data fiduciary, app, API key, and a published consent policy | 7 |
| `tsi-privacy-vault` | v0.3.0 | `SYSTEM_ADMIN`, entities (ID, data, file), a client key with permissions | 10 |
| `tsi-compass` | v0.4.3 | administrator (its generated API keys cannot authenticate in this version) | 6 |
| `tsi-dx-node` | v0.3.0 | two nodes: identities, partner handshake, a data contract, and a sender application with an API key | 6 per node |

### Added

- **`tsi compose`** with `init`, `up`, `bootstrap`, `status`, `creds`, `test`, `logs`, `down` and `modules`; `--plan` dry runs, `-o json`, and stable exit codes with a fix hint on every error.
- **One-command setup.** `up --bootstrap` generates secrets, prepares databases, starts the modules, waits until each is ready and provisions it through the module's own API. A first run takes under a minute with the images present.
- **Re-runnable and recoverable.** Completed steps are skipped, one-time secrets are saved before the next call, and objects left by an interrupted run are found again instead of duplicated.
- **Database modes:** one shared PostgreSQL server (a database and role per module), one server per module, or your own external server. New projects shift every port by 300 so they never collide with modules run from their repositories (`port_offset`).
- **Database scripts fetched, not copied.** Each module pins a repository ref and a SHA-256 per file. Files are downloaded on first use, verified, cached, and applied in order with a migration ledger; `migration_dirs` uses a local checkout for offline work.
- **Two-node data exchange.** `tsi-dx-node:node-a` and `tsi-dx-node:node-b` run as a pair with generated node identities, an automated handshake and contract, and a smoke test that sends a real package and checks the receiver got it with personal data hashed and masked.
- **Smoke tests** for every module, with text, JSON and JUnit reports.
- **Safety rules.** Permanent keys (encryption keys, vault master key, node private keys) are locked once data exists and never regenerated; secrets live in `.tsi/` with restrictive file modes and are git-ignored; output is redacted; a missing state file stops `up` rather than generate new keys over existing data; ports bind to `127.0.0.1`.
- **Go library.** `compose` for programmatic control and `testkit.Start(t, ...)` to run real modules from `go test`, with automatic cleanup.
- **Configuration** in `tsi.yaml`: modules and named instances, environment overrides, per-module bootstrap settings, database mode, port offset.
- **Documentation:** a guide set (getting started, bootstrap and signing in, configuration, databases and ports, secrets and state, commands, testing and CI, troubleshooting) with a page per module, and a combined functional and technical architecture document.
- **Tests and CI:** unit tests with the race detector, a network test that checks every pinned database script against upstream, and an end-to-end test that runs all four modules on one PostgreSQL. `Makefile` targets `build`, `test`, `network-test`, `integration`; a GitHub Actions workflow.

### Known limits

- Local, development and CI use only; no Kubernetes output, TLS, backup or upgrade handling.
- `tsi-dx-node`: at most two nodes, under fixed names, asynchronous exchange only.
- `tsi-compass`: first administrator and a smoke check only; no business data is seeded.
- `tsi-dpdp-cms`: fiduciary domain validation stays pending on localhost (it needs real DNS).
- `tsi-privacy-vault`: runs with the local key anchor and database file storage; AWS KMS and S3 are not set up.
- The first use of each module version needs internet access, unless `migration_dirs` is set.
- Not included in this release: `tsi-ledger` and `tsi-sign`, release packaging (Homebrew, binaries), and any agent interface.
