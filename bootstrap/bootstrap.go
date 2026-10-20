// Package bootstrap provisions a running module through its own API: first administrator,
// login, tenant app, API key. Steps come from the catalog, are idempotent, and the one-time
// secrets a module returns are saved before the next step runs.
package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/tsicoop/tsi-cli/catalog"
	"github.com/tsicoop/tsi-cli/internal/redact"
	"github.com/tsicoop/tsi-cli/secrets"
	"github.com/tsicoop/tsi-cli/tsierr"
)

// Admin is the administrator identity to create.
type Admin struct {
	Name, Email, Password string
}

// Runner executes a module's bootstrap steps.
type Runner struct {
	Module *catalog.Module
	// Key names this instance in messages (the module name when there is only one).
	Key string
	// Extra supplies additional template context, evaluated for each step: for modules with
	// an identity it holds Identity, Node and Peers.
	Extra   func() map[string]any
	BaseURL string
	Env     map[string]string
	Admin   Admin
	// Spec is the module's provisioning spec (catalog defaults merged with tsi.yaml).
	Spec  map[string]any
	State *secrets.ModuleState
	// Save persists state; it is called after every step that changes it.
	Save func() error
	Log  func(format string, a ...any)

	hc   *http.Client
	vars map[string]string
	item any // current .Item of a foreach step
}

// Phases returns the highest phase number used by the module's steps.
func (r *Runner) Phases() int {
	max := 1
	for _, st := range r.Module.Bootstrap {
		if st.Phase > max {
			max = st.Phase
		}
	}
	return max
}

// Run executes every phase in order and returns the credentials. Use RunPhase to
// interleave phases across several instances.
func (r *Runner) Run(ctx context.Context) (*secrets.Credentials, error) {
	for p := 1; p <= r.Phases(); p++ {
		if err := r.RunPhase(ctx, p); err != nil {
			return nil, err
		}
	}
	return r.Credentials(), nil
}

// RunPhase executes the steps of one phase.
func (r *Runner) RunPhase(ctx context.Context, phase int) error {
	r.init()
	m := r.Module
	if len(r.Admin.Password) < m.MinPasswordLen {
		return tsierr.Newf(tsierr.CodeUsage, r.label(), "admin password shorter than the module minimum of %d", m.MinPasswordLen)
	}
	spec, err := r.renderSpec()
	if err != nil {
		return err
	}
	r.Spec = spec

	for _, st := range m.Bootstrap {
		stPhase := st.Phase
		if stPhase == 0 {
			stPhase = 1
		}
		if stPhase != phase {
			continue
		}
		if ok, err := r.shouldRun(st); err != nil {
			return err
		} else if !ok {
			continue
		}
		for _, inst := range r.expand(st) {
			r.item = inst.item
			if prev, done := r.stepState(inst.step.ID); done && prev.Done && !inst.step.Volatile {
				for k, v := range prev.Vars {
					r.vars[k] = v
				}
				r.Log("%s: %s (already done)", r.label(), inst.step.Description)
				continue
			}
			if err := r.runStep(ctx, inst.step); err != nil {
				return err
			}
		}
	}
	r.item = nil
	return nil
}

func (r *Runner) label() string {
	if r.Key != "" {
		return r.Key
	}
	return r.Module.Name
}

func (r *Runner) init() {
	if r.hc == nil {
		jar, _ := cookiejar.New(nil)
		r.hc = &http.Client{Timeout: 60 * time.Second, Jar: jar}
	}
	if r.vars == nil {
		r.vars = map[string]string{}
	}
	if r.Log == nil {
		r.Log = func(string, ...any) {}
	}
}

// AdminCall logs in as the administrator (running the catalog's login step) and posts
// body to path with the admin session. Used by smoke tests that need extra fixtures.
func (r *Runner) AdminCall(ctx context.Context, path string, body map[string]any) (int, any, error) {
	r.init()
	if r.Spec == nil || r.vars["token"] == "" {
		spec, err := r.renderSpec()
		if err != nil {
			return 0, nil, err
		}
		r.Spec = spec
	}
	r.Log = func(string, ...any) {}
	for attempt := 0; attempt < 2; attempt++ {
		if r.vars["token"] == "" {
			if err := r.adminLogin(ctx); err != nil {
				return 0, nil, err
			}
		}
		status, resp, err := r.call(ctx, catalog.Step{Path: path, Auth: "bearer"}, body)
		if err != nil {
			return 0, nil, err
		}
		if status == http.StatusUnauthorized && attempt == 0 {
			delete(r.vars, "token") // expired: log in once more
			continue
		}
		var doc any
		if json.Unmarshal(resp, &doc) != nil {
			doc = string(resp)
		}
		return status, doc, nil
	}
	return 0, nil, fmt.Errorf("administrator session was refused")
}

