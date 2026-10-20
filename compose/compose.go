// Package compose is the public API: it ties the catalog, config, secrets, renderer, engine,
// migrations, readiness, bootstrap and smoke tests into one Stack.
package compose

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/tsicoop/tsi-cli/bootstrap"
	"github.com/tsicoop/tsi-cli/catalog"
	"github.com/tsicoop/tsi-cli/certs"
	"github.com/tsicoop/tsi-cli/config"
	"github.com/tsicoop/tsi-cli/engine"
	"github.com/tsicoop/tsi-cli/health"
	"github.com/tsicoop/tsi-cli/migrate"
	"github.com/tsicoop/tsi-cli/render"
	"github.com/tsicoop/tsi-cli/secrets"
	"github.com/tsicoop/tsi-cli/tsierr"
	"github.com/tsicoop/tsi-cli/tsitest"
)

// StateDir is where tsi keeps everything it generates, relative to the project directory.
const StateDir = ".tsi"

// Stack is a set of TSI modules in one project directory.
type Stack struct {
	Dir   string
	Cfg   *config.Config
	Cat   *catalog.Catalog
	Eng   engine.Engine
	Store *secrets.Store
	// Migrations supplies module migration SQL (default: download the pinned release).
	Migrations migrate.Source
	// Out receives progress messages (nil discards).
	Out io.Writer

	cfgPath string
}

// Option configures New.
type Option func(*options)

type options struct {
	dir     string
	modules []string
	eng     engine.Engine
	cfgFile string
	cfg     *config.Config
	out     io.Writer
	project string
	offset  *int
	// migrations replaces the migration source (tests).
	migrations migrate.Source
}

// WithDir sets the project directory (default: the current directory).
func WithDir(path string) Option { return func(o *options) { o.dir = path } }

// WithModules selects modules, overriding tsi.yaml.
func WithModules(names ...string) Option { return func(o *options) { o.modules = names } }

// WithEngine replaces the container runtime (tests use a fake).
func WithEngine(e engine.Engine) Option { return func(o *options) { o.eng = e } }

// WithConfigFile reads the config from a specific file instead of <dir>/tsi.yaml.
func WithConfigFile(path string) Option { return func(o *options) { o.cfgFile = path } }

// WithConfig supplies a config directly.
func WithConfig(c *config.Config) Option { return func(o *options) { o.cfg = c } }

// WithOutput sets where progress messages go.
func WithOutput(w io.Writer) Option { return func(o *options) { o.out = w } }

// WithProject sets the compose project name (default "tsi").
func WithProject(name string) Option { return func(o *options) { o.project = name } }

// WithMigrationSource replaces where migration SQL comes from (tests use a fake).
func WithMigrationSource(src migrate.Source) Option { return func(o *options) { o.migrations = src } }

// WithPortOffset shifts every default host port.
func WithPortOffset(n int) Option { return func(o *options) { o.offset = &n } }

// New builds a Stack. The config comes from WithConfig, else tsi.yaml if present, else
// defaults for the modules given with WithModules.
func New(opts ...Option) (*Stack, error) {
	var o options
	for _, f := range opts {
		f(&o)
	}
	if o.dir == "" {
		o.dir = "."
	}
	dir, err := filepath.Abs(o.dir)
	if err != nil {
		return nil, err
	}
	cat, err := catalog.Load()
	if err != nil {
		return nil, err
	}
	cfgPath := o.cfgFile
	if cfgPath == "" {
		cfgPath = filepath.Join(dir, config.FileName)
	}
	cfg := o.cfg
	if cfg == nil {
		if _, statErr := os.Stat(cfgPath); statErr == nil {
			if cfg, err = config.Load(cfgPath); err != nil {
				return nil, err
			}
		} else if len(o.modules) > 0 {
			cfg = config.Default()
		} else {
			return nil, tsierr.New(tsierr.CodeNotFound, "", "", config.FileName+" not found",
				"run `tsi compose init` in this directory first", nil)
		}
	}
	if len(o.modules) > 0 {
		cfg.Modules = o.modules
	}
	if o.project != "" {
		cfg.Project = o.project
	}
	if o.offset != nil {
		cfg.PortOffset = *o.offset
	}
	if err := cfg.Validate(); err != nil {
		return nil, tsierr.New(tsierr.CodeUsage, "", "", "invalid configuration: "+err.Error(), "", nil)
	}
	counts := map[string]int{}
	for _, ref := range cfg.Modules {
		base, _ := config.SplitRef(ref)
		mod, err := cat.Get(base)
		if err != nil {
			return nil, err
		}
		counts[base]++
		if id := mod.Identity; id != nil && id.MaxInstances > 0 && counts[base] > id.MaxInstances {
			return nil, tsierr.New(tsierr.CodeUsage, base, "", fmt.Sprintf("at most %d instances of %s are supported", id.MaxInstances, base),
				"remove an instance from modules in tsi.yaml", nil)
		}
		if counts[base] > 1 && mod.Identity == nil && !strings.Contains(ref, ":") {
			return nil, tsierr.New(tsierr.CodeUsage, base, "", base+" listed more than once",
				"run it once, or give each instance a name like "+base+":one", nil)
		}
	}
	eng := o.eng
	if eng == nil {
		eng = &engine.Docker{Out: o.out}
	}
	st := &Stack{Dir: dir, Cfg: cfg, Cat: cat, Eng: eng, Out: o.out, cfgPath: cfgPath,
		Store: &secrets.Store{Dir: filepath.Join(dir, StateDir)}}
	st.Migrations = o.migrations
	if st.Migrations == nil {
		st.Migrations = &migrate.Fetcher{LocalDirs: cfg.MigrationDirs, Warn: st.logf}
	}
	return st, nil
}

