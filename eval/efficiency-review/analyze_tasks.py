"""Compare three taskbench.v2 runs without rescoring business completion."""
import argparse
import hashlib
import json
from pathlib import Path
import sys

import analyze


def validate(reports):
    baseline = reports['baseline']
    indexes = {}
    tenants = set()
    for arm, report in reports.items():
        if report.get('schema') != 'taskbench.v2':
            raise ValueError(f'{arm}: requires taskbench.v2')
        metadata = report.get('metadata', {})
        if metadata.get('jev_mode') != ('off' if arm == 'baseline' else 'on'):
            raise ValueError(f'{arm}: incorrect jev_mode')
        if not report.get('dataset_sha256') or report['dataset_sha256'] != baseline.get('dataset_sha256'):
            raise ValueError(f'{arm}: dataset hash mismatch')
        for key in ('model', 'source_git', 'binary_sha256', 'evaluator_binary_sha256'):
            if not metadata.get(key) or metadata[key] != baseline.get('metadata', {}).get(key):
                raise ValueError(f'{arm}: source/model/binary mismatch: {key}')
        results = report.get('results')
        if not isinstance(results, list) or len(results) != 24:
            raise ValueError(f'{arm}: expected all 24 tasks')
        index = {}
        for task in results:
            case_id = task.get('case', {}).get('id')
            if not isinstance(case_id, str) or not case_id or case_id in index:
                raise ValueError(f'{arm}: duplicate/missing task ID')
            if not isinstance(task.get('success'), bool) or not analyze.number(task.get('duration_ms')):
                raise ValueError(f'{arm}: invalid task result {case_id}')
            if not isinstance(task.get('turns'), list):
                raise ValueError(f'{arm}: turns unavailable for {case_id}')
            tenant = task.get('fixture', {}).get('tenant_id')
            if isinstance(tenant, int) and tenant > 0:
                if tenant in tenants:
                    raise ValueError(f'{arm}: reused tenant {tenant}; each task/arm needs an independent fixture')
                tenants.add(tenant)
            elif not task.get('setup_error'):
                raise ValueError(f'{arm}: tenant provenance unavailable {case_id}')
            index[case_id] = task
        indexes[arm] = index
    base = indexes['baseline']
    for arm, index in indexes.items():
        if index.keys() != base.keys():
            raise ValueError(f'{arm}: actual task IDs mismatch')
        for case_id, task in index.items():
            if task['case'] != base[case_id]['case']:
                raise ValueError(f'{arm}: actual case content mismatch {case_id}')
    return indexes


def turns(task):
    rows = []
    for ordinal, turn in enumerate(task['turns']):
        try:
            raw = json.loads(turn.get('raw_body', ''))
            t = raw.get('telemetry') if isinstance(raw, dict) else None
            if not isinstance(t, dict):
                t = None
        except (ValueError, TypeError):
            t = None
        # Typed response telemetry intentionally ignored: it discards most fields.
        if not analyze.number(turn.get('latency_ms')):
            raise ValueError(f"invalid turn latency {task['case']['id']}:{ordinal}")
        rows.append({'Sample': {'id': task['case']['id'] + ':' + str(ordinal)},
                     'Telemetry': t, 'Latency': turn['latency_ms'] * 1000000,
                     'HTTPCode': turn.get('status'), 'Err': turn.get('error', ''),
                     'stage': turn.get('stage', 'unreported')})
    return rows


def units(rows, prices):
    observed = analyze.usage(rows)
    return {'usage': observed, 'jev_usage': analyze.jev_usage(rows),
            'estimated_cost': analyze.cost(rows, prices, observed),
            'llm_attempts_complete': all(analyze.number(analyze.telemetry(p).get('llm_calls')) for p in rows),
            'turn_attempts': len(rows),
            'request_latency_all_ms': analyze.latency([p['Latency'] / 1000000 for p in rows]),
            'request_failure_details': [{'id': p['Sample']['id'], 'status': p['HTTPCode'], 'error': p['Err'],
                                         'fast_selected': analyze.selected(p),
                                         'fast_reason': analyze.telemetry(p).get('jev_fast_path_reason')}
                                        for p in rows if p['HTTPCode'] != 200 or p['Err']],
            'fast_status_missing_turn_ids': [p['Sample']['id'] for p in rows if 'jev_fast_path' not in analyze.telemetry(p)],
            'missing_raw_telemetry_ids': [p['Sample']['id'] for p in rows if not analyze.telemetry(p)]}


