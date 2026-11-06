"""Pure/negative V15 window boundaries; no helpers, signing or actual freeze."""
import copy
import hashlib
import json
import os
from pathlib import Path
import sys
import tempfile
import types
import unittest

HERE = Path(__file__).resolve().parents[1]


def load():
    path = HERE/'full1220-v15-window.py'; module = types.ModuleType('pure15window'); module.__file__ = str(path)
    sys.modules[module.__name__] = module
    if path.exists(): exec(compile(path.read_bytes(), str(path), 'exec'), module.__dict__)
    return module


def wire(value):
    return json.dumps(value, ensure_ascii=False, separators=(',', ':')).encode()


def scope():
    fixture = json.loads((HERE/'fixtures/v15-contracts/go-declarations.json').read_bytes())
    declarations = {name: {'go_json': fixture[key+'_go_json'], 'sha256': fixture[key+'_sha256']} for name, key in (('budget', 'budget'), ('completion', 'completion'), ('policy', 'operation_policy'))}
    data = json.loads((HERE/'fixtures/v9-contracts/fixture-scope-nonactual.json').read_bytes())
    data.update(schema='full-window-scope.v15', baseline_evidence_sha256='fec84f82af6951488d0886b42855f63ea2614d951f4c9f429836638933ea795c', collection_policy_sha256='3fd68fc05f8ebc4b3a5d87cbeb905caa46abbcedc5b34de836d910a5425f3213')
    data['bindings']['policy_sha256'] = 'eed1d1deb720a48b1ebd2725dfa3b0586f35e28b21e03f9635fe8e015cc618a0'
    data['budget_declaration'] = json.loads(declarations['budget']['go_json'])
    data['budget_declaration_sha256'] = declarations['budget']['sha256']
    data['retry_declaration'] = json.loads((HERE/'fixtures/v13-contracts/go-declarations.json').read_bytes())['retry']
    data['retry_declaration'] = json.loads(data['retry_declaration']['canonical'])
    data['retry_policy_sha256'] = '26fef0e90b466ed651b2e8bc11790d336d12d75554f14e7fd06a8d18009cf088'
    data['completion_v15'] = json.loads(declarations['completion']['go_json'])
    data['operation_policy_v14'] = json.loads(declarations['policy']['go_json'])
    data['windows'] = data['windows'][55:]
    data['observation'] = {'schema': 'full-window-observation-declaration.v11', 'private_settings_sha256': 'f'*64, 'spool_root': '/synthetic/spool', 'spool_run_id': '2'*64, 'spool_max_bytes': 4<<30, 'observation_receipt_boundary': 'durable_local', 'observer_identity_sha256': 'a'*64}
    return data


