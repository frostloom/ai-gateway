package store

import "testing"

func TestKafkaMessageKeyUsesTenant(t *testing.T) {
	ev := &BillingEvent{TenantID: 7, RequestID: "req-a", EventID: "req-a:reserve"}
	if got := KafkaMessageKey(ev); got != "7" {
		t.Fatalf("tenant key = %s", got)
	}
	other := &BillingEvent{TenantID: 7, RequestID: "req-b", EventID: "req-b:settle"}
	if KafkaMessageKey(ev) != KafkaMessageKey(other) {
		t.Fatal("same tenant must share one partition key")
	}
	if got := KafkaMessageKey(&BillingEvent{RequestID: "req-c", EventID: "req-c:reserve"}); got != "req-c" {
		t.Fatalf("request key = %s", got)
	}
	payload := []byte(`{"tenant_id":7,"request_id":"req-a","event_id":"req-a:reserve"}`)
	if got := KafkaMessageKeyFromPayload(payload, "req-a:reserve"); got != "7" {
		t.Fatalf("payload key = %s", got)
	}
	if got := KafkaMessageKeyFromPayload([]byte(`{"request_id":"req-c"}`), "fallback"); got != "fallback" {
		t.Fatalf("missing tenant key = %s", got)
	}
}
