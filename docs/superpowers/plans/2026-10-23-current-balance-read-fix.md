# Current Balance Read Fix Implementation Plan

> **For agentic workers:** Use executing-plans inline; user requested continuing the diagnosed repair. Stay on main.

**Goal:** Reject a concrete cash balance answer without a successful current-turn balance read, and reject missing evidence in read-only-required evaluation turns.

**Architecture:** Extend the existing bounded reply repair at the execution boundary; the model continues choosing tools. Detect explicit numeric cash-balance claims, request one correction, and return a limitation without an amount if correction does not obtain current evidence. In taskbench require a successful read when the frozen turn permits only read, leaving clarification alternatives pending semantic review.

**Tech Stack:** Existing Go agent handler, taskbench runner, local fake LLM/billing and isolated MySQL/Redis.

**Spec:** docs/pi-second-live-checkpoint-2026-10-23.md and user instruction to continue fixing.

## Constraints

- No paid requests, no changes to frozen definitions or historical results, no production deployment.
- Keep same-turn read reuse; never automatically invoke a write or select a tool in repair.
- The cash claim detector is a bounded consistency guard, not a universal semantic verifier.
- Preserve unrelated changes, commit and push only this fix.

## Review Focus

- History alone or a failed/unrelated read must not authorize a current cash claim.
- Repeated unsupported answer must stop within one repair and expose no stale amount.
- Current successful reads, clarification and unit explanations must still work.
- Missing required read must fail; clarification-or-read alternatives retain semantic review.
- Both routing arms use the same model-loop boundary; no budget resets or hidden calls.

### Task 1: Reproduce then repair runtime boundary
Files: internal/agent/reply_boundary.go, chat.go, current_balance_test.go.
- [x] Test scripted model reusing a previous turn amount, then selecting a current read after correction; test repeated claim fallback and failed read.
- [x] Observe RED, add minimal successful-current-read check and one-repair fallback; run agent tests.

### Task 2: Reproduce then repair evaluation evidence check
Files: internal/taskbench/conversation_runner.go, conversation_http_test.go.
- [x] Test required read with unreviewed/no-tool answer fails while allowed clarification remains pending.
- [x] Observe RED, require read evidence for read-only expected action; run taskbench tests.

### Task 3: Verify and deliver
- [x] Full Go suite, independent review, document limitations and zero inference.
- [x] Commit main, push normally, verify remote SHA and update/read back ai-gateway memory.
