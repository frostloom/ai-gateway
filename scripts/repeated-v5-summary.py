"""Read explicit SHA-bound local evidence; write descriptive summaries to a new directory.

No environment discovery, network, inference, scoring publication or input mutation.
Artifacts are trusted provenance inputs, not cryptographic execution attestations.
"""
import argparse
import hashlib
import importlib.util
import json
import re
import statistics
from decimal import Decimal
from pathlib import Path

CASES = ['balance-001/answer_complete', 'balance-020/explicit_scope_followup',
         'recharge-002/confirm', 'recharge-002/cancel',
         'recharge-001/supplement_then_confirm', 'refund-001/balance_cap']
ARMS = ['model', 'jev_selective']
PLAN_SHA = '527334cf64842106d8b28003f7b40c1010bca02455fab20572f79affb7ccc8bf'
CUSTOMER = '50e3c8dab14b4b6cc25322fd340a274e6970200b'
CONTRACT = 'a95cacd041830012619a0f4d7fd8208c184aa9e8d45b7ee05afaa239a30e1e8f'
CLOCKS = {'server_ms': 'Sum of turn.response.telemetry.total_latency_ms',
          'http_ms': 'Sum of turn.latency_ms', 'conversation_ms': 'result.duration_ms',
          'runner_ms': 'finish.runner_elapsed_ms; excludes global database preparation/deletion, final export and manual scoring'}


def require(ok, message):
    if not ok:
        raise ValueError(message)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def read_ref(ref, base):
    require(isinstance(ref, dict) and set(ref) == {'path', 'sha256'}, 'Explicit path and SHA required')
    path = base / ref['path']
    require(path.is_file() and path.stat().st_size <= 16 << 20, 'Missing or oversized evidence file')
    raw = path.read_bytes()
    require(sha(raw) == ref['sha256'], 'Evidence SHA mismatch')
    return raw


def decode(raw):
    def unique(pairs):
        obj = {}
        for k, v in pairs:
            require(k not in obj, 'Duplicate JSON key')
            obj[k] = v
        return obj
    return json.loads(raw, object_pairs_hook=unique, parse_constant=lambda x: (_ for _ in ()).throw(ValueError('Nonfinite JSON')))


def number(value):
    if value is None:
        return None
    require(type(value) in (int, float), 'Numeric metric required')
    value = Decimal(str(value))
    require(value.is_finite() and value >= 0, 'Nonnegative finite metric required')
    return value


def textnum(value):
    return None if value is None else format(value.normalize(), 'f')


def total(values):
    return None if any(v is None for v in values) else sum(values, Decimal(0))


def clock_equal(a, b):
    # Go float64 accumulation may differ from decimal sums below one nanosecond.
    return a == b or (a is not None and b is not None and abs(a-b) <= Decimal('0.000001'))


def metrics(row, finish, contract):
    result = finish['result']
    observations = result.get('observations') or []
    require(row.get('observed_turns') == len(observations), 'Observed turn count mismatch')
    server, http = [], []
    for observation in observations:
        turn = observation['turn']
        telemetry = (turn.get('response') or {}).get('telemetry') or {}
        require((telemetry.get('runtime') or {}).get('contract_sha256') == contract, 'Runtime contract mismatch')
        server.append(number(telemetry.get('total_latency_ms')))
        http.append(number(turn.get('latency_ms')))
    # A zero-observation failed arm does not establish zero elapsed task time.
    server_sum, http_sum = (total(server), total(http)) if observations else (None, None)
    reported_server = number(row.get('telemetry', {}).get('response_latency_ms'))
    require((reported_server is None and row.get('status') in ('failed','setup_failed')) or clock_equal(reported_server, server_sum),
            'Report server clock differs from original telemetry')
    require(number(row.get('runner_elapsed_ms')) == number(finish.get('runner_elapsed_ms')), 'Runner clock mismatch')
    before, after = finish.get('budget_before'), finish.get('budget_after')
    # Historical journals put budget_before on start; caller supplies it below.
    attempts, cost = number(row.get('guarded_upstream_model_attempts')), number(row.get('priced_model_usage_micro_cny'))
    if before is not None and after is not None:
        require(attempts == number(after['attempts']) - number(before['attempts']), 'Guard model attempt mismatch')
        a, b = number(after.get('priced_usage_micro_cny')), number(before.get('priced_usage_micro_cny'))
        require(cost == (None if a is None or b is None else a-b), 'Guard priced usage mismatch')
    tokens = row.get('telemetry', {}).get('tokens') or {}
    jev_input = number(tokens.get('jev_input'))
    # Frozen configuration price: $0.042/M input, free output. No FX conversion or invoice claim.
    return {'model_attempts': attempts, 'model_micro_cny': cost,
            'jev_micro_usd': None if jev_input is None else jev_input * Decimal('0.042'),
            'server_ms': server_sum, 'http_ms': http_sum,
            'conversation_ms': number(result.get('duration_ms')), 'runner_ms': number(finish.get('runner_elapsed_ms'))}


