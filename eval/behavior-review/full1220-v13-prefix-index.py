"""Read-only original55 DATA index discovery. Never a completion/proof factory.

Audit files locate raw evidence; their all_verified labels are not consumed.
Every returned ref is physically read and SHA checked. Source/native/semantic,
fresh capture and original once/ACK/GET validation belong to the V13 factory.
"""
import hashlib,json,importlib.util
from pathlib import Path


def discover(repo):
    repo=Path(repo);base=repo/'.tmp/v9-live-score-horizon-20261031'
    spec=importlib.util.spec_from_file_location('v13_index_data_reader',repo/'eval/behavior-review/full1220-v13-window.py')
    w=importlib.util.module_from_spec(spec);spec.loader.exec_module(w)
    readers={};audit_reader=w.Evidence(base);windows=[];diagnostic=[]
    def raw(path,pin=None):
        path=Path(path);r=readers.setdefault(path.parent,w.Evidence(path.parent));return r.read(path.name,pin)
    def ref(path,pin=None):
        path=Path(path);data=raw(path,pin);return {'path':str(path),'sha256':hashlib.sha256(data).hexdigest()}
    for i in range(1,56):
        audit_name='independent-first-publication-audit.json' if i==1 else f'independent-window-{i:06d}-publication-audit.json' if i<40 else 'independent-h040-corrected-delivery-audit.json' if i==40 else f'independent-h{i:03d}-normal-delivery-audit.json'
        audit=w._json(audit_reader.read(audit_name));artifacts=audit['artifact_sha256'];paths={}
        for name,pin in artifacts.items():
            p=Path(name);p=p if p.is_absolute() else repo/p
            paths[p]=pin
        # First audit1 predates later GET-only evidence: collect finite existing
        # per-window run directories too, pinning actual bytes in the DATA index.
        for domain in (base/'runs',base/'capture-input-v1/runs'):
            if not domain.exists():continue
            for run in domain.glob(f'window-{i:06d}-*'):
                getdir=run/'get-evidence'
                if not getdir.is_dir():continue
                files=list(getdir.glob('get-*.body'))
                if len(files)>128:raise ValueError('bounded original GET namespace')
                for body in files:paths.setdefault(body,None)
        record=w._json(raw(Path(w.ORIGINAL_ROOT)/f'records/window-{i:06d}/receipt.json'))
        traces={r['trace_id'] for r in record['rows']};obs={};scores=[]
        for path,pin in sorted(paths.items(),key=lambda x:str(x[0])):
            if path.suffix!='.body':continue
            intent=path.with_name(path.name.replace('.body','.intent.json'));response=path.with_name(path.name.replace('.body','.response.json'))
            if not intent.exists() or not response.exists():continue
            ir=raw(intent,paths.get(intent));rr=raw(response,paths.get(response));br=raw(path,pin)
            try:kind,trace,data=w.parse_original_get(ir,rr,br)
            except ValueError:continue # preserved initial empty/failed reads are not selected
            triple={'intent':ref(intent,paths.get(intent)),'response':ref(response,paths.get(response)),'body':ref(path,pin)}
            if kind=='scores':
                scores.append(triple) # retain initial empty/false and later complete pages
            elif trace in traces:
                if not data:continue
                if trace in obs:
                    if obs[trace]['body']['sha256']!=triple['body']['sha256']:raise ValueError(f'conflicting complete original observation {i}/{trace}')
                else:obs[trace]=triple
        if set(obs)!=traces:raise ValueError(f'missing actual original observation refs window{i}: {traces-set(obs)}')
        if not scores:raise ValueError(f'missing actual score GET bodies window{i}')
        batch=w._json(raw(Path(w.ORIGINAL_REGISTRY)/f'window-{i:06d}.batch.json'))
        plans={p for p in paths if p.name=='plan.json'}
        for domain in (base/'runs',base/'capture-input-v1/runs'):
            if domain.exists():plans.update(p/'plan.json' for p in domain.glob(f'window-{i:06d}-*') if (p/'plan.json').is_file())
        chosen=[]
        for plan in sorted(plans):
            data=raw(plan,paths.get(plan));value=w._json(data)
            canonical=json.dumps(value,sort_keys=True,ensure_ascii=False,separators=(',',':'),allow_nan=False).encode()
            if hashlib.sha256(canonical).hexdigest()==batch['plan_sha256']:chosen.append(ref(plan,paths.get(plan)))
        if not chosen:raise ValueError(f'missing exact original once-bound plan window{i}')
        review=Path(w.ORIGINAL_ROOT)/f'semantic-review/window-{i:06d}-review.json'
        windows.append({'index':i,'review':ref(review,paths.get(review)),'plan':chosen[0],'gets':[obs[t] for t in sorted(obs)]+scores})
        diagnostic.append({'index':i,'audit':ref(base/audit_name),'observation_bodies':len(obs),'score_bodies':len(scores)})
    audit_reader.recheck()
    for reader in readers.values():reader.recheck()
    return {'schema':'full1220-v13-prefix-original-index.v2','windows':windows},{'data_only':True,'windows':diagnostic,'observations':sum(x['observation_bodies'] for x in diagnostic),'no_factory_or_helper_executed':True,'new_GETs':0,'new_POSTs':0}
