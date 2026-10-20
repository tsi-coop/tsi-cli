package compose

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tsicoop/tsi-cli/engine"
	"github.com/tsicoop/tsi-cli/tsierr"
)

func newStack(t *testing.T, eng engine.Engine, opts ...Option) *Stack {
	t.Helper()
	base := []Option{WithDir(t.TempDir()), WithModules("tsi-dpdp-cms"), WithEngine(eng), WithProject("unit")}
	s, err := New(append(base, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestWriteCreatesPrivateFilesAndGitignore(t *testing.T) {
	s := newStack(t, &engine.Fake{})
	if err := s.Write(context.Background()); err != nil {
		t.Fatal(err)
	}
	for path, mode := range map[string]os.FileMode{
		".tsi/state.yaml": 0o600, ".tsi/env/postgres.env": 0o600, ".tsi/env/tsi-dpdp-cms.env": 0o600, ".tsi": 0o700,
	} {
		fi, err := os.Stat(filepath.Join(s.Dir, path))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != mode {
			t.Errorf("%s: mode %v, want %v", path, fi.Mode().Perm(), mode)
		}
	}
	gi, _ := os.ReadFile(filepath.Join(s.Dir, ".gitignore"))
	if !strings.Contains(string(gi), ".tsi/") {
		t.Fatal(".tsi/ must be git-ignored")
	}
	if _, err := os.Stat(filepath.Join(s.Dir, ".tsi/certs/tsi-dpdp-cms.p12")); err != nil {
		t.Fatal("keystore not generated")
	}
}

func TestWriteTwiceKeepsSecretsAndKeystore(t *testing.T) {
	s := newStack(t, &engine.Fake{})
	ctx := context.Background()
	s.Write(ctx)
	env1, _ := os.ReadFile(filepath.Join(s.Dir, ".tsi/env/tsi-dpdp-cms.env"))
	ks1, _ := os.ReadFile(filepath.Join(s.Dir, ".tsi/certs/tsi-dpdp-cms.p12"))
	if err := s.Write(ctx); err != nil {
		t.Fatal(err)
	}
	env2, _ := os.ReadFile(filepath.Join(s.Dir, ".tsi/env/tsi-dpdp-cms.env"))
	ks2, _ := os.ReadFile(filepath.Join(s.Dir, ".tsi/certs/tsi-dpdp-cms.p12"))
	if string(env1) != string(env2) {
		t.Fatal("secrets were regenerated on the second write")
	}
	if string(ks1) != string(ks2) {
		t.Fatal("the keystore was replaced; it must never be overwritten")
	}
}

func TestGitignoreNotDuplicated(t *testing.T) {
	s := newStack(t, &engine.Fake{})
	os.WriteFile(filepath.Join(s.Dir, ".gitignore"), []byte("bin/"), 0o644)
	s.Write(context.Background())
	s.Write(context.Background())
	gi, _ := os.ReadFile(filepath.Join(s.Dir, ".gitignore"))
	if strings.Count(string(gi), ".tsi/") != 1 || !strings.HasPrefix(string(gi), "bin/\n") {
		t.Fatalf("gitignore: %q", gi)
	}
}

func TestPlanHasNoSideEffects(t *testing.T) {
	s := newStack(t, &engine.Fake{})
	p, err := s.Plan()
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Migrations["tsi-dpdp-cms"]) != 14 || len(p.Steps["tsi-dpdp-cms"]) == 0 {
		t.Fatalf("plan: %+v", p)
	}
	entries, _ := os.ReadDir(s.Dir)
	if len(entries) != 0 {
		t.Fatalf("Plan wrote files: %v", entries)
	}
}

func TestUpRefusesWhenStateLost(t *testing.T) {
	fake := &engine.Fake{Containers: []engine.Container{{Service: "tsi-dpdp-cms", State: "running"}}}
	s := newStack(t, fake)
	err := s.Up(context.Background())
	var te *tsierr.Error
	if !errors.As(err, &te) || te.Code != tsierr.CodeStateLost {
		t.Fatalf("want state-lost error, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(s.Dir, ".tsi/env")); statErr == nil {
		t.Fatal("nothing may be written (and no secret regenerated) when state is lost")
	}
}

func TestUpStopsOnPreflightFailure(t *testing.T) {
	fake := &engine.Fake{Err: map[string]error{"Preflight": tsierr.Newf(tsierr.CodePreflight, "", "no docker")}}
	s := newStack(t, fake)
	if err := s.Up(context.Background()); err == nil {
		t.Fatal("expected the preflight error")
	}
	if len(fake.Calls) != 1 {
		t.Fatalf("nothing else may run after a failed preflight: %v", fake.Calls)
	}
}

func TestUnknownModuleAndMissingConfig(t *testing.T) {
	if _, err := New(WithDir(t.TempDir()), WithModules("nope")); err == nil {
		t.Fatal("unknown module must fail")
	}
	if _, err := New(WithDir(t.TempDir())); err == nil {
		t.Fatal("no modules and no tsi.yaml must fail")
	}
}

func TestTestRequiresCredentials(t *testing.T) {
	s := newStack(t, &engine.Fake{})
	s.Write(context.Background())
	_, err := s.Test(context.Background())
	var te *tsierr.Error
	if !errors.As(err, &te) || te.Code != tsierr.CodeNotFound {
		t.Fatalf("got %v", err)
	}
}

func TestDownWithVolumesRemovesState(t *testing.T) {
	fake := &engine.Fake{}
	s := newStack(t, fake)
	s.Write(context.Background())
	if err := s.Down(context.Background(), WithVolumes()); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{".tsi", "docker-compose.tsi.yml"} {
		if _, err := os.Stat(filepath.Join(s.Dir, p)); err == nil {
			t.Errorf("%s should be removed by down --volumes", p)
		}
	}
}

func TestMergeSpecDeep(t *testing.T) {
	got := mergeSpec(
		map[string]any{"app": map[string]any{"name": "a", "phone": "1"}, "x": 1},
		map[string]any{"app": map[string]any{"name": "b"}})
	app := got["app"].(map[string]any)
	if app["name"] != "b" || app["phone"] != "1" || got["x"] != 1 {
		t.Fatalf("merge: %v", got)
	}
}

func TestMoreThanTwoDxNodesRefused(t *testing.T) {
	_, err := New(WithDir(t.TempDir()), WithEngine(&engine.Fake{}),
		WithModules("tsi-dx-node:a", "tsi-dx-node:b", "tsi-dx-node:c"))
	if err == nil || !strings.Contains(err.Error(), "at most 2") {
		t.Fatalf("got %v", err)
	}
	if _, err := New(WithDir(t.TempDir()), WithEngine(&engine.Fake{}), WithModules("tsi-dx-node:a", "tsi-dx-node:b")); err != nil {
		t.Fatalf("two nodes are fine: %v", err)
	}
	if _, err := New(WithDir(t.TempDir()), WithEngine(&engine.Fake{}), WithModules("tsi-dpdp-cms", "tsi-dpdp-cms")); err == nil {
		t.Fatal("the same single-instance module twice must be refused")
	}
}

func TestPlanWithInstancesNamesEachOne(t *testing.T) {
	s := newStack(t, &engine.Fake{}, WithModules("tsi-dx-node:node-a", "tsi-dx-node:node-b"))
	p, err := s.Plan()
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Migrations["node-a"]) != 5 || len(p.Migrations["node-b"]) != 5 || len(p.Steps["node-a"]) == 0 {
		t.Fatalf("%+v", p)
	}
	joined := strings.Join(p.Ports, " ")
	if !strings.Contains(joined, "node-a 127.0.0.1:8082") || !strings.Contains(joined, "node-b 127.0.0.1:8083") {
		t.Fatalf("ports: %v", p.Ports)
	}
}
