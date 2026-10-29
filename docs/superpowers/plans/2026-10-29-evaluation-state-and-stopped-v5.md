# Evaluation State and Stopped V5 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Correct the evaluator's thousands-separated currency false positive and establish the concrete 32-claim stopped baseline needed by a future complete 1220 comparison.

**Architecture:** Keep the customer, original cases, routing, model parameters and both-off knowledge configuration unchanged. Correct only finite fact extraction for future runs; the consumed R2 source/records/negative states remain immutable. Add a separate concrete stopped-v5 reader using existing bounded raw/native readers and the exact closed R2 evidence, with no paid execution capability.

**Tech Stack:** Existing Go taskbench, standard library, existing Python offline score utilities; no dependencies or deployment.

**Spec:** `docs/full1220-v5-r2-stopped-2026-10-29.md`; exact private evidence at `.tmp/paired-live-private/full1220-actual-v5-r2-freeze-20261029/`.

## Global Constraints

- Original 1220 × 3 = 3660 pairs / 7320 arms / 11772 defined turns; 1851 boundaries separate.
- Customer50e/v5, both knowledge off; no original source-case/rubric or reply-repair/model-parameter changes.
- Current global parent ¥130+$1 was explicitly approved (artifact SHAef6322fe60807a7a608a2707edf016501717f85e9d649e28d0311de2b6ed4bce). Stopped R2 history and Task2 historical baseline retain their original parent¥105+$1; a future current-budget epoch must separately bind the new130 approval. 32 permanent claims42961430µCNY/344493µUSD, original risk2023424/65 and old held74752/2688 retained. Tasks1–3 are offline only; no new actual entry/paid claim/inference is authorized by their implementation.
- R2 owner30726 is permanently closed, exit0 after external verification refusal. Claim4 reserved/unrun; no ledger4/checkpoint4/claim5. Never resume/reclaim/release or rewrite any frozen source/tool/record.
- Work directly on main. Preserve unrelated web/dist, integrations, plate_sim.py and all old archives. Python -B and dont_write_bytecode before imports; never import frozen modules.
- Full Go validation uses the approved original20m window; single DB/HTTP timeouts and failure-stop policies unchanged. No full-suite repetition per tiny step: targeted checks per unit, one complete run after final combined implementation before claiming completion.

## Review Focus

- Correct `failed/structural_pass=true/success=false` is an original fact-check failure, not a promoted success; exact raw state and typed issues remain bound in Task2.
- `¥5,000.00` and `¥5000.00` mean500000cent; `¥50,00`/double commas must not be truncated into a smaller valid balance (Task1).
- Existing wrong-value/foreign-account/unsupported fact failures remain failures; finite extraction never proves full semantic correctness (Task1).
- A closed operator exit0 differs from binary exit1, and summary3.closed=false differs from terminal.closed=true; neither may be rewritten (Task2).
- Read32 original claim/native/record bytes under bounded readers, not just hashes or a caller-provided verifier; missing/extra claim, ledger4 and any reopened owner evidence reject (Task2).

---

### Task 1: Recognize grouped currency without accepting malformed groups

**Files:**
- Modify: `internal/taskbench/conversation_facts.go`
- Test: `internal/taskbench/conversation_facts_test.go`

**Interfaces:**
- Consumes: existing `claimCent(raw string) (int64, bool)` and `CheckConversationFacts(ConversationResult) []ConversationFactCheck`.
- Produces: same signatures and fact/status JSON; grouped whole amounts parsed before existing cent/overflow checks. No result-state or routing changes.

- [ ] Add table tests for ¥5,000.00, ￥5,000.00, 5,000.00元, negative grouped cash, multi-group amounts and original ungrouped forms; expected exact cents. Reject malformed50,00 / 5,,000 / 1,23,456, excess precision and overflow.
- [ ] Add real finite-check tests: matching cash500000cent/grouped reply stays match; wrong grouped amount stays mismatch; grouped recharge/refund assertions retain existing business delta checks. `applyConversationFactChecks` leaves StructuralPass true while marking genuine mismatch failed/Successfalse.
- [ ] Run original-source tests first and retain the expected amount/claim mismatch RED (not compile/setup error).
- [ ] Implement one shared numeric token pattern that captures the complete comma-containing candidate without consuming a following punctuation comma. Validate grouping before removing commas in claimCent; preserve existing precision/sign/overflow bounds. Apply to the three money claim regexes consistently.
- [ ] Run targeted fact tests and race; confirm old ungroupped/wrong-value tests still pass. Re-evaluate two R2 original replies only in a new offline report; do not edit old failed records or original tool bytes.
- [ ] Independent review; commit only task files and evidence/docs.

