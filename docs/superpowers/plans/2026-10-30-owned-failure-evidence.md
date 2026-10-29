# Owned failure evidence implementation plan

> **For agentic workers:** Use executing-plans for this authorized inline implementation; root performs the independent review and commit. Work directly on main as explicitly requested.

**Goal:** Preserve independently inspectable failure, persistence, terminal-read and cleanup evidence without changing global fail-stop, accounting holds, or advancement eligibility.

**Architecture:** Add a private, explicit, default-off diagnostic option to window acquisition. A stopped owned driver retains the controlled failed response session for identity correlation. The current oracle is not read-only, so terminal Final is explicitly unknown without another oracle call. Typed diagnostics are retained in the stopped receipt row, separately from the unchanged conversation result and normal/quality proofs. Provider failures retain descriptor and ledger outcomes independently. No new scope, ledger state, public execution option, or continuation mechanism.

**Tech stack:** Existing Go taskbench collector, in-process Handler driver, journal and payload stores; fake HTTP transport/oracle, temporary files, optional local miniredis. Fixed `/Users/a1/开发工具/go/bin/go`, SDK26.5, offline modules.

**Spec and authority:** Private `continuation-design/evidence-first-engineering.md` SHA `9d870b385820d2be930f87f7b9ff07a8b559a45cfab19320e146ad1cd311fdec` and isolation assessment SHA `aa0da83c0cfda1acf89c7a42073ebf9a6eef2af58ff6d8163d7034aa8ef62b64`, plus explicit implementation instruction. This document is the concise public spec; it does not authorize another paid run.

## Constraints

- Do not edit the 15 customer production files, frozen616 tree, claims, historical false flags or any delivered runner. Preserve all old artifacts and unrelated working-tree changes.
- Existing scopes have diagnostics off, preserving their record/receipt JSON. New diagnostic evidence is not an opaque proof, completed checkpoint, quality end, or permission to advance.
- No retry, next-arm execution, held release, halt reset, poisoned/closed reset or guard bypass. Invalid ownership remains an error even for terminal reads or cleanup.
- No real model/JEV, network GET/POST, settings/key reads, production DB, owner or financial claim. Only selected offline tests; root owns the later whole-Go run and commit.

## Task 1: controlled terminal Final

Files: `internal/taskbench/window_failure_evidence.go` (new types and private identity/read-only helpers), `window_owned_driver.go`, `window_collector.go`; new `window_failure_evidence_test.go`.

Interfaces: private acquisition/permit `captureFailureEvidence` option; per-driver typed diagnostic accumulator. On a failed owned Step retain its input session and returned session identity. Final correlates the owned retained session only after verifying the exact active driver, fixture, campaign, ledger, claim, schedule and stopped state. Missing/conflicting identity or cancelled context produces explicit unknown evidence. Because the existing oracle may write, even valid identity yields unknown/oracle_may_write without a new oracle call. Final never invokes Step or changes permit eligibility. The runner fallback snapshot is never described as a new successful Final.

Behavior tests (RED before implementation): in-process fake Handler returns HTTP500 with a new session; real runner/owned-driver control flow retains that session but refuses a second potentially mutating state read; original task still fails and next Step is blocked. Wrong prior session, changed returned session, wrong fixture/active owner, absent session and oracle failure cannot manufacture a successful final. Default-off retains legacy rejection and JSON omission.

## Task 2: preserve transport persistence failures

Files: `window_transport.go`, new evidence file and tests; reuse real temporary accounting/payload stores and existing fault wrappers.

Interfaces: private optional diagnostic sink supplied by owned driver; typed sanitized provider/failure-stage, descriptor-write and ledger-halt statuses. Keep the original provider classification alongside persistence failures. Inspect durable ledger outcome under its lock because `Fail` intentionally returns a non-nil halt error even after success. No upstream/body/error strings in evidence.

Behavior tests: provider EOF with successful evidence; descriptor write/fsync/readback mismatch and ledger fsync failure after admission; simultaneous failures; each keeps pending and full held amount, no settlement and at most one upstream call, including a rejected second request. Default-off errors and old schemas stay unchanged.

## Task 3: terminal cleanup and stopped-row evidence

Files: `window_owned_driver.go`, `window_collector.go`, `window_quality_owned.go` failure cleanup closure, new evidence file/tests.

Interfaces: on diagnostic failure revoke permit and close owned Handler server before cleanup. Record cleanup attempted/verified/failed/unknown with bound driver identity; do not discard cleanup error. Capture observer snapshot/incomplete state and ledger pending/unknown independently. Attach only diagnostic data to stopped row after finish; original result/status/errors/Success and proof requirements remain unchanged.

Behavior tests: cleanup failure remains observable, no proof/cursor movement; invalid identity prevents deletion; observer mismatch remains diagnostic failure; pending held remains intact. Run related existing transport, ledger and non-DB owned/Handler tests once after targeted GREEN. Preserve all RED/GREEN logs and precise command/environment metadata under a new ignored preparation directory.

## Review and completion

Review for accidental eligibility changes, identity bypass, raw error leakage, loss of concurrent failure causes, default-off JSON drift, and assumptions about background work. A synchronous in-process Handler return plus revoked owned transport blocks additional owned egress; do not claim this proves arbitrary unrelated goroutines or all database state clean. Terminal oracle covers only its existing fields. Report exact test coverage and limits. No full suite, no commit, no paid-readiness claim.

## Verified design ruling before implementation

`pairedStoreOracle.State` → `LiveBackend.snapshotWithOverview` → `Store.MeOverview`/`meOverviewCash` (`internal/billing/store/plan.go:296–318`) can call `RebuildBalance`. Therefore Task 1 will record `final=unknown`, `final_reason=oracle_may_write` after failed Step and will not invoke this potentially mutating oracle. Tests require no second oracle call, retained controlled session or explicit unknown identity, unchanged failed result and blocked next execution. A genuinely read-only SQL oracle is deferred. Root explicitly confirmed this narrowed boundary; no recovery capability or fake successful Final is introduced.