// adminLogin runs the catalog's login step. The session is kept for later AdminCalls, so
// a suite logs in once (some modules rate-limit sign-ins).
func (r *Runner) adminLogin(ctx context.Context) error {
	for _, st := range r.Module.Bootstrap {
		if st.ID == "login" {
			return r.runStep(ctx, st)
		}
	}
	return tsierr.Newf(tsierr.CodeBootstrap, r.Module.Name, "the module has no login step")
}

type instance struct {
	step catalog.Step
	item any
}

// expand turns a foreach step into one instance per list item, each with its own state id.
func (r *Runner) expand(st catalog.Step) []instance {
	if st.Foreach == "" {
		return []instance{{step: st}}
	}
	v, _ := lookup(r.context(), st.Foreach)
	list, _ := v.([]any)
	var out []instance
	for i, it := range list {
		s := st
		s.Foreach = ""
		s.ID = fmt.Sprintf("%s#%d", st.ID, i)
		out = append(out, instance{step: s, item: it})
	}
	return out
}

// Credentials returns what the user is given once bootstrap is complete.
func (r *Runner) Credentials() *secrets.Credentials {
	c := &secrets.Credentials{BaseURL: r.BaseURL, APIKey: r.vars["api_key"], APISecret: r.vars["api_secret"], Extra: map[string]string{}}
	for k, v := range r.vars {
		if k != "api_key" && k != "api_secret" && k != "token" {
			c.Extra[k] = v
		}
	}
	if r.Module.Credentials == "admin" {
		c.AdminEmail, c.AdminPassword = r.Admin.Email, r.Admin.Password
		return c
	}
	if c.APIKey == "" {
		return nil
	}
	return c
}

func (r *Runner) context() map[string]any {
	vars := map[string]any{}
	for k, v := range r.vars {
		vars[k] = v
	}
	env := map[string]any{}
	for k, v := range r.Env {
		env[k] = v
	}
	ctx := map[string]any{
		"Admin": map[string]any{"Name": r.Admin.Name, "Email": r.Admin.Email, "Password": r.Admin.Password},
		"Env":   env,
		"Spec":  r.Spec,
		"Vars":  vars,
		"Item":  r.item,
	}
	if r.Extra != nil {
		for k, v := range r.Extra() {
			ctx[k] = v
		}
	}
	return ctx
}

// Vars returns a copy of the variables captured so far (for peers to read).
func (r *Runner) Vars() map[string]string {
	out := map[string]string{}
	for k, v := range r.vars {
		out[k] = v
	}
	return out
}

// RenderedSpec returns the module spec with its templates expanded.
func (r *Runner) RenderedSpec() (map[string]any, error) { return r.renderSpec() }

func (r *Runner) renderSpec() (map[string]any, error) {
	ctx := r.context()
	ctx["Spec"] = map[string]any{}
	out, err := render(r.Spec, ctx)
	if err != nil {
		return nil, tsierr.New(tsierr.CodeUsage, r.Module.Name, "spec", "invalid bootstrap spec", "", err)
	}
	return out.(map[string]any), nil
}

func (r *Runner) shouldRun(st catalog.Step) (bool, error) {
	if st.When != "" && !r.truthy(st.When) {
		return false, nil
	}
	if st.Unless != "" && r.truthy(st.Unless) {
		return false, nil
	}
	return true, nil
}

// truthy reports whether a dotted path in the template context is set to something
// non-empty.
func (r *Runner) truthy(path string) bool {
	v, ok := lookup(r.context(), path)
	if !ok {
		return false
	}
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x != ""
	case nil:
		return false
	case []any:
		return len(x) > 0
	}
	return true
}

