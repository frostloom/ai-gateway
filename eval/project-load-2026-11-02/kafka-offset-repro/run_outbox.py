#!/usr/bin/env python3
"""Run outbox fault injection only against a disposable loopback MySQL."""
import hashlib,json,os,pathlib,subprocess,sys,tempfile,time,uuid
root=pathlib.Path(__file__).resolve().parents[3];here=pathlib.Path(__file__).resolve().parent
name='outbox-dlq-'+uuid.uuid4().hex[:12]; redis=name+'-redis';image='docker.m.daocloud.io/library/mysql:8.0.40'
try:
 subprocess.run(['docker','run','-d','--name',name,'-e','MYSQL_ROOT_PASSWORD=isolated','-e','MYSQL_DATABASE=isolated','-p','127.0.0.1::3306',image],check=True,stdout=subprocess.DEVNULL)
 subprocess.run(['docker','run','-d','--name',redis,'-p','127.0.0.1::6379','docker.m.daocloud.io/library/redis:7-alpine'],check=True,stdout=subprocess.DEVNULL)
 for _ in range(120):
  if subprocess.run(['docker','exec',name,'mysqladmin','ping','-h','127.0.0.1','-uroot','-pisolated','--silent'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0:break
  time.sleep(.5)
 else:raise RuntimeError('MySQL readiness timeout')
 port=subprocess.check_output(['docker','port',name,'3306/tcp'],text=True).strip().split(':')[-1]
 redisport=subprocess.check_output(['docker','port',redis,'6379/tcp'],text=True).strip().split(':')[-1]
 env=dict(os.environ,ISOLATED_MYSQL_DSN=f'root:isolated@tcp(127.0.0.1:{port})/isolated?charset=utf8mb4&parseTime=true&loc=Local',REDIS_ADDR=f'127.0.0.1:{redisport}',GOPROXY='off',GOTOOLCHAIN='local',SDKROOT='/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk')
 with tempfile.TemporaryDirectory(prefix='outbox-overlay-') as tmp:
  overlay=pathlib.Path(tmp)/'overlay.json'; replacements={str(root/'internal/billing/outbox/isolated_dlq_test.go'):str(here/'outbox_dlq_test.go.txt')}
  if '--fair' in sys.argv[1:]: replacements[str(root/'internal/billing/outbox/isolated_fair_test.go')]=str(here/'fair_scan_test.go.txt')
  if '--candidate' in sys.argv[1:]: replacements[str(root/'internal/billing/outbox/relay.go')]=str(here/'relay_parallel_candidate.go.txt')
  overlay.write_text(json.dumps({'Replace':replacements}))
  selected='^TestIsolatedFairSweep(Recovery|LateCommit)$' if '--fair' in sys.argv[1:] else '^TestIsolated'
  cmd=['go','test']+(['-race'] if '--race' in sys.argv[1:] else [])+['-count=1','-v','-overlay',str(overlay),'./internal/billing/outbox','-run',selected]
  p=subprocess.run(cmd,cwd=root,env=env,text=True,capture_output=True,timeout=120)
  source=(root/'internal/billing/store/reserve_test.go').read_text(); old='root:root@tcp(127.0.0.1:3307)/'; assert source.count(old)==1
  adapted=pathlib.Path(tmp)/'reserve_test.go';adapted.write_text(source.replace(old,f'root:isolated@tcp(127.0.0.1:{port})/'))
  overlay.write_text(json.dumps({'Replace':{str(root/'internal/billing/store/reserve_test.go'):str(adapted)}}))
  q=subprocess.run(['go','test','-count=1','-v','-overlay',str(overlay),'./internal/billing/store','-run','^TestOutboxLifecycle$'],cwd=root,env=env,text=True,capture_output=True,timeout=120)
 label=sys.argv[1] if len(sys.argv)>1 else 'outbox-result'
 if not label.replace('-','').isalnum():raise ValueError('invalid label')
 paths=['internal/billing/outbox/relay.go','internal/billing/store/outbox.go']
 data={'head':subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip(),'mysql_image_id':subprocess.check_output(['docker','image','inspect',image,'--format','{{.Id}}'],text=True).strip(),'source_sha256':{f:hashlib.sha256((root/f).read_bytes()).hexdigest() for f in paths},'test_sha256':hashlib.sha256((here/('fair_scan_test.go.txt' if '--fair' in sys.argv[1:] else 'outbox_dlq_test.go.txt')).read_bytes()).hexdigest(),'returncode':p.returncode,'stdout':p.stdout,'stderr':p.stderr,'lifecycle':{'returncode':q.returncode,'stdout':q.stdout,'stderr':q.stderr,'test_sha256':hashlib.sha256((root/'internal/billing/store/outbox_test.go').read_bytes()).hexdigest()}}
 (here/(label+'.json')).write_text(json.dumps(data,ensure_ascii=False,indent=2)+'\n');print(json.dumps(data,ensure_ascii=False,indent=2));sys.exit(p.returncode or q.returncode)
finally:
 subprocess.run(['docker','rm','-f',name,redis],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
