// Package render turns the config, the catalog and the saved secrets into the files that
// are handed to Docker: the compose file and one env file per container. Rendering is
// deterministic, so the same inputs always give byte-identical output.
package render

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"

	"github.com/tsicoop/tsi-cli/catalog"
	"github.com/tsicoop/tsi-cli/config"
	"github.com/tsicoop/tsi-cli/secrets"
	"github.com/tsicoop/tsi-cli/tsierr"
)

// Paths used in the generated compose file, relative to the project directory.
const (
	EnvDir   = ".tsi/env"
	CertDir  = ".tsi/certs"
	Compose  = "docker-compose.tsi.yml"
	PGUser   = "tsi_admin"
	pgImage  = "postgres:15-alpine"
	pgShared = "postgres"
)

var identRe = regexp.MustCompile(`[^a-zA-Z0-9]`)

// KV is one environment entry.
type KV struct{ Key, Value string }

// Module is a module resolved against the config and state.
type Module struct {
	Mod *catalog.Module
	// Key names this instance: its instance name, or the module name. It is the compose
	// service, the state key and the env file name.
	Key string
	// Index is the position among instances of the same module; it shifts host ports.
	Index int
	// Hostname is the name peers use to reach this instance on the compose network.
	Hostname string
	// Instance is true when the module was given an explicit instance name.
	Instance   bool
	AppPort    int // host port
	DBHostPort int // host port of a per-module Postgres
	DBHost     string
	DBPort     int // port inside the network (or of the external server)
	DBName     string
	DBUser     string
	DBPass     string
	Origin     string
	Env        []KV
	EnvFile    string // path relative to the project dir
	Keystore   string // host path relative to the project dir, empty if none
}

// Layout is the resolved stack.
type Layout struct {
	Cfg     *config.Config
	Modules []*Module
	// SharedDB is true when a shared Postgres container is part of the stack.
	SharedDB bool
}

// Resolve computes every module's runtime values, generating missing secrets into st.
func Resolve(cat *catalog.Catalog, cfg *config.Config, st *secrets.State) (*Layout, error) {
	l := &Layout{Cfg: cfg, SharedDB: cfg.Database.Mode == config.Shared}
	if l.SharedDB && st.DBAdminPassword == "" {
		pw, err := secrets.Generate("password")
		if err != nil {
			return nil, err
		}
		st.DBAdminPassword = pw
	}
	count := map[string]int{}
	for _, ref := range cfg.Modules {
		base, inst := config.SplitRef(ref)
		m, err := cat.Get(base)
		if err != nil {
			return nil, err
		}
		rm, err := resolveModule(m, config.Key(ref), inst != "", count[base], cfg, st)
		if err != nil {
			return nil, err
		}
		count[base]++
		l.Modules = append(l.Modules, rm)
	}
	return l, nil
}

func resolveModule(m *catalog.Module, key string, instance bool, idx int, cfg *config.Config, st *secrets.State) (*Module, error) {
	ms := st.Module(key)
	if ms.DBPassword == "" {
		pw, err := secrets.Generate("password")
		if err != nil {
			return nil, err
		}
		ms.DBPassword = pw
	}
	dbName, dbUser := m.DB.Name, m.DB.User
	if instance {
		suffix := "_" + identRe.ReplaceAllString(key, "_")
		dbName, dbUser = dbName+suffix, dbUser+suffix
	}
	rm := &Module{
		Mod:        m,
		Key:        key,
		Index:      idx,
		Instance:   instance,
		AppPort:    cfg.AppPort(m.Ports.App + idx),
		DBHostPort: cfg.ModuleDBPort(m.Ports.DB + idx),
		DBName:     dbName,
		DBUser:     dbUser,
		DBPass:     ms.DBPassword,
		EnvFile:    EnvDir + "/" + key + ".env",
	}
	rm.Hostname = key
	if id := m.Identity; id != nil && idx < len(id.Hostnames) {
		rm.Hostname = id.Hostnames[idx]
	}
	rm.Origin = fmt.Sprintf("http://localhost:%d,http://127.0.0.1:%d", rm.AppPort, rm.AppPort)
	switch cfg.Database.Mode {
	case config.Shared:
		rm.DBHost, rm.DBPort = pgShared, 5432
	case config.PerModule:
		rm.DBHost, rm.DBPort = key+"-db", 5432
	case config.External:
		host, port, err := externalHostPort(cfg)
		if err != nil {
			return nil, err
		}
		_ = host
		rm.DBHost, rm.DBPort = cfg.Database.ModuleHost, port
	}

	ctx := map[string]any{
		"DBHost": rm.DBHost, "DBPort": rm.DBPort, "DBName": rm.DBName,
		"DBUser": rm.DBUser, "DBPass": rm.DBPass, "Origin": rm.Origin,
	}
	overrides := cfg.Overrides[key]
	for name := range overrides {
		if !hasEnv(m, name) {
			return nil, tsierr.New(tsierr.CodeUsage, key, "", "override for unknown variable "+name,
				"check the variable name in the module's env list", nil)
		}
	}
	for _, ev := range m.Env {
		val, err := envValue(m, key, ev, ctx, st)
		if err != nil {
			return nil, err
		}
		if o, ok := overrides[ev.Name]; ok {
			if ev.Immutable {
				if err := st.CheckImmutable(key, ev.Name, o); err != nil {
					return nil, err
				}
				ms.Secrets[ev.Name] = o
			}
			val = o
		}
		rm.Env = append(rm.Env, KV{ev.Name, val})
	}
	if ks := m.Keystore; ks != nil {
		pass, err := st.EnsureSecret(key, "KEYSTORE_PASSWORD", "password")
		if err != nil {
			return nil, err
		}
		rm.Keystore = CertDir + "/" + key + ".p12"
		rm.Env = append(rm.Env,
			KV{ks.PathEnv, ks.ContainerPath}, KV{ks.PassEnv, pass}, KV{ks.AliasEnv, ks.Alias})
	}
	return rm, nil
}