func (r *Runner) runStep(ctx context.Context, st catalog.Step) error {
	tctx := r.context()
	body, err := render(st.Body, tctx)
	if err != nil {
		return tsierr.New(tsierr.CodeBootstrap, r.label(), st.ID, "cannot build the request", "", err)
	}
	var deadline time.Time
	if st.Poll > 0 {
		deadline = time.Now().Add(time.Duration(st.Poll) * time.Second)
	}
	for {
		status, resp, err := r.call(ctx, st, body)
		if err != nil {
			return tsierr.New(tsierr.CodeBootstrap, r.label(), st.ID, "request failed", "is the module running? try `tsi compose status`", err)
		}
		if status == 401 && st.Auth == "bearer" && st.ID != "login" {
			return tsierr.New(tsierr.CodeBootstrap, r.label(), st.ID, "the administrator session was rejected", "re-run `tsi compose bootstrap`", nil)
		}
		alreadyByBody := st.AlreadyDoneBody != "" && status >= 400 && strings.Contains(string(resp), st.AlreadyDoneBody)
		switch {
		case contains(st.AlreadyDone, status) || alreadyByBody:
			r.Log("%s: %s (already done, HTTP %d)", r.label(), st.Description, status)
			var recovered map[string]string
			if st.Recover != nil {
				if recovered, err = r.recover(ctx, st); err != nil {
					return err
				}
				for k, v := range recovered {
					r.vars[k] = v
				}
			}
			r.markDone(st, recovered)
			return r.save()
		case !contains(expected(st), status):
			hint := ""
			if st.ID == "login" {
				hint = "if this stack was set up before, the saved administrator password may be missing from .tsi/state.yaml"
			}
			return tsierr.New(tsierr.CodeBootstrap, r.label(), st.ID,
				fmt.Sprintf("unexpected HTTP %d", status), hint, fmt.Errorf("%s", redact.String(truncate(string(resp), 300))))
		}
		captured := map[string]string{}
		if st.Select != nil {
			found, err := r.selectItem(st, resp, captured)
			if err != nil {
				return err
			}
			if !found {
				if st.Select.Optional {
					r.markDone(st, captured)
					return r.save()
				}
				if st.Poll > 0 && time.Now().Before(deadline) {
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-time.After(2 * time.Second):
					}
					continue
				}
				return tsierr.New(tsierr.CodeBootstrap, r.label(), st.ID, "no matching item in the response",
					"the module's behaviour may have changed, or it did not settle in time; report this", nil)
			}
		}
		if len(st.Capture) > 0 {
			var doc any
			if err := json.Unmarshal(resp, &doc); err != nil {
				return tsierr.New(tsierr.CodeBootstrap, r.label(), st.ID, "response is not JSON", "", err)
			}
			for name, path := range st.Capture {
				v, ok := lookup(doc, path)
				if !ok || fmt.Sprint(v) == "" {
					return tsierr.New(tsierr.CodeBootstrap, r.label(), st.ID,
						fmt.Sprintf("response has no %q field", path), "the module's API may have changed; report this", nil)
				}
				captured[name] = fmt.Sprint(v)
			}
		}
		for _, s := range st.Secret {
			redact.Add(captured[s])
		}
		for k, v := range captured {
			r.vars[k] = v
		}
		r.markDone(st, captured)
		r.Log("%s: %s", r.label(), st.Description)
		// Persist immediately: a secret shown once must not be lost to a later failure.
		return r.save()
	}
}

func (r *Runner) stepState(id string) (secrets.StepState, bool) {
	if r.State == nil {
		return secrets.StepState{}, false
	}
	s, ok := r.State.Steps[id]
	return s, ok
}

func (r *Runner) save() error {
	if r.Save == nil {
		return nil
	}
	return r.Save()
}

// selectItem captures fields from the list item that matches the step's Select.
func (r *Runner) selectItem(st catalog.Step, resp []byte, captured map[string]string) (bool, error) {
	var doc any
	if err := json.Unmarshal(resp, &doc); err != nil {
		return false, tsierr.New(tsierr.CodeBootstrap, r.label(), st.ID, "response is not JSON", "", err)
	}
	want := map[string]string{}
	for k, v := range st.Select.Match {
		rv, err := render(v, r.context())
		if err != nil {
			return false, tsierr.New(tsierr.CodeBootstrap, r.label(), st.ID, "cannot build the match", "", err)
		}
		want[k] = fmt.Sprint(rv)
	}
	list := doc
	if st.Select.From != "" {
		list, _ = lookup(doc, st.Select.From)
	}
	for _, item := range items(list) {
		obj, ok := item.(map[string]any)
		if !ok || !matches(obj, want) {
			continue
		}
		for name, path := range st.Select.Capture {
			v, ok := lookup(obj, path)
			if !ok {
				return false, tsierr.New(tsierr.CodeBootstrap, r.label(), st.ID, fmt.Sprintf("item has no %q field", path), "", nil)
			}
			captured[name] = fmt.Sprint(v)
		}
		return true, nil
	}
	return false, nil
}

