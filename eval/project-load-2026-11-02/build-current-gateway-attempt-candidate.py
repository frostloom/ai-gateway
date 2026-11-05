#!/usr/bin/env python3
"""Construct a test-only handler overlay, preserving current handler behavior."""
from pathlib import Path
import hashlib
root = Path(__file__).resolve().parents[2]
source = (root / 'internal/gateway/handler/chat.go').read_text()
old = (root / 'eval/project-load-2026-11-02/gateway_attempt_nonblocking_candidate.go.txt').read_text()
def once(s, anchor, replacement):
    if s.count(anchor) != 1: raise ValueError(f'anchor count {s.count(anchor)}: {anchor!r}')
    return s.replace(anchor, replacement, 1)
begin = old[old.index('\t\tguard, ok := b.(interface { BeginUpstreamAttempt'):old.index('\n\t\tupResp, err := upstreamClient.Do', old.index('\t\tguard, ok := b.(interface { BeginUpstreamAttempt'))]
finish = old[old.index('// Test-only attempt completion;'):old.index('func setUpstreamHeaders', old.index('// Test-only attempt completion;'))]
s = source
s = once(s, '\t\tupResp, err := upstreamClient.Do(uReq)', begin + '\n\t\tupResp, err := upstreamClient.Do(uReq)')
s = once(s, '\t\tresp, err := streamClient.Do(uReq)', begin + '\n\t\tresp, err := streamClient.Do(uReq)')
s = once(s, '\t\t// 成功：上报健康 + 按实际结算', '\t\tif err := experimentalFinishAttempt(b, c, requestID, attempt, prov.ProviderID); err != nil { log.Error("attempt completion failed; continue billing", "request_id", requestID, "err", err) }\n\t\t// 成功：上报健康 + 按实际结算')
s = once(s, '\tvar upResp *http.Response\n', '\tvar upResp *http.Response\n\tselectedAttempt := -1\n')
s = once(s, '\t\tupResp = resp\n', '\t\tselectedAttempt = attempt\n\t\tupResp = resp\n')
s = once(s, '\tif invalidUsage {\n', '\tif doneSeen && !invalidUsage { if err := experimentalFinishAttempt(b, c, requestID, selectedAttempt, prov.ProviderID); err != nil { log.Error("attempt completion failed; continue billing", "request_id", requestID, "err", err) } }\n\tif invalidUsage {\n')
s = once(s, 'func setUpstreamHeaders(', finish + 'func setUpstreamHeaders(')
out = root / 'eval/project-load-2026-11-02/gateway_attempt_current_candidate.go.txt'
out.write_text(s)
print('handler base sha256:', hashlib.sha256(source.encode()).hexdigest())
print('handler overlay sha256:', hashlib.sha256(s.encode()).hexdigest())