func (s *Stack) logf(format string, a ...any) {
	if s.Out != nil {
		fmt.Fprintf(s.Out, format+"\n", a...)
	}
}

func (s *Stack) project() engine.Project {
	return engine.Project{Name: s.Cfg.Project, Dir: s.Dir, File: render.Compose}
}

// SaveConfig writes tsi.yaml.
func (s *Stack) SaveConfig() error { return s.Cfg.Save(s.cfgPath) }

// ConfigPath is where tsi.yaml lives.
func (s *Stack) ConfigPath() string { return s.cfgPath }

// Plan describes what Write and Up would do, without side effects.
type Plan struct {
	Files      []string
	Services   []string
	Ports      []string
	Migrations map[string][]string
	Steps      map[string][]string
}

// Plan resolves the stack without writing anything or generating persistent state.
func (s *Stack) Plan() (*Plan, error) {
	st, err := s.Store.Load()
	if err != nil {
		return nil, err
	}
	l, err := render.Resolve(s.Cat, s.Cfg, st)
	if err != nil {
		return nil, err
	}
	p := &Plan{Migrations: map[string][]string{}, Steps: map[string][]string{}}
	p.Files = append(p.Files, render.Compose, ".tsi/state.yaml")
	if s.Cfg.Database.Mode == config.Shared {
		p.Files = append(p.Files, render.EnvDir+"/postgres.env")
		p.Ports = append(p.Ports, fmt.Sprintf("postgres %s:%d", s.Cfg.Bind, s.Cfg.SharedDBPort()))
	}
	p.Services = append(l.DBServiceNames(), l.AppServiceNames()...)
	for _, m := range l.Modules {
		p.Files = append(p.Files, m.EnvFile)
		if m.Keystore != "" {
			p.Files = append(p.Files, m.Keystore)
		}
		p.Ports = append(p.Ports, fmt.Sprintf("%s %s:%d", m.Key, s.Cfg.Bind, m.AppPort))
		for _, ms := range m.Mod.Migrations.Steps {
			p.Migrations[m.Key] = append(p.Migrations[m.Key], ms.ID())
		}
		for _, bs := range m.Mod.Bootstrap {
			p.Steps[m.Key] = append(p.Steps[m.Key], bs.ID)
		}
	}
	return p, nil
}

// Write generates secrets and writes the compose file, env files and keystores. Existing
// secrets are never regenerated.
func (s *Stack) Write(ctx context.Context) error {
	if err := s.Store.Lock(); err != nil {
		return err
	}
	defer s.Store.Unlock()
	_, _, err := s.write()
	return err
}

