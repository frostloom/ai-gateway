#!/usr/bin/env python3
"""Isolated local end-to-end load harness. Never touches existing services."""
import datetime, hashlib, json, os, pathlib, signal, socket, subprocess, sys, tempfile, time, urllib.request, uuid
ROOT=pathlib.Path(__file__).resolve().parents[2]
OUT=ROOT/'eval/project-load-2026-11-02/runs'/datetime.datetime.now().strftime('%Y%m%d-%H%M%S')
OUT.mkdir(parents=True,exist_ok=False)
IDENT='project-load-'+uuid.uuid4().hex[:12]
MYSQL=IDENT+'-mysql'; REDIS=IDENT+'-redis'; processes=[]; opened=[]
def run(args,**kw): return subprocess.run(args,check=True,text=True,**kw)
def free_port():
 with socket.socket() as s: s.bind(('127.0.0.1',0)); return s.getsockname()[1]
def port(name,inside): return run(['docker','port',name,inside],capture_output=True).stdout.strip().split(':')[-1]
def wait_http(url,proc):
 for _ in range(120):
  if proc.poll() is not None: raise RuntimeError(f'process exited: {url} code={proc.returncode}')
  try:
   with urllib.request.urlopen(url,timeout=.5) as r:
    if r.status==200:return
  except Exception: time.sleep(.25)
 raise RuntimeError('readiness timeout '+url)
