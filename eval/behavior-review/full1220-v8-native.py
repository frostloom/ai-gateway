"""Pure V8 native data reconciliation, never financial or execution authority.

replay_ledger accepts an explicitly supplied immutable contract. Bundle replay is
currently limited to synthetic single-window, initial-extra-chain fixtures.
Neither SHA self-consistency nor diagnostic TLS JSON proves durable files,
actual claims, global campaign quota, trusted signatures or live owned proofs.
"""
import copy
import hashlib
import json
import re

PROVIDERS = ('model', 'jev')
REQUEST = 'native_attempt provider model case_key pass arm turn attempt body_sha256 bytes'.split()
LIMIT = 'model max_attempts max_request_bytes max_input_tokens max_output_tokens input_micro_per_million output_micro_per_million cache_hit_input_micro_per_million'.split()
CONTRACT = 'schema scope_sha256 schedule_sha256 policy_sha256 claim_sha256 window_id permanent_model_micro_cny permanent_jev_micro_usd model jev'.split()
EVENT = 'descriptor_sha256 unknown_sequence provider amount extra_claim_sha256 kind index previous_sha256 contract sequence request response_sha256 usage code'.split()
USAGE = 'input_tokens output_tokens cache_hit_tokens cache_miss_tokens'.split()
EXTRA = 'schema index provider_index root scope_sha256 execution_sha256 source_sha256 policy_sha256 authorization_sha256 owner_id base_claim_sha256 window_id previous_extra_sha256 unknown_sequence request descriptor_sha256 amount model_extras jev_extras permanent'.split()
HALTS = {'invalid_request','concurrent_request','duplicate_request','budget_exhausted','invalid_ticket','upstream_failure','protocol_failure','invalid_failure','usage_missing','usage_invalid','usage_out_of_bounds','response_identity','pending_at_close'}


def need(ok, message):
    if not ok:
        raise ValueError(message)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def integer(value, lo=0, hi=(1 << 63)-1):
    need(type(value) is int and lo <= value <= hi, 'integer bounds/type')
    return value


def digest(value):
    need(type(value) is str and re.fullmatch('[0-9a-f]{64}', value) is not None, 'digest')
    return value


def text(value):
    need(type(value) is str, 'text required')
    try:
        return value.encode('utf-8', 'strict')
    except UnicodeError as exc:
        raise ValueError('invalid unicode') from exc


def fields(value, order, optional=()):
    need(type(value) is dict and set(order)-set(optional) <= set(value) <= set(order), 'exact fields')
    return {k: value[k] for k in order if k in value}


def pairs(items):
    out = {}
    for k, v in items:
        need(k not in out, 'duplicate JSON key')
        out[k] = v
    return out


def invalid_number(_):
    raise ValueError('nonfinite number')


def decimal_integer(token):
    need(token != '-0', 'negative zero is not unsigned decimal usage')
    return int(token)


def parse(raw, bound=16 << 20):
    need(type(raw) is bytes and 0 < len(raw) <= bound, 'bounded bytes')
    try:
        value = json.loads(raw.decode('utf-8'), object_pairs_hook=pairs, parse_constant=invalid_number, parse_int=decimal_integer)
        def visit(v, depth=0):
            need(depth <= 128, 'JSON depth')
            if type(v) is str: text(v)
            elif type(v) is dict:
                for k, x in v.items(): text(k); visit(x, depth+1)
            elif type(v) is list:
                for x in v: visit(x, depth+1)
        visit(value)
    except (UnicodeError, RecursionError) as exc:
        raise ValueError('invalid JSON encoding/depth') from exc
    need(type(value) is dict, 'JSON object')
    return value


def wire(obj):
    s = json.dumps(obj, ensure_ascii=False, separators=(',', ':'), allow_nan=False)
    for c, e in (('&','\\u0026'),('<','\\u003c'),('>','\\u003e'),('\u2028','\\u2028'),('\u2029','\\u2029')):
        s = s.replace(c, e)
    return s.encode('utf-8')