func (s *Stack) write() (*render.Layout, *secrets.State, error) {
	st, err := s.Store.Load()
	if err != nil {
		return nil, nil, err
	}
	l, err := render.Resolve(s.Cat, s.Cfg, st)
	if err != nil {
		return nil, nil, err
	}
	// Persist generated secrets before anything uses them.
	if err := s.Store.Save(st); err != nil {
		return nil, nil, err
	}
	if err := s.ensureGitignore(); err != nil {
		return nil, nil, err
	}
	envDir := filepath.Join(s.Dir, render.EnvDir)
	if err := os.MkdirAll(envDir, 0o700); err != nil {
		return nil, nil, err
	}
	if l.SharedDB {
		if err := writeFile(filepath.Join(envDir, "postgres.env"), l.PostgresEnvFileContent(st.DBAdminPassword), 0o600); err != nil {
			return nil, nil, err
		}
	}
	for _, m := range l.Modules {
		if err := writeFile(filepath.Join(s.Dir, m.EnvFile), m.EnvFileContent(), 0o600); err != nil {
			return nil, nil, err
		}
		if s.Cfg.Database.Mode == config.PerModule {
			if err := writeFile(filepath.Join(envDir, m.Key+"-db.env"), m.DBEnvFileContent(), 0o600); err != nil {
				return nil, nil, err
			}
		}
		if m.Keystore != "" {
			pass, _ := st.EnsureSecret(m.Key, "KEYSTORE_PASSWORD", "password")
			created, err := certs.WriteKeystore(filepath.Join(s.Dir, m.Keystore), pass, m.Mod.Keystore.CommonName)
			if err != nil {
				return nil, nil, err
			}
			if created {
				s.logf("generated keystore %s", m.Keystore)
			}
		}
	}
	yml, err := l.ComposeYAML()
	if err != nil {
		return nil, nil, err
	}
	if err := writeFile(filepath.Join(s.Dir, render.Compose), yml, 0o644); err != nil {
		return nil, nil, err
	}
	return l, st, nil
}

