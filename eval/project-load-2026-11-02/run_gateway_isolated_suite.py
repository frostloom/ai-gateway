#!/usr/bin/env python3
"""Run gateway package tests with the hardcoded Redis fixture redirected."""
import json
import os
from pathlib import Path
import subprocess
import tempfile

root = Path(__file__).resolve().parents[2]
address = os.environ['COMPARISON_REDIS_ADDR']
if not address.startswith('127.0.0.1:') or '\n' in address:
    raise SystemExit('expected loopback Redis address from isolated harness')
original = root / 'internal/gateway/middleware/auth_rate_test.go'
source = original.read_text()
old = '127.0.0.1:6381'
if source.count(old) != 1:
    raise SystemExit('fixture no longer has the expected single hardcoded address')
with tempfile.TemporaryDirectory(prefix='gateway-suite-overlay-') as directory:
    directory = Path(directory)
    replacement = directory / original.name
    replacement.write_text(source.replace(old, address))
    overlay = directory / 'overlay.json'
    overlay.write_text(json.dumps({'Replace': {str(original): str(replacement)}}))
    command = ['go', 'test', '-race', '-overlay', str(overlay), './internal/gateway/client', './internal/gateway/middleware', './internal/gateway/handler', './internal/pkg/controlplane', '-count=1', '-v']
    raise SystemExit(subprocess.run(command, cwd=root).returncode)
