# Full3660 V12 Completion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver the full original3660-pair comparison using the fixed already-delivered385 prefix and a newly frozen3275 suffix, preserving all stopped data and budget liabilities.

**Architecture:** Reuse V10's exact full-slot composition and V11's durable suffix observation/proof path under a new epoch. Separate concrete154 historical DATA from runtime permission and from the final delivered-prefix/normal-suffix opaque proof. Preserve all existing global-stop and no-body-replay rules.

**Tech Stack:** Existing Go1.25.12/SDK26.5 and Python3.12 tools, original protobuf/OTel/Ed25519 interfaces, immutable private archives; no dependency changes.

**Spec:** `docs/superpowers/specs/2026-11-04-full3660-completion-v12-design.md`.

## Global Constraints

- Full3660/7320/11772,1851 boundaries separate; prefix original001..055/1..385/770arms1268turns; new456 windows original056..511/386..3660/6550arms10504turns.
- Global200m microCNY/4m microUSD, Parent195m/3m, old15479620965/838363; base110191257/1818872+risk2023424/65+extras1495040/53760=total193330686/2711060. Reserves are not bills.
- Model90s/JEV10s/window300s, same narrow TLS retry20/provider; suffix logical52520/5252, native52540/5272. Any technical failure closes scope; no new per-arm continuation.
- Knowledgeoff both arms; answering/routing/business/case/profile/config equivalence independently source-bound; new key only in fresh private settings. No old claim/scope restart, evidence edits or hold releases.
- Ordinary production/direct observer defaults unchanged. Original OTLP bytes/IDs, durable-local versus remote proof, final-only suffix shipment and once score registry preserved.
- All unrelated current dirty billing/gateway/UI files remain untouched. Direct main is explicitly authorized; exact archives exclude these uncommitted changes. Root alone handles keys, real constructors/DB/LF/provider IO, final binding and execution.

## Review Focus

- A manifest-signed source map must not authorize an unreviewed business/routing change; freeze an exact production-diff allowlist and reject unmatched customer/fixture/profile/config values (Tasks2/4).
- R3 exit1, pending request and cleanup PermissionError must not become a normal completion or free allowance (Tasks1/3).
- Same case names with changed pass/arm/order/turn partition or an overlapping primary source must fail complete composition (Tasks2/3).
- An old LF summary or JSON report cannot grant new shipment/score authority; old published bodies/scores are never repeated (Task3).
- A canceled/late/unknown delivery or paid operation must retain evidence and block its original scope; no background context, readback absence or fresh key bypasses it (Tasks1–4).

## Files and execution method

New Go units: `internal/taskbench/window_stopped_v11.go`, `window_stopped_v11_bundle.go`, `window_stopped_v11_test.go`; `window_completion_v12.go`, `window_scope_v12.go`, `window_executor_equivalence_v12.go`, `window_scope_v12_test.go`; `window_claim_v12.go`, `window_actual_v12.go`, `window_campaign_v12.go` plus focused tests. Existing dispatch-only edits belong to Task2 after Task1 acceptance; no old epoch broadening.

New Python/helper units under `eval/behavior-review/`: `full1220-v12-contracts.py`, `full1220-v12-window.py`, `full1220-v12-owner-template.py`, `full1220-v12-scores.py`, `full1220-v12-delivery.py`, selected helper-runtime/source helper and focused `tests/test_full1220_v12_*.py`. Reuse exact native/content/spool mechanisms; do not duplicate the observability exporter.

Fresh implementers and independent reviewers follow the existing execution method. Root owns shared dispatch/integration changes and every real IO. Independent pure declarations may proceed alongside the historical reader; actual runtime waits for accepted concrete154. Each task is reviewed before downstream authority uses it. No new user confirmation is required for already delegated engineering work; report exact authority honestly.

### Task 1: Concrete154 stopped history, DATA only

**Files:** New stopped-V11 Go units above and focused tests; do not modify stopped-V10 or older semantics.

