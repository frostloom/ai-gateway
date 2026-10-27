# Full1220 owned quality-end policy v2

Design only; implement after the window execution checkpoint. Preserve the old v1 policy ID/SHA604c68b1c192c17594ba422b85367579cabd60c31623af6a9632278e401ae970, public runners, original issues and JSON. Reuse the existing scope/campaign/ledger/collector. A separately frozen v2 acquisition-policy SHA flows through the existing scope/execution/claim identities; no actual cap or scope is selected here.

## Behavior

A known successful, source-wrong preview may end THIS arm as a quality failure, before a later confirmation reaches Handler. Initially admit only preview-origin candidates that already stop the original v1 runner on that observed turn. Do not truncate a source or pretend a shortened record is a completed/replayable v1 task. A bare issue-name allowlist is insufficient: pending_identity mixes source mismatch and corrupt actual identity.

Before another arm, prove all of the following from the same concrete live owned execution:

- Normal HTTP200, complete runtime/trace and bounded settled provider usage; no transport/observer/ledger failure or concurrent operation.
- Exactly one known current-emitter successful preview (`phase=preview,outcome=pending,confirmed=false`), no failed/unknown/execute trace. Successful known reads may coexist.
- Previous pending absent. Response pending, actual preview trace and authoritative private Redis pending agree on tool/typed resolved arguments/owned tenant. Capture actual Pending Preview and IdemKey; there is no top-level session tenant field.
- Finite supported argument/target schema: positive integral bounded money, known owned paid refund order or uniquely resolved known plan as applicable. Foreign/unknown targets, hidden or inconsistent pending, malformed arguments and unsupported emitters globally halt.
- Source mismatch is positively present, independently of trace/pending self-consistency. Correct expected previews must continue their original confirmation schedule.
- THIS-turn business, foreign controls and unrelated state are unchanged against the preceding safe capture; check full balances/orders/topups/refunds/subscription/quota/independent billing evidence. Missing queries or time-boundary uncertainty halt.
- Exact audit completeness: match synchronous read/preview trace operations to independent SQL audit-row deltas. Preview audit result is `pending_confirmation`. Existing rows stay identical; no unexpected extra row. Do not require consecutive IDs. `sameDomain` ignores audits and cannot supply this proof; Handler.audit swallows SQL failure, so missing rows halt.

Quiesce this arm before finalization: revoke its active phase and refuse another Step or confirmation. Do not start another arm while certification is pending. Capture Final and preserve immutable original v1/candidate records first. Then delete only the exact owned miniredis session key, require one deletion and absence, re-read business/control state, and flush the ended task root with zero observation failures. Persist cleanup evidence separately; the final v2 envelope references the pre-cleanup and post-cleanup digests. Only the genuine live registry may mint an owned quality-end proof. Any predicate, persistence, cleanup, final-state or export failure globally halts.

The isolated synchronous/quiesced environment permits consistency checks at before/current/final/post-cleanup observation points. This does not claim a transactional SQL/Redis snapshot. Concurrent/background changes, missing evidence and billing-cycle uncertainty halt.

## Integration and verification

1. Add a private v2 policy/helper and sidecar verifier; keep the old raw v1 assessment intact. Use typed origin facts, not Error strings or assistant prose.
2. Add the narrow mode-aware capture hook only if the existing typed origin record cannot supply the necessary distinction. Default nil mode preserves old behavior.
3. Teach the owned driver to latch a candidate, quiesce, gather authoritative pending/audit/effect facts, retain pre-cleanup records, clean/read back/flush and seal the live proof.
4. Add a typed union to collector/campaign evidence. Complete arms still require ObservedTurns==Defined. Quality-ended arms require genuine registry proof plus Observed+ExplicitUnrunSuffix==Defined and exact source-rule/suffix bindings. Never replace equality with a generic <= check.
5. Keep all3660pairs/7320arms/11772defined turns and1851separate boundaries visible. Preserve failed/needs_review/Successfalse, and distinguish observed, quality-end-unrun and global-unrun turns. Exclude the entire affected pair from complete-task timing/call/cost savings; show observed expense separately. No retry, replacement arm, fourth pass or cap release.
6. RED→GREEN positive known-own wrong amount/order/plan/tool cases and negative hidden/foreign pending, failed/multiple traces, actual write, audit omission/extra/change, future confirmation, forged proof, cleanup/final/export/durability failures. Recheck v1 SHA/JSON and original runner behavior. Run related integration/race and independent review before direct main delivery.

Independent read-only design review found the narrowed policy implementable; audit result matching and immutable pre-cleanup versus final envelope separation above incorporate its recommendations. The design creates no execution permission or paid readiness by itself. Actual envelope, coordinator, authorization, full regression and freeze gates remain mandatory.
