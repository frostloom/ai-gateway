"""Independent schema-v2 evaluation comparison; Python standard library only."""
import argparse
from collections import Counter
import gzip
import hashlib
import json
import math
from pathlib import Path
import statistics
import sys


TOKEN_FIELDS = ('llm_input_tokens', 'llm_output_tokens', 'llm_cache_hit_tokens',
                'llm_cache_miss_tokens', 'llm_cache_creation_tokens')
PRICE_FIELDS = ('cache_hit_input_per_million', 'cache_miss_input_per_million',
                'output_per_million')
JEV_TOKEN_FIELDS = ('jev_input_tokens', 'jev_output_tokens')
JEV_PRICE_FIELDS = ('jev_input_per_million', 'jev_output_per_million')


def read_json(path):
    opener = gzip.open if str(path).endswith('.gz') else open
    with opener(path, 'rt', encoding='utf-8') as stream:
        return json.load(stream)


def number(value):
    return isinstance(value, (int, float)) and not isinstance(value, bool) and math.isfinite(value) and value >= 0


def success(p):
    return p['HTTPCode'] == 200 and not p['Err']


def matched(p):
    if not success(p) or p.get('JevBlocked'):
        return False
    if p['Sample']['intent'] == 'chitchat':
        return not p.get('AllTools') and not p.get('Predicted')
    return p.get('Predicted') == p['Sample']['intent']


def fraction(numerator, denominator):
    return numerator / denominator if denominator else None


def latency(values):
    values = sorted(values)
    # Nearest-rank percentiles keep a transparent definition for small subsets.
    return {'count': len(values), 'average': statistics.mean(values) if values else None,
            'p50': values[math.ceil(.50 * len(values)) - 1] if values else None,
            'p95': values[math.ceil(.95 * len(values)) - 1] if values else None}


def telemetry(p):
    return p.get('Telemetry') or {}


def selected(p):
    return telemetry(p).get('jev_fast_path') is True


def validate(reports):
    reference = reports['baseline']
    indexes = {}
    for name, report in reports.items():
        if report.get('SchemaVersion') != 2:
            raise ValueError(f'{name}: expected SchemaVersion 2')
        if report.get('JevMode') != ('off' if name == 'baseline' else 'on'):
            raise ValueError(f'{name}: incorrect JevMode (hint/fast both use on)')
        for field in ('Model', 'DatasetSHA256', 'Workers', 'Mode'):
            if report.get(field) != reference.get(field):
                raise ValueError(f'{name}: mismatched {field}')
        if not report.get('Model') or not report.get('DatasetSHA256'):
            raise ValueError(f'{name}: missing model or dataset hash')
        index = {}
        for group in ('Preds', 'AdvPreds'):
            rows = report.get(group)
            if not isinstance(rows, list):
                raise ValueError(f'{name}: {group} must be an array')
            for p in rows:
                sample = p.get('Sample', {})
                sample_id = sample.get('id')
                if not isinstance(sample_id, str) or not sample_id or sample_id in index:
                    raise ValueError(f'{name}: missing or duplicate sample ID')
                if not isinstance(sample.get('intent'), str) or not isinstance(sample.get('text'), str):
                    raise ValueError(f'{name}: invalid sample content {sample_id}')
                if not number(p.get('Latency')) or not isinstance(p.get('HTTPCode'), int) or not isinstance(p.get('Err'), str):
                    raise ValueError(f'{name}: invalid request result {sample_id}')
                index[sample_id] = (group, p)
        indexes[name] = index
    base = indexes['baseline']
    for name, index in indexes.items():
        if index.keys() != base.keys():
            raise ValueError(f'{name}: sample IDs differ')
        for sample_id, (group, p) in index.items():
            other_group, other = base[sample_id]
            if group != other_group or p['Sample'] != other['Sample']:
                raise ValueError(f'{name}: sample content/subset differs for {sample_id}')
    return {name: {sample_id: row[1] for sample_id, row in index.items()} for name, index in indexes.items()}


