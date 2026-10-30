# Independent V8 window verification and inactive owner template

Build a separately named V8 read-only verifier using the reviewed V8 codec/native replay modules and final Go structs. Preserve old verifier `cf390f74…`, owner `15489cfc…`, score source `776f8670…` and all consumed artifacts. Never run the stopped owner, create actual authority, publish scores, access provider keys or make paid calls.

The template must remain inactive until its new root, manifest/source/binary/scope/execution and helper/executable hashes are frozen. Caller-provided keys, success JSON or a template `approved` flag cannot establish trust. Final execution authority must be checked against the public key in the authenticated frozen manifest, its exact human-reply bytes, both currencies and policy. Do not create actual authorization or sign with real keys in this unit.

Implement canonical V8 manifest/base/extra/ledger/payload/receipt/checkpoint/summary verification, raw-file identity/singlelink checks and closed namespace inventory. Reconcile global base/extra permanent responsibility, both provider20 quotas, current-window unknown and settled native union, logical counters, cache/usage and all original row/clock/quality/observer/cleanup bindings. Keep stopped/unknown/unrun states explicit and preserve failure denominators; no completion from a journal alone.

Use a genuine Go-produced complete local_stub window when supplied for a controlled positive, without relabeling it actual or manufacturing a session summary. Actual summary verification may use the exact production struct contract and negative tests until a real authorized execution supplies its bytes. Keep synthetic and actual schemas/modes distinct.

Add explicit V8 score/source/quality helper support in new files if old APIs cannot accept the new contracts. Genuine in-process proof requirements stay mandatory. A helper may validate recorded data, but must not reconstruct an owned capability from caller JSON, turn a failed source result into success or emit a fake verification result. If the necessary Go API is absent, report the exact missing interface to root; do not fake it.

Prepare a new inactive owner template only after the verifier contract is stable: explicit freeze/authorization gates, one launch and exclusive outputs, ordered windows, verification before advancing, fatal stop, process-group cleanup and no restart/reclaim. Do not execute an owner here. Test using pure fixtures/subprocess fakes, not a supplier or real Langfuse.

Use TDD and exact Go-emitted raw artifacts for acceptance, mutation and tampering cases. No imports or execution of old frozen scripts. Preserve all failures. Coordinate any controlled Go-helper integration with root's sole Go window; do not run Go while runtime/material export is active. The final full-source Go regression and freeze run once after all code and helper integration is complete. Current130+1 and unapproved140+1.1/retry remain unchanged.
