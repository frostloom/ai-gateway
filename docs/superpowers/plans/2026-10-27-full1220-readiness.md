# Full 1220 Readiness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Produce executable offline preparation and a source-bound, per-case coverage/budget audit for all 1220 source tasks, without confusing preparation with actual evaluation.

**Architecture:** Reuse the frozen definition loader and original primary-scenario priority in Go. A new offline CLI validates per-case pair configuration through the existing real preparation API and exposes the existing pilot whitelist gap; a Python audit verifies the emitted identities and computes integer reservation totals against explicit historical claim evidence.

**Tech Stack:** Existing Go taskbench and Python standard library only.

**Spec:** docs/customer-service-evaluation-contract-2026-10-09.md; docs/customer-service-full-preflight-2026-10-14.md; user's 2026-10-27 correction that final delivery is 1220-source V1/V2 comparison, not six-case/24-pair pilot completion.

## Global Constraints

- Direct main development/push, preserve unrelated web assets and all prior immutable archives/claims.
- Offline only: no credentials, .env, database, network, provider construction for inference, paid execution or claim creation.
- Exactly 1220 unique primary source IDs, original priority answer_complete / explicit_scope_followup / supplement_then_confirm / confirm / no_subscription; 1851 non-primary complete boundaries separate.
- 2440 arms/3924 defined turns per pass are coverage, not success; formal contract requires at least three interleaved passes, reported separately from one-pass budget.
- Existing 128 output / five model iterations / safety, timeout and permanent-reservation policies unchanged; current remaining authorized ceiling derives from consumed claim evidence, not actual lower usage.
- Public prepare ready_for_paid_run and allow_paid remain false. Existing pilot modes and whitelist are not broadened.
- Keep historical price evidence dated; do not claim fresh tariff verification or request envelope proof for the full set.

## Review Focus

- Duplicate/missing sources or mutated primary metadata must reject instead of silently reducing 1220.
- Pilot-supported cases must not imply full execution readiness; configuration construction is weaker than real Handler execution.
- Full schedule retains failures/unrun cases and separate boundary denominator.
- Budget calculation must use integer arithmetic, separate CNY/USD, include three-pass contract, and not recycle consumed reservations.
- CLI must not overwrite outputs or accept execution/provider/credential switches.

### Task 1: Canonical full preparation and offline Go CLI

**Files:** Create internal/taskbench/full_prepare.go and full_prepare_test.go; cmd/full-customer-preflight/main.go and main_test.go.

**Interfaces:** `PrepareFullCustomerPair(ConversationSuite) (FullCustomerPairPlan, error)` returns detached full primary suite and original-order boundary keys. `run([]string) int` exposes only explicit definitions/profiles/output paths; uses `PrepareAgentPairConfig` with public target, disabled transport and one primary case at a time. Each row records key, profile, turns, config contract and existing pilot-whitelist membership, with all paid flags false.

- [x] RED tests assert exact IDs/counts/1962 source turns/1851 boundaries, deterministic selection and detached mutation; reject duplicate/missing primary.
- [x] Implement minimal full selector, preserving all prior APIs.
- [x] RED CLI tests for 1220 prepared configurations and coverage counts against actual PrepareSmallPair, non-overwrite and forbidden execution/secret switches.
- [x] Implement offline CLI; targeted tests then original full Go regression -timeout=20m.

### Task 2: Verified full audit and budget gap delivery

**Files:** Create scripts/audit-full-customer-readiness.py, scripts/tests/test_full_customer_readiness.py, docs/full1220-readiness-2026-10-27.md, eval/paired-pilots/2026-10-27-full1220-readiness/.

**Interfaces:** `audit(root: Path, probe: dict, reservation: dict) -> dict` independently matches all rows to frozen source selection, records source/preparation-only capability boundaries and computes one-/three-pass integer ceilings. Reservation input is an explicit SHA-bound consumed claim, never inferred from remaining account balance. `--probe`, `--reservation`, `--out` create a fresh directory only.

- [x] RED test all IDs and exact scenario/profile/turn identity; reject duplicates, altered counts and false ready flags.
- [x] RED test historical declared per-request 33792 microCNY and 2688 microUSD: one pass 19620/1962 calls, 662999040/5273856 reservation; three passes triple. Remaining current ceiling 70938880/776896 cannot fit these envelopes.
- [x] Implement audit with dated evidence, no tariff/envelope or paid-ready claims.
- [x] Run full Python suite, inspect exact Go probe and archive SHA manifest, independent whole-change review, update AGENTS/Hindsight final target correction, commit/push.

## Execution rulings and progress

Ruling: User opted out of repeated questions and explicitly chose autonomous direct-main work; execute inline without another plan approval or worktree.
Ruling: This plan delivers a necessary full-set preparation/audit, not the final real 1220 evaluation. Do not mark the overall requirement completed when this plan passes.

Verification: final Go19 packages/512.514s; Python195/15 original skips. Initial Kafka failure and two review P2 RED→GREEN retained in archive. All real inference/claim/deployment counts remain zero for this plan. Final1220 comparison remains incomplete.
