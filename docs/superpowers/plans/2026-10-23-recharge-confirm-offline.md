# Recharge confirmation offline acceptance plan

> Execute inline on main using executing-plans. User approved offline acceptance and an independent proposal only.

**Goal:** Verify the third frozen pilot case recharge-002/confirm using actual handlers, owned MySQL/Redis fixtures, in-process OpenAI and FakeJEV; separately exercise the existing duplicate_confirm case. No external model/JEV, credentials, deployment, new paid authorization, canonical claim or historical evidence changes.

**Design:** Keep frozen cases/definitions unchanged. Add a taskbench regression covering two routing arms under normal and one-repair provider scripts, the old 3-attempt cap and a declared 10-attempt envelope. A separate duplicate-confirm script deliberately proposes recharge again on the duplicate confirmation to verify no second mutation. Measure request bytes, turns, model/JEV attempts and canonical state evidence. Amount is 100 yuan, balance delta10000cent, one paid order/topup and one confirmed recharge audit per arm. Preview must not alter business state; unrelated tenants and allowance unchanged.

**Files:** Create internal/taskbench/recharge_confirm_offline_test.go; add eval/paired-pilots/2026-10-23-recharge-confirm-offline/ and docs/recharge-confirm-offline-2026-10-23.md. No runtime changes unless a concrete failing safety condition is found; use systematic-debugging/TDD if so.

- [x] Run real-handler acceptance for normal confirm, old-cap repair stop, sufficient-cap repair and adversarial duplicate confirm. Verify both arms/side effects and no external transport.
- [x] Verify max model-loop bound from source and derive a false one-case Pi proposal; duplicate case is an auxiliary offline safety check and not added to paid scope. Pricing rates are declarations, not newly verified tariffs.
- [x] Capture private local journals using a test-only overlay; archive redacted summaries and immutable source hashes. Keep local_stub and needs_review, never claim real-model quality.
- [x] Full Go regression and independent review. Validate old source evidence hashes, then scoped commit/push/remote verify and Hindsight write/readback.

Review focus: replayed confirm must not create another pending/write; partial budget exhaustion cannot masquerade as both arms completed; previews must preserve balances/orders/subscriptions; synthetic evidence cannot become paid authorization; output cap/thinking/protocol must match Pi OpenAI serializer.

Ruling: This delivery is an offline acceptance audit and blocked proposal; do not bundle new customer behavior changes into it. Two concrete blockers (false completion before preview and a new preview after duplicate confirmation) are archived with reproduction evidence and must be fixed before paid work. Cost: customer runtime retains these known gaps until the next fix; no paid run/deployment is permitted by this artifact. The probe computes failed gates and does not require defects to persist.

Local serializer note: OpenAI protocol/output128 are exercised on a literal loopback host; the DeepSeek-host-only thinking override does not apply there. The separate Pi preflight config resolves thinking=disabled, but no claim is made that local bytes establish actual vendor token billing.

Review scope limits resolved by root source inspection: paired_environment.go creates random ai_gateway_pair_ schemas and verifies DROP, llmLoop iter<5 and bound confirmation branch precedes routing. Full Go regression18 packages passed and read-only MySQL confirms remaining schemas0.
