// Package secrets generates module secrets and keeps the state file that holds everything
// the modules cannot give back: generated keys, administrator passwords, tenant API
// secrets. The state file is the only copy, so writes are atomic, locked and 0600.
package secrets

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"gopkg.in/yaml.v3"

	"github.com/tsi-coop/tsi-cli/internal/redact"
	"github.com/tsi-coop/tsi-cli/tsierr"
)

// Generate produces a secret by generator name.
func Generate(gen string) (string, error) {
	switch gen {
	case "random32": // 32 random bytes, hex encoded (64 chars)
		return hexBytes(32)
	case "hex16":
		return hexBytes(16)
	case "token":
		return hexBytes(24)
	case "password":
		return alnum(28)
	case "strong":
		return strong(28)
	}
	return "", fmt.Errorf("unknown generator %q", gen)
}

func hexBytes(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

const alphabet = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func alnum(n int) (string, error) {
	out := make([]byte, n)
	max := big.NewInt(int64(len(alphabet)))
	for i := range out {
		v, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		out[i] = alphabet[v.Int64()]
	}
	return string(out), nil
}

// StepState records one completed bootstrap step.
type StepState struct {
	Done bool              `yaml:"done"`
	Vars map[string]string `yaml:"vars,omitempty"`
}

// Credentials are the tenant credentials a module returned once.
type Credentials struct {
	BaseURL   string `yaml:"base_url"`
	APIKey    string `yaml:"api_key"`
	APISecret string `yaml:"api_secret"`
	// AdminEmail and AdminPassword are set for modules driven by an admin login.
	AdminEmail    string `yaml:"admin_email,omitempty"`
	AdminPassword string `yaml:"admin_password,omitempty"`
	// Extra holds module-specific identifiers (fiduciary_id, app_id...).
	Extra map[string]string `yaml:"extra,omitempty"`
}

// ModuleState is everything saved for one module.
type ModuleState struct {
	Secrets       map[string]string `yaml:"secrets,omitempty"`
	AdminPassword string            `yaml:"admin_password,omitempty"`
	DBPassword    string            `yaml:"db_password,omitempty"`
	// DataInitialized is set once migrations have run, after which immutable secrets are locked.
	DataInitialized bool                 `yaml:"data_initialized,omitempty"`
	Steps           map[string]StepState `yaml:"steps,omitempty"`
	Credentials     *Credentials         `yaml:"credentials,omitempty"`
}

// State is the contents of .tsi/state.yaml.
type State struct {
	Version         int                     `yaml:"version"`
	DBAdminPassword string                  `yaml:"db_admin_password,omitempty"`
	Modules         map[string]*ModuleState `yaml:"modules"`
}

// Store reads and writes the state file under a directory.
type Store struct {
	Dir  string
	lock *os.File
}

// StatePath is the state file location.
func (s *Store) StatePath() string { return filepath.Join(s.Dir, "state.yaml") }

// Lock takes an exclusive lock so two tsi processes cannot interleave state writes.
func (s *Store) Lock() error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(s.Dir, "lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return tsierr.New(tsierr.CodeUsage, "", "", "another tsi process is using this project", "wait for it to finish", err)
	}
	s.lock = f
	return nil
}

// Unlock releases the lock.
func (s *Store) Unlock() {
	if s.lock != nil {
		_ = syscall.Flock(int(s.lock.Fd()), syscall.LOCK_UN)
		s.lock.Close()
		s.lock = nil
	}
}

// Load reads the state, returning an empty one if the file does not exist.
func (s *Store) Load() (*State, error) {
	st := &State{Version: 1, Modules: map[string]*ModuleState{}}
	b, err := os.ReadFile(s.StatePath())
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(b, st); err != nil {
		return nil, tsierr.New(tsierr.CodeStateLost, "", "", "state file is corrupt", "restore "+s.StatePath()+" from a backup", err)
	}
	if st.Modules == nil {
		st.Modules = map[string]*ModuleState{}
	}
	st.registerSecrets()
	return st, nil
}

// Exists reports whether a state file is present.
func (s *Store) Exists() bool {
	_, err := os.Stat(s.StatePath())
	return err == nil
}

// Save writes the state atomically with mode 0600.
func (s *Store) Save(st *State) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	b, err := yaml.Marshal(st)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.Dir, "state-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	st.registerSecrets()
	return os.Rename(tmp.Name(), s.StatePath())
}

// Module returns (creating if needed) the state of a module.
func (st *State) Module(name string) *ModuleState {
	m, ok := st.Modules[name]
	if !ok {
		m = &ModuleState{}
		st.Modules[name] = m
	}
	if m.Secrets == nil {
		m.Secrets = map[string]string{}
	}
	if m.Steps == nil {
		m.Steps = map[string]StepState{}
	}
	return m
}

// EnsureSecret returns the saved secret, generating and recording it if absent.
func (st *State) EnsureSecret(module, name, generator string) (string, error) {
	m := st.Module(module)
	if v, ok := m.Secrets[name]; ok && v != "" {
		return v, nil
	}
	v, err := Generate(generator)
	if err != nil {
		return "", err
	}
	m.Secrets[name] = v
	return v, nil
}

// CheckImmutable refuses a change to an immutable secret once data exists.
func (st *State) CheckImmutable(module, name, newValue string) error {
	m := st.Module(module)
	old, ok := m.Secrets[name]
	if !ok || old == newValue || !m.DataInitialized {
		return nil
	}
	return tsierr.New(tsierr.CodeImmutable, module, "", name+" cannot change once the database holds data",
		"keep the original value, or run `tsi compose down --volumes` to start over (this deletes the data)", nil)
}

func (st *State) registerSecrets() {
	if st.DBAdminPassword != "" {
		redact.Add(st.DBAdminPassword)
	}
	for _, m := range st.Modules {
		for _, v := range m.Secrets {
			redact.Add(v)
		}
		redact.Add(m.AdminPassword, m.DBPassword)
		if m.Credentials != nil {
			redact.Add(m.Credentials.APISecret, m.Credentials.AdminPassword)
		}
		for _, ss := range m.Steps {
			for k, v := range ss.Vars {
				if k == "token" || k == "api_secret" {
					redact.Add(v)
				}
			}
		}
	}
}

const specials = "!@%^*_+"

// strong returns a password with at least two characters from each of upper, lower,
// digit and special, avoiding characters that need quoting in env files, SQL or URLs.
func strong(n int) (string, error) {
	classes := []string{"ABCDEFGHJKLMNPQRSTUVWXYZ", "abcdefghijkmnopqrstuvwxyz", "23456789", specials}
	out := make([]byte, 0, n)
	pick := func(set string) error {
		v, err := rand.Int(rand.Reader, big.NewInt(int64(len(set))))
		if err != nil {
			return err
		}
		out = append(out, set[v.Int64()])
		return nil
	}
	for _, c := range classes {
		for i := 0; i < 2; i++ {
			if err := pick(c); err != nil {
				return "", err
			}
		}
	}
	all := strings.Join(classes, "")
	for len(out) < n {
		if err := pick(all); err != nil {
			return "", err
		}
	}
	// Fisher-Yates shuffle so the guaranteed characters are not always first.
	for i := len(out) - 1; i > 0; i-- {
		j, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return "", err
		}
		out[i], out[j.Int64()] = out[j.Int64()], out[i]
	}
	return string(out), nil
}
