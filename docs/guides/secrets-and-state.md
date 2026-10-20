# Secrets and state

What `tsi` generates, where it keeps it, what you must not lose, and how to start over.

## What lives where

```
my-tsi-stack/
├── tsi.yaml                    your choices, no secrets (commit it)
├── docker-compose.tsi.yml      generated compose file, no secrets
├── tsi.lock                    image digests that were pulled
├── .gitignore                  tsi adds ".tsi/" to it
└── .tsi/                       directory mode 0700, git-ignored
    ├── state.yaml              the single source of truth (mode 0600)
    ├── env/<module>.env        one env file per container (mode 0600)
    ├── env/postgres.env        PostgreSQL superuser password (shared mode)
    ├── certs/<module>.p12      keystore for modules that need one
    ├── certs/<instance>.pem    node identity certificates (dx-node)
    ├── certs/<instance>.key    node identity private keys (mode 0600)
    └── lock                    stops two tsi commands running at once
```

`docker-compose.tsi.yml` refers to the env files by path; it never contains a secret, so it is safe to read, review and commit.

## `state.yaml`

`.tsi/state.yaml` is the only place `tsi` keeps the things the modules cannot give back:

- generated secrets for each module (JWT secrets, encryption keys, setup tokens, database passwords);
- the administrator password for each module;
- the API key and secret each module returned (shown only once);
- which bootstrap steps are done, and what they returned (ids of the app, fiduciary, partner, contract...);
- whether each module's database holds data yet (this locks the permanent secrets).

It is written atomically (a temporary file renamed over the old one), with mode 0600, and under a lock, so a crash cannot leave half a file and two commands cannot interleave.

**Back it up** if the stack holds anything you care about. Without it:

- a running stack cannot be managed: `tsi compose up` refuses to run if containers exist but the file is missing (exit code 8), instead of generating new keys over existing data;
- the data may be unreadable: see below.

## Permanent secrets

Some secrets protect data at rest. If they change, data already stored cannot be read again.

| Module | Permanent secrets |
|---|---|
| `tsi-dpdp-cms` | `DB_ENCRYPTION_KEY`, `TSI_LOOKUP_SALT` |
| `tsi-privacy-vault` | `TSI_PRIVACY_VAULT_MASTER_KEY`, `TSI_PRIVACY_VAULT_INDEX_SECRET` |
| `tsi-compass` | `DB_ENCRYPTION_KEY`, `TSI_LOOKUP_SALT` |
| `tsi-dx-node` | the node's private key (`.tsi/certs/<instance>.key`); peers have pinned its public key |

`tsi` locks them as soon as a module's migrations have run. After that:

- a `tsi.yaml` override of one of them is refused (exit code 7);
- the migration step that stores the consent module's key refuses to run with a different value;
- the node identity files are never replaced.

If you need different values, you are starting over, and the existing data is gone.

## Starting over

| Command | Containers | Data volumes | `.tsi/`, compose file, `tsi.lock` |
|---|---|---|---|
| `tsi compose down` | removed | **kept** | **kept** |
| `tsi compose down --volumes --yes` | removed | **deleted** | **deleted** |

`down` then `up -d` brings the same stack back with the same data and the same credentials. `--volumes` is destructive and asks you to type the project name unless you pass `--yes` (a non-interactive run needs `--yes`). Your `tsi.yaml` is never deleted.

After `down --volumes` run `tsi compose init` again: new secrets, a new database and fresh credentials.

## Git

`tsi` appends `.tsi/` to `.gitignore` the first time it writes. Check that before you push a stack directory. Do not copy `.tsi/` to another machine or paste its contents into an issue: it holds working credentials.

Output from `tsi` redacts the secrets it knows, in logs, errors and `docker` output. A secret you typed into `tsi.yaml` yourself is not known to it, so keep secrets out of `tsi.yaml`.

## Rotating a secret

There is no rotate command. For secrets that are not permanent (JWT secrets, setup tokens), edit the value under `modules.<module>.secrets` in `state.yaml`, then run `tsi compose up -d`: the env file is regenerated and the container recreated. Existing sessions end. API keys and admin passwords are changed in the module itself; then update `state.yaml` to match if you want `tsi compose test` to keep working.