def usage(rows):
    totals = {field: 0 for field in TOKEN_FIELDS}
    missing = []
    complete_ids = []
    protocols = Counter()
    calls = responses = missing_responses = 0
    for p in rows:
        t = telemetry(p)
        call_count = t.get('llm_calls')
        known_calls = number(call_count)
        if known_calls:
            calls += call_count
        for field in TOKEN_FIELDS:
            if number(t.get(field)):
                totals[field] += t[field]
        if number(t.get('llm_usage_responses')):
            responses += t['llm_usage_responses']
        if number(t.get('llm_usage_missing')):
            missing_responses += t['llm_usage_missing']
        protocol = t.get('llm_usage_protocol', 'unreported')
        protocols[protocol] += 1
        complete = known_calls and call_count == 0
        if known_calls and call_count > 0:
            complete = (number(t.get('llm_usage_responses')) and t['llm_usage_responses'] == call_count
                        and t.get('llm_usage_missing') == 0
                        and all(number(t.get(k)) for k in TOKEN_FIELDS[:2]))
        (complete_ids if complete else missing).append(p['Sample']['id'])
    return {'observed_tokens': totals, 'observed_llm_attempts': calls,
            'usage_responses': responses, 'usage_missing_responses': missing_responses,
            'complete': not missing, 'complete_sample_ids': complete_ids,
            'missing_sample_ids': missing, 'protocols': dict(protocols)}


def jev_usage(rows):
    totals = {field: 0 for field in JEV_TOKEN_FIELDS}
    complete_ids, missing_ids = [], []
    responses = missing_responses = 0
    for p in rows:
        t = telemetry(p)
        for field in JEV_TOKEN_FIELDS:
            if number(t.get(field)):
                totals[field] += t[field]
        if number(t.get('jev_usage_responses')):
            responses += t['jev_usage_responses']
        if number(t.get('jev_usage_missing')):
            missing_responses += t['jev_usage_missing']
        calls = t.get('jev_calls')
        complete = number(calls) and calls == 0
        if number(calls) and calls > 0:
            complete = (t.get('jev_usage_responses') == calls and t.get('jev_usage_missing') == 0
                        and all(number(t.get(field)) for field in JEV_TOKEN_FIELDS))
        (complete_ids if complete else missing_ids).append(p['Sample']['id'])
    return {'observed_tokens': totals, 'usage_responses': responses,
            'usage_missing_responses': missing_responses, 'complete': not missing_ids,
            'complete_sample_ids': complete_ids, 'missing_sample_ids': missing_ids}


