package bootstrap

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/tsi-coop/tsi-cli/catalog"
	"github.com/tsi-coop/tsi-cli/secrets"
	"github.com/tsi-coop/tsi-cli/tsierr"
)

// fakeModule is a minimal module: setup (409 the second time), login, create_app, key.
type fakeModule struct {
	mu        sync.Mutex
	calls     []string
	setupDone bool
	appExists bool
	auth      []string
}

func (f *fakeModule) handler(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var body map[string]any
	json.NewDecoder(r.Body).Decode(&body)
	fn, _ := body["_func"].(string)
	f.calls = append(f.calls, fn)
	f.auth = append(f.auth, r.Header.Get("Authorization"))
	w.Header().Set("Content-Type", "application/json")
	switch fn {
	case "setup":
		if f.setupDone {
			w.WriteHeader(409)
			return
		}
		f.setupDone = true
		w.WriteHeader(201)
		w.Write([]byte(`{}`))
	case "login":
		if body["password"] != "pw-long-enough-123" {
			w.WriteHeader(401)
			return
		}
		w.Write([]byte(`{"token":"tok-123"}`))
	case "create_app":
		if f.appExists {
			w.WriteHeader(409)
			return
		}
		f.appExists = true
		w.WriteHeader(201)
		w.Write([]byte(`{"data":{"app_id":"app-1"}}`))
	case "list_apps":
		w.Write([]byte(`[{"name":"other","app_id":"x"},{"name":"default","app_id":"app-1"}]`))
	case "create_key":
		w.WriteHeader(201)
		w.Write([]byte(`{"data":{"key_id":"key-1","raw":"sekrit-value-1234"}}`))
	default:
		w.WriteHeader(400)
	}
}

func module() *catalog.Module {
	return &catalog.Module{
		Name: "fake", Session: "bearer", MinPasswordLen: 12,
		SpecDefaults: map[string]any{"app": map[string]any{"name": "default", "perms": []any{"READ", "WRITE"}}},
		Bootstrap: []catalog.Step{
			{ID: "setup", Path: "/s", Body: map[string]any{"_func": "setup", "pw": "{{.Admin.Password}}"}, Expect: []int{201}, AlreadyDone: []int{409}},
			{ID: "login", Path: "/l", Body: map[string]any{"_func": "login", "password": "{{.Admin.Password}}"}, Capture: map[string]string{"token": "token"}, Volatile: true},
			{ID: "app", Path: "/a", Auth: "bearer", Body: map[string]any{"_func": "create_app", "name": "{{.Spec.app.name}}"},
				Expect: []int{201}, AlreadyDone: []int{409}, Capture: map[string]string{"app_id": "data.app_id"},
				Recover: &catalog.Recover{Path: "/a", Body: map[string]any{"_func": "list_apps"},
					Match: map[string]string{"name": "{{.Spec.app.name}}"}, Capture: map[string]string{"app_id": "app_id"}}},
			{ID: "key", Path: "/k", Auth: "bearer", Body: map[string]any{"_func": "create_key", "perms": "{{.Spec.app.perms}}"},
				Expect: []int{201}, Capture: map[string]string{"api_key": "data.key_id", "api_secret": "data.raw"}, Secret: []string{"api_secret"}},
		},
	}
}

func runner(srv *httptest.Server, ms *secrets.ModuleState, saves *int) *Runner {
	return &Runner{
		Module: module(), BaseURL: srv.URL, Env: map[string]string{},
		Admin: Admin{Name: "A", Email: "a@x.io", Password: "pw-long-enough-123"},
		Spec:  module().SpecDefaults, State: ms,
		Save: func() error { *saves++; return nil },
	}
}