class Window(unittest.TestCase):
    def setUp(self):
        self.w = load()
        self.assertTrue(hasattr(self.w, '_scope'), 'V15 window module missing')
        self.n = self.w.dependency('native')

    def test_exact_schedule_full_denominators_and_stopped178_amounts(self):
        data = scope(); self.w._scope(data, self.n.wire(data), self.n)
        self.assertEqual(self.w.execution_totals(data), {'Pairs': 3275, 'Arms': 6550, 'DefinedUserTurns': 10504, 'Boundaries': 0, 'ModelCap': 110191257, 'JEVCap': 1818872})
        self.assertEqual((self.w.HISTORICAL_MODEL, self.w.HISTORICAL_JEV), (85219758, 936184))
        for index in (1, 55, 56, 456):
            self.assertEqual(self.w.execution_window_at(data, index)['id'], f'window-{index+55:03d}')

    def test_old_schema_completion_and_missing_or_changed_policy_rejected(self):
        for mutate in (lambda s: s.update(schema='full-window-scope.v13'), lambda s: s.update(completion_v13={}), lambda s: s.pop('operation_policy_v14'), lambda s: s['operation_policy_v14'].update(window_seconds=300), lambda s: s['operation_policy_v14'].update(model_seconds=91), lambda s: s['operation_policy_v14'].update(jev_seconds=11), lambda s: s['budget_declaration'].update(human_authorized=True)):
            data = scope(); mutate(data)
            with self.assertRaises(ValueError): self.w._scope(data, self.n.wire(data), self.n)

    def test_unfrozen_actual_and_source_entries_fail_before_io(self):
        with self.assertRaisesRegex(ValueError, 'UNFROZEN'): self.w.verify_actual_cohort(object())
        with self.assertRaisesRegex(ValueError, 'UNFROZEN'):
            self.w.verify_source_v15(b'{}', b'{}', {}, {}, {}, source_root='/', source_hashes={}, output_dir='/', execute=True, context=object())
        with self.assertRaisesRegex(ValueError, 'UNFROZEN'): self.w.load_freeze_binding(object(), 'runtime-binding.json', 'a'*64)

    def test_technical_context_cannot_mint_actual_context_or_normal_cohort(self):
        context = self.w._TechnicalContext(self.w._TECHNICAL_SEAL, Path('/synthetic/v15'), {}, {}, None)
        self.assertIsNot(context.seal, self.w._ACTUAL_SEAL)
        with self.assertRaises(ValueError): self.w._require_actual_context(context)
        for proof in (None, {}, types.SimpleNamespace(seal=self.w._AUTHORITY_SEAL)):
            with self.assertRaises(ValueError): self.w._ActualContext(self.w._ACTUAL_SEAL, context, b'{}', proof)
        with self.assertRaises(ValueError): self.w.VerifiedV15Cohort(self.w._COHORT_SEAL, context, (), (), {})
        with self.assertRaises(ValueError): self.w._VerifiedWindow(self.w._WINDOW_SEAL, {}, {}, None, context)
        for candidate in ({'authorized': True}, object(), None):
            with self.assertRaises(ValueError): self.w.check_verified_cohort(candidate)

    def policy_data(self):
        vectors = json.loads((HERE/'fixtures/v15-contracts/go-operation-wire-data.json').read_bytes())
        auth = copy.deepcopy(vectors['execution']['document']); approval = copy.deepcopy(vectors['approval']['document'])
        root = Path('/synthetic/v15')
        auth['operation_policy_approval']['path'] = str(root/'operation-policy-approval.json')
        approval['human_reply']['path'] = str(root/'operation-policy-human-reply.txt')
        source = {'synthetic.go': 'b'*64}; source_sha = hashlib.sha256(self.n.wire(source)).hexdigest()
        auth['source_sha256'] = approval['source_sha256'] = source_sha
        auth['scope_sha256'] = approval['scope_sha256'] = hashlib.sha256(self.n.wire(scope())).hexdigest()
        auth['operation_policy_approval']['sha256'] = hashlib.sha256(wire(approval)).hexdigest()
        manifest = {'schema': 'full-window-actual-ready.v15', 'scope': scope(), 'sources': source, 'scope_sha256': auth['scope_sha256'], 'execution_sha256': auth['execution_sha256'], 'private_settings_sha256': auth['private_settings_sha256'], 'spool_root': auth['spool_root'], 'observer': auth['observer'], 'executor_equivalence': {'sha256': auth['executor_equivalence_sha256']}, 'campaign_root': str(root/'campaign')}
        return root, manifest, auth, approval

    def test_policy_shape_validation_returns_only_data(self):
        root, manifest, auth, approval = self.policy_data()
        got = self.w.validate_operation_policy_authority_data(manifest, wire(auth), wire(approval), root=root, manifest_sha256='a'*64, binary_sha256='c'*64)
        self.assertIs(type(got), tuple)
        self.assertEqual(got, (auth, approval))
        with self.assertRaises(ValueError): self.w._require_actual_context(got)

    def test_new_policy_refs_and_each_binding_reject_mutations(self):
        for target, key, value in (('auth', 'scope_sha256', '0'*64), ('approval', 'manifest_sha256', '0'*64), ('approval', 'source_sha256', '0'*64), ('approval', 'binary_sha256', '0'*64), ('approval', 'scope_sha256', '0'*64), ('approval', 'execution_sha256', '0'*64), ('approval', 'operation_policy_sha256', '0'*64)):
            root, manifest, auth, approval = self.policy_data()
            (auth if target == 'auth' else approval)[key] = value
            if target == 'approval': auth['operation_policy_approval']['sha256'] = hashlib.sha256(wire(approval)).hexdigest()
            with self.subTest(target=target, key=key), self.assertRaises(ValueError):
                self.w.validate_operation_policy_authority_data(manifest, wire(auth), wire(approval), root=root, manifest_sha256='a'*64, binary_sha256='c'*64)

    def test_whole_window_timing_requires600_without_changing_request_policy(self):
        self.assertTrue(callable(getattr(self.w, 'validate_operation_timing', None)), 'V15 timing validation missing')
        receipt = {'operation_limit_seconds': 600, 'duration_ms': 599999.9, 'operation_deadline': 'synthetic-deadline'}
        self.w.validate_operation_timing(receipt)
        for key, value in (('operation_limit_seconds', 300), ('operation_limit_seconds', True),
                           ('duration_ms', 600000.1), ('duration_ms', float('nan')),
                           ('duration_ms', True), ('operation_deadline', '')):
            data = dict(receipt); data[key] = value
            with self.subTest(key=key, value=value), self.assertRaises(ValueError): self.w.validate_operation_timing(data)

    def test_sidecar_names_preserve_fixed_v13_owned_policy_and_digest_join(self):
        row = {'ordinal': 393, 'arm': 'jev_selective', 'recording_sha256': 'b'*64,
               'preview_failure': {'policy_sha256': 'eed1d1deb720a48b1ebd2725dfa3b0586f35e28b21e03f9635fe8e015cc618a0',
                   'recording_sha256': 'b'*64, 'sidecar_file': 'preview-failure-000393-jev_selective.json', 'sidecar_sha256': 'c'*64}}
        self.assertEqual(self.w._sidecar_names([row]), {'preview-failure-000393-jev_selective.json': 'c'*64})
        for mutate in (lambda r: r['preview_failure'].update(recording_sha256='a'*64),
                       lambda r: r['preview_failure'].update(sidecar_file='../private.json'),
                       lambda r: r['preview_failure'].update(policy_sha256='a'*64), lambda r: r.update(quality_end={})):
            data = copy.deepcopy(row); mutate(data)
            with self.assertRaises(ValueError): self.w._sidecar_names([data])
        with self.assertRaises(ValueError): self.w.require_quality([row])

    def test_helpers_measured_data_and_no_old_epoch_alias(self):
        self.assertEqual(self.w.SOURCE_HELPER, 'helpers/full1220_source_v15_test.go.txt')
        self.assertEqual(self.w.SOURCE_HELPER_SHA256, 'UNFROZEN')
        self.assertEqual(self.w.DEPENDENCIES['runner'], ('full1220-v15-helper-runtime.py', hashlib.sha256((HERE/'full1220-v15-helper-runtime.py').read_bytes()).hexdigest()))
        self.w.dependency('runner') # Process/catalog DATA, still no selected actual scope.
        with self.assertRaisesRegex(ValueError, 'UNFROZEN'): self.w._selected()
        self.assertEqual(self.w.CAPTURE_INPUT_SCHEMA, 'full1220-content-projection-input.v1')
        for name in ('native', 'source', 'codec', 'prefix_scores'): self.w.dependency(name)

    def test_phase_diagnostics_are_bounded_detached_data_only(self):
        phases = [{'stage': 'history', 'duration_ms': 118000, 'deadline_remaining_ms': 481000, 'outcome': 'ok'},
                  {'stage': 'other_inputs', 'duration_ms': 0, 'deadline_remaining_ms': 0, 'outcome': 'deadline_exceeded'},
                  {'stage': 'other_inputs', 'duration_ms': 0, 'deadline_remaining_ms': 0, 'outcome': 'canceled'}]
        got = self.w.validate_phase_timings_v15(phases)
        self.assertEqual(got, phases)
        phases[0]['stage'] = 'authority'
        self.assertEqual(got[0]['stage'], 'history')
        for proof in (got, {'phase_timings_v15': got, 'closed': True, 'acquisition_done': True}):
            with self.assertRaises(ValueError): self.w._require_actual_context(proof)
            with self.assertRaises(ValueError): self.w.check_verified_completion(proof, {})
        for key, value in (('stage', 'credential/path'), ('outcome', 'passed'), ('duration_ms', -1),
                           ('duration_ms', True), ('duration_ms', 1.0), ('duration_ms', 1 << 63),
                           ('deadline_remaining_ms', 600001), ('deadline_remaining_ms', False), ('secret', 'x')):
            bad = copy.deepcopy(got); bad[0][key] = value
            with self.subTest(key=key, value=value), self.assertRaises(ValueError): self.w.validate_phase_timings_v15(bad)
        for bad in (None, {}, got * 6):
            with self.assertRaises(ValueError): self.w.validate_phase_timings_v15(bad)
        self.assertEqual(self.w.validate_phase_timings_v15([]), [])

    def test_valid_phase_diagnostics_cannot_rescue_forged_completed_summary(self):
        phases = [{'stage': 'acquisition', 'duration_ms': 1, 'deadline_remaining_ms': 599999, 'outcome': 'ok'}]
        summary = {'phase_timings_v15': phases, 'closed': True, 'acquisition_done': True}
        raw = wire(summary)
        with self.assertRaises(ValueError): self.w.verify_summary_chain([raw], [b'{}'], scope(), self.w.sha(raw))

    def test_summary_phase_field_is_optional_and_never_replaces_receipt_flags(self):
        # Synthetic DATA-only prefix exercises summary checks, never a factory.
        receipt = {'schema': 'full-window-acquisition-receipt.v15', 'evidence_kind': 'recorded_run',
                   'status': 'collected_window', 'window_index': 1, 'attempted_arms': 2, 'finished_arms': 2,
                   'unrun_arms': 0, 'observed_turns': 2, 'accounting': {'model_intents': 1, 'jev_intents': 1},
                   'v8': {}, 'observation': {}, 'payload_manifest_sha256': 'a'*64, 'ledger_sha256': 'b'*64,
                   'advanced': True, 'cleanup': True, 'environment_cleanup': True, 'observer': True, 'payload_verified': True}
        schedule = {'windows': [{'last_ordinal': 386}], 'slots': [{'arm_order': ['model', 'jev_selective']}]*387}
        rr = wire(receipt)
        summary = {'observation': {}, 'v8': {}, 'evidence_kind': 'recorded_run', 'acquisition_done': False,
                   'closed': False, 'next_window_index': 2, 'last_receipt_sha256': '', 'planned_pairs': 3275,
                   'planned_arms': 6550, 'defined_turns': 10504, 'boundaries_excluded': 0, 'attempted_arms': 2,
                   'finished_arms': 2, 'failed_attempted_arms': 0, 'unattempted_arms': 6548, 'observed_turns': 2,
                   'quality_end_unrun_turns': 0, 'global_unrun_turns': 10502, 'model_attempts': 1, 'jev_attempts': 1,
                   'window_index': 1, 'report_sha256': self.w.sha(rr), 'payload_manifest_sha256': 'a'*64,
                   'ledger_sha256': 'b'*64, 'first_unrun_ordinal': 387, 'first_unrun_arm': 'model',
                   'future_unrun_from_ordinal': 387, 'report_status': 'collected_window'}
        for phases in (None, [], [{'stage': 'history', 'duration_ms': 1, 'deadline_remaining_ms': 1, 'outcome': 'error'}]):
            current = copy.deepcopy(summary)
            if phases is not None: current['phase_timings_v15'] = phases
            raw = wire(current)
            self.assertEqual(self.w.verify_summary_chain([raw], [rr], schedule, self.w.sha(raw)), {})
        for key in ('advanced', 'cleanup', 'environment_cleanup', 'observer', 'payload_verified'):
            bad = dict(receipt); bad[key] = False; rraw = wire(bad)
            current = dict(summary); current['report_sha256'] = self.w.sha(rraw)
            current['phase_timings_v15'] = [{'stage': 'acquisition', 'duration_ms': 1, 'deadline_remaining_ms': 1, 'outcome': 'ok'}]
            raw = wire(current)
            with self.subTest(flag=key), self.assertRaises(ValueError): self.w.verify_summary_chain([raw], [rraw], schedule, self.w.sha(raw))
        summary['phase_timings_v15'] = [{'stage': 'history', 'duration_ms': 1, 'deadline_remaining_ms': 1, 'outcome': 'ok', 'proof': True}]
        raw = wire(summary)
        with self.assertRaises(ValueError): self.w.verify_summary_chain([raw], [rr], schedule, self.w.sha(raw))

    def test_pending10_held2688_and_fixed178_reserve_are_not_settlements(self):
        ledger = {'logical_counts': {'model': 8, 'jev': 2}, 'native_counts': {'model': 8, 'jev': 2},
                  'retry_counts': {'model': 0, 'jev': 0}, 'settlements': [None]*9, 'unknown_intents': [],
                  'pending': {'sequence': 10}, 'retry_required': None, 'halt': 'upstream_failure',
                  'known': {'model': 0, 'jev': 0}, 'unknown_held': {'model': 0, 'jev': 0},
                  'pending_held': {'model': 0, 'jev': 2688}, 'contract': {'window_id': 'window-056'}}
        before = copy.deepcopy(ledger); zero = {'model_micro_cny': 0, 'jev_micro_usd': 0}
        permanent = self.w._permanent(zero, [], self.n)
        self.assertEqual(permanent, {'model_micro_cny': 85219758, 'jev_micro_usd': 936184})
        report = self.w._runtime(ledger, zero, [], permanent, self.n)
        self.assertEqual(report['native_pending'], 1)
        self.assertEqual(report['native_settlements'], 9)
        self.assertEqual(report['pending_held'], {'model_micro_cny': 0, 'jev_micro_usd': 2688})
        self.assertEqual(report['halt'], 'upstream_failure')
        self.assertEqual(ledger, before)

    def test_unsafe_ancestor_and_relative_evidence_paths_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve(); (root/'real').mkdir(); (root/'real/data').write_bytes(b'original')
            (root/'alias').symlink_to(root/'real', target_is_directory=True)
            reader = self.w.Evidence(root)
            for name in ('alias/data', '../data', '/data', 'real/../real/data', 'real//data'):
                with self.subTest(name=name), self.assertRaises(ValueError): reader.read(name)

    def test_foreign_or_serialized_proofs_do_not_grant_completion(self):
        other = load(); source = other._SourceProof(other._SOURCE_SEAL, {'inputs': {}})
        with self.assertRaises(ValueError): self.w._check_source_proof(source, {})
        for value in ({'prefix': {'all_verified': True}, 'suffix': {'closed': True}}, object(), None):
            with self.assertRaises(ValueError): self.w.check_verified_completion(value, {})
        with self.assertRaises(ValueError): self.w.check_verified_cohort(object.__new__(other.VerifiedV15Cohort))

    def test_final_token_is_checkpoint456_and_never_claim457(self):
        checkpoint = b'synthetic original checkpoint456'; claims = ['a'*64]*456
        self.w._verify_advance_token(456, self.w.sha(checkpoint), claims, checkpoint)
        for index, token, selected in ((456, 'a'*64, claims), (456, self.w.sha(checkpoint), claims+['b'*64]), (457, 'a'*64, claims)):
            with self.assertRaises(ValueError): self.w._verify_advance_token(index, token, selected, checkpoint)

    def test_primary_partition_keeps_all3660_and_rejects_v13_substitution(self):
        slots = []; rows = []
        for ordinal in range(1, 3661):
            turns = 2 if ordinal <= 249 or 386 <= ordinal < 2363 else 1
            slot = {'ordinal': ordinal, 'pass': (ordinal-1)//1220+1, 'case_key': f'case{ordinal}', 'arm_order': ['model', 'jev_selective'], 'user_turns': turns}
            slots.append(slot)
            for arm in slot['arm_order']:
                rows.append({'ordinal': ordinal, 'pass': slot['pass'], 'case_key': slot['case_key'], 'arm': arm, 'defined_turns': turns,
                             'trace_id': f'{len(rows)+1:032x}', 'attempted': True, 'finished': True, 'status': 'failed', 'semantic': 'not_assessable'})
        self.assertEqual(self.w.validate_primary_partition(rows[:770], rows[770:], slots), {'Pairs': 3660, 'Arms': 7320, 'DefinedUserTurns': 11772, 'Boundaries': 1851})
        for change in ('gap', 'overlap', 'order', 'unattempted', 'trace'):
            bad = copy.deepcopy(rows)
            if change == 'gap': bad.pop(770)
            elif change == 'overlap': bad[770] = copy.deepcopy(bad[0])
            elif change == 'order': bad[770], bad[771] = bad[771], bad[770]
            elif change == 'unattempted': bad[770]['attempted'] = False
            else: bad[770]['trace_id'] = bad[0]['trace_id']
            with self.subTest(change=change), self.assertRaises(ValueError): self.w.validate_primary_partition(bad[:770], bad[770:], slots)

    def test_original_prefix_wire_and_stored_get_remain_original_epochs(self):
        manifest = {'schema': 'full-window-actual-ready.v9', 'scope': {'schema': 'full-window-scope.v9'}, 'scope_sha256': self.w.ORIGINAL_SCOPE_SHA256, 'execution_sha256': 'e'*64}
        receipt = {'schema': 'full-window-acquisition-receipt.v8', 'window_index': 55, 'window_id': 'window-055', 'scope_sha256': manifest['scope_sha256'], 'execution_sha256': manifest['execution_sha256']}
        self.w.validate_original_prefix_wire(manifest, receipt)
        receipt['schema'] = 'full-window-acquisition-receipt.v15'
        with self.assertRaises(ValueError): self.w.validate_original_prefix_wire(manifest, receipt)
        body = b'{"data":[]}'
        response = {'status': 200, 'complete': True, 'raw_sha256': hashlib.sha256(body).hexdigest(), 'raw_bytes': len(body)}
        intent = {'method': 'GET', 'path': '/api/public/v3/scores?scoreIds=x'}
        self.assertEqual(self.w.parse_original_get(wire(intent), wire(response), body)[0], 'scores')
        response['complete'] = False
        with self.assertRaises(ValueError): self.w.parse_original_get(wire(intent), wire(response), body)

    def test_local_evidence_rejects_links_extra_namespace_and_changed_bytes(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve(); (root/'data').write_bytes(b'original')
            reader = self.w.Evidence(root); reader.read('data', self.w.sha(b'original')); reader.inventory({'data'})
            (root/'extra').write_bytes(b'extra')
            with self.assertRaises(ValueError): reader.inventory({'data'})
            (root/'extra').unlink(); (root/'link').symlink_to(root/'data')
            with self.assertRaises(ValueError): reader.read('link')
            (root/'link').unlink(); os.link(root/'data', root/'hardlink')
            with self.assertRaises(ValueError): reader.read('hardlink')
            (root/'hardlink').unlink(); (root/'data').write_bytes(b'changed')
            with self.assertRaises(ValueError): reader.recheck()
    def test_policy_companion_paths_are_exact_new_namespace(self):
        for target, key in (('auth', 'operation_policy_approval'), ('approval', 'human_reply')):
            root, manifest, auth, approval = self.policy_data()
            (auth if target == 'auth' else approval)[key]['path'] = str(root/'wrong.json')
            with self.subTest(target=target), self.assertRaises(ValueError):
                self.w.validate_operation_policy_authority_data(manifest, wire(auth), wire(approval), root=root, manifest_sha256='a'*64, binary_sha256='c'*64)

    def test_technical_audit_requires_explicit_policy_gate_without_authorizing(self):
        manifest = {'scope_sha256': 'a'*64, 'execution_sha256': 'b'*64}
        binding = {'verifier_sha256': 'c'*64, 'owner_sha256': 'd'*64}
        artifacts = {'technical-start-audit.json': 'e'*64, 'actual-manifest.json': 'f'*64}
        audit = {'schema': 'full1220-v15-technical-start-audit.v1', 'technical_checks_passed': True, 'confirmed_P1_P2': 0,
                 'actual_execution_authorized': False, 'human_reviewed_exact_scope': False, 'delegated_authorization_required': True,
                 'operation_policy_approval_required': True,
                 'operation_policy_sha256': '40f1ad8fe36d38a54e43e4d7fdc319d1137c8cde6aeac2255a200a44370f83a3',
                 'archive_commit': 'UNFROZEN', 'archive_tree': 'UNFROZEN', 'source_sha256': 'UNFROZEN',
                 'manifest_sha256': 'UNFROZEN', 'binary_sha256': 'UNFROZEN', 'scope_sha256': 'a'*64,
                 'execution_sha256': 'b'*64, 'verifier_sha256': 'c'*64, 'owner_sha256': 'd'*64,
                 'helper_catalog_sha256': 'UNFROZEN', 'artifact_sha256': {'actual-manifest.json': 'f'*64}}
        # Pure metadata validation alone cannot pass _selected or mint context.
        self.w._validate_technical_audit(audit, manifest, binding, artifacts, self.n)
        for key, value in (('operation_policy_approval_required', False), ('actual_execution_authorized', True),
                           ('human_reviewed_exact_scope', True), ('operation_policy_sha256', '0'*64)):
            changed = dict(audit); changed[key] = value
            with self.subTest(key=key), self.assertRaises(ValueError):
                self.w._validate_technical_audit(changed, manifest, binding, artifacts, self.n)
        with self.assertRaises(ValueError): self.w._require_actual_context(audit)


if __name__ == '__main__': unittest.main()
