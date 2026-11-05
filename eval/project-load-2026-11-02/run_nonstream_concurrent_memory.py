#!/usr/bin/env python3
"""Local handler/fake billing experiment; --candidate uses a temporary overlay."""
import json
import os
import pathlib
import subprocess
import sys
import tempfile

root = pathlib.Path(__file__).resolve().parents[2]
if '--candidate' in sys.argv and '--gate' in sys.argv:
    raise SystemExit('choose at most one test candidate')
fixture = root / 'internal/gateway/handler/chat_stream_test.go'
test = root / 'eval/project-load-2026-11-02/nonstream_concurrent_memory_test.go.txt'
with tempfile.TemporaryDirectory(prefix='nonstream-memory-') as temp:
    tmp = pathlib.Path(temp)
    target = tmp / 'chat_stream_test.go'
    target.write_text(fixture.read_text().replace('"context"', '"context"\n "bytes"\n "os"\n "runtime"\n "sort"\n "sync"', 1) + test.read_text())
    replacements = {str(fixture): str(target)}
    if '--candidate' in sys.argv:
        subprocess.run([sys.executable, str(root / 'eval/billing-comparison-2026-11-02/build_nonstream_limit_overlay.py'), str(tmp)], check=True)
        replacements.update(json.loads((tmp / 'overlay.json').read_text())['Replace'])
    (tmp / 'combined.json').write_text(json.dumps({'Replace': replacements}))
    env = dict(os.environ, SDKROOT='/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk', GOPROXY='off', GOTOOLCHAIN='local', NONSTREAM_MEMORY_LIMITED='1' if '--candidate' in sys.argv else '0', NONSTREAM_MEMORY_GATE='1' if '--gate' in sys.argv else '0')
    args = ['go', 'test'] + (['-race'] if '--race' in sys.argv else []) + ['-overlay', str(tmp / 'combined.json'), './internal/gateway/handler', '-run', '^TestNonstreamConcurrentMemoryCharacterization$', '-count=1' if '--race' in sys.argv else '-count=3', '-v']
    raise SystemExit(subprocess.run(args, cwd=root, env=env).returncode)
