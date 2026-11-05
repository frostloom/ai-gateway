#!/usr/bin/env python3
"""Isolated local end-to-end load harness. Never touches existing services."""
import csv, datetime, hashlib, json, os, pathlib, signal, socket, subprocess, sys, tempfile, time, urllib.request, urllib.error, uuid
ROOT=pathlib.Path(__file__).resolve().parents[2]
OUT=ROOT/'eval/project-load-2026-11-02/redis-fault-runs'/datetime.datetime.now().strftime('%Y%m%d-%H%M%S')
OUT.mkdir(parents=True,exist_ok=False)
IDENT='project-redis-fault-'+uuid.uuid4().hex[:12]
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
 subprocess.run(['docker','unpause',REDIS],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
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
  builds={'billing':'./cmd/billing','router':'./cmd/router','reconciler':'./cmd/reconciler','gateway':'./cmd/gateway','mock':'./cmd/mock-provider','seed':'./eval/project-load-2026-11-02/seed','load':'./eval/project-load-2026-11-02/trace-load','load_plain':'./eval/project-load-2026-11-02/load'}
  buildenv=os.environ.copy();buildenv['GOPROXY']='off';buildenv['GOTOOLCHAIN']='local';buildenv['SDKROOT']='/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk'
  for name,pkg in builds.items():
   dest=bindir/name; run(['go','build','-o',str(dest),pkg],cwd=ROOT,env=buildenv,timeout=180,stdout=subprocess.DEVNULL,stderr=open(OUT/(name+'-build.log'),'w'));binaries[name]=str(dest)
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
  seedenv=env(TEST_MYSQL_DSN=base['MYSQL_DSN'],TEST_REDIS_ADDR=base['REDIS_ADDR'],TEST_MOCK_PORT=ports['mock'],TEST_TENANTS=64);seed=run([binaries['seed']],cwd=ROOT,env=seedenv,capture_output=True,timeout=300);(OUT/'seed.log').write_text(seed.stdout+seed.stderr)
  router=start('router',[binaries['router']],env(ROUTER_PORT=ports['router']));wait_http(f'http://127.0.0.1:{ports["router"]}/healthz',router)
  recon=start('reconciler',[binaries['reconciler'],'-interval','5','-grace','30','-no-rebuild'],env(RECONCILER_PORT=ports['reconciler'],BILLING_ADDR=f'127.0.0.1:{ports["billing"]}'));wait_http(f'http://127.0.0.1:{ports["reconciler"]}/healthz',recon)
  gw=start('gateway',[binaries['gateway']],env(GATEWAY_PORT=ports['gateway'],BILLING_ADDR=f'127.0.0.1:{ports["billing"]}',BILLING_ADMIN_ADDR=f'http://127.0.0.1:{ports["admin"]}',ROUTER_ADDR=f'http://127.0.0.1:{ports["router"]}',AGENT_ADDR='http://127.0.0.1:1',RECONCILER_ADDR=f'http://127.0.0.1:{ports["reconciler"]}',RATE_LIMIT_PER_MIN=100000));wait_http(f'http://127.0.0.1:{ports["gateway"]}/metrics',gw)
  config={'head':run(['git','rev-parse','HEAD'],cwd=ROOT,capture_output=True).stdout.strip(),'branch':run(['git','branch','--show-current'],cwd=ROOT,capture_output=True).stdout.strip(),'git_status':run(['git','status','--short'],cwd=ROOT,capture_output=True).stdout,'source_sha256':{f:hashlib.sha256((ROOT/f).read_bytes()).hexdigest() for f in ['internal/billing/store/mysql.go','internal/gateway/handler/chat.go','internal/gateway/middleware/apikey.go','internal/gateway/middleware/idempotency.go','internal/gateway/middleware/ratelimit.go','internal/pkg/redisx/redisx.go','cmd/gateway/main.go','cmd/router/main.go','cmd/billing/main.go','cmd/mock-provider/main.go']},'images':{myimage:docker_image(myimage),reimage:docker_image(reimage)},'gomaxprocs':8,'mysql_pool_max':100,'rate_per_min':100000,'mock_latency_ms':15,'mock_completion_tokens':100,'kafka':'disabled','isolated_agents':'disabled','reconciler':'sweep every 5s, grace 30s (test only); rebuild disabled','ports':ports,'mysql_port':mp,'redis_port':rp,'quick':quick,'started':datetime.datetime.now().isoformat(),'go_version':run(['go','version'],cwd=ROOT,capture_output=True).stdout.strip(),'sdkroot':'/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk','binary_sha256':{k:hashlib.sha256(pathlib.Path(v).read_bytes()).hexdigest() for k,v in binaries.items()}}
  (OUT/'config.json').write_text(json.dumps(config,ensure_ascii=False,indent=2))
  config['fault']='docker pause owned Redis while idle, requests with and without idempotency key; unpause after drains';config['tenants']=64
  (OUT/'config.json').write_text(json.dumps(config,ensure_ascii=False,indent=2))
  def audit(label):
   p=run([binaries['seed']],cwd=ROOT,env=dict(seedenv,TEST_MODE='audit'),capture_output=True,timeout=120)
   (OUT/f'audit-{label}.json').write_text(p.stdout)
  def phase(label,keyed):
   args=[binaries['load' if keyed else 'load_plain'],'-url',f'http://127.0.0.1:{ports["gateway"]}','-workers','16','-tenants','16','-seconds','3']
   trace=OUT/f'trace-{label}.csv'
   if keyed:args += ['-trace-file',str(trace)]
   p=run(args,cwd=ROOT,env=base,capture_output=True,timeout=120)
   (OUT/f'result-{label}.json').write_text(p.stdout)
   (OUT/f'load-{label}.log').write_text(p.stderr)
   print(label,p.stdout.strip(),flush=True)
   if keyed:
    q=run([binaries['seed']],cwd=ROOT,env=dict(seedenv,TEST_MODE='trace',TEST_TRACE_PATH=str(trace)),capture_output=True,timeout=120)
    (OUT/f'trace-audit-{label}.json').write_text(q.stdout)
  phase('before-key',True);phase('before-plain',False);audit('before')
  traces=[]
  for cycle in (1,2):
   run(['docker','pause',REDIS],capture_output=True,timeout=30)
   (OUT/f'pause-{cycle}.txt').write_text(datetime.datetime.now().isoformat())
   try:
    phase(f'paused-key-{cycle}',True);traces.append(f'paused-key-{cycle}')
    phase(f'paused-plain-{cycle}',False)
   finally:
    run(['docker','unpause',REDIS],capture_output=True,timeout=30)
    (OUT/f'unpause-{cycle}.txt').write_text(datetime.datetime.now().isoformat())
   with (OUT/f'trace-paused-key-{cycle}.csv').open() as source:
    failed=[x['request_id'] for x in csv.DictReader(source) if x['status']=='500']
   count=0
   if failed:
    output=run(['docker','exec',REDIS,'redis-cli','--raw','EXISTS',*failed],capture_output=True,timeout=30)
    count=int(output.stdout.strip())
   (OUT/f'idempotency-snapshot-{cycle}.json').write_text(json.dumps({'at':datetime.datetime.now().isoformat(),'failed_requests':len(failed),'present_keys':count,'snapshot_only':True}))
   # Retry exactly one failed synthetic request whose Redis key is present.
   retry=None
   for request_id in failed:
    present=run(['docker','exec',REDIS,'redis-cli','--raw','EXISTS',request_id],capture_output=True,timeout=30)
    if int(present.stdout.strip())!=1:continue
    key=request_id.removeprefix('idem:');worker=int(key.rsplit('-',2)[1])
    payload=json.dumps({'model':'test-model','messages':[{'role':'user','content':'hello load'}],'max_tokens':100,'stream':False}).encode()
    req=urllib.request.Request(f'http://127.0.0.1:{ports["gateway"]}/v1/chat/completions',data=payload,headers={'Content-Type':'application/json','Authorization':f'Bearer sk-isolated-{worker%16+1:03d}','Idempotency-Key':key},method='POST')
    try:
     with urllib.request.urlopen(req,timeout=15) as response:code=response.status;response_body=response.read().decode()
    except urllib.error.HTTPError as exc:code=exc.code;response_body=exc.read().decode()
    retry={'synthetic_request_id':request_id,'retry_status':code,'retry_response':response_body};break
   (OUT/f'retry-paused-key-{cycle}.json').write_text(json.dumps(retry,ensure_ascii=False,indent=2))
   phase(f'after-key-{cycle}',True);traces.append(f'after-key-{cycle}')
   phase(f'after-plain-{cycle}',False);audit(f'after-{cycle}')
  # Pause during requests; phase timing is captured but does not identify the exact server step.
  for cycle in (() if '--no-inflight' in sys.argv else (1,2)):
   label=f'inflight-{cycle}';trace=OUT/f'trace-{label}.csv'
   args=[binaries['load'],'-url',f'http://127.0.0.1:{ports["gateway"]}','-workers','64','-tenants','16','-seconds','4','-trace-file',str(trace)]
   starttime=time.monotonic();p=subprocess.Popen(args,cwd=ROOT,env=base,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,start_new_session=True);processes.append((label,p))
   time.sleep(.15)
   run(['docker','pause',REDIS],capture_output=True,timeout=30);pause_at=time.monotonic()-starttime
   try:time.sleep(3)
   finally:
    run(['docker','unpause',REDIS],capture_output=True,timeout=30);unpause_at=time.monotonic()-starttime
   stdout,stderr=p.communicate(timeout=120)
   (OUT/f'timing-{label}.json').write_text(json.dumps({'pause_after_start_seconds':pause_at,'unpause_after_start_seconds':unpause_at,'load_exit':p.returncode}))
   (OUT/f'load-{label}.log').write_text(stderr)
   if p.returncode!=0:raise RuntimeError(f'{label} load exit {p.returncode}')
   (OUT/f'result-{label}.json').write_text(stdout);print(label,stdout.strip(),flush=True)
   q=run([binaries['seed']],cwd=ROOT,env=dict(seedenv,TEST_MODE='trace',TEST_TRACE_PATH=str(trace)),capture_output=True,timeout=120)
   (OUT/f'trace-audit-{label}.json').write_text(q.stdout);traces.append(label)
   audit(f'after-{label}')
  time.sleep(40)
  audit('reconciled')
  for label in traces:
   q=run([binaries['seed']],cwd=ROOT,env=dict(seedenv,TEST_MODE='trace',TEST_TRACE_PATH=str(OUT/f'trace-{label}.csv')),capture_output=True,timeout=120)
   (OUT/f'trace-reconciled-{label}.json').write_text(q.stdout)
  (OUT/'completed').write_text('completed\n')
 finally:
  cleanup()
  import shutil;shutil.rmtree(bindir,ignore_errors=True)
if __name__=='__main__':
 try: main()
 except Exception as e:
  (OUT/'error.txt').write_text(repr(e)+'\n');print(f'run failed: {e}; raw: {OUT}',file=sys.stderr);raise
