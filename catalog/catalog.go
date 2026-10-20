// Package catalog holds the declarative description of every TSI module: image, ports,
// environment, migrations, readiness probe, bootstrap steps and smoke tests. A module is
// data, so adding one is a YAML file and its migrations, not new Go code.
package catalog

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/tsicoop/tsi-cli/tsierr"
)

//go:embed modules/*.yaml
var content embed.FS

var (
	sha256Re = regexp.MustCompile(`^[0-9a-f]{64}$`)
	repoRe   = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
)

// Module describes one TSI product.
type Module struct {
	Name          string     `yaml:"name"`
	Title         string     `yaml:"title"`
	Purpose       string     `yaml:"purpose"`
	Image         Image      `yaml:"image"`
	ContainerPort int        `yaml:"container_port"`
	Ports         Ports      `yaml:"ports"`
	DB            DBSpec     `yaml:"db"`
	Env           []EnvVar   `yaml:"env"`
	Keystore      *Keystore  `yaml:"keystore"`
	Volumes       []Volume   `yaml:"volumes"`
	Migrations    Migrations `yaml:"migrations"`
	Ready         Probe      `yaml:"ready"`
	// Session is "bearer" or "cookie": how admin calls are authenticated after login.
	Session string `yaml:"session"`
	// SetupToken names the env var holding the one-time setup token, if the module has one.
	SetupToken string `yaml:"setup_token"`
	// Credentials says what a user is given at the end of bootstrap: "apikey" (the default:
	// an X-API-Key/X-API-Secret pair for tenant calls) or "admin" (the administrator login,
	// for modules whose API is driven by an admin session).
	Credentials string `yaml:"credentials"`
	// AdminPassword names the generator for the administrator password ("strong" adds
	// upper, lower, digit and special characters, for modules that enforce a policy).
	AdminPassword string `yaml:"admin_password"`
	// Identity, when set, gives each instance an X.509 identity and node details, and
	// lets instances of this module find each other as peers.
	Identity *Identity `yaml:"identity"`
	// MinPasswordLen is the shortest admin password the module accepts.
	MinPasswordLen int `yaml:"min_password_len"`
	// SpecDefaults seeds the per-module bootstrap spec.
	SpecDefaults map[string]any `yaml:"spec_defaults"`
	// SpecReplace lists dotted spec paths whose map values a user replaces wholesale instead of
	// merging into the default (a policy document, a JSON schema).
	SpecReplace []string    `yaml:"spec_replace"`
	Bootstrap   []Step      `yaml:"bootstrap"`
	Smoke       []SmokeTest `yaml:"smoke"`
}

// Image is a pinned container image.
type Image struct {
	Repo string `yaml:"repo"`
	Tag  string `yaml:"tag"`
}

// Ref returns repo:tag.
func (i Image) Ref() string { return i.Repo + ":" + i.Tag }

// Ports are the default host ports the upstream compose file uses.
type Ports struct {
	App int `yaml:"app"`
	DB  int `yaml:"db"`
}

// DBSpec names the database and role this module uses.
type DBSpec struct {
	Name       string   `yaml:"name"`
	User       string   `yaml:"user"`
	Extensions []string `yaml:"extensions"`
}

// EnvVar is one environment variable of the module.
type EnvVar struct {
	Name string `yaml:"name"`
	// Generator, when set, produces the value once and keeps it in state: random32, hex16, password, token.
	Generator string `yaml:"generator"`
	// Value is a template over the render context (see render package).
	Value   string `yaml:"value"`
	Default string `yaml:"default"`
	Secret  bool   `yaml:"secret"`
	// Immutable secrets cannot change once data exists.
	Immutable bool `yaml:"immutable"`
}

// Identity describes a module whose instances talk to each other.
type Identity struct {
	// P2PPort is the port peers use to reach this module inside the compose network.
	P2PPort int `yaml:"p2p_port"`
	// MaxInstances limits how many instances a stack may hold (the bootstrap recipe pairs them).
	MaxInstances int `yaml:"max_instances"`
	// Hostnames are the names the instances answer to on the compose network, in order. The
	// image's built-in TLS certificate is only valid for specific names, so peers must use them.
	Hostnames []string `yaml:"hostnames"`
}

// Keystore describes a PKCS#12 keystore the module needs mounted.
type Keystore struct {
	PathEnv       string `yaml:"path_env"`
	PassEnv       string `yaml:"pass_env"`
	AliasEnv      string `yaml:"alias_env"`
	Alias         string `yaml:"alias"`
	ContainerPath string `yaml:"container_path"`
	CommonName    string `yaml:"common_name"`
}

