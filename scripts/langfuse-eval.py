"""Offline score plans or explicit local Langfuse synchronization. Never runs inference."""
import argparse
import importlib.util
import base64
import hashlib
import json
import os
import re
import sys
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path


def encoded(value):
    return (json.dumps(value, ensure_ascii=False, sort_keys=True, indent=2) + '\n').encode('utf-8')


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def build_plan(reports):
    scores, unlinked, traces = [], [], {}
    for report in reports:
        if report.get('schema') != 'small-paired-report.v1' or report.get('evidence_kind') not in ('local_stub', 'recorded_run') or not re.fullmatch('[a-f0-9]{64}', report.get('journal_sha256', '')):
            raise ValueError('A trusted paired report with journal binding is required')
        for row in report['rows']:
            status = row['status']
            if status not in ('passed', 'failed', 'setup_failed', 'needs_review', 'not_started', 'interrupted_unknown'):
                raise ValueError('Unknown evaluation status')
            trace = row.get('langfuse_trace_id')
            if not trace:
                unlinked.append({'key': row['key'], 'arm': row['arm'], 'status': status})
                continue
            if not re.fullmatch('[a-f0-9]{32}', trace) or trace == '0' * 32 or trace in traces or row['arm'] not in ('model', 'jev_selective'):
                raise ValueError('Invalid or duplicate trace association')
            if status in ('not_started', 'interrupted_unknown'):
                raise ValueError('Unfinished task must not receive completed-task scores')
            observed_turns = row.get('observed_turns')
            if type(observed_turns) is not int or observed_turns < 0 or status in ('passed', 'needs_review') and observed_turns == 0:
                raise ValueError('Report with actual observed turn count required; regenerate offline from journal')
            binding = {'case_key': row['key'], 'arm': row['arm'], 'evidence_kind': report['evidence_kind'],
                       'expected_turns': observed_turns, 'require_answers': status in ('passed', 'needs_review')}
            traces[trace] = binding
            facts = [x['status'] for x in row.get('fact_checks', [])]
            if any(x not in ('match', 'mismatch', 'not_evaluated') for x in facts):
                raise ValueError('Unknown fact-check status')
            fact = ('mismatch' if 'mismatch' in facts else 'partial' if 'match' in facts and 'not_evaluated' in facts
                    else 'supported_claims_match' if facts and all(x == 'match' for x in facts) else 'not_evaluated')
            clarifications = row.get('necessary_clarifications', [])
            clarification = ('not_required' if not clarifications else 'not_observed' if any(x['recorded_action'] == 'not_observed' for x in clarifications)
                             else 'reviewed' if all(x['semantic_review'] == 'reviewed' for x in clarifications) else 'pending_independent_review')
            values = {'evaluation_status': ('CATEGORICAL', status),
                      'structural_pass': ('BOOLEAN', int(row['structural_pass'])),
                      'task_success': ('CATEGORICAL', 'passed' if status == 'passed' else 'unknown' if status == 'needs_review' else 'not_established'),
                      'answer_review_status': ('CATEGORICAL', 'reviewed_pass' if status == 'passed' else 'pending_independent_review' if status == 'needs_review' else 'not_established'),
                      'fact_check_status': ('CATEGORICAL', fact),
                      'necessary_clarification_status': ('CATEGORICAL', clarification),
                      'http_failures': ('NUMERIC', row['http_failures'])}
            for name, (kind, value) in values.items():
                scores.append({'id': sha(('customer-service.v1:' + trace + ':' + name).encode()),
                               'traceId': trace, 'name': name, 'value': value, 'dataType': kind,
                               'environment': 'evaluation', 'metadata': {**binding, 'journal_sha256': report['journal_sha256']},
                               'comment': 'Local deterministic assessment; supported facts are not whole-answer correctness. Independent review may remain pending.'})
    if len(scores) > 256:
        raise ValueError('At most 256 score writes per explicit batch')
    return {'schema': 'langfuse-score-plan.v1', 'model_calls': 0, 'jev_calls': 0,
            'scores': scores, 'trace_bindings': traces, 'unlinked_rows': unlinked}


def check_settings(c):
    checks = {'monitor_enabled': c.get('AGENT_LANGFUSE_ENABLED') in ('true', '1'),
              'content_enabled': c.get('AGENT_LANGFUSE_CAPTURE_CONTENT') in ('true', '1'),
              'evaluation_environment': c.get('AGENT_LANGFUSE_ENVIRONMENT') == 'evaluation',
              'full_sampling': c.get('AGENT_LANGFUSE_SAMPLE_RATE') == '1',
              'identity_hash_key_valid': len(c.get('AGENT_LANGFUSE_HASH_KEY', '').encode('utf-8')) >= 32,
              'chosen_channel': c.get('AGENT_LLM_BASE_URL', '').rstrip('/') == 'https://maas.qianwenaiapi.com/apps/anthropic',
              'chosen_model': c.get('AGENT_LLM_MODEL') == 'deepseek-v4.1-flash',
              'chosen_protocol': c.get('AGENT_LLM_PROTOCOL') == 'anthropic',
              'model_key_present': bool(c.get('AGENT_LLM_API_KEY', '').strip()),
              'jev_key_present': bool(c.get('JEV_API_KEY', '').strip()),
              'pinned_jev_model': c.get('JEV_MODEL') == 'jev-1.13.0'}
    return {'settings_valid': all(checks.values()), 'checks': checks, 'ready_for_paid_run': False,
            'model_calls': 0, 'jev_calls': 0,
            'remaining_gates': ['Small real paired acceptance', 'Independent semantic review', 'Fresh channel prices and explicit currency budgets', 'User paid-run authorization']}


