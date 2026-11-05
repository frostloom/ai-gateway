#!/usr/bin/env python3
import json,os,pathlib,subprocess,tempfile
root=pathlib.Path(__file__).resolve().parents[2];fixture=root/'internal/gateway/handler/chat_stream_test.go';test=(root/'eval/project-load-2026-11-02/stream_memory_test.go.txt').read_text()
with tempfile.TemporaryDirectory(prefix='nonstream-error-') as tmp:
 p=pathlib.Path(tmp); f=p/'chat_stream_test.go'; f.write_text(fixture.read_text().replace('"context"','"context"\n "runtime"\n "sync"\n "sync/atomic"',1)+test)
 overlay=p/'overlay.json';overlay.write_text(json.dumps({'Replace':{str(fixture):str(f)}}));env=dict(os.environ,SDKROOT='/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk',GOPROXY='off',GOTOOLCHAIN='local')
 result=subprocess.run(['go','test','-race','-overlay',str(overlay),'./internal/gateway/handler','-run','^TestConservativeStreamDoesNotRetainForwardedText$','-count=3','-v'],cwd=root,env=env)
 raise SystemExit(result.returncode)
