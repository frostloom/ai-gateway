# V9 scope and typed stopped46 implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prepare a new data-only full3660 V9 scope and a concrete read-only stopped46 baseline without resuming V8.

**Architecture:** Reuse fullWindowScope and the existing scalar budget/retry declaration shapes. Repartition the original slots greedily into at most12 pairs and24 both-arm defined turns, pin all511 caps. A separately typed stoppedV8 baseline reads the exact451 original files and old44 through the same explicit history pass; only Finish may release the constructed top-level result.

**Tech Stack:** Go standard library, existing taskbench accounting and history pass.

**Spec:** .tmp/new-full-matrix-plan-20261030/scenario-admission-plan.md (af249322dee9266f212524b523cd8e54490053601f7739e9f7f0a316ccab2b26), selected vector6a407ce8aafab334dc37cf1e0d445a69fd03ac527be2b29097506a3df4000182; root implementation brief.

## Global Constraints

- Old AA/V8 files and consumed scopes remain unchanged and permanently stopped.
- Parent180000000/2500000 is a narrower candidate inside global200000000/4000000; human_authorized/allow_paid/ready_for_paid_run remain false and no V9 exact authorization is asserted.
- Original3660/7320/11772/1851, original order/identity except window_id, knowledge off, logical58860/5886/native58880/5906, global20 extras per provider,300/90/10 deadlines unchanged.
- Historical46 permanent52654504/408701; actual3557/918. Existing unknown liability and inner/outer contradictory-looking states remain as recorded.
- No old constructor/factory/claim path is weakened; no actual-ready/owner/signing/claims/providers wired by this unit.
- A exclusively owns first/V2–V7 pass wiring; root exclusively owns campaign/collector/context guard; B exclusively owns helper catalog/runner.

## Review Focus

- Coherently rehashed altered caps/order/window boundaries must fail validation.
- Collected inner receipt must never be promoted to a completed outer window.
- Claim2 remains permanent without ledger2/checkpoint2; any extra owned path fails namespace checks.
- New baseline may not recursively start a second top-level history pass or escape before Finish.
- Exact read-only audit is an explicit reasoning trust dependency, not a new full-window/quality proof.

### Task1: Fixed V9 scope declaration

**Files:** Create internal/taskbench/window_scope_v9.go, window_scope_v9_test.go and fixed cap data; modify only the V9 case in window_scope.go.

**Interfaces:** prepareFullWindowScopeV9(s ConversationSuite,p PairedProfiles,caps []fullWindowCaps,b fullWindowBindings,baselineSHA string)(fullWindowScope,error); validateWindowV9Declarations(scope fullWindowScope)error. Reuse existing budget/retry shapes and canonical hash helper. Separate exact caps hash includes the511 ordered pairs.

- [ ] Write semantic tests proving unchanged non-window slots, greedy boundaries,511 count, totals, separate-currency arithmetic, unchanged retry limits, no runtime authority, and coherent mutation rejection.
- [ ] Run RED with focused selector, preserving output; missing API compile errors require initial data-only stub before meaningful behavior RED.
- [ ] Implement minimal repartition using existing original schedule constructor, fixed511 caps and exact declarations; validator rederives scope through new switch branch.
- [ ] Run focused normal/race plus old scope1–8 serialization/rejection tests; no full Go/real history.

### Task2: Typed stopped46 loader

**Files:** Create internal/taskbench/window_stopped_v8.go, window_stopped_v8_bundle.go, window_stopped_v8_pins.go and corresponding tests; new immutable descriptor outside old V8 root.

**Interfaces:** loadWindowStoppedV8BaselineContext(ctx context.Context,path,expectedSHA string)(windowStoppedV8Baseline,error), loadWindowStoppedV8BaselineWithPass(p *historyReadPass,path,expectedSHA string)(windowStoppedV8Baseline,error). Reuse A loadWindowStoppedV7BaselineWithPass/historyStoppedV7File/readWindowStoppedV7InventoryWithPass and epoch8 lock. No edits to A files.

- [ ] Prepare pure portable bundle tests and temporary filesystem namespace/cancellation tests; assert old44+two pinned claims, exact stop chain, v3 ledger/runtime totals, no second ledger or extra claim, original terminal and receipt flags preserved.
- [ ] Run meaningful RED before production validation implementation.
- [ ] Implement exact descriptor/audit/inventory/source-confirmation anchors and original manifest/binary attestation; predecessor twice within same pass, no nested top-level wrapper. Explicit451 streams and namespace, before/after identity; End fresh all dependencies. Preserve audit reasoning trust and do not create issuer capability.
- [ ] Run narrow normal/race; root separately schedules complete-original no-paid validation after review.

### Integration boundary

This unit does not make V9 runnable. Subsequent actual-entry work must bind the final source/binary/scope, new authorization and typed46, retain all authority gates, and undergo independent review. V9 readiness is not inferred from data-only tests.
