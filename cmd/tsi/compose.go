package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/tsi-coop/tsi-cli/catalog"
	"github.com/tsi-coop/tsi-cli/compose"
	"github.com/tsi-coop/tsi-cli/config"
	"github.com/tsi-coop/tsi-cli/engine"
	"github.com/tsi-coop/tsi-cli/internal/redact"
	"github.com/tsi-coop/tsi-cli/render"
	"github.com/tsi-coop/tsi-cli/tsierr"
	"github.com/tsi-coop/tsi-cli/tsitest"
)

func newCompose(g *globals) *cobra.Command {
	c := &cobra.Command{Use: "compose", Short: "Run and provision TSI modules locally with Docker"}
	pf := c.PersistentFlags()
	pf.StringVar(&g.dir, "dir", ".", "project directory")
	pf.BoolVar(&g.nonInteractive, "non-interactive", false, "never prompt; use flags and defaults")
	pf.StringVarP(&g.output, "output", "o", "text", "output format: text or json")
	pf.BoolVar(&g.plan, "plan", false, "show what would happen and change nothing")
	pf.StringVar(&g.project, "project", "", "compose project name (default from tsi.yaml, or tsi)")
	pf.IntVar(&g.portOffset, "port-offset", 0, "add this to every default host port")
	c.PersistentPreRun = func(cmd *cobra.Command, _ []string) {
		g.portOffsetSet = cmd.Flags().Changed("port-offset")
	}
	c.AddCommand(
		newModules(g), newInit(g), newUp(g), newBootstrap(g), newStatus(g),
		newCreds(g), newTest(g), newLogs(g), newDown(g),
	)
	return c
}

func (g *globals) json() bool { return g.output == "json" }

func (g *globals) stack(cmd *cobra.Command, extra ...compose.Option) (*compose.Stack, error) {
	opts := []compose.Option{compose.WithDir(g.dir)}
	if !g.json() {
		opts = append(opts, compose.WithOutput(cmd.ErrOrStderr()))
	}
	if g.project != "" {
		opts = append(opts, compose.WithProject(g.project))
	}
	if g.portOffsetSet {
		opts = append(opts, compose.WithPortOffset(g.portOffset))
	}
	return compose.New(append(opts, extra...)...)
}

func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// ---- modules

func newModules(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "modules",
		Short: "List the available modules",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cat, err := catalog.Load()
			if err != nil {
				return err
			}
			type row struct {
				Name, Purpose, Image string
				Port                 int
			}
			var rows []row
			for _, n := range cat.Names() {
				m, _ := cat.Get(n)
				rows = append(rows, row{n, m.Purpose, m.Image.Ref(), m.Ports.App + config.DefaultPortOffset})
			}
			if g.json() {
				return printJSON(cmd.OutOrStdout(), rows)
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "MODULE\tPORT\tIMAGE\tPURPOSE")
			for _, r := range rows {
				fmt.Fprintf(tw, "%s\t%d\t%s\t%s\n", r.Name, r.Port, r.Image, r.Purpose)
			}
			return tw.Flush()
		},
	}
}

// ---- init

