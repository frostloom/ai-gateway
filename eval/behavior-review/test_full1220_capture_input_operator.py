"""Synthetic operator boundaries. No settings, actual proofs or network IO."""
import contextlib
import hashlib
import io
import json
import os
from pathlib import Path
import tempfile
import types
import unittest
from unittest.mock import patch
import urllib.request

HERE = Path(__file__).resolve().parent
EVIDENCE = HERE.parents[1] / '.tmp/v9-live-score-horizon-20261031/capture-input-v1/operator-tests'


def load(path):
    m = types.ModuleType('synthetic_operator_test'); m.__file__ = str(path)
    exec(compile(path.read_bytes(), str(path), 'exec'), m.__dict__)
    return m


class Response:
    status = 200
    def __init__(self, raw=b'{"data":[]}'): self.raw = raw
    def __enter__(self): return self
    def __exit__(self, *a): pass
    def read(self, *a): return self.raw


class Delegate:
    def __init__(self): self.urls = []
    def open(self, req, *a, **kw): self.urls.append(req.full_url); return Response()


class OperatorTests(unittest.TestCase):
    def setUp(self):
        p = HERE / 'full1220-capture-input-operator.py'
        self.assertTrue(p.exists(), 'Task3 operator missing')
        self.m = load(p)
        EVIDENCE.mkdir(mode=0o700, exist_ok=True)
        self.root = Path(tempfile.mkdtemp(prefix='synthetic-', dir=EVIDENCE))
        self.base = self.m.load_base()

    def replay(self):
        traces = [f'{i:032x}' for i in range(10)]
        rows = {self.m.observation_path(t): {'body': b'{"data":[]}', 'status': 200} for t in traces[:7]}
        delegate = Delegate()
        op = self.m.ReplayOpener(delegate, 'http://localhost:3000', traces, rows)
        return op, delegate, traces, rows

    def test_passive_no_io(self):
        with patch.object(self.m, 'load_base', side_effect=AssertionError('IO')):
            with contextlib.redirect_stdout(io.StringIO()) as out: self.assertEqual(self.m.main([]), 0)
        self.assertIn('UNFROZEN', out.getvalue())

    def test_replay_exact_seven_and_only_three_new_gets(self):
        op, delegate, traces, _ = self.replay()
        for t in reversed(traces):
            with op.open(urllib.request.Request(op.base + self.m.observation_path(t))) as r:
                self.assertEqual(r.read(2097153), b'{"data":[]}')
        op.complete()
        self.assertEqual(len(delegate.urls), 3)
        op.open(urllib.request.Request(op.base + '/api/public/v3/scores?limit=100'))
        self.assertEqual(len(delegate.urls), 4)

    def test_replay_wrong_url_repeat_missing_or_partial_rejected(self):
        for variant in ('host', 'path', 'query', 'post', 'duplicate', 'missing', 'partial'):
            with self.subTest(variant=variant):
                op, delegate, traces, _ = self.replay()
                url = op.base + self.m.observation_path(traces[0])
                if variant == 'host': url = url.replace('localhost', '127.0.0.1')
                if variant == 'path': url = url.replace('observations', 'other')
                if variant == 'query': url += '&cursor=x'
                req = urllib.request.Request(url, data=b'{}' if variant == 'post' else None)
                if variant == 'missing':
                    with self.assertRaises(ValueError): op.complete()
                elif variant == 'partial':
                    with op.open(req) as r:
                        with self.assertRaises(ValueError): r.read(1)
                    with self.assertRaises(ValueError): op.complete()
                elif variant == 'duplicate':
                    op.open(req).read(2097153)
                    with self.assertRaises(ValueError): op.open(req)
                else:
                    with self.assertRaises(ValueError): op.open(req)
                self.assertEqual(delegate.urls, [])

    def scores(self):
        raw = (self.m.D / 'historical-scores.py').read_bytes()
        s = self.base.load_scores(raw)
        s._PUBLICATION_REGISTRY_ROOT = self.root / 'registry'
        return s

    def plan(self, s, index=40):
        trace = '1' * 32
        logical = {'scope_sha256': self.m.SCOPE, 'window_id': 'synthetic-window', 'pass': 1,
                   'ordinal': 1, 'arm': 'model', 'trace_id': trace, 'metric': 'full1220.v1.original_status'}
        identity = s._json_bytes(logical).decode(); identifier = s._sha(identity.encode())
        return {'schema': 'full1220-window-score-plan.v1', 'window_index': index, 'window_id': 'synthetic-window',
                'provenance': {'scope_sha256': self.m.SCOPE, 'manifest_sha256': 'a'*64},
                'trace_bindings': {trace: dict(ordinal=1, **{'pass':1}, case_key='synthetic', arm='model',
                    recording_sha256='b'*64, expected_defined_turns=1, expected_observed_turns=1)},
                'scores': [{'id': identifier, 'logical_identity': identity, 'traceId':trace,
                            'name':logical['metric'], 'value':'failed',
                            'metadata': {'pass':1, 'ordinal':1, 'arm':'model',
                                         'original_status':'failed', 'original_structural_pass':False}}]}

    def test_every_registry_conflict_including_dangling_blocks_before_io(self):
        s = self.scores(); plan = self.plan(s)
        for n in self.m.registry_names(plan):
            for dangling in (False, True):
                with self.subTest(name=n, dangling=dangling):
                    s._PUBLICATION_REGISTRY_ROOT = self.root / ('registry-'+str(len(list(self.root.iterdir()))))
                    h = s._open_publication_registry(self.m.SCOPE, create=True)
                    try:
                        p = h.scope / n
                        if dangling: p.symlink_to(self.root / 'missing')
                        else: p.write_bytes(b'consumed')
                        with self.assertRaises(ValueError): self.m.require_empty(h, plan)
                    finally: h.close()

    def test_future_namespaces_and_legacy_sentinel_prevent_reentry(self):
        old = self.root / 'old'; new = self.root / 'new'; old.mkdir(mode=0o700); new.mkdir(mode=0o700)
        with patch.object(self.m, 'OLD_RUNS', old), patch.object(self.m, 'RUNS', new):
            out = self.m.reserve(self.base, 'normal', 41, 'a'*64)
            self.assertTrue((old/'window-000041-publish-once'/'new-entry-owned-sentinel.json').is_file())
            for mode in ('normal', 'completion'):
                with self.assertRaises(ValueError): self.m.reserve(self.base, mode, 41, 'b'*64)
            with patch.object(self.base, 'RUNS', old):
                with self.assertRaises(ValueError): self.base.reserve('publish', 41, None, 'b'*64)
            self.assertTrue(out.is_dir())
            (old/'window-000042-publish-v2-once').symlink_to(self.root/'missing')
            with self.assertRaises(ValueError): self.m.reserve(self.base, 'normal', 42, 'c'*64)

    def test_completion_rejects_alternate_legacy_operation_even_dangling(self):
        old = self.root/'old'; new = self.root/'new'; old.mkdir(mode=0o700); new.mkdir(mode=0o700)
        (old/'window-000040-publish-once').mkdir(mode=0o700)
        (old/'window-000040-publish-v2-once').symlink_to(self.root/'absent')
        with patch.object(self.m,'OLD_RUNS',old), patch.object(self.m,'RUNS',new):
            with self.assertRaises(ValueError): self.m.reserve(self.base,'completion',40,'a'*64)
        self.assertEqual(list(new.iterdir()), [])

    def run_publisher(self, uncertain=False, mismatch=False, conflict=None, tamper=False):
        s = self.scores(); plan = self.plan(s); calls = []
        binary = self.root/'synthetic-binary'; binary.write_bytes(b'synthetic')
        gate = lambda kind, data: s._PublicationProof(s._PUBLICATION_SEAL, kind,
                {'manifest_sha256':'a'*64, **data}, plan)
        chain = gate('receipt_chain', {'chain_sha256':'c'*64})
        signed = gate('attestation', {'binary_path':str(binary), 'binary_sha256':s._sha(b'synthetic'), 'attestation_sha256':'d'*64})
        projection = s._ContentProjection(s._PROJECTION_SEAL, b'{}', plan, {})
        class Inner:
            def request(inner, path, score):
                calls.append(score['id'])
                if uncertain: raise TimeoutError('synthetic uncertainty')
                return {'id':score['id']}
            def verify_scores(inner, rows): pass
        client = s._WindowScoreClient(s._PUBLICATION_SEAL, Inner(), 'a'*64)
        self.m.install_publication_guards(s, plan, lambda: None)
        if tamper:
            guarded = s._publication_event
            def event(handle, name, value):
                guarded(handle, name, value)
                if name.endswith('.sent.json'):
                    (handle.scope/'window-000040.batch.json').write_bytes(b'changed-before-POST')
            s._publication_event = event
        # Content fixture is isolated; actual proof factory is never mocked/minted.
        def validate(*a):
            if mismatch: raise ValueError('Captured turn input mismatch')
            return {'all_pass':True}
        with patch.object(s, 'validate_window_traces', side_effect=validate):
            if conflict:
                h = s._open_publication_registry(self.m.SCOPE, create=True); (h.scope/conflict).symlink_to(self.root/'missing'); h.close()
            try: result = s._publish_window_scores(plan, projection, chain, signed, client, execute=True)
            except ValueError: result = None
            with self.assertRaises(ValueError): s._publish_window_scores(plan, projection, chain, signed, client, execute=True)
        return result, calls, s, plan

    def test_real_publisher_order_and_uncertain_post_never_resends(self):
        result, calls, s, plan = self.run_publisher(uncertain=True)
        self.assertIsNone(result); self.assertEqual(len(calls), 1)
        scope = s._PUBLICATION_REGISTRY_ROOT / self.m.SCOPE; identifier = plan['scores'][0]['id']
        self.assertTrue((scope/f'{identifier}.sent.json').exists())
        self.assertFalse((scope/f'{identifier}.accepted.json').exists())
        self.assertTrue((scope/'window-000040.batch.json').exists())

    def test_publisher_success_and_content_mismatch(self):
        result, calls, _, _ = self.run_publisher()
        self.assertEqual(result['acknowledged'], 1); self.assertEqual(len(calls), 1)

    def test_content_mismatch_has_no_durable_intents_or_post(self):
        result, calls, s, plan = self.run_publisher(mismatch=True)
        self.assertIsNone(result); self.assertEqual(calls, [])
        scope = s._PUBLICATION_REGISTRY_ROOT/self.m.SCOPE
        self.assertEqual([p.name for p in scope.iterdir()], ['scope.json'])

    def test_immediately_before_post_rechecks_owned_batch_and_intents(self):
        result, calls, _, _ = self.run_publisher(tamper=True)
        self.assertIsNone(result); self.assertEqual(calls, [])

    def test_second_publisher_lock_conflict_blocks_observation_reads(self):
        s = self.scores(); plan = self.plan(s)
        h = s._open_publication_registry(self.m.SCOPE, create=True)
        try:
            self.m.require_empty(h, plan)
            (h.scope/('logical-'+plan['scores'][0]['id']+'.intent.json')).symlink_to(self.root/'absent')
        finally: h.close()
        self.m.install_publication_guards(s, plan, lambda: None)
        with self.assertRaises(ValueError): s._open_publication_registry(self.m.SCOPE, create=True)

    def completion_fixture(self, variant=None):
        d = self.root/'original'; d.mkdir(mode=0o700)
        (d/'get-evidence').mkdir(mode=0o700)
        traces = [f'{i:032x}' for i in range(10)]
        plan = {'window_index':40, 'provenance':{'summary_sha256':self.m.SUMMARY,'scope_sha256':self.m.SCOPE},
                'trace_bindings':{t:{} for t in traces}, 'scores':[{'id':f'{i:064x}'} for i in range(28)]}
        values = {'plan.json': self.base.wire(plan), 'operation-error.json': self.base.wire({
            'error_type':'ValueError','mode':'publish','next_action':'verify-only','publish_reentry_forbidden':True}),
            'publish-api-intent.json':self.base.wire({'may_have_sent_POST':True,'plan_sha256':self.m.sha(self.base.wire(plan).rstrip(b'\n'))})}
        for i, trace in enumerate(traces[:7], 1):
            body = self.base.wire({'data':[{'traceId':trace}]})
            path = self.m.observation_path(trace)
            response = {'complete':True,'raw_bytes':len(body),'raw_sha256':self.m.sha(body),'same_request_no_additional_GET':True,'status':200}
            if i == 7:
                if variant == 'status': response['status'] = 206
                elif variant == 'partial': response['complete'] = False
                elif variant == 'sha': response['raw_sha256'] = 'f'*64
                elif variant == 'duplicate': path = self.m.observation_path(traces[0])
                elif variant == 'substitute':
                    body = self.base.wire({'data':[{'traceId':traces[0]}]}); response.update(raw_bytes=len(body),raw_sha256=self.m.sha(body))
                elif variant == 'wrong-path': path += '&cursor=foreign'
            stem = f'get-evidence/get-{i:06d}'
            values[stem+'.body'] = body
            values[stem+'.intent.json'] = self.base.wire({'method':'GET','may_have_been_sent':True,'path':path})
            values[stem+'.response.json'] = self.base.wire(response)
        for name, raw in values.items(): (d/name).write_bytes(raw)
        return d, {name:self.m.sha(raw) for name,raw in values.items()}

    def test_wrong_partial_substituted_replay_evidence_is_rejected(self):
        for variant in ('status','partial','sha','duplicate','substitute','wrong-path'):
            with self.subTest(variant=variant):
                self.root = Path(tempfile.mkdtemp(prefix='synthetic-bad-replay-', dir=EVIDENCE))
                d, pins = self.completion_fixture(variant)
                with patch.object(self.m,'OLD_OPERATION',d), patch.object(self.m,'H40_FILES',pins):
                    with self.assertRaises(ValueError): self.m.load_completion(self.base,self.base.Reader())

    def test_initial_lock_before_loader_and_failed_completion_is_permanent(self):
        d, pins = self.completion_fixture()
        runs = self.root/'runs'; runs.mkdir(mode=0o700)
        component = self.root/'synthetic-component.py'
        component.write_text("BINDING_PATH='synthetic-never-read'\ndef load_corrected_consumer(*a):\n raise ValueError('synthetic-loader-stop')\n")
        scores = self.scores()
        data = {'consumer_raw':b'synthetic', 'reader':self.base.Reader(), 'job':{
            'target_index':40,'horizon_index':40,'target_summary_sha256':self.m.SUMMARY,'horizon_summary_sha256':self.m.SUMMARY}}
        with patch.object(self.m,'OLD_OPERATION',d), patch.object(self.m,'H40_FILES',pins), patch.object(self.m,'ROOT',self.root), patch.object(self.m,'OLD_RUNS',self.root), patch.object(self.m,'RUNS',runs), patch.object(self.m,'COMPONENT',component), patch.object(self.m,'COMPONENT_SHA',self.m.sha(component.read_bytes())), patch.object(self.m,'load_base',return_value=self.base), patch.object(self.base,'preflight',return_value=data), patch.object(self.base,'load_scores',return_value=scores):
            contract, plan, _ = self.m.load_completion(self.base,self.base.Reader())
            job = {'schema':'full1220-capture-input-operation-job.v1','mode':'completion',
                   'operator_sha256':self.m.sha(Path(self.m.__file__).read_bytes()),
                   'component_sha256':self.m.COMPONENT_SHA,'binding_sha256':'a'*64,
                   'base_job':'synthetic-base.json','base_job_sha256':'b'*64,'completion':contract}
            path = self.root/'job.json'; raw = self.base.wire(job); path.write_bytes(raw)
            with self.assertRaisesRegex(ValueError,'synthetic-loader-stop'): self.m.operate(path,self.m.sha(raw),execute=True)
            self.assertTrue((runs/'window-000040-completion-once/operation-error.json').is_file())
            with self.assertRaisesRegex(ValueError,'namespace consumed'): self.m.operate(path,self.m.sha(raw),execute=True)
            # Initial lock has been released: real original handle can reacquire.
            h = scores._open_publication_registry(self.m.SCOPE,create=True); h.close()

    def test_source_output_passes_original_issuer_path_gate(self):
        self.assertTrue(hasattr(self.m, 'reserve_source_output'), 'source compatibility namespace missing')
        issuer = load(self.m.D/'historical-window.py')
        name = 'synthetic-task3-'+self.root.name.replace('_','-')
        out = self.m.reserve_source_output(self.base, name)
        issuer.check_output_paths(str(out/'horizon-source'), str(out/'target-source'))
        self.assertTrue((out/'new-entry-source-namespace.json').is_file())
        with self.assertRaises(ValueError):
            issuer.check_output_paths(str(self.root/'horizon-source'), str(self.root/'target-source'))
        with self.assertRaises((ValueError,FileExistsError)): self.m.reserve_source_output(self.base,name)

    def test_original_contract_pins_and_literal_triples(self):
        contract, plan, replay = self.m.load_completion(self.base, self.base.Reader())
        self.assertEqual(len(contract['score_ids']), 28); self.assertEqual(len(replay), 7)
        self.assertEqual(set(plan['trace_bindings']), set(contract['traces']))
        self.assertTrue(contract['original_may_have_sent_POST'])
        for path, row in replay.items():
            self.assertIn('traceId=', path); self.assertEqual(row['status'], 200)
            self.assertEqual(hashlib.sha256(row['body']).hexdigest(), row['body_sha256'])

if __name__ == '__main__': unittest.main()
