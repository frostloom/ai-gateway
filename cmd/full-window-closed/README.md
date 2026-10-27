# Closed native-capture session

This command cannot call actual model/JEV providers. It constructs concrete local substitutes and a loopback OTLP ACK sink, uses synthetic accounting and temporary owned MySQL/Redis fixtures, and keeps one campaign owner alive across explicit windows. It does not authorize actual funds, attest a build, prove supplier token bounds or complete the real1220comparison.

Without `--serve-closed` it prints a read-only response. It does not open a session or read the fixture DSN. There is no paid mode, provider key/address, custom transport, automatic loop or disk resume option.

Prepare a regular0600file containing only the explicit loopback MySQL administration DSN, with no selected database. Do not use the provider settings file. An illustrative closed session is:

```sh
./full-window-closed --serve-closed \
  --definitions eval/task-definitions/2026-10-12-full-dev-v1 \
  --profiles eval/runtime-profiles/2026-10-13-six-profiles-v1 \
  --private-root .tmp/new-unique-closed-root \
  --width 32 --source-revision <declared-40-hex-revision> \
  --allow-fixture-creation --fixture-dsn-file .tmp/private-fixture-dsn
```

Keep that process and stdin alive. Submit one JSON object per line:

```json
{"operation":"status"}
{"operation":"run_window","expected_index":1,"prior_receipt_sha256":""}
```

Read the returned `receipt_sha256` before submitting the next window with its exact index and digest. The receipt checksum is emitted outside the persisted receipt to avoid self-reference. `null`, duplicate/unknown fields, stale operations and oversized lines reject and stop; they do not trigger retries. Every acquisition operation retains the existing300second context limit and original bounded cleanup behavior.

```json
{"operation":"close"}
```

EOF and signals close the owner, including while idle. Existing roots remain audit-only; starting a second process cannot resume them. A successful window has already reserved the next synthetic claim; closing does not release it. Final closed acquisition is explicitly distinguished from the actual real-evaluation goal.

Native model/JEV request and response bodies are private0600evidence. A declared revision is only a label; execution for a reproducible full capture must additionally bind the archived source, build command and binary SHA. The OTLP sink discards acknowledged span bodies and is not Langfuse persistence/readback evidence. Original `Success=false`/needs_review/failed values remain; injected usage and synthetic rates are not accuracy, tokenizer or invoice evidence.
