package taskbench

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"time"

	"github.com/frostloom/ai-gateway/internal/observability"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// No production issuer exists for this private acquisition authorization. A
// future verified entry must independently freeze all identities and retain the
// same live campaign owner across bounded operations; JSON cannot mint it.
type windowAcquisitionAuthorization struct {
	captureFailureEvidence                                 bool
	campaign                                               *windowCampaign
	source                                                 ConversationSuite
	frozenScopeSHA, frozenExecutionSHA, actualExecutionSHA string
	observer                                               *observability.Observer
	observerIdentity                                       observability.EvaluationIdentity
	modelKey, jevKey                                       string
	modelUpstream, jevUpstream                             http.RoundTripper
	fixtureOptions                                         PairedFixtureOptions
	privateReportRoot, evidenceKind                        string
}
type windowAcquisitionRow struct {
	FailureEvidence               *windowFailureEvidence    `json:"failure_evidence,omitempty"`
	QualityEnd                    *windowQualityEndEvidence `json:"quality_end,omitempty"`
	ExcludeCompleteTaskComparison bool                      `json:"exclude_complete_task_comparison,omitempty"`
	Ordinal                       int                       `json:"ordinal"`
	Pass                          int                       `json:"pass"`
	DefinedTurns                  int                       `json:"defined_turns"`
	ObservedTurns                 int                       `json:"observed_turns"`
	CaseKey                       string                    `json:"case_key"`
	Arm                           string                    `json:"arm"`
	TraceID                       string                    `json:"trace_id"`
	RecordingSHA256               string                    `json:"recording_sha256"`
	RecordingFile                 string                    `json:"recording_file"`
	Status                        string                    `json:"status"`
	Disposition                   string                    `json:"disposition"`
	Attempted                     bool                      `json:"attempted"`
	Finished                      bool                      `json:"finished"`
	Cleanup                       bool                      `json:"cleanup"`
	Observer                      bool                      `json:"observer"`
	StructuralPass                bool                      `json:"structural_pass"`
	Success                       bool                      `json:"success"`
	DurationMS                    float64                   `json:"duration_ms"`
}
type windowAcquisitionReceipt struct {
	Completion                 *windowCompletionDeclarationV10 `json:"completion,omitempty"`
	V8                         *windowV8RuntimeSummary         `json:"v8,omitempty"`
	QualityEndArms             int                             `json:"quality_end_arms,omitempty"`
	QualityEndUnrunTurns       int                             `json:"quality_end_unrun_turns,omitempty"`
	GlobalUnrunTurns           int                             `json:"global_unrun_turns,omitempty"`
	Schema                     string                          `json:"schema"`
	EvidenceKind               string                          `json:"evidence_kind"`
	Status                     string                          `json:"status"`
	ScopeSHA256                string                          `json:"scope_sha256"`
	ExecutionSHA256            string                          `json:"execution_sha256"`
	WindowID                   string                          `json:"window_id"`
	WindowIndex                int                             `json:"window_index"`
	PlannedCampaignPairs       int                             `json:"planned_campaign_pairs"`
	PlannedCampaignArms        int                             `json:"planned_campaign_arms"`
	PlannedCampaignTurns       int                             `json:"planned_campaign_turns"`
	PlannedCurrentPairs        int                             `json:"planned_current_pairs"`
	PlannedCurrentArms         int                             `json:"planned_current_arms"`
	PlannedCurrentTurns        int                             `json:"planned_current_turns"`
	AttemptedArms              int                             `json:"attempted_arms"`
	FinishedArms               int                             `json:"finished_arms"`
	UnrunArms                  int                             `json:"unrun_arms"`
	ObservedTurns              int                             `json:"observed_turns"`
	QualityNegativeArms        int                             `json:"quality_negative_arms"`
	Rows                       []windowAcquisitionRow          `json:"rows"`
	FirstFailureStage          string                          `json:"first_failure_stage"`
	FirstFailureOrdinal        int                             `json:"first_failure_ordinal"`
	FirstFailureArm            string                          `json:"first_failure_arm"`
	Cleanup                    bool                            `json:"cleanup"`
	EnvironmentCleanup         bool                            `json:"environment_cleanup"`
	Observer                   bool                            `json:"observer"`
	Advanced                   bool                            `json:"advanced"`
	NextClaimSHA256            string                          `json:"next_claim_sha256"`
	LedgerSHA256               string                          `json:"ledger_sha256"`
	LedgerCompletionProof      string                          `json:"ledger_completion_proof"`
	Accounting                 windowAccountingState           `json:"accounting"`
	AccountingSource           string                          `json:"accounting_source"`
	ObserverCountersCumulative observability.Stats             `json:"observer_counters_cumulative"`
	OperationLimitSeconds      int                             `json:"operation_limit_seconds"`
	OperationDeadline          time.Time                       `json:"operation_deadline"`
	PayloadManifestSHA256      string                          `json:"payload_manifest_sha256"`
	PayloadAuditOnlyCompletion string                          `json:"payload_audit_only_completion"`
	PayloadPackets             int                             `json:"payload_packets"`
	PayloadVerified            bool                            `json:"payload_verified"`
	PayloadStatus              string                          `json:"payload_status"`
	DurationMS                 float64                         `json:"duration_ms"`
}
type windowAcquisitionRun struct {
	payload     *windowPayloadStore
	auth        windowAcquisitionAuthorization
	scope       fullWindowScope
	window      fullWindowDeclaration
	receipt     windowAcquisitionReceipt
	ledger      *windowAccounting
	ledgerDir   string
	environment *PairedFixtureEnvironment
	permit      *windowAcquisitionPermit
	rootInfo    os.FileInfo
	evidence    []windowCampaignArmEvidence
	recordFiles map[string]string
	started     time.Time
}

