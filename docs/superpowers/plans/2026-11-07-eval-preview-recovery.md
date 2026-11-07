# Failed-preview recovery and full3660 completion implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans for root implementation; independent reviewers inspect each completed gate. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete the original3660 paired evaluation and once-only Langfuse delivery after preserving the permanently stopped V15 scope.

**Architecture:** Add a separately selected evidence policy for proven non-executing preview failures followed by one correct pending preview. Add a new default-UNFROZEN V16 completion entry which validates249 historical claims and composes the original385 plus V15 normal567 prefix with exactly2708 remaining pairs. Historical policies and all original stopped data remain unchanged.

**Tech stack:** Existing Go taskbench/owned driver, SQL/session capture, pure Python contracts and source/score/delivery adapters; no new dependency or customer-agent changes.

**Spec:** [2026-11-07-eval-preview-recovery-design.md](../specs/2026-11-07-eval-preview-recovery-design.md). Fresh builder check found nothing important left. The user opted out of repeated questions and delegated execution; root proceeds through these gates without asking them to select an execution method again.

## Global constraints

- Original1220×3=3660 pairs/7320 arms/11772 turns/1851 boundaries, original caps/order and both knowledge=off remain fixed.
- Preserve original new3275 suffix as567 completed pairs/70 windows plus2708 new pairs/386 windows; never count stopped ordinal953 as complete.
- Global CNY200+USD4; narrower Parent CNY200+USD3. Candidate maximum199095362 microCNY/2812692 microUSD, subject to independent recalculation before signing.
- Whole window600 seconds including history, model90 seconds, JEV10 seconds; existing TLS same-context/same-body eligibility, bounded extras and all other technical-stop rules unchanged.
- V15 and older scopes are permanently closed; no resume/reissue/claim release.249 reservations and old pending10/held2688 remain preserved.
- Root alone owns Go/DB/helper/actual/signing operations. Read-only reviewers do not duplicate heavy checks or receive secrets/native bodies.
- No authority from JSON, ordinary build, component tests, controlled providers or this plan. New actual authority comes only after independent exact freeze.

## Review focus

- Failed preview followed by malformed/wrong/foreign pending must never reach the scripted confirmation.
- A matching error string without exact source-derived argument/state proof must still halt.
- Session history, audit/SQL/control changes, aliasing and cancellation must be checked live and on replay, without cross-pass caching.
- A gap, duplicate, wrong original slot or stopped arm in the952 prefix must prevent full-cohort composition and score publication.
- Old-policy payloads and consumed owners must retain their original rejection/closure; new schema selection cannot reinterpret old records.

## Task1: Pure versioned recovery evidence

**Files:** Create `internal/taskbench/collection_preview_recovery.go`, `collection_preview_recovery_test.go`; modify `collection_policy.go:30` (`collectionRecorder`) and `conversation_runner.go` only for an explicitly selected new recorder field. Keep `collection_preview_failure.go` and its public policy identity/behavior intact.

**Interfaces:** New `collectionPreviewRecoveryEvidence` contains turn, before/after raw sessions, complete before/after `qualityEffectCapture`, optional `*qualityPendingCapture` and existing complete-order projection when needed. New `collectionPreviewRecoveryRecording` embeds `collectionRecording` plus `preview_recoveries`. Produce `recordCollectionPreviewRecoveryConversation(context.Context, ConversationCase, ConversationDriver) collectionPreviewRecoveryRecording`, `verifyCollectionPreviewRecoveryRecording(collectionPreviewRecoveryRecording) error`, `verifyCollectionPreviewRecoveryEvidence(ConversationCase, Fixture, ConversationState, ConversationObservation, collectionPreviewRecoveryEvidence) error` and `collectionPreviewRecoveryPolicySHA() string`. A new provider method returns exact turn evidence; no data type implements an owned permit.

- [ ] Add `TestCollectionPreviewRecoveryMissingRefundBasisThenCorrectPending`: synthetic own refund{} failure, correct amount100 terminal pending, complete unchanged state/SQL, exact session/audits and raw response; original v2 verifier must reject this shape. First run must fail against current implementation, before adding production behavior.
- [ ] Add table-driven rejection tests for wrong/foreign/duplicate/after-pending traces, unknown tool/outcome, confirmed/execute trace, pending before turn, wrong tenant/amount/preview/idem, malformed raw/session/telemetry, source-error mismatch, SQL/control/audit changes and cancelled context. Test empty-basis without final pending separately, and retain all old v2 known-family tests.
- [ ] Implement only the observed missing-refund-basis derivation and existing v2 failure families, with strict trace ordering and at most the existing source iteration count. Reuse existing validators without weakening their old entry behavior. Failed calls/repairs remain in the original trace and accounting.
- [ ] Run focused normal and race tests for new recovery and old failure/collection tests; verify old policy SHAs and JSON omission behavior unchanged. Root records exact commands/results and requests a fresh source/spec review before integration.

