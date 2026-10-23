# Recharge confirmation controlled entry implementation plan

> **For agentic workers:** Use superpowers:executing-plans; implement in this session on main.

**Goal:** Prepare one fixed recharge-002/confirm real-evaluation scope, without executing paid inference.

**Architecture:** Extend the existing independent-scope checker/runner with a fixed recharge identity and distinct schemas. Reuse immutable input verification, billing checks and permanent claim; bind deployed customer source/contract, offline acceptance and approved cumulative budget by SHA256. Reserve only this scope's 10 model attempts and 1 JEV; no generic campaign expansion.

**Tech Stack:** Go, existing paired-live and paired-eval commands, JSON evidence.

**Spec:** Agreed chat scope and docs/recharge-confirm-fix-offline-2026-10-23.md; deployment and approved ceiling in docs/customer-recharge-boundary-deployment-2026-10-23.md.

## Global constraints

Main only; preserve unrelated changes and old evidence. Ceiling ¥100+$1 is approved, exact new execution remains disabled in examples. One fixed claim survives failure and changed output/revision. Billing evidence stays false until actually verified. No model/JEV calls, service redeployment or balance checks in this goal.

## Review focus

- Legacy authorization and billing cannot be rebound to recharge.
- A different case, larger cap, budget digest or offline evidence fails validation.
- Default check and rejected execute read no credentials or create output/claim.
- Changed output/revision cannot reacquire a consumed fixed claim.
- Runner receipt must retain budget/offline provenance and finite reservations.

### Task 1: Fixed scope and offline preparation

Files: internal/taskbench/authorized_retest.go, authorized_recharge.go (new), authorized_entry.go, authorized_entry_run.go, campaign_prepare.go; cmd/paired-live/retest.go and help; new tests in internal/taskbench and cmd/paired-live; reuse strict offline reader in cmd/paired-eval.

Interfaces: CheckAuthorizedRetest returns the existing private AuthorizedPilot; RunAuthorizedRetest consumes it. New fixed ID recharge-confirm-20261023, distinct recharge-confirm-authorization.v1 / recharge-confirm-billing-attestation.v1 / authorized-recharge-confirm.v1. Public provenance accessor supplies budget/offline digests for durable claim and receipt. Limits 10 models/337920 microCNY, 1 JEV/2688 microUSD; 128 output tokens, 16384 request bytes and input-token ceiling.

- [x] Write acceptance/rejection, frozen input, no-credential CLI, claim and local-handler runner tests.
- [x] Observe new scope rejected before implementation (RED).
- [x] Implement minimal fixed scope and provenance binding, preserving old contracts.
- [x] Pass targeted tests, offline CLI preparation and full Go suite (GREEN).
- [x] Independently review; fix important findings; verify archival hashes.
- [ ] Commit precise files, normally push and verify remote SHA; update Hindsight and read back.

Artifacts: new eval/paired-pilots/2026-10-23-recharge-confirm-entry with plan, authorization/billing false examples and derived preparation; document limitations and remaining execution binding. Budget record itself remains immutable. Future scopes must account for permanent reservation; this entry exposes only this one scope under the new ceiling.

Execution ledger: RED observed for missing fixed scope and legacy billing mixing; targeted Handler/fixture complete and both failure paths passed; full Go 18 packages passed, cleanup inventory 0. Independent review and supplemental artifact review found no important issues. Commit/push and final committed archive binding follow verification.
