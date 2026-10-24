# Repeated v5 evaluation implementation plan

> Implement inline using executing-plans; final fresh independent review. This document is an offline proposal, no paid execution.

**Goal:** Bind reproducible reversed arm order and 24 explicit repeated pairs without widening historical authorizations.
**Architecture:** Add optional explicit arm order to hashed pair configuration, then a separately checked repeated scope and permanent claim; aggregate observations without changing original reports.
**Tech Stack:** Existing Go taskbench/paired-live, JSON evidence and Python local Langfuse tooling.
**Spec:** docs/superpowers/specs/2026-10-24-repeated-v5-evaluation-design.md

## Global constraints

main only; preserve unrelated web changes. No new claim/paid calls/deployment during implementation. Customer50e3c8d/v5 unchanged; original six cases,128output; 4 blocks/24 pairs/48 arms/104 turns, candidate248model24JEV. Keep 5-minute whole-command deadline, failure-stop, full permanent reservation and original historical scopes. Freeze final source and current fee/token evidence before seeking new precise paid approval.

## Review focus

- Empty/duplicate/unknown explicit arm orders reject; omission preserves legacy ordering and legacy contract bytes.
- Altered schedule/order/caps with coherently recomputed digests still reject fixed-scope checking.
- Identical case across blocks never collides in output, traces, scoring or budget; no fixture/session reuse.
- First failed reversed arm remains durable and prevents second arm; unknown usage never becomes zero cost.
- Partial/quality-failed pairs remain visible and unrun suffix is never treated as successful or free.

### Task 1: Bind explicit pair order

Files: internal/taskbench/paired_runner.go, paired_runner_test.go, paired_adapter integration tests.
Interface: add `ArmOrder []string` with json `arm_order,omitempty` to PairRunConfig; nil retains historical pairOrder parity; explicit order is exactly model/jev_selective in either permutation and applies to every case in that config. No routing-name swaps. PairRunContract includes explicit order.

- [x] RED tests: forward/reversed actual factory and persisted start order; empty non-nil/duplicate/unknown/single entries reject before files; nil old configuration serialization and contract unchanged; changed order cannot resume journal or reuse budget binding.
- [x] Implement minimal validation/order selection, preserving omitted legacy behavior.
- [x] GREEN targeted tests and real Handler local reverse pair; first-arm cleanup/provider/JEV failure leaves one durable failed arm and second unrun. Run related taskbench/CLI tests; independent review before commit/push this unit.

### Task 2: Separate repeated scope and ledger

Files: new internal/taskbench/authorized_repeated.go, authorized_repeated_run.go and tests; new cmd/paired-live/repeated.go/tests; explicit CLI route in main.go. Reuse pure scope checker/owned fixture/guard machinery without registering this identity as a historical scope.
Interfaces: CheckAuthorizedRepeated(authRaw,planRaw,billingRaw,evidenceRaw []byte)(AuthorizedPilot,error), RunAuthorizedRepeated(ctx,dir,AuthorizedFirstBatchOptions)(AuthorizedFirstBatchReceipt,error); private discriminator checked by every entry path. Derive fixed schedule as defined in spec; proposal pair identity is distinct from original case_key. Expose discriminator only after validation.

- [ ] Freeze separate scope identity and auth/billing/receipt schemas, fixed schedule/block/pair identities,248/24 caps and six prior claim SHAs. No public true authorization. Do not reuse the six-batch cohort validator or broaden its constants.
- [ ] RED tests for all cross-entry attempts, coherent scope expansion, schedule/order/duplicate pair mutation, six missing/tampered predecessors, replay under different output/revision, full248/24 permanent allocation versus started prefix.
- [ ] Implement fixed checker/claim and runner, preserving old schemas/config contracts. Explicit release/observer metadata includes block/pair identity; original single-pair scoring remains valid because journals and trace IDs are unique.
- [ ] GREEN local captures normal/repair all104turns, both orders twice per case, independent fixtures; reverse-first provider/JEV/structural/cleanup failures, missingusage, observer loss, receipt failure and deadline stop preserve prefix and suffix. Original definitions/agent sources unchanged.
- [ ] Standard SDKROOT MacOSX26.5.sdk `go test ./... -count=1 -json`, independent review and exact commit/push. Preserve any failures; full final source pass required before readiness.

### Task 3: Offline repeated-result aggregation and readiness

Files: new scripts/repeated-v5-summary.py and focused tests; docs and false eval archive for the newly frozen scope.
Interface: read-only explicit schedule, original journals/reports, independent reviews and immutable receipts; output a new summary directory, never mutate input. No env discovery/network/model/JEV calls.

- [ ] RED synthetic fixtures: all24 pairs; reversed order; duplicate/missing trace/pair; version/SHA mismatch; null usage; stopped/failed/unrun pair; independent review mismatch; exact CNY/USD and four clock sums. Reject mislabeled clocks, never infer absent values as0.
- [ ] Implement per-pair selective−model differences, case/order stratification, quality table and descriptive median/range only for complete independently passed pairs; retain all other statuses/costs and exclusion reasons. No statistical or population benefit assertion.
- [ ] GREEN aggregator tests and offline replay original cohort via explicit mapping only as compatibility smoke (one block, not fabricated repeats); current104turn requests/token envelopes captured with stubs, all raw SHA bound. Separate immutable original scores and new summaries.
- [ ] Refresh primary public pricing and read-only settings health; freeze final executor provenance after commit/push and false authorization/billing materials. Hindsight durable facts with successful readback. New exact paid scope approval is last gate, never automatically execute on this plan.
