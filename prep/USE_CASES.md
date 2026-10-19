# tsi compose: What We Could Do With It

Companion to `IMPLEMENTATION_PLAN.md`. Module behaviour below comes from reading the repos (`tsi-dpdp-cms`, `tsi-privacy-vault`, `tsi-ledger`, `tsi-compass`, `tsi-dx-node`, `tsi-sign`). `tsi compose` itself is **planned, not built**. Everything described as a capability is a goal of the plan.

## What it replaces

Today each product is set up by hand: write an env file, mount its `db/` folder into Postgres, start it, open a browser console, create the first admin, create an app, generate an API key, copy the secret (shown only once). Every product has slightly different steps, setup tokens and variable names. `tsi compose` does all of that from one command.

## For a developer or architect evaluating TSI

- **A working module in minutes.** `init`, `up`, `test`. Pick any of the six products. Secrets are generated, the database is prepared, and you get a working API key at the end.
- **Take only what you need.** The six products don't depend on each other. Run only the consent service, or only the ledger.
- **Try, then discard.** `down --volumes` removes containers, data and saved credentials together.

## For ISVs and in-house teams building on TSI

- **Integration tests against the real thing.** `tsitest.Start(t, ...)` from `go test` starts a real module, creates a tenant key, hands you the URL and credentials, and cleans up. Examples of what teams could test without mocks:
  - **Consent:** record, check, withdraw and re-check a consent.
  - **Vault:** store a record, fetch it by reference, erase it, verify the audit chain.
  - **Ledger:** a balanced entry is accepted, an unbalanced one is rejected, the trial balance matches.
  - **Sign:** upload a document, seal it, download it, check the certificate.
- **CI pipelines.** The same stack runs in CI, with JUnit or JSON output.
- **Reproducible environments.** Pinned versions and a lock file mean the same stack on every machine.

## For DevOps

- **Generated, reviewable files.** A compose file and an env file you can read and keep in your own repository.
- **One place for status and logs** across all running modules.
- **Local, shared or existing database.** One Postgres for all modules, one per module, or your own.
- **Secrets stay local, with safety rules built in.** Files are private (0600) and git-ignored. Permanent keys (the database encryption key, the vault master key) are never silently regenerated. First-time admin setup is done before the port is reachable from outside the machine.

## For the TSI Coop itself

- **One onboarding path** for all products, instead of six different READMEs.
- **A smoke-test suite per release.** The same checks users run can run against every module version before it ships.
- **Catches inconsistencies early.** Writing the catalog exposes gaps such as modules with no health endpoint, or setup that accepts the first caller without a token (see section 11 of the plan).
- **New module, new file.** Adding a product means adding a catalog description, not new Go code.

## Things we could build on top

- A demo environment that resets with one command.
- Preview stacks per pull request in an ISV's CI.
- A conformance check an ISV runs to show their product works with TSI modules.
- Two `tsi-dx-node` instances on one machine, with generated identity certificates, a partner handshake, a contract and a sample transfer.

## Limits

- **Local and development use first.** It is not a production orchestrator, and there is no Kubernetes output in v1.
- **Needs Docker** (or a compatible engine). Go also has to be installed to build it; it isn't installed on this machine today.
- **Some steps cannot be automated.** A real eSign CA for `tsi-sign` needs certificates from the customer. A fiduciary's domain validation in `tsi-dpdp-cms` needs real DNS, so it stays pending on localhost.
- **Compass has a very large surface** (tickets, assets, risk, controls, DPIA, vendor portal). v1 would automate first-run and a smoke check only.
- **Unverified details remain.** The plan marks inferred items **[I]**. A live run of each module settles them before the code depends on them.

## To revisit

- Which audience matters most? Prioritising the `tsitest` harness would move the ISV use case earlier in the milestones.
- Whether to start with ledger and consent (simplest, and most useful for ISVs), as the plan suggests.