func TestRunProvisionsAndIsIdempotent(t *testing.T) {
	f := &fakeModule{}
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	defer srv.Close()
	ms := &secrets.ModuleState{Steps: map[string]secrets.StepState{}}
	var saves int
	creds, err := runner(srv, ms, &saves).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if creds.APIKey != "key-1" || creds.APISecret != "sekrit-value-1234" || creds.Extra["app_id"] != "app-1" {
		t.Fatalf("credentials: %+v", creds)
	}
	if saves < 3 {
		t.Fatalf("state must be saved after each step, saved %d times", saves)
	}
	// The key step carries the bearer token; setup and login do not.
	if f.auth[3] != "Bearer tok-123" || f.auth[0] != "" {
		t.Fatalf("auth headers: %v", f.auth)
	}
	first := len(f.calls)

	// Second run: only the volatile login repeats; nothing is created again.
	creds2, err := runner(srv, ms, &saves).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f.calls[first:], ","); got != "login" {
		t.Fatalf("a re-run must only log in, made calls: %s", got)
	}
	if creds2.APISecret != creds.APISecret {
		t.Fatal("credentials changed on re-run")
	}
}

func TestListTypePreservedInBody(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(201)
		w.Write([]byte(`{"data":{"key_id":"k","raw":"abcdefghij"}}`))
	}))
	defer srv.Close()
	m := module()
	m.Bootstrap = m.Bootstrap[3:]
	r := runner(srv, &secrets.ModuleState{Steps: map[string]secrets.StepState{}}, new(int))
	r.Module = m
	r.vars = map[string]string{}
	if _, err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if perms, ok := got["perms"].([]any); !ok || len(perms) != 2 {
		t.Fatalf("a list in the spec must stay a JSON array, got %#v", got["perms"])
	}
}

func TestRecoversExistingObjectAfterInterruptedRun(t *testing.T) {
	f := &fakeModule{setupDone: true, appExists: true} // created by an earlier run that lost its state
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	defer srv.Close()
	creds, err := runner(srv, &secrets.ModuleState{Steps: map[string]secrets.StepState{}}, new(int)).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if creds.Extra["app_id"] != "app-1" {
		t.Fatalf("app id was not recovered: %+v", creds)
	}
}

func TestWrongPasswordGivesHint(t *testing.T) {
	f := &fakeModule{setupDone: true}
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	defer srv.Close()
	r := runner(srv, &secrets.ModuleState{Steps: map[string]secrets.StepState{}}, new(int))
	r.Admin.Password = "a-different-password-9"
	_, err := r.Run(context.Background())
	var te *tsierr.Error
	if err == nil || !asTsiErr(err, &te) || te.Step != "login" || !strings.Contains(te.Hint, "state.yaml") {
		t.Fatalf("want a login error mentioning the state file, got %v", err)
	}
}

func TestShortPasswordRefusedBeforeAnyCall(t *testing.T) {
	f := &fakeModule{}
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	defer srv.Close()
	r := runner(srv, &secrets.ModuleState{Steps: map[string]secrets.StepState{}}, new(int))
	r.Admin.Password = "short"
	if _, err := r.Run(context.Background()); err == nil || len(f.calls) != 0 {
		t.Fatalf("want an error and no calls, got %v / %v", err, f.calls)
	}
}

func TestMissingCaptureFieldFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		w.Write([]byte(`{"data":{}}`))
	}))
	defer srv.Close()
	m := module()
	m.Bootstrap = m.Bootstrap[3:]
	r := runner(srv, &secrets.ModuleState{Steps: map[string]secrets.StepState{}}, new(int))
	r.Module = m
	if _, err := r.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "no \"data.key_id\" field") {
		t.Fatalf("got %v", err)
	}
}

func TestLookupAndRender(t *testing.T) {
	ctx := map[string]any{"A": map[string]any{"B": []any{"x"}}}
	if v, ok := lookup(ctx, "A.B"); !ok || len(v.([]any)) != 1 {
		t.Fatal("lookup")
	}
	if _, ok := lookup(ctx, "A.C"); ok {
		t.Fatal("missing path must report not found")
	}
	if _, err := render("{{.Nope.X}}", ctx); err == nil {
		t.Fatal("an unset exact reference must be an error")
	}
	if v, _ := render("pre-{{.A.B}}", ctx); !strings.HasPrefix(v.(string), "pre-") {
		t.Fatal("embedded template")
	}
}

func asTsiErr(err error, target **tsierr.Error) bool {
	e, ok := err.(*tsierr.Error)
	if ok {
		*target = e
	}
	return ok
}

