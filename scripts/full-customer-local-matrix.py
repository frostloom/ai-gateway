"""Plan and audit all 1220 primary tasks using closed local substitutes only."""
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[1]
IDENTITY = Path('eval/paired-pilots/2026-10-27-full1220-adapter/selection-identity.json')
RUNTIME_SHA = {'prompt_sha256':'731344ccef1bfc40456b6797e9c7cb5c5c3d628799d3e5ee8ab05f3142c6f7fa',
               'tool_schema_sha256':'3962937d7fd08af4794758a56070b7bbadfb5c3e188cf1fa539355780b9e0e17'}

def require(ok, message):
    if not ok:
        raise ValueError(message)

def sha(raw):
    return hashlib.sha256(raw).hexdigest()

def reference(path):
    path = Path(path).resolve()
    return {'path': str(path), 'sha256': sha(path.read_bytes())}

def read_ref(ref):
    require(isinstance(ref, dict) and set(ref) == {'path', 'sha256'}, 'Explicit SHA file reference required')
    raw = Path(ref['path']).read_bytes()
    require(sha(raw) == ref['sha256'], 'Evidence SHA changed')
    return raw

def canonical(raw):
    # Go encoding/json's compact representation, including its HTML escapes.
    return json.dumps(raw, ensure_ascii=False, separators=(',',':')).replace('&','\\u0026').replace('<','\\u003c').replace('>','\\u003e').replace('\u2028','\\u2028').replace('\u2029','\\u2029').encode()

def typed_case(row, scenario):
    boolean_fields = ['business_state_unchanged','pending_required','pending_optional','bound_pending_required',
                      'pending_cleared','no_second_execution','no_execution_without_new_preview','no_write',
                      'clarification_is_necessary_not_failure']
    turns = []
    for t in scenario['turns']:
        v = {'user':t['user'],'expected_actions':t.get('expected_actions'),'allowed_tools':t.get('allowed_tools'),
             **{k:t.get(k,False) for k in boolean_fields}}
        if t.get('answer'): v['answer']=t['answer']
        turns.append(v)
    return {'source_case_id':row['source_case_id'],'answer_assertions':[{k:a[k] for k in ['id','requirement']} for a in row.get('answer_assertions',[])],
            'scenario':{'id':scenario['id'],'profile':scenario['profile'],'turns':turns,
                        'terminal':scenario.get('terminal'),'operation':({k:scenario['operation'].get(k) for k in ['tool','arguments']} if scenario.get('operation') else None),
                        'measurement_scope':scenario['measurement_scope'],
                        'full_task_metric_eligible':scenario['full_task_metric_eligible']}}

def case_digest(case):
    return sha(json.dumps(case,sort_keys=True,ensure_ascii=False,separators=(',',':')).encode())

def prepare(root):
    root = Path(root)
    spec = importlib.util.spec_from_file_location('original_full_source', root/'scripts/prepare-full-customer-pilot.py')
    source = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(source)
    selected = source.select_primary(source.load_definitions(root))
    identity = json.loads((root/IDENTITY).read_bytes())
    require(identity['definition_sha256'] == sha((root/source.ARCHIVE/'definitions.json').read_bytes())
            and identity['manifest_sha256'] == source.MANIFEST_SHA
            and identity['profile_sha256'] == source.PROFILE_SHA
            and sha((root/source.PROFILE).read_bytes()) == source.PROFILE_SHA, 'Frozen source changed')
    cases = [{'key': row['source_case_id']+'/'+scenario['id'], 'turns': len(scenario['turns']),
              'profile': scenario['profile'], 'case_sha256':case_digest(typed_case(row,scenario))} for row, scenario in selected]
    require(len(cases) == len({r['key'] for r in cases}) == 1220 and sum(r['turns'] for r in cases) == 1962,
            'Full denominator changed')
    return {'schema': 'full-primary-local-matrix-plan.v1',
            **{k: identity[k] for k in ['selection_scope','selection_sha256','definition_sha256','manifest_sha256','profile_sha256']},
            'source_cases': 1220, 'planned_arms': 2440, 'planned_user_turns': 3924,
            'windows': [{'id': f'window-{i//100+1:03d}', 'timeout_seconds': 300, 'cases': cases[i:i+100]}
                        for i in range(0,1220,100)], 'allow_paid': False, 'actual_full1220_complete': False}

