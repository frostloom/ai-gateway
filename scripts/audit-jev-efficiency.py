"""Audit complete recorded Anthropic pairs. No network, env or model access."""
import argparse
import hashlib
import json
import math
from pathlib import Path


def require(condition, message):
    if not condition:
        raise ValueError(message)


def count(value):
    require(type(value) is int and value >= 0, 'invalid or missing usage count')
    return value


def close(left, right, label):
    valid = lambda x: type(x) in (int, float) and math.isfinite(x) and x >= 0
    require(valid(left) and valid(right) and math.isclose(left, right, rel_tol=1e-12, abs_tol=1e-6), label + ' mismatch')


def verify_digest(raw, expected):
    require(hashlib.sha256(raw).hexdigest() == expected, 'source digest mismatch')


def audit_documents(report, journal, budget, receipt, manifest):
    require(report['evidence_kind'] == 'recorded_run', 'recorded_run required')
    rows = report['rows']
    indexed = {}
    for row in rows:
        key = (row['key'], row['arm'])
        require(key not in indexed, 'duplicate report arm')
        require(row['arm'] in ('model', 'jev_selective'), 'unknown arm')
        indexed[key] = row
    require(len(rows) == report['planned_arms'] == report['finished_arms'] and report['unrun_arms'] == 0, 'incomplete paired run')
    require(len({r['key'] for r in rows}) * 2 == len(rows), 'missing paired arm')
    require(report['planned_cases'] * 2 == len(rows), 'planned case count mismatch')
    require(budget[0]['kind'] == 'config', 'budget config missing')
    limits = budget[0]['limits']
    def price(i, o):
        return (i * count(limits['input_micro_cny_per_million']) + 999999) // 1000000 + (o * count(limits['output_micro_cny_per_million']) + 999999) // 1000000
    settled = []
    pending = None
    for event in budget[1:]:
        if event['kind'] == 'reserve':
            require(pending is None and event['id'] == len(settled) + 1, 'budget reservation sequence')
            pending = event['id']
        else:
            require(event['kind'] == 'settle' and pending == event['id'] and not event.get('stop'), 'budget settlement sequence')
            count(event['Input']); count(event['Output'])
            settled.append(event)
            pending = None
    require(pending is None, 'budget interrupted')
    fields = ('attempts', 'reserved_micro_cny', 'input_tokens', 'output_tokens', 'priced_usage_micro_cny')
    require(journal[0]['kind'] == 'config' and journal[0]['budget_binding']['limits'] == limits, 'budget configuration mismatch')
    previous = journal[0]['budget_binding']['initial']
    require(all(count(previous.get(k)) == 0 for k in fields), 'this audit requires a fresh zero budget')
    cursor = 0
    result = {'schema': 'jev-efficiency-offline-audit.v1', 'rows': [], 'arms': {}, 'findings': [], 'effect_claims_allowed': False, 'total_system_cost_micro_cny': None, 'external_model_calls': 0, 'external_jev_calls': 0}
    seen = set()
    for event in journal:
        if event['kind'] != 'finish':
            continue
        key = (event['key'], event['arm'])
        require(key not in seen and key in indexed, 'duplicate or unknown journal arm')
        seen.add(key)
        row = indexed[key]
        require(all(count(event['budget_before'].get(k)) == count(previous.get(k)) for k in fields), 'budget snapshot continuity mismatch')
        observed = event['result']['observations']
        require(len(observed) == row['observed_turns'], 'observed turn count mismatch')
        item = dict(key=key[0], arm=key[1], server_latency_sum_ms=0, llm_calls=0, jev_calls=0, llm_noncache_input_tokens=0, llm_cache_read_tokens=0, llm_cache_creation_tokens=0, llm_billable_input_tokens=0, llm_output_tokens=0, jev_input_tokens=0, jev_output_tokens=0, estimated_model_micro_cny=0, direct_read_turns=0)
        for observation in observed:
            t = observation['turn']['telemetry_raw']
            if isinstance(t, str):
                t = json.loads(t)
            ms = t.get('total_latency_ms')
            close(ms, ms, 'latency')
            item['server_latency_sum_ms'] += ms
            for prefix in ('llm', 'jev'):
                calls = count(t.get(prefix + '_calls'))
                require(count(t.get(prefix + '_usage_missing')) == 0 and count(t.get(prefix + '_usage_responses')) == calls, 'incomplete usage')
                item[prefix + '_calls'] += calls
            item['direct_read_turns'] += int(t.get('jev_fast_path_reason') == 'direct_read')
            item['jev_input_tokens'] += count(t.get('jev_input_tokens'))
            item['jev_output_tokens'] += count(t.get('jev_output_tokens'))
            usages = t.get('llm_usage_raw', [])
            require(len(usages) == t['llm_calls'], 'raw usage count mismatch')
            base = output = cache = creation = 0
            for u in usages:
                protocol = t.get('llm_usage_protocol')
                require(protocol == 'anthropic', 'this audit requires explicit Anthropic cache usage')
                i, o = count(u.get('input_tokens')), count(u.get('output_tokens'))
                hit, made = count(u.get('cache_read_input_tokens')), count(u.get('cache_creation_input_tokens'))
                billed = i + hit + made
                require(cursor < len(settled) and (settled[cursor]['Input'], settled[cursor]['Output']) == (billed, o), 'budget per-call usage mismatch')
                cursor += 1
                base += i; output += o; cache += hit; creation += made
                item['llm_billable_input_tokens'] += billed
                item['estimated_model_micro_cny'] += price(billed, o)
            require(base == count(t.get('llm_input_tokens')) and output == count(t.get('llm_output_tokens')), 'raw usage totals mismatch')
            if usages and t['llm_usage_protocol'] == 'anthropic':
                require(cache == count(t.get('llm_cache_hit_tokens')) and creation == count(t.get('llm_cache_creation_tokens')), 'cache usage mismatch')
            item['llm_noncache_input_tokens'] += base
            item['llm_output_tokens'] += output
            item['llm_cache_read_tokens'] += cache
            item['llm_cache_creation_tokens'] += creation
        close(item['server_latency_sum_ms'], row['telemetry']['response_latency_ms'], 'latency')
        for provider, report_key in [('llm', 'llm'), ('jev', 'jev')]:
            require(item[provider + '_calls'] == row['telemetry'][report_key + '_calls'], 'call count mismatch')
        tok = row['telemetry']['tokens']
        require(tok == dict(llm_input=item['llm_noncache_input_tokens'], llm_output=item['llm_output_tokens'], jev_input=item['jev_input_tokens'], jev_output=item['jev_output_tokens']), 'report usage mismatch')
        before, after = event['budget_before'], event['budget_after']
        for field, expected in [('attempts', item['llm_calls']), ('input_tokens', item['llm_billable_input_tokens']), ('output_tokens', item['llm_output_tokens']), ('priced_usage_micro_cny', item['estimated_model_micro_cny'])]:
            require(count(after.get(field)) - count(before.get(field)) == expected, 'budget snapshot mismatch: ' + field)
        require(row['priced_model_usage_micro_cny'] == item['estimated_model_micro_cny'] and row['guarded_upstream_model_attempts'] == item['llm_calls'], 'budget report mismatch')
        reserved = item['llm_calls'] * price(count(limits['input_token_ceiling']), count(limits['max_output_tokens']))
        require(count(after.get('reserved_micro_cny')) - count(before.get('reserved_micro_cny')) == reserved == row['reserved_model_micro_cny'], 'reserved budget mismatch')
        previous = after
        result['rows'].append(item)
        total = result['arms'].setdefault(key[1], {k: 0 for k in item if k not in ('key', 'arm')})
        for k in total:
            total[k] += item[k]
    require(seen == set(indexed), 'missing journal arms')
    for arm, total in result['arms'].items():
        comparison = report['comparison']['model' if arm == 'model' else 'selective']
        close(comparison['response_latency_ms'], total['server_latency_sum_ms'], 'comparison latency')
        require(comparison['llm_calls'] == total['llm_calls'] and comparison['jev_calls'] == total['jev_calls'], 'comparison calls mismatch')
        require(comparison['tokens'] == dict(llm_input=total['llm_noncache_input_tokens'], llm_output=total['llm_output_tokens'], jev_input=total['jev_input_tokens'], jev_output=total['jev_output_tokens']), 'comparison tokens mismatch')
    for field, key in [('Input', 'llm_billable_input_tokens'), ('Output', 'llm_output_tokens')]:
        require(sum(x[field] for x in settled) == sum(x[key] for x in result['rows']), 'budget journal usage mismatch')
    estimated = sum(price(x['Input'], x['Output']) for x in settled)
    require(estimated == sum(x['estimated_model_micro_cny'] for x in result['rows']), 'budget journal cost mismatch')
    require(len(settled) == sum(x['llm_calls'] for x in result['rows']), 'budget journal calls mismatch')
    result.update(budget_model_input_tokens=sum(x['Input'] for x in settled), budget_model_output_tokens=sum(x['Output'] for x in settled), estimated_model_micro_cny=estimated)
    require(receipt.get('runner_completed') is True, 'receipt runner incomplete')
    require(receipt.get('model_http_statuses') == [200] * len(settled), 'receipt HTTP attempts mismatch')
    require(all(count(receipt['model_budget'].get(k)) == count(previous.get(k)) for k in fields), 'receipt budget mismatch')
    if manifest['priced_usage_micro_cny'] != estimated:
        result['findings'].append('manifest_priced_usage_mismatch')
    result['limitations'] = ['Recorded four-task sample only; no population accuracy or causal improvement claim.', 'Model estimates use declared conservative rates; no cache discount and no supplier invoice.', 'JEV has no CNY tariff or verified account charge here; total system cost remains unknown.', 'Server latency sums exclude user think time and differ from runner setup/cleanup elapsed time.', 'Original manifests and reports are preserved, including any inconsistent summary.']
    return result


