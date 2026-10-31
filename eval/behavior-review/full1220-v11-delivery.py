"""V11 unpaid original-spool shipment and independent GET-only remote proofs.

Inactive until the separately selected issuer/key and signed delivery catalog
are frozen. No serialized report, HTTP ACK or local spool mints score authority.
"""
import base64
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import stat
import sys
import urllib.parse

FROZEN_ROOT = DELIVERY_PUBLIC_KEY = 'UNFROZEN'
_CONTENT_HELPER = '98a26427ab04797c26c88356e88800d417362e215b5fbf9c39531b2de36c41d9'
_SEAL = object()

def need(ok, message):
    if not ok: raise ValueError(message)

def wire(value): return (json.dumps(value,sort_keys=True,ensure_ascii=False,separators=(',',':'),allow_nan=False)+'\n').encode()
def sha(raw): return hashlib.sha256(raw).hexdigest()
def document(raw):
    def pairs(items):
        result={}
        for k,v in items:
            need(k not in result,'duplicate JSON key');result[k]=v
        return result
    def invalid(value):raise ValueError('nonfinite JSON')
    return json.loads(raw,object_pairs_hook=pairs,parse_constant=invalid)

def _identity(st): return (st.st_dev,st.st_ino,st.st_mode,st.st_uid,st.st_gid,st.st_nlink,st.st_size,st.st_mtime_ns,st.st_ctime_ns)
def _canonical(path):
    p=Path(path);need(p.is_absolute() and p.resolve()==p,'canonical unaliased path required');return p

def _raw(path,limit=4<<20):
    path=_canonical(path);before=path.lstat()
    need(stat.S_ISREG(before.st_mode) and before.st_nlink==1 and before.st_uid==os.getuid() and before.st_size<=limit,'owned bounded single-link file required')
    fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
    try:
        need(_identity(os.fstat(fd))==_identity(before),'opened file changed');blocks=[];size=0
        while True:
            b=os.read(fd,min(1<<20,limit+1-size))
            if not b:break
            blocks.append(b);size+=len(b);need(size<=limit,'file exceeds bound')
        need(_identity(os.fstat(fd))==_identity(before)==_identity(path.lstat()),'file changed during read')
        raw=b''.join(blocks);need(len(raw)==before.st_size,'short file read');return raw
    finally:os.close(fd)

def _sync(path):
    fd=os.open(path,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW)
    try:os.fsync(fd)
    finally:os.close(fd)

def _exclusive(path,raw):
    path=Path(path);_canonical(path.parent)
    fd=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600)
    try:
        with os.fdopen(fd,'wb',closefd=False) as f:f.write(raw);f.flush();os.fsync(fd)
        marker=_identity(os.fstat(fd))
    finally:os.close(fd)
    _sync(path.parent)
    need(_identity(path.lstat())==marker and _raw(path)==raw,'exclusive write bytes/FD identity changed')
    return marker

class _Snapshot:
    """Fresh original bytes plus closed namespace/ancestor/FD identity checks."""
    def __init__(self,root):
        self.root=_canonical(root);self.files={}
        self.directories={p:(p.stat().st_dev,p.stat().st_ino,p.stat().st_mode) for p in (self.root,*self.root.parents)}
    def write(self,name,raw):
        path=self.root/name;need(path.parent==self.root and path not in self.files,'new readback leaf required')
        self.check()
        marker=_exclusive(path,raw)
        self.files[path]=(marker,sha(raw))
        self.check()
    def capture(self):
        need(not self.files,'readback baseline cannot be recaptured')
        for base,dirs,names in os.walk(self.root,followlinks=False):
            for name in dirs:
                p=_canonical(Path(base)/name);self.directories[p]=(p.stat().st_dev,p.stat().st_ino,p.stat().st_mode)
            for name in names:
                p=Path(base)/name;raw=_raw(p,2<<20);self.files[p]=(_identity(p.lstat()),sha(raw))
        self.check()
    def check(self):
        for p,marker in self.directories.items():
            _canonical(p);s=p.stat();need((s.st_dev,s.st_ino,s.st_mode)==marker,'readback directory changed')
        found=set()
        for base,dirs,names in os.walk(self.root,followlinks=False):
            for name in dirs:need(Path(base)/name in self.directories,'foreign readback directory')
            for name in names:found.add(Path(base)/name)
        need(found==set(self.files),'readback namespace changed')
        for p,(marker,digest) in self.files.items():need(_identity(p.lstat())==marker and sha(_raw(p,2<<20))==digest,'original readback changed')