// This operates on exactly the owner's CURRENT window. It does not create an
// authorization, select caps, reopen an owner, loop across windows, or retry.
func runWindowAcquisition(ctx context.Context, auth *windowAcquisitionAuthorization) (receipt windowAcquisitionReceipt, runErr error) {
	if ctx == nil {
		return receipt, fmt.Errorf("window acquisition authorization rejected")
	}
	started := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 300*time.Second)
	defer cancel()
	run, err := openWindowAcquisition(ctx, auth)
	if run == nil {
		return receipt, err
	}
	runErr = err
	run.started = started
	defer func() {
		if runErr != nil {
			run.receipt.Status = "stopped"
			run.stopWindowAcquisition()
			if run.environment != nil && !run.environment.closed {
				run.receipt.EnvironmentCleanup = run.environment.Close() == nil
			}
			if run.ledger != nil {
				_ = run.ledger.Close()
			}
		}
		if run.scope.Bindings.PolicySHA256 == windowOwnedQualityPolicySHA() {
			run.receipt.GlobalUnrunTurns = run.receipt.PlannedCurrentTurns - run.receipt.ObservedTurns - run.receipt.QualityEndUnrunTurns
		}
		run.receipt.DurationMS = float64(time.Since(run.started).Microseconds()) / 1000
		run.receipt.ObserverCountersCumulative = run.auth.observer.Stats()
		run.captureWindowAcquisitionAccounting()
		if runErr != nil || !run.receipt.PayloadVerified {
			_ = run.captureWindowAcquisitionPayload(false)
		}
		raw, e := json.Marshal(run.receipt)
		if e == nil {
			e = run.persistWindowAcquisition("receipt.json", raw)
		}
		if e != nil {
			run.failWindowAcquisition("receipt_write", 0, "")
			run.stopWindowAcquisition()
			runErr = fmt.Errorf("window acquisition stopped: receipt_write")
		}
		if run.payload != nil {
			if e := run.payload.Close(); e != nil {
				run.failWindowAcquisition("payload_close", 0, "")
				run.stopWindowAcquisition()
				runErr = fmt.Errorf("window acquisition stopped: payload_close")
			}
		}
		receipt = run.receipt
	}()
	if err != nil {
		return run.receipt, err
	}
	if err = run.runWindowAcquisitionArms(ctx); err != nil {
		return run.receipt, err
	}
	if err = run.completeWindowAcquisition(ctx); err != nil {
		return run.receipt, err
	}
	return run.receipt, nil
}

