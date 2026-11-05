#!/usr/bin/env python3
"""Check the retained artifacts of the isolated usage-acknowledgement experiment."""
import json
import pathlib
import sys

p = pathlib.Path(sys.argv[1])
def read(name):
    return json.loads((p / name).read_text())

config = read('config.json')
response = read('http-recovery-response.json')
before = read('http-recovery-before.json')
after = read('http-recovery-after.json')
wait = read('recovery-wait.json')
audit = read('audit-final.json')
key = response['request_id']
assert (p / 'completed').exists(), 'run did not complete'
assert config['test_usage_ack_loss_after_store'] and config['http_recovery_failure_stage'] == 'usage_after_commit_ack_loss'
assert wait['requested_seconds'] >= 610 and wait['elapsed_seconds'] >= wait['requested_seconds']
assert response['status'] == 200
usage = response['body']['usage']
b, a = before['records'][key], after['records'][key]
assert b['status'] == 'pending' and b['usage_reported'] and b['settle_rows'] == 0
assert b['events'] == {'reserve': 1, 'usage': 1, 'settle': 0, 'reverse': 0}
assert (b['prompt_tokens'], b['completion_tokens']) == (usage['prompt_tokens'], usage['completion_tokens'])
assert a['status'] == 'settled' and a['usage_reported'] and a['settle_rows'] == 1
assert a['events'] == {'reserve': 1, 'usage': 1, 'settle': 1, 'reverse': 0}
assert (a['prompt_tokens'], a['completion_tokens']) == (b['prompt_tokens'], b['completion_tokens'])
assert before['balance'] == before['ledger'] and after['balance'] == after['ledger']
assert not audit['balance_mismatches'] and not audit['missing_balance_keys']
metrics = (p / 'http-recovery-gateway-metrics.prom').read_text()
assert 'gateway_billing_completion_total{result="usage_unacknowledged"} 1' in metrics
print(json.dumps({'request_id': key, 'before': b['status'], 'after': a['status'], 'wait_seconds': wait['elapsed_seconds'], 'balance': after['balance']}, ensure_ascii=False))
