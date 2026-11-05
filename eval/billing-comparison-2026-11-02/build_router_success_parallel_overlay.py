#!/usr/bin/env python3
"""Test-only concurrent successes; failures remain exclusive."""
import json,os,pathlib,sys
root=pathlib.Path(__file__).resolve().parents[2];out=pathlib.Path(sys.argv[1]);out.mkdir(parents=True,exist_ok=True)
src=root/'internal/router/registry.go';s=src.read_text()
limit=int(os.environ.get('TEST_ROUTER_SUCCESS_LIMIT','0'))
if limit not in (0,2): raise ValueError('allowed test limit: 0 or 2')
global_limit=int(os.environ.get('TEST_ROUTER_REPORT_GLOBAL','0'))
if global_limit not in (0,4) or (global_limit and limit != 2): raise ValueError('global cap requires per-provider 2 and value 4')
cap_failures=os.environ.get('TEST_ROUTER_CAP_FAILURES','0')
if cap_failures not in ('0','1') or (cap_failures=='1' and global_limit!=4): raise ValueError('failure cap requires global cap 4')
publish_first=os.environ.get('TEST_ROUTER_PUBLISH_BEFORE_CAP','0')
if publish_first not in ('0','1') or (publish_first=='1' and cap_failures!='1'): raise ValueError('publish first requires all-report cap')
a='reportMu        sync.Mutex // serialize persistence for the same provider';b='reportMu        sync.RWMutex // test-only parallel success reports'
assert s.count(a)==1;s=s.replace(a,b)
a='\tb.reportMu.Lock()\n\tdefer b.reportMu.Unlock()';b='\tif ok {\n\t\tb.reportMu.RLock()\n\t\tdefer b.reportMu.RUnlock()\n\t} else {\n\t\tb.reportMu.Lock()\n\t\tdefer b.reportMu.Unlock()\n\t}'
assert s.count(a)==1;s=s.replace(a,b)
if limit:
 a='reportMu        sync.RWMutex // test-only parallel success reports';b=a+'\n\tsuccessSlots    chan struct{}'
 assert s.count(a)==1;s=s.replace(a,b)
 a='b := &breaker{state: breakerClosed}';b='b := &breaker{state: breakerClosed, successSlots: make(chan struct{}, 2)}'
 assert s.count(a)==1;s=s.replace(a,b)
 a='\t\tb.reportMu.RLock()';b='\t\tb.successSlots <- struct{}{}\n\t\tdefer func() { <-b.successSlots }()\n'+a
 assert s.count(a)==1;s=s.replace(a,b)
if global_limit:
 a='\trefreshPermit   chan struct{}';b=a+'\n\treportSlots     chan struct{}'
 assert s.count(a)==1;s=s.replace(a,b)
 a='\t\trefreshPermit: make(chan struct{}, 1),';b=a+'\n\t\treportSlots: make(chan struct{}, 4),'
 assert s.count(a)==1;s=s.replace(a,b)
 a='\t\tdefer func() { <-b.successSlots }()';b=a+'\n\t\tr.reportSlots <- struct{}{}\n\t\tdefer func() { <-r.reportSlots }()'
 assert s.count(a)==1;s=s.replace(a,b)
 if cap_failures=='1':
  a='\t\tr.reportSlots <- struct{}{}\n\t\tdefer func() { <-r.reportSlots }()'
  assert s.count(a)==1;s=s.replace(a,'')
  if publish_first=='1':
   for old,new in [('return r.resetDBFails(providerID)','return r.testReportWrite(func() error { return r.resetDBFails(providerID) })'),('err := r.persistBan(providerID, failures)','err := r.testReportWrite(func() error { return r.persistBan(providerID, failures) })'),('return r.persistFail(providerID)','return r.testReportWrite(func() error { return r.persistFail(providerID) })')]:
    assert s.count(old)==1;s=s.replace(old,new)
   s += '\nfunc (r *Registry) testReportWrite(fn func() error) error { r.reportSlots <- struct{}{}; defer func() { <-r.reportSlots }(); return fn() }\n'
  else:
   a='\t}\n\tr.mu.Lock()\n\tnow := time.Now()';b='\t}\n\tr.reportSlots <- struct{}{}\n\tdefer func() { <-r.reportSlots }()\n\tr.mu.Lock()\n\tnow := time.Now()'
   assert s.count(a)==1;s=s.replace(a,b)
candidate=out/'registry.go';candidate.write_text(s)
(out/'overlay.json').write_text(json.dumps({'Replace':{str(src):str(candidate)}}))