### Task 2: Concrete stopped-v5 32-claim proof

**Files:**
- Create: `internal/taskbench/window_stopped_v5.go`, `window_stopped_v5_pins.go`, `window_stopped_v5_bundle.go`, `window_stopped_v5_test.go`.
- Reuse unchanged: stopped-v4 native/raw/quality readers and previous concrete28 baseline; add a small focused evidence file only if needed to keep responsibilities separate.

**Interfaces:**
- Consumes: `windowStoppedV4Baseline` from descriptor2f8c8354…; fixed R2 original artifact SHA pins and readonly `.lock`.
- Produces: concrete `windowStoppedV5Baseline` with `loadWindowStoppedV5Baseline(path, expectedSHA string) (windowStoppedV5Baseline, error)`; `revalidateWindowStoppedHistory() (fullWindowBaseline,string,string,error)`; `windowStoppedPredecessorRootsV6() [5]string`. No embedded old capabilities or caller verifier/provider/issuer.
- Baseline result:32 claims, permanent42961430/344493, remaining62038570/655507, actual2292/585. Post-risk caps maximum60015146/655442 is a separate candidate check, not a proof that a full run will finish.

- [x] Freeze new private descriptor `.tmp/stopped-v5-proof-work/stopped-v5.private.json` (schema `full-window-stopped-baseline.v5`, SHA `b363ba194e641374a8a63bfacc7a1312a5b080718f637036ab4040fc075a3fc1`), binding previous concrete28 descriptor, original R2 root and exact105 budget approval. Fixed top24 pins are `.tmp/stopped-v5-proof-work/artifact-pins.json` SHA `cbe4ea0fa7bac59bdc73dd51ed5bea2c818557a1bc7be2232b3ae0c7624707a3`; complete claim32 map is beside it, canonicalSHA `7f6a967d739a1e338b69153413a5e9f06dcc3fb7d7bae05b64e2988686b5dc1b`. These are factual inputs, not a returned proof or paid readiness. No old file writes.
- [ ] Write RED positive actual-evidence loader and owned local-fixture tests, then missing/tampered top-file/native-body/record/checkpoint/claim tests. Invalid new-root/old-root alias, ledger4/checkpoint4/claim5, modified terminal/ownerexit and any altered fact-negative record must reject before returning a baseline.
- [ ] Implement bounded concrete reader with previous28 fresh before/after, exact campaign/records inventories, schema5/131schedule/old28map/105 bindings, per-window original accountingschema2 replay, bounded native descriptor decoding plus typed semantic equality with pinned payload metadata, and every native body/record raw SHA and usage; lexical descriptor JSON bytes are outside this inherited loader guarantee, while the separate before/after original-root inventory audits byte identity. Sum 391 packets and168 records, require the observed R2 zero quality-end sidecar set in all3 windows (negative-arm counts3/0/2), leave ancestor P4 quality-end validation unchanged, and bind the two exact structurally-valid fact failures71/79 without promoting status or Success.
- [ ] Preserve the exact two previous summaries and summary3.closed=false; require terminal.closed=true/acquisition_done=false/exit0/relaunchfalse and fourth claim permanently reserved with no descendants.
- [ ] Run pure unit/race tests; run one readonly actual bundle check and compare original explicit evidence inventories before/after. No new campaign constructor, no paid owner, no API/DB call.
- [ ] Independent review; commit only task files/descriptor public hashes/docs. Do not claim actual6 readiness.

### Final combined validation and next decision

Controller-owned after all selected implementation task reviews; task implementers do not launch this combined suite.

- [ ] Run original `go test ./... -count=1 -timeout=20m -json` once on the final selected source, restore the same owned9 services/identities and retain failures if any. This run does not authorize inference.
- [ ] Before any future runtime verifier is frozen, replay R2 complete3windows/4claims and all eight original quality issue codes, legitimate structuraltrue fact failures, zero/nonzero quality suffixes and technical unknowns. Positive state may never be obtained by rewriting original errors/status or removing evidence.
- [ ] Review complete-scope budget using all current bounds and declared conditional admission, not an unsupported worst-case-completion guarantee or linear extrapolation of84pairs. No automatic expansion, no shrinking/splicing3660 and no new paid freeze merely because caps can be made smaller.


