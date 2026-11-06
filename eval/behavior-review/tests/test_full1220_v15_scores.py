"""Pure/negative V15 score contracts. No signing, credentials or LF requests."""
import copy
import hashlib
import json
from pathlib import Path
import sys
import types
import unittest

HERE = Path(__file__).resolve().parents[1]


def load():
    path = HERE/'full1220-v15-scores.py'; module = types.ModuleType('pure15scores'); module.__file__ = str(path)
    sys.modules[module.__name__] = module
    if path.exists(): exec(compile(path.read_bytes(), str(path), 'exec'), module.__dict__)
    return module


def candidate_scope():
    fixture = json.loads((HERE/'fixtures/v15-contracts/go-declarations.json').read_bytes())
    vectors = {name: {'go_json': fixture[key+'_go_json'], 'sha256': fixture[key+'_sha256']} for name, key in (('budget', 'budget'), ('completion', 'completion'), ('policy', 'operation_policy'))}
    scope = json.loads((HERE/'fixtures/v9-contracts/fixture-scope-nonactual.json').read_bytes())
    scope.update(schema='full-window-scope.v15', completion_v15=json.loads(vectors['completion']['go_json']),
                 operation_policy_v14=json.loads(vectors['policy']['go_json']))
    scope['windows'] = scope['windows'][55:]
    scope['budget_declaration'] = json.loads(vectors['budget']['go_json'])
    scope['budget_declaration_sha256'] = vectors['budget']['sha256']
    scope['baseline_evidence_sha256'] = 'fec84f82af6951488d0886b42855f63ea2614d951f4c9f429836638933ea795c'
    return scope


def forbidden(*args, **kwargs):
    raise AssertionError('unauthenticated file/settings/registry/request access')


