# Bounded export diagnostics implementation plan

> Execute inline with executing-plans/TDD; one independent final review. User explicitly delegates implementation choices and continuation after verified commit/push; no routine approval questions.

**Goal:** Retain bounded safe evidence for future OTLP failures without changing delivery, timing, error or stop policy.
**Architecture:** A private HTTP transport/body wrapper observes scalars through an export-context capture; the processor keeps first8 immutable failure snapshots, and Stats optionally serializes them into existing execution receipts. Successful/disabled Stats omit the new field.
**Tech:** existing Go/OTLP SDK, no new dependencies.
**Spec:** docs/observer-export-diagnosis-2026-10-24.md and this turn's scoped design.

## Constraints and settled choices

main only; preserve unrelated web changes. No model/JEV calls, old trace replay, deployment, claim or authorization change. Existing 2s production timeout, HTTP client, redirect rejection, disabled retry, batching, counters, fixed error text and stop rules remain. Diagnostic elapsed time is observed, not a delivery or durability assertion.

New optional `Stats.ExportDiagnostics *ExportDiagnostics` JSON `export_diagnostics,omitempty`: `failed_batches`, `omitted_failures`, `failures` (first8). Each record has `batch_sequence`, `span_count`, `elapsed_ms`, `error_kind`, `http_round_trips`, optional `last_request_sequence`, optional numeric `http_status`, `stage`, optional `transport_cause`. Request sequence counts wrapper RoundTrip invocations, not wire/TCP attempts. No URL/header/body/content/error-string/trace-ID storage. First8 retained, later failures counted as omitted; snapshot copies cannot mutate stored evidence. Nil diagnostics preserve successful/disabled legacy JSON.

Per-export context capture is private, lock-protected, bounded to latest request scalars; body Read/Close delegate once and return original results. Classifications use errors.Is/As/types, context, numeric HTTP status and protobuf sentinel only. Unknown stays unknown. SDK's result remains authoritative for existing success/failure counters: do not convert a failed result to success or retroactively change historical records.

## Review focus

No secret/error text leak, even in nested URL/certificate/response errors; no extra read/close, retries, headers or redirects; no missing-status zero invention; diagnostics bounded and race-free; exact failed receipt persisted while later arm remains unrun; nil optional field retains old bytes.

### Task 1: Capture and snapshot

Files: new internal/observability/export_diagnostics.go and export_diagnostics_test.go; modify processor.go and observer.go; extend export_ack_test.go.

- [x] RED actual HTTP503 has diagnostic JSON status503, three failed spans, one RoundTrip, fixed safe error; successful/disabled Stats keep old JSON; existing five-mode probes gain bounded finite classifications.
- [x] Implement capture/transport/body delegates and typed classification; retain fixed failure result, only first8 snapshots plus omitted count.
- [x] RED→GREEN EOF/truncated body/TLS/context/unknown cases, sequence over success then failure, snapshot mutation, cap10failures→8+2; concurrent Stats safe. Run package and focused race checks.

### Task 2: Receipt proof and delivery

Files: extend existing repeated observer-failure test or new focused taskbench receipt test; new docs/false offline archive.

- [x] RED→GREEN observer failure diagnostics survive actual stopped receipt JSON readback, original counters/first-arm stop/unrun suffix and full permanent reservation unchanged. Local model/JEV stubs only.
- [x] Standard SDKROOT MacOSX26.5.sdk full go test ./... -count=1 -json; inspect every failure; independent review; protected private hashes/DB0; commit/push exact files.
- [x] Persist autonomy preference and completed facts in AGENTS/Hindsight (actual readback only); after push independently proceed to zero-inference validation/readiness work. No automatic paid execution or reused claim.
