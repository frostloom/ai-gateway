#!/usr/bin/env python3
import datetime,json,pathlib,re,sys,time,urllib.request
p=pathlib.Path(sys.argv[1]).resolve(); cfg=json.loads((p/'config.json').read_text());port=cfg['ports']['gateway']; url=f'http://127.0.0.1:{int(port)}/metrics';end=time.monotonic()+90;fail=0
with (p/'inflight.jsonl').open('w') as out:
 while time.monotonic()<end:
  begin=time.monotonic();item={'time':datetime.datetime.now().isoformat()}
  try:
   with urllib.request.urlopen(url,timeout=1.0) as r: text=r.read().decode()
   match=re.search(r'^gateway_http_in_flight_requests\{service="gateway"\}\s+([0-9.eE+-]+)',text,re.M)
   if not match:raise ValueError('gauge absent')
   item['gateway_inflight_including_scrape']=float(match.group(1));fail=0
  except Exception as e:
   item['error']=type(e).__name__;fail+=1
  item['elapsed_ms']=round((time.monotonic()-begin)*1000,2);out.write(json.dumps(item)+'\n');out.flush()
  if fail>=5: break
  time.sleep(max(0,1-(time.monotonic()-begin)))
