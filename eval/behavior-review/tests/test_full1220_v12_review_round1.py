"""Fix-round1 regressions. No actual factory/helper/key/network operation.

Public-boundary wiring tests explicitly replace downstream IO/proof gates and
never claim actual authority. The semantic test reads existing original raw
records/review only; it runs a pure DATA builder, not any genuine factory.
"""
import copy,hashlib,json,sys,tempfile,types,unittest
from pathlib import Path
from unittest.mock import patch
from test_full1220_v12_composition import load,HERE

class Round1(unittest.TestCase):
 def test_fake_score_module_correct_verifier_pointer_rejected_before_prefix_io(self):
  w=load('full1220-v12-window.py');fake=types.SimpleNamespace(_cohort_verifier_module=w,project_prefix_content=lambda *a,**k:object(),validate_window_traces=lambda *a:{'all_pass':True})
  touched=[]
  class Reader:
   def read(self,*a,**k):touched.append(True);raise AssertionError('unchecked fake module reached original/index IO')
  ctx=w._ActualContext(w._ACTUAL_SEAL,Path('/synthetic-v12'),{}, {},Reader(),None)
  with patch.object(w,'_selected',lambda:None),patch.object(w,'PREFIX_INDEX_SHA256','a'*64):
   with self.assertRaises(ValueError):w.verify_delivered_prefix(ctx,index_path=ctx.root/'prefix-evidence-index.json',helper_output_dir=ctx.root/'out',score_module=fake)
  self.assertEqual(touched,[])
 def test_fake_score_module_correct_pointer_rejected_before_suffix_factory(self):
  w=load('full1220-v12-window.py');fake=types.SimpleNamespace(_cohort_verifier_module=w);touched=[]
  def forbidden(*a,**k):touched.append(True);raise AssertionError('unchecked score module reached suffix factory')
  sys.modules[w.__name__]=w
  with patch.object(w,'_selected',lambda:None),patch.object(w,'verify_actual_cohort',forbidden):
   with self.assertRaises(ValueError):w.verify_actual_completion(score_module=fake)
  self.assertEqual(touched,[])
 def test_exact_original_mixed_v9_manifest_v8_receipt_checkpoint(self):
  w=load('full1220-v12-window.py');f=getattr(w,'validate_original_prefix_wire',None);self.assertTrue(callable(f),'original mixed-version wire validator missing')
  if f is None:return
  m={'schema':'full-window-actual-ready.v9','scope':{'schema':'full-window-scope.v9'},'scope_sha256':w.ORIGINAL_SCOPE_SHA256,'execution_sha256':'a'*64}
  r={'schema':'full-window-acquisition-receipt.v8','window_index':1,'window_id':'window-001','scope_sha256':w.ORIGINAL_SCOPE_SHA256,'execution_sha256':'a'*64}
  cp={'schema':'full-window-campaign-owned-checkpoint.v8','index':1,'scope_sha256':w.ORIGINAL_SCOPE_SHA256,'execution_sha256':'a'*64}
  f(m,r,cp)
  for target in ('receipt','checkpoint','manifest'):
   mm,rr,cc=copy.deepcopy(m),copy.deepcopy(r),copy.deepcopy(cp)
   if target=='receipt':rr['schema']='full-window-acquisition-receipt.v9'
   elif target=='checkpoint':cc['schema']='full-window-campaign-owned-checkpoint.v9'
   else:mm['schema']='full-window-actual-ready.v8'
   with self.subTest(target=target),self.assertRaises(ValueError):f(mm,rr,cc)
 def test_original_raw_pure_build_reaches_turn_metadata_and_matches_once_plan(self):
  w=load('full1220-v12-window.py');s=w.dependency('prefix_scores');root=Path(w.ORIGINAL_ROOT);i=1
  mr=(root/'actual-manifest.json').read_bytes();sr=(root/f'records/window-{i:06d}-summary.json').read_bytes();rr=(root/f'records/window-{i:06d}/receipt.json').read_bytes();r=json.loads(rr)
  review=(root/f'semantic-review/window-{i:06d}-review.json').read_bytes();records={row['recording_file']:(root/f'records/window-{i:06d}'/row['recording_file']).read_bytes() for row in r['rows']}
  anchors={name+'_sha256':hashlib.sha256(raw).hexdigest() for name,raw in [('manifest',mr),('summary',sr),('receipt',rr),('review',review)]};anchors['records']={name:hashlib.sha256(raw).hexdigest() for name,raw in records.items()}
  plan=s._build(mr,sr,rr,records,review,anchors,w.ORIGINAL_BINDING_SHA256)
  self.assertEqual(len(plan['trace_bindings']),24)
  batch=json.loads((Path(w.ORIGINAL_REGISTRY)/'window-000001.batch.json').read_bytes())
  original=(Path(w.ORIGINAL_ROOT).parents[1]/'v9-live-score-horizon-20261031/runs/window-000001-publish-once/plan.json').read_bytes()
  reconciled=w.rebuild_original_prefix_plan(s,(mr,sr,rr,records,review,anchors,w.ORIGINAL_BINDING_SHA256),original,batch)
  self.assertEqual(hashlib.sha256(s._json_bytes(reconciled)).hexdigest(),batch['plan_sha256'])
  # Preserve all scores and their original per-turn metadata, not just counts.
  self.assertEqual(plan['scores'],reconciled['scores'])
  issuer=(Path(w.ORIGINAL_ROOT).parents[1]/'v9-live-score-horizon-20261031/historical-window.py').read_bytes()
  self.assertEqual(hashlib.sha256(issuer).hexdigest(),w._ORIGINAL_HISTORICAL_ISSUER_SHA256)
  for mutate in ('drop_score','drop_requirement','NA_label','positive_hole','source_status'):
   tampered=copy.deepcopy(reconciled)
   if mutate=='drop_score':tampered['scores'].pop()
   elif mutate=='drop_requirement':tampered['rows'][0]['requirements'].pop()
   elif mutate=='NA_label':tampered['rows'][0]['semantic_task_verdict']='not_assessable'
   elif mutate=='positive_hole':tampered['rows'][0]['positive_score_suppressed']=True
   else:tampered['rows'][0]['original_status']='passed'
   batch_tampered={**batch,'plan_sha256':hashlib.sha256(s._json_bytes(tampered)).hexdigest()}
   with self.subTest(mutate=mutate),self.assertRaises(ValueError):w.rebuild_original_prefix_plan(s,(mr,sr,rr,records,review,anchors,w.ORIGINAL_BINDING_SHA256),s._json_bytes(tampered),batch_tampered)

  reader=w.Evidence(root)
  for name in ['records/startup.json','campaign/.lock']:reader.read(name)
  h=reconciled['provenance']['historical_horizon_index']
  for j in range(1,h+1):
   prefix=f'records/window-{j:06d}/'
   for path in (root/prefix).rglob('*'):
    if path.is_file():reader.read(path.relative_to(root).as_posix())
  receipt_raws=[(root/f'records/window-{j:06d}/receipt.json').read_bytes() for j in range(1,h+1)]
  w.verify_original_horizon_data(reader,reconciled,receipt_raws)
  for binding in plan['trace_bindings'].values():self.assertEqual(binding['turn_metadata'][0]['turn_index'],1)
 def test_public_remote_adapter_passes_retained_completion(self):
  s=load('full1220-v12-scores.py');sys.modules[s.__name__]=s;composite=object();suffix=object();token=object();seen=[]
  actual=types.SimpleNamespace(_completion=composite,_module=object(),_proof=types.SimpleNamespace(_cohort=suffix))
  def verify(**kw):
   seen.append(kw['cohort'])
   if kw['cohort'] is not composite:load('full1220-v12-window.py').check_verified_completion(kw['cohort'],{})
   self.assertIs(kw['cohort'],composite);return token
  issuer=types.SimpleNamespace(verify_remote_window=verify)
  with patch.object(s,'_check_actual',lambda *a:None),patch.object(s,'_validate_projection_plan_binding',lambda *a:None),patch.object(s,'remote_verifier',lambda:issuer),patch.object(s,'remote_binding',lambda *a:{}),patch.object(s,'_check_remote',lambda *a:None):
   self.assertIs(s.verify_remote_score_inputs({'window_index':1},object(),actual_proof=actual,readback_root='/synthetic'),token)
   actual._completion=suffix
   with self.assertRaises(ValueError):s.verify_remote_score_inputs({'window_index':1},object(),actual_proof=actual,readback_root='/synthetic')
  self.assertEqual(seen,[composite,suffix])
 def test_actual_shaped_v12_shipper_stdout_exact_filename_once(self):
  d=load('full1220-v12-delivery.py');f=getattr(d,'_verify_shipper_receipt',None);self.assertTrue(callable(f),'shipper stdout adapter validator missing')
  if f is None:return
  raw=b'{"schema":"durable-otel.delivery-range-result.v1"}';digest=hashlib.sha256(raw).hexdigest().encode()
  line=b'    full1220_spool_delivery_v12_test.go:137: FULL1220_DELIVERY_OUTPUT_SHA256='+digest+b'\n'
  stdout=b'=== RUN   TestFull1220SpoolDeliveryV12\n'+line+b'--- PASS: TestFull1220SpoolDeliveryV12 (0.01s)\nPASS\n'
  f(stdout,raw);f(stdout.replace(b'\n',b'\r\n'),raw)
  for bad in (stdout+line,stdout.replace(b'_v12_test.go',b'_test.go'),stdout.replace(b'_test.go',b'_testXgo'),stdout.replace(digest,b'0'*64)):
   with self.assertRaises(ValueError):f(bad,raw)
 def test_original_source_closed_namespace_end_and_reuse_reject_additions(self):
  w=load('full1220-v12-window.py');f=getattr(w,'_recheck_prefix_reader',None);self.assertTrue(callable(f),'shared factory-End/reuse namespace gate missing')
  if f is None:return
  for addition in ('leaf','emptydir'):
   with tempfile.TemporaryDirectory() as tmp:
    root=Path(tmp).resolve();(root/'source.go').write_bytes(b'original');reader=w.Evidence(root);reader.read('source.go');reader.inventory(reader.markers);reader.prefix_closed_namespace=True;f(reader)
    if addition=='leaf':(root/'extra.go').write_bytes(b'new')
    else:(root/'empty').mkdir()
    with self.subTest(addition=addition),self.assertRaises(ValueError):f(reader)
 def test_execution_accessor_is_detached(self):
  w=load('full1220-v12-window.py');scope={'schema':'full-window-scope.v12','windows':[{'id':f'window-{i+55:03d}','first_ordinal':386+i-1,'last_ordinal':386+i-1,'nested':{'source':'fixed'}} for i in range(1,457)]}
  got=w.execution_window_at(scope,1);got['first_ordinal']=999;got['nested']['source']='changed'
  self.assertEqual(scope['windows'][0]['first_ordinal'],386);self.assertEqual(scope['windows'][0]['nested']['source'],'fixed')

