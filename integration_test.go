//go:build integration

package tsicli_test

import (
	"context"
	"testing"

	"github.com/tsi-coop/tsi-cli/compose"
	"github.com/tsi-coop/tsi-cli/config"
	"github.com/tsi-coop/tsi-cli/testkit"
)

// TestEndToEnd needs Docker. It brings the modules up together from nothing on one shared
// Postgres, provisions them, runs the smoke suites, then runs bootstrap again and expects
// nothing to change.
func TestEndToEnd(t *testing.T) {
	// The soft-launch set, with dx-node as a pair so a real exchange is tested.
	modules := []string{"tsi-dpdp-cms", "tsi-privacy-vault", "tsi-compass", "tsi-dx-node:node-a", "tsi-dx-node:node-b"}
	s := testkit.Start(t, compose.WithModules(modules...))
	ctx := context.Background()

	before := map[string]string{}
	for _, m := range modules {
		key := config.Key(m)
		c, ok := s.Credentials(key)
		if !ok || (c.APIKey == "" && c.AdminEmail == "") {
			t.Fatalf("%s: no credentials after Start: %+v", key, c)
		}
		before[key] = c.APIKey + "/" + c.APISecret + "/" + c.AdminPassword
	}
	results, err := s.Test(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		for _, c := range r.Checks {
			if !c.Passed && !c.Skipped {
				t.Errorf("%s/%s: %s", r.Module, c.ID, c.Error)
			}
		}
	}
	if err := s.Bootstrap(ctx); err != nil {
		t.Fatalf("second bootstrap: %v", err)
	}
	for _, m := range modules {
		key := config.Key(m)
		after, _ := s.Credentials(key)
		if after.APIKey+"/"+after.APISecret+"/"+after.AdminPassword != before[key] {
			t.Fatalf("%s: a second bootstrap changed the credentials", key)
		}
	}
}
