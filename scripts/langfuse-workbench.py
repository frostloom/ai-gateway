"""Zero-inference local Langfuse workbench. Plan, explicit publish, or read-only verify."""
import argparse
import importlib.util
import json
import os
import re
import sys
import urllib.parse
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
ARCHIVE = Path('eval/paired-pilots/2026-10-15-langfuse-content-scores-v1')
NAME = '客服开发任务-1220-v1'
DASH = '客服本机替身验收-v1（非真实模型成绩）'


def module(name, file):
    spec = importlib.util.spec_from_file_location(name, file)
    value = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(value)
    return value


legacy = module('workbench_scores', ROOT / 'scripts/langfuse-eval.py')
base = module('workbench_definitions', ROOT / 'scripts/prepare-full-customer-pilot.py')
encoded, sha = legacy.encoded, legacy.sha


def categories(name, labels, description):
    return {'name': name, 'dataType': 'CATEGORICAL',
            'categories': [{'label': label, 'value': i} for i, label in enumerate(labels)],
            'description': description}


def build_plan(root):
    rows = base.load_definitions(root)
    selected = base.select_primary(rows)
    if len(selected) != 1220:
        raise ValueError('Exactly 1220 frozen sources required')
    manifest = json.loads((root / ARCHIVE / 'manifest.json').read_bytes())
    # These are immutable local-stub reports, not reconstructed model outputs.
    digests = manifest.get('files', {})
    reports = []
    for i in range(1, 5):
        name = f'report-{i}.json'
        raw = (root / ARCHIVE / name).read_bytes()
        if digests.get(name) != sha(raw):
            raise ValueError('Frozen report differs from its manifest')
        reports.append(json.loads(raw))
    scores = legacy.build_plan(reports)
    items = []
    definitions_sha = sha((root / base.ARCHIVE / 'definitions.json').read_bytes())
    for row, scenario in selected:
        key = row['source_case_id'] + '/' + scenario['id']
        items.append({'id': sha(('customer-workbench-v1:' + key).encode()), 'datasetName': NAME,
            'input': {'question': row['evaluation_text'], 'turns': scenario['turns'], 'profile': scenario['profile']},
            'expectedOutput': {'first_turn': row['first_turn'], 'answer_assertions': row['answer_assertions'],
                               'terminal': scenario['terminal'], 'invariants': scenario['invariants']},
            'metadata': {'case_key': key, 'source_case_id': row['source_case_id'],
                'scenario_id': scenario['id'], 'measurement_scope': 'complete_task',
                'partition': row['partition'], 'independent_review': 'pending',
                'annotation_status': row['annotation_status'], 'historical_truth_recovered': False,
                'execution_status': 'defined_not_run', 'definitions_sha256': definitions_sha,
                'scenario_sha256': sha(encoded(scenario)), 'necessary_clarification_is_not_failure': True},
            'status': 'ACTIVE'})
    raw_prompt = (root / 'internal/agent/prompt.go').read_text(encoding='utf-8-sig')
    match = re.search(r'const systemPrompt = `([^`]*)`', raw_prompt)
    if match is None:
        raise ValueError('Unsupported Go prompt representation')
    text = match[1].replace('\r\n', '\n')
    prompt_sha = sha(text.encode())
    prompt = {'name': 'customer-service/system/' + prompt_sha, 'type': 'chat',
        'prompt': [{'role': 'system', 'content': text}],
        'labels': ['snapshot-' + prompt_sha[:12]], 'tags': ['code-snapshot', 'not-production-deployment'],
        'config': {'scope': 'base_system_only', 'sha256': prompt_sha,
                   'dynamic_context': 'JEV hints, pending preview, history and tools are assembled in application code.'},
        'commitMessage': 'Archive exact code base-system prompt; no inference, no production label.'}
    human = [categories('review_task_outcome_v1', ['pass', 'fail', 'unknown'],
                        '完整多轮任务是否满足期望终态；未审核或证据不足选 unknown，不把必要追问判错。'),
             categories('review_answer_facts_v1', ['pass', 'fail', 'unknown', 'not_applicable'],
                        '基于工具快照和合成账户审核事实；局部事实匹配不代表整段回答正确。'),
             categories('review_clarification_v1', ['necessary', 'unnecessary', 'missing', 'unknown', 'not_applicable'],
                        '审核缺失字段和歧义；必要反问属于正确行为，避免重复询问已给参数。')]
    rule_categories = {
        'evaluation_status': ['passed', 'failed', 'setup_failed', 'needs_review', 'not_started', 'interrupted_unknown'],
        'task_success': ['passed', 'unknown', 'not_established'],
        'answer_review_status': ['reviewed_pass', 'pending_independent_review', 'not_established'],
        'fact_check_status': ['mismatch', 'partial', 'supported_claims_match', 'not_evaluated'],
        'necessary_clarification_status': ['not_required', 'not_observed', 'reviewed', 'pending_independent_review'],
    }
    configs = human + [categories(name, rule_categories[name],
                                  '现有确定性评分口径；本机替身验证，不是模型准确率。')
                      for name in sorted({x['name'] for x in scores['scores'] if x['dataType'] == 'CATEGORICAL'})]
    configs += [{'name': 'structural_pass', 'dataType': 'BOOLEAN', 'description': '执行结构检查；不等于语义正确。'},
                {'name': 'http_failures', 'dataType': 'NUMERIC', 'minValue': 0, 'description': '记录的 HTTP 失败次数。'}]
    scope_raw = (root / 'eval/langfuse-workbench/2026-10-15-v1/trace-scope.json').read_bytes()
    if sha(scope_raw) != '51e758dee962a2c3261bc4a86036c4aedaae3e7798197c6899a27b120b6f4e3a':
        raise ValueError('Frozen dashboard scope digest differs')
    scope = json.loads(scope_raw)
    if set(scope) != set(scores['trace_bindings']) or any(
            not matching(scope[t], b) for t, b in scores['trace_bindings'].items()):
        raise ValueError('Frozen dashboard scope differs from report bindings')
    run_ids = sorted({x['run_id'] for x in scope.values()})
    if len(run_ids) != 4 or any(not re.fullmatch('[a-f0-9]{64}', x) for x in run_ids):
        raise ValueError('Four fixed local run identifiers required')
    release = 'langfuse-content-scores-final-2026-10-15'
    common = [
        {'column': 'traceRelease', 'type': 'string', 'operator': '=', 'value': release},
        {'column': 'metadata', 'type': 'stringObject', 'key': 'evidence_kind', 'operator': '=', 'value': 'local_stub'},
    ]
    widgets = []
    for arm, title in [('model', 'V1 模型'), ('jev_selective', 'V2 JEV 分流')]:
        scoped = common + [{'column': 'metadata', 'type': 'stringObject', 'key': 'arm', 'operator': '=', 'value': arm}]
        widgets.append({'name': f'本机替身 {title} 任务审核状态-v1', 'view': 'scores-categorical',
            'dimensions': [{'field': 'stringValue'}], 'metrics': [{'measure': 'count', 'agg': 'count'}],
            'filters': scoped + [{'column': 'name', 'type': 'string', 'operator': '=', 'value': 'task_success'}],
            'chartType': 'HORIZONTAL_BAR', 'description': 'unknown 为待审核；不是失败，更不是成功率。仅固定8臂。'})
        for metric, metrics, name in [
            ('evaluation.task', [{'measure': 'latency', 'agg': 'p50'}, {'measure': 'latency', 'agg': 'p95'}], '任务跨度耗时毫秒'),
            ('llm.attempt', [{'measure': 'count', 'agg': 'count'}], '本机假模型调用数')]:
            widgets.append({'name': f'本机替身 {title} {name}-v1', 'view': 'observations',
                'dimensions': [{'field': 'name'}], 'metrics': metrics,
                'filters': scoped + [{'column': 'name', 'type': 'string', 'operator': '=', 'value': metric}],
                'chartType': 'PIVOT_TABLE', 'description': '工程验证数据，不能证明真实延迟或节省；任务跨度含初始化及状态检查。'})
    return {'schema': 'langfuse-workbench-plan.v1', 'model_calls': 0, 'jev_calls': 0,
        'dataset': {'name': NAME, 'description': '1220个冻结开发主任务；含合理首轮行为与完整终态。AI事后标注，尚非独立金标；未执行。',
                    'metadata': {'definitions_sha256': definitions_sha, 'independent_gold': False, 'source_cases': 1220}},
        'items': items, 'prompt': prompt, 'score_configs': configs,
        'queue': {'name': '本机替身完整客服任务独立审核-v1',
                  'description': '8个本机替身任务臂，展开 agent.turn 查看完整问答；按案例ID对照1220开发集定义。local_stub不可当真实模型质量。无自动裁判。'},
        'human_config_names': [x['name'] for x in human], 'widgets': widgets,
        'dashboard': {'name': DASH, 'filters': [], 'description': '仅固定8条local_stub追踪，非真实模型成绩。任务成功待审核；无真实成本/节省证明。费用缺渠道价格与完整用量，保持unknown，不配置默认价格。'},
        'trace_bindings': scores['trace_bindings'], 'scores': scores['scores'],
        'source_bindings': {'definitions_sha256': definitions_sha, 'reports_manifest_sha256': sha((root / ARCHIVE / 'manifest.json').read_bytes()),
                            'prompt_sha256': prompt_sha, 'trace_scope_sha256': sha(scope_raw)},
        'dashboard_scope': scope, 'dashboard_release': release,
        'expected_model_counts': {arm: sum(row['telemetry']['llm_calls'] for report in reports
            for row in report['rows'] if row['arm'] == arm) for arm in ('model', 'jev_selective')},
        'limits': ['Development definitions are not independent gold.', 'No new execution or historical account truth.',
                   'No LLM connection, automatic judge, production prompt switch, pricing assumption or inference.']}