def _selected(verifier,cohort,score_module):
    need(type(FROZEN_ROOT) is str and FROZEN_ROOT.startswith('/') and re.fullmatch('[0-9a-f]{64}',DELIVERY_PUBLIC_KEY or ''),'UNFROZEN delivery issuer')
    root=_canonical(FROZEN_ROOT)
    need(score_module is not None and sys.modules.get(getattr(score_module,'__name__',None)) is score_module,'selected registered score module required')
    need(verifier is not None and verifier is score_module._cohort_verifier_module,'score-selected verifier required')
    need(Path(verifier.__file__)==root/'verifier-preparation/verify-window.py' and verifier.FROZEN_ROOT==FROZEN_ROOT and score_module.FROZEN_ROOT==FROZEN_ROOT,'fixed selected V11 paths')
    need(sha(_raw(verifier.__file__))==score_module.FROZEN_VERIFIER_SHA256,'selected verifier bytes changed')
    report=verifier.check_verified_cohort(cohort)
    need(report['schema']=='full1220-v11-final-cohort-data.v1' and report['normal_owner_verified_separately'] is True and report['Pairs']==3660 and report['Arms']==7320 and report['DefinedUserTurns']==11772 and report['Boundaries']==1851,'full original normal cohort required')
    reader=verifier.Evidence(root)
    catalog=document(reader.read('delivery-preparation/catalog.json',bound=1<<20))
    need(type(catalog) is dict and set(catalog)=={'payload','signature'},'signed delivery catalog envelope')
    payload=catalog['payload'];need(type(payload) is dict and set(payload)==set('schema manifest_sha256 scope_sha256 execution_sha256 verifier_sha256 score_sha256 issuer_sha256 helper_sha256 helper_source_sha256 settings_sha256 journal_root'.split()) and payload['schema']=='full1220-v11-delivery-catalog.v1','delivery catalog fields')
    verifier._signature(DELIVERY_PUBLIC_KEY,wire(payload),catalog['signature'])
    for k in ('manifest_sha256','scope_sha256','execution_sha256'):need(payload[k]==report[k],'catalog genuine cohort binding')
    for k in payload:
        if k.endswith('_sha256'):need(type(payload[k]) is str and re.fullmatch('[0-9a-f]{64}',payload[k]),'catalog digest')
    need(payload['verifier_sha256']==score_module.FROZEN_VERIFIER_SHA256,'catalog verifier pin')
    need(Path(score_module.__file__)==root/'score-preparation/scores.py','fixed score module path')
    need(sha(reader.read('score-preparation/scores.py',bound=4<<20))==payload['score_sha256'],'selected score bytes changed')
    need(Path(__file__)==root/'delivery-preparation/delivery.py' and sha(reader.read('delivery-preparation/delivery.py',bound=4<<20))==payload['issuer_sha256'],'selected issuer bytes changed')
    need(payload['journal_root']==str(root/'delivery-journal'),'fixed shipment journal root')
    reader.recheck()
    return root,payload,reader,report


def _read_settings(root,catalog,manifest):
    path=root/'delivery-preparation/langfuse-read-settings.json';st=path.lstat()
    need(stat.S_IMODE(st.st_mode)==0o600,'private LF-only settings required')
    raw=_raw(path,16384);need(sha(raw)==catalog['settings_sha256'],'selected LF settings bytes')
    settings=document(raw)
    need(set(settings)==set('OTEL_EXPORTER_OTLP_TRACES_ENDPOINT AGENT_LANGFUSE_ENVIRONMENT LANGFUSE_PUBLIC_KEY LANGFUSE_SECRET_KEY'.split()),'LF-only credential boundary')
    need(settings['AGENT_LANGFUSE_ENVIRONMENT']=='evaluation','evaluation environment required')
    for key in ('LANGFUSE_PUBLIC_KEY','LANGFUSE_SECRET_KEY'):need(type(settings[key]) is str and settings[key] and not any(c in settings[key] for c in '\r\n:'),'LF project key shape')
    need(sha(settings['OTEL_EXPORTER_OTLP_TRACES_ENDPOINT'].encode())==manifest['observer']['endpoint_sha256'] and sha(settings['LANGFUSE_PUBLIC_KEY'].encode())==manifest['observer']['project_sha256'],'frozen LF endpoint/project')
    _endpoint(settings);return settings


