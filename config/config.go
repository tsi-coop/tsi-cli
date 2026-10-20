// Package config holds the user's intent (tsi.yaml): which modules, which database mode,
// ports, overrides and bootstrap choices. It never contains generated secrets.
package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/tsi-coop/tsi-cli/tsierr"
)

// DBMode selects where module databases live.
type DBMode string

const (
	// Shared runs one Postgres container with one database and role per module.
	Shared DBMode = "shared"
	// PerModule runs one Postgres container per module, as the upstream compose files do.
	PerModule DBMode = "per-module"
	// External uses a Postgres the user already runs.
	External DBMode = "external"
)

// DefaultPortOffset is added to every module's upstream host port in a new project, so a
// stack made by tsi does not collide with modules run straight from their own repositories
// (8080-8088 for apps, 5432-5440 for databases). Set port_offset: 0 to use the upstream ports.
const DefaultPortOffset = 300

// FileName is the default config file.
const FileName = "tsi.yaml"

// Config is the contents of tsi.yaml.
type Config struct {
	Version  int      `yaml:"version"`
	Project  string   `yaml:"project"`
	Modules  []string `yaml:"modules"`
	Database Database `yaml:"database"`
	// PortOffset is added to every default host port. It is always written, because a missing
	// value means DefaultPortOffset, so an explicit 0 has to be kept.
	PortOffset int `yaml:"port_offset"`
	// Bind is the host address ports are published on. Default 127.0.0.1.
	Bind string `yaml:"bind,omitempty"`
	// Overrides sets module environment values: module -> NAME -> value.
	Overrides map[string]map[string]string `yaml:"overrides,omitempty"`
	Admin     Admin                        `yaml:"admin"`
	// MigrationDirs makes tsi read a module's migration files from a local directory
	// (module -> path) instead of downloading the pinned release. For module development
	// and offline use.
	MigrationDirs map[string]string `yaml:"migration_dirs,omitempty"`
	// Bootstrap overrides each module's provisioning spec: module -> key -> value.
	Bootstrap map[string]map[string]any `yaml:"bootstrap,omitempty"`
}

// Database configures database placement.
type Database struct {
	Mode DBMode `yaml:"mode"`
	// Port is the host port of the shared Postgres.
	Port int `yaml:"port,omitempty"`
	// Host, Port and AdminUser/AdminPassword-less DSN for external mode:
	// DSN is an admin connection string (it must be able to create roles and databases).
	DSN string `yaml:"dsn,omitempty"`
	// ModuleHost is how module containers reach an external database (default host.docker.internal).
	ModuleHost string `yaml:"module_host,omitempty"`
}

// Admin is the administrator identity created in every module.
type Admin struct {
	Name  string `yaml:"name"`
	Email string `yaml:"email"`
}

// SplitRef splits a modules entry into the catalog module and an optional instance name:
// "tsi-dx-node:node-a" is the module tsi-dx-node run as an instance called node-a. Without
// an instance the key is the module name.
func SplitRef(ref string) (module, instance string) {
	if i := strings.Index(ref, ":"); i >= 0 {
		return ref[:i], ref[i+1:]
	}
	return ref, ""
}

// Key is the name a modules entry goes by: its instance name, or the module name.
// It is the compose service name, the state key and the env file name.
func Key(ref string) string {
	m, inst := SplitRef(ref)
	if inst != "" {
		return inst
	}
	return m
}

var instanceRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}$`)

var projectRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
var emailRe = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// Default returns a config with sensible defaults for the given modules.
func Default(modules ...string) *Config {
	return &Config{
		Version:    1,
		Project:    "tsi",
		Modules:    modules,
		Database:   Database{Mode: Shared, Port: 5432},
		Bind:       "127.0.0.1",
		PortOffset: DefaultPortOffset,
		Admin:      Admin{Name: "Administrator", Email: "admin@example.org"},
	}
}

// Validate checks the config and fills defaults.
func (c *Config) Validate() error {
	if c.Version == 0 {
		c.Version = 1
	}
	if c.Version != 1 {
		return fmt.Errorf("unsupported tsi.yaml version %d", c.Version)
	}
	if c.Project == "" {
		c.Project = "tsi"
	}
	if !projectRe.MatchString(c.Project) {
		return fmt.Errorf("project %q must be lowercase letters, digits, - or _", c.Project)
	}
	if len(c.Modules) == 0 {
		return fmt.Errorf("no modules selected")
	}
	seen := map[string]bool{}
	for _, ref := range c.Modules {
		mod, inst := SplitRef(ref)
		if mod == "" {
			return fmt.Errorf("empty module name in %q", ref)
		}
		if strings.Contains(ref, ":") && !instanceRe.MatchString(inst) {
			return fmt.Errorf("instance name %q must be lowercase letters, digits and -, starting with a letter", inst)
		}
		if seen[Key(ref)] {
			return fmt.Errorf("%q listed twice (give each instance of a module its own name, like %s:node-a)", Key(ref), mod)
		}
		seen[Key(ref)] = true
	}
	switch c.Database.Mode {
	case "":
		c.Database.Mode = Shared
	case Shared, PerModule, External:
	default:
		return fmt.Errorf("database.mode must be shared, per-module or external, got %q", c.Database.Mode)
	}
	if c.Database.Mode == External && c.Database.DSN == "" {
		return fmt.Errorf("database.dsn is required when database.mode is external")
	}
	if c.Database.Port == 0 {
		c.Database.Port = 5432
	}
	if c.Database.ModuleHost == "" {
		c.Database.ModuleHost = "host.docker.internal"
	}
	if c.Bind == "" {
		c.Bind = "127.0.0.1"
	}
	if c.PortOffset < 0 || c.PortOffset > 50000 {
		return fmt.Errorf("port_offset out of range")
	}
	if c.Admin.Name == "" {
		c.Admin.Name = "Administrator"
	}
	if !emailRe.MatchString(c.Admin.Email) {
		return fmt.Errorf("admin.email %q is not a valid email address", c.Admin.Email)
	}
	return nil
}

// Load reads and validates a config file.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, tsierr.New(tsierr.CodeNotFound, "", "", path+" not found",
				"run `tsi compose init` in this directory first", err)
		}
		return nil, err
	}
	// Start from the defaults: a key that is absent from the file keeps its default, so a
	// tsi.yaml without port_offset gets the default offset rather than the upstream ports.
	c := Config{PortOffset: DefaultPortOffset}
	dec := yaml.NewDecoder(bytesReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return nil, tsierr.New(tsierr.CodeUsage, "", "", "cannot parse "+path, "fix the YAML syntax or unknown key", err)
	}
	if err := c.Validate(); err != nil {
		return nil, tsierr.New(tsierr.CodeUsage, "", "", "invalid "+path+": "+err.Error(), "", nil)
	}
	return &c, nil
}

// Save writes the config (it contains no secrets) with mode 0644.
func (c *Config) Save(path string) error {
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// AppPort returns the host port for an app given its default.
func (c *Config) AppPort(def int) int { return def + c.PortOffset }

// SharedDBPort returns the host port of the shared Postgres.
func (c *Config) SharedDBPort() int { return c.Database.Port + c.PortOffset }

// ModuleDBPort returns the host port of a per-module Postgres.
func (c *Config) ModuleDBPort(def int) int { return def + c.PortOffset }
