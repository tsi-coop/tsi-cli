// Package engine runs containers. v1 shells out to `docker compose`; the Engine
// interface lets tests use a fake and leaves room for a Docker SDK implementation.
package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tsicoop/tsi-cli/internal/redact"
	"github.com/tsicoop/tsi-cli/tsierr"
)

// Container is the state of one compose service.
type Container struct {
	Service string
	State   string // running, exited...
	Health  string // healthy, unhealthy, starting, ""
	Ports   string
}

// LogOptions tune log streaming.
type LogOptions struct {
	Follow bool
	Tail   int
}

// Engine is the container runtime.
type Engine interface {
	Preflight(ctx context.Context) error
	Pull(ctx context.Context, project Project, services ...string) error
	// Up starts the services and waits until they are running (and healthy, where a
	// healthcheck exists) when wait is true.
	Up(ctx context.Context, project Project, wait bool, services ...string) error
	Down(ctx context.Context, project Project, removeVolumes bool) error
	PS(ctx context.Context, project Project) ([]Container, error)
	Logs(ctx context.Context, project Project, w io.Writer, o LogOptions, services ...string) error
	// Digest returns the repo digest of a local image, or "" if it has none.
	Digest(ctx context.Context, image string) (string, error)
}

// Project identifies a compose project on disk.
type Project struct {
	Name string
	Dir  string // project directory (where the compose file lives)
	File string // compose file name inside Dir
}

// Docker implements Engine with the docker compose CLI.
type Docker struct {
	// Out receives progress output from docker (nil discards).
	Out io.Writer
}

func (d *Docker) compose(ctx context.Context, p Project, args ...string) *exec.Cmd {
	// Without a compose file yet (before the first write), Docker can still answer
	// questions about the project by name, such as whether containers already exist.
	full := []string{"compose", "-p", p.Name}
	if _, err := os.Stat(filepath.Join(p.Dir, p.File)); err == nil {
		full = append(full, "-f", p.File)
	}
	full = append(full, args...)
	cmd := exec.CommandContext(ctx, "docker", full...)
	cmd.Dir = p.Dir
	return cmd
}

func (d *Docker) run(ctx context.Context, step string, cmd *exec.Cmd) error {
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	if d.Out != nil {
		fmt.Fprint(d.Out, redact.String(buf.String()))
	}
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return tsierr.New(tsierr.CodePreflight, "", step, "docker failed", lastLines(redact.String(buf.String()), 12), err)
	}
	return nil
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// Preflight checks that Docker and the compose plugin are reachable.
func (d *Docker) Preflight(ctx context.Context) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return tsierr.New(tsierr.CodePreflight, "", "preflight", "docker is not installed or not on PATH",
			"install Docker: https://docs.docker.com/get-docker/", err)
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(cctx, "docker", "info", "--format", "{{.ServerVersion}}").CombinedOutput(); err != nil {
		return tsierr.New(tsierr.CodePreflight, "", "preflight", "cannot reach the Docker daemon",
			"start Docker, or add your user to the docker group: "+strings.TrimSpace(string(out)), err)
	}
	if out, err := exec.CommandContext(cctx, "docker", "compose", "version", "--short").CombinedOutput(); err != nil {
		return tsierr.New(tsierr.CodePreflight, "", "preflight", "the docker compose plugin is missing",
			"install Docker Compose v2: "+strings.TrimSpace(string(out)), err)
	}
	return nil
}

// Pull downloads images, skipping ones already present locally.
func (d *Docker) Pull(ctx context.Context, p Project, services ...string) error {
	args := append([]string{"pull", "--ignore-buildable", "--policy", "missing"}, services...)
	return d.run(ctx, "pull", d.compose(ctx, p, args...))
}

// Up starts services.
func (d *Docker) Up(ctx context.Context, p Project, wait bool, services ...string) error {
	args := []string{"up", "-d", "--no-build"}
	if wait {
		args = append(args, "--wait", "--wait-timeout", "180")
	}
	args = append(args, services...)
	return d.run(ctx, "up", d.compose(ctx, p, args...))
}

// Down stops and removes the project's containers.
func (d *Docker) Down(ctx context.Context, p Project, removeVolumes bool) error {
	args := []string{"down", "--remove-orphans"}
	if removeVolumes {
		args = append(args, "--volumes")
	}
	return d.run(ctx, "down", d.compose(ctx, p, args...))
}

// PS lists the project's containers.
func (d *Docker) PS(ctx context.Context, p Project) ([]Container, error) {
	cmd := d.compose(ctx, p, "ps", "--all", "--format", "json")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return nil, tsierr.New(tsierr.CodePreflight, "", "ps", "docker compose ps failed", strings.TrimSpace(errb.String()), err)
	}
	var cs []Container
	dec := json.NewDecoder(&out)
	for dec.More() {
		var row struct {
			Service    string
			State      string
			Health     string
			Publishers []struct {
				URL           string
				PublishedPort int
				TargetPort    int
			}
		}
		if err := dec.Decode(&row); err != nil {
			return nil, err
		}
		var ports []string
		for _, pub := range row.Publishers {
			if pub.PublishedPort != 0 {
				ports = append(ports, fmt.Sprintf("%s:%d->%d", pub.URL, pub.PublishedPort, pub.TargetPort))
			}
		}
		cs = append(cs, Container{Service: row.Service, State: row.State, Health: row.Health, Ports: strings.Join(dedupe(ports), ",")})
	}
	return cs, nil
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// Logs streams container logs to w.
func (d *Docker) Logs(ctx context.Context, p Project, w io.Writer, o LogOptions, services ...string) error {
	args := []string{"logs", "--no-color"}
	if o.Follow {
		args = append(args, "-f")
	}
	if o.Tail > 0 {
		args = append(args, "--tail", fmt.Sprint(o.Tail))
	}
	args = append(args, services...)
	cmd := d.compose(ctx, p, args...)
	rw := &redactWriter{w: w}
	cmd.Stdout, cmd.Stderr = rw, rw
	if err := cmd.Run(); err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}

// Digest returns the registry digest of a pulled image.
func (d *Docker) Digest(ctx context.Context, image string) (string, error) {
	out, err := exec.CommandContext(ctx, "docker", "image", "inspect", "--format", "{{if .RepoDigests}}{{index .RepoDigests 0}}{{end}}", image).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

type redactWriter struct{ w io.Writer }

func (r *redactWriter) Write(p []byte) (int, error) {
	_, err := io.WriteString(r.w, redact.String(string(p)))
	return len(p), err
}
