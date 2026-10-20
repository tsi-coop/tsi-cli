# TSI CLI

A CLI to pick, configure, and launch the tsi modules your stack needs.

> **Status: soft launch.** Four modules are implemented and tested end to end against the real containers: `tsi-dpdp-cms`, `tsi-privacy-vault`, `tsi-compass` and `tsi-dx-node`. See [Modules](#modules).

## Why TSI CLI?

Each TSI module (consent, privacy vault, compliance, data exchange) is set up by hand today: write an env file, mount its `db/` folder into Postgres, start it, open a browser console, create the first administrator, create an app, generate an API key, and copy a secret that is shown only once. Every module has slightly different steps, setup tokens and variable names.

`tsi` does all of that from one command, and then proves it works.

## Who this is for

`tsi` is for **local development, evaluation and CI**: trying a module in minutes, and giving an ISV's `go test` runs a real module to talk to. It binds ports to `127.0.0.1`, generates secrets for you, and has no TLS, backup, upgrade or high-availability story. **It is not a production installer.** For production, follow the deployment guide in each module's own repository.

## Key Features

- **Pick only what you need.** The modules are independent. Run one, or all, on one shared PostgreSQL server.
- **Secrets generated for you.** JWT secrets, setup tokens, encryption keys and keystores are created with real values, never the insecure defaults the compose files ship with.
- **Provisioned through the modules' own APIs.** Administrator, tenant app, API key and the rest of each module's first-run setup are done for you and saved locally (file mode 0600).
- **Smoke tests included.** `tsi compose test` checks each module with the credentials it just created, including a real package sent between two data-exchange nodes.
- **Safe to re-run.** Bootstrap is idempotent. Permanent keys are never silently regenerated.
- **Also a Go library.** Use `testkit.Start(t, ...)` in your own `go test` runs to test against real modules instead of mocks.
- **Local first.** No telemetry. Everything lives in `.tsi/` next to your project.
- **Nothing vendored.** The database scripts are downloaded from each module's own repository at a pinned release and checked against a SHA-256 before they run.

## Quick Start

You need Docker and a terminal.

1. Install `tsi`:
   ```bash
   git clone https://github.com/tsi-coop/tsi-cli.git && cd tsi-cli
   make build && sudo mv bin/tsi /usr/local/bin/
   ```
2. Create a working directory and choose your modules:
   ```bash
   mkdir my-tsi-stack && cd my-tsi-stack
   tsi compose init                      # or: tsi compose init --modules tsi-dpdp-cms --non-interactive
   ```
3. Start the stack and provision it:
   ```bash
   tsi compose up -d --bootstrap
   ```
4. Check that everything works, and find how to sign in:
   ```bash
   tsi compose test
   tsi compose status
   tsi compose creds --show-secret
   ```
5. When you are done:
   ```bash
   tsi compose down --volumes --yes      # without --volumes, data and credentials are kept
   ```

You need Docker 24+ with the `docker compose` plugin, and internet access the first time (the images and the database scripts are downloaded). New projects use ports 8380 and up for the apps and 5732 for PostgreSQL. More in the [Getting started guide](docs/guides/getting-started.md).

## Guides

The generic guides apply to every module; each product has its own page for what is specific to it. Start with the [guides index](docs/guides/README.md).

| I want to... | Guide |
|---|---|
| Install `tsi` and run my first module | [Getting started](docs/guides/getting-started.md) |
| Find the admin password, API key or setup token; choose automatic or manual setup | [Bootstrap and signing in](docs/guides/bootstrap-and-signing-in.md) |
| Change ports, names, secrets, entities... in `tsi.yaml` | [Configuration](docs/guides/configuration.md) |
| Understand shared, per-module and external databases, ports, and where the database scripts come from | [Databases and ports](docs/guides/databases-and-ports.md) |
| Look after `.tsi/`, back it up, start over | [Secrets and state](docs/guides/secrets-and-state.md) |
| Look up a command, a flag or an exit code | [Command reference](docs/guides/commands.md) |
| Run the smoke tests, use them in CI, or start modules from `go test` | [Testing and CI](docs/guides/testing-and-ci.md) |
| Fix a problem | [Troubleshooting](docs/guides/troubleshooting.md) |

## Modules

| Module | Purpose | Version | Smoke tests | Guide |
|---|---|---|---|---|
| `tsi-dpdp-cms` | Consent management | v0.5.3 | 7 | [guide](docs/guides/products/tsi-dpdp-cms.md) |
| `tsi-privacy-vault` | PII vault, tokenization and erasure | v0.3.0 | 10 | [guide](docs/guides/products/tsi-privacy-vault.md) |
| `tsi-compass` | IT GRC and compliance operations | v0.4.3 | 6 | [guide](docs/guides/products/tsi-compass.md) |
| `tsi-dx-node` | Peer-to-peer governed data exchange, run as a pair | v0.3.0 | 6 per node | [guide](docs/guides/products/tsi-dx-node.md) |

## Use it from Go

```go
import (
    "github.com/tsi-coop/tsi-cli/compose"
    "github.com/tsi-coop/tsi-cli/testkit"
)

func TestMyApp(t *testing.T) {
    s := testkit.Start(t, compose.WithModules("tsi-dpdp-cms")) // starts, provisions, cleans up
    c, _ := s.Credentials("tsi-dpdp-cms")
    // point the app under test at c.BaseURL with c.APIKey / c.APISecret
}
```

See [Testing and CI](docs/guides/testing-and-ci.md#using-real-modules-from-go-test) for the library API.

## Development

```bash
make test          # unit tests with the race detector
make build         # bin/tsi
make network-test  # downloads every pinned migration from GitHub and checks its checksum
make integration   # real Docker: all four modules, dx-node as a pair, about 70 seconds
```

How it is built, and why: the [architecture](docs/design/architecture.md) (functional and technical design in one document). What changed in each release: the [changelog](CHANGELOG.md).

## Limits

- Local and development use first; there is no Kubernetes output.
- `tsi-compass` is large (tickets, risks, controls, DPIA, vendor portal). `tsi` provisions the first administrator and smoke-tests the basics; it does not seed business data.
- `tsi-dx-node` runs as at most two nodes, and only the asynchronous lane is set up. See its [guide](docs/guides/products/tsi-dx-node.md#limits).
- The consent module's fiduciary domain validation needs real DNS, so it stays pending on localhost.

## License & Contributions

This project is distributed under the Apache 2.0 License. See [LICENSE](LICENSE) and [NOTICE](NOTICE).

To contribute, email a short summary of your change and a link to your branch to admin@tsicoop.org. Direct commits to `main` are restricted, and every contribution is reviewed for fit, readability and long-term maintenance before it is merged. See [CONTRIBUTING.md](CONTRIBUTING.md) and the [Code of Conduct](CODE_OF_CONDUCT.md). Report vulnerabilities privately, as described in [SECURITY.md](SECURITY.md).
