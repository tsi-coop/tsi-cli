package tsitest

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const vault = "tsi-privacy-vault"

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// entityCode returns the code of the first entity of a flavor in the provisioning spec.
func (e *Env) entityCode(flavor string) (string, error) {
	list, _ := e.Spec["entities"].([]any)
	for _, it := range list {
		m, _ := it.(map[string]any)
		if fmt.Sprint(m["flavor"]) == flavor {
			return fmt.Sprint(m["code"]), nil
		}
	}
	return "", fmt.Errorf("no %s entity in the provisioning spec", flavor)
}

func (e *Env) vaultCall(ctx context.Context, body map[string]any) (int, any, error) {
	return e.tenant(ctx, "/api/client/vault", body)
}

func init() {
	register(vault, []Check{
		{"ready", "The app answers the readiness probe", func(ctx context.Context, e *Env) error { return e.Ready(ctx) }},
		{"reject_bad_key", "A wrong API secret is refused with 401", func(ctx context.Context, e *Env) error {
			st, doc, err := e.post(ctx, "/api/client/vault",
				map[string]string{"X-API-Key": e.Creds.APIKey, "X-API-Secret": "wrong-secret"},
				map[string]any{"_func": "get_authorized_entities"})
			if err != nil {
				return err
			}
			return expect(st, doc, 401)
		}},
		storeFetch("store_fetch_id", "ID", "ID", func() string { return fmt.Sprintf("%012d", time.Now().UnixNano()%1_000_000_000_000) }),
		storeFetch("store_fetch_data", "DATA", "DATA", func() string { return "tsi smoke note " + time.Now().UTC().Format(time.RFC3339Nano) }),
		{"store_fetch_file", "A file is stored and fetched back with its name", func(ctx context.Context, e *Env) error {
			code, err := e.entityCode("FILE")
			if err != nil {
				return err
			}
			content := b64("%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\n%%EOF\n")
			st, doc, err := e.vaultCall(ctx, map[string]any{"_func": "store_data", "entityType": "FILE",
				"entityName": code, "content": content, "fileName": "tsi-smoke.pdf"})
			if err != nil {
				return err
			}
			if err := expect(st, doc, 201); err != nil {
				return err
			}
			st, doc, err = e.vaultCall(ctx, map[string]any{"_func": "fetch_data", "reference-key": field(doc, "reference_key")})
			if err != nil {
				return err
			}
			if err := expect(st, doc, 200); err != nil {
				return err
			}
			if field(doc, "value") != content || field(doc, "fileName") != "tsi-smoke.pdf" {
				return errors.New("fetched file differs from the stored file or lost its name")
			}
			return nil
		}},
		{"validation", "A value that fails the entity pattern is rejected", func(ctx context.Context, e *Env) error {
			code, err := e.entityCode("ID")
			if err != nil {
				return err
			}
			st, doc, err := e.vaultCall(ctx, map[string]any{"_func": "store_data", "entityType": "ID",
				"entityName": code, "content": b64("12345")})
			if err != nil {
				return err
			}
			return expect(st, doc, 422)
		}},
		{"search", "A stored value is found by the blind index", func(ctx context.Context, e *Env) error {
			code, err := e.entityCode("DATA")
			if err != nil {
				return err
			}
			word := fmt.Sprintf("Zephyr%d", time.Now().UnixNano())
			st, doc, err := e.vaultCall(ctx, map[string]any{"_func": "store_data", "entityType": "DATA",
				"entityName": code, "content": b64(word + " Quillfeather")})
			if err != nil {
				return err
			}
			if err := expect(st, doc, 201); err != nil {
				return err
			}
			ref := field(doc, "reference_key")
			st, doc, err = e.vaultCall(ctx, map[string]any{"_func": "search_data", "searchTerm": word})
			if err != nil {
				return err
			}
			if err := expect(st, doc, 200); err != nil {
				return err
			}
			if !strings.Contains(fmt.Sprint(doc), ref) {
				return fmt.Errorf("search for %q did not return reference %s", word, ref)
			}
			return nil
		}},
		{"unknown_reference", "An unknown reference is not returned", func(ctx context.Context, e *Env) error {
			st, doc, err := e.vaultCall(ctx, map[string]any{"_func": "fetch_data", "reference-key": "00000000-0000-0000-0000-000000000000"})
			if err != nil {
				return err
			}
			if st == http.StatusOK {
				return fmt.Errorf("an unknown reference returned data: %v", doc)
			}
			return nil
		}},
		{"read_only_cannot_store", "A read-only key can fetch but not store", readOnly},
		{"erasure", "Shredding a principal makes its records read as erased", erasure},
	})
}

