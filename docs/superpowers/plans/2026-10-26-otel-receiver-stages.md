# OTLP Receiver Stage Diagnostics Implementation Plan

> **For agentic workers:** Use executing-plans inline; complete one fresh review after implementation. User has authorized autonomous main development and direct push without routine checkpoints.

**Goal:** Offline-test an opt-in, bounded diagnostic module that locates a slow OTLP request in S3 upload, queue publication, or other receiver work, without changing business results or deadlines.

**Architecture:** CommonJS module using AsyncLocalStorage per request. Caller wraps the OTLP handler with run(fn) and existing async operations with stage(name, fn). No image patch, activation, HTTP endpoint, deployment or paid execution in this task.

**Tech Stack:** Built-in Node.js async_hooks, perf_hooks, util.types and node:test; no dependency installation.

## Global Constraints

- Disabled by default; disabled calls preserve returned promise/value and thrown error identity.
- Fixed 2000ms observation timer; it observes, never cancels or retries business work.
- Only s3_upload, queue_add and other labels; locally generated sequence, numeric times, fixed outcomes. No request objects, headers, URLs, keys, bodies, results, error messages or stacks in output.
- At most 8 stored/emitted records, 8 tracked concurrent requests, 2 phase summaries per record; snapshot clones and exact omitted/skipped counters.
- Pending record at threshold; terminal record only on slow completion or failure. Fast success emits nothing.
- Logging exceptions cannot change business outcome; concurrent requests remain isolated. Late timer callbacks cannot emit after completion.
- Go20min policy, real5min command, existing2s exporter/no retry/stop, consumed claims and reserves unchanged.

## Review Focus

- Delayed logger/timer callbacks cannot fabricate another request's phase.
- Detached/nested/concurrent stages do not attribute unrelated errors to a past phase.
- Overflow has bounded storage and correct counters; snapshot mutation cannot alter internal records.
- Secrets in rejected error messages/results/custom then getters never enter output or become diagnostic-induced exceptions.
- Module is not wired into the running receiver; synthetic success is not root-cause or paid-readiness proof.

## Task 1: Module and deterministic offline contract

- [x] Add tests for disabled identity, pending S3/queue/other, errors, fast/slow success, logger failure, cancellation of observation only, concurrency/overlap, overflow, snapshots and privacy.
- [x] Run tests RED before implementation; save output privately.
- [x] Implement deploy/langfuse/diagnostics/otel-stages.cjs and README with integration contract.
- [x] Run GREEN; run existing Langfuse Node suites and a fresh independent review.

## Task 2: Isolated actual-implementation integration and delivery

- [x] Use a separate Node process, current route module binding, fresh dedicated Redis/S3 prefixes, no consumers, no old spans. Load module in memory, wrap publisher/S3/add only in that process.
- [x] Verify returned result and stage count, no healthy diagnostics; independently verify dedicated-prefix cleanup zero.
- [x] Preserve prior evidence and consumed claim hashes; archive tests/integration and limitations. Update project memory and AGENTS.
- [x] Commit selected files and push main; verify remote. No deployment or paid call.

## Ledger

Ruling: The user opted out of design questions and routine approvals; proceed with the small design stated in chat. No new paid or deployment scope is inferred.
Ruling: The module is an offline deliverable, not a patch installer. Exact HTTP-route activation and image integration remain a separate task after this contract is verified.

Outcome: 23 module tests and 40 combined Node tests passed; final isolated publisher243.583ms, no diagnostic output on fast success, private prefix cleanup independently zero. Two reviewer P2 findings fixed RED→GREEN; targeted recheck23 passed.
Ruling: Host translation tests lacked their original /app Meriyah dependency; virtual-source runner then exposed the existing assets __filename requirement. Both failures retained; final suite ran unchanged current sources from a dedicated temporary directory inside the pinned container and removed that directory. No dependency installation, source relaxation or skip.
Ruling: Production Go and receiver image/configuration remain unchanged; this is an offline contract, not receiver activation or paid readiness.