func writeFile(path string, b []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tsi-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(mode); err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// ensureGitignore keeps secrets out of git.
func (s *Stack) ensureGitignore() error {
	p := filepath.Join(s.Dir, ".gitignore")
	b, _ := os.ReadFile(p)
	if strings.Contains(string(b), ".tsi/") {
		return nil
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	prefix := ""
	if len(b) > 0 && !strings.HasSuffix(string(b), "\n") {
		prefix = "\n"
	}
	_, err = f.WriteString(prefix + "# tsi-cli: generated secrets and state\n.tsi/\n")
	return err
}

// UpOption tunes Up.
type UpOption func(*upOptions)

type upOptions struct{ bootstrap bool }

// WithBootstrap provisions the modules after they are ready.
func WithBootstrap() UpOption { return func(o *upOptions) { o.bootstrap = true } }

// Up renders the files, starts Postgres, applies migrations, starts the modules and waits
// until each answers its readiness probe.
func (s *Stack) Up(ctx context.Context, opts ...UpOption) error {
	var uo upOptions
	for _, f := range opts {
		f(&uo)
	}
	if err := s.Store.Lock(); err != nil {
		return err
	}
	defer s.Store.Unlock()

	if err := s.Eng.Preflight(ctx); err != nil {
		return err
	}
	hadState := s.Store.Exists()
	running, err := s.Eng.PS(ctx, s.project())
	if err != nil {
		return err
	}
	if !hadState && len(running) > 0 {
		return tsierr.New(tsierr.CodeStateLost, "", "up", "containers for project "+s.Cfg.Project+" exist but "+s.Store.StatePath()+" is missing",
			"restore the state file, or run `tsi compose down --volumes` to start over (this deletes the data)", nil)
	}
	l, st, err := s.write()
	if err != nil {
		return err
	}
	if len(running) == 0 {
		if err := s.checkPorts(l); err != nil {
			return err
		}
	}
	for _, m := range l.Modules {
		s.logf("%s: fetching migrations (%s@%s)", m.Key, m.Mod.Migrations.Source.Repo, m.Mod.Migrations.Source.Ref)
		if err := migrate.Prefetch(ctx, s.Migrations, m.Mod); err != nil {
			return err
		}
	}
	s.logf("pulling images")
	if err := s.Eng.Pull(ctx, s.project()); err != nil {
		return err
	}
	s.writeLock(ctx, l)

	if dbs := l.DBServiceNames(); len(dbs) > 0 {
		s.logf("starting postgres")
		if err := s.Eng.Up(ctx, s.project(), true, dbs...); err != nil {
			return err
		}
	}
	if err := s.migrateAll(ctx, l, st); err != nil {
		return err
	}
	s.logf("starting modules")
	if err := s.Eng.Up(ctx, s.project(), false, l.AppServiceNames()...); err != nil {
		return err
	}
	if err := s.waitReady(ctx, l, 3*time.Minute); err != nil {
		return err
	}
	if uo.bootstrap {
		return s.bootstrap(ctx, l, st)
	}
	return nil
}

func (s *Stack) migrateAll(ctx context.Context, l *render.Layout, st *secrets.State) error {
	for _, m := range l.Modules {
		adminDSN := l.AdminDSN(m, st.DBAdminPassword)
		wctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		err := migrate.WaitForServer(wctx, adminDSN)
		cancel()
		if err != nil {
			return tsierr.New(tsierr.CodeMigration, m.Key, "migrate", "database not reachable", "check database settings and that the port is free", err)
		}
		if err := migrate.EnsureDatabase(ctx, adminDSN, m.DBUser, m.DBPass, m.DBName); err != nil {
			return tsierr.New(tsierr.CodeMigration, m.Key, "migrate", "cannot create the module database", "", err)
		}
		dsn, err := l.HostModuleDSN(m)
		if err != nil {
			return err
		}
		res, err := migrate.Apply(ctx, s.Migrations, m.Mod, dsn, adminDSN, m.DBName, m.EnvValue)
		if err != nil {
			return err
		}
		ms := st.Module(m.Key)
		if !ms.DataInitialized {
			// From here on the immutable secrets are in use and are locked.
			ms.DataInitialized = true
			if err := s.Store.Save(st); err != nil {
				return err
			}
		}
		s.logf("%s: migrations applied %d, already present %d", m.Key, len(res.Applied), len(res.Skipped))
	}
	return nil
}

func (s *Stack) waitReady(ctx context.Context, l *render.Layout, timeout time.Duration) error {
	for _, m := range l.Modules {
		if err := health.WaitReady(ctx, m.Mod, m.BaseURL(s.Cfg.Bind), timeout); err != nil {
			return err
		}
		s.logf("%s: ready at %s", m.Key, m.BaseURL(s.Cfg.Bind))
	}
	return nil
}

// WaitReady blocks until every module answers its readiness probe.
func (s *Stack) WaitReady(ctx context.Context, timeout time.Duration) error {
	st, err := s.Store.Load()
	if err != nil {
		return err
	}
	l, err := render.Resolve(s.Cat, s.Cfg, st)
	if err != nil {
		return err
	}
	return s.waitReady(ctx, l, timeout)
}

// checkPorts fails early, naming the module, when a host port is taken.
func (s *Stack) checkPorts(l *render.Layout) error {
	type pt struct {
		name string
		port int
	}
	var pts []pt
	switch s.Cfg.Database.Mode {
	case config.Shared:
		pts = append(pts, pt{"postgres", s.Cfg.SharedDBPort()})
	case config.PerModule:
		for _, m := range l.Modules {
			pts = append(pts, pt{m.Key + " database", m.DBHostPort})
		}
	}
	for _, m := range l.Modules {
		pts = append(pts, pt{m.Key, m.AppPort})
	}
	for _, p := range pts {
		ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", s.Cfg.Bind, p.port))
		if err != nil {
			return tsierr.New(tsierr.CodePreflight, p.name, "preflight", fmt.Sprintf("port %d is already in use", p.port),
				"free the port, or set port_offset in tsi.yaml (for example `port_offset: 100`)", nil)
		}
		ln.Close()
	}
	return nil
}

// tsi.lock records the image digests that were pulled, so a run can be reproduced.
func (s *Stack) writeLock(ctx context.Context, l *render.Layout) {
	images := map[string]string{"postgres:15-alpine": ""}
	for _, m := range l.Modules {
		images[m.Mod.Image.Ref()] = ""
	}
	for ref := range images {
		if d, err := s.Eng.Digest(ctx, ref); err == nil {
			images[ref] = d
		}
	}
	b, err := yaml.Marshal(map[string]any{"version": 1, "images": images})
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(s.Dir, "tsi.lock"), b, 0o644)
}

// Credentials returns the saved tenant credentials of a module.
func (s *Stack) Credentials(module string) (secrets.Credentials, bool) {
	st, err := s.Store.Load()
	if err != nil {
		return secrets.Credentials{}, false
	}
	ms, ok := st.Modules[module]
	if !ok || ms.Credentials == nil {
		return secrets.Credentials{}, false
	}
	return *ms.Credentials, true
}

// BootstrapOption tunes Bootstrap.
type BootstrapOption func(*bootstrapOptions)

type bootstrapOptions struct{}

// Bootstrap provisions every module: administrator, tenant app and API key. It is safe to
// run again; completed steps are skipped.
func (s *Stack) Bootstrap(ctx context.Context, opts ...BootstrapOption) error {
	if err := s.Store.Lock(); err != nil {
		return err
	}
	defer s.Store.Unlock()
	st, err := s.Store.Load()
	if err != nil {
		return err
	}
	l, err := render.Resolve(s.Cat, s.Cfg, st)
	if err != nil {
		return err
	}
	return s.bootstrap(ctx, l, st)
}

func (s *Stack) runner(m *render.Module, st *secrets.State) (*bootstrap.Runner, error) {
	ms := st.Module(m.Key)
	if ms.AdminPassword == "" {
		gen := "password"
		if m.Mod.AdminPassword != "" {
			gen = m.Mod.AdminPassword
		}
		pw, err := secrets.Generate(gen)
		if err != nil {
			return nil, err
		}
		ms.AdminPassword = pw
		// Saved before the module is told about it.
		if err := s.Store.Save(st); err != nil {
			return nil, err
		}
	}
	env := map[string]string{}
	for _, kv := range m.Env {
		env[kv.Key] = kv.Value
	}
	spec := mergeSpec(m.Mod.SpecDefaults, firstSpec(s.Cfg.Bootstrap, m.Key, m.Mod.Name), m.Mod.SpecReplace)
	return &bootstrap.Runner{
		Module: m.Mod, Key: m.Key, BaseURL: m.BaseURL(s.Cfg.Bind), Env: env,
		Admin: bootstrap.Admin{Name: s.Cfg.Admin.Name, Email: s.Cfg.Admin.Email, Password: ms.AdminPassword},
		Spec:  spec, State: ms,
		Save: func() error { return s.Store.Save(st) },
		Log:  s.logf,
	}, nil
}

func (s *Stack) bootstrap(ctx context.Context, l *render.Layout, st *secrets.State) error {
	runners := map[string]*bootstrap.Runner{}
	var active []*render.Module
	for _, m := range l.Modules {
		if len(m.Mod.Bootstrap) == 0 {
			continue
		}
		r, err := s.runner(m, st)
		if err != nil {
			return err
		}
		runners[m.Key] = r
		active = append(active, m)
	}
	for _, m := range active {
		if m.Mod.Identity == nil {
			continue
		}
		extra, err := s.identityContext(l, m, runners)
		if err != nil {
			return err
		}
		runners[m.Key].Extra = extra
	}
	// Phases run across all instances: everyone finishes phase 1 (so every node exists and
	// knows its peers) before phase 2 (the handshake) starts.
	maxPhase := 1
	for _, r := range runners {
		if p := r.Phases(); p > maxPhase {
			maxPhase = p
		}
	}
	for phase := 1; phase <= maxPhase; phase++ {
		for _, m := range active {
			if err := runners[m.Key].RunPhase(ctx, phase); err != nil {
				return err
			}
		}
	}
	for _, m := range active {
		if creds := runners[m.Key].Credentials(); creds != nil {
			st.Module(m.Key).Credentials = creds
		}
	}
	return s.Store.Save(st)
}

// identityContext builds the Identity, Node and Peers template values for a module whose
// instances talk to each other. Peers are the other instances of the same module.
func (s *Stack) identityContext(l *render.Layout, m *render.Module, runners map[string]*bootstrap.Runner) (func() map[string]any, error) {
	certDir := filepath.Join(s.Dir, render.CertDir)
	ident, created, err := certs.LoadOrCreateIdentity(certDir, m.Key, m.Hostname)
	if err != nil {
		return nil, err
	}
	if created {
		s.logf("%s: generated node identity %s/%s.pem", m.Key, render.CertDir, m.Key)
	}
	var group []*render.Module
	for _, o := range l.Modules {
		if o.Mod.Name == m.Mod.Name {
			group = append(group, o)
		}
	}
	idx := 0
	for i, o := range group {
		if o.Key == m.Key {
			idx = i
		}
	}
	port := m.Mod.Identity.P2PPort
	nodeID := func(key string) string { return strings.ToUpper(key) }
	peers := func() []any {
		var out []any
		for _, o := range group {
			if o.Key == m.Key {
				continue
			}
			pi, _, err := certs.LoadOrCreateIdentity(certDir, o.Key, o.Hostname)
			if err != nil {
				continue
			}
			vars := map[string]any{}
			if r := runners[o.Key]; r != nil {
				for k, v := range r.Vars() {
					vars[k] = v
				}
			}
			out = append(out, map[string]any{
				"key": o.Key, "node_id": nodeID(o.Key), "fqdn": o.Hostname,
				"address": fmt.Sprintf("%s:%d", o.Hostname, port), "certificate_pem": pi.CertificatePEM, "vars": vars,
			})
		}
		return out
	}
	return func() map[string]any {
		ps := peers()
		return map[string]any{
			"Identity": map[string]any{"certificate_pem": ident.CertificatePEM, "private_key_pem": ident.PrivateKeyPEM},
			"Node": map[string]any{
				"key": m.Key, "id": nodeID(m.Key), "fqdn": m.Hostname, "port": int64(port), "index": int64(idx),
				"sender": idx == 0 && len(ps) > 0, "receiver": idx == 1 && len(ps) > 0,
			},
			"Peers": ps,
		}
	}, nil
}

func firstSpec(all map[string]map[string]any, keys ...string) map[string]any {
	for _, k := range keys {
		if v, ok := all[k]; ok {
			return v
		}
	}
	return nil
}

// mergeSpec overlays user values on the catalog defaults, merging nested maps one level at a
// time. A list, or a map at a path in replace, is replaced wholesale.
func mergeSpec(base, over map[string]any, replace []string) map[string]any {
	set := map[string]bool{}
	for _, r := range replace {
		set[r] = true
	}
	return mergeAt(base, over, "", set)
}

func mergeAt(base, over map[string]any, prefix string, replace map[string]bool) map[string]any {
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		if bm, ok := out[k].(map[string]any); ok && !replace[path] {
			if om, ok := v.(map[string]any); ok {
				out[k] = mergeAt(bm, om, path, replace)
				continue
			}
		}
		out[k] = v
	}
	return out
}

