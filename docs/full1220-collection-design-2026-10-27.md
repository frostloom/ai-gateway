# Full1220 recording scope: proposed design, 2026-10-27

Status: design only, not a paid-ready scope or an execution approval record. The user's autonomous-work authorization remains valid, but no new claim may be consumed until the concrete source, schedule, envelopes, permanent chain, accounting and acceptance evidence are frozen. Old scopes and guards remain unchanged.

## Actual target and evidence boundary

Record the1220 frozen primary complete tasks in model versus jev_selective mode, with at least three interleaved passes:3660 planned pairs,7320 arms and11772 defined user turns. Selection identity remains `b9b6f48daccffa6bd7c0f86e3f8d4857eedc719404406014d3426b0656edd2f8`. Keep1851 boundary scenarios separate. Configuration preparation, the local substitute matrix and historical six-case pilots do not complete this target.

Freeze every case/pass/arm/window assignment before recording. Counterbalance first-arm order across case/pass strata and disclose that three passes give each individual case unequal first-arm counts. Negative outcomes do not change later pass order or cause a fourth pass. Both arms use the same customer50e3c8d/v5 prompt, tools, fixtures, model, protocol,128-output bound and five-loop limit; only the JEV routing variable changes. A later customer revision needs its own explicit binding.

## Preserve quality negatives without losing safety stops

Current full-primary APIs require all four strict guards, and the campaign runner independently rejects structural failures. Their scorer mixes unmet goals and real safety failures in the same Errors/StructuralPass fields. Simply disabling a guard is unsafe and will not enable full collection correctly.

Add a distinct versioned recording policy and narrow entry. Preserve old API behavior and old JSON. New typed issues are produced at validation origins; never classify error strings. Keep the original Errors, StructuralPass, Status and Success values. Collection disposition is additional evidence, not a semantic verdict.

| Evidence | New collection disposition |
| --- | --- |
| Complete execution evidence; wrong/incomplete answer, unnecessary clarification, safe wrong read, missing preview/execution with no prohibited effect | Retain negative or needs_review and continue the fixed schedule. |
| False factual reply or false completion words, with authoritative evidence proving no prohibited effects | Retain mismatch; continue only if every safety/evidence invariant holds. |
| Wrong pending amount/target, unbound or duplicate execution, cross-tenant effect, premature/cancelled write, corrupted ledger or foreign controls | Halt before a later scripted turn, arm, batch or window. |
| Unknown issue/tool/phase, missing or malformed required trace/pending, identity/oracle uncertainty | Halt closed. Empty trace, HTTP200 or assistant prose alone cannot prove safety. |
| Factory/setup/step/final/cleanup, provider/HTTP/JEV/observer/journal/durability failure, missing usage, exhausted cap or changed runtime/claim | Halt; retain unknown charge and failed/unrun denominator. No fallback, retry, recovery or new window claim. |

Checks must run after each observed turn, before a scripted confirmation can execute a wrong preview. A post-arm classifier is too late. The zero-write missing-confirmation case may be an unmet goal; multiple or wrongly bound actual writes are a safety stop. Terminal assertion names alone do not distinguish those cases. Unknown or untyped errors always halt.

New pair and window completion means recording completed. It cannot change a task-negative into a task pass. Persist result/issues/disposition, validate runtime and accounting, and flush observation before advancing. Replay must recompute the disposition from raw source/state/pending/trace evidence and reject an altered label.

## Budget design to implement separately

Historical actual evaluation usage remains143model/37JEV. Nine immutable claims permanently reserve29,061,120microCNY and223,104microUSD. The remaining approved ceiling is70,938,880microCNY and776,896microUSD. Never release, reclaim or resume those claims.

The old three-pass all-attempt envelope is1,988,997,120microCNY plus15,821,568microUSD, beyond the remainder. It is a conservative declaration, not a forecast. Existing PairBudgetTransport and CampaignBudget permanently charge worst-case attempts/child envelopes. An outer wrapper cannot silently turn those old contracts into settlement accounting.

