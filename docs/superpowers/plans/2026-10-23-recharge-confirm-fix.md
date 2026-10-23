# Recharge confirmation boundary fixes

> Execute inline on main using test-driven-development and executing-plans; no deployment or paid calls. User permits increasing a candidate budget but has not specified a new paid scope/ceiling.

Goal: Fix both blockers from 97c8f40 without a second write router or hidden retries.

Design: After the existing pending-confirm branch, a complete confirmation phrase with no pending returns a truthful no-op before LLM/JEV. Compound new applications retain normal routing. For finite explicit current recharge applications, a leading affirmative recharge-success claim is invalid in the LLM path: repair once, then return the existing truthful limitation. Confirmed execution remains the direct handler branch. A recharge preview's accompanying leading false success prose is replaced without repeating preview/inference. Quoted, conditional, historical and negative prose is not reclassified as a new application. Update shared runtime contract. Valid historical v2 artifacts remain parseable but receive runtime_contract_mismatch and cannot become Ready, validate frozen execution or claim/run under the new contract; do not rewrite old scope constants, archives or consumed markers.

Tasks:
- [x] RED direct handler tests for both routing arms: empty-pending exact confirm bypass, false-success repair/bounded exhaustion, preview explanation suppression; non-confirm compound application/history/condition boundaries.
- [x] GREEN minimal shared execution guards and runtime contract update. No extra model/JEV attempts beyond one repair.
- [x] RED/GREEN historical v2 checker/CLI gate compatibility: read-only parse, no execution or claim on new contract; preserve exact identity/billing bindings and claim permanence tests.
- [x] Re-run frozen recharge confirm/duplicate probes; normal/duplicate/accepted-correction paths must be structurally safe and semantic needs_review. Sustained noncompliance must fail honestly without a write. Budget3 still cannot fit two repairs; candidate10 is declarative, not authorization or vendor evidence.
- [x] Full Go regression, independent review, correct important findings, archive new local evidence while preserving all old evidence. Scoped commit/push/remote verification, Hindsight write/readback.

Review focus: no-op must not swallow compound requests or real bound confirmations; exhausted correction must not claim write success; tool-bearing false completion must not leak; old contract artifacts must not be silently rebound; tests must reject real supplier traffic and not turn local_stub into quality or paid evidence.

Final verification: 18 Go packages passed, independent review no blocking findings; all old private source evidence hashes unchanged, owned fixture schemas0. Delivery commit/push/readback procedure follows this validated state; no paid or deployment step.
