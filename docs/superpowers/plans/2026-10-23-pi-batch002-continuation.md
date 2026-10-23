# Pi Batch Two Continuation Implementation Plan

> **For agentic workers:** Use executing-plans inline. User approved the four-step scope in chat; remain on main.

**Goal:** Resume only batch-002 from a reviewed batch-001 checkpoint, with no live inference in this implementation session.

**Architecture:** Reopen the original campaign ledger with its original binding, preserving full permanent reservations. Validate immutable checkpoint evidence before credentials and fixtures, and require separate explicit continuation permission plus a durable per-batch claim before execution. New output contains a separate receipt; original evidence stays intact.

**Tech Stack:** Go taskbench, paired-live CLI, local fake providers.

**Spec:** docs/pi-batch002-offline-2026-10-22.md and approved scope in chat.

## Global Constraints

- main only; preserve unrelated dirty files; no paid model/JEV calls.
- Only batch-002; stop at checkpoint_pending, no automatic retries.
- Preserve original claim and campaign reservations; no budget refunds.
- Read-only check must not load env or create files.

## Review Focus

- Changed or incomplete review, receipt, journal or cleanup outcome rejects before execution.
- Wrong plan, authorization, billing or revision binding rejects.
- Interrupted, failed, already reserved campaign rejects without new provider calls.
- Different output paths cannot repeat batch-002.
- Existing first-batch behavior remains compatible.

### Task 1: Checkpoint evidence and continuation runner

Files: internal/taskbench/authorized_continuation.go, authorized_continuation_test.go, authorized_entry_run.go.
- [x] Add failing tests for evidence validation and single-batch continuation using real handler/fixture and fake external providers.
- [x] Implement validated private continuation snapshot, revalidate source evidence, reuse original campaign and select index 1 only. Record previous actual usage separately from permanent campaign reservations.
- [x] Run taskbench regression.

### Task 2: CLI and durable claim

Files: cmd/paired-live/main.go, continuation.go, continuation_test.go.
- [x] Add failing tests for read-only check, explicit permission binding and duplicate claim rejection.
- [x] Add continue-from/review/continuation-authorization flags, validate before env/fixtures; verify original claim output and exclusively claim batch-002.
- [x] Run CLI regression and full Go suite.

### Task 3: Review and delivery

- [x] Independent code review, fix important findings and verify again.
- [x] Document commands, limitations, zero inference; commit only related files and push main, verify remote SHA.

## Decisions

Explicit continuation permission is a separate operator JSON bound to plan, original receipt and review hashes. This implementation does not create that permission for the user. Historical billing policy must match the original checkpoint. Original campaign binding retains original execution revision; new child binds the current reviewed revision.

## Execution evidence

Targeted handler/fixture tests passed for success, provider failure, replay rejection and tampered sidecars/markers. Independent review fixes received RED→GREEN coverage. Archived real first-batch evidence passes read-only check with two model and one JEV prior attempts. Full-suite final result is recorded in the delivery document; no live inference.