Recommended new design: predetermined windows with permanent per-window claims and a separate durable request-admission ledger. The top-level full schedule declares the ceiling but is not a second permanent claim. All window caps, plus any separately paid review/measurement allocation, must fit the remaining currency independently. A window's whole cap remains permanently consumed after success or failure; unused cap cannot fund another window.

Inside one permanently claimed window, allow one external request in flight across both providers. Before I/O, sync an intent binding claim, sequence, case/pass/arm/turn/attempt, exact request SHA, model/version, rates, bounds and maximum-charge hold. Admission requires known conservatively priced usage plus unresolved holds plus the next hold to fit that currency's cap. A valid bounded successful usage record must be durably settled before returning to the Handler. Replacing a transient hold by known usage inside the same window never releases its permanent cap.

Transport/HTTP/parse/durability failure, missing or excessive usage, or a crash with unresolved intent stops the entire collection. Keep the unresolved maximum hold and unknown-charge status; never price it as zero or call it an invoice. Reopening is read-only audit, not recovery. A provider envelope violation is reported as an accounting incident; local caps cannot guarantee supplier billing behavior.

The global claim chain and request ledger need exclusive admission, integer arithmetic and file/directory sync. Reject racing processes, missing/truncated/orphaned records, changed identities or stale locks. Successful checkpoints advance only to the predetermined next window; failure does not.

A single permanent root cap would waste less unused window capacity, but could consume the entire remainder on the first failure. Window caps limit that exposure. No numeric cap or window width is chosen by this design: affordability and latency must be derived from complete request capture and existing actual evidence, with sensitivity to repairs, fallback, unresolved holds and cap waste. Do not promise all1220 will finish merely because a hard stop prevents more requests.

## Measurements and acceptance before paid execution

1. Finish the complete local Handler matrix. Capture actual serialized OpenAI/model and full JEV request paths for all primary tasks, including history, tools, repairs and loop bounds. Bind tokenizer/version/SHA and conservative input bounds. Synthetic usage and observed normal maxima are not a proof of all future paths.
2. Verify then-current official prices, cache/failure billing uncertainty and exact model versions. Retain conservative bounds where smaller bounds lack evidence. Use peak configured integer rates, separate currencies, and state that these are not invoices.
3. Freeze three-pass schedule, initial-validity/negative/safety policy, source/runtime, request envelopes, window deadlines <=300seconds, caps and all nine predecessor SHA links. Derive windows from measured setup/cleanup and actual latency evidence; local substitutes alone cannot predict remote timing.
4. Prove admission/durability/crash boundaries with zero-external-call assertions. Test exact-fit versus one micro-unit over, invalid usage, JEV failure blocking model fallback, racing claims, stale/orphaned evidence, no recovery/release and no next-window claim after halt.
5. Prove quality-negative continuation, per-turn unsafe-preview halt, wrong actual effect versus zero-effect unmet goal, unknown-issue halt, and unchanged strict legacy APIs/JSON. Replay old full local evidence under the new checks and keep new local fault cases independent of historical outputs.
6. Require full original-configuration regression, independent review, isolated-resource cleanup, exact archived executable/source binding and read-only service/price checks before consuming the first new window claim. User autonomy avoids another routine confirmation; it does not waive readiness or budget limits.

## Final report and Langfuse

Every planned source/pass/arm appears once, including negatives, pending review, interrupted and unrun entries. Report fixture-invalid cases separately. Keep defined versus observed turns distinct. Independent semantic review binds exact requirement/reply/state/trace/source SHA; synthetic or stale reviews cannot certify recorded runs. Preserve original result records and publish only supported deterministic/independent scores, once, with readback verification and no republishing for visibility delay.

Full quality comparison uses the valid planned denominator and discloses unknown/unrun coverage. Survivor-only efficiency is not a whole-set benefit. Keep currencies and four timing boundaries separate. The final goal remains incomplete until the actual full collection, independent review, score/content readback and traceable report are verified and pushed.
