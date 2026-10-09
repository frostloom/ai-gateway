#!/usr/bin/env python3
"""Append-only, standard-library contract exporter/scorer. No service or model calls."""
import argparse
from collections import Counter
import gzip
import hashlib
import json
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[1]
AUDIT = ROOT / 'eval/reviews/2026-10-09-intent-offline-audit'
DEFAULT_OUT = ROOT / 'eval/contracts/2026-10-09-customer-service-v1'
WRITE = {'recharge', 'refund_recharge', 'subscribe_plan', 'change_plan', 'cancel_subscription'}
READ = {'get_balance', 'get_consumption', 'get_recent_bills', 'list_models', 'list_plans', 'get_my_subscription'}
INDEPENDENT_SOURCES = {'synthetic_regression', 'frozen_pre_run_fixture'}
VERDICTS = ('pass', 'fail', 'unverifiable', 'service_failure')
DIMENSIONS = ('next_action', 'answer', 'numeric_truth', 'preview_parameters', 'full_task')


def sha(data):
    return hashlib.sha256(data).hexdigest()


def read_json(path):
    return json.loads(Path(path).read_text(encoding='utf-8'))


def read_report(path):
    raw = Path(path).read_bytes()
    if str(path).endswith('.gz'):
        raw = gzip.decompress(raw)
    return json.loads(raw.decode('utf-8'))


def write_new(path, value):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open('x', encoding='utf-8', newline='\n') as handle:
        json.dump(value, handle, ensure_ascii=False, indent=2, sort_keys=True)
        handle.write('\n')


def normalize_text(sample, proof):
    """Only exact ID+original-text allowlist entries may change evaluation input."""
    entry = proof.get(sample['id'])
    return entry['text'] if entry and entry['original_text'] == sample['text'] else sample['text']


def normalization_proof(rows):
    generator = ROOT / 'web/scripts/gen-intent-suite.mjs'
    manifest = read_json(ROOT / 'eval/intent-suite/manifest.json')
    source = generator.read_text(encoding='utf-8')
    if manifest['generatedBy'] != 'web/scripts/gen-intent-suite.mjs' or 'text = `${text} #${n}`' not in source:
        raise ValueError('Generator provenance changed; normalization refused')
    proof = {}
    for group in manifest['classes']:
        samples = read_json(ROOT / 'eval/intent-suite' / group['file'])
        seen = {}
        # IDs are allocated before the generator shuffles the output rows.
        for sample in sorted(samples, key=lambda s: s['id']):
            match = re.fullmatch(r'(.+) #(\d+)', sample['text'])
            if match and int(match[2]) >= 2 and match[1] in seen:
                proof[sample['id']] = {'original_text': sample['text'], 'text': match[1],
                    'base_case_id': seen[match[1]], 'source_file': 'eval/intent-suite/' + group['file'],
                    'generator_sha256': sha(generator.read_bytes()),
                    'source_sha256': sha((ROOT / 'eval/intent-suite' / group['file']).read_bytes()),
                    'reason': 'Exact generated duplicate with preceding unsuffixed base in same source class'}
            seen[sample['text']] = sample['id']
    originals = {r['id']: r['original'] for r in rows}
    for sid, entry in proof.items():
        if originals.get(sid, {}).get('text') != entry['original_text']:
            raise ValueError('Audit differs from generator source: ' + sid)
    return proof