func storeFetch(id, flavor, entityFlavor string, value func() string) Check {
	return Check{id, flavor + " value is stored and fetched back unchanged", func(ctx context.Context, e *Env) error {
		code, err := e.entityCode(entityFlavor)
		if err != nil {
			return err
		}
		content := b64(value())
		st, doc, err := e.vaultCall(ctx, map[string]any{"_func": "store_data", "entityType": flavor, "entityName": code, "content": content})
		if err != nil {
			return err
		}
		if err := expect(st, doc, 201); err != nil {
			return err
		}
		ref := field(doc, "reference_key")
		if ref == "" {
			return errors.New("store returned no reference_key")
		}
		st, doc, err = e.vaultCall(ctx, map[string]any{"_func": "fetch_data", "reference-key": ref})
		if err != nil {
			return err
		}
		if err := expect(st, doc, 200); err != nil {
			return err
		}
		if field(doc, "value") != content || field(doc, "flavor") != flavor {
			return fmt.Errorf("fetched record differs from what was stored: %v", doc)
		}
		return nil
	}}
}

func readOnly(ctx context.Context, e *Env) error {
	code, err := e.entityCode("DATA")
	if err != nil {
		return err
	}
	st, doc, err := e.Admin(ctx, "/api/admin/apikey", map[string]any{"_func": "create_key", "label": "tsi smoke read-only", "active": true})
	if err != nil {
		return err
	}
	if err := expect(st, doc, 200, 201); err != nil {
		return err
	}
	data, _ := asMap(doc)["data"].(map[string]any)
	key, secret := fmt.Sprint(data["api_key_id"]), fmt.Sprint(data["raw_api_secret"])
	defer func() { // leave no usable key behind
		e.Admin(ctx, "/api/admin/apikey", map[string]any{"_func": "update_key", "api_key_id": key, "label": "tsi smoke read-only", "active": false})
	}()
	st, doc, err = e.Admin(ctx, "/api/admin/permission", map[string]any{"_func": "create_permission", "api_key": key,
		"resource_type": "ENTITY", "resource_id": code, "can_read": true, "can_write": false, "granted_by": "tsi-cli"})
	if err != nil {
		return err
	}
	if err := expect(st, doc, 200, 201); err != nil {
		return err
	}
	// A record made with the read-write key...
	st, rec, err := e.vaultCall(ctx, map[string]any{"_func": "store_data", "entityType": "DATA", "entityName": code, "content": b64("ro check")})
	if err != nil {
		return err
	}
	if err := expect(st, rec, 201); err != nil {
		return err
	}
	ro := map[string]string{"X-API-Key": key, "X-API-Secret": secret}
	// ...can be read by the read-only key...
	st, doc2, err := e.post(ctx, "/api/client/vault", ro, map[string]any{"_func": "fetch_data", "reference-key": field(rec, "reference_key")})
	if err != nil {
		return err
	}
	if err := expect(st, doc2, 200); err != nil {
		return fmt.Errorf("read-only key cannot read: %w", err)
	}
	// ...but not written.
	st, doc2, err = e.post(ctx, "/api/client/vault", ro, map[string]any{"_func": "store_data", "entityType": "DATA", "entityName": code, "content": b64("nope")})
	if err != nil {
		return err
	}
	return expect(st, doc2, 403)
}

func erasure(ctx context.Context, e *Env) error {
	code, err := e.entityCode("DATA")
	if err != nil {
		return err
	}
	principal := fmt.Sprintf("tsi-smoke-%d", time.Now().UnixNano())
	st, doc, err := e.vaultCall(ctx, map[string]any{"_func": "store_data", "entityType": "DATA", "entityName": code,
		"content": b64("erase me " + principal), "principal_id": principal})
	if err != nil {
		return err
	}
	if err := expect(st, doc, 201); err != nil {
		return err
	}
	ref := field(doc, "reference_key")
	// A confirmation that does not match is refused and destroys nothing.
	st, doc, err = e.Admin(ctx, "/api/admin/erasure", map[string]any{"_func": "shred_principal", "principal_id": principal, "confirm": "nope"})
	if err != nil {
		return err
	}
	if err := expect(st, doc, 400); err != nil {
		return err
	}
	st, doc, err = e.Admin(ctx, "/api/admin/erasure", map[string]any{"_func": "shred_principal", "principal_id": principal, "confirm": principal})
	if err != nil {
		return err
	}
	if err := expect(st, doc, 200); err != nil {
		return err
	}
	st, doc, err = e.vaultCall(ctx, map[string]any{"_func": "fetch_data", "reference-key": ref})
	if err != nil {
		return err
	}
	return expect(st, doc, 410)
}
