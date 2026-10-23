# Recharge diagnostic retest implementation plan

> Use executing-plans in this session, on main. No paid inference in preparation.

Goal: one fixed independent recharge-confirm-diagnostic-retest-20261023, with unchanged recharge-002/confirm and shared 10 model/337920 microCNY + 1 JEV/2688 microUSD caps.

Design: extend existing reviewed recharge scope with v2 authorization/billing/receipt identities. Bind original failed journal/review, diagnostic source 5c2f669413c0787ba1bd3a429c9552d628e1a44c and diagnostic manifest SHA. Preserve deployed customer056a760/contract35c117 and original offline acceptance. Claim creation additionally requires the exact consumed original claim hash. Both claims permanently reserve a cumulative 675840 microCNY/5376 microUSD, below approved100CNY+1USD. This remains two fixed allocations, not generic scope expansion.

- [x] Add scope/billing/frozen-input rejection, missing/tampered predecessor claim, independent permanent new claim and local runner tests; observe RED.
- [x] Implement minimal v2 binding and predecessor guard; observe GREEN with local failure diagnosis persisted.
- [x] Generate offline proposal/check, keep exact human execution consent and billing examples false; verify no live claim/inference.
- [x] Run full Go suite, independently review, verify all old evidence unchanged.
- [ ] Commit/push only goal files, bind committed archive build, update and read back Hindsight.

Review focus: old approvals/billing cannot rebind; false approval stops before credentials; arbitrary future IDs rejected; missing/corrupt parent stops before new claim; changed version/output never resets new claim; cumulative reservation never releases failed attempts. Unknown original failure root cause remains unknown; richer diagnostic does not establish delivery/charges. Diagnostic source changes do not authorize real retries.

Execution evidence: new scope RED then GREEN; three local Handler paths include actual EOF cause persistence; full Go18 packages passed, independent review clear, cleanup0 and143 old private hashes unchanged. Final commit/push/archive binding follows verified preparation.