// Volume is a named volume mounted into the app container.
type Volume struct {
	Name string `yaml:"name"`
	Path string `yaml:"path"`
}

// Migrations lists SQL files (and special actions) applied in order. The SQL is not kept
// here: it is fetched from the module's repository at a pinned ref and checked against
// the SHA-256 recorded for each file.
type Migrations struct {
	Source Source          `yaml:"source"`
	Steps  []MigrationStep `yaml:"steps"`
}

// Source is where a module's migration files live.
type Source struct {
	// Repo is "owner/name" on GitHub.
	Repo string `yaml:"repo"`
	// Ref is a tag or a full commit SHA. Branch names are not allowed: they move.
	Ref string `yaml:"ref"`
	// Dir is the directory inside the repository.
	Dir string `yaml:"dir"`
}

// MigrationStep is a SQL file or a named action.
type MigrationStep struct {
	File string `yaml:"file"`
	// SHA256 is the expected hex digest of File.
	SHA256 string `yaml:"sha256"`
	Action string `yaml:"action"` // "set_database_setting"
	// For set_database_setting.
	Setting string `yaml:"setting"`
	FromEnv string `yaml:"from_env"`
}

// ID is a stable identifier used in the migration ledger.
func (s MigrationStep) ID() string {
	if s.File != "" {
		return s.File
	}
	return s.Action + ":" + s.Setting
}

// Probe is a cheap unauthenticated call that proves the app and its database are up.
type Probe struct {
	Path   string         `yaml:"path"`
	Body   map[string]any `yaml:"body"`
	Expect []int          `yaml:"expect"`
}

// Step is one API call in the bootstrap sequence.
type Step struct {
	ID      string            `yaml:"id"`
	Path    string            `yaml:"path"`
	Body    map[string]any    `yaml:"body"`
	Auth    string            `yaml:"auth"` // "" or "bearer"
	Headers map[string]string `yaml:"headers"`
	Expect  []int             `yaml:"expect"` // default 200, 201
	// AlreadyDone statuses mean a previous run completed this step (for example 409).
	AlreadyDone []int `yaml:"already_done"`
	// Capture maps a variable name to a dotted JSON path in the response.
	Capture map[string]string `yaml:"capture"`
	// Secret lists captured variables that are secrets (shown once by the module).
	Secret []string `yaml:"secret"`
	// Phase orders bootstrap across instances: every instance finishes phase 1 before any
	// starts phase 2, and so on. Steps default to phase 1.
	Phase int `yaml:"phase"`
	// Poll repeats the request every two seconds, up to this many seconds, until a Select
	// finds its item. For state that settles asynchronously (a partner turning Active).
	Poll int `yaml:"poll"`
	// Foreach repeats the step for every item of the list at this dotted path in the template
	// context (for example Spec.entities); the item is available as .Item.
	Foreach string `yaml:"foreach"`
	// AlreadyDoneBody treats a failing response whose body contains this text as "already done".
	AlreadyDoneBody string `yaml:"already_done_body"`
	// Select picks one item out of a list response and captures fields from it (instead
	// of Capture, which reads the response itself).
	Select *Select `yaml:"select"`
	// Recover finds the object a previous, interrupted run already created, when the create
	// call answers with one of the AlreadyDone statuses. Without it such a status just
	// means "nothing to do".
	Recover *Recover `yaml:"recover"`
	// Volatile steps (such as login) run every time and their variables are not saved.
	Volatile bool `yaml:"volatile"`
	// When is a dotted path into the template context; the step runs only if it is truthy.
	When string `yaml:"when"`
	// Unless skips the step when this dotted path is truthy (typically a variable set by an
	// earlier look-up step: "create it unless it already exists").
	Unless      string `yaml:"unless"`
	Description string `yaml:"description"`
}

// Select matches an item in a list response.
type Select struct {
	Match   map[string]string `yaml:"match"`
	Capture map[string]string `yaml:"capture"`
	// From is a dotted path to the list inside the response when it is not the response
	// itself (for example "participants").
	From string `yaml:"from"`
	// Optional means finding no item is not an error; the variables stay unset.
	Optional bool `yaml:"optional"`
}

