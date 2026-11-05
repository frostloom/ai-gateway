#!/usr/bin/env python3
"""Test-only: add request-lock coordination on the current Store snapshot."""
from pathlib import Path
import hashlib
root=Path(__file__).resolve().parents[2]
source=(root/'internal/billing/store/mysql.go').read_text()
s=source
def replace_once(a,b):
 global s
 if s.count(a)!=1:raise ValueError((a,s.count(a)))
 s=s.replace(a,b,1)
# Usage only holds request lock; reverse uses existing tenant -> request order.
anchor='''	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row Bill'''
replacement='''	conn, release, err := s.lockReserveRequest(ctx, req.RequestId)
	if err != nil { return status.Errorf(codes.Unavailable, "usage lock: %v", err) }
	defer release()
	lockedDB := s.db.Session(&gorm.Session{NewDB: true}).WithContext(ctx)
	lockedDB.Statement.ConnPool = conn
	err = lockedDB.Transaction(func(tx *gorm.DB) error {
		var row Bill'''
replace_once(anchor,replacement)
anchor='''	resp, terminalErr := locked.reverseCash(locked.cashSQLContext(), req)
	var repairFailure *terminalProjectionRepairFailure'''
replacement='''	_, releaseRequest, err := locked.lockReserveRequest(locked.cashSQLContext(), req.RequestId)
	if err != nil { return nil, status.Errorf(codes.Unavailable, "reverse request lock: %v", err) }
	defer releaseRequest()
	resp, terminalErr := locked.reverseCash(locked.cashSQLContext(), req)
	// Release request lock before releasing tenant lock and reacquiring it for repair.
	releaseRequest()
	var repairFailure *terminalProjectionRepairFailure'''
replace_once(anchor,replacement)
anchor='''	if err := s.cashMutationAllowed(); err != nil {
		return nil, err
	}
	if err := s.repairOrphanGuard(ctx, row.TenantID, DeltaGuardKey(req.RequestId)); err != nil {'''
# Both settlement and reversal have this anchor; locate only reversal.
pos=s.index('func (s *Store) reverseCash(')
pre,post=s[:pos],s[pos:]
if post.count(anchor)!=1:raise ValueError('reverse anchor')
post=post.replace(anchor,'''\tif req.Reason == "recon:no_usage" && row.UsageReportedAt != nil {
		return nil, status.Error(codes.FailedPrecondition, "cannot reconcile reverse reported usage")
	}
'''+anchor,1)
s=pre+post
out=root/'eval/project-load-2026-11-02/usage_reverse_current_candidate.go.txt';out.write_text(s)
print('base_sha256='+hashlib.sha256(source.encode()).hexdigest())
print('candidate_sha256='+hashlib.sha256(s.encode()).hexdigest())
