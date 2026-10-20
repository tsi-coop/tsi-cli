package main

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// version is set at release time with -ldflags "-X main.version=...".
var version = ""

type globals struct {
	dir            string
	nonInteractive bool
	output         string
	plan           bool
	project        string
	portOffset     int
	portOffsetSet  bool
}

func newRoot() *cobra.Command {
	g := &globals{}
	root := &cobra.Command{
		Use:           "tsi",
		Short:         "Pick, configure and launch the TSI modules your stack needs",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintln(cmd.OutOrStdout(), "tsi", buildVersion())
		},
	})
	root.AddCommand(newCompose(g))
	return root
}

func buildVersion() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}