def _endpoint(settings):
    u=urllib.parse.urlsplit(settings['OTEL_EXPORTER_OTLP_TRACES_ENDPOINT'])
    need(u.scheme in ('http','https') and u.hostname in ('localhost','127.0.0.1','::1') and not u.username and not u.password and not u.query and not u.fragment and u.path=='/api/public/otel/v1/traces','fixed local LF endpoint required')
    return u

class _ReadbackClient:
    """GET-only client; no redirects, proxies, SDK, retries or score interface."""
    def __init__(self,settings,root,snapshot=None):
        self.settings=settings;self.root=_canonical(root);self.rows={}
        self.snapshot=snapshot if snapshot is not None else _Snapshot(self.root)
        if snapshot is None:self.snapshot.capture()
        need(self.snapshot.root==self.root,'original readback snapshot root');self.snapshot.check()
    def observations(self,trace):
        need(type(trace) is str and re.fullmatch('[0-9a-f]{32}',trace) and trace!='0'*32,'original trace ID')
        need(trace not in self.rows,'one GET per trace in a readback attempt')
        self.rows[trace]=None
        intent=self.root/(trace+'.intent.json')
        try:self.snapshot.write(intent.name,wire({'schema':'full1220-v11-readback-intent.v1','trace_id':trace,'method':'GET'}))
        except FileExistsError:raise ValueError('prior GET intent retained; use a fresh GET-only attempt') from None
        endpoint=_endpoint(self.settings)
        cls=http.client.HTTPSConnection if endpoint.scheme=='https' else http.client.HTTPConnection
        conn=cls(endpoint.hostname,endpoint.port,timeout=10);status=0;outcome='unknown'
        try:
            auth=base64.b64encode((self.settings['LANGFUSE_PUBLIC_KEY']+':'+self.settings['LANGFUSE_SECRET_KEY']).encode()).decode()
            path='/api/public/v2/observations?'+urllib.parse.urlencode({'traceId':trace,'limit':1000,'fields':'core,basic,metadata,usage,io'})
            conn.request('GET',path,headers={'Authorization':'Basic '+auth,'Connection':'close'})
            response=conn.getresponse();status=response.status;raw=response.read((2<<20)+1)
            self.snapshot.write(trace+'.response',raw)
            need(status==200 and len(raw)<=2<<20,'GET status/size rejected')
            value=document(raw);need(type(value) is dict and type(value.get('data')) is list and len(value['data'])<=1000 and not value.get('meta',{}).get('cursor'),'bounded complete GET page required')
            self.rows[trace]=value['data'];outcome='received';return value['data']
        except (OSError,TimeoutError,http.client.HTTPException,json.JSONDecodeError,UnicodeError,ValueError):raise ValueError('remote GET unavailable or invalid; no POST permission') from None
        finally:
            conn.close();self.snapshot.write(trace+'.result.json',wire({'schema':'full1220-v11-readback-result.v1','trace_id':trace,'http_status':status,'outcome':outcome}))


def _exact_coverage(spans,rows,traces):
    expected={}
    for trace,sid,parent in spans:
        if trace in traces:
            need((trace,sid) not in expected,'duplicate accepted spool ID');expected[trace,sid]=parent
    need(set(rows)==set(traces) and expected,'exact original trace set')
    seen=set()
    for trace,observations in rows.items():
        need(type(observations) is list,'remote observation list required')
        for row in observations:
            key=(trace,row['id']);need(row['traceId']==trace and key not in seen and key in expected,'foreign/duplicate remote ID')
            need((row.get('parentObservationId') or '')==expected[key],'original accepted parent mismatch');seen.add(key)
    need(seen==set(expected),'missing accepted spool observation IDs')