### Task 3: Independent v6 scope and current130 campaign accounting

**Files:** Create focused `window_scope_v6.go`, `window_campaign_v6.go`, `window_budget130.go` and their tests in internal/taskbench; minimally modify common window_scope.go/window_campaign.go. No actual-ready/CLI/score changes in this unit.

**Interfaces:** Prepare/validate `full-window-scope.v6`; fixed width28,131windows, full original3660 schedule, exact cap-vector canonicalSHA30c8151031f40cb05562b255b789b9fb021a93467288e677c4eb39d87806113d totals85000000/600000. Baseline descriptorb363ba194e641374a8a63bfacc7a1312a5b080718f637036ab4040fc075a3fc1 and approvalef6322fe60807a7a608a2707edf016501717f85e9d649e28d0311de2b6ed4bce. Concrete constructor accepts windowStoppedV5Baseline and windowActualFile approval; no arbitrary caller verifier/provider/issuer.

- [ ] Independently recompute candidate vector from .tmp/full1220-v6-design-20261029/candidate.json and referenced conditional scenario tables: fixed minima plus largest-remainder surplus weights; write a private arithmetic report. Conditional stress assumptions are not an invoice or completion guarantee.
- [ ] RED tests first: preserve old1–5 canonical behavior, reject fixedvector/width/historical/approval/currentparent mutations and overflow. Extract existing pure schedule builder with explicit per-currency bounds, keep original prepareFullWindowScope wrapper bounds70938880/776896 exactly; v6 uses afterrisk85015146/655442 and fixed vector validation. No placeholder caps followed by overwriting a scope.
- [ ] New named-field concrete130 history wrapper rereads exact approval SHA bounded, fresh-revalidates concrete32 at original105, compares expected historical32 baseline, then clones map and projects only parent130000000/1000000 and remaining87038570/655507. Permanent42961430/344493, actual2292/585 and exact32map unchanged. Forward five predecessor roots, disjoint new root. No historical evidence writes. This wrapper supplies fresh revalidation before every claim.
- [ ] Add v6-only campaign validation/accounting/claim branches. Accountingschema2 cache prices and original74752/2688 holds, risk2023424/65, ceilings58860/5886 unchanged. Claim schema full-window-campaign-claim.v6 binds new approval/baseline, current130 parent, old32 plus claimed-prefix liabilities. Keep historical1–5 constructors and byte behavior unchanged. Only concrete v6 factory can reach actual campaign; local_stub tests may use existing internal test history interface, not exported capability.
- [ ] Focused unit/race and closed no-provider campaign tests for new declaration, current130 projection, claim permanence/fresh tamper/root overlap and old epoch compatibility. Private actual-history check may use test overlay against read-only original evidence; no DB/API/real claim under private old roots. Save output under .tmp and task report; controller owns final combined fullGo.
- [ ] Commit only task code/tests. Fresh task review afterward; no new actual6 readiness claim.

### Subsequent integration boundary

Task3 creates no actual entry. After its review, controller will specify separate actual6/score6 compatibility units including exact approval in envelope basis, complete R2 state replay and source-backed handling of legal negative states before any freeze. Existing actual5 remains permanently closed. One combined fullGo runs on final selected implementation; independent freeze and authenticated fee/token/zero-inference Langfuse checks precede any new paid scope.


### Task 4: Read-only v6 readiness and current-only actual entry

**Files:** Create internal/taskbench/window_actual_v6.go, window_actual_v6_test.go, window_actual_v6_collector_test.go; minimally modify window_actual_ready.go, window_actual_session.go, window_actual_v2.go, window_actual_v3.go and exact affected old guard tests. No score/Python/customer/routing edits.

**Interfaces:** Consume Task3 concrete v6 factory (v5 signature plus trailing windowStoppedV5Baseline and windowActualFile approval). Add stoppedV5 field to FullWindowActualReady and concrete v6 load/binding/revalidation functions. Existing manifest JSON shape unchanged. Current entry accepts only matching actual-ready.v6/scope.v6; old matching2–5 readiness remains read-only inspectable but OpenFullWindowActual rejects before settings, root creation or network.

