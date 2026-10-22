# Local campaign rehearsal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rehearse the reviewed six-case campaign sequentially through real handlers and existing guards without external inference.

**Architecture:** A closed local runner owns an in-process Anthropic transport, FakeJEV and loopback OTLP capture. It reuses PrepareCampaignProposal validation and RunCampaignBatch, writes replay reports and a receipt, and stops before later batches on any execution or evidence error.

**Tech Stack:** Existing Go/taskbench/observability/OTLP dependencies only.

**Spec:** docs/superpowers/specs/2026-10-22-local-campaign-rehearsal-design.md

## Global Constraints

- Only main; preserve unrelated changes. User opted out of questions and authorized incremental commit/push.
- No paid CLI, credentials, arbitrary provider factories, or environment-driven provider setup.
- Public LocalCampaignOptions contains DefinitionsDir, Environment, Revision, Plan only; caller owns fixture cleanup.
- Receipt schema local-campaign-rehearsal.v1, evidence local_stub; paid/readiness false; zero external model/JEV calls.
- Synthetic usage and declared CNY/USD reservations are local evidence, not invoices or verified token bounds.
- Fresh output directory; preserve all planned batches and partial evidence on failure, never resume/retry automatically.

## Review Focus

- Invalid later batches must reject before output creation or any execution.
- Interrupted, budget-exhausted or evidence-failed batches must prevent every later reservation.
- Receipt writes/cleanup errors must propagate and cannot produce a successful return.
- OTLP counts must match each batch's replayed task/turn/model attempts, including associations.
- A fact mismatch or pending semantic review must not become a model quality success claim.

### Task 1: Closed local campaign runner and six-case integration rehearsal

**Files:**
- Create internal/taskbench/campaign_rehearsal.go: LocalCampaignOptions, LocalCampaignReceipt, RunLocalCampaign and sequential execution/readback.
- Create internal/taskbench/campaign_rehearsal_dependencies.go: private synthetic transport/FakeJEV scripts/local OTLP collector.
- Create internal/taskbench/campaign_rehearsal_test.go: real six-case run, invalid scope/no overwrite, budget stop and dependency fault coverage.
- Create docs/paired-local-campaign-rehearsal-2026-10-22.md with invocation and evidence boundaries.

**Interfaces:**
- RunLocalCampaign(ctx context.Context, dir string, o LocalCampaignOptions) (LocalCampaignReceipt, error).
- LocalCampaignOptions: DefinitionsDir string, Environment *PairedFixtureEnvironment, Revision string, Plan CampaignPreparationPlan.
- Receipt includes all ordered batch IDs/keys/status, replay reports, completed count, failed batch/stage and local task/turn/generation and model/FakeJEV counts.
- Private dependency construction may be replaced in package tests to reproduce provider/observer failures; no exported fault knobs.

- [x] Write tests: six planned batches produce six passed-local checkpoints, 12 arms/26 turns/20 model attempts/6 JEV, independent fixtures, all reports need semantic review; existing dir and invalid later case reject without execution.
- [x] Run `SDKROOT=/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk go test ./internal/taskbench -run TestLocalCampaign -count=1`; observe missing-function RED.
- [x] Implement closed dependencies and sequential runner; rerun to GREEN.
- [x] Add tests for second-batch MaxCalls=1 and private dependency failures (model HTTP/usage, JEV, observer), fact mismatch readback; confirm RED for each missing stop/gate before minimal fix.
- [x] Execute the six-case rehearsal with persistent scratch evidence via test opt-in; verify ordered reserve/finish, original plan denominator, report hashes, permissions and no credentials.
- [x] Run `go test ./...` (SDKROOT as above, external diagnostic opt-ins disabled), `go vet ./internal/taskbench ./cmd/paired-eval`, whitespace/diff checks and independent review.
- [ ] Update report and plan checkboxes; retain/read back durable Hindsight conclusion. Stage only this milestone, commit on main, fetch/push normally and verify remote SHA.
