# Troubleshooting

Find your message, or your symptom, below. Errors from `tsi` have the form `error: [step] module: what happened`, then a `fix:` line. Exit codes are in the [command reference](commands.md#exit-codes).

## Starting

### `port 5432 is already in use` (or any port)

Something on the machine already listens there, often a module running from its own repository. Exit code 3. Nothing was created.

- Check which port the run would use: `tsi compose up --plan`.
- If the number is an upstream default (5432, 8080...), your `tsi.yaml` or binary predates the default offset of 300. Rebuild `tsi`, and make sure the `port_offset` line is there or absent (absent means 300). An explicit `port_offset: 0` means the upstream ports.
- Pick another offset: `port_offset: 500` in `tsi.yaml`. See [Databases and ports](databases-and-ports.md#ports).
- Find the process: `ss -ltnp | grep 5732`.

### `cannot reach the Docker daemon` / `docker is not installed`

Start Docker, or add your user to the `docker` group (`sudo usermod -aG docker $USER`, then log in again). `tsi` needs the `docker compose` plugin (v2), not the old `docker-compose` script.

### `cannot download the migration`

The database scripts are fetched from GitHub on first use (exit code 10, nothing started). Check the network and any proxy (`HTTPS_PROXY`). Offline: use a local checkout with `migration_dirs` ([how](databases-and-ports.md#offline-or-developing-a-module)). The error prints the exact URL.

### `downloaded file does not match the pinned checksum`

A file at the pinned tag changed upstream after your `tsi` build pinned it. Nothing was cached or applied. Upgrade `tsi`, or report it. Do not work around it.

### `not ready after 3m0s`

The module started but did not answer its readiness call. See what it says:

```bash
tsi compose logs --tail 80 tsi-compass
tsi compose status
```

Common causes: a wrong value in an `overrides` entry (for example a `BRAND_NAME` over 12 characters in the consent module stops it at startup), or too little memory for several JVM containers. Fix the cause and run `tsi compose up -d` again.

### `containers for project ... exist but .tsi/state.yaml is missing`

Exit code 8. `tsi` will not generate new keys over existing data. Restore `.tsi/state.yaml` from a backup, or `tsi compose down --volumes --yes` to start over. See [Secrets and state](secrets-and-state.md).

### `<variable> cannot change once the database holds data`

Exit code 7. You overrode a permanent secret after the first run. Remove the override, or start over (which deletes the data). See [Permanent secrets](secrets-and-state.md#permanent-secrets).

## Signing in

### The module's setup page says setup is already complete

Expected after `up --bootstrap`. Sign in with `tsi compose creds --show-secret`. To see the setup page instead, use a fresh stack and `up` without `--bootstrap`. See [Bootstrap and signing in](bootstrap-and-signing-in.md).

### I need the setup token

`grep -H BOOTSTRAP_TOKEN .tsi/env/*.env`. See [Finding the setup token](bootstrap-and-signing-in.md#finding-the-setup-token).

### `tsi compose bootstrap` or `test` fails at `[login]`

`tsi` logs in with the administrator password it generated, kept in `.tsi/state.yaml`. It fails if you created the administrator yourself, if you changed the password in the module, or if the state is from another stack. Put the real password in `modules.<module>.admin_password` in `.tsi/state.yaml`. Compass and the consent module also limit sign-in attempts per address, so wait a few minutes after repeated failures (HTTP 429).

### The consent or Compass API key is rejected

For `tsi-compass`, this is a known limitation of v0.4.3: API keys it generates cannot authenticate. Use the administrator login. See the [tsi-compass guide](products/tsi-compass.md#api-keys).

## Running

### A smoke test fails

The report shows the HTTP status and the start of the response. Re-run just that module with `tsi compose test --module <name>`, and look at `tsi compose logs --tail 80 <module>`. If it fails the same way on a fresh stack, please report it with the output.

### `[handshake]` or `Peer certificate mismatch` (dx-node)

The two nodes could not establish trust. They must run under the names `node-a-server-1` and `node-b-server-1`, which `tsi` arranges when you name the instances `node-a` and `node-b` in order. See the [tsi-dx-node guide](products/tsi-dx-node.md#limits).

### Changing a setting did nothing

- **Environment variables** (`overrides`): run `tsi compose up -d`.
- **Bootstrap settings** (`bootstrap:`): these apply the first time only. Start over to change them ([why](configuration.md#bootstrap-first-run-settings)).
- **Ports and the database mode**: run `up` again; changing the database mode of a stack that holds data does not move the data, so start over instead.

### Start completely over

```bash
tsi compose down --volumes --yes
tsi compose init --modules ... --non-interactive
tsi compose up -d --bootstrap
```

### Leftover containers or volumes

`tsi` names everything after the project, so you can find it:

```bash
docker ps -a --filter name=<project>
docker volume ls | grep <project>
docker compose -p <project> down --volumes     # last resort, if tsi cannot
```

## Still stuck

Collect the output of `tsi version`, `tsi compose status`, `tsi compose up --plan` and the failing command with its `error:` line. Do not include the contents of `.tsi/`.
