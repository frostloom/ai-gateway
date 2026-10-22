# Authorized live entry Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans inline. User opted out of questions and requires main.

**Goal:** Provide a fail-closed, explicit first-batch execution entry for the authorized small pilot.

**Architecture:** A pure file-bound readiness validator feeds a one-shot first-batch executor and a small standalone CLI. Reuse campaign/model/JEV guards and observer; preserve offline CLI.

**Tech Stack:** Existing Go dependencies, standard JSON/env parsing and filesystem.

**Spec:** docs/superpowers/specs/2026-10-22-authorized-live-entry-design.md

## Global Constraints

- main only; preserve unrelated changes; explicit authorized caps 6/24/6 and CNY1/USD0.02.
- No balance checks, automatic retries, resume or second-batch execution.
- No paid test calls or private values in Git/logs; billing attestations remain separate from authorization.

## Review Focus

- Tampered later case/budget/model/endpoint rejects before resource creation.
- Missing/unverified evidence cannot reach provider/key loading even with execute requested.
- Fresh path cannot bypass per-batch attempts; failures preserve partial evidence and cannot advance later cases.
- Reporter/shutdown/durable write failures propagate; errors cannot leak credentials.
- Default check does not load env files or create outputs; successful run stays checkpoint_pending.

### Task 1: Pure readiness, first-batch runner and CLI

**Files:** Create internal/taskbench/authorized_entry.go, authorized_entry_test.go, authorized_entry_run.go; cmd/paired-live/main.go and main_test.go; docs/paired-live-entry-2026-10-22.md.

**Interfaces:**
- CheckAuthorizedPilot(authorizationRaw, planRaw, billingRaw, evidenceRaw []byte) (AuthorizedPilot, error), with readiness + stable blockers; validates exact existing scope.
- RunAuthorizedFirstBatch(ctx, newDir, AuthorizedFirstBatchOptions) (AuthorizedFirstBatchReceipt,error). Explicit observer/model/JEV settings and evidence; no credential discovery.
- CLI flags: -authorization, -plan, -billing-evidence, -billing-source, -definitions, -profiles, -revision, -out, -env-file (repeat), -release, -execute. Check is default; execute only first batch.

- [x] Write pure tests for scope/hash tamper, unsupported or missing evidence and endpoint/version mismatch; run missing-symbol RED then implement minimal validator to GREEN.
- [x] Write one-shot real-handler loopback integration tests: all evidence guards, first-case counts, later batches not started, no overwrite, model error stop. Watch RED then implement runner to GREEN.
- [x] Write CLI tests: default check no env access/output and nonready execute no side effects; RED then implement CLI and GREEN.
- [x] Run targeted tests, full Go regression and vet; independent review then fix important findings once with regression tests.
- [x] Run check against actual authorization/unverified evidence only; preserve blocker; update report and retain/readback memory.

Delivery protocol: stage only this milestone, commit on main, normal push after remote ancestry check, verify remote SHA; record outcome in the local execution ledger and final report.

Review fixes: durable authorization claim keyed by canonical scope, independent of output and metadata; sync file and parent directories before fixtures. Validate frozen archives before reading secrets. Require actual VCS-stamped unmodified build revision matching HEAD and reject later runtime/input edits. A dirty or unstamped build cannot execute. Preserve unrelated changes.
