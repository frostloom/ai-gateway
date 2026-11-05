#!/usr/bin/env python3
"""Build a test-only, fail-closed reverse overlay; never changes production files."""
import json, os, pathlib, sys

root = pathlib.Path(__file__).resolve().parents[2]
source = root / 'internal/billing/store/mysql.go'
target = pathlib.Path(sys.argv[1])
text = source.read_text()
start = text.index('func (s *Store) reverseCash(')
end = text.index('// RebuildBalance', start)
section = text[start:end]
old = '''if err := tx.Model(&Bill{}).
			Where("request_id = ? AND phase = ? AND status = ?", req.RequestId, PhaseReserve, StatusPending).
			Update("status", StatusReversed).Error; err != nil {
			return err
		}'''
new = '''result := tx.Model(&Bill{}).
			Where("request_id = ? AND phase = ? AND status = ? AND usage_reported_at IS NULL", req.RequestId, PhaseReserve, StatusPending).
			Update("status", StatusReversed)
		if result.Error != nil { return result.Error }
		if result.RowsAffected != 1 { return status.Error(codes.FailedPrecondition, "reverse requires pending bill without reported usage") }'''
assert section.count(old) == 1
section = section.replace(old, new)
old = '\t\treturn nil, status.Errorf(codes.Unavailable, "reverse update: %v", err)'
if os.environ.get('TEST_FORCE_PROJECTION') == '1':
    new = '''\t\tif status.Code(err) == codes.FailedPrecondition {
            if _, repairErr := s.rebuildCash(ctx, row.TenantID, "", ""); repairErr != nil {
                return nil, &terminalProjectionRepairFailure{operation: "reverse condition", cause: err, repair: repairErr}
            }
            return nil, err
        }
''' + old
else:
    new = '\t\tif status.Code(err) == codes.FailedPrecondition { return nil, err }\n' + old
if os.environ.get('TEST_FORCE_ROLLBACK') == '1':
    new = '''\t\tif _, repairErr := s.rebuildCash(ctx, row.TenantID, "", ""); repairErr != nil {
            return nil, &terminalProjectionRepairFailure{operation: "reverse rollback", cause: err, repair: repairErr}
        }
''' + new
assert section.count(old) == 1
text = text[:start] + section.replace(old, new) + text[end:]
if os.environ.get('TEST_FORCE_RECOVERY') == '1':
    start = text.index('func (s *Store) recoverTerminalProjection(')
    end = text.index('// Reverse ', start)
    section = text[start:end]
    old = 'err = locked.repairOrphanGuard(locked.cashSQLContext(), tenant, DeltaGuardKey(requestID))'
    new = old + '''
        if err == nil { _, err = locked.rebuildCash(locked.cashSQLContext(), tenant, "", "") }'''
    assert section.count(old) == 1
    text = text[:start] + section.replace(old, new) + text[end:]
candidate = target / 'mysql.go'
candidate.write_text(text)
(target / 'overlay.json').write_text(json.dumps({'Replace': {str(source): str(candidate)}}))
