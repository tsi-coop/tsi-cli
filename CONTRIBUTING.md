# Contributing

TSI CLI is fully open-source (Apache 2.0). Forking, modifying, and
running your own customized version is unrestricted.

Direct commits to `main` are restricted so the core platform stays stable
and securely managed. To give back a change:

1. Make your change in a branch or fork.
2. Email a brief summary of the change and a link to your branch to the
   repository owner at **admin@tsicoop.org**.
3. Every submission is manually evaluated for architectural alignment,
   readability, and long-term maintenance impact before integration.

There is no separate PR-based intake process at this time; email is the
process.

## Code of conduct

All interactions with the project, including contribution emails, are
covered by our [Code of Conduct](CODE_OF_CONDUCT.md).

## Reporting security issues

Do not open a public issue or PR for a vulnerability. See
[SECURITY.md](SECURITY.md) for the disclosure process.

## Building and testing

Build with `make build` and run the unit tests with `make test`. The
integration test (`make integration`) starts the real modules and needs
Docker. See the [Getting started guide](docs/guides/getting-started.md) and
the [architecture](docs/design/architecture.md) for how the tool is built.
