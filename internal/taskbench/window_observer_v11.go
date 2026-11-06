package taskbench

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/frostloom/ai-gateway/internal/observability"
)

// The only live owner transition is made from the selected genuine Observer.
// Neither JSON nor an initial-root-freshness boolean can stand in for this object.
type windowV11ObservationOwner struct {
	context  context.Context
	observer *observability.Observer
	identity observability.EvaluationIdentity
	root     string
}
type windowObservationReceiptV11 struct {
	Boundary       string                           `json:"observation_receipt_boundary"`
	RemoteDelivery string                           `json:"remote_delivery"`
	Spool          observability.DurableSpoolStatus `json:"spool"`
	NormalClosure  bool                             `json:"normal_closure"`
}

func validateWindowV11SpoolBinding(m windowActualManifest) error {
	id := m.Observer
	if m.Schema == "full-window-actual-ready.v15" && (id.SpoolRunID == "878871f56b992d123b3e1d8e0eee4613bcb1b1f7a1c55e85a50829b40f73241f" || m.PrivateSettingsSHA256 == "4e78740578d56091f092e4fb85ce7824514760f66450496c998549380da64efe") {
		return fmt.Errorf("v15 consumed V14 observation identity")
	}
	if (m.Schema == "full-window-actual-ready.v14" || m.Schema == "full-window-actual-ready.v15") && (id.SpoolRunID == "dd3af173632b3d28051540c33e2437affb0c3c65ffab342bde35ca143c21c29b" || m.PrivateSettingsSHA256 == "33e8b67d9d7b1aa1550972f1a2df33b16e105cd426c7b86151edea61ee6385ed") {
		return fmt.Errorf("v14 consumed V13 observation identity")
	}

	if (m.Schema == "full-window-actual-ready.v13" || (m.Schema == "full-window-actual-ready.v14" || m.Schema == "full-window-actual-ready.v15")) && (id.SpoolRunID == "f419a799e9915b8f0249a8f09e8834aeb209d3336cd811ddfb2791445550b308" || m.PrivateSettingsSHA256 == "25cdb8728d5a32b3402685f01d79687eac4a29a7859749aab3c22eea329a7811") {
		return fmt.Errorf("v13 consumed observation identity")
	}
	if (m.Schema == "full-window-actual-ready.v12" || (m.Schema == "full-window-actual-ready.v13" || (m.Schema == "full-window-actual-ready.v14" || m.Schema == "full-window-actual-ready.v15"))) && (id.SpoolRunID == "c7d34cb23ca0507a18e9892b5f90ae3f0580a6772289b7276c4bf46db83a9ac4" || m.PrivateSettingsSHA256 == "8c4f09c6ba7a04fa1812fe1b45e623ff9be73a1905380dab6032ffa4684e5394" || windowV8DeclarationSHA(id) == "6d6684c766677fba1cd6bb9f1ac352bc282c1aac48f558383e1c1f193e7ced87") {
		return fmt.Errorf("v12 consumed observation identity")
	}

	if m.Schema == "full-window-actual-ready.v15" && m.Scope.Schema != "full-window-scope.v15" {
		return fmt.Errorf("durable epoch mismatch")
	}
	if m.Schema == "full-window-actual-ready.v14" && m.Scope.Schema != "full-window-scope.v14" || m.Schema == "full-window-actual-ready.v13" && m.Scope.Schema != "full-window-scope.v13" || m.Schema == "full-window-actual-ready.v12" && m.Scope.Schema != "full-window-scope.v12" || m.Schema == "full-window-actual-ready.v11" && m.Scope.Schema != "full-window-scope.v11" {
		return fmt.Errorf("durable epoch mismatch")
	}
	if m.Schema != "full-window-actual-ready.v11" && m.Schema != "full-window-actual-ready.v12" && m.Schema != "full-window-actual-ready.v13" && m.Schema != "full-window-actual-ready.v14" && m.Schema != "full-window-actual-ready.v15" {
		if m.Scope.Observation != nil || m.SpoolRoot != "" || id.ObservationReceiptBoundary != "" || id.SpoolRootSHA256 != "" || id.SpoolRunID != "" || id.SpoolMaxBytes != 0 {
			return fmt.Errorf("old epoch cannot carry durable identity")
		}
		return nil
	}
	d := m.Scope.Observation
	if (m.Scope.Schema != "full-window-scope.v11" && m.Scope.Schema != "full-window-scope.v12" && m.Scope.Schema != "full-window-scope.v13" && m.Scope.Schema != "full-window-scope.v14" && m.Scope.Schema != "full-window-scope.v15") || d.validate() != nil || d.PrivateSettingsSHA256 != m.PrivateSettingsSHA256 || d.SpoolRoot != m.SpoolRoot || d.SpoolRunID != id.SpoolRunID || d.SpoolMaxBytes != id.SpoolMaxBytes || d.Boundary != id.ObservationReceiptBoundary || d.ObserverIdentitySHA256 != windowV8DeclarationSHA(id) {
		return fmt.Errorf("v11 scope/manifest observation mismatch")
	}
	if canonicalRootWindowCampaign(m.SpoolRoot) != nil || id.ObservationReceiptBoundary != "durable_local" || id.SpoolRootSHA256 != conversationHash([]byte(m.SpoolRoot)) || !comparisonDigest(id.SpoolRunID) || id.SpoolMaxBytes != 4<<30 || id.SampleRate != 1 {
		return fmt.Errorf("v11 exact durable spool binding required")
	}
	for _, r := range []string{m.SourceRoot, m.SessionRoot, m.CampaignRoot, m.ClaimsDir} {
		if !filepath.IsAbs(r) || filepath.Clean(r) != r || !windowPayloadDisjoint(m.SpoolRoot, r) {
			return fmt.Errorf("v11 disjoint canonical spool required")
		}
	}
	roots := []string{m.SourceRoot, m.SessionRoot, m.CampaignRoot, m.ClaimsDir}
	for i, r := range roots {
		for _, previous := range roots[:i] {
			if !windowPayloadDisjoint(r, previous) {
				return fmt.Errorf("v11 source/session/campaign/history overlap")
			}
		}
	}
	return nil
}
func checkWindowV11FreshSpool(m windowActualManifest) error {
	if e := validateWindowV11SpoolBinding(m); e != nil {
		return e
	}
	if m.Schema != "full-window-actual-ready.v11" && m.Schema != "full-window-actual-ready.v12" && m.Schema != "full-window-actual-ready.v13" && m.Schema != "full-window-actual-ready.v14" && m.Schema != "full-window-actual-ready.v15" {
		return nil
	}
	if _, e := os.Lstat(m.SpoolRoot); !os.IsNotExist(e) {
		return fmt.Errorf("v11 initial spool must be absent")
	}
	return nil
}
func checkWindowV11SettingsSpool(s windowActualSettings, m windowActualManifest) error {
	if e := validateWindowV11SpoolBinding(m); e != nil {
		return e
	}
	d := s.Observer.Durable
	if d == nil || d.Root != m.SpoolRoot || d.RunID != m.Observer.SpoolRunID || d.MaxBytes != m.Observer.SpoolMaxBytes {
		return fmt.Errorf("v11 settings spool mismatch")
	}
	return checkWindowActualSettingsDigest(s, m.PrivateSettingsSHA256)
}
func selectWindowV11ObservationOwner(ctx context.Context, m windowActualManifest, o *observability.Observer) (*windowV11ObservationOwner, error) {
	if e := validateWindowV11SpoolBinding(m); e != nil {
		return nil, e
	}
	if m.Schema != "full-window-actual-ready.v11" && m.Schema != "full-window-actual-ready.v12" && m.Schema != "full-window-actual-ready.v13" && m.Schema != "full-window-actual-ready.v14" && m.Schema != "full-window-actual-ready.v15" || o == nil || o.EvaluationIdentity() != m.Observer {
		return nil, fmt.Errorf("v11 selected observer mismatch")
	}
	if e := checkWindowObservationReceipt(ctx, m.Scope.Schema, o, m.Observer); e != nil {
		return nil, e
	}
	return &windowV11ObservationOwner{observer: o, identity: m.Observer, root: m.SpoolRoot, context: ctx}, nil
}
func (o *windowV11ObservationOwner) healthy(ctx context.Context, m windowActualManifest) error {
	if ctx == nil || ctx.Err() != nil || o == nil || o.context == nil || o.context.Err() != nil || o.observer == nil || o.root != m.SpoolRoot || o.identity != m.Observer || o.observer.EvaluationIdentity() != o.identity {
		return fmt.Errorf("v11 owned observer changed")
	}
	st, e := o.observer.DurableStatus()
	if e != nil || st.Closed || st.Poisoned || st.DataOnly || st.RootSHA256 != o.identity.SpoolRootSHA256 || st.RunID != o.identity.SpoolRunID || st.MaxBytes != 4<<30 {
		return fmt.Errorf("v11 owned spool unhealthy")
	}
	return nil
}
func checkWindowObservationReceipt(ctx context.Context, schema string, o *observability.Observer, id observability.EvaluationIdentity) error {
	if ctx == nil || ctx.Err() != nil || o == nil || o.EvaluationIdentity() != id {
		return fmt.Errorf("observation identity/context changed")
	}
	st := o.Stats()
	if st.Failed != 0 || st.Dropped != 0 || st.QueueDepth != 0 {
		return fmt.Errorf("observation counters unhealthy")
	}
	if schema == "full-window-scope.v11" || schema == "full-window-scope.v12" || (schema == "full-window-scope.v13" || (schema == "full-window-scope.v14" || schema == "full-window-scope.v15")) {
		if !id.Enabled || !id.CaptureContent || id.SampleRate != 1 || id.Environment != "evaluation" || id.ObservationReceiptBoundary != "durable_local" || !comparisonDigest(id.SpoolRootSHA256) || !comparisonDigest(id.SpoolRunID) || id.SpoolMaxBytes != 4<<30 || st.Accepted != st.Persisted || st.Exported != 0 {
			return fmt.Errorf("v11 local receipt boundary required")
		}
		return o.CheckDurableReceipt(ctx)
	}
	if id.ObservationReceiptBoundary != "" || id.SpoolRootSHA256 != "" || id.SpoolRunID != "" || id.SpoolMaxBytes != 0 || st.Persisted != 0 || st.Accepted != st.Exported {
		return fmt.Errorf("direct observation receipt required")
	}
	return nil
}
func checkWindowArmObservationReceipt(ctx context.Context, schema string, o *observability.Observer, id observability.EvaluationIdentity, start observability.Stats, turns int) error {
	if e := checkWindowObservationReceipt(ctx, schema, o, id); e != nil {
		return e
	}
	st := o.Stats()
	if turns < 0 || st.Turns < start.Turns || st.SampledTurns < start.SampledTurns || st.Turns-start.Turns != uint64(turns) || st.SampledTurns-start.SampledTurns != uint64(turns) {
		return fmt.Errorf("exact arm turn sampling required")
	}
	return nil
}
func windowV11Receipt(o *observability.Observer, normal bool) (*windowObservationReceiptV11, error) {
	st, e := o.DurableStatus()
	// Failure status is retained as diagnostic DATA, never a healthy receipt.
	return &windowObservationReceiptV11{Boundary: "durable_local", RemoteDelivery: "pending", Spool: st, NormalClosure: e == nil && normal && st.Closed && !st.Poisoned && !st.DataOnly}, e
}
