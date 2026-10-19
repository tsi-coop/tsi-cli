# tsi-cli: Go Library and CLI Implementation Plan (`tsi compose`)

**Repository:** `tsi-cli` (renamed from `tsi-compose` to avoid confusion with `tsi-compass`). **Go module:** `github.com/tsicoop/tsi-cli`. **Binary:** `tsi`. **First feature:** the `tsi compose` command group. In this document, "`tsi compose`" is the feature and "`tsi-cli`" is the repo.

**Basis:** this plan is built from the module repositories themselves, not from the architecture docx. I read each repo's `docker-compose.yml`, `Dockerfile`, env templates, `db/` migrations, `_processor.tsi` route table, setup/login/key services and validator schemas. The docx is used only for the original intent (a CLI that picks, configures and launches modules). Wherever it disagrees with the code, the code wins (section 3).

Repos read (all under `/home/tsi/`): `tsi-dpdp-cms` (tag v0.5.3), `tsi-privacy-vault` (v0.3.0), `tsi-dx-node` (v0.3.0), `tsi-compass` (v0.4.3 per README), `tsi-ledger`, `tsi-sign`.
Not read, out of scope for now: `tsi-kyc`, `tsi-nexus`, `tsi-verify`, `tsi-ratings`, `tsi-digital-hub` and the community repos.

**Confidence marks used below:** **[V]** = verified by reading the code. **[I]** = inferred from a route table, validator schema or naming, and needs a live check before coding. Nothing here has been run.

---

## 1. Goal

A **Go library** (plus a thin `tsi` CLI on top, whose first command group is `tsi compose`) that takes a developer from nothing to a running, configured and tested TSI stack in minutes:

```go
import "github.com/tsicoop/tsi-cli/compose"

stack, _ := compose.New(compose.WithModules("tsi-ledger", "tsi-privacy-vault"))
stack.Up(ctx)                            // render, pull, start, wait ready
creds, _ := stack.Bootstrap(ctx, spec)   // admin, tenant app, API key: all via the modules' own APIs
stack.Test(ctx)                          // smoke tests with the generated credentials
defer stack.Down(ctx)
```

Usable three ways: from the CLI, from `go test` (integration tests against real modules), and from CI.

**In scope (v1):** catalog of modules, config and secret generation, manifest rendering, lifecycle, readiness, provisioning ("bootstrap"), smoke tests, CLI.
**Out of scope (v1):** Kubernetes output, remote or production deploys, telemetry, changing the modules themselves, and any other `tsi` command groups (such as `tsi verify` or `tsi vault`). Nothing in the module repos supports those today. Add them only when there is a concrete need, so the repo name does not promise more than it delivers.
**Last:** natural-language provisioning (section 13).

## 2. What the repos actually are

Six products, one shared shape **[V]**:

- Java 17 WAR on Jetty, container port **8080**; Postgres 15 with its **own** database per product.
- Compose files use service names `postgres_db` + `jetty_app` (dx-node uses `database` + `server`).
- The schema comes from numbered `db/*.sql` files that each compose file **mounts into Postgres `docker-entrypoint-initdb.d`**. **The image does not contain the schema.**
- No `HEALTHCHECK` in any Dockerfile, and no `/health` endpoint in any module.
- All API calls are JSON `POST`. Most use a `_func` field in the body to pick the action.
- First-run flow is the same everywhere: one-time admin setup, then login, then create a tenant "app", then generate an API key, and clients then call with `X-API-Key` and `X-API-Secret`.
- All pull `tsicoop/<name>:latest` from Docker Hub, except `tsi-sign` which is `build:` only (no image reference).

### 2.1 Per-module facts