def list_all(client, path, params=None):
    rows = []
    for page in range(1, 101):
        response = client.request(path + '?' + urllib.parse.urlencode({**(params or {}), 'page': page, 'limit': 50}))
        batch = response['data']
        if not isinstance(batch, list):
            raise ValueError('Unexpected pagination')
        rows.extend(batch)
        total = response.get('meta', {}).get('totalPages')
        if total is not None and page >= total or total is None and len(batch) < 50:
            return rows
    raise ValueError('Resource listing exceeds local bound')


def matching(got, expected):
    return all(got.get(k) == v for k, v in expected.items())


def existing_or_create(rows, body, create):
    found = [x for x in rows if x.get('name') == body['name']]
    if len(found) > 1 or found and not matching(found[0], body):
        raise ValueError('Existing versioned resource conflicts: ' + body['name'])
    return found[0] if found else create(body)


def validate_scope(plan, client):
    client.validate_traces(plan)
    client.verify_scores(plan['scores'])
    for trace, expected in plan['dashboard_scope'].items():
        roots = [x for x in client.observations(trace) if x['name'] == 'evaluation.task']
        if len(roots) != 1 or not matching(roots[0].get('metadata', {}),
                {k: expected[k] for k in ('run_id', 'case_key', 'arm', 'evidence_kind')}):
            raise ValueError('Dashboard run association differs')
    # A contract hash is not a run-instance ID. Enumerate the entire release,
    # including unrelated observations, so repeats cannot silently change N.
    filters = [{'column': 'metadata', 'type': 'stringObject', 'key': 'attributes.langfuse.release',
                'operator': '=', 'value': plan['dashboard_release']}]
    listed = client.request('/api/public/v2/observations?' + urllib.parse.urlencode({
        'filter': json.dumps(filters), 'limit': 1000, 'fields': 'core,basic,metadata'}))
    if listed.get('meta', {}).get('cursor') or {x['traceId'] for x in listed['data']} != set(plan['trace_bindings']):
        raise ValueError('Dashboard release contains missing or additional traces')