def verify_bindings(directory):
    root = Path(__file__).resolve().parents[1]
    path = Path(directory)
    manifest = json.loads((path / 'manifest.json').read_text(encoding='utf-8'))
    raw = (path / 'proposal.json').read_bytes()
    if manifest.get('schema') != 'observed-preparation-manifest.v2' or manifest.get('files') != {'proposal.json': sha(raw)}:
        raise ValueError('Invalid observed preparation manifest')
    proposal = json.loads(raw)
    if proposal.get('schema') != 'full-pilot-observed-preparation.v2' or proposal['source_cases'] != 1220 or proposal['paired_arms'] != 2440:
        raise ValueError('Invalid observed preparation scope')
    bindings = proposal['source_bindings']['runtime_files_sha256']
    if not bindings or 'scripts/langfuse-eval.py' not in bindings:
        raise ValueError('Missing startup-check code binding')
    for name, digest in bindings.items():
        relative = Path(name)
        if relative.is_absolute() or '..' in relative.parts or relative.suffix not in ('.py', '.go') or sha((root / relative).read_bytes().replace(b'\r\n', b'\n')) != digest:
            raise ValueError('Runtime differs from prepared evaluation')
    if manifest['generator_sha256'] != sha((root / 'scripts/prepare-observed-customer-pilot.py').read_bytes()):
        raise ValueError('Preparation generator differs')
    spec = importlib.util.spec_from_file_location('observed_preparation_check', root / 'scripts/prepare-observed-customer-pilot.py')
    generator = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(generator)
    if raw != encoded(generator.prepare(root)):
        raise ValueError('Frozen definitions, profiles or previous preparation differ')
    return sha(raw)


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


class Client:
    def __init__(self, c):
        endpoint = urllib.parse.urlsplit(c.get('OTEL_EXPORTER_OTLP_TRACES_ENDPOINT', ''))
        if endpoint.scheme not in ('http', 'https') or endpoint.hostname not in ('localhost', '127.0.0.1') or endpoint.username or endpoint.password or endpoint.query or endpoint.fragment or endpoint.path != '/api/public/otel/v1/traces':
            raise ValueError('Explicit local Langfuse endpoint required')
        if c.get('AGENT_LANGFUSE_ENVIRONMENT') != 'evaluation':
            raise ValueError('Evaluation environment required')
        keys = [c.get('LANGFUSE_PUBLIC_KEY', ''), c.get('LANGFUSE_SECRET_KEY', '')]
        if any(not x or any(ch in x for ch in '\r\n:') for x in keys):
            raise ValueError('Independent Langfuse project credentials required')
        self.base = urllib.parse.urlunsplit((endpoint.scheme, endpoint.netloc, '', '', ''))
        self.auth = 'Basic ' + base64.b64encode(':'.join(keys).encode()).decode()
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())

    def request(self, path, body=None):
        req = urllib.request.Request(self.base + path, data=encoded(body) if body is not None else None,
                                     headers={'Authorization': self.auth, 'Content-Type': 'application/json'})
        try:
            with self.opener.open(req, timeout=10) as response:
                raw = response.read((2 << 20) + 1)
                if len(raw) > 2 << 20:
                    raise ValueError('Langfuse response exceeds local bound')
                return json.loads(raw)
        except (urllib.error.URLError, TimeoutError, OSError, json.JSONDecodeError):
            raise ValueError('Langfuse request failed; no automatic retry') from None

    def observations(self, trace):
        data = self.request('/api/public/v2/observations?' + urllib.parse.urlencode({
            'traceId': trace, 'limit': 1000, 'fields': 'core,basic,metadata,usage,io'}))
        if data.get('meta', {}).get('cursor'):
            raise ValueError('Observation batch exceeds local bound')
        return data['data']

    def validate_traces(self, plan):
        summaries = []
        for trace, binding in plan['trace_bindings'].items():
            obs = self.observations(trace)
            roots = [x for x in obs if x.get('name') == 'evaluation.task']
            if len(roots) != 1 or any(roots[0].get('metadata', {}).get(k) != binding[k] for k in ('case_key', 'arm', 'evidence_kind')):
                raise ValueError('Trace is absent or belongs to different case/arm/evidence')
            turns = [x for x in obs if x.get('name') == 'agent.turn']
            def has_content(value):
                if isinstance(value, str):
                    try:
                        value = json.loads(value)
                    except json.JSONDecodeError:
                        pass
                return bool(value)
            content_turns = sum(has_content(x.get('input')) and has_content(x.get('output')) for x in turns)
            indices = []
            for turn in turns:
                index = turn.get('metadata', {}).get('turn_index')
                if isinstance(index, str) and index.isascii() and index.isdigit() and str(int(index)) == index:
                    index = int(index)
                if type(index) is not int or index < 1:
                    raise ValueError('Invalid exported turn index')
                indices.append(index)
            if len(turns) != binding['expected_turns'] or sorted(indices) != list(range(1, len(turns) + 1)) or binding['require_answers'] and content_turns != len(turns):
                raise ValueError('Recorded turn/content coverage is incomplete')
            summaries.append({'trace_id': trace, **binding, 'turns': len(turns),
                              'content_turns': content_turns})
        return summaries

    def verify_scores(self, scores):
        for offset in range(0, len(scores), 50):
            chunk = scores[offset:offset + 50]
            response = self.request('/api/public/v3/scores?' + urllib.parse.urlencode({
                'id': ','.join(x['id'] for x in chunk), 'limit': 100, 'fields': 'subject,details'}))
            found = {x['id']: x for x in response['data']}
            for expected in chunk:
                got = found.get(expected['id'], {})
                if got.get('value') != expected['value'] or got.get('name') != expected['name'] or got.get('dataType') != expected['dataType'] or got.get('subject', {}).get('id') != expected['traceId']:
                    raise ValueError('Score readback incomplete or different; retry synchronization only, never inference')


