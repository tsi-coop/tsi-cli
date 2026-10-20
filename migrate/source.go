package migrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tsi-coop/tsi-cli/catalog"
	"github.com/tsi-coop/tsi-cli/tsierr"
)

// Source supplies the SQL of a module's migration files.
type Source interface {
	Get(ctx context.Context, m *catalog.Module, step catalog.MigrationStep) ([]byte, error)
}

// DefaultBaseURL serves raw files of public GitHub repositories.
const DefaultBaseURL = "https://raw.githubusercontent.com"

// Fetcher downloads migration files from the module's repository at the pinned ref,
// verifies each against the SHA-256 recorded in the catalog, and caches the verified
// file. A file that does not match is never written to the cache and never run.
//
// If LocalDirs names a directory for a module, files are read from it instead. That is
// for developing a module or working offline; those files are not checked against the
// catalog (the point is to try changed SQL), so a warning is logged.
type Fetcher struct {
	// CacheDir defaults to the user cache directory (override with TSI_CACHE_DIR).
	CacheDir string
	// BaseURL defaults to DefaultBaseURL.
	BaseURL string
	HTTP    *http.Client
	// LocalDirs maps a module name to a local directory holding its migration files.
	LocalDirs map[string]string
	// Warn receives notices (nil discards).
	Warn func(format string, a ...any)
}

func (f *Fetcher) cacheDir() (string, error) {
	if f.CacheDir != "" {
		return f.CacheDir, nil
	}
	if d := os.Getenv("TSI_CACHE_DIR"); d != "" {
		return d, nil
	}
	d, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "tsi-cli"), nil
}

// Get returns the verified SQL for a migration step.
func (f *Fetcher) Get(ctx context.Context, m *catalog.Module, step catalog.MigrationStep) ([]byte, error) {
	if dir, ok := f.LocalDirs[m.Name]; ok && dir != "" {
		b, err := os.ReadFile(filepath.Join(dir, step.File))
		if err != nil {
			return nil, tsierr.New(tsierr.CodeMigration, m.Name, step.File, "cannot read the local migration file",
				"check migration_dirs for "+m.Name+" in tsi.yaml", err)
		}
		if sum(b) != step.SHA256 && f.Warn != nil {
			f.Warn("%s: %s differs from the pinned release (using your local copy)", m.Name, step.File)
		}
		return b, nil
	}

	src := m.Migrations.Source
	cache, err := f.cacheDir()
	if err != nil {
		return nil, err
	}
	cached := filepath.Join(cache, "migrations", strings.ReplaceAll(src.Repo, "/", "_"), src.Ref, src.Dir, step.File)
	if b, err := os.ReadFile(cached); err == nil && sum(b) == step.SHA256 {
		return b, nil
	}

	base := f.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	url := fmt.Sprintf("%s/%s/%s/%s/%s", strings.TrimRight(base, "/"), src.Repo, src.Ref, src.Dir, step.File)
	b, err := f.download(ctx, url)
	if err != nil {
		return nil, tsierr.New(tsierr.CodeMigration, m.Name, step.File, "cannot download the migration",
			"check your network, or use a local checkout: set migration_dirs."+m.Name+" in tsi.yaml to its db directory (source: "+url+")", err)
	}
	if got := sum(b); got != step.SHA256 {
		return nil, tsierr.New(tsierr.CodeMigration, m.Name, step.File,
			fmt.Sprintf("downloaded file does not match the pinned checksum (got %s, want %s)", got[:12], step.SHA256[:12]),
			"the file changed upstream after this tsi release pinned it; upgrade tsi, or report it. Nothing was applied", nil)
	}
	if err := writeAtomic(cached, b); err != nil {
		// A cache that cannot be written is not fatal: the verified bytes are still good.
		if f.Warn != nil {
			f.Warn("cannot cache %s: %v", step.File, err)
		}
	}
	return b, nil
}

func (f *Fetcher) download(ctx context.Context, url string) ([]byte, error) {
	hc := f.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * 700 * time.Millisecond):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		resp, err := hc.Do(req)
		if err != nil {
			last = err
			continue
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		switch {
		case err != nil:
			last = err
		case resp.StatusCode == http.StatusOK:
			return b, nil
		case resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests:
			last = fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
		default: // 404 and friends will not get better
			return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
		}
	}
	return nil, last
}

// Prefetch downloads and verifies every migration file of a module, so a bad network or a
// checksum problem is reported before any container is started.
func Prefetch(ctx context.Context, src Source, m *catalog.Module) error {
	for _, st := range m.Migrations.Steps {
		if st.File == "" {
			continue
		}
		if _, err := src.Get(ctx, m, st); err != nil {
			return err
		}
	}
	return nil
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func writeAtomic(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".dl-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
