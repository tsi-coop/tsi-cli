package tsitest

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/tsi-coop/tsi-cli/catalog"
)

// The smoke list in the catalog is documentation; the code is the truth. They must agree.
func TestCatalogSmokeMatchesSuites(t *testing.T) {
	cat, _ := catalog.Load()
	for _, n := range cat.Names() {
		m, _ := cat.Get(n)
		suite, ok := Suite(n)
		if !ok {
			t.Errorf("%s: no suite registered", n)
			continue
		}
		if len(suite) != len(m.Smoke) {
			t.Errorf("%s: catalog lists %d smoke tests, suite has %d", n, len(m.Smoke), len(suite))
			continue
		}
		for i, c := range suite {
			if m.Smoke[i].ID != c.ID {
				t.Errorf("%s: smoke[%d] is %q in catalog, %q in code", n, i, m.Smoke[i].ID, c.ID)
			}
		}
	}
}

func TestReports(t *testing.T) {
	res := []Result{{Module: "m", Checks: []CheckResult{
		{ID: "a", Description: "ok one", Passed: true},
		{ID: "b", Description: "bad one", Error: "boom\nline2"},
	}}}
	var h bytes.Buffer
	p, f := WriteHuman(&h, res)
	if p != 1 || f != 1 || !strings.Contains(h.String(), "✘") || !strings.Contains(h.String(), "    line2") {
		t.Fatalf("human: %d/%d\n%s", p, f, h.String())
	}
	var x bytes.Buffer
	if err := WriteJUnit(&x, res); err != nil || !strings.Contains(x.String(), `failures="1"`) || !strings.Contains(x.String(), "<failure") {
		t.Fatalf("junit: %v\n%s", err, x.String())
	}
	var j bytes.Buffer
	if err := WriteJSON(&j, res); err != nil || !strings.Contains(j.String(), `"passed": true`) {
		t.Fatalf("json: %v\n%s", err, j.String())
	}
}

func TestRunReportsFailuresWithoutAborting(t *testing.T) {
	register("fake-mod", []Check{
		{"first", "fails", func(ctx context.Context, e *Env) error { return context.DeadlineExceeded }},
		{"second", "still runs", func(ctx context.Context, e *Env) error { return nil }},
	})
	defer delete(suites, "fake-mod")
	res, err := Run(context.Background(), &Env{Module: "fake-mod"}, nil)
	if err != nil || res.Passed() != 1 || res.Failed() != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	only, _ := Run(context.Background(), &Env{Module: "fake-mod"}, []string{"second"})
	if len(only.Checks) != 1 {
		t.Fatal("only filter")
	}
}

func TestSkippedChecksAreCountedSeparately(t *testing.T) {
	register("skip-mod", []Check{
		{"a", "passes", func(ctx context.Context, e *Env) error { return nil }},
		{"b", "not applicable", func(ctx context.Context, e *Env) error { return Skipf("only the sender") }},
		{"c", "fails", func(ctx context.Context, e *Env) error { return context.Canceled }},
	})
	defer delete(suites, "skip-mod")
	res, _ := Run(context.Background(), &Env{Module: "skip-mod"}, nil)
	if res.Passed() != 1 || res.Skipped() != 1 || res.Failed() != 1 {
		t.Fatalf("passed=%d skipped=%d failed=%d", res.Passed(), res.Skipped(), res.Failed())
	}
	if res.Checks[1].Error != "only the sender" || !res.Checks[1].Skipped {
		t.Fatalf("%+v", res.Checks[1])
	}
	var out bytes.Buffer
	p, f := WriteHuman(&out, []Result{res})
	if p != 1 || f != 1 || !strings.Contains(out.String(), "1 skipped") || !strings.Contains(out.String(), "–") {
		t.Fatalf("%d/%d\n%s", p, f, out.String())
	}
}