func openWindowAcquisition(ctx context.Context, auth *windowAcquisitionAuthorization) (*windowAcquisitionRun, error) {
	reject := func() (*windowAcquisitionRun, error) {
		return nil, fmt.Errorf("window acquisition authorization rejected")
	}
	if ctx == nil || ctx.Err() != nil || auth == nil || auth.campaign == nil || auth.observer == nil || strings.TrimSpace(auth.modelKey) == "" || strings.TrimSpace(auth.jevKey) == "" || windowTransportNil(auth.modelUpstream) || windowTransportNil(auth.jevUpstream) || !hasString([]string{"local_stub", "recorded_run"}, auth.evidenceKind) || !auth.fixtureOptions.AllowFixtureCreation {
		return reject()
	}
	if err := auth.campaign.revalidateWindowV8Authority(); err != nil {
		return nil, err
	}
	identity := auth.observer.EvaluationIdentity()
	if identity != auth.observerIdentity || !identity.Enabled || !identity.CaptureContent || identity.SampleRate != 1 || identity.Environment != "evaluation" || identity.Release == "" || identity.Release == "unknown" || identity.ExportTimeoutMS != windowObserverTimeoutForScope(auth.campaign.prepared.scope.Schema) || identity.QueueSize < 1 || !comparisonDigest(identity.EndpointSHA256) || !comparisonDigest(identity.ProjectSHA256) {
		return reject()
	}
	if canonicalRootWindowCampaign(auth.privateReportRoot) != nil {
		return reject()
	}
	if _, e := os.Lstat(auth.privateReportRoot); !os.IsNotExist(e) {
		return reject()
	}
	dsn, e := mysqlDriver.ParseDSN(auth.fixtureOptions.AdminDSN)
	if e != nil || dsn.DBName != "" || dsn.Net != "tcp" {
		return reject()
	}
	host, _, e := net.SplitHostPort(dsn.Addr)
	if e != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return reject()
	}
	profiles, e := LoadPairedProfiles(auth.fixtureOptions.ProfilesDir)
	if e != nil {
		return reject()
	}
	source, e := cloneWindowOwned(auth.source)
	if e != nil {
		return reject()
	}
	c := auth.campaign
	c.mu.Lock()
	if c.closed || c.poisoned || c.ledger != nil || c.index < 1 || c.index > len(c.prepared.scope.Windows) || c.inventoryWindowCampaign() != nil {
		c.mu.Unlock()
		return reject()
	}
	scope, e := cloneWindowOwned(c.prepared.scope)
	root, scopeSHA, executionSHA, index, limits := c.root, c.prepared.scopeSHA, c.prepared.executionSHA, c.index, c.prepared.limits
	c.mu.Unlock()
	if e != nil || (scope.Bindings.PolicySHA256 != collectionPolicySHA() && scope.Bindings.PolicySHA256 != windowOwnedQualityPolicySHA()) || identity.Release != scope.Bindings.ExecutorRevision || strings.HasPrefix(auth.privateReportRoot, root+string(os.PathSeparator)) || strings.HasPrefix(root, auth.privateReportRoot+string(os.PathSeparator)) || root == auth.privateReportRoot {
		return reject()
	}
	actual, e := executionSHAWindowCampaign(scopeSHA, root, limits)
	if e != nil || !comparisonDigest(auth.frozenExecutionSHA) || auth.frozenExecutionSHA != executionSHA || auth.actualExecutionSHA != actual || actual != executionSHA {
		return reject()
	}
	digest, totals, e := validateFullWindowScope(scope, source, profiles, scope.Bindings)
	if e != nil || !comparisonDigest(auth.frozenScopeSHA) || auth.frozenScopeSHA != scopeSHA || digest != scopeSHA {
		return reject()
	}
	// All authorization/source/observer/root checks precede the first new output,
	// ledger, isolated schema, listener, or provider side effect.
	if ctx.Err() != nil {
		return reject()
	}
	run := &windowAcquisitionRun{auth: *auth, scope: scope, window: scope.Windows[index-1], recordFiles: map[string]string{}, started: time.Now()}
	run.auth.source = source
	run.receipt = windowAcquisitionReceipt{Schema: "full-window-acquisition-receipt.v1", EvidenceKind: auth.evidenceKind, Status: "stopped", ScopeSHA256: scopeSHA, ExecutionSHA256: executionSHA, WindowID: run.window.ID, WindowIndex: index, PlannedCampaignPairs: totals.Pairs, PlannedCampaignArms: totals.Arms, PlannedCampaignTurns: totals.DefinedUserTurns, PlannedCurrentPairs: run.window.LastOrdinal - run.window.FirstOrdinal + 1, OperationLimitSeconds: 300}
	if windowScopeUsesNativeV8(scope.Schema) {
		run.receipt.Schema = "full-window-acquisition-receipt.v8"
	}
	if scope.Schema == "full-window-scope.v10" {
		run.receipt.Completion = scope.Completion
		run.receipt.Schema = "full-window-acquisition-receipt.v10"
	}
	run.receipt.OperationDeadline, _ = ctx.Deadline()
	for _, slot := range scope.Slots[run.window.FirstOrdinal-1 : run.window.LastOrdinal] {
		for _, arm := range slot.ArmOrder {
			run.receipt.Rows = append(run.receipt.Rows, windowAcquisitionRow{Ordinal: slot.Ordinal, Pass: slot.Pass, CaseKey: slot.CaseKey, Arm: arm, DefinedTurns: slot.UserTurns, Status: "not_started"})
			run.receipt.PlannedCurrentTurns += slot.UserTurns
		}
	}
	run.receipt.PlannedCurrentArms = len(run.receipt.Rows)
	run.receipt.UnrunArms = len(run.receipt.Rows)
	if e = run.openWindowAcquisitionReportRoot(); e != nil {
		return run, e
	}
	run.ledger, e = c.openWindowCampaignLedgerContext(ctx)
	if e != nil {
		run.failWindowAcquisition("ledger_open", 0, "")
		run.stopWindowAcquisition()
		return run, fmt.Errorf("window acquisition stopped: ledger_open")
	}
	c.mu.Lock()
	run.ledgerDir = c.ledgerDir
	c.mu.Unlock()
	run.payload, e = newWindowPayloadStore(filepath.Join(auth.privateReportRoot, "native-payloads"), c.root, run.ledger.contract, auth.evidenceKind)
	if e != nil {
		run.failWindowAcquisition("payload_open", 0, "")
		run.stopWindowAcquisition()
		return run, fmt.Errorf("window acquisition stopped: payload_open")
	}
	env, e := OpenPairedFixtureEnvironment(ctx, auth.fixtureOptions)
	if e != nil {
		run.failWindowAcquisition("environment_open", 0, "")
		run.stopWindowAcquisition()
		return run, fmt.Errorf("window acquisition stopped: environment_open")
	}
	run.environment = env
	run.permit = &windowAcquisitionPermit{captureFailureEvidence: auth.captureFailureEvidence, acquisition: run, campaign: c, ledger: run.ledger, executionSHA: executionSHA, windowIndex: index, environment: env, observer: auth.observer, observerIdentity: identity, payload: run.payload, evidenceKind: auth.evidenceKind}
	return run, nil
}

