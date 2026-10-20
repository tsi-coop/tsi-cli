package tsitest

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const compass = "tsi-compass"

func init() {
	register(compass, []Check{
		{"ready", "The app answers the readiness probe", func(ctx context.Context, e *Env) error { return e.Ready(ctx) }},
		{"setup_complete", "Setup reports that the first administrator exists", func(ctx context.Context, e *Env) error {
			st, doc, err := e.post(ctx, "/api/v1/bootstrap/setup", nil, map[string]any{"_func": "check_setup"})
			if err != nil {
				return err
			}
			if err := expect(st, doc, 200); err != nil {
				return err
			}
			if field(doc, "setup_done") != "true" {
				return fmt.Errorf("setup_done is not true: %v", doc)
			}
			return nil
		}},
		{"wrong_password_refused", "A wrong administrator password is refused with 401", func(ctx context.Context, e *Env) error {
			st, doc, err := e.post(ctx, "/api/v1/admin/user", nil, map[string]any{"_func": "login",
				"email": e.Creds.AdminEmail, "password": "definitely-not-the-password"})
			if err != nil {
				return err
			}
			return expect(st, doc, 401)
		}},
		{"unauthenticated_refused", "An admin call without a session is refused with 401", func(ctx context.Context, e *Env) error {
			st, doc, err := e.post(ctx, "/api/v1/admin/ops", nil, map[string]any{"_func": "list_assets"})
			if err != nil {
				return err
			}
			return expect(st, doc, 401)
		}},
		{"reference_data", "The migrations seeded the asset categories", func(ctx context.Context, e *Env) error {
			st, doc, err := e.Admin(ctx, "/api/v1/admin/ops", map[string]any{"_func": "list_asset_categories"})
			if err != nil {
				return err
			}
			if err := expect(st, doc, 200); err != nil {
				return err
			}
			cats, _ := asMap(doc)["categories"].([]any)
			if len(cats) == 0 {
				return errors.New("no asset categories were seeded")
			}
			return nil
		}},
		{"asset_roundtrip", "An asset can be created, listed and deleted", assetRoundtrip},
	})
}

func assetRoundtrip(ctx context.Context, e *Env) error {
	st, doc, err := e.Admin(ctx, "/api/v1/admin/ops", map[string]any{"_func": "list_asset_categories"})
	if err != nil {
		return err
	}
	if err := expect(st, doc, 200); err != nil {
		return err
	}
	cats, _ := asMap(doc)["categories"].([]any)
	if len(cats) == 0 {
		return errors.New("no asset categories to attach the asset to")
	}
	catID := fmt.Sprint(cats[0].(map[string]any)["id"])
	tag := fmt.Sprintf("TSI-SMOKE-%d", time.Now().UnixNano())
	st, doc, err = e.Admin(ctx, "/api/v1/admin/ops", map[string]any{"_func": "add_asset",
		"name": "tsi smoke asset", "asset_tag": tag, "category_id": catID, "criticality": "LOW"})
	if err != nil {
		return err
	}
	if err := expect(st, doc, 200, 201); err != nil {
		return err
	}
	id := field(doc, "id")
	if id == "" {
		return errors.New("add_asset returned no id")
	}
	deleted := false
	defer func() { // do not leave test data behind if a later step fails
		if !deleted {
			e.Admin(ctx, "/api/v1/admin/ops", map[string]any{"_func": "delete_asset", "id": id})
		}
	}()
	found := func() (bool, error) {
		st, doc, err := e.Admin(ctx, "/api/v1/admin/ops", map[string]any{"_func": "list_assets", "search": tag})
		if err != nil {
			return false, err
		}
		if err := expect(st, doc, 200); err != nil {
			return false, err
		}
		list, _ := asMap(doc)["assets"].([]any)
		for _, a := range list {
			if m, ok := a.(map[string]any); ok && fmt.Sprint(m["asset_tag"]) == tag {
				return true, nil
			}
		}
		return false, nil
	}
	if ok, err := found(); err != nil || !ok {
		return fmt.Errorf("the new asset is not in the list (err=%v)", err)
	}
	st, doc, err = e.Admin(ctx, "/api/v1/admin/ops", map[string]any{"_func": "delete_asset", "id": id})
	if err != nil {
		return err
	}
	if err := expect(st, doc, 200); err != nil {
		return err
	}
	deleted = true
	if ok, err := found(); err != nil || ok {
		return fmt.Errorf("the asset is still listed after delete (err=%v)", err)
	}
	return nil
}