def audit_pilot(pilot):
    pilot = Path(pilot).resolve()
    manifest = json.loads((pilot / 'manifest.json').read_text(encoding='utf-8'))
    required = {'attempt-01/report.json', 'attempt-01/run.jsonl', 'attempt-01/budget.jsonl', 'attempt-01/execution-receipt.json'}
    require(required <= set(manifest['files']), 'manifest missing required sources')
    for name, digest in manifest['files'].items():
        path = (pilot / name).resolve()
        require(path.is_relative_to(pilot), 'manifest path escaped pilot')
        verify_digest(path.read_bytes(), digest)
    read = lambda name: json.loads((pilot / name).read_text(encoding='utf-8'))
    lines = lambda name: [json.loads(line) for line in (pilot / name).read_text(encoding='utf-8').splitlines()]
    report = read('attempt-01/report.json')
    verify_digest((pilot / 'attempt-01/run.jsonl').read_bytes(), report['journal_sha256'])
    result = audit_documents(report, lines('attempt-01/run.jsonl'), lines('attempt-01/budget.jsonl'), read('attempt-01/execution-receipt.json'), manifest)
    result['source_manifest_sha256'] = hashlib.sha256((pilot / 'manifest.json').read_bytes()).hexdigest()
    return result


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('pilot', type=Path)
    args = parser.parse_args()
    print(json.dumps(audit_pilot(args.pilot), ensure_ascii=False, indent=2, allow_nan=False))