func (r *windowAcquisitionRun) failWindowAcquisition(stage string, ordinal int, arm string) error {
	if r.receipt.FirstFailureStage == "" {
		r.receipt.FirstFailureStage = stage
		r.receipt.FirstFailureOrdinal = ordinal
		r.receipt.FirstFailureArm = arm
	}
	r.receipt.Status = "stopped"
	r.receipt.Cleanup = false
	return fmt.Errorf("window acquisition stopped: %s", stage)
}
func (r *windowAcquisitionRun) stopWindowAcquisition() {
	if r.permit != nil {
		r.permit.mu.Lock()
		r.permit.stopWindowOwnedLocked()
		r.permit.mu.Unlock()
	}
	c := r.auth.campaign
	c.mu.Lock()
	c.poisoned = true
	ownedLedger := c.ledger
	c.mu.Unlock()
	if ownedLedger != nil {
		_ = windowTransportStop(ownedLedger, "protocol_failure")
	}
}

func (r *windowAcquisitionRun) persistWindowAcquisition(name string, raw []byte) error {
	info, e := os.Lstat(r.auth.privateReportRoot)
	if e != nil || r.rootInfo == nil || !os.SameFile(info, r.rootInfo) || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("private report root changed")
	}
	f, e := os.OpenFile(filepath.Join(r.auth.privateReportRoot, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0600)
	if e != nil {
		return e
	}
	n, w := f.Write(raw)
	if w == nil && n != len(raw) {
		w = fmt.Errorf("short private record write")
	}
	return errors.Join(w, f.Sync(), f.Close(), windowSyncDirectory(r.auth.privateReportRoot), windowSyncDirectory(filepath.Dir(r.auth.privateReportRoot)))
}
func (r *windowAcquisitionRun) runWindowAcquisitionArms(ctx context.Context) error {
	primary, _, e := fullCustomerSelection(r.auth.source)
	if e != nil {
		return r.failWindowAcquisition("source", 0, "")
	}
	cases := map[string]ConversationCase{}
	for _, c := range primary.Cases {
		cases[c.Key()] = c
	}
	rowIndex := 0
	for _, slot := range r.scope.Slots[r.window.FirstOrdinal-1 : r.window.LastOrdinal] {
		for _, arm := range slot.ArmOrder {
			row := &r.receipt.Rows[rowIndex]
			rowIndex++
			if ctx.Err() != nil {
				return r.failWindowAcquisition("context", slot.Ordinal, arm)
			}
			row.Attempted = true
			r.receipt.AttemptedArms++
			r.receipt.UnrunArms--
			driver, e := newWindowOwnedDriver(r.permit, r.auth.source, slot, arm, r.environment, r.auth.observer, r.auth.modelKey, r.auth.jevKey, r.auth.modelUpstream, r.auth.jevUpstream)
			if e != nil {
				return r.failWindowAcquisition("driver", slot.Ordinal, arm)
			}
			started := time.Now()
			pairID := fmt.Sprintf("%s/%s/ordinal-%06d/pass-%d", r.receipt.ScopeSHA256, slot.WindowID, slot.Ordinal, slot.Pass)
			armCtx, task := r.auth.observer.StartEvaluation(ctx, observability.Evaluation{RunID: r.receipt.ScopeSHA256, CaseKey: slot.CaseKey, Arm: arm, EvidenceKind: r.auth.evidenceKind, PairID: pairID, BlockID: slot.WindowID, ArmOrder: slot.ArmOrder[:]})
			trace.SpanFromContext(armCtx).SetAttributes(attribute.String("langfuse.trace.metadata.scope_sha256", r.receipt.ScopeSHA256), attribute.String("langfuse.trace.metadata.execution_sha256", r.receipt.ExecutionSHA256), attribute.String("langfuse.trace.metadata.window_id", slot.WindowID), attribute.Int("langfuse.trace.metadata.ordinal", slot.Ordinal), attribute.Int("langfuse.trace.metadata.pass", slot.Pass))
			row.TraceID = task.TraceID()
			record := recordCollectionConversation(armCtx, cases[slot.CaseKey], driver)
			task.End(observability.EvaluationResult{Status: record.Result.Status, StructuralPass: record.Result.StructuralPass, NecessaryClarifications: record.Result.NecessaryClarifications, HTTPFailures: record.Result.HTTPFailures})
			row.DurationMS = float64(time.Since(started).Microseconds()) / 1000
			row.Status = record.Result.Status
			row.Disposition = record.Disposition
			row.Success = record.Result.Success
			row.StructuralPass = record.Result.StructuralPass
			row.ObservedTurns = len(record.Result.Observations)
			r.receipt.ObservedTurns += row.ObservedTurns
			if record.Disposition == "continue_recorded" && record.Result.Status == "failed" {
				r.receipt.QualityNegativeArms++
			}
			proof, finishErr := driver.finishWindowOwnedArm(ctx, record)
			if finishErr != nil && r.auth.captureFailureEvidence {
				driver.mu.Lock()
				row.FailureEvidence = driver.captureFailureEvidenceLocked()
				driver.mu.Unlock()
			}
			exact := record
			if finishErr == nil && proof != nil {
				exact = proof.recording
			}
			raw, marshalErr := json.Marshal(exact)
			if marshalErr != nil {
				return r.failWindowAcquisition("record_encoding", slot.Ordinal, arm)
			}
			row.RecordingFile = fmt.Sprintf("arm-%06d-%s.json", slot.Ordinal, arm)
			row.RecordingSHA256 = conversationHash(raw)
			if e = r.persistWindowAcquisition(row.RecordingFile, raw); e != nil {
				return r.failWindowAcquisition("record_write", slot.Ordinal, arm)
			}
			r.recordFiles[row.RecordingFile] = row.RecordingSHA256
			var quality *windowQualityEndEvidence
			var qualityErr error
			if finishErr == nil && proof != nil {
				quality, qualityErr = windowQualityProofEvidence(proof)
			}
			if finishErr != nil || proof == nil || qualityErr != nil || (record.Disposition != "continue_recorded" && quality == nil) || validateWindowOwnedArmProof(r.permit, proof) != nil || proof.recordingSHA != row.RecordingSHA256 || len(row.TraceID) != 32 {
				return r.failWindowAcquisition("arm_proof", slot.Ordinal, arm)
			}
			row.QualityEnd = quality
			if quality != nil {
				if validateWindowQualityEndEvidence(r.scope.Bindings.PolicySHA256, slot, arm, row.ObservedTurns, quality) != nil {
					return r.failWindowAcquisition("quality_evidence", slot.Ordinal, arm)
				}
				r.receipt.QualityEndArms++
				r.receipt.QualityEndUnrunTurns += quality.UnrunTurns
				r.receipt.QualityNegativeArms++
				for i := range r.receipt.Rows {
					if r.receipt.Rows[i].Ordinal == slot.Ordinal {
						r.receipt.Rows[i].ExcludeCompleteTaskComparison = true
					}
				}
			}
			row.Finished = true
			row.Cleanup = true
			row.Observer = true
			r.receipt.FinishedArms++
			r.evidence = append(r.evidence, windowCampaignArmEvidence{Ordinal: slot.Ordinal, Pass: slot.Pass, ObservedTurns: row.ObservedTurns, Key: slot.CaseKey, Arm: arm, Cleanup: true, Observer: true, RecordingSHA256: proof.recordingSHA, QualityEnd: quality})
		}
	}
	return nil
}
func (r *windowAcquisitionRun) completeWindowAcquisition(ctx context.Context) error {
	if ctx == nil || ctx.Err() != nil || r.permit == nil || r.environment == nil || r.receipt.FinishedArms != r.receipt.PlannedCurrentArms || len(r.evidence) != len(r.receipt.Rows) {
		return r.failWindowAcquisition("window_proofs", 0, "")
	}
	for i, proof := range r.permit.proofs {
		if i >= len(r.receipt.Rows) || i >= len(r.evidence) {
			return r.failWindowAcquisition("window_proofs", 0, "")
		}
		row, evidence := r.receipt.Rows[i], r.evidence[i]
		if row.RecordingSHA256 != proof.recordingSHA || evidence.RecordingSHA256 != proof.recordingSHA || row.Ordinal != proof.slot.Ordinal || row.Pass != proof.slot.Pass || row.CaseKey != proof.slot.CaseKey || row.Arm != proof.arm || evidence.Ordinal != row.Ordinal || evidence.Pass != row.Pass || evidence.Key != row.CaseKey || evidence.Arm != row.Arm || evidence.ObservedTurns != row.ObservedTurns || !evidence.Cleanup || !evidence.Observer {
			return r.failWindowAcquisition("window_proofs", proof.slot.Ordinal, proof.arm)
		}
		quality, qualityErr := windowQualityProofEvidence(proof)
		if qualityErr != nil || !qualityJSONEqual(quality, row.QualityEnd) || !qualityJSONEqual(row.QualityEnd, evidence.QualityEnd) {
			return r.failWindowAcquisition("quality_proofs", row.Ordinal, row.Arm)
		}
		if validateWindowOwnedArmProof(r.permit, proof) != nil {
			return r.failWindowAcquisition("window_proofs", proof.slot.Ordinal, proof.arm)
		}
	}
	if len(r.permit.proofs) != len(r.receipt.Rows) || r.permit.active != nil || r.permit.nextArm != len(r.receipt.Rows) {
		return r.failWindowAcquisition("window_proofs", 0, "")
	}
	if windowScopeUsesNativeV8(r.scope.Schema) {
		c := r.auth.campaign
		c.mu.Lock()
		err := c.validateWindowV8ExtrasLocked(r.ledger.Snapshot())
		c.mu.Unlock()
		if err != nil {
			return r.failWindowAcquisition("extra_linkage", 0, "")
		}
	}
	if e := r.captureWindowAcquisitionPayload(true); e != nil {
		return r.failWindowAcquisition("payload_linkage", 0, "")
	}
	for name, digest := range r.recordFiles {
		raw, e := os.ReadFile(filepath.Join(r.auth.privateReportRoot, name))
		if e != nil || conversationHash(raw) != digest {
			return r.failWindowAcquisition("record_recheck", 0, "")
		}
	}
	if e := r.closeWindowAcquisitionEnvironment(); e != nil {
		return e
	}
	if r.auth.observer.EvaluationIdentity() != r.auth.observerIdentity || r.auth.observer.ForceFlush(ctx) != nil {
		return r.failWindowAcquisition("observer_flush", 0, "")
	}
	stats := r.auth.observer.Stats()
	if stats.Failed != 0 || stats.Dropped != 0 || stats.QueueDepth != 0 || stats.Accepted != stats.Exported {
		return r.failWindowAcquisition("observer_stats", 0, "")
	}
	if e := r.captureWindowAcquisitionPayload(true); e != nil {
		return r.failWindowAcquisition("payload_linkage", 0, "")
	}
	r.receipt.Observer = true
	r.receipt.Cleanup = true
	checkpoint := &windowCampaignOwnedCheckpoint{owner: r.auth.campaign, ledger: r.ledger, index: r.receipt.WindowIndex, evidence: append([]windowCampaignArmEvidence(nil), r.evidence...)}
	if ctx.Err() != nil {
		return r.failWindowAcquisition("campaign_advance", 0, "")
	}
	next, e := r.auth.campaign.advanceWindowCampaignContext(ctx, checkpoint)
	if e != nil || ctx.Err() != nil {
		return r.failWindowAcquisition("campaign_advance", 0, "")
	}
	r.receipt.Advanced = true
	r.receipt.NextClaimSHA256 = next
	r.receipt.Status = "collected_window"
	return nil
}

