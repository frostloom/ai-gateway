#!/usr/bin/env python3
"""Isolated local end-to-end load harness. Never touches existing services."""
import concurrent.futures, datetime, hashlib, json, os, pathlib, signal, socket, subprocess, sys, tempfile, time, urllib.request, urllib.error, uuid
ROOT=pathlib.Path(__file__).resolve().parents[2]
OUT=ROOT/'eval/project-load-2026-11-02/stale-sweep-candidate-runs'/datetime.datetime.now().strftime('%Y%m%d-%H%M%S')
OUT.mkdir(parents=True,exist_ok=False)
IDENT='project-stale-candidate-'+uuid.uuid4().hex[:12]
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
  builds={'billing':'./cmd/billing','router':'./cmd/router','reconciler':'./cmd/reconciler','gateway':'./cmd/gateway','mock':'./eval/project-load-2026-11-02/idem-mock','seed':'./eval/project-load-2026-11-02/seed','load':'./eval/project-load-2026-11-02/trace-load','stale':'./eval/project-load-2026-11-02/stale-sweep'}
  buildenv=os.environ.copy();buildenv['GOPROXY']='off';buildenv['GOTOOLCHAIN']='local';buildenv['SDKROOT']='/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk'
  candidate=(ROOT/'eval/project-load-2026-11-02/usage_reverse_candidate.go.txt').read_text()
  replacement=bindir/'mysql.go';replacement.write_text(candidate)
  overlay=bindir/'overlay.json';overlay.write_text(json.dumps({'Replace':{str(ROOT/'internal/billing/store/mysql.go'):str(replacement)}}))
  (OUT/'candidate-sha256.txt').write_text(hashlib.sha256(candidate.encode()).hexdigest()+'\n')
  for name,pkg in builds.items():
   dest=bindir/name; args=['go','build']+(['-overlay',str(overlay)] if name=='billing' else [])+['-o',str(dest),pkg]; run(args,cwd=ROOT,env=buildenv,timeout=180,stdout=subprocess.DEVNULL,stderr=open(OUT/(name+'-build.log'),'w'));binaries[name]=str(dest)
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
  seedenv=env(TEST_MYSQL_DSN=base['MYSQL_DSN'],TEST_REDIS_ADDR=base['REDIS_ADDR'],TEST_MOCK_PORT=ports['mock'],TEST_TENANTS=1);seed=run([binaries['seed']],cwd=ROOT,env=seedenv,capture_output=True,timeout=300);(OUT/'seed.log').write_text(seed.stdout+seed.stderr)
  config={'head':run(['git','rev-parse','HEAD'],cwd=ROOT,capture_output=True).stdout.strip(),'branch':run(['git','branch','--show-current'],cwd=ROOT,capture_output=True).stdout.strip(),'sources':{f:hashlib.sha256((ROOT/f).read_bytes()).hexdigest() for f in ['internal/billing/store/mysql.go','internal/billing/reconciler/reconciler.go','internal/billing/server/service.go','eval/project-load-2026-11-02/stale-sweep/main.go']},'binaries':{k:hashlib.sha256(pathlib.Path(v).read_bytes()).hexdigest() for k,v in binaries.items()},'images':{myimage:docker_image(myimage),reimage:docker_image(reimage)},'grace_s':0,'note':'single controlled scan; test-only interposition on Reverse; no upstream'}
  (OUT/'config.json').write_text(json.dumps(config,indent=2))
  experiment=run([binaries['stale']],cwd=ROOT,env=env(TEST_MYSQL_DSN=base['MYSQL_DSN'],TEST_BILLING_ADDR=f'127.0.0.1:{ports["billing"]}',TEST_SECOND_SWEEP='1'),capture_output=True,timeout=45)
  (OUT/'observed.json').write_text(experiment.stdout);(OUT/'stale-stderr.txt').write_text(experiment.stderr)
  x=json.loads(experiment.stdout); request_id=x['request_id']
  sql=run(['docker','exec',MYSQL,'mysql','-uroot','-pisolated','-N','project_load','-e',"SELECT phase,status,COUNT(*) FROM bills WHERE request_id='"+request_id+"' GROUP BY phase,status ORDER BY phase; SELECT event_key FROM outbox_events WHERE event_key LIKE '"+request_id+":%' ORDER BY event_key;"],capture_output=True,timeout=30);(OUT/'rows.txt').write_text(sql.stdout)
  audit=run([binaries['seed']],cwd=ROOT,env=dict(seedenv,TEST_MODE='audit'),capture_output=True,timeout=120);(OUT/'audit.json').write_text(audit.stdout)
  print(experiment.stdout,flush=True)
  (OUT/'completed').write_text('completed\n')
 finally:
  cleanup()
  import shutil;shutil.rmtree(bindir,ignore_errors=True)
if __name__=='__main__':
 signal.signal(signal.SIGTERM, lambda *_: (_ for _ in ()).throw(SystemExit(143)))
 try: main()
 except Exception as e:
  (OUT/'error.txt').write_text(repr(e)+'\n');print(f'run failed: {e}; raw: {OUT}',file=sys.stderr);raise
