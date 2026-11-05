import json,pathlib,subprocess,sys,tempfile
root=pathlib.Path(__file__).resolve().parents[2]
with tempfile.TemporaryDirectory(prefix='router-query-') as d:
 args=['go','test','-race']
 if '--baseline' in sys.argv:
  overlay=pathlib.Path(d)/'overlay.json';overlay.write_text(json.dumps({'Replace':{str(root/'internal/router/registry.go'):str(root/'eval/billing-comparison-2026-11-02/router_no_query_context_baseline.go.txt')}}));args+=['-overlay',str(overlay)]
 args+=['./eval/billing-comparison-2026-11-02','-run','^TestRouterCanceledInflightCacheQuery$','-count=3','-v']
 raise SystemExit(subprocess.run(args,cwd=root).returncode)