def paired_latency(indexes, ids):
    return {'ids': ids, 'count': len(ids),
            'runs': {arm: analyze.latency([index[i]['duration_ms'] for i in ids]) for arm, index in indexes.items()},
            'baseline_minus_fast_ms': analyze.latency([indexes['baseline'][i]['duration_ms'] - indexes['fast'][i]['duration_ms'] for i in ids]),
            'hint_minus_fast_ms': analyze.latency([indexes['hint'][i]['duration_ms'] - indexes['fast'][i]['duration_ms'] for i in ids])}


def matched_units(indexes, ids, prices):
    result = {'ids': ids, 'count': len(ids), 'runs': {}}
    for arm, index in indexes.items():
        rows = [p for i in ids for p in turns(index[i])]
        result['runs'][arm] = units(rows, prices)
    for arm in ('baseline', 'hint'):
        before, after = result['runs'][arm], result['runs']['fast']
        result[arm + '_minus_fast_llm_attempts'] = (before['usage']['observed_llm_attempts'] - after['usage']['observed_llm_attempts']
                                                   if before['llm_attempts_complete'] and after['llm_attempts_complete'] else None)
        result[arm + '_minus_fast_estimated_cost'] = (before['estimated_cost']['total'] - after['estimated_cost']['total']
                                                     if before['estimated_cost']['total'] is not None and after['estimated_cost']['total'] is not None else None)
        result[arm + '_minus_fast_tokens'] = ({field: before['usage']['observed_tokens'][field] - after['usage']['observed_tokens'][field]
                                               for field in analyze.TOKEN_FIELDS[:2]}
                                              if before['usage']['complete'] and after['usage']['complete'] else None)
    return result


