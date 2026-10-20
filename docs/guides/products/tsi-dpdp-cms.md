# tsi-dpdp-cms

Consent management under the DPDP Act. Version **v0.5.3**.

Generic topics (credentials, databases, ports, secrets) are in the [guides index](../README.md). This page is what is specific to this module.

| | |
|---|---|
| Image | `tsicoop/tsi-dpdp-cms:v0.5.3` |
| Host port | 8380 (upstream 8080, plus the default offset of 300) |
| Database | `tsi_cms`, role `tsi_cms` |
| Database scripts | 13 files from `tsi-coop/tsi-dpdp-cms` at tag `v0.5.3`, plus the `app.enc_key` database setting |
| Credentials from `tsi compose creds` | admin login, and an API key and secret for the app |

## Run it

```bash
tsi compose init --modules tsi-dpdp-cms --non-interactive
tsi compose up -d --bootstrap
tsi compose test
tsi compose creds --show-secret
```

## What bootstrap creates

| Step | Result |
|---|---|
| Setup | The first administrator (role ADMIN). The password is generated, 28 characters |
| Login | An administrator session, used for the steps below |
| Data fiduciary | `Default Fiduciary`, on `example.com`, with `consent.example.com` as its CNAME. The administrator's email is its contact email |
| App | An app named `default` under that fiduciary |
| API key | A key with scopes `READ` and `WRITE` for the app. The secret is shown by the module once, and saved by `tsi` |
| Policy | A draft consent policy `tsi-default-policy` (jurisdiction `IN`) with one purpose, `purpose_service` |
| Publish and activate | Publishing a policy makes it `UNDER_REVIEW`. The module also creates a draft ROPA entry for it; `tsi` publishes that entry, which activates the policy |

After that, `get_active_policy` returns the policy and consent can be recorded against it.

## Settings

Under `bootstrap: tsi-dpdp-cms:` in `tsi.yaml` ([how these work](../configuration.md#bootstrap-first-run-settings); they apply the first time only):

```yaml
bootstrap:
  tsi-dpdp-cms:
    fiduciary:
      name: Acme Pvt Ltd
      contact_person: Priya Rao
      email: privacy@acme.example
      phone: "9999999999"
      primary_domain: acme.example      # must look like name.tld
      cms_cname: consent.acme.example
    app:
      name: web
      email: privacy@acme.example
      phone: "9999999999"
      processing_purposes: Consent management
    api_key:
      description: web app
      permissions: [READ, WRITE]        # PURGE is also a scope
    policy:
      id: acme-policy
      jurisdiction: IN
      content: {...}                    # the policy document, keyed by language ("en")
```

The default `policy.content` is a one-purpose English policy. If you give `policy.content` it **replaces** the default entirely (it is not merged), so write the whole document. For real policy documents see `examples/policy/` in the module's repository.

## Environment variables you can override

```yaml
overrides:
  tsi-dpdp-cms:
    BRAND_NAME: Acme Privacy        # at most 12 characters, or the module will not start
    ALLOWED_ORIGINS: https://consent.example.com
```

| Variable | Default | Notes |
|---|---|---|
| `BRAND_NAME` | `TSI DPDP CMS` | Shown in the UI. 12 characters maximum |
| `ALLOWED_ORIGINS` | `http://localhost:<port>,http://127.0.0.1:<port>` | Browser origins the admin API accepts |
| `TSI_DPDP_CMS_ENV` | `local` | |
| `TSI_EXPORT_PATH` | `/var/lib/tsi/exports/` | |

Generated for you: `JWT_SECRET`, `DB_ENCRYPTION_KEY` and `TSI_LOOKUP_SALT` (the last two are [permanent](../secrets-and-state.md#permanent-secrets)), `TSI_BOOTSTRAP_TOKEN`, and the database password. A PKCS#12 keystore is generated and mounted; the module reads it only when `TSI_DPDP_CMS_ENV=production`, which `tsi` does not set.

## Using the API

Client calls are `POST /api/v1/client/<service>` with the API key and secret. Take the values from `tsi compose creds -o json --show-secret`.

```bash
BASE=http://127.0.0.1:8380
KEY=...; SECRET=...
H=(-H 'Content-Type: application/json' -H "X-API-Key: $KEY" -H "X-API-Secret: $SECRET")

# the active policy
curl -s "$BASE/api/v1/client/policy" "${H[@]}" \
  -d '{"_func":"get_active_policy","fiduciary_id":"<fiduciary_id from creds>","jurisdiction":"IN"}'

# record a consent
curl -s "$BASE/api/v1/client/consent" "${H[@]}" -d '{
  "_func":"record_consent","user_id":"user-1","policy_id":"tsi-default-policy","language_selected":"en",
  "data_point_consents":[{"data_point_id":"purpose_service","consent_granted":true,
    "purpose_agreed_to":"Service delivery","timestamp_updated":"2026-01-01T00:00:00Z"}]}'

# check it, then withdraw it
curl -s "$BASE/api/v1/client/consent" "${H[@]}" \
  -d '{"_func":"validate_consent","user_id":"user-1","required_purpose_id":"purpose_service"}'
curl -s "$BASE/api/v1/client/consent" "${H[@]}" -d '{"_func":"withdraw_consent","user_id":"user-1"}'
```

The administrator API is `POST /api/v1/admin/<service>` with `Authorization: Bearer <token>` from `POST /api/v1/admin/operator` (`{"_func":"login","identifier":"<email>","password":"..."}`). The module's API reference is `docs/api/` in its repository.

## Smoke tests

`tsi compose test --module tsi-dpdp-cms` runs seven checks: readiness; a wrong API key is refused with 401; the active policy is returned; a consent can be recorded; it validates as granted; withdrawing it makes it validate as not granted; and a `READ`-only key can read but is refused when it tries to record consent. They create their own consents and a temporary key.

## Manual setup

Start without `--bootstrap`, take the token from `.tsi/env/tsi-dpdp-cms.env` (`TSI_BOOTSTRAP_TOKEN`), and use the module's setup page (the module's own configuration notes put it at `/console/setup/init.html`). The administrator password must be at least 12 characters. See [Bootstrap and signing in](../bootstrap-and-signing-in.md).

## Things to know

- **Domain validation stays pending on localhost.** The module checks a DNS TXT record for the fiduciary's domain, which cannot pass locally. The fiduciary is created and usable; its validation status is `PENDING`.
- **Roles:** only an `ADMIN` can create fiduciaries and keys. An `OPERATOR` cannot create or publish policies.
- **Sign-in and setup are rate limited per address.** Repeated wrong passwords give HTTP 429 for a few minutes.
- **Policies need a ROPA entry to go live.** If you publish a policy yourself in the console, it stays `UNDER_REVIEW` until its ROPA entry is published.