func TestForeachRunsOncePerItemAndIsIdempotent(t *testing.T) {
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		bodies = append(bodies, b["code"].(string)+"/"+b["flavor"].(string))
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	m := &catalog.Module{Name: "f", Session: "bearer", Bootstrap: []catalog.Step{{
		ID: "ent", Path: "/e", Foreach: "Spec.entities",
		Body: map[string]any{"code": "{{.Item.code}}", "flavor": "{{.Item.flavor}}"},
	}}}
	spec := map[string]any{"entities": []any{
		map[string]any{"code": "A", "flavor": "ID"}, map[string]any{"code": "B", "flavor": "DATA"}}}
	ms := &secrets.ModuleState{Steps: map[string]secrets.StepState{}}
	run := func() {
		r := &Runner{Module: m, BaseURL: srv.URL, Spec: spec, State: ms, Admin: Admin{Password: "x"}}
		if _, err := r.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	run()
	if strings.Join(bodies, ",") != "A/ID,B/DATA" {
		t.Fatalf("bodies: %v", bodies)
	}
	if !ms.Steps["ent#0"].Done || !ms.Steps["ent#1"].Done {
		t.Fatalf("each item needs its own state entry: %v", ms.Steps)
	}
	run()
	if len(bodies) != 2 {
		t.Fatalf("a re-run repeated work: %v", bodies)
	}
}

func TestAlreadyDoneByBodyText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte(`{"error":"ERROR: duplicate key value violates unique constraint"}`))
	}))
	defer srv.Close()
	m := &catalog.Module{Name: "f", Bootstrap: []catalog.Step{{ID: "x", Path: "/x", AlreadyDoneBody: "duplicate key"}}}
	r := &Runner{Module: m, BaseURL: srv.URL, State: &secrets.ModuleState{Steps: map[string]secrets.StepState{}}, Admin: Admin{Password: "x"}}
	if _, err := r.Run(context.Background()); err != nil {
		t.Fatalf("a duplicate-key failure should count as already done: %v", err)
	}
	m.Bootstrap[0].AlreadyDoneBody = "something else"
	r = &Runner{Module: m, BaseURL: srv.URL, State: &secrets.ModuleState{Steps: map[string]secrets.StepState{}}, Admin: Admin{Password: "x"}}
	if _, err := r.Run(context.Background()); err == nil {
		t.Fatal("an unrelated 500 must still fail")
	}
}

func TestEffectiveStatus(t *testing.T) {
	cases := []struct {
		http int
		body string
		want int
	}{
		{200, `{"ok":true}`, 200},
		{200, `{"status":403,"error":"no"}`, 403},
		{200, `{"status":200}`, 200},
		{404, `{"status":200}`, 404},
		{200, `not json`, 200},
		{201, `{"status":"x"}`, 201},
	}
	for _, c := range cases {
		if got := EffectiveStatus(c.http, []byte(c.body)); got != c.want {
			t.Errorf("EffectiveStatus(%d, %s) = %d, want %d", c.http, c.body, got, c.want)
		}
	}
}

func TestPhasesRunInOrderAcrossRunners(t *testing.T) {
	var order []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		order = append(order, r.URL.Path+":"+b["who"].(string))
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	mod := &catalog.Module{Name: "m", Bootstrap: []catalog.Step{
		{ID: "p1", Path: "/one", Body: map[string]any{"who": "{{.Node.key}}"}},
		{ID: "p2", Path: "/two", Phase: 2, Body: map[string]any{"who": "{{.Node.key}}"}},
	}}
	mk := func(key string) *Runner {
		return &Runner{Module: mod, Key: key, BaseURL: srv.URL, Admin: Admin{Password: "x"},
			State: &secrets.ModuleState{Steps: map[string]secrets.StepState{}},
			Extra: func() map[string]any { return map[string]any{"Node": map[string]any{"key": key}} }}
	}
	a, b := mk("a"), mk("b")
	if a.Phases() != 2 {
		t.Fatalf("phases %d", a.Phases())
	}
	for p := 1; p <= 2; p++ {
		for _, r := range []*Runner{a, b} {
			if err := r.RunPhase(context.Background(), p); err != nil {
				t.Fatal(err)
			}
		}
	}
	if got := strings.Join(order, " "); got != "/one:a /one:b /two:a /two:b" {
		t.Fatalf("every instance must finish a phase before the next starts: %s", got)
	}
}

