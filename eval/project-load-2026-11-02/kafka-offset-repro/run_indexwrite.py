#!/usr/bin/env python3
"""Run the write probe only on a disposable local MySQL container."""
import hashlib, json, os, pathlib, subprocess, time, uuid

here = pathlib.Path(__file__).resolve().parent
root = here.parents[2]
image = 'docker.m.daocloud.io/library/mysql:8.0.40'
name = 'outbox-write-' + uuid.uuid4().hex[:12]
result = {'container': name, 'image': image}
try:
    result['head'] = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=root, text=True).strip()
    result['source_sha256'] = hashlib.sha256((here / 'indexwrite/main.go').read_bytes()).hexdigest()
    result['runner_sha256'] = hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest()
    subprocess.run(['docker', 'run', '-d', '--name', name, '-e', 'MYSQL_ROOT_PASSWORD=isolated',
                    '-e', 'MYSQL_DATABASE=isolated', '-p', '127.0.0.1::3306', image], check=True, stdout=subprocess.DEVNULL)
    for _ in range(120):
        p = subprocess.run(['docker', 'exec', name, 'mysqladmin', 'ping', '-h', '127.0.0.1', '-uroot', '-pisolated', '--silent'], capture_output=True)
        if p.returncode == 0: break
        time.sleep(.5)
    else: raise RuntimeError('MySQL readiness timeout')
    port = subprocess.check_output(['docker', 'port', name, '3306/tcp'], text=True).strip().split(':')[-1]
    result['image_id'] = subprocess.check_output(['docker', 'image', 'inspect', image, '--format', '{{.Id}}'], text=True).strip()
    env = dict(os.environ, ISOLATED_MYSQL_DSN=f'root:isolated@tcp(127.0.0.1:{port})/isolated?parseTime=true', GOPROXY='off', GOTOOLCHAIN='local', SDKROOT='/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk')
    p = subprocess.run(['go', 'run', './eval/project-load-2026-11-02/kafka-offset-repro/indexwrite'], cwd=root, env=env, text=True, capture_output=True, timeout=180)
    result.update(returncode=p.returncode, stdout=p.stdout, stderr=p.stderr)
finally:
    subprocess.run(['docker', 'rm', '-f', name], capture_output=True)
    (here / 'outbox-indexwrite-20261104.json').write_text(json.dumps(result, ensure_ascii=False, indent=2) + '\n')
if result.get('returncode') != 0: raise SystemExit(1)
