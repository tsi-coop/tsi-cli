// Package health decides when a module is ready. No TSI module has a /health endpoint, so
// readiness is a cheap unauthenticated API call that touches the database; a 200 proves
// both the app and its database are up.
package health

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/tsicoop/tsi-cli/catalog"
	"github.com/tsicoop/tsi-cli/internal/redact"
	"github.com/tsicoop/tsi-cli/tsierr"
)

// Check runs the module's readiness probe once.
func Check(ctx context.Context, hc *http.Client, baseURL string, p catalog.Probe) error {
	body, _ := json.Marshal(p.Body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+p.Path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	expect := p.Expect
	if len(expect) == 0 {
		expect = []int{200}
	}
	for _, c := range expect {
		if resp.StatusCode == c {
			return nil
		}
	}
	return fmt.Errorf("HTTP %d: %s", resp.StatusCode, redact.String(string(b)))
}

// WaitReady polls with exponential backoff until the probe passes or the timeout elapses.
func WaitReady(ctx context.Context, m *catalog.Module, baseURL string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	hc := &http.Client{Timeout: 10 * time.Second}
	delay := 500 * time.Millisecond
	var last error
	for {
		last = Check(ctx, hc, baseURL, m.Ready)
		if last == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return tsierr.New(tsierr.CodeNotReady, m.Name, "ready", fmt.Sprintf("not ready after %s", timeout),
				"check `tsi compose logs "+m.Name+"` and that the port is free", last)
		case <-time.After(delay):
		}
		if delay < 5*time.Second {
			delay = delay * 3 / 2
		}
	}
}
