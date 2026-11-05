#!/usr/bin/env python3
"""Test-only gateway usage retry; do not modify production source."""
import json,pathlib,sys
root=pathlib.Path(__file__).resolve().parents[2];out=pathlib.Path(sys.argv[1]);out.mkdir(parents=True,exist_ok=True)
p=root/'internal/gateway/handler/chat.go';s=p.read_text()
old='''	_, errU := b.ReportUsage(settleCtx, &billingv1.ReportUsageRequest{
		RequestId: requestID, BillId: billID, PromptTokens: prompt, CompletionTokens: completion,
		ProviderId: providerID,
	})'''
new='''	usageReq := &billingv1.ReportUsageRequest{
		RequestId: requestID, BillId: billID, PromptTokens: prompt, CompletionTokens: completion,
		ProviderId: providerID,
	}
	var errU error
	for attempt := 0; attempt < 2; attempt++ {
		attemptCtx, cancel := context.WithTimeout(settleCtx, 3*time.Second)
		response, err := b.ReportUsage(attemptCtx, usageReq)
		cancel()
		errU = err
		if err == nil {
			if response == nil || !response.Accepted {
				errU = status.Error(codes.FailedPrecondition, "usage was not accepted")
			}
			break
		}
		if attempt == 1 || settleCtx.Err() != nil || (status.Code(err) != codes.Unavailable && status.Code(err) != codes.DeadlineExceeded) {
			break
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-settleCtx.Done():
			timer.Stop()
			errU = settleCtx.Err()
		case <-timer.C:
		}
		if settleCtx.Err() != nil { break }
	}'''
if s.count(old)!=1:raise ValueError('usage retry anchor changed')
q=out/'chat.go';q.write_text(s.replace(old,new,1));(out/'overlay.json').write_text(json.dumps({'Replace':{str(p):str(q)}}))