func newInit(g *globals) *cobra.Command {
	var modules []string
	var dbMode, dsn, email, name string
	var force bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Choose modules, generate secrets, write tsi.yaml and the compose files",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cat, err := catalog.Load()
			if err != nil {
				return err
			}
			cfg := config.Default()
			path := g.dir + "/" + config.FileName
			if _, err := os.Stat(path); err == nil && !force {
				return tsierr.New(tsierr.CodeUsage, "", "init", path+" already exists", "pass --force to overwrite the config (secrets are never overwritten)", nil)
			}
			in := bufio.NewReader(cmd.InOrStdin())
			interactive := !g.nonInteractive && isTerminal(os.Stdin)
			if len(modules) == 0 {
				if !interactive {
					return tsierr.New(tsierr.CodeUsage, "", "init", "no modules selected", "pass --modules, for example --modules tsi-dpdp-cms", nil)
				}
				modules = promptModules(cmd.ErrOrStderr(), in, cat)
			}
			cfg.Modules = modules
			if dbMode != "" {
				cfg.Database.Mode = config.DBMode(dbMode)
			} else if interactive {
				cfg.Database.Mode = config.DBMode(ask(cmd.ErrOrStderr(), in, "Database (shared, per-module, external)", "shared"))
			}
			cfg.Database.DSN = dsn
			if interactive && email == "" {
				email = ask(cmd.ErrOrStderr(), in, "Admin email", cfg.Admin.Email)
			}
			if email != "" {
				cfg.Admin.Email = email
			}
			if name != "" {
				cfg.Admin.Name = name
			}
			cfg.Project = config.ProjectFromDir(g.dir)
			if abs, err := filepath.Abs(g.dir); err == nil {
				cfg.Project = config.ProjectFromDir(abs)
			}
			if g.project != "" {
				cfg.Project = g.project
			}
			if g.portOffsetSet {
				cfg.PortOffset = g.portOffset
			}
			s, err := g.stack(cmd, compose.WithConfig(cfg))
			if err != nil {
				return err
			}
			if g.plan {
				return showPlan(cmd, g, s)
			}
			if err := s.SaveConfig(); err != nil {
				return err
			}
			if err := s.Write(cmd.Context()); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "✔ generated secrets (kept in .tsi/state.yaml, mode 0600, git-ignored)\n")
			fmt.Fprintf(cmd.ErrOrStderr(), "✔ wrote %s, %s, .tsi/env/\n", config.FileName, render.Compose)
			fmt.Fprintf(cmd.ErrOrStderr(), "next: tsi compose up -d --bootstrap\n")
			return nil
		},
	}
	f := cmd.Flags()
	f.StringSliceVar(&modules, "modules", nil, "modules to run, comma separated")
	f.StringVar(&dbMode, "database", "", "database mode: shared, per-module or external")
	f.StringVar(&dsn, "dsn", "", "admin connection string for --database external")
	f.StringVar(&email, "admin-email", "", "administrator email")
	f.StringVar(&name, "admin-name", "", "administrator name")
	f.BoolVar(&force, "force", false, "overwrite an existing tsi.yaml")
	return cmd
}

func promptModules(w io.Writer, in *bufio.Reader, cat *catalog.Catalog) []string {
	names := cat.Names()
	fmt.Fprintln(w, "Available modules:")
	for i, n := range names {
		m, _ := cat.Get(n)
		fmt.Fprintf(w, "  %d) %s  %s\n", i+1, n, m.Purpose)
	}
	ans := ask(w, in, "Select modules (numbers or names, comma separated)", "1")
	var out []string
	for _, p := range strings.Split(ans, ",") {
		p = strings.TrimSpace(p)
		var idx int
		if _, err := fmt.Sscanf(p, "%d", &idx); err == nil && idx >= 1 && idx <= len(names) {
			out = append(out, names[idx-1])
		} else if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func ask(w io.Writer, in *bufio.Reader, q, def string) string {
	fmt.Fprintf(w, "%s [%s]: ", q, def)
	line, _ := in.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

func showPlan(cmd *cobra.Command, g *globals, s *compose.Stack) error {
	p, err := s.Plan()
	if err != nil {
		return err
	}
	if g.json() {
		return printJSON(cmd.OutOrStdout(), p)
	}
	w := cmd.OutOrStdout()
	fmt.Fprintln(w, "plan (nothing was changed):")
	fmt.Fprintln(w, "  files:   ", strings.Join(p.Files, ", "))
	fmt.Fprintln(w, "  services:", strings.Join(p.Services, ", "))
	fmt.Fprintln(w, "  ports:   ", strings.Join(p.Ports, ", "))
	var mods []string
	for m := range p.Migrations {
		mods = append(mods, m)
	}
	sort.Strings(mods)
	for _, m := range mods {
		fmt.Fprintf(w, "  %s migrations: %s\n", m, strings.Join(p.Migrations[m], " "))
		fmt.Fprintf(w, "  %s bootstrap:  %s\n", m, strings.Join(p.Steps[m], " → "))
	}
	return nil
}

// ---- up / bootstrap

func newUp(g *globals) *cobra.Command {
	var detach, boot bool
	cmd := &cobra.Command{
		Use:   "up",
		Short: "Start the stack; with --bootstrap also create the admin, app and API key",
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := g.stack(cmd)
			if err != nil {
				return err
			}
			if g.plan {
				return showPlan(cmd, g, s)
			}
			var opts []compose.UpOption
			if boot {
				opts = append(opts, compose.WithBootstrap())
			}
			if err := s.Up(cmd.Context(), opts...); err != nil {
				return err
			}
			return printStatus(cmd, g, s)
		},
	}
	cmd.Flags().BoolVarP(&detach, "detach", "d", true, "run in the background (always on; accepted for docker compose familiarity)")
	cmd.Flags().BoolVar(&boot, "bootstrap", false, "provision the modules after they are ready")
	return cmd
}

func newBootstrap(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "bootstrap",
		Short: "Create the admin, app and API key in each module (safe to re-run)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := g.stack(cmd)
			if err != nil {
				return err
			}
			if g.plan {
				return showPlan(cmd, g, s)
			}
			if err := s.Bootstrap(cmd.Context()); err != nil {
				return err
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "✔ bootstrap complete; see `tsi compose creds`")
			return nil
		},
	}
}