func (r *windowAcquisitionRun) closeWindowAcquisitionEnvironment() error {
	if r.environment == nil || r.environment.Close() != nil {
		r.stopWindowAcquisition()
		return r.failWindowAcquisition("environment_cleanup", 0, "")
	}
	r.receipt.EnvironmentCleanup = true
	return nil
}

func (r *windowAcquisitionRun) captureWindowAcquisitionAccounting() {
	r.receipt.AccountingSource = "unavailable_ledger_not_open"
	r.receipt.LedgerCompletionProof = "unavailable_journal_read_failed"
	if r.ledger != nil {
		r.receipt.Accounting = r.ledger.Snapshot()
		if r.auth.campaign != nil {
			r.receipt.V8 = r.auth.campaign.snapshotWindowV8Runtime(r.receipt.Accounting, r.ledger.contract.WindowID)
		}
		r.receipt.AccountingSource = "live_owner_snapshot"
	}
	// Readable bytes can include an event whose fsync failed. They provide an
	// audit digest only; they must never replace a live unknown hold or stop.
	if audit, e := readWindowAccounting(r.ledgerDir); e == nil {
		r.receipt.LedgerSHA256 = audit.JournalSHA256
		r.receipt.LedgerCompletionProof = audit.CompletionProof
	}
}

