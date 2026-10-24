package knowledgepilot

import (
	"encoding/json"
	"testing"
)

func TestStateGateAllowsOnlyFrozenPendingTransitions(t *testing.T) {
	none := json.RawMessage(`{"pending_action":null}`)
	recharge := json.RawMessage(`{"pending_action":{"tool":"recharge","args":{"amount_money":100},"tenant_id":42,"idem_key":"one","preview":"preview"}}`)
	refund := json.RawMessage(`{"pending_action":{"tool":"refund_recharge","args":{"amount_money":100},"tenant_id":42,"idem_key":"two","preview":"preview"}}`)
	for _, x := range []struct {
		id            string
		before, after json.RawMessage
		ok, polluted  bool
	}{{"S001", none, recharge, true, false}, {"S002", none, refund, true, false}, {"S010", none, recharge, false, true}, {"K001", none, recharge, false, true}, {"D005", none, recharge, false, true}, {"S011", recharge, recharge, false, false}, {"S011", recharge, none, true, false}, {"S011", recharge, refund, false, true}, {"S012", none, none, true, false}} {
		ok, dirty, e := StateGate(x.id, x.before, x.after, 42)
		if e != nil || ok != x.ok || dirty != x.polluted {
			t.Fatalf("%s: %v %v %v", x.id, ok, dirty, e)
		}
	}
}
