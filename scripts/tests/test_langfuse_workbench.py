import importlib.util, json, tempfile, unittest, copy, io
from pathlib import Path
ROOT = Path(__file__).resolve().parents[2]

def load():
    s = importlib.util.spec_from_file_location('workbench', ROOT / 'scripts/langfuse-workbench.py')
    m = importlib.util.module_from_spec(s)
    s.loader.exec_module(m)
    return m

class WorkbenchTests(unittest.TestCase):

    def test_frozen_dataset_has_1220_unique_complete_tasks_not_new_gold(self):
        m = load()
        p = m.build_plan(ROOT)
        items = p['items']
        self.assertEqual(len(items), 1220)
        self.assertEqual(len({x['id'] for x in items}), 1220)
        for x in items:
            self.assertEqual(x['metadata']['independent_review'], 'pending')
            self.assertFalse(x['metadata']['historical_truth_recovered'])
            self.assertEqual(x['metadata']['measurement_scope'], 'complete_task')
            self.assertIn('first_turn', x['expectedOutput'])
            self.assertIn('terminal', x['expectedOutput'])
            self.assertIn('answer_assertions', x['expectedOutput'])
            self.assertTrue(x['input']['turns'])
        self.assertEqual(p['model_calls'], 0)
        self.assertEqual(p['jev_calls'], 0)
        self.assertEqual(len(p['trace_bindings']), 8)

    def test_stable_plan_and_prompt_exactly_matches_base_system(self):
        m = load()
        p = m.build_plan(ROOT)
        self.assertEqual(m.encoded(p), m.encoded(m.build_plan(ROOT)))
        prompt = p['prompt']
        text = prompt['prompt'][0]['content']
        self.assertEqual(prompt['name'], 'customer-service/system/' + m.sha(text.encode()))
        self.assertEqual(len(prompt['prompt']), 1)
        self.assertNotIn('production', prompt['labels'])
        self.assertEqual(prompt['config']['scope'], 'base_system_only')

    def test_dataset_and_widget_scope_never_mix_stub_and_real(self):
        p = load().build_plan(ROOT)
        for w in p['widgets']:
            self.assertIn('本机替身', w['name'])
            self.assertTrue(any((f['column'] == 'traceRelease' and f['value'] == p['dashboard_release'] for f in w['filters'])))
            self.assertTrue(any((f.get('key') == 'evidence_kind' and f['value'] == 'local_stub' for f in w['filters'])))
        self.assertTrue(all((x['metadata']['annotation_status'] == 'ai_posthoc_not_independent_gold' for x in p['items'])))

    def test_conflicting_remote_resource_is_never_overwritten(self):
        m = load()
        with self.assertRaises(ValueError):
            m.existing_or_create([{'id': 'old', 'name': 'same', 'description': 'different'}], {'name': 'same', 'description': 'expected'}, lambda body: self.fail('must not POST'))

    def test_publisher_is_idempotent_and_missing_traces_write_nothing(self):
        m = load()
        p = m.build_plan(ROOT)
        p['items'] = p['items'][:2]
        client = FakeClient(p)
        out = io.StringIO()
        m.publish(p, client, out)
        count = len(client.posts)
        self.assertGreater(count, 0)
        m.publish(p, client, io.StringIO())
        self.assertEqual(len(client.posts), count)
        bad = FakeClient(p)
        bad.bad_trace = True
        with self.assertRaises(ValueError):
            m.publish(p, bad, io.StringIO())
        self.assertEqual(bad.posts, [])

    def test_dashboard_global_filter_cannot_hide_denominator(self):
        m = load()
        p = m.build_plan(ROOT)
        p['items'] = p['items'][:1]
        c = FakeClient(p)
        m.publish(p, c, io.StringIO())
        c.tables['/api/public/unstable/dashboards'][0]['filters'] = [{'column': 'traceId', 'type': 'stringOptions', 'operator': 'none of', 'value': list(p['trace_bindings'])}]
        with self.assertRaises(ValueError):
            m.verify(p, c)

    def test_readback_detects_tampered_item(self):
        m = load()
        p = m.build_plan(ROOT)
        p['items'] = p['items'][:1]
        c = FakeClient(p)
        m.publish(p, c, io.StringIO())
        c.tables['/api/public/dataset-items'][0]['expectedOutput'] = {}
        with self.assertRaises(ValueError):
            m.verify(p, c)

    def test_pagination_collects_all_pages_and_keeps_filter(self):
        m = load()
        calls = []

        class Pages:

            def request(self, path):
                from urllib.parse import urlsplit, parse_qs
                q = parse_qs(urlsplit(path).query)
                calls.append(q)
                page = int(q['page'][0])
                return {'data': [page], 'meta': {'totalPages': 3}}
        self.assertEqual(m.list_all(Pages(), '/api/public/dataset-items', {'datasetName': m.NAME}), [1, 2, 3])
        self.assertEqual(len(calls), 3)
        self.assertTrue(all((x['datasetName'] == [m.NAME] for x in calls)))

    def test_ambiguous_write_is_journaled_and_explicit_resume_does_not_duplicate(self):
        m = load()
        p = m.build_plan(ROOT)
        p['items'] = p['items'][:2]

        class Broken(FakeClient):

            def request(self, path, body=None):
                result = super().request(path, body)
                if body is not None and path == '/api/public/dataset-items' and (not getattr(self, 'failed', False)):
                    self.failed = True
                    raise ValueError('connection lost after acceptance')
                return result
        c = Broken(p)
        receipt = io.StringIO()
        with self.assertRaises(ValueError):
            m.publish(p, c, receipt)
        events = [json.loads(x) for x in receipt.getvalue().splitlines()]
        self.assertEqual(events[-1]['kind'], 'write_start')
        self.assertEqual(len(c.tables['/api/public/dataset-items']), 1)
        m.publish(p, c, io.StringIO())
        self.assertEqual(len(c.tables['/api/public/dataset-items']), 2)
        self.assertEqual(sum((path == '/api/public/dataset-items' for path, body in c.posts)), 2)

    def test_frozen_report_tampering_is_rejected(self):
        import shutil
        m = load()
        (ROOT / '.tmp').mkdir(exist_ok=True)
        with tempfile.TemporaryDirectory(dir=ROOT / '.tmp') as tmp:
            root = Path(tmp)
            shutil.copytree(ROOT / m.base.ARCHIVE, root / m.base.ARCHIVE)
            shutil.copytree(ROOT / m.ARCHIVE, root / m.ARCHIVE)
            (root / m.ARCHIVE / 'report-1.json').write_bytes(b'{}')
            with self.assertRaisesRegex(ValueError, 'Frozen report'):
                m.build_plan(root)

    def test_repeated_contract_additional_trace_stops_before_writes(self):
        m = load()
        p = m.build_plan(ROOT)

        class Repeated(FakeClient):

            def request(self, path, body=None):
                result = super().request(path, body)
                if path.startswith('/api/public/v2/observations?'):
                    result['data'].append({'traceId': 'f' * 32})
                return result
        c = Repeated(p)
        with self.assertRaisesRegex(ValueError, 'additional traces'):
            m.publish(p, c, io.StringIO())
        self.assertEqual(c.posts, [])

    def test_empty_quality_chart_is_not_a_verified_dashboard(self):
        m = load()
        p = m.build_plan(ROOT)

        class Empty(FakeClient):

            def request(self, path, body=None):
                if path.startswith('/api/public/v2/metrics?'):
                    return {'data': []}
                return super().request(path, body)
        with self.assertRaisesRegex(ValueError, 'Widget data differs'):
            m.verify_widget_queries(p, Empty(p))