func hasEnv(m *catalog.Module, name string) bool {
	for _, e := range m.Env {
		if e.Name == name {
			return true
		}
	}
	return false
}

func envValue(m *catalog.Module, key string, ev catalog.EnvVar, ctx map[string]any, st *secrets.State) (string, error) {
	switch {
	case ev.Generator != "":
		return st.EnsureSecret(key, ev.Name, ev.Generator)
	case ev.Value != "":
		t, err := template.New(ev.Name).Option("missingkey=error").Parse(ev.Value)
		if err != nil {
			return "", fmt.Errorf("%s: env %s: %w", m.Name, ev.Name, err)
		}
		var b bytes.Buffer
		if err := t.Execute(&b, ctx); err != nil {
			return "", fmt.Errorf("%s: env %s: %w", m.Name, ev.Name, err)
		}
		return b.String(), nil
	}
	return ev.Default, nil
}

// EnvFile renders a module's env file. Compose interpolates $ in env files, so it is escaped.
func (m *Module) EnvFileContent() []byte {
	var b strings.Builder
	b.WriteString("# generated by tsi, do not edit; changes are overwritten\n")
	for _, kv := range m.Env {
		fmt.Fprintf(&b, "%s=%s\n", kv.Key, strings.ReplaceAll(kv.Value, "$", "$$"))
	}
	return []byte(b.String())
}

// EnvValue returns a resolved env value by name.
func (m *Module) EnvValue(name string) string {
	for _, kv := range m.Env {
		if kv.Key == name {
			return kv.Value
		}
	}
	return ""
}

// BaseURL is how the host reaches the app.
func (m *Module) BaseURL(bind string) string {
	host := bind
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s:%d", host, m.AppPort)
}

// PostgresEnvFileContent is the env file of the shared Postgres container.
func (l *Layout) PostgresEnvFileContent(adminPass string) []byte {
	return []byte(fmt.Sprintf("# generated by tsi, do not edit\nPOSTGRES_USER=%s\nPOSTGRES_PASSWORD=%s\n",
		PGUser, strings.ReplaceAll(adminPass, "$", "$$")))
}

// DBEnvFileContent is the env file of a per-module Postgres container.
func (m *Module) DBEnvFileContent() []byte {
	return []byte(fmt.Sprintf("# generated by tsi, do not edit\nPOSTGRES_DB=%s\nPOSTGRES_USER=%s\nPOSTGRES_PASSWORD=%s\n",
		m.DBName, m.DBUser, strings.ReplaceAll(m.DBPass, "$", "$$")))
}

type service struct {
	Image       string            `yaml:"image"`
	Restart     string            `yaml:"restart,omitempty"`
	Ports       []string          `yaml:"ports,omitempty"`
	EnvFile     []string          `yaml:"env_file,omitempty"`
	Volumes     []string          `yaml:"volumes,omitempty"`
	Healthcheck *healthcheck      `yaml:"healthcheck,omitempty"`
	DependsOn   map[string]dep    `yaml:"depends_on,omitempty"`
	ExtraHosts  []string          `yaml:"extra_hosts,omitempty"`
	Networks    map[string]attach `yaml:"networks,omitempty"`
	User        string            `yaml:"user,omitempty"`
}