**Interfaces:** `windowStoppedV11Baseline` contains the concrete predecessor `windowStoppedV10Baseline`, detached `fullWindowBaseline`, exact R3 evidence/root/claim identities and a `predecessorRoots() []string` method. Provide `expectedStoppedV11WindowBaseline() fullWindowBaseline`, `loadWindowStoppedV11BaselineContext(context.Context,string,string) (windowStoppedV11Baseline,error)` and `loadWindowStoppedV11BaselineWithPass(*historyReadPass,string,string) (windowStoppedV11Baseline,error)`. It has no actual/source/score issuer. The exact root-prepared descriptor path/SHA and raw map are pinned before implementation; no UNFROZEN value is accepted by an actual entry.

- [ ] Write RED tests: expected154/79620965/838363/6659/1677,50 original R3 claims and no51; changed claim/source/ledger/body/namespace/identity/End/cancel rejected; stopped50/pending18model74752/ownerexit1+PermissionError remain DATA-only; no release/retry/normal proof. Include old raw bytes and ordinary APIs compatibility assertions.
- [ ] Run fixed-toolchain focused normal test, retain RED. Expected assertion failure from the missing/new154 implementation, not environment failure.
- [ ] Implement new physical reader using actual predecessor and exact new inventory. Retain bounded reads, supplyingFD/hash/length/metadata/fresh-before-after/namespace/locks/context and complete End checks; sourceRoot uses R3's exact R2 archive path. Validate all50 journals/payloads, including the final written/no-response pending event, without network/DB/credentials. Deep-copy invocation-local maps; no persistent cache.
- [ ] Run `go test ./internal/taskbench -run 'Test.*StoppedV11' -count=1 -timeout 180s` and matching `-race`; root separately invokes the genuine production154 constructor once with actual original files and fresh output. Expected tests0fail/skip and real constructor exact154/amounts/calls/zero new claim/provider IO, then reaped empty group. Preserve failures; do not widen a limit without measured cause and a separately ledgered new-epoch-only ruling.
- [ ] Independently review focused production diff/tests and raw constructor evidence, fix confirmed issues once, then commit only owned paths. Acceptance produces concrete154 for Task2.

### Task 2: Fixed V12 composition, exact suffix and actual runtime

**Files:** New V12 Go units/tests; root integrates only necessary actual-ready/session/scope/collector/owner/claim dispatch and `cmd/full-window-actual/main.go`. New declaration first; ordinary old entry rejection stays.

**Interfaces:** New `windowCompletionDeclarationV12` accurately identifies the original R5 prefix, original fullSlots/schedule, exact suffix, historical154 and all excluded stopped/supplemental sources; do not reuse V10's old AllAttempt totals as current totals. Provide `expectedWindowCompletionDeclarationV12()`, `prepareFullWindowScopeV12(ConversationSuite,PairedProfiles,[]fullWindowCaps,fullWindowBindings,string) (fullWindowScope,error)`, `validateWindowV12ExecutionShape(fullWindowScope) error`; use the existing exact mapped execution accessor. New actual preparation/authorization/session follows V11 opaque interface patterns and new `.v12` schemas with typed154; old scopes never become executable.

- [ ] Write/run RED vectors: exact3660slots+456 declarations/counts/caps/maxima/budget; reject prefix permits, arbitrary range, overlap/gap/pass/arm/reordering, fake source equivalence, old104/oldnonce/oldauthority, out-of-budget and local457; local456 finishes without another claim. Expected new-epoch missing/rejection assertions fail before implementation.
- [ ] Implement pure declarations and immutable exact production-source/config equivalence allowlist from R5→63b9 plus new executor-only changes. Add runtime dispatch after Task1 acceptance, retaining V11 durable observer and original paid/global-stop/native/fixture/accounting semantics. No model/JEV policy change.
- [ ] Run focused V12 normal/race and original V10/V11 dispatch/JSON tests with bounded selectors in root's exact private overlays. Expected0fail/skip in focused suite, original opt-in skips remain only where previously defined. Independent review must inspect the complete allowed production diff, including transitive billing/state dependencies.
- [ ] Commit accepted Task2 paths; no real signing, owner, model, claim or LF IO by worker.

### Task 3: Authentic delivered-prefix plus complete-suffix proof consumers

**Files:** New V12 Python/helper units and tests above; old frozen V8–V11 scripts/IDs/once journals untouched.