def same(a, b):
    # Python bool equals int; canonical JSON comparison preserves type distinctions.
    return json.dumps(a, sort_keys=True, ensure_ascii=False) == json.dumps(b, sort_keys=True, ensure_ascii=False)


def limits(p, model):
    p = fields(p, LIMIT, ('cache_hit_input_micro_per_million',))
    need(type(p['model']) is str and re.fullmatch(r'[a-zA-Z0-9._/-]{1,128}', p['model']), 'provider model')
    for k, lo, hi in [('max_attempts',1,10000),('max_request_bytes',1,1<<20),('max_input_tokens',1,10000000),('max_output_tokens',1 if model else 0,512 if model else 1000000),('input_micro_per_million',1,1000000000),('output_micro_per_million',1 if model else 0,1000000000)]:
        integer(p[k], lo, hi)
    cache = p.get('cache_hit_input_micro_per_million', 0)
    integer(cache, 1 if model else 0, p['input_micro_per_million'] if model else 0)
    need(model or 'cache_hit_input_micro_per_million' not in p, 'canonical omitted zero JEV cache rate')
    return p


def contract_data(c):
    c = fields(c, CONTRACT)
    need(c['schema'] == 'full-window-accounting.v3', 'v3 contract required')
    for key in CONTRACT[1:5]: digest(c[key])
    need(type(c['window_id']) is str and re.fullmatch(r'window-[0-9]{3,6}',c['window_id']), 'window id')
    for p in PROVIDERS:
        c[p] = limits(c[p], p == 'model')
        cap = integer(c[cap_key(p)], 1, 1000000000)
        need(max_hold(c[p]) <= cap, 'base cannot admit maximum hold')
    return c


def cap_key(p):
    return 'permanent_model_micro_cny' if p == 'model' else 'permanent_jev_micro_usd'


def max_hold(p):
    return (p['max_input_tokens']*p['input_micro_per_million']+999999)//1000000+(p['max_output_tokens']*p['output_micro_per_million']+999999)//1000000


def settlement_cost(p, usage):
    u = fields(usage, USAGE, ('cache_hit_tokens','cache_miss_tokens'))
    inp = integer(u['input_tokens'], 0, p['max_input_tokens'])
    out = integer(u['output_tokens'], 0, p['max_output_tokens'])
    need(('cache_hit_tokens' in u) == ('cache_miss_tokens' in u), 'complete cache pair required')
    numerator = inp*p['input_micro_per_million']
    if 'cache_hit_tokens' in u:
        hit = integer(u['cache_hit_tokens'], 0, p['max_input_tokens'])
        miss = integer(u['cache_miss_tokens'], 0, p['max_input_tokens'])
        need(hit+miss == inp, 'cache pair sum')
        if p.get('cache_hit_input_micro_per_million',0):
            numerator = hit*p['cache_hit_input_micro_per_million']+miss*p['input_micro_per_million']
    return (numerator+999999)//1000000+(out*p['output_micro_per_million']+999999)//1000000


def request_data(r, c):
    r = fields(r, REQUEST)
    integer(r['native_attempt'],1,2)
    need(r['provider'] in PROVIDERS, 'request provider')
    p = c[r['provider']]
    need(r['model'] == p['model'], 'request model')
    need(type(r['case_key']) is str and re.fullmatch('[a-z0-9_./-]{1,256}', r['case_key']), 'case key')
    integer(r['pass'],1,3); integer(r['turn'],1,32); integer(r['attempt'],1,p['max_attempts'])
    need(r['arm'] in ('model','jev_selective') and (r['provider'] != 'jev' or r['arm'] == 'jev_selective'), 'request arm')
    digest(r['body_sha256']); integer(r['bytes'],1,p['max_request_bytes'])
    return r


