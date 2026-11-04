"""Controlled V12 boundaries; synthetic DATA is never an actual proof."""
import copy
import json
from pathlib import Path
import types
import unittest

HERE = Path(__file__).resolve().parents[1]

def load(name):
    p = HERE / ('full1220-v12-'+name+'.py')
    if not p.exists(): raise AssertionError('V12 '+name+' implementation missing')
    m = types.ModuleType('test_v12_'+name.replace('-', '_')); m.__file__ = str(p)
    exec(compile(p.read_bytes(), str(p), 'exec'), m.__dict__)
    return m

def scope(m):
    s=json.loads((HERE/'fixtures/v9-contracts/fixture-scope-nonactual.json').read_bytes())
    v=json.loads((HERE/'fixtures/v12-contracts/go-declarations.json').read_bytes())
    s['schema']='full-window-scope.v12';s['baseline_evidence_sha256']=m.BASELINE
    s['budget_declaration']=json.loads(v['budget']['canonical']);s['budget_declaration_sha256']=v['budget']['sha256']
    s['retry_declaration']=json.loads(v['retry']['canonical']);s['retry_policy_sha256']=v['retry']['sha256']
    s['completion_v12']=json.loads(v['completion']['canonical']);s['windows']=s['windows'][55:]
    s['observation']={'schema':'full-window-observation-declaration.v11','private_settings_sha256':'a'*64,'spool_root':'/controlled/spool','spool_run_id':'b'*64,'spool_max_bytes':4<<30,'observation_receipt_boundary':'durable_local','observer_identity_sha256':'c'*64}
    return s

class V12Boundaries(unittest.TestCase):
    def setUp(self): self.m=load('window');self.n=self.m.dependency('native')
    def test_full_original_schedule_and_mapping(self):
        s=scope(self.m);self.m._scope(s,self.n.wire(s),self.n)
        self.assertEqual(self.m.execution_totals(s),{'Pairs':3275,'Arms':6550,'DefinedUserTurns':10504,'Boundaries':0,'ModelCap':110191257,'JEVCap':1818872})
        for i in (1,55,56,456):self.assertEqual(self.m.execution_window_at(s,i)['id'],f'window-{i+55:03d}')
    def test_old_reduced_reordered_and_composed_rejected(self):
        for mutate in (lambda s:s.update(schema='full-window-scope.v10'),lambda s:s['windows'].pop(),lambda s:s['slots'].pop(),lambda s:s.update(completion={}),lambda s:s['slots'].reverse(),lambda s:s.pop('observation')):
            s=scope(self.m);mutate(s)
            with self.assertRaises((ValueError,KeyError)):self.m._scope(s,self.n.wire(s),self.n)
    def test_no_json_or_foreign_proof(self):
        for value in ({'verified':True},None,object()):
            with self.assertRaises(ValueError):self.m.window_proof(value,1)
            with self.assertRaises(ValueError):self.m.check_verified_cohort(value)
        with self.assertRaises(ValueError):self.m.VerifiedV12Cohort(object(),None,None,None)
    def test_unfrozen_before_paths(self):
        with self.assertRaisesRegex(ValueError,'UNFROZEN'):self.m.verify_actual_cohort(object())
    def test_live_durable_is_not_remote_or_normal(self):
        i={'spool_run_id':'b'*64,'spool_root_sha256':'c'*64,'spool_max_bytes':4<<30}
        c={'Accepted':2,'Persisted':2,'Exported':0,'Dropped':0,'Failed':0,'QueueDepth':0,'Turns':1,'SampledTurns':1,'Batches':1}
        o={'observation_receipt_boundary':'durable_local','remote_delivery':'pending','normal_closure':False,'spool':{'run_id':'b'*64,'root_sha256':'c'*64,'max_bytes':4<<30,'spans':2,'batches':1,'bytes':1,'stored_bytes':2,'last_link_sha256':'d'*64,'closed':False,'poisoned':False,'data_only':False,'durability_return_unproven':False,'owner_completion_unproven':False}}
        self.m.validate_observation_receipt(o,c,i,final=False)
        with self.assertRaises(ValueError):self.m.validate_observation_receipt(o,c,i,final=True)
        for field,value in [('Exported',2),('Failed',1),('Persisted',1),('Accepted',True)]:
            bad=dict(c);bad[field]=value
            with self.assertRaises(ValueError):self.m.validate_observation_receipt(o,bad,i,final=False)
        for field,value in [('remote_delivery','verified'),('normal_closure',True)]:
            bad=copy.deepcopy(o);bad[field]=value
            with self.assertRaises(ValueError):self.m.validate_observation_receipt(bad,c,i,final=False)

