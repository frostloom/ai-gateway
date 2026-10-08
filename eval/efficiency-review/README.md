# JEV efficiency review

Independent Python standard-library reporting; this script never edits cases,
source reports, existing metrics, or historical experiment assets. Compare a
fresh same-code three-arm experiment on the entire 1200 normal + 20 adversarial
suite. A reduced run is labeled `full_suite_1220: false`, never presented as the
full-suite result.

```powershell
python eval/efficiency-review/analyze.py compare `
  --baseline noJEV.json --hint JEVhint.json --fast JEVfast.json `
  --out eval/reports/NEW-efficiency-run
python -m unittest discover -s eval/efficiency-review -p test_analyze.py
```

Input JSON or `.json.gz` is the actual `cmd/eval` schema: `SchemaVersion: 2`,
`JevMode: off` for baseline and `on` for hint/fast, `Model`, `DatasetSHA256`,
`Preds`, `AdvPreds`, `Metrics`. Workers and run mode must also match. IDs,
complete `Sample` content, and normal/adversarial membership must match across
all three reports; input order need not match. Duplicate IDs and missing result
fields are rejected. `Latency` is Go `time.Duration` nanoseconds. Capture and
retain run configuration/provenance separately: `JevMode: on` alone cannot
distinguish hint from fast. Prefer newly generated reports with the new telemetry.

Output is `comparison.json` plus a self-contained `comparison.md`. Any existing
output directory is rejected, even an empty one. Every input file's path and
SHA256 are saved. Source metrics are retained as reference but calculations use
the individual predictions, not a changed scoring definition.

Reported definitions:

- Full request mean and nearest-rank P50/P95 include failed attempts and JEV
  overhead. Successful-only latency is a separate explicitly labeled subset.
  Normal, adversarial, and combined populations are all shown.
- Paired latency uses exactly the same IDs successful in **all three arms**.
  A second explicit subset contains fast-selected IDs successful in all three.
  Per-sample latency differences retain negative regressions. Included IDs and
  subset sizes prevent different success rates from masquerading as speedups.
- Fast selection includes execution failures. Successful normal fast-path
  coverage divides by **all normal attempts**. Accepted-path accuracy also
  divides by all selected paths, including failures; a successful-only version
  is separate. Strict first-tool matching follows `IntentMatched`, including
  the chitchat/no-tools rule and normal false blocks. This is a **tool path
  metric**, not an independent test of business completion.
- Confidence bins are [0,.5), [.5,.8), [.8,.9), [.9,1]. Scores are uncalibrated
  predictions, not true probabilities. Missing confidence and fast status are
  listed; incomplete fast status makes overall coverage unavailable.
- Fallback reasons exclude selected paths; selected tool errors remain in
  failure details. All HTTP/nonempty-error failures, adversarial tool/pending
  routes, normal blocks, JEV errors, and risky accepted IDs remain visible.
  Safety observations do not establish confirmation correctness, funds safety,
  or tenant isolation; use functional regression tests for those claims.
- Real LLM attempts avoided compare `llm_calls` on **the same fast-selected
  IDs**, including failures. Missing telemetry makes the total unavailable;
  observed complete-subset differences and excluded IDs remain visible.
- Raw token totals sum observed usage across all attempts, including failed
  requests and fallbacks. Completeness requires usage responses covering every
  LLM attempt and zero reported missing usage. An explicit zero-LLM count needs
  no usage response. Missing telemetry is not a zero count. Accepted/fallback
  usage populations and complete/missing selected IDs are separate.
- Matched token differences compare only identical IDs with complete usage in
  all arms and report excluded IDs. No extrapolation to the excluded samples.
  Partial raw totals must not be described as full-run savings.

Optional unit prices:

```json
{
  "currency": "YOUR_CURRENCY",
  "cache_hit_input_per_million": 0.0,
  "cache_miss_input_per_million": 0.0,
  "output_per_million": 0.0,
  "jev_per_request": 0.0
}
```

These are explicit configuration fields, **not supplied tariffs**. Replace all
values with documented prices, including JEV's request tariff, and pass
`--prices path/to/prices.json`. The three LLM prices and currency are required.
Choose exactly one JEV billing basis: the explicit `jev_per_request` assumption
above, or replace that field with both of these token prices:

```json
{
  "jev_input_per_million": 0.0,
  "jev_output_per_million": 0.0
}
```

This second snippet shows only the replacement JEV fields; retain currency and
all three LLM fields in the complete configuration. The two billing bases are
mutually exclusive and cannot be summed. Replace the example values with your
documented tariffs. An explicitly documented free output tariff can be `0`;
that price does not excuse missing output usage. Token estimation requires real
`jev_input_tokens` and `jev_output_tokens`, `jev_usage_responses` covering every
actual `jev_calls` attempt, and `jev_usage_missing: 0`. Raw JEV usage, complete
and missing sample IDs, chosen pricing basis, and observed units are saved.
Unreported tokens are never assumed zero; partial usage is never extrapolated.
Explicitly zero JEV calls need no usage response. The request-based assumption
requires an observed request count and does not require token usage.

Estimates save assumptions, observed units, and JEV request counts;
they are not actual invoices. Costs stay unavailable when prices are omitted,
usage is incomplete, JEV counts are missing, or OpenAI cache-hit/cache-miss
accounting is missing/inconsistent. Anthropic raw usage and cache-creation tokens
are preserved, but cost estimation is unsupported rather than pretending its
cache-write units are OpenAI cache misses. No tokenizer-derived tokens, fabricated
usage, or inferred JEV fees are used. A missing JEV tariff cannot establish total
monetary savings.

Separate sequential runs can have different upstream conditions. The report
offers observational paired comparisons, not a causal speedup guarantee.
