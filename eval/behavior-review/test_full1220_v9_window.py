"""Pure V9 schedule/identity boundaries; fixture is explicitly nonactual."""
import copy
import hashlib
import json
from pathlib import Path
import types
import unittest

HERE = Path(__file__).resolve().parent
FIXTURE = HERE/'fixtures/v9-contracts/fixture-scope-nonactual.json'
FIXTURE_SHA = 'ea09e5cac060682a7a06d53fd19d11d618d331b2cf49dce057a7b31273854859'
# Root-emitted real Go final-branch SYNTHETIC vector, not511-window acquisition.
GO_FINAL_VECTOR_RAW = b'{\n  "actual_financial_claims": 0,\n  "actual_model_JEV_calls": 0,\n  "checkpoint_sha256": "0ad3bfcd18242c4c3f3212c72adb3008bff536663c0578e7c82d904ae9e9463d",\n  "checkpoint_utf8": "{\\"v8\\":{\\"schema\\":\\"full-window-runtime-evidence.v8\\",\\"logical_model_calls\\":0,\\"logical_jev_calls\\":0,\\"native_model_intents\\":0,\\"native_jev_intents\\":0,\\"model_retry_intents\\":0,\\"jev_retry_intents\\":0,\\"native_settlements\\":0,\\"native_unknown\\":0,\\"native_pending\\":0,\\"retry_required\\":false,\\"halt\\":\\"\\",\\"configured_known\\":{\\"model_micro_cny\\":0,\\"jev_micro_usd\\":0},\\"unknown_held\\":{\\"model_micro_cny\\":0,\\"jev_micro_usd\\":0},\\"pending_held\\":{\\"model_micro_cny\\":0,\\"jev_micro_usd\\":0},\\"historical_permanent\\":{\\"model_micro_cny\\":52654504,\\"jev_micro_usd\\":408701},\\"base_permanent\\":{\\"model_micro_cny\\":123827032,\\"jev_micro_usd\\":2037474},\\"extra_permanent\\":{\\"model_micro_cny\\":0,\\"jev_micro_usd\\":0},\\"total_permanent\\":{\\"model_micro_cny\\":176481536,\\"jev_micro_usd\\":2446175},\\"unknown_intents\\":[],\\"extras\\":[]},\\"schema\\":\\"full-window-campaign-owned-checkpoint.v8\\",\\"index\\":511,\\"scope_sha256\\":\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\",\\"execution_sha256\\":\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\",\\"definition_sha256\\":\\"50848e347b985669e3424688f5f1e57b369d6c5577f3aa7e5de25e03144b7ebd\\",\\"schedule_sha256\\":\\"95469ad60b07de7f11eeaf16c638fbc8e9b677e708eb3b4f7247246535859ccf\\",\\"policy_sha256\\":\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\",\\"claim_sha256\\":\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\",\\"ledger_sha256\\":\\"cf6bcd34050cd0bf3e38155b5af2eba30eeb4112678addbe095ad9138061e6b2\\",\\"evidence\\":[{\\"Ordinal\\":3650,\\"Pass\\":3,\\"ObservedTurns\\":1,\\"Key\\":\\"plans-082/answer_complete\\",\\"Arm\\":\\"jev_selective\\",\\"RecordingSHA256\\":\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\",\\"Cleanup\\":true,\\"Observer\\":true},{\\"Ordinal\\":3650,\\"Pass\\":3,\\"ObservedTurns\\":1,\\"Key\\":\\"plans-082/answer_complete\\",\\"Arm\\":\\"model\\",\\"RecordingSHA256\\":\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\",\\"Cleanup\\":true,\\"Observer\\":true},{\\"Ordinal\\":3651,\\"Pass\\":3,\\"ObservedTurns\\":1,\\"Key\\":\\"plans-083/answer_complete\\",\\"Arm\\":\\"model\\",\\"RecordingSHA256\\":\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\",\\"Cleanup\\":true,\\"Observer\\":true},{\\"Ordinal\\":3651,\\"Pass\\":3,\\"ObservedTurns\\":1,\\"Key\\":\\"plans-083/answer_complete\\",\\"Arm\\":\\"jev_selective\\",\\"RecordingSHA256\\":\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\",\\"Cleanup\\":true,\\"Observer\\":true},{\\"Ordinal\\":3652,\\"Pass\\":3,\\"ObservedTurns\\":1,\\"Key\\":\\"plans-084/answer_complete\\",\\"Arm\\":\\"jev_selective\\",\\"RecordingSHA256\\":\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\",\\"Cleanup\\":true,\\"Observer\\":true},{\\"Ordinal\\":3652,\\"Pass\\":3,\\"ObservedTurns\\":1,\\"Key\\":\\"plans-084/answer_complete\\",\\"Arm\\":\\"model\\",\\"RecordingSHA256\\":\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\",\\"Cleanup\\":true,\\"Observer\\":true},{\\"Ordinal\\":3653,\\"Pass\\":3,\\"ObservedTurns\\":1,\\"Key\\":\\"plans-085/answer_complete\\",\\"Arm\\":\\"model\\",\\"RecordingSHA256\\":\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\",\\"Cleanup\\":true,\\"Observer\\":true},{\\"Ordinal\\":3653,\\"Pass\\":3,\\"ObservedTurns\\":1,\\"Key\\":\\"plans-085/answer_complete\\",\\"Arm\\":\\"jev_selective\\",\\"RecordingSHA256\\":\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\",\\"Cleanup\\":true,\\"Observer\\":true},{\\"Ordinal\\":3654,\\"Pass\\":3,\\"ObservedTurns\\":1,\\"Key\\":\\"plans-086/answer_complete\\",\\"Arm\\":\\"jev_selective\\",\\"RecordingSHA256\\":\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\",\\"Cleanup\\":true,\\"Observer\\":true},{\\"Ordinal\\":3654,\\"Pass\\":3,\\"ObservedTurns\\":1,\\"Key\\":\\"plans-086/answer_complete\\",\\"Arm\\":\\"model\\",\\"RecordingSHA256\\":\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\",\\"Cleanup\\":true,\\"Observer\\":true},{\\"Ordinal\\":3655,\\"Pass\\":3,\\"ObservedTurns\\":1,\\"Key\\":\\"plans-087/answer_complete\\",\\"Arm\\":\\"model\\",\\"RecordingSHA256\\":\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\",\\"Cleanup\\":true,\\"Observer\\":true},{\\"Ordinal\\":3655,\\"Pass\\":3,\\"ObservedTurns\\":1,\\"Key\\":\\"plans-087/answer_complete\\",\\"Arm\\":\\"jev_selective\\",\\"RecordingSHA256\\":\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\",\\"Cleanup\\":true,\\"Observer\\":true},{\\"Ordinal\\":3656,\\"Pass\\":3,\\"ObservedTurns\\":1,\\"Key\\":\\"plans-088/answer_complete\\",\\"Arm\\":\\"jev_selective\\",\\"RecordingSHA256\\":\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\",\\"Cleanup\\":true,\\"Observer\\":true},{\\"Ordinal\\":3656,\\"Pass\\":3,\\"ObservedTurns\\":1,\\"Key\\":\\"plans-088/answer_complete\\",\\"Arm\\":\\"model\\",\\"RecordingSHA256\\":\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\",\\"Cleanup\\":true,\\"Observer\\":true},{\\"Ordinal\\":3657,\\"Pass\\":3,\\"ObservedTurns\\":1,\\"Key\\":\\"plans-089/answer_complete\\",\\"Arm\\":\\"model\\",\\"RecordingSHA256\\":\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\",\\"Cleanup\\":true,\\"Observer\\":true},{\\"Ordinal\\":3657,\\"Pass\\":3,\\"ObservedTurns\\":1,\\"Key\\":\\"plans-089/answer_complete\\",\\"Arm\\":\\"jev_selective\\",\\"RecordingSHA256\\":\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\",\\"Cleanup\\":true,\\"Observer\\":true},{\\"Ordinal\\":3658,\\"Pass\\":3,\\"ObservedTurns\\":1,\\"Key\\":\\"plans-090/answer_complete\\",\\"Arm\\":\\"jev_selective\\",\\"RecordingSHA256\\":\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\",\\"Cleanup\\":true,\\"Observer\\":true},{\\"Ordinal\\":3658,\\"Pass\\":3,\\"ObservedTurns\\":1,\\"Key\\":\\"plans-090/answer_complete\\",\\"Arm\\":\\"model\\",\\"RecordingSHA256\\":\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\",\\"Cleanup\\":true,\\"Observer\\":true},{\\"Ordinal\\":3659,\\"Pass\\":3,\\"ObservedTurns\\":1,\\"Key\\":\\"plans-091/answer_complete\\",\\"Arm\\":\\"model\\",\\"RecordingSHA256\\":\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\",\\"Cleanup\\":true,\\"Observer\\":true},{\\"Ordinal\\":3659,\\"Pass\\":3,\\"ObservedTurns\\":1,\\"Key\\":\\"plans-091/answer_complete\\",\\"Arm\\":\\"jev_selective\\",\\"RecordingSHA256\\":\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\",\\"Cleanup\\":true,\\"Observer\\":true},{\\"Ordinal\\":3660,\\"Pass\\":3,\\"ObservedTurns\\":1,\\"Key\\":\\"plans-092/answer_complete\\",\\"Arm\\":\\"jev_selective\\",\\"RecordingSHA256\\":\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\",\\"Cleanup\\":true,\\"Observer\\":true},{\\"Ordinal\\":3660,\\"Pass\\":3,\\"ObservedTurns\\":1,\\"Key\\":\\"plans-092/answer_complete\\",\\"Arm\\":\\"model\\",\\"RecordingSHA256\\":\\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\",\\"Cleanup\\":true,\\"Observer\\":true}]}\\n",\n  "claim512_absent": true,\n  "evidence_kind": "synthetic_final_private_advance_branch_not_full511_acquisition",\n  "index": 511,\n  "next_claim_sha256": "0ad3bfcd18242c4c3f3212c72adb3008bff536663c0578e7c82d904ae9e9463d"\n}'