def replay_ledger(raw, expected_contract):
    """Replay supplied canonical journal bytes; readable close is not durability proof."""
    c = contract_data(copy.deepcopy(expected_contract))
    need(type(raw) is bytes and 0 < len(raw) <= 32 << 20 and raw.endswith(b'\n'), 'truncated/bounded ledger')
    zeros = lambda: dict.fromkeys(PROVIDERS,0)
    r = dict(contract=c,native_counts=zeros(),logical_counts=zeros(),retry_counts=zeros(),known=zeros(),unknown_held=zeros(),pending_held=zeros(),extra_funded=zeros(),pending=None,retry_required=None,unknown_intents=[],intents={},settlements={},funding={},halt='',closed=False,envelope_violation=False,completion_proof='unavailable_from_journal')
    seen=set(); claim_seen=set(); previous=''
    # Match bufio.ScanLines: LF delimits tokens; drop only one trailing CR.
    # Internal bare CR is not an event separator and fails canonical parsing.
    for index,line in enumerate(raw[:-1].split(b'\n')):
        if line.endswith(b'\r'):
            line = line[:-1]
        e=parse(line,1<<20); e=fields(e,EVENT, set(EVENT)-{'kind','index'})
        integer(e['index']);need(e['index']==index and e.get('previous_sha256','')==previous, 'event chain/index')
        if 'contract' in e:e['contract']=contract_data(e['contract'])
        if 'request' in e:e['request']=request_data(e['request'],c)
        if 'usage' in e:e['usage']=fields(e['usage'],USAGE,('cache_hit_tokens','cache_miss_tokens'))
        need(wire(e)==line,'noncanonical ledger event')
        need(not r['closed'],'event after close')
        kind=e['kind']; common={'kind','index','previous_sha256'}
        if index==0:
            need(set(e)=={'kind','index','contract'} and kind=='config' and same(e['contract'],c),'config contract')
        elif kind=='intent':
            need(set(e)==common|{'sequence','request'} and not r['halt'] and r['pending'] is None,'intent state/fields')
            seq=integer(e['sequence'],1);need(seq==len(r['intents'])+1,'native sequence')
            req=e['request']; p=req['provider']; hold=max_hold(c[p])
            slot=tuple(req[k] for k in REQUEST if k not in ('body_sha256','bytes'))
            need(slot not in seen,'duplicate native identity')
            u=r['retry_required']
            if u is not None:
                need(req['native_attempt']==2 and u.get('extra_claim_sha256') and all(req[k]==u['request'][k] for k in REQUEST if k!='native_attempt'),'same funded logical/body required')
            else:need(req['native_attempt']==1,'unsolicited native2')
            need(r['native_counts'][p]<c[p]['max_attempts'],'native cap')
            need(r['known'][p]+r['unknown_held'][p]+r['pending_held'][p]+hold<=c[cap_key(p)]+r['extra_funded'][p],'maximum admission')
            seen.add(slot);r['intents'][seq]=req;r['native_counts'][p]+=1
            r['logical_counts' if req['native_attempt']==1 else 'retry_counts'][p]+=1
            r['pending']={'sequence':seq,'request':req};r['pending_held'][p]=hold
        elif kind=='unknown':
            need(set(e)==common|{'sequence','descriptor_sha256'} and not r['halt'] and r['pending'] is not None and r['retry_required'] is None,'unknown state/fields')
            pending=r['pending'];need(integer(e['sequence'],1)==pending['sequence'] and pending['request']['native_attempt']==1,'first native unknown only')
            p=pending['request']['provider'];hold=r['pending_held'][p]
            u={**pending,'descriptor_sha256':digest(e['descriptor_sha256']),'max_hold':hold}
            r['unknown_intents'].append(u);r['unknown_held'][p]=integer(r['unknown_held'][p]+hold)
            r['pending_held'][p]=0;r['pending']=None;r['retry_required']=u
        elif kind=='extra_funding':
            need(set(e)==common|{'unknown_sequence','provider','amount','extra_claim_sha256'} and not r['halt'] and r['pending'] is None,'funding state/fields')
            u=r['retry_required'];p=e['provider'];claim=digest(e['extra_claim_sha256'])
            need(u is not None and integer(e['unknown_sequence'],1)==u['sequence'] and p==u['request']['provider'] and integer(e['amount'],1)==u['max_hold'] and not u.get('extra_claim_sha256') and claim not in claim_seen,'funding association/duplicate')
            need(sum(x['request']['provider']==p and 'extra_claim_sha256' in x for x in r['unknown_intents'])<20,'local funding cap')
            u['extra_claim_sha256']=claim;claim_seen.add(claim);r['funding'][claim]=copy.deepcopy(e)
            r['extra_funded'][p]=integer(r['extra_funded'][p]+e['amount'])
        elif kind=='settle':
            need(set(e)==common|{'sequence','response_sha256','usage'} and not r['halt'] and r['pending'] is not None,'settle state/fields')
            pending=r['pending'];seq=integer(e['sequence'],1);need(seq==pending['sequence'],'settlement ticket')
            p=pending['request']['provider'];digest(e['response_sha256']);cost=settlement_cost(c[p],e['usage'])
            need(cost<=r['pending_held'][p] and r['known'][p]+r['unknown_held'][p]+r['pending_held'][p]<=c[cap_key(p)]+r['extra_funded'][p],'settlement liability')
            r['known'][p]=integer(r['known'][p]+cost);r['pending_held'][p]=0;r['settlements'][seq]=e
            if pending['request']['native_attempt']==2:r['retry_required']=None
            r['pending']=None
        elif kind=='halt':
            need(set(e)==common|{'code'} and not r['halt'] and e['code'] in HALTS,'halt event')
            r['halt']=e['code'];r['envelope_violation']=e['code']=='usage_out_of_bounds'
        elif kind=='close':
            need(set(e)==common and (r['halt'] or (r['pending'] is None and r['retry_required'] is None)),'unresolved close')
            r['closed']=True
        else:raise ValueError('unsupported ledger event')
        previous=sha(line)
    r['native_resolved']=not r['halt'] and r['pending'] is None and r['retry_required'] is None
    r['journal_sha256']=sha(raw)
    return copy.deepcopy(r)


