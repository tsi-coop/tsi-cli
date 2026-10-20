package tsitest

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const cms = "tsi-dpdp-cms"

func init() {
	register(cms, []Check{
		{"ready", "The app and its database answer the readiness probe", func(ctx context.Context, e *Env) error {
			return e.Ready(ctx)
		}},
		{"reject_bad_key", "An invalid API key is refused with 401", func(ctx context.Context, e *Env) error {
			st, doc, err := e.post(ctx, "/api/v1/client/consent",
				map[string]string{"X-API-Key": "00000000-0000-0000-0000-000000000000", "X-API-Secret": "invalid"},
				map[string]any{"_func": "get_active_consent", "user_id": "tsi-smoke"})
			if err != nil {
				return err
			}
			return expect(st, doc, 401)
		}},
		{"active_policy", "The published policy is returned", func(ctx context.Context, e *Env) error {
			st, doc, err := e.tenant(ctx, "/api/v1/client/policy", map[string]any{
				"_func": "get_active_policy", "fiduciary_id": e.Creds.Extra["fiduciary_id"], "jurisdiction": "IN"})
			if err != nil {
				return err
			}
			if err := expect(st, doc, 200); err != nil {
				return err
			}
			id := field(doc, "policy_id")
			if id == "" {
				return errors.New("active policy has no policy_id")
			}
			e.state["policy_id"] = id
			return nil
		}},
		{"record_consent", "A consent can be recorded", func(ctx context.Context, e *Env) error {
			pid, _ := e.state["policy_id"].(string)
			if pid == "" {
				return errors.New("no active policy (active_policy failed)")
			}
			user := fmt.Sprintf("tsi-smoke-%d", time.Now().UnixNano())
			e.state["user"] = user
			st, doc, err := e.tenant(ctx, "/api/v1/client/consent", map[string]any{
				"_func": "record_consent", "user_id": user, "policy_id": pid, "language_selected": "en",
				"data_point_consents": []any{map[string]any{
					"data_point_id": "purpose_service", "consent_granted": true,
					"purpose_agreed_to": "Service delivery", "timestamp_updated": time.Now().UTC().Format(time.RFC3339)}},
			})
			if err != nil {
				return err
			}
			return expect(st, doc, 201)
		}},
		{"validate_consent", "The recorded consent validates", func(ctx context.Context, e *Env) error {
			user, _ := e.state["user"].(string)
			if user == "" {
				return errors.New("no consent was recorded (record_consent failed)")
			}
			st, doc, err := e.tenant(ctx, "/api/v1/client/consent", map[string]any{
				"_func": "validate_consent", "user_id": user, "required_purpose_id": "purpose_service"})
			if err != nil {
				return err
			}
			if err := expect(st, doc, 200); err != nil {
				return err
			}
			if field(doc, "consent_granted") != "true" {
				return fmt.Errorf("consent not reported as granted: %v", doc)
			}
			return nil
		}},
		{"withdraw_consent", "The consent can be withdrawn", func(ctx context.Context, e *Env) error {
			user, _ := e.state["user"].(string)
			if user == "" {
				return errors.New("no consent was recorded (record_consent failed)")
			}
			st, doc, err := e.tenant(ctx, "/api/v1/client/consent", map[string]any{
				"_func": "withdraw_consent", "user_id": user, "reason": "tsi smoke test"})
			if err != nil {
				return err
			}
			if err := expect(st, doc, 200); err != nil {
				return err
			}
			st, doc, err = e.tenant(ctx, "/api/v1/client/consent", map[string]any{
				"_func": "validate_consent", "user_id": user, "required_purpose_id": "purpose_service"})
			if err != nil {
				return err
			}
			if err := expect(st, doc, 200); err != nil {
				return err
			}
			if field(doc, "consent_granted") != "false" {
				return fmt.Errorf("consent is still granted after withdrawal: %v", doc)
			}
			return nil
		}},
		{"read_only_cannot_write", "A READ-only key cannot record consent", func(ctx context.Context, e *Env) error {
			st, doc, err := e.Admin(ctx, "/api/v1/admin/apikey", map[string]any{
				"_func": "generate_api_key", "fiduciary_id": e.Creds.Extra["fiduciary_id"],
				"app_id": e.Creds.Extra["app_id"], "description": "tsi smoke read-only", "permissions": []any{"READ"}})
			if err != nil {
				return err
			}
			if err := expect(st, doc, 201); err != nil {
				return err
			}
			data, _ := asMap(doc)["data"].(map[string]any)
			key, secret := fmt.Sprint(data["key_id"]), fmt.Sprint(data["raw_api_key"])
			ro := map[string]string{"X-API-Key": key, "X-API-Secret": secret}
			// The key must be accepted for a read (404: authenticated, no such consent)...
			stR, docR, err := e.post(ctx, "/api/v1/client/consent", ro,
				map[string]any{"_func": "get_active_consent", "user_id": "tsi-smoke-ro"})
			if err != nil {
				return err
			}
			if err := expect(stR, docR, 200, 404); err != nil {
				return fmt.Errorf("READ-only key is not accepted for reads: %w", err)
			}
			// ...and refused for a write (this module answers 401).
			pid, _ := e.state["policy_id"].(string)
			st2, _, err := e.post(ctx, "/api/v1/client/consent", ro,
				map[string]any{"_func": "record_consent", "user_id": "tsi-smoke-ro", "policy_id": pid, "language_selected": "en",
					"data_point_consents": []any{map[string]any{"data_point_id": "purpose_service", "consent_granted": true,
						"purpose_agreed_to": "Service delivery", "timestamp_updated": time.Now().UTC().Format(time.RFC3339)}}})
			if err != nil {
				return err
			}
			if st2 != 401 && st2 != 403 {
				return fmt.Errorf("READ-only key should be refused with 401/403, got %d", st2)
			}
			return nil
		}},
	})
}