// Recover looks an existing object up so its identifiers can be captured again.
type Recover struct {
	Path string         `yaml:"path"`
	Body map[string]any `yaml:"body"`
	// Match selects the item by field values (templates allowed).
	Match map[string]string `yaml:"match"`
	// Capture maps variable names to dotted paths inside the matched item.
	Capture map[string]string `yaml:"capture"`
}

// SmokeTest names a smoke suite entry implemented by tsitest, with its description.
type SmokeTest struct {
	ID          string `yaml:"id"`
	Description string `yaml:"description"`
}

// Catalog is the loaded set of modules.
type Catalog struct {
	modules map[string]*Module
}

// Load reads and validates every embedded module.
func Load() (*Catalog, error) {
	entries, err := fs.ReadDir(content, "modules")
	if err != nil {
		return nil, err
	}
	c := &Catalog{modules: map[string]*Module{}}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		b, err := content.ReadFile(path.Join("modules", e.Name()))
		if err != nil {
			return nil, err
		}
		var m Module
		dec := yaml.NewDecoder(strings.NewReader(string(b)))
		dec.KnownFields(true)
		if err := dec.Decode(&m); err != nil {
			return nil, fmt.Errorf("catalog %s: %w", e.Name(), err)
		}
		if err := m.validate(); err != nil {
			return nil, fmt.Errorf("catalog %s: %w", e.Name(), err)
		}
		c.modules[m.Name] = &m
	}
	return c, nil
}

func (m *Module) validate() error {
	switch {
	case m.Name == "":
		return fmt.Errorf("name is required")
	case m.Image.Repo == "" || m.Image.Tag == "":
		return fmt.Errorf("%s: image repo and tag are required", m.Name)
	case m.Image.Tag == "latest":
		return fmt.Errorf("%s: image tag must be pinned, not latest", m.Name)
	case m.Ports.App == 0 || m.Ports.DB == 0 || m.ContainerPort == 0:
		return fmt.Errorf("%s: ports are required", m.Name)
	case m.DB.Name == "" || m.DB.User == "":
		return fmt.Errorf("%s: db name and user are required", m.Name)
	case m.Ready.Path == "":
		return fmt.Errorf("%s: ready probe is required", m.Name)
	case m.Credentials != "" && m.Credentials != "apikey" && m.Credentials != "admin":
		return fmt.Errorf("%s: credentials must be apikey or admin", m.Name)
	case m.Session != "bearer" && m.Session != "cookie":
		return fmt.Errorf("%s: session must be bearer or cookie", m.Name)
	}
	seen := map[string]bool{}
	for _, s := range m.Bootstrap {
		if s.ID == "" || s.Path == "" {
			return fmt.Errorf("%s: bootstrap steps need id and path", m.Name)
		}
		if seen[s.ID] {
			return fmt.Errorf("%s: duplicate bootstrap step %q", m.Name, s.ID)
		}
		seen[s.ID] = true
	}
	hasFiles := false
	for _, mg := range m.Migrations.Steps {
		if (mg.File == "") == (mg.Action == "") {
			return fmt.Errorf("%s: a migration step needs exactly one of file or action", m.Name)
		}
		if mg.File != "" {
			hasFiles = true
			if !sha256Re.MatchString(mg.SHA256) {
				return fmt.Errorf("%s: migration %s needs a 64-character hex sha256", m.Name, mg.File)
			}
			if strings.ContainsAny(mg.File, `/\`) || strings.Contains(mg.File, "..") {
				return fmt.Errorf("%s: migration file %q must be a plain file name", m.Name, mg.File)
			}
		}
	}
	if hasFiles {
		src := m.Migrations.Source
		if !repoRe.MatchString(src.Repo) || src.Ref == "" || src.Dir == "" || strings.Contains(src.Dir, "..") {
			return fmt.Errorf("%s: migrations.source needs repo (owner/name), ref and dir", m.Name)
		}
		if src.Ref == "main" || src.Ref == "master" || src.Ref == "HEAD" {
			return fmt.Errorf("%s: migrations.source.ref must be a tag or commit, not a branch", m.Name)
		}
	}
	return nil
}

// Get returns a module by name.
func (c *Catalog) Get(name string) (*Module, error) {
	m, ok := c.modules[name]
	if !ok {
		return nil, tsierr.New(tsierr.CodeNotFound, name, "", "unknown module",
			"run `tsi compose modules` to list the available modules", nil)
	}
	return m, nil
}

// Names lists module names in sorted order.
func (c *Catalog) Names() []string {
	var n []string
	for k := range c.modules {
		n = append(n, k)
	}
	sort.Strings(n)
	return n
}
