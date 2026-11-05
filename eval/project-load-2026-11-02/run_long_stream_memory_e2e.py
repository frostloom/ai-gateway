#!/usr/bin/env python3
"""Isolated local end-to-end load harness. Never touches existing services."""
import concurrent.futures, datetime, hashlib, json, os, pathlib, signal, socket, subprocess, sys, tempfile, time, urllib.request, urllib.error, uuid
ROOT=pathlib.Path(__file__).resolve().parents[2]
OUT=ROOT/'eval/project-load-2026-11-02/long-stream-memory-runs'/datetime.datetime.now().strftime('%Y%m%d-%H%M%S')
OUT.mkdir(parents=True,exist_ok=False)
IDENT='project-long-stream-'+uuid.uuid4().hex[:12]
MYSQL=IDENT+'-mysql'; REDIS=IDENT+'-redis'; processes=[]; opened=[]
def run(args,**kw): return subprocess.run(args,check=True,text=True,**kw)
def free_port():
 with socket.socket() as s: s.bind(('127.0.0.1',0)); return s.getsockname()[1]
def port(name,inside): return run(['docker','port',name,inside],capture_output=True).stdout.strip().split(':')[-1]
def wait_http(url,proc):
 for _ in range(720):
  if proc.poll() is not None: raise RuntimeError(f'process exited: {url} code={proc.returncode}')
  try:
   with urllib.request.urlopen(url,timeout=.5) as r:
    if r.status==200:return
  except Exception: time.sleep(.25)
 raise RuntimeError('readiness timeout '+url)
def wait_tcp(n,proc):
 for _ in range(720):
  if proc.poll() is not None: raise RuntimeError(f'process exited for port {n}: {proc.returncode}')
  try:
   with socket.create_connection(('127.0.0.1',n),timeout=.3):return
  except OSError:time.sleep(.25)
 raise RuntimeError('tcp readiness timeout')
def start(name,argv,env):
 f=open(OUT/(name+'.log'),'w');opened.append(f);p=subprocess.Popen(argv,cwd=ROOT,env=env,stdout=f,stderr=subprocess.STDOUT,start_new_session=True);processes.append((name,p));return p