def validate_configuration(plan, c, cfg, revision, contract):
    case=cfg['suite']['cases'][0]
    require(case_digest(case) == c['case_sha256'], 'Frozen scenario contents differ')
    require(sha(canonical(cfg)) == contract, 'Configuration contract differs')
    for arm in ['model','selective']:
        runtime=cfg[arm]
        require(all(runtime.get(k)==v for k,v in RUNTIME_SHA.items()), 'Pinned prompt/tool schema changed')
        require(runtime['execution_revision'] == revision
                and runtime['evidence_kind'] == 'local_stub' and runtime['model'] == 'local-full-customer'
                and runtime['provider_protocol'] == 'anthropic' and runtime['output_budget'] == 128
                and runtime['thinking_mode'] == 'disabled'
                and runtime['definition_sha256'] == plan['definition_sha256']
                and runtime['manifest_sha256'] == plan['manifest_sha256']
                and runtime['profile_sha256'] == {c['profile']:plan['profile_sha256']}, 'Runtime contract differs')
    expected_selective={**cfg['model'],'routing_mode':'jev_selective'}
    require(cfg['model']['routing_mode'] == 'model' and cfg['selective']==expected_selective, 'Unexpected comparison variable')

def checked_window(plan, window, evidence):
    r = json.loads(read_ref(evidence['receipt']))
    directory = Path(evidence['receipt']['path']).resolve().parent
    wp = json.loads(read_ref(evidence['plan']))
    require(evidence['plan']['path'] == str(directory/'plan.json'), 'Window plan location differs')
    for obj in [r, wp]:
        for k in ['selection_scope','selection_sha256','definition_sha256','manifest_sha256','profile_sha256']:
            require(obj.get(k) == plan[k], 'Window source differs: '+k)
        require(obj.get('allow_paid') is False, 'Local window claimed paid permission')
        require(obj.get('execution_revision') == evidence['execution_revision']
                and re.fullmatch('[a-f0-9]{40}', evidence['execution_revision']), 'Executable revision differs')
    require(r.get('schema') == 'full-primary-local-window-receipt.v1'
            and wp.get('schema') == 'full-primary-local-window-plan.v1'
            and r.get('evidence_kind') == 'local_stub' and r.get('status') == 'completed_local'
            and not r.get('failed_stage') and r.get('completed_batches') == len(window['cases']), 'Incomplete local window')
    for k in ['ready_for_paid_run','actual_full1220_complete']:
        require(r.get(k) is False, 'Synthetic evidence claimed readiness or actual completion')
    for k in ['external_model_calls','external_jev_calls']:
        require(type(r.get(k)) is int and r[k] == 0, 'External inference in local evidence')
    require(len(r['batches']) == len(wp['batches']) == len(window['cases']), 'Window cases missing')
    turns = sum(c['turns']*2 for c in window['cases'])
    require(r['planned_cases'] == len(window['cases']) and r['planned_arms'] == len(window['cases'])*2
            and r['planned_user_turns'] == wp['planned_user_turns'] == turns, 'Window denominator differs')
    capture_raw = (directory/'local-observations.json').read_bytes()
    require(sha(capture_raw) == r['local_otlp_sha256'], 'Raw local capture binding differs')
    spans = json.loads(capture_raw)['spans']
    require(len({s['span_id'] for s in spans}) == len(spans), 'Duplicate local span ID')
    consumed = set()
    rows, generations, traces = [], 0, set()
    for index, (c, b, declaration) in enumerate(zip(window['cases'], r['batches'], wp['batches'])):
        bid = f'batch-{index+1:03d}'
        require(b['id'] == declaration['id'] == bid and b['key'] == declaration['key'] == c['key']
                and b['status'] == 'checked_local' and not b.get('failed_stage'), 'Wrong local batch')
        cfg = declaration['configuration']
        arm_order=['model','jev_selective'] if index%2==0 else ['jev_selective','model']
        require(b.get('arm_order') == declaration.get('arm_order') == cfg.get('arm_order') == arm_order,
                'Frozen local arm order differs')
        case = cfg['suite']['cases'][0]
        validate_configuration(plan,c,cfg,evidence['execution_revision'],declaration['pair_run_contract'])
        require(len(cfg['suite']['cases']) == 1 and case['source_case_id']+'/'+case['scenario']['id'] == c['key']
                and len(case['scenario']['turns']) == c['turns'] and case['scenario']['profile'] == c['profile']
                and cfg['selection_scope'] == plan['selection_scope'] and cfg['selection_sha256'] == plan['selection_sha256']
                and cfg['allow_paid'] is False, 'Batch configuration differs')
        report_raw = (directory/'campaign'/bid/'report.json').read_bytes()
        journal_raw = (directory/'campaign'/bid/'run.jsonl').read_bytes()
        report = json.loads(report_raw)
        require(sha(report_raw) == b['report_sha256'] and report == b['report']
                and sha(journal_raw) == b['journal_sha256'] == report['journal_sha256']
                and b['pair_run_contract'] == declaration['pair_run_contract'], 'Raw report/journal binding differs')
        events = [json.loads(line) for line in journal_raw.splitlines()]
        require(events[0].get('kind') == 'config' and events[0].get('config') == cfg, 'Raw journal configuration differs')
        require([e['kind'] for e in events] == ['config','start','finish','start','finish'], 'Journal state/order differs')
        expected_order = cfg['arm_order']
        require([e['arm'] for e in events if e['kind']=='start'] == expected_order
                and [e['arm'] for e in events if e['kind']=='finish'] == expected_order
                and all(e.get('key') == c['key'] for e in events[1:]), 'Journal arm ordering differs')
        finish = [e for e in events if e['kind'] == 'finish']
        require(len(finish) == 2 and {(e['key'],e['arm']) for e in finish} == {(c['key'],a) for a in ['model','jev_selective']}, 'Raw finished arms differ')
        require(report['evidence_kind'] == 'local_stub' and report['effect_claims_allowed'] is False
                and report['planned_cases'] == 1 and report['planned_arms'] == report['finished_arms'] == 2
                and report['unrun_arms'] == 0 and len(report['rows']) == 2, 'Incomplete local report')
        require({row['arm'] for row in report['rows']} == {'model','jev_selective'}, 'Duplicate report arm')
        for row in report['rows']:
            require(row['key'] == c['key'] and row['status'] == 'needs_review' and row['structural_pass'] is True
                    and row['fact_errors'] == row['http_failures'] == 0 and not row.get('execution_failures')
                    and row['observed_turns'] == c['turns'], 'Synthetic structural or unknown semantic status differs')
            event = next(e for e in finish if e['arm'] == row['arm'])
            result = event['result']
            require(event.get('langfuse_trace_id') == row['langfuse_trace_id'] and not event.get('execution_failures')
                    and result.get('status') == row['status'] and result.get('structural_pass') is True
                    and result.get('success') is False and result.get('setup_error') is False
                    and result.get('http_failures') == 0 and len(result.get('observations',[])) == row['observed_turns']
                    and event['budget_after']['attempts'] - event['budget_before']['attempts'] == row['guarded_upstream_model_attempts'],
                    'Raw finish contradicts report')
            trace = row['langfuse_trace_id']
            require(re.fullmatch('[a-f0-9]{32}',trace) and trace not in traces, 'Trace identity missing/duplicate')
            traces.add(trace)
            observed = [s for s in spans if s['trace_id'] == trace]
            counts = {'evaluation.task':0, 'agent.turn':0, 'llm.attempt':0}
            for span in observed:
                meta = span['metadata']
                require(meta.get('case_key') == c['key'] and meta.get('arm') == row['arm']
                        and meta.get('run_id') == b['pair_run_contract'] and meta.get('evidence_kind') == 'local_stub',
                        'Raw local span association differs')
                require(span['name'] in counts or span['name'].startswith('tool.') or span['name'].startswith('jev.'), 'Unknown local span')
                if span['name'] == 'evaluation.task':
                    require(meta.get('task_success') == 'unknown', 'Synthetic task success claimed')
                if span['name'] in counts: counts[span['name']] += 1
                consumed.add(span['span_id'])
            require(counts == {'evaluation.task':1,'agent.turn':c['turns'],
                               'llm.attempt':row['guarded_upstream_model_attempts']}, 'Raw local span coverage differs')
            generations += row['guarded_upstream_model_attempts']
        rows.append({**c, 'window_id':window['id'], 'status':'checked_local', 'arms':2,
                     'observed_turns':2*c['turns'], 'semantic_review':'not_performed'})
    require(len(consumed) == len(spans), 'Unassociated local span')
    require(r['checked_local_otlp'] == {'tasks':2*len(rows),'turns':turns,'generations':generations}
            and r['synthetic_model_attempts'] == generations, 'Local OTLP capture totals differ')
    return rows, generations, traces

