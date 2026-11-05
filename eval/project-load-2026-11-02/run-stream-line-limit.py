#!/usr/bin/env python3
import json,os,pathlib,subprocess,sys,tempfile
root=pathlib.Path(__file__).resolve().parents[2];fixture=root/'internal/gateway/handler/chat_stream_test.go';test=(root/'eval/project-load-2026-11-02/stream_line_limit_test.go.txt').read_text()
with tempfile.TemporaryDirectory(prefix='line-limit-') as tmp:
 p=pathlib.Path(tmp);f=p/'chat_stream_test.go';f.write_text(fixture.read_text().replace('"context"','"context"\n "fmt"',1)+test);replace={str(fixture):str(f)}
 if '--candidate' in sys.argv:replace[str(root/'internal/gateway/handler/chat.go')]=str(root/'eval/project-load-2026-11-02/gateway_line_limit_candidate.go.txt')
 overlay=p/'overlay.json';overlay.write_text(json.dumps({'Replace':replace}));env=dict(os.environ,SDKROOT='/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk',GOPROXY='off',GOTOOLCHAIN='local',GIN_MODE='release')
 result=subprocess.run(['go','test','-race','-overlay',str(overlay),'./internal/gateway/handler','-run','^TestStreamLineLimit','-count=3','-v'],cwd=root,env=env);raise SystemExit(result.returncode)