// TestOption tunes Test.
type TestOption func(*testOptions)

type testOptions struct{ modules, checks []string }

// OnlyModules limits the run to some modules.
func OnlyModules(names ...string) TestOption { return func(o *testOptions) { o.modules = names } }

// OnlyChecks limits the run to some check ids.
func OnlyChecks(ids ...string) TestOption { return func(o *testOptions) { o.checks = ids } }

// Test runs the smoke suites with the saved tenant credentials.
func (s *Stack) Test(ctx context.Context, opts ...TestOption) ([]tsitest.Result, error) {
	var to testOptions
	for _, f := range opts {
		f(&to)
	}
	st, err := s.Store.Load()
	if err != nil {
		return nil, err
	}
	l, err := render.Resolve(s.Cat, s.Cfg, st)
	if err != nil {
		return nil, err
	}
	// Build an environment for every module first: a suite for one instance may need to
	// reach its peers (the receiver of a transfer, for example).
	envs := map[string]*tsitest.Env{}
	for _, m := range l.Modules {
		ms := st.Modules[m.Key]
		if ms == nil || ms.Credentials == nil {
			if len(to.modules) > 0 && !containsStr(to.modules, m.Key) {
				continue
			}
			return nil, tsierr.New(tsierr.CodeNotFound, m.Key, "test", "no saved credentials", "run `tsi compose bootstrap` first", nil)
		}
		// The runner is used only for admin fixtures; it works on a copy so tests never change saved state.
		cp := *ms
		cp.Steps = map[string]secrets.StepState{}
		r, err := s.runner(m, &secrets.State{Modules: map[string]*secrets.ModuleState{m.Key: &cp}})
		if err != nil {
			return nil, err
		}
		r.Save = nil
		mod := m
		spec, err := r.RenderedSpec()
		if err != nil {
			return nil, err
		}
		envs[m.Key] = &tsitest.Env{
			Module: m.Key, Suite: m.Mod.Name, BaseURL: m.BaseURL(s.Cfg.Bind), Creds: *ms.Credentials, Spec: spec,
			Admin: r.AdminCall,
			Ready: func(ctx context.Context) error {
				return health.Check(ctx, httpClient(), mod.BaseURL(s.Cfg.Bind), mod.Mod.Ready)
			},
		}
	}
	for _, m := range l.Modules {
		e := envs[m.Key]
		if e == nil {
			continue
		}
		for _, o := range l.Modules {
			if o.Key == m.Key {
				continue
			}
			if o.Mod.Name == m.Mod.Name && envs[o.Key] != nil {
				e.Peers = append(e.Peers, envs[o.Key])
			} else if o.Mod.Name == m.Mod.Name {
				continue
			}
		}
		for i, o := range l.Modules {
			if o.Key == m.Key {
				e.Index = countBefore(l.Modules, i, m.Mod.Name)
			}
		}
	}
	var results []tsitest.Result
	for _, m := range l.Modules {
		if len(to.modules) > 0 && !containsStr(to.modules, m.Key) {
			continue
		}
		res, err := tsitest.Run(ctx, envs[m.Key], to.checks)
		if err != nil {
			return nil, err
		}
		results = append(results, res)
	}
	return results, nil
}

