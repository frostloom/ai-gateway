# Historical JSON validation allocation implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove avoidable value-tree allocation in historical typed JSON validation so the original3660-pair evaluation can execute inside its unchanged operation bound.

**Architecture:** Validate UTF-8 and standard JSON grammar, then scan object keys, depth and nulls without materializing values. Keep the original generic windowJSONObject parser for protocol consumers and malformed-input error fallback. Typed decoders still use encoding/json with DisallowUnknownFields.

**Tech Stack:** Existing Go1.25.12/encoding/json, no dependency.

**Spec:** Existing window_protocol.go duplicate decoded keys/UTF-8/depth128/trailing/root-object policy; window_actual_ready.go recursive non-null; stopped recording/quality legitimate null handling. Measured readonly R5profile attempt001: inventory0.131710708s, typed46 load395.078861333s; CPU JSON validation/allocation prominent. This is a performance hypothesis, not a proved original physical cause.

## Global Constraints

- Original3660pairs7320arms11772definedturns/1851boundaries, knowledgeoff botharms; global¥200+$4 unchanged.
- R5permanentlyclosed at56; no resume/reclaim/release/newpaid until new independent freeze.
- Business300/model90/JEV10 unchanged. No changes to routing, requests, customer behavior, knowledge or fee policy.
- Preserve original files, fresh physical history/End/FD/namespace/lock/context checks; no semantic cache.
- Directmain explicitly authorized; preserve unrelated userdist/integrations/plate_sim edits.

## Review Focus

- Escaped equivalent keys, including surrogate replacement, must reject duplicates as before.
- Nulls accepted in recordings/quality but rejected recursively in readiness.
- Primitive/container depths128/129 and root-object restrictions unchanged.
- Invalid UTF-8, malformed grammar, trailing values and unknown typed fields remain rejected.
- Large primitive values must not rebuild a generic value tree solely for validation.

### Task 1: Fast validation with original acceptance semantics

**Files:** Create internal/taskbench/window_json_shape.go and window_json_shape_test.go; modify only decodeWindowActualJSON in window_actual_ready.go, decodeWindowStoppedV2Recording in window_stopped_v2_bundle.go, decodeWindowStoppedV4QualityData in window_stopped_v4_evidence.go.

**Interfaces:** validateWindowJSONShape(raw []byte, nonnull bool) error; no authority, IO, cache or exported API.

- [x] Write TestWindowTypedJSONValidationAllocations over4096 numeric values and real decodeWindowActualJSON; assert exact typed values and <=128 allocations, then run against original decoder and retain RED.
- [x] Write strict validation cases/differential checks against original windowJSONObject: decoded duplicate keys, surrogate replacement, UTF-8, null policy, depth and trailing data.
- [x] Implement grammar gate json.Valid plus bounded scanner for keys/depth/null; malformed/nonobject cases delegate to original parser to preserve legacy errors. Skip string values; decode escaped keys using standard json.Unmarshal. Defer null rejection until structural checks finish. Integrate three typed decoders only.
- [x] Run focused normal/race + strict protocol/decoder adjacent tests; bounded differential fuzz; retain raw output.
- [x] Independent whole change review; fix material findings, rerun covering tests.

### Task 2: Real readonly history measurement and integration gate

**Files:** Private profile artifacts and candidate overlays only; no old source/archive mutation.

**Interfaces:** Task1 functions compiled as explicitly pinned candidate overlay into original R5readonly probe. Original raw2315source and175stopfiles stay SHA/bytes/identity identical; candidatecode pins recorded separately.

- [x] One new exclusive candidate invocation measuring same inertinventory and one typed46 loader, outside actual execution/financial authority. Preserve failed attempts; compare with original395.078861333s without claiming cold-cache control/general speed.
- [x] Verify original files unchanged and independently recalculate stage/profile facts. If inadequate, investigate profile before further fix; no paid scope.
- [ ] Only after performance correctness is established: exact new-source regression/independent technical freeze/new budget-bound scope. Full3660 delivery remains active; this plan alone cannot complete it.

Observed results: original allocation regression28723 ->32; focusednormal0.940s/race3.259s;8576 differential fuzz executions/4.200s;90adjacentprotocol/strictdecoder pass0skip0fail/0.551s. Independentcode8cd1463b.../actualprofile77c93bc0... noP1/P2. Candidatev2 actualone typed46 loader78.674732541s vs original395.078861333s; inputwitnesses2487 same and2315sources+175stopfiles identities/SHA/bytes unchanged. No business300s success claim. Initialcandidatev1 wrongoutputpath was refused by exclusive mkdir beforeanyGo/write; preserved and correctednewv2 alloutputpathsbound. Finalwhole-source regression/newscopefreeze remains pending.