class Scores(unittest.TestCase):
    def setUp(self):
        self.m = load()
        self.assertTrue(callable(getattr(self.m, '_check_actual', None)), 'V15 score adapter missing')

    def test_default_main_and_public_actual_entries_deny_before_io(self):
        self.m._regular_bytes = forbidden; self.m._projection_private_settings = forbidden
        self.m._open_publication_registry = forbidden
        self.assertEqual(self.m.main([]), 0)
        for call in (
            lambda: self.m.verify_actual_score_inputs('/untrusted'),
            lambda: self.m.build_window_score_plan(b'{}', b'{}', b'{}', {}, b'{}', {}, actual_proof={'verified': True}),
            lambda: self.m.load_window_score_client(b'{}', '/untrusted', actual_proof={'verified': True}),
            lambda: self.m.publish_window_scores({}, None, None, None, actual_proof={'verified': True}, execute=True),
            lambda: self.m.verify_window_scores({}, None, actual_proof={'verified': True}),
            lambda: self.m.verify_remote_score_inputs({}, None, actual_proof={'verified': True}),
            lambda: self.m.project_window_content(b'{}', b'{}', b'{}', {}, b'{}', {}, source_root='/untrusted',
                        output_dir='/untrusted', execute=True, actual_proof={'verified': True}, private_settings_path='/untrusted'),
        ):
            with self.assertRaisesRegex(ValueError, 'UNFROZEN'): call()

    def test_private_io_cores_require_actual_authority_too(self):
        self.m._regular_bytes = forbidden; self.m._open_publication_registry = forbidden
        self.m._check_remote = forbidden; self.m.Path = forbidden
        for call in (
            lambda: self.m._load_window_score_client(b'{}', '/untrusted'),
            lambda: self.m._publish_window_scores({}, None, None, None, None, execute=True),
            lambda: self.m._verify_window_scores({}, None),
        ):
            with self.assertRaisesRegex(ValueError, 'UNFROZEN'): call()

    def test_context_or_mock_data_cannot_construct_score_client_or_actual_proof(self):
        with self.assertRaises(ValueError): self.m._WindowScoreClient(self.m._PUBLICATION_SEAL, object(), 'a'*64)
        with self.assertRaises(ValueError): self.m._ScoreActualProof(self.m._SCORE_SEAL, object(), {}, {}, b'{}', b'{}', b'{}', {}, {}, b'{}', {}, {})

    def test_valid_selection_pins_still_do_not_turn_mock_into_actual_authority(self):
        self.m.FROZEN_ROOT = '/synthetic/v15'; self.m.FROZEN_VERIFIER_SHA256 = 'a'*64; self.m.FROZEN_BINDING_SHA256 = 'b'*64
        self.m._regular_bytes = forbidden; self.m._open_publication_registry = forbidden
        for value in ({'verified': True}, object(), None):
            with self.assertRaisesRegex(ValueError, 'Opaque authenticated V15'): self.m._check_actual(value)
            with self.assertRaises(ValueError): self.m._load_window_score_client(b'{}', '/untrusted', actual_proof=value)
        client = object.__new__(self.m._WindowScoreClient)
        client._actual_proof = {'verified': True}; client._inner = types.SimpleNamespace(request=forbidden, observations=forbidden, verify_scores=forbidden)
        for call in (lambda: client.post_score({}), lambda: client.observations('a'*32), lambda: client.verify_scores([])):
            with self.assertRaises(ValueError): call()

    def test_selected_technical_context_is_not_normal_score_authority(self):
        self.m.FROZEN_ROOT = '/synthetic/v15'; self.m.FROZEN_VERIFIER_SHA256 = 'a'*64; self.m.FROZEN_BINDING_SHA256 = 'b'*64
        path = HERE/'full1220-v15-window.py'; verifier = types.ModuleType('technical15scoretest'); verifier.__file__ = str(path)
        exec(compile(path.read_bytes(), str(path), 'exec'), verifier.__dict__)
        self.m._cohort_verifier_module = verifier
        technical = verifier._TechnicalContext(verifier._TECHNICAL_SEAL, Path('/synthetic/v15'), {}, {}, None)
        completion = object.__new__(verifier.VerifiedV15Completion)
        object.__setattr__(completion, '_seal', verifier._COMPLETION_SEAL)
        object.__setattr__(completion, '_context', technical)
        with self.assertRaisesRegex(ValueError, 'actual V15 context'):
            self.m._require_score_authority(verifier, completion)

    def test_planned_rows_preserve_stopped600_as_data_without_promoting_completion(self):
        scope = candidate_scope(); window = scope['windows'][0]
        selected = [slot for slot in scope['slots'] if slot['window_id'] == window['id']]
        rows = []
        for slot in selected:
            for arm in slot['arm_order']:
                rows.append({key: slot[key] for key in ('ordinal', 'pass', 'case_key')})
                rows[-1].update(arm=arm, defined_turns=slot['user_turns'], observed_turns=0,
                                attempted=False, finished=False, cleanup=False, observer=False, structural_pass=False, success=False)
        manifest = {'scope': scope}
        receipt = {'window_index': 1, 'window_id': 'window-056', 'status': 'stopped', 'rows': rows,
                   'operation_limit_seconds': 600, 'duration_ms': 600001,
                   'planned_campaign_pairs': 3275, 'planned_campaign_arms': 6550, 'planned_campaign_turns': 10504,
                   'planned_current_pairs': len(selected), 'planned_current_arms': len(rows),
                   'planned_current_turns': sum(slot['user_turns']*2 for slot in selected),
                   'attempted_arms': 0, 'finished_arms': 0, 'unrun_arms': len(rows), 'observed_turns': 0}
        self.assertEqual(len(self.m._planned_rows(manifest, receipt)), len(rows))
        receipt['operation_limit_seconds'] = 300
        with self.assertRaises(ValueError): self.m._planned_rows(manifest, receipt)
        receipt['operation_limit_seconds'] = 600; receipt['status'] = 'collected_window'
        with self.assertRaises(ValueError): self.m._planned_rows(manifest, receipt)

    def test_fixed_completion_preserves_prefix_holes_and_full_denominator(self):
        scope = candidate_scope(); completion = self.m._completion(scope)
        self.assertEqual(completion['prefix']['pairs'], 385)
        self.assertEqual(completion['execution']['pairs'], 3275)
        self.assertEqual(completion['primary'], {'pairs': 3660, 'arms': 7320, 'defined_user_turns': 11772})
        self.assertEqual((completion['boundaries'], completion['prefix_score_count'], completion['prefix_positive_score_holes']), (1851, 2300, 10))
        self.assertEqual((completion['historical_claims'], completion['historical_model_attempts'], completion['historical_jev_attempts']), (178, 7245, 1871))
        for mutate in (lambda s: s.update(schema='full-window-scope.v13'), lambda s: s.update(completion_v13={}),
                       lambda s: s['completion_v15'].update(prefix_positive_score_holes=0),
                       lambda s: s['completion_v15'].update(historical_claims=156),
                       lambda s: s['operation_policy_v14'].update(window_seconds=300),
                       lambda s: s['operation_policy_v14'].update(model_seconds=91), lambda s: s['operation_policy_v14'].update(jev_seconds=11)):
            data = candidate_scope(); mutate(data)
            with self.assertRaises(ValueError): self.m._completion(data)

    def test_capture_api_stays_exact_and_native_content_metadata_only(self):
        provenance = {key: 'a'*64 for key in ('manifest_sha256', 'receipt_sha256', 'scope_sha256', 'execution_sha256', 'archive_commit', 'archive_tree')}
        job = self.m._projection_job({'rows': [], 'provenance': provenance, 'window_id': 'window-056'}, {'private_settings_sha256': 'b'*64})
        self.assertEqual(job['schema'], 'full1220-content-projection-input.v1')
        self.assertEqual(self.m._PROJECTION_HELPER_RELATIVE, 'internal/observability/full1220_content_projection_v8_test.go')
        packet = {'sequence': 1, 'identity': {'native_attempt': 1, 'provider': 'model', 'model': 'deepseek-flash',
            'case_key': 'case/one', 'pass': 1, 'arm': 'model', 'turn': 1, 'attempt': 1, 'body_sha256': 'a'*64, 'bytes': 42},
            'claim_sha256': 'b'*64, 'response': {'complete': True, 'available': True, 'status': 200}}
        metadata = self.m.native_metadata(packet)
        self.assertEqual(metadata['native_content_mode'], 'metadata_only')
        self.assertNotIn('input', metadata); self.assertNotIn('output', metadata)

    def test_new_remote_binding_includes_separately_verified_policy_identity(self):
        provenance = {key: 'a'*64 for key in ('manifest_sha256', 'scope_sha256', 'execution_sha256', 'receipt_sha256', 'summary_sha256')}
        provenance.update(operation_policy_sha256='40f1ad8fe36d38a54e43e4d7fdc319d1137c8cde6aeac2255a200a44370f83a3', operation_policy_approval_sha256='b'*64)
        plan = {'provenance': provenance, 'trace_bindings': {'c'*32: {}}}
        projection = types.SimpleNamespace(_raw_sha='d'*64)
        binding = self.m.remote_binding(plan, projection)
        self.assertEqual(binding['schema'], 'full1220-v15-remote-window-binding.v1')
        self.assertEqual(binding['operation_policy_approval_sha256'], 'b'*64)
        self.assertEqual(binding['operation_policy_sha256'], provenance['operation_policy_sha256'])
        del provenance['operation_policy_approval_sha256']
        with self.assertRaises((ValueError, KeyError)): self.m.remote_binding(plan, projection)

    def test_negative_record_and_private_sidecar_boundary_are_preserved(self):
        result = {'errors': ['semantic negative'], 'observations': [{}], 'structural_pass': True,
                  'success': False, 'setup_error': False, 'review_requirements': [{}], 'status': 'failed'}
        record = {'policy_id': 'full-customer-recording-preview-failure.v2', 'policy_sha256': '3fd68fc05f8ebc4b3a5d87cbeb905caa46abbcedc5b34de836d910a5425f3213',
                  'result_sha256': self.m._sha(self.m._json_bytes(result)), 'disposition': 'continue_recorded',
                  'issues': [{'code': 'semantic_negative', 'turn': 1, 'error_index': 0, 'disposition': 'continue_recorded'}], 'result': result}
        self.assertEqual(self.m.validate_completed_record_state(self.m._json_bytes(record)), 'recorded_negative')
        result['success'] = True; record['result_sha256'] = self.m._sha(self.m._json_bytes(result))
        with self.assertRaises(ValueError): self.m.validate_completed_record_state(self.m._json_bytes(record))

    def test_document_rejects_non_json_trailing_whitespace_and_duplicates(self):
        for raw in (b'{"x":1}\v', b'{"x":1}\x1c', b'{"x":1}\xc2\xa0', b'{"x":1,"x":2}'):
            with self.subTest(raw=raw), self.assertRaises(ValueError): self.m._Document(raw)
        self.m._Document(b'{"x":1} \t\r\n')

    def publication_plan(self, value='failed'):
        logical = {'scope_sha256': 'a'*64, 'window_id': 'window-056', 'pass': 1, 'ordinal': 386,
                   'arm': 'model', 'trace_id': 'b'*32, 'metric': 'full1220.v1.semantic_task'}
        logical_raw = self.m._json_bytes(logical).decode()
        score = {'id': hashlib.sha256(logical_raw.encode()).hexdigest(), 'logical_identity': logical_raw,
                 'traceId': 'b'*32, 'name': 'full1220.v1.semantic_task', 'value': value,
                 'metadata': {'pass': 1, 'ordinal': 386, 'arm': 'model', 'original_status': 'failed', 'original_structural_pass': False}}
        return {'schema': 'full1220-window-score-plan.v1', 'provenance': {'scope_sha256': 'a'*64},
                'window_id': 'window-056', 'trace_bindings': {'b'*32: {}}, 'scores': [score]}

    def test_once_identity_cannot_be_reset_and_failed_results_cannot_publish_pass(self):
        plan = self.publication_plan(); selected = self.m._publication_scores(plan)
        self.assertEqual(selected[0]['id'], 'e8addd5242ef7fc4982ea32284d23f81bb83d421aea8b563141c254125764773')
        with self.assertRaises(ValueError): self.m._publication_scores(self.publication_plan('passed'))
        changed = copy.deepcopy(plan); changed['scores'][0]['id'] = 'c'*64
        with self.assertRaises(ValueError): self.m._publication_scores(changed)
        changed = copy.deepcopy(plan); changed['scores'].append(changed['scores'][0])
        with self.assertRaises(ValueError): self.m._publication_scores(changed)

    def test_projection_receipt_requires_genuine_filename_and_one_marker(self):
        raw = b'{"synthetic":"projection"}\n'; digest = hashlib.sha256(raw).hexdigest().encode()
        line = b'    full1220_content_projection_v8_test.go:231: FULL1220_PROJECTION_OUTPUT_SHA256='+digest+b'\n'
        self.m._verify_projection_receipt(line, raw)
        for stdout, data in ((line+line, raw), (line, raw+b' '), (line.replace(b'v8_test', b'v15_test'), raw)):
            with self.assertRaises(ValueError): self.m._verify_projection_receipt(stdout, data)

    def pure_unattempted_plan_inputs(self, eligible=False):
        # Entirely synthetic stopped DATA: no records, calls or actual proof.
        encode = lambda value: json.dumps(value, ensure_ascii=False, separators=(',', ':')).encode()
        scope = candidate_scope(); window = scope['windows'][0]; rows = []; reviews = []
        for slot in scope['slots'][window['first_ordinal']-1:window['last_ordinal']]:
            for arm in slot['arm_order']:
                identity = {key: slot[key] for key in ('ordinal', 'pass', 'case_key')}; identity['arm'] = arm
                rows.append({**identity, 'defined_turns': slot['user_turns'], 'observed_turns': 0,
                             'attempted': False, 'finished': False, 'cleanup': False, 'observer': False,
                             'structural_pass': False, 'success': False, 'status': 'not_started',
                             'trace_id': '', 'recording_file': '', 'recording_sha256': ''})
                reviews.append({**identity, 'original_status': 'not_started', 'original_success': False,
                                'pass_score_publish_eligible': eligible, 'task_verdict': 'not_started', 'requirements': []})
        scope_sha = hashlib.sha256(encode(scope)).hexdigest(); archive = scope['bindings']['executor_revision']
        manifest = {'schema': 'full-window-actual-ready.v15', 'evidence_kind': 'actual', 'scope': scope,
                    'scope_sha256': scope_sha, 'execution_sha256': 'b'*64, 'archive_commit': archive, 'archive_tree': 'a'*40,
                    'sources': {'internal/taskbench/conversation_runner.go': 'a'*64},
                    'knowledge_model': 'off', 'knowledge_jev': 'off', 'historical_claims': {}, 'envelope': {'sha256': 'c'*64}}
        receipt = {'schema': 'full-window-acquisition-receipt.v15', 'evidence_kind': 'recorded_run', 'status': 'stopped',
                   'scope_sha256': scope_sha, 'execution_sha256': 'b'*64, 'window_index': 1, 'window_id': 'window-056', 'rows': rows,
                   'operation_limit_seconds': 600, 'duration_ms': 600001, 'ledger_sha256': 'd'*64, 'payload_manifest_sha256': 'e'*64,
                   'planned_campaign_pairs': 3275, 'planned_campaign_arms': 6550, 'planned_campaign_turns': 10504,
                   'planned_current_pairs': len(rows)//2, 'planned_current_arms': len(rows),
                   'planned_current_turns': sum(row['defined_turns'] for row in rows), 'attempted_arms': 0,
                   'finished_arms': 0, 'unrun_arms': len(rows), 'observed_turns': 0}
        mraw = encode(manifest); rraw = encode(receipt)
        summary = {'evidence_kind': 'recorded_run', 'planned_pairs': 3275, 'planned_arms': 6550, 'defined_turns': 10504,
                   'boundaries_excluded': 0, 'window_index': 1, 'report_sha256': hashlib.sha256(rraw).hexdigest(),
                   'report_status': 'stopped', 'ledger_sha256': 'd'*64, 'payload_manifest_sha256': 'e'*64,
                   'attempted_arms': 0, 'finished_arms': 0, 'unattempted_arms': 6550, 'failed_attempted_arms': 0,
                   'observed_turns': 0, 'quality_end_unrun_turns': 0, 'global_unrun_turns': 10504}
        review = {'schema': 'full1220-window-independent-review.v1', 'method': 'independent_agent_evidence_review',
                  'reviewer': 'synthetic-data-review', 'rows': reviews,
                  'bindings': {'manifest_sha256': hashlib.sha256(mraw).hexdigest(), 'scope_sha256': scope_sha,
                               'execution_sha256': 'b'*64, 'receipt_sha256': hashlib.sha256(rraw).hexdigest(),
                               'archive_commit': archive, 'archive_tree': 'a'*40}}
        sraw = encode(summary); review_raw = encode(review)
        anchors = {name+'_sha256': hashlib.sha256(value).hexdigest() for name, value in
                   (('manifest', mraw), ('summary', sraw), ('receipt', rraw), ('review', review_raw))}
        anchors['records'] = {}
        return mraw, sraw, rraw, {}, review_raw, anchors

    def test_pure_stopped_plan_cannot_claim_source_authority_or_invent_scores(self):
        plan = self.m.build_window_score_plan(*self.pure_unattempted_plan_inputs())
        self.assertIs(plan['publish_ready'], False)
        self.assertEqual(plan['scores'], [])
        self.assertEqual(plan['provenance']['actual_source_and_native_replay'], 'unverified_data_only')
        self.assertNotIn('operation_policy_approval_sha256', plan['provenance'])
        self.assertEqual(plan['denominators']['campaign'], {'pairs': 3660, 'arms': 7320, 'defined_turns': 11772, 'boundaries_excluded': 1851})
        with self.assertRaises(ValueError): self.m.build_window_score_plan(*self.pure_unattempted_plan_inputs(eligible=True))
        with self.assertRaises(ValueError): self.m.publish_window_scores(plan, None, None, None, execute=True)

    def test_tool_phase_parent_content_and_missing_output_remain_distinct(self):
        projected = [{'turn_index': 1, 'tools': [{'name': 'tool.get_balance', 'phase': 'read', 'input': {}, 'output': '余额'}]}]
        observations = [{'id': 'turn', 'name': 'agent.turn', 'metadata': {'turn_index': 1}},
                        {'id': 'tool', 'name': 'tool.get_balance', 'parentObservationId': 'turn', 'metadata': {'phase': 'read'}, 'input': {}, 'output': '余额'}]
        self.assertTrue(self.m.validate_tool_observations(observations, projected))
        for key, value in (('output', 'wrong'), ('input', {'private': 'wrong'}), ('parentObservationId', 'foreign')):
            changed = copy.deepcopy(observations); changed[1][key] = value
            with self.assertRaises(ValueError): self.m.validate_tool_observations(changed, projected)
        changed = copy.deepcopy(observations); del changed[1]['output']
        self.assertFalse(self.m.validate_tool_observations(changed, projected))


if __name__ == '__main__': unittest.main()
