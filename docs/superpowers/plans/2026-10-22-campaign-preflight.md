# Campaign preflight Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the one-off six-batch proposal generator with a reproducible offline CLI that binds explicit batch limits and configurations to a reviewed revision.

**Architecture:** Add a pure taskbench proposal builder using existing configuration, contract and budget-estimate functions. Extend paired-eval with campaign-preflight and a strict JSON batch plan; it writes one proposal without opening budgets, databases or providers.

**Tech Stack:** Existing Go standard library and taskbench; no new dependencies.

**Spec:** docs/paired-execution-proposal-2026-10-21.md, especially the six batches, separate budgets and execution gates. This is a preparatory subset; no execution driver or authorization is introduced.

## Global Constraints

- Work only on main, preserving all unrelated workspace changes. The user has opted out of questions and authorized incremental commit/push.
- Every output keeps allow_paid=false, ready_for_paid_run=false, provider_calls=0 and authorized_spending=null.
- Prices, token ceilings and account evidence are declarations; no command checks or claims provider billing correctness.
- New mode requires the same explicit model/profile/revision/JEV flags as preflight plus -campaign-plan. Existing modes reject -campaign-plan.
- Strict input schema campaign-preflight-plan.v1; unknown fields, trailing JSON, duplicate/missing keys, empty batches, invalid budgets and any aggregate overrun fail before output creation.
- One case per batch, stable batch-001 etc.; preserve specified order, no implicit extra cases or budget reallocation.
- The example is the 2026-10-21 declaration (6 cases, 24 model calls, 6 JEV, CNY cap 1000000 micro, USD cap 20000 micro), not a refreshed price or approval.

## Review Focus

- Malformed or unsupported plan data must not silently expand scope or use zero-valued limits.
- CNY and USD overruns and model/JEV count overruns must independently reject, including excess batch count.
- Child model output ceiling must agree with the prepared model configuration.
- Changing the supplied execution revision must change every batch contract; caller data must remain detached.
- Configured model endpoints and inherited credentials must never cause HTTP requests or get serialized into a proposal.

### Task 1: Reproducible offline campaign preparation

**Files:**
- Create internal/taskbench/campaign_prepare.go and campaign_prepare_test.go: plan/proposal types and pure builder.
- Modify internal/taskbench/campaign_budget.go: extract existing CampaignLimits validation for shared use without changing limits.
- Modify cmd/paired-eval/main.go and main_test.go: new offline mode, strict file decode and CLI coverage.
- Create eval/paired-pilots/2026-10-22-campaign-preflight/plan.json and docs/paired-campaign-preflight-2026-10-22.md.

**Interfaces:**
- Consumes existing ConversationSuite, PairedProfiles, PairModelSettings, PrepareAgentPairConfig, PairRunContract, EstimateDeclaredPairBudget and CampaignLimits.
- Produces PrepareCampaignProposal(source ConversationSuite, profiles PairedProfiles, model PairModelSettings, revision, jevModel string, plan CampaignPreparationPlan) (CampaignProposal, error).
- CampaignPreparationPlan contains schema, campaign_limits and batches of key/model_limits/max_jev_calls. Proposal contains explicit per-batch configurations, bindings, estimates and aggregate declared reservations, plus unverified execution gates.

- [ ] Write tests for the six-case totals (12 arms/26 turns, 24 model/6 JEV, 811008 microCNY/16128 microUSD), order, false authorization, changed-revision binding, detached output, invalid plans and all independent overrun dimensions.
- [ ] Run targeted builder tests; expected compile failure from missing new types/function. Implement the pure builder and shared validation, then rerun to pass.
- [ ] Write CLI tests for campaign-preflight output and no overwrite; strict unknown/trailing/malformed input rejection; required flags; rejection outside new mode; loopback endpoint counter remains zero even with inherited API key settings.
- [ ] Run CLI tests; expected failure because new mode/flag is absent. Add strict decode and dispatch, then run both packages' relevant tests and go vet.
- [ ] Generate one local proposal from example via the actual CLI using current reviewed HEAD; check counts, contracts, declared reservations, no run/budget journals, and 0600 output.
- [ ] Read-only independent review; fix material findings with regression tests; review exact staged scope and diff whitespace.
- [ ] Write/read back durable Hindsight result, commit and push only this milestone, verify GitHub SHA and report limitations.