class FakeClient:

    def __init__(self, p):
        self.p = p
        self.posts = []
        self.tables = {}
        self.bad_trace = False

    def validate_traces(self, p):
        if self.bad_trace:
            raise ValueError('missing trace')
        return []

    def verify_scores(self, s):
        return None

    def observations(self, trace):
        return [{'name': 'evaluation.task', 'metadata': self.p['dashboard_scope'][trace]}]

    def request(self, path, body=None):
        from urllib.parse import urlsplit, parse_qs
        u = urlsplit(path)
        base = u.path
        q = parse_qs(u.query)
        if body is None:
            if base == '/api/public/v2/observations':
                return {'data': [{'traceId': t} for t in self.p['trace_bindings']], 'meta': {}}
            if base == '/api/public/v2/metrics':
                query = json.loads(q['query'][0])
                arm = next((x['value'] for x in query['filters'] if x.get('key') == 'arm'))
                if query['view'] == 'scores-categorical':
                    rows = [{'stringValue': 'unknown', 'count_count': sum((x['arm'] == arm for x in self.p['trace_bindings'].values()))}]
                elif query['metrics'][0]['measure'] == 'count':
                    rows = [{'name': 'llm.attempt', 'count_count': self.p['expected_model_counts'][arm]}]
                else:
                    rows = [{'name': 'evaluation.task', 'p50_latency': 1000, 'p95_latency': 1200}]
                return {'data': rows}
            if base.endswith('/placements'):
                raise ValueError('API has no GET placements')
            if base.startswith('/api/public/unstable/dashboards/'):
                ident = base.rsplit('/', 1)[1]
                return copy.deepcopy(next((x for x in self.tables['/api/public/unstable/dashboards'] if x['id'] == ident)))
            if base.startswith('/api/public/v2/prompts/'):
                name = __import__('urllib.parse', fromlist=['unquote']).unquote(base.split('/prompts/', 1)[1])
                rows = self.tables.get('/api/public/v2/prompts', [])
                return next((x for x in rows if x['name'] == name))
            return {'data': copy.deepcopy(self.tables.get(base, [])), 'meta': {'totalPages': 1}}
        self.posts.append((base, copy.deepcopy(body)))
        row = {**copy.deepcopy(body), 'id': body.get('id', str(len(self.posts))), 'version': 1}
        self.tables.setdefault(base, []).append(row)
        if base.endswith('/placements'):
            ident = base.split('/dashboards/', 1)[1].split('/')[0]
            dash = next((x for x in self.tables['/api/public/unstable/dashboards'] if x['id'] == ident))
            dash.setdefault('definition', {'widgets': []})['widgets'].append(row)
        return copy.deepcopy(row)
if __name__ == '__main__':
    unittest.main()