Command family: `go test ./internal/taskbench -run 'TestCollectionPreview(Recovery|Failure)' -count=1 -timeout=20m -json`; repeat with `-race`. A component pass is not actual readiness.

## Task2: Owned capture, durable sidecars and proof isolation

**Files:** Create `internal/taskbench/window_preview_recovery_owned.go`, `window_preview_recovery_test.go`; modify owned driver/proof declarations and dispatch in `window_owned_driver.go`, strict policy selection in `window_preview_failure_owned.go`, and the optional receipt reference in `window_collector.go:39` (`windowAcquisitionRow`). Add `preview_recovery` fields with `omitempty`; old scopes must reject them.

**Interfaces:** Produce new `windowOwnedPreviewRecoveryPolicySHA() string`, scope-selected recovery mode, owned `captureWindowPreviewRecovery(context.Context, qualityEffectCapture, json.RawMessage, ConversationObservation) error`, deterministic recovery replay, a new immutable sidecar/reference and owned proof evidence accessor. Retain pending data exactly; never clear or manufacture it to make evidence pass. No old scope selects these methods.

- [ ] Add a red controlled owned-driver test for the same failed{}→pending100 request and explicit next-turn confirmation; assert exactly one10000-cent negative refund row, exact pending binding, no intermediate business effects and original failed trace retained.
- [ ] Add red tests for unsafe pending and for capture/sidecar failure revoking the next phase, including cancelled owner and mutated file/link/session. Verify old V13–V15 modes keep rejecting the mixed shape.
- [ ] Implement owned quiescent capture using the existing same-owner full SQL/session/audit machinery. Persist a new versioned sidecar before cleanup; verify same-process and external replay through the same pure predicates.
- [ ] Run focused normal/race tests and an owned fake-provider same-path test under unchanged limits. Independent reviewer checks the next-confirmation boundary, immutable old behavior and every negative mutation before proceeding.

## Task3: Concrete stopped249 history

**Files:** Create `internal/taskbench/window_stopped_v15.go`, `window_stopped_v15_bundle.go` and matching tests following the existing stopped-V14 pattern. Create a root-owned private exact descriptor and retained genuine-run results outside the source archive; no secrets in public fixtures.

**Interfaces:** New `windowStoppedV15Baseline` wraps frozen `windowStoppedV14Baseline` and adds exactly71 original V15 claims. Produce `expectedStoppedV15WindowBaseline() fullWindowBaseline` and `loadWindowStoppedV15BaselineContext(context.Context,string,string) (windowStoppedV15Baseline,error)` plus same-pass internal loader. Returned baseline is DATA, not execution authority. Descriptor path/SHA become fixed only after root construction/review.

- [ ] Add red declaration/typed tests for249 claims,9184/2437 attempts and permanent103307765/1224400, stopped summary/terminal/exit links, exact normal70 receipts and stopped71 negative. Mutated claim/checkpoint/ledger/native/source/context/identity/locks must fail; no claim72 or checkpoint71 may be required or invented.
- [ ] Implement by invoking the original178 loader unchanged within one fresh pass, then validating the new original V15 physical bundle and all prior maps/metadata/locks. No quota expansion, recursive duplication or cross-pass cache.
- [ ] Run focused normal/race tests and one genuine root constructor against unchanged originals. Record duration, file identities and complete source maps before/after. Perform a live-shaped read-only verification with forbidden actual/provider/claim sentinels; do not call it actual-ready.

## Task4: Exact952 prefix and remaining2708 declaration/entry

**Files:** Create V16 counterparts of `window_completion_v15.go`, `window_scope_v15.go`, `window_claim_v15.go`, `window_campaign_v15.go`, `window_actual_v15.go`, `window_executor_equivalence_v15.go`, `window_operation_approval_v15.go` and focused tests. Modify shared strict DTO/dispatcher/CLI declarations only to select the new exact version; add `cmd/full-window-actual/v16_test.go`.

**Interfaces:** `expectedWindowCompletionDeclarationV16()` binds original385 delivered proof, V15 normal windows1..70 and stopped249 baseline. `prepareFullWindowScopeV16(ConversationSuite, PairedProfiles, []fullWindowCaps, fullWindowBindings, string) (fullWindowScope,error)` takes the fixed249 descriptor SHA and selects only original windows126..511 while retaining all3660 slots. `prepareWindowCampaignV16Technical(root, frozenRoot string, scope fullWindowScope, source ConversationSuite, profiles PairedProfiles, bindings fullWindowBindings, frozenSHA, frozenExecutionSHA string, limits windowCampaignLimits) (windowCampaignPrepared,error)` is DATA-only; actual preparation additionally requires `windowStoppedV15Baseline` through a dedicated typed field. Only the V16 owned entry can select the new recovery policy. Policy/authority types remain default false and `human_reviewed_exact_scope=false`.

