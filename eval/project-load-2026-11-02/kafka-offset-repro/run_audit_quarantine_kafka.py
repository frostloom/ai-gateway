#!/usr/bin/env python3
"""Real consumer + disposable Kafka/MySQL; no existing service is touched."""
import hashlib, json, os, pathlib, socket, subprocess, sys, tempfile, time, uuid
root=pathlib.Path(__file__).resolve().parents[3]; here=pathlib.Path(__file__).resolve().parent
name='audit-quarantine-'+uuid.uuid4().hex[:12]; kafka=name+'-kafka'; mysql=name+'-mysql'
def run(args,**kw): return subprocess.run(args,check=True,**kw)
with socket.socket() as s: s.bind(('127.0.0.1',0)); kp=s.getsockname()[1]
env={'KAFKA_NODE_ID':'1','KAFKA_PROCESS_ROLES':'broker,controller','KAFKA_CONTROLLER_QUORUM_VOTERS':'1@127.0.0.1:9093','KAFKA_LISTENERS':'PLAINTEXT://0.0.0.0:9092,INTERNAL://0.0.0.0:9094,CONTROLLER://0.0.0.0:9093','KAFKA_ADVERTISED_LISTENERS':f'PLAINTEXT://127.0.0.1:{kp},INTERNAL://127.0.0.1:9094','KAFKA_INTER_BROKER_LISTENER_NAME':'INTERNAL','KAFKA_LISTENER_SECURITY_PROTOCOL_MAP':'CONTROLLER:PLAINTEXT,PLAINTEXT:PLAINTEXT,INTERNAL:PLAINTEXT','KAFKA_CONTROLLER_LISTENER_NAMES':'CONTROLLER','KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR':'1','KAFKA_GROUP_INITIAL_REBALANCE_DELAY_MS':'0','KAFKA_AUTO_CREATE_TOPICS_ENABLE':'false'}
try:
 run(['docker','run','-d','--name',mysql,'-e','MYSQL_ROOT_PASSWORD=isolated','-e','MYSQL_DATABASE=isolated','-p','127.0.0.1::3306','docker.m.daocloud.io/library/mysql:8.0.40'],stdout=subprocess.DEVNULL)
 args=['docker','run','-d','--name',kafka,'-p',f'127.0.0.1:{kp}:9092']
 for k,v in env.items():args+=['-e',k+'='+v]
 run(args+['docker.m.daocloud.io/apache/kafka:3.7.0'],stdout=subprocess.DEVNULL)
 for _ in range(100):
  my=subprocess.run(['docker','exec',mysql,'mysqladmin','ping','-h','127.0.0.1','-uroot','-pisolated','--silent'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
  try: ka=subprocess.run(['docker','exec',kafka,'/opt/kafka/bin/kafka-topics.sh','--bootstrap-server','127.0.0.1:9094','--list'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=10)
  except subprocess.TimeoutExpired: time.sleep(1); continue
  if my.returncode==ka.returncode==0:break
  time.sleep(1)
 else:raise RuntimeError('readiness timeout')
 run(['docker','exec',kafka,'/opt/kafka/bin/kafka-topics.sh','--bootstrap-server','127.0.0.1:9094','--create','--topic','audit-quarantine-test','--partitions','1','--replication-factor','1'],capture_output=True)
 port=subprocess.check_output(['docker','port',mysql,'3306/tcp'],text=True).strip().split(':')[-1]
 runenv=dict(os.environ,ISOLATED_MYSQL_DSN=f'root:isolated@tcp(127.0.0.1:{port})/isolated?charset=utf8mb4&parseTime=true&loc=Local',ISOLATED_KAFKA_ADDR=f'127.0.0.1:{kp}',GOPROXY='off',GOTOOLCHAIN='local',SDKROOT='/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk')
 cmd=['go','test','-race','-count=1','-v','./eval/billing-comparison-2026-11-02','-run','^TestAuditQuarantineKafkaReplay$']
 p=subprocess.run(cmd,cwd=root,env=runenv,text=True,capture_output=True,timeout=180)

 label=sys.argv[1] if len(sys.argv)>1 else 'result'
 if not label.replace('-','').isalnum(): raise ValueError('invalid label')
 files=['internal/billing/consumer/consumer.go','internal/billing/outbox/relay.go','internal/billing/store/outbox.go','internal/pkg/kafka/consumer.go','internal/pkg/kafka/producer.go']
 data={'head':subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip(),'source_sha256':{f:hashlib.sha256((root/f).read_bytes()).hexdigest() for f in files},'test_sha256':hashlib.sha256((root/'eval/billing-comparison-2026-11-02/audit_quarantine_kafka_test.go').read_bytes()).hexdigest(),'returncode':p.returncode,'stdout':p.stdout,'stderr':p.stderr}
 (here/(label+'.json')).write_text(json.dumps(data,ensure_ascii=False,indent=2)+'\n');print(json.dumps(data,ensure_ascii=False,indent=2));sys.exit(p.returncode)
finally:
 subprocess.run(['docker','rm','-f',mysql,kafka],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
