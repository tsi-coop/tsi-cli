# tsi-compose: What We Could Do With It

Companion to `IMPLEMENTATION_PLAN.md`. Capabilities marked **(spec)** come from `TSI Compose_ Functional Architecture.docx`; **(plan)** are additions proposed in the implementation plan.

## For a developer or architect evaluating TSI

- **Working DPDP backend in minutes (spec).** `tsi compose init`, pick modules, `up -d`. Consent management, PII tokenization, eSign, peer-to-peer exchange and compliance logging run locally with no manual Docker or config work.
- **Adopt only what you need (spec).** Take just the consent service or just the vault. Dependencies are resolved automatically; existing gateway and IAM stay untouched.
- **Try before you commit.** Spin up a throwaway stack, explore the APIs, then `down --volumes` and leave nothing behind.

## For ISVs and in-house teams building on TSI

- **Integration tests against real modules (plan).** `tsitest.Start(t, WithModules("tsi-dpdp-cms"))` from `go test` brings up a real module, waits for readiness, returns its URL and tears down afterwards. Likely the highest-value capability for ISVs: compliance flows tested without mocks.
- **CI pipelines (plan).** The same stack runs in CI with JUnit/JSON output, so regressions in how a product talks to TSI modules fail the build.
- **Reproducible environments (plan).** Pinned image versions plus a lockfile give every developer and CI run the same stack.

## For DevOps

- **Generated, reviewable deployment files (spec).** Deterministic `docker-compose.tsi.yml` and `.env.tsi` that can be committed or adapted for your own pipeline.
- **Unified status and logs (spec).** `status` shows health, ports and readiness across modules; `logs` gives one color-coded stream.
- **Local or existing databases (spec).** Throwaway Postgres container, or point at the enterprise instance.
- **Secrets stay local (spec).** Generated on the host, never transmitted; `tsi-sign` pinned to `StoreToDB=false`.

## For the TSI Coop itself

- **Standard onboarding path.** Docs and demos reduce to "run these three commands".
- **Module smoke-test suite (plan).** The probes that verify a user's stack can also run against every module release, catching broken images or changed endpoints before users do.
- **Catalog grows by adding a file (plan).** A new module is a YAML descriptor, not new Go code.

## Things we could build on top

- A "compliance sandbox" demo environment that resets with one command.
- Per-pull-request preview stacks in an ISV's CI.
- A conformance check an ISV runs to show their product works with TSI modules.
- Two `tsi-dx-node` instances on one machine to demo peer-to-peer exchange with generated dev certificates.

## Limits

- Local and dev-oriented for v1; not a production orchestrator, no Kubernetes output.
- Smoke tests depend on module API details not yet reviewed; these must come from the module repos.
- Requires Docker (or a compatible engine) on the machine.

## To revisit

- Which audience matters most? Prioritizing the `tsitest` harness would move the ISV use case earlier in the milestones.
