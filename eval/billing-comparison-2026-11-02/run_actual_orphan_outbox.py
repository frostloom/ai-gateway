#!/usr/bin/env python3
"""Run real Store with optional temporary Outbox duplicate-error overlay."""
import json, os, pathlib, subprocess, tempfile
root = pathlib.Path(__file__).resolve().parents[2]
with tempfile.TemporaryDirectory(prefix='actual-outbox-') as d:
    args = ['go', 'test', '-race']
    if os.environ.get('TEST_OUTBOX_OLD_BEHAVIOR') == '1':
        source = root / 'internal/billing/store/outbox.go'
        original = source.read_text()
        old = '\t}).Error\n\treturn err\n}'
        if original.count(old) != 1:
            raise RuntimeError('unexpected Outbox code; no overlay applied')
        candidate = pathlib.Path(d) / 'outbox.go'
        candidate.write_text(original.replace(old, '\t}).Error\n\tif err != nil && isDupKey(err) {\n\t\treturn nil\n\t}\n\treturn err\n}', 1))
        overlay = pathlib.Path(d) / 'overlay.json'
        overlay.write_text(json.dumps({'Replace': {str(source): str(candidate)}}))
        args += ['-overlay', str(overlay)]
    pattern = '^TestActual(Store(OrphanOutboxConflictCharacterization|OutboxNormalReplay|TerminalOutboxConflicts)|SubscriptionOutboxConflicts)$'
    if os.environ.get('TEST_OUTBOX_EXTENDED') == '1':
        pattern = '^Test(ActualStoreConcurrentSameRequestReplay|TerminalCommitAcknowledgementLoss|SubscriptionReservedQuotaLifecycle|ContentFingerprintSubscriptionConcurrentQuota|ReserveRedisAppliedReplyLostThenRetry|SettlementRedisAppliedReplyLostThenRetry)$'
    args += ['./eval/billing-comparison-2026-11-02', '-run', pattern, '-count=3', '-v']
    raise SystemExit(subprocess.run(args, cwd=root, env=dict(os.environ, TEST_ACTUAL_ORPHAN_OUTBOX='1', TEST_TERMINAL_ACK_LOSS='1')).returncode)
