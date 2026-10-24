# Current v5 six-case offline acceptance plan

> Execute inline on main per user preference. One final independent review. No paid calls or deployment.

Goal: verify the original six cases together using current50e3c8d customer Handler semantics, contractv5 a95cacd, with complete paired journals and authoritative isolated state.
Architecture: add a test-only unified acceptance driver around existing PrepareSmallPair/PrepareAgentPairConfig/RunAgentPair; select exact six original frozen cases in historical order, no definition changes. Literal loopback custom transport and FakeJEV only. All cases in one journal per path, fresh tenants per arm, shared bounded local budgets and one FakeJEV per selective case. No new paid scope, claim or schema.
Tech: Go existing taskbench fixture/oracle/observer and JSON evidence, Python local hashes only.

## Task1 — unified acceptance

Files: internal/taskbench/six_case_v5_offline_test.go; docs/v5-six-case-offline-2026-10-24.md; eval/paired-pilots/2026-10-24-v5-six-case-offline/.
- [x] Assert exact six frozen keys/turns/profiles; all12 arms same runtimecontract/model/protocol/prompt/tools. Max128 output, loopback request target only, allow_paidfalse, local_stub.
- [x] Normal: explicit balance reads both rounds, first balance FakeJEV fastread; amount-missing clarify, then real preview and deterministic confirm/cancel. Both arms independent state, protected controls/allowance/subscriptions/order invariants.
- [x] Repair: unsupported recharge/refund success gets at most one correction; second cash read omitted twice with amount claim recovers exactly once via audited execution-layer read. Require freshread and correct state, do not credit model/JEV.
- [x] Negative probes: missing second-round cash read without recoverable amount claim must fail; wrong refund request amount despite balance cap must fail. No weakening frozen expectations; if detection gap found reportblocked and diagnose first.
- [x] Capture all four paths through overlay only for preserving local journals/requests; original source test uses t.TempDir. No real provider requests or Langfuse score writes.

## Task2 — verify and deliver

- [x] Targeted tests and standard fullGo regression, SDKROOT26.5, unchanged parallelism/locks/default timeout; independent final code/evidence review.
- [x] Validate12rows/path, complete observed rounds, contract/source hashes and request envelope, negative cases remain failed, automaticneeds_review not semanticpassed. Old private evidence SHA unchanged, isolatedDB0.
- [x] Document readiness as same-version local acceptance only, not real quality or benefit; no new paid entry/claim/auth. Hindsight durable write/readback and exact commit/push milestone.

Review focus: fixture scripts cannot fake business execution; stale read/wrong requested amount negatives effective; source/metadata common across6; pending binding/confirmation once/cancel no write; no mutation to frozen cases or old claims; local counts not paid estimates.

Task1 verified: four paths12arms/26turns each, normal22/repair30/missingread20/wrongamount22 synthetic models,6FakeJEV each. Initial setup failures retained (nil writeOperation for read, fake model ID, cancel trace, expected diagnostic wording); no production or frozen input changes.
Task2 verification: fullGo first17packages pass/consumer KafkaNotLeader failure, taskbench401.533s pass including newtest; bounded unchanged Kafka test12.26s pass non-skip, readonly current60partition leaders present. Root unconfirmed. Independent review noP1/P2, reviewer integration sandbox blocked.3263oldprivate hashes unchanged/DB0.
Ruling: deliver the scoped offline acceptance with initial full-project failure explicitly retained, rather than claim all-green or infer Kafka permanently fixed; only test/evidence changed and relevant unified test passed in full run. Cost if wrong: shared-service interference remains unresolved and must block future real-entry validation until a complete gate passes. No new paid readiness is asserted.
