package catalog

import (
	"strings"
	"testing"
)

func TestLoadAllModules(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range c.Names() {
		m, _ := c.Get(n)
		if len(m.Bootstrap) == 0 {
			t.Errorf("%s: no bootstrap steps", n)
		}
	}
}

func TestUnknownModule(t *testing.T) {
	c, _ := Load()
	if _, err := c.Get("nope"); err == nil || !strings.Contains(err.Error(), "unknown module") {
		t.Fatalf("want unknown module error, got %v", err)
	}
}

func TestValidateRejects(t *testing.T) {
	good := func() Module {
		return Module{Name: "x", Image: Image{"r/x", "v1"}, ContainerPort: 8080, Ports: Ports{1, 2},
			DB: DBSpec{Name: "d", User: "u"}, Ready: Probe{Path: "/"}, Session: "bearer"}
	}
	cases := map[string]func(*Module){
		"latest tag":        func(m *Module) { m.Image.Tag = "latest" },
		"no ready":          func(m *Module) { m.Ready.Path = "" },
		"bad session":       func(m *Module) { m.Session = "basic" },
		"duplicate step":    func(m *Module) { m.Bootstrap = []Step{{ID: "a", Path: "/"}, {ID: "a", Path: "/"}} },
		"migration neither": func(m *Module) { m.Migrations.Steps = []MigrationStep{{}} },
		"no checksum": func(m *Module) {
			m.Migrations = Migrations{Source: Source{"o/r", "v1", "db"}, Steps: []MigrationStep{{File: "a.sql"}}}
		},
		"branch ref": func(m *Module) {
			m.Migrations = Migrations{Source: Source{"o/r", "main", "db"}, Steps: []MigrationStep{{File: "a.sql", SHA256: strings.Repeat("a", 64)}}}
		},
		"path in file name": func(m *Module) {
			m.Migrations = Migrations{Source: Source{"o/r", "v1", "db"}, Steps: []MigrationStep{{File: "../a.sql", SHA256: strings.Repeat("a", 64)}}}
		},
		"bad repo": func(m *Module) {
			m.Migrations = Migrations{Source: Source{"nonsense", "v1", "db"}, Steps: []MigrationStep{{File: "a.sql", SHA256: strings.Repeat("a", 64)}}}
		},
	}
	for name, mut := range cases {
		m := good()
		mut(&m)
		if err := m.validate(); err == nil {
			t.Errorf("%s: expected a validation error", name)
		}
	}
	m := good()
	if err := m.validate(); err != nil {
		t.Fatalf("good module rejected: %v", err)
	}
}
