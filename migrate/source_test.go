package migrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/tsi-coop/tsi-cli/catalog"
	"github.com/tsi-coop/tsi-cli/tsierr"
)

const sqlBody = "CREATE TABLE t (id int);\n"

func hash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

func module() (*catalog.Module, catalog.MigrationStep) {
	st := catalog.MigrationStep{File: "01_init.sql", SHA256: hash(sqlBody)}
	return &catalog.Module{Name: "mod", Migrations: catalog.Migrations{
		Source: catalog.Source{Repo: "org/repo", Ref: "v1.0.0", Dir: "db"}, Steps: []catalog.MigrationStep{st}}}, st
}

func TestFetchVerifiesCachesAndBuildsURL(t *testing.T) {
	var hits int32
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		path = r.URL.Path
		w.Write([]byte(sqlBody))
	}))
	defer srv.Close()
	m, st := module()
	f := &Fetcher{CacheDir: t.TempDir(), BaseURL: srv.URL}
	for i := 0; i < 3; i++ {
		b, err := f.Get(context.Background(), m, st)
		if err != nil || string(b) != sqlBody {
			t.Fatalf("get: %v %q", err, b)
		}
	}
	if path != "/org/repo/v1.0.0/db/01_init.sql" {
		t.Fatalf("url path %q", path)
	}
	if hits != 1 {
		t.Fatalf("a verified file must be cached: %d downloads", hits)
	}
}

func TestChecksumMismatchIsRefusedAndNotCached(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("DROP TABLE users;"))
	}))
	defer srv.Close()
	m, st := module()
	cache := t.TempDir()
	f := &Fetcher{CacheDir: cache, BaseURL: srv.URL}
	_, err := f.Get(context.Background(), m, st)
	var te *tsierr.Error
	if err == nil || !asErr(err, &te) || !strings.Contains(te.Msg, "does not match the pinned checksum") {
		t.Fatalf("want a checksum error, got %v", err)
	}
	var cached []string
	filepath.Walk(cache, func(p string, i os.FileInfo, _ error) error {
		if i != nil && !i.IsDir() {
			cached = append(cached, p)
		}
		return nil
	})
	if len(cached) != 0 {
		t.Fatalf("a file that failed verification was cached: %v", cached)
	}
}

func TestTamperedCacheIsReplaced(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(sqlBody)) }))
	defer srv.Close()
	m, st := module()
	cache := t.TempDir()
	f := &Fetcher{CacheDir: cache, BaseURL: srv.URL}
	if _, err := f.Get(context.Background(), m, st); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(cache, "migrations", "org_repo", "v1.0.0", "db", "01_init.sql")
	os.WriteFile(p, []byte("evil"), 0o644)
	b, err := f.Get(context.Background(), m, st)
	if err != nil || string(b) != sqlBody {
		t.Fatalf("a corrupt cache entry must be re-downloaded: %v %q", err, b)
	}
}

func TestNotFoundDoesNotRetryAndHintsLocalDir(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(404)
	}))
	defer srv.Close()
	m, st := module()
	_, err := (&Fetcher{CacheDir: t.TempDir(), BaseURL: srv.URL}).Get(context.Background(), m, st)
	if err == nil || !strings.Contains(err.Error(), "migration_dirs.mod") {
		t.Fatalf("want a hint about migration_dirs, got %v", err)
	}
	if hits != 1 {
		t.Fatalf("a 404 must not be retried: %d", hits)
	}
}

func TestServerErrorIsRetried(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) < 2 {
			w.WriteHeader(503)
			return
		}
		w.Write([]byte(sqlBody))
	}))
	defer srv.Close()
	m, st := module()
	if _, err := (&Fetcher{CacheDir: t.TempDir(), BaseURL: srv.URL}).Get(context.Background(), m, st); err != nil || hits != 2 {
		t.Fatalf("err=%v hits=%d", err, hits)
	}
}

func TestLocalDirOverridesWithoutNetworkAndWarnsOnDifference(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "01_init.sql"), []byte("-- edited locally"), 0o644)
	m, st := module()
	var warned string
	f := &Fetcher{BaseURL: "http://127.0.0.1:1", LocalDirs: map[string]string{"mod": dir},
		Warn: func(format string, a ...any) { warned = format }}
	b, err := f.Get(context.Background(), m, st)
	if err != nil || string(b) != "-- edited locally" {
		t.Fatalf("%v %q", err, b)
	}
	if warned == "" {
		t.Fatal("using a file that differs from the pinned release must warn")
	}
	if _, err := (&Fetcher{LocalDirs: map[string]string{"mod": t.TempDir()}}).Get(context.Background(), m, st); err == nil {
		t.Fatal("a missing local file must fail")
	}
}

func asErr(err error, target **tsierr.Error) bool {
	e, ok := err.(*tsierr.Error)
	if ok {
		*target = e
	}
	return ok
}