type attach struct {
	Aliases []string `yaml:"aliases,omitempty"`
}

type dep struct {
	Condition string `yaml:"condition"`
}

type healthcheck struct {
	Test     []string `yaml:"test"`
	Interval string   `yaml:"interval"`
	Timeout  string   `yaml:"timeout"`
	Retries  int      `yaml:"retries"`
}

type composeFile struct {
	Name     string              `yaml:"name"`
	Services map[string]*service `yaml:"services"`
	Volumes  map[string]struct{} `yaml:"volumes,omitempty"`
}

// DBServiceNames returns the names of the database services in the stack.
func (l *Layout) DBServiceNames() []string {
	var n []string
	switch l.Cfg.Database.Mode {
	case config.Shared:
		n = append(n, pgShared)
	case config.PerModule:
		for _, m := range l.Modules {
			n = append(n, m.Key+"-db")
		}
	}
	return n
}

// AppServiceNames returns the names of the module services.
func (l *Layout) AppServiceNames() []string {
	var n []string
	for _, m := range l.Modules {
		n = append(n, m.Key)
	}
	return n
}

// ComposeYAML renders the compose file.
func (l *Layout) ComposeYAML() ([]byte, error) {
	cf := composeFile{Name: l.Cfg.Project, Services: map[string]*service{}, Volumes: map[string]struct{}{}}
	bind := l.Cfg.Bind
	pgHealth := &healthcheck{
		Test:     []string{"CMD-SHELL", "pg_isready -U $${POSTGRES_USER}"},
		Interval: "5s", Timeout: "5s", Retries: 20,
	}
	switch l.Cfg.Database.Mode {
	case config.Shared:
		cf.Services[pgShared] = &service{
			Image: pgImage, Restart: "unless-stopped",
			Ports:       []string{fmt.Sprintf("%s:%d:5432", bind, l.Cfg.SharedDBPort())},
			EnvFile:     []string{EnvDir + "/postgres.env"},
			Volumes:     []string{"pgdata:/var/lib/postgresql/data"},
			Healthcheck: pgHealth,
		}
		cf.Volumes["pgdata"] = struct{}{}
	case config.PerModule:
		for _, m := range l.Modules {
			vol := m.Key + "-pgdata"
			cf.Services[m.Key+"-db"] = &service{
				Image: pgImage, Restart: "unless-stopped",
				Ports:       []string{fmt.Sprintf("%s:%d:5432", bind, m.DBHostPort)},
				EnvFile:     []string{EnvDir + "/" + m.Key + "-db.env"},
				Volumes:     []string{vol + ":/var/lib/postgresql/data"},
				Healthcheck: pgHealth,
			}
			cf.Volumes[vol] = struct{}{}
		}
	}
	for _, m := range l.Modules {
		s := &service{
			Image:   m.Mod.Image.Ref(),
			Restart: "unless-stopped",
			Ports:   []string{fmt.Sprintf("%s:%d:%d", bind, m.AppPort, m.Mod.ContainerPort)},
			EnvFile: []string{m.EnvFile},
			User:    "root",
		}
		for _, v := range m.Mod.Volumes {
			vol := m.Key + "-" + v.Name
			s.Volumes = append(s.Volumes, vol+":"+v.Path)
			cf.Volumes[vol] = struct{}{}
		}
		if m.Keystore != "" {
			s.Volumes = append(s.Volumes, "./"+m.Keystore+":"+m.Mod.Keystore.ContainerPath+":ro")
		}
		switch l.Cfg.Database.Mode {
		case config.Shared:
			s.DependsOn = map[string]dep{pgShared: {"service_healthy"}}
		case config.PerModule:
			s.DependsOn = map[string]dep{m.Key + "-db": {"service_healthy"}}
		case config.External:
			s.ExtraHosts = []string{"host.docker.internal:host-gateway"}
		}
		if m.Hostname != m.Key {
			// Reachable by the name its TLS certificate is valid for.
			s.Networks = map[string]attach{"default": {Aliases: []string{m.Hostname}}}
		}
		cf.Services[m.Key] = s
	}
	b, err := yaml.Marshal(&cf)
	if err != nil {
		return nil, err
	}
	header := "# generated by tsi, do not edit; regenerate with `tsi compose init --force` or `tsi compose up`\n"
	return append([]byte(header), b...), nil
}

// ModuleNames returns sorted module names for stable iteration.
func (l *Layout) ModuleNames() []string {
	var n []string
	for _, m := range l.Modules {
		n = append(n, m.Key)
	}
	sort.Strings(n)
	return n
}
