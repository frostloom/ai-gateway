"""Independent V9 window data audit. No journal/JSON can mint an owned proof.

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
    'codec': ('full1220-v9-contracts.py', 'ca4b5810f36e7599ea30bf7c51b9446609153e4a25c8ccfe35253b59922aaf6d'),
    'source': ('full1220-v8-source.py', '9ec4994cee9b9a845487b53dd5bc6bcc8ce91fb05e2536cdba78ffb2978fa9da'),
    'runner': ('full1220-v9-helper-runtime.py', '59d0997208f697001dd681d87f3f01ad164c28c27c898c068675b765e93edeb5'),
}
BASELINE = '2f87853fe1a8df139a2093e1a34d91eb8758cd6ea547755e02d80bb98d3b2a3c'
BUDGET = 'd6584f9462b6ffbfe94acede036e0b62de536fe04aacc4bb57394ccd71184664'
RETRY = '6a2fb5869eed58f8ed2fee18b6a471ddbb264bffa56f8aabbc6b4eac95598d63'
CAPS = 'e75a0f693700f12d95bb7bf29a2c32d6d7f11e0890433fca3ad49fbf407a244a'
HISTORICAL_MODEL = 52654504
HISTORICAL_JEV = 408701
FROZEN_ROOT = FROZEN_MANIFEST_SHA256 = FROZEN_SOURCE_SHA256 = 'UNFROZEN'
FROZEN_BINARY_SHA256 = FROZEN_NODE_SHA256 = 'UNFROZEN'
FROZEN_HELPER_CATALOG_SHA256 = 'UNFROZEN'


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

    def inventory(self, expected):
        self._root_check(); found=set(); directories=[self.root]
        while directories:
            directory=directories.pop()
            for item in directory.iterdir():
                name=item.relative_to(self.root).as_posix(); st=item.lstat()
                need(not stat.S_ISLNK(st.st_mode), 'inventory symlink')
                if stat.S_ISDIR(st.st_mode):
                    need(any(x.startswith(name+'/') for x in expected), 'orphan directory')
                    directories.append(item)
                else:
                    need(stat.S_ISREG(st.st_mode) and st.st_nlink==1, 'inventory file type/link')
                    found.add(name)
                need(len(found)+len(directories)<=100000, 'inventory bound')
        need(found==set(expected), 'closed namespace inventory mismatch')

    def recheck(self):
        self._root_check()
        for name in list(self.markers): self.read(name,bound=self.bounds[name])


def dependency(name):
    file,pin=DEPENDENCIES[name]; reader=Evidence(Path(__file__).resolve().parent)
    raw=reader.read(file,pin); module=types.ModuleType('v9_'+name)
    module.__file__=str(reader.root/file)
    if name=='runner':sys.modules[module.__name__]=module
    exec(compile(raw,module.__file__,'exec'),module.__dict__)
    reader.recheck()
    return module


def require_quality(rows, proof=None, binding=None):
    # The zero-quality path must not inspect a helper, runtime, path or marker.
    if not any(row.get('quality_end') is not None for row in rows): return None
    need(binding is not None, 'genuine V9 source proof binding required')
    return _check_source_proof(proof,binding)




def source_document(raw): return dependency('source')._Document(raw)


def _json(raw): return source_document(raw).value


def _number(value, upper=(1 << 63)-1):
    need(type(value) is int and 0 <= value <= upper, 'integer counter bound'); return value


def _duration(value):
    need(type(value) in (int,float) and math.isfinite(value) and value >= 0, 'finite duration'); return value


def _scope(scope, raw, n):
    need(scope['schema']=='full-window-scope.v9' and scope['baseline_evidence_sha256']==BASELINE, 'V9 scope/stopped46 baseline')
    need(scope['allow_paid'] is False and scope['ready_for_paid_run'] is False and scope['budget_declaration']['human_authorized'] is False,
         'declarations are not authorization')
    need(scope['budget_declaration_sha256']==BUDGET==sha(n.wire(scope['budget_declaration'])) and
         scope['retry_policy_sha256']==RETRY==sha(n.wire(scope['retry_declaration'])), 'fixed V9 budget/unchanged retry declaration')
    need(type(scope['width']) is int and scope['width']==12 and len(scope['slots'])==3660 and len(scope['windows'])==511, 'full511 original schedule')
    need(scope['odd_pass_first_arm_imbalance'] is True,'original odd-pass imbalance')
    need(sha(n.wire(scope['slots']))==scope['schedule_sha256']=='95469ad60b07de7f11eeaf16c638fbc8e9b677e708eb3b4f7247246535859ccf', 'fixed greedy511 schedule digest')
    caps=[{k:w[k] for k in ('model_cap_micro_cny','jev_cap_micro_usd')} for w in scope['windows']]
    need(sha(n.wire(caps))==CAPS, 'fixed511 cap vector')
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
    for got,want in zip(scope['windows'],windows):
        need(n.same({k:got[k] for k in want},want), 'exact greedy511 window declaration')
    need(sha(raw)==sha(n.wire(scope)), 'canonical scope bytes')


def _contract(scope, manifest, claim, claim_sha, n):
    w=claim['window']; limits=manifest['provider_limits']
    expected_limits={'model':{'model':'deepseek-flash','max_request_bytes':32768,'max_input_tokens':36864,'max_output_tokens':128,'input_micro_per_million':2000000,'output_micro_per_million':8000000,'cache_hit_input_micro_per_million':40000},
                     'jev':{'model':'jev-1.13.0','max_request_bytes':4096,'max_input_tokens':64000,'max_output_tokens':1000000,'input_micro_per_million':42000,'output_micro_per_million':0}}
    need(n.same(limits,expected_limits),'fixed V9 provider/cache rates and envelopes')
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
    """Fixed46 liabilities plus exactly selected base and funded extra holds."""
    return n.amounts(HISTORICAL_MODEL+base['model_micro_cny']+sum(e['amount']['model_micro_cny'] for e in extras),
                     HISTORICAL_JEV+base['jev_micro_usd']+sum(e['amount']['jev_micro_usd'] for e in extras))


def _verify_advance_token(index,token,claim_shas,last_checkpoint_raw):
    """Go advance returns next claim normally, FINAL checkpoint at window511.

    Finalization poisons the completed campaign without creating claim512. The
    collector writes the returned token unchanged despite its NextClaim name.
    """
    need(type(index) is int and 1<=index<=511 and type(claim_shas) is list and
         len(claim_shas)==index+int(index<511),'exact base-claim prefix; no final claim512')
    need(all(type(s) is str and re.fullmatch('[0-9a-f]{64}',s) for s in claim_shas),'base claim digest prefix')
    need(type(last_checkpoint_raw) is bytes,'original last checkpoint bytes')
    expected=claim_shas[-1] if index<511 else sha(last_checkpoint_raw)
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
            state=source.validate_completed_record_state(raw);negatives+=int(state=='recorded_negative')
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


def _replay_completed_window(files, expected_hashes, index, source_proof=None, actual_context=None):
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
    if actual:need(type(actual_context) is _ActualContext and actual_context.seal is _ACTUAL_SEAL and actual_context.authority_raw is not None,'authenticated actual context')
    manifest_raw=take('actual-manifest.json' if actual else 'synthetic-manifest.json');manifest=source._Document(manifest_raw).value
    need(manifest['schema']=='full-window-actual-ready.v9' and manifest['evidence_kind']==('actual' if actual else 'local_stub'),'explicit manifest mode')
    need(n.same(manifest['scope'],scope) and manifest['scope_sha256']==sha(scope_raw),'manifest exact scope')
    auth_raw=take('execution-authorization.json' if actual else 'synthetic-authorization.json');auth=codec.parse_execution_authorization(auth_raw)
    need(auth['manifest_sha256']==sha(manifest_raw) and auth['scope_sha256']==manifest['scope_sha256'] and
         auth['execution_sha256']==manifest['execution_sha256'],'auth data bindings')
    need(auth['source_sha256']==sha(n.wire(dict(sorted(manifest['sources'].items())))),'source map data binding')
    need(sha(take('human-reply.txt' if actual else 'synthetic-reply.txt'))==auth['human_reply']['sha256'],'synthetic reply raw SHA')
    execution={'schema':'full-window-campaign-execution.v1','scope_sha256':manifest['scope_sha256'],
               'absolute_root':manifest['campaign_root'],'provider_limits':manifest['provider_limits']}
    need(sha(n.wire(execution))==manifest['execution_sha256'],'campaign execution identity')
    need(type(index) is int and 1<=index<=511,'window index')
    receipt=data('records/receipt.json');w=scope['windows'][index-1]
    need(receipt['schema']=='full-window-acquisition-receipt.v8' and receipt['evidence_kind']==('recorded_run' if actual else 'local_stub') and receipt['status']=='collected_window','completed V9 local_stub receipt required')
    need(n.same(receipt['window_index'],index) and receipt['window_id']==w['id'],'receipt window identity')
    for k in ('scope_sha256','execution_sha256'):need(receipt[k]==manifest[k],'receipt '+k)
    for k in ('cleanup','environment_cleanup','observer','advanced','payload_verified'):need(receipt[k] is True,'receipt complete flag '+k)
    for k in ('first_failure_stage','first_failure_arm'):need(receipt[k]=='','completed failure stage')
    need(n.same(receipt['first_failure_ordinal'],0) and n.same(receipt['unrun_arms'],0),'completed failure counters')
    for k,v in [('planned_campaign_pairs',3660),('planned_campaign_arms',7320),('planned_campaign_turns',11772),('operation_limit_seconds',300)]:need(n.same(receipt[k],v),'receipt original bounds')
    currentpairs=w['last_ordinal']-w['first_ordinal']+1;currentturns=sum(s['user_turns']*2 for s in scope['slots'][w['first_ordinal']-1:w['last_ordinal']])
    for k,v in [('planned_current_pairs',currentpairs),('planned_current_arms',2*currentpairs),('planned_current_turns',currentturns),('attempted_arms',2*currentpairs),('finished_arms',2*currentpairs)]:need(n.same(receipt[k],v),'receipt current denominator')
    _duration(receipt['duration_ms']);need(type(receipt['operation_deadline']) is str and bool(receipt['operation_deadline']),'operation deadline')
    counters=receipt['observer_counters_cumulative']
    for k in ('Accepted','Dropped','Exported','Failed','Batches','Turns','SampledTurns','QueueDepth'):_number(counters[k])
    need(counters['Accepted']==counters['Exported'] and counters['Accepted']>0 and all(counters[k]==0 for k in ('Dropped','Failed','QueueDepth')),'observer closure counters')
    need(counters['Turns']>=receipt['observed_turns'] and counters['SampledTurns']>=receipt['observed_turns'],'observer turn coverage')
    # Full raw prefix of ledger/base/extra/checkpoint data; no cached report is
    # described as a full prior raw replay. Prior semantic records are not claimed.
    claims=[];claim_shas=[];checkpoints=[];replays=[]
    maximum_claim=index+(index<511)
    for j in range(1,maximum_claim+1):
        name=f'campaign/claim-{j:06d}.json';raw=take(name);claim=source._Document(raw).value
        n.fields(claim,'schema baseline_evidence_sha256 budget_approval_sha256 parent_model_micro_cny parent_jev_micro_usd root index scope_sha256 execution_sha256 definition_sha256 manifest_sha256 selection_sha256 profile_sha256 schedule_sha256 policy_sha256 previous_claim_sha256 previous_checkpoint_sha256 window historical_claims permanent_model_micro_cny permanent_jev_micro_usd'.split(),('previous_claim_sha256','previous_checkpoint_sha256'))
        need(raw==n.wire(claim)+b'\n','canonical base claim bytes')
        need(claim['schema']=='full-window-campaign-claim.v9' and n.same(claim['index'],j) and
             claim['baseline_evidence_sha256']==BASELINE and claim['budget_approval_sha256']==sha(auth_raw),'base authority data binding')
        need(claim['root']==manifest['campaign_root'] and n.same(claim['window'],scope['windows'][j-1]) and
             n.same(claim['historical_claims'],manifest['historical_claims']) and len(claim['historical_claims'])==46,'base root/window/history data')
        for k in ('scope_sha256','execution_sha256'):need(claim[k]==manifest[k],'base '+k)
        for k in ('definition_sha256','manifest_sha256','selection_sha256','profile_sha256','schedule_sha256'):need(claim[k]==scope[k],'base source '+k)
        need(claim['policy_sha256']==scope['bindings']['policy_sha256'] and n.same(claim['parent_model_micro_cny'],180000000) and n.same(claim['parent_jev_micro_usd'],2500000),'base policy/parent')
        need(claim.get('previous_claim_sha256','')==(claim_shas[-1] if j>1 else ''),'base previous chain')
        expected_checkpoint=sha(take(f'campaign/checkpoint-{j-1:06d}.json')) if j>1 else ''
        need(claim.get('previous_checkpoint_sha256','')==expected_checkpoint,'base checkpoint chain')
        claims.append(claim);claim_shas.append(sha(raw))
        if j<=index:
            contract=_contract(scope,manifest,claim,sha(raw),n)
            ledger=take(f'campaign/ledger-{j:06d}/accounting.jsonl');r=n.replay_ledger(ledger,contract)
            need(r['closed'] and r['native_resolved'] and not r['envelope_violation'],'unresolved/unclean closed ledger')
            replays.append(r)
            cpraw=take(f'campaign/checkpoint-{j:06d}.json');cp=source._Document(cpraw).value
            n.fields(cp,'v8 schema index scope_sha256 execution_sha256 definition_sha256 schedule_sha256 policy_sha256 claim_sha256 ledger_sha256 evidence'.split())
            need(cpraw==n.wire(cp)+b'\n','canonical checkpoint bytes')
            need(cp['schema']=='full-window-campaign-owned-checkpoint.v8' and n.same(cp['index'],j) and cp['claim_sha256']==sha(raw) and cp['ledger_sha256']==sha(ledger),'checkpoint ledger/base identity')
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
        prior_extras=[e for e in extras if int(e['window_id'].split('-')[1])<j]
        base=n.amounts(sum(x['window']['model_cap_micro_cny'] for x in claims[:j]),sum(x['window']['jev_cap_micro_usd'] for x in claims[:j]))
        total=_permanent(base,prior_extras,n)
        need(n.same(claim['permanent_model_micro_cny'],total['model_micro_cny']) and n.same(claim['permanent_jev_micro_usd'],total['jev_micro_usd']),'base cumulative permanent including prior extras')
        if j<=index:
            window_extras=[e for e in extras if int(e['window_id'].split('-')[1])<=j]
            total=_permanent(base,window_extras,n)
            need(n.same(checkpoints[j-1]['v8'],_runtime(replays[j-1],base,window_extras,total,n)), 'checkpoint before next base runtime')
    # Receipt captured after successful advance and pre-reserving the next base.
    base=n.amounts(sum(c['window']['model_cap_micro_cny'] for c in claims),sum(c['window']['jev_cap_micro_usd'] for c in claims))
    total=_permanent(base,extras,n)
    need(total['model_micro_cny']+2023424<=180000000 and total['jev_micro_usd']+65<=2500000,'parent permanent plus unchanged risk')
    runtime=_runtime(r,base,extras,total,n);need(n.same(receipt['v8'],runtime),'receipt after next base runtime')
    record_bytes={row['recording_file']:files['records/'+row['recording_file']] for row in receipt['rows']}
    sidecars={q[k]:files['records/'+q[k]] for row in receipt['rows'] if (q:=row.get('quality_end')) is not None for k in ('candidate_file','cleanup_file')}
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
         payload['evidence_kind']==('recorded_run' if actual else 'local_stub') and payload['completion_proof']=='unavailable_from_payload_capture' and n.same(payload['contract'],r['contract']), 'V9 payload header/contract')
    payload_files={k.removeprefix('records/native-payloads/'):v for k,v in files.items() if k.startswith('records/native-payloads/')}
    cache=_payload(payload_files,payload,r,n)
    need(n.same(receipt['payload_packets'],len(r['intents'])) and receipt['payload_status']=='verified_ledger_linkage' and receipt['payload_audit_only_completion']=='unavailable_from_payload_capture','receipt payload reconciliation')
    expected_campaign={f'campaign/claim-{j:06d}.json' for j in range(1,maximum_claim+1)}|{f'campaign/checkpoint-{j:06d}.json' for j in range(1,index+1)}|{f'campaign/ledger-{j:06d}/accounting.jsonl' for j in range(1,index+1)}|set(extra_names)|{'campaign/.lock'}
    need({k for k in files if k.startswith('campaign/')}==expected_campaign,'closed campaign namespace/no unexpected claim')
    need(take('campaign/.lock')==b'', 'campaign lock raw expected empty; no ownership inferred')
    return {**runtime,'schema':'full-window-external-data-audit.v9','evidence_kind':'controlled_local_stub_data_only',
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
        raise ValueError('invalid V9 window data: '+type(exc).__name__) from exc


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
         result['advanced'] is True and result['old6395_and44_unchanged'] is True and result['protected26_source_unchanged'] is True,
         'terminal controlled export required')
    need(result['actual_session_summary']=='not_created_or_claimed','no manufactured actual summary')
    out=replay_completed_window(files,hashes,1);reader.recheck();out['inventory_sha256']=sha(invraw)
    return out



SOURCE_HELPER = 'helpers/full1220_source_v9_test.go.txt'
SOURCE_HELPER_SHA256 = 'afee3c37dee28fd75cf6c09885a4054fb6c8729e5122567b7d770c1eda1c8966'
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
         dependency('native').same(json.loads(proof._raw)['inputs'],binding), 'genuine matching in-process V9 source proof required')
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


def verify_quality_v9(manifest_raw, receipt_raw, records, sidecars, expected_hashes, *,
                      source_root, source_hashes, output_dir, execute=False, context=None):
    """No-quality returns None before any helper/runtime/output/source access."""
    need(type(receipt_raw) is bytes and len(receipt_raw)<=16<<20,'bounded raw receipt')
    def pairs(items):
        value={}
        for key,item in items:
            need(key not in value,'duplicate receipt key');value[key]=item
        return value
    def invalid(value):raise ValueError('nonfinite receipt number')
    receipt=json.loads(receipt_raw.decode('utf-8'),object_pairs_hook=pairs,parse_constant=invalid)
    need(type(receipt) is dict and type(receipt.get('rows')) is list and all(type(r) is dict for r in receipt['rows']), 'receipt rows')
    if not any(row.get('quality_end') is not None for row in receipt['rows']):return None
    return verify_source_v9(manifest_raw,receipt_raw,records,sidecars,expected_hashes,
        source_root=source_root,source_hashes=source_hashes,output_dir=output_dir,execute=execute,context=context)





def verify_source_v9(manifest_raw, receipt_raw, records, sidecars, expected_hashes, *,
                     source_root, source_hashes, output_dir, execute=False, context=None):
    """Fresh source-case replay using the separately authenticated V9 artifact.

    Technical preparation may provide a genuine context without execution
    authority; this returns ONLY narrow source replay evidence, never ownership.
    """
    _selected()
    need(execute is True,'explicit offline source execution required')
    need(type(context) is _ActualContext and context.seal is _ACTUAL_SEAL and context.helper is not None,'genuine V9 helper context required')
    n=dependency('native');source=dependency('source');h=context.helper;p=h['runner']
    need(sha(manifest_raw)==FROZEN_MANIFEST_SHA256 and source_root==context.manifest['source_root'] and
         n.same(source_hashes,context.manifest['sources']), 'selected source manifest/map/root')
    binding=source_binding(manifest_raw,receipt_raw,records,sidecars)
    need(n.same(expected_hashes,binding),'source original input pins')
    manifest=source._Document(manifest_raw).value;receipt=source._Document(receipt_raw).value
    need(manifest['schema']=='full-window-actual-ready.v9' and receipt['schema']=='full-window-acquisition-receipt.v8' and
         manifest['scope_sha256']==receipt['scope_sha256'] and manifest['execution_sha256']==receipt['execution_sha256'],'source V9 binding')
    _scope(manifest['scope'],n.wire(manifest['scope']),n)
    rows=receipt['rows'];need(0<len(rows)<=24 and set(records)=={r['recording_file'] for r in rows},'source current rows/record set')
    expected_sidecars={}
    for row in rows:
        name=row['recording_file'];need('/' not in name and name not in ('','.','..') and sha(records[name])==row['recording_sha256'],'source record identity')
        q=row.get('quality_end')
        if q is None:source.validate_completed_record_state(records[name])
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
    job=n.wire({'scope':manifest['scope'],'rows':rows,'binding':proof_binding});_exclusive(output/'input.json',job)
    env={'PATH':'/usr/bin:/bin:/usr/sbin:/sbin','TMPDIR':str(output/'tmp'),
         'FULL1220_V9_SOURCE_INPUT':str(output/'input.json'),'FULL1220_V9_SOURCE_INPUT_SHA256':sha(job),
         'FULL1220_V9_SOURCE_OUTPUT':str(output/'source-proof.json')}
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
        need(n.same(source._Document(result).value,{'schema':'full1220-source-v9-replay.v1','input_sha256':sha(job),'binding':proof_binding}),'source helper exact output')
        stdout=Evidence(outdir).read('stdout',bound=8<<20)
        markers=re.findall(rb'(?m)^\s+full1220_source_v9_test\.go:[0-9]+: FULL1220_V9_SOURCE_OUTPUT_SHA256=([0-9a-f]{64})\r?$',stdout)
        need(markers==[sha(result).encode()],'source real stdout receipt')
    process=p.run_owned(state_root=context.root/'helper-supervision',attempt_name='source-'+sha(str(output).encode())[:32],
        argv=list(selected.argv),cwd=Path(selected.cwd),env=env,executable_pin=selected.binary,inputs=pins,trees=[h['runtime']],
        timeout=150,phase='source',validate_output=validate)
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


class _ActualContext:
    def __init__(self, seal, root, binding, manifest, reader, authority_raw):
        need(seal is _ACTUAL_SEAL,'actual freeze issuer only')
        self.seal=seal;self.root=root;self.binding=binding;self.manifest=manifest
        self.reader=reader;self.authority_raw=authority_raw
        self.helper=None


def _selected():
    for value,size in [(FROZEN_MANIFEST_SHA256,64),(FROZEN_SOURCE_SHA256,64),(FROZEN_BINARY_SHA256,64),(FROZEN_COMMIT,40),(FROZEN_TREE,40)]:
        need(type(value) is str and re.fullmatch('[0-9a-f]{'+str(size)+'}',value),'UNFROZEN exact V9 selection')
    need(type(FROZEN_FILE_COUNT) is int and type(FROZEN_GO_COUNT) is int and 0<FROZEN_GO_COUNT<=FROZEN_FILE_COUNT,'UNFROZEN source counts')
    need(type(FROZEN_ROOT) is str and FROZEN_ROOT.startswith('/') and 'v9' in Path(FROZEN_ROOT).name,'UNFROZEN independent V9 root')
    need(type(FROZEN_HELPER_CATALOG_SHA256) is str and re.fullmatch('[0-9a-f]{64}',FROZEN_HELPER_CATALOG_SHA256),'UNFROZEN V9 helper catalog')


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
    need(set(seal)=={'schema','source_map_sha256','modes'} and seal['schema']=='full1220-v9-readonly-source-seal.v1' and
         seal['source_map_sha256']==sha(n.wire(dict(sorted(sources.items())))), 'V9 exact source seal')
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
    need(root==Path(FROZEN_ROOT) and root.resolve()==root and filename=='runtime-binding.json','fixed new V9 root/binding')
    n=dependency('native');codec=dependency('codec');reader=Evidence(root)
    binding=_json(reader.read(filename,expected))
    need(set(binding)=={'schema','artifact_sha256','scope_sha256','execution_sha256','verifier_sha256','owner_sha256','dependency_sha256'},'exact V9 binding fields')
    need(binding['schema']=='full1220-v9-private-runtime-binding.v1','V9 binding schema')
    artifacts=binding['artifact_sha256'];required={'actual-manifest.json','full-window-actual-v9','actual-manifest.json.binary-attestation.json','source-readonly-seal.json','technical-start-audit.json','actual-preparation-result.json',
        'helper-build/catalog.json','helper-build/source.test','helper-build/capture.test'}
    need(set(artifacts)==required,'exact technical artifact inventory')
    need(artifacts['actual-manifest.json']==FROZEN_MANIFEST_SHA256 and artifacts['full-window-actual-v9']==FROZEN_BINARY_SHA256,'compiled manifest/binary selection')
    for name,pin in artifacts.items():reader.read(name,pin,256<<20 if name in ('full-window-actual-v9','helper-build/source.test','helper-build/capture.test') else 32<<20)
    manifest_raw=reader.read('actual-manifest.json',FROZEN_MANIFEST_SHA256);m=_json(manifest_raw)
    need(m['schema']=='full-window-actual-ready.v9' and m['evidence_kind']=='actual' and m['archive_commit']==FROZEN_COMMIT and m['archive_tree']==FROZEN_TREE,'actual manifest identity')
    need(sha(n.wire(dict(sorted(m['sources'].items()))))==FROZEN_SOURCE_SHA256 and len(m['sources'])==FROZEN_FILE_COUNT and sum(x.endswith('.go') for x in m['sources'])==FROZEN_GO_COUNT,'source selection/map/counts')
    need(m['source_root']==str(root/'regression/source') and m['campaign_root']==str(root/'campaign') and m['session_root']==str(root/'records'),'new fixed root paths')
    need(m['knowledge_model']=='off' and m['knowledge_jev']=='off' and len(m['historical_claims'])==46 and m['baseline_evidence']['sha256']==BASELINE,'unchanged customer knowledge/history declaration')
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
    authority_raw=None
    if require_authority:
        authority_raw=reader.read('execution-authorization.json',bound=65536);auth=codec.parse_execution_authorization(authority_raw)
        for key,value in [('manifest_sha256',FROZEN_MANIFEST_SHA256),('source_sha256',FROZEN_SOURCE_SHA256),('binary_sha256',FROZEN_BINARY_SHA256),('scope_sha256',m['scope_sha256']),('execution_sha256',m['execution_sha256'])]:need(auth[key]==value,'exact signed authority '+key)
        _signature(m['build_attestation_public_key'],codec.authorization_signing_bytes(authority_raw),auth['signature'])
        reader.read(_relative(root,auth['human_reply']['path']),auth['human_reply']['sha256'],1<<20)
        reader.read(_relative(root,auth['autonomous_authorization']['path']),auth['autonomous_authorization']['sha256'],65536)
    source_reader.recheck();reader.recheck()
    context=_ActualContext(_ACTUAL_SEAL,root,binding,m,reader,authority_raw)
    context.helper=_load_helper_artifacts(context)
    return context


def _validate_technical_audit(audit,manifest,binding,artifacts,n):
    """Pure technical preparation metadata; never execution authorization."""
    need('human_approval_required' not in audit,'obsolete exact-human approval metadata forbidden')
    expected={'schema':'full1220-v9-technical-start-audit.v1','technical_checks_passed':True,'confirmed_P1_P2':0,
        'actual_execution_authorized':False,'human_reviewed_exact_scope':False,'delegated_authorization_required':True,
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
    extras={'internal/taskbench/full1220_source_v9_test.go':SOURCE_HELPER_SHA256,
            'internal/observability/full1220_content_projection_v8_test.go':'2aa266ad067173e8b6701c7bce793411e29e5fc1e52872cae1df5c8ea3cfac2d'}
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
    specs={'source':('TestFull1220SourceV9','full1220-source-v9-job.v1','full1220-source-v9-replay.v1','internal/taskbench/full1220_source_v9_test.go'),
           'capture':('TestFull1220FrozenContentProjection','full1220-content-projection-job.v1','full1220-content-projection.v1','internal/observability/full1220_content_projection_v8_test.go')}
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
    n=dependency('native');need(type(summary_raws) is list and len(summary_raws)==len(receipt_raws) and 0<len(summary_raws)<=511,'summary/raw receipt prefix')
    need(sha(summary_raws[-1])==expected_latest_sha256,'latest summary raw pin')
    previous='';arms=observed=quality=model=jev=0;merged=None
    additive='logical_model_calls logical_jev_calls native_model_intents native_jev_intents model_retry_intents jev_retry_intents native_settlements native_unknown native_pending'.split()
    fields='v8 evidence_kind acquisition_done closed next_window_index last_receipt_sha256 planned_pairs planned_arms defined_turns boundaries_excluded attempted_arms finished_arms failed_attempted_arms unattempted_arms observed_turns quality_end_unrun_turns global_unrun_turns model_attempts jev_attempts window_index report_sha256 payload_manifest_sha256 ledger_sha256 first_unrun_ordinal first_unrun_arm future_unrun_from_ordinal report_status'.split()
    for index,(sraw,rraw) in enumerate(zip(summary_raws,receipt_raws),1):
        s=_json(sraw);r=_json(rraw);need(set(s)==set(fields),'exact actual summary fields')
        done=index==511;nextordinal=scope['windows'][index-1]['last_ordinal']+1
        need(r['schema']=='full-window-acquisition-receipt.v8' and r['evidence_kind']=='recorded_run' and r['status']=='collected_window' and
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
        expected={'v8':runtime,'evidence_kind':'recorded_run','acquisition_done':done,'closed':done,'next_window_index':0 if done else index+1,
            'last_receipt_sha256':previous,'planned_pairs':3660,'planned_arms':7320,'defined_turns':11772,'boundaries_excluded':1851,
            'attempted_arms':arms,'finished_arms':arms,'failed_attempted_arms':0,'unattempted_arms':7320-arms,
            'observed_turns':observed,'quality_end_unrun_turns':quality,'global_unrun_turns':11772-observed-quality,
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
    names={p.name for p in records.iterdir()}
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


def _verify_actual_window(root=None, *, binding_sha256=None, window_index=None, summary_sha256=None, helper_output_dir=None):
    _selected()  # Fail before arbitrary path or caller JSON access when unrendered.
    context=load_freeze_binding(root,'runtime-binding.json',binding_sha256)
    need(context.authority_raw is not None,'actual signed authority required')
    need(type(window_index) is int and 1<=window_index<=511,'explicit current window')
    root=context.root;reader=context.reader;manifest=context.manifest;n=dependency('native')
    summary_raws=[];receipts=[]
    for index in range(1,window_index+1):
        summary_raws.append(reader.read(f'records/window-{index:06d}-summary.json'))
        summary=_json(summary_raws[-1]);receipts.append(reader.read(f'records/window-{index:06d}/receipt.json',summary['report_sha256']))
    cumulative=verify_summary_chain(summary_raws,receipts,manifest['scope'],summary_sha256)
    expected_names={'startup.json'}|{f'window-{i:06d}' for i in range(1,window_index+1)}|{f'window-{i:06d}-summary.json' for i in range(1,window_index+1)}
    if window_index==511:expected_names.add('terminal.json')
    expected_names=frozenset(expected_names)
    records_marker=_records_top_marker(root,expected_names)
    startup=_json(reader.read('records/startup.json'))
    need(startup['evidence_kind']=='recorded_run' and startup['closed'] is False and startup['acquisition_done'] is False and
         startup['next_window_index']==1 and startup['last_receipt_sha256']=='' and startup['attempted_arms']==0,'original fresh session startup')
    if window_index==511:
        terminal=_json(reader.read('records/terminal.json'));summary=_json(summary_raws[-1])
        status_keys=set(startup)|{'v8'}
        expected_terminal={k:summary[k] for k in status_keys}
        expected_terminal['last_receipt_sha256']=sha(summary_raws[-1])
        need(n.same(terminal,expected_terminal),'final terminal binds last SUMMARY, not raw receipt')

    # Logical view retains every original raw byte; physical paths are authenticated
    # above/below. It only removes the current window directory prefix for replay.
    files={'actual-manifest.json':reader.read('actual-manifest.json',FROZEN_MANIFEST_SHA256),
           'execution-authorization.json':context.authority_raw,'scope.json':n.wire(manifest['scope'])}
    auth=dependency('codec').parse_execution_authorization(context.authority_raw)
    reply=reader.read(_relative(root,auth['human_reply']['path']),auth['human_reply']['sha256'],1<<20)
    files['human-reply.txt']=reply
    current=root/f'records/window-{window_index:06d}';current_reader=Evidence(current)
    # Inventory is discovered, bounded and read safely; replay determines the
    # exact permitted closed namespace and rejects orphan raw data.
    for base,dirs,names in os.walk(current,followlinks=False):
        for d in dirs:need(not (Path(base)/d).is_symlink(),'record directory link')
        for name in names:
            rel=(Path(base)/name).relative_to(current).as_posix();files['records/'+rel]=current_reader.read(rel)
    campaign_reader=Evidence(root/'campaign')
    for base,dirs,names in os.walk(root/'campaign',followlinks=False):
        for d in dirs:need(not (Path(base)/d).is_symlink(),'campaign directory link')
        for name in names:
            rel=(Path(base)/name).relative_to(root/'campaign').as_posix();files['campaign/'+rel]=campaign_reader.read(rel)
    receipt=_json(files['records/receipt.json']);records={row['recording_file']:files['records/'+row['recording_file']] for row in receipt['rows']}
    sidecars={q[k]:files['records/'+q[k]] for row in receipt['rows'] if (q:=row.get('quality_end')) is not None for k in ('candidate_file','cleanup_file')}
    binding=source_binding(files['actual-manifest.json'],files['records/receipt.json'],records,sidecars)
    proof=verify_source_v9(files['actual-manifest.json'],files['records/receipt.json'],records,sidecars,binding,
        source_root=manifest['source_root'],source_hashes=manifest['sources'],output_dir=helper_output_dir,execute=True,context=context)
    _records_top_marker(root,expected_names,records_marker)
    output=_replay_completed_window(files,{k:sha(v) for k,v in files.items()},window_index,proof,context)
    need(n.same(cumulative['total_permanent'],output['total_permanent']),'cumulative runtime permanent')
    for i,raw in enumerate(receipts,1):
        cp=_json(files[f'campaign/checkpoint-{i:06d}.json']);r=_json(raw)
        need(cp['ledger_sha256']==r['ledger_sha256'],'prior raw receipt ledger chain')
    current_reader.inventory({k.removeprefix('records/') for k in files if k.startswith('records/')})
    campaign_reader.inventory({k.removeprefix('campaign/') for k in files if k.startswith('campaign/')})
    current_reader.recheck();campaign_reader.recheck();reader.recheck()
    _records_top_marker(root,expected_names,records_marker)
    output.update(schema='full1220-v9-completed-window-readonly-verification.v1',evidence_kind='actual_recorded_data_audit',
        summary_sha256=sha(summary_raws[-1]),previous_summary_sha256=sha(summary_raws[-2]) if len(summary_raws)>1 else '',
        script_sha256=context.binding['verifier_sha256'],binding_sha256=binding_sha256,cumulative=cumulative,
        actual_session_summary='verified_original_bytes',source_case_external_validation='genuine_in_process_replay',
        signature_trust_proved=True,financial_claim_authenticity_proved=False,owned_completion_proved=False,
        current_native_payloads_fully_replayed=True,prior_native_payloads_replayed=False,
        prior_ledger_claim_checkpoint_raw_replayed=True,independent_semantic_review='pending',scores='pending')
    return _VerifiedWindow(_WINDOW_SEAL,{**binding,'summary_sha256':sha(summary_raws[-1]),'binding_sha256':binding_sha256},output,proof)


_WINDOW_SEAL = object()
class _VerifiedWindow:
    __slots__=('_seal','_inputs','_report','_source')
    def __init__(self,seal,inputs,report,source):
        need(seal is _WINDOW_SEAL,'verified window issuer only')
        object.__setattr__(self,'_seal',seal)
        object.__setattr__(self,'_inputs',json.dumps(inputs,sort_keys=True,ensure_ascii=False,separators=(',',':')).encode())
        object.__setattr__(self,'_report',json.dumps(report,sort_keys=True,ensure_ascii=False,separators=(',',':')).encode())
        object.__setattr__(self,'_source',source)
    def __setattr__(self,name,value):raise AttributeError('immutable verified window')


def check_verified_window(proof, expected_inputs):
    need(type(proof) is _VerifiedWindow and proof._seal is _WINDOW_SEAL,'genuine current-process verified window required')
    inputs=json.loads(proof._inputs)
    need(dependency('native').same(inputs,expected_inputs),'verified window raw input binding')
    _check_source_proof(proof._source,{k:v for k,v in inputs.items() if k not in ('summary_sha256','binding_sha256')})
    return json.loads(proof._report)


def verify_actual_window(root=None, **kwargs):
    """Fresh opaque data-verification gate for V9 scoring, never owner authority."""
    return _verify_actual_window(root,**kwargs)


def verify_actual(root=None, **kwargs):
    """Owner's diagnostic facade; JSON cannot reconstruct the opaque score gate."""
    proof=verify_actual_window(root,**kwargs)
    return check_verified_window(proof,json.loads(proof._inputs))


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

if __name__=='__main__':raise SystemExit(main())