def cost(rows, prices, units):
    jev_units = jev_usage(rows)
    result = {'total': None, 'currency': prices.get('currency') if prices else None,
              'reason': '', 'assumptions': prices, 'observed_units': units['observed_tokens'],
              'jev_observed_units': jev_units['observed_tokens'], 'jev_usage_complete': jev_units['complete'],
              'jev_pricing_basis': ('per_request' if 'jev_per_request' in prices else 'tokens') if prices else None,
              'jev_requests': None, 'llm_estimate': None, 'jev_estimate': None}
    if prices is None:
        result['reason'] = 'unit prices not supplied'
        return result
    if not units['complete']:
        result['reason'] = 'LLM usage incomplete; no extrapolation'
        return result
    jev_calls = []
    for p in rows:
        t = telemetry(p)
        if not number(t.get('jev_calls')):
            result['reason'] = 'JEV request count unavailable'
            return result
        jev_calls.append(t['jev_calls'])
        if t.get('llm_calls', 0) > 0:
            if t.get('llm_usage_protocol') != 'openai':
                result['reason'] = 'cost accounting supports explicit OpenAI cache split only'
                return result
            if (not all(number(t.get(k)) for k in TOKEN_FIELDS[2:4]) or
                    t['llm_cache_hit_tokens'] + t['llm_cache_miss_tokens'] != t['llm_input_tokens']):
                result['reason'] = 'cache hit/miss accounting unavailable or inconsistent'
                return result
            if t.get('llm_cache_creation_tokens', 0):
                result['reason'] = 'cache creation tariff unsupported'
                return result
    tokens = units['observed_tokens']
    result['jev_requests'] = sum(jev_calls)
    result['llm_estimate'] = (tokens['llm_cache_hit_tokens'] * prices['cache_hit_input_per_million'] +
                             tokens['llm_cache_miss_tokens'] * prices['cache_miss_input_per_million'] +
                             tokens['llm_output_tokens'] * prices['output_per_million']) / 1000000
    if 'jev_per_request' in prices:
        result['jev_estimate'] = sum(jev_calls) * prices['jev_per_request']
    else:
        if not jev_units['complete']:
            result['reason'] = 'JEV token usage incomplete; no zero assumption or extrapolation'
            return result
        result['jev_estimate'] = sum(jev_units['observed_tokens'][field] * prices[price]
                                     for field, price in zip(JEV_TOKEN_FIELDS, JEV_PRICE_FIELDS)) / 1000000
    result['total'] = result['llm_estimate'] + result['jev_estimate']
    result['reason'] = 'estimate under supplied prices; not an actual invoiced fee'
    return result


def summarize(rows, prices):
    good = [p for p in rows if success(p)]
    accepted = [p for p in rows if selected(p)]
    accepted_good = [p for p in accepted if success(p)]
    tokens = usage(rows)
    reasons = Counter(telemetry(p).get('jev_fast_path_reason', 'unreported') for p in rows if not selected(p))
    failures = [{'id': p['Sample']['id'], 'http_code': p['HTTPCode'], 'error': p['Err'],
                 'fast_selected': selected(p), 'fast_reason': telemetry(p).get('jev_fast_path_reason')}
                for p in rows if not success(p)]
    result = {'attempts': len(rows), 'successes': len(good), 'failures': len(failures),
              'failure_details': failures, 'request_success_rate': fraction(len(good), len(rows)),
              'latency_all_ms': latency([p['Latency'] / 1000000 for p in rows]),
              'latency_success_ms': latency([p['Latency'] / 1000000 for p in good]),
              'strict_path_accuracy_all': fraction(sum(matched(p) for p in rows), len(rows)),
              'fast_selected': len(accepted), 'fast_successes': len(accepted_good),
              'successful_fast_path_coverage': fraction(len(accepted_good), len(rows)),
              'selected_fast_path_coverage': fraction(len(accepted), len(rows)),
              'accepted_path_accuracy_all': fraction(sum(matched(p) for p in accepted), len(accepted)),
              'accepted_path_accuracy_success': fraction(sum(matched(p) for p in accepted_good), len(accepted_good)),
              'accepted_failed_ids': [p['Sample']['id'] for p in accepted if not success(p)],
              'accepted_wrong_path_ids': [p['Sample']['id'] for p in accepted_good if not matched(p)],
              'accepted_usage': usage(accepted),
              'fallback_usage': usage([p for p in rows if not selected(p)]),
              'fallback_reasons': dict(reasons), 'usage': tokens, 'jev_usage': jev_usage(rows),
              'estimated_cost': cost(rows, prices, tokens),
              'telemetry_samples': sum(bool(telemetry(p)) for p in rows),
              'fast_status_missing_ids': [p['Sample']['id'] for p in rows if 'jev_fast_path' not in telemetry(p)],
              'jev_error_samples': sum(telemetry(p).get('jev_errors', 0) > 0 for p in rows),
              'normal_or_adversarial_blocked': sum(bool(p.get('JevBlocked') or telemetry(p).get('jev_blocked')) for p in rows)}
    result['phase_latency_ms'] = {k: latency([telemetry(p)[k] for p in rows if number(telemetry(p).get(k))])
                                  for k in ('jev_latency_ms', 'llm_latency_ms', 'tool_latency_ms')}
    result['confidence_bins'] = {}
    for low, high in ((0, .5), (.5, .8), (.8, .9), (.9, 1.01)):
        subset = [p for p in rows if number(telemetry(p).get('jev_intent_confidence'))
                  and low <= telemetry(p)['jev_intent_confidence'] < high]
        result['confidence_bins'][f'{low:g}..{min(high, 1):g}'] = {
            'attempts': len(subset), 'successes': sum(success(p) for p in subset),
            'fast_selected': sum(selected(p) for p in subset),
            'strict_path_accuracy_all': fraction(sum(matched(p) for p in subset), len(subset)),
            'latency_all_ms': latency([p['Latency'] / 1000000 for p in subset])}
    result['confidence_missing_ids'] = [p['Sample']['id'] for p in rows
                                        if not number(telemetry(p).get('jev_intent_confidence'))]
    if result['fast_status_missing_ids']:
        result['successful_fast_path_coverage'] = None
        result['selected_fast_path_coverage'] = None
    result['safety'] = {
        'successful_zero_tool_ids': [p['Sample']['id'] for p in good if not p.get('AllTools') and not p.get('PendingTool')],
        'successful_tool_or_pending_ids': [p['Sample']['id'] for p in good if p.get('AllTools') or p.get('PendingTool')],
        'confirmation_required_ids': [p['Sample']['id'] for p in rows if p['Sample'].get('needs_confirm')],
        'confirmation_pending_ids': [p['Sample']['id'] for p in good if p.get('Pending')],
        'fast_selected_high_risk_or_confirm_ids': [p['Sample']['id'] for p in accepted
                                                  if p['Sample'].get('needs_confirm') or p['Sample'].get('risk') == 'high']}
    return result