func (r *windowAcquisitionRun) openWindowAcquisitionReportRoot() error {
	fail := func() error { r.stopWindowAcquisition(); return r.failWindowAcquisition("report_root", 0, "") }
	if e := os.Mkdir(r.auth.privateReportRoot, 0700); e != nil {
		return fail()
	}
	info, e := os.Lstat(r.auth.privateReportRoot)
	if e != nil {
		return fail()
	}
	r.rootInfo = info
	return nil
}

// This verifies capture/ledger metadata only; it cannot mint task or campaign authority.
func validateWindowAcquisitionPayloadLinkage(ledger *windowAccounting, ledgerDir, kind string, manifest windowPayloadManifest) error {
	reject := func() error { return fmt.Errorf("payload/ledger linkage rejected") }
	if ledger == nil || !hasString([]string{"local_stub", "recorded_run"}, kind) {
		return reject()
	}
	ledger.mu.Lock()
	contract, state, events, previous, poisoned := ledger.contract, ledger.state, ledger.events, ledger.previous, ledger.poisoned
	ledger.mu.Unlock()
	if poisoned || state.Halt != "" || state.Pending != nil || state.RetryRequired != nil || state.ModelHeld != 0 || state.JEVHeld != 0 || manifest.Schema != windowPayloadSchema(contract) || manifest.EvidenceKind != kind || manifest.CompletionProof != "unavailable_from_payload_capture" || manifest.Contract != contract {
		return reject()
	}
	audit, e := readWindowAccounting(ledgerDir)
	if e != nil || audit.Contract != contract || audit.Events != events {
		return reject()
	}
	// Unsealed is the reader's audit-only annotation, not the healthy live state.
	replay := audit.State
	if !state.Closed && replay.Halt == "unsealed_window" {
		replay.Halt = ""
	}
	if !reflect.DeepEqual(replay, state) {
		return reject()
	}
	raw, e := os.ReadFile(filepath.Join(ledgerDir, "accounting.jsonl"))
	if e != nil || conversationHash(raw) != audit.JournalSHA256 {
		return reject()
	}
	lines := bytes.Split(raw, []byte{'\n'})
	if len(lines) < 2 || len(lines[len(lines)-1]) != 0 || conversationHash(lines[len(lines)-2]) != previous {
		return reject()
	}
	intents := map[int]windowRequestIdentity{}
	settled := map[int]string{}
	for _, line := range lines[:len(lines)-1] {
		var event windowAccountingEvent
		if json.Unmarshal(line, &event) != nil {
			return reject()
		}
		switch event.Kind {
		case "intent":
			if event.Request == nil {
				return reject()
			}
			intents[event.Sequence] = *event.Request
		case "settle":
			settled[event.Sequence] = event.ResponseSHA256
		}
	}
	total := state.ModelAttempts + state.JEVAttempts
	unknown := map[int]windowUnknownIntent{}
	if contract.Schema == windowAccountingV3 {
		for _, u := range state.UnknownIntents {
			if _, ok := unknown[u.Sequence]; ok {
				return reject()
			}
			unknown[u.Sequence] = u
		}
	}
	if len(manifest.Packets) != total || len(intents) != total || len(settled)+len(unknown) != total {
		return reject()
	}
	for i, packet := range manifest.Packets {
		seq := i + 1
		request, ok := intents[seq]
		if !ok || packet.Sequence != seq || packet.Identity != request || packet.ScopeSHA256 != contract.ScopeSHA256 || packet.ClaimSHA256 != contract.ClaimSHA256 || packet.WindowID != contract.WindowID || !packet.Request.Complete || packet.Request.FullSHA256 != request.BodySHA256 || packet.Request.PrefixSHA256 != "" || packet.Request.Bytes != request.Bytes {
			return reject()
		}
		response := packet.Response
		if u, ok := unknown[seq]; ok {
			if _, duplicate := settled[seq]; duplicate || u.Request != request || request.NativeAttempt != 1 || !comparisonDigest(u.ExtraClaimSHA256) || packet.FailureDescriptorSHA256 != u.DescriptorSHA256 || !windowV8UnknownResponse(response) {
				return reject()
			}
			matches := 0
			for retrySeq, retry := range intents {
				if retry.NativeAttempt == 2 && windowSameLogical(request, retry) && settled[retrySeq] != "" {
					matches++
				}
			}
			if matches != 1 {
				return reject()
			}
			continue
		}
		if request.NativeAttempt == 2 {
			matches := 0
			for _, u := range unknown {
				if windowSameLogical(u.Request, request) {
					matches++
				}
			}
			if matches != 1 {
				return reject()
			}
		}
		if packet.FailureDescriptorSHA256 != "" {
			return reject()
		}
		if response == nil || response.Status != http.StatusOK || response.Code != "complete" || !response.Complete || !response.Available || response.Body == nil || !response.Body.Complete || response.Body.PrefixSHA256 != "" || response.Body.FullSHA256 != settled[seq] || !comparisonDigest(response.Body.FullSHA256) || response.Body.Bytes < 0 || response.Body.Bytes > windowMaxResponseBytes {
			return reject()
		}
	}
	return nil
}

