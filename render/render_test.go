package render

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tsicoop/tsi-cli/catalog"
	"github.com/tsicoop/tsi-cli/config"
	"github.com/tsicoop/tsi-cli/secrets"
)

var update = flag.Bool("update", false, "rewrite golden files")

// fixedState has deterministic secrets so output is byte-stable.
func fixedState() *secrets.State {
	st := &secrets.State{DBAdminPassword: "adminpass-fixed", Modules: map[string]*secrets.ModuleState{
		"tsi-dpdp-cms": {DBPassword: "dbpass-fixed", Secrets: map[string]string{
			"JWT_SECRET": "jwt-fixed", "DB_ENCRYPTION_KEY": "enc-fixed", "TSI_LOOKUP_SALT": "salt-fixed",
			"TSI_BOOTSTRAP_TOKEN": "token-fixed", "KEYSTORE_PASSWORD": "ks-fixed"}},
	}}
	st.Modules["tsi-privacy-vault"] = &secrets.ModuleState{DBPassword: "vdbpass-fixed", Secrets: map[string]string{
		"TSI_PRIVACY_VAULT_MASTER_KEY": "master-fixed", "TSI_PRIVACY_VAULT_INDEX_SECRET": "index-fixed",
		"TSI_PRIVACY_VAULT_JWT_SECRET": "vjwt-fixed", "TSI_PRIVACY_VAULT_BOOTSTRAP_TOKEN": "vtoken-fixed"}}
	for _, k := range []string{"node-a", "node-b"} {
		st.Modules[k] = &secrets.ModuleState{DBPassword: "dbpass-" + k, Secrets: map[string]string{
			"TSI_DX_NODE_JWT_SECRET": "jwt-" + k, "TSI_DX_NODE_BOOTSTRAP_TOKEN": "token-" + k}}
	}
	st.Modules["tsi-compass"] = &secrets.ModuleState{DBPassword: "cdbpass-fixed", Secrets: map[string]string{
		"JWT_SECRET": "cjwt-fixed", "DB_ENCRYPTION_KEY": "cenc-fixed", "TSI_LOOKUP_SALT": "csalt-fixed", "TSI_BOOTSTRAP_TOKEN": "ctoken-fixed"}}
	return st
}