def aggregate(plan, window_evidence):
    require(plan == prepare(ROOT), 'Matrix plan differs from original frozen selector')
    require(len(window_evidence) <= len(plan['windows']), 'Extra windows')
    rows, traces, generations, stopped = [], set(), 0, False
    for index, window in enumerate(plan['windows']):
        if index >= len(window_evidence):
            rows.extend({**c,'window_id':window['id'],'status':'not_started'} for c in window['cases'])
            continue
        ev = window_evidence[index]
        require(not stopped and ev.get('window_id') == window['id'] and type(ev.get('return_code')) is int,
                'Duplicate, reordered or continued-after-failure window')
        require(all(k in ev for k in ['stdout','stderr','binary']), 'Missing execution provenance')
        for name in ['stdout','stderr','binary']:
            read_ref(ev[name])
        require(re.fullmatch('[a-f0-9]{40}',ev.get('execution_revision','')), 'Archived execution revision required')
        if index:
            first=window_evidence[0]
            require(ev['execution_revision'] == first['execution_revision']
                    and ev['binary']['sha256'] == first['binary']['sha256'], 'Matrix executable changed between windows')
        if ev['return_code'] != 0:
            stopped = True
            rows.extend({**c,'window_id':window['id'],'status':'failed_window_unverified'} for c in window['cases'])
            continue
        checked, count, ids = checked_window(plan, window, ev)
        require(not (ids & traces), 'Trace reused across windows')
        rows.extend(checked); generations += count; traces |= ids
    complete = sum(r['status']=='checked_local' for r in rows)
    return {'schema':'full-primary-local-matrix-result.v1','status':'completed_local' if complete==1220 else 'incomplete_local',
            'source_cases':1220,'checked_local_pairs':complete,'checked_local_arms':complete*2,
            'observed_local_turns':sum(r.get('observed_turns',0) for r in rows),
            'unrun_cases':sum(r['status']=='not_started' for r in rows),
            'failed_window_unverified_cases':sum(r['status']=='failed_window_unverified' for r in rows),
            'synthetic_model_attempts':generations,'unique_local_traces':len(traces),'cases':rows,
            'external_model_calls':0,'external_jev_calls':0,'allow_paid':False,'ready_for_paid_run':False,
            'actual_full1220_complete':False,'independent_semantic_review':'not_performed',
            'limitations':['Oracle-informed closed substitutes; no model quality, route benefits, invoice or paid token-envelope proof.',
                           'Reports, local capture and source provenance are trusted; SHA bindings are not cryptographic execution attestations.',
                           'Failed windows remain unverified here; their private partial receipts and journals are preserved.']}

def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('mode',choices=['prepare','aggregate'])
    p.add_argument('--plan',type=Path)
    p.add_argument('--evidence',type=Path)
    p.add_argument('--out',type=Path,required=True)
    a=p.parse_args()
    result=prepare(ROOT) if a.mode=='prepare' else aggregate(json.loads(a.plan.read_bytes()),json.loads(a.evidence.read_bytes()))
    with a.out.open('x') as f: json.dump(result,f,ensure_ascii=False,indent=2);f.write('\n')

if __name__=='__main__':main()