def normalize_usage(provider, raw, p):
    obj=parse(raw,1<<20)
    need(obj.get('model')==p['model'] and 'error' not in obj,'response model/error')
    u=obj.get('usage');allowed=['input_tokens','output_tokens']
    if provider=='model':allowed+=['cache_read_input_tokens','cache_creation_input_tokens','service_tier']
    u=fields(u,allowed,allowed[2:])
    inp=integer(u['input_tokens'],0,p['max_input_tokens']);out=integer(u['output_tokens'],0,p['max_output_tokens'])
    normalized={'input_tokens':inp,'output_tokens':out};availability=None
    if provider=='model':
        need(u.get('service_tier','standard')=='standard','unsupported service tier')
        read=integer(u.get('cache_read_input_tokens',0),0,p['max_input_tokens'])
        create=integer(u.get('cache_creation_input_tokens',0),0,p['max_input_tokens'])
        need(inp+read+create<=p['max_input_tokens'],'normalized input bound')
        normalized.update(input_tokens=inp+read+create,cache_hit_tokens=read,cache_miss_tokens=inp+create)
        availability='reported_cache_read' if 'cache_read_input_tokens' in u else 'unknown_raw_cache_read'
    return normalized,availability


def validate_request_body(raw,req,p):
    obj=parse(raw,p['max_request_bytes'])
    need(obj.get('model')==p['model'],'serialized request model')
    if req['provider']=='model':
        fields(obj,'model max_tokens messages system tools temperature thinking stream'.split(),('system','tools','temperature','stream'))
        need(integer(obj['max_tokens'],1)==p['max_output_tokens'] and obj.get('stream',False) is False and same(obj['thinking'],{'type':'disabled'}),'model request options')
        need(type(obj['messages']) is list and obj['messages'],'messages')
        for m in obj['messages']:
            fields(m,('role','content'));need(m['role'] in ('user','assistant') and type(m['content']) is list and m['content'],'message')
    else:
        fields(obj,('model','state','questions'));need(type(obj['state']) in (str,dict,list) and type(obj['questions']) is dict and obj['questions'],'JEV request')
        for k,q in obj['questions'].items():
            fields(q,('type','instructions','criteria'),('instructions','criteria'))
            need(k and q['type'] in ('noul','choice','score'),'JEV question')


