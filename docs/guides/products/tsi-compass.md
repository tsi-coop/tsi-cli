# tsi-compass

IT GRC and compliance operations. Version **v0.4.3**.

Generic topics (credentials, databases, ports, secrets) are in the [guides index](../README.md). This page is what is specific to this module.

| | |
|---|---|
| Image | `tsicoop/tsi-compass:v0.4.3` |
| Host port | 8385 (upstream 8085, plus the default offset of 300) |
| Database | `tsi_compass`, role `tsi_compass` |
| Database scripts | 22 files from `tsi-coop/tsi-compass`, pinned to commit `ccabfff` (the repository has no git tags; this is the v0.4.3 release commit) |
| Credentials from `tsi compose creds` | the administrator login |

## Run it

```bash
tsi compose init --modules tsi-compass --non-interactive
tsi compose up -d --bootstrap
tsi compose test
tsi compose creds --show-secret      # admin email and password
```

Open the URL from `tsi compose status` and sign in with those credentials.

## What bootstrap creates

| Step | Result |
|---|---|
| Setup | The first administrator, with the name and email from `tsi.yaml` and a generated 28-character password (the module needs at least 10) |
| Login | An administrator session |

That is all by default. Compass is a large application (tickets, assets, risks, controls, DPIA, data governance, third-party risk, vendor portal), and `tsi` does not create business data. The migrations seed reference data such as asset categories; everything else you add in the console.

## API keys

The module can issue API keys, and `tsi` can request one:

```yaml
bootstrap:
  tsi-compass:
    api_key: {create: true, name: my-integration}
```

**In v0.4.3 these keys cannot authenticate any request**, so `tsi` does not create one unless you ask, and the credential `tsi` gives you is the administrator login. The cause is in the module: key generation stores a hash in columns that request authentication does not read. See [Notes for the module maintainers](../maintainer-notes.md). Use the administrator login for scripts:

```bash
BASE=http://127.0.0.1:8385
TOKEN=$(curl -s "$BASE/api/v1/admin/user" -H 'Content-Type: application/json' \
  -d '{"_func":"login","email":"admin@example.org","password":"<password>"}' | jq -r .token)

curl -s "$BASE/api/v1/admin/ops" -H 'Content-Type: application/json' -H "Authorization: Bearer $TOKEN" \
  -d '{"_func":"list_assets"}'
```

Every call is `POST /api/v1/admin/<service>` with a `_func`. The running application has the full reference at `/tour/api.html`.

## Environment variables you can override

| Variable | Default | Notes |
|---|---|---|
| `BRAND_NAME` | `TSI COMPASS` | Shown in the UI |
| `ALLOWED_ORIGINS` | `http://localhost:<port>,http://127.0.0.1:<port>` | Browser origins the admin API accepts |
| `TSI_VENDOR_PORTAL_ENABLED` | `false` | Turns on the vendor portal |
| `TSI_COMPASS_ENV` | `local` | |
| `TSI_EXPORT_PATH` | `/var/lib/tsi-compass/exports/` | |

Generated for you: `JWT_SECRET`, `DB_ENCRYPTION_KEY` and `TSI_LOOKUP_SALT` (the last two are [permanent](../secrets-and-state.md#permanent-secrets)), the setup token and the database password.

## Smoke tests

`tsi compose test --module tsi-compass` runs six checks: readiness; setup reports that the first administrator exists; a wrong password is refused (401); an admin call without a session is refused (401); the migrations seeded the asset categories; and an asset can be created, listed and deleted. The asset it creates is removed even if a later step fails.

## Manual setup

Start without `--bootstrap`, take the token from `.tsi/env/tsi-compass.env` (`TSI_BOOTSTRAP_TOKEN`), and use the module's setup page. The administrator password must be at least 10 characters. See [Bootstrap and signing in](../bootstrap-and-signing-in.md), including how to let `tsi` test a stack you set up by hand.

## Things to know

- **Sign-in attempts are rate limited** (HTTP 429 after repeated failures). Wait a few minutes.
- The module has a password recovery flow (an administrator reset, or a recovery passphrase set per user) described in its README.
- `tsi` does not exercise the vendor portal, third-party risk or data governance areas.