def compare(reports, prices):
    indexes = validate(reports)
    ids = sorted(indexes['baseline'])
    runs = {}
    selected_ids = []
    for arm, index in indexes.items():
        tasks = {}
        rows = []
        for case_id in ids:
            task = index[case_id]
            requests = turns(task)
            rows.extend(requests)
            detail = units(requests, prices)
            detail.update({key: task.get(key) for key in ('success', 'errors', 'setup_error', 'duration_ms',
                          'clarification_offered', 'redundant_clarification', 'preview_observed',
                          'preview_correct', 'premature_write', 'cancel_no_write_observed', 'time_to_preview_ms')})
            detail['fast_selected_turn_ids'] = [p['Sample']['id'] for p in requests if analyze.selected(p)]
            detail['fast_selected'] = bool(detail['fast_selected_turn_ids'])
            detail['tenant_id'] = task.get('fixture', {}).get('tenant_id')
            detail['stage_usage'] = {stage: units([p for p in requests if p['stage'] == stage], prices)
                                     for stage in sorted(set(p['stage'] for p in requests))}
            tasks[case_id] = detail
            if arm == 'fast' and detail['fast_selected']:
                selected_ids.append(case_id)
        summary = units(rows, prices)
        summary.update({'source_metrics': reports[arm].get('metrics', {}), 'source_limitations': reports[arm].get('limitations', []),
                        'metadata': reports[arm]['metadata'], 'tasks': tasks,
                        'latency_all_tasks_ms': analyze.latency([index[i]['duration_ms'] for i in ids]),
                        'latency_successful_tasks_ms': analyze.latency([index[i]['duration_ms'] for i in ids if index[i]['success']]),
                        'failure_details': [{'id': i, 'errors': index[i].get('errors', []),
                                             'setup_error': index[i].get('setup_error'), 'fast_selected': tasks[i]['fast_selected']}
                                            for i in ids if not index[i]['success']],
                        'stage_usage': {stage: units([p for p in rows if p['stage'] == stage], prices)
                                        for stage in sorted(set(p['stage'] for p in rows))}})
        runs[arm] = summary
    successful = [i for i in ids if all(index[i]['success'] for index in indexes.values())]
    unknown = [i for i in ids if runs['fast']['tasks'][i]['fast_status_missing_turn_ids'] and i not in selected_ids]
    nonselected = [i for i in ids if i not in selected_ids and i not in unknown]
    other = matched_units(indexes, nonselected, prices)
    other['interpretation'] = 'Differences on tasks without a selected JEV fast path are not attributable to JEV fast-path execution; confirmation/cancellation guards and ordinary model variation must be examined separately.'
    return {'schema': 'taskbench.efficiency.v1', 'dataset_sha256': reports['baseline']['dataset_sha256'],
            'actual_cases_sha256': hashlib.sha256(json.dumps([indexes['baseline'][i]['case'] for i in ids],
                                      sort_keys=True, separators=(',', ':'), ensure_ascii=False).encode('utf-8')).hexdigest(),
            'actual_cases': [indexes['baseline'][i]['case'] for i in ids], 'task_count': len(ids), 'runs': runs,
            'paired_latency': {'all_three_success': paired_latency(indexes, successful),
                               'fast_selected_all_three_success': paired_latency(indexes, [i for i in successful if i in selected_ids])},
            'fast_selected_task_comparison': matched_units(indexes, selected_ids, prices),
            'non_fast_selected_task_comparison': other,
            'fast_selection_unknown_task_ids': unknown,
            'matched_quality': {arm + '_to_fast': {
                'new_failure_ids': [i for i in ids if indexes[arm][i]['success'] and not indexes['fast'][i]['success']],
                'recovered_failure_ids': [i for i in ids if not indexes[arm][i]['success'] and indexes['fast'][i]['success']]}
                for arm in ('baseline', 'hint')},
            'limitations': [
                'Original task success, clarification and safety rubric retained without rescoring. Necessary followups are not errors.',
                'Redundant clarification is a heuristic candidate for manual review, not a proven error.',
                'Task duration includes final state verification but excludes fixture setup; synthetic followups have no user think time.',
                'All turn attempts, including failed responses and selected tool errors, remain in usage and cost denominators.',
                'Raw body telemetry is authoritative; missing or malformed raw telemetry stays unavailable, not zero.',
                'Same-task subsets may contain different turn sequences; they measure complete tasks, not a fabricated one-to-one turn match.',
                'Selected-task savings include the whole task and do not independently isolate a causal JEV effect.',
                'Independent fixture tenant IDs are checked across every task and arm; setup failures lacking an ID remain explicit.',
                'Repeated runs remain subject to upstream load and model variability.']}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    cli = parser.add_subparsers(dest='command', required=True).add_parser('compare')
    for name in ('baseline', 'hint', 'fast', 'out'):
        cli.add_argument('--' + name, type=Path, required=True)
    cli.add_argument('--prices', type=Path)
    args = parser.parse_args()
    try:
        if args.out.exists():
            raise ValueError('output directory exists; overwrite refused')
        prices = analyze.read_json(args.prices) if args.prices else None
        if prices is not None:
            if not isinstance(prices, dict) or not prices.get('currency') or not all(analyze.number(prices.get(k)) for k in analyze.PRICE_FIELDS):
                raise ValueError('currency and three finite nonnegative LLM prices required')
            request_tariff = 'jev_per_request' in prices
            token_tariff = any(k in prices for k in analyze.JEV_PRICE_FIELDS)
            if request_tariff == token_tariff or not all(analyze.number(prices.get(k)) for k in (('jev_per_request',) if request_tariff else analyze.JEV_PRICE_FIELDS)):
                raise ValueError('choose one complete JEV tariff: per-request or token-based')
        reports = {arm: analyze.read_json(getattr(args, arm)) for arm in ('baseline', 'hint', 'fast')}
        result = compare(reports, prices)
        result['sources'] = {arm: {'path': str(getattr(args, arm).resolve()),
                                  'sha256': hashlib.sha256(getattr(args, arm).read_bytes()).hexdigest()} for arm in reports}
        content = json.dumps(result, indent=2, ensure_ascii=False, allow_nan=False) + '\n'
        args.out.mkdir(parents=True, exist_ok=False)
        (args.out / 'comparison.json').write_text(content, encoding='utf-8')
        (args.out / 'comparison.md').write_text('# Multi-turn task efficiency comparison\n\nOriginal completion and clarification rubric retained. All failures and usage completeness remain visible.\n\n```json\n' + content + '```\n', encoding='utf-8')
        print('Wrote task comparison.json and comparison.md')
        return 0
    except (ValueError, OSError, KeyError, TypeError) as exc:
        print(f'task comparison rejected: {exc}', file=sys.stderr)
        return 2


if __name__ == '__main__':
    sys.exit(main())