// countBefore counts modules of the same kind that come before position i.
func countBefore(mods []*render.Module, i int, name string) int {
	n := 0
	for _, o := range mods[:i] {
		if o.Mod.Name == name {
			n++
		}
	}
	return n
}

func containsStr(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// ModuleStatus is the state of one module.
type ModuleStatus struct {
	Module      string `json:"module"`
	Container   string `json:"container"`
	Ready       bool   `json:"ready"`
	URL         string `json:"url"`
	Provisioned bool   `json:"provisioned"`
	Detail      string `json:"detail,omitempty"`
}

// Report is the result of Status.
type Report struct {
	Project    string             `json:"project"`
	Database   string             `json:"database"`
	Containers []engine.Container `json:"containers"`
	Modules    []ModuleStatus     `json:"modules"`
}

// Status reports container state, readiness and whether each module is provisioned.
func (s *Stack) Status(ctx context.Context) (*Report, error) {
	st, err := s.Store.Load()
	if err != nil {
		return nil, err
	}
	l, err := render.Resolve(s.Cat, s.Cfg, st)
	if err != nil {
		return nil, err
	}
	cs, err := s.Eng.PS(ctx, s.project())
	if err != nil {
		return nil, err
	}
	rep := &Report{Project: s.Cfg.Project, Database: string(s.Cfg.Database.Mode), Containers: cs}
	for _, m := range l.Modules {
		ms := ModuleStatus{Module: m.Key, URL: m.BaseURL(s.Cfg.Bind)}
		for _, c := range cs {
			if c.Service == m.Key {
				ms.Container = c.State
			}
		}
		if ms.Container == "" {
			ms.Container = "not created"
		}
		if ms.Container == "running" {
			cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			if err := health.Check(cctx, httpClient(), ms.URL, m.Mod.Ready); err == nil {
				ms.Ready = true
			} else {
				ms.Detail = err.Error()
			}
			cancel()
		}
		if mst, ok := st.Modules[m.Key]; ok && mst.Credentials != nil {
			ms.Provisioned = true
		}
		rep.Modules = append(rep.Modules, ms)
	}
	return rep, nil
}

// Logs streams container logs.
func (s *Stack) Logs(ctx context.Context, w io.Writer, o engine.LogOptions, modules ...string) error {
	return s.Eng.Logs(ctx, s.project(), w, o, modules...)
}

// DownOption tunes Down.
type DownOption func(*downOptions)

type downOptions struct{ volumes bool }

// WithVolumes also deletes the data volumes and the saved state.
func WithVolumes() DownOption { return func(o *downOptions) { o.volumes = true } }

// Down stops the stack. With WithVolumes it also removes data volumes, generated files and
// saved credentials; the secrets are gone for good, so the data could not be read anyway.
func (s *Stack) Down(ctx context.Context, opts ...DownOption) error {
	var do downOptions
	for _, f := range opts {
		f(&do)
	}
	if err := s.Store.Lock(); err != nil {
		return err
	}
	defer s.Store.Unlock()
	if _, err := os.Stat(filepath.Join(s.Dir, render.Compose)); err != nil {
		return tsierr.New(tsierr.CodeNotFound, "", "down", "nothing to stop: "+render.Compose+" does not exist", "", nil)
	}
	if err := s.Eng.Down(ctx, s.project(), do.volumes); err != nil {
		return err
	}
	if do.volumes {
		for _, p := range []string{StateDir, render.Compose, "tsi.lock"} {
			if err := os.RemoveAll(filepath.Join(s.Dir, p)); err != nil {
				return err
			}
		}
	}
	return nil
}
