// Package tsitest holds the smoke suites that prove a provisioned module works, using the
// tenant credentials tsi created. It is importable from an application's own go test runs.
package tsitest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/tsi-coop/tsi-cli/bootstrap"
	"github.com/tsi-coop/tsi-cli/internal/redact"
	"github.com/tsi-coop/tsi-cli/secrets"
)

// Env is what a check can use.
type Env struct {
	// Module names the instance under test (its key); Suite is the catalog module whose
	// checks apply (the same for a single instance).
	Module string
	Suite  string
	// Index is the position among instances of the same module (0 is the first).
	Index int
	// Peers are the other instances of the same module, for checks that cross nodes.
	Peers   []*Env
	BaseURL string
	Creds   secrets.Credentials
	// Spec is the module's rendered provisioning spec (entity codes, names...).
	Spec map[string]any
	// Admin logs in as the administrator and posts to an admin path, for checks that need
	// to create extra fixtures (for example a read-only key).
	Admin func(ctx context.Context, path string, body map[string]any) (int, any, error)
	// Ready runs the module's readiness probe once.
	Ready func(ctx context.Context) error

	hc    *http.Client
	state map[string]any
}

// ErrSkip, returned (wrapped) by a check, marks it as not applicable to this instance, for
// example a sender-side check on the receiving node. A skipped check counts as passed.
var ErrSkip = errors.New("skipped")

// Skipf returns an error that marks a check as skipped, with a reason.
func Skipf(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrSkip, fmt.Sprintf(format, a...))
}

// Check is one smoke test.
type Check struct {
	ID          string
	Description string
	Run         func(ctx context.Context, e *Env) error
}

// CheckResult is the outcome of a check.
type CheckResult struct {
	ID          string        `json:"id"`
	Description string        `json:"description"`
	Passed      bool          `json:"passed"`
	Skipped     bool          `json:"skipped,omitempty"`
	Error       string        `json:"error,omitempty"`
	Duration    time.Duration `json:"duration_ns"`
}

// Result is the outcome of a module's suite.
type Result struct {
	Module   string        `json:"module"`
	Checks   []CheckResult `json:"checks"`
	Duration time.Duration `json:"duration_ns"`
}

// Passed counts checks that ran and passed.
func (r Result) Passed() int {
	n := 0
	for _, c := range r.Checks {
		if c.Passed && !c.Skipped {
			n++
		}
	}
	return n
}

// Skipped counts checks that did not apply to this instance.
func (r Result) Skipped() int {
	n := 0
	for _, c := range r.Checks {
		if c.Skipped {
			n++
		}
	}
	return n
}

// Failed counts failing checks.
func (r Result) Failed() int { return len(r.Checks) - r.Passed() - r.Skipped() }

// Suite returns the checks for a module.
func Suite(module string) ([]Check, bool) {
	s, ok := suites[module]
	return s, ok
}

var suites = map[string][]Check{}

func register(module string, checks []Check) { suites[module] = checks }

// Run executes a module's suite. Checks run in order and share state, so a failed early
// check makes later dependent checks fail with a clear message instead of being skipped.
func Run(ctx context.Context, e *Env, only []string) (Result, error) {
	suite := e.Suite
	if suite == "" {
		suite = e.Module
	}
	checks, ok := Suite(suite)
	if !ok {
		return Result{}, fmt.Errorf("no smoke tests for module %s", suite)
	}
	e.hc = &http.Client{Timeout: 30 * time.Second}
	e.state = map[string]any{}
	start := time.Now()
	res := Result{Module: e.Module}
	for _, c := range checks {
		if len(only) > 0 && !contains(only, c.ID) {
			continue
		}
		t0 := time.Now()
		err := c.Run(ctx, e)
		cr := CheckResult{ID: c.ID, Description: c.Description, Passed: err == nil, Duration: time.Since(t0)}
		if errors.Is(err, ErrSkip) {
			cr.Passed, cr.Skipped, cr.Error = true, true, strings.TrimPrefix(err.Error(), ErrSkip.Error()+": ")
		} else if err != nil {
			cr.Error = redact.String(err.Error())
		}
		res.Checks = append(res.Checks, cr)
	}
	res.Duration = time.Since(start)
	return res, nil
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// post sends a JSON POST with the tenant credentials (or custom headers) and decodes the reply.
func (e *Env) post(ctx context.Context, path string, headers map[string]string, body map[string]any) (int, any, error) {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.BaseURL+path, bytes.NewReader(b))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := e.hc.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	var doc any
	if json.Unmarshal(raw, &doc) != nil {
		doc = string(raw)
	}
	return bootstrap.EffectiveStatus(resp.StatusCode, raw), doc, nil
}

func (e *Env) tenant(ctx context.Context, path string, body map[string]any) (int, any, error) {
	return e.post(ctx, path, map[string]string{"X-API-Key": e.Creds.APIKey, "X-API-Secret": e.Creds.APISecret}, body)
}

func expect(status int, doc any, want ...int) error {
	for _, w := range want {
		if status == w {
			return nil
		}
	}
	s, _ := json.Marshal(doc)
	if len(s) > 300 {
		s = append(s[:300], "…"...)
	}
	return fmt.Errorf("HTTP %d (want %v): %s", status, want, s)
}

func field(doc any, key string) string {
	if m, ok := doc.(map[string]any); ok {
		if v, ok := m[key]; ok {
			return fmt.Sprint(v)
		}
	}
	return ""
}

// asMap returns doc as an object, or an empty map.
func asMap(doc any) map[string]any {
	m, _ := doc.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}