func layout(t *testing.T, mut func(*config.Config)) *Layout {
	t.Helper()
	cat, err := catalog.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default("tsi-dpdp-cms")
	cfg.PortOffset = 0 // goldens show the upstream ports; the default offset has its own test
	if mut != nil {
		mut(cfg)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	l, err := Resolve(cat, cfg, fixedState())
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	p := filepath.Join("testdata", name)
	if *update {
		os.WriteFile(p, got, 0o644)
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("missing golden %s (run with -update): %v", p, err)
	}
	if string(want) != string(got) {
		t.Fatalf("%s differs from golden; run `go test ./render -update` and review the diff\n--- got ---\n%s", name, got)
	}
}

func TestComposeGolden(t *testing.T) {
	for name, mut := range map[string]func(*config.Config){
		"compose_shared.yml":     nil,
		"compose_per_module.yml": func(c *config.Config) { c.Database.Mode = config.PerModule },
		"compose_external.yml": func(c *config.Config) {
			c.Database.Mode = config.External
			c.Database.DSN = "postgres://u:p@db.internal:6000/postgres"
		},
		"compose_offset.yml": func(c *config.Config) { c.PortOffset = 100 },
		"compose_two_modules.yml": func(c *config.Config) {
			c.Modules = []string{"tsi-dpdp-cms", "tsi-privacy-vault"}
		},
		"compose_dx_pair.yml": func(c *config.Config) {
			c.Modules = []string{"tsi-dx-node:node-a", "tsi-dx-node:node-b"}
		},
		"compose_dx_pair_per_module.yml": func(c *config.Config) {
			c.Modules = []string{"tsi-dx-node:node-a", "tsi-dx-node:node-b"}
			c.Database.Mode = config.PerModule
		},
		"compose_all_four.yml": func(c *config.Config) {
			c.Modules = []string{"tsi-dpdp-cms", "tsi-privacy-vault", "tsi-compass", "tsi-dx-node:node-a", "tsi-dx-node:node-b"}
		},
		"compose_two_per_module.yml": func(c *config.Config) {
			c.Modules = []string{"tsi-dpdp-cms", "tsi-privacy-vault"}
			c.Database.Mode = config.PerModule
		},
	} {
		t.Run(name, func(t *testing.T) {
			b, err := layout(t, mut).ComposeYAML()
			if err != nil {
				t.Fatal(err)
			}
			golden(t, name, b)
		})
	}
}

func TestEnvFileGolden(t *testing.T) {
	golden(t, "cms.env", layout(t, nil).Modules[0].EnvFileContent())
}

func TestVaultEnvFileGolden(t *testing.T) {
	l := layout(t, func(c *config.Config) { c.Modules = []string{"tsi-privacy-vault"} })
	golden(t, "vault.env", l.Modules[0].EnvFileContent())
}

func TestTwoModulesGetDistinctPortsAndDatabases(t *testing.T) {
	l := layout(t, func(c *config.Config) { c.Modules = []string{"tsi-dpdp-cms", "tsi-privacy-vault"} })
	a, b := l.Modules[0], l.Modules[1]
	if a.AppPort == b.AppPort || a.DBName == b.DBName || a.DBUser == b.DBUser {
		t.Fatalf("modules collide: %+v %+v", a.AppPort, b.AppPort)
	}
	if a.EnvValue("JWT_SECRET") == "" || b.EnvValue("TSI_PRIVACY_VAULT_JWT_SECRET") == "" {
		t.Fatal("secrets missing")
	}
}

func TestInstancesGetOwnPortsDatabasesAndHostnames(t *testing.T) {
	l := layout(t, func(c *config.Config) {
		c.Modules = []string{"tsi-dx-node:node-a", "tsi-dx-node:node-b"}
	})
	a, b := l.Modules[0], l.Modules[1]
	if a.Key != "node-a" || b.Key != "node-b" {
		t.Fatalf("keys %q %q", a.Key, b.Key)
	}
	if a.AppPort != 8082 || b.AppPort != 8083 || a.DBHostPort != 5434 || b.DBHostPort != 5435 {
		t.Fatalf("ports: %d %d %d %d", a.AppPort, b.AppPort, a.DBHostPort, b.DBHostPort)
	}
	if a.DBName != "tsi_dx_node_node_a" || b.DBName != "tsi_dx_node_node_b" || a.DBUser == b.DBUser {
		t.Fatalf("databases: %s %s", a.DBName, b.DBName)
	}
	if a.Hostname != "node-a-server-1" || b.Hostname != "node-b-server-1" {
		t.Fatalf("hostnames: %s %s", a.Hostname, b.Hostname)
	}
	if a.EnvValue("TSI_DX_NODE_JWT_SECRET") == b.EnvValue("TSI_DX_NODE_JWT_SECRET") {
		t.Fatal("instances must not share a JWT secret")
	}
	y, _ := l.ComposeYAML()
	if !strings.Contains(string(y), "node-a-server-1") || !strings.Contains(string(y), "aliases") {
		t.Fatalf("service aliases missing:\n%s", y)
	}
}

func TestRenderIsDeterministic(t *testing.T) {
	a, _ := layout(t, nil).ComposeYAML()
	b, _ := layout(t, nil).ComposeYAML()
	if string(a) != string(b) {
		t.Fatal("rendering is not deterministic")
	}
}

func TestEnvEscapesDollar(t *testing.T) {
	m := layout(t, func(c *config.Config) {
		c.Overrides = map[string]map[string]string{"tsi-dpdp-cms": {"BRAND_NAME": "A$B"}}
	}).Modules[0]
	if !strings.Contains(string(m.EnvFileContent()), "BRAND_NAME=A$$B") {
		t.Fatalf("$ must be doubled for compose:\n%s", m.EnvFileContent())
	}
}

func TestUnknownOverrideRejected(t *testing.T) {
	cat, _ := catalog.Load()
	cfg := config.Default("tsi-dpdp-cms")
	cfg.Overrides = map[string]map[string]string{"tsi-dpdp-cms": {"NOPE": "1"}}
	if _, err := Resolve(cat, cfg, fixedState()); err == nil {
		t.Fatal("an override of an unknown variable must fail")
	}
}

func TestImmutableOverrideRefusedOnceInitialized(t *testing.T) {
	cat, _ := catalog.Load()
	cfg := config.Default("tsi-dpdp-cms")
	cfg.Overrides = map[string]map[string]string{"tsi-dpdp-cms": {"DB_ENCRYPTION_KEY": "different"}}
	st := fixedState()
	if _, err := Resolve(cat, cfg, st); err != nil {
		t.Fatalf("allowed before data exists: %v", err)
	}
	st = fixedState()
	st.Modules["tsi-dpdp-cms"].DataInitialized = true
	if _, err := Resolve(cat, cfg, st); err == nil {
		t.Fatal("must refuse to change an immutable secret once data exists")
	}
}

func TestSecretsNeverInComposeFile(t *testing.T) {
	b, _ := layout(t, nil).ComposeYAML()
	for _, s := range []string{"jwt-fixed", "enc-fixed", "dbpass-fixed", "adminpass-fixed", "token-fixed"} {
		if strings.Contains(string(b), s) {
			t.Fatalf("secret %q leaked into the compose file", s)
		}
	}
}

func TestDefaultPortOffsetAvoidsUpstreamPorts(t *testing.T) {
	cat, _ := catalog.Load()
	cfg := config.Default("tsi-dpdp-cms", "tsi-privacy-vault", "tsi-compass", "tsi-dx-node:node-a", "tsi-dx-node:node-b")
	if cfg.PortOffset != config.DefaultPortOffset || cfg.PortOffset < 100 {
		t.Fatalf("new projects should shift ports by a few hundred, got %d", cfg.PortOffset)
	}
	l, err := Resolve(cat, cfg, fixedState())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"tsi-dpdp-cms": 8380, "tsi-privacy-vault": 8381, "tsi-compass": 8385, "node-a": 8382, "node-b": 8383}
	for _, m := range l.Modules {
		if m.AppPort != want[m.Key] {
			t.Errorf("%s: port %d, want %d", m.Key, m.AppPort, want[m.Key])
		}
		if m.AppPort >= 8080 && m.AppPort <= 8099 {
			t.Errorf("%s: %d is in the range upstream compose files use", m.Key, m.AppPort)
		}
	}
	if cfg.SharedDBPort() != 5732 {
		t.Fatalf("shared postgres port %d, want 5732", cfg.SharedDBPort())
	}
}