def verify_widget_queries(plan, client):
    results = []
    for widget in plan['widgets']:
        query = {'view': widget['view'], 'dimensions': widget['dimensions'],
                 'metrics': [{'measure': x['measure'], 'aggregation': x['agg']} for x in widget['metrics']],
                 'filters': widget['filters'], 'fromTimestamp': '2026-10-01T00:00:00Z',
                 'toTimestamp': '2026-11-01T00:00:00Z'}
        rows = client.request('/api/public/v2/metrics?' + urllib.parse.urlencode({'query': json.dumps(query)}))['data']
        arm = next(x['value'] for x in widget['filters'] if x.get('key') == 'arm')
        if widget['view'] == 'scores-categorical':
            expected = sum(x['arm'] == arm for x in plan['trace_bindings'].values())
            valid = rows == [{'stringValue': 'unknown', 'count_count': expected}]
        elif widget['metrics'][0]['measure'] == 'count':
            valid = rows == [{'name': 'llm.attempt', 'count_count': plan['expected_model_counts'][arm]}]
        else:
            valid = len(rows) == 1 and rows[0].get('name') == 'evaluation.task' and all(
                isinstance(rows[0].get(k), (float, int)) and rows[0][k] >= 0 for k in ('p50_latency', 'p95_latency'))
        if not valid:
            raise ValueError('Widget data differs from frozen local evidence: ' + widget['name'])
        results.append({'name': widget['name'], 'data': rows})
    return results