if __name__=='__main__':unittest.main()

class FinalDataBoundaries(unittest.TestCase):
    def setUp(self):self.m=load('window')
    def test_spool_exact_full_trace_parent_and_data_boundary(self):
        receipts=[{'rows':[{'trace_id':f'{i:032x}'} for i in range(1,6551)]}]
        spans=[{'trace_id':f'{i:032x}','span_id':f'{i:016x}','parent_span_id':'','start_time_unix_nano':1,'end_time_unix_nano':2} for i in range(1,6551)]
        live={'spans':6550,'data_only':False,'durability_return_unproven':False,'owner_completion_unproven':False}
        data={'summary':{**live,'data_only':True,'durability_return_unproven':True,'owner_completion_unproven':True},'spans':spans}
        self.assertEqual(len(self.m.validate_spool_coverage(data,live,receipts)),6550)
        for change in ('missing','duplicate','foreign','parent','timestamp','json_live'):
            bad=copy.deepcopy(data)
            if change=='missing':bad['spans'].pop()
            elif change=='duplicate':bad['spans'][-1]=copy.deepcopy(bad['spans'][0])
            elif change=='foreign':bad['spans'][0]['trace_id']='f'*32
            elif change=='parent':bad['spans'][0]['parent_span_id']='f'*16
            elif change=='timestamp':bad['spans'][0]['start_time_unix_nano']=3
            else:bad['summary']['data_only']=False
            with self.subTest(change=change),self.assertRaises(ValueError):self.m.validate_spool_coverage(bad,live,receipts)
    def test_closed_snapshot_rejects_changed_extra_missing_alias_and_hardlink(self):
        import tempfile,os
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp).resolve();(root/'raw').write_bytes(b'original')
            reader,files=self.m._snapshot(root);self.assertEqual(files,{'raw':b'original'})
            self.m._fresh_snapshot(reader)
            (root/'extra').write_bytes(b'extra')
            with self.assertRaises(ValueError):self.m._fresh_snapshot(reader)
            (root/'extra').unlink();(root/'raw').write_bytes(b'changed')
            with self.assertRaises(ValueError):self.m._fresh_snapshot(reader)
            os.link(root/'raw',root/'link')
            with self.assertRaises(ValueError):self.m._snapshot(root)
    def test_fake_live_window_never_satisfies_final_projection(self):
        w=self.m._VerifiedWindow(self.m._WINDOW_SEAL,{}, {'final_normal_cohort':False},None)
        with self.assertRaises(ValueError):self.m.check_verified_window(w,{})

