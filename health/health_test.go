package health

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tsicoop/tsi-cli/catalog"
)

func TestWaitReadyRetriesUntilProbePasses(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&n, 1) < 3 {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()
	m := &catalog.Module{Name: "x", Ready: catalog.Probe{Path: "/p", Body: map[string]any{"_func": "f"}, Expect: []int{200}}}
	if err := WaitReady(context.Background(), m, srv.URL, 30*time.Second); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&n) != 3 {
		t.Fatalf("probe called %d times", n)
	}
}

func TestWaitReadyTimesOutWithModuleName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer srv.Close()
	m := &catalog.Module{Name: "tsi-x", Ready: catalog.Probe{Path: "/p"}}
	err := WaitReady(context.Background(), m, srv.URL, 1500*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "tsi-x") {
		t.Fatalf("got %v", err)
	}
}
