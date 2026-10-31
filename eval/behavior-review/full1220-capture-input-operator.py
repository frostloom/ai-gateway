#!/usr/bin/env python3
"""Inactive by default. Fixed H40 first publication or future once publication.

Compose the frozen operator with a fresh corrected consumer. No saved diagnostic
is promoted to a proof. Original scope, IDs, comparison leaves and publisher stay
in charge; every attempted output and legacy namespace barrier is permanent.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import types
import urllib.parse

PROJECT = Path('/Users/a1/项目/ai-gateway')
D = PROJECT / '.tmp/v9-live-score-horizon-20261031'
ROOT = D / 'capture-input-v1'
RUNS = ROOT / 'runs'
OLD_RUNS = D / 'runs'
OLD_OPERATION = OLD_RUNS / 'window-000040-publish-once'
SCOPE = 'e38eff5e9b79c7b1ad245cfd7e6eb09400260d7c579affece93ec1281e9976c9'
SUMMARY = '571f1c5553a554d8d52a6990909ba33d8ed064efaa60a87508ca99073e4ff127'
BASE_SHA = '10fcf065a4c6413922b64ddc52b1f67cb704d2e3425df239379e21a9cafd6a60'
COMPONENT_SHA = 'fcca724cec129a2dbc28874571c531496c50aef31df81424b5307b3140276833'
COMPONENT = PROJECT / 'eval/behavior-review/full1220-capture-input-v1.py'


def need(ok, message):
    if not ok: raise ValueError(message)


def sha(raw): return hashlib.sha256(raw).hexdigest()


def load_base():
    path = D / 'operate-historical-scores.py'
    raw = path.read_bytes()
    need(sha(raw) == BASE_SHA, 'original operator changed')
    m = types.ModuleType('capture_input_private_base'); m.__file__ = str(path)
    exec(compile(raw, str(path), 'exec'), m.__dict__)
    return m


def observation_path(trace):
    need(type(trace) is str and re.fullmatch('[0-9a-f]{32}', trace), 'literal trace ID')
    return '/api/public/v2/observations?' + urllib.parse.urlencode({
        'traceId': trace, 'limit': 1000, 'fields': 'core,basic,metadata,usage,io'})


def entry_exists(directory, name):
    # lstat/dir_fd inspects the entry itself, including dangling symlinks.
    fd = os.open(directory, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        try: os.stat(name, dir_fd=fd, follow_symlinks=False); return True
        except FileNotFoundError: return False
    finally: os.close(fd)


def registry_names(plan):
    names = ['window-%06d.batch.json' % plan['window_index']]
    for score in plan['scores']:
        identifier = score['id']
        need(re.fullmatch('[0-9a-f]{64}', identifier), 'literal original score ID')
        names.extend(('logical-'+identifier+'.intent.json', 'score-'+identifier+'.intent.json',
                      identifier+'.sent.json', identifier+'.accepted.json'))
    return names


def require_empty(handle, plan):
    handle.check()
    for name in registry_names(plan):
        try: os.stat(name, dir_fd=handle.scope_fd, follow_symlinks=False)
        except FileNotFoundError: continue
        raise ValueError('Publication entry consumed/conflicting; verify only')
    handle.check()


def require_unused_namespaces(base, index):
    for parent in (OLD_RUNS, RUNS):
        base.private_dir(parent)
        # All old/v2 publication spellings and new completion/normal spellings.
        for name in os.listdir(parent):
            if name.startswith(f'window-{index:06d}-') and ('publish' in name or 'completion' in name):
                raise ValueError('Publication namespace consumed; never resume')


def reserve(base, mode, index, job_sha):
    need(mode in ('completion', 'normal'), 'explicit publication mode')
    need(type(index) is int and (index == 40 if mode == 'completion' else 40 < index <= 511), 'fixed completion/future target')
    base.private_dir(RUNS)
    name = f'window-{index:06d}-' + ('completion-once' if mode == 'completion' else 'publish-once')
    if mode == 'normal': require_unused_namespaces(base, index)
    else:
        base.private_dir(OLD_RUNS)
        for n in os.listdir(OLD_RUNS):
            need(not (n.startswith('window-000040-') and ('publish' in n or 'completion' in n) and n != 'window-000040-publish-once'), 'alternate H40 operation consumed')
        for n in os.listdir(RUNS):
            need(not (n.startswith('window-000040-') and ('publish' in n or 'completion' in n)), 'H40 completion namespace consumed')
    output = RUNS/name
    if mode == 'normal':
        # Under the original scope lock, consume the legacy namespace FIRST.
        # Any later marker/output/write failure leaves old reserve blocked.
        # This is explicitly a new-entry-owned sentinel, not an old execution.
        sentinel = OLD_RUNS/f'window-{index:06d}-publish-once'
        sentinel.mkdir(mode=0o700); base.sync_dir(OLD_RUNS)
        base.record(sentinel/'new-entry-owned-sentinel.json', {
            'schema':'full1220-capture-input-legacy-barrier.v1', 'entry':'capture-input-v1',
            'target_index':index, 'job_sha256':job_sha, 'output':str(output),
            'old_operator_executed':False, 'permanent_no_reentry':True})
    output.mkdir(mode=0o700); base.sync_dir(RUNS)
    base.record(output/'operation-intent.json', {'schema':'full1220-capture-input-operation.v1',
        'entry':'capture-input-v1', 'mode':mode, 'target_index':index, 'job_sha256':job_sha,
        'reentry_forbidden':True})
    return output


def reserve_source_output(base, operation_name):
    # The unchanged historical issuer only accepts D/runs descendants. This
    # supplemental source-only namespace has no old publisher semantics.
    need(re.fullmatch('[a-z0-9-]{1,120}', operation_name), 'fixed source operation name')
    parent = OLD_RUNS/'capture-input-v1-source'
    if not entry_exists(OLD_RUNS, parent.name):
        parent.mkdir(mode=0o700); base.sync_dir(OLD_RUNS)
    base.private_dir(parent)
    output = parent/operation_name
    output.mkdir(mode=0o700); base.sync_dir(parent)
    base.record(output/'new-entry-source-namespace.json', {
        'schema':'full1220-capture-input-source-namespace.v1', 'entry':'capture-input-v1',
        'operation_name':operation_name, 'old_publisher_executed':False, 'no_reentry':True})
    return output


class ReplayResponse:
    def __init__(self, raw, status, done):
        self.raw, self.status, self.done, self.used = raw, status, done, False
    def __enter__(self): return self
    def __exit__(self, *args): pass
    def read(self, size=-1):
        need(not self.used, 'original response consumed once'); self.used = True
        need(type(size) is int and (size == -1 or size >= len(self.raw)), 'partial replay forbidden')
        self.done(); return self.raw


class ReplayOpener:
    """Exact original observation paths only; score verification is delegated."""
    def __init__(self, opener, base, traces, replay):
        self.opener, self.base = opener, base
        self.paths = {observation_path(t) for t in traces}
        need(len(self.paths) == len(traces) and set(replay) <= self.paths, 'exact unique replay trace set')
        self.replay, self.attempted, self.read = replay, set(), set()
    def open(self, request, *args, **kwargs):
        url = request.full_url; parsed = urllib.parse.urlsplit(url)
        path = urllib.parse.urlunsplit(('', '', parsed.path, parsed.query, ''))
        need(url == self.base+path, 'literal original request origin/path')
        if request.get_method() == 'GET' and parsed.path == '/api/public/v3/scores':
            return self.opener.open(request, *args, **kwargs)
        if request.get_method() == 'POST' and path == '/api/public/scores':
            self.complete(); return self.opener.open(request, *args, **kwargs)
        need(request.get_method() == 'GET' and path in self.paths, 'literal original observation request')
        need(path not in self.attempted, 'observation request already attempted'); self.attempted.add(path)
        if path in self.replay:
            row = self.replay[path]
            return ReplayResponse(row['body'], row['status'], lambda: self.read.add(path))
        response = self.opener.open(request, *args, **kwargs)
        outer = self
        class NewResponse:
            def __enter__(self): response.__enter__(); return self
            def __exit__(self, *a): return response.__exit__(*a)
            @property
            def status(self): return response.status
            def read(self, *a, **kw):
                need(path not in outer.read, 'new response consumed once')
                raw = response.read(*a, **kw)
                need(type(raw) is bytes and response.status == 200, 'complete new observation HTTP200')
                outer.read.add(path); return raw
        return NewResponse()
    def complete(self):
        need(self.attempted == self.paths and self.read == self.paths, 'all original trace requests/read bodies required')


def load_completion(base, reader):
    values = {}
    for name, digest in H40_FILES.items():
        values[name] = reader.read(OLD_OPERATION/name, digest)
    plan = base.document(values['plan.json'])
    error = base.document(values['operation-error.json'])
    intent = base.document(values['publish-api-intent.json'])
    need(error == {'error_type':'ValueError','mode':'publish','next_action':'verify-only','publish_reentry_forbidden':True}, 'original fixed failure')
    need(intent['may_have_sent_POST'] is True and intent['plan_sha256'] == sha(base.wire(plan).rstrip(b'\n')), 'original outer uncertainty remains true')
    need(plan['window_index'] == 40 and plan['provenance']['summary_sha256'] == SUMMARY and plan['provenance']['scope_sha256'] == SCOPE, 'original H40 plan identity')
    replay = {}
    for n in range(1, 8):
        stem = f'get-evidence/get-{n:06d}'
        original = base.document(values[stem+'.intent.json']); response = base.document(values[stem+'.response.json']); body = values[stem+'.body']
        need(original == {'method':'GET', 'path':original['path'], 'may_have_been_sent':True}, 'exact original GET intent')
        matches = [t for t in plan['trace_bindings'] if observation_path(t) == original['path']]
        need(len(matches) == 1 and original['path'] not in replay, 'exact unique original GET trace')
        need(response == {'complete':True,'raw_bytes':len(body),'raw_sha256':sha(body),'same_request_no_additional_GET':True,'status':200}, 'complete exact original HTTP200 body')
        payload = base.document(body)
        need(type(payload.get('data')) is list and not payload.get('meta', {}).get('cursor'), 'complete original observation response')
        need(all(x.get('traceId') == matches[0] for x in payload['data']), 'original body trace substitution')
        replay[original['path']] = {'body':body, 'status':200, 'body_sha256':sha(body)}
    ids = [s['id'] for s in plan['scores']]
    need(len(ids) == len(set(ids)) == 28 and len(plan['trace_bindings']) == 10, 'original 28 IDs/10 traces')
    contract = {'files':H40_FILES, 'score_ids':ids, 'traces':list(plan['trace_bindings']),
                'target_index':40, 'horizon_index':40, 'summary_sha256':SUMMARY, 'original_may_have_sent_POST':True}
    return contract, plan, replay


def install_publication_guards(scores, plan, before_batch):
    """Fresh module only. Original publisher owns its second lock until ACK."""
    original_open = scores._open_publication_registry
    original_event = scores._publication_event
    original_post = scores._WindowScoreClient.post_score
    own = {}; locked = [None]
    expected_names = registry_names(plan)
    def opened(scope, *, create=False):
        need(scope == plan['provenance']['scope_sha256'], 'original scope lock')
        handle = original_open(scope, create=create)
        if create:
            try: require_empty(handle, plan)
            except BaseException: handle.close(); raise
            locked[0] = handle
        return handle
    def check_owned(handle):
        # This registry is now intentionally reserved by this operation.
        for expected in expected_names:
            if expected.endswith(('.sent.json', '.accepted.json')) and expected not in own:
                need(not entry_exists(handle.scope, expected), 'prior sent/ACK conflict')
            else:
                need(expected in own, 'complete operation batch/intents before POST')
                need(scores._regular_bytes(handle.scope/expected) == own[expected], 'operation reservation changed')
        handle.check()
    def event(handle, name, value):
        need(handle is locked[0], 'same original publisher lock')
        need(name in expected_names and name not in own, 'exact operation event once')
        if name.endswith('.batch.json'): before_batch()
        if name.endswith('.sent.json'): check_owned(handle)
        original_event(handle, name, value)
        own[name] = scores._json_bytes(value)
    def post(client, payload):
        handle = locked[0]
        need(handle is not None, 'original publisher lock held at POST')
        identifier = payload['id']
        need(identifier+'.sent.json' in own and identifier+'.accepted.json' not in own, 'own sent-before-POST without ACK')
        check_owned(handle)
        expected = scores._Document(own['logical-'+identifier+'.intent.json']).value['score']
        need(scores._json_bytes(payload) == scores._json_bytes(expected), 'exact newly reserved score payload')
        return original_post(client, payload)
    scores._WindowScoreClient.post_score = post
    scores._open_publication_registry = opened
    scores._publication_event = event


def operate(job_path, job_sha, *, execute=False, readback_id=None):
    need(execute is True, 'UNFROZEN: explicit activation required')
    base = load_base(); reader = base.Reader()
    job_path = Path(job_path)
    need(job_path.parent == ROOT and job_path.resolve() == job_path, 'fixed supplemental job directory')
    job = base.document(reader.read(job_path, base.pin(job_sha), 1 << 20))
    need(set(job) == {'schema','mode','operator_sha256','component_sha256','binding_sha256','base_job','base_job_sha256','completion'}, 'exact capture operator job')
    need(job['schema'] == 'full1220-capture-input-operation-job.v1' and job['mode'] in ('completion','normal','verify'), 'fixed operator schema/mode')
    reader.read(Path(__file__).absolute(), base.pin(job['operator_sha256']), 1 << 20)
    raw = reader.read(COMPONENT, COMPONENT_SHA, 1 << 20)
    need(job['component_sha256'] == COMPONENT_SHA, 'fixed correction component')
    component = types.ModuleType('capture_input_private_component'); component.__file__ = str(COMPONENT)
    exec(compile(raw, str(COMPONENT), 'exec'), component.__dict__)
    base_path = ROOT/job['base_job']
    need(base_path.parent == ROOT and base_path.name == job['base_job'], 'base job basename only')
    mode = 'verify' if job['mode'] == 'verify' else 'publish'
    data = base.preflight(mode, base_path, base.pin(job['base_job_sha256']))
    data['reader'].items.update(reader.items); reader = data['reader']
    index = data['job']['target_index']
    contract = old_plan = None; replay = {}
    if job['mode'] == 'completion':
        contract, old_plan, replay = load_completion(base, reader)
        need(job['completion'] == contract, 'fixed original completion contract')
        need(index == data['job']['horizon_index'] == 40 and data['job']['target_summary_sha256'] == data['job']['horizon_summary_sha256'] == SUMMARY, 'fixed H40 summary/horizon')
    else:
        need(job['completion'] is None, 'normal/verify has no completion/recovery semantics')
        need(job['mode'] == 'verify' or 40 < index <= 511, 'normal future targets only')
    need(mode == 'verify' or readback_id is None, 'publication output is fixed')
    # Reserve permanently under original scope lock before corrected loader,
    # source helper, settings, capture, or any API. Never nest this with publish.
    old_scores = base.load_scores(data['consumer_raw'])
    if mode == 'publish':
        handle = old_scores._open_publication_registry(SCOPE, create=True)
        try:
            if old_plan is not None: require_empty(handle, old_plan)
            else:
                need(not entry_exists(handle.scope, f'window-{index:06d}.batch.json'), 'original target batch exists')
            reader.recheck()
            output = reserve(base, job['mode'], index, job_sha)
        finally: handle.close()
    else:
        base.RUNS = RUNS
        output = base.reserve('verify', index, readback_id, job_sha)
    try:
        source_output = reserve_source_output(base, output.name)
        scores = component.load_corrected_consumer(str(component.BINDING_PATH), base.pin(job['binding_sha256']))
        original_factory = scores.verify_actual_score_inputs
        def factory(*a, **kw):
            need(kw['horizon_helper_output_dir'] == str(output/'horizon-source') and kw['helper_output_dir'] == str(output/'target-source'), 'original operator helper outputs')
            kw['horizon_helper_output_dir'] = str(source_output/'horizon-source')
            kw['helper_output_dir'] = str(source_output/'target-source')
            return original_factory(*a, **kw)
        scores.verify_actual_score_inputs = factory
        original_build = scores.build_window_score_plan
        state = {}
        def build(*a, **kw):
            plan = original_build(*a, **kw)
            if old_plan is not None:
                need(scores._json_bytes(plan) == scores._json_bytes(old_plan), 'fresh original H40 plan changed')
            state['plan'] = plan
            if mode == 'publish':
                install_publication_guards(scores, plan, lambda: (reader.recheck(), state['replay'].complete()) if 'replay' in state else reader.recheck())
            return plan
        scores.build_window_score_plan = build
        original_client = scores.load_window_score_client
        def client(*a, **kw):
            result = original_client(*a, **kw)
            if job['mode'] == 'completion':
                opener = ReplayOpener(result._inner.opener, result._inner.base, list(state['plan']['trace_bindings']), replay)
                state['replay'] = opener; result._inner.opener = opener
            return result
        scores.load_window_score_client = client
        base.preflight = lambda *a: data
        base.load_scores = lambda raw: scores
        base.reserve = lambda *a: output
        base.PROJECTION_HELPER_SHA256 = component.TEMPLATE_SHA
        return base.operate(mode, base_path, job['base_job_sha256'], readback_id)
    except BaseException as error:
        # Base records its own failures. Loader failures also retain a terminal
        # marker without overwriting an existing record or exception text.
        if not entry_exists(output, 'operation-error.json'):
            base.record(output/'operation-error.json', {'entry':'capture-input-v1', 'error_type':type(error).__name__, 'reentry_forbidden':mode=='publish'})
        raise


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--execute', action='store_true'); parser.add_argument('--job', type=Path)
    parser.add_argument('--job-sha256'); parser.add_argument('--readback-id')
    args = parser.parse_args(argv)
    if not args.execute:
        print('{"state":"UNFROZEN","inputs_read":0,"key_reads":0,"GETs":0,"POSTs":0}'); return 0
    need(args.job is not None and args.job_sha256 is not None, 'explicit pinned job')
    os.umask(0o077)
    operate(args.job, args.job_sha256, execute=True, readback_id=args.readback_id); return 0


# Literal original evidence pins; no diagnosis is used as an authority.
H40_FILES = {'operation-error.json': 'f54a0a3e213bbb669a75998cd6853dc307593a2f7c8bc0334dc89a5eaf8ce54f',
 'plan.json': '5c3244b32ef37b3192a13f585e4a8fdb44bdba3f79773433982f47d892b8ed18',
 'publish-api-intent.json': '60ea3b2cee2306a88f77169dfd6588bc88679daba38979ef24078911a93290dc',
 'get-evidence/get-000001.body': '63792aa2adee472c404b3813a9ac9f6e593e025764d7705a291c281a77523547',
 'get-evidence/get-000001.intent.json': '0ae80c0752293357ab1b26f9f2536e1f58eff896c363c6f969c34a62587711c5',
 'get-evidence/get-000001.response.json': '82399d33063753a11004b1c6f5a3cdd7db601f96ffc1fd4df194db8f88bb63eb',
 'get-evidence/get-000002.body': '0bc934d8f7a1c987fc348d87dea12463a29b18effeca75579f389128a06765fd',
 'get-evidence/get-000002.intent.json': 'db34fee198e6e511c1bbb14f69e28841ce20227618f7aaf2bdda9ccf967cddb3',
 'get-evidence/get-000002.response.json': '2ca8bbd96b89c905475b6cf01844cb351d5a2583b8657aabba50be008b5eeb09',
 'get-evidence/get-000003.body': '41d2590e1c0920b2d5786b5a5c2ae5418082192076896c056755a06d983af548',
 'get-evidence/get-000003.intent.json': '7b6f8ad418671625dd7bd30c23bda3a5641b67148236b3d98b9be0c7442063b5',
 'get-evidence/get-000003.response.json': '70f7bad2de575f017b46f832359d3841173778e8c6db0b1890de381153836d2c',
 'get-evidence/get-000004.body': 'd04fcadaca8ac50b10e8f4036010eff9e880bc7815356750f56679845777b5bd',
 'get-evidence/get-000004.intent.json': '092826f6e1becd644d69335dc518a709ce46bcaf7095d8cc0611ae9b6779b640',
 'get-evidence/get-000004.response.json': '29f16aab64e5fabfe17d3bc71b3866c77d05352a34ace11821727d2ea3f9d323',
 'get-evidence/get-000005.body': '086ae540a4194c57b59a0927a09d7364743f8019cc40ed80986cd2210fa8f72f',
 'get-evidence/get-000005.intent.json': '20eacaeedd0b57211550bf6a04e9db24d583e6a764a603da32ffab9a82339207',
 'get-evidence/get-000005.response.json': '5cc4be727a57a87a355f0b1959c7c514befa4cb3457ce217612f8cd21f4f1888',
 'get-evidence/get-000006.body': 'bc52a864cf7738538f28aac10d32eb747a6292d71fbc006721fb0bef614aa589',
 'get-evidence/get-000006.intent.json': 'f19fdf5a05d2103fdf4ae0da65e65e8b9ec1984def911596a3c23a60091c74cd',
 'get-evidence/get-000006.response.json': 'be8d2a0d6cf0b60f1828245db7c8b162cb6e40a00369fc8098dc4c8ae8705212',
 'get-evidence/get-000007.body': '3621200334cc41a07b44adcc5a4a23a60e0af2adde5515a22951332b0ce799dc',
 'get-evidence/get-000007.intent.json': 'b0a8ac730ca2d7fad473e6aae360567bd02ee35e89f88c28c8dc30caa20a7373',
 'get-evidence/get-000007.response.json': 'e9ffa6ea8a2fc54ce0287910228296396c179fc917e667934ee1529c8b3bcb32'}

if __name__ == "__main__": raise SystemExit(main())
