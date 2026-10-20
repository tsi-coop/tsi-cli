package tsitest

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const dxnode = "tsi-dx-node"

// isSender reports whether this instance is the first node of a pair: it sends, its
// peer receives.
func (e *Env) isSender() bool { return e.Index == 0 && len(e.Peers) > 0 }

func (e *Env) senderOnly() error {
	if !e.isSender() {
		return Skipf("only the sending node (the first instance of a pair) runs this check")
	}
	return nil
}

func (e *Env) paired() error {
	if len(e.Peers) == 0 {
		return Skipf("needs two nodes: run tsi-dx-node:node-a and tsi-dx-node:node-b")
	}
	return nil
}

func init() {
	register(dxnode, []Check{
		{"ready", "The node answers the readiness probe", func(ctx context.Context, e *Env) error { return e.Ready(ctx) }},
		{"node_identity", "The node reports its identity", func(ctx context.Context, e *Env) error {
			st, doc, err := e.Admin(ctx, "/api/admin/node", map[string]any{"_func": "get_node_config"})
			if err != nil {
				return err
			}
			if err := expect(st, doc, 200); err != nil {
				return err
			}
			if field(doc, "node_id") == "" && field(dataOf(doc), "node_id") == "" {
				return fmt.Errorf("get_node_config returned no node_id: %v", doc)
			}
			return nil
		}},
		{"partner_active", "The partner is Active on this node", func(ctx context.Context, e *Env) error {
			if err := e.paired(); err != nil {
				return err
			}
			list, err := e.adminList(ctx, "/api/admin/partners", "list_partners")
			if err != nil {
				return err
			}
			for _, p := range list {
				if fmt.Sprint(p["status"]) == "Active" {
					return nil
				}
			}
			return fmt.Errorf("no Active partner: %v", list)
		}},
		{"contract_active", "A contract is Active on this node", func(ctx context.Context, e *Env) error {
			if err := e.paired(); err != nil {
				return err
			}
			list, err := e.adminList(ctx, "/api/admin/contracts", "list_contracts")
			if err != nil {
				return err
			}
			for _, c := range list {
				if fmt.Sprint(c["status"]) == "Active" && fmt.Sprint(c["name"]) == specString(e.Spec, "contract", "name") {
					return nil
				}
			}
			return fmt.Errorf("the contract is not Active: %v", list)
		}},
		{"bad_key_refused", "A wrong API secret is refused with 401", func(ctx context.Context, e *Env) error {
			if err := e.senderOnly(); err != nil {
				return err
			}
			st, doc, err := e.post(ctx, "/api/client/dx",
				map[string]string{"X-API-Key": e.Creds.APIKey, "X-API-Secret": "wrong-secret"},
				map[string]any{"_func": "list_contracts"})
			if err != nil {
				return err
			}
			return expect(st, doc, 401)
		}},
		{"async_transfer", "A governed package is delivered to the receiving node", asyncTransfer},
	})
}

// dataOf returns the "data" object of a response, or nil.
func dataOf(doc any) any {
	if m, ok := doc.(map[string]any); ok {
		return m["data"]
	}
	return nil
}

func specString(spec map[string]any, path ...string) string {
	var cur any = spec
	for _, p := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = m[p]
	}
	return fmt.Sprint(cur)
}

// adminList calls an admin list function; the nodes answer with a bare JSON array, or
// with {"data": [...]}.
func (e *Env) adminList(ctx context.Context, path, fn string) ([]map[string]any, error) {
	st, doc, err := e.Admin(ctx, path, map[string]any{"_func": fn})
	if err != nil {
		return nil, err
	}
	if err := expect(st, doc, 200); err != nil {
		return nil, err
	}
	return listOf(doc), nil
}

