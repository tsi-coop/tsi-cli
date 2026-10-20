# Bootstrap and signing in

How a module gets its first administrator, how you find the credentials, and what to do if you would rather set a module up yourself.

## Two ways to set a module up

| | Automatic: `up --bootstrap` | Manual: `up` |
|---|---|---|
| Who creates the administrator | `tsi`, through the module's own API | You, in the module's setup page |
| Who creates the app, API key, entities, policy... | `tsi` | You |
| Credentials | Generated and saved in `.tsi/state.yaml`; `tsi compose creds` prints them | You choose them; `tsi` does not know them |
| Module's setup page | Says setup is already complete | Waits for you, and asks for the setup token |
| `tsi compose test` | Works | Needs credentials `tsi` does not have (see [Mixing](#mixing-manual-setup-and-tsi)) |
| Good for | Trying a module in a minute; CI; tests | Learning the module's first-run flow as a customer would see it |

Use automatic unless you have a reason not to.

## Automatic: `up --bootstrap`

```bash
tsi compose up -d --bootstrap
```

Every module's bootstrap is a list of API calls, in order. `tsi` makes each one and saves anything the module will not show again, such as an API secret, **before** it makes the next call. The exact list per module is in its [product guide](products/).

Bootstrap is **idempotent**. Run it again, with `tsi compose bootstrap` or `up --bootstrap`, and every completed step is skipped; the saved credentials do not change. If a run was interrupted after a module created something but before `tsi` saved it, the next run finds the object again instead of creating a duplicate.

Bootstrap settings (names, entities, a policy) go under `bootstrap:` in `tsi.yaml` and **apply the first time only**. Once a step is recorded as done, changing its setting does nothing. To redo it, start over (see [Starting over](secrets-and-state.md#starting-over)). See [Configuration](configuration.md#bootstrap-first-run-settings).

### "Setup Already Complete"

After a bootstrap, a module's browser setup page shows a message such as *Setup Already Complete: TSI COMPASS has already been configured for your organisation*. This is expected. The administrator already exists. Sign in with the credentials below.

## Finding your credentials

```bash
tsi compose status                    # the URL of each module
tsi compose creds --show-secret       # sign-in details
tsi compose creds --module node-a     # one module or instance
tsi compose creds -o json             # machine-readable
```

Without `--show-secret` the secrets are masked (`3f58…7766`). What you get depends on the module:

| Module | `creds` shows | Use it for |
|---|---|---|
| `tsi-dpdp-cms` | admin login; an API key and secret for the app | the console; the consent API (`X-API-Key`, `X-API-Secret`) |
| `tsi-privacy-vault` | admin login; a client API key and secret | the console; the vault API |
| `tsi-compass` | admin login | the console |
| `tsi-dx-node` | admin login for each node; the sending node also has an API key and secret | the console; sending a transfer from the sending node |

The admin email is `admin.email` from `tsi.yaml` (default `admin@example.org`). The password is generated.

All of it is stored in `.tsi/state.yaml`. A module shows an API secret **once**, so that file is the only copy. Back it up if the stack matters ([Secrets and state](secrets-and-state.md)).

## Manual setup

To go through a module's own setup page as a customer would:

1. Start from a fresh stack. If the module was already bootstrapped, its setup page is closed.
   ```bash
   tsi compose down --volumes --yes     # only if there is an existing stack: this deletes its data
   tsi compose init --modules tsi-compass --non-interactive
   ```
2. Start it **without** `--bootstrap`:
   ```bash
   tsi compose up -d
   ```
3. Find the module's URL with `tsi compose status` and open it in a browser.
4. The setup page asks for a **setup token** (also called a bootstrap token). It is generated for you and kept in the module's env file.

### Finding the setup token

```bash
grep -H BOOTSTRAP_TOKEN .tsi/env/*.env
```

or for one module:

```bash
grep BOOTSTRAP_TOKEN .tsi/env/tsi-compass.env
```

The variable and file name differ by module:

| Module | File | Variable |
|---|---|---|
| `tsi-dpdp-cms` | `.tsi/env/tsi-dpdp-cms.env` | `TSI_BOOTSTRAP_TOKEN` |
| `tsi-privacy-vault` | `.tsi/env/tsi-privacy-vault.env` | `TSI_PRIVACY_VAULT_BOOTSTRAP_TOKEN` |
| `tsi-compass` | `.tsi/env/tsi-compass.env` | `TSI_BOOTSTRAP_TOKEN` |
| `tsi-dx-node` | `.tsi/env/<instance>.env`, for example `node-a.env` | `TSI_DX_NODE_BOOTSTRAP_TOKEN` |

The same values are in `.tsi/state.yaml` under `modules` → the module's name → `secrets`.

Treat the token like a password until setup is done: anyone who has it can create the first administrator. The files are mode 0600, readable only by your user.

### Setting the administrator password yourself

Each module has its own rules (for example 12 characters minimum for the consent module and the vault, 10 for Compass, and upper, lower, digit and special character for dx-node). The setup page tells you if a password is refused.

## Mixing manual setup and `tsi`

`tsi` keeps one administrator password per module in `.tsi/state.yaml`. It uses it to log in for `tsi compose bootstrap` and `tsi compose test`. If you create the administrator yourself:

- `tsi compose test` stops with *no saved credentials* until a bootstrap has succeeded, and `tsi compose bootstrap` logs in with the password `tsi` generated, which the module does not know, so it fails at the **login** step. The error says the saved administrator password may be missing from `.tsi/state.yaml`.
- To make them work, put your password into `modules.<module>.admin_password` in `.tsi/state.yaml` and keep the file at mode 0600. `tsi` then logs in as the email in `tsi.yaml` (`admin.email`) with your password, so use that email when you create the administrator. Then `tsi compose bootstrap` completes the remaining steps and `tsi compose test` runs. (I checked this with `tsi-compass`: manual setup, the edit, then `bootstrap` and `test` passed.)
- Anything you created by hand (apps, API keys) is not known to `tsi`; the smoke tests that need a tenant API key will not find one. Run the module-specific checks yourself, or let `tsi` bootstrap a separate stack.

If you want `tsi` to do the work, use `--bootstrap`. If you want to do it yourself, do not expect `tsi compose test` to pass without that edit.

## When credentials are lost

| What you have | What it means |
|---|---|
| `.tsi/state.yaml` is gone, containers exist | `tsi compose up` refuses (exit code 8) rather than generate new keys over existing data. Restore the file from a backup, or `tsi compose down --volumes --yes` to start over |
| API secret lost, state intact | Not recoverable from the module. Generate a new key in the module's console or API |
| Admin password lost, state intact | `tsi compose creds --show-secret` has it |
| Admin password lost, state gone | Use the module's own password recovery, or start over |

## Setup tokens after setup

The setup token stays in the env file after setup. The vault's own guide recommends removing it from the environment once setup is done; for a local, loopback-only stack this is optional. Do not copy a stack's `.tsi/` directory to another machine.