| | tsi-dpdp-cms | tsi-privacy-vault | tsi-ledger | tsi-compass | tsi-dx-node | tsi-sign |
|---|---|---|---|---|---|---|
| Purpose **[V]** | Consent management | PII vault, tokenization, erasure | Double-entry ledger | IT GRC and compliance operations | Peer-to-peer data exchange | Document sealing and eSign |
| Version | 0.5.3 | 0.3.0 | none | 0.4.3 | 0.3.0 | none |
| Host ports (app / db) | 8080 / 5432 | 8081 / 5433 | 8086 / 5438 | 8085 / 5437 | 8082 / 5434 (+8443 P2P) | 8088 / 5440 (+8091 sandbox) |
| API base | `/api/v1/{admin,client,bootstrap,public}/<svc>` | `/api/admin/<svc>`, `/api/client/vault` | `/api/v1/<svc>` | `/api/v1/<svc>` | `/api/admin/<svc>`, `/api/client/dx` | `/api/v1/...`, admin under `/api/v1/admin/<svc>` |
| Migrations | 13 `.sql` + `03z_enc_key.sh` | 5 | 1 | 22 | 5 | 11 |
| DB extensions | `pgcrypto`, `uuid-ossp` | none found | none found | `pgcrypto`, `uuid-ossp` | `uuid-ossp` | none found |

### 2.2 Setup, login, tenant credentials

| | Setup call (first admin) | Token needed? | Admin login | Tenant credential call | Returned once (field names) |
|---|---|---|---|---|---|
| cms **[V]** | `bootstrap/setup` `initial_setup` {email, name, password (min 12), setup_token} | Yes, `TSI_BOOTSTRAP_TOKEN` | `admin/operator` `login` {identifier, password} returns `token` (Bearer) | `admin/fiduciary` `create_fiduciary`, `admin/app` `create_app`, then `admin/apikey` `generate_api_key` {fiduciary_id, app_id, description, permissions[]} | `key_id` (= X-API-Key), `raw_api_key` (= X-API-Secret) |
| vault **[V]** | `/api/admin/setup` `register_admin` {username, password, email, role: `SYSTEM_ADMIN`, setup_token} | Yes, `TSI_PRIVACY_VAULT_BOOTSTRAP_TOKEN` | `/api/admin/login` `admin_login` {email, password} returns `token` | `/api/admin/apikey` `create_key` {label, active}; then `create_entity`, `create_permission` | `data.api_key_id`, `data.raw_api_secret` |
| ledger **[V]** | `/api/v1/setup` `initial_setup` {name, email, password} | **No** | `/api/v1/auth` `login` {email, password}. Sets a **session cookie** `tsi_ledger_session`. I found no Bearer support | `/api/v1/app` `create_app` {app_name, can_manage_accounts} | `api_key`, `api_secret` |
| compass **[V]** setup, **[I]** rest | `/api/v1/setup` `complete_setup` {admin_name, admin_email, admin_password (min 10), setup_token}; `check_setup` | Yes, `TSI_BOOTSTRAP_TOKEN` | `/api/v1/user` `login` {email, password} returns `token` | `/api/v1/apikey` `generate_api_key` | `api_key`, `api_secret` |
| dx-node **[V]** | `/api/admin/registeradmin` {node_id, fqdn, network_port, username, email, password, confirm, setup_token} | Yes, `TSI_DX_NODE_BOOTSTRAP_TOKEN` | `/api/admin/login` `admin_login` {email, password} returns `token` | `/api/admin/apps` `create_app` {name}, then `generate_api_key` {app_id} | `api_key`, `api_secret` |
| sign **[V]** setup, **[I]** login body | `/api/v1/admin/setup` `setup` {email, fullName, password (min 8)}; `setup_status` returns `{initialized}` | **No** | `/api/v1/admin/auth` `login` returns `token` (Bearer) | `/api/v1/admin/apps` `create_app` {appName, appSlug, ...} | `apiKey`, `apiSecret` (camelCase) |

Tenant calls to all six use headers `X-API-Key` and `X-API-Secret` **[V]** (ledger and sign confirmed in code; cms in code and docs; others in docs and key tables).

### 2.3 Secrets and config (what the library must generate)

