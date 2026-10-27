# Full-primary local Handler matrix — 2026-10-27

## Scope

The final deliverable remains the actual V1=model versus V2=jev_selective comparison across the1220 original primary complete tasks, with at least three interleaved passes and1851 boundary scenarios kept separate. This local stage exercises the implementation using oracle-informed synthetic responses; it cannot establish model quality, JEV routing quality, savings, supplier charges or a verified paid request envelope.

The immutable primary selection is `full-customer-primary.v1`, SHA `b9b6f48daccffa6bd7c0f86e3f8d4857eedc719404406014d3426b0656edd2f8`. Its1220 tasks contain1962 source user turns:2440 arms and3924 observed turns per complete local pass. Source/profile identities remain those in the full1220 adapter archive.

## Implementation

`RunFullLocalCustomerWindow` accepts1–100 explicit distinct primary keys. It calls the guarded full-primary real Handler adapter against an owned isolated MySQL schema and private Redis. Each case receives its own campaign batch and account/session pair. Model responses stay inside an in-process RoundTripper; it cannot delegate to an external transport. FakeJEV deliberately returns low confidence, forcing model fallback. There is no external provider/key option or dotenv discovery.

The script uses frozen source read parameters, amount arguments and symbolic catalogue plan names. Ordinary clarification, rejection and consultation replies are explicitly synthetic placeholders. Existing structural, factual, usage, observation and campaign guards remain enabled. Every completed answer remains `needs_review`; no semantic pass is issued.

The explicit CLI is `cmd/full-customer-local`: execution defaults off and requires `-execute-local`, a JSON keys file, a40-hex archived source revision, a new output directory and an explicit literal-loopback MySQL admin DSN without a database. It validates source/key inputs before opening fixtures. Its context is300seconds, including fixture startup; cleanup uses the existing independent bounded context. Fixture credentials are not printed or copied into public evidence.

Each new window writes its plan, per-case journals/reports, a receipt and a no-content local observation index. The index preserves trace/span IDs, names and association metadata, including unknown task success; raw prompts/tool bodies stay in private execution journals. Receipt SHA binds that index. Actual task/turn/generation counts are verified separately from additional tool/JEV spans.

`scripts/full-customer-local-matrix.py` independently reuses the original source selector to prepare13 ordered windows (12×100+20). Read-only aggregation requires explicit SHA references to the binary, stdout, stderr, window plan and receipt. It cross-checks substantive typed source cases, configuration contracts/revisions, journal start/finish order, recorded results/traces/budget attempt deltas, per-arm reports and raw observation associations. Failed windows remain unverified and later cases remain unrun; no retry/resume is implemented by the aggregator.

SHA links and source provenance are trusted evidence, not cryptographic execution attestations. All local usage/pricing is synthetic test scaffolding. No real model/JEV request, real claim, deployment or score publication is authorized by this CLI.

## Validation and delivery status

Implementation has passed final original-configuration Go regression (20packages, taskbench916.872seconds, wholecommand948.268seconds within20minutes). All1220 local windows and the actual full comparison remain pending; no execution completion is claimed by this implementation checkpoint. Private phase evidence is `.tmp/paired-live-private/full1220-local-matrix-20261027/`. Execution results and a public SHA manifest will be appended only after observing the frozen build's actual windows.

Fresh whole-change review identified two aggregation P2 findings: contradictory raw finish results could be hidden by a successful report, and substantive source/configuration changes were not independently checked. Both received counterexamples and fixes. New synthetic status flags remain false throughout.

The actual historical evaluation count remains143model/37JEV; permanent reservation remains¥29.061120+$0.223104, leaving¥70.938880+$0.776896 inside the approved¥100+$1 ceiling. No old claim is resumed, reclaimed or released. Overall full1220 evaluation remains incomplete.

The first broad regression failed in existing recharge fixture setup and was stopped (taskbench976.115seconds). Those original failures remain archived; subsequent success does not erase them. An interrupted owned schema was inspected and removed exactly, then schema count readback was0. One original failing confirmation path passed17.507seconds before the final full regression. Host/MySQL waiting observations are diagnostic clues, not a confirmed historical cause or product fix. No assertion, DB/HTTP timeout, test parallelism, service configuration or customer source was changed.

At this checkpoint325Go source hashes,15customer implementation files against50e3c8d,9277historical private artifacts and9permanent claims all match; isolated pair/fixture schemas read back0. User-authorized budget and historical actual counts are unchanged.
