#!/usr/bin/env python3
"""Real loopback HTTP, fake billing and router; no external services."""
import json,os,pathlib,subprocess,sys,tempfile
root=pathlib.Path(__file__).resolve().parents[2];fixture=root/'internal/gateway/handler/chat_stream_test.go';test=(root/'eval/project-load-2026-11-02/upstream_truncated_safety_test.go.txt').read_text()
with tempfile.TemporaryDirectory(prefix='upstream-unknown-') as tmp:
 temp=pathlib.Path(tmp);testfile=temp/'chat_stream_test.go';testfile.write_text(fixture.read_text()+test)
 replace={str(fixture):str(testfile)}
 if '--candidate' in sys.argv:replace[str(root/'internal/gateway/handler/chat.go')]=str(root/'eval/project-load-2026-11-02/gateway_truncated_candidate.go.txt')
 overlay=temp/'overlay.json';overlay.write_text(json.dumps({'Replace':replace}));env=dict(os.environ,SDKROOT='/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk',GOPROXY='off',GOTOOLCHAIN='local')
 args=['go','test','-race','-overlay',str(overlay),'./internal/gateway/handler']
 if '--suite' not in sys.argv:args+=['-run','^Test(TruncatedNonStreamMustNotSettleOrReverse|CompleteNonStreamWithoutUsageStillSucceeds|MalformedNonStreamMustNotSettleOrReverse|NonStreamErrorResponseCompleteness|StreamContentContainingDoneDoesNotEndStream|StreamDoneTextInCommentDoesNotEndStream|StreamFinalLineWithoutNewline)$']
 result=subprocess.run(args+['-count=1','-v'],cwd=root,env=env)
 raise SystemExit(result.returncode)
