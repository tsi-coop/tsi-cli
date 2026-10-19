# Composable Software: Take Only What You Need

You can order a fixed thali. Or you can pick the dishes you like.

Software works the same way. Big packages give you everything, even what you don't need. **Composable** software lets you pick only the parts you want.

## What is composability?

Software built from small parts. Each part does one job. You can add, remove, or swap a part without breaking the rest.

Think LEGO, not concrete.

## Why it helps

- **Pay for what you use.** No unused features to carry.
- **Start small.** Add more parts later.
- **Fix one part at a time.** The rest keeps working.
- **Keep your existing systems.** Nothing to throw away.
- **Move faster.** Small parts are easy to learn and test.

## The catch

Many small parts must be put together. Which ones do you need? What settings and secret keys does each one want? How do you create the first admin, the first app, the first API key? How do you know it all works?

Done by hand, this takes hours or days, for every module and every developer.

## The TSI building blocks

TSI Coop already offers its products as separate services. Each one runs on its own, with its own database, and talks to your apps through an API:

| Block | Job |
|---|---|
| **tsi-dpdp-cms** | Records and checks consent |
| **tsi-privacy-vault** | Stores personal data safely, with erasure and audit |
| **tsi-ledger** | Double-entry accounting records with an audit trail |
| **tsi-compass** | IT governance, risk and compliance work |
| **tsi-dx-node** | Secure data exchange between organisations |
| **tsi-sign** | Seals documents and collects eSignatures |

They have no dependency on each other. Pick one, or pick three.

## Introducing tsi compose

**tsi compose** is the command that does the assembling for you. One tool, the same steps, for every block:

```
tsi compose init       # pick your blocks
tsi compose up -d      # start them, set them up
tsi compose test       # check they really work
tsi compose down       # clean up
```

It creates strong secret keys. It prepares each database. It creates the first admin, the first app and the first API key, using each product's own API. Then it runs quick tests with those keys.

Your secrets stay on your machine.

## In short

Pick what you need. Start in minutes. Grow when you are ready.
