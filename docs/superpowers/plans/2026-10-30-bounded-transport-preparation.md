# Bounded transport preparation

The original 1220 × three-pass target and both routing arms remain intact. V7 is permanently stopped; no old owner, claim, request or failed arm is retried. Current authorization remains ¥130 + $1. This plan authorizes zero-paid engineering only. A new retry policy and any larger budget require a concrete final authorization before execution.

The candidate keeps the Handler blocked in the same logical HTTP call. It never re-enters a turn, tool or confirmation. A narrowly eligible TLS handshake EOF could receive at most one separately accounted native retry within the original request deadline. All other failures retain global stop. This candidate is not activated by this plan.

## Task 1: live TLS witness, isolated and unused

Add a private witness and tests in new taskbench files only. Do not change existing transport/probe/driver, production agent, journal, settings, protocol or timeout. The witness attaches httptrace callbacks to the existing request context and records event counts under a mutex. It preserves an independent parent context. Seal once after RoundTrip; require a nil response, an error matching io.EOF (not unexpected EOF, timeout or certificate error), exactly one TLS start and one failed TLS completion matching EOF, no successful handshake, no GotConn, no WroteHeaders/WroteRequest, no first response byte or 100 response. Missing, duplicate, contradictory, incomplete or late callbacks reject eligibility. Both original request and parent contexts must still be live when checked. Never create a replacement deadline/context or infer zero supplier processing/billing. A diagnostic snapshot is not a retry authority. The witness itself performs no retry, network, accounting or capability issuance and has no current runtime caller.

TDD must demonstrate behavioral RED, then GREEN: eligible complete EOF; bare EOF without callbacks; unexpected EOF; nil error; nonnil response; successful/duplicate/unfinished TLS; any acquired connection, partial write callback or response observation; cancelled parent/request; repeated seal; events after seal; concurrency and snapshot isolation. Use fake callback invocation only, no listener/network/DB. Cover wrapped EOF explicitly. Preserve old probe serialization unchanged.

## Task 2: parent accounting core specification

Design an independent versioned parent ledger: cumulative known costs, every permanent unknown liability, one pending native attempt, fixed funding tranches and duplicate-resistant nonfinancial child identity. Admission must fit funded capacity and the fixed parent ceiling. Created tranches stay permanently reserved; uncreated tranches are not pre-reserved. Persistence failure poisons the owner; closure cannot be resumed. No generic recovery framework or changes to legacy ledgers. Review this design before implementation.

## Task 3: offline accounting core, after clean specification review

Implement the narrowed specification `continuation-design/parent-ledger-spec.md`, SHA `95e192e5246afd61360d4aa29016029b9613b320f218809db89f444d64061f90`, under the V7 private preparation root. Preserve its original v1 and review. New parent-accounting source/test files only; leave the old window ledger and all runtime callers unchanged. The primitive manages dynamic logical requests and native attempts, not the 7320-arm cursor. Accounting tickets are not egress permits; real funding claims, verified configuration, TLS permission, source/owner guards and filesystem store integration remain separate gates.

Use an injected append/create-and-sync store contract with deterministic fault injection. No production filesystem adapter or executable recovery is part of this unit. Apply immutable events only after acknowledged persistence; poison after any uncertain write. Funding is the minimum ordered fixed tranches needed for a hold. Known plus all unknown plus the current pending liability must fit funded capacity and each currency's cap. A failed native hold survives a later successful attempt and future logical requests. Finish only a logical whose final native was settled; unknown cannot escape by opening a fresh logical. Check arithmetic and ticket uniqueness, provider bounds, original deadline, quotas, duplicate owner, create/append failures and terminal closure. Keep serialization/persistence hashes deterministic; a read-only audit must not reopen execution. Explicitly test multiple logical requests within the same arm.

Preserve behavioral RED and final selected GREEN, and run the selected race test once on the final implementation. Use the fixed offline Go/SDK environment. No network, DB, credentials, real claim, model call, owner restart or deployment. Root verifies the final files and tests, then obtains independent review before staging only this unit's files.

## Subsequent integration gate

Only after the two independent primitives are reviewed should a new version connect native retry identity, payload persistence, owned-driver guards, frozen scope/claims and independent verification. Preserve logical versus native call counts and all unknown holds. Final complete-source regression, end-to-end fake Handler tests and independent scope/budget review precede any real execution. Do not claim the isolated primitives establish readiness or completion.

## Execution rulings

- Work directly on main per the user's instruction; preserve unrelated web output and untracked files.
- Keep all engineering tests offline and bounded. Run the complete source regression once at the final integration gate, rather than repeating it after every isolated unit.
- Keep V7 prefix evidence and failed denominators immutable. Candidate engineering neither replaces the original target nor silently authorizes another scope.
