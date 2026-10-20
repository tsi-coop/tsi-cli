package engine

import (
	"context"
	"fmt"
	"io"
	"sync"
)

// Fake records calls and returns canned state. For unit tests.
type Fake struct {
	// Owners maps a host port to the description PortOwner returns.
	Owners map[int]string
	// Dirs is what ProjectDirs returns.
	Dirs       []string
	mu         sync.Mutex
	Calls      []string
	Containers []Container
	Err        map[string]error // method name -> error to return
}

func (f *Fake) rec(name string, args ...any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, fmt.Sprint(append([]any{name}, args...)...))
	return f.Err[name]
}

func (f *Fake) Preflight(ctx context.Context) error { return f.rec("Preflight") }
func (f *Fake) Pull(ctx context.Context, p Project, s ...string) error {
	return f.rec("Pull", s)
}
func (f *Fake) Up(ctx context.Context, p Project, wait bool, s ...string) error {
	return f.rec("Up", wait, s)
}
func (f *Fake) Down(ctx context.Context, p Project, v bool) error { return f.rec("Down", v) }
func (f *Fake) PS(ctx context.Context, p Project) ([]Container, error) {
	return f.Containers, f.rec("PS")
}
func (f *Fake) Logs(ctx context.Context, p Project, w io.Writer, o LogOptions, s ...string) error {
	return f.rec("Logs", s)
}

func (f *Fake) Digest(ctx context.Context, image string) (string, error) {
	return "", f.rec("Digest", image)
}

func (f *Fake) ProjectDirs(ctx context.Context, project string) ([]string, error) {
	return f.Dirs, f.rec("ProjectDirs")
}

func (f *Fake) PortOwner(ctx context.Context, port int) string { return f.Owners[port] }
