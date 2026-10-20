# tsi-cli guides

Start here. The guides in this folder apply to every module; the product guides under [products/](products/) cover what is specific to each one.

## Where to start

| Stage | I want to... | Go to |
|---|---|---|
| **1. Install and run** | Install `tsi` and bring up a module for the first time | [Getting started](getting-started.md) |
| **2. Sign in** | Find the admin password, API key or setup token; choose between automatic and manual setup | [Bootstrap and signing in](bootstrap-and-signing-in.md) |
| **3. Configure** | Change ports, secrets, names, entities and other settings | [Configuration (`tsi.yaml`)](configuration.md) |
| | Understand shared, per-module and external databases, and the port ranges | [Databases and ports](databases-and-ports.md) |
| **4. Operate** | Look after `.tsi/`, back it up, start over, understand permanent keys | [Secrets and state](secrets-and-state.md) |
| | Look up a command or flag, or an exit code | [Command reference](commands.md) |
| **5. Test** | Run the smoke tests, use them in CI, or start modules from `go test` | [Testing and CI](testing-and-ci.md) |
| **6. Fix** | Something failed | [Troubleshooting](troubleshooting.md) |

## Product guides

| Module | What it is | Guide |
|---|---|---|
| `tsi-dpdp-cms` | Consent management under the DPDP Act | [tsi-dpdp-cms](products/tsi-dpdp-cms.md) |
| `tsi-privacy-vault` | PII vault: tokenization, blind-index search, erasure | [tsi-privacy-vault](products/tsi-privacy-vault.md) |
| `tsi-compass` | IT GRC and compliance operations | [tsi-compass](products/tsi-compass.md) |
| `tsi-dx-node` | Peer-to-peer governed data exchange (run as a pair) | [tsi-dx-node](products/tsi-dx-node.md) |

## Other documents

- [Notes for the module maintainers](maintainer-notes.md): things found while integrating that the module repositories could change.
- [Architecture](../design/architecture.md): the functional and technical design of `tsi`, and why it is built this way.

## A word on scope

`tsi` is for **local development, evaluation and CI**. It binds ports to `127.0.0.1`, generates secrets for you and has no TLS, backup, upgrade or high-availability story. For production, follow the deployment guide in each module's own repository.
