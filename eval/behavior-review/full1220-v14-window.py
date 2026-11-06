"""Independent V14 window data audit. No journal/JSON can mint an owned proof.

Controlled exports are explicitly synthetic. The actual entry is inactive until
an independently reviewed frozen root, source, binary and authority are bound.
This module never starts an owner, claims funds, sends requests or scores.
"""
import sys
sys.dont_write_bytecode = True
import argparse
import copy
import hashlib
import json
import math
import os
from pathlib import Path
import re
import stat
import types

DEPENDENCIES = {
    'native': ('full1220-v8-native.py', '97a0c330b018f72b8aaf0370b3e5ec0a348eca980927b483ba387f12f6781383'),
    'codec': ('full1220-v14-contracts.py', 'b1fb001b17b5766d584a05ec2c7c79f3013987208d6fc1448e3faf64c6e6c9ca'),
    'prefix_scores': ('full1220-v12-prefix-leaves.py', 'f983c4a8a066d54ba535e865067432cb825ca402141d1919c3563375feb4c244'),
    'source': ('full1220-v14-source.py', 'e83033dcff7b100766c587e8d04b9b46834dafef1a9b2512a38a3517429136b0'),
    'runner': ('full1220-v14-helper-runtime.py', 'e472fcbbfd614fcc5b7fdf4d77be8fb9b9d6e4b5e2a83b8507c9dd8dfd709ef1'),
}
BASELINE = '3d5f19325c69165c603a070518d2a4b25faa43a1845323d494866486abe7230f'
BUDGET = '58f7bea973d9c401c6ae8231652a91fa30ccf26de90a8adcc4ad66b0d4539e7d'
RETRY = '26fef0e90b466ed651b2e8bc11790d336d12d75554f14e7fd06a8d18009cf088'
CAPS = '7e67ca0b51e7c1ef087cfa490ebbfbd7eda0d30f91e205e0d50272845116701e'
HISTORICAL_MODEL = 85037976
HISTORICAL_JEV = 932373
FROZEN_ROOT = FROZEN_MANIFEST_SHA256 = FROZEN_SOURCE_SHA256 = 'UNFROZEN'
FROZEN_BINARY_SHA256 = FROZEN_NODE_SHA256 = 'UNFROZEN'
FROZEN_HELPER_CATALOG_SHA256 = 'UNFROZEN'
COMPLETION_SHA256 = '9a2d01ba483bde017baf06f7ae66c151d99d7f4fd0cc0ed911a8e18c905a8042'
SUFFIX_WINDOWS_SHA256 = 'e383d21f0a3981e998ab1f44d2bd1eb886072d04320a727dc2c0fea5bb1e24d2'
EQUIVALENCE_SOURCES_SHA256 = EQUIVALENCE_CHANGES_SHA256 = 'UNFROZEN'
OPERATION_POLICY_BYTES = b'{"schema":"full-window-operation-policy.v14","window_seconds":600,"model_seconds":90,"jev_seconds":10}'
OPERATION_POLICY_SHA256 = hashlib.sha256(OPERATION_POLICY_BYTES).hexdigest()
COLLECTION_POLICY_SHA256 = '3fd68fc05f8ebc4b3a5d87cbeb905caa46abbcedc5b34de836d910a5425f3213'
OWNED_POLICY_SHA256 = 'eed1d1deb720a48b1ebd2725dfa3b0586f35e28b21e03f9635fe8e015cc618a0'
CAPTURE_INPUT_SCHEMA = 'full1220-content-projection-input.v1'


def need(ok, message):
    if not ok: raise ValueError(message)


def sha(raw): return hashlib.sha256(raw).hexdigest()


def identity(st):
    return (st.st_dev, st.st_ino, st.st_mode, st.st_nlink, st.st_size,
            st.st_mtime_ns, st.st_ctime_ns)


class Evidence:
    """Bounded no-follow reads with stable root/ancestor/leaf identities."""
    def __init__(self, root):
        self.root = Path(root)
        need(self.root.is_absolute() and self.root.resolve() == self.root, 'canonical root required')
        st = self.root.lstat(); need(stat.S_ISDIR(st.st_mode), 'root directory required')
        self.root_marker = (st.st_dev, st.st_ino, st.st_mode)
        self.markers = {}; self.directory_markers = {}; self.bounds = {}
        self._inventory_limits = (100000,100000,None)
        self._spool_snapshot = False

    def _root_check(self):
        st = self.root.lstat()
        need((st.st_dev, st.st_ino, st.st_mode) == self.root_marker and self.root.resolve() == self.root,
             'root changed or aliased')

    def _path(self, name):
        need(type(name) is str and name and not name.startswith('/') and
             all(x not in ('', '.', '..') for x in name.split('/')), 'canonical relative path required')
        self._root_check(); path = self.root/name
        for parent in reversed(path.parents):
            if parent == self.root or self.root in parent.parents:
                st = parent.lstat(); need(stat.S_ISDIR(st.st_mode), 'parent directory link/type')
                marker = (st.st_dev, st.st_ino, st.st_mode)
                need(self.directory_markers.get(parent, marker) == marker, 'parent directory replaced')
                self.directory_markers[parent] = marker
        return path

    def read(self, name, expected=None, bound=32 << 20):
        path = self._path(name); before = path.lstat()
        need(stat.S_ISREG(before.st_mode) and before.st_nlink == 1 and before.st_size <= bound,
             'regular single-link bounded file required')
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        try:
            need(identity(os.fstat(fd)) == identity(before), 'opened identity mismatch')
            chunks=[]; size=0
            while True:
                block=os.read(fd, min(1 << 20, bound + 1 - size))
                if not block: break
                chunks.append(block); size+=len(block); need(size <= bound, 'read size bound')
            need(identity(os.fstat(fd)) == identity(before), 'file changed during read')
        finally: os.close(fd)
        need(identity(path.lstat()) == identity(before), 'file changed after read')
        self._path(name); raw=b''.join(chunks); need(len(raw)==before.st_size, 'short read')
        marker=(identity(before),sha(raw))
        need(self.markers.get(name, marker)==marker, 'previous file replaced or changed')
        if expected is not None: need(type(expected) is str and marker[1] == expected, 'raw file SHA mismatch')
        self.markers[name]=marker; self.bounds[name]=bound
        return raw

    def inventory(self, expected=None):
        # Discovery and every later check share the selected physical limits.
        # scandir streams entries; listdir/Path.iterdir/os.walk can retain an
        # oversized directory before the first limit check runs.
        self._root_check(); found=set(); directories=[self.root]; visited=set(); size=0
        entries_limit,directory_limit,byte_limit=self._inventory_limits
        while directories:
            directory=directories.pop()
            with os.scandir(directory) as entries:
                for item in entries:
                    name=Path(item.path).relative_to(self.root).as_posix(); st=item.stat(follow_symlinks=False)
                    need(not stat.S_ISLNK(st.st_mode), 'inventory symlink')
                    need(len(found)+len(visited)<entries_limit, 'inventory bound')
                    if stat.S_ISDIR(st.st_mode):
                        need(len(visited)<directory_limit, 'inventory directory bound')
                        visited.add(name);directories.append(Path(item.path))
                    else:
                        need(stat.S_ISREG(st.st_mode) and st.st_nlink==1, 'inventory file type/link')
                        if self._spool_snapshot:_spool_leaf_size(name,st.st_size)
                        size+=st.st_size
                        need(byte_limit is None or size<=byte_limit, 'inventory byte bound')
                        found.add(name)
        # Include every ancestor, not only directories still on the DFS stack.
        ancestors=set()
        for name in found if expected is None else expected:
            parts=name.split('/')
            ancestors.update('/'.join(parts[:i]) for i in range(1,len(parts)))
        need(visited==ancestors, 'orphan directory')
        if expected is not None:need(found==set(expected), 'closed namespace inventory mismatch')
        self._root_check()
        return found

    def recheck(self):
        self._root_check()
        for name in list(self.markers): self.read(name,bound=self.bounds[name])


def dependency(name):
    file,pin=DEPENDENCIES[name]
    need(type(pin) is str and re.fullmatch('[0-9a-f]{64}',pin), 'UNFROZEN dependency '+name)
    reader=Evidence(Path(__file__).resolve().parent)
    raw=reader.read(file,pin); module=types.ModuleType('v14_'+name)
    module.__file__=str(reader.root/file)
    if name=='runner':sys.modules[module.__name__]=module
    exec(compile(raw,module.__file__,'exec'),module.__dict__)
    reader.recheck()
    return module


def require_quality(rows, proof=None, binding=None):
    # Every V14 arm, including empty admissions, requires genuine Go replay.
    # A Python SQL interpretation or sidecar label cannot grant continuation.
    need(binding is not None, 'genuine V14 source proof binding required')
    return _check_source_proof(proof,binding)




def source_document(raw): return dependency('source')._Document(raw)


def _json(raw): return source_document(raw).value


def _number(value, upper=(1 << 63)-1):
    need(type(value) is int and 0 <= value <= upper, 'integer counter bound'); return value


def _duration(value):
    need(type(value) in (int,float) and math.isfinite(value) and value >= 0, 'finite duration'); return value


def validate_operation_timing(receipt):
    """Completed V14 DATA must retain its separately approved whole600 limit."""
    need(type(receipt['operation_limit_seconds']) is int and receipt['operation_limit_seconds']==600,
         'V14 whole-window600 receipt required')
    need(_duration(receipt['duration_ms'])<=600000,'completed operation exceeded V14 whole-window limit')
    need(type(receipt['operation_deadline']) is str and bool(receipt['operation_deadline']),'operation deadline')


def _scope(scope, raw, n):
    need(scope['schema']=='full-window-scope.v14' and scope['baseline_evidence_sha256']==BASELINE, 'V14 scope/stopped177 baseline')
    _observation_declaration(scope.get('observation'))
    need('completion' not in scope and not any(k.startswith('completion_v') and k!='completion_v14' for k in scope), 'old completion cannot enter V14')
    codec=dependency('codec')
    need(type(scope.get('operation_policy_v14')) is dict, 'V14 operation policy required')
    need(codec.canonical_operation_policy(n.wire(scope['operation_policy_v14']))==OPERATION_POLICY_BYTES, 'fixed V14 whole600/model90/JEV10 policy')
    need(scope.get('collection_policy_sha256')==COLLECTION_POLICY_SHA256 and scope['bindings']['policy_sha256']==OWNED_POLICY_SHA256, 'fixed V14 collection/owned policy')
    need(type(scope.get('completion_v14')) is dict and sha(n.wire(scope['completion_v14']))==COMPLETION_SHA256, 'fixed full completion declaration')
    need(sha(n.wire(scope['windows']))==SUFFIX_WINDOWS_SHA256, 'exact original56..511 execution declarations')
    need(scope.get('budget_approval_sha256','')=='', 'no declaration authority')
    need(scope['allow_paid'] is False and scope['ready_for_paid_run'] is False and scope['budget_declaration']['human_authorized'] is False,
         'declarations are not authorization')
    need(scope['budget_declaration_sha256']==BUDGET==sha(n.wire(scope['budget_declaration'])) and
         scope['retry_policy_sha256']==RETRY==sha(n.wire(scope['retry_declaration'])), 'fixed V14 budget/unchanged retry declaration')
    need(type(scope['width']) is int and scope['width']==12 and len(scope['slots'])==3660 and len(scope['windows'])==456, 'full3660 slots and suffix456 execution')
    need(scope['odd_pass_first_arm_imbalance'] is True,'original odd-pass imbalance')
    need(sha(n.wire(scope['slots']))==scope['schedule_sha256']=='95469ad60b07de7f11eeaf16c638fbc8e9b677e708eb3b4f7247246535859ccf', 'fixed greedy511 schedule digest')
    caps=[{k:w[k] for k in ('model_cap_micro_cny','jev_cap_micro_usd')} for w in scope['windows']]
    need(sha(n.wire(caps))==CAPS, 'fixed456 suffix cap vector')
    first_cases=[]; total_turns=0; windows=[]; current_turns=0
    for i,slot in enumerate(scope['slots']):
        ordinal=i+1; p=i//1220+1; original=(i%1220+406*(p-1))%1220
        expected_order=['model','jev_selective'] if (original+p-1)%2==0 else ['jev_selective','model']
        _number(slot['user_turns'],12); need(slot['user_turns']>0, 'nonzero source turns')
        if not windows or ordinal-windows[-1]['first_ordinal']>=12 or current_turns+2*slot['user_turns']>24:
            windows.append({'id':f'window-{len(windows)+1:03d}','first_ordinal':ordinal,'last_ordinal':ordinal,
                            'max_model_calls':0,'max_jev_calls':0})
            current_turns=0
        w=windows[-1];w['last_ordinal']=ordinal;w['max_model_calls']+=10*slot['user_turns'];w['max_jev_calls']+=slot['user_turns']
        current_turns+=2*slot['user_turns']
        need(n.same(slot['ordinal'],ordinal) and n.same(slot['pass'],p) and slot['arm_order']==expected_order and
             slot['window_id']==w['id'], 'original pass/order and greedy12pair/24turn window')
        if p==1: first_cases.append((slot['case_key'],slot['user_turns']))
        else: need((slot['case_key'],slot['user_turns'])==first_cases[original], 'same cases across passes')
        total_turns+=2*slot['user_turns']
    need(len(set(k for k,_ in first_cases))==1220 and total_turns==11772 and len(windows)==511, 'full case/turn/window denominator')
    for got,want in zip(scope['windows'],windows[55:]):
        need(n.same({k:got[k] for k in want},want), 'exact greedy511 window declaration')
    need(sha(raw)==sha(n.wire(scope)), 'canonical scope bytes')


def _contract(scope, manifest, claim, claim_sha, n):
    w=claim['window']; limits=manifest['provider_limits']
    expected_limits={'model':{'model':'deepseek-flash','max_request_bytes':32768,'max_input_tokens':36864,'max_output_tokens':128,'input_micro_per_million':2000000,'output_micro_per_million':8000000,'cache_hit_input_micro_per_million':40000},
                     'jev':{'model':'jev-1.13.0','max_request_bytes':4096,'max_input_tokens':64000,'max_output_tokens':1000000,'input_micro_per_million':42000,'output_micro_per_million':0}}
    need(n.same(limits,expected_limits),'fixed V14 provider/cache rates and envelopes')
    c={
        'schema':'full-window-accounting.v3','scope_sha256':manifest['scope_sha256'],
        'schedule_sha256':scope['schedule_sha256'],'policy_sha256':scope['bindings']['policy_sha256'],
        'claim_sha256':claim_sha,'window_id':w['id'],
        'permanent_model_micro_cny':w['model_cap_micro_cny'],'permanent_jev_micro_usd':w['jev_cap_micro_usd']}
    for provider,counter in [('model','max_model_calls'),('jev','max_jev_calls')]:
        p=limits[provider]
        c[provider]={'model':p['model'],'max_attempts':w[counter]+20,**{k:v for k,v in p.items() if k!='model'}}
    return n.contract_data(c)


def _accounting_state(r,n):
    c=r['contract']; state={}
    if r['unknown_intents']:state['unknown_intents']=r['unknown_intents']
    for provider in ('model','jev'):
        for field,key in [('unknown_held','unknown_held'),('extra_funded','extra_funded')]:
            if r[key][provider]:state[field+'_'+provider]=r[key][provider]
    if r['retry_required'] is not None:state['retry_required']=r['retry_required']
    state.update(permanent_model_micro_cny=c['permanent_model_micro_cny'],permanent_jev_micro_usd=c['permanent_jev_micro_usd'],
        known_model_micro_cny=r['known']['model'],known_jev_micro_usd=r['known']['jev'],
        held_model_micro_cny=r['pending_held']['model'],held_jev_micro_usd=r['pending_held']['jev'],
        model_intents=r['native_counts']['model'],jev_intents=r['native_counts']['jev'])
    if r['pending'] is not None:state['pending']=r['pending']
    if r['halt']:state['halt']=r['halt']
    state.update(closed=r['closed'],envelope_violation=r['envelope_violation']);return state


def _runtime(r, base, extras, total, n):
    sums=lambda key:n.amounts(r[key]['model'],r[key]['jev'])
    return {'schema':'full-window-runtime-evidence.v8',
        'logical_model_calls':r['logical_counts']['model'],'logical_jev_calls':r['logical_counts']['jev'],
        'native_model_intents':r['native_counts']['model'],'native_jev_intents':r['native_counts']['jev'],
        'model_retry_intents':r['retry_counts']['model'],'jev_retry_intents':r['retry_counts']['jev'],
        'native_settlements':len(r['settlements']),'native_unknown':len(r['unknown_intents']),
        'native_pending':int(r['pending'] is not None),'retry_required':r['retry_required'] is not None,'halt':r['halt'],
        'configured_known':sums('known'),'unknown_held':sums('unknown_held'),'pending_held':sums('pending_held'),
        'historical_permanent':n.amounts(HISTORICAL_MODEL,HISTORICAL_JEV),'base_permanent':base,
        'extra_permanent':n.amounts(sum(e['amount']['model_micro_cny'] for e in extras),sum(e['amount']['jev_micro_usd'] for e in extras)),
        'total_permanent':total,'unknown_intents':[{'window_id':r['contract']['window_id'],**u} for u in r['unknown_intents']], 'extras':extras}


def _permanent(base,extras,n):
    """Fixed177 liabilities plus exactly selected base and funded extra holds."""
    return n.amounts(HISTORICAL_MODEL+base['model_micro_cny']+sum(e['amount']['model_micro_cny'] for e in extras),
                     HISTORICAL_JEV+base['jev_micro_usd']+sum(e['amount']['jev_micro_usd'] for e in extras))


def _verify_advance_token(index,token,claim_shas,last_checkpoint_raw):
    """Go advance returns next claim normally, FINAL checkpoint at window456.

    Finalization poisons the completed campaign without creating claim457. The
    collector writes the returned token unchanged despite its NextClaim name.
    """
    need(type(index) is int and 1<=index<=456 and type(claim_shas) is list and
         len(claim_shas)==index+int(index<456),'exact base-claim prefix; no final claim457')
    need(all(type(s) is str and re.fullmatch('[0-9a-f]{64}',s) for s in claim_shas),'base claim digest prefix')
    need(type(last_checkpoint_raw) is bytes,'original last checkpoint bytes')
    expected=claim_shas[-1] if index<456 else sha(last_checkpoint_raw)
    need(type(token) is str and token==expected,'next reserved claim or final original checkpoint token')


