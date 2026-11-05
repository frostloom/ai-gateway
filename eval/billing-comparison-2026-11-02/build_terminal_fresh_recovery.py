#!/usr/bin/env python3
from pathlib import Path
r=Path(__file__).resolve().parents[2]
s=(r/'internal/billing/store/mysql.go').read_text()
def one(a,b):
 global s
 if s.count(a)!=1: raise ValueError((a,s.count(a)))
 s=s.replace(a,b,1)
for method in ('settle','reverse'):
 a=f'\treturn locked.{method}Cash(locked.cashSQLContext(), req)'
 b=f'''\tresp, terminalErr := locked.{method}Cash(locked.cashSQLContext(), req)
\tvar repairFailure *terminalProjectionRepairFailure
\tif errors.As(terminalErr, &repairFailure) {{
\t\trelease()
\t\treturn nil, s.recoverTerminalProjection(ctx, row.TenantID, req.RequestId, repairFailure)
\t}}
\treturn resp, terminalErr'''
 one(a,b)
for op in ('settle insert','reverse update'):
 a=f'return nil, status.Errorf(codes.Unavailable, "{op}: %v; projection repair: %v", err, repairErr)'
 b=f'return nil, &terminalProjectionRepairFailure{{operation: "{op}", cause: err, repair: repairErr}}'
 count=s.count(a)
 if count != (2 if op == 'settle insert' else 1): raise ValueError((op,count))
 s=s.replace(a,b)
anchor='// Reverse 冲正/退款：pending → reversed，退回全部预扣。'
helper='''// terminalProjectionRepairFailure indicates that the first locked repair failed.
type terminalProjectionRepairFailure struct { operation string; cause, repair error }
func (e *terminalProjectionRepairFailure) Error() string { return fmt.Sprintf("%s: %v; projection repair: %v", e.operation, e.cause, e.repair) }
func (s *Store) recoverTerminalProjection(ctx context.Context, tenant uint64, requestID string, failure *terminalProjectionRepairFailure) error {
 recovery, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
 defer cancel()
 locked, release, err := s.lockCash(recovery, tenant)
 if err == nil {
  defer release()
  locked.cash.allowDisabledTerminal = true
  err = locked.repairOrphanGuard(locked.cashSQLContext(), tenant, DeltaGuardKey(requestID))
 }
 if err != nil { return status.Errorf(codes.Unavailable, "%s; fresh projection repair: %v", failure.Error(), err) }
 return status.Errorf(codes.Unavailable, "%s: %v", failure.operation, failure.cause)
}

'''
one(anchor,helper+anchor)
(r/'eval/billing-comparison-2026-11-02/terminal_fresh_recovery_candidate.go.txt').write_text(s)