def review_status(review, report, report_raw, journal_raw, revision):
    if review is None:
        return 'missing'
    require(review.get('schema') == 'pi-first-live-independent-review.v1'
            and review.get('method') == 'independent_agent_evidence_review'
            and bool(review.get('reviewer')) and review.get('execution_revision') == revision
            and review.get('journal_sha256') == sha(journal_raw)
            and review.get('report_sha256') == sha(report_raw), 'Independent review binding mismatch')
    rows = {(r['key'], r['arm']): r for r in report['rows'] if r.get('langfuse_trace_id')}
    audited = {(r['case_key'], r['arm']): r for r in review['rows']}
    require(len(audited) == len(review['rows']) and set(audited) == set(rows), 'Review coverage mismatch')
    passed = not review.get('blocking_findings')
    for identity, row in rows.items():
        audit = audited[identity]
        require(audit.get('verdict') in ('passed', 'failed') and bool(audit.get('rationale')), 'Review verdict required')
        requirements = [{k: v for k, v in q.items() if k != 'verdict'} for q in audit.get('requirements', [])]
        require(requirements == row.get('review_requirements', []), 'Review requirements mismatch')
        require(all(q.get('verdict') in ('passed', 'failed') for q in audit.get('requirements', [])), 'Requirement verdict required')
        passed &= audit['verdict'] == 'passed' and all(q['verdict'] == 'passed' for q in audit.get('requirements', []))
    if passed:
        spec = importlib.util.spec_from_file_location('langfuse_eval_summary', Path(__file__).with_name('langfuse-eval.py'))
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        module.build_review_plan(report, review, journal_raw, sha(json.dumps(review).encode()))
    return 'passed' if passed else 'failed'


