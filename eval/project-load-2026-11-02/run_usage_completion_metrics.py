#!/usr/bin/env python3
"""Run test overlay against actual gateway handler; no external services."""
import hashlib, json, os, pathlib, subprocess, sys, tempfile
root=pathlib.Path(__file__).resolve().parents[2]
source=root/'internal/gateway/handler/usage_ack_test.go'
replacement=root/'eval/project-load-2026-11-02/usage_completion_metrics_test.go.txt'
if hashlib.sha256(source.read_bytes()).hexdigest() != '9f32dc876e4fa14b5c6c3df6f3cb920ec86ab35ee248e27220908cead31b0dae':
 raise SystemExit('baseline test changed; refresh the test overlay before running')
with tempfile.TemporaryDirectory(prefix='usage-completion-metrics-') as directory:
 overlay=pathlib.Path(directory)/'overlay.json'
 overlay.write_text(json.dumps({'Replace':{str(source):str(replacement)}}))
 env=dict(os.environ,SDKROOT='/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk',GOPROXY='off',GOTOOLCHAIN='local')
 result=subprocess.run(['go','test','-race','-count=1','-overlay',str(overlay),'./internal/gateway/handler','-run','^(TestUsageCompletionMetricsObserveOutcomes|TestUsageMustBeAcknowledgedBeforeSettle|TestChatLeavesPendingWhenActualUsageCannotBeRecorded)$','-v'],cwd=root,env=env)
 sys.exit(result.returncode)
