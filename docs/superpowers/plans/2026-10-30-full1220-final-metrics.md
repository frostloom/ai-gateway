# Full1220 final read-only metrics implementation plan

> **For agentic workers:** Use subagent-driven-development for implementation and independent review. Continue existing authorized real acquisition concurrently.

**Goal:** Produce reproducible final metrics from the original 3660-pair campaign without changing acquisition, quality decisions, budget or publication.

**Architecture:** A standard-library Python postprocessor consumes an explicit SHA-pinned selection of the V7 manifest, terminal finality, original records/receipts/ledgers, and completed independent review plans. It writes a new JSON report only; no Go, DB, network, clients, owner commands, claims or score publication. It is a metrics calculation, not a replacement for the final independent raw audit or an opaque publication proof.

**Spec:** User's original full1220 comparison and metric requirements, plus the private read-only compatibility assessment `full1220-actual-v7-freeze-20261030/metrics-preparation/compatibility.md` (SHA b89f1abb0f021fd7ee3d9419d9519ccf950aaea8aa42019e4f208ec6fb2d0346).

**Tech stack:** Python standard library; private preparation directory under the current V7 root. Human has opted out of further design questions; choices here are autonomous engineering decisions.

## Global constraints

- Original 3660 pairs / 7320 arms / 11772 defined turns; 1851 boundaries separate. Same frozen source616c, scope8946 and manifest830. Knowledge off in both arms.
- Preserve all recorded negatives and independent semantic/strict decisions. Missing future identities come only from the frozen manifest, never fabricated observations or reviews.
- No alteration of frozen source, existing evidence, claims, scores or settings; default invocation performs no input reads. Actual aggregation requires explicit execution, input selection SHA and closed terminal finality. Partial closure must be labelled incomplete, never complete1220.
- Separate CNY model and USD JEV amounts. Reconcile settled costs with each window, preserve held/unknown, and include failed-task spend in cost-per-correct numerators. No FX conversion, billing claims or release of reservations.
- Cache absent/invalid is unknown, not zero. Four task clocks remain source-labelled; driver.Step includes oracle/guard and is not HTTP or user end-to-end latency. Missing/nonfinite times do not become zero.

## Review focus

- Duplicate, missing, mixed-scope or mismatched receipt/review/ledger identities must reject instead of changing denominators.
- Cumulative summaries must not be summed as independent window counts.
- Original negative with independent semantic pass retains strict failure and positive-score suppression.
- Missing cache pair, unsettled request or zero correct-task denominator must remain explicit unknown/unavailable.
- Complete-pair deltas must join the same ordinal/case/pass and retain actual arm order; partial prefixes must never masquerade as full results.

### Task 1: Minimal pure calculation and input binding

Create `metrics-preparation/final-metrics.py` and `test_final_metrics.py` inside the private V7 root. Define a pure calculation over explicit loaded evidence plus an inactive-by-default CLI. Use SHA-checked bounded regular files and explicit paths; never walk caches or discover arbitrary evidence. Cover identity joins, per-window versus cumulative denominators and terminal partial/full distinction.

- [ ] Write and run behavioral RED tests for duplicate/incorrect identity, wrong SHA, open finality, partial suffix and immutable original negative semantics.
- [ ] Implement the smallest reader/join that passes those tests; retain RED and GREEN outputs.

### Task 2: Requested comparison metrics

- [ ] Add hand-derived RED fixtures for mixed currencies, failed-task spend/zero successes, valid versus missing cache pairs, multi-turn clock sums, nearest-rank P50/P95, and pass/order-matched pairs.
- [ ] Implement totals and arm/pass/order strata, semantic and strict correct-task cost vectors, task latency distributions with valid/missing counts, raw cache coverage/tokens, model/JEV counts, and complete-pair descriptive deltas. Mark knowledge retrieval rates not applicable and distinguish supplied context from demonstrated usage.
- [ ] Reconcile ledger-settled currency totals with original per-window known totals; preserve unresolved holds. Emit original full population and observed coverage alongside every subset. No statistical/global benefit claim from an incomplete prefix.

### Task 3: Independent review and eventual actual run

- [ ] Run bounded Python tests and default-no-input-read verification. Do not rerun full Go or acquisition tests.
- [ ] Independently review input binding, joins, clock origins and literal arithmetic expectations. Fix only confirmed defects and retain prior artifacts.
- [ ] When the campaign closes, pin finality, final raw audit and every explicit evidence SHA, then run the reviewed aggregator once to a new output directory. Actual whole-campaign metrics remain pending until then.

Controller performs public commit/push; workers only write their assigned private preparation files and reports. This plan adds no model request, JEV request, claim or budget scope.
