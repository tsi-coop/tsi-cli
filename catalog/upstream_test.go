//go:build network

package catalog_test

import (
	"context"
	"testing"

	"github.com/tsi-coop/tsi-cli/catalog"
	"github.com/tsi-coop/tsi-cli/migrate"
)

// TestPinnedMigrationsMatchUpstream downloads every migration file of every module from
// GitHub and checks it against the checksum in the catalog. Run it in CI: it fails when a
// tag was moved or a checksum in the catalog is wrong. Needs internet access.
func TestPinnedMigrationsMatchUpstream(t *testing.T) {
	cat, err := catalog.Load()
	if err != nil {
		t.Fatal(err)
	}
	f := &migrate.Fetcher{CacheDir: t.TempDir()}
	for _, n := range cat.Names() {
		m, _ := cat.Get(n)
		t.Run(n, func(t *testing.T) {
			if err := migrate.Prefetch(context.Background(), f, m); err != nil {
				t.Fatal(err)
			}
		})
	}
}