- [ ] Add red pure tests for952+2708 composition,567+2708 original suffix,386 exact window objects and8870 remaining turns. Pin unchanged full schedule/caps/boundaries and reject any altered order, window, duplicate/gap or stopped953 arm promotion.
- [ ] Independently sum remaining original base92269133/1534467, logical44350/4435, native44370/4455 and unchanged risk/extras. Assert candidate199095362/2812692 within Parent and old249 retained. No claim creation from declarations.
- [ ] Implement strict version dispatch and exact source/equivalence bindings. Old ordinary/actual scopes reject new policy/fields before credential access. Human600 approval may be rebound as existing timing permission, never represented as a new exact-scope human answer.
- [ ] Run focused pure/dispatch/authorization negative and normal/race tests. Fresh review checks budget arithmetic, old authority isolation and no obsolete scope reactivation.

## Task5: Genuine prefix/source, score and delivery compatibility

**Files:** Create V16 counterparts of `eval/behavior-review/full1220-v15-{contracts,window,source,prefix-index,scores,delivery,helper-runtime,owner-template}.py`, the two V15 Go helper templates and corresponding role/contract/source/score tests. Reuse existing exact helpers by pinned identity when safe; do not copy or mutate historical archives.

**Interfaces:** V16 source composition returns an opaque same-process proof of original385 + selected V15 normal567 + new2708. It must validate original recording/source/fixture/native/receipt/sidecar maps and exact slot identities, not merely concatenate review JSON. Review schema/requirements remain source-bound; new recovery sidecars are verified separately. Delivery consumes only authorized selected traces/scores, excluding stopped71 and preserving original385 once registry.

- [ ] Add red tests for duplicate/gap/reordered/incorrect-source prefix, stopped71 selection, consumed owner/scope, wrong recovery sidecar and mismatched review. Prove pass-score suppression of original negatives and prior-prefix once-only behavior.
- [ ] Implement explicit mixed-origin composition and source/helper/capture catalog binding. Retain every historical false/failed/NA and content-capture switch. Verify that a globally stopped old spool cannot bypass per-selected-window proof or expose stopped traces.
- [ ] Run pure Python suites and real Go source/capture helpers against controlled/stopped copies without provider/LF calls. Root performs one genuine952-prefix read-only preflight; no former JSON output is substituted for fresh opaque proof.
- [ ] Independent review checks all content/score/once and original-source bindings. Do not publish partial positive scores or infer external payment settlement from ledger captures.

## Task6: Exact freeze, actual remaining2708 and full delivery

**Files:** Root-owned new private archive, regression/controlled/freeze evidence and unique owner; public frozen/progress/final reports. Source remains immutable after archive selection.

- [ ] Capture one exact current-source archive and run the existing full regression protocol (`go test ./... -count=1 -timeout=20m -json`) with controlled services/cleanup and all original conditional skips/errors retained. Run genuine controlled fake-provider two-window collection under600/90/10 through the final binary/source/owner/helper/delivery path; verify every input identity before/after.
- [ ] Complete independent final source/spec, genuine249,952-prefix composition, role/score/delivery and budget reviews. Bind exact archive/map/binary/manifest/scope/helpers to one fresh technical freeze; default authority stays false until all gates pass. New authority must use the existing delegated human budget/autonomy/timing permissions honestly and cannot reactivate V15.
- [ ] Launch exactly one new owner for all2708 remaining pairs/386 original windows, no per-window human prompts. On any technical stop preserve and close, no restart/reissue/reclaim/release. Review completed immutable windows concurrently; root verifies exact bindings and decisive evidence.
- [ ] After all3660 are genuinely complete and reviewed, derive full correctness/cost/cache/P50/P95 metrics and source composition. Perform only the authorized once-only Langfuse content/score sends with ACK and unique GET readback, preserving prior385 registry and excluding stopped audits.
- [ ] Verify all original end-state requirements before marking the goal complete. Component, substitute, partial prefix or publication eligibility is never completion. Commit/push the resulting reviewed evidence and code directly to main without force or rewriting unrelated changes.

## Current gate status

- [x] V15 permanent stop, independent audit and root original preservation checks.
- [x] All normal V15 windows1..70 independently reviewed and root DATA bindings checked.
- [x] Design builder review: nothing important left.
- [ ] Task1 RED/implementation, all later integration/freeze/actual2708 and Langfuse delivery.

No production code, fresh actual owner, financial claim, provider request or Langfuse request has been created by this plan.
