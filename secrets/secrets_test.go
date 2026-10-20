package secrets

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/tsicoop/tsi-cli/tsierr"
)

func TestGenerate(t *testing.T) {
	for gen, n := range map[string]int{"random32": 64, "hex16": 32, "token": 48, "password": 28} {
		a, err := Generate(gen)
		if err != nil || len(a) != n {
			t.Fatalf("%s: len %d err %v", gen, len(a), err)
		}
		b, _ := Generate(gen)
		if a == b {
			t.Fatalf("%s: not random", gen)
		}
	}
	if _, err := Generate("nope"); err == nil {
		t.Fatal("expected error for unknown generator")
	}
}

func TestStoreRoundTripAndPermissions(t *testing.T) {
	s := &Store{Dir: t.TempDir() + "/.tsi"}
	st, _ := s.Load()
	v, _ := st.EnsureSecret("m", "JWT", "random32")
	st.Module("m").Credentials = &Credentials{APIKey: "k", APISecret: "s3cret-value"}
	if err := s.Save(st); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(s.StatePath())
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("state mode %v, want 0600", fi.Mode().Perm())
	}
	di, _ := os.Stat(s.Dir)
	if di.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %v, want 0700", di.Mode().Perm())
	}
	st2, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := st2.EnsureSecret("m", "JWT", "random32"); got != v {
		t.Fatal("an existing secret was regenerated")
	}
	if st2.Modules["m"].Credentials.APISecret != "s3cret-value" {
		t.Fatal("credentials lost")
	}
}

func TestImmutableSecret(t *testing.T) {
	st := &State{Modules: map[string]*ModuleState{}}
	st.EnsureSecret("m", "KEY", "random32")
	if err := st.CheckImmutable("m", "KEY", "other"); err != nil {
		t.Fatalf("before data exists a change is allowed: %v", err)
	}
	st.Module("m").DataInitialized = true
	err := st.CheckImmutable("m", "KEY", "other")
	var te *tsierr.Error
	if err == nil || !asErr(err, &te) || te.Code != tsierr.CodeImmutable {
		t.Fatalf("want immutable error, got %v", err)
	}
	if err := st.CheckImmutable("m", "KEY", st.Modules["m"].Secrets["KEY"]); err != nil {
		t.Fatalf("same value must be accepted: %v", err)
	}
}

func asErr(err error, target **tsierr.Error) bool {
	e, ok := err.(*tsierr.Error)
	if ok {
		*target = e
	}
	return ok
}

func TestLockIsExclusive(t *testing.T) {
	dir := t.TempDir()
	a, b := &Store{Dir: dir}, &Store{Dir: dir}
	if err := a.Lock(); err != nil {
		t.Fatal(err)
	}
	if err := b.Lock(); err == nil || !strings.Contains(err.Error(), "another tsi process") {
		t.Fatalf("second lock should fail, got %v", err)
	}
	a.Unlock()
	if err := b.Lock(); err != nil {
		t.Fatalf("lock after release: %v", err)
	}
	b.Unlock()
}

func TestCorruptStateIsReported(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	os.WriteFile(s.StatePath(), []byte("modules: [unclosed"), 0o600)
	if _, err := s.Load(); err == nil {
		t.Fatal("expected an error for a corrupt state file")
	}
}

func TestStrongPasswordMeetsComplexityRules(t *testing.T) {
	// The rule dx-node enforces: upper, lower, digit and one of !@#$%^&*()_+, and nothing else.
	// (Go's regexp has no lookahead, so each requirement is its own pattern.)
	allowed := regexp.MustCompile(`^[A-Za-z0-9!@#$%^&*()_+]{8,}$`)
	needs := []*regexp.Regexp{regexp.MustCompile(`[a-z]`), regexp.MustCompile(`[A-Z]`),
		regexp.MustCompile(`[0-9]`), regexp.MustCompile(`[!@#$%^&*()_+]`)}
	for i := 0; i < 200; i++ {
		pw, err := Generate("strong")
		if err != nil || len(pw) != 28 || !allowed.MatchString(pw) {
			t.Fatalf("%q: %v", pw, err)
		}
		for _, n := range needs {
			if !n.MatchString(pw) {
				t.Fatalf("%q lacks a character class %v", pw, n)
			}
		}
	}
}
