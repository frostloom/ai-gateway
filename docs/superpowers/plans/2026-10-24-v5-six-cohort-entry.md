# Fixed current-v5 six-case cohort entry implementation plan

> Execute inline on main; final independent review. User delegated routine design and continuous progress. No paid execution or new claim during preparation.

Goal: prepare a single independently authorized six-case engineering acceptance on one current customer/runtime and final executor, with bounded failure-stop and permanent reservation.
Architecture: explicit paired-live -cohort, separate CheckAuthorizedCohort and RunAuthorizedCohort, shared private AuthorizedPilot/billing/proposal primitives. Do not admit this identity through -retest or legacy pilot/continuation. Six sequential case batches, one fixed cohort claim before fixture creation, complete only after all six, stop at failure and never resume or retry. All six after human approval are one frozen scope, not scope expansion. Not a repeated/order-balanced population benefit study.
Tech: Go existing campaign lease/model/JEV/fixture/observer machinery and CLI; JSON immutable public false templates/private proof later.

## Frozen design

- ID v5-six-case-cohort-20261024, auth/billing/receipt separate v1, pin customer50e3c8dab14b4b6cc25322fd340a274e6970200b/contracta95cacd041830012619a0f4d7fd8208c184aa9e8d45b7ee05afaa239a30e1e8f,647ba9f offline manifest/acceptance,5c2f669 diagnostics and100+1budgetSHA. Exact final executor revision bound later.
- Original ordered cases balance001/balance020/confirm/cancel/supplement/refund. Caps[6,12,10,10,12,12], total62model/2095104microCNY plus6JEV/16128microUSD;128output,16384bytes/input declared ceiling. Existing finite allowances except first balance6 allows3perarm read+answer+one correction; no guarantee every legal model loop completes.
- Permanent new scope allocation includes whole62/6 before execution even if first batch fails. Previous five allocations1824768/13440 retained; new cumulative3919872/29568; unallocated96080128/970432 under100000000/1000000.
- Five exact consumed claim SHAs required before any new claim: confirm2c9d64b9,diagnostic23047a14,cancela9ce7a22,supplement824e1e44,refund9a026381. Missing/tamper rejects without creating;O_EXCL/fsync fixed path; output/revision change cannot reset. No generic budget manager or legacy allocation rewrite.
- Provider/JEV/usage failures halt guarded attempts; add opt-in evaluator structural failure stop after persisting first failed arm, solely used by cohort runner. Legacy default behavior unchanged; not customer runtime behavior. Root report has completed prefix/failed batch/unstarted suffix and observed usage, separate full scope reservation from campaign prefix leases.

## Task1 — scope gates TDD

Files: new internal/taskbench/authorized_cohort.go/tests; private cohort discriminator in AuthorizedPilot; billing and preparation registration; CLI-cohort.go/tests + explicit flag routing. False plan/auth/billing and five public claim copies.
- [x] RED exactchecker/CLI gates (including coherent plan+auth expansion, historical crossuse, batch order/case/caps/identity); implement only fixed scope.
- [x] Five predecessor missing/tampered, consumed claim revision/output replay, durable sums; default/execution rejection before env/fixtures when unapproved.

## Task2 — guarded runner TDD

New authorized_cohort_run.go/test; minimal opt-in structural stop in AgentPairOptions/internal runner, no default behavior change; aggregate per-batch usage and whole-scope reservation in receipt.
- [x] RED normal/repair six batches and provider/JEV/structural failure prefix-stop, unknown usage, output reuse and wrong runner rejection.
- [x] Local realHandler official-target customtransport scripts/FakeJEV: normal22/6,repair30/6; batch2provider503 stops3model/1JEV, batch2JEVEOF6model/2JEV, missingfresh first failed arm5model/1JEV, no later cases. Counts local observations only.
- [x] Persist journals/request/receipt by overlay; six original task states/metadata and no business writes before confirms; content/usage guards and no retry.

## Task3 — readiness and delivery

- [x] Standard fullGo18package regression current exit0 required before any paid-ready request; preserve prior Kafka failures/rootunknown. Independent review, old hashes unchanged, isolatedDB0.
- [x] Official target serialized request capture/token envelope; current public pricing and settings/auth onlyread; no invoice guarantee. New human authfalse.
- Delivery procedure (post-commit result recorded in private final-binding-check.json to avoid changing the frozen executor SHA): Docs/AGENTS/Hindsight and exact main commit/push; after final reviewed commit archive executor/provenance and false private authorization+fee evidence matching exact SHA. Only human-authorization blocker may remain before asking new exact62+6scope approval. Never paid on continue.

Review focus: no cross-scope/coherent-widening escape; full permanent sum vs per-prefix leases; structural failure firstarm saved before stop; no fallback afterJEV failure; original scopes and customer unchanged; receipt usage unknown not silently zero; shared observer failures stop; no same-version real quality/population benefit claims from stubs.
