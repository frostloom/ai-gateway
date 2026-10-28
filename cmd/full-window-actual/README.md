# Full-window actual entry

Ordinary builds have empty readiness pins. With no arguments the command prints a read-only response and performs no readiness, settings, network, fixture or claim operation.

`--check --manifest /absolute/ready.json` checks the independently anchored manifest and mandatory signed binary attestation. It reads no provider keys or fixture DSN and creates no claim. An actual archive build must bind the reviewed manifest SHA, archive commit and tree; the separate signed attestation binds that manifest and the final binary bytes. Neither hand-written revision fields nor three `-X` strings alone certify the build. The complete tracked source tree, including embedded assets, and the original full-Go evidence are checked.

`--serve-actual --manifest /absolute/ready.json --private-settings /absolute/settings.json` checks readiness before loading strict0600 settings. The actual session verifies the settings digest, local fixture administration connection and Langfuse project health/identity before creating the new permanent first claim. Startup sends no model/JEV inference, but a successfully opened owner has consumed its first window cap. Closing never releases it.

The command accepts one JSON object per line, at most4KiB:

```json
{"operation":"status"}
{"operation":"run_window","expected_index":1,"prior_receipt_sha256":""}
{"operation":"close"}
```

Only `run_window` executes inference. A later operation uses its next expected index and the exact receipt digest returned by the previous successful operation. There is no automatic paid loop, retry or disk resume. EOF, cancellation, invalid input or a stopped window closes the owner. Loss of the live process leaves its consumed campaign audit-only.

The default source checkpoint does not include production readiness pins, a signed actual artifact, an executed actual campaign or passing full-Go evidence. Closed substitutes, unit factories and prepared manifests cannot certify production readiness. Collection completion is separate from independent semantic review, Langfuse score/content readback and the full comparison report.
