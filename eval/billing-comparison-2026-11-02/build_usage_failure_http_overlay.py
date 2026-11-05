#!/usr/bin/env python3
"""Test-only non-stream response when actual usage was not persisted."""
import json,pathlib,sys
root=pathlib.Path(__file__).resolve().parents[2];out=pathlib.Path(sys.argv[1]);out.mkdir(parents=True,exist_ok=True);p=root/'internal/gateway/handler/chat.go';s=p.read_text()
a='''		settleAndLog(c, b, log, requestID, billID, prompt, completion, int64(prov.ProviderID))
		c.Data(http.StatusOK, "application/json", body)'''
b='''		if !settleAndLog(c, b, log, requestID, billID, prompt, completion, int64(prov.ProviderID)) {
			abort(c, http.StatusServiceUnavailable, "usage recording unavailable")
			return
		}
		c.Data(http.StatusOK, "application/json", body)'''
if s.count(a)!=1:raise ValueError('non-stream response anchor changed')
s=s.replace(a,b,1)
a='func settleAndLog(c *gin.Context, b billingAPI, log *slog.Logger, requestID string, billID, prompt, completion, providerID int64) {'
if s.count(a)!=1:raise ValueError('settle helper signature changed')
s=s.replace(a,a[:-1]+'bool {',1)
a='''		log.Error("usage report failed, bill left pending for reconciliation", "request_id", requestID, "err_report", errU)
		return'''
b='''		log.Error("usage report failed, bill left pending for reconciliation", "request_id", requestID, "err_report", errU)
		return false'''
if s.count(a)!=1:raise ValueError('usage error anchor changed')
s=s.replace(a,b,1)
a='''			"request_id", requestID, "err_report", errU, "err_settle", errS)
		return'''
b='''			"request_id", requestID, "err_report", errU, "err_settle", errS)
		return true'''
if s.count(a)!=1:raise ValueError('settle error anchor changed')
s=s.replace(a,b,1)
a='\tlog.Info("settled", "request_id", requestID, "delta_quota", sResp.DeltaQuota, "balance_after", sResp.BalanceAfter)\n}'
b='\tlog.Info("settled", "request_id", requestID, "delta_quota", sResp.DeltaQuota, "balance_after", sResp.BalanceAfter)\n\treturn true\n}'
if s.count(a)!=1:raise ValueError('settle success anchor changed')
s=s.replace(a,b,1);q=out/'chat.go';q.write_text(s);(out/'overlay.json').write_text(json.dumps({'Replace':{str(p):str(q)}}))