**Interfaces:** `VerifiedV12Completion` is a same-process opaque composite of a genuine locally validated original-prefix delivery proof and a genuine normally completed new456-window suffix proof. New factories expose explicit prefix/suffix meanings, exact manifest/binding/summary/owner pins and complete original tuple/turn partition. Window projections/score/delivery consumers accept only their selected same module/proof identity. Pure declarations/plans and saved JSON cannot mint it. New source helper uses V12 suffix mapping, existing source-bound case/state/requirements/capture code and metadata-only native IO; fixed prefix artifacts are real original inputs, not JSON summary substitutes.

- [ ] Write/run RED: real prefix-artifact parser with source/IDs/once/ACK/GET requirements; unchanged ten positive holes; reject prefix summary-only/fake owner/cohort/stopped suffix, altered cases/content/parents, gaps/overlaps, dropped failures/NA, unattempted-as-complete and foreign modules. Check456-to-original056..511 mapping and full union3660/7320/11772+1851.
- [ ] Implement final-only **new suffix** ship/score eligibility, normal owner reaping and closed-spool proof; reuse exact original-body no-replay shipper/capture/remote proof. Original2300 prefix scores/bodies are referenced as published history and never re-POSTed. Unknown/partial/late body read still forbids resend; independentGET never authorizes retry.
- [ ] Run `python -B -m unittest discover -s eval/behavior-review/tests -p 'test_full1220_v12_*.py'` plus selected old V11 compatibility tests. Expected0fail, no new skip, no real network/provider/DB/keys. Root separately builds the selected Go helpers and genuinely exercises source/spool/localstub/prefix-original parsers once after review; no synthetic vector is actual completion.
- [ ] Independently review interfaces and adversarial rejection paths; commit only accepted owned paths. Worker never signs or publishes.

### Task 4: Root exact archive, freeze, execute and deliver full study

**Files:** Private new historical descriptor/inventory, selected archive/helpers/settings/authority/owner and audited public progress/final report. No old artifact edits or unrelated changes.

- [ ] Self-review Task1–3 interfaces, exact source equivalence/profile/knowledge/config,154 history/budget and prefix-original LF evidence; run only meaningful focused integration/race checks and a genuine controlled localstub. Expected honest DATA/permission/publication boundaries and full suffix vector, no actual claim/provider calls.
- [ ] Commit selected source, archive it excluding unrelated dirty files, run exact fullGo once under30m/package-p=1/original assertions/skips, restore9 original services by exact IDs/config/health, build selected source/capture/spool helpers and signed catalogs. Preserve all failed runs. FullGo success is not actual completion.
- [ ] Freeze new Parent195+3/global200+4, raw autonomy+latest budget authority, source/binary/new.env-derived private settings/spool and complete prefix/suffix bindings. Independent exact-material review, actual Go and Python pure authorization checks must pass before one fresh owner. No fabricated human exact-hash approval.
- [ ] Execute original056..511 exactly once per window under existing stops/retries; parallel independent semantic reviews; do not block acquisition on scores. A real stop closes the scope permanently and records every failed/NA/unrun item/hold; never silently restart full3660.
- [ ] After all456suffix windows normally finish and owner is reaped, verify opaque complete composition, exact suffix spool/IDs/parents/content and historical prefix delivery. Ship suffix original batches once, verify remote content, publish eligible suffix scores once and GET-read them. Preserve original prefix holes/suppression and all unknowns.
- [ ] Recompute and independently audit unique primary plus all-project attempts, costs/unknowns/holds/cache, cost per correct task, epoch-stratified P50/P95 with explicit clocks, delivery lag and full provenance. Direct-push main and verify remote SHA. Mark goal complete only when the full original task/data/fixture/semantic/score/content/report evidence is present.

## Root rulings and pre-flight

The old V11 single-new-cohort choice is superseded, not its failure-stop/immutable-data/security rules. The existing R3 run remains closed; changing a key does not repair or revive it. No per-arm fault continuation is included. Task1 produces concrete154; Task2 consumes that exact type and pinned descriptor; Task3 consumes only accepted V12 schemas and root-selected actual source/catalog; Task4 alone opens new actual authority. The current design audit supplies source/budget evidence but does not replace these dynamic gates. Update this ledgered interface contract if actual source inspection exposes a mismatch, rather than silently renaming an interface.