// ---- status

func newStatus(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show container state, readiness and whether each module is provisioned",
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := g.stack(cmd)
			if err != nil {
				return err
			}
			return printStatus(cmd, g, s)
		},
	}
}

func printStatus(cmd *cobra.Command, g *globals, s *compose.Stack) error {
	rep, err := s.Status(cmd.Context())
	if err != nil {
		return err
	}
	if g.json() {
		return printJSON(cmd.OutOrStdout(), rep)
	}
	tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "MODULE\tCONTAINER\tREADY\tPROVISIONED\tURL")
	for _, m := range rep.Modules {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", m.Module, m.Container, yesNo(m.Ready), yesNo(m.Provisioned), m.URL)
	}
	return tw.Flush()
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// ---- creds

func newCreds(g *globals) *cobra.Command {
	var module string
	var show bool
	cmd := &cobra.Command{
		Use:   "creds",
		Short: "Show the saved tenant credentials (secrets are masked unless --show-secret)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := g.stack(cmd)
			if err != nil {
				return err
			}
			type row struct {
				Module        string            `json:"module"`
				BaseURL       string            `json:"base_url"`
				APIKey        string            `json:"api_key"`
				APISecret     string            `json:"api_secret,omitempty"`
				AdminEmail    string            `json:"admin_email,omitempty"`
				AdminPassword string            `json:"admin_password,omitempty"`
				Extra         map[string]string `json:"extra,omitempty"`
			}
			var rows []row
			for _, ref := range s.Cfg.Modules {
				m := config.Key(ref)
				if module != "" && module != m {
					continue
				}
				c, ok := s.Credentials(m)
				if !ok {
					continue
				}
				sec := redact.Mask(c.APISecret)
				if show {
					sec = c.APISecret
				}
				apw := ""
				if c.AdminPassword != "" {
					apw = redact.Mask(c.AdminPassword)
					if show {
						apw = c.AdminPassword
					}
				}
				if c.APISecret == "" {
					sec = ""
				}
				rows = append(rows, row{Module: m, BaseURL: c.BaseURL, APIKey: c.APIKey, APISecret: sec,
					AdminEmail: c.AdminEmail, AdminPassword: apw, Extra: c.Extra})
			}
			if len(rows) == 0 {
				return tsierr.New(tsierr.CodeNotFound, module, "creds", "no saved credentials", "run `tsi compose up --bootstrap` first", nil)
			}
			if g.json() {
				return printJSON(cmd.OutOrStdout(), rows)
			}
			for _, r := range rows {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\n  base url:     %s\n", r.Module, r.BaseURL)
				if r.APIKey != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "  X-API-Key:    %s\n  X-API-Secret: %s\n", r.APIKey, r.APISecret)
				}
				if r.AdminEmail != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "  admin login:  %s\n  admin password: %s\n", r.AdminEmail, r.AdminPassword)
				}
				keys := make([]string, 0, len(r.Extra))
				for k := range r.Extra {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s: %s\n", k, r.Extra[k])
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&module, "module", "", "only this module")
	cmd.Flags().BoolVar(&show, "show-secret", false, "print the API secret in full")
	return cmd
}