def export_case(row, draft, proof):
    review, original = row['review'], row['original']
    allowed = review.get('allowed_first_actions') or [review['expected_action']]
    missing = draft.get('missing_slots', [])
    # The reviewed action takes precedence over the older rule draft.
    if 'clarify' in allowed and not missing:
        missing = ['clarify_user_goal_or_required_context']
    requirements = review['expected_reply_requirements']
    return {'id': 'cs-contract-v1:' + row['id'], 'source_case_id': row['id'],
        'original': original, 'text': normalize_text(original, proof),
        'normalization': proof.get(row['id']),
        'annotation_status': 'ai_posthoc_not_independent_gold', 'human_independent_review_count': 0,
        'fixture_status': 'not_instantiated', 'expected_action': review['expected_action'],
        'allowed_next_actions': allowed, 'allowed_tools': review['allowed_tools'],
        'necessary_slots': draft.get('missing_slots', []) + list(draft.get('parameters', {})),
        'missing_slots': missing, 'parameters_draft': draft.get('parameters', {}),
        'slot_status': 'draft_requires_fixture_and_business_validation',
        'required_facts': requirements,
        'reply_rules': [{'id': 'reply-' + str(i + 1), 'requirement': requirement,
            'verification': 'Explicit answer assertions matched to independent fixture; otherwise unverifiable'}
            for i, requirement in enumerate(requirements)],
        'confirmation': {'required': original['intent'] in WRITE, 'tool': original['intent'],
            'preview_required_before_execution': original['intent'] in WRITE,
            'preview_facts': ['target', 'verified_parameters', 'amount_or_effect', 'effective_time'],
            'binding': ['authenticated_tenant', 'operation', 'parameters'],
            'revalidate_on_confirm': True, 'cancel_without_write': True, 'idempotent_confirm': True},
        'forbidden': ['fabricated_account_facts', 'cross_tenant_access', 'unconfirmed_write',
                      'write_after_cancel', 'duplicate_charge', 'incorrect_preview_parameters'],
        'annotation_reason': review.get('annotation_reason', ''),
        'fixture_requirements': ['pre_run_initial_state', 'authenticated_tenant', 'tool_return_truth',
            'expected_answer', 'expected_terminal_state', 'supplement_confirm_cancel_branches'],
        'historical_review_reference': {'file': 'eval/reviews/2026-10-09-intent-offline-audit/reviews.json',
                                        'id': row['id']}}


def check_values(expected, observed):
    if not expected or not isinstance(observed, dict):
        return 'unverifiable'
    if any(key in observed and (type(observed[key]) is not type(value) or observed[key] != value)
           for key, value in expected.items()):
        return 'fail'
    return 'pass' if all(key in observed for key in expected) else 'unverifiable'


