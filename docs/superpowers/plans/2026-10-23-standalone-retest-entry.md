# Standalone Cash Retest Entry Implementation Plan

> **For agentic workers:** Use executing-plans inline on main. User authorized implementing the archived proposal, not paid execution.

**Goal:** Add a default-read-only, one-shot execution path for cash-read-retest-20261023 with shared 12 model / 1 JEV limits.

**Architecture:** Validate a separate retest authorization into the existing private AuthorizedPilot snapshot, with exact one-case Pi plan, explicit revision and prior failed-evidence bindings. Reuse the existing campaign runner, model/JEV guards, owned fixtures, observer, build provenance and outcome handling. Claim a fixed retest identity independently of output/plan formatting/revision before fixtures; never reopen old ledgers or claims.

**Tech Stack:** Go taskbench and paired-live; existing archive build script and local fake providers/MySQL fixture.

**Spec:** docs/pi-cash-read-retest-proposal-2026-10-23.md and eval/paired-pilots/2026-10-23-cash-read-retest-offline/.

## Constraints

- main only, preserve unrelated dirty assets; no paid calls or deployment.
- Exactly balance-020/explicit_scope_followup, two arms/two turns; 12 attempts/¥0.405504 and one JEV/$0.002688 shared.
- Fixed identity cash-read-retest-20261023; new schema, independent claim/ledger/output; no retry/refund/next-batch advancement.
- Default check allows unapproved template but stays unready; execution requires explicit consent and bound risk evidence.
- Old six-case authorization/billing and retest permissions cannot cross modes.

## Review Focus

- Wrong plan, target, limits, revision or prior failure binding rejects before private configuration/fixtures.
- Unapproved or missing rate/token evidence never enables execution; vendor JEV failure verified remains false under full reservation policy.
- Same identity cannot be reclaimed by changing whitespace, output, plan/revision or consent file.
- Shared guards stop at 12/1, including reply repairs and fallback; failures retain evidence and claim.
- Original first/second-batch flows and archived evidence remain compatible.

### Task 1: Authorization and runner
Files: internal/taskbench/authorized_retest.go, authorized_retest_test.go, authorized_entry.go, authorized_entry_run.go.
- [x] Write failing tests for isolated retest scope, read-only unapproved state, evidence/revision binding, legacy rejection.
- [x] Extract shared billing validation without relaxing old policies; implement retest snapshot and one-case runner dispatch/receipt identity.
- [x] Local OpenAI fake handler/fixture run verifies both turns read, checkpoint only, budget/failure behavior and no old evidence mutation.

### Task 2: CLI and persistent identity
Files: cmd/paired-live/retest.go, retest_test.go, main.go.
- [x] Write failing check/execution-gate/duplicate-claim tests.
- [x] Add -retest mode, incompatible continuation flags rejected, fixed identity durable claim before fixtures, reuse explicit env and build guards.
- [x] Verify read-only check creates no claim/output and ignores env files; failure/changed-output cannot repeat execution.

### Task 3: Review and delivery
- [x] Run full Go suite and independent code review, repair important findings with RED→GREEN tests.
- [x] Document false authorization/billing templates and operations; no approved artifacts created.
- [ ] Delivery after this source commit: push main and confirm remote SHA; archive build/read-only check; update/read back ai-gateway memory. Final report records the results without changing the reviewed source revision.

## Decisions

New schemas: cash-read-retest-authorization.v1 and paired-retest-billing-attestation.v1. Authorization binds campaign_id, exact plan SHA/target/limits, reviewed 40-character execution revision, and original failed journal/review SHA. Billing binds same plan/campaign/revision plus evidence source SHA and reviewed full-attempt failure policy. Changing a bound input invalidates permission, and changing a plan cannot reset the fixed claim identity.
