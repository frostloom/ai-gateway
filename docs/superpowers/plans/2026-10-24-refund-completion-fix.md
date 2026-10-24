# Finite refund completion repair Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans inline on main, per user instruction.

**Goal:** Repair unsupported leading refund-success replies for the frozen explicit own-account refund application before paid evaluation.
**Architecture:** Add finite whole-string refund application matching to existing replyRepairReason; reuse at-most-one correction/honest exhaustion. No automatic tool choice or write. Bump runtime contract and reject historical v4 supplement scope rather than rebind its authorization.
**Tech Stack:** Go / real Handler fixture / local model and FakeJEV.
**Spec:** docs/refund-cap-offline-2026-10-24.md blocking finding, preserved at6a3273c.

## Global Constraints

- Match ordinary explicit退款/退钱 and the frozen complete own-account100yuan refund application with optional preview suffix. Exclude negation, other owner, history/quotation/condition, consultation, token units, unknown suffix, refund-to-balance/withdrawal and compounds.
- Only leading退款成功 assertion; existing recharge guard and one-repair limit unchanged; correction exhausted means no pending/trace/write and honest unexecuted reply.
- Frozen normal case requested100yuan/available25; one confirmed refund25yuan. Reply repair path may cost6models vs normal4, declare caps separately.
- Contractv5 must differ from v4; original supplements v4 auth/plan/claim cannot execute new Handler. Do not rewrite historical evidence/claims.
- No paid calls or deployments during this source milestone; cumulative33/8 unchanged.

## Review Focus

- Positives/negative near misses and quoted reply shouldn't overmatch.
- Exhaustion cannot preserve unsupported success or choose tools automatically.
- Real six-turn correction succeeds with correct cap25 and one confirmed write.
- Historical scope inspection unready; execution/claim rejected before dependencies; durable storage-only tests stay independent.
- Previous failing logs stay intact; full standard Go regression and independent review required.

### Task1: TDD source fix and historical identity

**Files:** new internal/agent/refund_boundary_test.go; internal/agent/reply_boundary.go/runtime_metadata.go/chat.go; internal/taskbench/refund_cap_offline_test.go; historical supplement taskbench/CLI tests; docs/refund-completion-fix-offline-2026-10-24.md and new evidence archive.
**Interfaces:** replyRepairReason remains same; TestRefundCapRepairRequired reuses frozen acceptance helper with mandatory correction success.
- [x] RED finite table, real Handler repair/exhausted/tool-explanation and strict frozen refund repair; old-v4-runtime rejection.
- [x] Minimal matcher/repair reason and contractv5; preserve old scope constants and switch its obsolete local execution tests to rejection, storage tests to lower layer.
- [x] GREEN targeted + captured normal/correction/cap paths, full Go regression, independent review.
- [x] Integrity/cleanup, memory and documentation; commit/push/readback. Deployment/new exact scope are subsequent milestones and never paid implicitly.

Observed additional RED: a refund tool-call message with unsupported success prose appended that prose to the real preview. Add matching refund-only preview explanation replacement without extra model call; existing recharge handling unchanged.

Task1 verification: targeted captured tests exit0; full standard Go18 packages pass exit0. Independent review noP1/P2; reviewer MySQL access sandbox-blocked, primary integration evidence retained. 3095 historical file hashes unchanged, isolated databases0. Original failure archive unchanged. Commit/push follows final diff check.