class Boundaries(unittest.TestCase):
    def setUp(self):
        p = HERE/'full1220-v9-window.py'
        self.assertTrue(p.exists(), 'V9 verifier missing')
        self.m = types.ModuleType('v9_window_test'); self.m.__file__ = str(p)
        exec(compile(p.read_bytes(), str(p), 'exec'), self.m.__dict__)
        raw = FIXTURE.read_bytes()
        self.assertEqual(hashlib.sha256(raw).hexdigest(), FIXTURE_SHA)
        self.scope = json.loads(raw)
        self.n = self.m.dependency('native')

    def check(self, scope):
        return self.m._scope(scope, self.n.wire(scope), self.n)

    def test_genuine_go_data_fixture_full511_greedy_schedule(self):
        self.check(self.scope)
        self.assertEqual(len(self.scope['windows']), 511)
        self.assertEqual(sum(2*s['user_turns'] for s in self.scope['slots']), 11772)
        self.assertTrue(any(w['last_ordinal']-w['first_ordinal']+1 < 12 for w in self.scope['windows'][:-1]))

    def test_not_uniform_twelve_pairs_and_tampered_schedule_rejected(self):
        scope = copy.deepcopy(self.scope)
        for i, slot in enumerate(scope['slots']): slot['window_id'] = f'window-{i//12+1:03d}'
        scope['schedule_sha256'] = hashlib.sha256(self.n.wire(scope['slots'])).hexdigest()
        with self.assertRaises(ValueError): self.check(scope)

    def test_old_scope_parent_caps_or_claim_history_cannot_rebind(self):
        for field, value in [('schema', 'full-window-scope.v8'), ('width', 28),
                             ('baseline_evidence_sha256', '2a78a0188ffd5072f75aeb51c9d9d54c5fdd46edc18a521571aa8aaccf38e1df'),
                             ('budget_declaration_sha256', 'ca7a9c43141a0fc416b24f019567a489935d8988db155fd0f56bc87f5014d725')]:
            with self.subTest(field=field), self.assertRaises(ValueError):
                scope = copy.deepcopy(self.scope); scope[field] = value; self.check(scope)
        scope = copy.deepcopy(self.scope); scope['windows'][0]['model_cap_micro_cny'] += 1
        with self.assertRaises(ValueError): self.check(scope)

    def test_no_denominator_or_order_reduction(self):
        for mutate in (lambda s:s['slots'].pop(), lambda s:s['windows'].pop(),
                       lambda s:s['slots'][1].update(case_key=s['slots'][0]['case_key']),
                       lambda s:s['slots'][0].update(arm_order=list(reversed(s['slots'][0]['arm_order']))),
                       lambda s:s['slots'][0].update(user_turns=True)):
            scope = copy.deepcopy(self.scope); mutate(scope)
            scope['schedule_sha256'] = hashlib.sha256(self.n.wire(scope['slots'])).hexdigest()
            with self.assertRaises(ValueError): self.check(scope)

    def test_default_unfrozen_never_reads_settings_or_runs_helper(self):
        class Forbidden:
            def __getattr__(self, key): raise AssertionError('inactive touched IO')
        self.m.os = Forbidden(); self.m.Path = Forbidden()
        self.assertEqual(self.m.main([]), 0)
        with self.assertRaisesRegex(ValueError, 'UNFROZEN'):
            self.m.verify_actual({'manifest': 'untrusted'})

    def test_json_cannot_mint_source_actual_or_window_proof(self):
        with self.assertRaises(ValueError): self.m._SourceProof(object(), {})
        with self.assertRaises(ValueError): self.m._ActualContext(object(), None, None, None, None, None)
        with self.assertRaises(ValueError): self.m._VerifiedWindow(object(), {}, {}, None)
        with self.assertRaises(ValueError): self.m.check_verified_window({'verified': True}, {})

    def test_source_prebuilt_default_and_json_context_rejected(self):
        with self.assertRaisesRegex(ValueError, 'UNFROZEN'):
            self.m.verify_source_v9(b'', b'', {}, {}, {}, source_root=object(), source_hashes=object(), output_dir=object(), execute=True)
        with self.assertRaises(ValueError):
            self.m.verify_source_v9(b'', b'', {}, {}, {}, source_root=object(), source_hashes=object(), output_dir=object(), context={'verified': True}, execute=True)

    def test_quality_empty_path_never_loads_runtime(self):
        def forbidden(*a, **kw): raise AssertionError('zero-quality touched helper')
        self.m.dependency = forbidden
        self.m.Evidence = forbidden
        self.assertIsNone(self.m.verify_quality_v9(b'bad', b'{"rows":[]}', {}, {}, {}, source_root=object(), source_hashes=object(), output_dir=object()))

    def test_technical_audit_delegation_metadata_is_not_exact_human_scope_review(self):
        m=self.m
        m.FROZEN_COMMIT='1'*40;m.FROZEN_TREE='2'*40;m.FROZEN_SOURCE_SHA256='3'*64
        m.FROZEN_MANIFEST_SHA256='4'*64;m.FROZEN_BINARY_SHA256='5'*64;m.FROZEN_HELPER_CATALOG_SHA256='6'*64
        manifest={'scope_sha256':'7'*64,'execution_sha256':'8'*64}
        binding={'verifier_sha256':'9'*64,'owner_sha256':'a'*64}
        artifacts={'actual-manifest.json':'4'*64,'technical-start-audit.json':'b'*64,'helper-build/catalog.json':'6'*64}
        audit={'schema':'full1220-v9-technical-start-audit.v1','technical_checks_passed':True,'confirmed_P1_P2':0,
            'actual_execution_authorized':False,'human_reviewed_exact_scope':False,'delegated_authorization_required':True,
            'archive_commit':'1'*40,'archive_tree':'2'*40,'source_sha256':'3'*64,'manifest_sha256':'4'*64,
            'binary_sha256':'5'*64,'scope_sha256':'7'*64,'execution_sha256':'8'*64,'verifier_sha256':'9'*64,
            'owner_sha256':'a'*64,'helper_catalog_sha256':'6'*64,
            'artifact_sha256':{'actual-manifest.json':'4'*64,'helper-build/catalog.json':'6'*64}}
        m._validate_technical_audit(audit,manifest,binding,artifacts,self.n)
        for field,value in [('human_approval_required',False),('human_reviewed_exact_scope',True),
                            ('delegated_authorization_required',False),('actual_execution_authorized',True),
                            ('helper_catalog_sha256','0'*64)]:
            with self.subTest(field=field),self.assertRaises(ValueError):
                m._validate_technical_audit({**audit,field:value},manifest,binding,artifacts,self.n)

    def test_fixed46_firstbase_checkpoint_nextbase_and_extra_runtime(self):
        m=self.m;n=self.n
        first=n.amounts(228487,4383);nextbase=n.amounts(479073,8603)
        model_extra={'amount':n.amounts(74752,0)};jev_extra={'amount':n.amounts(0,2688)}
        self.assertEqual(m._permanent(first,[],n),n.amounts(52882991,413084))
        self.assertEqual(m._permanent(nextbase,[],n),n.amounts(53133577,417304))
        self.assertEqual(m._permanent(first,[model_extra],n),n.amounts(52957743,413084))
        self.assertEqual(m._permanent(nextbase,[model_extra,jev_extra],n),n.amounts(53208329,419992))
        r={'contract':{'window_id':'window-001'},'logical_counts':{'model':0,'jev':0},'native_counts':{'model':0,'jev':0},
           'retry_counts':{'model':0,'jev':0},'settlements':{},'unknown_intents':[],'pending':None,'retry_required':None,
           'halt':'','known':{'model':0,'jev':0},'unknown_held':{'model':0,'jev':0},'pending_held':{'model':0,'jev':0}}
        checkpoint=m._runtime(r,first,[model_extra],n.amounts(52957743,413084),n)
        self.assertEqual(checkpoint['historical_permanent'],n.amounts(52654504,408701))
        self.assertEqual(checkpoint['total_permanent'],n.amounts(52957743,413084))
        after=m._runtime(r,nextbase,[model_extra,jev_extra],m._permanent(nextbase,[model_extra,jev_extra],n),n)
        self.assertEqual(after['total_permanent'],n.amounts(53208329,419992))
        self.assertFalse(n.same(after['historical_permanent'],n.amounts(51200203,399529)))
        self.assertFalse(n.same(m._permanent(first,[],n),n.amounts(51428690,403912)))

    def test_original_records_top_namespace_rechecked_after_helper(self):
        import tempfile
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp).resolve();records=root/'records';records.mkdir()
            (records/'startup.json').write_text('{}');(records/'window-000001').mkdir();(records/'window-000001-summary.json').write_text('{}')
            original=frozenset(('startup.json','window-000001','window-000001-summary.json'))
            captured=self.m._records_top_marker(root,original)
            for name in ('window-000002','foreign-sibling'):
                (records/name).mkdir()
                with self.assertRaises(ValueError):self.m._records_top_marker(root,original,captured)
                (records/name).rmdir()
            self.m._records_top_marker(root,original,captured)
            (records/'window-000001').rmdir();(records/'window-000001').write_text('wrong type')
            with self.assertRaises(ValueError):self.m._records_top_marker(root,original,captured)

    def test_final511_advance_token_is_checkpoint_without_claim512(self):
        raw=b'{"schema":"synthetic-final-checkpoint","index":511}\n'
        digest=hashlib.sha256(raw).hexdigest();claims=['c'*64]*511
        self.m._verify_advance_token(511,digest,claims,raw)
        for token in ('','c'*64,hashlib.sha256(raw+b' ').hexdigest()):
            with self.subTest(token=token),self.assertRaises(ValueError):
                self.m._verify_advance_token(511,token,claims,raw)
        with self.assertRaises(ValueError):self.m._verify_advance_token(511,digest,claims+['d'*64],raw)

    def test_nonfinal_advance_token_remains_next_reserved_claim(self):
        raw=b'{"schema":"synthetic-checkpoint","index":510}\n';cp=hashlib.sha256(raw).hexdigest()
        claims=['a'*64]*510+['b'*64]
        self.m._verify_advance_token(510,'b'*64,claims,raw)
        self.m._verify_advance_token(1,'b'*64,['a'*64,'b'*64],raw)
        for token in ('',cp,'a'*64):
            with self.subTest(token=token),self.assertRaises(ValueError):self.m._verify_advance_token(510,token,claims,raw)
        with self.assertRaises(ValueError):self.m._verify_advance_token(510,'b'*64,claims[:-1],raw)

    def test_root_actual_go_synthetic_finalbranch_vector(self):
        self.assertEqual(hashlib.sha256(GO_FINAL_VECTOR_RAW).hexdigest(),'d0a67422ba99b47accf2d9bc187a64e2e8da7895ceba18ca63dd5a7b4f1c7be1')
        vector=json.loads(GO_FINAL_VECTOR_RAW)
        self.assertEqual(vector['evidence_kind'],'synthetic_final_private_advance_branch_not_full511_acquisition')
        self.assertEqual(vector['actual_model_JEV_calls'],0);self.assertEqual(vector['actual_financial_claims'],0)
        self.assertIs(vector['claim512_absent'],True);self.assertEqual(vector['index'],511)
        checkpoint_raw=vector['checkpoint_utf8'].encode()
        self.assertEqual(hashlib.sha256(checkpoint_raw).hexdigest(),vector['checkpoint_sha256'])
        self.assertEqual(vector['next_claim_sha256'],vector['checkpoint_sha256'])
        # Digest prefix is component data only; no claims/proofs are minted.
        self.m._verify_advance_token(511,vector['next_claim_sha256'],['c'*64]*511,checkpoint_raw)
        with self.assertRaises(ValueError):self.m._verify_advance_token(511,'',['c'*64]*511,checkpoint_raw)


if __name__ == '__main__': unittest.main()
