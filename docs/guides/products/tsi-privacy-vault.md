# tsi-privacy-vault

PII vault: tokenization, blind-index search and erasure. Version **v0.3.0**.

Generic topics (credentials, databases, ports, secrets) are in the [guides index](../README.md). This page is what is specific to this module.

| | |
|---|---|
| Image | `tsicoop/tsi-privacy-vault:v0.3.0` |
| Host port | 8381 (upstream 8081, plus the default offset of 300) |
| Database | `tsi_privacy_vault`, role `tsi_vault` |
| Database scripts | 5 files from `tsi-coop/tsi-privacy-vault` at tag `v0.3.0` |
| Credentials from `tsi compose creds` | admin login, and a client API key and secret |

## Run it

```bash
tsi compose init --modules tsi-privacy-vault --non-interactive
tsi compose up -d --bootstrap
tsi compose test
tsi compose creds --show-secret
```

## What bootstrap creates

| Step | Result |
|---|---|
| Setup | The first administrator, role `SYSTEM_ADMIN`, username `admin`, with the email from `tsi.yaml`. The vault allows only one |
| Login | An administrator session |
| Entities | Three kinds of data the vault holds: `TSI_ID_NUMBER` (an ID: 12 digits), `TSI_NOTE` (free data) and `TSI_DOCUMENT` (a file: PDF or PNG) |
| Client key | An API key labelled `tsi-cli`. The secret is shown by the vault once, and saved by `tsi` |
| Permissions | Read and write on each of the three entities for that key |

An *entity* is a type of record the vault accepts. A client key can only store and fetch the entities it has been granted.

## Settings

Under `bootstrap: tsi-privacy-vault:` in `tsi.yaml` ([how these work](../configuration.md#bootstrap-first-run-settings); they apply the first time only):

```yaml
bootstrap:
  tsi-privacy-vault:
    admin: {username: admin}
    api_key: {label: payments-app}
    entities:                          # a list you give replaces the default list
      - {code: PAN, name: PAN number, flavor: ID, regex: '^[A-Z]{5}[0-9]{4}[A-Z]$', mime_types: ""}
      - {code: NOTE, name: Note, flavor: DATA, regex: "", mime_types: ""}
      - {code: CONTRACT, name: Contract, flavor: FILE, regex: "", mime_types: "application/pdf"}
```

Every entity needs all five keys (use `""` for ones that do not apply). `flavor` is `ID`, `DATA` or `FILE`. The smoke tests use the first entity of each flavor, so keep at least one of each if you want them to pass.

## Environment variables you can override

| Variable | Default | Notes |
|---|---|---|
| `TSI_PRIVACY_VAULT_ENV` | `local` | Upstream defaults to `production`, which marks the console cookie `Secure` and breaks plain-http use |
| `TSI_PRIVACY_VAULT_NODE_ID` | `TSI_LOCAL` | Recorded in the audit trail |
| `TSI_PRIVACY_VAULT_MAX_UPLOAD_KB` | `5120` | Largest file |
| `TSI_PRIVACY_VAULT_ERASURE_JOB_MINUTES` | `60` | How often the erasure job runs |
| `TSI_PRIVACY_VAULT_KMS_PROVIDER` | `LOCAL` | Root key held in the environment. AWS KMS is not set up by `tsi` |
| `TSI_PRIVACY_VAULT_FILE_STORE` | `DATABASE` | Files in the database. S3 is not set up by `tsi` |
| `TSI_EXPORT_PATH` | `/var/lib/tsi/exports/` | |

Generated for you: the master key and the index secret (both [permanent](../secrets-and-state.md#permanent-secrets): data keys are wrapped by the master key, and search stops finding existing records if the index secret changes), the JWT secret, the setup token and the database password.

## Using the API

Client calls are `POST /api/client/vault`. Content is sent base64-encoded.

```bash
BASE=http://127.0.0.1:8381
KEY=...; SECRET=...
H=(-H 'Content-Type: application/json' -H "X-API-Key: $KEY" -H "X-API-Secret: $SECRET")

# store a note: the reply has a reference_key
curl -s "$BASE/api/client/vault" "${H[@]}" -d "{\"_func\":\"store_data\",\"entityType\":\"DATA\",
  \"entityName\":\"TSI_NOTE\",\"content\":\"$(printf 'hello' | base64)\"}"

# fetch it back
curl -s "$BASE/api/client/vault" "${H[@]}" -d '{"_func":"fetch_data","reference-key":"<reference_key>"}'

# find it by a word, without the vault decrypting records to look
curl -s "$BASE/api/client/vault" "${H[@]}" -d '{"_func":"search_data","searchTerm":"hello"}'
```

Add `"principal_id":"<person id>"` to `store_data` to link a record to a person, so everything about one person can later be erased together. An ID value that fails the entity's pattern is refused with 422.

Erasure is an administrator action: `POST /api/admin/erasure` with `{"_func":"shred_principal","principal_id":"<id>","confirm":"<id>"}`. Records of that person then read as `410` (erased); records under a retention hold are kept. The administrator API uses `Authorization: Bearer <token>` from `POST /api/admin/login` (`{"_func":"admin_login","email":"...","password":"..."}`). The module's guides in its repository (`docs/guides/`) cover entities, permissions, erasure and the audit trail.

## Smoke tests

`tsi compose test --module tsi-privacy-vault` runs ten checks: readiness; a wrong secret is refused with 401; an ID value, a DATA value and a file are each stored and fetched back unchanged; a value that fails the entity pattern is rejected; a stored value is found by the blind index; an unknown reference is not returned; a read-only key can fetch but not store (403); and shredding a principal makes its records read as erased, after a wrong confirmation is refused. They create a temporary read-only key and deactivate it afterwards.

## Manual setup

Start without `--bootstrap`, take the token from `.tsi/env/tsi-privacy-vault.env` (`TSI_PRIVACY_VAULT_BOOTSTRAP_TOKEN`), and use the vault's setup page (`init.html`, see the vault's README). The vault creates exactly one `SYSTEM_ADMIN`. See [Bootstrap and signing in](../bootstrap-and-signing-in.md).

## Things to know

- The master key is held in the container's environment (`LOCAL` key anchor). That is fine for development and tests; for a managed key follow the vault's own AWS KMS guide.
- A few vault errors come back as HTTP 200 with the real status in the body. `tsi` and its tests treat the body status as the result.
- Stopping the stack with `down` keeps the data. `down --volumes` deletes it along with the keys that protect it.