// recover finds an object created by an earlier run that was interrupted before its
// identifiers were saved.
func (r *Runner) recover(ctx context.Context, st catalog.Step) (map[string]string, error) {
	rc := st.Recover
	body, err := render(rc.Body, r.context())
	if err != nil {
		return nil, tsierr.New(tsierr.CodeBootstrap, r.label(), st.ID, "cannot build the lookup request", "", err)
	}
	status, resp, err := r.call(ctx, catalog.Step{Path: rc.Path, Auth: st.Auth}, body)
	if err != nil {
		return nil, tsierr.New(tsierr.CodeBootstrap, r.label(), st.ID, "lookup failed", "", err)
	}
	if status != 200 {
		return nil, tsierr.New(tsierr.CodeBootstrap, r.label(), st.ID, fmt.Sprintf("lookup returned HTTP %d", status), "", nil)
	}
	var doc any
	if err := json.Unmarshal(resp, &doc); err != nil {
		return nil, tsierr.New(tsierr.CodeBootstrap, r.label(), st.ID, "lookup response is not JSON", "", err)
	}
	want := map[string]string{}
	for k, v := range rc.Match {
		rv, err := render(v, r.context())
		if err != nil {
			return nil, err
		}
		want[k] = fmt.Sprint(rv)
	}
	for _, item := range items(doc) {
		obj, ok := item.(map[string]any)
		if !ok || !matches(obj, want) {
			continue
		}
		out := map[string]string{}
		for name, path := range rc.Capture {
			v, ok := lookup(obj, path)
			if !ok {
				return nil, tsierr.New(tsierr.CodeBootstrap, r.label(), st.ID, fmt.Sprintf("existing object has no %q field", path), "", nil)
			}
			out[name] = fmt.Sprint(v)
		}
		return out, nil
	}
	return nil, tsierr.New(tsierr.CodeBootstrap, r.label(), st.ID, "already exists but could not be found again",
		"it was probably created by someone else; remove it or change its name in tsi.yaml", nil)
}

func items(doc any) []any {
	switch d := doc.(type) {
	case []any:
		return d
	case map[string]any:
		if v, ok := d["data"]; ok {
			return items(v)
		}
	}
	return nil
}

func matches(obj map[string]any, want map[string]string) bool {
	for k, v := range want {
		if fmt.Sprint(obj[k]) != v {
			return false
		}
	}
	return true
}

func (r *Runner) markDone(st catalog.Step, captured map[string]string) {
	if st.Volatile {
		return
	}
	if r.State == nil {
		return
	}
	r.State.Steps[st.ID] = secrets.StepState{Done: true, Vars: captured}
}

func (r *Runner) call(ctx context.Context, st catalog.Step, body any) (int, []byte, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.BaseURL+st.Path, bytes.NewReader(b))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if st.Auth == "bearer" && r.Module.Session == "bearer" {
		req.Header.Set("Authorization", "Bearer "+r.vars["token"])
	}
	for k, v := range st.Headers {
		req.Header.Set(k, v)
	}
	resp, err := r.hc.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return EffectiveStatus(resp.StatusCode, out), out, err
}

func expected(st catalog.Step) []int {
	if len(st.Expect) == 0 {
		return []int{200, 201}
	}
	return st.Expect
}

func contains(l []int, n int) bool {
	for _, x := range l {
		if x == n {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

var exact = regexp.MustCompile(`^\{\{\s*(\.[A-Za-z0-9_.]+)\s*\}\}$`)

// render walks a value and expands templates in strings. A string that is exactly one
// {{.Path}} keeps the type of the value it resolves to (lists, maps, booleans).
func render(v any, ctx map[string]any) (any, error) {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			r, err := render(e, ctx)
			if err != nil {
				return nil, err
			}
			out[k] = r
		}
		return out, nil
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			r, err := render(e, ctx)
			if err != nil {
				return nil, err
			}
			out[i] = r
		}
		return out, nil
	case string:
		if !strings.Contains(x, "{{") {
			return x, nil
		}
		if m := exact.FindStringSubmatch(x); m != nil {
			val, ok := lookup(ctx, strings.TrimPrefix(m[1], "."))
			if !ok {
				return nil, fmt.Errorf("%s is not set", m[1])
			}
			return val, nil
		}
		t, err := template.New("v").Option("missingkey=error").Parse(x)
		if err != nil {
			return nil, err
		}
		var b bytes.Buffer
		if err := t.Execute(&b, ctx); err != nil {
			return nil, err
		}
		return b.String(), nil
	}
	return v, nil
}

// lookup resolves a dotted path in nested maps.
func lookup(doc any, path string) (any, bool) {
	cur := doc
	for _, part := range strings.Split(strings.TrimPrefix(path, "."), ".") {
		switch m := cur.(type) {
		case map[string]any:
			v, ok := m[part]
			if !ok {
				return nil, false
			}
			cur = v
		default:
			return nil, false
		}
	}
	return cur, true
}

// EffectiveStatus returns the HTTP status, unless the module answered 2xx with an error
// status in the body (the vault does this for some failures).
func EffectiveStatus(status int, body []byte) int {
	if status >= 400 {
		return status
	}
	var probe struct {
		Status json.Number `json:"status"`
	}
	if json.Unmarshal(body, &probe) == nil && probe.Status != "" {
		if n, err := probe.Status.Int64(); err == nil && n >= 400 && n < 600 {
			return int(n)
		}
	}
	return status
}