- [ ] RED matching/mixed version guards (1–6 cross product), including formerly-current5 with nonexistent settings path to prove early rejection. Keep old matching readiness schema acceptance distinct from current execution acceptance. Update old guard expectations truthfully to v6 and preserve historical constructor tests.
- [ ] Require one and only one exact approval ef6322fe60807a7a608a2707edf016501717f85e9d649e28d0311de2b6ed4bce in Envelope.Basis; no duplicate digest/path or alias. Existing bounded readWindowActualFile and strict JSON evidence validation remain. Bind scope budget SHA, concrete32 descriptorb363 and exact historical32 map; use named current130 wrapper from Task3, not proof.base at historical105, for prepared baseline/current claims. Preserve historical32 proof object unchanged. No top-level JSON field or caller verifier hook.
- [ ] V6 loader fresh-loads concrete32 and approval; validates original predecessor chain and five disjoint canonical roots against new SessionRoot/CampaignRoot. Revalidation checks fresh historical/approval/current130 snapshot before each claim; entire manifest/provider bases are rechecked before startup and before each RunWindow, preserving the inherited gate. The next-window claim may be conservatively reserved at current-window completion before the next RunWindow input gate; no following inference may occur if that gate fails. This does not assert full-input verification immediately before that pre-reservation. No cached proof can authorize changed files.
- [ ] OpenFullWindowActual v6 calls concrete factory with approval from checked envelope; original settings digest, no redirects, authenticated health, dedicated DB, observer capture/release,5minute per-window acquisition and stop policies unchanged. No handler/tool/customer/prompt changes.
- [ ] Closed Handler substitute collector tests prove normal acquisition and quality-negative paths use claimv6/current130/32map/accountingv2; all eight existing quality issue codes remain recorded negatives, real technical unknown/HTTP/usage/observer/cleanup/cancel failure halts with unrun suffix and permanent reservation. Include legitimate failed/structuraltrue/Successfalse fact issue, zero and nonzero quality-end suffixes. Use owned test roots/local substitute only; never old claims or actual vendor requests. Targeted tests may require existing isolated fixture service, controller will coordinate resource setup; no fullGo in author.
- [ ] Focused unit, necessary fixture and race evidence; independently reviewed commit. No new actual scope is frozen merely because this implementation passes.


### Task 5: Pure epoch6 planning, state compatibility and fixed attestation

**Files:** scripts/full1220-window-scores.py, scripts/tests/test_full1220_window_scores.py, new scripts/helpers/full1220_attestation_v6.go.txt. Existing attestation helper/hash and content projection helper bytes remain unchanged.

**Interfaces:** Keep existing positional API/CLI and2–5 provenance byte behavior. Add optional keyword epoch6_evidence=None for raw envelope/quality evidence to pure builders and their wrappers; CLI may read the manifest-bound envelope and receipt-bound sidecars from existing records_dir before calling pure functions. No implicit I/O in the pure planner. Exactmatching6 selects separate fixed attestation helper/hash.

- [ ] RED v6 configuration/plan/attestation bindings: baselineb363, exact32map7f6a, approvalef63, fixedvector30c815,130parent/85m600k/cachev2. Rejectcrossversion and coherentchanges; preserve prior2–5 fixture expectations.
- [ ] Authenticate supplied envelope raw bytes against manifest.envelope.sha256; strict decoded basis requires exactlyone ef63 approval reference. Keep source/manifest/receipt/review/record digests and original1220 requirement derivation, knowledgeoff and trace/release rules unchanged.
- [ ] Shared pure state validator verifies original typed flags/digest/issue origins/errors. Eight continue codes match collection_policy.go; legal late fact_mismatch retains failed/structuraltrue/Successfalse. Unknown/unbound technical state rejects; no row71/79 exception. Replay all168 originalR2records readonly without re-running corrected fact extraction against historical errors.
- [ ] Quality-end source verification is Task6. Pure hash/metadata-linked quality plans may explicitly remain source-replay-pending; if safely enabling those requires the concrete Task6 proof, failclosed now with an explicit error. V6 quality publication stays failclosed untilTask6. Do not claim fullv6readiness or silently substitute selfdeclared verified flags.
- [ ] Propagate epoch6_evidence through CLI/contentprojection wrappers; no changes to old helper/proof provenance or caller-selected helper paths. Capture raw RED/GREEN and final dedicatedPython suite with exactskip counts, minimal fixedattestation integration checks. Commit/review this independently usable pure/continue boundary; report exactquality deferred work.

