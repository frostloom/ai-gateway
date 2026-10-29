# Evaluation State and Stopped V5 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Correct the evaluator's thousands-separated currency false positive and establish the concrete 32-claim stopped baseline needed by a future complete 1220 comparison.

**Architecture:** Keep the customer, original cases, routing, model parameters and both-off knowledge configuration unchanged. Correct only finite fact extraction for future runs; the consumed R2 source/records/negative states remain immutable. Add a separate concrete stopped-v5 reader using existing bounded raw/native readers and the exact closed R2 evidence, with no paid execution capability.

**Tech Stack:** Existing Go taskbench, standard library, existing Python offline score utilities; no dependencies or deployment.

**Spec:** `docs/full1220-v5-r2-stopped-2026-10-29.md`; exact private evidence at `.tmp/paired-live-private/full1220-actual-v5-r2-freeze-20261029/`.

## Global Constraints

- Original 1220 × 3 = 3660 pairs / 7320 arms / 11772 defined turns; 1851 boundaries separate.
- Customer50e/v5, both knowledge off; no original source-case/rubric or reply-repair/model-parameter changes.
- Parent ¥105+$1; 32 permanent claims42961430µCNY/344493µUSD, original risk2023424/65 and old held74752/2688 retained. No new paid scope/claim/inference in this plan.
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
- [ ] Implement bounded concrete reader with previous28 fresh before/after, exact campaign/records inventories, schema5/131schedule/old28map/105 bindings, per-window original accountingschema2 replay and every original native descriptor/body SHA/usage. Sum 391 packets and168 records, require the observed R2 zero quality-end sidecar set in all3 windows (negative-arm counts3/0/2), leave ancestor P4 quality-end validation unchanged, and bind the two exact structurally-valid fact failures71/79 without promoting status or Success.
- [ ] Preserve the exact two previous summaries and summary3.closed=false; require terminal.closed=true/acquisition_done=false/exit0/relaunchfalse and fourth claim permanently reserved with no descendants.
- [ ] Run pure unit/race tests; run one readonly actual bundle check and compare original explicit evidence inventories before/after. No new campaign constructor, no paid owner, no API/DB call.
- [ ] Independent review; commit only task files/descriptor public hashes/docs. Do not claim actual6 readiness.

### Final combined validation and next decision

Controller-owned after both task reviews; task implementers do not launch this combined suite.

- [ ] Run original `go test ./... -count=1 -timeout=20m -json` once on the final selected source, restore the same owned9 services/identities and retain failures if any. This run does not authorize inference.
- [ ] Before any future runtime verifier is frozen, replay R2 complete3windows/4claims and all eight original quality issue codes, legitimate structuraltrue fact failures, zero/nonzero quality suffixes and technical unknowns. Positive state may never be obtained by rewriting original errors/status or removing evidence.
- [ ] Review complete-scope budget using all current bounds and declared conditional admission, not an unsupported worst-case-completion guarantee or linear extrapolation of84pairs. No automatic expansion, no shrinking/splicing3660 and no new paid freeze merely because caps can be made smaller.