// ---- test

func newTest(g *globals) *cobra.Command {
	var module, format string
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Run the smoke tests with the saved credentials",
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := g.stack(cmd)
			if err != nil {
				return err
			}
			var opts []compose.TestOption
			if module != "" {
				opts = append(opts, compose.OnlyModules(module))
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Minute)
			defer cancel()
			results, err := s.Test(ctx, opts...)
			if err != nil {
				return err
			}
			if g.json() {
				format = "json"
			}
			w := cmd.OutOrStdout()
			var failed int
			switch format {
			case "json":
				err = tsitest.WriteJSON(w, results)
				for _, r := range results {
					failed += r.Failed()
				}
			case "junit":
				err = tsitest.WriteJUnit(w, results)
				for _, r := range results {
					failed += r.Failed()
				}
			default:
				_, failed = tsitest.WriteHuman(w, results)
			}
			if err != nil {
				return err
			}
			if failed > 0 {
				return tsierr.Newf(tsierr.CodeTestFailed, "", "%d smoke test(s) failed", failed)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&module, "module", "", "only this module")
	cmd.Flags().StringVar(&format, "format", "text", "report format: text, json or junit")
	return cmd
}

// ---- logs

func newLogs(g *globals) *cobra.Command {
	var follow bool
	var tail int
	cmd := &cobra.Command{
		Use:   "logs [module...]",
		Short: "Show container logs",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := g.stack(cmd)
			if err != nil {
				return err
			}
			return s.Logs(cmd.Context(), cmd.OutOrStdout(), engine.LogOptions{Follow: follow, Tail: tail}, args...)
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "follow the log output")
	cmd.Flags().IntVar(&tail, "tail", 0, "number of lines to show from the end")
	return cmd
}

// ---- down

func newDown(g *globals) *cobra.Command {
	var volumes, yes bool
	cmd := &cobra.Command{
		Use:   "down",
		Short: "Stop the stack; --volumes also deletes the data, generated files and saved credentials",
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := g.stack(cmd)
			if err != nil {
				return err
			}
			if g.plan {
				fmt.Fprintf(cmd.OutOrStdout(), "plan: stop project %q (remove volumes and saved state: %v)\n", s.Cfg.Project, volumes)
				return nil
			}
			var opts []compose.DownOption
			if volumes {
				if !yes {
					if g.nonInteractive || !isTerminal(os.Stdin) {
						return tsierr.New(tsierr.CodeUsage, "", "down", "--volumes deletes data and credentials", "pass --yes to confirm", nil)
					}
					ans := ask(cmd.ErrOrStderr(), bufio.NewReader(cmd.InOrStdin()),
						"This deletes all data and the saved credentials for project "+s.Cfg.Project+". Type the project name to confirm", "")
					if ans != s.Cfg.Project {
						return tsierr.New(tsierr.CodeUsage, "", "down", "not confirmed", "", nil)
					}
				}
				opts = append(opts, compose.WithVolumes())
			}
			if err := s.Down(cmd.Context(), opts...); err != nil {
				return err
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "✔ stopped")
			return nil
		},
	}
	cmd.Flags().BoolVar(&volumes, "volumes", false, "also delete data volumes and saved state")
	cmd.Flags().BoolVar(&yes, "yes", false, "do not ask for confirmation")
	return cmd
}
