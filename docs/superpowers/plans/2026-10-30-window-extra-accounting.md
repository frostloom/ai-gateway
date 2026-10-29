# Window accounting with permanent unknown liability

Implement directly on main per the user's ongoing development authorization. This is zero-paid preparation; current ¥130+$1 and global no-retry execution policy remain unchanged. V7 stays permanently closed. No new owner, financial claim, model/JEV request, settings read, DB, deployment or runtime activation.

## Design ruling

Keep the existing per-window financial claims and separation. Park the replacement parent pool. In a future separately approved policy, at most one eligible same-logical-call retry may follow a TLS handshake EOF. The failed native attempt's maximum liability remains permanently unknown. Before retry, a separately verified, exclusive and durable extra claim of exactly that maximum augments only the current window's funded cap. No cross-window borrowing or hold release.

Candidate campaign extras are at most 20 model and 20 JEV, maximum ¥1.495040+$0.053760. With original fresh full-range base caps, 44 historical claims and retained risk, the candidate total is ¥139.218667+$1.053354; proposed ceiling ¥140+$1.1 is **not approved**. These figures are maximum declared reservations, not invoices or completion guarantees.

## Isolated implementation unit

First review the private `continuation-design/window-extra-accounting-spec.md` under the V7 preparation root. Then extend the private window ledger with a new v3 schema and a focused new helper/test file. Preserve v1/v2 serialized bytes, validation and fatal behavior. Existing actual entries must continue to produce only their fixed old contracts; no v3 runtime issuer is part of this unit.

The contract remains immutable. New v3 state distinguishes known cost, all unknown holds, the current pending hold, confirmed extra funding and a required retry association. Native attempt 1 may move to unknown without discarding its identity/descriptor digest. It blocks fresh logical requests. A funding event must bind that exact unknown, provider, maximum amount and unique claim digest. Native attempt 2 must retain the complete logical request identity and body, receive its own sequence/ticket and require the funded association. Its successful settlement clears only its own pending hold. A second failure keeps old fatal stop; no third attempt or new logical key bypass.

Compatibility-test ruling: `TestWindowAccountingCacheSchemaBounds/unknown` previously used v3 as its unknown-schema sample. Change only that token in `window_accounting_cache_test.go` to v4, keeping the assertion unchanged, because v3 is now the supported version. Preserve the original failing run; do not weaken any v1/v2 assertion.

Admission requires `known + all unknown + next maximum hold <= original cap + confirmed extra funding`, independently by currency. Enforce attempt bounds, checked arithmetic, unique identities, single pending request and no changes after close/poison. Persist events before applying their effects. Replay verifies canonical events, hashes and transitions but never returns an executable owner or completion proof. Snapshot must deep-copy new collections.

This unit records funding facts supplied by a future verified campaign adapter. A digest is not proof that a financial claim exists. TLS permission, exact O_EXCL/fsync claim creation, the global 20+20 quota, current-driver ownership, source binding, logical 5-model/1-JEV limits, request deadline and native payload/observer integration remain explicit subsequent gates. Accounting methods alone never authorize egress.

## Verification

Behavioral RED before implementation; selected GREEN and race on the final code. Cover unknown retention after retry success, exact funding and difference-of-one rejection, cross-provider/body/logical mismatch, duplicate funding/ticket, second failure/global stop, new-key escape, all persistence failures, canonical replay tampering, deep snapshots, closed/poisoned rejection and old v1/v2 compatibility. Preserve every failing log. Use fixed offline Go/SDK and bounded tests; complete-source regression once after integration, before a new execution freeze.

Root obtains independent spec/code review, freshly verifies the final unit, and stages only its explicit files before direct-main push. Keep unrelated user web output and all historical archives untouched.
