"""Independent data replay of pinned, real Go-emitted synthetic native bundles."""
import sys
sys.dont_write_bytecode=True
import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import unittest

ROOT=Path(__file__).parent
PINS={'model-direct':'3fbce0b6e9b01e560594d0d1f7f885c4c7db5383cdba8695c5b0ca88c413b03e','model-connect':'5a4e8cef9fa0d5632863e8ad4faf496c4cb6f8eaeaf881cd99862dc1ccf2f334','jev-direct':'85b8f50ca4e4b6030d5b7b3506c4d6d6f17f0f6c7ba7529a95e4f96d358d0233','jev-connect':'f80f3ccacea01c8783347b13347af124075d2472267a702626cfbb839b5c9235'}
def digest(b):return hashlib.sha256(b).hexdigest()
def wire(x):return json.dumps(x,ensure_ascii=False,separators=(',',':')).encode()
def fixture(name='model-direct'):
    raw=(ROOT/'fixtures/v8-native'/f'{name}.json').read_bytes()
    assert digest(raw)==PINS[name]
    return json.loads(raw)
def journal(v):return [json.loads(line) for line in v['ledger_utf8'].splitlines()]
def rechain(events):
    previous='';out=[]
    for i,e in enumerate(events):
        e['index']=i
        if i:e['previous_sha256']=previous
        raw=wire(e);out.append(raw);previous=digest(raw)
    return b'\n'.join(out)+b'\n'

class NativeTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        p=ROOT/'full1220-v8-native.py'
        if not p.exists():raise AssertionError('native replay not implemented')
        spec=importlib.util.spec_from_file_location('v8_native',p);cls.m=importlib.util.module_from_spec(spec);spec.loader.exec_module(cls.m)

    def test_go_ledgers_known_unknown_logical_and_open_state(self):
        for name in PINS:
            v=fixture(name);r=self.m.replay_ledger(v['ledger_utf8'].encode(),v['payload_manifest']['contract'])
            provider=name.split('-')[0]
            with self.subTest(name=name):
                self.assertEqual(r['native_counts'][provider],2)
                self.assertEqual(r['logical_counts'][provider],1)
                self.assertEqual(r['known'][provider],12 if provider=='model' else 1)
                self.assertEqual(r['unknown_held'][provider],74752 if provider=='model' else 2688)
                self.assertFalse(r['closed']);self.assertIsNone(r['pending']);self.assertIsNone(r['retry_required'])
                self.assertEqual(r['completion_proof'],'unavailable_from_journal')

    def test_ledger_mutations_rechained_still_rejected(self):
        mutations={
          'same logical body':lambda es:es[4]['request'].__setitem__('body_sha256','a'*64),
          'second native ordinal':lambda es:es[4]['request'].__setitem__('native_attempt',1),
          'new logical key':lambda es:es[4]['request'].__setitem__('attempt',2),
          'funding amount':lambda es:es[3].__setitem__('amount',74751),
          'funding provider':lambda es:es[3].__setitem__('provider','jev'),
          'funding unknown seq':lambda es:es[3].__setitem__('unknown_sequence',2),
          'unknown descriptor':lambda es:es[2].__setitem__('descriptor_sha256','no'),
          'duplicate funding':lambda es:es.insert(4,copy.deepcopy(es[3])),
          'missing funding':lambda es:es.pop(3),
          'second unknown':lambda es:es.__setitem__(5,{'descriptor_sha256':'a'*64,'kind':'unknown','index':5,'previous_sha256':'a'*64,'sequence':2}),
          'bad usage':lambda es:es[5]['usage'].__setitem__('cache_miss_tokens',1),
          'bool usage':lambda es:es[5]['usage'].__setitem__('input_tokens',True),
          'reorder':lambda es:es.__setitem__(slice(2,4),list(reversed(es[2:4]))),
          'unknown event':lambda es:es[2].__setitem__('kind','release'),
          'unknown field':lambda es:es[3].__setitem__('approved',True),
        }
        for name,mutate in mutations.items():
            v=fixture();es=journal(v);mutate(es)
            with self.subTest(name=name),self.assertRaises(ValueError):self.m.replay_ledger(rechain(es),v['payload_manifest']['contract'])

    def test_raw_chain_canonical_and_contract(self):
        v=fixture();raw=v['ledger_utf8'].encode();c=v['payload_manifest']['contract']
        for bad in (raw[:-1],raw.replace(b'"index":1',b'"index":2',1),raw.replace(b'"index":1',b'"index":1,"index":1',1),raw.replace(b'"kind":"unknown"',b'"kind": "unknown"')):
            with self.subTest(bad=bad[:80]),self.assertRaises(ValueError):self.m.replay_ledger(bad,c)
        changed=copy.deepcopy(c);changed['model']['max_attempts']+=1
        with self.assertRaises(ValueError):self.m.replay_ledger(raw,changed)

    def test_scanlines_lf_crlf_controls_and_bare_cr_rejected(self):
        v=fixture();raw=v['ledger_utf8'].encode();contract=v['payload_manifest']['contract']
        for line_ending in (b'\n',b'\r\n'):
            data=raw.replace(b'\n',line_ending)
            result=self.m.replay_ledger(data,contract)
            self.assertTrue(result['native_resolved'])
            self.assertFalse(result['closed'])
            self.assertEqual(result['journal_sha256'],digest(data))
        # Go ScanLines sees one token here, not one event per bare CR.
        bad=raw[:-1].replace(b'\n',b'\r')+b'\n'
        with self.assertRaises(ValueError):self.m.replay_ledger(bad,contract)
        # ScanLines drops at most one CR; a second one remains noncanonical.
        with self.assertRaises(ValueError):self.m.replay_ledger(raw.replace(b'\n',b'\r\r\n'),contract)

    def test_halt_pending_unknown_preserved_not_completed(self):
        v=fixture();c=v['payload_manifest']['contract'];es=journal(v)
        # Derived journal variants are explicit mutation cases, not Go positive fixtures.
        for prefix,unknown,pending in ((2,0,True),(3,74752,False),(5,74752,True)):
            part=copy.deepcopy(es[:prefix]);part.extend([{'kind':'halt','index':0,'previous_sha256':'','code':'upstream_failure'},{'kind':'close','index':0,'previous_sha256':''}])
            r=self.m.replay_ledger(rechain(part),c)
            self.assertTrue(r['closed']);self.assertEqual(r['halt'],'upstream_failure')
            self.assertEqual(r['pending'] is not None,pending);self.assertEqual(r['unknown_held']['model'],unknown)
            self.assertEqual(r['pending_held']['model'],74752 if pending else 0)
            self.assertFalse(r['native_resolved'])
        part=copy.deepcopy(es[:3]);part.append({'kind':'close','index':0,'previous_sha256':''})
        with self.assertRaises(ValueError):self.m.replay_ledger(rechain(part),c)

    def test_rounding_cache_missing_and_boundaries(self):
        c=fixture()['payload_manifest']['contract']
        self.assertEqual(self.m.settlement_cost(c['model'],{'input_tokens':3,'output_tokens':1,'cache_hit_tokens':1,'cache_miss_tokens':2}),13)
        self.assertEqual(self.m.settlement_cost(c['model'],{'input_tokens':3,'output_tokens':1}),14)
        self.assertEqual(self.m.settlement_cost(c['jev'],{'input_tokens':1,'output_tokens':0}),1)
        for u in ({'input_tokens':3,'output_tokens':1,'cache_hit_tokens':1},{'input_tokens':3,'output_tokens':129},{'input_tokens':None,'output_tokens':0}):
            with self.assertRaises(ValueError):self.m.settlement_cost(c['model'],u)

    def test_response_protocol_cache_metadata_and_unsigned_numbers(self):
        c=fixture()['payload_manifest']['contract']
        raw=b'{"model":"deepseek-flash","usage":{"input_tokens":2,"output_tokens":1,"cache_read_input_tokens":3,"cache_creation_input_tokens":4,"service_tier":"standard"}}'
        u,availability=self.m.normalize_usage('model',raw,c['model'])
        self.assertEqual(u,{'input_tokens':9,'output_tokens':1,'cache_hit_tokens':3,'cache_miss_tokens':6})
        self.assertEqual(availability,'reported_cache_read')
        self.assertEqual(self.m.settlement_cost(c['model'],u),21)
        for usage in ('{"input_tokens":-0,"output_tokens":1}', '{"input_tokens":1.0,"output_tokens":1}',
                      '{"input_tokens":1,"output_tokens":1,"input_tokens":2}',
                      '{"input_tokens":1,"output_tokens":1,"cache_read_input_tokens":null}',
                      '{"input_tokens":1,"output_tokens":1,"service_tier":"priority"}',
                      '{"input_tokens":36864,"output_tokens":1,"cache_creation_input_tokens":1}',
                      '{"input_tokens":1,"output_tokens":1,"future":0}'):
            with self.subTest(usage=usage),self.assertRaises(ValueError):
                self.m.normalize_usage('model',('{"model":"deepseek-flash","usage":'+usage+'}').encode(),c['model'])

    def test_admission_after_unknown_cannot_use_original_base_again(self):
        v=fixture();es=journal(v);es[0]['contract']['permanent_model_micro_cny']=74752
        req=copy.deepcopy(es[1]['request']);req['attempt']=2
        es.append({'kind':'intent','index':6,'previous_sha256':'','sequence':3,'request':req})
        with self.assertRaises(ValueError):self.m.replay_ledger(rechain(es),es[0]['contract'])

    def test_payload_nested_go_order_is_not_caller_selected(self):
        v=fixture();body=v['payload_manifest']['packets'][1]['response']['body']
        v['payload_manifest']['packets'][1]['response']['body']=dict(reversed(list(body.items())))
        desc=json.loads(v['payload_files_utf8']['response-000002.json'])
        desc['response']['body']=dict(reversed(list(body.items())))
        v['payload_files_utf8']['response-000002.json']=wire(desc).decode()
        raw=wire(v)
        with self.assertRaises(ValueError):self.m.replay_native_bundle(raw,digest(raw))

    def test_real_go_bundles_data_only_and_no_fake_close(self):
        for name,pin in PINS.items():
            raw=(ROOT/'fixtures/v8-native'/f'{name}.json').read_bytes()
            r=self.m.replay_native_bundle(raw,pin)
            with self.subTest(name=name):
                self.assertTrue(r['native_resolved']);self.assertFalse(r['closed'])
                self.assertFalse(r['execution_authority']);self.assertFalse(r['owned_completion_proved'])
                self.assertEqual(r['evidence_kind'],'synthetic_controlled_fixture_not_actual_proof')
                self.assertEqual(r['native_packets'],2)
                self.assertEqual(r['raw_cache_availability']['unknown_raw_cache_read'],1 if name.startswith('model') else 0)
                with self.assertRaises(ValueError):self.m.replay_native_bundle(raw,pin,require_closed=True)

    def test_bundle_raw_and_join_mutations(self):
        mutations={
          'raw response':lambda v:v['payload_files_utf8'].__setitem__('response-000002.body','{}'),
          'raw request':lambda v:v['payload_files_utf8'].__setitem__('request-000001.body','{}'),
          'missing packet':lambda v:v['payload_manifest']['packets'].pop(0),
          'missing file':lambda v:v['payload_files_utf8'].pop('response-000001.json'),
          'orphan file':lambda v:v['payload_files_utf8'].__setitem__('orphan','x'),
          'claim duplicate':lambda v:v['extra_claims'].append(copy.deepcopy(v['extra_claims'][0])),
          'claim amount':lambda v:v['extra_claims'][0]['amount'].__setitem__('model_micro_cny',74751),
          'raw claim newline':lambda v:v['extra_claim_files_utf8'].__setitem__('extra-000001.json',v['extra_claim_files_utf8']['extra-000001.json'].rstrip()),
          'raw auth newline':lambda v:v.__setitem__('authorization_utf8',v['authorization_utf8']+'\n'),
          'scope':lambda v:v['payload_manifest']['contract'].__setitem__('scope_sha256','a'*64),
          'unknown release':lambda v:v['runtime_summary']['unknown_held'].__setitem__('model_micro_cny',0),
          'native logical conflation':lambda v:v['runtime_summary'].__setitem__('logical_model_calls',2),
          'known cost':lambda v:v['runtime_summary']['configured_known'].__setitem__('model_micro_cny',13),
          'extra permanent':lambda v:v['runtime_summary']['extra_permanent'].__setitem__('model_micro_cny',0),
          'retry required':lambda v:v['runtime_summary'].__setitem__('retry_required',True),
          'fake completion':lambda v:v['payload_manifest'].__setitem__('completion_proof','verified'),
          'failure as success':lambda v:v['payload_manifest']['packets'][0]['response'].__setitem__('status',200),
        }
        for name,mutate in mutations.items():
            v=fixture();mutate(v);raw=wire(v)
            with self.subTest(name=name),self.assertRaises(ValueError):self.m.replay_native_bundle(raw,digest(raw))
        v=fixture();raw=wire(v)
        with self.assertRaises(ValueError):self.m.replay_native_bundle(raw,'a'*64)

if __name__=='__main__':unittest.main()
