# Recovery Retest Preparation Implementation Plan

> **For agentic workers:** Use executing-plans inline on main. User authorized non-paid preparation, not another paid run.

**Goal:** Prepare a new independently authorized one-shot retest of the deployed cash-read recovery while preserving the consumed first retest.

**Architecture:** Keep two immutable allowed scopes, not arbitrary campaign IDs. Retain v1 unchanged; v2 adds cash-read-recovery-retest-20261023, latest failed journal/review bindings, deployed customer revision and shared runtime contract. New plan, authorization and billing schemas bind these values. Reuse existing model/JEV budgets and runner; durably claim by validated fixed scope ID.

**Tech Stack:** Go taskbench/paired-live, local fake providers/fixtures, archive build.

**Spec:** User approved preparing a new scope after docs/customer-cash-read-recovery-deployment-2026-10-23.md; original frozen balance-020/explicit_scope_followup remains unchanged.

## Constraints / decisions

- Main only; no service deployment, paid calls, balance checks, approved real authorization or billing truth fabrication.
- New fixed identity cash-read-recovery-retest-20261023, schemas cash-read-retest-authorization.v2 / paired-retest-billing-attestation.v2 / authorized-cash-retest.v2.
- Bind latest failed journal7d48f9caff31099149a06a3b14e3e9a5367b4a4b12067537130bba6453a9f754, review785834102ecae9104df8e33239a7089ace5503cc8d33c5a6850862e77f24b6f6, customer source4c715556d893a5bf8dadcd6a164310692ec2570a, compiled contractb92458d1bafd3187a2daa6387b0575e9517da84f66df6602b1945a8d47ba9d8e.
- Exact same frozen case, Pi target, two arms/two turns, shared12 model/¥0.405504 and1JEV/$0.002688. Proposal figures, not current tariff verification or paid authorization.
- v2 plan must carry ID/customer/contract markers. v1 plan/auth/billing must not carry v2 bindings. Unknown identity and path traversal reject.
- Scope ID cannot change after validation. Old/new claims coexist; change of revision, JSON formatting or output cannot reset either claim. Failure never releases.
- False examples remain default read-only, unready, no env/fixture/output/claim. Final committed archive builds and a private false approval candidate bind final execution revision.

## Tasks

- [x] RED: v2 accepted only exact bindings; false consent unready; reject mixed schema/plan/billing/prior evidence, arbitrary IDs and contract mismatch; legacy still valid.
- [x] GREEN: immutable scope descriptors/private snapshot, shared billing and receipt scope, new marker validation and compiled contract pin.
- [x] RED/GREEN CLI: new false check no side effects; scoped claims coexist independently, duplicates/unknown IDs fail. Local v2 fake-provider runner covers four reads and receipt identity.
- [x] Full Go regression, independent review, correct important findings, document false scope/proposal and hashes.
- [x] Delivery procedure: commit/push/remote verify, then committed archive + false final-revision check and Hindsight readback. Final delivery evidence is private and reports any unresolved steps. No paid execution.
