#!/usr/bin/env python3
"""Measure MySQL query plans on synthetic, disposable outbox data only."""
import hashlib
import json
import pathlib
import subprocess
import time
import uuid

HERE = pathlib.Path(__file__).resolve().parent
ROOT = HERE.parents[2]
IMAGE = 'docker.m.daocloud.io/library/mysql:8.0.40'
NAME = 'outbox-plan-' + uuid.uuid4().hex[:12]
results = {'container': NAME, 'image': IMAGE, 'queries': []}

def mysql(sql):
    return subprocess.run(
        ['docker', 'exec', '-i', NAME, 'mysql', '-uroot', '-pisolated',
         '--database=isolated', '--batch', '--raw'],
        input=sql, text=True, capture_output=True, check=True).stdout

try:
    results['head'] = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    results['schema_sha256'] = hashlib.sha256((ROOT / 'scripts/schema.sql').read_bytes()).hexdigest()
    results['script_sha256'] = hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest()
    subprocess.run(['docker', 'run', '-d', '--name', NAME, '-e', 'MYSQL_ROOT_PASSWORD=isolated',
                    '-e', 'MYSQL_DATABASE=isolated', IMAGE], check=True, stdout=subprocess.DEVNULL)
    for _ in range(120):
        ready = subprocess.run(['docker', 'exec', NAME, 'mysqladmin', 'ping', '-h', '127.0.0.1',
                                '-uroot', '-pisolated', '--silent'], capture_output=True)
        if ready.returncode == 0:
            break
        time.sleep(.5)
    else:
        raise RuntimeError('isolated MySQL readiness timeout')
    results['image_id'] = subprocess.check_output(['docker', 'image', 'inspect', IMAGE, '--format', '{{.Id}}'], text=True).strip()
    schema = (ROOT / 'scripts/schema.sql').read_text().split('CREATE TABLE IF NOT EXISTS outbox_events (', 1)[1].split(';', 1)[0]
    mysql('CREATE TABLE outbox_events (' + schema + ';')
    # 100,000 rows, with nonuniform attempts to reflect possible retries. This
    # is synthetic and intentionally does not pretend to describe production.
    mysql('''SET SESSION cte_max_recursion_depth=100001;
        INSERT INTO outbox_events (id,topic,event_key,payload,created_at,sent_at,attempts,last_error)
        WITH RECURSIVE n AS (SELECT 1 AS id UNION ALL SELECT id+1 FROM n WHERE id<100000)
        SELECT id,'billing.events',CONCAT('synthetic:',id),JSON_OBJECT('id',id),NOW(3),
               IF(id % 100 = 0,NULL,NOW(3)),id % 7,'' FROM n;''')
    queries = {
        'current_poll': 'SELECT * FROM outbox_events WHERE sent_at IS NULL ORDER BY id ASC LIMIT 100',
        'fair_ceiling': 'SELECT COALESCE(MAX(id),0) FROM outbox_events WHERE sent_at IS NULL',
        'fair_first': 'SELECT * FROM outbox_events WHERE sent_at IS NULL AND id > 0 AND id <= 100000 ORDER BY id ASC LIMIT 100',
        'fair_middle': 'SELECT * FROM outbox_events WHERE sent_at IS NULL AND id > 50000 AND id <= 100000 ORDER BY id ASC LIMIT 100',
    }
    for distribution, update in [('sparse', None),
                                  ('dense', 'UPDATE outbox_events SET sent_at=IF(id % 10 = 0,NOW(3),NULL)')]:
        if update:
            mysql(update)
        for index, change in [('existing', None), ('candidate', 'CREATE INDEX idx_unsent_id ON outbox_events(sent_at,id)')]:
            if change:
                mysql(change)
            mysql('ANALYZE TABLE outbox_events')
            for label, query in queries.items():
                for iteration in range(1, 4):
                    output = mysql('EXPLAIN ANALYZE ' + query)
                    results['queries'].append({'distribution': distribution, 'index': index,
                                               'query': label, 'iteration': iteration, 'output': output})
            if index == 'candidate':
                mysql('DROP INDEX idx_unsent_id ON outbox_events')
    results['status'] = 'ok'
finally:
    subprocess.run(['docker', 'rm', '-f', NAME], capture_output=True)
    (HERE / 'outbox-plan-probe-20261104.json').write_text(json.dumps(results, ensure_ascii=False, indent=2) + '\n')