### Task 6: Concrete owned-quality proof and v6 publication integration

**Files:** new scripts/helpers/full1220_quality_v6_test.go.txt with fixedSHA; scripts/full1220-window-scores.py and scripts/tests/test_full1220_window_scores.py; a small focused helper runner Python file only if reusing existing authenticated-source runner cleanly requires it (explicit controller review first). No productionGo/DB/provider changes.

**Interfaces:** Consume Task5 optional raw epoch6_evidence and shared state validator. Add explicit offline verify_window_quality_v6 API (report exact signature) producing sealed _PublicationProof kind quality_v6; optional quality_proof=None publication parameter preserves all legacy callers. JSON verified flags or arbitrary helper/verifier callbacks cannot mint proof. Actual6 runtime verifier may reuse the same fixed API before a completed-window checkpoint; publication requires it for every v6 owned-quality row. Noquality rows require nohelperrun.

- [ ] Extend raw bundle with exact receipt-named candidate/cleanup bytes. Pure planner checks manifest/receipt/record/rawSHA and sidecar field/state/turn linkage, reports source-replay-pending without pretending it proves Oracle/native/semantic correctness. Enable legitimate zero/nonzero owned suffix plans only with honest pending/proved distinction; all originalnegative flags, failure denominators and passscoresuppression remain.
- [ ] Reuse existing authenticated isolated-source copying and fixed Go runtime/SDK checks. New pinned test helper in package taskbench invokes existing readWindowStoppedV4Quality with exact source policy/slot/row/record and validates original case against frozen definitions, giving existing source rules full candidate/cleanup/SQL-footprint replay. No imports of frozenPython, no frozen-source mutation, no DB/network/provider calls. Validate original-source inventory/Git identity and helper/executable SHA before/after execution; output folder new/unaliased and bounded.
- [ ] Proof binds manifestSHA, receiptSHA, original source inventory, helperSHA and complete ordinal/arm/record/candidate/cleanup/envelope bindings for all owned-quality rows. Only exact helper output from successful process yields sealed proof; wrong/missing/JSONfake/crossreceipt/reused-other-source proof rejects before scorepublication. Existing v2–5 proofs/batchids/client behavior unchanged; v6 batch provenance records the proof binding.
- [ ] CLI explicit publication execution may run fixed offline quality verification using existing runtime/outputdir job data; pure plan remains pure and read-only CLI plan never secretly launches Go. No arbitrary helper path, no implicit model/API call, no new human approval loop.
- [ ] Cover legitimate zero/nonzero suffixes, all source-owned preview origins plus incomplete, missing/altered/crosslinked sidecar, coherently unsafe candidate, cleanup change, wrongsource/slot/arm, invalid helper/runtime output and proof substitution. Existing eight continue origins/latefact/technicalunknown checks remain; full168R2state replay and complete old3windows4claims private verifier replay precede actual freeze. Use real source-built owned fixtures and fixed helper integration, not only mocked booleans; retain all raw logs.
- [ ] Run final dedicatedPython suite once and focused fixedhelperintegration; independent taskreview. Fullsource20mGo and wholebranchreview remain finalcontroller gate. No actual6freeze or paidrun before this unit is complete.

### Controller freeze gates after Tasks3–6

- One current-source fullGo20m after all task reviews, immutable source archive with correct Git metadata, source/Go inventory and same9 services restored. Never substitute prior740 source regression.
- New independent private6 verifier/owner dependencies compile the exact SHA-authenticated bytes, not re-import frozen modules. Replay complete R2 three windows/four claims and source-state fixtures before freeze; original runtime5 guard and poststop-only verifier remain immutable. New6 roots/claims disjoint, one owner, per-window300seconds, failurestop/no retry or resume.
- Fresh authenticated read-only fee/models health, exact token/request envelope and zero-inference Langfuse checks, original customer15Go50e and bothknowledgeoff, cap vector/current130 proof and independent exact freeze review. Only then use existing autonomous model authorization for new independent3660 acquisition; no further routine scope question. PermanentO_EXCL/fsync claims and fullfailure denominator/independent semantic review/Langfuse content+score readback/report/main push remain required.
