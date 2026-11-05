#!/usr/bin/env python3
"""Temporary overlay; loopback upstream and fake billing/router only."""
import json, os, pathlib, subprocess, sys, tempfile
root = pathlib.Path(__file__).resolve().parents[2]
fixture = root / 'internal/gateway/handler/chat_stream_test.go'
source = root / 'internal/gateway/handler/chat.go'
test = (root / 'eval/project-load-2026-11-02/nonstream_body_limit_test.go.txt').read_text()
with tempfile.TemporaryDirectory(prefix='body-limit-') as d:
    temp = pathlib.Path(d)
    tests = temp / 'chat_stream_test.go'
    tests.write_text(fixture.read_text().replace('"context"', '"context"\n "fmt"\n "compress/gzip"', 1) + test)
    replacements = {str(fixture): str(tests)}
    if '--candidate' in sys.argv:
        original = source.read_text()
        old = '\t\tbody, readErr := io.ReadAll(upResp.Body)\n\t\tupResp.Body.Close()'
        new = '\t\tbody, readErr := io.ReadAll(io.LimitReader(upResp.Body, (1<<20)+1))\n\t\tupResp.Body.Close()\n\t\tif len(body) > 1<<20 {\n\t\t\tlog.Error("upstream response exceeds experimental limit", "request_id", requestID)\n\t\t\tabort(c, http.StatusBadGateway, "upstream response too large")\n\t\t\treturn\n\t\t}'
        if original.count(old) != 1:
            raise RuntimeError('unexpected source; overlay not applied')
        candidate = temp / 'chat.go'
        candidate.write_text(original.replace(old, new, 1))
        replacements[str(source)] = str(candidate)
    overlay = temp / 'overlay.json'
    overlay.write_text(json.dumps({'Replace': replacements}))
    env = dict(os.environ, SDKROOT='/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk', GOPROXY='off', GOTOOLCHAIN='local')
    args = ['go', 'test', '-race', '-overlay', str(overlay), './internal/gateway/handler', '-count=3', '-v']
    if '--suite' not in sys.argv:
        args += ['-run', '^TestExperimental(Compressed)?NonStreamBodyLimit$']
    raise SystemExit(subprocess.run(args, cwd=root, env=env).returncode)