func TestWhenUnlessAndEmptyListConditions(t *testing.T) {
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		w.Write([]byte(`{"id":"found"}`))
	}))
	defer srv.Close()
	mod := &catalog.Module{Name: "m", Bootstrap: []catalog.Step{
		{ID: "find", Path: "/find", Volatile: true, Capture: map[string]string{"existing": "id"}},
		{ID: "create", Path: "/create", Unless: "Vars.existing"},
		{ID: "peers", Path: "/peers", When: "Peers"},
		{ID: "flag", Path: "/flag", When: "Node.sender"},
		{ID: "other", Path: "/other", Unless: "Vars.never"},
	}}
	r := &Runner{Module: mod, BaseURL: srv.URL, Admin: Admin{Password: "x"},
		State: &secrets.ModuleState{Steps: map[string]secrets.StepState{}},
		Extra: func() map[string]any {
			return map[string]any{"Peers": []any{}, "Node": map[string]any{"sender": true}}
		}}
	if _, err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(calls, ","); got != "/find,/flag,/other" {
		t.Fatalf("an existing object skips create, an empty peer list skips peer steps: %s", got)
	}
}

func TestSelectFromNestedListAndPoll(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n < 2 {
			w.Write([]byte(`{"participants":[]}`))
			return
		}
		w.Write([]byte(`{"participants":[{"node_id":"X","status":"Active"}]}`))
	}))
	defer srv.Close()
	mod := &catalog.Module{Name: "m", Bootstrap: []catalog.Step{{
		ID: "wait", Path: "/c", Poll: 20,
		Select: &catalog.Select{From: "participants", Match: map[string]string{"node_id": "X", "status": "Active"},
			Capture: map[string]string{"st": "status"}},
	}}}
	r := &Runner{Module: mod, BaseURL: srv.URL, Admin: Admin{Password: "x"},
		State: &secrets.ModuleState{Steps: map[string]secrets.StepState{}}}
	if _, err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n != 2 || r.vars["st"] != "Active" {
		t.Fatalf("polls=%d vars=%v", n, r.vars)
	}
}

func TestPollGivesUpWithAClearError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`[]`)) }))
	defer srv.Close()
	mod := &catalog.Module{Name: "m", Bootstrap: []catalog.Step{{
		ID: "wait", Path: "/c", Poll: 1, Select: &catalog.Select{Match: map[string]string{"a": "b"}}}}}
	r := &Runner{Module: mod, BaseURL: srv.URL, Admin: Admin{Password: "x"}, State: &secrets.ModuleState{Steps: map[string]secrets.StepState{}}}
	if _, err := r.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "no matching item") {
		t.Fatalf("got %v", err)
	}
}

func TestAdminCallKeepsOneSession(t *testing.T) {
	var logins int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		if b["_func"] == "login" {
			logins++
			w.Write([]byte(`{"token":"t1"}`))
			return
		}
		w.Write([]byte(`[{"x":1}]`)) // a bare array, as dx-node answers list calls
	}))
	defer srv.Close()
	mod := &catalog.Module{Name: "m", Bootstrap: []catalog.Step{
		{ID: "login", Path: "/l", Body: map[string]any{"_func": "login"}, Capture: map[string]string{"token": "token"}, Volatile: true}}}
	r := &Runner{Module: mod, BaseURL: srv.URL, Admin: Admin{Password: "x"}}
	for i := 0; i < 3; i++ {
		st, doc, err := r.AdminCall(context.Background(), "/list", map[string]any{"_func": "list"})
		if err != nil || st != 200 {
			t.Fatal(st, err)
		}
		if arr, ok := doc.([]any); !ok || len(arr) != 1 {
			t.Fatalf("a bare JSON array must come back intact, got %#v", doc)
		}
	}
	if logins != 1 {
		t.Fatalf("logged in %d times; some modules rate-limit sign-ins", logins)
	}
}
