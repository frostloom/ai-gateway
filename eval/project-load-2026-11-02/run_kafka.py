#!/usr/bin/env python3
"""Isolated local end-to-end load harness. Never touches existing services."""
import datetime, hashlib, json, os, pathlib, signal, socket, subprocess, sys, tempfile, time, urllib.request, uuid
ROOT=pathlib.Path(__file__).resolve().parents[2]
OUT=ROOT/'eval/project-load-2026-11-02/kafka-runs'/datetime.datetime.now().strftime('%Y%m%d-%H%M%S')
OUT.mkdir(parents=True,exist_ok=False)
IDENT='project-kafka-load-'+uuid.uuid4().hex[:12]
MYSQL=IDENT+'-mysql'; REDIS=IDENT+'-redis'; KAFKA=IDENT+'-kafka'; processes=[]; opened=[]
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
 subprocess.run(['docker','rm','-f',MYSQL,REDIS,KAFKA],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
def docker_image(name):
 return run(['docker','image','inspect',name,'--format','{{.Id}}'],capture_output=True).stdout.strip()
def main():
 quick='--quick' in sys.argv
 fixed_tenants=int(os.environ.get('KAFKA_LOAD_FIXED_TENANTS','0'))
 if fixed_tenants < 0 or fixed_tenants > 1024: raise ValueError('KAFKA_LOAD_FIXED_TENANTS must be between 0 and 1024')
 seed_tenants=int(os.environ.get('KAFKA_SEED_TENANTS','1024'))
 if seed_tenants < max(fixed_tenants,1) or seed_tenants > 1024: raise ValueError('invalid seed tenant count')
 trace_mode=os.environ.get('KAFKA_LOAD_TRACE','0') == '1'
 recovery_wait=int(os.environ.get('KAFKA_LOAD_RECOVERY_WAIT','0'))
 if recovery_wait < 0 or recovery_wait > 1200: raise ValueError('invalid recovery wait')
 if recovery_wait and not trace_mode: raise ValueError('recovery observation requires trace mode')
 inject_settle=os.environ.get('KAFKA_TEST_FAIL_SETTLE_ONCE') == '1'
 if inject_settle and not (trace_mode and recovery_wait): raise ValueError('settle injection requires trace and recovery observation')
 instrument_locks=os.environ.get('KAFKA_TEST_LOCK_TIMING') == '1'
 if instrument_locks and os.environ.get('EVAL_RELAY_OVERLAY'): raise ValueError('lock instrumentation and relay overlay cannot be combined')
 payload_drain=int(os.environ.get('KAFKA_PAYLOAD_DRAIN','0'))
 if payload_drain < 0 or payload_drain > 1200: raise ValueError('invalid payload drain wait')
 mysql_ready_sec=int(os.environ.get('KAFKA_MYSQL_READY_SEC','120'))
 if mysql_ready_sec < 10 or mysql_ready_sec > 900: raise ValueError('invalid mysql readiness bound')
 worker_values=[int(x) for x in os.environ.get('KAFKA_LOAD_WORKERS','32,64,128,256,512').split(',')]
 if not worker_values or any(x < 1 or x > 1024 for x in worker_values): raise ValueError('invalid worker list')
 if not fixed_tenants and seed_tenants < max(max(worker_values),128): raise ValueError('seed tenants below scenario requirement')
 bindir=pathlib.Path(tempfile.mkdtemp(prefix='codex-project-load-'))
 try:
  binaries={}
  builds={'billing':'./cmd/billing','router':'./cmd/router','reconciler':'./cmd/reconciler','consumer':'./cmd/event-consumer','gateway':'./cmd/gateway','mock':'./cmd/mock-provider','seed':'./eval/project-load-2026-11-02/seed','load':'./eval/project-load-2026-11-02/load'}
  if os.environ.get('KAFKA_LOAD_DIAGNOSTIC') == '1':
   builds['load']='./eval/project-load-2026-11-02/diagnostic-load'
  buildenv=os.environ.copy();buildenv['GOPROXY']='off';buildenv['GOTOOLCHAIN']='local';buildenv['SDKROOT']='/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk'
  trace_overlays={}
  if instrument_locks:
   src=ROOT/'internal/billing/store/mysql.go'; content=src.read_text()
   section_start=content.index('func (s *Store) lockNamed(');end=content.index('// reserveFingerprint',section_start);section=content[section_start:end]
   section=section.replace('var conn *sql.Conn','started := time.Now()\n\tvar conn *sql.Conn',1)
   needle='''var acquired sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 5)", name).Scan(&acquired); err != nil {'''
   replacement='''acquireStart := time.Now()
	var acquired sql.NullInt64
	lockErr := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 5)", name).Scan(&acquired)
	if len(name) >= 13 && name[:13] == "billing:cash:" { fmt.Printf("TEST_CASH_LOCK pool_us=%d wait_us=%d valid=%t acquired=%d error=%t\\n", acquireStart.Sub(started).Microseconds(), time.Since(acquireStart).Microseconds(), acquired.Valid, acquired.Int64, lockErr != nil) }
	if err := lockErr; err != nil {'''
   if section.count(needle)!=1:raise RuntimeError('lock timing anchor changed')
   section=section.replace(needle,replacement,1)
   needle='''var releaseOnce sync.Once
	return conn, func() {
		releaseOnce.Do(func() {'''
   replacement='''var releaseOnce sync.Once
	heldStart := time.Now()
	return conn, func() {
		releaseOnce.Do(func() {
			if len(name) >= 13 && name[:13] == "billing:cash:" { fmt.Printf("TEST_CASH_HOLD hold_us=%d\\n", time.Since(heldStart).Microseconds()) }'''
   if section.count(needle)!=1:raise RuntimeError('hold timing anchor changed')
   content=content[:section_start]+section.replace(needle,replacement,1)+content[end:]
   candidate=bindir/'billing-lock-timing.go';candidate.write_text(content)
   overlay=bindir/'billing-lock-timing-overlay.json';overlay.write_text(json.dumps({'Replace':{str(src):str(candidate)}}))
   trace_overlays['billing']=str(overlay)
   (OUT/'billing-lock-timing-sha256.txt').write_text(hashlib.sha256(candidate.read_bytes()).hexdigest()+'\n')
  if trace_mode:
   handler=(ROOT/'internal/gateway/handler/chat.go').read_text(); header='uReq.Header.Set("Content-Type", "application/json")'
   mock=(ROOT/'cmd/mock-provider/main.go').read_text(); marker='mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {'
   if handler.count(header)!=1 or mock.count(marker)!=1: raise RuntimeError('trace overlay anchors changed')
   handler=handler.replace(header,header+'\n\tuReq.Header.Set("X-Comparison-Request-ID", c.GetString("request_id"))',1)
   if os.environ.get('KAFKA_LOAD_STAGE_DETAIL') == '1':
    route='''prov, err := r.Route(c.Request.Context(), req.Model)
		if err != nil {'''
    route_log='''prov, err := r.Route(c.Request.Context(), req.Model)
		if err != nil {
			timeout, ok := err.(interface{ Timeout() bool })
			log.Error("TEST_ROUTE_FAILURE", "request_id", requestID, "timeout", ok && timeout.Timeout(), "context_done", c.Request.Context().Err() != nil)'''
    reserve='''\t\tcancel()
		if err != nil {
			// 目录里没有'''
    reserve_log='''\t\tif err != nil { log.Error("TEST_RESERVE_FAILURE", "request_id", requestID, "grpc_code", status.Code(err).String(), "context_done", rc.Err() != nil) }
	\tcancel()
	\tif err != nil {
	\t\t// 目录里没有'''
    if handler.count(route)!=1 or handler.count(reserve)!=1: raise RuntimeError('stage detail anchors changed')
    handler=handler.replace(route,route_log,1).replace(reserve,reserve_log,1)
   mock=mock.replace(marker,marker+'\n\t\tfmt.Fprintf(os.Stderr, "TRACE_REQUEST_ID %s\\n", r.Header.Get("X-Comparison-Request-ID"))',1)
   for name,source,contents in [('gateway','internal/gateway/handler/chat.go',handler),('mock','cmd/mock-provider/main.go',mock)]:
    candidate=bindir/(name+'-trace.go');candidate.write_text(contents)
    replacements={str(ROOT/source):str(candidate)}
    if name=='gateway' and inject_settle:
     client=(ROOT/'internal/gateway/client/billing.go').read_text()
     anchor='''func (b *Billing) Settle(ctx context.Context, req *billingv1.SettleRequest) (*billingv1.SettleResponse, error) {
	return b.cli.Settle(ctx, req)
}'''
     updated='''var testSettleFailure atomic.Bool

func (b *Billing) Settle(ctx context.Context, req *billingv1.SettleRequest) (*billingv1.SettleResponse, error) {
	if strings.HasPrefix(req.RequestId, "idem:") && testSettleFailure.CompareAndSwap(false, true) {
		return nil, status.Error(codes.DeadlineExceeded, "injected test settle failure before RPC")
	}
	return b.cli.Settle(ctx, req)
}'''
     if client.count(anchor)!=1: raise RuntimeError('settle injection anchor changed')
     client=client.replace('"context"','"context"\n\t"strings"\n\t"sync/atomic"',1).replace(anchor,updated,1)
     client_path=bindir/'gateway-billing-fail-once.go';client_path.write_text(client)
     replacements[str(ROOT/'internal/gateway/client/billing.go')]=str(client_path)
     (OUT/'gateway-billing-fail-once-sha256.txt').write_text(hashlib.sha256(client_path.read_bytes()).hexdigest()+'\n')
    overlay=bindir/(name+'-trace-overlay.json');overlay.write_text(json.dumps({'Replace':replacements}))
    trace_overlays[name]=str(overlay)
    (OUT/(name+'-trace-sha256.txt')).write_text(hashlib.sha256(candidate.read_bytes()).hexdigest()+'\n')
  for name,pkg in builds.items():
   dest=bindir/name
   overlay=(os.environ.get('EVAL_RELAY_OVERLAY') or trace_overlays.get(name)) if name=='billing' else trace_overlays.get(name)
   cmd=['go','build']+(['-overlay',overlay] if overlay else [])+['-o',str(dest),pkg]
   run(cmd,cwd=ROOT,env=buildenv,timeout=180,stdout=subprocess.DEVNULL,stderr=open(OUT/(name+'-build.log'),'w'));binaries[name]=str(dest)
  myimage='docker.m.daocloud.io/library/mysql:8.0.40';reimage='docker.m.daocloud.io/library/redis:7-alpine'
  run(['docker','run','-d','--name',MYSQL,'-e','MYSQL_ROOT_PASSWORD=isolated','-e','MYSQL_DATABASE=project_load','-p','127.0.0.1::3306',myimage],stdout=subprocess.DEVNULL)
  run(['docker','run','-d','--name',REDIS,'-p','127.0.0.1::6379',reimage],stdout=subprocess.DEVNULL)
  mysql_deadline=time.monotonic()+mysql_ready_sec
  while time.monotonic()<mysql_deadline:
   try: probe=subprocess.run(['docker','exec',MYSQL,'mysqladmin','ping','-h','127.0.0.1','-uroot','-pisolated','--silent'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=5)
   except subprocess.TimeoutExpired: continue
   if probe.returncode==0:break
   time.sleep(.5)
  else:raise RuntimeError('MySQL readiness timeout')
  mp,rp=port(MYSQL,'3306/tcp'),port(REDIS,'6379/tcp');ports={k:free_port() for k in ['gateway','billing','admin','router','mock','reconciler','consumer']}
  if len(set(ports.values()))!=len(ports):raise RuntimeError('port collision')
  kp=free_port();kimage='docker.m.daocloud.io/apache/kafka:3.7.0'
  kafkaEnv={'KAFKA_NODE_ID':'1','KAFKA_PROCESS_ROLES':'broker,controller','KAFKA_CONTROLLER_QUORUM_VOTERS':'1@127.0.0.1:9093','KAFKA_LISTENERS':'PLAINTEXT://0.0.0.0:9092,INTERNAL://0.0.0.0:9094,CONTROLLER://0.0.0.0:9093','KAFKA_ADVERTISED_LISTENERS':f'PLAINTEXT://127.0.0.1:{kp},INTERNAL://127.0.0.1:9094','KAFKA_INTER_BROKER_LISTENER_NAME':'INTERNAL','KAFKA_LISTENER_SECURITY_PROTOCOL_MAP':'CONTROLLER:PLAINTEXT,PLAINTEXT:PLAINTEXT,INTERNAL:PLAINTEXT','KAFKA_CONTROLLER_LISTENER_NAMES':'CONTROLLER','KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR':'1','KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR':'1','KAFKA_TRANSACTION_STATE_LOG_MIN_ISR':'1','KAFKA_GROUP_INITIAL_REBALANCE_DELAY_MS':'0','KAFKA_AUTO_CREATE_TOPICS_ENABLE':'false'}
  ka=['docker','run','-d','--name',KAFKA,'-p',f'127.0.0.1:{kp}:9092']
  for key,val in kafkaEnv.items():ka+=['-e',key+'='+val]
  run(ka+[kimage],stdout=subprocess.DEVNULL)
  ready=False
  for _ in range(90):
   try:
    probe=subprocess.run(['docker','exec',KAFKA,'/opt/kafka/bin/kafka-topics.sh','--bootstrap-server','127.0.0.1:9094','--list'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=10)
   except subprocess.TimeoutExpired:
    time.sleep(1);continue
   if probe.returncode==0:ready=True;break
   time.sleep(1)
  if not ready:raise RuntimeError('isolated Kafka did not become ready')
  for topic in ['billing.events','billing.events.dlq']:
   run(['docker','exec',KAFKA,'/opt/kafka/bin/kafka-topics.sh','--bootstrap-server','127.0.0.1:9094','--create','--if-not-exists','--topic',topic,'--partitions','3','--replication-factor','1'],capture_output=True,timeout=20)
  base={'PATH':os.environ.get('PATH','/usr/bin:/bin'),'HOME':os.environ.get('HOME','/tmp'),'TMPDIR':os.environ.get('TMPDIR','/tmp'),'CONTROL_PLANE_TOKEN':'isolated-test-token','MYSQL_DSN':f'root:isolated@tcp(127.0.0.1:{mp})/project_load?charset=utf8mb4&parseTime=true&loc=Local','REDIS_ADDR':f'127.0.0.1:{rp}','LISTEN_HOST':'127.0.0.1','LOG_LEVEL':'error','KAFKA_BROKERS':f'127.0.0.1:{kp}','GOMAXPROCS':'8'}
  def env(**kw):return dict(base,**{k:str(v) for k,v in kw.items()})
  mock=start('mock',[binaries['mock']],env(MOCK_PROVIDER_PORT=ports['mock'],MOCK_PROVIDER_NAME='isolated',MOCK_FAIL_RATE=0,MOCK_LATENCY_MS=15,MOCK_COMPLETION_TOKENS=100));wait_http(f'http://127.0.0.1:{ports["mock"]}/healthz',mock)
  bill=start('billing',[binaries['billing']],env(BILLING_PORT=ports['billing'],BILLING_ADMIN_PORT=ports['admin'],SUB_RENEW_INTERVAL_SEC=60));wait_tcp(ports['billing'],bill)
  seedenv=env(TEST_MYSQL_DSN=base['MYSQL_DSN'],TEST_REDIS_ADDR=base['REDIS_ADDR'],TEST_MOCK_PORT=ports['mock'],TEST_TENANTS=seed_tenants);seed=run([binaries['seed']],cwd=ROOT,env=seedenv,capture_output=True,timeout=30);(OUT/'seed.log').write_text(seed.stdout+seed.stderr)
  consumer=start('consumer',[binaries['consumer']],env(EVENT_CONSUMER_PORT=ports['consumer'],KAFKA_CONSUMER_GROUP='isolated-load'));wait_http(f'http://127.0.0.1:{ports["consumer"]}/healthz',consumer)
  router=start('router',[binaries['router']],env(ROUTER_PORT=ports['router']));wait_http(f'http://127.0.0.1:{ports["router"]}/healthz',router)
  recon=start('reconciler',[binaries['reconciler'],'-interval','5','-grace','600','-no-rebuild'],env(RECONCILER_PORT=ports['reconciler'],BILLING_ADDR=f'127.0.0.1:{ports["billing"]}'));wait_http(f'http://127.0.0.1:{ports["reconciler"]}/healthz',recon)
  gw=start('gateway',[binaries['gateway']],env(GATEWAY_PORT=ports['gateway'],BILLING_ADDR=f'127.0.0.1:{ports["billing"]}',BILLING_ADMIN_ADDR=f'http://127.0.0.1:{ports["admin"]}',ROUTER_ADDR=f'http://127.0.0.1:{ports["router"]}',AGENT_ADDR='http://127.0.0.1:1',RECONCILER_ADDR=f'http://127.0.0.1:{ports["reconciler"]}',RATE_LIMIT_PER_MIN=600));wait_http(f'http://127.0.0.1:{ports["gateway"]}/metrics',gw)
  config={'head':run(['git','rev-parse','HEAD'],cwd=ROOT,capture_output=True).stdout.strip(),'branch':run(['git','branch','--show-current'],cwd=ROOT,capture_output=True).stdout.strip(),'git_status':run(['git','status','--short'],cwd=ROOT,capture_output=True).stdout,'source_sha256':{f:hashlib.sha256((ROOT/f).read_bytes()).hexdigest() for f in ['internal/billing/store/mysql.go','internal/gateway/handler/chat.go','cmd/gateway/main.go','cmd/router/main.go','cmd/billing/main.go','cmd/mock-provider/main.go']},'images':{myimage:docker_image(myimage),reimage:docker_image(reimage),kimage:docker_image(kimage)},'gomaxprocs':8,'mysql_pool_max':100,'rate_per_min':600,'mock_latency_ms':15,'mock_completion_tokens':100,'kafka':'isolated KRaft, relay and event-consumer enabled','isolated_agents':'disabled','reconciler':'sweep every 5s, grace 600s; rebuild disabled','ports':ports,'mysql_port':mp,'redis_port':rp,'quick':quick,'started':datetime.datetime.now().isoformat(),'go_version':run(['go','version'],cwd=ROOT,capture_output=True).stdout.strip(),'sdkroot':'/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk','binary_sha256':{k:hashlib.sha256(pathlib.Path(v).read_bytes()).hexdigest() for k,v in binaries.items()}}
  config['fixed_tenants'] = fixed_tenants or None
  config['seed_tenants'] = seed_tenants
  config['trace_mode'] = trace_mode
  config['workers'] = worker_values
  config['diagnostic_load'] = os.environ.get('KAFKA_LOAD_DIAGNOSTIC') == '1'
  config['stage_detail'] = os.environ.get('KAFKA_LOAD_STAGE_DETAIL') == '1' and trace_mode
  config['lock_timing'] = instrument_locks
  config['recovery_wait_seconds'] = recovery_wait
  config['injected_gateway_settle_failure_once'] = inject_settle
  config['payload_drain_seconds'] = payload_drain
  config['mysql_readiness_limit_seconds'] = mysql_ready_sec
  config['load_source_sha256'] = hashlib.sha256((ROOT / builds['load'] / 'main.go').read_bytes()).hexdigest()
  (OUT/'config.json').write_text(json.dumps(config,ensure_ascii=False,indent=2))
  extra_sources=['internal/billing/consumer/consumer.go','internal/billing/outbox/relay.go','internal/billing/store/outbox.go','internal/pkg/kafka/consumer.go','internal/pkg/kafka/producer.go']
  (OUT/'event-source-sha256.json').write_text(json.dumps({f:hashlib.sha256((ROOT/f).read_bytes()).hexdigest() for f in extra_sources},indent=2)+'\n')
  if os.environ.get('EVAL_RELAY_OVERLAY'):
   overlay_path=pathlib.Path(os.environ['EVAL_RELAY_OVERLAY']); replacements=json.loads(overlay_path.read_text())['Replace']
   (OUT/'overlay-sha256.json').write_text(json.dumps({'overlay':str(overlay_path),'replacements':{k:{'path':v,'sha256':hashlib.sha256(pathlib.Path(v).read_bytes()).hexdigest()} for k,v in replacements.items()}},indent=2)+'\n')
  # Smoke actual authenticated request before load; its durable rows remain in audit.
  smoke=subprocess.run([binaries['load'],'-url',f'http://127.0.0.1:{ports["gateway"]}','-workers','1','-seconds','1'],cwd=ROOT,env=base,capture_output=True,text=True,timeout=25,check=True);(OUT/'smoke.json').write_text(smoke.stdout);sm=json.loads(smoke.stdout)
  if sm['valid_success']<1 or sm['valid_success']!=sm['requests']:raise RuntimeError('smoke failed: '+smoke.stdout)
  cumulative_success=sm['valid_success']
  scenarios=([(False,1,1)] if quick else [(False,fixed_tenants or w,w) for w in worker_values]+[(True,fixed_tenants or 128,128)])
  with open(OUT/'results.jsonl','w') as results,open(OUT/'resources.jsonl','w') as resources,open(OUT/'audits.jsonl','w') as audits:
   for stream,tenants,workers in scenarios:
    for rep in range(1,2 if quick else 3):
     args=[binaries['load'],'-url',f'http://127.0.0.1:{ports["gateway"]}','-workers',str(workers),'-tenants',str(tenants),'-seconds','1' if quick else '5']
     if stream:args.append('-stream')
     trace_path=OUT/f'trace-{workers}-{tenants}-{int(stream)}-{rep}.csv'
     if trace_mode:args += ['-trace-file',str(trace_path)]
     p=subprocess.Popen(args,cwd=ROOT,env=base,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
     while p.poll() is None:
      sample={'time':datetime.datetime.now().isoformat(),'workers':workers,'tenants':tenants,'stream':stream,'rep':rep}
      ids=','.join(str(proc.pid) for _,proc in processes if proc.poll() is None)
      sample['ps']=subprocess.run(['ps','-p',ids,'-o','pid=,%cpu=,rss='],capture_output=True,text=True).stdout
      sample['docker']=subprocess.run(['docker','stats','--no-stream','--format','{{json .}}',MYSQL,REDIS,KAFKA],capture_output=True,text=True).stdout
      resources.write(json.dumps(sample,ensure_ascii=False)+'\n');resources.flush()
      if p.poll() is None:time.sleep(1)
     stdout,stderr=p.communicate(timeout=20)
     if p.returncode!=0:raise RuntimeError(f'load exit {p.returncode}: {stderr}')
     row=json.loads(stdout);row['rep']=rep;cumulative_success+=row['valid_success'];results.write(json.dumps(row)+'\n');results.flush()
     if trace_mode:
      trace=subprocess.run([binaries['seed']],cwd=ROOT,env=dict(seedenv,TEST_MODE='trace',TEST_TRACE_PATH=str(trace_path)),capture_output=True,text=True,timeout=90)
      (OUT/(trace_path.stem+'-audit.json')).write_text(trace.stdout)
      (OUT/(trace_path.stem+'-audit.stderr')).write_text(trace.stderr)
      if trace.returncode!=0:raise RuntimeError(f'trace audit failed rc={trace.returncode}')
     audit=subprocess.run([binaries['seed']],cwd=ROOT,env=dict(seedenv,TEST_MODE='audit'),capture_output=True,text=True,timeout=90)
     (OUT/f'audit-{workers}-{tenants}-{int(stream)}-{rep}.stdout').write_text(audit.stdout)
     (OUT/f'audit-{workers}-{tenants}-{int(stream)}-{rep}.stderr').write_text(audit.stderr)
     if audit.returncode!=0: raise RuntimeError(f'audit failed rc={audit.returncode}, see preserved audit stdout/stderr')
     ar=json.loads(audit.stdout);ar['cumulative_valid_http']=cumulative_success;ar['settled_minus_valid_http']=ar['settled']-cumulative_success;ar.update({'workers':workers,'tenants':tenants,'stream':stream,'rep':rep});audits.write(json.dumps(ar)+'\n');audits.flush()
     print(json.dumps({'result':row,'audit':ar},ensure_ascii=False),flush=True)
  # Observe durable relay/consumer backlog; this is a bounded check, not proof of eventual delivery.
  with open(OUT/'backlog.jsonl','w') as back:
   for _ in range(30):
    q=run(['docker','exec',MYSQL,'mysql','-uroot','-pisolated','-N','project_load','-e',"SELECT (SELECT COUNT(*) FROM outbox_events WHERE sent_at IS NULL), (SELECT COUNT(*) FROM outbox_events), (SELECT COUNT(*) FROM audit_events);"],capture_output=True,timeout=10)
    parts=q.stdout.strip().split();item={'time':datetime.datetime.now().isoformat(),'unsent':int(parts[0]),'outbox':int(parts[1]),'audit':int(parts[2])};back.write(json.dumps(item)+'\n');back.flush()
    if item['unsent']==0 and item['outbox']==item['audit']:break
    time.sleep(1)
  query="SELECT (SELECT COUNT(*) FROM outbox_events o LEFT JOIN audit_events a ON a.event_id=o.event_key WHERE a.id IS NULL), (SELECT COUNT(*) FROM audit_events a LEFT JOIN outbox_events o ON o.event_key=a.event_id WHERE o.id IS NULL), (SELECT COUNT(*) FROM outbox_events WHERE sent_at IS NULL);"
  q=run(['docker','exec',MYSQL,'mysql','-uroot','-pisolated','-N','project_load','-e',query],capture_output=True,timeout=30)
  vals=[int(x) for x in q.stdout.strip().split()];(OUT/'event-reconciliation.json').write_text(json.dumps(dict(zip(['missing_audit','extra_audit','unsent'],vals)),indent=2)+'\n')
  if payload_drain:
   started=time.monotonic()
   with open(OUT/'payload-drain.jsonl','w') as dr:
    while True:
     q=run(['docker','exec',MYSQL,'mysql','-uroot','-pisolated','-N','project_load','-e',query],capture_output=True,timeout=30)
     vals=[int(x) for x in q.stdout.strip().split()]
     dr.write(json.dumps({'elapsed_seconds':time.monotonic()-started,'missing_audit':vals[0],'extra_audit':vals[1],'unsent':vals[2]})+'\n');dr.flush()
     if vals == [0,0,0] or time.monotonic()-started >= payload_drain: break
     time.sleep(2)
   # Audit columns are compared with parsed outbox JSON; JSON equality compares
   # the entire logical payload, not original Kafka wire bytes.
   content_query="""SELECT COUNT(*),
     COALESCE(SUM(NOT (o.payload <=> a.payload)),0),
     COALESCE(SUM(NOT (BINARY a.event_id <=> BINARY JSON_UNQUOTE(JSON_EXTRACT(o.payload,'$.event_id')))
      OR NOT (a.tenant_id <=> CAST(JSON_UNQUOTE(JSON_EXTRACT(o.payload,'$.tenant_id')) AS UNSIGNED))
      OR NOT (BINARY a.request_id <=> BINARY JSON_UNQUOTE(JSON_EXTRACT(o.payload,'$.request_id')))
      OR NOT (BINARY a.phase <=> BINARY JSON_UNQUOTE(JSON_EXTRACT(o.payload,'$.phase')))
      OR NOT (a.amount_cents <=> CAST(JSON_UNQUOTE(JSON_EXTRACT(o.payload,'$.amount_cents')) AS SIGNED))
      OR NOT (BINARY a.model <=> BINARY JSON_UNQUOTE(JSON_EXTRACT(o.payload,'$.model')))
      OR NOT (BINARY a.funding <=> BINARY JSON_UNQUOTE(JSON_EXTRACT(o.payload,'$.funding')))
      OR NOT (a.prompt_tokens <=> CAST(JSON_UNQUOTE(JSON_EXTRACT(o.payload,'$.prompt_tokens')) AS SIGNED))
      OR NOT (a.completion_tokens <=> CAST(JSON_UNQUOTE(JSON_EXTRACT(o.payload,'$.completion_tokens')) AS SIGNED))),0)
     FROM outbox_events o JOIN audit_events a ON a.event_id=o.event_key"""
   q=run(['docker','exec',MYSQL,'mysql','-uroot','-pisolated','-N','project_load','-e',content_query],capture_output=True,timeout=60)
   fields=[int(x) for x in q.stdout.strip().split()]
   (OUT/'payload-reconciliation.json').write_text(json.dumps(dict(zip(['joined','json_mismatch','projected_columns_mismatch'],fields)),indent=2)+'\n')
  if recovery_wait:
   started=time.monotonic()
   while time.monotonic()-started < recovery_wait:
    elapsed=time.monotonic()-started
    (OUT/'recovery-wait.json').write_text(json.dumps({'elapsed_seconds':elapsed,'target_seconds':recovery_wait})+'\n')
    time.sleep(min(5,max(0,recovery_wait-elapsed)))
   (OUT/'recovery-wait.json').write_text(json.dumps({'elapsed_seconds':time.monotonic()-started,'target_seconds':recovery_wait})+'\n')
   for trace_path in sorted(OUT.glob('trace-*.csv')):
    trace=subprocess.run([binaries['seed']],cwd=ROOT,env=dict(seedenv,TEST_MODE='trace',TEST_TRACE_PATH=str(trace_path)),capture_output=True,text=True,timeout=90)
    (OUT/('recovered-'+trace_path.stem+'-audit.json')).write_text(trace.stdout)
    (OUT/('recovered-'+trace_path.stem+'-audit.stderr')).write_text(trace.stderr)
    if trace.returncode!=0: raise RuntimeError(f'recovery trace audit failed rc={trace.returncode}')
   audit=subprocess.run([binaries['seed']],cwd=ROOT,env=dict(seedenv,TEST_MODE='audit'),capture_output=True,text=True,timeout=90)
   (OUT/'recovery-audit.json').write_text(audit.stdout)
   (OUT/'recovery-audit.stderr').write_text(audit.stderr)
   if audit.returncode!=0: raise RuntimeError(f'recovery audit failed rc={audit.returncode}')
   q=run(['docker','exec',MYSQL,'mysql','-uroot','-pisolated','-N','project_load','-e',query],capture_output=True,timeout=30)
   vals=[int(x) for x in q.stdout.strip().split()];(OUT/'recovery-event-reconciliation.json').write_text(json.dumps(dict(zip(['missing_audit','extra_audit','unsent'],vals)),indent=2)+'\n')
  (OUT/'completed').write_text('completed\n')
 finally:
  cleanup()
  import shutil;shutil.rmtree(bindir,ignore_errors=True)
if __name__=='__main__':
 try: main()
 except Exception as e:
  (OUT/'error.txt').write_text(repr(e)+'\n');print(f'run failed: {e}; raw: {OUT}',file=sys.stderr);raise
