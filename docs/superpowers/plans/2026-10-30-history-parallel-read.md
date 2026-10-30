# Historical stopped-evidence reads: bounded parallel candidate

The approved scope is an offline performance candidate. It does not restore any stopped scope or change provider/window deadlines, claim policy, historical evidence, customer code, source semantics or signatures.

## First implementation only

- Add a private batch reader for one to four independent files. A shared four-slot semaphore limits concurrent reads across overlapping callers. Start only short-lived workers, collect results by input index, join all workers before returning, and choose errors in input order. No filesystem cache, background prefetch, retry or cross-call result reuse.
- Keep `readWindowActualFile` and `readWindowActualBytes` unchanged. The adapters pass their original path, digest and size bound. The caller retains its original historical lock until all workers join.
- Read first-generation fixed pins/canonical claims and V2's 38 fixed top artifacts in sorted batches of four. Keep all root/count/name guards, predecessor revalidation and before/after passes.
- Read V2 native request body and request descriptor together (two jobs). Read each valid available response descriptor/body together (two jobs); an unavailable response still has no body read. Keep packet order, store/descriptor equality, sequence/identity, unknown response handling and exact namespace checks.
- Read up to four validated attempted V2 records at a time. Decode and replay them in original row order, retaining trace uniqueness, result digests, status/count linkage and directory checks. Do not read unattempted rows.
- Do not change V3–V7 readers or parallelize ancestors, windows, claims, replay or signature checks.

## Verification

1. Obtain an assertion RED against a serial reader using four blocked reads; release/join even on failure.
2. Verify real parallel start, process-wide maximum four active readers across two calls, exactly-once reads, stable result/error order and full joining after errors. Retain original reader mutation/path/size rejection tests and fresh reads across calls.
3. Run focused first/V2 normal and race tests with the fixed local Go runtime, SDK26.5, GOPROXY/GOSUMDB off. Original actual-evidence tests remain opt-in; no full Go, DB, provider, claim or actual owner operations in this unit.
4. Independently review the candidate and each source digest. Root then compares one fixed full44 read-only profile to the retained serial baseline. Only then decide whether to adopt the candidate. A warm-cache improvement is not a 300-second completion guarantee; deadline safety remains a separate unit.

The previous single-load profile was 61.316663584 seconds. No performance gain is asserted until the same historical inputs are measured after review. Failed scopes and their claims remain immutable.
