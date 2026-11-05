#!/usr/bin/env python3
"""Isolated local end-to-end load harness. Never touches existing services."""
import concurrent.futures, csv, datetime, hashlib, json, os, pathlib, signal, socket, subprocess, sys, tempfile, threading, time, urllib.request, urllib.error, uuid
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
 require_v2=os.environ.get('OPEN_LOAD_REQUIRE_V2','0')
 if require_v2 not in ('0','1'):raise ValueError('invalid billing v2 mode')
 hold_unknown=os.environ.get('OPEN_LOAD_HOLD_UNKNOWN','0')
 if hold_unknown not in ('0','1'):raise ValueError('invalid hold mode')
 rates=[int(x) for x in os.environ.get('OPEN_LOAD_RATES','40,100,160').split(',')]
 duration=int(os.environ.get('OPEN_LOAD_SECONDS','2' if quick else '10'))
 repetitions=int(os.environ.get('OPEN_LOAD_REPETITIONS','1' if quick else '2'))
 inflight=int(os.environ.get('OPEN_LOAD_MAX_INFLIGHT','1000'))
 router_candidate=os.environ.get('OPEN_LOAD_ROUTER_BATCH','0')
 router_success_parallel=os.environ.get('OPEN_LOAD_ROUTER_SUCCESS_PARALLEL','0')
 if router_success_parallel not in ('0','1'):raise ValueError('invalid router success parallel selection')
 router_lock_scope=os.environ.get('OPEN_LOAD_ROUTER_LOCK_SCOPE','0')
 router_deadline=os.environ.get('OPEN_LOAD_ROUTER_DEADLINE','0')
 router_fresh_scope=os.environ.get('OPEN_LOAD_ROUTER_FRESH_SCOPE','0')
 router_refresh_scope=os.environ.get('OPEN_LOAD_ROUTER_REFRESH_SCOPE','0')
 router_refresh_deadline=os.environ.get('OPEN_LOAD_ROUTER_REFRESH_DEADLINE','0')
 router_authority=os.environ.get('OPEN_LOAD_ROUTER_AUTHORITY_CHECK','0')
 router_metadata=os.environ.get('OPEN_LOAD_ROUTER_AUTHORITY_METADATA','0')
 if router_metadata not in ('0','1') or (router_metadata=='1' and router_authority!='1'):raise ValueError('router metadata check requires authority check')
 if router_authority not in ('0','1') or (router_authority=='1' and router_refresh_scope!='1'):raise ValueError('router authority check requires refresh scope')
 if router_refresh_deadline not in ('0','1') or (router_refresh_deadline=='1' and router_refresh_scope!='1'):raise ValueError('refresh deadline requires refresh scope')
 if router_refresh_scope not in ('0','1') or (router_refresh_scope=='1' and any(x=='1' for x in (router_candidate,router_lock_scope,router_deadline,router_fresh_scope))):raise ValueError('invalid router refresh selection')
 if router_fresh_scope not in ('0','1') or (router_fresh_scope=='1' and (router_candidate=='1' or router_lock_scope=='1' or router_deadline=='1')):raise ValueError('invalid fresh router scope selection')
 trace_mock=os.environ.get('OPEN_LOAD_TRACE_MOCK','0')
 unknown_store=os.environ.get('OPEN_LOAD_UNKNOWN_STORE','0')
 legacy_lookup=os.environ.get('OPEN_LOAD_LEGACY_LOOKUP','0')
 ignore_missing_log=os.environ.get('OPEN_LOAD_IGNORE_NOT_FOUND','0')
 lock_timing=os.environ.get('OPEN_LOAD_LOCK_TIMING','0')
 pool_stats=os.environ.get('OPEN_LOAD_POOL_STATS','0')
 if pool_stats not in ('0','1'):raise ValueError('invalid pool stats selection')
 reserve_stages=os.environ.get('OPEN_LOAD_RESERVE_STAGES','0')
 if reserve_stages not in ('0','1'):raise ValueError('invalid reserve stages selection')
 projection_stages=os.environ.get('OPEN_LOAD_PROJECTION_STAGES','0')
 if projection_stages not in ('0','1') or (projection_stages=='1' and reserve_stages!='1'):raise ValueError('projection stages require reserve stages')
 ledger_single=os.environ.get('OPEN_LOAD_LEDGER_SINGLE','0')
 if ledger_single not in ('0','1') or (ledger_single=='1' and projection_stages=='1'):raise ValueError('ledger single conflicts with projection stage overlay')
 auth_pool=os.environ.get('OPEN_LOAD_AUTH_POOL','0')
 auth_single=os.environ.get('OPEN_LOAD_AUTH_SINGLE','0')
 auth_two_query=os.environ.get('OPEN_LOAD_AUTH_TWO_QUERY','0')
 if auth_two_query not in ('0','1') or (auth_two_query=='1' and any(x=='1' for x in (auth_pool,auth_single,unknown_store,legacy_lookup,lock_timing))):raise ValueError('auth two-query requires exclusive billing overlay')
 browser_mix=os.environ.get('OPEN_LOAD_BROWSER_MIX','0')
 auth_cache_seconds=int(os.environ.get('OPEN_LOAD_AUTH_CACHE_SECONDS','0'))
 if auth_cache_seconds < 0 or auth_cache_seconds > 60:raise ValueError('invalid auth cache TTL')
 admission=os.environ.get('OPEN_LOAD_ADMISSION','0')
 if admission not in ('0','1'):raise ValueError('invalid admission candidate selection')
 if auth_single not in ('0','1') or browser_mix not in ('0','1') or (auth_single=='1' and (auth_pool=='1' or unknown_store=='1' or legacy_lookup=='1' or lock_timing=='1')):raise ValueError('invalid auth single candidate selection')
 usage_retry=os.environ.get('OPEN_LOAD_USAGE_RETRY','0')
 recovery_wait=int(os.environ.get('OPEN_LOAD_RECOVERY_WAIT','0'))
 recon_interval=int(os.environ.get('OPEN_LOAD_RECON_INTERVAL','5'))
 if recon_interval<1 or recon_interval>60:raise ValueError('invalid reconciler interval')
 http_recovery=os.environ.get('OPEN_LOAD_HTTP_RECOVERY','0')
 recon_early=os.environ.get('OPEN_LOAD_RECON_EARLY_USAGE','0')
 if recon_early not in ('0','1') or (recon_early=='1' and http_recovery!='1'):raise ValueError('early usage overlay requires HTTP recovery')
 http_usage_failure=os.environ.get('OPEN_LOAD_HTTP_USAGE_FAILURE','0')
 http_usage_ack_loss=os.environ.get('OPEN_LOAD_TEST_USAGE_ACK_LOSS','0')
 if http_usage_ack_loss not in ('0','1') or (http_usage_ack_loss=='1' and (http_recovery!='1' or http_usage_failure=='1' or usage_retry=='1')):raise ValueError('usage acknowledgement loss requires exclusive HTTP recovery without retry')
 if http_usage_failure not in ('0','1') or (http_usage_failure=='1' and (http_recovery!='1' or usage_retry=='1')):raise ValueError('usage failure requires HTTP recovery and no usage retry')
 if http_recovery not in ('0','1') or (http_recovery=='1' and (recovery_wait<1 or require_v2!='1' or hold_unknown!='1' or any(x=='1' for x in (unknown_store,legacy_lookup,lock_timing,auth_pool,auth_single,auth_two_query)))):raise ValueError('HTTP recovery requires strict V2, hold, a wait and no other billing overlay')
 stop_on_anomaly=os.environ.get('OPEN_LOAD_STOP_ON_ANOMALY','0')
 inject_load_usage=os.environ.get('OPEN_LOAD_TEST_LOAD_USAGE_FAILURE','0')
 if reserve_stages=='1' and any(x=='1' for x in (unknown_store,legacy_lookup,lock_timing,auth_pool,auth_single,auth_two_query,http_recovery,inject_load_usage)):raise ValueError('reserve stages requires exclusive billing store overlay')
 if stop_on_anomaly not in ('0','1') or (stop_on_anomaly=='1' and (recovery_wait<1 or hold_unknown!='1')):raise ValueError('stop-on-anomaly requires recovery wait and unknown hold')
 if inject_load_usage not in ('0','1') or (inject_load_usage=='1' and (stop_on_anomaly!='1' or require_v2!='1' or http_recovery=='1' or usage_retry=='1' or any(x=='1' for x in (unknown_store,legacy_lookup,lock_timing,auth_pool,auth_single,auth_two_query)))):raise ValueError('load usage failure requires isolated stop-on-anomaly and strict V2')
 synthetic_recovery=os.environ.get('OPEN_LOAD_SYNTHETIC_RECOVERY','0')
 if synthetic_recovery not in ('0','1') or (synthetic_recovery=='1' and (recovery_wait<1 or require_v2!='1' or hold_unknown!='1')):raise ValueError('synthetic recovery requires a wait, strict V2 and unknown hold')
 redis_restart=os.environ.get('OPEN_LOAD_REDIS_RESTART','0')
 probe_missing=os.environ.get('OPEN_LOAD_REDIS_PROBE_MISSING','0')
 probe_per_tenant=int(os.environ.get('OPEN_LOAD_REDIS_PROBE_PER_TENANT','1'))
 if probe_per_tenant<1 or probe_per_tenant>4 or (probe_per_tenant>1 and probe_missing!='1'):raise ValueError('invalid number of missing-key probes')
 if probe_missing not in ('0','1') or (probe_missing=='1' and redis_restart!='1'):raise ValueError('missing-key probes require redis restart')
 if redis_restart not in ('0','1') or (redis_restart=='1' and (len(rates)!=1 or repetitions!=3 or recovery_wait!=0)):raise ValueError('redis restart requires one rate, three repetitions and no recovery wait')
 if redis_restart=='1' and stop_on_anomaly=='1':raise ValueError('stop-on-anomaly cannot be combined with Redis restart mode')
 if router_deadline not in ('0','1') or (router_deadline=='1' and (router_candidate=='1' or router_lock_scope=='1')) or router_candidate not in ('0','1') or unknown_store not in ('0','1') or router_lock_scope not in ('0','1') or trace_mock not in ('0','1') or legacy_lookup not in ('0','1') or ignore_missing_log not in ('0','1') or usage_retry not in ('0','1') or auth_pool not in ('0','1') or (auth_pool=='1' and (legacy_lookup=='1' or unknown_store=='1' or lock_timing=='1')) or lock_timing not in ('0','1') or (lock_timing=='1' and (legacy_lookup=='1' or unknown_store=='1')) or (ignore_missing_log=='1' and legacy_lookup!='1') or (legacy_lookup=='1' and unknown_store=='1') or (router_candidate=='1' and router_lock_scope=='1'):raise ValueError('invalid candidate selection')
 if not rates or any(x<1 or x>1000 for x in rates) or duration<1 or duration>120 or repetitions<1 or repetitions>5 or inflight<1 or inflight>3000 or recovery_wait<0 or recovery_wait>660:raise ValueError('invalid bounded load configuration')
 bindir=pathlib.Path(tempfile.mkdtemp(prefix='codex-project-load-'))
 try:
  binaries={}
  builds={'billing':'./cmd/billing','router':'./cmd/router','reconciler':'./cmd/reconciler','gateway':'./cmd/gateway','mock':'./cmd/mock-provider','seed':'./eval/project-load-2026-11-02/seed','load':'./eval/project-load-2026-11-02/open-load','inject':'./eval/project-load-2026-11-02/pending-fixture'}
  buildenv=os.environ.copy();buildenv['GOPROXY']='off';buildenv['GOTOOLCHAIN']='local';buildenv['SDKROOT']='/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk'
  # Local-only diagnostic overlay logs error classes and elapsed time, no keys or bodies.
  handler=(ROOT/'internal/gateway/handler/chat.go').read_text()
  if usage_retry=='1':
   retry_candidate_dir=bindir/'usage-retry'
   run([sys.executable,str(ROOT/'eval/billing-comparison-2026-11-02/build_usage_retry_overlay.py'),str(retry_candidate_dir)],cwd=ROOT)
   handler=(retry_candidate_dir/'chat.go').read_text()
  handler=handler.replace('"context"','"context"\n "errors"',1)
  if trace_mock=='1':
   header='uReq.Header.Set("Content-Type", "application/json")'
   if handler.count(header)!=1:raise ValueError('gateway trace insertion ambiguous')
   handler=handler.replace(header,header+'\n\tuReq.Header.Set("X-Comparison-Request-ID", c.GetString("request_id"))',1)
  handler=handler.replace('prov, err := r.Route(c.Request.Context(), req.Model)','diagRouteStart := time.Now()\n\t\tprov, err := r.Route(c.Request.Context(), req.Model)')
  handler=handler.replace('if err != nil {\n\t\t\tabort(c, http.StatusServiceUnavailable, "routing unavailable")','if err != nil {\n\t\t\tlog.Error("DIAG route", "deadline", errors.Is(err,context.DeadlineExceeded), "elapsed_ms", time.Since(diagRouteStart).Milliseconds())\n\t\t\tabort(c, http.StatusServiceUnavailable, "routing unavailable")')
  handler=handler.replace('rc, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)','diagStart := time.Now()\n\t\trc, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)')
  handler=handler.replace('if err != nil {\n\t\t\t// 目录里没有', 'if err != nil {\n\t\t\tlog.Error("DIAG reserve", "request_id", requestID, "grpc_code", status.Code(err).String(), "elapsed_ms", time.Since(diagStart).Milliseconds())\n\t\t\t// 目录里没有')
  auth=(ROOT/'internal/gateway/middleware/apikey.go').read_text().replace('"context"','"context"\n "log"\n "google.golang.org/grpc/status"',1)
  auth=auth.replace('resp, err := b.ValidateAPIKey(ctx, hash)','diagStart := time.Now()\n\t\tresp, err := b.ValidateAPIKey(ctx, hash)')
  auth=auth.replace('if err != nil || resp == nil {','if err != nil || resp == nil {\n            log.Printf("DIAG auth code=%s elapsed_ms=%d", status.Code(err).String(),time.Since(diagStart).Milliseconds())')
  replace={}
  for name,content in [('internal/gateway/handler/chat.go',handler),('internal/gateway/middleware/apikey.go',auth)]:
   path=bindir/(pathlib.Path(name).name+'.go');path.write_text(content);replace[str(ROOT/name)]=str(path)
  if admission=='1':
   admission_source=bindir/'gateway-admission.go'
   run([sys.executable,str(ROOT/'eval/project-load-2026-11-02/build_admission_overlay.py'),str(admission_source)],cwd=ROOT)
   replace[str(ROOT/'cmd/gateway/main.go')]=str(admission_source)
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
  if http_recovery=='1' or inject_load_usage=='1':
   src=ROOT/'internal/billing/server/service.go'; content=src.read_text()
   anchor=('func (s *Service) ReportUsage(ctx context.Context, req *billingv1.ReportUsageRequest) (*billingv1.ReportUsageResponse, error) {' if http_usage_failure=='1' or inject_load_usage=='1' or http_usage_ack_loss=='1' else 'func (s *Service) Settle(ctx context.Context, req *billingv1.SettleRequest) (*billingv1.SettleResponse, error) {')
   if content.count(anchor)!=1 or content.count('"context"')!=1:raise ValueError('settle injection anchor changed')
   content=content.replace('"context"','"context"\n\t"strings"\n\t"sync/atomic"',1)
   inject_prefix='idem:stop-test-' if inject_load_usage=='1' else 'idem:recon-http-'
   if http_usage_ack_loss=='1':
    content=content.replace(anchor,'var testInjectedSettle atomic.Bool\n\n'+anchor,1)
    ack='\treturn &billingv1.ReportUsageResponse{Accepted: true}, nil'
    if content.count(ack)!=1:raise ValueError('usage acknowledgement anchor changed')
    content=content.replace(ack,'\tif strings.HasPrefix(req.RequestId, "'+inject_prefix+'") && testInjectedSettle.CompareAndSwap(false, true) { return nil, status.Error(codes.Unavailable, "test injected usage acknowledgement loss after store") }\n'+ack,1)
   else:
    content=content.replace(anchor,'var testInjectedSettle atomic.Bool\n\n'+anchor+'\n\tif strings.HasPrefix(req.RequestId, "'+inject_prefix+'") && testInjectedSettle.CompareAndSwap(false, true) { return nil, status.Error(codes.Unavailable, "test injected '+('usage' if http_usage_failure=='1' or inject_load_usage=='1' else 'settle')+' before store") }',1)
   injection_source=bindir/'billing-settle-injection.go';injection_source.write_text(content)
   billing_overlay.write_text(json.dumps({'Replace':{str(src):str(injection_source)}}))
  if legacy_lookup=='1':
   source=(ROOT/'internal/billing/store/mysql.go').read_text()
   current='\tresult := s.db.WithContext(ctx).\n\t\tWhere("request_id = ? AND phase = ?", requestID, PhaseReserve).\n\t\tOrder("id ASC").Limit(1).Find(&row)\n\tif result.Error != nil {\n\t\treturn nil, result.Error\n\t}\n\tif result.RowsAffected == 0 {\n\t\treturn nil, nil\n\t}'
   baseline='\terr := s.db.WithContext(ctx).\n\t\tWhere("request_id = ? AND phase = ?", requestID, PhaseReserve).\n\t\tFirst(&row).Error\n\tif errors.Is(err, gorm.ErrRecordNotFound) {\n\t\treturn nil, nil\n\t}\n\tif err != nil {\n\t\treturn nil, err\n\t}'
   if source.count(current)!=1:raise ValueError('legacy lookup overlay does not match current source')
   legacy_source=bindir/'billing-legacy-lookup.go';legacy_source.write_text(source.replace(current,baseline,1))
   legacy_mapping={str(ROOT/'internal/billing/store/mysql.go'):str(legacy_source)}
   if ignore_missing_log=='1':
    entry=(ROOT/'cmd/billing/main.go').read_text()
    import_anchor='\t"context"'
    logger_anchor='gormlogger.Default.LogMode(gormlogger.Error)'
    if entry.count(import_anchor)!=1 or entry.count(logger_anchor)!=1:raise ValueError('logger overlay does not match source')
    entry=entry.replace(import_anchor,import_anchor+'\n\tstdlog "log"',1)
    entry=entry.replace(logger_anchor,'gormlogger.New(stdlog.New(os.Stdout, "\\r\\n", stdlog.LstdFlags), gormlogger.Config{SlowThreshold: 200*time.Millisecond, LogLevel: gormlogger.Error, IgnoreRecordNotFoundError: true, Colorful: true})',1)
    logger_source=bindir/'billing-no-notfound-log.go';logger_source.write_text(entry)
    legacy_mapping[str(ROOT/'cmd/billing/main.go')]=str(logger_source)
   billing_overlay.write_text(json.dumps({'Replace':legacy_mapping}))
  if lock_timing=='1':
   src=ROOT/'internal/billing/store/mysql.go'; content=src.read_text()
   if content.count('"context"')!=1:raise ValueError('lock import anchor changed')
   content=content.replace('"context"','"context"\n\t"strings"',1)
   section_start=content.index('func (s *Store) lockNamed(');section_end=content.index('// reserveFingerprint',section_start);section=content[section_start:section_end]
   section=section.replace('var conn *sql.Conn','started := time.Now()\n\tvar conn *sql.Conn',1)
   needle='\t\tconn, err = db.Conn(ctx)\n\t\tif err != nil {\n\t\t\treturn nil, nil, err\n\t\t}'
   replacement='\t\tconn, err = db.Conn(ctx)\n\t\tif err != nil {\n\t\t\tfmt.Printf("TEST_LOCK_POOL_ERROR kind=%s pool_us=%d\\n", strings.Split(name, ":")[1], time.Since(started).Microseconds())\n\t\t\treturn nil, nil, err\n\t\t}'
   if section.count(needle)!=1:raise ValueError('pool timing anchor changed')
   section=section.replace(needle,replacement,1)
   needle='\tvar acquired sql.NullInt64\n\tif err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 5)", name).Scan(&acquired); err != nil {'
   replacement='\tacquireStart := time.Now()\n\tvar acquired sql.NullInt64\n\tlockErr := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 5)", name).Scan(&acquired)\n\tfmt.Printf("TEST_LOCK_ACQUIRE kind=%s pool_us=%d wait_us=%d valid=%t acquired=%d error=%t owned=%t\\n", strings.Split(name, ":")[1], acquireStart.Sub(started).Microseconds(), time.Since(acquireStart).Microseconds(), acquired.Valid, acquired.Int64, lockErr != nil, owned)\n\tif err := lockErr; err != nil {'
   if section.count(needle)!=1:raise ValueError('lock timing anchor changed')
   section=section.replace(needle,replacement,1)
   needle='\tvar releaseOnce sync.Once\n\treturn conn, func() {\n\t\treleaseOnce.Do(func() {'
   replacement='\tvar releaseOnce sync.Once\n\theldStart := time.Now()\n\treturn conn, func() {\n\t\treleaseOnce.Do(func() {\n\t\t\tfmt.Printf("TEST_LOCK_HOLD kind=%s hold_us=%d\\n", strings.Split(name, ":")[1], time.Since(heldStart).Microseconds())'
   if section.count(needle)!=1:raise ValueError('lock hold timing anchor changed')
   candidate=bindir/'billing-lock-timing.go';candidate.write_text(content[:section_start]+section.replace(needle,replacement,1)+content[section_end:])
   billing_overlay.write_text(json.dumps({'Replace':{str(src):str(candidate)}}))
  if reserve_stages=='1':
   src=ROOT/'internal/billing/store/mysql.go'; content=src.read_text()
   section_start=content.index('func (s *Store) Reserve(ctx '); section_end=content.index('// lockReserveRequest',section_start)
   outer=content[section_start:section_end]
   anchor='\tlocked, release, err := s.lockCash(ctx, uint64(req.TenantId))'
   if outer.count(anchor)!=1:raise ValueError('reserve outer stage anchor changed')
   outer=outer.replace(anchor,'\tdiagStart := time.Now()\n'+anchor+'\n\tfmt.Printf("TEST_RESERVE_LOCK id=%s elapsed_us=%d error=%t\\n", req.RequestId, time.Since(diagStart).Microseconds(), err != nil)',1)
   content=content[:section_start]+outer+content[section_end:]
   section_start=content.index('func (s *Store) reserveLocked(');section_end=content.index('// compensateReserve',section_start)
   section=content[section_start:section_end]
   init='\tdbctx := ctx'
   if section.count(init)!=1:raise ValueError('reserve stage init anchor changed')
   section=section.replace(init,'''\tdiagStarted := time.Now()
\tdiagStage := "initial_queries"
\tdiagNext := func(next string) {
\t\tfmt.Printf("TEST_RESERVE_STAGE id=%s stage=%s elapsed_us=%d\\n", req.RequestId, diagStage, time.Since(diagStarted).Microseconds())
\t\tdiagStage = next
\t\tdiagStarted = time.Now()
\t}
\tdefer func() { diagNext("end") }()
'''+init,1)
   anchors=[('\tconn, unlock, err := s.lockReserveRequest(ctx, req.RequestId)','request_lock'),('\tif err := s.repairOrphanGuard(dbctx, tenantID, ReqGuardKey(req.RequestId)); err != nil {','guard_and_recheck'),('\tif err := s.requireCashMatch(dbctx, tenantID); err != nil {','projection_check'),('\towner := rand.Text()','redis_reserve'),('\t// 3) 落 reserve 行','sql_transaction')]
   for needle,label in anchors:
    if section.count(needle)!=1:raise ValueError('reserve stage anchor changed: '+label)
    section=section.replace(needle,'\tdiagNext("'+label+'")\n'+needle,1)
   if projection_stages=='1':
    needle='\tdbctx, cancelSQL := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)'
    if section.count(needle)!=1:raise ValueError('projection context anchor changed')
    section=section.replace(needle,needle+'\n\tdbctx = context.WithValue(dbctx, testProjectionKey{}, req.RequestId)',1)
    helper='''type testProjectionKey struct{}

func testProjectionTimer(ctx context.Context, stage string) func() {
    id, ok := ctx.Value(testProjectionKey{}).(string)
    if !ok { return func() {} }
    started := time.Now()
    return func() { fmt.Printf("TEST_PROJECTION id=%s stage=%s elapsed_us=%d\\n", id, stage, time.Since(started).Microseconds()) }
}

'''
    content=content[:section_start]+helper+content[section_start:]
    section_start+=len(helper);section_end+=len(helper)
   candidate=bindir/'billing-reserve-stages.go';candidate.write_text(content[:section_start]+section+content[section_end:])
   reserve_map={str(src):str(candidate)}
   if projection_stages=='1':
    cash_src=ROOT/'internal/billing/store/cash.go';cash=cash_src.read_text()
    cstart=cash.index('func (s *Store) rebuildCash(');cend=cash.index('func (s *Store) repairOrphanGuard(',cstart);part=cash[cstart:cend]
    for needle,stage in [('\tif err := s.db.WithContext(ctx).First(&row, tenant).Error; err != nil {','tenant_lookup'),('\tbal, err := LedgerBalance(ctx, s.db, tenant)','ledger'),('\t_, err = s.runCashLua(ctx, `','redis_rebuild')]:
     if part.count(needle)!=1:raise ValueError('projection cash anchor changed: '+stage)
     part=part.replace(needle,'\tstop_'+stage+' := testProjectionTimer(ctx, "'+stage+'")\n'+needle,1)
    part=part.replace('\tif row.Status != 0', '\tstop_tenant_lookup()\n\tif row.Status != 0',1)
    part=part.replace('\tif err != nil {\n\t\treturn 0, err\n\t}\n\tstop_redis_rebuild', '\tstop_ledger()\n\tif err != nil {\n\t\treturn 0, err\n\t}\n\tstop_redis_rebuild',1)
    part=part.replace('\treturn bal, err\n}', '\tstop_redis_rebuild()\n\treturn bal, err\n}',1)
    cash_candidate=bindir/'billing-projection-cash.go';cash_candidate.write_text(cash[:cstart]+part+cash[cend:]);reserve_map[str(cash_src)]=str(cash_candidate)
    plan_src=ROOT/'internal/billing/store/plan.go';plan=plan_src.read_text();pstart=plan.index('func LedgerBalance(');pend=plan.index('// Intermediate sums',pstart);part=plan[pstart:pend]
    for needle,stage in [('\tif err := db.WithContext(ctx).Model(&Topup{}).','topups'),('\tif err := db.WithContext(ctx).Model(&Bill{}).\n\t\tWhere("tenant_id = ? AND phase = ? AND status = ?", tenantID, PhaseReserve, StatusPending).','pending'),('\tif err := db.WithContext(ctx).Model(&Bill{}).\n\t\tWhere("tenant_id = ? AND phase = ? AND status = ?", tenantID, PhaseSettle, StatusSettled).','settled'),('\tif err := db.WithContext(ctx).Model(&Tenant{}).','initial')]:
     if part.count(needle)!=1:raise ValueError('ledger stage anchor changed: '+stage)
     part=part.replace(needle,'\tstop_'+stage+' := testProjectionTimer(ctx, "'+stage+'")\n'+needle,1)
    for stage,nextstage in [('topups','pending'),('pending','settled'),('settled','initial')]:
     part=part.replace('\tstop_'+nextstage+' :=', '\tstop_'+stage+'()\n\tstop_'+nextstage+' :=',1)
    part=part.replace('\treturn checkedLedgerBalance(', '\tstop_initial()\n\treturn checkedLedgerBalance(',1)
    plan=plan[:pstart]+part+plan[pend:]
    pstart=plan.index('func (s *Store) requireCashMatch(');pend=plan.index('func insertTopup(',pstart);part=plan[pstart:pend]
    needle='\tlive, err := s.getBalance(ctx, tenantID)'
    if part.count(needle)!=1:raise ValueError('projection readback anchor changed')
    part=part.replace(needle,'\tstop_readback := testProjectionTimer(ctx, "redis_readback")\n'+needle+'\n\tstop_readback()',1)
    plan_candidate=bindir/'billing-projection-plan.go';plan_candidate.write_text(plan[:pstart]+part+plan[pend:]);reserve_map[str(plan_src)]=str(plan_candidate)
   billing_overlay.write_text(json.dumps({'Replace':reserve_map}))
  if auth_pool=='1':
   auth_candidate_dir=bindir/'auth-pool'
   run([sys.executable, str(ROOT/'eval/billing-comparison-2026-11-02/build_auth_pool_overlay.py'), str(auth_candidate_dir)],cwd=ROOT)
   billing_overlay=auth_candidate_dir/'overlay.json'
  if auth_single=='1':
   single_dir=bindir/'auth-single'
   run([sys.executable,str(ROOT/'eval/billing-comparison-2026-11-02/build_auth_single_overlay.py'),str(single_dir)],cwd=ROOT)
   billing_overlay=single_dir/'overlay.json'
  if auth_two_query=='1':
   two_query_dir=bindir/'auth-two-query'
   run([sys.executable,str(ROOT/'eval/billing-comparison-2026-11-02/build_auth_two_query_overlay.py'),str(two_query_dir)],cwd=ROOT)
   billing_overlay=two_query_dir/'overlay.json'
  if ledger_single=='1':
   single_ledger_dir=bindir/'ledger-single';single_ledger_dir.mkdir()
   run([sys.executable,str(ROOT/'eval/billing-comparison-2026-11-02/build_ledger_single_overlay.py'),str(single_ledger_dir)],cwd=ROOT)
   ledger_map=json.loads((single_ledger_dir/'overlay.json').read_text())['Replace']
   billing_map=json.loads(billing_overlay.read_text()) if billing_overlay.exists() else {'Replace':{}}
   if set(ledger_map).intersection(billing_map['Replace']):raise ValueError('ledger single overlay conflicts with billing overlay')
   billing_map['Replace'].update(ledger_map);billing_overlay=bindir/'billing-ledger-combined.json';billing_overlay.write_text(json.dumps(billing_map))
  if pool_stats=='1':
   main_src=ROOT/'cmd/billing/main.go'; main_content=main_src.read_text()
   if main_content.count('"context"')!=1 or main_content.count('\trdb := redisx.New(redisAddr, redisPwd)')!=1:raise ValueError('pool stats anchors changed')
   main_content=main_content.replace('"context"','"context"\n\t"fmt"',1)
   marker='\trdb := redisx.New(redisAddr, redisPwd)'
   observer='''\tif pool, poolErr := db.DB(); poolErr == nil {
\t\tgo func() {
\t\t\tticker := time.NewTicker(500 * time.Millisecond)
\t\t\tdefer ticker.Stop()
\t\t\tfor now := range ticker.C {
\t\t\t\ts := pool.Stats()
\t\t\t\tfmt.Printf("TEST_DB_POOL time=%s max=%d open=%d inuse=%d idle=%d wait_count=%d wait_ns=%d\\n", now.UTC().Format(time.RFC3339Nano), s.MaxOpenConnections, s.OpenConnections, s.InUse, s.Idle, s.WaitCount, s.WaitDuration.Nanoseconds())
\t\t\t}
\t\t}()
\t}
'''
   main_candidate=bindir/'billing-pool-stats.go';main_candidate.write_text(main_content.replace(marker,observer+marker,1))
   pool_map=json.loads(billing_overlay.read_text()) if billing_overlay.exists() else {'Replace':{}}
   if str(main_src) in pool_map['Replace']:raise ValueError('pool stats conflicts with billing main overlay')
   pool_map['Replace'][str(main_src)]=str(main_candidate)
   billing_overlay.write_text(json.dumps(pool_map))
  router_overlay=bindir/'router-overlay.json'
  if router_success_parallel=='1' and any(x=='1' for x in (router_candidate,router_lock_scope,router_deadline,router_fresh_scope,router_refresh_scope)):raise ValueError('parallel success requires exclusive router overlay')
  router_source=ROOT/'eval/project-load-2026-11-02/router_registry_batch_candidate.go.txt'
  if router_lock_scope=='1':router_source=ROOT/'eval/project-load-2026-11-02/router_registry_lock_scope_current_20261105.go.txt'
  if router_deadline=='1':
   deadline_dir=bindir/'router-deadline'
   run([sys.executable,str(ROOT/'eval/billing-comparison-2026-11-02/build_router_deadline_overlay.py'),str(deadline_dir)],cwd=ROOT)
   router_source=deadline_dir/'registry.go'
   (OUT/'router-deadline-candidate.go.txt').write_bytes(router_source.read_bytes())
  if router_fresh_scope=='1':
   fresh_dir=bindir/'router-fresh-scope'
   run([sys.executable,str(ROOT/'eval/billing-comparison-2026-11-02/build_router_fresh_scope_overlay.py'),str(fresh_dir)],cwd=ROOT)
   router_source=fresh_dir/'registry.go'
   (OUT/'router-fresh-scope-candidate.go.txt').write_bytes(router_source.read_bytes())
  if router_refresh_scope=='1':
   refresh_dir=bindir/'router-refresh-scope'
   run([sys.executable,str(ROOT/'eval/billing-comparison-2026-11-02/build_router_refresh_scope_overlay.py'),str(refresh_dir)],cwd=ROOT,env=dict(os.environ,TEST_ROUTER_REFRESH_UNFENCED='0',TEST_ROUTER_REFRESH_DEADLINE=router_refresh_deadline,TEST_ROUTER_AUTHORITY_CHECK=router_authority,TEST_ROUTER_AUTHORITY_METADATA=router_metadata))
   router_source=refresh_dir/'registry.go'
   (OUT/'router-refresh-scope-candidate.go.txt').write_bytes(router_source.read_bytes())
  if router_success_parallel=='1':
   parallel_dir=bindir/'router-success-parallel'
   run([sys.executable,str(ROOT/'eval/billing-comparison-2026-11-02/build_router_success_parallel_overlay.py'),str(parallel_dir)],cwd=ROOT)
   router_source=parallel_dir/'registry.go'
   (OUT/'router-success-parallel.go.txt').write_bytes(router_source.read_bytes())
  if router_success_parallel=='1' or router_candidate=='1' or router_lock_scope=='1' or router_deadline=='1' or router_fresh_scope=='1' or router_refresh_scope=='1':router_overlay.write_text(json.dumps({'Replace':{str(ROOT/'internal/router/registry.go'):str(router_source)}}))
  (OUT/'overlay-sha256.json').write_text(json.dumps({k:hashlib.sha256(pathlib.Path(v).read_bytes()).hexdigest() for k,v in replace.items()},indent=2))
  if recon_early=='1':
   early_dir=bindir/'recon-early'
   run([sys.executable,str(ROOT/'eval/billing-comparison-2026-11-02/build_recon_early_usage_overlay.py'),str(early_dir)],cwd=ROOT)
   (OUT/'recon-early-candidate.go.txt').write_bytes((early_dir/'reconciler.go').read_bytes())
  for name,pkg in builds.items():
   dest=bindir/name; args=['go','build']+(['-overlay',str(overlay)] if name=='gateway' else (['-overlay',str(router_overlay)] if name=='router' and (router_success_parallel=='1' or router_candidate=='1' or router_lock_scope=='1' or router_deadline=='1' or router_fresh_scope=='1' or router_refresh_scope=='1') else (['-overlay',str(billing_overlay)] if name=='billing' and (unknown_store=='1' or legacy_lookup=='1' or lock_timing=='1' or auth_pool=='1' or auth_single=='1' or auth_two_query=='1' or http_recovery=='1' or inject_load_usage=='1' or pool_stats=='1' or reserve_stages=='1' or ledger_single=='1') else (['-overlay',str(mock_overlay)] if name=='mock' and trace_mock=='1' else []))))+['-o',str(dest),pkg]
   if name=='reconciler' and recon_early=='1': args[2:2]=['-overlay',str(early_dir/'overlay.json')]
   run(args,cwd=ROOT,env=buildenv,timeout=180,stdout=subprocess.DEVNULL,stderr=open(OUT/(name+'-build.log'),'w'));binaries[name]=str(dest)
  myimage='docker.m.daocloud.io/library/mysql:8.0.40';reimage='docker.m.daocloud.io/library/redis:7-alpine'
  run(['docker','run','-d','--name',MYSQL,'-e','MYSQL_ROOT_PASSWORD=isolated','-e','MYSQL_DATABASE=project_load','-p','127.0.0.1::3306',myimage],stdout=subprocess.DEVNULL)
  redis_binding=f'127.0.0.1:{free_port()}:6379' if redis_restart=='1' else '127.0.0.1::6379'
  run(['docker','run','-d','--name',REDIS,'-p',redis_binding,reimage],stdout=subprocess.DEVNULL)
  for _ in range(120):
   if subprocess.run(['docker','exec',MYSQL,'mysqladmin','ping','-h','127.0.0.1','-uroot','-pisolated','--silent'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0:break
   time.sleep(.5)
  else:raise RuntimeError('MySQL readiness timeout')
  mp,rp=port(MYSQL,'3306/tcp'),port(REDIS,'6379/tcp');ports={k:free_port() for k in ['gateway','billing','admin','router','mock','reconciler']}
  if len(set(ports.values()))!=len(ports):raise RuntimeError('port collision')
  base={'PATH':os.environ.get('PATH','/usr/bin:/bin'),'HOME':os.environ.get('HOME','/tmp'),'TMPDIR':os.environ.get('TMPDIR','/tmp'),'CONTROL_PLANE_TOKEN':'isolated-test-token','MYSQL_DSN':f'root:isolated@tcp(127.0.0.1:{mp})/project_load?charset=utf8mb4&parseTime=true&loc=Local','REDIS_ADDR':f'127.0.0.1:{rp}','LISTEN_HOST':'127.0.0.1','LOG_LEVEL':'error','KAFKA_BROKERS':'','GOMAXPROCS':'8'}
  base['BILLING_AUTH_CACHE_TTL_SECONDS']=str(auth_cache_seconds)
  def env(**kw):return dict(base,**{k:str(v) for k,v in kw.items()})
  mock=start('mock',[binaries['mock']],env(MOCK_PROVIDER_PORT=ports['mock'],MOCK_PROVIDER_NAME='isolated',MOCK_FAIL_RATE=0,MOCK_LATENCY_MS=15,MOCK_COMPLETION_TOKENS=100));wait_http(f'http://127.0.0.1:{ports["mock"]}/healthz',mock)
  bill=start('billing',[binaries['billing']],env(BILLING_PORT=ports['billing'],BILLING_ADMIN_PORT=ports['admin'],SUB_RENEW_INTERVAL_SEC=60,BILLING_REQUIRE_RESERVE_V2=require_v2));wait_tcp(ports['billing'],bill)
  seedenv=env(TEST_MYSQL_DSN=base['MYSQL_DSN'],TEST_REDIS_ADDR=base['REDIS_ADDR'],TEST_MOCK_PORT=ports['mock'],TEST_TENANTS=64,TEST_AUDIT_ALLOW_MISSING=redis_restart,TEST_BROWSER_MIX=browser_mix);seed=run([binaries['seed']],cwd=ROOT,env=seedenv,capture_output=True,timeout=300);(OUT/'seed.log').write_text(seed.stdout+seed.stderr)
  router=start('router',[binaries['router']],env(ROUTER_PORT=ports['router']));wait_http(f'http://127.0.0.1:{ports["router"]}/healthz',router)
  recon=start('reconciler',[binaries['reconciler'],'-interval',str(recon_interval),'-grace','600','-no-rebuild']+(['-no-auto-reverse'] if hold_unknown=='1' else []),env(RECONCILER_PORT=ports['reconciler'],BILLING_ADDR=f'127.0.0.1:{ports["billing"]}'));wait_http(f'http://127.0.0.1:{ports["reconciler"]}/healthz',recon)
  gw=start('gateway',[binaries['gateway']],env(GATEWAY_PORT=ports['gateway'],BILLING_ADDR=f'127.0.0.1:{ports["billing"]}',BILLING_ADMIN_ADDR=f'http://127.0.0.1:{ports["admin"]}',ROUTER_ADDR=f'http://127.0.0.1:{ports["router"]}',AGENT_ADDR='http://127.0.0.1:1',RECONCILER_ADDR=f'http://127.0.0.1:{ports["reconciler"]}',RATE_LIMIT_PER_MIN=600,GATEWAY_HOLD_UNKNOWN=hold_unknown));wait_http(f'http://127.0.0.1:{ports["gateway"]}/metrics',gw)
  config={'head':run(['git','rev-parse','HEAD'],cwd=ROOT,capture_output=True).stdout.strip(),'branch':run(['git','branch','--show-current'],cwd=ROOT,capture_output=True).stdout.strip(),'git_status':run(['git','status','--short'],cwd=ROOT,capture_output=True).stdout,'source_sha256':{f:hashlib.sha256((ROOT/f).read_bytes()).hexdigest() for f in ['internal/billing/store/mysql.go','internal/billing/reconciler/reconciler.go','internal/router/registry.go','internal/billing/server/service.go','internal/gateway/client/billing.go','internal/gateway/handler/chat.go','internal/gateway/middleware/apikey.go','cmd/gateway/main.go','cmd/router/main.go','cmd/billing/main.go','cmd/mock-provider/main.go','eval/project-load-2026-11-02/pending-fixture/main.go']},'images':{myimage:docker_image(myimage),reimage:docker_image(reimage)},'gomaxprocs':8,'mysql_pool_max':100,'rate_per_min':600,'mock_latency_ms':15,'mock_completion_tokens':100,'kafka':'disabled','isolated_agents':'disabled','reconciler':'sweep every 5s, grace 600s; rebuild disabled','ports':ports,'mysql_port':mp,'redis_port':rp,'quick':quick,'started':datetime.datetime.now().isoformat(),'go_version':run(['go','version'],cwd=ROOT,capture_output=True).stdout.strip(),'sdkroot':'/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk','binary_sha256':{k:hashlib.sha256(pathlib.Path(v).read_bytes()).hexdigest() for k,v in binaries.items()}}
  config.update({'offered_rates':rates,'duration_seconds':duration,'repetitions':repetitions,'max_inflight':inflight,'recovery_wait_seconds':recovery_wait,'router_candidate':'unsafe_batch_test_only' if router_candidate=='1' else ('lock_scope_test_only' if router_lock_scope=='1' else ('deadline_poll_test_only' if router_deadline=='1' else 'current')),'router_candidate_sha256':hashlib.sha256(router_source.read_bytes()).hexdigest() if (router_candidate=='1' or router_lock_scope=='1' or router_deadline=='1') else None,'load_source_sha256':hashlib.sha256((ROOT/'eval/project-load-2026-11-02/open-load/main.go').read_bytes()).hexdigest(),'runner_sha256':hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest()})
  if router_fresh_scope=='1':config.update({'router_candidate':'fresh_lock_scope_test_only','router_candidate_sha256':hashlib.sha256(router_source.read_bytes()).hexdigest()})
  if router_refresh_scope=='1':config.update({'router_candidate':('refresh_metadata_test_only' if router_metadata=='1' else ('refresh_authority_test_only' if router_authority=='1' else ('refresh_deadline_test_only' if router_refresh_deadline=='1' else 'refresh_scope_test_only'))),'router_candidate_sha256':hashlib.sha256(router_source.read_bytes()).hexdigest(),'router_authority_check':router_authority=='1','router_authority_metadata':router_metadata=='1','router_refresh_deadline':router_refresh_deadline=='1'})
  config.update({'billing_require_reserve_v2':require_v2=='1','redis_restart_experiment':redis_restart=='1'})
  config.update({'hold_unknown': hold_unknown=='1', 'reconciler_no_auto_reverse': hold_unknown=='1'})
  config.update({'trace_mock': trace_mock=='1','trace_mock_source_sha256': hashlib.sha256(mock_path.read_bytes()).hexdigest() if trace_mock=='1' else None})
  config.update({'auth_cache_seconds':auth_cache_seconds,'auth_cache_source_sha256':hashlib.sha256((ROOT/'internal/billing/store/auth_cache.go').read_bytes()).hexdigest()})
  config.update({'pool_stats_overlay':pool_stats=='1','pool_stats_sha256':hashlib.sha256(main_candidate.read_bytes()).hexdigest() if pool_stats=='1' else None})
  config.update({'reserve_stages_overlay':reserve_stages=='1','reserve_stages_sha256':hashlib.sha256(candidate.read_bytes()).hexdigest() if reserve_stages=='1' else None})
  config.update({'projection_stages_overlay':projection_stages=='1','projection_stages_sha256':{p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in (cash_candidate,plan_candidate)} if projection_stages=='1' else None})
  config.update({'ledger_single_overlay':ledger_single=='1','ledger_single_sha256':hashlib.sha256((single_ledger_dir/'plan.go').read_bytes()).hexdigest() if ledger_single=='1' else None})
  config.update({'billing_candidate':'unknown_all_test_only' if unknown_store=='1' else 'current','billing_candidate_sha256':hashlib.sha256(billing_source.read_bytes()).hexdigest() if unknown_store=='1' else None})
  config.update({'legacy_lookup_overlay':legacy_lookup=='1','legacy_lookup_sha256':hashlib.sha256(legacy_source.read_bytes()).hexdigest() if legacy_lookup=='1' else None})
  config.update({'ignore_not_found_log_overlay':ignore_missing_log=='1','ignore_not_found_log_sha256':hashlib.sha256(logger_source.read_bytes()).hexdigest() if ignore_missing_log=='1' else None})
  config.update({'lock_timing_overlay':lock_timing=='1','lock_timing_sha256':hashlib.sha256(candidate.read_bytes()).hexdigest() if lock_timing=='1' else None})
  config.update({'auth_pool_overlay':auth_pool=='1','billing_pool_max':84 if auth_pool=='1' else 100,'auth_pool_max':16 if auth_pool=='1' else 0,'auth_pool_overlay_sha256':{n:hashlib.sha256((auth_candidate_dir/n).read_bytes()).hexdigest() for n in ['service.go','main.go']} if auth_pool=='1' else None})
  config.update({'auth_two_query_overlay':auth_two_query=='1','auth_two_query_sha256':hashlib.sha256((two_query_dir/'mysql.go').read_bytes()).hexdigest() if auth_two_query=='1' else None})
  config.update({'auth_single_overlay':auth_single=='1','auth_single_sha256':hashlib.sha256((single_dir/'mysql.go').read_bytes()).hexdigest() if auth_single=='1' else None,'browser_mix':browser_mix=='1','seed_source_sha256':hashlib.sha256((ROOT/'eval/project-load-2026-11-02/seed/main.go').read_bytes()).hexdigest()})
  config.update({'test_admission':admission=='1','test_admission_global':64 if admission=='1' else None,'test_admission_per_tenant':16 if admission=='1' else None,'test_admission_sha256':hashlib.sha256(admission_source.read_bytes()).hexdigest() if admission=='1' else None})
  config.update({'usage_retry_overlay':usage_retry=='1','usage_retry_sha256':hashlib.sha256((retry_candidate_dir/'chat.go').read_bytes()).hexdigest() if usage_retry=='1' else None})
  config['recon_early_usage_test_only']=recon_early=='1'
  config['recon_early_usage_sha256']=hashlib.sha256((early_dir/'reconciler.go').read_bytes()).hexdigest() if recon_early=='1' else None
  config['router_success_parallel_test_only']=router_success_parallel=='1'
  config['router_success_parallel_limit']=int(os.environ.get('TEST_ROUTER_SUCCESS_LIMIT','0')) if router_success_parallel=='1' else None
  config['router_report_global_limit']=int(os.environ.get('TEST_ROUTER_REPORT_GLOBAL','0')) if router_success_parallel=='1' else None
  config['router_cap_failures']=os.environ.get('TEST_ROUTER_CAP_FAILURES','0')=='1' if router_success_parallel=='1' else None
  config['router_publish_before_cap']=os.environ.get('TEST_ROUTER_PUBLISH_BEFORE_CAP','0')=='1' if router_success_parallel=='1' else None
  config['router_success_parallel_sha256']=hashlib.sha256(router_source.read_bytes()).hexdigest() if router_success_parallel=='1' else None
  config['test_usage_ack_loss_after_store']=http_usage_ack_loss=='1'
  config['stop_on_anomaly']=stop_on_anomaly=='1'
  config['test_load_usage_failure']=inject_load_usage=='1'
  config['test_load_usage_failure_sha256']=hashlib.sha256(injection_source.read_bytes()).hexdigest() if inject_load_usage=='1' else None
  config.update({'synthetic_recovery_fixture':synthetic_recovery=='1','http_recovery_injection':http_recovery=='1','http_recovery_failure_stage':'usage_after_commit_ack_loss' if http_usage_ack_loss=='1' else ('usage' if http_usage_failure=='1' else ('settle' if http_recovery=='1' else None)),'http_recovery_overlay_sha256':hashlib.sha256(injection_source.read_bytes()).hexdigest() if http_recovery=='1' else None})
  config['reconciler']=f'sweep every {recon_interval}s, grace 600s; rebuild disabled'
  (OUT/'config.json').write_text(json.dumps(config,ensure_ascii=False,indent=2))
  # Smoke actual authenticated request before load; its durable rows remain in audit.
  smoke=subprocess.run([binaries['load'],'-url',f'http://127.0.0.1:{ports["gateway"]}','-workers','16','-rate','5','-tenants','64','-seconds','1'],cwd=ROOT,env=base,capture_output=True,text=True,timeout=25,check=True);(OUT/'smoke.json').write_text(smoke.stdout);sm=json.loads(smoke.stdout)
  if sm['valid_success']<1 or sm['valid_success']!=sm['requests']:raise RuntimeError('smoke failed: '+smoke.stdout)
  cumulative_success=sm['valid_success']
  scenarios=[(False,64,rate) for rate in rates]
  stop_requested=False
  with open(OUT/'results.jsonl','w') as results,open(OUT/'resources.jsonl','w') as resources,open(OUT/'audits.jsonl','w') as audits:
   for stream,tenants,workers in scenarios:
    for rep in range(1,repetitions+1):
     if redis_restart=='1' and rep==2:
      run(['docker','kill',REDIS],capture_output=True,timeout=20)
     if redis_restart=='1' and rep==3:
      run(['docker','start',REDIS],capture_output=True,timeout=20)
      if port(REDIS,'6379/tcp')!=rp:raise RuntimeError('Redis published port changed after restart')
      for _ in range(100):
       ping=subprocess.run(['docker','exec',REDIS,'redis-cli','ping'],capture_output=True,text=True)
       if ping.returncode==0 and ping.stdout.strip()=='PONG':break
       time.sleep(.1)
      else:raise RuntimeError('Redis restart readiness timeout')
     args=[binaries['load'],'-url',f'http://127.0.0.1:{ports["gateway"]}','-workers',str(inflight),'-rate',str(workers),'-tenants',str(tenants),'-seconds',str(duration)]
     trace_path=OUT/f'trace-{tenants}-{workers}-{rep}.csv';args.extend(['-trace-file',str(trace_path)])
     if stream:args.append('-stream')
     if inject_load_usage=='1':args.extend(['-test-id-prefix','stop-test-'])
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
     if redis_restart=='1' and rep==2:
      ar={'redis_outage':True,'audit_deferred_until_restart':True,'rep':rep}
     else:
      traced=run([binaries['seed']],cwd=ROOT,env=dict(seedenv,TEST_MODE='trace',TEST_TRACE_PATH=str(trace_path)),capture_output=True,timeout=90);(OUT/f'trace-audit-{tenants}-{workers}-{rep}.json').write_text(traced.stdout)
      audit=subprocess.run([binaries['seed']],cwd=ROOT,env=dict(seedenv,TEST_MODE='audit'),capture_output=True,text=True,timeout=90); (OUT/f'audit-stderr-{tenants}-{workers}-{rep}.txt').write_text(audit.stderr); audit.check_returncode(); ar=json.loads(audit.stdout);ar['cumulative_valid_http']=cumulative_success;ar['settled_minus_valid_http']=ar['settled']-cumulative_success;ar.update({'workers':workers,'tenants':tenants,'stream':stream,'rep':rep});audits.write(json.dumps(ar)+'\n');audits.flush()
      if redis_restart=='1' and rep==3:
       prior_trace=OUT/f'trace-{tenants}-{workers}-2.csv'
       prior=run([binaries['seed']],cwd=ROOT,env=dict(seedenv,TEST_MODE='trace',TEST_TRACE_PATH=str(prior_trace)),capture_output=True,timeout=90);(OUT/f'trace-audit-{tenants}-{workers}-2-after-restart.json').write_text(prior.stdout)
     with urllib.request.urlopen(f'http://127.0.0.1:{ports["gateway"]}/metrics',timeout=10) as metric_response:
      (OUT/f'gateway-metrics-{tenants}-{workers}-{rep}.prom').write_bytes(metric_response.read())
     with urllib.request.urlopen(f'http://127.0.0.1:{ports["admin"]}/metrics',timeout=10) as metric_response:
      (OUT/f'billing-metrics-{tenants}-{workers}-{rep}.prom').write_bytes(metric_response.read())
     print(json.dumps({'result':row,'audit':ar},ensure_ascii=False),flush=True)
     if stop_on_anomaly=='1':
      trace_result=json.loads(traced.stdout)
      if trace_result.get('anomalies_truncated',0):raise RuntimeError('anomaly trace truncated')
      if trace_result.get('anomalies'):
       stop_requested=True
       (OUT/'stopped-on-anomaly.json').write_text(json.dumps({'trace':trace_path.name,'anomaly_count':len(trace_result['anomalies']),'at':datetime.datetime.now().astimezone().isoformat()},indent=2))
       print('stopped further load after anomalous request audit; beginning recovery observation',flush=True)
       break
    if stop_requested:break
  sql=run(['docker','exec',MYSQL,'mysql','-uroot','-pisolated','-N','-e',"SHOW GLOBAL STATUS WHERE Variable_name IN ('Max_used_connections','Threads_connected','Threads_running','Connection_errors_max_connections','Innodb_row_lock_waits','Innodb_row_lock_time'); SHOW VARIABLES LIKE 'max_connections'"],capture_output=True,timeout=30);(OUT/'mysql-final.txt').write_text(sql.stdout+sql.stderr)
  redis=run(['docker','exec',REDIS,'redis-cli','INFO'],capture_output=True,timeout=30);(OUT/'redis-final.txt').write_text(redis.stdout)
  if synthetic_recovery=='1':
   fixture=run([binaries['inject']],cwd=ROOT,env=env(TEST_BILLING_ADDR=f'127.0.0.1:{ports["billing"]}'),capture_output=True,timeout=60)
   fixture_path=OUT/'synthetic-recovery-fixture.json';fixture_path.write_text(fixture.stdout)
   fixture_ids=json.loads(fixture.stdout)
   fixture_env=dict(seedenv,TEST_MODE='fixture',TEST_FIXTURE_FILE=str(fixture_path))
   before_fixture=run([binaries['seed']],cwd=ROOT,env=fixture_env,capture_output=True,timeout=60)
   (OUT/'synthetic-recovery-before.json').write_text(before_fixture.stdout)
   snapshot=json.loads(before_fixture.stdout);used=snapshot['records'][fixture_ids['usage_request_id']];empty=snapshot['records'][fixture_ids['empty_request_id']]
   if snapshot['balance']!=snapshot['ledger'] or used['status']!='pending' or not used['usage_reported'] or used['events']!={'reserve':1,'usage':1,'settle':0,'reverse':0} or empty['status']!='pending' or empty['usage_reported'] or empty['events']!={'reserve':1,'usage':0,'settle':0,'reverse':0}:raise RuntimeError('synthetic initial fixture verification failed')
  if http_recovery=='1':
   key='recon-http-'+uuid.uuid4().hex
   target_id='idem:'+key
   body=json.dumps({'model':'test-model','messages':[{'role':'user','content':'hello load'}],'max_tokens':100,'stream':False}).encode()
   req=urllib.request.Request(f'http://127.0.0.1:{ports["gateway"]}/v1/chat/completions',data=body,method='POST',headers={'Content-Type':'application/json','Authorization':'Bearer sk-isolated-001','Idempotency-Key':key})
   with urllib.request.urlopen(req,timeout=30) as response:
    http_status=response.status; payload=json.loads(response.read())
   (OUT/'http-recovery-response.json').write_text(json.dumps({'request_id':target_id,'status':http_status,'body':payload},indent=2))
   with urllib.request.urlopen(f'http://127.0.0.1:{ports["gateway"]}/metrics',timeout=10) as metric_response:
    (OUT/'http-recovery-gateway-metrics.prom').write_bytes(metric_response.read())
   if http_status!=200 or not isinstance(payload.get('usage'),dict):raise RuntimeError('HTTP recovery response missing valid usage')
   target_path=OUT/'http-recovery-fixture.json';target_path.write_text(json.dumps({'usage_request_id':target_id,'empty_request_id':''}))
   target_env=dict(seedenv,TEST_MODE='target',TEST_FIXTURE_FILE=str(target_path))
   before=run([binaries['seed']],cwd=ROOT,env=target_env,capture_output=True,timeout=60)
   (OUT/'http-recovery-before.json').write_text(before.stdout)
   snapshot=json.loads(before.stdout); record=snapshot['records'][target_id]
   expected_usage=0 if http_usage_failure=='1' else 1
   if snapshot['balance']!=snapshot['ledger'] or record['status']!='pending' or record['usage_reported']!=bool(expected_usage) or record['settle_rows']!=0 or record['events']!={'reserve':1,'usage':expected_usage,'settle':0,'reverse':0}:raise RuntimeError('HTTP recovery initial verification failed')
  final=run([binaries['seed']],cwd=ROOT,env=dict(seedenv,TEST_MODE='audit'),capture_output=True,timeout=120);(OUT/'audit-before-wait.json').write_text(final.stdout)
  if probe_missing=='1':
   prior=json.loads(final.stdout)
   missing=prior.get('missing_balance_keys',[])
   probes=[]
   trace=OUT/'missing-probes.csv'
   tenants=[int(item.split()[0].split('=')[1]) for item in missing]
   if any(tenant<1 or tenant>64 for tenant in tenants):raise RuntimeError('invalid synthetic tenant ID')
   jobs=[(tenant,'probe-'+uuid.uuid4().hex) for tenant in tenants for _ in range(probe_per_tenant)]
   barrier=threading.Barrier(len(jobs)) if probe_per_tenant>1 and jobs else None
   def do_probe(job):
    tenant,key=job
    body=json.dumps({'model':'test-model','messages':[{'role':'user','content':'hello load'}],'max_tokens':100,'stream':False}).encode()
    req=urllib.request.Request(f'http://127.0.0.1:{ports["gateway"]}/v1/chat/completions',data=body,method='POST',headers={'Content-Type':'application/json','Authorization':f'Bearer sk-isolated-{tenant:03d}','Idempotency-Key':key})
    if barrier:barrier.wait(timeout=15)
    started=time.monotonic()
    try:
     with urllib.request.urlopen(req,timeout=15) as response:
      code=response.status;payload=json.loads(response.read());valid=code==200 and isinstance(payload.get('usage'),dict)
    except urllib.error.HTTPError as error:
     code=error.code;valid=False;error.close()
    except Exception:
     code='transport';valid=False
    return {'tenant':tenant,'request_id':'idem:'+key,'status':code,'valid':valid,'elapsed_ms':(time.monotonic()-started)*1000}
   if barrier:
    with concurrent.futures.ThreadPoolExecutor(max_workers=len(jobs)) as pool:probes=list(pool.map(do_probe,jobs))
   else:
    probes=[do_probe(job) for job in jobs]
   with trace.open('w',newline='') as output:
    writer=csv.writer(output);writer.writerow(['request_id','status','valid'])
    for probe in probes:writer.writerow([probe['request_id'],str(probe['status']),str(probe['valid']).lower()])
   (OUT/'missing-probes.json').write_text(json.dumps(probes,indent=2))
   if probes:
    traced=run([binaries['seed']],cwd=ROOT,env=dict(seedenv,TEST_MODE='trace',TEST_TRACE_PATH=str(trace)),capture_output=True,timeout=90);(OUT/'missing-probes-audit.json').write_text(traced.stdout)
  if recovery_wait:
   started=time.monotonic()
   while time.monotonic()-started<recovery_wait:
    for name,proc in processes:
     if proc.poll() is not None:raise RuntimeError(f'{name} exited during recovery wait: {proc.returncode}')
    time.sleep(max(0,min(5,recovery_wait-(time.monotonic()-started))))
   (OUT/'recovery-wait.json').write_text(json.dumps({'requested_seconds':recovery_wait,'elapsed_seconds':time.monotonic()-started}))
   with urllib.request.urlopen(f'http://127.0.0.1:{ports["reconciler"]}/status',timeout=10) as status_response:
    status_body=status_response.read()
    json.loads(status_body)
    (OUT/'reconciler-after-wait.json').write_bytes(status_body)
   if synthetic_recovery=='1':
    after_fixture=run([binaries['seed']],cwd=ROOT,env=fixture_env,capture_output=True,timeout=60)
    (OUT/'synthetic-recovery-after.json').write_text(after_fixture.stdout)
   if http_recovery=='1':
    after=run([binaries['seed']],cwd=ROOT,env=target_env,capture_output=True,timeout=60)
    (OUT/'http-recovery-after.json').write_text(after.stdout)
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