def amounts(model,jev):
    return {'model_micro_cny':model,'jev_micro_usd':jev}


def monetary(value):
    out=fields(value,('model_micro_cny','jev_micro_usd'))
    for v in out.values():integer(v)
    return out


def _replay_native_bundle(raw,expected_sha256,require_closed):
    need(type(require_closed) is bool,'explicit boolean close requirement')
    need(type(raw) is bytes and sha(raw)==digest(expected_sha256),'raw bundle SHA')
    b=parse(raw)
    fields(b,'authorization authorization_utf8 evidence_kind extra_claim_files_utf8 extra_claims ledger_utf8 payload_files_utf8 payload_manifest public_key_hex runtime_summary'.split())
    need(b['evidence_kind']=='synthetic_controlled_fixture_not_actual_proof','initial replay only accepts synthetic fixtures')
    m=fields(b['payload_manifest'],'schema evidence_kind completion_proof contract packets'.split())
    need(m['schema']=='full-window-payload.v2' and m['evidence_kind']=='local_stub' and m['completion_proof']=='unavailable_from_payload_capture','payload evidence kind/schema')
    c=contract_data(m['contract']);r=replay_ledger(text(b['ledger_utf8']),c)
    need(r['native_resolved'] and (not require_closed or r['closed']),'unresolved or unclosed native data')
    files=b['payload_files_utf8'];need(type(files) is dict,'raw payload files')
    read=set()
    def take(name):
        need(type(name) is str and re.fullmatch(r'(store\.json|(?:request|response)-[0-9]{6}\.(?:json|body))',name) and name in files,'payload filename/inventory')
        read.add(name);data=text(files[name]);need(len(data)<=1<<20,'payload file bound');return data
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
    # Raw authorization is only hash-linked data here. Trusted signature/key and
    # human-intent checks belong to the final authenticated manifest wrapper.
    authraw=text(b['authorization_utf8']);auth=parse(authraw,65536)
    need(same(auth,b['authorization']) and auth.get('schema')=='full-window-execution-authorization.v8','authorization raw projection')
    for k in ('scope_sha256','source_sha256','execution_sha256'):digest(auth[k])
    need(auth['scope_sha256']==c['scope_sha256'],'authorization scope')
    extras=b['extra_claims'];extra_files=b['extra_claim_files_utf8']
    need(type(extras) is list and type(extra_files) is dict and len(extras)==len(r['unknown_intents']),'extra inventory')
    history=amounts(51200203,399529);base=amounts(c[cap_key('model')],c[cap_key('jev')]);total={k:history[k]+base[k] for k in history}
    counts=dict.fromkeys(PROVIDERS,0);prior='';used=set();matched=set()
    for index,item in enumerate(extras,1):
        e=fields(item,EXTRA,('previous_extra_sha256',));e['request']=request_data(e['request'],c)
        for key in ('amount','permanent'):e[key]=monetary(e[key])
        name=f'extra-{index:06d}.json';need(name in extra_files,'extra raw file missing');eraw=text(extra_files[name]);used.add(name)
        need(eraw==wire(e)+b'\n','extra canonical actual file bytes/newline')
        esha=sha(eraw);need(esha in r['funding'] and esha not in matched,'funded raw extra SHA/duplicate');matched.add(esha)
        need(e['schema']=='full-window-extra-claim.v8' and integer(e['index'],1)==index and e.get('previous_extra_sha256','')==prior,'initial extra chain')
        need(type(e['root']) is str and e['root'].startswith('/') and type(e['owner_id']) is str and e['owner_id']==auth['owner_id'],'extra owner/root text only')
        for key in ('scope_sha256','execution_sha256','source_sha256'):need(e[key]==auth[key],'extra authorization binding')
        need(e['authorization_sha256']==sha(authraw) and e['base_claim_sha256']==c['claim_sha256'] and e['window_id']==c['window_id'] and e['policy_sha256']==c['policy_sha256'],'extra window/auth data binding')
        seq=integer(e['unknown_sequence'],1);u=unknown.get(seq)
        need(u is not None and same(u['request'],e['request']) and e['descriptor_sha256']==u['descriptor_sha256'] and u['extra_claim_sha256']==esha,'extra unknown association')
        p=u['request']['provider'];hold=74752 if p=='model' else 2688
        need(u['max_hold']==hold and same(e['amount'],amounts(hold,0) if p=='model' else amounts(0,hold)),'exact V8 extra liability')
        counts[p]+=1;need(counts[p]<=20 and integer(e['provider_index'],1)==counts[p] and integer(e['model_extras'])==counts['model'] and integer(e['jev_extras'])==counts['jev'],'local provider/global indices')
        for key in total:total[key]=integer(total[key]+e['amount'][key])
        need(same(e['permanent'],total) and total['model_micro_cny']+2023424<=140000000 and total['jev_micro_usd']+65<=1100000,'cumulative permanent/risk declaration')
        prior=esha
    need(used==set(extra_files) and matched==set(r['funding']),'extra raw inventory/funding coverage')
    expected={'schema':'full-window-runtime-evidence.v8',
        'logical_model_calls':r['logical_counts']['model'],'logical_jev_calls':r['logical_counts']['jev'],
        'native_model_intents':r['native_counts']['model'],'native_jev_intents':r['native_counts']['jev'],
        'model_retry_intents':r['retry_counts']['model'],'jev_retry_intents':r['retry_counts']['jev'],
        'native_settlements':len(r['settlements']),'native_unknown':len(unknown),'native_pending':int(r['pending'] is not None),
        'retry_required':r['retry_required'] is not None,'halt':r['halt'],
        'configured_known':amounts(r['known']['model'],r['known']['jev']),
        'unknown_held':amounts(r['unknown_held']['model'],r['unknown_held']['jev']),
        'pending_held':amounts(r['pending_held']['model'],r['pending_held']['jev']),
        'historical_permanent':history,'base_permanent':base,'extra_permanent':amounts(r['extra_funded']['model'],r['extra_funded']['jev']),
        'total_permanent':total,'unknown_intents':[{'window_id':c['window_id'],**u} for u in r['unknown_intents']], 'extras':extras}
    need(same(expected,b['runtime_summary']),'runtime summary reconciliation')
    return {'evidence_kind':b['evidence_kind'],'native_resolved':r['native_resolved'],'closed':r['closed'],
        'native_packets':len(packets),'ledger':r,'runtime_summary':expected,'raw_cache_availability':cache,
        'completion_proof':'unavailable_from_journal','execution_authority':False,'owned_completion_proved':False,
        'financial_claim_authenticity_proved':False,'signature_trust_proved':False,'global_campaign_quota_proved':False}


def replay_native_bundle(raw,expected_sha256,*,require_closed=False):
    """Reconcile pinned synthetic raw data; no file reads or permission issuance.

    Scope is one initial window and its full local extra chain, not a campaign
    cumulative report. Stopped journal data remains inspectable by replay_ledger;
    this resolved-native bundle join rejects unresolved data instead of promoting it.
    """
    try:
        return _replay_native_bundle(raw,expected_sha256,require_closed)
    except (KeyError,TypeError,IndexError) as exc:
        raise ValueError('invalid supplied native data structure') from exc
