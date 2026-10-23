# Cash Read Recovery Implementation Plan

> **For agentic workers:** Use executing-plans inline on main; user explicitly requested execution of the offline read fix. No deployment or paid inference.

**Goal:** Recover the real retest's second-turn missing read without trusting historical cash text or retrying providers.

**Architecture:** Keep the model loop and one bounded reply repair. Only after repair still produces an unsupported cash amount, a conservative full-message recognizer may allow one authenticated `get_balance({})` read for an explicit own cash query. Return the canonical tool result directly, with real trace/audit/observer timing, never append a fabricated tool-call pair. Other language stays on existing safe limitation path.

**Tech Stack:** Existing Go handler, billing tools, scripted model/local fixtures.

**Spec:** User's requested offline execution fix after docs/cash-read-retest-live-result-2026-10-23.md. The narrow recognizer and recovery placement are implementation choices; not a general intent router or promise to understand all cash requests.

## Constraints and design

- Main only; preserve dirty frontend/assets/unrelated files; zero real model/JEV requests, no service deployment.
- Reuse fixed one repair limit and five-iteration loop. Recovery adds at most one read, no extra LLM/JEV call. Don't retry an already attempted balance read, successful or failed.
- Recognize complete Chinese query wording for current own cash balance, optionally followed only by explicit token-scope/write exclusions. Other-account, historical/example, quoted, negative-balance, mixed action/custom scope and unrecognized text do not qualify. No case IDs or frozen literal matches in product code.
- A pending action prevents this recovery; preserve the actual preview, confirmation identity and all write semantics.
- Read uses empty args and authenticated tenant, through existing tool execution and audit; successful answer is canonical source text. Failure produces limitation plus real failed read trace, no amount or retry.
- Apply equally in both comparison arms and disclose changed shared execution contract hash.
- Do not modify consumed paid claims/budgets, frozen cases, historical journals or published scores. Local replays do not turn historical failed runs into passes.

## Tasks

- [x] RED: real failure reply variants repeated across two turns must recover one fresh read in both routing modes; successful response, tenant42, trace, audit, repair count, canonical result and no extra model calls.
- [x] RED/negative: lookup failure records one failure and stops; earlier failed read isn't retried; pending preview and quoted/history/negative/foreign/compound requests never trigger recovery; per-turn reads are fresh.
- [x] GREEN: full-message recognizer and one-shot recovery helper; integrate only after exhausted reply repair; update shared runtime contract.
- [x] Local HTTP/serializer replay of frozen case where fake model never selects second-turn tool; verify taskbench two-turn read requirement, unchanged business state and bounded usage. Raw historical journal still fails new reporter.
- [x] Run relevant tests and full Go regression; independent review and fix important findings with failing tests first.
- [ ] Delivery after source commit: push/verify remote SHA and update/read back Hindsight; final response records the completion without changing reviewed source revision.