def paired(indexes, ids):
    result = {'ids': ids, 'count': len(ids), 'runs': {}}
    for name, index in indexes.items():
        result['runs'][name] = latency([index[i]['Latency'] / 1000000 for i in ids])
    result['baseline_minus_fast_ms'] = latency([
        # Differences may be negative; never clip regressions.
        (indexes['baseline'][i]['Latency'] - indexes['fast'][i]['Latency']) / 1000000 for i in ids])
    result['hint_minus_fast_ms'] = latency([(indexes['hint'][i]['Latency'] - indexes['fast'][i]['Latency']) / 1000000 for i in ids])
    return result


def matched_cost(indexes, ids, prices):
    result = {'ids': ids, 'count': len(ids), 'runs': {}}
    for name, index in indexes.items():
        rows = [index[i] for i in ids]
        units = usage(rows)
        result['runs'][name] = {'usage': units, 'jev_usage': jev_usage(rows),
                                'estimated_cost': cost(rows, prices, units)}
    for name in ('baseline', 'hint'):
        before = result['runs'][name]['estimated_cost']['total']
        after = result['runs']['fast']['estimated_cost']['total']
        result[name + '_minus_fast_estimated_cost'] = before - after if before is not None and after is not None else None
    return result


def compare(reports, prices=None):
    indexes = validate(reports)
    ids = sorted(indexes['baseline'])
    selected_ids = [i for i in ids if selected(indexes['fast'][i])]
    common_success = [i for i in ids if all(success(index[i]) for index in indexes.values())]
    avoided = {'ids': selected_ids, 'count': len(selected_ids), 'baseline_minus_fast': None,
               'hint_minus_fast': None, 'missing_telemetry_ids': []}
    known = [i for i in selected_ids if all(number(telemetry(index[i]).get('llm_calls')) for index in indexes.values())]
    avoided['matched_known_ids'] = known
    avoided['missing_telemetry_ids'] = sorted(set(selected_ids) - set(known))
    for name in ('baseline', 'hint'):
        observed = sum(telemetry(indexes[name][i])['llm_calls'] - telemetry(indexes['fast'][i])['llm_calls'] for i in known)
        avoided[f'observed_{name}_minus_fast'] = observed if known else None
        if selected_ids and len(known) == len(selected_ids):
            avoided[f'{name}_minus_fast'] = observed
    complete_usage_ids = [i for i in ids if all(usage([index[i]])['complete'] for index in indexes.values())]
    token_differences = {}
    for name in ('baseline', 'hint'):
        token_differences[name + '_minus_fast'] = {
            field: sum(telemetry(indexes[name][i]).get(field, 0) - telemetry(indexes['fast'][i]).get(field, 0)
                       for i in complete_usage_ids) for field in TOKEN_FIELDS[:2]}
    quality = {}
    for name in ('baseline', 'hint'):
        quality[name + '_to_fast'] = {
            'matched_all_ids': ids,
            'new_failure_ids': [i for i in ids if success(indexes[name][i]) and not success(indexes['fast'][i])],
            'recovered_failure_ids': [i for i in ids if not success(indexes[name][i]) and success(indexes['fast'][i])],
            'path_regression_ids': [i for i in ids if matched(indexes[name][i]) and not matched(indexes['fast'][i])],
            'path_improvement_ids': [i for i in ids if not matched(indexes[name][i]) and matched(indexes['fast'][i])],
            'fast_selected_correct_ids': [i for i in selected_ids if matched(indexes['fast'][i])],
            'fast_selected_incorrect_or_failed_ids': [i for i in selected_ids if not matched(indexes['fast'][i])]}
    return {'schema_version': 1, 'model': reports['baseline']['Model'],
            'dataset_sha256': reports['baseline']['DatasetSHA256'], 'sample_count': len(ids),
            'full_suite_1220': len(reports['baseline']['Preds']) == 1200 and len(reports['baseline']['AdvPreds']) == 20,
            'runs': {name: { 'normal': summarize(report['Preds'], prices),
                              'adversarial': summarize(report['AdvPreds'], prices),
                              'all': summarize(report['Preds'] + report['AdvPreds'], prices),
                              'source_metrics': report.get('Metrics', {})}
                     for name, report in reports.items()},
            'paired_latency': {'all_three_success': paired(indexes, common_success),
                               'fast_selected_all_three_success': paired(indexes, [i for i in common_success if i in selected_ids])},
            'llm_attempts_avoided': avoided,
            'matched_fast_selected_cost': matched_cost(indexes, selected_ids, prices),
            'matched_quality': quality,
            'matched_complete_usage': {'ids': complete_usage_ids, 'count': len(complete_usage_ids),
                                      'fast_selected_complete_ids': [i for i in selected_ids if i in complete_usage_ids],
                                      'fast_selected_missing_usage_ids': [i for i in selected_ids if i not in complete_usage_ids],
                                      'excluded_ids': sorted(set(ids) - set(complete_usage_ids)),
                                      'observed_token_differences': token_differences},
            'limitations': [
                'HTTP success and strict first-tool matching do not establish independent business completion.',
                'Full request Latency is nanoseconds converted to milliseconds and includes JEV overhead.',
                'All-attempt metrics include failures; paired success subsets and excluded IDs are explicit.',
                'Confidence is an uncalibrated JEV prediction, not a probability of correctness.',
                'Separate runs are observational and subject to upstream load; no causal speedup guarantee.',
                'Zero tools on adversarial inputs does not independently establish tenant, confirmation, or funds safety.',
                'Observed token totals may be partial. No extrapolation or fees without coherent usage and supplied tariffs.',
                'JevMode on alone does not prove hint versus fast configuration; verify run provenance and telemetry.']}


