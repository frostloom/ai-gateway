# Full 1220 Local Handler Matrix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Exercise every one of the1220 frozen primary tasks through the new full-selection real Handler/fixture adapter, with closed in-process model responses, FakeJEV and local OTLP capture; produce a SHA-bound complete coverage matrix without any actual model/JEV/score call or semantic-success claim.

**Architecture:** Reuse localCampaignCapture/newLocalCampaignDependencies for local observation and existing full-selection/campaign guards. A new oracle-informed script handles frozen read/preview/no-tool branches and symbolic plan names. Explicit windows of at most100 sources each fit a300-second local deadline; a full-source plan and read-only aggregator preserve all1220 IDs, failures and unrun windows. Old six-case rehearsal behavior is unchanged.

**Tech Stack:** Go taskbench, MySQL isolated fixtures/private Redis, in-process HTTP RoundTripper/FakeJEV/local OTLP; Python standard-library planning and aggregation.

**Spec:** docs/full1220-readiness-2026-10-27.md; docs/full1220-adapter-2026-10-27.md; customer-service-evaluation-contract-2026-10-09.md. This is a required preparation stage, never the final actual1220 comparison.

## Global Constraints

- Direct main, no new real inference/paid scope/claim/score/deployment or budget changes. Current actual143/37, permanent reserve29.061120CNY/.223104USD unchanged.
- Frozen full selection SHA from eval/paired-pilots/2026-10-27-full1220-adapter/selection-identity.json; original definition/profile SHA and exactly1220 IDs preserved,1851 boundaries excluded.
- Source-dependent fake responses are deliberately oracle-informed, not a model test. All answers remain needs_review; task_success/real comparison eligibility never becomes true.
- New local options cannot configure model/JEV endpoints or credentials. The model RoundTripper never delegates to network; observer only its new local sink. Only explicit loopback MySQL admin DSN may create owned fixture schemas.
- Windows1..100 unique primary keys, each300s and failure-stop, never overwrite/resume an old output directory. Controller stops on any failure; original negative/unrun evidence remains. Only unpaid fresh local reruns after actual fixes may be considered, never paid scope recovery.
- Existing five model iterations/128 output and all source/observer/usage/campaign guards stay enabled. Synthetic usage/prices are test scaffolding, not evidence for the eventual paid token envelope or invoice.

## Review Focus

- Wrong case/turn or symbolic plan translation must fail rather than become a plausible canned pass.
- No external provider fallback through a missing script or malformed request; no accidental dotenv or operator API-key discovery.
- Do not mark generic no-tool replies as independently correct clarification/refusal/consultation; only structure is checked.
- Aggregate must reject duplicate/missing IDs, changed source/scope, missing receipts and falsely completed windows; preserve failed/unrun coverage.
- Timeout/factory/report/cleanup faults must preserve stop state and only remove exact owned resources; no old artifacts touched.

### Task 1: Closed full-primary local script and window runner

**Files:** Create internal/taskbench/full_local_model.go, full_local_model_test.go, full_local_campaign.go, full_local_campaign_test.go. Reuse existing local capture helpers without widening localCampaignCaseSupported or original RunLocalCampaign.

**Interfaces:** `FullLocalCampaignOptions{DefinitionsDir string, Environment *PairedFixtureEnvironment, Revision string, Keys []string}`; `RunFullLocalCustomerWindow(ctx context.Context,dir string,o FullLocalCampaignOptions) (FullLocalCampaignReceipt,error)`.

- [x] RED pure script tests cover all expected action families: read, preview, clarify, respond/consult/limited/reject; accept only local Anthropic128/disabled requests. Missing/malformed/unknown-turn input returns error, never uses http.DefaultTransport.
- [x] Read tool args are taken from the pinned source parameters_from_original layer (not guessed); plan_reference resolves to the pinned catalogue plan_name, never a guessed numeric ID. Case operation amount uses its frozen value. Tool-result prose is returned as synthetic text; ordinary no-tool script clearly remains unreviewed.
- [x] RED window tests: explicit1..100 primary keys only, duplicates/boundaries rejected before deps/output; real local cases spanning all12 source families and clarify/preview branches. Use RunFullCustomerCampaignBatch with all four guards and new full-selection hash.
- [x] Implement one case per temporary batch, fixed synthetic model/loopback identity and process-only transport; FakeJEV low-confidence fallback so no forced route quality claim. Model/JEV limits derive worst-case frozen turns, currencies explicitly synthetic. Capture checkpoint validates2 roots, actual turns/generations, case/arm/full contract and unknown task_success. Durable new-only receipt lists every requested case as checked_local/failed/not_started.
- [x] Add dependency/provider/observer/budget/receipt/cleanup failure and cancellation tests; preserve whole temporary reservation and stop next batch, no retry.

### Task 2: Explicit local CLI and full1220 window aggregation

**Files:** Create cmd/full-customer-local/main.go/main_test.go, scripts/full-customer-local-matrix.py, scripts/tests/test_full_customer_local_matrix.py.

**Interfaces:** CLI `-execute-local -keys <JSON array> -mysql-dsn <explicit loopback admin> -revision <40hex> -out <new dir>` plus frozen definitions/profiles paths; no provider/key/paid flags. Timeout fixed300s. Python exposes `prepare(root: Path) -> dict` and `aggregate(plan: dict,window_evidence: list) -> dict` with explicit SHA refs.

- [x] RED CLI tests default disabled, malformed/non-loopback/db-named DSNs and absent/over100 keys reject without fixture/output; never print or archive DSN/API credentials. Validate all selection inputs before creating owned fixture environment; cleanup via independent bounded context.
- [x] Implement CLI; pure Python plan matches original selector exactly and yields13 ordered windows (12x100+20),each source exactly once with original1220 denominator and selection identity.
- [x] RED aggregate tests duplicate/missing/tampered source/window/receipt or fake ready/semantic-success claims; incomplete evidence is unknown/unrun, not success. Successful local matrix status requires1220 distinct checked_local pairs,2440 arms/3924 observed turns and complete local trace counts; real evaluation remains incomplete.
- [x] Implement planning/aggregation only, no hidden subprocess retry/resume. Full run controller records actual command attempts and hash-bound stdout/receipt; stops after first nonzero/incomplete window, never reuses an old output directory. Private DSN not copied to public evidence.

### Task 3: Execute actual full local matrix and deliver

- [x] Run targeted RED→GREEN and representative local windows, then fresh whole-change review, final original-config Go20min and Python suite; fix real defects without weakening source/scorer assertions.
- [x] Commit code first, archive-build new local CLI from that commit with exact Go/source/binary hashes; freeze all13 window definitions before first run.
- [x] Execute each window once, at most300s, closed substitutes only; on any failure preserve all partial evidence and investigate without treating a tool timeout as terminal until process state checked.
- [x] Independently rederive1220 coverage, observed turns, local spans, source contracts and no semantic/actual-model claims; query isolated-schema cleanup and original evidence/claim SHA.
- [x] Archive public per-case structural matrix and private raw journals/requests separately, update AGENTS/Hindsight, push main. Overall goal stays active for real full-scope scheduling, quality/failure policy, budget, actual execution and independent scoring.

## Execution rulings

User already opted out of repeated plan approvals and authorized autonomous direct-main work. This plan does not permit any increased paid budget or live-scope resume. Forcing low-confidence FakeJEV and using source-aware scripts intentionally removes any route-accuracy/benefit claim; the deliverable is mechanical Handler/fixture/scorer and observation coverage only.