def _payload(files, m, r, n):
    # Packet-level replay mirrors the reviewed native module; full campaign
    # joining below is separate, without rewriting a supplied runtime summary.
    fields, integer, same, request_data = n.fields, n.integer, n.same, n.request_data
    validate_request_body, normalize_usage, wire = n.validate_request_body, n.normalize_usage, n.wire
    c=r['contract']
    read=set()
    def take(name):
        need(type(name) is str and re.fullmatch(r'(store\.json|(?:request|response)-[0-9]{6}\.(?:json|body))',name) and name in files,'payload filename/inventory')
        read.add(name);data=files[name];need(type(data) is bytes,'raw payload bytes');need(len(data)<=1<<20,'payload file bound');return data
    store={**m,'contract':c,'packets':[]}
    need(take('store.json')==wire(store),'initial store canonical bytes')
    packets=m['packets'];need(type(packets) is list and len(packets)==len(r['intents']),'packet count')
    unknown={u['sequence']:u for u in r['unknown_intents']}
    cache={'reported_cache_read':0,'unknown_raw_cache_read':0}
    for sequence,packet in enumerate(packets,1):
        packet=fields(packet,'failure_descriptor_sha256 sequence identity scope_sha256 claim_sha256 window_id request request_descriptor_file response'.split(),('failure_descriptor_sha256',))
        need(integer(packet['sequence'],1)==sequence and same(packet['identity'],r['intents'][sequence]),'packet native identity')
        req=request_data(packet['identity'],c);p=c[req['provider']]
        for key in ('scope_sha256','claim_sha256','window_id'):need(packet[key]==c[key],'packet contract identity')
        reqname=f'request-{sequence:06d}.body';reqdesc=f'request-{sequence:06d}.json';resdesc=f'response-{sequence:06d}.json'
        reqraw=take(reqname)
        packet['request']=fields(packet['request'],('file','bytes','complete','full_sha256'))
        need(same(packet['request'],{'file':reqname,'bytes':len(reqraw),'complete':True,'full_sha256':sha(reqraw)}) and packet['request_descriptor_file']==reqdesc,'request body descriptor')
        need(len(reqraw)==req['bytes'] and sha(reqraw)==req['body_sha256'],'request raw hash')
        validate_request_body(reqraw,req,p)
        initial={key:packet[key] for key in ('sequence','identity','scope_sha256','claim_sha256','window_id','request','request_descriptor_file')}
        initial['identity']=req
        need(take(reqdesc)==wire(initial),'request descriptor canonical bytes')
        resp=fields(packet['response'],'status code complete available body descriptor_file transport_failure'.split(),('body','transport_failure'))
        need(resp['descriptor_file']==resdesc,'response filename')
        if 'body' in resp:resp['body']=fields(resp['body'],('file','bytes','complete','full_sha256'))
        if 'transport_failure' in resp:resp['transport_failure']=fields(resp['transport_failure'],'cause phase elapsed_ms trace_observed connection_reused request_written first_byte'.split())
        expected={key:packet[key] for key in ('sequence','identity','scope_sha256','claim_sha256','window_id')}
        expected['identity']=req;expected['response']=resp
        descraw=take(resdesc);need(descraw==wire(expected),'response descriptor canonical bytes')
        if sequence in unknown:
            u=unknown[sequence]
            failure=fields(resp.get('transport_failure'),'cause phase elapsed_ms trace_observed connection_reused request_written first_byte'.split())
            need(same({k:resp[k] for k in ('status','code','complete','available')},{'status':0,'code':'round_trip','complete':False,'available':False}) and 'body' not in resp,'unknown response must remain unavailable')
            need(failure['cause']=='eof' and failure['phase']=='tls_handshake' and failure['trace_observed'] is True and all(failure[k] is False for k in ('connection_reused','request_written','first_byte')),'first failure data shape')
            integer(failure['elapsed_ms'])
            need(packet.get('failure_descriptor_sha256')==sha(descraw)==u['descriptor_sha256'],'failure descriptor raw SHA')
        else:
            need('failure_descriptor_sha256' not in packet and 'transport_failure' not in resp,'settled packet failure marker')
            need(same({k:resp[k] for k in ('status','code','complete','available')},{'status':200,'code':'complete','complete':True,'available':True}),'settled HTTP200 complete')
            resname=f'response-{sequence:06d}.body';resraw=take(resname)
            need(same(resp.get('body'),{'file':resname,'bytes':len(resraw),'complete':True,'full_sha256':sha(resraw)}),'settled body hash/bytes')
            settle=r['settlements'].get(sequence);need(settle is not None and settle['response_sha256']==sha(resraw),'response settlement SHA')
            usage,availability=normalize_usage(req['provider'],resraw,p)
            need(same(usage,settle['usage']),'raw usage normalization')
            if availability:cache[availability]+=1
    need(read==set(files),'orphan/missing payload files')
    return cache


def _record_header(raw):
    return dependency('source').record_header(raw)


def _completed_record_state(record_raw):
    return dependency('source').validate_completed_record_state(record_raw)


def _sidecar_names(rows):
    """Exact sidecar references only. SQL/handler semantics stay in Go replay."""
    expected={}
    for row in rows:
        q=row.get('quality_end');p=row.get('preview_failure')
        need((q is None)!=(p is None), 'exactly one V14 normal or quality sidecar shape')
        if q is not None:
            need(q['policy_sha256']==OWNED_POLICY_SHA256,'V14 quality owned policy')
            refs=[(q['candidate_file'],q['candidate_sha256']),(q['cleanup_file'],q['cleanup_sha256'])]
        else:
            need(type(p) is dict and set(p)=={'policy_sha256','recording_sha256','sidecar_file','sidecar_sha256'},'exact V14 preview failure reference')
            need(p['policy_sha256']==OWNED_POLICY_SHA256 and p['recording_sha256']==row['recording_sha256'] and p['sidecar_file']==f"preview-failure-{row['ordinal']:06d}-{row['arm']}.json",'V14 sidecar row/policy binding')
            refs=[(p['sidecar_file'],p['sidecar_sha256'])]
        for name,pin in refs:
            need(type(name) is str and '/' not in name and '\\' not in name and name not in ('','.','..') and name not in expected, 'unique V14 sidecar leaf')
            need(type(pin) is str and re.fullmatch('[0-9a-f]{64}',pin),'V14 sidecar digest')
            expected[name]=pin
    return expected


def _sidecars(rows,files):
    return {name:files[name] for name in _sidecar_names(rows)}


def _rows(files, receipt, scope, index, n, source, source_proof, binding):
    w=scope['windows'][index-1]; slots=scope['slots'][w['first_ordinal']-1:w['last_ordinal']]
    expected=[(slot,arm) for slot in slots for arm in slot['arm_order']]
    need(len(receipt['rows'])==len(expected), 'complete row inventory')
    require_quality(receipt['rows'],source_proof,binding)
    traces=set(); allowed={'records/receipt.json','records/payload-manifest.json'}
    telemetry={}; negatives=observed=quality_turns=quality_arms=0; evidence=[]; clocks=[]
    quality_ordinals={row['ordinal'] for row in receipt['rows'] if row.get('quality_end') is not None}
    for row,(slot,arm) in zip(receipt['rows'],expected):
        for k in ('ordinal','pass','case_key'):need(n.same(row[k],slot[k]),'row slot identity')
        need(row['arm']==arm and all(row[k] is True for k in ('attempted','finished','cleanup','observer')),'finished row flags/order')
        need(n.same(row['defined_turns'],slot['user_turns']) and 0<_number(row['observed_turns'],slot['user_turns']), 'observed row partition')
        trace=row['trace_id'];need(type(trace) is str and re.fullmatch('[0-9a-f]{32}',trace) and trace not in traces,'unique trace identity');traces.add(trace)
        name=f"arm-{slot['ordinal']:06d}-{arm}.json"; need(row['recording_file']==name,'record filename')
        file='records/'+name;allowed.add(file);raw=files[file]
        need(sha(raw)==row['recording_sha256'],'record original SHA')
        document=source._Document(raw);record=document.value;result=record['result'];case=result['case']
        q=row.get('quality_end')
        if q is None:
            state=_completed_record_state(raw);negatives+=int(state=='recorded_negative')
            p=row['preview_failure'];_sidecar_names([row]);sidecar='records/'+p['sidecar_file']
            allowed.add(sidecar);need(sha(files[sidecar])==p['sidecar_sha256'],'preview failure sidecar bytes')
            need(n.same(row['observed_turns'],slot['user_turns']), 'ordinary full row turns')
        else:
            _record_header(raw);_sidecar_names([row])
            need(record['disposition']=='halt' and result['status']=='failed' and result['success'] is False and result['structural_pass'] is False,
                 'genuine quality state remains negative')
            need(q['policy_sha256']==scope['bindings']['policy_sha256'] and n.same(q['observed_turns'],row['observed_turns']) and
                 n.same(q['unrun_turns'],slot['user_turns']-row['observed_turns']), 'quality suffix partition')
            for filekey,hashkey in [('candidate_file','candidate_sha256'),('cleanup_file','cleanup_sha256')]:
                name=q[filekey];need('/' not in name and name not in ('','.','..'), 'quality relative sidecar')
                allowed.add('records/'+name);need(sha(files['records/'+name])==q[hashkey], 'quality sidecar bytes')
            negatives+=1;quality_arms+=1;quality_turns+=q['unrun_turns']
        need(case['source_case_id']+'/'+case['scenario']['id']==slot['case_key'] and len(case['scenario']['turns'])==slot['user_turns'],'record case identity')
        need(n.same(row['status'],result['status']) and n.same(row['success'],result['success']) and
             n.same(row['structural_pass'],result['structural_pass']) and row['disposition']==record['disposition'],'preserve original result flags')
        need(row.get('exclude_complete_task_comparison',False) is (slot['ordinal'] in quality_ordinals) and row.get('failure_evidence') is None,'no failure marker on completed ordinary row')
        need(len(result['observations'])==row['observed_turns'] and result['setup_error'] is False and n.same(result['http_failures'],0),'record observation completeness')
        server=[];driver=[]
        for turn,obs in enumerate(result['observations'],1):
            response=obs['turn'];need(n.same(response['status'],200) and not response.get('error') and not response['response'].get('error'),'turn response completed')
            tm=response['response']['telemetry'];runtime=tm['runtime']
            need(tm['langfuse_trace_id']==trace and runtime['model']=='deepseek-flash' and runtime['protocol']=='anthropic' and runtime['routing_mode']==arm and
                 n.same(runtime['max_output_tokens'],128) and runtime['thinking_mode']=='disabled' and n.same(runtime['reply_repair_limit'],1),'frozen model/routing/trace')
            _number(tm['reply_repairs'],1)
            telemetry[(slot['case_key'],slot['pass'],arm,turn)]={'model':_number(tm['llm_calls'],5),'jev':_number(tm['jev_calls'],1)}
            # Retain clock names from source. No clock is called HTTP-only.
            server.append(_duration(tm['total_latency_ms']))
            driver.append(_duration(response['latency_ms']))
        clocks.append({'ordinal':slot['ordinal'],'arm':arm,'server_turn_ms':server,'driver_turn_ms':driver,
                       'conversation_ms':_duration(result['duration_ms']),'arm_runner_ms':_duration(row['duration_ms'])})
        observed+=row['observed_turns']
        evidence.append({**({'QualityEnd':q} if q is not None else {'PreviewFailure':row['preview_failure']}),'Ordinal':slot['ordinal'],'Pass':slot['pass'],'ObservedTurns':row['observed_turns'],'Key':slot['case_key'],'Arm':arm,
                         'RecordingSHA256':row['recording_sha256'],'Cleanup':True,'Observer':True})
    need(n.same(receipt['quality_negative_arms'],negatives) and n.same(receipt['observed_turns'],observed),'source negative/observed counters')
    for k,v in [('quality_end_arms',quality_arms),('quality_end_unrun_turns',quality_turns),('global_unrun_turns',0)]:need(n.same(receipt.get(k,0),v),'window suffix counters')
    need(observed+quality_turns==receipt['planned_current_turns'],'observed/quality partition')
    need({k for k in files if k.startswith('records/') and not k.startswith('records/native-payloads/')}==allowed,'orphan or missing record namespace')
    return evidence,telemetry,clocks


def _replay_completed_window(files, expected_hashes, index, source_proof=None, actual_context=None, _native_cache=None):
    n=dependency('native');source=dependency('source');codec=dependency('codec')
    need(type(files) is dict and type(expected_hashes) is dict and set(files)==set(expected_hashes),'exact raw file pin inventory')
    for name,raw in files.items():
        need(type(name) is str and not name.startswith('/') and all(x not in ('','.','..') for x in name.split('/')), 'raw relative identity')
        need(type(raw) is bytes and sha(raw)==expected_hashes[name], 'raw input SHA')
    used=set()
    def take(name):
        used.add(name);need(name in files,'missing raw file '+name);return files[name]
    def data(name):return source._Document(take(name)).value
    scope_raw=take('scope.json');scope=source._Document(scope_raw).value;_scope(scope,scope_raw,n)
    actual=actual_context is not None
    if actual:_require_actual_context(actual_context)
    manifest_raw=take('actual-manifest.json' if actual else 'synthetic-manifest.json');manifest=source._Document(manifest_raw).value
    need(manifest['schema']=='full-window-actual-ready.v14' and manifest['evidence_kind']==('actual' if actual else 'local_stub'),'explicit manifest mode')
    need(n.same(manifest['scope'],scope) and manifest['scope_sha256']==sha(scope_raw),'manifest exact scope')
    validate_manifest_observer(manifest)
    auth_raw=take('execution-authorization.json' if actual else 'synthetic-authorization.json');auth=codec.parse_execution_authorization(auth_raw)
    if actual:need(auth_raw==actual_context.authority_raw and sha(manifest_raw)==FROZEN_MANIFEST_SHA256,'original authenticated actual bytes')
    need(auth['manifest_sha256']==sha(manifest_raw) and auth['scope_sha256']==manifest['scope_sha256'] and
         auth['execution_sha256']==manifest['execution_sha256'],'auth data bindings')
    need(auth['private_settings_sha256']==manifest['private_settings_sha256'] and auth['spool_root']==manifest['spool_root'] and n.same(auth['observer'],manifest['observer']), 'authority observer/settings binding')
    need(auth['source_sha256']==sha(n.wire(dict(sorted(manifest['sources'].items())))),'source map data binding')
    need(auth['collection_policy_sha256']==scope['collection_policy_sha256']==COLLECTION_POLICY_SHA256 and auth['completion_sha256']==COMPLETION_SHA256 and auth['executor_equivalence_sha256']==manifest['executor_equivalence']['sha256'],'composition/equivalence data binding')
    need(sha(take('human-reply.txt' if actual else 'synthetic-reply.txt'))==auth['human_reply']['sha256'],'synthetic reply raw SHA')
    execution={'schema':'full-window-campaign-execution.v1','scope_sha256':manifest['scope_sha256'],
               'absolute_root':manifest['campaign_root'],'provider_limits':manifest['provider_limits']}
    need(sha(n.wire(execution))==manifest['execution_sha256'],'campaign execution identity')
    need(type(index) is int and 1<=index<=456,'window index')
    receipt=data('records/receipt.json');w=scope['windows'][index-1]
    need(receipt['schema']=='full-window-acquisition-receipt.v14' and receipt['evidence_kind']==('recorded_run' if actual else 'local_stub') and receipt['status']=='collected_window','completed V14 local_stub receipt required')
    need(n.same(receipt['window_index'],index) and receipt['window_id']==w['id'],'receipt window identity')
    for k in ('scope_sha256','execution_sha256'):need(receipt[k]==manifest[k],'receipt '+k)
    for k in ('cleanup','environment_cleanup','observer','advanced','payload_verified'):need(receipt[k] is True,'receipt complete flag '+k)
    for k in ('first_failure_stage','first_failure_arm'):need(receipt[k]=='','completed failure stage')
    need(n.same(receipt['first_failure_ordinal'],0) and n.same(receipt['unrun_arms'],0),'completed failure counters')
    for k,v in [('planned_campaign_pairs',3275),('planned_campaign_arms',6550),('planned_campaign_turns',10504),('operation_limit_seconds',600)]:need(n.same(receipt[k],v),'receipt original bounds')
    currentpairs=w['last_ordinal']-w['first_ordinal']+1;currentturns=sum(s['user_turns']*2 for s in scope['slots'][w['first_ordinal']-1:w['last_ordinal']])
    for k,v in [('planned_current_pairs',currentpairs),('planned_current_arms',2*currentpairs),('planned_current_turns',currentturns),('attempted_arms',2*currentpairs),('finished_arms',2*currentpairs)]:need(n.same(receipt[k],v),'receipt current denominator')
    validate_operation_timing(receipt)
    counters=receipt['observer_counters_cumulative']
    for k in ('Accepted','Persisted','Dropped','Exported','Failed','Batches','Turns','SampledTurns','QueueDepth'):_number(counters[k])
    validate_observation_receipt(receipt['observation'],counters,manifest['observer'],final=False)
    need(counters['Turns']>=receipt['observed_turns'] and counters['SampledTurns']>=receipt['observed_turns'],'observer turn coverage')
    # Full raw prefix of ledger/base/extra/checkpoint data; no cached report is
    # described as a full prior raw replay. Prior semantic records are not claimed.
    claims=[];claim_shas=[];checkpoints=[];replays=[]
    maximum_claim=index+(index<456)
    for j in range(1,maximum_claim+1):
        name=f'campaign/claim-{j:06d}.json';raw=take(name);claim=source._Document(raw).value
        n.fields(claim,'schema baseline_evidence_sha256 budget_approval_sha256 parent_model_micro_cny parent_jev_micro_usd root index scope_sha256 execution_sha256 definition_sha256 manifest_sha256 selection_sha256 profile_sha256 schedule_sha256 policy_sha256 previous_claim_sha256 previous_checkpoint_sha256 window historical_claims permanent_model_micro_cny permanent_jev_micro_usd'.split(),('previous_claim_sha256','previous_checkpoint_sha256'))
        need(raw==n.wire(claim)+b'\n','canonical base claim bytes')
        need(claim['schema']=='full-window-campaign-claim.v14' and n.same(claim['index'],j) and
             claim['baseline_evidence_sha256']==BASELINE and claim['budget_approval_sha256']==sha(auth_raw),'base authority data binding')
        need(claim['root']==manifest['campaign_root'] and n.same(claim['window'],scope['windows'][j-1]) and
             n.same(claim['historical_claims'],manifest['historical_claims']) and len(claim['historical_claims'])==177,'base root/window/history data')
        for k in ('scope_sha256','execution_sha256'):need(claim[k]==manifest[k],'base '+k)
        for k in ('definition_sha256','manifest_sha256','selection_sha256','profile_sha256','schedule_sha256'):need(claim[k]==scope[k],'base source '+k)
        need(claim['policy_sha256']==scope['bindings']['policy_sha256'] and n.same(claim['parent_model_micro_cny'],200000000) and n.same(claim['parent_jev_micro_usd'],3000000),'base policy/parent')
        need(claim.get('previous_claim_sha256','')==(claim_shas[-1] if j>1 else ''),'base previous chain')
        expected_checkpoint=sha(take(f'campaign/checkpoint-{j-1:06d}.json')) if j>1 else ''
        need(claim.get('previous_checkpoint_sha256','')==expected_checkpoint,'base checkpoint chain')
        claims.append(claim);claim_shas.append(sha(raw))
        if j<=index:
            contract=_contract(scope,manifest,claim,sha(raw),n)
            ledger=take(f'campaign/ledger-{j:06d}/accounting.jsonl')
            cache_key=(sha(ledger),sha(n.wire(contract)))
            if _native_cache is not None and cache_key in _native_cache:r=_native_cache[cache_key]
            else:
                r=n.replay_ledger(ledger,contract)
                if _native_cache is not None:_native_cache[cache_key]=r
            need(r['closed'] and r['native_resolved'] and not r['envelope_violation'],'unresolved/unclean closed ledger')
            replays.append(r)
            cpraw=take(f'campaign/checkpoint-{j:06d}.json');cp=source._Document(cpraw).value
            n.fields(cp,'v8 schema index scope_sha256 execution_sha256 definition_sha256 schedule_sha256 policy_sha256 claim_sha256 ledger_sha256 evidence'.split())
            need(cpraw==n.wire(cp)+b'\n','canonical checkpoint bytes')
            need(cp['schema']=='full-window-campaign-owned-checkpoint.v14' and n.same(cp['index'],j) and cp['claim_sha256']==sha(raw) and cp['ledger_sha256']==sha(ledger),'checkpoint ledger/base identity')
            for k in ('scope_sha256','execution_sha256'):need(cp[k]==manifest[k],'checkpoint '+k)
            for k in ('definition_sha256','schedule_sha256'):need(cp[k]==scope[k],'checkpoint source '+k)
            need(cp['policy_sha256']==scope['bindings']['policy_sha256'],'checkpoint policy')
            checkpoints.append(cp)
    r=replays[-1]
    need(receipt['ledger_sha256']==r['journal_sha256'] and receipt['ledger_completion_proof']=='unavailable_from_journal' and
         receipt['accounting_source']=='live_owner_snapshot' and n.same(receipt['accounting'],_accounting_state(r,n)), 'receipt accounting raw replay')
    _verify_advance_token(index,receipt['next_claim_sha256'],claim_shas,take(f'campaign/checkpoint-{index:06d}.json'))
    extras=[];extra_raws=[];extra_names=sorted(k for k in files if re.fullmatch(r'campaign/extra-[0-9]{6}\.json',k))
    unknowns={(rr['contract']['window_id'],u['sequence']):(u,rr) for rr in replays for u in rr['unknown_intents']}
    need(len(extra_names)==len(unknowns),'global extra/unknown exact coverage')
    counts={'model':0,'jev':0};prior='';matched=set()
    for i,name in enumerate(extra_names,1):
        raw=take(name);e=source._Document(raw).value;n.fields(e,n.EXTRA,('previous_extra_sha256',))
        need(name==f'campaign/extra-{i:06d}.json' and raw==n.wire(e)+b'\n','canonical ordered extra raw files')
        need(e['schema']=='full-window-extra-claim.v8' and n.same(e['index'],i) and e.get('previous_extra_sha256','')==prior,'extra global chain')
        u,rr=unknowns[(e['window_id'],e['unknown_sequence'])];c=rr['contract'];p=u['request']['provider'];hold=n.max_hold(c[p])
        need(sha(raw)==u['extra_claim_sha256'] and sha(raw) in rr['funding'] and sha(raw) not in matched,'exact unknown funded extra raw SHA')
        need(n.same(e['request'],u['request']) and e['descriptor_sha256']==u['descriptor_sha256'] and n.same(u['max_hold'],hold),'extra failure identity')
        for k in ('scope_sha256','execution_sha256','source_sha256'):need(e[k]==auth[k],'extra auth '+k)
        need(e['root']==manifest['campaign_root'] and e['owner_id']==auth['owner_id'] and e['authorization_sha256']==sha(auth_raw) and
             e['base_claim_sha256']==c['claim_sha256'] and e['policy_sha256']==c['policy_sha256'],'extra authority/window binding')
        counts[p]+=1;need(counts[p]<=20 and n.same(e['provider_index'],counts[p]) and n.same(e['model_extras'],counts['model']) and n.same(e['jev_extras'],counts['jev']),'provider20/global extra counters')
        need(n.same(e['amount'],n.amounts(hold,0) if p=='model' else n.amounts(0,hold)), 'exact extra maximum hold')
        j=next(j for j,csha in enumerate(claim_shas,1) if csha==c['claim_sha256'])
        extra_base=n.amounts(sum(x['window']['model_cap_micro_cny'] for x in claims[:j]),sum(x['window']['jev_cap_micro_usd'] for x in claims[:j]))
        permanent=_permanent(extra_base,extras+[e],n)
        need(n.same(e['permanent'],permanent),'extra permanent currencies')
        extras.append(e);extra_raws.append(sha(raw));matched.add(sha(raw));prior=sha(raw)
    for j,claim in enumerate(claims,1):
        prior_extras=[e for e in extras if _extra_local_index(scope,e)<j]
        base=n.amounts(sum(x['window']['model_cap_micro_cny'] for x in claims[:j]),sum(x['window']['jev_cap_micro_usd'] for x in claims[:j]))
        total=_permanent(base,prior_extras,n)
        need(n.same(claim['permanent_model_micro_cny'],total['model_micro_cny']) and n.same(claim['permanent_jev_micro_usd'],total['jev_micro_usd']),'base cumulative permanent including prior extras')
        if j<=index:
            window_extras=[e for e in extras if _extra_local_index(scope,e)<=j]
            total=_permanent(base,window_extras,n)
            need(n.same(checkpoints[j-1]['v8'],_runtime(replays[j-1],base,window_extras,total,n)), 'checkpoint before next base runtime')
    # Receipt captured after successful advance and pre-reserving the next base.
    base=n.amounts(sum(c['window']['model_cap_micro_cny'] for c in claims),sum(c['window']['jev_cap_micro_usd'] for c in claims))
    total=_permanent(base,extras,n)
    need(total['model_micro_cny']+2023424<=200000000 and total['jev_micro_usd']+65<=3000000,'parent permanent plus unchanged risk')
    runtime=_runtime(r,base,extras,total,n);need(n.same(receipt['v8'],runtime),'receipt after next base runtime')
    record_bytes={row['recording_file']:files['records/'+row['recording_file']] for row in receipt['rows']}
    sidecars={name:files['records/'+name] for name in _sidecar_names(receipt['rows'])}
    binding=source_binding(manifest_raw,files['records/receipt.json'],record_bytes,sidecars)
    if source_proof is not None:_check_source_proof(source_proof,binding)
    evidence,telemetry,clocks=_rows(files,receipt,scope,index,n,source,source_proof,binding)
    need(n.same(checkpoints[-1]['evidence'],evidence),'checkpoint exact source row evidence')
    logical={key:{'model':0,'jev':0} for key in telemetry};sequences={}
    order={key:i for i,key in enumerate(telemetry)};last_rank=-1
    for req in r['intents'].values():
        key=tuple(req[k] for k in ('case_key','pass','arm','turn'));need(key in telemetry,'native belongs to current source turn')
        need(order[key]>=last_rank,'native preserves source arm/turn order');last_rank=order[key]
        if req['native_attempt']==1:
            p=req['provider'];logical[key][p]+=1;sequences.setdefault((key,p),[]).append(req['attempt'])
    need(n.same(list(logical.values()),list(telemetry.values())),'logical telemetry is not native count')
    for seqs in sequences.values():need(seqs==list(range(1,len(seqs)+1)),'logical attempt sequence')
    payload_raw=take('records/payload-manifest.json');payload=source._Document(payload_raw).value
    need(sha(payload_raw)==receipt['payload_manifest_sha256'] and payload['schema']=='full-window-payload.v2' and
         payload['evidence_kind']==('recorded_run' if actual else 'local_stub') and payload['completion_proof']=='unavailable_from_payload_capture' and n.same(payload['contract'],r['contract']), 'V14 payload header/contract')
    payload_files={k.removeprefix('records/native-payloads/'):v for k,v in files.items() if k.startswith('records/native-payloads/')}
    cache=_payload(payload_files,payload,r,n)
    need(n.same(receipt['payload_packets'],len(r['intents'])) and receipt['payload_status']=='verified_ledger_linkage' and receipt['payload_audit_only_completion']=='unavailable_from_payload_capture','receipt payload reconciliation')
    expected_campaign={f'campaign/claim-{j:06d}.json' for j in range(1,maximum_claim+1)}|{f'campaign/checkpoint-{j:06d}.json' for j in range(1,index+1)}|{f'campaign/ledger-{j:06d}/accounting.jsonl' for j in range(1,index+1)}|set(extra_names)|{'campaign/.lock'}
    need({k for k in files if k.startswith('campaign/')}==expected_campaign,'closed campaign namespace/no unexpected claim')
    need(take('campaign/.lock')==b'', 'campaign lock raw expected empty; no ownership inferred')
    return {**runtime,'schema':'full-window-external-data-audit.v14','evidence_kind':'controlled_local_stub_data_only',
        'window_index':index,'scope_sha256':manifest['scope_sha256'],'execution_sha256':manifest['execution_sha256'],
        'receipt_sha256':sha(files['records/receipt.json']),'checkpoint_sha256':sha(files[f'campaign/checkpoint-{index:06d}.json']),
        'attempted_arms':receipt['attempted_arms'],'finished_arms':receipt['finished_arms'],'observed_turns':receipt['observed_turns'],
        'raw_cache_availability':cache,'clocks':clocks,
        'execution_authority':False,'owned_completion_proved':False,'actual_session_summary':'not_created_or_claimed',
        'source_case_external_validation':'genuine_in_process_replay' if source_proof is not None else 'pending_genuine_pinned_source_helper',
        'financial_claim_authenticity_proved':False,'signature_trust_proved':False,
        'completion_proof':'unavailable_from_journal','quality_end_arms':receipt.get('quality_end_arms',0)}


