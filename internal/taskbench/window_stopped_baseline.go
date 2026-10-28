package taskbench

import "fmt"

// This pure check will be used by the separately versioned stopped-bundle
// loader. It does not settle a ledger, create a claim or authorize execution.
func verifyWindowStoppedLiability(a windowAccountingAudit, p windowPayloadPacket, body []byte) (int64, error) {
	reject := func() (int64, error) { return 0, fmt.Errorf("stopped response liability not proven") }
	c, s := a.Contract, a.State
	if c.validate() != nil || c.Model.Model != "deepseek-flash" || c.Model.InputRate != 2000000 || c.Model.OutputRate != 8000000 || c.Model.MaxInput != 36864 || c.Model.MaxOutput != 128 || c.ModelCap != 414586 || c.JEVCap != 4079 {
		return reject()
	}
	if !s.Closed || s.Halt != "protocol_failure" || s.EnvelopeViolation || s.ModelPermanent != c.ModelCap || s.JEVPermanent != c.JEVCap || s.ModelKnown != 0 || s.JEVKnown != 0 || s.ModelAttempts != 1 || s.JEVAttempts != 0 || s.JEVHeld != 0 || s.Pending == nil || s.Pending.Sequence != 1 {
		return reject()
	}
	if s.ModelHeld != c.Model.cost(c.Model.MaxInput, c.Model.MaxOutput) || s.ModelHeld > s.ModelPermanent || p.Sequence != 1 || p.Identity != s.Pending.Request || p.Identity.Provider != "model" || p.Identity.Model != c.Model.Model || p.ScopeSHA256 != c.ScopeSHA256 || p.ClaimSHA256 != c.ClaimSHA256 || p.WindowID != c.WindowID {
		return reject()
	}
	if !comparisonDigest(p.Identity.BodySHA256) || p.Identity.Bytes < 1 || p.Identity.Bytes > c.Model.MaxRequestBytes || !p.Request.Complete || p.Request.Bytes != p.Identity.Bytes || p.Request.FullSHA256 != p.Identity.BodySHA256 || p.Request.PrefixSHA256 != "" || p.Request.File != "request-000001.body" || p.RequestDescriptorFile != "request-000001.json" {
		return reject()
	}
	r := p.Response
	if r == nil || r.Status != 200 || r.Code != "complete" || !r.Complete || !r.Available || r.Body == nil || !r.Body.Complete || r.Body.Bytes != len(body) || r.Body.FullSHA256 != conversationHash(body) || r.Body.PrefixSHA256 != "" || r.Body.File != "response-000001.body" || r.DescriptorFile != "response-000001.json" {
		return reject()
	}
	obj, err := windowJSONObject(body)
	if err != nil {
		return reject()
	}
	u, ok := obj["usage"].(map[string]any)
	if !ok || u["service_tier"] != "standard" {
		return reject()
	}
	usage, err := parseWindowResponseUsage("model", body, c.Model)
	if err != nil {
		return reject()
	}
	cost := c.Model.cost(*usage.Input, *usage.Output)
	if cost > s.ModelHeld {
		return reject()
	}
	return cost, nil
}
