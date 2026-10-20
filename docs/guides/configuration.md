# Configuration (`tsi.yaml`)

`tsi.yaml` holds your choices. It contains **no secrets**, so it is safe to commit. `tsi compose init` creates it; you can edit it by hand, and every command reads it.

Unknown keys are rejected rather than ignored, so a typo is reported.

## Reference

```yaml
version: 1
project: my-stack                 # compose project name (containers, network, volumes)
modules:                          # what to run
  - tsi-dpdp-cms
  - tsi-privacy-vault
  - tsi-dx-node:node-a            # module:instance, for modules you run more than once
  - tsi-dx-node:node-b
database:
  mode: shared                    # shared | per-module | external
  port: 5432                      # base port of the shared PostgreSQL (the offset is added)
  dsn: ""                         # external only: an administrator connection string
  module_host: host.docker.internal   # external only: how containers reach the server
port_offset: 300                  # added to every upstream host port
bind: 127.0.0.1                   # the address ports are published on
admin:
  name: Administrator             # the administrator created in every module
  email: admin@example.org
overrides:                        # environment variable values per module
  tsi-dpdp-cms:
    BRAND_NAME: Acme Privacy
bootstrap:                        # first-run settings per module (names, entities, ...)
  tsi-privacy-vault:
    api_key: {label: my-app}
migration_dirs:                   # local database scripts instead of the pinned release
  tsi-privacy-vault: /home/me/tsi-privacy-vault/db
```

| Key | Default | Notes |
|---|---|---|
| `project` | `tsi` | Lowercase letters, digits, `-`, `_`. Used in container, volume and network names, so two stacks on one machine need different projects |
| `modules` | none | At least one. `module:instance` runs a module under an instance name. The instance name is the service, state and env-file name, and must be lowercase letters, digits and `-` |
| `database.mode` | `shared` | See [Databases and ports](databases-and-ports.md) |
| `port_offset` | `300` for new projects, and when the key is missing | `0` gives the upstream ports. The key is always written by `init` |
| `bind` | `127.0.0.1` | `0.0.0.0` publishes on every interface. Do not do that on a shared network: the stack has no TLS and uses development settings, and the generated administrator passwords protect real consoles |
| `admin.email` | `admin@example.org` | Must be accepted by every module you run. Avoid `.local` domains: `tsi-dx-node` accepts them at setup and rejects them at login |
| `admin.name` | `Administrator` | |

## Modules and instances

A module normally runs once, under its own name. Only modules that support it can run more than once; today that is `tsi-dx-node`, at most **two** instances.

```yaml
modules:
  - tsi-dx-node:node-a
  - tsi-dx-node:node-b
```

The name after the colon is what you use everywhere else: in `overrides`, `bootstrap`, `migration_dirs`, `--module` and `tsi compose creds`. Instances get consecutive host ports and their own database. See the [tsi-dx-node guide](products/tsi-dx-node.md).

Listing a single-instance module twice is refused.

## `overrides`: environment variables

Sets an environment variable of a module's container. The key is the module (or instance) name; the variable must be one the module's catalog entry lists, or `tsi` refuses the override.

```yaml
overrides:
  tsi-dpdp-cms:
    BRAND_NAME: Acme Privacy
    ALLOWED_ORIGINS: https://consent.example.com
```

Apply a change by running `tsi compose up -d` again. The container is recreated with the new value; data is kept.

**Permanent secrets cannot be overridden once the database holds data.** `DB_ENCRYPTION_KEY`, `TSI_LOOKUP_SALT`, the vault's `TSI_PRIVACY_VAULT_MASTER_KEY` and `TSI_PRIVACY_VAULT_INDEX_SECRET` are locked after the first migration, and `tsi` refuses (exit code 7). See [Secrets and state](secrets-and-state.md#permanent-secrets).

The variables each module accepts are listed in its [product guide](products/).

## `bootstrap`: first-run settings

Settings for the bootstrap steps: what to name the fiduciary, which entities the vault holds, what the contract between two nodes looks like. Each module's keys are in its [product guide](products/).

```yaml
bootstrap:
  tsi-dpdp-cms:
    fiduciary: {name: Acme Pvt Ltd, primary_domain: acme.example}
    app: {name: web}
```

- Your values are merged over the module's defaults, one level at a time: setting `fiduciary.name` keeps the default `fiduciary.phone`.
- A **list** you give replaces the default list entirely (for example the vault's `entities`).
- A few **documents** replace the default entirely instead of merging, so a default field cannot leak into yours: the consent module's `policy.content`, and the exchange module's `contract.schema`, `contract.governance_rules` and `sample`. Each product guide says which.
- Strings may use `{{.Admin.Email}}` and `{{.Admin.Name}}`.
- **Settings apply the first time only.** Once a bootstrap step is recorded as done, changing its setting changes nothing. To apply different settings, start over: `tsi compose down --volumes --yes`, then `init` and `up --bootstrap`.
- The key is the instance name, or the module name to apply to every instance of that module.

## `migration_dirs`

Reads a module's database scripts from a local directory instead of downloading the pinned release. See [Databases and ports](databases-and-ports.md#offline-or-developing-a-module).

## Command-line overrides

Flags override `tsi.yaml` for one command: `--dir`, `--project`, `--port-offset`. For `init` they set the value that is written. See [Command reference](commands.md).
