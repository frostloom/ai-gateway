#!/usr/bin/env python3
"""Only test overlays and local httptest servers; no production services."""
import json,os,pathlib,subprocess,sys,tempfile
root=pathlib.Path(__file__).resolve().parents[2]
source=root/'internal/gateway/handler/chat_stream_test.go'
test=(root/'eval/project-load-2026-11-02/gateway_report_identity_regression.go.txt').read_text()
with tempfile.TemporaryDirectory(prefix='gateway-identity-') as tmp:
 p=pathlib.Path(tmp);fixture=p/'chat_stream_test.go'
 fixture.write_text(source.read_text().replace('"context"','"context"\n "encoding/json"\n "fmt"\n "sync"',1)+test)
 replace={str(source):str(fixture)}
 if '--candidate' in sys.argv:
  for original,candidate in [('internal/gateway/handler/chat.go','eval/project-load-2026-11-02/gateway_chat_identity_candidate.go.txt'),('internal/gateway/client/router.go','eval/project-load-2026-11-02/gateway_router_identity_candidate.go.txt')]:replace[str(root/original)]=str(root/candidate)
 overlay=p/'overlay.json';overlay.write_text(json.dumps({'Replace':replace}))
 env=dict(os.environ,SDKROOT='/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk',GOPROXY='off',GOTOOLCHAIN='local')
 result=subprocess.run(['go','test','-race','-overlay',str(overlay),'./internal/gateway/handler','-run','^TestGatewayReportIdentityAcrossFailover$','-count=1','-v'],cwd=root,env=env)
 raise SystemExit(result.returncode)
