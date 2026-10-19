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

Many small parts must be put together. Which ones do you need? What does each depend on? What settings and keys? In what order do they start?

Doing this by hand takes days. So composable parts need a good way to assemble them.

## Why DPDP compliance fits

The DPDP Act needs a few clear jobs: consent, protecting personal data, signatures, secure data exchange, and proof. Some companies need all of them. Others need two.

You don't need one giant platform. You need the right blocks.

## Introducing tsi compose

The TSI Coop offers each job as a separate backend service:

| Block | Job |
|---|---|
| **tsi-dpdp-cms** | Consent records |
| **tsi-privacy-vault** | Keeps personal data safe |
| **tsi-sign** | eSign documents |
| **tsi-dx-node** | Secure data exchange |
| **tsi-compass** | Compliance evidence |

They have no screens. They plug into your own apps and keep your login and gateway as they are.

**tsi compose** assembles them for you:

```
tsi compose init     # pick your blocks
tsi compose up -d    # start them
tsi compose status   # check they work
tsi compose down     # clean up
```

It adds the dependencies, creates the setup files, and generates strong keys. Your secrets stay on your machine.

## In short

Pick what you need. Start in minutes. Grow when you are ready.