def replay_completed_window(files, expected_hashes, window_index, *, source_proof=None):
    """Reconcile raw controlled data. All hashes are inputs, not trust anchors."""
    try:return _replay_completed_window(files,expected_hashes,window_index,source_proof)
    except (KeyError,TypeError,IndexError,StopIteration,UnicodeError) as exc:
        raise ValueError('invalid V14 window data: '+type(exc).__name__) from exc


def verify_controlled_export(root, inventory_sha256):
    reader=Evidence(Path(root)); invraw=reader.read('export-inventory.json',inventory_sha256);inv=_json(invraw)
    need(inv['evidence_kind']=='controlled_local_stub_export_not_authority','controlled export marker')
    files={};hashes={}
    for name,item in inv['files'].items():
        need(item['path']==str(reader.root/name),'inventory path/root alias')
        files[name]=reader.read(name,item['sha256']);need(len(files[name])==item['bytes'],'inventory raw length')
        hashes[name]=item['sha256']
    reader.inventory(set(files)|{'export-inventory.json'})
    result=_json(files['export-result.json'])
    need(result['evidence_kind']=='genuine_local_stub_owned_window_not_actual_ready' and result['test_failed'] is False and
         result['advanced'] is True and result['original_v10_and_r3_evidence_sources_unchanged'] is True and result['candidate_go_sources_unchanged'] is True,
         'terminal controlled export required')
    need(result['actual_session_summary']=='not_created_or_claimed','no manufactured actual summary')
    auth=dependency('codec').parse_execution_authorization(files['synthetic-authorization.json'])
    replyname=Path(auth['human_reply']['path']).name
    need(replyname=='synthetic-budget-provenance.json','controlled retained budget provenance')
    files['synthetic-reply.txt']=files[replyname];hashes['synthetic-reply.txt']=sha(files[replyname])
    out=replay_completed_window(files,hashes,1);reader.recheck();out['inventory_sha256']=sha(invraw)
    return out



CAPTURE_HELPER_SHA256 = '98a26427ab04797c26c88356e88800d417362e215b5fbf9c39531b2de36c41d9'

SOURCE_HELPER = 'helpers/full1220_source_v14_test.go.txt'
SOURCE_HELPER_SHA256 = 'a527ad6500e3bec3a123190dfb03e7ae4ef6d1e6c902566be793b57eff5a7662'
GO_EXECUTABLE = '/Users/a1/开发工具/go/bin/go'
GO_SHA256 = '8612de418d551a418517845c05cebdcfed49095cd08ef0a4d682bb2a5cf4896c'
SDK_ROOT = '/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk'
SDK_METADATA_SHA256 = 'f8d005f09381389167f9e0aeaa169bc9e7dff162ef22ca2fd8e98df7ff1acafe'
_SOURCE_SEAL = object()


class _SourceProof:
    """Process-local replay evidence, never an owned driver/campaign capability."""
    __slots__ = ('_raw', '_seal')
    def __init__(self, seal, binding):
        need(seal is _SOURCE_SEAL, 'source proof issuer only')
        object.__setattr__(self, '_raw', json.dumps(binding,ensure_ascii=False,sort_keys=True,separators=(',',':')).encode())
        object.__setattr__(self, '_seal', seal)
    def __setattr__(self, name, value):raise AttributeError('immutable source replay proof')


def source_binding(manifest_raw, receipt_raw, records, sidecars):
    return {'manifest_sha256':sha(manifest_raw),'receipt_sha256':sha(receipt_raw),
            'records':{k:sha(v) for k,v in sorted(records.items())},
            'sidecars':{k:sha(v) for k,v in sorted(sidecars.items())}}


def _check_source_proof(proof, binding):
    need(type(proof) is _SourceProof and proof._seal is _SOURCE_SEAL and
         dependency('native').same(json.loads(proof._raw)['inputs'],binding), 'genuine matching in-process V14 source proof required')
    return json.loads(proof._raw)


def _exclusive(path, raw):
    fd=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600)
    try:
        view=memoryview(raw)
        while view:
            count=os.write(fd,view);need(count>0,'short exclusive write');view=view[count:]
        os.fsync(fd)
    finally:os.close(fd)
    fd=os.open(Path(path).parent,os.O_RDONLY)
    try:os.fsync(fd)
    finally:os.close(fd)


def verify_quality_v14(manifest_raw, receipt_raw, records, sidecars, expected_hashes, *,
                      source_root, source_hashes, output_dir, execute=False, context=None):
    """Every V14 arm requires source replay, including empty admissions."""
    need(type(receipt_raw) is bytes and len(receipt_raw)<=16<<20,'bounded raw receipt')
    def pairs(items):
        value={}
        for key,item in items:
            need(key not in value,'duplicate receipt key');value[key]=item
        return value
    def invalid(value):raise ValueError('nonfinite receipt number')
    receipt=json.loads(receipt_raw.decode('utf-8'),object_pairs_hook=pairs,parse_constant=invalid)
    need(type(receipt) is dict and type(receipt.get('rows')) is list and all(type(r) is dict for r in receipt['rows']), 'receipt rows')
    return verify_source_v14(manifest_raw,receipt_raw,records,sidecars,expected_hashes,
        source_root=source_root,source_hashes=source_hashes,output_dir=output_dir,execute=execute,context=context)





def verify_source_v14(manifest_raw, receipt_raw, records, sidecars, expected_hashes, *,
                     source_root, source_hashes, output_dir, execute=False, context=None):
    """Fresh source-case replay using the separately authenticated V14 artifact.

    Technical preparation may provide a genuine context without execution
    authority; this returns ONLY narrow source replay evidence, never ownership.
    """
    _selected()
    need(execute is True,'explicit offline source execution required')
    _require_technical_context(context);need(context.helper is not None,'genuine V14 helper context required')
    n=dependency('native');source=dependency('source');h=context.helper;p=h['runner']
    need(sha(manifest_raw)==FROZEN_MANIFEST_SHA256 and source_root==context.manifest['source_root'] and
         n.same(source_hashes,context.manifest['sources']), 'selected source manifest/map/root')
    binding=source_binding(manifest_raw,receipt_raw,records,sidecars)
    need(n.same(expected_hashes,binding),'source original input pins')
    manifest=source._Document(manifest_raw).value;receipt=source._Document(receipt_raw).value
    need(manifest['schema']=='full-window-actual-ready.v14' and receipt['schema']=='full-window-acquisition-receipt.v14' and
         manifest['scope_sha256']==receipt['scope_sha256'] and manifest['execution_sha256']==receipt['execution_sha256'],'source V14 binding')
    _scope(manifest['scope'],n.wire(manifest['scope']),n)
    rows=receipt['rows'];need(0<len(rows)<=24 and set(records)=={r['recording_file'] for r in rows},'source current rows/record set')
    expected_sidecars=_sidecar_names(rows)
    for row in rows:
        name=row['recording_file'];need('/' not in name and name not in ('','.','..') and sha(records[name])==row['recording_sha256'],'source record identity')
        _record_header(records[name])
    need(set(sidecars)==set(expected_sidecars) and not set(sidecars)&set(records),'source sidecar set')
    for name,pin in expected_sidecars.items():need(sha(sidecars[name])==pin,'source sidecar bytes')
    originals=Evidence(Path(source_root))
    for name,pin in source_hashes.items():originals.read(name,pin)
    original_names=set(source_hashes)
    if (originals.root/'.git').exists():originals.read('.git',bound=4096);original_names.add('.git')
    originals.inventory(original_names)
    selected=p.select_helper(h['catalog'],'source',artifact_root=context.root,runtime_tree=h['runtime'],tool_pins=h['tools'])
    output=Path(output_dir);need(output.is_absolute() and output.resolve()==output and output.parent.is_dir(),'canonical new source output')
    need(output!=originals.root and originals.root not in output.parents and output not in originals.root.parents,'source/output separation')
    output.mkdir(mode=0o700);rd=output/'records';rd.mkdir(mode=0o700);(output/'tmp').mkdir(mode=0o700)
    for name,raw in {**records,**sidecars}.items():_exclusive(rd/name,raw)
    proof_binding={'inputs':binding,'source_map_sha256':FROZEN_SOURCE_SHA256,'helper_sha256':SOURCE_HELPER_SHA256,
                   'go_sha256':GO_SHA256,'sdk_metadata_sha256':SDK_METADATA_SHA256,
                   'helper_catalog_sha256':FROZEN_HELPER_CATALOG_SHA256,'helper_binary_sha256':selected.binary.sha256}
    job=n.wire({'schema':'full1220-source-v14-job.v1','observation_receipt_boundary':'durable_local','scope':manifest['scope'],'rows':rows,'binding':proof_binding});_exclusive(output/'input.json',job)
    env={'PATH':'/usr/bin:/bin:/usr/sbin:/sbin','TMPDIR':str(output/'tmp'),
         'FULL1220_V14_SOURCE_INPUT':str(output/'input.json'),'FULL1220_V14_SOURCE_INPUT_SHA256':sha(job),
         'FULL1220_V14_SOURCE_OUTPUT':str(output/'source-proof.json')}
    pins=[p.pin_file(originals.root/name,immutable=False) for name in original_names]
    pins += [p.pin_file(output/'input.json',immutable=False)]
    pins += [p.pin_file(rd/name,immutable=False) for name in {**records,**sidecars}]
    pins += list(h['tools'].values())+[p.pin_file(context.root/'helper-build/catalog.json')]
    original_records=set(records)|set(sidecars)
    def validate(outdir):
        originals.inventory(original_names);originals.recheck()
        rr=Evidence(rd)
        for name,raw in {**records,**sidecars}.items():rr.read(name,sha(raw))
        rr.inventory(original_records)
        result=Evidence(output).read('source-proof.json')
        need(n.same(source._Document(result).value,{'schema':'full1220-source-v14-replay.v1','input_sha256':sha(job),'binding':proof_binding}),'source helper exact output')
        stdout=Evidence(outdir).read('stdout',bound=8<<20)
        markers=re.findall(rb'(?m)^\s+full1220_source_v14_test\.go:[0-9]+: FULL1220_V14_SOURCE_OUTPUT_SHA256=([0-9a-f]{64})\r?$',stdout)
        need(markers==[sha(result).encode()],'source real stdout receipt')
    process=p.run_owned(state_root=context.root/'helper-supervision',attempt_name='source-'+sha(str(output).encode())[:32],
        argv=list(selected.argv),cwd=Path(selected.cwd),env=env,executable_pin=selected.binary,inputs=pins,trees=[h['runtime']],
        timeout=240,phase='source',validate_output=validate)
    need(process['accepted'] is True and process['cleanup']['confirmed'] is True and process['output_validation_supplied'] is True,'source process/output/cleanup failed; no retry/proof')
    actual_path=context.root/'helper-supervision/attempts'/('source-'+sha(str(output).encode())[:32])/process['receipt_file']
    need(n.same(_json(Evidence(actual_path.parent).read(actual_path.name)),process),'source authoritative process receipt')
    _exclusive(output/'process.json',n.wire({'path':str(actual_path),'sha256':sha(actual_path.read_bytes()),'accepted':True}))
    return _SourceProof(_SOURCE_SEAL,proof_binding)


FROZEN_COMMIT = FROZEN_TREE = 'UNFROZEN'
FROZEN_FILE_COUNT = FROZEN_GO_COUNT = 'UNFROZEN'
NODE_EXECUTABLE = '/Users/a1/开发工具/node/node-v24.22.0-darwin-arm64/bin/node'
NODE_SHA256 = 'b468fb9c67427c9d3aec07ee1105e63020923a631a9a2a00beb71a7ba3768bfd'
_ACTUAL_SEAL = object()
_TECHNICAL_SEAL = object()
_AUTHORITY_SEAL = object()


class _TechnicalContext:
    """Authenticated technical freeze; no actual authority or normal cohort."""
    def __init__(self, seal, root, binding, manifest, reader):
        need(seal is _TECHNICAL_SEAL,'technical freeze issuer only')
        self.seal=seal;self.root=root;self.binding=binding;self.manifest=manifest
        self.reader=reader;self.authority_raw=None;self.helper=None


class _VerifiedAuthority:
    """Only the crypto-and-file verifier constructs this process-local proof."""
    __slots__=('seal','authority_sha256','approval_sha256')
    def __init__(self,seal,authority_raw,approval_raw):
        need(seal is _AUTHORITY_SEAL,'authority verifier only')
        object.__setattr__(self,'seal',seal)
        object.__setattr__(self,'authority_sha256',sha(authority_raw))
        object.__setattr__(self,'approval_sha256',sha(approval_raw))
    def __setattr__(self,key,value):raise AttributeError('immutable authority proof')
    def __reduce__(self):raise TypeError('authority proof cannot serialize')


class _ActualContext:
    """Separate from technical preparation; requires both verified signatures."""
    def __init__(self,seal,technical,authority_raw,proof):
        need(seal is _ACTUAL_SEAL and type(technical) is _TechnicalContext and
             technical.seal is _TECHNICAL_SEAL and type(proof) is _VerifiedAuthority and
             proof.seal is _AUTHORITY_SEAL and type(authority_raw) is bytes and
             sha(authority_raw)==proof.authority_sha256,'verified separate V14 authority required')
        self.seal=seal;self.root=technical.root;self.binding=technical.binding
        self.manifest=technical.manifest;self.reader=technical.reader
        self.authority_raw=authority_raw;self.authority_proof=proof
        self.source_reader=technical.source_reader;self.helper=technical.helper


