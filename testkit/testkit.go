// Package testkit starts real TSI modules for an application's own tests:
//
//	func TestMyApp(t *testing.T) {
//	    s := testkit.Start(t, compose.WithModules("tsi-dpdp-cms"))
//	    c, _ := s.Credentials("tsi-dpdp-cms")
//	    // point the app under test at c.BaseURL with c.APIKey / c.APISecret
//	}
//
// Start picks free ports, provisions the modules, and removes everything when the test ends.
package testkit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/tsi-coop/tsi-cli/catalog"
	"github.com/tsi-coop/tsi-cli/compose"
	"github.com/tsi-coop/tsi-cli/config"
	"github.com/tsi-coop/tsi-cli/tsierr"
)

// Start brings up and provisions the stack, and registers cleanup with t. It needs Docker.
func Start(t testing.TB, opts ...compose.Option) *compose.Stack {
	t.Helper()
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	base := []compose.Option{
		compose.WithDir(t.TempDir()),
		compose.WithProject("tsitest-" + hex.EncodeToString(b)),
		compose.WithOutput(testLog{t}),
	}
	s, err := compose.New(append(base, opts...)...)
	if err != nil {
		t.Fatalf("tsi: %v", err)
	}
	offset, err := freeOffset(s)
	if err != nil {
		t.Fatalf("tsi: %v", err)
	}
	s.Cfg.PortOffset = offset
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		var te *tsierr.Error
		if err := s.Down(ctx, compose.WithVolumes()); err != nil && !(errors.As(err, &te) && te.Code == tsierr.CodeNotFound) {
			t.Errorf("tsi: cleanup: %v", err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	if err := s.Up(ctx, compose.WithBootstrap()); err != nil {
		t.Fatalf("tsi: %v", err)
	}
	return s
}

type testLog struct{ t testing.TB }

func (l testLog) Write(p []byte) (int, error) {
	l.t.Log(string(p))
	return len(p), nil
}

// freeOffset finds a port offset at which every host port the stack needs is free.
func freeOffset(s *compose.Stack) (int, error) {
	cat, err := catalog.Load()
	if err != nil {
		return 0, err
	}
	var ports []int
	ports = append(ports, s.Cfg.Database.Port)
	seen := map[string]int{}
	for _, ref := range s.Cfg.Modules {
		base, _ := config.SplitRef(ref)
		m, err := cat.Get(base)
		if err != nil {
			return 0, err
		}
		ports = append(ports, m.Ports.App+seen[base], m.Ports.DB+seen[base])
		seen[base]++
	}
	for off := 1000; off < 30000; off += 20 {
		ok := true
		for _, p := range ports {
			ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", s.Cfg.Bind, p+off))
			if err != nil {
				ok = false
				break
			}
			ln.Close()
		}
		if ok {
			return off, nil
		}
	}
	return 0, fmt.Errorf("no free port range found")
}
