# Notes for the module maintainers

Found while building and testing the `tsi` integrations. None of these blocks `tsi`; each has a workaround in its catalog entry. They are listed because fixing them upstream would simplify `tsi` and help anyone integrating the modules.

## All modules

- **The schema is not in the images.** The database scripts live only in each repository's `db/` folder and are mounted into PostgreSQL. A database has to be prepared outside the image. `tsi` downloads the pinned files and verifies them. If the images carried their migrations (for example `COPY db /opt/tsi/db`) or the applications applied them on startup, an image and its schema could not disagree.
- **No health endpoint.** No module has `/health` or a Docker `HEALTHCHECK`. `tsi` uses a cheap unauthenticated call that touches the module as its readiness probe, a different one per module.
- **Different names for the same idea.** Environment variable names (`JWT_SECRET`, `TSI_PRIVACY_VAULT_JWT_SECRET`, `TSI_DX_NODE_JWT_SECRET`), setup-token variables, response field names for API credentials (`key_id`/`raw_api_key`, `api_key_id`/`raw_api_secret`, `api_key`/`api_secret`) and password minimums (12, 10, none, a complexity rule) all differ. A shared convention would simplify clients.

## `tsi-compass`

- **Generated API keys cannot authenticate.** Key generation (`ApiKey.java`) inserts into `api_keys (name, description, key_prefix, api_key_hash, api_secret_hash, created_by)`. Request authentication (`InputProcessor.isValidApiClient` and `retrievePerms`) reads `api_keys WHERE id = ?` with columns `key_value`, `status` and `fiduciary_id`, and expects the key to be a UUID. These are leftovers from the consent module. The documented machine access with `X-API-Key` and `X-API-Secret` does not work in v0.4.3: calling `/api/v1/client/consent` with a generated key returns HTTP 500 (`Invalid UUID string`), and `/api/v1/admin/ops` returns 401.
- **No git tags.** The v0.4.3 image is built from commit `ccabfff` ("v0.4.3 release"). Tags would let consumers pin readable versions.
- **`check_setup` hides database errors.** It answers `{"setup_done": false}` with HTTP 200 when the query fails.

## `tsi-dx-node`

- **Re-registering is destructive.** Creating a partner that already exists resets its status to `Pending`; creating a contract with an existing name creates a second one. Neither returns a conflict.
- **TLS certificate names are baked in.** The image's certificate is valid only for `node-a-server-1`, `node-b-server-1`, `localhost` and `tsi-dx-node`. The code sets `jdk.internal.httpclient.disableHostnameVerification` after the HTTP client is created, which has no effect, so a node verifies the peer's name and a peer under any other name fails the handshake with "Sovereign Trust Denied: Peer certificate mismatch" (the message is produced for any `SSLHandshakeException`, not only a certificate mismatch).
- **Email domains.** Setup accepts `.local` addresses and login rejects them.
- **Setup status codes.** A first-run call with a wrong setup token answers 401 before the database is touched, which is a good readiness probe; the example scripts expect 409 for an initialised node.

## `tsi-privacy-vault`

- **Some errors return HTTP 200.** The real status is in the body (`"status": 4xx`). The module's own example scripts already handle this.
- **Setup uses `create_user`.** The admin setup route accepts only `_func: create_user` with `role: SYSTEM_ADMIN` and the setup token; a second call is answered with 403 ("Setup Phase Expired"), not 409. Body validation (`active`, `email`) runs before the token check.
- **`/api/admin/clients`** is mapped to a class that does not exist, and a `register_client` schema is unused. Use `create_key`.
- **`TSI_PRIVACY_VAULT_ENV` defaults to `production`** in the compose file, which marks the console cookie `Secure` and breaks plain-http local use.

## `tsi-dpdp-cms`

- **Publishing a policy does not activate it.** It becomes `UNDER_REVIEW`, and a draft ROPA entry is created; publishing that entry activates the policy. The setup and integration guides could say so.
- **Validation order.** Request schema validation happens after authentication (good), but the error for a missing field in `create_app` mentions a field (`data_categories_processed`) the code does not require.