def _require_actual_context(context):
    need(type(context) is _ActualContext and context.seal is _ACTUAL_SEAL and
         type(context.authority_proof) is _VerifiedAuthority and context.authority_proof.seal is _AUTHORITY_SEAL and
         type(context.authority_raw) is bytes and sha(context.authority_raw)==context.authority_proof.authority_sha256,
         'verified actual V14 context required; technical/mock DATA is insufficient')
    # Rehash all originally authenticated files, including both signed envelopes
    # and the distinct timing reply. A JSON report cannot replace this reader.
    context.reader.recheck()
    return context


def _require_technical_context(context):
    if type(context) is _ActualContext:return _require_actual_context(context)
    need(type(context) is _TechnicalContext and context.seal is _TECHNICAL_SEAL,
         'authenticated technical V14 context required')
    return context


def validate_operation_policy_authority_data(manifest,authorization_raw,approval_raw,*,root,manifest_sha256,binary_sha256):
    """Pure exact binding checks returning DATA, without verifying signatures.

    This function cannot issue an actual context. The runtime issuer below must
    verify both signatures and the pinned human replies before constructing one.
    """
    codec=dependency('codec');n=dependency('native');root=Path(root)
    need(root.is_absolute() and os.path.normpath(str(root))==str(root),'canonical V14 root')
    auth=codec.parse_execution_authorization(authorization_raw)
    approval=codec.parse_operation_policy_approval(approval_raw)
    need(manifest['schema']=='full-window-actual-ready.v14' and manifest['campaign_root']==str(root/'campaign'),'new V14 actual namespace')
    _scope(manifest['scope'],n.wire(manifest['scope']),n)
    need(manifest['scope_sha256']==sha(n.wire(manifest['scope'])),'exact V14 scope digest')
    identities={'manifest_sha256':manifest_sha256,'source_sha256':sha(n.wire(dict(sorted(manifest['sources'].items())))),
                'binary_sha256':binary_sha256,'scope_sha256':manifest['scope_sha256'],
                'execution_sha256':manifest['execution_sha256'],'operation_policy_sha256':OPERATION_POLICY_SHA256}
    for key,value in identities.items():
        need(auth[key]==approval[key]==value,'exact separately approved V14 identity '+key)
    need(auth['operation_policy_approval']=={'path':str(root/'operation-policy-approval.json'),'sha256':sha(approval_raw)},'fixed distinct policy approval companion')
    need(approval['human_reply']['path']==str(root/'operation-policy-human-reply.txt'),'fixed new timing reply companion')
    need(auth['private_settings_sha256']==manifest['private_settings_sha256'] and auth['spool_root']==manifest['spool_root'] and
         n.same(auth['observer'],manifest['observer']),'signed settings/observer identity')
    need(auth['collection_policy_sha256']==manifest['scope']['collection_policy_sha256']==COLLECTION_POLICY_SHA256 and
         auth['completion_sha256']==COMPLETION_SHA256 and auth['executor_equivalence_sha256']==manifest['executor_equivalence']['sha256'],
         'signed composition/equivalence identity')
    return auth,approval


def _authenticate_execution_authority(technical):
    """The sole actual-context issuer: two signatures, then fresh reply bytes."""
    need(type(technical) is _TechnicalContext and technical.seal is _TECHNICAL_SEAL,'technical freeze required')
    root=technical.root;reader=technical.reader;m=technical.manifest;codec=dependency('codec')
    authority_raw=reader.read('execution-authorization.json',bound=65536)
    auth=codec.parse_execution_authorization(authority_raw)
    need(auth['operation_policy_approval']['path']==str(root/'operation-policy-approval.json'),'fixed new policy approval path')
    approval_raw=reader.read('operation-policy-approval.json',auth['operation_policy_approval']['sha256'],65536)
    auth,approval=validate_operation_policy_authority_data(m,authority_raw,approval_raw,root=root,
        manifest_sha256=FROZEN_MANIFEST_SHA256,binary_sha256=FROZEN_BINARY_SHA256)
    public_key=m['build_attestation_public_key']
    _signature(public_key,codec.authorization_signing_bytes(authority_raw),auth['signature'])
    _signature(public_key,codec.policy_approval_signing_bytes(approval_raw),approval['signature'])
    reader.read('operation-policy-human-reply.txt',approval['human_reply']['sha256'],1<<20)
    reader.read(_relative(root,auth['human_reply']['path']),auth['human_reply']['sha256'],1<<20)
    reader.read(_relative(root,auth['autonomous_authorization']['path']),auth['autonomous_authorization']['sha256'],65536)
    technical.source_reader.recheck();reader.recheck()
    proof=_VerifiedAuthority(_AUTHORITY_SEAL,authority_raw,approval_raw)
    return _ActualContext(_ACTUAL_SEAL,technical,authority_raw,proof)


def _selected():
    for value,size in [(FROZEN_MANIFEST_SHA256,64),(FROZEN_SOURCE_SHA256,64),(FROZEN_BINARY_SHA256,64),(FROZEN_COMMIT,40),(FROZEN_TREE,40)]:
        need(type(value) is str and re.fullmatch('[0-9a-f]{'+str(size)+'}',value),'UNFROZEN exact V14 selection')
    need(type(FROZEN_FILE_COUNT) is int and type(FROZEN_GO_COUNT) is int and 0<FROZEN_GO_COUNT<=FROZEN_FILE_COUNT,'UNFROZEN source counts')
    need(type(FROZEN_ROOT) is str and FROZEN_ROOT.startswith('/') and 'v14' in Path(FROZEN_ROOT).name,'UNFROZEN independent V14 root')
    need(type(FROZEN_HELPER_CATALOG_SHA256) is str and re.fullmatch('[0-9a-f]{64}',FROZEN_HELPER_CATALOG_SHA256),'UNFROZEN V14 helper catalog')


def _signature(public_hex, message, signature_hex):
    """Data crypto only; callers select trusted key from pinned manifest first."""
    need(type(public_hex) is str and re.fullmatch('[0-9a-fA-F]{64}',public_hex) and
         type(signature_hex) is str and re.fullmatch('[0-9a-fA-F]{128}',signature_hex),'Ed25519 hex encoding')
    executable=Path(NODE_EXECUTABLE);reader=Evidence(executable.parent.resolve());reader.read(executable.name,NODE_SHA256,256<<20)
    program="""const c=require('node:crypto');let s='';process.stdin.setEncoding('utf8');
process.stdin.on('data',x=>s+=x);process.stdin.on('end',()=>{try{const x=JSON.parse(s);
const k=c.createPublicKey({key:Buffer.concat([Buffer.from('302a300506032b6570032100','hex'),Buffer.from(x.key,'hex')]),format:'der',type:'spki'});
const ok=c.verify(null,Buffer.from(x.message,'hex'),k,Buffer.from(x.signature,'hex'));process.stdout.write(ok?'verified\\n':'rejected\\n');process.exitCode=ok?0:2;}catch(e){process.exitCode=3;}});"""
    import subprocess
    result=subprocess.run([str(executable),'--no-warnings','-e',program],
        input=json.dumps({'key':public_hex,'message':message.hex(),'signature':signature_hex}).encode(),
        env={'PATH':'/usr/bin:/bin','NODE_OPTIONS':''},stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=15,check=False)
    need(result.returncode==0 and result.stdout==b'verified\n' and result.stderr==b'','Ed25519 signature rejected')
    reader.recheck()


def _relative(root,path):
    p=Path(path);need(p.is_absolute() and p.resolve()==p and root in p.parents,'evidence outside independent frozen root/alias')
    return p.relative_to(root).as_posix()


def verify_source_seal(root, sources, seal, freeze_root):
    n=dependency('native');reader=Evidence(Path(root));freeze_root=Path(freeze_root)
    need(set(seal)=={'schema','source_map_sha256','modes'} and seal['schema']=='full1220-v14-readonly-source-seal.v1' and
         seal['source_map_sha256']==sha(n.wire(dict(sorted(sources.items())))), 'V14 exact source seal')
    need('.git' not in sources and not any(x.startswith('.git/') for x in sources),'git cannot be signed source')
    dirs={'.'}
    for name in sources:
        reader._path(name)
        dirs.update(str(p) for p in Path(name).parents if str(p)!='.')
    modes=seal['modes'];need(set(modes)==set(sources)|dirs|{'.git'},'closed sealed source modes')
    directory_ids={}
    for name,mode in modes.items():
        path=reader.root if name=='.' else reader.root/name;st=path.lstat()
        need(type(mode) is int and stat.S_IMODE(st.st_mode)==mode and not mode&0o222,'source readonly mode')
        if name in dirs:
            need(stat.S_ISDIR(st.st_mode),'source directory type');directory_ids[name]=identity(st)
        else:
            expected=sha(('gitdir: '+str(freeze_root/'archive-git-metadata')+'\n').encode()) if name=='.git' else sources[name]
            reader.read(name,expected,16<<20)
    reader.inventory(set(sources)|{'.git'});reader.recheck()
    for name,marker in directory_ids.items():need(identity((reader.root if name=='.' else reader.root/name).lstat())==marker,'source directory changed')
    return reader


def load_freeze_binding(root, filename, expected, *, require_authority=True):
    """Pinned technical bytes first; final human authority is signed afterwards.

    The explicit binding SHA is controller input. Manifest/source/binary selection
    is separately compiled into this template before the human reply exists.
    """
    _selected();root=Path(root)
    need(root==Path(FROZEN_ROOT) and root.resolve()==root and filename=='runtime-binding.json','fixed new V14 root/binding')
    n=dependency('native');codec=dependency('codec');reader=Evidence(root)
    binding=_json(reader.read(filename,expected))
    need(set(binding)=={'schema','artifact_sha256','scope_sha256','execution_sha256','verifier_sha256','owner_sha256','dependency_sha256'},'exact V14 binding fields')
    need(binding['schema']=='full1220-v14-private-runtime-binding.v1','V14 binding schema')
    artifacts=binding['artifact_sha256'];required={'actual-manifest.json','full-window-actual-v14','actual-manifest.json.binary-attestation.json','source-readonly-seal.json','technical-start-audit.json','actual-preparation-result.json',
        'helper-build/catalog.json','helper-build/source.test','helper-build/capture.test','helper-build/spool.test'}
    need(set(artifacts)==required,'exact technical artifact inventory')
    need(artifacts['actual-manifest.json']==FROZEN_MANIFEST_SHA256 and artifacts['full-window-actual-v14']==FROZEN_BINARY_SHA256,'compiled manifest/binary selection')
    for name,pin in artifacts.items():reader.read(name,pin,256<<20 if name in ('full-window-actual-v14','helper-build/source.test','helper-build/capture.test','helper-build/spool.test') else 32<<20)
    manifest_raw=reader.read('actual-manifest.json',FROZEN_MANIFEST_SHA256);m=_json(manifest_raw)
    need(m['schema']=='full-window-actual-ready.v14' and m['evidence_kind']=='actual' and m['archive_commit']==FROZEN_COMMIT and m['archive_tree']==FROZEN_TREE,'actual manifest identity')
    need(sha(n.wire(dict(sorted(m['sources'].items()))))==FROZEN_SOURCE_SHA256 and len(m['sources'])==FROZEN_FILE_COUNT and sum(x.endswith('.go') for x in m['sources'])==FROZEN_GO_COUNT,'source selection/map/counts')
    need(m['source_root']==str(root/'regression/source') and m['campaign_root']==str(root/'campaign') and m['session_root']==str(root/'records'),'new fixed root paths')
    need(m['knowledge_model']=='off' and m['knowledge_jev']=='off' and len(m['historical_claims'])==177 and m['baseline_evidence']['sha256']==BASELINE,'unchanged customer knowledge/history declaration')
    _scope(m['scope'],n.wire(m['scope']),n)
    need(m['scope_sha256']==sha(n.wire(m['scope']))==binding['scope_sha256'] and m['execution_sha256']==binding['execution_sha256'],'exact scope/execution binding')
    execution={'schema':'full-window-campaign-execution.v1','scope_sha256':m['scope_sha256'],'absolute_root':m['campaign_root'],'provider_limits':m['provider_limits']}
    need(sha(n.wire(execution))==m['execution_sha256'],'fixed execution limits/root')
    seal=_json(reader.read('source-readonly-seal.json',artifacts['source-readonly-seal.json']))
    source_reader=verify_source_seal(Path(m['source_root']),m['sources'],seal,root)
    # Technical evidence references are signed by the selected manifest. No key
    # or model settings are read; source-selected Go preparation owns full schema.
    for ref in (m['prices'],m['envelope'],m['full_go']['result']):reader.read(_relative(root,ref['path']),ref['sha256'])
    need(m['full_go']['source_sha256']==FROZEN_SOURCE_SHA256,'full Go selected source')
    att=_json(reader.read('actual-manifest.json.binary-attestation.json',artifacts['actual-manifest.json.binary-attestation.json']))
    order='schema archive_commit archive_tree ready_manifest_sha256 binary_sha256'.split()
    need(set(att)==set(order)|{'signature'} and att['schema']=='full-window-actual-binary-attestation.v1' and att['archive_commit']==FROZEN_COMMIT and att['archive_tree']==FROZEN_TREE and att['ready_manifest_sha256']==FROZEN_MANIFEST_SHA256 and att['binary_sha256']==FROZEN_BINARY_SHA256,'binary attestation identity')
    _signature(m['build_attestation_public_key'],n.wire({k:att[k] for k in order}),att['signature'])
    need(artifacts['helper-build/catalog.json']==FROZEN_HELPER_CATALOG_SHA256,'compiled helper catalog selection')
    helper_ref=m['helper_catalog'];need(set(helper_ref)=={'path','sha256'} and helper_ref['path']==str(root/'helper-build/catalog.json') and
        helper_ref['sha256']==FROZEN_HELPER_CATALOG_SHA256,'signed manifest helper catalog pointer')
    verifier=reader.read('verifier-preparation/verify-window.py',binding['verifier_sha256'])
    need(Path(__file__).absolute()==root/'verifier-preparation/verify-window.py' and Path(__file__).resolve()==Path(__file__).absolute(),'canonical executing verifier bytes')
    reader.read('owner-preparation/run-owner.py',binding['owner_sha256'])
    expected_dependencies={file:pin for file,pin in DEPENDENCIES.values()};expected_dependencies[SOURCE_HELPER]=SOURCE_HELPER_SHA256
    need(n.same(binding['dependency_sha256'],expected_dependencies),'fixed verifier dependency set')
    for name,pin in expected_dependencies.items():reader.read('verifier-preparation/'+name,pin)
    audit=_json(reader.read('technical-start-audit.json',artifacts['technical-start-audit.json']))
    _validate_technical_audit(audit,m,binding,artifacts,n)
    validate_manifest_observer(m)
    eref=m['executor_equivalence'];eraw=reader.read(_relative(root,eref['path']),eref['sha256']);_validate_executor_equivalence(m,_json(eraw),n)
    source_reader.recheck();reader.recheck()
    context=_TechnicalContext(_TECHNICAL_SEAL,root,binding,m,reader)
    context.source_reader=source_reader
    context.helper=_load_helper_artifacts(context)
    return _authenticate_execution_authority(context) if require_authority else context


def _validate_technical_audit(audit,manifest,binding,artifacts,n):
    """Pure technical preparation metadata; never execution authorization."""
    need('human_approval_required' not in audit,'obsolete exact-human approval metadata forbidden')
    expected={'schema':'full1220-v14-technical-start-audit.v1','technical_checks_passed':True,'confirmed_P1_P2':0,
        'actual_execution_authorized':False,'human_reviewed_exact_scope':False,'delegated_authorization_required':True,
        'operation_policy_approval_required':True,'operation_policy_sha256':OPERATION_POLICY_SHA256,
        'archive_commit':FROZEN_COMMIT,'archive_tree':FROZEN_TREE,'source_sha256':FROZEN_SOURCE_SHA256,
        'manifest_sha256':FROZEN_MANIFEST_SHA256,'binary_sha256':FROZEN_BINARY_SHA256,
        'scope_sha256':manifest['scope_sha256'],'execution_sha256':manifest['execution_sha256'],
        'verifier_sha256':binding['verifier_sha256'],'owner_sha256':binding['owner_sha256'],
        'helper_catalog_sha256':FROZEN_HELPER_CATALOG_SHA256}
    for key,value in expected.items():need(n.same(audit.get(key),value),'reviewed technical audit '+key)
    need(n.same(audit['artifact_sha256'],{k:v for k,v in artifacts.items() if k!='technical-start-audit.json'}),'technical audit exact artifacts')


def _load_helper_artifacts(context):
    """Manifest-selected catalog/real files only; produces no execution proof."""
    root=context.root;m=context.manifest;p=dependency('runner')
    raw=context.reader.read('helper-build/catalog.json',FROZEN_HELPER_CATALOG_SHA256)
    parsed=_json(raw)
    runtime_map=dict(m['sources'])
    extras={'internal/taskbench/full1220_source_v14_test.go':SOURCE_HELPER_SHA256,
            'internal/observability/full1220_content_projection_v8_test.go':CAPTURE_HELPER_SHA256}
    need(not set(runtime_map)&set(extras),'helper extras outside original source map')
    runtime_map.update(extras)
    expected={**parsed['identity'],'commit':FROZEN_COMMIT,'tree':FROZEN_TREE,'source_map_sha256':FROZEN_SOURCE_SHA256,
              'runtime_map_sha256':p.sha(p.canonical(runtime_map)),'go_sha256':GO_SHA256,'sdk_sha256':SDK_METADATA_SHA256,'platform':'darwin/arm64'}
    def signature(key,message,sig):
        _signature(key,message,sig)
        return True
    catalog=p.load_catalog(raw,expected_sha256=FROZEN_HELPER_CATALOG_SHA256,trusted_public_key=m['build_attestation_public_key'],
                           expected_identity=expected,verify_signature=signature)
    tool_values=list(catalog['identity']['tools'].values())
    need({'path':GO_EXECUTABLE,'sha256':GO_SHA256} in tool_values and
         {'path':str(Path(SDK_ROOT)/'SDKSettings.json'),'sha256':SDK_METADATA_SHA256} in tool_values and
         any(v['path']=='/usr/bin/clang' for v in tool_values) and any(v['path']=='/usr/bin/clang++' for v in tool_values),
         'fresh Go/SDK/clang/clang++ tools included')
    tools={name:p.pin_tool(Path(value['path'])) for name,value in catalog['identity']['tools'].items()}
    runtime=p.pin_tree(root/'helper-build/runtime',runtime_map)
    specs={'spool':('TestFull1220SpoolV14','full1220-v14-closed-spool-job.v1','full1220-v14-closed-spool-data.v1','internal/taskbench/full1220_source_v14_test.go'),
           'source':('TestFull1220SourceV14','full1220-source-v14-job.v1','full1220-source-v14-replay.v1','internal/taskbench/full1220_source_v14_test.go'),
           'capture':('TestFull1220FrozenContentProjection',CAPTURE_INPUT_SCHEMA,'full1220-content-projection.v1','internal/observability/full1220_content_projection_v8_test.go')}
    for kind,(test,inschema,outschema,helperpath) in specs.items():
        entry=next(e for e in catalog['artifacts'] if e['kind']==kind)
        need(entry['path']==f'helper-build/{kind}.test' and entry['test']==test and entry['input_schema']==inschema and
             entry['output_schema']==outschema and entry['helper_path']==helperpath and entry['helper_sha256']==extras[helperpath], 'fixed helper API/layout')
        need(entry['sha256']==context.binding['artifact_sha256'][entry['path']],'catalog/runtime binding binary SHA')
        p.select_helper(catalog,kind,artifact_root=root,runtime_tree=runtime,tool_pins=tools)
    context.reader.recheck()
    return {'runner':p,'catalog':catalog,'runtime':runtime,'tools':tools}


