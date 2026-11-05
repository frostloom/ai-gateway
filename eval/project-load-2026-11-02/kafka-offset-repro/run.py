#!/usr/bin/env python3
"""Run Kafka offset reproduction against a disposable, loopback-only broker."""
import hashlib, json, os, pathlib, socket, subprocess, time, uuid
root = pathlib.Path(__file__).resolve().parents[3]
out = pathlib.Path(__file__).resolve().parent
name = 'offset-repro-' + uuid.uuid4().hex[:12]
image = 'docker.m.daocloud.io/apache/kafka:3.7.0'
with socket.socket() as s:
    s.bind(('127.0.0.1', 0)); port = s.getsockname()[1]
env = {'KAFKA_NODE_ID':'1','KAFKA_PROCESS_ROLES':'broker,controller','KAFKA_CONTROLLER_QUORUM_VOTERS':'1@127.0.0.1:9093','KAFKA_LISTENERS':'PLAINTEXT://0.0.0.0:9092,INTERNAL://0.0.0.0:9094,CONTROLLER://0.0.0.0:9093','KAFKA_ADVERTISED_LISTENERS':f'PLAINTEXT://127.0.0.1:{port},INTERNAL://127.0.0.1:9094','KAFKA_INTER_BROKER_LISTENER_NAME':'INTERNAL','KAFKA_LISTENER_SECURITY_PROTOCOL_MAP':'CONTROLLER:PLAINTEXT,PLAINTEXT:PLAINTEXT,INTERNAL:PLAINTEXT','KAFKA_CONTROLLER_LISTENER_NAMES':'CONTROLLER','KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR':'1','KAFKA_GROUP_INITIAL_REBALANCE_DELAY_MS':'0','KAFKA_AUTO_CREATE_TOPICS_ENABLE':'false'}
args = ['docker','run','-d','--name',name,'-p',f'127.0.0.1:{port}:9092']
for k,v in env.items(): args += ['-e',k+'='+v]
try:
    subprocess.run(args+[image],check=True,stdout=subprocess.DEVNULL)
    for _ in range(90):
        p = subprocess.run(['docker','exec',name,'/opt/kafka/bin/kafka-topics.sh','--bootstrap-server','127.0.0.1:9094','--list'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=10)
        if p.returncode == 0: break
        time.sleep(1)
    else: raise RuntimeError('broker readiness timeout')
    subprocess.run(['docker','exec',name,'/opt/kafka/bin/kafka-topics.sh','--bootstrap-server','127.0.0.1:9094','--create','--topic','offset-repro','--partitions','1','--replication-factor','1'],check=True,capture_output=True)
    runenv = dict(os.environ, ISOLATED_KAFKA_ADDR=f'127.0.0.1:{port}', GOPROXY='off', GOTOOLCHAIN='local', SDKROOT='/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk')
    p = subprocess.run(['go','run','./eval/project-load-2026-11-02/kafka-offset-repro'],cwd=root,env=runenv,text=True,capture_output=True,timeout=100)
    data={'head':subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip(),'image_id':subprocess.check_output(['docker','image','inspect',image,'--format','{{.Id}}'],text=True).strip(),'go_program_sha256':hashlib.sha256((out/'main.go').read_bytes()).hexdigest(),'returncode':p.returncode,'stdout':p.stdout,'stderr':p.stderr}
    (out/'result.json').write_text(json.dumps(data,ensure_ascii=False,indent=2)+'\n')
    print(json.dumps(data,ensure_ascii=False,indent=2))
    if p.returncode: raise SystemExit(p.returncode)
finally:
    subprocess.run(['docker','rm','-f',name],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