class VerifiedRemoteWindow:
    __slots__=('_seal','_binding','_snapshot','_verifier','_cohort','_score','_window','_catalog')
    def __init__(self,seal,binding,snapshot,verifier,cohort,score,window,catalog=None):
        need(seal is _SEAL,'remote proof factory only')
        for k,v in zip(self.__slots__,(seal,wire(binding),snapshot,verifier,cohort,score,window,catalog)):object.__setattr__(self,k,v)
    def __setattr__(self,k,v):raise AttributeError('immutable remote window proof')
    def __reduce__(self):raise TypeError('remote proof is current-process only')


def verify_remote_window(*,verifier,cohort,window_index,binding,plan,projection,readback_root,score_module):
    root,catalog,catalog_reader,report=_selected(verifier,cohort,score_module)
    need(type(window_index) is int and 1<=window_index<=511,'original full window index')
    need(type(projection) is score_module._ContentProjection,'selected owning projection module')
    score_module._validate_projection_plan_binding(plan,projection)
    need(wire(binding)==wire(score_module.remote_binding(plan,projection)) and binding['content_helper_sha256']==_CONTENT_HELPER and plan['window_index']==window_index,'exact remote binding')
    proof=verifier.window_proof(cohort,window_index);inputs=document(proof._inputs)
    verified=verifier.check_verified_window(proof,inputs)
    need(verified['final_normal_cohort'] is True,'normal original window required')
    for k in ('manifest_sha256','receipt_sha256','summary_sha256'):need(binding[k]==inputs[k],'selected window raw binding')
    for k in ('scope_sha256','execution_sha256'):need(binding[k]==report[k],'selected execution binding')
    path=_canonical(readback_root)
    need(path.parent==root/'delivery-readbacks' and not path.exists(),'fresh fixed GET-only readback attempt')
    path.mkdir(mode=0o700);_sync(path.parent)
    snapshot=_Snapshot(path);snapshot.write('binding.json',wire(binding))
    # This LF-only loader is independent of the remote-gated score key loader.
    settings=_read_settings(root,catalog,cohort._context.manifest)
    client=_ReadbackClient(settings,path,snapshot)
    validation=score_module.validate_window_traces(plan,projection,client)
    snapshot.write('validation.json',wire(validation))
    need(validation.get('all_pass') is True,'actual original metadata/content readback incomplete')
    _exact_coverage(report['spans'],client.rows,binding['trace_ids'])
    verifier.check_verified_cohort(cohort);catalog_reader.recheck()
    _selected(verifier,cohort,score_module)
    snapshot.check()
    result=VerifiedRemoteWindow(_SEAL,binding,snapshot,verifier,cohort,score_module,window_index,catalog_reader)
    check_verified_remote_window(result,binding);return result


def check_verified_remote_window(proof,expected_binding):
    need(type(proof) is VerifiedRemoteWindow and proof._seal is _SEAL,'genuine current-process remote proof required')
    need(proof._binding==wire(expected_binding),'remote proof foreign binding')
    proof._catalog.recheck()
    _selected(proof._verifier,proof._cohort,proof._score)
    proof._verifier.window_proof(proof._cohort,proof._window)
    proof._snapshot.check()
    return {'all_verified':True,'actual_remote_ids_parents_content_verified':True,'binding_sha256':sha(proof._binding),'readback_root':str(proof._snapshot.root)}


def ship_batch(*,verifier,cohort,batch_index,score_module=None):
    """Single-batch wrapper; returns the same explicit range partition."""
    return ship_range(verifier=verifier,cohort=cohort,first_batch=batch_index,last_batch=batch_index,score_module=score_module)


def _range_result(value,batches):
    need(type(value) is dict and set(value)=={'schema','results','pending','unattempted','stop_reason'} and value['schema']=='durable-otel.delivery-range-result.v1','range result schema')
    need(value['stop_reason'] in ('','context','invalid_range','prior_journal','incomplete_batch'),'range terminal reason')
    need(all(type(value[k]) is list for k in ('results','pending','unattempted')) and len(value['pending'])<=1,'range partition lists')
    completed=[]
    for row in value['results']:
        need(type(row) is dict and set(row)=={'schema','batch','body_sha256','outcome','attempts'} and row['schema']=='durable-otel.delivery-result.v1' and row['outcome'] in ('acknowledged','unknown','unsent') and type(row['attempts']) is int and 1<=row['attempts']<=5,'original terminal batch outcome')
        completed.append({'batch':row['batch'],'body_sha256':row['body_sha256']})
    need(wire(completed+value['pending']+value['unattempted'])==wire(batches),'exact ordered attempted/pending/untouched partition')
    need(bool(value['stop_reason'])==bool(value['pending'] or value['unattempted']),'complete range versus stopped suffix')
    return value


