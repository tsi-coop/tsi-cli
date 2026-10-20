# tsi-dx-node

Peer-to-peer governed data exchange between organisations. Version **v0.3.0**.

Generic topics (credentials, databases, ports, secrets) are in the [guides index](../README.md). This page is what is specific to this module.

| | |
|---|---|
| Image | `tsicoop/tsi-dx-node:v0.3.0` |
| Host ports | 8382 for the first node, 8383 for the second (upstream 8082 and 8083, plus the default offset of 300) |
| Database | one per node: `tsi_dx_node_node_a`, `tsi_dx_node_node_b` |
| Database scripts | 5 files from `tsi-coop/tsi-dx-node` at tag `v0.3.0` |
| Credentials from `tsi compose creds` | the administrator login of each node; the sending node also has an API key and secret |

## The idea

A node sends governed data packages to another node under a **data contract**. The receiving node defines the contract (the schema, which fields are personal data, how they are transformed on arrival) and invites the sender. The sender accepts, and an application on the sender can then send packages. Trust between the nodes is a public key each one registers for the other.

To try this you need two nodes, so `tsi` runs them as a pair.

## Run it as a pair

```bash
tsi compose init --modules tsi-dx-node:node-a,tsi-dx-node:node-b --non-interactive
tsi compose up -d --bootstrap
tsi compose test
tsi compose creds --show-secret
```

or in `tsi.yaml`:

```yaml
modules:
  - tsi-dx-node:node-a        # the sender (first instance)
  - tsi-dx-node:node-b        # the receiver (second instance)
```

The names after the colon are yours to choose, but the **first instance is the sender and the second the receiver**. Their node ids are the names in capitals (`NODE-A`, `NODE-B`). You can also run a single node on its own: it is set up with its identity and administrator, and the exchange checks are skipped.

## What bootstrap does

It runs in phases across both nodes, so every node exists before anyone shakes hands:

| Phase | Where | What happens |
|---|---|---|
| 1 | each node | Initialise the node and create its administrator. Give it a generated identity (RSA key and certificate) and activate it. Register the other node as a partner, with that node's public key. Looks first, and registers only if absent |
| 2 | sender, then both | The sender starts the handshake. Wait until both nodes show the partner as `Active` |
| 3 | receiver | Define the data contract, then invite the sender. Looks first, and creates only if absent |
| 4 | sender | Find the invitation, accept the contract, wait until it is `Active` |
| 5 | sender | Register an application authorised for the contract, and issue it an API key |

The generated identities are in `.tsi/certs/<instance>.pem` and `.key`. They are created once and never replaced, because the other node has pinned the public key.

## Settings

Under `bootstrap:` in `tsi.yaml`, keyed by `tsi-dx-node` (both nodes) or by an instance name ([how these work](../configuration.md#bootstrap-first-run-settings); they apply the first time only). The contract is created on the receiver:

```yaml
bootstrap:
  tsi-dx-node:
    contract:
      name: Weekly loan applications
      description: Applications sent for scoring
      retention_days: 30
      pii_fields: [applicant_id, mobile]
      governance_rules:
        pii_anonymization: {applicant_id: HASH, mobile: MASK}
      schema:                       # a JSON Schema; mark personal data with x-pii: true
        type: object
        required: [applicant_id, mobile, product, amount]
        properties:
          applicant_id: {type: string, x-pii: true}
          mobile: {type: string, x-pii: true}
          product: {type: string, enum: [loan, credit_card, insurance]}
          amount: {type: number}
    sample:                         # the payload the smoke test sends; it must satisfy the schema
      {applicant_id: A-1001, mobile: "9876543210", product: loan, amount: 250000}
    app: {name: scoring-app}
```

The defaults describe a loan application like the module's own example. `contract.schema`, `contract.governance_rules` and `sample` **replace** the defaults when you give them (they are not merged), so write them in full. Other `contract` keys you leave out keep their defaults.

## Environment variables you can override

| Variable | Default | Notes |
|---|---|---|
| `TSI_DX_NODE_ENV` | `development` | Upstream defaults to `production`, which marks the console cookie `Secure` and breaks plain-http use |
| `TSI_EXPORT_PATH` | `/var/lib/tsi/exports/` | |

Generated for you, per node: the JWT secret, the setup token, the administrator password (generated to meet the node's rule: upper, lower, digit and one of `!@#$%^&*()_+`) and the database password.

## Sending a package yourself

The sender's API key and secret come from `tsi compose creds --module node-a --show-secret`. Look up the contract id with the key, send, and follow the transfer:

```bash
BASE=http://127.0.0.1:8382           # the sending node
H=(-H 'Content-Type: application/json' -H "X-API-Key: $KEY" -H "X-API-Secret: $SECRET")

curl -s "$BASE/api/client/dx" "${H[@]}" -d '{"_func":"list_contracts"}'

curl -s "$BASE/api/client/dx" "${H[@]}" -d "{\"_func\":\"initiate_transfer\",\"contract_id\":\"<contract_id>\",
  \"file_name\":\"apps.json\",\"file_data\":\"$(base64 -w0 payload.json)\"}"      # the reply has a transfer_id

curl -s "$BASE/api/client/dx" "${H[@]}" -d '{"_func":"get_transfer_status","transfer_id":"<transfer_id>"}'   # Delivered
```

On the receiver, an administrator sees it with `list_transfers` and `get_transfer_payload` on `POST /api/admin/transfers` (`Authorization: Bearer <token>` from `POST /api/admin/login` with `{"_func":"admin_login","email":"...","password":"..."}`). The payload is stored **governed**: with the sample contract, `applicant_id` is hashed and `mobile` is masked. The module's `docs/guides/` and `examples/scripts/` in its repository explain the exchange step by step.

## Smoke tests

`tsi compose test` runs six checks on each node. Four apply to every node: readiness, the node reports its identity, the partner is `Active`, and the contract is `Active`. Two run only on the sender and show as skipped on the receiver: a wrong API secret is refused with 401, and **a real package is sent from the sender and checked on the receiver**: it must reach `Delivered` on the sender and `Received` on the receiver, with the personal-data fields transformed as the contract says and a non-personal field unchanged.

## Manual setup

Possible but long: each node needs an identity, partner registration, a handshake and a contract. For a manual walk-through use the module's own `examples/scripts/` and local testing guide. To do only the first step by hand, start without `--bootstrap`, take each node's token from `.tsi/env/<instance>.env` (`TSI_DX_NODE_BOOTSTRAP_TOKEN`), and use the node's setup page. The administrator password must meet the complexity rule above. See [Bootstrap and signing in](../bootstrap-and-signing-in.md).

## Limits

- **Two nodes at most**, answering to the names `node-a-server-1` and `node-b-server-1` on the stack's network. The image ships a built-in TLS certificate that is valid only for those names (and `localhost`), and a node checks its peer's certificate against the name it connects to. `tsi` gives each instance the right network alias. For other names you would need to supply your own transport certificates, which `tsi` does not do.
- **Asynchronous exchange only.** The synchronous request/response lane needs a responder service for the contract to call; `tsi` does not set one up.
- The nodes talk to each other over the stack's private network, on the module's P2P port (8443). It is not published on the host.
- Re-registering a partner or re-creating a contract by hand in the console can reset the partner to `Pending` or create a duplicate contract. `tsi` avoids this by looking first; do the same when you work by hand.
