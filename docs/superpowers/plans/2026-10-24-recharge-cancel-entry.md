# Recharge Cancellation Entry Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Add a fixed independent cancellation scope without paid execution.

**Architecture:** Reuse AuthorizedPilot, existing retest runner and durable exclusive claims. Extend only fixed identity dispatch and predecessor checks; keep runtime Handler unchanged.

**Tech Stack:** Go, JSON, existing MySQL isolated fixture and local transport substitutes.

**Spec:** docs/superpowers/specs/2026-10-24-recharge-cancel-entry-design.md

## Global Constraints

- main only, no new worktree; preserve unrelated changes.
- 10 model/337920 micro CNY +1 JEV/2688 micro USD; no paid calls or deployment.
- Exact existing confirm and diagnostic claims required; permanent reservations retained.
- False authorization/billing examples; no secret or raw live evidence committed.

## Review Focus

- Rebinding confirmation authorization/billing to cancel must fail.
- Changing plan case or expanding allowance, even with updated digest, must fail.
- Removing either old claim must not create a new claim.
- Changed output/revision after a failure must not permit a second claim.
- Provider/JEV errors must halt; cancellation must leave business state unchanged.

### Task 1: Fixed scope, artifacts and durable allocation

**Files:** Create internal/taskbench/authorized_recharge_cancel.go and corresponding tests, cmd/paired-live/recharge_cancel_test.go, eval/paired-pilots/2026-10-24-recharge-cancel-entry/ and docs/recharge-cancel-entry-2026-10-24.md. Modify campaign_prepare.go, authorized_entry.go and cmd/paired-live/retest.go.

**Interfaces:** Consume `CheckAuthorizedRetest`, `RunAuthorizedRetest`, `persistRetestClaim`; produce the fixed cancel identity accepted by the same interfaces.

- [x] Create false examples and tests for accepted synthetic-only authorization, rejecting mutated scope/budget/evidence and old schemas; CLI default/execute refusal and two-predecessor durable claim tests.
- [x] Run `go test ./internal/taskbench ./cmd/paired-live -run 'TestCancelRecharge' -count=1`; expected fail on unknown new scope.
- [x] Add minimal dispatch, billing schema and claim predecessor checks; run targeted tests, expected pass.
- [x] Add real Handler runner integration for complete, provider-failure and JEV-failure; inspect cancel audit/state and no retries; run targeted tests, expected pass.
- [x] Run `go test ./... -count=1` with process SDKROOT MacOSX26.5.sdk; report actual failures without hiding them.
- [x] Independent review, archive verification and evidence integrity; commit exact files, fetch/check remote, push normally, verify remote SHA. Record durable project facts in Hindsight and actually recall.

## Execution record

Target scope RED→GREEN and final focused regression passed. Initial full suite:16 packages passed, store concurrent reserve failure and taskbench package10m timeout. Bounded rechecks:store failure persists; timed taskbench test passes. Full suite gate remains blocked; no production lock/config changes. Reviewer matching-digest budget-expansion gap addressed and reverified. Exact owned orphan fixture dropped, final residual0;2780 old local files unchanged. New paid claim absent. Integration/paid readiness explicitly withheld pending regression diagnosis.

Subsequent goal-mode iteration: unchanged code passed a fresh standard full18-package regression after diagnostic measurements; original failures retained, cause unconfirmed. Current execution preparation and limits are in docs/recharge-cancel-execution-preparation-2026-10-24.md; do not interpret the historical blocked state above as the current regression gate.
