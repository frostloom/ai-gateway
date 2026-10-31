# New V10 controlled local-stub export test

Only new `internal/taskbench/window_collected_v10_export_test.go` and this report changed. The opt-in acquisition was **not run** by this worker. No original102 loader/readback, DB, listener, HTTP provider/OTLP receiver, actual constructor, actual authorization, real claim or paid request was invoked. Root controls the genuine controlled run after its history fix and review.

Code SHA256 `04943adb2c2cd12ab9cbbf01c287759f62e08c13098432afa4a92c5960ea9969`. It adapts the existing V9 export flow into a separate V10 test; the old V9 test and all production files remain unchanged by this subtask.

## API / root-run inputs

Opt-in test: `TestWindowV10CollectedWindowFixtureExport`. Defaults to immediate skip before path/history setup unless `TASKBENCH_V10_COLLECTED_FIXTURE_EXPORT=1`.

Required environment:

- `TASKBENCH_V10_COLLECTED_FIXTURE_EXPORT=1`
- `TASKBENCH_V10_COLLECTED_FIXTURE_EXPORT_ROOT`: new exclusive canonical child of `/Users/a1/项目/ai-gateway/.tmp/v10-collected-local-stub-20261031/`; parent must already exist, attempt root must not exist. Never reuse an old attempt directory.
- `TASKBENCH_V10_COLLECTED_FIXTURE_SOURCE_ROOT`: explicit canonical selected source archive, disjoint from export output; the test does not silently protect the working repository.
- `TASKBENCH_V10_COLLECTED_FIXTURE_SOURCE_PINS`: canonical absolute JSON file with complete `{relative.go.path:sha256}` map for the selected archive. The `.go` namespace is walked exactly before and after; missing/unprotected/additional Go files, aliases and changed bytes reject. Required critical V10/agent/observer paths must be present. The map includes this new test.
- `TASKBENCH_V10_COLLECTED_FIXTURE_SOURCE_PINS_SHA256`: SHA256 of exact pin-file bytes.

Recommended root-only runner, from the selected archive: pinned Go `test -json ./internal/taskbench -run '^TestWindowV10CollectedWindowFixtureExport$' -count=1 -timeout=15m`. A previously built owned test binary may instead use equivalent `-test.run`, `-test.count=1`, `-test.timeout=15m`, with its package cwd selected by root. Outer engineering timeout is driver controlled; output recommends900seconds and explicitly labels `outer_test_timeout_source=go_test_driver`. Business acquisition remains300seconds, model90 and JEV10 unchanged. No claim600seconds is sufficient for repeated genuine102 history checks.

## Controlled flow and outputs

The explicit concrete102 load is followed by physical before receipts, production `prepareFullWindowScopeV10`, `prepareWindowCampaignV10Technical`, private concrete102 pointer and candidate185/3, a fresh synthetic V10 authority, production `startWindowCampaign` and `runWindowAcquisition`. Only local TLS fake providers and local OTLP receiver are configured. New synthetic signing key is memory-only; copied retained provenance and a stable synthetic equivalence marker are bound in the signature. Manifest evidence_kind=local_stub and source/binary/equivalence markers explicitly are not actual source proof, actual ready or actual Session authority.

Exact first mapping is new local1 → original declaration `window-056`/global386..390:5pairs10arms22definedturns. Entire scope retains3660Slots with456suffix Windows, actual3275pairs6550arms10504definedturns/boundaries0, separate complete Completion object unchanged. Success asserts outer receipt.v10, exact current/campaign totals and Completion, advance to new local claim2, no relaxation of owner/source/observer/cleanup/payload guards. Fake model/JEV counts are measured from returned production/native data rather than copied from old24-arm expectations.

The inherited local TLS witness injects one pureEOF before any request bytes on a first native attempt, permitting only original strict same-context/body native2. Success checks one unknown74752 permanent hold plus one74752 funded extra, exactly one model retry, no JEV retry, no pending/retry/halt and closed original ledger; raw unknown is retained. Existing native runtime/ledger/extra schemas remain unchanged under new V10 outer schemas.

`export-result.json` schema is `full1220-v10-controlled-export-result.v1`, evidence_kind=`genuine_local_stub_owned_window_not_actual_ready`. It includes measured `old_stopped102_unchanged` and `selected_source_unchanged`, phase/timings/current receipt/native stats and explicit zero new actual calls/financial claims. Original protection receipts independently read old46 chain plus original9055 inventory (47257122bytes, exact campaign/records namespaces), original2315 source pins, all102claim pins and descriptor/source-confirmation/manifest/owner/stop/budget references before and after. Claim57 must remain absent. Both unchanged booleans can become true only after actual byte/SHA/length comparisons (plus selected Go namespace checks); they are not hard-coded. Raw records, campaign, scope, synthetic signature/manifest/provenance, OTLP bodies, native counts, source before/after maps and export inventory are retained in the new attempt output. Copied campaign material remains explicitly non-live/non-reopenable.

## Verification performed (compile/default only)

Private exact `git archive HEAD` plus this one new test file under `/tmp/v10-controlled-export-compile-20261031`. Pinned Go1.25.12, SDK26.5, GOENV/GOWORK/GOPROXY/GOSUMDB off, GOTOOLCHAIN local, GOTELEMETRY off, existing private cache. Command:

`go test -json ./internal/taskbench -run '^TestWindowV10(CollectedWindowFixtureExport|ControlledExportDeclaration)$' -count=1`

Final result: one intended opt-in skip, one pure declaration pass,0fail; package0.969seconds; stderr empty/no compiler warnings. Pure helper uses production execution mapping/totals and rejects a rewritten prefix first ordinal. Earlier same default/pure checks1.306s and1.417s were rerun only after output metadata and namespace-protection edits. All raw outputs are in `.tmp/v10-controlled-export-verification-20261031/`. `git diff --check` passed and final new file is byte-identical to tested private snapshot.

No genuine controlled-window success, 300second readiness, real sourceproof, fullGo success, actual freeze, or3660 completion is claimed. Root still must run and inspect the genuine controlled attempt, preserve any failure, and independently review/freeze its final exact archive and artifacts.
