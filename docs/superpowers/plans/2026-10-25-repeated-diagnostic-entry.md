# Repeated export-diagnostic entry implementation plan

> **For agentic workers:** Use executing-plans inline, TDD, then one independent final reviewer. User explicitly requests autonomous execution and no routine confirmation; do not pause for design/method approval. New paid scope still separately authorized.

**Goal:** Add an independent, single-use scope for one fresh fixed repeated cohort with bounded export diagnostics.
**Architecture:** Keep historical scope constants/checkers/claims intact. Share the existing fixed-cohort engine and repeated schedule only through an explicit allowlist of the two exact repeated scope IDs; new checker, CLI mode and claim own the new authorization boundary.
**Tech Stack:** existing Go, no dependencies.
**Spec:** docs/repeated-diagnostic-design-2026-10-25.md; eval/paired-pilots/2026-10-25-repeated-diagnostic-design/candidate.json.

## Global constraints

Main only; preserve unrelated web/.tmp/integrations/plate changes. Customer50e3c8d/v5, runtimea95cacd. Exactly24pairs/48arms/104turns,248model/24JEV, whole-command5minutes, failure-stop/no retry/resume/reclaim. No paid calls or claim during implementation. New full permanent reservation only after exact human authorization and exclusive claim. Do not edit consumed claims or old archives. Existing success/disabled Stats wire shape and diagnostic first8 bound remain.

## Review focus

Cross-mode/new-schema reuse must reject before credentials or claim. Recomputed hashes must not permit altered schedule/caps. Missing/changed seventh predecessor must leave new claim absent. First-arm export failure must persist diagnostic and prevent second-arm work. No historical binding may silently become current execution revision or be overwritten.

### Task 1: Exact checker, dispatch and claim boundary

Files: create internal/taskbench/authorized_repeated_diagnostic.go and *_test.go; cmd/paired-live/repeated_diagnostic.go and *_test.go. Modify internal/taskbench/campaign_prepare.go, authorized_entry.go, authorized_repeated.go, authorized_repeated_run.go, authorized_cohort_run.go as narrowly needed; cmd/paired-live/main.go.

- [ ] RED `TestRepeatedDiagnosticScopeIsSeparate`: new checker signature `CheckAuthorizedRepeatedDiagnostic(authRaw,planRaw,billingRaw,evidenceRaw []byte)(AuthorizedPilot,error)` rejects all old schemas; old checkers reject new schema; false template yields Readyfalse without env/claim access.
- [ ] Define `V5RepeatedDiagnosticID="v5-repeated-export-diagnostic-20261025"`; separate authorization `v5-repeated-export-diagnostic-authorization.v1`, billing `v5-repeated-export-diagnostic-billing-attestation.v1`, receipt `authorized-v5-repeated-export-diagnostic.v1`, claim `paired-live-repeated-export-diagnostic-claim.v1`. New CLI `-repeated-diagnostic` rejects all other mode/continuation flags.
- [ ] Preserve `IsRepeated()` as the common policy class so fact/observer stops remain active; add `IsRepeatedDiagnostic()` for exact new dispatch before historical IsRepeated branch. Historical runner/claim retain exact old-ID guards; new runner `RunAuthorizedRepeatedDiagnostic(ctx,dir,opts)` requires exact new ID/revision then shared engine.
- [ ] Bind a new typed optional `RepeatedDiagnosticBinding` with string fields `ObserverRevision` (`observer_revision`), `ObserverSourceSHA` (`observer_source_sha256`), `SyntheticProvenanceSHA` (`synthetic_provenance_sha256`), `PriorResultManifestSHA` (`prior_result_manifest_sha256`), exactly equal to candidate_binding; these bind the explicit source/provenance/result manifest files in candidate source_sha256 in new authorization, checked pilot and new receipt with `omitempty`; retain original JEV diagnostic fields. Populate exact constants only after hashing committed archives, never placeholder digests.
- [ ] RED altered cases/order/block/pair/call caps, billing ID/schema/revision, observer/source SHA, refund amount or target reject despite recomputed plan digest. Allow duplicate case keys only for the two validated exact schedules, not arbitrary campaign IDs. GREEN minimal implementation.
- [ ] RED `TestRepeatedDiagnosticClaimChain`: each missing/tampered old predecessor including c7f14ee3 refuses before creation; correct seven original SHAs allow one O_EXCL/0600/fsync claim; second output/version refuses. Assert model cumulative20680704microCNY, JEV158592microUSD; unchanged old six-predecessor claim path and constants.
- [ ] Run focused taskbench/paired-live tests with SDKROOT MacOSX26.5.sdk; source review of dispatch ensures no dependency construction before check/claim gates. Commit/push after this independently verified unit; no claim in project root.

### Task 2: Full local execution and persisted stop proof

Files: internal/taskbench/authorized_repeated_diagnostic_run_test.go; reuse focused helpers from authorized_repeated_run_test.go without weakening old assertions. New public false examples/offline archive under eval/paired-pilots/2026-10-25-repeated-diagnostic-entry/ and result docs.

- [ ] RED→GREEN actual Handler normal and correction full24pair paths:48independent accounts,104turns,exact ordered starts, config/report/trace metadata and business oracles; all stub/Fake usage labeled local, not billable evidence.
- [ ] RED→GREEN first-arm observer503 and truncated200ACK after complete body persist first8 bounded diagnostic and original Failed count; next arm/suffix unrun; no export replay. Provider/JEV/usage/fact/structure/cleanup/receipt failures stop under both arm orders. Full scope reservation retained separately from started prefix leases.
- [ ] Final standard `SDKROOT=/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk go test ./... -count=1 -json`, no parallelism/timeout changes; inspect all package/test failures. Independent final source/evidence review, protected SHA and post-test schema0 checks. Commit/push exact scope; update AGENTS/Hindsight on confirmed readback.

### Task 3: Freeze execution package without authorizing it

Files: same public entry archive; private new preparation directory, not old execution artifacts.

- [ ] Recheck exact complete requests/tokenizer/official target thinking/output and current public rates; preserve failures, no model POSTs. Read-only health checks do not prove paid success. Reuse no credentials in public.
- [ ] Archive committed final execution source with SHA/provenance, freeze plan/auth/billing/evidence digests and false-consent templates, verify seven consumed predecessors and absent new claim. Calculate unchanged current reservation and conditional new totals from integers.
- [ ] Pure checker Readyfalse without env side effects; independent review of exact binding. Push preparation then request only the new exact paid scope if all technical/evidence gates pass. Do not ask total budget again or run under old approval.
