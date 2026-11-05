#!/usr/bin/env python3
"""Build an isolated single-statement ledger read candidate."""
import json, pathlib, sys

root = pathlib.Path(__file__).resolve().parents[2]
source = root / 'internal/billing/store/plan.go'
target = pathlib.Path(sys.argv[1])
text = source.read_text()
start = text.index('func LedgerBalance(')
end = text.index('// Intermediate sums', start)
candidate = '''func LedgerBalance(ctx context.Context, db *gorm.DB, tenantID uint64) (int64, error) {
    var row struct { Initial int64; Topups int64; Pending int64; Settled int64 }
    err := db.WithContext(ctx).Raw(`SELECT
      COALESCE((SELECT initial_quota FROM tenants WHERE id = ?),0) AS initial,
      (SELECT COALESCE(SUM(amount),0) FROM topups WHERE tenant_id = ?) AS topups,
      (SELECT COALESCE(SUM(pre_quota),0) FROM bills WHERE tenant_id = ? AND phase = ? AND status = ?) AS pending,
      (SELECT COALESCE(SUM(actual_quota),0) FROM bills WHERE tenant_id = ? AND phase = ? AND status = ?) AS settled`,
      tenantID, tenantID, tenantID, PhaseReserve, StatusPending, tenantID, PhaseSettle, StatusSettled).Scan(&row).Error
    if err != nil { return 0, err }
    return checkedLedgerBalance(row.Initial, row.Topups, row.Pending, row.Settled)
}

'''
out = target / 'plan.go'; out.write_text(text[:start] + candidate + text[end:])
(target / 'overlay.json').write_text(json.dumps({'Replace': {str(source):str(out)}}))
