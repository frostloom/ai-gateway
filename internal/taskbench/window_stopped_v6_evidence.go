package taskbench

import (
	"fmt"
	"path/filepath"
	"reflect"
)

func readWindowStoppedV6Files(root string) (map[string][]byte, error) {
	files := map[string][]byte{}
	for name, sha := range windowStoppedV6ArtifactPins() {
		raw, err := readWindowActualFile(filepath.Join(root, filepath.FromSlash(name)), sha, 8<<20)
		if err != nil {
			return nil, fmt.Errorf("stopped v6 original top artifact changed: %s", name)
		}
		files[name] = raw
	}
	return files, nil
}

// Reuses strict nullable-record and native body/descriptor/store readers. The
// original v6 ledger is schema2; ancestors retain their original accounting.
// Independently add parsed native weighted input/output or JEV input costs to
// reconcile canonical replay. This is configured liability, not an invoice.
func readWindowStoppedV6Native(dir string, payload windowPayloadManifest, ledger []byte) (windowAccountingAudit, error) {
	reject := func() (windowAccountingAudit, error) {
		return windowAccountingAudit{}, fmt.Errorf("stopped v6 weighted native linkage rejected")
	}
	audit, err := readWindowStoppedV3Native(dir, payload, ledger)
	if err != nil {
		return windowAccountingAudit{}, err
	}
	limits := func(p windowProviderLimits) windowCampaignProviderBounds {
		return windowCampaignProviderBounds{Model: p.Model, MaxRequestBytes: p.MaxRequestBytes, MaxInput: p.MaxInput, MaxOutput: p.MaxOutput, InputRate: p.InputRate, OutputRate: p.OutputRate, CacheHitInputRate: p.CacheHitInputRate}
	}
	if audit.Contract.Schema != "full-window-accounting.v2" || payload.EvidenceKind != "recorded_run" || audit.State.Halt != "invalid_request" || !reflect.DeepEqual(windowCampaignLimits{Model: limits(audit.Contract.Model), JEV: limits(audit.Contract.JEV)}, windowV4ExpectedLimits()) {
		return reject()
	}
	var modelKnown, jevKnown int64
	models, jevs := 0, 0
	attempts := map[string]int{}
	for _, packet := range payload.Packets {
		identity := packet.Identity
		limit := audit.Contract.Model
		max := 5
		if identity.Provider == "jev" {
			limit = audit.Contract.JEV
			max = 1
			if identity.Arm != "jev_selective" {
				return reject()
			}
		} else if identity.Provider != "model" {
			return reject()
		}
		key := fmt.Sprintf("%s/%s/%d/%s/%d", identity.Provider, identity.CaseKey, identity.Pass, identity.Arm, identity.Turn)
		attempts[key]++
		if identity.Attempt != attempts[key] || identity.Attempt > max {
			return reject()
		}
		raw, err := readWindowActualFile(filepath.Join(dir, packet.Response.Body.File), packet.Response.Body.FullSHA256, windowMaxResponseBytes)
		if err != nil {
			return reject()
		}
		usage, err := parseWindowResponseUsage(identity.Provider, raw, limit)
		if err != nil {
			return reject()
		}
		if identity.Provider == "model" {
			if usage.CacheHit == nil || usage.CacheMiss == nil {
				return reject()
			}
			modelKnown += (*usage.CacheMiss*2000000+*usage.CacheHit*40000+999999)/1000000 + (*usage.Output*8000000+999999)/1000000
			models++
		} else {
			jevKnown += (*usage.Input*42000 + 999999) / 1000000
			jevs++
		}
	}
	if modelKnown != audit.State.ModelKnown || jevKnown != audit.State.JEVKnown || models != audit.State.ModelAttempts || jevs != audit.State.JEVAttempts {
		return reject()
	}
	return audit, nil
}