def summarize(manifest, base):
    base = Path(base)
    require(manifest.get('schema') == 'repeated-v5-summary-input.v1', 'Unknown input schema')
    mode = manifest.get('mode')
    require(mode in ('repeated', 'historical-single-block'), 'Explicit evidence mode required')
    require(manifest.get('evidence_origin') in ('synthetic_test', 'actual'), 'Explicit evidence origin required')
    revision = manifest.get('execution_revision', '')
    require(re.fullmatch('[a-f0-9]{40}', revision) is not None, 'Execution revision required')
    require(manifest.get('customer_source_revision') == CUSTOMER and manifest.get('runtime_contract_sha256') == CONTRACT,
            'Customer source/runtime version mismatch')
    plan_raw = read_ref(manifest['plan'], base)
    plan = decode(plan_raw)
    repeated = mode == 'repeated'
    count = 24 if repeated else 6
    require(not repeated or sha(plan_raw) == PLAN_SHA, 'Fixed repeated plan SHA mismatch')
    require(len(plan['batches']) == count and len(manifest['pairs']) == count, f'Exactly {count} explicit pairs required')
    receipt = decode(read_ref(manifest['receipt'], base))
    require(receipt.get('schema') == ('authorized-v5-repeated-cohort.v1' if repeated else 'authorized-v5-six-case-cohort.v1'), 'Receipt schema mismatch')
    require(receipt.get('execution_revision') == revision and receipt.get('customer_source_revision') == CUSTOMER
            and receipt.get('runtime_contract_sha256') == CONTRACT and receipt.get('plan_sha256') == sha(plan_raw), 'Receipt binding mismatch')
    require(receipt.get('status') in ('checkpoint_pending', 'stopped') and len(receipt['batches']) == count, 'Receipt status/batch count mismatch')
    traces, pairs, suffix = set(), [], False
    for i, (batch, evidence, recorded) in enumerate(zip(plan['batches'], manifest['pairs'], receipt['batches'])):
        batch_id = f'batch-{i+1:03d}'
        block, position = divmod(i, 6)
        order = ARMS if not repeated or block % 2 == 0 else ARMS[::-1]
        key = CASES[(position+block) % 6] if repeated else CASES[position]
        pair_id, block_id = f'block-{block:02d}-pair-{position:02d}', f'block-{block:02d}'
        require(evidence.get('batch_id') == batch_id and recorded.get('id') == batch_id, 'Duplicate/missing batch identity')
        require(batch['key'] == key and recorded['key'] == key, 'Case schedule mismatch')
        if repeated:
            for field, want in [('pair_id', pair_id), ('block_id', block_id), ('arm_order', order)]:
                require(batch.get(field) == want and recorded.get(field) == want, 'Pair/block/order schedule mismatch')
        pair = {'batch_id': batch_id, 'pair_id': pair_id, 'block_id': block_id, 'case_key': key, 'arm_order': order,
                'status': recorded['status'], 'arms': {a: {'status': 'not_started', 'metrics': None} for a in ARMS},
                'exclusion_reasons': [], 'delta_selective_minus_model': None}
        if evidence.get('journal') is None or evidence.get('report') is None:
            require(evidence.get('journal') is None and evidence.get('report') is None and evidence.get('review') is None
                    and recorded.get('report') is None and recorded['status'] == 'not_started'
                    and receipt['status'] == 'stopped', 'Missing evidence for started batch')
            suffix = True
            pair['exclusion_reasons'] = ['not_started', 'cost_and_latency_unknown']
            pairs.append(pair)
            continue
        require(not suffix, 'Started batch after unrun suffix')
        if recorded['status'] != 'completed_pending_review' or recorded.get('failed_stage') or recorded.get('error'):
            pair['exclusion_reasons'].append('batch_failed')
        journal_raw, report_raw = read_ref(evidence['journal'], base), read_ref(evidence['report'], base)
        report = decode(report_raw)
        require(report == recorded.get('report') and report.get('journal_sha256') == sha(journal_raw), 'Report/receipt/journal mismatch')
        require(report.get('schema') == 'small-paired-report.v1' and report.get('evidence_kind') == 'recorded_run'
                and report.get('planned_arms') == 2 and len(report['rows']) == 2, 'Original two-arm report required')
        events = [decode(line) for line in journal_raw.splitlines()]
        require(events and events[0].get('kind') == 'config', 'Journal config required')
        config = events[0]['config']
        require(all(config.get(a, {}).get('execution_revision') == revision for a in ('model', 'selective')), 'Journal revision mismatch')
        if repeated:
            require(all(config.get(k) == batch[k] and report.get(k) == batch[k] for k in ('pair_id','block_id','arm_order')), 'Journal/report schedule mismatch')
        else:
            require(config.get('arm_order', ARMS) == ARMS, 'Historical order mismatch')
        finishes, pending, started = {}, None, []
        for event in events[1:]:
            require(event.get('key') == key and event.get('arm') in order, 'Journal case/arm mismatch')
            arm = event['arm']
            if event.get('kind') == 'start':
                require(pending is None and len(started) < 2 and arm == order[len(started)], 'Journal start order mismatch')
                pending = event
                started.append(arm)
            elif event.get('kind') == 'finish':
                require(pending is not None and pending['arm'] == arm, 'Journal finish order mismatch')
                finishes[arm] = {**event, 'budget_before': pending.get('budget_before')}
                pending = None
            else:
                raise ValueError('Unknown journal event')
        require(report.get('finished_arms') == len(finishes) and report.get('unrun_arms') == 2-len(finishes), 'Finished/unrun counts mismatch')
        require([r['arm'] for r in report['rows']] == ARMS, 'Report row identity mismatch')
        for row in report['rows']:
            arm = row['arm']
            require(row['key'] == key, 'Report key mismatch')
            if arm not in finishes:
                require(row['status'] in ('not_started','interrupted_unknown') and not row.get('langfuse_trace_id'), 'Unfinished arm identity mismatch')
                pair['arms'][arm]['status'] = row['status']
                pair['exclusion_reasons'].append('incomplete_pair')
                continue
            finish, trace = finishes[arm], row.get('langfuse_trace_id')
            require(trace == finish.get('langfuse_trace_id') and isinstance(trace,str) and re.fullmatch('[a-f0-9]{32}',trace)
                    and trace != '0'*32 and trace not in traces, 'Missing/duplicate trace')
            traces.add(trace)
            original = finish['result']
            if finish.get('execution_failures') or original.get('structural_pass') is not True or original.get('errors') or original.get('status') != 'needs_review':
                pair['exclusion_reasons'].append('original_execution_failed')
            measured = metrics(row, finish, CONTRACT)
            pair['arms'][arm] = {'status': row['status'], 'trace_id': trace, 'metrics': {k:textnum(v) for k,v in measured.items()}}
            if row['status'] != 'needs_review' or row.get('structural_pass') is not True or row.get('fact_errors') != 0 or row.get('http_failures') != 0 or row.get('errors') or row.get('execution_failures'):
                pair['exclusion_reasons'].append('execution_or_quality_failed')
            if any(v is None for v in measured.values()):
                pair['exclusion_reasons'].append('unknown_metric')
        review = decode(read_ref(evidence['review'],base)) if evidence.get('review') else None
        quality = review_status(review, report, report_raw, journal_raw, revision)
        pair['independent_review'] = quality
        if quality != 'passed':
            pair['exclusion_reasons'].append('independent_review_'+quality)
        if not pair['exclusion_reasons']:
            pair['delta_selective_minus_model'] = {k:textnum(Decimal(pair['arms']['jev_selective']['metrics'][k])-Decimal(v))
                for k,v in pair['arms']['model']['metrics'].items()}
        pair['exclusion_reasons'] = sorted(set(pair['exclusion_reasons']))
        pairs.append(pair)
    strata = []
    for key in CASES:
        for order in (ARMS, ARMS[::-1]):
            eligible = [p for p in pairs if p['case_key']==key and p['arm_order']==order and p['delta_selective_minus_model'] is not None]
            if not eligible:
                continue
            stats = {}
            for metric in eligible[0]['delta_selective_minus_model']:
                values = [Decimal(p['delta_selective_minus_model'][metric]) for p in eligible]
                stats[metric] = {k:textnum(v) for k,v in [('median',statistics.median(values)),('min',min(values)),('max',max(values))]}
            strata.append({'case_key':key,'arm_order':order,'eligible_pairs':len(eligible),'delta_descriptive':stats})
    return {'schema':'repeated-v5-summary.v1','mode':mode,'evidence_origin':manifest['evidence_origin'],
            'execution_revision':revision,'receipt_status':receipt['status'],
            'receipt_accounting':{k:receipt.get(k) for k in ('model_budget','jev_attempts',
                'scope_reserved_model_calls','scope_reserved_jev_calls','scope_reserved_micro_cny','scope_reserved_micro_usd')},
            'eligible_pairs':sum(p['delta_selective_minus_model'] is not None for p in pairs),
            'pairs':pairs,'strata':strata,'clock_definitions':CLOCKS,'population_effect_claims_allowed':False,
            'limitations':['Descriptive fixed-schedule observations only; no population, statistical or causal benefit claim.',
                           'Prices are frozen configuration estimates in separate currencies, not invoices.',
                           'Synthetic evidence validates tooling only; historical-single-block is not a repeated sample.',
                           'Unknown and unrun metrics remain null; excluded pairs remain visible; no user E2E clock.']}


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--input',type=Path,required=True)
    parser.add_argument('--out',type=Path,required=True)
    args=parser.parse_args()
    raw=args.input.read_bytes()
    result=summarize(decode(raw),args.input.resolve().parent)
    result['input_manifest_sha256']=sha(raw)
    args.out.mkdir(parents=False,exist_ok=False)
    (args.out/'summary.json').write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n')


if __name__=='__main__': main()
