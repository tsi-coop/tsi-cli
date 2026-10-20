// Command tsi picks, configures and launches TSI modules. It is a thin shell over the
// library: flag parsing and printing only.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/tsi-coop/tsi-cli/internal/redact"
	"github.com/tsi-coop/tsi-cli/tsierr"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	root := newRoot()
	root.SetContext(ctx)
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "error:", redact.String(err.Error()))
		os.Exit(tsierr.ExitCode(err))
	}
}
