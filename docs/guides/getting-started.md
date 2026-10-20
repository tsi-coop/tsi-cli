# Getting started

From nothing to a running, provisioned and tested module.

## What you need

| Requirement | Why |
|---|---|
| Docker 24+ with the `docker compose` plugin | Runs the modules and PostgreSQL |
| Internet access on the first run of each module version | `tsi` downloads the database scripts from the module's GitHub repository ([details](databases-and-ports.md#where-the-database-scripts-come-from)); Docker pulls the images |
| Free ports | New projects use 8380 to 8385 for the apps and 5732 for PostgreSQL ([details](databases-and-ports.md#ports)) |
| Go 1.22+ | Only to build `tsi` from source |

## Install `tsi`

```bash
git clone https://github.com/tsi-coop/tsi-cli.git
cd tsi-cli
make build                          # writes bin/tsi
sudo mv bin/tsi /usr/local/bin/
tsi version
```

Or `go install github.com/tsi-coop/tsi-cli/cmd/tsi@latest` once the repository is published at that path. Homebrew and release binaries are planned.

`tsi version` prints the nearest git tag, or the short commit hash if there are no tags. A `-dirty` suffix means the build came from a checkout with uncommitted changes.

## Run your first module

Work in an empty directory. Everything `tsi` creates goes there.

```bash
mkdir my-tsi-stack && cd my-tsi-stack

tsi compose init                    # choose modules and a database mode
tsi compose up -d --bootstrap       # start, wait until ready, provision
tsi compose test                    # smoke tests with the credentials it just made
tsi compose status                  # what is running and where
tsi compose creds --show-secret     # how to sign in
```

Skip the prompts with flags:

```bash
tsi compose init --modules tsi-dpdp-cms --non-interactive
```

Several modules in one stack:

```bash
tsi compose init --modules tsi-dpdp-cms,tsi-privacy-vault,tsi-compass --non-interactive
```

## What `init` does

It writes these files in your directory:

| File | What it is | Commit it? |
|---|---|---|
| `tsi.yaml` | Your choices: modules, database mode, ports, overrides. No secrets. See [Configuration](configuration.md) | Yes |
| `docker-compose.tsi.yml` | The generated compose file. Reviewable, deterministic, contains no secrets | Optional |
| `.tsi/` | Generated secrets, per-container env files, keystores, node identities, saved credentials. See [Secrets and state](secrets-and-state.md) | **No.** `tsi` adds it to `.gitignore` |
| `tsi.lock` | The image digests pulled, after the first `up` | Optional |

`init` never overwrites an existing `tsi.yaml` without `--force`, and never regenerates a secret that already exists.

## What `up --bootstrap` does

1. Checks Docker, and that the ports are free.
2. Downloads and verifies the database scripts for each module.
3. Pulls images and starts PostgreSQL.
4. Creates each module's database and applies its migrations.
5. Starts the modules and waits until each answers a readiness call.
6. **Bootstrap:** creates the administrator and whatever else the module needs (an app and API key, entities, a policy, partner nodes), and saves what the module returns.

Step 6 is the part you would otherwise do by hand in each module's console. Without `--bootstrap`, steps 1 to 5 run and you set the module up yourself: see [Bootstrap and signing in](bootstrap-and-signing-in.md).

A first run takes under a minute once the images are present.

## Next

- Sign in: [Bootstrap and signing in](bootstrap-and-signing-in.md).
- Module specifics: the [product guides](products/).
- Stop or start over: `tsi compose down` keeps your data and credentials; `tsi compose down --volumes --yes` deletes the data, the saved credentials and the generated files. See [Secrets and state](secrets-and-state.md#starting-over).