def publish(plan, client, receipt):
    # Validate ALL associations before any score write. Append/sync intent before
    # each POST; on failure leave partial evidence and exit, never run inference.
    summaries = client.validate_traces(plan)
    def record(event):
        receipt.write(json.dumps(event, ensure_ascii=False, sort_keys=True) + '\n')
        receipt.flush()
        os.fsync(receipt.fileno())
    record({'kind': 'plan', 'sha256': sha(encoded(plan)), 'trace_summaries': summaries})
    for score in plan['scores']:
        record({'kind': 'start', 'id': score['id']})
        client.request('/api/public/scores', score)
        record({'kind': 'accepted', 'id': score['id']})
    client.verify_scores(plan['scores'])
    record({'kind': 'verified', 'score_count': len(plan['scores']), 'model_calls': 0, 'jev_calls': 0})


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=('plan', 'check', 'publish', 'verify'))
    parser.add_argument('--report', action='append', default=[])
    parser.add_argument('--env-file', action='append', default=[])
    parser.add_argument('--out', required=True)
    parser.add_argument('--execute', action='store_true')
    parser.add_argument('--preflight')
    args = parser.parse_args(argv)
    if args.mode == 'publish' and not args.execute:
        print('Explicit --execute required for local score writes', file=sys.stderr)
        return 2
    try:
        if Path(args.out).exists():
            raise ValueError('New output path required; existing evidence is immutable')
        if args.mode == 'check' and not args.preflight:
            raise ValueError('Explicit versioned preflight required')
        binding = verify_bindings(args.preflight) if args.mode == 'check' else None
        if args.mode in ('plan', 'publish', 'verify'):
            if not args.report:
                raise ValueError('Explicit trusted report paths required')
            plan = build_plan([json.loads(Path(p).read_text(encoding='utf-8')) for p in args.report])
        if args.mode != 'plan':
            config = dict(os.environ)
            for p in args.env_file:
                for line in Path(p).read_text(encoding='utf-8-sig').splitlines():
                    if line.strip() and not line.lstrip().startswith('#') and '=' in line:
                        key, value = line.split('=', 1)
                        config[key.strip()] = value.strip().strip('"').strip("'")
            client = Client(config)
        if args.mode == 'publish':
            with Path(args.out).open('x', encoding='utf-8', newline='\n') as receipt:
                publish(plan, client, receipt)
        else:
            value = plan if args.mode == 'plan' else check_settings(config)
            if args.mode == 'verify':
                summaries = client.validate_traces(plan)
                client.verify_scores(plan['scores'])
                value = {'kind': 'verified', 'score_count': len(plan['scores']),
                         'trace_summaries': summaries, 'plan_sha256': sha(encoded(plan)),
                         'model_calls': 0, 'jev_calls': 0}
            if args.mode == 'check':
                client.request('/api/public/health?failIfDatabaseUnavailable=true')
                client.request('/api/public/v3/scores?limit=1')
                value['local_langfuse_health_and_auth'] = True
                value['runtime_binding_valid'] = True
                value['proposal_sha256'] = binding
            with Path(args.out).open('xb') as output:
                output.write(encoded(value))
            if args.mode == 'check' and not value['settings_valid']:
                print('Settings check failed; see named checks, zero inference calls', file=sys.stderr)
                return 2
        print('Local Langfuse evaluation operation complete; zero model/JEV calls')
        return 0
    except (ValueError, OSError, KeyError, TypeError):
        print('Langfuse evaluation operation failed; check trusted input/configuration or partial receipt. No automatic retry.', file=sys.stderr)
        return 2


if __name__ == '__main__':
    sys.exit(main())