def wait_tcp(n,proc):
 for _ in range(120):
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
  builds={'billing':'./cmd/billing','router':'./cmd/router','reconciler':'./cmd/reconciler','gateway':'./cmd/gateway','mock':'./cmd/mock-provider','seed':'./eval/project-load-2026-11-02/seed','load':'./eval/project-load-2026-11-02/load'}
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
  seedenv=env(TEST_MYSQL_DSN=base['MYSQL_DSN'],TEST_REDIS_ADDR=base['REDIS_ADDR'],TEST_MOCK_PORT=ports['mock']);seed=run([binaries['seed']],cwd=ROOT,env=seedenv,capture_output=True,timeout=30);(OUT/'seed.log').write_text(seed.stdout+seed.stderr)
  router=start('router',[binaries['router']],env(ROUTER_PORT=ports['router']));wait_http(f'http://127.0.0.1:{ports["router"]}/healthz',router)
  recon=start('reconciler',[binaries['reconciler'],'-interval','5','-grace','600','-no-rebuild'],env(RECONCILER_PORT=ports['reconciler'],BILLING_ADDR=f'127.0.0.1:{ports["billing"]}'));wait_http(f'http://127.0.0.1:{ports["reconciler"]}/healthz',recon)
  gw=start('gateway',[binaries['gateway']],env(GATEWAY_PORT=ports['gateway'],BILLING_ADDR=f'127.0.0.1:{ports["billing"]}',BILLING_ADMIN_ADDR=f'http://127.0.0.1:{ports["admin"]}',ROUTER_ADDR=f'http://127.0.0.1:{ports["router"]}',AGENT_ADDR='http://127.0.0.1:1',RECONCILER_ADDR=f'http://127.0.0.1:{ports["reconciler"]}',RATE_LIMIT_PER_MIN=100000));wait_http(f'http://127.0.0.1:{ports["gateway"]}/metrics',gw)
  config={'head':run(['git','rev-parse','HEAD'],cwd=ROOT,capture_output=True).stdout.strip(),'branch':run(['git','branch','--show-current'],cwd=ROOT,capture_output=True).stdout.strip(),'git_status':run(['git','status','--short'],cwd=ROOT,capture_output=True).stdout,'source_sha256':{f:hashlib.sha256((ROOT/f).read_bytes()).hexdigest() for f in ['internal/billing/store/mysql.go','internal/gateway/handler/chat.go','cmd/gateway/main.go','cmd/router/main.go','cmd/billing/main.go','cmd/mock-provider/main.go']},'images':{myimage:docker_image(myimage),reimage:docker_image(reimage)},'gomaxprocs':8,'mysql_pool_max':100,'rate_per_min':100000,'mock_latency_ms':15,'mock_completion_tokens':100,'kafka':'disabled','isolated_agents':'disabled','reconciler':'sweep every 5s, grace 600s; rebuild disabled','ports':ports,'mysql_port':mp,'redis_port':rp,'quick':quick,'started':datetime.datetime.now().isoformat(),'go_version':run(['go','version'],cwd=ROOT,capture_output=True).stdout.strip(),'sdkroot':'/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk','binary_sha256':{k:hashlib.sha256(pathlib.Path(v).read_bytes()).hexdigest() for k,v in binaries.items()}}
  (OUT/'config.json').write_text(json.dumps(config,ensure_ascii=False,indent=2))
  # Smoke actual authenticated request before load; its durable rows remain in audit.
  smoke=subprocess.run([binaries['load'],'-url',f'http://127.0.0.1:{ports["gateway"]}','-workers','1','-seconds','1'],cwd=ROOT,env=base,capture_output=True,text=True,timeout=25,check=True);(OUT/'smoke.json').write_text(smoke.stdout);sm=json.loads(smoke.stdout)
  if sm['valid_success']<1 or sm['valid_success']!=sm['requests']:raise RuntimeError('smoke failed: '+smoke.stdout)
  cumulative_success=sm['valid_success']
  scenarios=([(False,1,1)] if quick else [(False,t,w) for t in (1,8) for w in (1,8,32,64,128,256,512)]+[(True,8,w) for w in (8,32,64,128)])
  with open(OUT/'results.jsonl','w') as results,open(OUT/'resources.jsonl','w') as resources,open(OUT/'audits.jsonl','w') as audits:
   for stream,tenants,workers in scenarios:
    for rep in range(1,2 if quick else 3):
     args=[binaries['load'],'-url',f'http://127.0.0.1:{ports["gateway"]}','-workers',str(workers),'-tenants',str(tenants),'-seconds','1' if quick else '5']
     if stream:args.append('-stream')
     p=subprocess.Popen(args,cwd=ROOT,env=base,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
     while p.poll() is None:
      sample={'time':datetime.datetime.now().isoformat(),'workers':workers,'tenants':tenants,'stream':stream,'rep':rep}
      ids=','.join(str(proc.pid) for _,proc in processes if proc.poll() is None)
      sample['ps']=subprocess.run(['ps','-p',ids,'-o','pid=,%cpu=,rss='],capture_output=True,text=True).stdout
      sample['docker']=subprocess.run(['docker','stats','--no-stream','--format','{{json .}}',MYSQL,REDIS],capture_output=True,text=True).stdout
      resources.write(json.dumps(sample,ensure_ascii=False)+'\n');resources.flush()
      if p.poll() is None:time.sleep(1)
     stdout,stderr=p.communicate(timeout=20)
     if p.returncode!=0:raise RuntimeError(f'load exit {p.returncode}: {stderr}')
     row=json.loads(stdout);row['rep']=rep;cumulative_success+=row['valid_success'];results.write(json.dumps(row)+'\n');results.flush()
     audit=run([binaries['seed']],cwd=ROOT,env=dict(seedenv,TEST_MODE='audit'),capture_output=True,timeout=90);ar=json.loads(audit.stdout);ar['cumulative_valid_http']=cumulative_success;ar['settled_minus_valid_http']=ar['settled']-cumulative_success;ar.update({'workers':workers,'tenants':tenants,'stream':stream,'rep':rep});audits.write(json.dumps(ar)+'\n');audits.flush()
     print(json.dumps({'result':row,'audit':ar},ensure_ascii=False),flush=True)
  (OUT/'completed').write_text('completed\n')
 finally:
  cleanup()
  import shutil;shutil.rmtree(bindir,ignore_errors=True)
if __name__=='__main__':
 try: main()
 except Exception as e:
  (OUT/'error.txt').write_text(repr(e)+'\n');print(f'run failed: {e}; raw: {OUT}',file=sys.stderr);raise
