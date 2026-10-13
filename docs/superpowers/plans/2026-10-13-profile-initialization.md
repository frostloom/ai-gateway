# Profile Initialization Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans for inline implementation and fresh review before completion.

**Goal:** Instantiate and verify six synthetic development account profiles without model/JEV calls.

**Architecture:** Preserve frozen v1 definitions. Export a separately hashed runtime profile overlay: initial_quota=50 cent, integer-yuan recharge=(target_balance+200)/100, followed by the existing250cent settled consumption. Seed in a disposable MySQL database and private Redis double, then verify with real store read methods and clean up.

**Spec:** docs/customer-service-task-definitions-2026-10-12.md; user selected first step (account initialization). No full conversation adapter in this task.

**Constraints:** No credentials or unrelated files committed; no model/JEV/API requests; no shared database writes; historical artifacts immutable.

- [x] RED→GREEN Python tests for integer recharge/ledger invariant, unchanged task expectations and refusing overwrite/tampering.
- [x] Export and verify runtime overlay bound to source profile SHA and generator SHA.
- [x] RED→GREEN Go integration test: six own accounts + foreign controls; real recharge/subscription business methods, staged synthetic bills; independent identities, wallet/ledger, orders, cycles, allowance and latest bills; cleanup only generated database.
- [x] Run relevant Go tests/vet, Python offline suite and all evidence verify commands; fresh review; record limitations. Commit/push recorded in Git history.

Ruling: Frozen v1 paid-order cents cannot be produced by integer-yuan recharge. Add explicit50cent legacy initial quota and reduce recharge seed by50cent in a new overlay, preserving all visible wallet/consumption and refund terminal expectations. This does not reconstruct historical truth.

Ruling: Cancelled persistence and MeOverview projection conflict. Record database state and actual nil projection separately, with an explicit business blocker; do not silently relax the full-task contract or alter frozen production code in this initialization-only task.

Evidence: Python3 initial missing-function/file failures thenPASS; Go missing helpers compileRED then test implementation. Pointer-method compile error corrected; real database test exposed request ID width failure and cancelled projection mismatch. Seed ID fixed, projection gap independently asserted/documented. Six-profile non-skip testPASS3.40s; relevant Go packagesPASS32.969s/7.890s, vetPASS; sixverifycommandsPASS. Python92 first run had a Windows temporary-directory cleanup lock, serial rerunPASS44.610s. Final fresh review no remaining blocking implementation findings; cancelled business gap remains intentionally open.
