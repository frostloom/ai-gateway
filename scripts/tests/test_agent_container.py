"""Isolated Docker checks. Never operates on the existing platform Compose project."""
import argparse,json,subprocess,uuid
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
def call(*args, capture=False):
    return subprocess.run(list(args),cwd=ROOT,check=True,text=True,stdout=subprocess.PIPE if capture else None,timeout=300).stdout
def validate():
    config=json.loads(call('docker','compose','-f','deploy/agent/compose.yml','--profile','knowledge','config','--format','json',capture=True))
    agent=config['services']['agent-knowledge']
    assert agent['read_only'] and agent['user']=='65532:65532'
    assert agent['environment']['AGENT_KNOWLEDGE_ENABLED']=='false'
    assert not agent.get('volumes'), 'runtime corpus must be immutable image data'
    print('PASS compose isolation/defaults')
def invalid_release():
    p=subprocess.run(['docker','run','--rm','--read-only','-e','CONTROL_PLANE_TOKEN=smoke-control','-e','AGENT_KNOWLEDGE_ENABLED=true','-e','AGENT_KNOWLEDGE_DIR=/missing','ai-gateway-agent-knowledge:smoke'],cwd=ROOT,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=20)
    assert p.returncode!=0 and 'knowledge initialization failed' in p.stdout, p.stdout
    print('PASS invalid release fails before database connection')
if __name__=='__main__':
    p=argparse.ArgumentParser();p.add_argument('--validate',action='store_true');p.add_argument('--smoke',action='store_true');a=p.parse_args();validate()
    if a.smoke:
        project='knowledge-smoke-'+uuid.uuid4().hex[:10]
        cmd=['docker','compose','-p',project,'-f','deploy/agent/compose-smoke.yml']
        try:
            call(*cmd,'up','-d','--build','--wait','--wait-timeout','180')
            call(*cmd,'exec','-T','fixture','/app/knowledge-smoke','-probe','http://agent-knowledge:9105/chat')
            call(*cmd,'exec','-T','agent-knowledge','/app/knowledge-smoke','-check-readonly')
            invalid_release()
            print('PASS isolated container knowledge smoke')
        except Exception:
            try:
                call(*cmd,'exec','-T','agent-knowledge','/app/knowledge-smoke','-network-probe')
            except Exception:
                pass
            call(*cmd,'logs','--no-color','--tail','60')
            raise
        finally:
            call(*cmd,'down','--volumes','--remove-orphans')