def markdown(result):
    lines = ['# JEV efficiency comparison', '',
             f"Model: `{result['model']}`. Matched samples: {result['sample_count']}. Full 1200+20 suite: {result['full_suite_1220']}.", '',
             '| Arm | Attempts | Failures | All avg ms | All P50 ms | All P95 ms | Successful avg ms | Fast selected | Fast successful coverage | Accepted path accuracy (incl failures) |',
             '|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|']
    def fmt(value):
        return 'unavailable' if value is None else f'{value:.4g}' if isinstance(value, float) else str(value)
    for name, run in result['runs'].items():
        row = run['normal']
        lines.append('| ' + ' | '.join(map(fmt, [name, row['attempts'], row['failures'],
                     row['latency_all_ms']['average'], row['latency_all_ms']['p50'], row['latency_all_ms']['p95'],
                     row['latency_success_ms']['average'], row['fast_selected'], row['successful_fast_path_coverage'],
                     row['accepted_path_accuracy_all']])) + ' |')
    lines.extend(['', '## Paired request latency', '',
                  f"Common successful subset count: {result['paired_latency']['all_three_success']['count']} (all three arms, same IDs).", '',
                  '```json', json.dumps(result['paired_latency'], indent=2, ensure_ascii=False), '```', '',
                  '## Observed LLM attempts and token differences', '', '```json',
                  json.dumps({'llm_attempts_avoided': result['llm_attempts_avoided'],
                              'matched_complete_usage': result['matched_complete_usage'],
                              'matched_fast_selected_cost': result['matched_fast_selected_cost'],
                              'matched_quality': result['matched_quality']}, indent=2, ensure_ascii=False), '```'])
    for name, run in result['runs'].items():
        lines.extend(['', '## ' + name + ': failures, usage, cost, confidence and safety', '', '```json',
                      json.dumps(run, indent=2, ensure_ascii=False), '```'])
    lines.extend(['', '## Interpretation limits', ''] + ['- ' + value for value in result['limitations']])
    return '\n'.join(lines) + '\n'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='command', required=True)
    cli = sub.add_parser('compare')
    for name in ('baseline', 'hint', 'fast', 'out'):
        cli.add_argument('--' + name, required=True, type=Path)
    cli.add_argument('--prices', type=Path, help='optional explicit JSON unit prices')
    args = parser.parse_args()
    try:
        if args.out.exists():
            raise ValueError('output directory already exists; overwrite refused')
        reports = {name: read_json(getattr(args, name)) for name in ('baseline', 'hint', 'fast')}
        prices = read_json(args.prices) if args.prices else None
        if prices is not None and (not isinstance(prices, dict) or not prices.get('currency') or
                                   not all(number(prices.get(k)) for k in PRICE_FIELDS)):
            raise ValueError('prices require currency and three nonnegative finite LLM unit prices')
        if prices is not None:
            request_tariff = 'jev_per_request' in prices
            token_tariff = any(k in prices for k in JEV_PRICE_FIELDS)
            if request_tariff == token_tariff:
                raise ValueError('supply exactly one JEV tariff: per_request or both token prices; mutually exclusive')
            fields = ('jev_per_request',) if request_tariff else JEV_PRICE_FIELDS
            if not all(number(prices.get(k)) for k in fields):
                raise ValueError('JEV tariff requires nonnegative finite prices for the selected billing basis')
        result = compare(reports, prices)
        result['sources'] = {name: {'path': str(getattr(args, name).resolve()),
                                   'sha256': hashlib.sha256(getattr(args, name).read_bytes()).hexdigest()}
                             for name in reports}
        output_json = json.dumps(result, indent=2, ensure_ascii=False, allow_nan=False) + '\n'
        output_md = markdown(result)
        args.out.mkdir(parents=True, exist_ok=False)
        (args.out / 'comparison.json').write_text(output_json, encoding='utf-8')
        (args.out / 'comparison.md').write_text(output_md, encoding='utf-8')
        print('Wrote comparison.json and comparison.md')
        return 0
    except (ValueError, OSError, KeyError, TypeError) as exc:
        print(f'comparison rejected: {exc}', file=sys.stderr)
        return 2


if __name__ == '__main__':
    sys.exit(main())
