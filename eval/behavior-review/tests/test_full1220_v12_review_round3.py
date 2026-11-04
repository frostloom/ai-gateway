"""Portable helper ABI regression; no Go, original history or external IO."""
import hashlib,json,tempfile,types,unittest
from pathlib import Path
from unittest.mock import patch
from test_full1220_v12_composition import load,HERE

class Round3(unittest.TestCase):
 def test_source_helper_pin_matches_actual_selected_file(self):
  w=load('full1220-v12-window.py')
  self.assertEqual(w.SOURCE_HELPER_SHA256,hashlib.sha256((HERE/w.SOURCE_HELPER).read_bytes()).hexdigest())
 def test_controlled_export_exact_v12_terminal_shape(self):
  w=load('full1220-v12-window.py')
  result={'evidence_kind':'genuine_local_stub_owned_window_not_actual_ready','test_failed':False,'advanced':True,'original_v10_and_r3_evidence_sources_unchanged':True,'candidate_go_sources_unchanged':True,'actual_session_summary':'not_created_or_claimed'}
  # Only terminal ABI is under test: downstream codec/replay are isolated.
  dep=w.dependency
  codec=types.SimpleNamespace(parse_execution_authorization=lambda raw:{'human_reply':{'path':'synthetic-budget-provenance.json'}})
  def run(value):
   with tempfile.TemporaryDirectory() as td:
    root=Path(td).resolve();files={'export-result.json':json.dumps(value).encode(),'synthetic-authorization.json':b'{}','synthetic-budget-provenance.json':b'{}'}
    inv={'evidence_kind':'controlled_local_stub_export_not_authority','files':{}}
    for name,raw in files.items():
     (root/name).write_bytes(raw);inv['files'][name]={'path':str(root/name),'sha256':hashlib.sha256(raw).hexdigest(),'bytes':len(raw)}
    raw=json.dumps(inv).encode();(root/'export-inventory.json').write_bytes(raw)
    with patch.object(w,'dependency',side_effect=lambda name:codec if name=='codec' else dep(name)),patch.object(w,'replay_completed_window',return_value={'data_only':True}):
     return w.verify_controlled_export(root,hashlib.sha256(raw).hexdigest())
  self.assertTrue(run(result)['data_only'])
  for key,value in [('test_failed',True),('advanced',False),('original_v10_and_r3_evidence_sources_unchanged',False),('candidate_go_sources_unchanged',False),('actual_session_summary','manufactured')]:
   with self.subTest(key=key),self.assertRaises(ValueError):run({**result,key:value})
  old=dict(result);del old['original_v10_and_r3_evidence_sources_unchanged'];old['original181_source2369_unchanged']=True
  with self.assertRaises((ValueError,KeyError)):run(old)
