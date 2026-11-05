#!/usr/bin/env python3
import json,os,pathlib,subprocess,sys,tempfile
root=pathlib.Path(__file__).resolve().parents[2];fixture=root/'internal/gateway/handler/chat_stream_test.go';tests=(root/'eval/project-load-2026-11-02/stream_memory_test.go.txt').read_text()+(root/'eval/project-load-2026-11-02/stream_long_line_test.go.txt').read_text()
with tempfile.TemporaryDirectory(prefix='stream-memory-') as tmp:
 p=pathlib.Path(tmp);f=p/'chat_stream_test.go';f.write_text(fixture.read_text().replace('"context"','"context"\n "runtime"\n "sync"\n "sync/atomic"',1)+tests)
 replace={str(fixture):str(f)};env=dict(os.environ,SDKROOT='/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk',GOPROXY='off',GOTOOLCHAIN='local',GIN_MODE='release')
 if '--baseline' in sys.argv:replace[str(root/'internal/gateway/handler/chat.go')]=str(root/'eval/project-load-2026-11-02/gateway_stream_memory_baseline.go.txt');env['TEST_STREAM_BASELINE']='1'
 overlay=p/'overlay.json';overlay.write_text(json.dumps({'Replace':replace}));result=subprocess.run(['go','test','-race','-overlay',str(overlay),'./internal/gateway/handler','-run','^TestConservativeLongUnterminatedLineCharacterization$','-count=3','-v'],cwd=root,env=env);raise SystemExit(result.returncode)