def ship_range(*,verifier,cohort,first_batch,last_batch,score_module=None):
    """At most128 original bodies, one full closed-spool verification per helper.

    The helper's210s parent bounds the range. A journaled unknown never becomes
    resend permission. Untouched suffixes require a new disjoint bounded range.
    """
    root,catalog,reader,report=_selected(verifier,cohort,score_module)
    need(type(first_batch) is int and type(last_batch) is int and 0<first_batch<=last_batch and last_batch-first_batch<128,'bounded original batch range')
    manifest=cohort._context.manifest;spool_reader=cohort._snapshots[2];batches=[]
    for index in range(first_batch,last_batch+1):
        name=f'{index:020d}.pb';need(name in spool_reader.markers,'accepted original batch required')
        batches.append({'batch':index,'body_sha256':sha(spool_reader.read(name,bound=1<<20))})
    p=verifier.dependency('runner');binary=root/'delivery-preparation/shipper.test'
    pin=p.pin_file(binary);need(pin.sha256==catalog['helper_sha256'],'fixed shipment helper binary')
    template=root/'delivery-preparation/full1220_spool_delivery_test.go.txt'
    need(sha(reader.read('delivery-preparation/'+template.name,bound=1<<20))==catalog['helper_source_sha256'],'fixed shipment helper source')
    name=f'range-{first_batch:020d}-{last_batch:020d}'
    output=root/'delivery-jobs'/name;output.mkdir(mode=0o700);_sync(output.parent)
    job={'schema':'full1220-v11-delivery-range-job.v1','root':manifest['spool_root'],'run_id':manifest['observer']['spool_run_id'],'max_bytes':4<<30,'batches':batches,'journal_root':catalog['journal_root']}
    job_raw=wire(job);_exclusive(output/'input.json',job_raw)
    env={'PATH':'/usr/bin:/bin:/usr/sbin:/sbin','FULL1220_V11_DELIVERY_INPUT':str(output/'input.json'),'FULL1220_V11_DELIVERY_INPUT_SHA256':sha(job_raw),'FULL1220_V11_DELIVERY_OUTPUT':str(output/'result.json')}
    def stdin():
        _selected(verifier,cohort,score_module)
        for batch in batches:need(sha(spool_reader.read(f"{batch['batch']:020d}.pb",bound=1<<20))==batch['body_sha256'],'fresh original range')
        return wire(_read_settings(root,catalog,manifest))
    result={}
    def validate(outdir):
        raw=_raw(output/'result.json',1<<20);value=_range_result(document(raw),batches)
        stdout=_raw(Path(outdir)/'stdout',8<<20)
        need(re.findall(rb'(?m)^\s+full1220_spool_delivery_test.go:[0-9]+: FULL1220_DELIVERY_OUTPUT_SHA256=([0-9a-f]{64})\r?$',stdout)==[sha(raw).encode()],'selected real helper output receipt')
        result.update(value)
    process=p.run_owned(state_root=root/'delivery-supervision',attempt_name=name,argv=[str(binary),'-test.run=^TestFull1220SpoolDeliveryV11$','-test.v','-test.count=1'],cwd=Path(manifest['source_root'])/'internal/observability',env=env,executable_pin=pin,inputs=[p.pin_file(output/'input.json',immutable=False),p.pin_file(root/'delivery-preparation/catalog.json'),p.pin_file(template)],timeout=240,stdin_factory=stdin,validate_output=validate,phase='helper-run')
    need(process['accepted'] is True and process['cleanup']['confirmed'] is True and process['output_validation_supplied'] is True,'shipment helper did not complete; no repeat')
    _selected(verifier,cohort,score_module);reader.recheck()
    return {**result,'remote_verified':False,'process':process}

if __name__=='__main__':print('inactive: requires selected final V11 cohort and independently frozen delivery catalog')
