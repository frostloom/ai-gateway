# Full1220 permanent-window accounting implementation plan

> Execute inline under existing autonomous authorization. No real provider call, paid claim, chosen production cap, deployment or old-claim modification is authorized by this implementation plan. Direct main; preserve unrelated web changes.

**Goal:** Implement the distinct bounded accounting/claim primitives and guarded provider integration needed to acquire the full fixed three-pass1220 comparison inside the existing total budget. This does not redefine completion as the accounting unit.

**Spec:** docs/full1220-collection-design-2026-10-27.md; frozen original contract; current nine canonical claims/latestf09de5bb. Actual143model/37JEV; old permanent reserve29,061,120microCNY/223,104microUSD; remainder70,938,880/776,896. No exchange between currencies, releasing old reserves, resuming consumed scope or automatic retry.

## Design decisions

- Use a new full schedule with permanent per-window claims. A top-level declaration is not a second permanent claim. All predeclared window caps plus any separately paid review/measurement must fit the remainder before the first claim.
- Within one permanently reserved window, one request across both providers may be in flight. Sync its intent/max-charge hold before external I/O; admit only when known conservatively priced usage + unresolved holds + next hold fits that currency. Persist valid bounded usage/price before returning success. This changes transient admission inside a NEW contract, never old permanent reservations.
- Unknown charge, malformed/missing/excess usage, external/HTTP/parse/durability/runtime/observer/isolation/safety failure stops expansion. A failed or interrupted window is audit-only, never resumed or retried. Success checkpoint alone advances the predetermined next window.
- Keep old PairBudgetTransport/CampaignBudget/AuthorizedPilot schemas and behavior unchanged. Old campaign admission permanently sums child worst envelopes and cannot be relabeled as settlement. If old conservative child transport is retained as an inner request/count bound, explicitly separate its counters from new global permanent caps and known-usage accounting; no double counting or underfunded child promise.
- No new public paid CLI until verified source/schedule/rates/envelopes/prior SHA and opaque claim capability exist. Accounting primitives do not themselves authorize or send a provider request. Tests use new temporary synthetic scopes only.

## Implementation tasks

- [ ] Define a narrow versioned private window contract/state/event schema with integer rates, separate provider bounds/currencies, policy/source/schedule/window/claim identities and maximum attempts. RED exact-fit/one-unit-over, zero/negative/null/overflow, wrong provider/model/identity and detached snapshots. Caps remain synthetic test values.
- [ ] Implement new-only exclusive ledger creation with file/directory sync and a deterministic read-only verifier. RED intent-before-I/O, one cross-provider inflight request, request identity/body SHA/ordinal binding, partial/truncated/duplicate/out-of-order/stale/orphaned events, crash before/after settlement. Reopening never exposes mutation/resume.
- [ ] Implement known bounded usage settlement and unknown-charge halt. Retain maximum unresolved hold on failure; keep known prior usage visible; permanently reserved cap never decreases. RED price rounding per component, cache/usage variants, excessive output/input, missing usage and serialization/fsync failures. Assert no subsequent admission after any halt.
- [ ] Implement separate global permanent window claim chain with exclusive admission across processes, all nine canonical predecessor SHA bindings and fixed full schedule. RED cap sums exceeding either100+1 currency, missing/changed predecessors, racing different-window claims, wrong order/policy/source, partial claim persistence, previous pending/failed checkpoint. Never delete/release a created claim to recover from a failure.
- [ ] Add guarded model and JEV request adapters using the new opaque validated capability and ledger. Bind exact serialized requests, official endpoint/model/rate/bound declarations and actual response usage. RED zero upstream calls on missing capability/bounds/cap/intent-sync failures; failure/missing JEV usage blocks model fallback. Test only closed transports/loopback substitutes; no real credentials or requests.
- [ ] Integrate minimal new window lease/checkpoint ownership without accepting old leases as equivalent. Task-quality dispositions preserve negatives; technical/safety/unknown issues halt. Any task-only guarded proposal termination must be separately specified and require positive owned cleanup before another arm; current corev1 globally halts wrong preview. No blanket structural bypass or error-string allowlist.
- [ ] Independent whole-unit review; RED→GREEN fixes; once stable, original Go20minute/full Python/race and isolated resource checks. Freeze source/build, archive synthetic boundary evidence, AGENTS/Hindsight readback and main push. Do not claim paid-ready or actual full completion.

## Remaining actual-scope prerequisites

Complete exact real-protocol request capture across all1220 tasks and tool/history/repair/loop bounds; verify current official prices/models and derive conservative pre-I/O envelopes. Measure window time and cap-waste sensitivity using full local evidence plus existing actual observations, clearly separate forecasts from hard bounds. Freeze actual caps and window assignment only after evidence; no numeric window width or cap is selected here. New actual claims can execute under existing user preauthorization only after those gates pass.

Keep all3660planned pairs/7320arms/11772defined turns and1851separate boundaries in the final work. Recording failures/partial/unrun slots stay visible; complete actual acquisition, independent semantic review, Langfuse score/content readback and traceable report are still required after this unit.