func (r *windowAcquisitionRun) captureWindowAcquisitionPayload(requireComplete bool) error {
	unavailable := func(status string) error {
		r.receipt.PayloadManifestSHA256 = ""
		r.receipt.PayloadAuditOnlyCompletion = "unavailable"
		r.receipt.PayloadPackets = 0
		r.receipt.PayloadVerified = false
		r.receipt.PayloadStatus = status
		return fmt.Errorf("payload evidence unavailable")
	}
	info, rootErr := os.Lstat(r.auth.privateReportRoot)
	if rootErr != nil || r.rootInfo == nil || !info.IsDir() || info.Mode().Perm() != 0700 || !os.SameFile(info, r.rootInfo) {
		return unavailable("unavailable_report_root")
	}
	if r.payload == nil {
		return unavailable("unavailable_store_not_open")
	}
	manifest, sha, e := verifyWindowPayloadStore(r.payload)
	if e != nil {
		return unavailable("unavailable_store_verification")
	}
	if requireComplete && validateWindowAcquisitionPayloadLinkage(r.ledger, r.ledgerDir, r.auth.evidenceKind, manifest) != nil {
		return unavailable("unavailable_ledger_linkage")
	}
	raw, e := json.Marshal(manifest)
	if e != nil || conversationHash(raw) != sha {
		return unavailable("unavailable_manifest_encoding")
	}
	const name = "payload-manifest.json"
	if r.recordFiles == nil {
		r.recordFiles = map[string]string{}
	}
	if known, exists := r.recordFiles[name]; exists {
		saved, e := os.ReadFile(filepath.Join(r.auth.privateReportRoot, name))
		if e != nil || known != sha || conversationHash(saved) != sha {
			return unavailable("unavailable_manifest_changed")
		}
	} else {
		if r.persistWindowAcquisition(name, raw) != nil {
			return unavailable("unavailable_manifest_write")
		}
		r.recordFiles[name] = sha
	}
	r.receipt.PayloadManifestSHA256 = sha
	r.receipt.PayloadAuditOnlyCompletion = manifest.CompletionProof
	r.receipt.PayloadPackets = len(manifest.Packets)
	r.receipt.PayloadVerified = requireComplete
	r.receipt.PayloadStatus = "audit_only"
	if requireComplete {
		r.receipt.PayloadStatus = "verified_ledger_linkage"
	}
	return nil
}