func listOf(doc any) []map[string]any {
	var arr []any
	switch d := doc.(type) {
	case []any:
		arr = d
	case map[string]any:
		arr, _ = d["data"].([]any)
	}
	var out []map[string]any
	for _, a := range arr {
		if m, ok := a.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func asyncTransfer(ctx context.Context, e *Env) error {
	if err := e.senderOnly(); err != nil {
		return err
	}
	if e.Creds.APIKey == "" {
		return errors.New("the sending node has no API key (bootstrap incomplete)")
	}
	receiver := e.Peers[0]

	// The contract id as the sender knows it.
	contracts, err := e.adminList(ctx, "/api/admin/contracts", "list_contracts")
	if err != nil {
		return err
	}
	var cid string
	for _, c := range contracts {
		if fmt.Sprint(c["name"]) == specString(e.Spec, "contract", "name") && fmt.Sprint(c["status"]) == "Active" {
			cid = fmt.Sprint(c["contract_id"])
		}
	}
	if cid == "" {
		return errors.New("no Active contract on the sending node")
	}

	sample, _ := e.Spec["sample"].(map[string]any)
	raw, _ := json.Marshal(sample)
	name := fmt.Sprintf("tsi-smoke-%d.json", time.Now().UnixNano())
	st, doc, err := e.post(ctx, "/api/client/dx",
		map[string]string{"X-API-Key": e.Creds.APIKey, "X-API-Secret": e.Creds.APISecret},
		map[string]any{"_func": "initiate_transfer", "contract_id": cid, "file_name": name,
			"file_data": base64.StdEncoding.EncodeToString(raw)})
	if err != nil {
		return err
	}
	if err := expect(st, doc, 200, 201); err != nil {
		return err
	}
	tid := field(doc, "transfer_id")
	if tid == "" {
		return fmt.Errorf("initiate_transfer returned no transfer_id: %v", doc)
	}

	// The sender tracks it to Delivered.
	deadline := time.Now().Add(60 * time.Second)
	var status string
	for {
		st, doc, err = e.post(ctx, "/api/client/dx",
			map[string]string{"X-API-Key": e.Creds.APIKey, "X-API-Secret": e.Creds.APISecret},
			map[string]any{"_func": "get_transfer_status", "transfer_id": tid})
		if err != nil {
			return err
		}
		status = field(doc, "status")
		if status == "Delivered" || status == "Failed" || time.Now().After(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	if status != "Delivered" {
		return fmt.Errorf("transfer %s ended as %q: %v", tid, status, doc)
	}

	// The receiver shows it as Received, with the contract's governance applied.
	list, err := receiver.adminList(ctx, "/api/admin/transfers", "list_transfers")
	if err != nil {
		return fmt.Errorf("receiver: %w", err)
	}
	var recv string
	for _, t := range list {
		if fmt.Sprint(t["transfer_id"]) == tid {
			recv = fmt.Sprint(t["status"])
		}
	}
	if recv != "Received" {
		return fmt.Errorf("the receiving node shows %q for transfer %s, want Received", recv, tid)
	}
	st, doc, err = receiver.Admin(ctx, "/api/admin/transfers", map[string]any{"_func": "get_transfer_payload", "transfer_id": tid})
	if err != nil {
		return err
	}
	if err := expect(st, doc, 200); err != nil {
		return err
	}
	var stored map[string]any
	if err := json.Unmarshal([]byte(field(doc, "payload")), &stored); err != nil {
		return fmt.Errorf("receiver payload is not JSON: %v", doc)
	}
	if fmt.Sprint(stored["applicant_id"]) == fmt.Sprint(sample["applicant_id"]) {
		return errors.New("applicant_id arrived in clear; the contract says HASH")
	}
	if fmt.Sprint(stored["mobile"]) == fmt.Sprint(sample["mobile"]) {
		return errors.New("mobile arrived in clear; the contract says MASK")
	}
	if !strings.EqualFold(fmt.Sprint(stored["product"]), fmt.Sprint(sample["product"])) {
		return fmt.Errorf("a non-PII field changed in transit: %v", stored)
	}
	return nil
}
