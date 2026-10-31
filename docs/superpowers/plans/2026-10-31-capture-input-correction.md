# Capture-time Tool Input Correction Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (- [ ]) syntax for tracking.

**Goal:** Correct the offline tool-input projection and finish the first publication of the unconsumed H40 score IDs without changing the acquisition or repeating any paid request.

**Architecture:** A new test-only Go capture template and independently signed supplemental catalog preserve the original production snapshot. A new passive consumer retains the original factories and exact validators. A narrowly bound H40 completion operator replays the original seven GETs, fetches only the other three traces, and reaches the unchanged once registry only after complete validation.

**Tech Stack:** Existing Go 1.25.12 / SDK26.5, Python 3.12.15, original owned helper build/run and Ed25519 catalog signer; no new dependencies.

**Spec:** docs/superpowers/specs/2026-10-31-capture-input-correction-design.md

## Global Constraints

- Work on main as explicitly required by the user and AGENTS.md; preserve unrelated web/dist, integrations and plate_sim.py changes.
- Never modify the read-only R5 snapshot, original helper runtime/catalog, manifest, scope, binary, financial ledger, owner, old publisher, failed output or original GET bytes.
- No model/JEV call, financial claim, deployment, first observation GET or score POST during implementation/tests. Agents may compile/run isolated Go helpers with synthetic fixture keys. Root alone performs later actual-key helper runs, real catalog signing and activation steps.
- User explicitly opted out of stepwise confirmation; make reviewable engineering decisions within this goal and record them rather than requesting another design approval.
- Actual entry points default to UNFROZEN/inactive. No proof from JSON, comparator relaxation, subset matching, dynamic publication-root option or silent fallback to an untrusted helper.
- Keep all original failures, unknown barriers, scores and reservations. H40 remains idle until its corrected external validation and first score publication/readback close.

## Review Focus

- Preview mutates plan arguments: test the pre-call map against a literal name-only expected input, not the same projection builder twice.
- Other phases and tools: preserve execute resolved arguments, cancel {}, local amount conversion and absent/default/clamped bill limits without injecting business-derived fields.
- Forged or stale helper/proof: reject wrong signature, old-catalog/new-binary combinations and source/runtime changes before key delivery. A helper that already started may have read stdin before cleanup fails; failed/unknown cleanup must prevent proof and all later network IO, without falsely claiming no key delivery.
- Consumed or uncertain publication identity: any batch/intent/sent/ACK conflict rejects completion before GET/POST; uncertain POST cannot become another POST.
- Replay substitution: wrong trace/path/status/SHA, partial body, missing seventh response, an eighth replay, or a repeated fetch of a replayed trace must fail closed.

## Task 1: Corrected helper and a real regression

**Files:**
- Create eval/behavior-review/helpers/full1220_content_projection_input_v1_test.go.txt.
- Create eval/behavior-review/test_full1220_capture_input_projection.py and its narrowly scoped fixtures if needed.
- Do not edit the original full1220_content_projection_v8_test.go.txt.

**Interfaces:** The new template implements the existing full1220 projection job/output contract and receipt format. Its SHA is a new artifact identity. It reads original recording bytes; successful preview selects the persisted call-time args, while read/execute preserve the original resolved-args behavior.

- [ ] Write a literal regression for original trace.args name-only versus resolved_args containing plan_id 2. Run the old real Go helper and retain the expected assertion failure for preview input.
- [ ] Add phase/shape/failure tests from Review Focus, including explicit ID and canonicalized name mutation. Use independently derived literal values and the frozen producer's documented parsing boundary. Fail closed on evidence the producer cannot faithfully reconstruct.
- [ ] Implement only the argument-source selection change and required evidence checks in the new template. Keep original content() and validators unchanged.
- [ ] Run the helper tests with the real Go content/projection path using a synthetic in-memory HashKey in an isolated runtime. No provider, database, Langfuse or R5 settings access.
- [ ] Report exact RED/GREEN commands, raw logs, file SHAs and any remaining scope limits. Independently review, then commit/push only this tested helper unit.

## Task 2: Authenticated supplemental capture component

**Files:**
- Create eval/behavior-review/full1220-capture-input-v1.py.
- Create eval/behavior-review/test_full1220_capture_input_v1.py.
- Create supplemental prebuild/activation artifacts only under .tmp/v9-live-score-horizon-20261031/capture-input-v1/.

**Interfaces:** Provide a passive renderer/loader and corrected project_window_content API matching the current consumer call contract. The generated fresh consumer must use the unchanged historical issuer, its own module-local projection seals, original source factories and original once validators. An explicit correction binding pins the original manifest/binding/base consumer plus the new helper template, catalog, executable, runtime inventory and process authority.

