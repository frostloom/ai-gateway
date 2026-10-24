# Bounded OTLP export failure diagnostics

The observer now retains safe evidence when its exporter returns an error. This addresses the evidence gap described in [the historical diagnosis](observer-export-diagnosis-2026-10-24.md); it does not establish that incident’s cause or repair delivery.

## Behavior

Failed Stats optionally include `export_diagnostics`: total failed exporter batches, first eight records, and the count omitted thereafter. Records contain batch sequence, span count, elapsed milliseconds, RoundTrip count/latest sequence, optional numeric HTTP status, and finite stage/error/cause classifications. RoundTrip sequence counts wrapper invocations, not TCP or wire attempts. Absent status stays absent. No URL, headers, credentials, body, error string or trace ID is retained. Snapshots deep-copy records and status pointers.

A context-bound transport/body wrapper delegates the original request, reads and closes exactly once. The first observed body failure survives a later Close failure. A protobuf decode classification requires an observed successful HTTP status; a pre-HTTP protobuf error remains unknown. Existing timeout, disabled retries, redirect rejection, counters, fixed error text and stop policy remain unchanged. Successful and disabled Stats omit the field and preserve their old JSON shape. The feature has not been deployed to the customer service.

## Verification

Actual loopback HTTP probes cover normal ACK, complete request receipt followed by timeout, 503, connection close, malformed protobuf ACK, and truncated response. Typed TLS, EOF, context and unknown cases, successful-then-failed sequence, first-eight bound, concurrent snapshots and mutation isolation are covered. Neither reading a request nor an ACK proves durable storage.

RED→GREEN evidence covers missing HTTP diagnostics, TLS value classification, read-error preservation and rejecting a false pre-HTTP decode classification. An old-source overlay makes the actual Handler stopped-receipt probe fail; current source persists and reads back the diagnostic records while preserving Failed counters, the full permanent reservation, first-arm stop and the unrun suffix. This probe uses six model stubs and one FakeJEV, not paid inference.

Final observability package and race checks pass. The race linker emits an existing macOS LC_DYSYMTAB warning; it does not report a race. Independent source review found no remaining P1/P2 after the pre-HTTP classification correction. The reviewer’s test setup was blocked by cache permissions; primary verification provides the test evidence.

Final standard `go test ./... -count=1 -json` exits 0: all 18 packages pass, taskbench 486.178 seconds. All 7,978 protected private files retain their SHA; post-test isolated schema count is zero (a mid-run read found two active test schemas). Source SHA matches the final tested code. See [verification archive](../eval/paired-pilots/2026-10-25-observer-export-diagnostics/verification.json). Private raw logs remain local. No model/JEV call, new claim, old trace replay, customer deployment or shared service configuration change is part of this work. Actual cumulative calls remain 117 models/31 JEV; the repeated cohort remains stopped with its full reservation and consumed claim.

## Continuation

The user requests autonomous continuation after each verified commit/push. The next validation uses fresh, explicitly synthetic local telemetry with no inference, stops on any exporter failure, and reads it back without replay. New exact paid evaluation ranges retain their separate authorization requirement.
