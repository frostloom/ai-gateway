#!/usr/bin/env python3
import collections,json,pathlib,sys
p=pathlib.Path(sys.argv[1]);r=[json.loads(x) for x in (p/'results.jsonl').read_text().splitlines()];a=[json.loads(x) for x in (p/'audits.jsonl').read_text().splitlines()]
if len(r)!=len(a):raise SystemExit(f'results={len(r)} audits={len(a)}; incomplete')
g=collections.defaultdict(list);prevp=prevd=0
for x,y in zip(r,a):
 dp=y['pending']-prevp; dd=y['settled_minus_valid_http']-prevd;prevp=y['pending'];prevd=y['settled_minus_valid_http'];g[(x['stream'],x['tenants'],x['workers'])].append((x,dp,dd))
print('| 模式 | 账户 | 并发请求 | 成功请求/秒（两轮） | 错误率 %（两轮） | P95 ms（两轮） | P99 ms（两轮） | pending 净变化 |')
print('|---|---:|---:|---|---|---|---|---:|')
for (stream,tenants,workers),vals in sorted(g.items()):
 def ls(f):return ' / '.join(f(x) for x,_,_ in vals)
 rps=ls(lambda x:f"{x['success_per_second']:.1f}")
 err=ls(lambda x:f"{100*(x['requests']-x['valid_success'])/x['requests']:.1f}" if x['requests'] else 'NA')
 p95=ls(lambda x:f"{x['p95_ms']:.0f}");p99=ls(lambda x:f"{x['p99_ms']:.0f}")
 print(f'| {"流式" if stream else "非流式"} | {tenants} | {workers} | {rps} | {err} | {p95} | {p99} | {sum(v[1] for v in vals)} |')
