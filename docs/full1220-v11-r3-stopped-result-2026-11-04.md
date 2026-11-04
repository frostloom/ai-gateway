# Full1220 V11 R3 stopped result — 2026-11-04 verification

The original1220×3 target remains **3660 pairs /7320 arms /11772 defined turns**, with1851 boundary cases reported separately. It is unfinished. This document verifies the existing R3 run; it does not record a new paid execution or authorize reopening it.

## Permanent stop and acquisition partition

R3 permanently stopped in window50 at ordinal362, `change_plan-042/supplement_then_confirm`, `jev_selective`, turn2. The original owner session8832 exited1; PID/PGID80632 was absent at the2026-11-04 inspection. No window51 command or claim exists. The original owner cleanup records `leader_reaped=true` and `group_empty=true` **with PermissionError probe/signal errors**, which remain disclosed. The fresh process-group inspection found no members; it does not rewrite the original cleanup record.

The first49 windows contain358 normally collected pairs /716 arms /1132 observed turns, each with the original owner source/native data verification. Window50 attempted7 arms, finished6, and observed20 turns: three additional pairs finished, ordinal362/selective did not finish, and ordinal362/model was not attempted. Those stopped-window rows are not a normally verified window or replacement primary sample. Cumulative acquisition is723 attempted arms /722 finished arms /1152 observed turns, with6597 arms unattempted and10620 defined turns unrun. All original failure, cleanup, observer, advance and payload-verification fields remain unchanged.

The stopped summary is `f29a890ca00ddb7ac4c3041a08fcc4082faf28d7f2055856fba2a55a584820d2`; its window receipt is `21a1844a0e3011443fb37776424e9c05ee29f9b877e222403b04d23c6e55820e`. Both summary and terminal say `closed=true`, `acquisition_done=false`, `next_window_index=50`. This scope must not restart, continue, retry its command, reclaim, or release a permanent reserve.

## Recorded provider failure

The final native intent is model sequence18, `deepseek-flash`. Its recorded transport failure is `connection_reset`, `await_response`,1877ms, reused connection, `request_written=true`, `first_byte=false`, status0, no response body. This proves a recorded transport failure after request writing, not an invalid-key HTTP response, a supplier charge, or a physical network root cause. It is outside the frozen first-TLS-pure-EOF/zero-write retry predicate; no retry occurred.

Window50 has18 native intents:14 model and4 JEV;17 settled and one model intent remains pending with74752 microCNY held. A read-only audit checked all38 zero-based journal events and their original raw SHA chain, intent/packet identities,18 request bodies and17 response bodies. Configured known window cost is9164 microCNY +204 microUSD, matching the cumulative-summary delta. The first ad-hoc audit incorrectly assumed a one-based journal index and failed before writing an audit artifact; the corrected reader used the original index0 convention without changing evidence.

## Costs and observations

R3 recorded1495 model and362 JEV native attempts,1856 settlements, one pending intent and zero retries. Historical-plus-R3 recorded attempts are6659 model /1677 JEV; the historical series does not uniformly distinguish logical/native/retry attempts. R3 configured known cost is1303055 microCNY +18492 microUSD, not a supplier invoice.

All154 permanent claims remain:79620965 microCNY +838363 microUSD, including the old104 and R3's50 claims. These reserves are not spending and are not added to configured costs as a second charge. The pending74752 microCNY remains included and unreleased. Global authorization remains200 CNY +4 USD.

The terminal local spool is closed and contains2256 batches /6426 spans /12864632 encoded bytes /14574170 stored bytes. Its boundary remains `durable_local`, remote delivery `pending`, and `normal_closure=false`. **No R3 OTLP shipment, content readback or score publication has occurred.** The current final-only V11 cohort and delivery gates correctly reject this incomplete stopped acquisition; a closed disk spool alone does not authorize shipment.

## Independent reviews and evidence

The first49 windows now have complete independent, nonblind agent evidence reviews and pure score-plan contract checks:716 arms /3456 requirements; semantic404 passed /312 failed, strict394 passed /322 failed. These are not human gold labels or whole-cohort quality estimates. Original structural failures and positive-score suppression remain even where independent semantic judgment differs. The pure plans have no actual final-cohort or remote publication authority.

Window50 has a separate stopped-data review:6 record-level task passes, one unfinished failure and one not-started arm;49 original requirements partition into48 passes /one failure, with9 additional unobserved obligations explicitly not assessable. The original7 record hashes,8 scheduled identities, source cases, original statuses/errors and all49 requirement/reply bindings passed the root's read-only binding check. This is not a normal source/window proof, and the entire window remains score-held. The review discloses its interpretation of the two ordinal361 intent-clarifying replies and its inspection limitations. Review SHA: `f72c6aff992e1eeb9940541717d3161be9743f0260258959f3cd0e68f82a4a98`; binding-check SHA: `2b74ca05e59817d0753067c3385587380112441025861be3f525cb82dd29796a`.

The selected stopped-original inventory binds13125 files /60172507 bytes under the existing campaign, records, spool, owner artifacts, frozen manifest/binding/source seal and49 owner/data-verification pairs. Every selected file was checked by original FD identity, type/link count, byte length, SHA256 and final metadata. Inventory SHA: `2ff9ee20cbd11e51b5b31a63b9bf31f53625f602194b5bb0d123bff4a462df9c`. This is a selected original-data inventory, not a new full-source replay, normal completion or remote proof. The separate stopped native audit SHA is `8c41b95727fb79837e2935335edee65ac1a0d192eb827a856bb2ad147bbf3fcb`.

## Credential update and completion work

The user replaced the model key in the local `.env`; a local presence/difference check confirmed it differs from the consumed R3 settings. JEV's key is unchanged. `.env` is mode0600. No key value was printed, no provider validation request was made, and the immutable old settings were not edited. A future independently frozen configuration must consume the new key.

With all154 permanent holds, the original risk and20 model /20 JEV extra allowances retained, another complete new3660-pair run at the original caps would declare206966461 microCNY +2929662 microUSD, exceeding the200 CNY limit. A composition of R3's normally verified358-pair prefix and a new3302-pair suffix, original windows050..511, declares194352968 microCNY +2733684 microUSD. This arithmetic fits the existing budget but does not itself prove source equivalence, composed-cohort eligibility, historical spool shipment, or new execution authority. An alternative using the already delivered original385-pair prefix is under independent source/compatibility review. No new scope has been opened.

The next engineering decision must preserve every original slot, three-pass arm order, failures/NA, fixture isolation, raw historical holds, retry limits and real Langfuse readback obligations. It must explicitly document any primary composition and observation/timing differences. Current unrelated, uncommitted billing/gateway/UI work is not part of this frozen run and must not be overwritten or accidentally committed.

The subsequent independent design review found no actionable P1/P2 contradiction. V12 now selects the fixed already-delivered385-pair R5 prefix plus a fresh3275-pair suffix, declaring193330686 microCNY +2711060 microUSD including all154 historical holds, risk and maximum extras. See [V12 design](superpowers/specs/2026-11-04-full3660-completion-v12-design.md) and [implementation plan](superpowers/plans/2026-11-04-full3660-completion-v12.md). Typed154, exact source equivalence, original prefix delivery evidence, new-code regression and independent actual freeze are still required; this selection is not a new actual run or completion claim.