def publish(plan, client, receipt):
    # Validate every existing report association before the first write.
    validate_scope(plan, client)
    def record(value):
        receipt.write(json.dumps(value, ensure_ascii=False, sort_keys=True) + '\n')
        receipt.flush()
        try:
            os.fsync(receipt.fileno())
        except (AttributeError, OSError):
            pass
    def post(path, body):
        record({'kind': 'write_start', 'path': path, 'id': body.get('id'), 'name': body.get('name')})
        result = client.request(path, body)
        record({'kind': 'write_accepted', 'path': path, 'id': result.get('id'), 'name': body.get('name')})
        return result
    record({'kind': 'plan', 'sha256': sha(encoded(plan)), 'model_calls': 0, 'jev_calls': 0})
    existing_or_create(list_all(client, '/api/public/v2/datasets'), plan['dataset'], lambda b: post('/api/public/v2/datasets', b))
    current = {x['id']: x for x in list_all(client, '/api/public/dataset-items', {'datasetName': NAME})}
    for item in plan['items']:
        if item['id'] in current:
            if not matching(current[item['id']], item):
                raise ValueError('Dataset item differs; never overwrite archived definitions')
        else:
            post('/api/public/dataset-items', item)
    config_rows = list_all(client, '/api/public/score-configs')
    ids = {}
    for config in plan['score_configs']:
        row = existing_or_create(config_rows, config, lambda b: post('/api/public/score-configs', b))
        ids[config['name']] = row['id']
    queue_body = {**plan['queue'], 'scoreConfigIds': [ids[n] for n in plan['human_config_names']]}
    queue = existing_or_create(list_all(client, '/api/public/annotation-queues'), queue_body, lambda b: post('/api/public/annotation-queues', b))
    queue_path = '/api/public/annotation-queues/' + queue['id'] + '/items'
    queued = {(x['objectId'], x['objectType']) for x in list_all(client, queue_path)}
    for trace in plan['trace_bindings']:
        if (trace, 'TRACE') not in queued:
            post(queue_path, {'objectId': trace, 'objectType': 'TRACE'})
    prompts = list_all(client, '/api/public/v2/prompts', {'name': plan['prompt']['name']})
    if any(x['name'] == plan['prompt']['name'] for x in prompts):
        saved = client.request('/api/public/v2/prompts/' + urllib.parse.quote(plan['prompt']['name'], safe='') + '?version=1')
        if not matching(saved, {k: plan['prompt'][k] for k in ('name', 'type', 'prompt', 'config')}):
            raise ValueError('Content-addressed prompt version conflicts')
    else:
        saved = post('/api/public/v2/prompts', plan['prompt'])
        if saved['version'] != 1:
            raise ValueError('Content-addressed prompt must be version 1')
    widget_rows = list_all(client, '/api/public/unstable/dashboard-widgets')
    widgets = [existing_or_create(widget_rows, w, lambda b: post('/api/public/unstable/dashboard-widgets', b)) for w in plan['widgets']]
    dashboard = existing_or_create(list_all(client, '/api/public/unstable/dashboards'), plan['dashboard'], lambda b: post('/api/public/unstable/dashboards', b))
    placement_path = '/api/public/unstable/dashboards/' + dashboard['id'] + '/placements'
    placed = {x.get('widgetId') for x in client.request('/api/public/unstable/dashboards/' + dashboard['id']).get('definition', {}).get('widgets', [])}
    for i, w in enumerate(widgets):
        if w['id'] not in placed:
            post(placement_path, {'type': 'widget', 'widgetId': w['id'], 'x': 6*(i % 2), 'y': 6*(i//2), 'width': 6, 'height': 6})
    result = verify(plan, client)
    record(result)
    return result


def verify(plan, client):
    validate_scope(plan, client)
    def require(path, body):
        found = [x for x in list_all(client, path) if x.get('name') == body['name']]
        if len(found) != 1 or not matching(found[0], body):
            raise ValueError('Resource readback differs: ' + body['name'])
        return found[0]
    dataset = require('/api/public/v2/datasets', plan['dataset'])
    items = list_all(client, '/api/public/dataset-items', {'datasetName': NAME})
    actual = {x['id']: x for x in items}
    if len(actual) != len(plan['items']) or any(not matching(actual.get(x['id'], {}), x) for x in plan['items']):
        raise ValueError('Dataset item count or content differs')
    ids = {x['name']: require('/api/public/score-configs', x)['id'] for x in plan['score_configs']}
    queue = require('/api/public/annotation-queues', {**plan['queue'], 'scoreConfigIds': [ids[n] for n in plan['human_config_names']]})
    queued = list_all(client, '/api/public/annotation-queues/' + queue['id'] + '/items')
    if {(x['objectId'], x['objectType']) for x in queued} != {(t, 'TRACE') for t in plan['trace_bindings']}:
        raise ValueError('Queue coverage differs')
    prompt = client.request('/api/public/v2/prompts/' + urllib.parse.quote(plan['prompt']['name'], safe='') + '?version=1')
    if not matching(prompt, {k: plan['prompt'][k] for k in ('name', 'type', 'prompt', 'config')}) or prompt.get('version') != 1:
        raise ValueError('Prompt readback differs')
    widget_ids = [require('/api/public/unstable/dashboard-widgets', w)['id'] for w in plan['widgets']]
    dashboard = require('/api/public/unstable/dashboards', plan['dashboard'])
    placements = client.request('/api/public/unstable/dashboards/' + dashboard['id']).get('definition', {}).get('widgets', [])
    if {x.get('widgetId') for x in placements} != set(widget_ids):
        raise ValueError('Dashboard placement differs')
    widget_data = verify_widget_queries(plan, client)
    return {'kind': 'verified', 'plan_sha256': sha(encoded(plan)), 'dataset_id': dataset['id'],
            'dataset_name': NAME, 'dataset_items': len(items), 'annotation_queue_id': queue['id'], 'queued_tasks': len(queued),
            'score_config_count': len(ids), 'existing_rule_scores': len(plan['scores']), 'prompt_name': prompt['name'],
            'prompt_version': 1, 'dashboard_id': dashboard['id'], 'widget_count': len(widget_ids),
            'widget_data': widget_data, 'model_calls': 0, 'jev_calls': 0}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=['plan', 'publish', 'verify'])
    parser.add_argument('--plan', required=True)
    parser.add_argument('--out')
    parser.add_argument('--env-file', action='append', default=[])
    parser.add_argument('--execute', action='store_true')
    args = parser.parse_args(argv)
    try:
        plan = build_plan(ROOT)
        if args.mode == 'plan':
            with Path(args.plan).open('xb') as f:
                f.write(encoded(plan))
        else:
            if Path(args.plan).read_bytes() != encoded(plan):
                raise ValueError('Plan differs from frozen inputs/current prompt')
            if not args.out or Path(args.out).exists() or args.mode == 'publish' and not args.execute:
                raise ValueError('New receipt path and explicit --execute for publish required')
            config = dict(os.environ)
            for p in args.env_file:
                for line in Path(p).read_text(encoding='utf-8-sig').splitlines():
                    if line.strip() and not line.lstrip().startswith('#') and '=' in line:
                        k, v = line.split('=', 1)
                        config[k.strip()] = v.strip().strip('"').strip("'")
            client = legacy.Client(config)
            with Path(args.out).open('x', encoding='utf-8', newline='\n') as output:
                if args.mode == 'publish':
                    publish(plan, client, output)
                else:
                    output.write(encoded(verify(plan, client)).decode())
        print('PASS: local workbench; external model/JEV calls = 0')
        return 0
    except (ValueError, OSError, KeyError, TypeError) as err:
        # Only controlled validation errors are shown; network errors stay sanitized in Client.
        print('Workbench stopped: ' + str(err), file=sys.stderr)
        return 2


if __name__ == '__main__':
    sys.exit(main())