def cleanup():
 for _,p in reversed(processes):
  if p.poll() is None:
   try:os.killpg(p.pid,signal.SIGTERM)
   except ProcessLookupError:pass
 for _,p in reversed(processes):
  try:p.wait(timeout=5)
  except subprocess.TimeoutExpired:
   try:os.killpg(p.pid,signal.SIGKILL)
   except ProcessLookupError:pass
   p.wait()
 for f in opened:f.close()
 subprocess.run(['docker','rm','-f',MYSQL,REDIS],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
def docker_image(name):
 return run(['docker','image','inspect',name,'--format','{{.Id}}'],capture_output=True).stdout.strip()
def main():
 quick='--quick' in sys.argv
 bindir=pathlib.Path(tempfile.mkdtemp(prefix='codex-project-load-'))
 try:
  binaries={}
  builds={'billing':'./cmd/billing','router':'./cmd/router','reconciler':'./cmd/reconciler','gateway':'./cmd/gateway','mock':'./eval/project-load-2026-11-02/long-stream-mock','seed':'./eval/project-load-2026-11-02/seed','load':'./eval/project-load-2026-11-02/trace-load'}
  buildenv=os.environ.copy();buildenv['GOPROXY']='off';buildenv['GOTOOLCHAIN']='local';buildenv['SDKROOT']='/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk'
  for name,pkg in builds.items():
   dest=bindir/name
   args=['go','build']
   if name=='gateway' and '--baseline' in sys.argv:
    overlay=bindir/'overlay.json';overlay.write_text(json.dumps({'Replace':{str(ROOT/'internal/gateway/handler/chat.go'):str(ROOT/'eval/project-load-2026-11-02/gateway_stream_memory_baseline.go.txt')}}));args+=['-overlay',str(overlay)]
   args+=['-o',str(dest),pkg]
   with (OUT/(name+'-build.log')).open('w') as log:run(args,cwd=ROOT,env=buildenv,timeout=180,stdout=subprocess.DEVNULL,stderr=log)
   binaries[name]=str(dest)
  myimage='docker.m.daocloud.io/library/mysql:8.0.40';reimage='docker.m.daocloud.io/library/redis:7-alpine'
  run(['docker','run','-d','--name',MYSQL,'-e','MYSQL_ROOT_PASSWORD=isolated','-e','MYSQL_DATABASE=project_load','-p','127.0.0.1::3306',myimage],stdout=subprocess.DEVNULL)
  run(['docker','run','-d','--name',REDIS,'-p','127.0.0.1::6379',reimage],stdout=subprocess.DEVNULL)
  for _ in range(120):
   if subprocess.run(['docker','exec',MYSQL,'mysqladmin','ping','-h','127.0.0.1','-uroot','-pisolated','--silent'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0:break
   time.sleep(.5)
  else:raise RuntimeError('MySQL readiness timeout')
  mp,rp=port(MYSQL,'3306/tcp'),port(REDIS,'6379/tcp');ports={k:free_port() for k in ['gateway','billing','admin','router','mock','reconciler']}
  if len(set(ports.values()))!=len(ports):raise RuntimeError('port collision')
  base={'PATH':os.environ.get('PATH','/usr/bin:/bin'),'HOME':os.environ.get('HOME','/tmp'),'TMPDIR':os.environ.get('TMPDIR','/tmp'),'CONTROL_PLANE_TOKEN':'isolated-test-token','MYSQL_DSN':f'root:isolated@tcp(127.0.0.1:{mp})/project_load?charset=utf8mb4&parseTime=true&loc=Local','REDIS_ADDR':f'127.0.0.1:{rp}','LISTEN_HOST':'127.0.0.1','LOG_LEVEL':'error','KAFKA_BROKERS':'','GOMAXPROCS':'8'}
  def env(**kw):return dict(base,**{k:str(v) for k,v in kw.items()})
  mock=start('mock',[binaries['mock']],env(MOCK_PROVIDER_PORT=ports['mock'],MOCK_PROVIDER_NAME='isolated',MOCK_FAIL_RATE=0,MOCK_LATENCY_MS=15,MOCK_COMPLETION_TOKENS=100));wait_http(f'http://127.0.0.1:{ports["mock"]}/healthz',mock)
  bill=start('billing',[binaries['billing']],env(BILLING_PORT=ports['billing'],BILLING_ADMIN_PORT=ports['admin'],SUB_RENEW_INTERVAL_SEC=60));wait_tcp(ports['billing'],bill)
  seedenv=env(TEST_MYSQL_DSN=base['MYSQL_DSN'],TEST_REDIS_ADDR=base['REDIS_ADDR'],TEST_MOCK_PORT=ports['mock'],TEST_TENANTS=8);seed=run([binaries['seed']],cwd=ROOT,env=seedenv,capture_output=True,timeout=300);(OUT/'seed.log').write_text(seed.stdout+seed.stderr)
  router=start('router',[binaries['router']],env(ROUTER_PORT=ports['router']));wait_http(f'http://127.0.0.1:{ports["router"]}/healthz',router)
  recon=start('reconciler',[binaries['reconciler'],'-interval','1','-grace','5','-no-rebuild','-no-auto-reverse'],env(RECONCILER_PORT=ports['reconciler'],BILLING_ADDR=f'127.0.0.1:{ports["billing"]}'));wait_http(f'http://127.0.0.1:{ports["reconciler"]}/healthz',recon)
  gw=start('gateway',[binaries['gateway']],env(GATEWAY_PORT=ports['gateway'],BILLING_ADDR=f'127.0.0.1:{ports["billing"]}',BILLING_ADMIN_ADDR=f'http://127.0.0.1:{ports["admin"]}',ROUTER_ADDR=f'http://127.0.0.1:{ports["router"]}',AGENT_ADDR='http://127.0.0.1:1',RECONCILER_ADDR=f'http://127.0.0.1:{ports["reconciler"]}',RATE_LIMIT_PER_MIN=100000));wait_http(f'http://127.0.0.1:{ports["gateway"]}/metrics',gw)
  config={'head':run(['git','rev-parse','HEAD'],cwd=ROOT,capture_output=True).stdout.strip(),'branch':run(['git','branch','--show-current'],cwd=ROOT,capture_output=True).stdout.strip(),'git_status':run(['git','status','--short'],cwd=ROOT,capture_output=True).stdout,'source_sha256':{f:hashlib.sha256((ROOT/f).read_bytes()).hexdigest() for f in ['internal/billing/store/mysql.go','internal/billing/server/service.go','internal/billing/reconciler/reconciler.go','internal/gateway/client/billing.go','internal/gateway/handler/chat.go','internal/gateway/middleware/idempotency.go','eval/project-load-2026-11-02/long-stream-mock/main.go','cmd/gateway/main.go','cmd/router/main.go','cmd/billing/main.go','cmd/reconciler/main.go','cmd/mock-provider/main.go']},'images':{myimage:docker_image(myimage),reimage:docker_image(reimage)},'gomaxprocs':8,'mysql_pool_max':100,'rate_per_min':100000,'mock_latency_ms':15,'mock_completion_tokens':100,'kafka':'disabled','isolated_agents':'disabled','reconciler':'sweep every 1s, grace 5s (test only); rebuild disabled; no-auto-reverse enabled','ports':ports,'mysql_port':mp,'redis_port':rp,'quick':quick,'started':datetime.datetime.now().isoformat(),'go_version':run(['go','version'],cwd=ROOT,capture_output=True).stdout.strip(),'gateway_variant':'baseline_overlay' if '--baseline' in sys.argv else 'current_worktree', 'candidate_sha256':None if '--baseline' not in sys.argv else hashlib.sha256((ROOT/'eval/project-load-2026-11-02/gateway_stream_memory_baseline.go.txt').read_bytes()).hexdigest(),'sdkroot':'/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk','binary_sha256':{k:hashlib.sha256(pathlib.Path(v).read_bytes()).hexdigest() for k,v in binaries.items()}}
  (OUT/'config.json').write_text(json.dumps(config,ensure_ascii=False,indent=2))
  total=8;run_tag=uuid.uuid4().hex
  body=json.dumps({'model':'test-model','messages':[{'role':'user','content':'hello load'}],'max_tokens':100,'stream':True}).encode()
  import threading
  gate=threading.Barrier(total)
  def request(index):
   req=urllib.request.Request(f'http://127.0.0.1:{ports["gateway"]}/v1/chat/completions',data=body,headers={'Content-Type':'application/json','Authorization':f'Bearer sk-isolated-{index+1:03d}','Idempotency-Key':f'stream-{run_tag}-{index:03d}'},method='POST')
   gate.wait(timeout=20);began=time.monotonic()
   try:
    with urllib.request.urlopen(req,timeout=30) as response:
     raw=response.read();return {'index':index,'status':response.status,'bytes':len(raw),'done':b'[DONE]' in raw,'elapsed_seconds':time.monotonic()-began}
   except Exception as e:return {'index':index,'error':repr(e),'elapsed_seconds':time.monotonic()-began}
  def rss():return int(run(['ps','-o','rss=','-p',str(gw.pid)],capture_output=True,timeout=5).stdout.strip())
  rss_before=rss();pool=concurrent.futures.ThreadPoolExecutor(max_workers=total)
  try:
   futures=[pool.submit(request,i) for i in range(total)]
   deadline=time.monotonic()+30;stats={}
   while time.monotonic()<deadline:
    with urllib.request.urlopen(f'http://127.0.0.1:{ports["mock"]}/count',timeout=2) as response:stats=json.load(response)
    if stats.get('sent')==total:break
    time.sleep(.05)
   if stats.get('sent')!=total:raise RuntimeError('streams not fully sent '+str(stats))
   time.sleep(.25);rss_during=rss()
   results=[f.result(timeout=30) for f in futures]
  finally:pool.shutdown(wait=True)
  (OUT/'responses.json').write_text(json.dumps(results,indent=2))
  if any(x.get('status')!=200 or not x.get('done') or x.get('bytes',0)<4_000_000 for x in results):raise RuntimeError('stream response invalid '+str(results))
  query="SELECT COUNT(*),COUNT(DISTINCT request_id),SUM(status='settled') FROM bills WHERE phase='reserve' AND request_id LIKE 'idem:stream-"+run_tag+"-%'; SELECT COUNT(*),SUM(event_key LIKE '%:reserve'),SUM(event_key LIKE '%:usage'),SUM(event_key LIKE '%:settle'),SUM(event_key LIKE '%:reverse') FROM outbox_events WHERE event_key LIKE 'idem:stream-"+run_tag+"-%';"
  rows=run(['docker','exec',MYSQL,'mysql','-uroot','-pisolated','-N','project_load','-e',query],capture_output=True,timeout=20).stdout;(OUT/'rows.txt').write_text(rows)
  if rows.splitlines()!=[f'{total}\t{total}\t{total}',f'{total*3}\t{total}\t{total}\t{total}\t0']:raise RuntimeError('bad rows '+repr(rows))
  audit=run([binaries['seed']],cwd=ROOT,env=dict(seedenv,TEST_MODE='audit'),capture_output=True,timeout=120);(OUT/'audit.json').write_text(audit.stdout);a=json.loads(audit.stdout)
  if a['balance_mismatches'] or a['pending']!=0 or a['settled']!=total or a['reversed']!=0:raise RuntimeError('audit failed '+audit.stdout)
  observed={'requests':total,'upstream_calls':stats['calls'],'sent_before_rss_sample':stats['sent'],'rss_before_kib':rss_before,'rss_during_kib':rss_during,'rss_delta_kib':rss_during-rss_before,'responses_ok':total,'out':str(OUT)}
  (OUT/'observed.json').write_text(json.dumps(observed,indent=2));print(json.dumps(observed),flush=True)
  (OUT/'completed').write_text('completed\n')
 finally:
  cleanup()
  import shutil;shutil.rmtree(bindir,ignore_errors=True)
if __name__=='__main__':
 signal.signal(signal.SIGTERM, lambda *_: (_ for _ in ()).throw(SystemExit(143)))
 try: main()
 except Exception as e:
  (OUT/'error.txt').write_text(repr(e)+'\n');print(f'run failed: {e}; raw: {OUT}',file=sys.stderr);raise
