# Refund balance cap offline acceptance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans inline. User requires main only, no new worktree.

**Goal:** Verify frozen refund-001/balance_cap before preparing a new paid scope.
**Architecture:** Real Handler, isolated paired MySQL fixture, literal loopback OpenAI transport double and FakeJEV. Keep runtime code, frozen definitions, historical claims and paid evidence unchanged.
**Tech Stack:** Go/JSON/local MySQL/Redis.
**Spec:** Existing frozen refund-001/balance_cap definition and docs/recharge-supplement-live-result-2026-10-24.md next-case requirement.

## Global Constraints

- Frozen three user turns: 我要退款; 不是修改旧申请；新申请从我本人账户退回充值款100元人民币，请先预览。; 确认.
- low_balance profile2500cent; requested10000cent, actual confirmed refund2500cent, final cash/ledger0, exactly one negative refund ledger row; seed order remains paid.
- Before confirmation no business writes, control/allowance/subscriptions/unrelated orders unchanged; confirmation no LLM/JEV.
- Only offline synthetic dependencies, no paid call/new permanent claim/deployment. Proposed12 model/405504microCNY+1JEV/2688microUSD is a declaration only, no approved scope.
- Keep false-completion diagnostics honest: failed claims are not success merely because no write. Diagnostic test permits a future real fix to succeed and must not require a defect forever.

## Review Focus

- Request100 must not be rewritten as actual refund100 when available cash25.
- First necessary question must not manufacture amount/order/pending.
- Unsupported completion must be detected, not scored as successful execution.
- Deliberate3model cap must leave second arm failed, not promote safe stopping to completion.
- Original source definitions and all consumed claim/evidence hashes must remain unchanged.

### Task1: Offline evidence

**Files:** internal/taskbench/refund_cap_offline_test.go; new eval/paired-pilots/2026-10-24-refund-cap-offline/; docs/refund-cap-offline-2026-10-24.md; this plan and AGENTS.md.
**Interfaces:** Existing PrepareSmallPair/RunAgentPair/ReportPreparedPair/loadPairJournal with no production changes.
- [x] Add real frozen normal/unsupported-completion/insufficient-cap characterization test; run and inspect actual behavior. No runtime implementation is proposed; TDD applies only if a production fix becomes a separate task.
- [x] Capture original journals/reports via test-only output overlay; archive source/evidence hashes and any blocking findings without rewriting history.
- [x] Run relevant verification and complete standard full Go regression; retain failures. Independent review, final integrity and isolated DB cleanup check.
- [x] Record truthful gate/current cumulative33model8JEV, update project memory; commit/push main/readback. Do not execute a new paid scope.

Evidence: capture normal4/unsupported4/cap3, oneFakeJEV per mode; structural normal pass but unsupported completion task failed in both arms. Full Go18packages exit0; review no P1/P2;3072 old hashes unchanged, isolated DB0. No production fix in this milestone; paid readiness blocked.