| Module | Required config | Permanent once data exists |
|---|---|---|
| cms **[V]** | `POSTGRES_*`, `JWT_SECRET`, `DB_ENCRYPTION_KEY`, `TSI_LOOKUP_SALT`, `TSI_BOOTSTRAP_TOKEN`, `TSI_KEYSTORE_PATH/_PASS/_ALIAS` (pass must not be `changeit`; used by `Legal.java`), `ALLOWED_ORIGINS`, optional `BRAND_NAME` (max 12 chars, app refuses to start otherwise) | `DB_ENCRYPTION_KEY` |
| vault **[V]** | `POSTGRES_*`, `TSI_PRIVACY_VAULT_{MASTER_KEY (32 bytes), JWT_SECRET, INDEX_SECRET, BOOTSTRAP_TOKEN, NODE_ID, ENV, KMS_PROVIDER, FILE_STORE, ...}` plus optional AWS KMS/S3 | `MASTER_KEY` (data keys are wrapped by it) |
| ledger **[V]** | `POSTGRES_*`, `JWT_SECRET`, `ALLOWED_ORIGINS`, `TSI_LEDGER_ENV` | none |
| compass **[V]** | `POSTGRES_*`, `JWT_SECRET`, `DB_ENCRYPTION_KEY`, `TSI_LOOKUP_SALT`, `TSI_BOOTSTRAP_TOKEN`, `BRAND_NAME`, `TSI_VENDOR_PORTAL_ENABLED`, export path | `DB_ENCRYPTION_KEY` |
| dx-node **[V]** | `POSTGRES_*`, `TSI_DX_NODE_JWT_SECRET` (falls back to the vault's JWT variable name), `TSI_DX_NODE_BOOTSTRAP_TOKEN`; node identity certificate imported after setup | the node's private key |
| sign **[V]** | `POSTGRES_*`, `JWT_SECRET`, `KEYSTORE_*` (the image bakes a dev keypair with password `changeit`), `DEFAULT_KEY_ALIAS`, `STORAGE_LOCAL_FS_PATH`, `PUBLIC_BASE_URL`, `DEFAULT_ESIGN_PROVIDER_ID`, optional `TSA_*`, and `ESIGN_<CA>_*` for a real CA | the sealing keystore |

Every compose file ships **insecure fallbacks** (`secure_dev_password`, `local-dev-...-do-not-use-in-production`). The library must always supply real values.

## 3. Where the docx and the repos disagree

Decisions in this plan follow the repos.

| Docx says | Repos show |
|---|---|
| 5 modules | 6. `tsi-ledger` is missing from the docx |
| Ports 8081 to 8085 per module | Container port is always 8080. Host ports differ per repo (table 2.1) |
| Images on `ghcr.io/tsicoop/...` | Compose files reference Docker Hub `tsicoop/<name>`. `tsi-sign` has none |
| Vault needs PostgreSQL and Redis | Postgres only. No Redis in any compose file |
| Vault depends on cms ("consent gating") | No such link in the vault compose or env |
| `tsi-sign` stateless, no dependencies | Own Postgres, document volume, keystore and a sandbox CA container |
| `/health` endpoint on each module | None exists |
| dx-node needs "mTLS certificates" as input | The image generates a dev keystore. The node's identity is imported through the admin API after setup |
| One shared `.env.tsi` | Modules use different variable names for the same idea (section 5.3) |
| Compass is "IT GRC and evidence system of record" | Matches the repo |
| Sample manifest `version: '3.8'` | Obsolete in compose-spec, so the library omits it |

## 4. Design

### 4.1 Package layout

```
tsi-cli/                         # module: github.com/tsicoop/tsi-cli
├── compose/       # public API: Stack, options, Up/Down/Status/Bootstrap/Test
├── catalog/       # one YAML per module (embedded) + schema + loader
│   ├── modules/*.yaml
│   └── assets/<module>/db/*.sql   # migrations, embedded from the repos at a pinned version
├── config/        # typed config, defaults, validation, tsi.yaml read/write
├── secrets/       # generators, immutable-secret rules, state store (0600)
├── render/        # text/template to compose YAML + env file; golden-tested
├── engine/        # Engine interface: docker compose CLI impl, fake for tests
├── migrate/       # runs db/*.sql over pgx (for shared or external Postgres)
├── certs/         # PKCS#12 keystores, X.509 node identities
├── health/        # readiness probes with backoff
├── bootstrap/     # step runner: ordered, idempotent API calls, session handling
├── tsitest/       # smoke tests per module + go-test helpers
├── logs/          # multiplexed log streaming
└── cmd/tsi/       # thin CLI (cobra): `tsi compose ...` today, room for other groups later
```

**Library packages stay at the top level, not under `internal/`.** ISVs must be able to import `tsitest` and `compose` in their own `go test` runs (`github.com/tsicoop/tsi-cli/tsitest`). Only truly private helpers go in `internal/`. `cmd/tsi` contains no logic beyond flag parsing and printing, so everything the CLI can do is also available as a library call.

### 4.2 Key decisions, from what the repos force

1. **The library runs the migrations.** The schema is not in the images. Embedding `db/*.sql` per module (pinned to a tag) and applying them with `pgx` works for a local container and for the user's own Postgres through one code path. cms needs one extra step the library must reproduce: `03z_enc_key.sh` runs `ALTER DATABASE ... SET app.enc_key = '<DB_ENCRYPTION_KEY>'` and fails if the key is empty.
2. **Catalog is data, not code.** Each module's YAML holds image and tag, ports, env schema (name, secret, immutable, generator, default), migrations, readiness probe, setup call, login style, credential call, response field names, and smoke probes. This absorbs the naming differences in section 2.
3. **One shared Postgres or one per module.** The repos ship one per module, each on its own host port. Default to **one Postgres container with one database and one role per module** (fewer containers, same migrations path). Offer `database: per-module` and `database: external`. Decide in open question 1.
4. **Readiness cannot rely on a health endpoint.** There is none. Use a cheap unauthenticated call per module: ledger and sign `setup_status`, compass `check_setup`, cms `public/principal` `list_active_fiduciaries`, vault and dx-node: pick one **[I]**. A 200 from a status call that touches the DB also proves the DB works.
5. **Bootstrap session handling must support both styles:** Bearer token (cms, vault, compass, dx-node, sign) and cookie jar (ledger).
6. **Credentials come back once.** The key is stored hashed in every module that I checked (cms, ledger, compass, dx-node, sign, vault). The library saves them to `.tsi/state.yaml` (0600) immediately, and never regenerates silently. Rotation is an explicit flag.
7. **Immutable secrets:** `DB_ENCRYPTION_KEY` (cms, compass), `MASTER_KEY` (vault) and private keys cannot be regenerated once a volume or data exists. The library refuses, and says why.
8. **Open first-run setup (ledger, sign).** Anyone who reaches the port first becomes admin. Publish ports on `127.0.0.1`, bootstrap, then widen only if the user asks.
9. **Idempotency from status codes.** Setup returns 409 once done (cms, compass, ledger, sign, dx-node all do this in code **[V]**). The runner treats that as "already done" and loads saved credentials. If the saved state is missing, it says so, because admin passwords and API secrets cannot be recovered.
10. **Pin versions.** Compose files use `:latest`. The catalog pins a tag per module (only cms, vault and dx-node have git tags) and records the digest in `tsi.lock`.
11. **No telemetry.** All state is local under `.tsi/`.

### 4.3 Public API sketch

```go
func New(opts ...Option) (*Stack, error)
func WithModules(names ...string) Option
func WithDir(path string) Option
func WithEngine(e engine.Engine) Option
func WithConfigFile(path string) Option            // tsi.yaml

func (s *Stack) Configure(opts ...ConfigOption) error
func Database(mode DBMode, dsn ...string) ConfigOption   // Shared | PerModule | External
func Override(module, key, value string) ConfigOption
func PortOffset(n int) ConfigOption
func Instance(module, name string) ConfigOption          // e.g. two dx-nodes

func (s *Stack) Plan() (Plan, error)                     // dry run
func (s *Stack) Write(ctx context.Context) error         // compose file + env + state
func (s *Stack) Up(ctx context.Context, opts ...UpOption) error
func (s *Stack) WaitReady(ctx context.Context, d time.Duration) error
func (s *Stack) Bootstrap(ctx context.Context, spec BootstrapSpec) (*Credentials, error)
func (s *Stack) Credentials(module string) (Credentials, bool)
func (s *Stack) Test(ctx context.Context, opts ...TestOption) (tsitest.Result, error)
func (s *Stack) Status(ctx context.Context) (Report, error)
func (s *Stack) Logs(ctx context.Context, w io.Writer, opts ...LogOption) error
func (s *Stack) Down(ctx context.Context, opts ...DownOption) error
```

```go
func TestMyApp(t *testing.T) {
    s := tsitest.Start(t, compose.WithModules("tsi-ledger")) // Up, Bootstrap, WaitReady, t.Cleanup(Down)
    c, _ := s.Credentials("tsi-ledger")
    // point the app under test at c.BaseURL with c.APIKey / c.APISecret
}
```

## 5. Provisioning recipe per module

Every step is a call the module already exposes. `tsi compose` replaces what a person does in each product's browser console.

### 5.1 tsi-dpdp-cms (reference case)
1. Render: Postgres plus app. Mount a generated PKCS#12 keystore. Generate JWT secret, DB encryption key, lookup salt, bootstrap token.
2. Migrate `01`..`13` and run the `app.enc_key` step.
3. Ready: `POST /api/v1/public/principal` with `_func: list_active_fiduciaries`.
4. `bootstrap/setup` creates the admin. `admin/operator login`. `create_fiduciary`. `create_app` (needs `fiduciary_id`, `name`, `email`, `phone`, `processing_purposes`). `generate_api_key` with scopes `READ`, `WRITE` (and `PURGE` if asked). `create_policy` then `publish_policy` from a JSON file.
5. Caveats **[V]**: only role `ADMIN` can create fiduciaries and keys; `OPERATOR` cannot create or publish policies; domain validation does real DNS and cannot pass on localhost, so the fiduciary stays `PENDING`; setup and login are rate-limited per IP.
6. Test: `get_active_policy`, `record_consent`, `get_active_consent`, `validate_consent`, `withdraw_consent`, `submit_grievance`; invalid key returns 401, READ-only key cannot record.

### 5.2 Other five

**tsi-ledger** (smallest). Migrate `01_init.sql`. Ready: `setup_status`. Setup with no token, login (cookie), `create_app`, then seed accounts through `create_account` {account_code, account_name, account_type}. Test: balanced `post_entry` accepted, unbalanced rejected, `trial_balance`, `verify_audit`. The repo's `.env` lists `ADMIN_MASTER_KEY`, but nothing in `src/` reads it and compose does not pass it, so the library ignores it.

**tsi-privacy-vault.** Migrate `00`..`04`. Setup must use role `SYSTEM_ADMIN`. After login: `create_key` for credentials, `create_entity` per data type (flavours seen in the repo's fixtures script: `ID`, `DATA`, file), `create_permission` per key and entity. Optional `activate_key_anchor` for AWS KMS. Test: `store_data`, `fetch_data`, `search_data`, `shred_principal`, `get_erasure_certificate`, `verify_audit_chain`. The repo already has `examples/scripts/setup/setup-test-fixtures.sh`, which does this sequence. Also note: `_processor.tsi` maps `/api/admin/clients` to a class that does not exist, and a `register_client` schema exists but nothing handles it. Use `create_key`, not `register_client` **[V]**. Compose defaults `TSI_PRIVACY_VAULT_ENV` to `production`.

**tsi-compass.** Migrate `01`..`22`. Setup (`complete_setup`), login via `/api/v1/user`, optional `generate_api_key`. Its surface is very large (tickets, assets, risk, controls, DPIA, vendor portal, TPRM). v1 automates first-run and a smoke check only. Compose uses `pull_policy: always` and a Jetty 11 image, unlike the others.

**tsi-dx-node** (hardest). Needs two instances of one module. Create the external Docker network `tsi-p2p-net`; run two compose projects with their own env, ports and overlay `docker-compose.p2p.yml`. Per node: `registeradmin`, `admin_login`, generate a key and self-signed cert in Go (CN/SAN must match the hostname the peer uses), `import_node_certificate`; then each node `create_partner` with the other's public key; `accept_partnership`; then contracts (`propose_contract`, `invite_node`, `accept_contract`); optionally `create_app` plus `generate_api_key`. Test: `initiate_transfer`, `execute_sync_request`, and the negative cases. The repo's `examples/scripts/01`..`07` and `lib/common.sh` already encode this sequence with expected status codes. The compose file also contains a `mock-responder` test service; leave it out of the default stack.

**tsi-sign.** Compose builds from source, so first decide how to obtain an image (open question 4). Services: app, Postgres, `esign_sandbox` (built from `./sandbox`, shares a CA volume with the app). Generate a real keystore instead of the baked-in `changeit` one. Migrate `01`..`11`. Ready: `setup_status`. Setup (no token), login, `create_app` with `appName`, `appSlug`. Test: tenant `ping` returns `{"status":"ok"}`, `upload_document`, `seal_local`, `download_document`, `generate_legal_certificate`, and `initiate_esign` against the sandbox. A **real eSign CA** needs customer-supplied CA, ESP and ASP certificates and cannot be automated; the library only templates the `ESIGN_<CA>_*` settings.

### 5.3 Naming differences the catalog absorbs

| Idea | Variants found |
|---|---|
| JWT secret | `JWT_SECRET`, `TSI_PRIVACY_VAULT_JWT_SECRET`, `TSI_DX_NODE_JWT_SECRET` |
| Setup token | `TSI_BOOTSTRAP_TOKEN`, `TSI_PRIVACY_VAULT_BOOTSTRAP_TOKEN`, `TSI_DX_NODE_BOOTSTRAP_TOKEN`, none (ledger, sign) |
| Image variable | `IMAGE_NAME`, `APP_IMAGE`, or build only |
| Compose services | `postgres_db` + `jetty_app`, or `database` + `server` |
| Admin password minimum | 12 (cms), 10 (compass), 8 (sign), unchecked elsewhere |
| JSON casing | snake_case (most), camelCase (sign) |
| Secret field names | section 2.2 |

## 6. Speed targets

| Target | How |
|---|---|
| `init` under 1 minute | Sensible defaults, `--non-interactive`, secrets generated |
| Up and ready under 3 minutes (warm machine) | Parallel image pulls, readiness polling with backoff, migrations run from the library without waiting on container restarts |
| Clear failures | Preflight: Docker reachable, ports free, disk space, images pullable. Errors name the module and give a fix |
| Re-runs are safe | Idempotent setup, no duplicate apps, no silent key rotation |
| Reproducible | Pinned tags, `tsi.lock` |

## 7. Testing

**Library itself**
- Unit tests: catalog loading and validation, env and secret generation, immutable-secret rules, renderer output (golden files), migration ordering, cookie and Bearer session handling, bootstrap runner with a fake HTTP server using recorded responses.
- Validate rendered compose files with `compose-spec/compose-go`.
- Integration (build tag `integration`, real Docker): bring up each module alone and in combination, run bootstrap twice (second run must change nothing), run `tsitest`.
- A **contract check per module**: the catalog's calls are checked against that repo's `web/WEB-INF/validator/*.jschema` (or equivalent) in CI, so a module change that breaks provisioning is caught. Note some schemas are stale (vault `register_client`), so live runs stay the final check.

**What `tsitest` offers users:** liveness, readiness, per-module smoke probes (sections 5.1–5.2), cross-module checks later, JSON and JUnit output.

## 8. Milestones

| # | Milestone | Done when |
|---|---|---|
| M0 | Repo skeleton, Go toolchain, CI (lint, test, build), license | CI green. **Go is not installed on this machine today** (Docker 29.2.1 is) |
| M1 | Catalog schema; YAML for all six modules with env, ports, migrations (embedded), probes. Run each module once by hand to confirm every **[I]** item | Every cell in section 2 is verified live |
| M2 | Config, secrets, immutable-secret rules, state store, `tsi.yaml` | Idempotent re-run; permissions 0600 |
| M3 | Renderer and golden tests; shared, per-module and external DB modes | Valid compose output for every module combination |
| M4 | Engine, lifecycle (`Up`, `Down`, `Status`), preflight, migration runner | `tsi-ledger` comes up, migrated, from nothing |
| M5 | Readiness probes, logs multiplexer | `Up` reports ready accurately |
| M6 | Bootstrap runner with Bearer and cookie sessions; CMS and ledger recipes | Fresh stack to a working tenant credential, second run is a no-op |
| M7 | `tsitest` harness, smoke suites for CMS and ledger, `tsitest.Start(t)` | Suites pass on real Docker |
| M8 | Vault, sign (sandbox), compass recipes and suites | Each passes bootstrap and smoke tests twice |
| M9 | dx-node: multiple instances, shared network, certs, handshake, contract, transfer | Two nodes complete an async and a sync exchange |
| M10 | CLI (init, up, bootstrap, status, creds, test, logs, down), JSON output, `--plan`, release (goreleaser, Homebrew formula `tsi`, `go install github.com/tsicoop/tsi-cli/cmd/tsi@latest`) | Section 12 flows work end to end |
| M11 | Natural-language layer (section 13) | An agent provisions a stack from one sentence and passes tests, with no secret visible to the model |

Order of module work (smallest first): ledger, cms, vault, sign, compass, dx-node. The first thing to do is a spike: M1's live check plus M4's `ledger` bring-up, to confirm the Docker approach before building more.

## 9. Dependencies

`compose-spec/compose-go/v2` (validate output), `jackc/pgx/v5` (migrations), `spf13/cobra` (CLI), `charmbracelet/huh` (prompts), `gopkg.in/yaml.v3`, `software.sslmate.com/src/go-pkcs12` (the Go standard library cannot write PKCS#12), stdlib `crypto/x509`, `embed`, `net/http/cookiejar`, `text/template`.
Engine v1 shells out to `docker compose`. A Docker SDK engine can follow behind the same interface.

## 10. Risks

| Risk | Mitigation |
|---|---|
| Module APIs change between versions | Pin tags, embed migrations per tag, contract checks in CI |
| **[I]** items turn out wrong | M1 verifies each by a live run before dependent code is written |
| Images missing or private (`tsi-sign`, possibly others) | Open question 4. Support `build:` from a local checkout as a fallback |
| Losing one-time credentials or immutable keys | State file, refuse silent regeneration, clear recovery messages |
| First-caller admin setup on ledger and sign | Localhost binding until bootstrap completes |
| Port clashes on a dev machine | Preflight, `PortOffset`, per-module overrides |
| Secrets leaking into logs or git | 0600 files, `.gitignore` entry on init, redaction in all output |
| dx-node networking and certificate hostnames | Dedicated two-node profile, hostnames derived from instance names |

## 11. Findings for the module maintainers

Collected while reading the repos (none are blockers for `tsi-cli`):

1. No module has a health endpoint or Docker `HEALTHCHECK`.
2. Schemas are not in the images, so every user must mount `db/` (or use `tsi compose`).
3. `tsi-ledger` and `tsi-sign` accept the first setup caller with no token. The others require a bootstrap token.
4. `tsi-sign` has no published image reference, and its image bakes a keystore password of `changeit`.
5. `tsi-privacy-vault`: `/api/admin/clients` is mapped to a missing class, and a `register_client` schema is unused.
6. `tsi-ledger/.env` lists `ADMIN_MASTER_KEY`, which nothing reads.
7. `tsi-dpdp-cms/.help` in the repo root contains what look like a real API key and secret. Worth rotating and removing from history.
8. Env variable names, credential field names and JSON casing differ across modules (section 5.3). A shared convention would simplify clients.
9. Only three repos carry git tags, which makes version pinning harder.

## 12. CLI sketch

A design sketch only. Flags can change.

```bash
$ tsi compose init
? Select modules:  [x] tsi-ledger  [x] tsi-privacy-vault  [ ] tsi-dpdp-cms  [ ] tsi-sign  [ ] tsi-compass  [ ] tsi-dx-node
? Database:        > One shared local PostgreSQL   (or: one per module, existing database)
? Admin email:     admin@acme.test
✔ Generated secrets (JWT, master key, index secret, bootstrap tokens)
✔ Wrote docker-compose.tsi.yml, .env.tsi (0600, gitignored), tsi.yaml

$ tsi compose up -d --bootstrap
✔ postgres        healthy
✔ tsi-ledger      ready  http://127.0.0.1:8086
✔ tsi-privacy-vault ready  http://127.0.0.1:8081
✔ migrations applied (ledger 1, vault 5)
✔ bootstrap: ledger admin created, app "default" created, API key saved
✔ bootstrap: vault admin created, entities and API key created, permissions granted

$ tsi compose test
✔ ledger   balanced entry accepted, unbalanced rejected, trial balance matches   4/4
✔ vault    store, fetch, search, erase, audit chain verified                      5/5
9 passed, 0 failed (22s)
```

| Command | What it does |
|---|---|
| `tsi compose init [--non-interactive --modules a,b --config tsi.yaml]` | Writes the files. Never overwrites secrets without `--force` |
| `tsi compose up [-d] [--build] [--bootstrap]` | Starts, optionally provisions |
| `tsi compose bootstrap [--rotate-key <module>]` | Provisioning only. Re-run changes nothing |
| `tsi compose status` | Container health, ports, readiness |
| `tsi compose creds [--module m] [--show-secret]` | Shows saved tenant credentials (masked by default) |
| `tsi compose test [--module m] [--format json\|junit]` | Smoke tests |
| `tsi compose logs [-f] [--tail N] [module...]` | Combined logs |
| `tsi compose down [--volumes]` | Stops. `--volumes` also wipes data and state |

Declarative form for CI:

```yaml
modules: [tsi-ledger, tsi-privacy-vault]
database: {mode: shared}
bootstrap:
  tsi-ledger:         {admin: {name: Admin, email: admin@acme.test}, app: {name: default}, accounts: ./coa.csv}
  tsi-privacy-vault:  {admin: {username: admin, email: admin@acme.test}, entities: [{code: PAN, flavor: ID}]}
```

Every command also supports `--non-interactive`, `--output json` and `--plan` (dry run), and returns stable exit codes and typed errors with a suggested fix. These are cheap to build now and are the foundation for section 13.

## 13. Natural-language provisioning (last milestone)

Only after the CLI and library are solid. Three options, in order of preference:

1. **An agent runs the CLI.** Works as soon as the CLI exists.
2. **An MCP server over the library** (`tsi mcp`) with tools `plan`, `init`, `up`, `bootstrap`, `status`, `test`, `creds` (masked), `logs`, `down`. Structured in and out.
3. **`tsi compose ai "<request>"`** which writes a `tsi.yaml`, shows the plan, and runs it on approval. Optional.

Rules: always plan, then confirm; destructive actions (`down --volumes`, key rotation) need explicit confirmation; **secret values never reach the model** (tools return handles like "key for app default (stored)"); local and dev only by default; every action written to `.tsi/audit.log`; only the listed operations are exposed, no arbitrary shell or free-form API calls. The model can help with provisioning but not with decisions such as which consent purposes a policy should contain.

## 14. Open questions

1. One shared Postgres for all modules, or one per module as the repos ship?
2. Which exact readiness call should be used for vault and dx-node? (Needs the live check in M1.)
3. Where do the migrations come from at build time: vendored copies in this repo, or fetched from each module's tag?
4. How do users get a `tsi-sign` image (and are the other `tsicoop/*:latest` images actually published and public)?
5. Should `up` bootstrap by default, or only with `--bootstrap`?
6. Should the secrets-never-reach-the-model rule be absolute?
7. Is a real eSign CA setup (tsi-sign) in scope, or only the bundled sandbox?
8. Should dx-node's two-node setup be a general "multiple instances" feature, or a dedicated demo profile?
9. Is `tsi compose ai` wanted, or is the MCP server plus a documented agent workflow enough?
10. Should the library ask the module owners to add a health endpoint and setup tokens (section 11), or live with the workarounds?
