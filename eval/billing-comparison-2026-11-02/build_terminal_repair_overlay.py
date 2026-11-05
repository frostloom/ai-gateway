#!/usr/bin/env python3
"""Test-only overlay: rebuild after terminal SQL errors, without changing policy."""
import hashlib, json, pathlib, sys

root = pathlib.Path(__file__).resolve().parents[2]
source = root / 'internal/billing/store/mysql.go'
target = pathlib.Path(sys.argv[1])
text = source.read_text()
if hashlib.sha256(source.read_bytes()).hexdigest() != 'be5ced15d64432b55979cf62b24e005c89ca340c509a01020d94f63147eaa6c9':
    raise SystemExit('historical overlay requires its recorded pre-fix mysql.go; use current production tests instead')
start = text.index('\tbal, applied, err := s.applyDelta(')
end = text.index('// RebuildBalance', start)
terminal = text[start:end]
for operation in ('settle insert', 'reverse update'):
    old = f'\t\treturn nil, status.Errorf(codes.Unavailable, "{operation}: %v", err)'
    # The subscription settle branch uses deeper indentation and is left alone:
    # its delta is zero. This overlay is restricted to the cash path.
    assert terminal.count(old) == 1, (operation, terminal.count(old))
    new = f'''\t\tif _, repairErr := s.rebuildCash(ctx, row.TenantID, "", ""); repairErr != nil {{
            return nil, &terminalProjectionRepairFailure{{operation: "{operation}", cause: err, repair: repairErr}}
        }}
''' + old
    terminal = terminal.replace(old, new, 1)
text = text[:start] + terminal + text[end:]
start = text.index('func (s *Store) recoverTerminalProjection(')
end = text.index('// Reverse ', start)
section = text[start:end]
old = 'err = locked.repairOrphanGuard(locked.cashSQLContext(), tenant, DeltaGuardKey(requestID))'
assert section.count(old) == 1
section = section.replace(old, old + '\n        if err == nil { _, err = locked.rebuildCash(locked.cashSQLContext(), tenant, "", "") }')
text = text[:start] + section + text[end:]
candidate = target / 'mysql.go'
candidate.write_text(text)
(target / 'overlay.json').write_text(json.dumps({'Replace': {str(source): str(candidate)}}))
