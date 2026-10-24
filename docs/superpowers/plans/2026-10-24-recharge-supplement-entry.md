# Recharge supplement independent entry Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans. Work inline on main per user instruction.

**Goal:** Freeze an independent guarded entry for recharge-001/supplement_then_confirm, without paid execution.
**Architecture:** Reuse paired-live -retest with a new fixed schema/identity, pin deployed262a47f/v4 and repaired offline evidence. Persist a one-shot claim only after all three exact consumed predecessor claims validate.
**Tech Stack:** Go, JSON evidence, local MySQL/Redis fixtures and loopback transport doubles.
**Spec:** Scope stated in chat and existing docs/customer-recharge-supplement-deployment-2026-10-24.md; builder choices below.

## Global Constraints

- New fixed recharge-supplement-20261024, two arms / three user rounds, only frozen recharge-001/supplement_then_confirm.
- Shared12 model attempts /405504 microCNY and1JEV/2688 microUSD; output128, request/input ceiling16384; no automatic retry, expansion or claim release.
- Preserve prior three allocations1013760/8064 micro units; new cumulative1419264/10752, unallocated98580736/989248 under approved100CNY+1USD.
- Exact262a47f customer source and v4 contract; final executor commit separately bound. Templates unapproved and billing unverified.
- No production customer behavior changes, deployment, paid requests or real new claim; original claims/evidence untouched.

## Review Focus

- Coherent auth+plan budget expansion must fail, even with updated hashes.
- Each missing/tampered predecessor fails before new claim creation.
- Alternate output/revision cannot reclaim; old scopes cannot authorize new scope.
- Provider503/JEV EOF stop without fallback or retry; diagnostics retained.
- Normal and correction six-turn Handler flow writes only after confirmation, control state unchanged; no quality claims from doubles.

### Task1: Fixed scope and durable claim

**Files:** new internal/taskbench/authorized_recharge_supplement.go and tests; cmd/paired-live/retest.go and recharge_supplement_test.go; internal/taskbench/authorized_entry.go; eval/paired-pilots/2026-10-24-recharge-supplement-entry/.
**Interfaces:** checkedRechargeSupplementScope(schema,id) returns(cashRetestScope,bool); claimRetest validates ready snapshots and persists fixed identity with three predecessor SHA checks.
- [x] Write failing scope/claim/CLI tests, run target tests and observe unknown scope rejection (RED).
- [x] Implement fixed identity/billing and allocation without changing historical bindings.
- [x] Run real Handler normal/repair/provider503/JEV EOF entry tests; capture full normal/repair requests locally.
- [x] Run targeted tests then full go test ./... -count=1 -json with original parallelism/timeouts; independently review.
- [x] Freeze public manifests and source/evidence hashes; check historical integrity, isolation cleanup, no new real claim; commit/push/readback. Use current public pricing/token evidence only when actually reverified; otherwise leave false.

Evidence: initial full16 pass/2failed retained; bounded failure/pass rechecks retained; unchanged second full18pass exit0, independent review no P1/P2,2962 old hashes unchanged, isolated databases0. Final archive binding and remote readback follow this commit without modifying Go sources.
