# Testing and CI

`tsi` can prove a module works after it is provisioned, and it can start real modules for your own tests.

## Smoke tests

```bash
tsi compose test                       # every module in the stack
tsi compose test --module tsi-compass  # one module or instance
tsi compose test --format junit > report.xml
tsi compose test -o json
```

The tests use the credentials `tsi` saved at bootstrap, talk to the real modules over HTTP, and clean up what they create. They check behaviour you would otherwise check by hand: that a wrong key is refused, that a stored value comes back unchanged, that an erased record reads as erased.

Output looks like this:

```
✔ tsi-dpdp-cms           record_consent           A consent can be recorded
✘ tsi-dpdp-cms           withdraw_consent         The consent can be withdrawn
    HTTP 500 (want [200]): {...}
– node-b                 async_transfer           A governed package is delivered to the receiving node
    only the sending node (the first instance of a pair) runs this check
3 passed, 1 failed, 1 skipped (not applicable to that node)
```

`✔` passed, `✘` failed with the reason, `–` skipped because it does not apply to that instance (a skipped check is not a failure). The command exits with code 6 if anything failed.

Which checks exist is in each [product guide](products/). Tests need a tenant credential that `tsi` created, so they work after `up --bootstrap`, not after manual setup ([why](bootstrap-and-signing-in.md#mixing-manual-setup-and-tsi)).

## In CI

`tsi` needs Docker, so use a runner with Docker (GitHub's `ubuntu-latest` has it).

```yaml
# .github/workflows/tsi.yml
jobs:
  tsi:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: {go-version: "1.22"}
      - run: go install github.com/tsicoop/tsi-cli/cmd/tsi@latest
      - run: |
          mkdir stack && cd stack
          tsi compose init --modules tsi-dpdp-cms,tsi-privacy-vault --non-interactive
          tsi compose up -d --bootstrap
          tsi compose test --format junit > ../tsi-report.xml
      - if: always()
        run: cd stack && tsi compose down --volumes --yes
```

Notes:

- Cache the download directory (`~/.cache/tsi-cli`) between runs to avoid downloading the database scripts every time. Docker images are cached by your runner's Docker.
- Use `--port-offset` or different `project:` values if jobs share a runner.
- Do not upload the stack directory as an artifact: `.tsi/` holds credentials.

## Using real modules from `go test`

The library lets a test start the real modules, get their credentials, and clean up afterwards. It needs Docker.

```go
import (
    "testing"

    "github.com/tsicoop/tsi-cli/compose"
    "github.com/tsicoop/tsi-cli/testkit"
)

func TestMyApp(t *testing.T) {
    s := testkit.Start(t, compose.WithModules("tsi-dpdp-cms"))
    c, _ := s.Credentials("tsi-dpdp-cms")
    // point the application under test at c.BaseURL with c.APIKey and c.APISecret
}
```

`testkit.Start`:

- uses a temporary directory and a unique project name;
- picks a free range of host ports;
- brings the modules up and bootstraps them;
- registers a cleanup that deletes the containers, volumes and files when the test ends, even if it fails.

Credentials come back by the module's key: its name, or its instance name. For modules driven by an admin login (`tsi-compass`, `tsi-dx-node`), `c.AdminEmail` and `c.AdminPassword` are set; for the others, `c.APIKey` and `c.APISecret`.

Use the library directly for more control:

```go
stack, err := compose.New(compose.WithDir("."), compose.WithModules("tsi-dpdp-cms"))
err = stack.Up(ctx, compose.WithBootstrap())      // render, fetch, migrate, start, wait, provision
results, err := stack.Test(ctx)                   // the same smoke tests the CLI runs
err = stack.Down(ctx, compose.WithVolumes())      // delete everything
```

Options include `compose.WithPortOffset`, `compose.WithProject`, `compose.WithOutput` and `compose.WithMigrationSource` (a fake source for tests). Every method takes a `context.Context` and stops cleanly when it is cancelled.

## Testing `tsi` itself

From the repository:

```bash
make test          # unit tests with the race detector
make network-test  # downloads every pinned database script from GitHub and checks its checksum
make integration   # real Docker: all four modules (dx-node as a pair) on one PostgreSQL, about 70 seconds
```
