#!/usr/bin/env python3
"""Isolated local end-to-end load harness. Never touches existing services."""
import csv, datetime, hashlib, json, os, pathlib, signal, socket, subprocess, sys, tempfile, time, urllib.request, uuid
ROOT=pathlib.Path(__file__).resolve().parents[2]
OUT=ROOT/'eval/project-load-2026-11-02/gateway-open-load-runs'/datetime.datetime.now().strftime('%Y%m%d-%H%M%S')
OUT.mkdir(parents=True,exist_ok=False)
IDENT='project-gateway-open-'+uuid.uuid4().hex[:12]
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
 mock_failure=int(os.environ.get('OPEN_LOAD_FAILURE_PERCENT','0'))
 grace=int(os.environ.get('OPEN_LOAD_GRACE','600'))
 if not 0 <= mock_failure <= 100 or not 1 <= grace <= 600:raise ValueError('invalid mock failure or grace')
 hold_unknown=os.environ.get('OPEN_LOAD_HOLD_UNKNOWN','0')
 if hold_unknown not in ('0','1'):raise ValueError('invalid hold mode')
 rates=[int(x) for x in os.environ.get('OPEN_LOAD_RATES','40,100,160').split(',')]
 duration=int(os.environ.get('OPEN_LOAD_SECONDS','2' if quick else '10'))
 repetitions=int(os.environ.get('OPEN_LOAD_REPETITIONS','1' if quick else '2'))
 inflight=int(os.environ.get('OPEN_LOAD_MAX_INFLIGHT','1000'))
 router_candidate=os.environ.get('OPEN_LOAD_ROUTER_BATCH','0')
 router_lock_scope=os.environ.get('OPEN_LOAD_ROUTER_LOCK_SCOPE','0')
 trace_mock=os.environ.get('OPEN_LOAD_TRACE_MOCK','0')
 unknown_store=os.environ.get('OPEN_LOAD_UNKNOWN_STORE','0')
 recovery_wait=int(os.environ.get('OPEN_LOAD_RECOVERY_WAIT','0'))
 if router_candidate not in ('0','1') or unknown_store not in ('0','1') or router_lock_scope not in ('0','1') or trace_mock not in ('0','1') or (router_candidate=='1' and router_lock_scope=='1'):raise ValueError('invalid candidate selection')
 if not rates or any(x<1 or x>1000 for x in rates) or duration<1 or duration>120 or repetitions<1 or repetitions>5 or inflight<1 or inflight>3000 or recovery_wait<0 or recovery_wait>660:raise ValueError('invalid bounded load configuration')
 bindir=pathlib.Path(tempfile.mkdtemp(prefix='codex-project-load-'))
 try:
  binaries={}
  builds={'billing':'./cmd/billing','router':'./cmd/router','reconciler':'./cmd/reconciler','gateway':'./cmd/gateway','mock':'./cmd/mock-provider','seed':'./eval/project-load-2026-11-02/seed','load':'./eval/project-load-2026-11-02/open-load','inject':'./eval/project-load-2026-11-02/pending-fixture'}
  buildenv=os.environ.copy();buildenv['GOPROXY']='off';buildenv['GOTOOLCHAIN']='local';buildenv['SDKROOT']='/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk'
  # Local-only diagnostic overlay logs error classes and elapsed time, no keys or bodies.
  handler=(ROOT/'internal/gateway/handler/chat.go').read_text()
  handler=handler.replace('"context"','"context"\n "errors"',1)
  if trace_mock=='1':
   header='uReq.Header.Set("Content-Type", "application/json")'
   if handler.count(header)!=1:raise ValueError('gateway trace insertion ambiguous')
   handler=handler.replace(header,header+'\n\tuReq.Header.Set("X-Comparison-Request-ID", c.GetString("request_id"))',1)
  handler=handler.replace('prov, err := r.Route(c.Request.Context(), req.Model)','diagRouteStart := time.Now()\n\t\tprov, err := r.Route(c.Request.Context(), req.Model)')
  handler=handler.replace('if err != nil {\n\t\t\tabort(c, http.StatusServiceUnavailable, "routing unavailable")','if err != nil {\n\t\t\tlog.Error("DIAG route", "deadline", errors.Is(err,context.DeadlineExceeded), "elapsed_ms", time.Since(diagRouteStart).Milliseconds())\n\t\t\tabort(c, http.StatusServiceUnavailable, "routing unavailable")')
  handler=handler.replace('rc, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)','diagStart := time.Now()\n\t\trc, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)')
  handler=handler.replace('if err != nil {\n\t\t\t// 目录里没有', 'if err != nil {\n\t\t\tlog.Error("DIAG reserve", "grpc_code", status.Code(err).String(), "elapsed_ms", time.Since(diagStart).Milliseconds())\n\t\t\t// 目录里没有')
  auth=(ROOT/'internal/gateway/middleware/apikey.go').read_text().replace('"context"','"context"\n "log"\n "google.golang.org/grpc/status"',1)
  auth=auth.replace('resp, err := b.ValidateAPIKey(ctx, hash)','diagStart := time.Now()\n\t\tresp, err := b.ValidateAPIKey(ctx, hash)')
  auth=auth.replace('if err != nil || resp == nil {','if err != nil || resp == nil {\n            log.Printf("DIAG auth code=%s elapsed_ms=%d", status.Code(err).String(),time.Since(diagStart).Milliseconds())')
  replace={}
  for name,content in [('internal/gateway/handler/chat.go',handler),('internal/gateway/middleware/apikey.go',auth)]:
   path=bindir/(pathlib.Path(name).name+'.go');path.write_text(content);replace[str(ROOT/name)]=str(path)
  overlay=bindir/'diagnostic-overlay.json';overlay.write_text(json.dumps({'Replace':replace}))
  mock_overlay=bindir/'mock-overlay.json'
  if trace_mock=='1':
   mock_source=(ROOT/'cmd/mock-provider/main.go').read_text()
   marker='mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {'
   if mock_source.count(marker)!=1:raise ValueError('mock trace insertion ambiguous')
   mock_source=mock_source.replace(marker,marker+'\n\t\tfmt.Fprintf(os.Stderr, "TRACE_REQUEST_ID %s\\n", r.Header.Get("X-Comparison-Request-ID"))',1)
   mock_path=bindir/'mock-trace.go';mock_path.write_text(mock_source)
   mock_overlay.write_text(json.dumps({'Replace':{str(ROOT/'cmd/mock-provider/main.go'):str(mock_path)}}))
  billing_overlay=bindir/'billing-overlay.json'
  billing_source=ROOT/'eval/project-load-2026-11-02/billing_unknown_candidate.go.txt'
  if unknown_store=='1':billing_overlay.write_text(json.dumps({'Replace':{str(ROOT/'internal/billing/store/mysql.go'):str(billing_source)}}))
  router_overlay=bindir/'router-overlay.json'
  router_source=ROOT/'eval/project-load-2026-11-02/router_registry_batch_candidate.go.txt'
  if router_lock_scope=='1':router_source=ROOT/'eval/project-load-2026-11-02/router_registry_lock_scope_candidate.go.txt'
  if router_candidate=='1' or router_lock_scope=='1':router_overlay.write_text(json.dumps({'Replace':{str(ROOT/'internal/router/registry.go'):str(router_source)}}))
  (OUT/'overlay-sha256.json').write_text(json.dumps({k:hashlib.sha256(pathlib.Path(v).read_bytes()).hexdigest() for k,v in replace.items()},indent=2))
  for name,pkg in builds.items():
   dest=bindir/name; args=['go','build']+(['-overlay',str(overlay)] if name=='gateway' else (['-overlay',str(router_overlay)] if name=='router' and (router_candidate=='1' or router_lock_scope=='1') else (['-overlay',str(billing_overlay)] if name=='billing' and unknown_store=='1' else (['-overlay',str(mock_overlay)] if name=='mock' and trace_mock=='1' else []))))+['-o',str(dest),pkg]; run(args,cwd=ROOT,env=buildenv,timeout=180,stdout=subprocess.DEVNULL,stderr=open(OUT/(name+'-build.log'),'w'));binaries[name]=str(dest)
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
  mock=start('mock',[binaries['mock']],env(MOCK_PROVIDER_PORT=ports['mock'],MOCK_PROVIDER_NAME='isolated',MOCK_FAIL_RATE=mock_failure,MOCK_LATENCY_MS=15,MOCK_COMPLETION_TOKENS=100));wait_http(f'http://127.0.0.1:{ports["mock"]}/healthz',mock)
  bill=start('billing',[binaries['billing']],env(BILLING_PORT=ports['billing'],BILLING_ADMIN_PORT=ports['admin'],SUB_RENEW_INTERVAL_SEC=60));wait_tcp(ports['billing'],bill)
  seedenv=env(TEST_MYSQL_DSN=base['MYSQL_DSN'],TEST_REDIS_ADDR=base['REDIS_ADDR'],TEST_MOCK_PORT=ports['mock'],TEST_TENANTS=64);seed=run([binaries['seed']],cwd=ROOT,env=seedenv,capture_output=True,timeout=300);(OUT/'seed.log').write_text(seed.stdout+seed.stderr)
  router=start('router',[binaries['router']],env(ROUTER_PORT=ports['router']));wait_http(f'http://127.0.0.1:{ports["router"]}/healthz',router)
  recon=start('reconciler',[binaries['reconciler'],'-interval','5','-grace',str(grace),'-no-rebuild']+(['-no-auto-reverse'] if hold_unknown=='1' else []),env(RECONCILER_PORT=ports['reconciler'],BILLING_ADDR=f'127.0.0.1:{ports["billing"]}'));wait_http(f'http://127.0.0.1:{ports["reconciler"]}/healthz',recon)
  gw=start('gateway',[binaries['gateway']],env(GATEWAY_PORT=ports['gateway'],BILLING_ADDR=f'127.0.0.1:{ports["billing"]}',BILLING_ADMIN_ADDR=f'http://127.0.0.1:{ports["admin"]}',ROUTER_ADDR=f'http://127.0.0.1:{ports["router"]}',AGENT_ADDR='http://127.0.0.1:1',RECONCILER_ADDR=f'http://127.0.0.1:{ports["reconciler"]}',RATE_LIMIT_PER_MIN=600,GATEWAY_HOLD_UNKNOWN=hold_unknown));wait_http(f'http://127.0.0.1:{ports["gateway"]}/metrics',gw)
  config={'head':run(['git','rev-parse','HEAD'],cwd=ROOT,capture_output=True).stdout.strip(),'branch':run(['git','branch','--show-current'],cwd=ROOT,capture_output=True).stdout.strip(),'git_status':run(['git','status','--short'],cwd=ROOT,capture_output=True).stdout,'source_sha256':{f:hashlib.sha256((ROOT/f).read_bytes()).hexdigest() for f in ['internal/billing/store/mysql.go','internal/billing/server/service.go','internal/gateway/client/billing.go','internal/gateway/handler/chat.go','internal/gateway/middleware/apikey.go','cmd/gateway/main.go','cmd/router/main.go','cmd/billing/main.go','cmd/mock-provider/main.go','eval/project-load-2026-11-02/pending-fixture/main.go']},'images':{myimage:docker_image(myimage),reimage:docker_image(reimage)},'gomaxprocs':8,'mysql_pool_max':100,'rate_per_min':600,'mock_latency_ms':15,'mock_completion_tokens':100,'kafka':'disabled','isolated_agents':'disabled','reconciler':'sweep every 5s, grace 600s; rebuild disabled','ports':ports,'mysql_port':mp,'redis_port':rp,'quick':quick,'started':datetime.datetime.now().isoformat(),'go_version':run(['go','version'],cwd=ROOT,capture_output=True).stdout.strip(),'sdkroot':'/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk','binary_sha256':{k:hashlib.sha256(pathlib.Path(v).read_bytes()).hexdigest() for k,v in binaries.items()}}
  config.update({'offered_rates':rates,'duration_seconds':duration,'repetitions':repetitions,'max_inflight':inflight,'recovery_wait_seconds':recovery_wait,'router_candidate':'unsafe_batch_test_only' if router_candidate=='1' else ('lock_scope_test_only' if router_lock_scope=='1' else 'current'),'router_candidate_sha256':hashlib.sha256(router_source.read_bytes()).hexdigest() if (router_candidate=='1' or router_lock_scope=='1') else None,'load_source_sha256':hashlib.sha256((ROOT/'eval/project-load-2026-11-02/open-load/main.go').read_bytes()).hexdigest(),'runner_sha256':hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest()})
  config.update({'mock_failure_percent':mock_failure,'grace_seconds':grace})
  config.update({'hold_unknown': hold_unknown=='1', 'reconciler_no_auto_reverse': hold_unknown=='1'})
  config.update({'trace_mock': trace_mock=='1','trace_mock_source_sha256': hashlib.sha256(mock_path.read_bytes()).hexdigest() if trace_mock=='1' else None})
  config.update({'billing_candidate':'unknown_all_test_only' if unknown_store=='1' else 'current','billing_candidate_sha256':hashlib.sha256(billing_source.read_bytes()).hexdigest() if unknown_store=='1' else None})
  (OUT/'config.json').write_text(json.dumps(config,ensure_ascii=False,indent=2))
  # A failure-mode run cannot require an all-success smoke request.
  cumulative_success=0
  scenarios=[(False,64,rate) for rate in rates]
  with open(OUT/'results.jsonl','w') as results,open(OUT/'resources.jsonl','w') as resources,open(OUT/'audits.jsonl','w') as audits:
   for stream,tenants,workers in scenarios:
    for rep in range(1,repetitions+1):
     args=[binaries['load'],'-url',f'http://127.0.0.1:{ports["gateway"]}','-workers',str(inflight),'-rate',str(workers),'-tenants',str(tenants),'-seconds',str(duration)]
     trace_path=OUT/f'trace-{tenants}-{workers}-{rep}.csv';args.extend(['-trace-file',str(trace_path)])
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
     traced=run([binaries['seed']],cwd=ROOT,env=dict(seedenv,TEST_MODE='trace',TEST_TRACE_PATH=str(trace_path)),capture_output=True,timeout=90);(OUT/f'trace-audit-{tenants}-{workers}-{rep}.json').write_text(traced.stdout)
     audit=run([binaries['seed']],cwd=ROOT,env=dict(seedenv,TEST_MODE='audit'),capture_output=True,timeout=90);ar=json.loads(audit.stdout);ar['cumulative_valid_http']=cumulative_success;ar['settled_minus_valid_http']=ar['settled']-cumulative_success;ar.update({'workers':workers,'tenants':tenants,'stream':stream,'rep':rep});audits.write(json.dumps(ar)+'\n');audits.flush()
     print(json.dumps({'result':row,'audit':ar},ensure_ascii=False),flush=True)
  sql=run(['docker','exec',MYSQL,'mysql','-uroot','-pisolated','-N','-e',"SHOW GLOBAL STATUS WHERE Variable_name IN ('Max_used_connections','Threads_connected','Threads_running','Connection_errors_max_connections','Innodb_row_lock_waits','Innodb_row_lock_time'); SHOW VARIABLES LIKE 'max_connections'"],capture_output=True,timeout=30);(OUT/'mysql-final.txt').write_text(sql.stdout+sql.stderr)
  redis=run(['docker','exec',REDIS,'redis-cli','INFO'],capture_output=True,timeout=30);(OUT/'redis-final.txt').write_text(redis.stdout)
  final=run([binaries['seed']],cwd=ROOT,env=dict(seedenv,TEST_MODE='audit'),capture_output=True,timeout=120);(OUT/'audit-before-wait.json').write_text(final.stdout)
  if recovery_wait:
   started=time.monotonic()
   while time.monotonic()-started<recovery_wait:
    for name,proc in processes:
     if proc.poll() is not None:raise RuntimeError(f'{name} exited during recovery wait: {proc.returncode}')
    time.sleep(max(0,min(5,recovery_wait-(time.monotonic()-started))))
   (OUT/'recovery-wait.json').write_text(json.dumps({'requested_seconds':recovery_wait,'elapsed_seconds':time.monotonic()-started}))
   for trace in sorted(OUT.glob('trace-*.csv')):
    traced=run([binaries['seed']],cwd=ROOT,env=dict(seedenv,TEST_MODE='trace',TEST_TRACE_PATH=str(trace)),capture_output=True,timeout=120)
    (OUT/('recovered-'+trace.stem+'.json')).write_text(traced.stdout)
  final=run([binaries['seed']],cwd=ROOT,env=dict(seedenv,TEST_MODE='audit'),capture_output=True,timeout=120);(OUT/'audit-final.json').write_text(final.stdout)
  (OUT/'completed').write_text('completed\n')
 finally:
  cleanup()
  import shutil;shutil.rmtree(bindir,ignore_errors=True)
if __name__=='__main__':
 signal.signal(signal.SIGTERM, lambda *_: (_ for _ in ()).throw(SystemExit(143)))
 try: main()
 except Exception as e:
  (OUT/'error.txt').write_text(repr(e)+'\n');print(f'run failed: {e}; raw: {OUT}',file=sys.stderr);raise