def verify_summary_chain(summary_raws, receipt_raws, scope, expected_latest_sha256):
    """Exact production FullWindowActualReceipt contract; no fabricated positive."""
    n=dependency('native');need(type(summary_raws) is list and len(summary_raws)==len(receipt_raws) and 0<len(summary_raws)<=456,'summary/raw receipt prefix')
    need(sha(summary_raws[-1])==expected_latest_sha256,'latest summary raw pin')
    previous='';arms=observed=quality=model=jev=0;merged=None
    additive='logical_model_calls logical_jev_calls native_model_intents native_jev_intents model_retry_intents jev_retry_intents native_settlements native_unknown native_pending'.split()
    fields='observation v8 evidence_kind acquisition_done closed next_window_index last_receipt_sha256 planned_pairs planned_arms defined_turns boundaries_excluded attempted_arms finished_arms failed_attempted_arms unattempted_arms observed_turns quality_end_unrun_turns global_unrun_turns model_attempts jev_attempts window_index report_sha256 payload_manifest_sha256 ledger_sha256 first_unrun_ordinal first_unrun_arm future_unrun_from_ordinal report_status'.split()
    for index,(sraw,rraw) in enumerate(zip(summary_raws,receipt_raws),1):
        s=_json(sraw);r=_json(rraw);need(set(s)==set(fields),'exact actual summary fields')
        done=index==456;nextordinal=scope['windows'][index-1]['last_ordinal']+1
        need(r['schema']=='full-window-acquisition-receipt.v14' and r['evidence_kind']=='recorded_run' and r['status']=='collected_window' and
             r['window_index']==index and all(r[k] is True for k in ('advanced','cleanup','environment_cleanup','observer','payload_verified')),'completed actual receipt prefix')
        need(r['attempted_arms']==r['finished_arms'] and r['unrun_arms']==0,'closed successful prefix arms')
        arms+=r['finished_arms'];observed+=r['observed_turns'];quality+=r.get('quality_end_unrun_turns',0)
        model+=r['accounting']['model_intents'];jev+=r['accounting']['jev_intents']
        runtime=copy.deepcopy(r['v8'])
        if merged is not None:
            for key in additive:runtime[key]+=merged[key]
            for key in ('configured_known','unknown_held','pending_held'):
                for currency in ('model_micro_cny','jev_micro_usd'):runtime[key][currency]+=merged[key][currency]
            runtime['unknown_intents']=merged['unknown_intents']+runtime['unknown_intents']
        merged=runtime
        expected={'observation':r['observation'],'v8':runtime,'evidence_kind':'recorded_run','acquisition_done':done,'closed':done,'next_window_index':0 if done else index+1,
            'last_receipt_sha256':previous,'planned_pairs':3275,'planned_arms':6550,'defined_turns':10504,'boundaries_excluded':0,
            'attempted_arms':arms,'finished_arms':arms,'failed_attempted_arms':0,'unattempted_arms':6550-arms,
            'observed_turns':observed,'quality_end_unrun_turns':quality,'global_unrun_turns':10504-observed-quality,
            'model_attempts':model,'jev_attempts':jev,'window_index':index,'report_sha256':sha(rraw),
            'payload_manifest_sha256':r['payload_manifest_sha256'],'ledger_sha256':r['ledger_sha256'],
            'first_unrun_ordinal':0 if done else nextordinal,'first_unrun_arm':'' if done else scope['slots'][nextordinal-1]['arm_order'][0],
            'future_unrun_from_ordinal':0 if done else nextordinal,'report_status':'collected_window'}
        need(n.same(s,expected),'actual summary chain/denominators/raw receipt/native partition')
        previous=sha(sraw)
    return merged


def _records_top_marker(root,expected_names,original=None):
    """Same original closed namespace/set/types before and after source replay."""
    records=Path(root)/'records';st=records.lstat()
    need(stat.S_ISDIR(st.st_mode) and not records.is_symlink(),'records root directory type')
    names=set()
    with os.scandir(records) as entries:
        for item in entries:
            need(len(names)<len(expected_names),'records top namespace bound')
            names.add(item.name)
    need(names==set(expected_names),'closed session namespace; stopped/future output rejected')
    entries=[]
    for name in sorted(expected_names):
        leaf=(records/name).lstat();directory=re.fullmatch(r'window-[0-9]{6}',name) is not None
        need((stat.S_ISDIR(leaf.st_mode) if directory else stat.S_ISREG(leaf.st_mode) and leaf.st_nlink==1),
             'records namespace entry type/link')
        entries.append((name,leaf.st_dev,leaf.st_ino,leaf.st_mode,leaf.st_nlink,leaf.st_uid))
    marker=(st.st_dev,st.st_ino,st.st_mode,st.st_uid,tuple(entries))
    need(original is None or marker==original,'original records top namespace identity changed')
    return marker



_WINDOW_SEAL = object()
class _VerifiedWindow:
    __slots__=('_seal','_inputs','_report','_source','_cohort')
    def __init__(self,seal,inputs,report,source,context=None):
        need(seal is _WINDOW_SEAL,'verified window issuer only')
        _require_actual_context(context)
        object.__setattr__(self,'_seal',seal)
        object.__setattr__(self,'_inputs',json.dumps(inputs,sort_keys=True,ensure_ascii=False,separators=(',',':')).encode())
        object.__setattr__(self,'_report',json.dumps(report,sort_keys=True,ensure_ascii=False,separators=(',',':')).encode())
        object.__setattr__(self,'_source',source)
        object.__setattr__(self,'_cohort',None)
    def __setattr__(self,name,value):raise AttributeError('immutable verified window')


def check_verified_window(proof, expected_inputs):
    need(type(proof) is _VerifiedWindow and proof._seal is _WINDOW_SEAL,'genuine current-process verified window required')
    need(type(proof._cohort) is VerifiedV14Cohort and proof in proof._cohort._windows,'final normal cohort membership required')
    need(window_proof(proof._cohort,json.loads(proof._report)['window_index']) is proof,'selected immutable window membership')
    inputs=json.loads(proof._inputs)
    need(dependency('native').same(inputs,expected_inputs),'verified window raw input binding')
    _check_source_proof(proof._source,{k:v for k,v in inputs.items() if k not in ('summary_sha256','binding_sha256')})
    return json.loads(proof._report)


def verify_actual_window(root=None, **kwargs):
    """Fresh opaque data-verification gate for V14 scoring, never owner authority."""
    need(False,'Use final VerifiedV14Cohort and window_proof; live audit grants no shipment')


def verify_actual(root=None, **kwargs):
    """Owner's diagnostic facade; JSON cannot reconstruct the opaque score gate."""
    proof=verify_live_window(root,**kwargs)
    return json.loads(proof._report)


def main(argv=None):
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--controlled-export');parser.add_argument('--inventory-sha256');parser.add_argument('--actual',action='store_true')
    parser.add_argument('--root');parser.add_argument('--binding-sha256');parser.add_argument('--window-index',type=int)
    parser.add_argument('--summary-sha256');parser.add_argument('--helper-output-dir');parser.add_argument('--output')
    args=parser.parse_args(argv)
    if args.actual:
        result=verify_actual(args.root,binding_sha256=args.binding_sha256,window_index=args.window_index,summary_sha256=args.summary_sha256,helper_output_dir=args.helper_output_dir)
        need(args.output is not None,'exclusive verification output required');_exclusive(Path(args.output),json.dumps(result,ensure_ascii=False,sort_keys=True).encode()+b'\n');return 0
    if not args.controlled_export:
        need(args.inventory_sha256 is None,'root required with inventory');print('inactive: no files, helpers, claims or network accessed');return 0
    need(args.inventory_sha256 is not None,'explicit controlled inventory pin required')
    print(json.dumps(verify_controlled_export(args.controlled_export,args.inventory_sha256),ensure_ascii=False,sort_keys=True))
    return 0




def _observation_declaration(d):
    n=dependency('native')
    n.fields(d,'schema private_settings_sha256 spool_root spool_run_id spool_max_bytes observation_receipt_boundary observer_identity_sha256'.split())
    need(d['schema']=='full-window-observation-declaration.v11' and d['observation_receipt_boundary']=='durable_local','V14 durable declaration')
    for k in ('private_settings_sha256','spool_run_id','observer_identity_sha256'):need(type(d[k]) is str and re.fullmatch('[0-9a-f]{64}',d[k]),'observation digest')
    need(type(d['spool_root']) is str and d['spool_root'].startswith('/') and not d['spool_root'].startswith('//') and os.path.normpath(d['spool_root'])==d['spool_root'],'observation canonical spool')
    need(type(d['spool_max_bytes']) is int and d['spool_max_bytes']==4<<30,'4GiB spool bound')


def validate_manifest_observer(m):
    d=m['scope']['observation'];_observation_declaration(d)
    codec=dependency('codec');observer=codec.observer_identity(m['observer'])
    need(d['private_settings_sha256']==m['private_settings_sha256'] and d['spool_root']==m['spool_root'] and d['spool_run_id']==observer['spool_run_id'] and d['spool_max_bytes']==observer['spool_max_bytes'] and d['observer_identity_sha256']==sha(codec._go_bytes(observer)) and observer['spool_root_sha256']==sha(m['spool_root'].encode()),'manifest/scope/settings/observer identity')
    roots=[m[k] for k in ('spool_root','source_root','session_root','campaign_root','claims_dir')]
    for i,root in enumerate(roots):
        need(type(root) is str and root.startswith('/') and os.path.normpath(root)==root,'canonical independent root')
        for other in roots[:i]:need(root!=other and not root.startswith(other+'/') and not other.startswith(root+'/'),'disjoint roots')
    need(m['knowledge_model']=='off' and m['knowledge_jev']=='off','original knowledge off both arms')


def execution_window_at(scope,index):
    """Pure mapping only: newlocal1→olddeclaration56, never a permit."""
    need(type(index) is int and 1<=index<=456 and scope.get('schema')=='full-window-scope.v14' and
         type(scope.get('windows')) is list and len(scope['windows'])==456, 'exact V14 local window index')
    w=scope['windows'][index-1]
    need(w['id']==f'window-{index+55:03d}' and 386<=w['first_ordinal']<=w['last_ordinal']<=3660,
         'local suffix declaration mapping')
    return copy.deepcopy(w)


def execution_totals(scope):
    n=dependency('native');_scope(scope,n.wire(scope),n)
    return {'Pairs':3275,'Arms':6550,'DefinedUserTurns':10504,'Boundaries':0,
            'ModelCap':110191257,'JEVCap':1818872}


def validate_observation_receipt(o,c,identity,*,final):
    need(type(o) is dict and set(o)=={'observation_receipt_boundary','remote_delivery','spool','normal_closure'},'exact observation receipt')
    need(o['observation_receipt_boundary']=='durable_local' and o['remote_delivery']=='pending','local receipt is not remote delivery')
    for k in ('Accepted','Persisted','Exported','Dropped','Failed','QueueDepth','Turns','SampledTurns','Batches'):_number(c[k])
    need(c['Accepted']>0 and c['Accepted']==c['Persisted'] and c['Exported']==c['Dropped']==c['Failed']==c['QueueDepth']==0 and c['Turns']==c['SampledTurns'],'durable local counters')
    s=o['spool'];need(type(s) is dict and set(s)==set('run_id root_sha256 max_bytes batches spans bytes stored_bytes last_link_sha256 closed poisoned data_only durability_return_unproven owner_completion_unproven'.split()),'exact spool status')
    need(s['run_id']==identity['spool_run_id'] and s['root_sha256']==identity['spool_root_sha256'] and type(s['max_bytes']) is int and s['max_bytes']==identity['spool_max_bytes']==4<<30,'selected spool identity')
    for k in ('batches','spans','bytes','stored_bytes'):_number(s[k])
    need(s['spans']==c['Persisted'] and s['batches']>0 and 0<s['bytes']<=s['stored_bytes']<=4<<30 and type(s['last_link_sha256']) is str and re.fullmatch('[0-9a-f]{64}',s['last_link_sha256']),'exact persisted coverage/count')
    need(s['poisoned'] is False and s['data_only'] is False and s['durability_return_unproven'] is False and s['owner_completion_unproven'] is False,'genuine live owner receipt required')
    need(type(final) is bool and o['normal_closure'] is final and s['closed'] is final,'normal final closure distinct from live receipt')
    return copy.deepcopy(s)


# Final artifact pins are supplied only in the independently frozen verifier,
# never by a job, receipt, or serialized proof.
_COHORT_SEAL=object()
class VerifiedV14Cohort:
    __slots__=('_seal','_context','_snapshots','_windows','_summary')
    def __init__(self,seal,context,snapshots,windows,summary=None):
        need(seal is _COHORT_SEAL,'final cohort factory only')
        _require_actual_context(context)
        for key,value in zip(self.__slots__,(seal,context,tuple(snapshots),tuple(windows),json.dumps(summary,sort_keys=True).encode())):object.__setattr__(self,key,value)
    def __setattr__(self,key,value):raise AttributeError('immutable final cohort')
    def __reduce__(self):raise TypeError('process-local cohort cannot serialize')


def _records_limits(scope,index=None):
    # Four native leaves/packet, arm recording plus two possible quality
    # sidecars/arm, receipt+payload manifest+store/window, and one native dir.
    # The whole records root adds each window dir/summary and startup/terminal.
    windows=scope['windows'] if index is None else [execution_window_at(scope,index)]
    packets=sum(w['max_model_calls']+w['max_jev_calls'] for w in windows)+40
    arms=sum(2*(w['last_ordinal']-w['first_ordinal']+1) for w in windows)
    count=len(windows);directories=count*(2 if index is None else 1)
    files=4*packets+3*arms+3*count+(count+2 if index is None else 0)
    return files+directories,directories,None


def _campaign_limits():
    # 456 claims, checkpoints, ledgers and ledger directories; 40 extras; lock.
    return 4*456+40+1,456,None


_SPOOL_CAPACITY=4<<30
# A valid protobuf batch has >=one span: trace ID (18), span ID (10),
# nonzero fixed64 start/end (9+9), three enclosing length-delimited tags (6).
# Its canonical commit contains at least trace hex (32), span hex (16),
# body SHA hex (64), even ignoring every JSON key/punctuation/other field.
# These are mathematical lower bounds, not average sizes or a batch quota.
_SPOOL_MIN_BODY_BYTES=52
_SPOOL_MIN_COMMIT_BYTES=112


def _spool_limits():
    batches=_SPOOL_CAPACITY//(_SPOOL_MIN_BODY_BYTES+_SPOOL_MIN_COMMIT_BYTES)
    return 3+2*batches,0,_SPOOL_CAPACITY


