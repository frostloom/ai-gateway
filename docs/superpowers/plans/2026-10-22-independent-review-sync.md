# Independent Review Score Synchronization Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans inline. User already approved this bounded task and requested no repeated questions; work only on main.

**Goal:** Publish independently reviewed first-batch results to Langfuse with evidence bindings, then prepare batch-002 offline without paid inference.

**Architecture:** Extend the existing offline Langfuse score tool with optional explicit review and raw-journal inputs. Keep the original deterministic scores and execution artifacts unchanged; publish separate `independent_review_status` and `reviewed_task_success` scores only after validating every requirement and actual exported reply. Continue using the existing local endpoint client, explicit publish flag, receipts, and score readback.

**Tech Stack:** Python standard library, unittest, existing paired-eval Go offline CLI.

**Spec:** User-approved scope in this conversation: sync the independent first-batch semantic review and read it back; batch-002 remains offline. Original review scope and hashes are in `eval/paired-pilots/2026-10-22-pi-first-live-result/independent-review.json`.

## Global Constraints

- Only main; preserve unrelated frontend changes, private env and immutable run records.
- Model/JEV calls stay zero. No claim reset or later paid batch execution.
- Independent review is a trusted local operator artifact, not proof of reviewer identity or a new LLM judge.
- Retain original needs_review/unknown scores as historical execution-time assessment; separate reviewed scores show the completed independent review.

## Review Focus

- Wrong journal/revision or arm cannot attach a review to another run.
- Missing, extra, duplicate or changed requirements/replies cannot produce a passing score.
- Failed/partial/unfinished rows or nonpassing reviews cannot be promoted.
- Wrong remote input/output content must block all writes before the first POST.
- CLI cannot ignore review flags, overwrite evidence, issue inference, or retry failed POSTs.

### Task 1: Bound independent review score synchronization

**Files:** Modify `scripts/langfuse-eval.py`; create `scripts/tests/test_langfuse_review.py`.

**Interfaces:** `build_review_plan(report, review, journal_raw, review_sha256)` returns a normal Langfuse score plan with additional expected input/output hashes on trace bindings. CLI accepts `--review` and `--journal` together with exactly one `--report` for plan/publish/verify only.

- [x] Write failing tests for full two-arm review, distinct stable score IDs, journal/revision/trace/hash/coverage mismatch, failed rows, and remote content mutation.
- [x] Run the focused unittest suite and confirm failures describe the absent behavior.
- [x] Implement bounded parsing, exact requirement matching and reply hashing; keep original build_plan behavior unchanged.
- [x] Require the remote case/arm/content to match before score writes; verify returned values and review evidence metadata.
- [x] Run both Langfuse test modules, then request independent code review and resolve material findings.
- [x] Generate an offline score plan; explicitly publish only to local Langfuse; preserve partial receipts and use separate read-only verification if ingestion is delayed.
- [x] Archive sanitized sync/readback evidence and documentation, verify hashes, commit and push this tested milestone.

### Task 2: Batch-002 offline preparation

**Files:** New versioned preparation result under `eval/paired-pilots/` and documentation.

- [ ] Reuse existing campaign-preflight CLI with pinned definitions/profiles and current committed revision; no private credentials needed.
- [ ] Verify batch-002 has two turns per arm: ambiguous initial request, explicit cash-balance follow-up; keep the frozen case unchanged.
- [ ] Check declared max 7 model attempts / 1 JEV, CNY 0.236544 model reserve / USD 0.002688 JEV reserve. Distinguish reservations from spend.
- [ ] Record allow_paid=false, no provider calls, no checkpoint advancement, and whether a later execution entry exists; do not invent a continuation authorization.
- [ ] Verify source hashes and actual output, commit/push the preparation milestone; retain useful project facts in Hindsight and confirm by readback.
