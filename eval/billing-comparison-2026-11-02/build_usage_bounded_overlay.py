#!/usr/bin/env python3
"""Test-only bounded retry admission layered on the jitter experiment."""
import pathlib,subprocess,sys
root=pathlib.Path(__file__).resolve().parents[2];out=pathlib.Path(sys.argv[1]);out.mkdir(parents=True,exist_ok=True)
subprocess.run([sys.executable,str(root/'eval/billing-comparison-2026-11-02/build_usage_jitter_overlay.py'),str(out)],check=True)
p=out/'chat.go';s=p.read_text();anchor='// settleAndLog ReportUsage + Settle（独立后台 ctx）。'
helper='''// Test-only per-process retry limits; not a distributed or durable queue.
var usageRetryActive = make(chan struct{}, 16)
var usageRetryWaiting = make(chan struct{}, 64)

func acquireUsageRetry(ctx context.Context) bool {
	select {
	case usageRetryWaiting <- struct{}{}:
	default:
		return false
	}
	defer func(){ <-usageRetryWaiting }()
	select {
	case usageRetryActive <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

'''
old='''		attemptCtx, cancel := context.WithTimeout(settleCtx, 3*time.Second)
		response, err := b.ReportUsage(attemptCtx, usageReq)
		cancel()'''
new='''		if attempt > 0 && !acquireUsageRetry(settleCtx) {
			errU = status.Error(codes.ResourceExhausted, "usage retry admission unavailable")
			break
		}
		attemptCtx, cancel := context.WithTimeout(settleCtx, 3*time.Second)
		response, err := func() (*billingv1.ReportUsageResponse, error) {
			if attempt > 0 { defer func(){ <-usageRetryActive }() }
			return b.ReportUsage(attemptCtx, usageReq)
		}()
		cancel()'''
if s.count(anchor)!=1 or s.count(old)!=1:raise ValueError('bounded retry overlay anchor changed')
p.write_text(s.replace(anchor,helper+anchor,1).replace(old,new,1))