def _spool_leaf_size(name,size):
    if name=='lock':need(size==64,'spool leaf lock size');return
    if name in ('header.json','closed.json'):
        need(0<size<=4096,'spool leaf control size');return
    match=re.fullmatch(r'([0-9]{20})\.(pb|commit)',name)
    need(match is not None,'spool namespace foreign leaf')
    need(0<int(match[1])<=(_spool_limits()[0]-3)//2,'spool namespace batch bound')
    low,high=(_SPOOL_MIN_BODY_BYTES,1<<20) if match[2]=='pb' else (_SPOOL_MIN_COMMIT_BYTES,16<<20)
    need(low<=size<=high,'spool leaf physical size bound')


def _tighten_spool_snapshot(reader,summary):
    # Called only after the one genuine helper and final live-summary/coverage
    # comparison. Keep the original pre-helper reader, hashes and identities.
    batches=_number(summary['batches'],(_spool_limits()[0]-3)//2)
    need(batches<=_number(summary['spans']) and batches*_SPOOL_MIN_BODY_BYTES<=_number(summary['bytes'],_SPOOL_CAPACITY),
         'closed spool batch/span/byte bound')
    need(batches*(_SPOOL_MIN_BODY_BYTES+_SPOOL_MIN_COMMIT_BYTES)<=_number(summary['stored_bytes'],_SPOOL_CAPACITY),
         'closed spool stored byte bound')
    need(len(reader.markers)==3+2*batches,'closed spool exact helper namespace count')
    reader._inventory_limits=(3+2*batches,0,_SPOOL_CAPACITY)


def _snapshot(root,*,keep_raw=True,limits=None,spool=False):
    r=Evidence(Path(root));files={}
    if limits is not None:r._inventory_limits=limits
    if spool:r._spool_snapshot=True
    names=r.inventory()  # Reject oversized/foreign-dir trees before body reads.
    for rel in sorted(names):
        raw=r.read(rel);files[rel]=raw if keep_raw else None
    r.inventory(files);return r,files


def _fresh_snapshot(reader):
    reader.inventory(reader.markers);reader.recheck();reader.inventory(reader.markers)



def _fresh_closed_identity(reader):
    """Whole namespace and original FD identity closure; no derived disk cache."""
    reader.inventory(reader.markers)
    for name,(marker,_) in reader.markers.items():
        path=reader._path(name);need(identity(path.lstat())==marker,'closed snapshot leaf changed')
        fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
        try:need(identity(os.fstat(fd))==marker==identity(path.lstat()),'closed snapshot FD changed')
        finally:os.close(fd)
    reader.inventory(reader.markers)

def check_verified_cohort(proof):
    need(type(proof) is VerifiedV14Cohort and proof._seal is _COHORT_SEAL,'genuine current-process final V14 cohort required')
    _require_actual_context(proof._context)
    for reader in proof._snapshots:_fresh_closed_identity(reader)
    _fresh_closed_identity(proof._context.source_reader)
    proof._context.reader.recheck()
    return json.loads(proof._summary)


def window_proof(cohort,index):
    need(type(cohort) is VerifiedV14Cohort and cohort._seal is _COHORT_SEAL,'genuine final cohort required')
    need(type(index) is int and 1<=index<=456,'original window index')
    check_verified_cohort(cohort)
    proof=cohort._windows[index-1]
    records,campaign,_=cohort._snapshots
    prefix=f'window-{index:06d}/'
    for name in records.markers:
        if name.startswith(prefix) or name==f'window-{index:06d}-summary.json':records.read(name)
    for name in (f'claim-{index:06d}.json',f'checkpoint-{index:06d}.json',f'ledger-{index:06d}/accounting.jsonl'):campaign.read(name)
    # Selected raw inputs are rehashed and the whole immutable namespace/FD
    # identities are checked. Native
    # replay and source helper execution happened once during cohort factory.
    return proof








def _verify_spool_helper(context,output_dir):
    p=context.helper['runner'];h=context.helper;m=context.manifest
    selected=p.select_helper(h['catalog'],'spool',artifact_root=context.root,runtime_tree=h['runtime'],tool_pins=h['tools'])
    output=Path(output_dir);need(output.is_absolute() and output.resolve()==output and output.parent.is_dir(),'new canonical spool-helper output')
    output.mkdir(mode=0o700);(output/'tmp').mkdir(mode=0o700)
    job=dependency('native').wire({'schema':'full1220-v14-closed-spool-job.v1','root':m['spool_root'],'run_id':m['observer']['spool_run_id'],'max_bytes':4<<30})
    _exclusive(output/'input.json',job)
    env={'PATH':'/usr/bin:/bin:/usr/sbin:/sbin','TMPDIR':str(output/'tmp'),'FULL1220_V14_SPOOL_INPUT':str(output/'input.json'),'FULL1220_V14_SPOOL_INPUT_SHA256':sha(job),'FULL1220_V14_SPOOL_OUTPUT':str(output/'spool-data.json')}
    result={}
    def validate(outdir):
        raw=Evidence(output).read('spool-data.json',bound=256<<20);data=dependency('source')._Document(raw,limit=256<<20).value
        need(set(data)=={'schema','input_sha256','summary','spans'} and data['schema']=='full1220-v14-closed-spool-data.v1' and data['input_sha256']==sha(job),'closed spool helper DATA identity')
        stdout=Evidence(outdir).read('stdout',bound=8<<20)
        markers=re.findall(rb'(?m)^\s+full1220_source_v14_test\.go:[0-9]+: FULL1220_V14_SPOOL_OUTPUT_SHA256=([0-9a-f]{64})\r?$',stdout)
        need(markers==[sha(raw).encode()],'real spool helper stdout receipt');result.update(data)
    process=p.run_owned(state_root=context.root/'helper-supervision',attempt_name='spool-'+sha(str(output).encode())[:32],argv=list(selected.argv),cwd=Path(selected.cwd),env=env,executable_pin=selected.binary,inputs=[p.pin_file(output/'input.json',immutable=False),*h['tools'].values(),p.pin_file(context.root/'helper-build/catalog.json')],trees=[h['runtime']],timeout=240,phase='source',validate_output=validate)
    need(process['accepted'] is True and process['cleanup']['confirmed'] is True and process['output_validation_supplied'] is True,'closed spool helper process failed')
    return result


def validate_spool_coverage(data,terminal_spool,receipts):
    """Only DATA comparison. Cannot construct a cohort or authorize remote IO."""
    n=dependency('native');s=data['summary'];expected=copy.deepcopy(terminal_spool)
    for k in ('data_only','durability_return_unproven','owner_completion_unproven'):expected[k]=True
    need(n.same(s,expected),'closed reader DATA must match live final spool exactly')
    rows=[row for r in receipts for row in r['rows']];traces={r['trace_id'] for r in rows}
    need(len(rows)==6550 and len(traces)==len(rows),'full primary unique arm trace coverage')
    spans=data['spans'];need(type(spans) is list and len(spans)==s['spans'],'complete accepted span inventory')
    ids={};roots={}
    for span in spans:
        n.fields(span,'trace_id span_id parent_span_id start_time_unix_nano end_time_unix_nano'.split())
        t=span['trace_id'];sid=span['span_id'];parent=span['parent_span_id']
        need(t in traces and type(sid) is str and re.fullmatch('[0-9a-f]{16}',sid) and sid!='0'*16 and (t,sid) not in ids,'foreign/duplicate/invalid accepted span')
        need(type(parent) is str and (parent=='' or re.fullmatch('[0-9a-f]{16}',parent)),'span parent ID')
        start=_number(span['start_time_unix_nano']);end=_number(span['end_time_unix_nano']);need(0<start<=end,'fixed span timestamp order')
        ids[t,sid]=span
        if not parent or parent=='0'*16:roots[t]=roots.get(t,0)+1
    need(set(roots)==traces and all(v==1 for v in roots.values()),'one accepted root per original arm')
    for (trace,sid),span in ids.items():
        seen=set();parent=span['parent_span_id']
        while parent and parent!='0'*16:
            need((trace,parent) in ids and parent not in seen and parent!=sid,'closed accepted parent chain')
            seen.add(parent);parent=ids[trace,parent]['parent_span_id']
    return tuple((v['trace_id'],v['span_id'],v['parent_span_id']) for v in spans)


def _normal_owner(reader,completion_pins,binding_sha,manifest_sha):
    need(type(completion_pins) is dict and set(completion_pins)=={'owner_process_sha256','owner_exit_sha256','owner_cleanup_sha256','terminal_sha256'},'root-selected final completion pins required')
    for pin in completion_pins.values():need(type(pin) is str and re.fullmatch('[0-9a-f]{64}',pin),'root selected completion SHA')
    exit_data=_json(reader.read('actual-owner-exit.json',completion_pins['owner_exit_sha256']))
    cleanup=_json(reader.read('actual-owner-group-cleanup.json',completion_pins['owner_cleanup_sha256']))
    process=_json(reader.read('actual-owner-process.json',completion_pins['owner_process_sha256']));launch=_json(reader.read('actual-owner-launch-intent.json'))
    need(type(exit_data['exit_code']) is int and exit_data['exit_code']==0 and type(exit_data['supervisor_exit_code']) is int and exit_data['supervisor_exit_code']==0 and exit_data['supervision_failure'] is None and exit_data['supervision_signal'] is None and exit_data['relaunch_allowed'] is False,'normal supervisor/child exit required')
    need(cleanup['leader_reaped'] is True and cleanup['group_empty'] is True and cleanup['confirmed'] is True and cleanup['actions']==[] and cleanup['errors']==[] and cleanup['error_type'] is None,'normal naturally reaped empty owner required')
    need(type(process['pid']) is int and process['pid']>0 and process['pid']==process['owned_pgid']==cleanup['owned_pgid'] and process['new_session'] is True,'exact owned process identity')
    need(launch['schema']=='full1220-v14-once-owner-launch.v1' and launch['binding_sha256']==binding_sha and launch['manifest_sha256']==manifest_sha and launch['relaunch_allowed'] is False,'original once owner launch binding')
    expected_command=[str(reader.root/'full-window-actual-v14'),'--serve-actual','--epoch-v14','--manifest',str(reader.root/'actual-manifest.json'),'-authorization',str(reader.root/'execution-authorization.json'),'-private-settings',str(reader.root/'final-private-settings.json')]
    need(process['command']==expected_command and launch['binary_sha256']==FROZEN_BINARY_SHA256,'owned command/binary binding')
    return _json(reader.read('records/terminal.json',completion_pins['terminal_sha256']))


def verify_actual_cohort(root=None,*,binding_sha256=None,summary_sha256=None,completion_pins=None,helper_output_dir=None):
    """One final, complete456 source/native/closed-spool verification in-process.

    completion_pins are independently selected by the trusted root controller
    after observing supervisor reaping. No serialized report is accepted as a
    proof. This verifies DATA/normal closure, never actual remote receipt.
    """
    _selected()
    context=load_freeze_binding(root,'runtime-binding.json',binding_sha256)
    _require_actual_context(context)
    m=context.manifest;n=dependency('native');reader=context.reader;root=context.root
    terminal=_normal_owner(reader,completion_pins,binding_sha256,FROZEN_MANIFEST_SHA256)
    records_reader,all_records=_snapshot(root/'records',limits=_records_limits(m['scope']))
    campaign_reader,all_campaign=_snapshot(root/'campaign',limits=_campaign_limits())
    spool_reader,_=_snapshot(m['spool_root'],keep_raw=False,limits=_spool_limits(),spool=True)
    expected_names={'startup.json','terminal.json'}|{f'window-{i:06d}' for i in range(1,457)}|{f'window-{i:06d}-summary.json' for i in range(1,457)}
    _records_top_marker(root,expected_names)
    summaries=[all_records[f'window-{i:06d}-summary.json'] for i in range(1,457)]
    receipts=[all_records[f'window-{i:06d}/receipt.json'] for i in range(1,457)]
    cumulative=verify_summary_chain(summaries,receipts,m['scope'],summary_sha256)
    parsed=[_json(r) for r in receipts];last=_json(summaries[-1]);startup=_json(all_records['startup.json'])
    need(startup['evidence_kind']=='recorded_run' and startup['closed'] is False and startup['acquisition_done'] is False and startup['next_window_index']==1 and startup['last_receipt_sha256']=='' and startup['attempted_arms']==0,'original empty startup')
    expected_terminal={k:last[k] for k in set(startup)|{'v8','observation'}};expected_terminal['last_receipt_sha256']=sha(summaries[-1]);expected_terminal['observation']=terminal['observation']
    need(n.same(terminal,expected_terminal),'final normal terminal binds complete final summary')
    final_spool=validate_observation_receipt(terminal['observation'],parsed[-1]['observer_counters_cumulative'],m['observer'],final=True)
    helper_root=Path(helper_output_dir);need(helper_root.is_absolute() and helper_root.resolve()==helper_root and helper_root.parent.is_dir(),'new final helper root');helper_root.mkdir(mode=0o700)
    auth=dependency('codec').parse_execution_authorization(context.authority_raw)
    common={'actual-manifest.json':reader.read('actual-manifest.json',FROZEN_MANIFEST_SHA256),'execution-authorization.json':context.authority_raw,'scope.json':n.wire(m['scope']),'human-reply.txt':reader.read(_relative(root,auth['human_reply']['path']),auth['human_reply']['sha256'],1<<20)}
    windows=[];native_cache={};seen_traces=set()
    for index,r in enumerate(parsed,1):
        prefix=f'window-{index:06d}/';current={name.removeprefix(prefix):raw for name,raw in all_records.items() if name.startswith(prefix)}
        records={row['recording_file']:current[row['recording_file']] for row in r['rows']}
        sidecars=_sidecars(r['rows'],current)
        binding=source_binding(common['actual-manifest.json'],receipts[index-1],records,sidecars)
        source_proof=verify_source_v14(common['actual-manifest.json'],receipts[index-1],records,sidecars,binding,source_root=m['source_root'],source_hashes=m['sources'],output_dir=helper_root/f'source-{index:06d}',execute=True,context=context)
        selected={}
        for name,raw in all_campaign.items():
            match=re.fullmatch(r'(claim|checkpoint)-(\d{6})\.json|ledger-(\d{6})/accounting.jsonl',name)
            if name=='.lock':selected[name]=raw
            elif match:
                kind=match[1] or 'ledger';j=int(match[2] or match[3]);maximum=index+(index<456) if kind=='claim' else index
                if j<=maximum:selected[name]=raw
            elif re.fullmatch(r'extra-\d{6}\.json',name) and _extra_local_index(m['scope'],_json(raw))<=index:selected[name]=raw
        files={**common,**{'records/'+k:v for k,v in current.items()},**{'campaign/'+k:v for k,v in selected.items()}}
        report=_replay_completed_window(files,{k:sha(v) for k,v in files.items()},index,source_proof,context,_native_cache=native_cache)
        for row in r['rows']:need(row['trace_id'] not in seen_traces,'duplicate arm trace across windows');seen_traces.add(row['trace_id'])
        if index==456:need(set(selected)==set(all_campaign),'final closed campaign exact namespace')
        report.update(evidence_kind='actual_recorded_data_audit',final_normal_cohort=True,observation_receipt_boundary='durable_local',remote_delivery='pending',summary_sha256=sha(summaries[index-1]),binding_sha256=binding_sha256,whole_cohort_source_native_verified=True)
        windows.append(_VerifiedWindow(_WINDOW_SEAL,{**binding,'summary_sha256':sha(summaries[index-1]),'binding_sha256':binding_sha256},report,source_proof,context))
    need(len(native_cache)==456,'each closed ledger replayed exactly once in final factory')
    data=_verify_spool_helper(context,helper_root/'closed-spool')
    spans=validate_spool_coverage(data,final_spool,parsed)
    _tighten_spool_snapshot(spool_reader,data['summary'])
    for snap in (records_reader,campaign_reader,spool_reader):_fresh_snapshot(snap)
    reader.recheck()
    report={'schema':'full1220-v14-final-cohort-data.v1',**execution_totals(m['scope']),'manifest_sha256':FROZEN_MANIFEST_SHA256,'scope_sha256':m['scope_sha256'],'execution_sha256':m['execution_sha256'],'final_summary_sha256':sha(summaries[-1]),'completion_pins':dict(completion_pins),'observation_receipt_boundary':'durable_local','remote_delivery':'pending','scores_publish_authorized':False,'closed_spool_data_only':True,'normal_owner_verified_separately':True,'spans':list(spans),'cumulative':cumulative}
    cohort=VerifiedV14Cohort(_COHORT_SEAL,context,(records_reader,campaign_reader,spool_reader),windows,report)
    for proof in windows:object.__setattr__(proof,'_cohort',cohort)
    return cohort





def verify_live_window(root=None,*,binding_sha256=None,window_index=None,summary_sha256=None,helper_output_dir=None):
    """Current-window DATA receipt for once-owner advancement. No final proof.

    Prior summaries/receipts are checked as a cheap hash/counter chain. Prior
    native ledgers/payloads are not replayed here; final cohort verifies all once.
    """
    _selected();context=load_freeze_binding(root,'runtime-binding.json',binding_sha256)
    _require_actual_context(context);m=context.manifest;n=dependency('native');reader=context.reader
    w=execution_window_at(m['scope'],window_index);index=window_index
    summaries=[reader.read(f'records/window-{i:06d}-summary.json') for i in range(1,index+1)]
    receipts=[reader.read(f'records/window-{i:06d}/receipt.json',_json(s)['report_sha256']) for i,s in enumerate(summaries,1)]
    verify_summary_chain(summaries,receipts,m['scope'],summary_sha256)
    rr,current=_snapshot(context.root/f'records/window-{index:06d}',limits=_records_limits(m['scope'],index));r=_json(receipts[-1]);need(current['receipt.json']==receipts[-1],'live raw receipt')
    validate_observation_receipt(r['observation'],r['observer_counters_cumulative'],m['observer'],final=False)
    records={row['recording_file']:current[row['recording_file']] for row in r['rows']};sidecars=_sidecars(r['rows'],current)
    mraw=reader.read('actual-manifest.json',FROZEN_MANIFEST_SHA256);binding=source_binding(mraw,receipts[-1],records,sidecars)
    source_proof=verify_source_v14(mraw,receipts[-1],records,sidecars,binding,source_root=m['source_root'],source_hashes=m['sources'],output_dir=helper_output_dir,execute=True,context=context)
    cr=Evidence(context.root/'campaign');claimraw=cr.read(f'claim-{index:06d}.json');claim=_json(claimraw)
    need(claim['schema']=='full-window-campaign-claim.v14' and n.same(claim['window'],w) and claim['scope_sha256']==m['scope_sha256'] and claim['execution_sha256']==m['execution_sha256'],'current claim identity')
    contract=_contract(m['scope'],m,claim,sha(claimraw),n);ledger=cr.read(f'ledger-{index:06d}/accounting.jsonl');replay=n.replay_ledger(ledger,contract)
    need(replay['closed'] and replay['native_resolved'] and not replay['envelope_violation'] and n.same(r['accounting'],_accounting_state(replay,n)),'current closed accounting')
    cpraw=cr.read(f'checkpoint-{index:06d}.json');cp=_json(cpraw)
    need(cp['schema']=='full-window-campaign-owned-checkpoint.v14' and cp['claim_sha256']==sha(claimraw) and cp['ledger_sha256']==sha(ledger)==r['ledger_sha256'],'current checkpoint linkage')
    evidence,_,_=_rows({'records/'+k:v for k,v in current.items()},r,m['scope'],index,n,dependency('source'),source_proof,binding)
    need(n.same(cp['evidence'],evidence),'current source checkpoint evidence')
    payload=_json(current['payload-manifest.json']);need(sha(current['payload-manifest.json'])==r['payload_manifest_sha256'] and n.same(payload['contract'],contract),'current payload binding')
    _payload({k.removeprefix('native-payloads/'):v for k,v in current.items() if k.startswith('native-payloads/')},payload,replay,n)
    if index<456:need(r['next_claim_sha256']==sha(cr.read(f'claim-{index+1:06d}.json')),'next genuine claim receipt')
    else:need(r['next_claim_sha256']==sha(cpraw),'final checkpoint token')
    _fresh_snapshot(rr);cr.recheck();reader.recheck()
    report={'schema':'full1220-v14-live-local-data.v1','window_index':index,'receipt_sha256':sha(receipts[-1]),'summary_sha256':summary_sha256,'observation_receipt_boundary':'durable_local','remote_delivery':'pending','final_normal_cohort':False,'shipment_authorized':False,'scores_publish_authorized':False,'current_source_native_replayed':True,'prior_native_replayed':False}
    return _VerifiedWindow(_WINDOW_SEAL,{**binding,'summary_sha256':summary_sha256,'binding_sha256':binding_sha256},report,source_proof,context)




def _extra_local_index(scope,extra):
    indices=[i for i,w in enumerate(scope['windows'],1) if w['id']==extra['window_id']]
    need(len(indices)==1, 'extra belongs to one executable suffix declaration')
    return indices[0]


def _validate_executor_equivalence(manifest,e,n):
    need(type(e) is dict and set(e)=={'schema','original_sources_sha256','sources_sha256','original_sources','changed_files'} and e['schema']=='full-window-executor-equivalence.v14','V14 exact equivalence schema')
    for pin in (EQUIVALENCE_SOURCES_SHA256,EQUIVALENCE_CHANGES_SHA256):need(type(pin) is str and re.fullmatch('[0-9a-f]{64}',pin),'UNFROZEN independently reviewed exact production diff')
    before=e['original_sources'];after=manifest['sources'];changes=e['changed_files']
    need(type(before) is dict and type(after) is dict and type(changes) is dict,'exact source maps')
    need(e['original_sources_sha256']==manifest['scope']['completion_v14']['original_sources_sha256']==sha(n.wire(dict(sorted(before.items())))),'original source map')
    need(e['sources_sha256']==EQUIVALENCE_SOURCES_SHA256==sha(n.wire(dict(sorted(after.items())))) and sha(n.wire(dict(sorted(changes.items()))))==EQUIVALENCE_CHANGES_SHA256,'exact reviewed source/diff pins')
    expected={name:{'before':before.get(name,''),'after':after.get(name,'')} for name in set(before)|set(after) if before.get(name,'')!=after.get(name,'')}
    need(n.same(changes,expected),'exhaustive reviewed production diff')
    for name,pin in {**before,**after}.items():need(type(name) is str and not name.startswith('/') and all(x not in ('','.','..') for x in name.split('/')) and type(pin) is str and re.fullmatch('[0-9a-f]{64}',pin),'canonical equivalence input')

def verify_prefix_source(manifest_raw, receipt_raw, records, sidecars, expected_hashes, *,
                     source_root, source_hashes, output_dir, execute=False, context=None):
    """Fresh source-case replay using the separately authenticated V14 artifact.

    Technical preparation may provide a genuine context without execution
    authority; this returns ONLY narrow source replay evidence, never ownership.
    """
    _selected()
    need(execute is True,'explicit offline source execution required')
    _require_technical_context(context);need(context.helper is not None,'genuine V14 helper context required')
    n=dependency('native');source=dependency('source');h=context.helper;p=h['runner']
    need(sha(manifest_raw)==ORIGINAL_MANIFEST_SHA256 and sha(n.wire(dict(sorted(source_hashes.items()))))==ORIGINAL_SOURCES_SHA256,'original prefix source map')
    binding=source_binding(manifest_raw,receipt_raw,records,sidecars)
    need(n.same(expected_hashes,binding),'source original input pins')
    manifest=source._Document(manifest_raw).value;receipt=source._Document(receipt_raw).value
    need(manifest['schema']=='full-window-actual-ready.v9' and receipt['schema']=='full-window-acquisition-receipt.v8' and
         manifest['scope_sha256']==receipt['scope_sha256'] and manifest['execution_sha256']==receipt['execution_sha256'],'source V14 binding')
    validate_original_prefix_wire(manifest,receipt)
    need(manifest['scope_sha256']==ORIGINAL_SCOPE_SHA256==sha(n.wire(manifest['scope'])) and source_root==manifest['source_root'] and n.same(source_hashes,manifest['sources']),'exact original prefix scope/source')
    need(1<=receipt['window_index']<=55 and receipt['window_id']==f"window-{receipt['window_index']:03d}",'delivered original prefix only')
    rows=receipt['rows'];need(0<len(rows)<=24 and set(records)=={r['recording_file'] for r in rows},'source current rows/record set')
    expected_sidecars={}
    for row in rows:
        name=row['recording_file'];need('/' not in name and name not in ('','.','..') and sha(records[name])==row['recording_sha256'],'source record identity')
        q=row.get('quality_end')
        if q is None:source.validate_original_prefix_record_state(records[name])
        else:
            for fk,hk in [('candidate_file','candidate_sha256'),('cleanup_file','cleanup_sha256')]:
                name=q[fk];need('/' not in name and name not in ('','.','..') and name not in expected_sidecars,'source sidecar name')
                expected_sidecars[name]=q[hk]
    need(set(sidecars)==set(expected_sidecars) and not set(sidecars)&set(records),'source sidecar set')
    for name,pin in expected_sidecars.items():need(sha(sidecars[name])==pin,'source sidecar bytes')
    originals=Evidence(Path(source_root))
    for name,pin in source_hashes.items():originals.read(name,pin)
    original_names=set(source_hashes)
    if (originals.root/'.git').exists():originals.read('.git',bound=4096);original_names.add('.git')
    originals.inventory(original_names)
    selected=p.select_helper(h['catalog'],'source',artifact_root=context.root,runtime_tree=h['runtime'],tool_pins=h['tools'])
    output=Path(output_dir);need(output.is_absolute() and output.resolve()==output and output.parent.is_dir(),'canonical new source output')
    need(output!=originals.root and originals.root not in output.parents and output not in originals.root.parents,'source/output separation')
    output.mkdir(mode=0o700);rd=output/'records';rd.mkdir(mode=0o700);(output/'tmp').mkdir(mode=0o700)
    for name,raw in {**records,**sidecars}.items():_exclusive(rd/name,raw)
    proof_binding={'inputs':binding,'source_map_sha256':FROZEN_SOURCE_SHA256,'helper_sha256':SOURCE_HELPER_SHA256,
                   'go_sha256':GO_SHA256,'sdk_metadata_sha256':SDK_METADATA_SHA256,
                   'helper_catalog_sha256':FROZEN_HELPER_CATALOG_SHA256,'helper_binary_sha256':selected.binary.sha256}
    job=n.wire({'schema':'full1220-source-v14-job.v1','observation_receipt_boundary':'original_delivered_prefix','scope':manifest['scope'],'rows':rows,'binding':proof_binding});_exclusive(output/'input.json',job)
    env={'PATH':'/usr/bin:/bin:/usr/sbin:/sbin','TMPDIR':str(output/'tmp'),
         'FULL1220_V14_SOURCE_INPUT':str(output/'input.json'),'FULL1220_V14_SOURCE_INPUT_SHA256':sha(job),
         'FULL1220_V14_SOURCE_OUTPUT':str(output/'source-proof.json')}
    pins=[p.pin_file(originals.root/name,immutable=False) for name in original_names]
    pins += [p.pin_file(output/'input.json',immutable=False)]
    pins += [p.pin_file(rd/name,immutable=False) for name in {**records,**sidecars}]
    pins += list(h['tools'].values())+[p.pin_file(context.root/'helper-build/catalog.json')]
    original_records=set(records)|set(sidecars)
    def validate(outdir):
        originals.inventory(original_names);originals.recheck()
        rr=Evidence(rd)
        for name,raw in {**records,**sidecars}.items():rr.read(name,sha(raw))
        rr.inventory(original_records)
        result=Evidence(output).read('source-proof.json')
        need(n.same(source._Document(result).value,{'schema':'full1220-source-v14-replay.v1','input_sha256':sha(job),'binding':proof_binding}),'source helper exact output')
        stdout=Evidence(outdir).read('stdout',bound=8<<20)
        markers=re.findall(rb'(?m)^\s+full1220_source_v14_test\.go:[0-9]+: FULL1220_V14_SOURCE_OUTPUT_SHA256=([0-9a-f]{64})\r?$',stdout)
        need(markers==[sha(result).encode()],'source real stdout receipt')
    process=p.run_owned(state_root=context.root/'helper-supervision',attempt_name='source-'+sha(str(output).encode())[:32],
        argv=list(selected.argv),cwd=Path(selected.cwd),env=env,executable_pin=selected.binary,inputs=pins,trees=[h['runtime']],
        timeout=240,phase='source',validate_output=validate)
    need(process['accepted'] is True and process['cleanup']['confirmed'] is True and process['output_validation_supplied'] is True,'source process/output/cleanup failed; no retry/proof')
    actual_path=context.root/'helper-supervision/attempts'/('source-'+sha(str(output).encode())[:32])/process['receipt_file']
    need(n.same(_json(Evidence(actual_path.parent).read(actual_path.name)),process),'source authoritative process receipt')
    _exclusive(output/'process.json',n.wire({'path':str(actual_path),'sha256':sha(actual_path.read_bytes()),'accepted':True}))
    return _SourceProof(_SOURCE_SEAL,proof_binding)

def _prefix_rows(files, receipt, scope, index, n, source, source_proof, binding):
    w=scope['windows'][index-1]; slots=scope['slots'][w['first_ordinal']-1:w['last_ordinal']]
    expected=[(slot,arm) for slot in slots for arm in slot['arm_order']]
    need(len(receipt['rows'])==len(expected), 'complete row inventory')
    require_quality(receipt['rows'],source_proof,binding)
    traces=set(); allowed={'records/receipt.json','records/payload-manifest.json'}
    telemetry={}; negatives=observed=quality_turns=quality_arms=0; evidence=[]; clocks=[]
    quality_ordinals={row['ordinal'] for row in receipt['rows'] if row.get('quality_end') is not None}
    for row,(slot,arm) in zip(receipt['rows'],expected):
        for k in ('ordinal','pass','case_key'):need(n.same(row[k],slot[k]),'row slot identity')
        need(row['arm']==arm and all(row[k] is True for k in ('attempted','finished','cleanup','observer')),'finished row flags/order')
        need(n.same(row['defined_turns'],slot['user_turns']) and 0<_number(row['observed_turns'],slot['user_turns']), 'observed row partition')
        trace=row['trace_id'];need(type(trace) is str and re.fullmatch('[0-9a-f]{32}',trace) and trace not in traces,'unique trace identity');traces.add(trace)
        name=f"arm-{slot['ordinal']:06d}-{arm}.json"; need(row['recording_file']==name,'record filename')
        file='records/'+name;allowed.add(file);raw=files[file]
        need(sha(raw)==row['recording_sha256'],'record original SHA')
        document=source._Document(raw);record=document.value;result=record['result'];case=result['case']
        q=row.get('quality_end')
        if q is None:
            state=source.validate_original_prefix_record_state(raw);negatives+=int(state=='recorded_negative')
            need(n.same(row['observed_turns'],slot['user_turns']), 'ordinary full row turns')
        else:
            need(record['policy_id']=='full-customer-recording-policy.v1' and record['policy_sha256']==source._COLLECTION_POLICY_SHA256 and
                 record['result_sha256']==sha(document.fragment('result').encode()), 'quality original result digest')
            need(record['disposition']=='halt' and result['status']=='failed' and result['success'] is False and result['structural_pass'] is False,
                 'genuine quality state remains negative')
            need(q['policy_sha256']==scope['bindings']['policy_sha256'] and n.same(q['observed_turns'],row['observed_turns']) and
                 n.same(q['unrun_turns'],slot['user_turns']-row['observed_turns']), 'quality suffix partition')
            for filekey,hashkey in [('candidate_file','candidate_sha256'),('cleanup_file','cleanup_sha256')]:
                name=q[filekey];need('/' not in name and name not in ('','.','..'), 'quality relative sidecar')
                allowed.add('records/'+name);need(sha(files['records/'+name])==q[hashkey], 'quality sidecar bytes')
            negatives+=1;quality_arms+=1;quality_turns+=q['unrun_turns']
        need(case['source_case_id']+'/'+case['scenario']['id']==slot['case_key'] and len(case['scenario']['turns'])==slot['user_turns'],'record case identity')
        need(n.same(row['status'],result['status']) and n.same(row['success'],result['success']) and
             n.same(row['structural_pass'],result['structural_pass']) and row['disposition']==record['disposition'],'preserve original result flags')
        need(row.get('exclude_complete_task_comparison',False) is (slot['ordinal'] in quality_ordinals) and row.get('failure_evidence') is None,'no failure marker on completed ordinary row')
        need(len(result['observations'])==row['observed_turns'] and result['setup_error'] is False and n.same(result['http_failures'],0),'record observation completeness')
        server=[];driver=[]
        for turn,obs in enumerate(result['observations'],1):
            response=obs['turn'];need(n.same(response['status'],200) and not response.get('error') and not response['response'].get('error'),'turn response completed')
            tm=response['response']['telemetry'];runtime=tm['runtime']
            need(tm['langfuse_trace_id']==trace and runtime['model']=='deepseek-flash' and runtime['protocol']=='anthropic' and runtime['routing_mode']==arm and
                 n.same(runtime['max_output_tokens'],128) and runtime['thinking_mode']=='disabled' and n.same(runtime['reply_repair_limit'],1),'frozen model/routing/trace')
            _number(tm['reply_repairs'],1)
            telemetry[(slot['case_key'],slot['pass'],arm,turn)]={'model':_number(tm['llm_calls'],5),'jev':_number(tm['jev_calls'],1)}
            # Retain clock names from source. No clock is called HTTP-only.
            server.append(_duration(tm['total_latency_ms']))
            driver.append(_duration(response['latency_ms']))
        clocks.append({'ordinal':slot['ordinal'],'arm':arm,'server_turn_ms':server,'driver_turn_ms':driver,
                       'conversation_ms':_duration(result['duration_ms']),'arm_runner_ms':_duration(row['duration_ms'])})
        observed+=row['observed_turns']
        evidence.append({**({'QualityEnd':q} if q is not None else {}),'Ordinal':slot['ordinal'],'Pass':slot['pass'],'ObservedTurns':row['observed_turns'],'Key':slot['case_key'],'Arm':arm,
                         'RecordingSHA256':row['recording_sha256'],'Cleanup':True,'Observer':True})
    need(n.same(receipt['quality_negative_arms'],negatives) and n.same(receipt['observed_turns'],observed),'source negative/observed counters')
    for k,v in [('quality_end_arms',quality_arms),('quality_end_unrun_turns',quality_turns),('global_unrun_turns',0)]:need(n.same(receipt.get(k,0),v),'window suffix counters')
    need(observed+quality_turns==receipt['planned_current_turns'],'observed/quality partition')
    need({k for k in files if k.startswith('records/') and not k.startswith('records/native-payloads/')}==allowed,'orphan or missing record namespace')
    return evidence,telemetry,clocks


# Original identity is immutable; the new index only selects original raw files.
ORIGINAL_MANIFEST_SHA256='aa9a664d767c080648213e3d98c63c340d2207afea9121d104bbb2775fdc7666'
ORIGINAL_SCOPE_SHA256='e38eff5e9b79c7b1ad245cfd7e6eb09400260d7c579affece93ec1281e9976c9'
ORIGINAL_SOURCES_SHA256='47486b91579a39e5304c721a0ddc140797b4109864e79406b5b54b6831328d00'
ORIGINAL_BINDING_SHA256='541bb3c79b9d11d96edba5470a6f15dde8c6c4067abfdd1a239cb5ad137c58cb'
ORIGINAL_ROOT='/Users/a1/项目/ai-gateway/.tmp/paired-live-private/full1220-actual-v9-freeze-r5-20261031'
ORIGINAL_REGISTRY='/Users/a1/项目/ai-gateway/.tmp/full1220-score-publications/'+ORIGINAL_SCOPE_SHA256
PREFIX_INDEX_SHA256='UNFROZEN'
_PREFIX_SEAL=object()
_COMPLETION_SEAL=object()


class VerifiedDeliveredPrefix:
    __slots__=('_seal','_readers','_sources','_report','_rows','_context','_score_module','_captures')
    def __init__(self,seal,readers,sources,report,rows,context,score_module,captures):
        need(seal is _PREFIX_SEAL,'prefix factory only')
        for key,value in zip(self.__slots__,(seal,tuple(readers),tuple((p,json.dumps(b,sort_keys=True).encode()) for p,b in sources),json.dumps(report,sort_keys=True).encode(),tuple(json.dumps(row,sort_keys=True).encode() for row in rows),context,score_module,tuple((p,json.dumps(plan,sort_keys=True).encode()) for p,plan in captures))):object.__setattr__(self,key,value)
    def __setattr__(self,k,v):raise AttributeError('immutable original-prefix proof')
    def __reduce__(self):raise TypeError('prefix proof is same-process only')


class VerifiedV14Completion:
    __slots__=('_seal','_prefix','_suffix','_report','_context','_snapshots')
    def __init__(self,seal,prefix,suffix,report):
        need(seal is _COMPLETION_SEAL,'completion factory only')
        _require_actual_context(suffix._context)
        for key,value in zip(self.__slots__,(seal,prefix,suffix,json.dumps(report,sort_keys=True).encode(),suffix._context,suffix._snapshots)):object.__setattr__(self,key,value)
    def __setattr__(self,k,v):raise AttributeError('immutable V14 completion')
    def __reduce__(self):raise TypeError('completion is same-process only')


def parse_original_get(intent_raw,response_raw,body):
    """Read stored responses only; a GET never creates resend permission."""
    from urllib.parse import urlsplit,parse_qs
    intent=_json(intent_raw);response=_json(response_raw)
    need(type(body) is bytes and response.get('status')==200 and type(response.get('status')) is int and response.get('complete') is True and response.get('raw_sha256')==sha(body) and type(response.get('raw_bytes')) is int and response['raw_bytes']==len(body),'complete original HTTP200 body required')
    need(intent.get('method')=='GET' and type(intent.get('path')) is str,'stored original GET only')
    url=urlsplit(intent['path']);query=parse_qs(url.query)
    need(not url.scheme and not url.netloc and not url.fragment,'original API relative path')
    data=_json(body);need(type(data) is dict and type(data.get('data')) is list,'original GET data list')
    if url.path=='/api/public/v2/observations':
        traces=query.get('traceId');need(type(traces) is list and len(traces)==1 and re.fullmatch('[0-9a-f]{32}',traces[0]),'one original trace GET')
        return 'observations',traces[0],data['data']
    need(url.path=='/api/public/v3/scores','original score GET path')
    return 'scores',None,data['data']


def _prefix_once(plan,registry,score_rows,s):
    """Validate actual original IDs/logical intents/sent/ACK/GET, without writes."""
    n=dependency('native')
    expected=s._publication_scores(plan);batch=_json(registry.read(f"window-{plan['window_index']:06d}.batch.json"))
    integrity=dict(batch);digest=integrity.pop('batch_sha256')
    need(sha(s._json_bytes(integrity))==digest,'original once batch integrity')
    for k,v in [('schema','full1220-score-batch.v1'),('scope_sha256',ORIGINAL_SCOPE_SHA256),('manifest_sha256',ORIGINAL_MANIFEST_SHA256),('window_id',plan['window_id']),('window_index',plan['window_index']),('plan_sha256',sha(s._json_bytes(plan))),('trace_source_identity',s._batch_source_identity(plan))]:need(n.same(batch[k],v),'original once batch '+k)
    want=[{k:row[k] for k in ('id','logical_identity','traceId','name')} for row in expected]
    need(n.same(batch['scores'],want),'complete original score set; positive holes stay holes')
    actual={}
    for row in score_rows:
        need(type(row) is dict and type(row.get('id')) is str,'original GET score identity')
        if row['id'] in actual:need(n.same(actual[row['id']],row),'conflicting duplicate original score')
        actual[row['id']]=row
    for planned in expected:
        identifier=planned['id'];event=_json(registry.read('logical-'+identifier+'.intent.json'))
        data=dict(event);claimed=data.pop('intent_sha256');need(sha(s._json_bytes(data))==claimed,'original logical intent digest')
        need(event['logical_identity']==planned['logical_identity'] and event['scope_sha256']==ORIGINAL_SCOPE_SHA256 and event['window_id']==plan['window_id'] and event['batch_sha256']==digest,'once intent original binding')
        payload=event['score'];base=dict(planned);base.pop('logical_identity')
        for key in base:
            if key=='metadata':
                for mk,mv in base[key].items():need(n.same(payload[key][mk],mv),'original score metadata retained')
            else:need(n.same(payload[key],base[key]),'original score payload retained')
        need(payload['metadata']['plan_sha256']==batch['plan_sha256'],'original intent plan')
        link=_json(registry.read('score-'+identifier+'.intent.json'))
        need(link=={'logical_sha256':identifier,'intent_sha256':claimed},'original ID link')
        sent=_json(registry.read(identifier+'.sent.json'));ack=_json(registry.read(identifier+'.accepted.json'))
        need(sent=={'intent_sha256':claimed,'may_have_been_sent':True} and ack=={'intent_sha256':claimed,'ack_id':identifier},'original sent/ACK retained')
        need(identifier in actual,'original score GET missing')
        got=original_score_payload(actual[identifier])
        for key in ('id','traceId','name','value','dataType','comment','metadata'):
            if key in payload:need(n.same(got.get(key),payload[key]),'original score GET payload mismatch '+key)
    # Suppressed semantic positives have stable IDs and must remain absent.
    holes=[]
    for trace,b in plan['trace_bindings'].items():
        if b['positive_score_suppressed']:
            logical={'scope_sha256':ORIGINAL_SCOPE_SHA256,'window_id':plan['window_id'],'pass':b['pass'],'ordinal':b['ordinal'],'arm':b['arm'],'trace_id':trace,'metric':s.NAMESPACE+'semantic_task'}
            identifier=sha(s._json_bytes(logical));need(identifier not in actual,'original positive hole must stay absent')
            for name in ('logical-'+identifier+'.intent.json','score-'+identifier+'.intent.json',identifier+'.sent.json',identifier+'.accepted.json'):need(not (registry.root/name).exists(),'positive hole once journal unexpectedly present')
            holes.append(identifier)
    return len(expected),holes


def _check_delivered_prefix(proof):
    need(type(proof) is VerifiedDeliveredPrefix and proof._seal is _PREFIX_SEAL,'genuine same-module delivered prefix required')
    s=_require_selected_score_module(proof._score_module,proof._context)
    need(len(proof._captures)==55,'complete original55 genuine capture proof set')
    for projection,plan_raw in proof._captures:s._validate_projection_plan_binding(json.loads(plan_raw),projection)
    for reader in proof._readers:_recheck_prefix_reader(reader)
    for identifier in json.loads(proof._report)['positive_holes']:
        for name in ('logical-'+identifier+'.intent.json','score-'+identifier+'.intent.json',identifier+'.sent.json',identifier+'.accepted.json'):need(not (Path(ORIGINAL_REGISTRY)/name).exists(),'original positive hole filled after proof')
    for p,b in proof._sources:_check_source_proof(p,json.loads(b))
    need(sha(proof._context.reader.read('prefix-evidence-index.json',PREFIX_INDEX_SHA256))==PREFIX_INDEX_SHA256,'selected prefix index still exact')
    proof._context.reader.recheck();_fresh_closed_identity(proof._context.source_reader)
    return json.loads(proof._report)


def validate_primary_partition(prefix_rows,suffix_rows,slots):
    """Full ordered primary tuples, preserving every failure and NA row."""
    need(type(slots) is list and len(slots)==3660 and len(prefix_rows)==770 and len(suffix_rows)==6550,'exact prefix/suffix arm denominator')
    n=dependency('native')
    rows=list(prefix_rows)+list(suffix_rows);traces=set();turns=0
    expected=[(slot,arm) for slot in slots for arm in slot['arm_order']]
    for row,(slot,arm) in zip(rows,expected):
        for key in ('ordinal','pass','case_key'):need(n.same(row[key],slot[key]),'complete original primary tuple '+key)
        need(row['arm']==arm and row['defined_turns']==slot['user_turns'] and row['attempted'] is True and row['finished'] is True,'complete original primary tuple/attempt/turn')
        need(row['trace_id'] not in traces,'primary duplicate trace across epochs');traces.add(row['trace_id']);turns+=row['defined_turns']
    need(turns==11772 and len(traces)==7320,'full3660 union with11772 defined turns;1851 boundaries separate')
    return {'Pairs':3660,'Arms':7320,'DefinedUserTurns':11772,'Boundaries':1851}


def verify_delivered_prefix(context,*,index_path,helper_output_dir,score_module):
    """Genuine original source/native/semantic/once/ACK/saved-GET validation.

    The signed index contains55 rows (index,review,get triples), no asserted
    verdicts. All helpers/supervision outputs go to the NEW freeze root.
    No original body, score, once event or evidence file is written or fetched.
    """
    _require_selected_score_module(score_module,context)
    _selected();_require_technical_context(context)
    need(type(PREFIX_INDEX_SHA256) is str and re.fullmatch('[0-9a-f]{64}',PREFIX_INDEX_SHA256),'UNFROZEN original55 raw index')
    need(Path(index_path)==context.root/'prefix-evidence-index.json','fixed reviewed prefix index path')
    idx=_json(context.reader.read('prefix-evidence-index.json',PREFIX_INDEX_SHA256))
    need(set(idx)=={'schema','windows'} and idx['schema']=='full1220-v14-prefix-original-index.v2' and type(idx['windows']) is list and [x['index'] for x in idx['windows']]==list(range(1,56)),'all55 actual original window refs required')
    old=Evidence(Path(ORIGINAL_ROOT));registry=Evidence(Path(ORIGINAL_REGISTRY));external={};n=dependency('native');s=score_module
    def ref(value):
        need(type(value) is dict and set(value)=={'path','sha256'},'exact original ref')
        path=Path(value['path']);need(path.is_absolute() and path.resolve()==path and path.is_file(),'canonical original artifact ref')
        need(path!=context.root and context.root not in path.parents,'original evidence cannot come from new scope')
        reader=external.setdefault(path.parent,Evidence(path.parent));return reader.read(path.name,value['sha256'])
    mr=old.read('actual-manifest.json',ORIGINAL_MANIFEST_SHA256);m=_json(mr);scope=m['scope']
    need(m['scope_sha256']==ORIGINAL_SCOPE_SHA256==sha(n.wire(scope)) and sha(n.wire(dict(sorted(m['sources'].items()))))==ORIGINAL_SOURCES_SHA256,'exact original manifest/scope/source')
    originalsource=Evidence(Path(m['source_root']));originalsource.prefix_closed_namespace=True
    for name,pin in m['sources'].items():originalsource.read(name,pin)
    if (originalsource.root/'.git').exists():originalsource.read('.git',bound=4096)
    originalsource.inventory(originalsource.markers)
    # Immutable leaf semantic builder is selected by raw module SHA; never old owner/publisher.
    leaf=dependency('prefix_scores')
    ref({'path':'/Users/a1/项目/ai-gateway/.tmp/v9-live-score-horizon-20261031/historical-window.py','sha256':_ORIGINAL_HISTORICAL_ISSUER_SHA256})
    rows=[];sources=[];captures=[];counts=holes=observations=0;allholes=[];summaries=[];receipts=[];closed_windows=[]
    out=Path(helper_output_dir);need(out.is_absolute() and out.resolve()==out and out.parent.is_dir() and context.root in out.parents,'new prefix helper output domain');out.mkdir(mode=0o700)
    for entry in idx['windows']:
        need(set(entry)=={'index','review','plan','gets'},'prefix index row contains only raw selections')
        i=entry['index'];prefix=f'records/window-{i:06d}/';sr=old.read(f'records/window-{i:06d}-summary.json');summary=_json(sr)
        need(summary['last_receipt_sha256']==(sha(summaries[-1]) if summaries else ''),'original55 raw summary chain')
        rr=old.read(prefix+'receipt.json',summary['report_sha256']);r=_json(rr)
        need(r['schema']=='full-window-acquisition-receipt.v8' and r['window_index']==i and r['window_id']==f'window-{i:03d}' and r['evidence_kind']=='recorded_run' and r['status']=='collected_window' and all(r[k] is True for k in ('advanced','cleanup','environment_cleanup','observer','payload_verified')),'original normally collected delivered prefix')
        need(r['scope_sha256']==ORIGINAL_SCOPE_SHA256 and r['execution_sha256']==m['execution_sha256'],'original receipt binding')
        records={row['recording_file']:old.read(prefix+row['recording_file'],row['recording_sha256']) for row in r['rows']}
        sidecars={q[k]:old.read(prefix+q[k],q[k.replace('_file','_sha256')]) for row in r['rows'] if (q:=row.get('quality_end')) is not None for k in ('candidate_file','cleanup_file')}
        binding=source_binding(mr,rr,records,sidecars)
        sp=verify_prefix_source(mr,rr,records,sidecars,binding,source_root=m['source_root'],source_hashes=m['sources'],output_dir=out/f'source-{i:06d}',execute=True,context=context);sources.append((sp,binding))
        current={'records/'+k:v for k,v in {**records,**sidecars}.items()};current['records/receipt.json']=rr
        pr=old.read(prefix+'payload-manifest.json',r['payload_manifest_sha256']);payload=_json(pr);current['records/payload-manifest.json']=pr
        evidence,telemetry,clocks=_prefix_rows(current,r,scope,i,n,dependency('source'),sp,binding)
        cr=old.read(f'campaign/claim-{i:06d}.json');claim=_json(cr);lr=old.read(f'campaign/ledger-{i:06d}/accounting.jsonl')
        need(claim['schema']=='full-window-campaign-claim.v9' and claim['index']==i and n.same(claim['window'],scope['windows'][i-1]) and claim['scope_sha256']==ORIGINAL_SCOPE_SHA256 and claim['execution_sha256']==m['execution_sha256'],'original claim identity')
        need(i==1 or _json(receipts[-1])['next_claim_sha256']==sha(cr),'original next claim hash chain')
        contract=_contract(scope,m,claim,sha(cr),n);replay=n.replay_ledger(lr,contract)
        need(replay['closed'] and replay['native_resolved'] and not replay['envelope_violation'] and r['ledger_sha256']==sha(lr) and n.same(r['accounting'],_accounting_state(replay,n)),'original settled native ledger')
        cp=_json(old.read(f'campaign/checkpoint-{i:06d}.json'));validate_original_prefix_wire(m,r,cp);need(cp['schema']=='full-window-campaign-owned-checkpoint.v8' and cp['index']==i and cp['scope_sha256']==ORIGINAL_SCOPE_SHA256 and cp['execution_sha256']==m['execution_sha256'] and cp['policy_sha256']==scope['bindings']['policy_sha256'] and cp['claim_sha256']==sha(cr) and cp['ledger_sha256']==sha(lr) and n.same(cp['evidence'],evidence),'original checkpoint/source/native chain')
        need(payload['schema']=='full-window-payload.v2' and payload['evidence_kind']=='recorded_run' and n.same(payload['contract'],contract),'original payload contract')
        names={'store.json'}
        for packet in payload['packets']:
            j=packet['sequence'];names.update((f'request-{j:06d}.json',f'request-{j:06d}.body',f'response-{j:06d}.json'))
            if 'body' in packet['response']:names.add(f'response-{j:06d}.body')
        bodies={name:old.read(prefix+'native-payloads/'+name) for name in names};_payload(bodies,payload,replay,n)
        wr=Evidence(Path(ORIGINAL_ROOT)/prefix);wr.prefix_closed_namespace=True
        expected_files={**records,**sidecars,'receipt.json':rr,'payload-manifest.json':pr,**{'native-payloads/'+k:v for k,v in bodies.items()}}
        for name,raw in expected_files.items():wr.read(name,sha(raw))
        wr.inventory(expected_files);closed_windows.append(wr)
        need(sum(x['model'] for x in telemetry.values())==r['v8']['logical_model_calls'] and sum(x['jev'] for x in telemetry.values())==r['v8']['logical_jev_calls'],'original logical/native telemetry coverage')
        need(entry['review']['path']==str(Path(ORIGINAL_ROOT)/f'semantic-review/window-{i:06d}-review.json'),'original independent review path only')
        review=ref(entry['review']);anchors={'manifest_sha256':sha(mr),'summary_sha256':sha(sr),'receipt_sha256':sha(rr),'review_sha256':sha(review),'records':{k:sha(v) for k,v in records.items()}}
        original_plan_raw=ref(entry['plan']);original_plan=_json(original_plan_raw);original_batch=_json(registry.read(f'window-{i:06d}.batch.json'))
        need(sha(leaf._json_bytes(original_plan))==original_batch['plan_sha256'],'once-bound original publication plan before historical replay')
        verify_original_horizon_data(old,original_plan,receipts)
        plan=rebuild_original_prefix_plan(leaf,(mr,sr,rr,records,review,anchors,ORIGINAL_BINDING_SHA256),original_plan_raw,original_batch)
        captured={};score_rows=[]
        need(type(entry['gets']) is list and 0<len(entry['gets'])<=128,'bounded original GET refs')
        for get in entry['gets']:
            need(set(get)=={'intent','response','body'},'original GET triple')
            kind,trace,data=parse_original_get(*(ref(get[k]) for k in ('intent','response','body')))
            if kind=='scores':score_rows.extend(data)
            else:
                need(trace not in captured,'exact once selected complete original trace');captured[trace]=data
        need(set(captured)==set(plan['trace_bindings']),'all original observation GET bodies present')
        projection=s.project_prefix_content(mr,rr,records,pr,plan,source_proof=sp,verifier=sys.modules.get(__name__) or None,context=context,output_dir=out/f'capture-{i:06d}',private_settings_path=Path(ORIGINAL_ROOT)/'final-private-settings.json')
        class StoredOnly:
            def observations(self,trace):return captured[trace]
        s._validate_projection_plan_binding(plan,projection)
        validation=s.validate_window_traces(plan,projection,StoredOnly());need(validation['all_pass'] is True,'strict original stored parent/content/native/tool/turn validation')
        captures.append((projection,plan))
        count,suppressed=_prefix_once(plan,registry,score_rows,leaf);counts+=count;allholes+=suppressed
        observations+=sum(len(v) for v in captured.values());rows.extend(r['rows']);summaries.append(sr);receipts.append(rr)
    need(counts==2300 and len(allholes)==len(set(allholes))==10 and observations==6849,'original2300 scores6849 observations10 unchanged holes')
    need(sha(summaries[-1])=='42be06b9399b999bb83e939c0722e0e2d0c952608af529ed923c29046f00206d','original55 summary pin')
    need(len(rows)==770 and sum(x['defined_turns'] for x in rows)==1268,'original385 delivered prefix complete')
    for reader in (old,registry,originalsource,*closed_windows,*external.values()):_recheck_prefix_reader(reader)
    report={'schema':'full1220-v14-delivered-original-prefix.v1','Pairs':385,'Arms':770,'DefinedUserTurns':1268,'scores':2300,'observations':6849,'positive_holes':allholes,'original_manifest_sha256':ORIGINAL_MANIFEST_SHA256,'original_scope_sha256':ORIGINAL_SCOPE_SHA256,'prefix_index_sha256':PREFIX_INDEX_SHA256,'new_POSTs':0,'new_GETs':0,'source_native_semantic_once_ACK_GET_verified':True}
    return VerifiedDeliveredPrefix(_PREFIX_SEAL,(old,registry,originalsource,*closed_windows,*external.values()),sources,report,rows,context,s,captures)


def verify_actual_completion(root=None,*,binding_sha256=None,summary_sha256=None,completion_pins=None,helper_output_dir=None,score_module=None):
    """One selected same-process delivered385 + normally completed3275 proof."""
    _require_selected_score_module(score_module)
    _selected()
    suffix=verify_actual_cohort(root,binding_sha256=binding_sha256,summary_sha256=summary_sha256,completion_pins=completion_pins,helper_output_dir=helper_output_dir)
    context=suffix._context;prefix=verify_delivered_prefix(context,index_path=context.root/'prefix-evidence-index.json',helper_output_dir=Path(helper_output_dir)/'original-prefix',score_module=score_module)
    suffixrows=[]
    for i in range(1,457):suffixrows.extend(_json(suffix._snapshots[0].read(f'window-{i:06d}/receipt.json'))['rows'])
    union=validate_primary_partition([json.loads(row) for row in prefix._rows],suffixrows,context.manifest['scope']['slots'])
    report={**json.loads(suffix._summary),**union,'schema':'full1220-v14-complete-composition.v1','prefix':_check_delivered_prefix(prefix),'suffix':check_verified_cohort(suffix),'measurement_epochs':'original_R5_prefix_and_new_V14_suffix','homogeneous_contemporaneous_measurement':False,'historical_attempts_are_primary':False}
    proof=VerifiedV14Completion(_COMPLETION_SEAL,prefix,suffix,report);check_verified_completion(proof,{});return proof


def check_verified_completion(proof,expected_binding):
    need(type(proof) is VerifiedV14Completion and proof._seal is _COMPLETION_SEAL,'genuine same-module V14 completion required')
    prefix=_check_delivered_prefix(proof._prefix);suffix=check_verified_cohort(proof._suffix);report=json.loads(proof._report)
    need(report['prefix']==prefix and report['suffix']==suffix,'original prefix/normal suffix identity retained')
    for key,value in expected_binding.items():need(dependency('native').same(report[key],value),'selected completion binding '+key)
    need(suffix['Pairs']==3275 and suffix['Arms']==6550 and suffix['DefinedUserTurns']==10504 and suffix['Boundaries']==0 and suffix['normal_owner_verified_separately'] is True,'normal complete456 suffix only')
    return report


def completion_window_proof(completion,index):
    check_verified_completion(completion,{})
    return window_proof(completion._suffix,index)


def original_score_payload(row):
    need(type(row) is dict and type(row.get('subject')) is dict and row['subject'].get('kind')=='trace','original V3 trace score subject')
    trace=row['subject'].get('id');need(type(trace) is str and re.fullmatch('[0-9a-f]{32}',trace),'original score trace ID')
    need('traceId' not in row or row['traceId']==trace,'conflicting score subject')
    return {**row,'traceId':trace}





_score_module=None

def score_selection_signing_bytes(payload):
    """Root signs AFTER verifier/score rendering. No mutual embedded SHA cycle."""
    keys=('schema','root','manifest_sha256','verifier_sha256','score_sha256')
    need(type(payload) is dict and set(payload)==set(keys),'exact score selection payload')
    return b'full1220-v14-score-module-selection.v1\n'+dependency('codec')._go_bytes({k:payload[k] for k in keys})


def load_score_module(*,context=None):
    """Canonical root-signed score loader; technical preparation is sufficient.

    No execution authorization, owner or suffix records are required. The
    already pinned manifest's build key authenticates the external selection
    envelope AFTER both rendered files exist, avoiding circular source hashes.
    """
    global _score_module
    _selected()
    if context is None:
        need(_score_module is not None,'load score module first with genuine technical preparation context')
        context=_score_module._v14_selection_context
    _require_technical_context(context);need(context.root==Path(FROZEN_ROOT),'genuine selected technical context')
    reader=context.reader;root=context.root
    envelope=_json(reader.read('score-preparation/selection.json',bound=65536))
    need(type(envelope) is dict and set(envelope)=={'payload','signature'},'signed score selection envelope')
    payload=envelope['payload'];message=score_selection_signing_bytes(payload)
    verifier_sha=sha(reader.read('verifier-preparation/verify-window.py',context.binding['verifier_sha256'],4<<20))
    need(payload['schema']=='full1220-v14-score-module-selection.v1' and payload['root']==str(root) and payload['manifest_sha256']==FROZEN_MANIFEST_SHA256 and payload['verifier_sha256']==verifier_sha,'score selection exact root/manifest/verifier')
    need(type(payload['score_sha256']) is str and re.fullmatch('[0-9a-f]{64}',payload['score_sha256']),'selected score SHA')
    _signature(context.manifest['build_attestation_public_key'],message,envelope['signature'])
    raw=reader.read('score-preparation/scores.py',payload['score_sha256'],4<<20)
    current=sys.modules.get(__name__);need(current is not None and getattr(current,'__file__',None)==__file__,'registered owning verifier required')
    if _score_module is None:
        module=types.ModuleType('full1220_v14_selected_scores_'+verifier_sha[:16]);module.__file__=str(root/'score-preparation/scores.py')
        sys.modules[module.__name__]=module
        try:exec(compile(raw,module.__file__,'exec'),module.__dict__)
        except BaseException:
            sys.modules.pop(module.__name__,None);raise
        need(module.FROZEN_ROOT==str(root) and module.FROZEN_VERIFIER_SHA256==verifier_sha and module.FROZEN_BINDING_SHA256==sha(reader.read('runtime-binding.json')),'score selected verifier/root/binding')
        module._cohort_verifier_module=current;module._v14_selection_context=context
        _score_module=module
    need(type(_score_module) is types.ModuleType and sys.modules.get(_score_module.__name__) is _score_module and _score_module.__file__==str(root/'score-preparation/scores.py') and _score_module._cohort_verifier_module is current,'selected owning score module identity')
    reader.recheck()
    return _score_module


def _require_selected_score_module(module,context=None):
    # Reject attacker-supplied objects/modules BEFORE any helper or original IO.
    need(type(module) is types.ModuleType and module is _score_module and sys.modules.get(module.__name__) is module,'selected owning score module required')
    need(load_score_module(context=context) is module,'fresh authenticated selected score module required')
    return module


def validate_original_prefix_wire(manifest,receipt,checkpoint=None):
    need(manifest['schema']=='full-window-actual-ready.v9' and manifest['scope']['schema']=='full-window-scope.v9' and manifest['scope_sha256']==ORIGINAL_SCOPE_SHA256,'exact original V9 manifest/scope')
    i=receipt['window_index']
    need(receipt['schema']=='full-window-acquisition-receipt.v8' and type(i) is int and 1<=i<=55 and receipt['window_id']==f'window-{i:03d}' and receipt['scope_sha256']==manifest['scope_sha256'] and receipt['execution_sha256']==manifest['execution_sha256'],'exact original V8 receipt in V9 prefix')
    if checkpoint is not None:need(checkpoint['schema']=='full-window-campaign-owned-checkpoint.v8' and checkpoint['index']==i and checkpoint['scope_sha256']==manifest['scope_sha256'] and checkpoint['execution_sha256']==manifest['execution_sha256'],'exact original V8 checkpoint in V9 prefix')


def _recheck_prefix_reader(reader):
    if getattr(reader,'prefix_closed_namespace',False):reader.inventory(reader.markers)
    reader.recheck()
    if getattr(reader,'prefix_closed_namespace',False):reader.inventory(reader.markers)





_ORIGINAL_HISTORICAL_ISSUER_SHA256='4d1af5e1b8c8f4e0814774dca3b0e554468b5ca67eb324490609604bd34d8b76'

def rebuild_original_prefix_plan(leaf,args,published_raw,batch):
    """Reconcile original publication DATA; never reconstitute its opaque proof.

    The old wrapper added historical provenance AFTER constructing all row/score
    metadata. Preserve those original bytes/IDs while checking the entire pure
    semantic core and independently checking the referenced raw horizon below.
    """
    core=leaf._build(*args);published=_json(published_raw);n=dependency('native')
    need(sha(leaf._json_bytes(published))==batch['plan_sha256'],'original once-bound complete published plan')
    normalized=copy.deepcopy(published);provenance=normalized['provenance'];original=published['provenance']
    need(original['actual_source_and_native_replay']=='verified_by_historical_score_issuer_and_original_v9_horizon_proof' and original['historical_issuer_sha256']==_ORIGINAL_HISTORICAL_ISSUER_SHA256,'original historical provenance DATA identity')
    horizon=original['historical_horizon_index'];need(type(horizon) is int and published['window_index']<=horizon<=55,'original completed historical horizon only')
    for key in ('historical_horizon_summary_sha256','historical_horizon_files_sha256'):need(type(original[key]) is str and re.fullmatch('[0-9a-f]{64}',original[key]),'original historical raw reference')
    for key in ('historical_horizon_index','historical_horizon_summary_sha256','historical_horizon_files_sha256','historical_issuer_sha256'):provenance.pop(key)
    provenance['actual_source_and_native_replay']=core['provenance']['actual_source_and_native_replay']
    need(n.same(normalized,core),'complete original semantic plan/source/metadata/IDs mismatch')
    return published


def verify_original_horizon_data(reader,plan,receipts):
    """Exact old records/campaign raw map hash, without old scope/factory IO."""
    p=plan['provenance'];h=p['historical_horizon_index'];source=dependency('prefix_scores');expected={};horizon_receipts=[]
    need(type(h) is int and plan['window_index']<=h<=55 and p['historical_issuer_sha256']==_ORIGINAL_HISTORICAL_ISSUER_SHA256,'original bounded historical provenance')
    for key in ('historical_horizon_summary_sha256','historical_horizon_files_sha256'):need(type(p[key]) is str and re.fullmatch('[0-9a-f]{64}',p[key]),'original historical digest')
    def take(name,expected_sha=None):
        raw=reader.read(name,expected_sha);expected[name]=sha(raw);return raw
    take('records/startup.json')
    for j in range(1,h+1):
        summary=_json(take(f'records/window-{j:06d}-summary.json',p['historical_horizon_summary_sha256'] if j==h else None))
        prefix=f'records/window-{j:06d}/'
        rr=take(prefix+'receipt.json',summary['report_sha256']);r=_json(rr);horizon_receipts.append(rr)
        if j<=len(receipts):need(rr==receipts[j-1],'same selected original horizon receipt')
        payload=_json(take(prefix+'payload-manifest.json',r['payload_manifest_sha256']))
        need(type(r['rows']) is list and 0<len(r['rows'])<=24 and type(payload['packets']) is list and len(payload['packets'])<=172,'bounded original horizon declared records')
        for row in r['rows']:
            take(prefix+row['recording_file'],row['recording_sha256'])
            if (q:=row.get('quality_end')) is not None:
                for fk,hk in [('candidate_file','candidate_sha256'),('cleanup_file','cleanup_sha256')]:take(prefix+q[fk],q[hk])
        take(prefix+'native-payloads/store.json')
        for sequence,packet in enumerate(payload['packets'],1):
            need(packet['sequence']==sequence,'original horizon ordered native sequence')
            for name in (f'request-{sequence:06d}.json',f'request-{sequence:06d}.body',f'response-{sequence:06d}.json'):take(prefix+'native-payloads/'+name)
            if 'body' in packet['response']:take(prefix+f'native-payloads/response-{sequence:06d}.body')
        take(f'campaign/checkpoint-{j:06d}.json');take(f'campaign/ledger-{j:06d}/accounting.jsonl')
    need(take('campaign/.lock')==b'','original lock bytes, not ownership')
    for j in range(1,h+2):
        pin=_json(horizon_receipts[j-2])['next_claim_sha256'] if j>1 else None
        take(f'campaign/claim-{j:06d}.json',pin)
    need(sha(source._json_bytes(dict(sorted(expected.items()))))==p['historical_horizon_files_sha256'],'exact original historical horizon raw map DATA')
    return expected


if __name__=='__main__':raise SystemExit(main())