def score_case(case, observation, fixture=None):
    result = {'id': case['id'], 'source_case_id': case.get('source_case_id', case['id']),
              'next_action': 'unverifiable', 'answer': 'unverifiable',
              'numeric_truth': 'unverifiable', 'preview_parameters': 'unverifiable',
              'full_task': 'unverifiable', 'reasons': []}
    if observation is None:
        result['reasons'].append('Missing observation retained in denominator')
        return result
    if observation.get('http_code') is not None and (observation['http_code'] != 200 or observation.get('err')):
        result.update({key: 'service_failure' for key in DIMENSIONS})
        result['reasons'].append('HTTP/error failure; not classified as intent error')
        return result
    if observation.get('err'):
        result.update({key: 'service_failure' for key in DIMENSIONS})
        return result
    if observation.get('http_code') != 200:
        result['reasons'].append('HTTP outcome missing; cannot verify normal response')
        return result
    trace = observation.get('trace') or []
    tools = [step.get('tool') for step in trace]
    assertions = observation.get('assertions') or {}
    action = assertions.get('action') or {}
    reply, pending = observation.get('reply', ''), observation.get('pending')
    allowed, allowed_tools = case['allowed_next_actions'], set(case['allowed_tools'])
    executed = any(step.get('tool') in WRITE and step.get('phase') != 'preview' for step in trace)
    if set(tools) - allowed_tools or executed:
        result['next_action'] = 'fail'
        result['reasons'].append('Forbidden tool or executed write in first-turn trace')
    elif pending:
        result['next_action'] = ('pass' if 'preview' in allowed and isinstance(pending, dict)
            and pending.get('tool') == case['confirmation']['tool'] else 'fail')
    elif action:
        kind = action.get('kind')
        span = action.get('reply_span')
        if kind not in allowed:
            result['next_action'] = 'fail'
        elif not span or span not in reply:
            result['reasons'].append('Action assertion requires a real reply span')
        elif kind == 'clarify' and action.get('slot') not in case.get('missing_slots', []):
            result['next_action'] = 'fail'
        else:
            result['next_action'] = 'pass'
    elif tools and 'read' in allowed and set(tools).issubset(READ):
        result['next_action'] = 'pass'
    else:
        result['reasons'].append('No tools alone do not prove clarification, response or refusal')
    independent = (fixture and fixture.get('id') == case['id'] and fixture.get('independent') is True
        and fixture.get('initial_state_valid') is True and fixture.get('source') in INDEPENDENT_SOURCES
        and fixture.get('evidence_ref') and assertions.get('evidence_ref'))
    if not independent:
        result['reasons'].append('Independent fixture and measured assertion evidence absent')
        return result
    result['answer'] = check_values(fixture.get('expected_answer'), assertions.get('answer'))
    numeric = {key: value for key, value in fixture.get('expected_answer', {}).items()
               if isinstance(value, (int, float)) and not isinstance(value, bool)}
    result['numeric_truth'] = check_values(numeric, assertions.get('answer'))
    result['preview_parameters'] = check_values(fixture.get('expected_pending'), pending)
    state = check_values(fixture.get('expected_state'), assertions.get('state'))
    write_terminal_verified = (not case.get('confirmation', {}).get('required')
        or fixture.get('expected_state', {}).get('pending') is False)
    preview_required = ('expected_pending' in fixture
        or case.get('confirmation', {}).get('preview_required_before_execution') is True)
    necessary_preview_verified = not preview_required or result['preview_parameters'] == 'pass'
    if result['answer'] == 'fail' or state == 'fail' or result['next_action'] == 'fail' or result['preview_parameters'] == 'fail':
        result['full_task'] = 'fail'
    elif (result['answer'] == state == 'pass' and assertions.get('terminal') is True
          and fixture.get('expected_state', {}).get('forbidden_side_effects') == 0
          and write_terminal_verified and necessary_preview_verified):
        result['full_task'] = 'pass'
    elif preview_required and result['preview_parameters'] == 'unverifiable':
        result['reasons'].append('Required preview lacks independent truth or complete observed parameters')
    return result


def unique_index(rows):
    index = {row['id']: row for row in rows}
    if len(index) != len(rows):
        raise ValueError('Duplicate IDs refused')
    return index


