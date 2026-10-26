# Pinned Receiver Adapter Implementation Plan

> **For agentic workers:** Execute inline using executing-plans and TDD; one fresh independent review at the end. User authorized autonomous direct-main development/push; no routine approval checkpoint.

**Goal:** Produce an offline image candidate and reversible adapter for the pinned Langfuse OTLP call sites, default off, without replacing the current receiver.

**Architecture:** Verify version4.58.0 and exact SHA of the two current post-translation chunks. Patch only the authenticated OTLP callback, publisher S3 await and queue add. Absolute CommonJS runtime path shares one diagnostic instance; fixed environment value1 enables it. Installer validates all sources before writes, retains byte backups/manifests and refuses unexpected state on install/restore.

**Tech Stack:** Node built-ins, existing diagnostic module and pinned official Docker base plus existing zh build. No new dependencies, API endpoints or HTTP metadata.

## Constraints

- Observer remains2000ms, no cancellation/retry/extra business calls; no paid calls/new claims/resume/old-span resend.
- Diagnostic starts inside authenticated OTLP callback; auth, route/module loading and earlier middleware remain outside coverage.
- Strict source anchors+complete file SHA+syntax check; unknown version/layout/tampering fails before mutation.
- Offline rollback restores exact original bytes and removes only own backup/manifest; no claim that loaded runtime modules can be hot-unpatched.
- No compose/runtime activation, current image tag replacement, credentials in build context, service restarts or shared config changes.
- Candidate tag separate; base official digest unchanged. Verify build output independently and remove no existing images/volumes.

## Tasks

- [x] Write failing adapter/default-enable/transform/install/restore tests.
- [x] Implement runtime adapter, pinned layout, transactional installer and README; test ROOT/version/hash/duplicate/symlink/tamper/partial write failures.
- [x] Build candidate from pinned official base, existing zh layer and diagnostics; run all Node tests in build and verify patch manifest. No deployment.
- [x] Validate default-off/enabled/error/phase scopes against actual patched call sites offline, plus restore on an isolated copy. Preserve failed attempts.
- [x] Fresh review, fix material findings RED→GREEN, rerun affected checks; archive and update memory.
- [x] Verify old evidence/source/claim unchanged, commit selected files, push main and verify remote.

## Ledger

Ruling: Rebuild a separate candidate from the official digest and existing zh scripts, not a mutable local tag; exact post-translation chunk SHA pins the adapter to the observed build.
Ruling: Tests may copy installer and a minimal fixture registry into a dedicated temporary directory; production CLI has no override/force mechanism and always loads its adjacent pinned registry.

Outcome: final v4 candidate built from original fixed official digest with58 Node tests,18 adapter tests and7 compiled-callback cases passed; exact final sources/UID1001/default-off confirmed. Offline COW restore verifies original bytes/mode0664/uid1001/gid1001, current service image unchanged.
Ruling: Review P2 cleanup loss fixed with4 RED→GREEN regressions and persistent restored state; prepared mixed-state recovery tested. Packaging inspection found umask and ownership changes, each reproduced and fixed without weakening restore validation. Independent targeted rechecks18 passed; root→1001 ownership regression passed in final build.
Ruling: Initial BuildKit resolution canceled, same digest already cached; legacy cache builds retain deprecation warning. v1/v2/v3 remain pre-acceptance images, not deployed; v4 is the only final candidate. No source-hash or version relaxation.
Ruling: Diagnostic excludes pre-auth/middleware/module-load time; successful offline cases do not establish full HTTP/live root cause or new paid readiness.