- [ ] Write rejection tests for missing/unfrozen bindings, forged projection dictionaries, changed source/runtime, wrong catalog signature and old catalog paired with the new helper. Assert rejection before the delayed key callback. Separately test failed/unknown cleanup after child start: no proof and no network; do not assert the already-started helper never received stdin.
- [ ] Reuse the existing read-only source -> isolated runtime -> two helper builds -> catalog.message -> technical signer pipeline. Source helper text stays unchanged; the catalog truthfully records the new capture template/generated source/executable and exact runtime map. Do not mutate or impersonate catalog b9f28123.
- [ ] Implement the new capture path and fresh consumer derivation. Do not monkey-patch the old live module, modify the old f7e/10fc files, or revive an opaque proof from saved projection JSON. Preserve exact comparator behavior and all native/parent/content-switch/registry checks.
- [ ] Exercise the real helper build/run with synthetic keys and inputs. Run the complete new component test suite plus the relevant existing helper/runtime, historical consumer/operator and score suites. No production Go changes or deployment; do not claim a new full-Go regression.
- [ ] Report reproducible build commands and new pin placeholders. Root performs the real catalog signing and actual-key offline replay only after independent code review.

## Task 3: Singleton first-publication completion and future normal entry

**Files:**
- Create eval/behavior-review/full1220-capture-input-operator.py.
- Create eval/behavior-review/test_full1220_capture_input_operator.py.
- Render fixed activation jobs/output namespaces under the new private capture-input-v1 directory, never the old t40 operation directory.

**Interfaces:** The H40 completion job binds the original operation-error/plan/publish-intent SHA, exact 28 original score IDs, exact seven GET triples and fixed target/summary/horizon. The replay opener returns only those original bodies for their original observation requests and delegates only the three unrequested trace GETs once. Score verification GETs retain the original client behavior. Future normal once-publication mode uses the same corrected capture path, fresh proofs and original registry/IDs, with no recovery semantics for already attempted windows.

- [ ] Test original registry batch, any logical/score intent, sent marker or ACK conflict; each blocks all completion IO. Both locked absence checks use no-follow directory-entry inspection, so dangling symlinks count as consumed/conflicting entries. Test wrong/partial/substituted replay responses and repeated/missing traces with literal URL/trace/status/SHA expectations.
- [ ] Implement one fixed H40 completion namespace and original-scope locking. Make an initial locked check of the old failure and empty candidate registry before helper/key work, then release that check lock before building the helper proof. The original publisher reacquires its own lock and rechecks emptiness before any observation read; it retains the lock through batch/intents/POST. Do not nest two acquisitions of the same lock. Immediately before POST validate this operation's newly created batch/intents and absence of prior sent/ACK markers, rather than requiring the now-reserved registry to remain empty.
- [ ] Reuse the seven original GETs without new fetches; permit only the other three original trace observation GETs once. Ten exact content/native/parent validations precede any durable score intent. Preserve batch -> logical intents -> sent-before-POST -> ACK order, exact original IDs and suppression rules.
- [ ] Test an uncertain POST and a second completion invocation: retained evidence, no resend. Test a content mismatch remains a rejection. Default invocation remains passive/UNFROZEN.
- [ ] Test switching completion/normal mode, output namespace or legacy/new publisher paths cannot bypass the once barrier. Completion is fixed to H40 and its original failure; normal mode is restricted to future targets and rejects any existing old or new publication operation for that target.
- [ ] Run the entire new operator suite and relevant existing publisher/consumer suites. Independently review this task and the complete change before any actual activation.

## Task 4: Root-only offline evidence, freeze and bounded completion

**Files:** New immutable evidence/bindings under the private correction directory; docs/full1220-v9-live-progress-2026-10-31.md changes only after truthful closure.

- [ ] Root builds/signs the supplemental catalog with the existing technical signer, pins source/runtime/template/executable/catalog and rechecks original R5 source, binary, ledger and all raw evidence unchanged. HashKey is delivered only by the original checked delayed stdin mechanism.
- [ ] Run a genuine corrected capture on the H40 original records and strictly compare the first seven stored GETs offline. Verify the name-only preview and resolved execute inputs without changing either side. Compare prior successful delivered rows using bounded genuine helper replay; retain original and new receipts as distinct artifacts, never actual permissions from diagnostics.
- [ ] Independently freeze the exact correction/operation binding, original failure linkage, score IDs, seven replay responses, remaining three traces and permanent once barriers. Keep activation false until all checks pass.
- [ ] Under the existing human autonomous-delivery authorization, activate the single first-publication completion only if every original candidate remains unconsumed. No old operation reentry, repeated original GET, model/JEV request, financial claim or scope reset.
- [ ] Verify all first score POST ACKs and score GET readback. Independently audit the real suppression holes and complete H40 counts; then send H41 once to the original owner using the verified H40 summary, update/push the report and Hindsight, and continue the original complete evaluation.

## Controller rulings

- Main-only development and no new worktree follow the user's explicit instruction. Build isolation is a separate private runtime; user changes remain untouched.
- Existing source/binary/financial authorization stay frozen. A separately authenticated verifier artifact corrects interpretation of original evidence; it does not create a new paid experiment.
- Old operation failure remains permanent. A newly frozen entry may perform the first POST of proven uncreated score IDs; any conflicting or uncertain intent changes the path to verification/hold only. This interpretation is independently documented in ce08f4bd... and must not become a generic retry feature.
- Keep the plan workspace and original logs where needed for the evaluation's immutable evidence requirement; do not run cleanup that deletes them.
