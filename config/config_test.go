package config

import (
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	c := Default("tsi-dpdp-cms")
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := map[string]func(*Config){
		"no modules":      func(c *Config) { c.Modules = nil },
		"dup module":      func(c *Config) { c.Modules = []string{"a", "a"} },
		"bad mode":        func(c *Config) { c.Database.Mode = "cloud" },
		"external no dsn": func(c *Config) { c.Database.Mode = External },
		"bad project":     func(c *Config) { c.Project = "Bad Name" },
		"bad email":       func(c *Config) { c.Admin.Email = "nope" },
		"negative offset": func(c *Config) { c.PortOffset = -1 },
	}
	for name, mut := range bad {
		c := Default("tsi-dpdp-cms")
		mut(c)
		if err := c.Validate(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestPortsAndRoundTrip(t *testing.T) {
	c := Default("tsi-dpdp-cms")
	c.PortOffset = 100
	if c.AppPort(8080) != 8180 || c.SharedDBPort() != 5532 || c.ModuleDBPort(5432) != 5532 {
		t.Fatal("port offset not applied")
	}
	p := t.TempDir() + "/tsi.yaml"
	if err := c.Save(p); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil || got.PortOffset != 100 || got.Modules[0] != "tsi-dpdp-cms" {
		t.Fatalf("round trip failed: %v %+v", err, got)
	}
}

func TestUnknownKeyRejected(t *testing.T) {
	p := t.TempDir() + "/tsi.yaml"
	writeFile(t, p, "version: 1\nmodules: [a]\ntypo_key: 1\n")
	if _, err := Load(p); err == nil {
		t.Fatal("an unknown key must be rejected, not ignored")
	}
}

func TestInstanceRefs(t *testing.T) {
	if m, i := SplitRef("tsi-dx-node:node-a"); m != "tsi-dx-node" || i != "node-a" {
		t.Fatal(m, i)
	}
	if m, i := SplitRef("tsi-compass"); m != "tsi-compass" || i != "" {
		t.Fatal(m, i)
	}
	if Key("tsi-dx-node:node-a") != "node-a" || Key("tsi-compass") != "tsi-compass" {
		t.Fatal("key")
	}
	ok := Default("tsi-dx-node:node-a", "tsi-dx-node:node-b", "tsi-compass")
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mods := range map[string][]string{
		"same instance twice":          {"tsi-dx-node:a", "tsi-dx-node:a"},
		"module twice":                 {"tsi-compass", "tsi-compass"},
		"bad instance name":            {"tsi-dx-node:Node_A"},
		"empty instance":               {"tsi-dx-node:"},
		"instance clashes with module": {"tsi-compass", "tsi-dx-node:tsi-compass"},
	} {
		c := Default(mods...)
		if err := c.Validate(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestMissingPortOffsetMeansDefaultAndExplicitZeroIsKept(t *testing.T) {
	dir := t.TempDir()
	write := func(body string) *Config {
		p := dir + "/tsi.yaml"
		writeFile(t, p, body)
		c, err := Load(p)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	base := "version: 1\nmodules: [tsi-dpdp-cms]\nadmin: {name: A, email: a@example.com}\n"
	if got := write(base).PortOffset; got != DefaultPortOffset {
		t.Fatalf("a tsi.yaml without port_offset must get the default %d, got %d", DefaultPortOffset, got)
	}
	if got := write(base + "port_offset: 0\n").PortOffset; got != 0 {
		t.Fatalf("an explicit port_offset: 0 must be honoured, got %d", got)
	}
	if got := write(base + "port_offset: 500\n").PortOffset; got != 500 {
		t.Fatalf("got %d", got)
	}
	// Saving a config whose offset is 0 must write it, or the next load would turn it into 300.
	c := Default("tsi-dpdp-cms")
	c.PortOffset = 0
	p := dir + "/saved.yaml"
	if err := c.Save(p); err != nil {
		t.Fatal(err)
	}
	back, err := Load(p)
	if err != nil || back.PortOffset != 0 {
		t.Fatalf("round trip lost an explicit 0: %v %+v", err, back)
	}
}

func TestProjectFromDir(t *testing.T) {
	for dir, want := range map[string]string{
		"/home/me/work/my-tsi-stack":      "my-tsi-stack",
		"/home/me/work/My Stack (v2)":     "my-stack-v2",
		"/home/me/work/UPPER_case":        "upper_case",
		"/home/me/work/-.-":               "tsi",
		"/":                               "tsi",
		"/tmp/" + strings.Repeat("a", 80): strings.Repeat("a", 40),
	} {
		got := ProjectFromDir(dir)
		if got != want {
			t.Errorf("ProjectFromDir(%q) = %q, want %q", dir, got, want)
		}
		if !projectRe.MatchString(got) {
			t.Errorf("%q is not a valid project name", got)
		}
	}
	if ProjectFromDir("/a/stack-one") == ProjectFromDir("/a/stack-two") {
		t.Fatal("different directories must get different projects")
	}
}
