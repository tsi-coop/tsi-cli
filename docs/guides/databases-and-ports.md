# Databases and ports

Where the PostgreSQL servers run, which ports are used, and where the database scripts come from.

## Database modes

Set the mode in `tsi.yaml` (`database.mode`) or with `tsi compose init --database <mode>`.

| Mode | PostgreSQL servers | Databases and roles |
|---|---|---|
| `shared` (default) | **One** container for the whole stack | One database and one login role per module (or per instance) |
| `per-module` | One container per module or instance, like the modules' own compose files | One database and role in each |
| `external` | Your own server; `tsi` does not start one | `tsi` creates one database and role per module on it |

### What "shared" means

One PostgreSQL *server*: one container, one data volume, one host port. It does **not** mean the modules share a database. Each one has its own database and its own role with its own generated password, and only that module's connection settings point at it, so a module cannot see another's tables. For example:

| Module | Database | Role |
|---|---|---|
| `tsi-dpdp-cms` | `tsi_cms` | `tsi_cms` |
| `tsi-privacy-vault` | `tsi_privacy_vault` | `tsi_vault` |
| `tsi-compass` | `tsi_compass` | `tsi_compass` |
| `tsi-dx-node` instance `node-a` | `tsi_dx_node_node_a` | `tsi_dx_node_node_a` |

A superuser, `tsi_admin`, exists only so `tsi` can create those databases.

The trade-off: the modules share a failure domain. If the container stops or its volume is deleted, every module is affected, and a heavy query in one can slow the others. Use `per-module` if you want each module isolated. It costs about 40 MB more per container and one more host port each.

### Not shared with anything else

Each stack has its own PostgreSQL container and volume, named after the project (`<project>-postgres-1`, volume `<project>_pgdata`; the project is `project:` in `tsi.yaml`). It never touches a module you run from its own repository. To see them:

```bash
docker ps --format '{{.Names}}  {{.Ports}}' | grep postgres
docker exec <project>-postgres-1 psql -U tsi_admin -c '\l'
```

### Several stacks, one PostgreSQL

"Shared" covers one project directory. A second project directory starts its own container, and both want the same port, so the second stops at the port check. To run several stacks against one server, use `external` and point each at it.

### External PostgreSQL

```bash
tsi compose init --modules tsi-compass --database external \
  --dsn 'postgres://admin:password@db.example.com:5432/postgres'
```

Requirements:

- The DSN is an **administrator** connection. It must be a superuser, because `tsi` creates roles and databases, and the consent module needs a database-level setting that only a superuser can set.
- The server must be reachable **from the machine running `tsi`** (to apply migrations) **and from the module containers**. Containers reach it as `host.docker.internal` by default; set `database.module_host` in `tsi.yaml` if the server is somewhere else. A server listening only on `127.0.0.1` of the host is not reachable from a container.
- `tsi` creates a database and role per module, with generated passwords, and does not touch other databases.

## Ports

Each module has the host port its own repository uses (the "upstream" port). A new project **adds an offset of 300** to every one, so a stack made by `tsi` does not collide with modules you run from their repositories.

| | Upstream | With the default offset |
|---|---|---|
| `tsi-dpdp-cms` | 8080 | **8380** |
| `tsi-privacy-vault` | 8081 | **8381** |
| `tsi-dx-node` (first instance, then second) | 8082, 8083 | **8382, 8383** |
| `tsi-compass` | 8085 | **8385** |
| Shared PostgreSQL | 5432 | **5732** |
| `per-module` PostgreSQL: cms, vault, dx-node (a, b), compass | 5432, 5433, 5434, 5435, 5437 | 5732, 5733, 5734, 5735, 5737 |

All ports are published on `127.0.0.1` only (`bind:` in `tsi.yaml`).

Change the offset in `tsi.yaml`:

```yaml
port_offset: 300      # add this to every upstream port; 0 gives the upstream ports
```

or for a project's `init`: `tsi compose init --port-offset 500`. Run `tsi compose up --plan` to see the ports a run would use without starting anything. `tsi compose modules` lists each module's port with the default offset.

A `tsi.yaml` with no `port_offset` line gets the default of 300. An explicit `port_offset: 0` is kept.

If a port is taken, `up` stops before creating anything and names the port and module: change `port_offset`, or free the port.

## Where the database scripts come from

The module images do not contain their database schema. In the module repositories it lives in a `db/` folder that is mounted into PostgreSQL. `tsi` does not copy those scripts into its own repository. Instead:

1. The module's catalog entry pins a **repository, a tag or commit, and a SHA-256 for every file**.
2. On first use, `tsi` downloads each file from `raw.githubusercontent.com` at that ref, checks the checksum, and caches it under your user cache directory (`~/.cache/tsi-cli`, or `$TSI_CACHE_DIR`).
3. A file that does not match its checksum is **never cached or run**.
4. Downloading happens before any container starts, so a network problem fails early and changes nothing.
5. The files are applied in order, and each one is recorded in a `_tsi_cli_migrations` table in the module's database with its checksum. A re-run applies only new files; a file that changed after being applied is refused.

### Offline, or developing a module

Point `tsi` at a local checkout of the module's `db/` folder:

```yaml
# tsi.yaml
migration_dirs:
  tsi-privacy-vault: /home/me/tsi-privacy-vault/db
```

Those files are used as they are, with a warning if they differ from the pinned release. The key is the module name (or the instance name for a multi-instance module).

### Upgrading a module

Changing a module's version is a catalog change (a new `ref` and new checksums), not a setting. To produce the checksums from a checkout:

```bash
hack/sums.sh ~/tsi-privacy-vault v0.3.0 db     # prints the "steps:" lines
make network-test                              # downloads and verifies every pinned file
```

`tsi` is not an upgrade tool. For a database that already holds data, follow the module's own upgrade instructions.
