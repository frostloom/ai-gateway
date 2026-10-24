# Observer export diagnosis — offline execution plan

User has delegated continued autonomous iteration; execute inline on main. This is a bounded investigation, not approval for model/JEV calls or telemetry replay. Preserve all historical claims, failure receipts, unrelated web files and shared configuration.

Goal: establish what the 232 accepted /229 exported /3 failed counters do and do not prove, and whether available historical evidence identifies a cause. Do not turn a plausible timeout into a historical fact.

1. [x] Read ai-gateway memory and pinned source; freeze old private evidence hashes. Collect only incident-window Langfuse/dependency logs and current container state. Derive timeline from original spans, with no writes to any external service.
2. [x] Add `internal/observability/export_ack_test.go`: use the real Observer/OTLP exporter and five literal-loopback receivers (normal ACK; complete body received then response timeout; complete body then503; complete body then connection close; complete body then malformed protobuf200). Synthetic three-span batch only. Assert receiver body counts, one request/no retries, client counters, flush/shutdown failure and absence of raw response secrets. These are diagnostic characterization tests, no production implementation changes. They demonstrate alternative explanations; none reproduces the actual incident.
3. [x] Run the focused probes and appropriate verification; independently review evidence/conclusions. Archive sanitized results and SHA bindings, leave raw logs private. Record historical cause unknown unless directly corroborated; record next diagnostic needs without changing timeout/retry/service settings. Verify all protected hashes, commit/push exact scope, persist/read back Hindsight facts.

Success: reproducible local evidence and a candid incident assessment. No assertion of a repaired exporter, complete repeated cohort, reclaimed reservation or new paid authorization.
