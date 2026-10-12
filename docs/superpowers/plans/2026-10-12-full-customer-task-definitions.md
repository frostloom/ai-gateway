# Full Customer Task Definitions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Define all 1078 remaining source tasks without paid calls or changing historical evidence.

**Architecture:** A new deterministic local exporter produces a separate full 1220-source development definition archive. Source-specific first-action alternatives and reply requirements remain authoritative; synthetic account profiles and explicit supplementary user messages define future branch oracles. This schema is deliberately separate from the narrower executable taskbench format.

**Tech Stack:** Python standard library, unittest, immutable JSON with SHA256.

**Spec:** docs/customer-service-evaluation-contract-2026-10-09.md and frozen eval/contracts/2026-10-09-customer-service-v1/cases.json; user authorizes implementation and GitHub push after checks.

## Global Constraints

- Zero model/JEV/network calls by exporter; no database writes.
- Preserve all historical bytes, source IDs, annotations and the previous 214-task archive.
- Definitions are synthetic development material, not recovered history, independent gold, or new scores.
- Native inline execution; prior user instruction forbids unnecessary approval questions.
- No changes to default 12-task entry, production routing, or .env.

## Review Focus

- Ambiguous first actions retain alternatives instead of forcing a single tool.
- Missing slots receive explicit new user authorization, never invented first-turn parameters.
- Refund, cancellation and next-cycle plan changes have different state effects.
- Limited records, date ranges and missing capability evidence cannot support broad claims.
- All source IDs, remainder membership, cross-artifact references and byte hashes are verified.

### Task 1: Source-specific definition exporter

**Files:** Create scripts/define-customer-tasks.py and scripts/tests/test_define_customer_tasks.py.

**Interfaces:** build(cases, readiness) returns definitions/profiles/summary; export(output) refuses existing directory; verify(output) regenerates and checks bytes and file set.

- [x] Write tests for exact 1220/1078/142 coverage, all seven actions, preserved reply facts/actions, synthetic truth, branch safety, tool references and parameter provenance.
- [x] Run tests RED before implementation.
- [x] Implement deterministic profiles, per-source first-turn assertions and explicit follow-up scenario definitions. Unsupported execution adapters remain explicitly false, never a missing task definition.
- [x] Run focused tests GREEN, including cancellation/no write, refund clipping, plan identity, alternatives, normalization and overwrite/tampering rejection.

### Task 2: Archive and document

**Files:** New eval/task-definitions/2026-10-12-full-dev-v1, .gitattributes, CI verify step, progress/preparation docs, docs/interview/CHANGELOG.md.

- [x] Export into new non-existing directory; verify all definitions and preserve old archives with their existing verify commands.
- [x] Document actual coverage, profile formulas, branch counts, independent-review and adapter limitations.
- [x] Run full offline Python suite and diff checks; request fresh whole-change review and fix findings. Commit/push recorded in Git history.

## Execution ledger

Ruling: Complete declarative definitions rather than extending the live runner in this task — the user selected task definitions, not runtime implementation — counting definitions as executable/full-answer gold would overstate progress.

Evidence: initial 7 tests failed with missing exporter; final 15 focused tests passed. Review-found continuation, ledger, usage, bill-order, provenance and placeholder defects were pinned by failing tests before fixes. Full Python89 passed54.013s; fiveverifycommands and diff checks passed. Final fresh review found no blocking issues. No new model/JEV/database calls; source production code unchanged, so no additional Go test run for this documentation/data/exporter change.