def import_observations(cases, report):
    """Convert captured reports, never invent phase/arguments or semantic assertions."""
    unique_index(cases)
    sources = {c['source_case_id']: c for c in cases}
    if len(sources) != len(cases):
        raise ValueError('Duplicate source case IDs refused')
    predictions = list(report.get('Preds') or []) + list(report.get('AdvPreds') or [])
    seen, observations = set(), []
    for prediction in predictions:
        sample = prediction.get('Sample') or {}
        sid = sample.get('id')
        if not isinstance(sid, str) or sid not in sources or sid in seen:
            raise ValueError('Unknown, invalid or duplicate report sample ID: ' + str(sid))
        seen.add(sid)
        case = sources[sid]
        if sample.get('text') not in {case['original']['text'], case['text']}:
            raise ValueError('Report sample text differs from original and normalized text: ' + sid)
        body, warnings = {}, []
        raw_body = prediction.get('RawBody')
        if raw_body:
            try:
                body = json.loads(raw_body)
                if not isinstance(body, dict):
                    body = {}
                    warnings.append('RawBody is not an object; only captured report fields used')
            except (ValueError, TypeError):
                warnings.append('RawBody is not JSON; retained verbatim')
        if 'trace' in body:
            trace, trace_source = body['trace'] or [], 'raw_body'
        else:
            trace, trace_source = prediction.get('Trace') or [], 'report_trace'
        if not isinstance(trace, list) or any(not isinstance(step, dict) for step in trace):
            raise ValueError('Invalid captured trace structure: ' + sid)
        # Go JSON tags normally use lower case; support equivalent structured captures.
        trace = [{({'Tool': 'tool', 'Phase': 'phase', 'Outcome': 'outcome',
                    'ResolvedArgs': 'resolved_args', 'Args': 'args'}.get(k, k)): v
                  for k, v in step.items()} for step in trace]
        pending_flag = body.get('pending_confirm', prediction.get('Pending'))
        pending_tool = body.get('pending_tool', prediction.get('PendingTool', ''))
        pending = None
        if pending_flag is True:
            previews = [step for step in trace if step.get('tool') == pending_tool
                        and step.get('phase') == 'preview' and step.get('outcome') == 'pending']
            if previews:
                pending = {'tool': pending_tool}
                resolved = previews[-1].get('resolved_args')
                try:
                    params = json.loads(resolved) if isinstance(resolved, str) else resolved
                except ValueError:
                    params = None
                if isinstance(params, dict):
                    pending['params'] = params
        http_error = body.get('error') or prediction.get('HTTPError', '')
        observations.append({'id': case['id'], 'source_case_id': sid,
            'http_code': prediction.get('HTTPCode'), 'err': prediction.get('Err') or http_error,
            'http_error': http_error, 'reply': body.get('reply', prediction.get('Reply', '')),
            'trace': trace, 'trace_source': trace_source if trace else 'missing_or_empty',
            'pending': pending, 'pending_observed': pending_flag,
            'pending_tool_observed': pending_tool,
            'pending_preview_observed': body.get('pending_preview', prediction.get('PendingPreview', '')),
            'reported_tools': prediction.get('AllTools') or [], 'raw_body': raw_body,
            'import_warnings': warnings, 'assertions': {}})
    return observations


def score_cases(cases, observations, fixtures=()):
    index, fixture_index = unique_index(observations), unique_index(fixtures)
    ids = set(unique_index(cases))
    if set(index) - ids or set(fixture_index) - ids:
        raise ValueError('Unknown observation or fixture IDs refused')
    rows = [score_case(c, index.get(c['id']), fixture_index.get(c['id'])) for c in cases]
    counts = {key: {v: sum(row[key] == v for row in rows) for v in VERDICTS}
              for key in DIMENSIONS}
    return {'schema_version': 1, 'denominator': len(cases), 'counts': counts, 'rows': rows,
        'limitations': ['Posthoc AI labels are not independent gold.',
            'First tool is diagnostic only. Read-then-clarify is allowed with focused evidence.',
            'Missing truth remains unverifiable; service failures stay in denominator.',
            'Measured assertions must come from a trusted runner/extractor, never model self-report.',
            'No rate is reported as task accuracy unless all fixtures are independent and complete.']}


def export(output):
    output = Path(output).resolve()
    if output.parent != (ROOT / 'eval/contracts').resolve() or output.exists():
        raise ValueError('Export requires a new directory directly under eval/contracts')
    rows, drafts = read_json(AUDIT / 'reviews.json'), read_json(AUDIT / 'draft-cases.json')
    draft_index = unique_index(drafts)
    if set(unique_index(rows)) != set(draft_index) or len(rows) != 1220:
        raise ValueError('Expected exact 1220-case coverage')
    proof = normalization_proof(rows)
    cases = [export_case(r, draft_index[r['id']], proof) for r in rows]
    sources = [AUDIT / 'reviews.json', AUDIT / 'draft-cases.json',
               ROOT / 'web/scripts/gen-intent-suite.mjs', ROOT / 'eval/intent-suite/manifest.json']
    manifest = {'schema_version': 1, 'version': 'customer-service-contract-v1', 'count': len(cases),
        'normalization_count': len(proof), 'human_independent_review_count': 0,
        'annotation_status': 'ai_posthoc_not_independent_gold', 'fixture_count': 0,
        'sources': {str(p.relative_to(ROOT)).replace('\\', '/'): sha(p.read_bytes()) for p in sources},
        'exporter_sha256': sha(Path(__file__).read_bytes()),
        'action_counts': dict(Counter(c['expected_action'] for c in cases))}
    write_new(output / 'cases.json', cases)
    write_new(output / 'normalization-proof.json', proof)
    inputs = [{'id': c['id'], 'source_case_id': c['source_case_id'], 'text': c['text'],
               'original_text': c['original']['text']} for c in cases]
    write_new(output / 'evaluation-inputs.json', inputs)
    manifest['artifacts'] = {p.name: sha(p.read_bytes()) for p in output.glob('*.json')}
    write_new(output / 'manifest.json', manifest)
    return manifest


