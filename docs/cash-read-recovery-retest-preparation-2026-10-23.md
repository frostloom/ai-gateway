# Cash-read recovery independent retest preparation

The deployed recovery is customer source `4c715556d893a5bf8dadcd6a164310692ec2570a`, contract `b92458d1bafd3187a2daa6387b0575e9517da84f66df6602b1945a8d47ba9d8e`. This change prepares fixed scope `cash-read-recovery-retest-20261023`; it does not execute or approve another paid run or redeploy the customer service.

## Scope and safeguards

The frozen `balance-020/explicit_scope_followup` case runs two arms, two turns each. Limits remain 12 model attempts / ¥0.405504 and one JEV attempt / $0.002688. The consumed earlier retest and its failure are retained. The new authorization anchors its journal `7d48f9caff31099149a06a3b14e3e9a5367b4a4b12067537130bba6453a9f754` and review `785834102ecae9104df8e33239a7089ace5503cc8d33c5a6850862e77f24b6f6`.

The v2 authorization, plan, billing and receipt bind the fixed campaign, deployed customer source, runtime contract and execution revision. The checker compares the contract to compiled handler metadata. The old v1 scope remains isolated; arbitrary IDs and cross-scope evidence fail. Each exact scope can be permanently claimed once under `.tmp/paired-live-retests/`; changing output, revision or JSON formatting cannot reset it. Failures retain claims. No automatic paid retries or scope expansion.

Public materials are in `eval/paired-pilots/2026-10-23-cash-read-recovery-retest-offline/`. Both authorization and billing examples are false. Zero execution revisions are placeholders. The offline proposal records preparation baseline `5bb2d3774f458bb017aea645e93d29de32bbb2cf`; it is not final execution provenance. After commit, the archive builder produces a binary and provenance manifest for that commit; a separate private false authorization candidate and independently reviewed billing evidence bind that exact revision. Tariff evidence is distinct from human authorization. Model token reservations are conservative operator judgments, not exact server billing counts or universal framing guarantees. JEV failure charging remains unverified; reserve the full attempt and stop on failure.

## Validation

Full Go regression: 18 test packages passed. New regression tests first failed against the old scope/claim implementation and strict preflight decoder, then passed with the fix. The local fake-provider v2 handler run intentionally omits the follow-up tool even after correction: four cash reads succeed across both arms with eight fake model attempts and one fake JEV attempt. This does not establish the real model result. Independent code review found no important issues. Validation and public file hashes are archived alongside the proposal.

## Next execution gate

Use `paired-live -retest` with the new plan and final-revision authorization/billing evidence. Default checking loads no model credentials and claims nothing. Only after explicit approval of this new scope may a true private authorization be created and `-execute` used with a pristine committed archive binary. External payload comprises synthetic frozen test utterances, system prompts, tool definitions/history and isolated fixture results to DeepSeek `deepseek-flash`, plus evaluation context to TypeSafe `jev-1.13.0`. No real customer records. Prior paid approval cannot be inherited.

Private delivery evidence lives under `.tmp/paired-live-private/cash-read-recovery-retest-20261023/`; it must not be committed. Existing frontend build changes and unrelated untracked files are outside this goal.
