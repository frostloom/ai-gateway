#!/usr/bin/env python3
"""Build a test-only state-machine overlay on the current Store, never production code."""
from pathlib import Path
import hashlib
root = Path(__file__).resolve().parents[2]
base = (root / 'internal/billing/store/mysql.go').read_text()
old = (root / 'eval/project-load-2026-11-02/billing_execution_state_v5_candidate.go.txt').read_text()
def extract(start, end):
    return old[old.index(start):old.index(end, old.index(start))]
def replace_once(s, a, b):
    if s.count(a) != 1:
        raise ValueError(f'expected one anchor, found {s.count(a)}: {a!r}')
    return s.replace(a, b, 1)
s = replace_once(base, '"encoding/json"', '"encoding/json"\n\t"encoding/hex"')
s = replace_once(s, 'type Bill struct {\n', 'type Bill struct {\n' + extract('\tExecutionState string', '\tRequestID '))
s = replace_once(s, 'func (s *Store) ReserveWithReplaySignal(', extract('type experimentalExecutionIntentKey', 'func (s *Store) ReserveWithReplaySignal(') + 'func (s *Store) ReserveWithReplaySignal(')
# Both fresh reservation paths create a row and outbox in the same transaction.
s = replace_once(s, 'row := &Bill{\n\t\t\tRequestID:', 'row := &Bill{\n\t\t\tExecutionState: "reserved",\n\t\t\tRequestID:')
s = replace_once(s, 'row := &Bill{\n\t\tRequestID:', 'row := &Bill{\n\t\tExecutionState: "reserved",\n\t\tRequestID:')
for indent, amount in (("\t\t\t", "0"), ("\t\t", "pre")):
    anchor = "\n" + indent + f"return insertOutbox(tx, newBillingEvent(row, {amount}, PhaseReserve))"
    s = replace_once(s, anchor, "\n" + indent + "if err := experimentalWriteExecutionIntent(ctx, tx, row); err != nil { return err }" + anchor)
s = replace_once(s, 'func (s *Store) ReportUsage(ctx', extract('type experimentalCompletionKey', 'func (s *Store) ReportUsage(ctx') + 'func (s *Store) ReportUsage(ctx')
anchor = '\t\tupdates := map[string]any{"prompt_tokens":'
s = replace_once(s, anchor, '\t\tif row.ExecutionState != "forwarding" { return status.Error(codes.FailedPrecondition, "experimental forwarding intent required for usage") }\n' + anchor)
block = extract('\t\tif attemptID, ok := ctx.Value(experimentalCompletionKey{})', '\t\tif err := tx.Model(&row).Updates(updates)')
anchor = '\t\tif err := tx.Model(&row).Updates(updates).Error; err != nil {'
s = replace_once(s, anchor, block + anchor)
start = s.index('func (s *Store) reverseCash(')
pos = s.index('\tif err := s.cashMutationAllowed(); err != nil {', start)
s = s[:pos] + '\tif row.ExecutionState != "reversing" { return nil, status.Error(codes.FailedPrecondition, "experimental reverse intent required") }\n' + s[pos:]
out = root / 'eval/project-load-2026-11-02/billing_execution_state_current_candidate.go.txt'
out.write_text(s)
print('base sha256:', hashlib.sha256(base.encode()).hexdigest())
print('candidate sha256:', hashlib.sha256(s.encode()).hexdigest())