def verify_artifacts(output):
    output = Path(output).resolve()
    manifest = read_json(output / 'manifest.json')
    for name, digest in manifest['artifacts'].items():
        path = (output / name).resolve()
        if path.parent != output or sha(path.read_bytes()) != digest:
            raise ValueError('Artifact hash mismatch: ' + name)
    for name, digest in manifest.get('sources', {}).items():
        path = (ROOT / name).resolve()
        if ROOT not in path.parents or sha(path.read_bytes()) != digest:
            raise ValueError('Source hash mismatch: ' + name)
    if manifest.get('exporter_sha256') and sha(Path(__file__).read_bytes()) != manifest['exporter_sha256']:
        raise ValueError('Exporter changed; produce a new contract version')
    archive_path = output / 'archive-manifest.json'
    if manifest.get('archive_manifest') == 'archive-manifest.json' and not archive_path.is_file():
        raise ValueError('Required full-directory archive manifest is missing')
    if archive_path.exists():
        archive = read_json(archive_path)
        for name, digest in archive['artifacts'].items():
            path = (output / name).resolve()
            if output not in path.parents or path == archive_path or not path.is_file():
                raise ValueError('Archive entry must stay inside contract directory: ' + name)
            if sha(path.read_bytes()) != digest:
                raise ValueError('Archive artifact hash mismatch: ' + name)
        actual_files = {p.relative_to(output).as_posix() for p in output.rglob('*')
                        if p.is_file() and p != archive_path}
        if actual_files != set(archive['artifacts']):
            raise ValueError('Archive file coverage differs from contract directory')
    return manifest


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='command', required=True)
    build = sub.add_parser('export')
    build.add_argument('--output', type=Path, default=DEFAULT_OUT)
    verify = sub.add_parser('verify')
    verify.add_argument('--directory', type=Path, default=DEFAULT_OUT)
    observations = sub.add_parser('observations')
    observations.add_argument('--cases', required=True, type=Path)
    observations.add_argument('--report', required=True, type=Path)
    observations.add_argument('--output', required=True, type=Path)
    score = sub.add_parser('score')
    score.add_argument('--cases', required=True, type=Path)
    score.add_argument('--observations', required=True, type=Path)
    score.add_argument('--fixtures', type=Path)
    score.add_argument('--output', required=True, type=Path)
    args = parser.parse_args()
    if args.command == 'export':
        summary = export(args.output)
        print(json.dumps({'count': summary['count'], 'normalized': summary['normalization_count']}))
    elif args.command == 'verify':
        summary = verify_artifacts(args.directory)
        print(json.dumps({'verified': True, 'count': summary['count'], 'normalized': summary['normalization_count']}))
    elif args.command == 'observations':
        rows = import_observations(read_json(args.cases), read_report(args.report))
        report_sha = sha(args.report.read_bytes())
        for row in rows:
            row['report_evidence'] = {'path': str(args.report), 'sha256': report_sha}
        write_new(args.output, rows)
        print(json.dumps({'observations': len(rows), 'assertions_generated': 0}))
    else:
        result = score_cases(read_json(args.cases), read_json(args.observations),
                             read_json(args.fixtures) if args.fixtures else [])
        write_new(args.output, result)
        print(json.dumps({'denominator': result['denominator'], 'counts': result['counts']}))


if __name__ == '__main__':
    main()
