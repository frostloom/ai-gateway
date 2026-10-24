# Refund balance-cap independent entry implementation plan

Goal: prepare original refund-001/balance_cap for a single independent controlled paid evaluation, without executing it.
Architecture: same paired-live -retest, new fixed refund-cap-20261024 identity and authorization/billing/receipt v1. Main only; no new worktree. User has delegated routine design and continuous execution.

## Frozen design

- Pin deployed customer50e3c8dab14b4b6cc25322fd340a274e6970200b and v5 a95cacd041830012619a0f4d7fd8208c184aa9e8d45b7ee05afaa239a30e1e8f, repair offline manifest/acceptance, original diagnostic and100+1budget digest.
- Fixed original refund three turns / low_balance25yuan / request100 / final refund25 once, two arms. Shared12model attempts/405504microCNY and1JEV/2688microUSD,128outputtokens; no retries/release.
- Four exact original confirmation/diagnostic/cancel/supplement claims must exist and match complete SHA before newclaim; missing/tamper rejects before creating. Permanent fifth cumulative1824768microCNY/13440microUSD; unallocated98175232/986560 under100+1.
- Templates human authorization/rate/token false, no credentials/fixture/claim in default checks. New precise authorization still required; previous consumed scopes preserved.

## Task1: fixed scope and entry gates

New internal/taskbench/authorized_refund_cap{,_test,_runner_test}.go, cmd/paired-live/refund_cap_test.go. Modify only scope registration, billing recognition, persistRetestClaim handling. Add eval/paired-pilots/2026-10-24-refund-cap-entry false templates and docs/refund-cap-entry-2026-10-24.md.
- [x] RED scope/binding/widening and four predecessor tests; implement minimal new fixedscope.
- [x] Real Handler local complete4/repair6, provider503stop1/JEVEOFstop2, output reuse refused; state cap25/onewrite/controls asserted, thinkingdisabled checked at official target custom transport.
- [x] Standard full Go regression/defaults and independent final code review. Preserve historical files/claims, isolated DB0.
- [ ] Capture real serialized requests locally, current official tokenizer/pricing/settings connectivity and final executor provenance. False candidate exact binding only; execute refuses before credentials. No paid requests or new actualclaim.
- [ ] Docs/memory/commit/push each bounded verified milestone; ask only exact paid execution after remaining blocker solely human authorization.

## Review focus

Authorization cannot widen coherent auth+plan; old billing/scope cannot cross-use; no predecessor bypass, O_EXCL durable one-shot, permanent sum includes fourth supplement. Provider/JEV failures stop before further calls, EOF diagnostic persisted, no changes to customer runtime. Local structural pass remains needs_review rather than semantic success.

Task1 source/entry verification: RED rejected unknown fixedscope; GREEN gate tests/four Handler capture paths passed. Full standard18 Go packages exit0, nofail; isolated DB0,3142historicalfiles unchanged. Independent review noP1/P2; independently ran gate tests only, integration/full by primary. Request/pricing/settings reviewed; final committed executor/provenance and false approval binding follow this source commit.