class SnapshotScaleBoundaries(unittest.TestCase):
    """Synthetic namespace entries, never a large tree or an actual proof."""
    def setUp(self):
        self.m=load('window')

    def virtual_inventory(self, count, limits, *, spool=False):
        import stat
        from unittest.mock import patch
        from contextlib import contextmanager
        root=Path('/synthetic-v12-snapshot')
        reader=object.__new__(self.m.Evidence);reader.root=root
        reader._root_check=lambda:None
        reader._inventory_limits=limits;reader._spool_snapshot=spool
        def names():
            if spool:
                yield from ('lock','header.json','closed.json')
                for i in range(1,(count-3)//2+1):
                    yield f'{i:020d}.pb';yield f'{i:020d}.commit'
            else:
                for i in range(count):yield f'leaf-{i}'
        expected=set(names())
        def info(path):
            size=(64 if Path(path).name=='lock' else 457) if spool else 1
            return types.SimpleNamespace(st_mode=stat.S_IFREG|0o600,st_nlink=1,st_size=size)
        @contextmanager
        def scan(path):
            yield (types.SimpleNamespace(path=str(root/name),name=name,stat=lambda name=name,**kw:info(root/name)) for name in names())
        with patch.object(Path,'iterdir',lambda p:(root/name for name in names())),patch.object(Path,'lstat',info),patch.object(self.m.os,'scandir',scan):
            reader.inventory(expected)
        return reader

    def test_records_permitted_over_old_ceiling(self):
        # The inherited 100000 ceiling must not reject this permitted count.
        try:self.virtual_inventory(100001,(253636,912,None))
        except ValueError as exc:self.fail('permitted full records namespace rejected: '+str(exc))

    def test_spool_permitted_49999_batches_over_old_ceiling(self):
        try:self.virtual_inventory(100001,(100001,0,4294967296),spool=True)
        except ValueError as exc:self.fail('permitted closed spool namespace rejected: '+str(exc))

    def test_frozen_domain_arithmetic(self):
        self.assertTrue(hasattr(self.m,'_records_limits'),'frozen domain limit derivation missing')
        s=scope(self.m)
        self.assertEqual(self.m._records_limits(s),(253636,912,None))
        self.assertEqual(self.m._records_limits(s,1),(678,1,None))
        self.assertEqual(self.m._campaign_limits(),(1865,456,None))
        entries,dirs,total=self.m._spool_limits()
        self.assertEqual(dirs,0);self.assertEqual(total,4294967296)
        self.assertGreater(entries,100001)

    def test_default_evidence_ceiling_stays_strict(self):
        with self.assertRaisesRegex(ValueError,'inventory bound'):
            self.virtual_inventory(100001,(100000,100000,None))

    def test_each_domain_rejects_overbound_during_discovery_before_read(self):
        import tempfile
        from unittest.mock import patch
        # Small projected boundaries exercise the same scanner without huge files.
        for domain,limits in [('records',(4,2,None)),('campaign',(3,1,None)),('spool',(3,0,4294967296)),('window',(2,1,None))]:
            with self.subTest(domain=domain),tempfile.TemporaryDirectory() as tmp:
                root=Path(tmp).resolve()
                for i in range(limits[0]+1):(root/f'leaf-{i}').write_bytes(b'raw')
                reader=self.m.Evidence(root);reader._inventory_limits=limits;reader._spool_snapshot=False
                reads=[];original=reader.read
                def read(*args,**kwargs):reads.append(args[0]);return original(*args,**kwargs)
                reader.read=read
                with patch.object(self.m,'Evidence',return_value=reader):
                    with self.assertRaisesRegex(ValueError,'inventory bound'):self.m._snapshot(root)
                self.assertEqual(reads,[],'oversized namespace read before bound rejection')

    def test_each_domain_rejects_orphan_directories_and_rechecks_growth(self):
        import tempfile
        from unittest.mock import patch
        for domain,limits in [('records',(4,2,None)),('campaign',(3,1,None)),('spool',(3,0,None)),('window',(2,1,None))]:
            with self.subTest(domain=domain),tempfile.TemporaryDirectory() as tmp:
                root=Path(tmp).resolve();(root/'raw').write_bytes(b'original')
                reader=self.m.Evidence(root);reader._inventory_limits=limits;reader._spool_snapshot=False
                with patch.object(self.m,'Evidence',return_value=reader):reader,_=self.m._snapshot(root)
                (root/'orphan').mkdir()
                with self.assertRaises(ValueError):self.m._fresh_snapshot(reader)
                with self.assertRaises(ValueError):self.m._fresh_closed_identity(reader)
                (root/'orphan').rmdir()
                for i in range(limits[0]):(root/f'extra-{i}').write_bytes(b'x')
                with self.assertRaisesRegex(ValueError,'inventory bound'):self.m._fresh_snapshot(reader)
                with self.assertRaisesRegex(ValueError,'inventory bound'):self.m._fresh_closed_identity(reader)

    def test_directory_budget_counts_visited_directories(self):
        import tempfile
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp).resolve();(root/'one').mkdir();(root/'two').mkdir()
            for d in ('one','two'):(root/d/'raw').write_bytes(b'raw')
            reader=self.m.Evidence(root);reader._inventory_limits=(10,1,None);reader._spool_snapshot=False
            with self.assertRaisesRegex(ValueError,'inventory directory bound'):reader.inventory({'one/raw','two/raw'})

    def test_spool_byte_cap_rejects_before_body_reads(self):
        import tempfile
        from unittest.mock import patch
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp).resolve();(root/'lock').write_bytes(b'x'*64);(root/'header.json').write_bytes(b'x'*64)
            reader=self.m.Evidence(root);reader._inventory_limits=(100001,0,100);reader._spool_snapshot=False
            with patch.object(self.m,'Evidence',return_value=reader),patch.object(reader,'read',side_effect=AssertionError('body read before byte bound')):
                with self.assertRaisesRegex(ValueError,'inventory byte bound'):self.m._snapshot(root)

    def test_spool_malformed_small_and_foreign_leaves_rejected_during_discovery(self):
        import tempfile
        from unittest.mock import patch
        for name,raw in [('00000000000000000001.pb',b''),('00000000000000000001.commit',b'{}'),('foreign',b'x'*457)]:
            with self.subTest(name=name),tempfile.TemporaryDirectory() as tmp:
                root=Path(tmp).resolve();(root/name).write_bytes(raw)
                reader=self.m.Evidence(root);reader._inventory_limits=(100001,0,4294967296);reader._spool_snapshot=True
                with patch.object(self.m,'Evidence',return_value=reader),patch.object(reader,'read',side_effect=AssertionError('malformed spool read')):
                    with self.assertRaisesRegex(ValueError,'spool namespace|spool leaf'):self.m._snapshot(root)

    def test_exact_helper_batch_limit_tightens_original_snapshot(self):
        import tempfile
        self.assertTrue(hasattr(self.m,'_tighten_spool_snapshot'),'verified exact spool bound missing')
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp).resolve()
            for name in ('lock','header.json','closed.json','00000000000000000001.pb','00000000000000000001.commit'):(root/name).write_bytes(b'x'*(64 if name=='lock' else 457))
            reader,_=self.m._snapshot(root,limits=self.m._spool_limits(),spool=True)
            original_markers=copy.deepcopy(reader.markers)
            self.m._tighten_spool_snapshot(reader,{'batches':1,'spans':1,'bytes':457,'stored_bytes':2112})
            self.assertEqual(reader._inventory_limits,(5,0,4294967296));self.assertEqual(reader.markers,original_markers)
            self.m._fresh_snapshot(reader);self.m._fresh_closed_identity(reader)
            (root/'00000000000000000002.pb').write_bytes(b'x'*457)
            with self.assertRaisesRegex(ValueError,'inventory bound'):self.m._fresh_snapshot(reader)
            with self.assertRaisesRegex(ValueError,'inventory bound'):self.m._fresh_closed_identity(reader)
            with self.assertRaises(ValueError):self.m._tighten_spool_snapshot(reader,{'batches':0,'spans':1,'bytes':457,'stored_bytes':2112})

    def test_actual_entry_selects_each_frozen_domain_before_replay(self):
        """UNIT MOCK gate wiring only; no context/proof/helper is manufactured."""
        from unittest.mock import patch
        s=scope(self.m);root=Path('/synthetic-v12-cohort');calls=[]
        context=types.SimpleNamespace(authority_raw=b'synthetic',manifest={'scope':s,'spool_root':str(root/'spool')},reader=None,root=root)
        class Done(Exception):pass
        def snapshot(path,**kwargs):
            calls.append((path,kwargs))
            if len(calls)==3:raise Done
            return None,{}
        with patch.object(self.m,'_selected'),patch.object(self.m,'load_freeze_binding',return_value=context),patch.object(self.m,'_normal_owner',return_value={}),patch.object(self.m,'_snapshot',side_effect=snapshot):
            with self.assertRaises(Done):self.m.verify_actual_cohort(root)
        self.assertEqual(calls[0],(root/'records',{'limits':(253636,912,None)}))
        self.assertEqual(calls[1],(root/'campaign',{'limits':(1865,456,None)}))
        self.assertEqual(calls[2],(str(root/'spool'),{'keep_raw':False,'limits':(52377651,0,4294967296),'spool':True}))

    def test_full_domain_entry_ceiling_plus_one(self):
        for count,limits in [(253637,(253636,912,None)),(1866,(1865,456,None)),(679,(678,1,None))]:
            with self.subTest(limit=limits[0]),self.assertRaisesRegex(ValueError,'inventory bound'):
                self.virtual_inventory(count,limits)

    def test_top_records_recheck_streams_to_exact_declared_count(self):
        import tempfile
        from contextlib import contextmanager
        from unittest.mock import patch
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp).resolve();(root/'records').mkdir();seen=[]
            @contextmanager
            def scan(path):
                def entries():
                    for i in range(5):
                        seen.append(i)
                        if i==3:raise AssertionError('read beyond exact top namespace bound')
                        yield types.SimpleNamespace(name=f'orphan-{i}')
                yield entries()
            with patch.object(self.m.os,'scandir',scan),patch.object(Path,'iterdir',side_effect=AssertionError('unbounded top listing')):
                with self.assertRaisesRegex(ValueError,'records top namespace bound'):
                    self.m._records_top_marker(root,{'startup.json','terminal.json'})
            self.assertEqual(seen,[0,1,2])
