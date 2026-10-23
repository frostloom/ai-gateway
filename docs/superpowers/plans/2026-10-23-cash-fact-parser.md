# Cash fact parser offline correction

Scope: Fix the two observed recovery-retest first-turn forms without changing customer runtime, frozen definitions, paid scopes, raw reports or historical scores. No new model/JEV calls or deployment. Continue on main; preserve unrelated frontend/untracked changes.

Design: Extend the finite cash label grammar to current cash/current available cash and the explicit own prefix 你的. Keep anchored clause boundaries. For replies containing conditional advice, check only an independent first sentence before the conditional; quotes, reported speech and hypotheses keep the existing conservative exclusion. All other fact kinds retain the whole-reply exclusion. An amount match is only one fact, never semantic or task approval. Implicit ledger equality is not converted into a numeric assertion.

1. RED: exact real replies, wrong amount, hypothetical/quoted/foreign/conditional-before-claim boundaries, and semantic review preservation.
2. GREEN: minimal finite-parser change. Run targeted facts/report tests and full Go regression.
3. Re-evaluate the completed journal offline into a new separately versioned derived report. Bind source journal, old report and evaluator revision; retain old reports/scores. Document remaining semantic/fresh-read/paid gates.
4. Independent review, scoped commit/push/remote verification. Update/read back ai-gateway memory. Paid claims remain consumed.

Review fix pass: two P2 findings reproduced by four failing tests (conditional/reported/incomplete prefixes and explicit ledger in conditional reply). Require the complete numeric label at reply offset zero and keep only its first cash match. Final relevant tests passed. Full regression and final artifact validation tracked in validation.json; no second review or paid run.

Related prefix audit: two additional no-advice conditional/reported tests failed, then passed after limiting new current-cash labels to direct reply prefixes. This is the same review fix pass; no semantic approval expansion.
