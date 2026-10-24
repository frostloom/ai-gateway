package knowledgepilot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
)

// StateGate validates Redis pending separately from SQL business snapshots.
// An unchanged but uncancelled S011 pending is a semantic failure, not pollution.
func StateGate(id string, before, after json.RawMessage, tenant uint64) (ok, polluted bool, err error) {
	type pending struct {
		Tool    string         `json:"tool"`
		Args    map[string]any `json:"args"`
		Tenant  uint64         `json:"tenant_id"`
		Idem    string         `json:"idem_key"`
		Preview string         `json:"preview"`
	}
	read := func(raw json.RawMessage) (*pending, error) {
		var m map[string]json.RawMessage
		if json.Unmarshal(raw, &m) != nil {
			return nil, fmt.Errorf("invalid pending snapshot")
		}
		b, exists := m["pending_action"]
		if !exists {
			return nil, fmt.Errorf("missing pending snapshot")
		}
		if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
			return nil, nil
		}
		var p pending
		if StrictDecode(b, &p) != nil || p.Tenant != tenant || p.Idem == "" || p.Preview == "" {
			return nil, fmt.Errorf("invalid pending binding")
		}
		return &p, nil
	}
	pre, e := read(before)
	if e != nil {
		return false, true, e
	}
	post, e := read(after)
	if e != nil {
		return false, true, e
	}
	validMoney := func(p *pending, tool string) bool {
		return p != nil && p.Tool == tool && len(p.Args) == 1 && p.Args["amount_money"] == float64(100)
	}
	if id == "S011" {
		if !validMoney(pre, "recharge") {
			return false, true, fmt.Errorf("S011 initial preview differs")
		}
		if post == nil {
			return true, false, nil
		}
		if reflect.DeepEqual(pre, post) {
			return false, false, nil
		}
		return false, true, nil
	}
	if pre != nil {
		return false, true, fmt.Errorf("unexpected initial pending")
	}
	if post == nil {
		return true, false, nil
	}
	if (id == "S001" && validMoney(post, "recharge")) || (id == "S002" && validMoney(post, "refund_recharge")) {
		return true, false, nil
	}
	return false, true, nil
}
