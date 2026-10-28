package taskbench

import (
	"encoding/json"
	"reflect"
	"testing"
)

func stoppedLiabilityFixture(t *testing.T) (windowAccountingAudit, windowPayloadPacket, []byte) {
	t.Helper()
	c := windowTestContract()
	c.Model = windowProtocolLimits("model")
	c.Model.MaxInput, c.Model.MaxRequestBytes, c.Model.MaxAttempts = 36864, 32768, 300
	c.ModelCap, c.JEVCap = 414586, 4079
	r := windowTestRequest("model")
	r.Model = "deepseek-flash"
	body := []byte(windowStandardTierResponse)
	p := windowPayloadPacket{Sequence: 1, Identity: r, ScopeSHA256: c.ScopeSHA256, ClaimSHA256: c.ClaimSHA256, WindowID: c.WindowID,
		Request: windowPayloadBody{File: "request-000001.body", Bytes: r.Bytes, Complete: true, FullSHA256: r.BodySHA256}, RequestDescriptorFile: "request-000001.json",
		Response: &windowPayloadResponse{Status: 200, Code: "complete", Complete: true, Available: true,
			Body: &windowPayloadBody{File: "response-000001.body", Bytes: len(body), Complete: true, FullSHA256: conversationHash(body)}, DescriptorFile: "response-000001.json"}}
	a := windowAccountingAudit{Contract: c, State: windowAccountingState{ModelPermanent: c.ModelCap, JEVPermanent: c.JEVCap,
		ModelHeld: 74752, ModelAttempts: 1, Pending: &windowPendingIntent{Sequence: 1, Request: r}, Halt: "protocol_failure", Closed: true}}
	return a, p, body
}

func TestWindowStoppedLiabilityIsBoundedWithoutSettling(t *testing.T) {
	a, p, body := stoppedLiabilityFixture(t)
	before, _ := json.Marshal(a)
	cost, err := verifyWindowStoppedLiability(a, p, body)
	if err != nil || cost != 3832 {
		t.Fatalf("complete old response not independently bounded: cost=%d err=%v", cost, err)
	}
	after, _ := json.Marshal(a)
	if !reflect.DeepEqual(before, after) || a.State.Pending == nil || a.State.ModelKnown != 0 || a.State.ModelHeld != 74752 {
		t.Fatal("retrospective proof rewrote original ledger")
	}
}

func TestWindowStoppedLiabilityRejectsUnboundedOrCrossedEvidence(t *testing.T) {
	mutations := map[string]func(*windowAccountingAudit, *windowPayloadPacket, *[]byte){
		"open ledger":       func(a *windowAccountingAudit, _ *windowPayloadPacket, _ *[]byte) { a.State.Closed = false },
		"missing pending":   func(a *windowAccountingAudit, _ *windowPayloadPacket, _ *[]byte) { a.State.Pending = nil },
		"rewritten known":   func(a *windowAccountingAudit, _ *windowPayloadPacket, _ *[]byte) { a.State.ModelKnown = 3832 },
		"insufficient hold": func(a *windowAccountingAudit, _ *windowPayloadPacket, _ *[]byte) { a.State.ModelHeld = 3831 },
		"hold exceeds cap": func(a *windowAccountingAudit, _ *windowPayloadPacket, _ *[]byte) {
			a.State.ModelHeld = a.State.ModelPermanent + 1
		},
		"different halt":      func(a *windowAccountingAudit, _ *windowPayloadPacket, _ *[]byte) { a.State.Halt = "upstream_failure" },
		"extra attempt":       func(a *windowAccountingAudit, _ *windowPayloadPacket, _ *[]byte) { a.State.ModelAttempts++ },
		"JEV pending":         func(a *windowAccountingAudit, _ *windowPayloadPacket, _ *[]byte) { a.State.JEVHeld = 1 },
		"different price":     func(a *windowAccountingAudit, _ *windowPayloadPacket, _ *[]byte) { a.Contract.Model.InputRate = 1 },
		"wrong scope":         func(_ *windowAccountingAudit, p *windowPayloadPacket, _ *[]byte) { p.ScopeSHA256 = "wrong" },
		"wrong claim":         func(_ *windowAccountingAudit, p *windowPayloadPacket, _ *[]byte) { p.ClaimSHA256 = "wrong" },
		"wrong window":        func(_ *windowAccountingAudit, p *windowPayloadPacket, _ *[]byte) { p.WindowID = "wrong" },
		"wrong identity":      func(_ *windowAccountingAudit, p *windowPayloadPacket, _ *[]byte) { p.Identity.Turn++ },
		"missing response":    func(_ *windowAccountingAudit, p *windowPayloadPacket, _ *[]byte) { p.Response = nil },
		"incomplete response": func(_ *windowAccountingAudit, p *windowPayloadPacket, _ *[]byte) { p.Response.Complete = false },
		"HTTP failure":        func(_ *windowAccountingAudit, p *windowPayloadPacket, _ *[]byte) { p.Response.Status = 503 },
		"body tampered":       func(_ *windowAccountingAudit, _ *windowPayloadPacket, b *[]byte) { *b = append(*b, ' ') },
		"different body digest": func(_ *windowAccountingAudit, p *windowPayloadPacket, _ *[]byte) {
			p.Response.Body.FullSHA256 = "wrong"
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			a, p, body := stoppedLiabilityFixture(t)
			mutate(&a, &p, &body)
			if _, err := verifyWindowStoppedLiability(a, p, body); err == nil {
				t.Fatal("unproven old liability accepted")
			}
		})
	}
}
