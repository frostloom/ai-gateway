# Full1220 collection policy implementation plan

> Execute inline with executing-plans; user already authorized autonomous work and opted out of routine plan questions. This plan does not authorize a paid call, claim or changed legacy stop/budget policy.

**Goal:** Implement the distinct evidence-bound quality-negative recording policy required for the actual full1220 comparison. Budget ledger, complete request envelope and paid entry are separate prerequisites; this policy alone is never paid readiness.

**Spec:** docs/full1220-collection-design-2026-10-27.md; docs/customer-service-evaluation-contract-2026-10-09.md.

**Architecture:** Keep old RunConversation/RunAgentPair/current full APIs as strict wrappers. Add a narrow internal versioned recording policy, origin-typed issues and per-turn safety halt; old errors/status/Success and omitted JSON remain identical. New report/replay consumes disposition only after independently recomputing its safety/evidence conditions. No generic policy framework or human-error-string classifier.

**Review focus:** Missing preview versus forbidden write; unsafe pending must stop before scripted confirmation; unknown evidence/error closes execution; old serialization and strict stop behavior; negative results never become passed; observer/accounting/runtime failures always stop expansion.

## Tasks

- [x] RED: independent local/replay missing-preview/no-write case retains failed status but recording may continue; old entry still stops. Add origin-typed issue representation with old JSON omitted by default. Preserve existing human-readable Errors and scorer results.
- [x] RED: wrong preview amount/target or hidden pending halts before next user turn; actual wrong/duplicate/unbound/foreign writes, ledger inconsistency, session/oracle uncertainty and unknown issue all halt. Add minimal per-turn checks at existing validation origins; classify zero-effect unmet goals separately using authoritative state, never prose alone.
- [ ] RED: distinct policy/source/SHA mismatch rejected before journal/provider/JEV construction. Thread policy through the private pair path without weakening current full/pilot APIs. Persist result and typed disposition before next arm; preserve all observer, runtime and charge guards. Do not add a paid CLI or use old AuthorizedPilot as new scope permission.
- [ ] RED: fully recorded negative batch accepted only by a distinct collection completion check, while strict campaign still rejects it. Incomplete/unsafe/technical batches stop and preserve reservation. Budget/lease integration must use the separately designed new ledger rather than pretending an old permanent lease has settlement semantics.
- [ ] RED: replay catches altered issue/disposition/content/source SHA, stale/synthetic reviews, missing raw evidence and unknown issue codes. Report collection completion separately from unchanged task outcomes and full frozen denominator.
- [ ] Verify with the completed1220 local raw journals offline, plus isolated targeted real Handler fault cases after the current matrix process ends. Run original Go20minute/full Python regression once final code stabilizes; independent whole-change review and RED→GREEN fixes; archive exact source and push main. No real model/JEV/claim/score/deployment.

**Financial prerequisites outside this unit:** new permanent window claim chain + durable bounded request admission/usage accounting, current prices/versions, complete request envelope and exact300second schedule. If those are missing or caps do not fit, leave paid readiness false and proceed with the missing prerequisite rather than looping six-case pilots.

## Core-unit execution ruling

The first delivery unit is the private assessment core plus a read-only, explicitly SHA-bound LOCAL-journal audit. It does not implement or claim the paid pair/campaign entry. Tasks3/4 and the future full scheduled report depend on a new financial ledger/lease, which does not exist yet; no old permanent-budget lease will be passed off as that new ledger. Complete these integration tasks after its implementation. This is an ordering decision, not a smaller final goal.

- [x] Add the read-only `full-customer-policy-audit` CLI and local source/runtime/policy/result binding; reject coherent source/runtime contradictions and duplicate/changed inputs, preserve original task-negative status and source denominator.
- [x] Replay all1220 prior local pair journals:2440arms/3924turns; existing strict assessment unchanged (ignoring wall time, error-list order and intentionally journal-redacted derived requirements), newpolicy2440continue/0halt; all observed runtime identities complete. No new Handler/DB/provider execution.
- [x] One fresh core-unit review: twoP2 findings (review issue turn attribution, contradictory runtime accepted) reproduced RED and fixed GREEN. No new permission inferred; wrong preview still halts globally in this private core. Task-only termination needs a separate disposition and verified owned cleanup before another arm.
- [ ] Final core-unit original Go20minute/Python/race verification, source/old-evidence preservation, archive from committed source, Hindsight/AGENTS update and main push. Paid readiness remains false.