class AuthenticatedScoreLoader(unittest.TestCase):
 """UNIT MOCK signature/context boundary; no actual authority or key is made."""
 def fixture(self,root):
  w=load('full1220-v12-window.py');sys.modules[w.__name__]=w
  (root/'verifier-preparation').mkdir();(root/'score-preparation').mkdir()
  original_path=Path(w.__file__);original=original_path.read_bytes();(root/'verifier-preparation/verify-window.py').write_bytes(original)
  for name,_ in w.DEPENDENCIES.values():(root/'verifier-preparation'/name).write_bytes((original_path.parent/name).read_bytes())
  w.__file__=str(root/'verifier-preparation/verify-window.py')
  verifier_sha=hashlib.sha256(original).hexdigest();binding=b'UNIT MOCK technical binding; NOT AUTHORITY';(root/'runtime-binding.json').write_bytes(binding)
  score=(f'FROZEN_ROOT={str(root)!r}\nFROZEN_VERIFIER_SHA256={verifier_sha!r}\nFROZEN_BINDING_SHA256={hashlib.sha256(binding).hexdigest()!r}\n_cohort_verifier_module=None\nEXECUTED=True\n').encode();(root/'score-preparation/scores.py').write_bytes(score)
  payload={'schema':'full1220-v12-score-module-selection.v1','root':str(root),'manifest_sha256':'b'*64,'verifier_sha256':verifier_sha,'score_sha256':hashlib.sha256(score).hexdigest()}
  (root/'score-preparation/selection.json').write_text(json.dumps({'payload':payload,'signature':'0'*128}))
  ctx=w._ActualContext(w._ACTUAL_SEAL,root,{'verifier_sha256':verifier_sha},{'build_attestation_public_key':'a'*64},w.Evidence(root),None)
  return w,ctx,payload
 def test_root_signed_external_selection_loads_exact_owning_bytes_then_detects_change(self):
  with tempfile.TemporaryDirectory(prefix='v12-score-selection-') as tmp:
   root=Path(tmp).resolve();w,ctx,p=self.fixture(root);signatures=[]
   expected=b'full1220-v12-score-module-selection.v1\n'+json.dumps(p,ensure_ascii=False,separators=(',',':')).encode()
   def signature(key,message,value):signatures.append(message);self.assertEqual(message,expected);self.assertEqual(key,'a'*64)
   with patch.object(w,'_selected',lambda:None),patch.object(w,'FROZEN_ROOT',str(root)),patch.object(w,'FROZEN_MANIFEST_SHA256','b'*64),patch.object(w,'_signature',signature):
    s=w.load_score_module(context=ctx);self.assertTrue(s.EXECUTED);self.assertIs(s._cohort_verifier_module,w)
    self.assertIs(w._require_selected_score_module(s,ctx),s)
    fake=types.ModuleType('correct_pointer_but_foreign');fake._cohort_verifier_module=w;sys.modules[fake.__name__]=fake
    with self.assertRaises(ValueError):w._require_selected_score_module(fake,ctx)
    (root/'score-preparation/scores.py').write_bytes(b'raise AssertionError("tampered code must not execute")')
    with self.assertRaises(ValueError):w.load_score_module(context=ctx)
   self.assertGreaterEqual(len(signatures),2)
 def test_signature_rejection_precedes_score_module_execution(self):
  with tempfile.TemporaryDirectory(prefix='v12-score-signature-') as tmp:
   root=Path(tmp).resolve();w,ctx,p=self.fixture(root)
   def reject(*a):raise ValueError('UNIT MOCK rejected signature')
   with patch.object(w,'_selected',lambda:None),patch.object(w,'FROZEN_ROOT',str(root)),patch.object(w,'FROZEN_MANIFEST_SHA256','b'*64),patch.object(w,'_signature',reject):
    with self.assertRaises(ValueError):w.load_score_module(context=ctx)
   self.assertIsNone(w._score_module)
 def test_prefix_keeps_owning_projection_and_detached_plan(self):
  w=load('full1220-v12-window.py');s=load('full1220-v12-scores.py');other=load('full1220-v12-scores.py');plan={'rows':[]};projection=s._ContentProjection(s._PROJECTION_SEAL,b'{}',plan,{})
  # Controlled local primitive only, never passed to any genuine proof checker.
  p=w.VerifiedDeliveredPrefix(w._PREFIX_SEAL,[],[],{},[],None,s,[(projection,plan)])
  plan['rows'].append('changed outside retained proof');retained,raw=p._captures[0]
  s._validate_projection_plan_binding(json.loads(raw),retained)
  for wrong in (object(),{},other._ContentProjection(other._PROJECTION_SEAL,b'{}',json.loads(raw),{})):
   with self.assertRaises(ValueError):s._validate_projection_plan_binding(json.loads(raw),wrong)
