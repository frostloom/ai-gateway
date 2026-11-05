#!/usr/bin/env python3
"""Isolated local end-to-end load harness. Never touches existing services."""
import datetime, hashlib, json, os, pathlib, signal, socket, subprocess, sys, tempfile, time, urllib.request, uuid
ROOT=pathlib.Path(__file__).resolve().parents[2]
OUT=ROOT/'eval/project-load-2026-11-02/replay-perf-runs'/datetime.datetime.now().strftime('%Y%m%d-%H%M%S')
OUT.mkdir(parents=True,exist_ok=False)
IDENT='project-replay-perf-'+uuid.uuid4().hex[:12]
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
 bindir=pathlib.Path(tempfile.mkdtemp(prefix='codex-replay-perf-'))
 try:
  # Baseline removes only replay negotiation; all other current fixes stay.
  original={}
  for f in ['internal/billing/store/mysql.go','internal/billing/server/service.go','internal/gateway/client/billing.go']:
   original[f]=(ROOT/f).read_text()
  baseline=dict(original)
  source=baseline['internal/billing/store/mysql.go']
  start=source.index('// ReserveWithReplaySignal preserves');end=source.index('func (s *Store) Reserve(ctx',start)
  source=source[:start]+source[end:];source=source.replace('reserveReplayCode(ctx)','billingv1.ReserveCode_RESERVE_OK')
  baseline['internal/billing/store/mysql.go']=source
  source=baseline['internal/billing/server/service.go']
  source=source.replace('\t"google.golang.org/grpc"\n','').replace('\t"google.golang.org/grpc/metadata"\n','').replace('\t"github.com/frostloom/ai-gateway/internal/pkg/controlplane"\n','')
  start=source.index('\tmd, _ := metadata.FromIncomingContext(ctx)',source.index('func (s *Service) Reserve('));end=source.index('\n\tif err != nil {\n\t\treturn nil, err\n\t}\n\ts.log.Info',start)
  source=source[:start]+'\tresp, err := s.store.Reserve(ctx, req)'+source[end:]
  baseline['internal/billing/server/service.go']=source
  source=baseline['internal/gateway/client/billing.go']
  for imp in ('\t"google.golang.org/grpc/codes"\n','\t"google.golang.org/grpc/metadata"\n','\t"google.golang.org/grpc/status"\n'):source=source.replace(imp,'')
  start=source.index('func (b *Billing) Reserve(');end=source.index('func (b *Billing) ReportUsage(',start)
  source=source[:start]+'''func (b *Billing) Reserve(ctx context.Context, req *billingv1.ReserveRequest) (*billingv1.ReserveResponse, error) {
 return b.cli.Reserve(ctx, req)
}

'''+source[end:];baseline['internal/gateway/client/billing.go']=source
  replace={}
  for idx,(name,content) in enumerate(baseline.items()):
   path=bindir/f'baseline-{idx}.go';path.write_text(content);replace[str(ROOT/name)]=str(path)
  overlay=bindir/'overlay.json';overlay.write_text(json.dumps({'Replace':replace}))
  env_build=os.environ.copy();env_build.update({'GOPROXY':'off','GOTOOLCHAIN':'local','SDKROOT':'/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk'})
  binaries={}
  jobs={'billing_old':('./cmd/billing',True),'gateway_old':('./cmd/gateway',True),'billing_new':('./cmd/billing',False),'gateway_new':('./cmd/gateway',False),'router':('./cmd/router',False),'reconciler':('./cmd/reconciler',False),'mock':('./cmd/mock-provider',False),'seed':('./eval/project-load-2026-11-02/seed',False),'load':('./eval/project-load-2026-11-02/perf-load',False)}
  for name,(pkg,use_overlay) in jobs.items():
   dest=bindir/name;args=['go','build'];
   if use_overlay:args+=['-overlay',str(overlay)]
   args+=['-o',str(dest),pkg]
   with (OUT/(name+'-build.log')).open('w') as log:run(args,cwd=ROOT,env=env_build,timeout=240,stdout=subprocess.DEVNULL,stderr=log)
   binaries[name]=str(dest)
  myimage='docker.m.daocloud.io/library/mysql:8.0.40';reimage='docker.m.daocloud.io/library/redis:7-alpine'
  run(['docker','run','-d','--name',MYSQL,'-e','MYSQL_ROOT_PASSWORD=isolated','-e','MYSQL_DATABASE=project_load','-p','127.0.0.1::3306',myimage],stdout=subprocess.DEVNULL)
  run(['docker','run','-d','--name',REDIS,'-p','127.0.0.1::6379',reimage],stdout=subprocess.DEVNULL)
  for _ in range(300):
   if subprocess.run(['docker','exec',MYSQL,'mysqladmin','ping','-h','127.0.0.1','-uroot','-pisolated','--silent'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0:break
   time.sleep(.5)
  else:raise RuntimeError('MySQL readiness timeout')
  mp,rp=port(MYSQL,'3306/tcp'),port(REDIS,'6379/tcp');ports={k:free_port() for k in ['gateway_old','gateway_new','billing_old','billing_new','admin_old','admin_new','router','mock','reconciler']}
  if len(set(ports.values()))!=len(ports):raise RuntimeError('port collision')
  base={'PATH':os.environ.get('PATH','/usr/bin:/bin'),'HOME':os.environ.get('HOME','/tmp'),'TMPDIR':os.environ.get('TMPDIR','/tmp'),'CONTROL_PLANE_TOKEN':'isolated-test-token','MYSQL_DSN':f'root:isolated@tcp(127.0.0.1:{mp})/project_load?charset=utf8mb4&parseTime=true&loc=Local','REDIS_ADDR':f'127.0.0.1:{rp}','LISTEN_HOST':'127.0.0.1','LOG_LEVEL':'error','KAFKA_BROKERS':'','GOMAXPROCS':'8'}
  def env(**kw):return dict(base,**{k:str(v) for k,v in kw.items()})
  mock=start('mock',[binaries['mock']],env(MOCK_PROVIDER_PORT=ports['mock'],MOCK_PROVIDER_NAME='isolated',MOCK_FAIL_RATE=0,MOCK_LATENCY_MS=15,MOCK_COMPLETION_TOKENS=100));wait_http(f'http://127.0.0.1:{ports["mock"]}/healthz',mock)
  for mode in ('old','new'):
   p=start('billing_'+mode,[binaries['billing_'+mode]],env(BILLING_PORT=ports['billing_'+mode],BILLING_ADMIN_PORT=ports['admin_'+mode],SUB_RENEW_INTERVAL_SEC=60));wait_tcp(ports['billing_'+mode],p)
  seedenv=env(TEST_MYSQL_DSN=base['MYSQL_DSN'],TEST_REDIS_ADDR=base['REDIS_ADDR'],TEST_MOCK_PORT=ports['mock'],TEST_TENANTS=768)
  seed=run([binaries['seed']],cwd=ROOT,env=seedenv,capture_output=True,timeout=600);(OUT/'seed.log').write_text(seed.stdout+seed.stderr)
  router=start('router',[binaries['router']],env(ROUTER_PORT=ports['router']));wait_http(f'http://127.0.0.1:{ports["router"]}/healthz',router)
  recon=start('reconciler',[binaries['reconciler'],'-interval','5','-grace','600','-no-rebuild'],env(RECONCILER_PORT=ports['reconciler'],BILLING_ADDR=f'127.0.0.1:{ports["billing_new"]}'));wait_http(f'http://127.0.0.1:{ports["reconciler"]}/healthz',recon)
  for mode in ('old','new'):
   p=start('gateway_'+mode,[binaries['gateway_'+mode]],env(GATEWAY_PORT=ports['gateway_'+mode],BILLING_ADDR=f'127.0.0.1:{ports["billing_"+mode]}',BILLING_ADMIN_ADDR=f'http://127.0.0.1:{ports["admin_"+mode]}',ROUTER_ADDR=f'http://127.0.0.1:{ports["router"]}',AGENT_ADDR='http://127.0.0.1:1',RECONCILER_ADDR=f'http://127.0.0.1:{ports["reconciler"]}',RATE_LIMIT_PER_MIN=600));wait_http(f'http://127.0.0.1:{ports["gateway_"+mode]}/metrics',p)
  config={'head':run(['git','rev-parse','HEAD'],cwd=ROOT,capture_output=True).stdout.strip(),'branch':run(['git','branch','--show-current'],cwd=ROOT,capture_output=True).stdout.strip(),'git_status':run(['git','status','--short'],cwd=ROOT,capture_output=True).stdout,'current_source_sha256':{f:hashlib.sha256(content.encode()).hexdigest() for f,content in original.items()},'baseline_overlay_sha256':{f:hashlib.sha256(content.encode()).hexdigest() for f,content in baseline.items()},'binary_sha256':{k:hashlib.sha256(pathlib.Path(v).read_bytes()).hexdigest() for k,v in binaries.items()},'images':{myimage:docker_image(myimage),reimage:docker_image(reimage)},'go_version':run(['go','version'],cwd=ROOT,capture_output=True).stdout.strip(),'gomaxprocs':8,'mysql_pool_max_per_billing':100,'rate_per_tenant_per_min':600,'mock_latency_ms':15,'redis_port':rp,'ports':ports,'tenants':768,'kafka':'disabled','reconciler_grace_s':600,'note':'both billing and gateway instances remain running; separate tenant group per phase; legacy mode lacks duplicate safety','started':datetime.datetime.now().isoformat()}
  (OUT/'config.json').write_text(json.dumps(config,ensure_ascii=False,indent=2))
  for mode in ('old','new'):
   args=[binaries['load'],'-url',f'http://127.0.0.1:{ports["gateway_"+mode]}','-workers','1','-tenants','1','-seconds','1','-tenant-offset','767']
   smoke=run(args,cwd=ROOT,env=base,capture_output=True,timeout=30);(OUT/f'smoke-{mode}.json').write_text(smoke.stdout)
   row=json.loads(smoke.stdout)
   if row['valid_success']==0 or row['valid_success']!=row['requests']:raise RuntimeError(f'{mode} smoke failed')
  offset=0
  with (OUT/'results.jsonl').open('w') as results,(OUT/'resources.jsonl').open('w') as resources:
   for rep in (1,2):
    for workers in (16,64,256):
     modes=('old','new') if rep==1 else ('new','old')
     for mode in modes:
      args=[binaries['load'],'-url',f'http://127.0.0.1:{ports["gateway_"+mode]}','-workers',str(workers),'-tenants','64','-tenant-offset',str(offset),'-seconds','3']
      p=subprocess.Popen(args,cwd=ROOT,env=base,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
      while p.poll() is None:
       sample={'time':datetime.datetime.now().isoformat(),'mode':mode,'workers':workers,'rep':rep}
       ids=','.join([str(proc.pid) for _,proc in processes if proc.poll() is None]+[str(p.pid)])
       sample['ps']=subprocess.run(['ps','-p',ids,'-o','pid=,%cpu=,rss='],capture_output=True,text=True).stdout
       sample['docker']=subprocess.run(['docker','stats','--no-stream','--format','{{json .}}',MYSQL,REDIS],capture_output=True,text=True).stdout
       resources.write(json.dumps(sample)+'\n');resources.flush()
       if p.poll() is None:time.sleep(1)
      stdout,stderr=p.communicate(timeout=20)
      if p.returncode!=0:raise RuntimeError(f'{mode} load exit {p.returncode}: {stderr}')
      row=json.loads(stdout);row.update({'mode':mode,'rep':rep,'tenant_offset':offset});results.write(json.dumps(row)+'\n');results.flush();print(json.dumps(row),flush=True);offset+=64
  audit=run([binaries['seed']],cwd=ROOT,env=dict(seedenv,TEST_MODE='audit'),capture_output=True,timeout=300);(OUT/'audit.json').write_text(audit.stdout)
  (OUT/'completed').write_text('completed\n')
 finally:
  cleanup()
  import shutil;shutil.rmtree(bindir,ignore_errors=True)
if __name__=='__main__':
 try:main()
 except Exception as e:
  (OUT/'error.txt').write_text(repr(e)+'\n');print(f'run failed: {e}; raw: {OUT}',file=sys.stderr);raise
