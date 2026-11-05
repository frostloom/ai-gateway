#!/usr/bin/env python3
import hashlib, json, os, pathlib, subprocess, sys, tempfile
root=pathlib.Path(__file__).resolve().parents[2]
with tempfile.TemporaryDirectory(prefix='execution-state-overlay-') as d:
 args=['go','test','-race']
 if any(flag in sys.argv for flag in ('--current-candidate','--strict-red','--strict-candidate','--strict-gateway','--strict-gateway-fault','--strict-kill','--strict-gate-load','--strict-network','--strict-usage-failure','--strict-inbox','--strict-inbox-fail','--strict-local','--strict-local-fail','--strict-cycle')):
  source=root/'internal/billing/store/mysql.go'
  historical=root/'eval/project-load-2026-11-02/billing_execution_state_v4_candidate.go.txt'
  body=source.read_text()
  old=historical.read_text()
  def replace_once(s, before, after):
   if s.count(before) != 1: raise SystemExit('unexpected current source layout: '+before[:80])
   return s.replace(before, after, 1)
  body=replace_once(body, '"encoding/json"', '"encoding/json"\n "encoding/hex"')
  body=replace_once(body, 'type Bill struct {', 'type Bill struct {\n ExecutionState string `gorm:"size:16;not null;default:legacy"`\n ExecutionOwnerHash string `gorm:"size:64"`\n ExecutionLeaseUntil *time.Time')
  start=old.index('type experimentalExecutionIntentKey struct{}')
  end=old.index('func (s *Store) ReserveWithReplaySignal', start)
  body=replace_once(body, 'func (s *Store) ReserveWithReplaySignal', old[start:end]+'func (s *Store) ReserveWithReplaySignal')
  # Only requests explicitly using the test intent API receive reserved state.
  body=replace_once(body, 'return tx.Model(row).Updates(map[string]any{"execution_owner_hash":intent.hash,', 'return tx.Model(row).Updates(map[string]any{"execution_state":"reserved", "execution_owner_hash":intent.hash,')
  if any(flag in sys.argv for flag in ('--strict-candidate','--strict-gateway','--strict-gateway-fault','--strict-kill','--strict-gate-load','--strict-network','--strict-usage-failure','--strict-inbox','--strict-inbox-fail','--strict-local','--strict-local-fail','--strict-cycle')):
   needle='func (s *Store) Reserve(ctx context.Context, req *billingv1.ReserveRequest) (*billingv1.ReserveResponse, error) {'
   body=replace_once(body, needle, needle+'\n if _, ok := ctx.Value(experimentalExecutionIntentKey{}).(experimentalExecutionIntent); !ok { return nil, status.Error(codes.FailedPrecondition, "experimental execution intent required") }')
  for amount in ('0','pre'):
   needle='return insertOutbox(tx, newBillingEvent(row, '+amount+', PhaseReserve))'
   body=replace_once(body, needle, 'if err := experimentalWriteExecutionIntent(ctx, tx, row); err != nil { return err }\n\t\t'+needle)
  needle='\t\tif row.Status != StatusPending {\n\t\t\treturn status.Error(codes.FailedPrecondition, "reserve is no longer pending")\n\t\t}'
  body=replace_once(body, needle, needle+'\n\t\tif row.ExecutionState != "forwarding" { return status.Error(codes.FailedPrecondition, "experimental forwarding intent required") }')
  if '--current-completion' in sys.argv:
   if '--current-candidate' not in sys.argv: raise SystemExit('completion requires --current-candidate')
   v5=(root/'eval/project-load-2026-11-02/billing_execution_state_current_candidate.go.txt').read_text()
   body=replace_once(body, 'type Bill struct {', 'type Bill struct {\n CompletionAttemptID string `gorm:"size:32"`')
   start_method=v5.index('type experimentalCompletionKey struct{}')
   end_method=v5.index('func (s *Store) ReportUsage(ctx', start_method)
   body=replace_once(body, 'func (s *Store) ReportUsage(ctx', v5[start_method:end_method]+'func (s *Store) ReportUsage(ctx')
   duplicate='\t\t\tif providerMatches && row.PromptTokens != nil && *row.PromptTokens == req.PromptTokens && row.CompletionTokens != nil && *row.CompletionTokens == req.CompletionTokens {\n'
   body=replace_once(body, duplicate, duplicate+'''                if attemptID, ok := ctx.Value(experimentalCompletionKey{}).(string); ok && row.CompletionAttemptID != attemptID {
                    return status.Error(codes.FailedPrecondition, "completion attempt differs from recorded evidence")
                }
''')
   start_guard=v5.index('\t\tif attemptID, ok := ctx.Value(experimentalCompletionKey{}).(string); ok {',v5.index('func (s *Store) ReportUsage(ctx'))
   end_guard=v5.index('\t\tif err := tx.Model(&row).Updates(updates).Error;',start_guard)
   usage_start=body.index('func (s *Store) ReportUsage(ctx')
   usage_end=body.index('func (s *Store) Settle(',usage_start)
   section=body[usage_start:usage_end]
   anchor='\t\tif err := tx.Model(&row).Updates(updates).Error;'
   section=replace_once(section,anchor,v5[start_guard:end_guard]+anchor)
   body=body[:usage_start]+section+body[usage_end:]
  start=body.index('func (s *Store) reverseCash(')
  head, tail=body[:start], body[start:]
  needle='\tif err := s.cashMutationAllowed(); err != nil {'
  tail=replace_once(tail, needle, '\tif row.ExecutionState != "reversing" { return nil, status.Error(codes.FailedPrecondition, "experimental reverse intent required") }\n'+needle)
  tail=replace_once(tail, 'Update("status", StatusReversed).Error', 'Updates(map[string]any{"status": StatusReversed, "execution_state": "reversed"}).Error')
  if '--strict-cycle' in sys.argv:
   needle='if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 5)", name).Scan(&acquired); err != nil {'
   replacement='lockStart := time.Now()\n lockErr := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 5)", name).Scan(&acquired)\n if len(name) >= 13 && name[:13] == "billing:cash:" { experimentalRecordLock(time.Since(lockStart)) }\n if err := lockErr; err != nil {'
   head=replace_once(head,needle,replacement)
   head=replace_once(head,'var releaseOnce sync.Once','heldStart := time.Now()\n var releaseOnce sync.Once')
   head=replace_once(head,'releaseOnce.Do(func() {','releaseOnce.Do(func() {\n if len(name) >= 13 && name[:13] == "billing:cash:" { experimentalRecordHeld(time.Since(heldStart)) }')
   tail+='\nvar experimentalLockMu sync.Mutex\nvar experimentalLockTimes, experimentalHeldTimes []int64\nfunc experimentalRecordLock(d time.Duration) { experimentalLockMu.Lock(); experimentalLockTimes=append(experimentalLockTimes,d.Nanoseconds()); experimentalLockMu.Unlock() }\nfunc experimentalRecordHeld(d time.Duration) { experimentalLockMu.Lock(); experimentalHeldTimes=append(experimentalHeldTimes,d.Nanoseconds()); experimentalLockMu.Unlock() }\nfunc (s *Store) ExperimentalLockSamples(reset bool) []int64 { experimentalLockMu.Lock(); defer experimentalLockMu.Unlock(); out:=append([]int64(nil),experimentalLockTimes...); if reset { experimentalLockTimes=nil }; return out }\nfunc (s *Store) ExperimentalHeldSamples(reset bool) []int64 { experimentalLockMu.Lock(); defer experimentalLockMu.Unlock(); out:=append([]int64(nil),experimentalHeldTimes...); if reset { experimentalHeldTimes=nil }; return out }\n'
  candidate=pathlib.Path(d)/'candidate.go'
  candidate.write_text(head+tail)
  print('current_source_sha256='+hashlib.sha256(source.read_bytes()).hexdigest(), flush=True)
  print('current_candidate_sha256='+hashlib.sha256(candidate.read_bytes()).hexdigest(), flush=True)
  overlay=pathlib.Path(d)/'overlay.json'
  replacements={str(source):str(candidate)}
  if '--current-attempt-guard' in sys.argv:
   gateway=root/'internal/gateway/handler/chat.go'
   gateway_body=gateway.read_text()
   for stream in ('false','true'):
    anchor='\t\tsetUpstreamHeaders(uReq, c, '+stream+', prov.APIKey)'
    addition='''
        guard, ok := b.(interface { BeginUpstreamAttempt(context.Context, int64, string, int, uint64) error })
        if !ok { abort(c, http.StatusServiceUnavailable, "attempt protection unavailable"); return }
        if err := guard.BeginUpstreamAttempt(c.Request.Context(), c.GetInt64("tenant_id"), requestID, attempt, prov.ProviderID); err != nil {
            log.Warn("upstream attempt not authorized", "request_id", requestID, "attempt", attempt, "err", err)
            abort(c, http.StatusBadGateway, "upstream attempt outcome uncertain")
            return
        }
'''
    gateway_body=replace_once(gateway_body, anchor, anchor+'\n'+addition)
   if '--current-attempt-finish' in sys.argv:
    gateway_body=replace_once(gateway_body, '\t\t// 成功：上报健康 + 按实际结算', '''        if err := experimentalFinishAttempt(b, c, requestID, attempt, prov.ProviderID); err != nil {
            log.Error("attempt completion failed", "request_id", requestID, "err", err)
            abort(c, http.StatusServiceUnavailable, "attempt completion unavailable")
            return
        }
\t\t// 成功：上报健康 + 按实际结算''')
    gateway_body=replace_once(gateway_body, '\tvar upResp *http.Response\n', '\tvar upResp *http.Response\n\tselectedAttempt := -1\n')
    gateway_body=replace_once(gateway_body, '\t\tupResp = resp\n', '\t\tselectedAttempt = attempt\n\t\tupResp = resp\n')
    anchor='\tif usageSeen != nil {\n\t\tsettleAndLog(c, b, log, requestID, billID, int64(usageSeen.PromptTokens)'
    gateway_body=replace_once(gateway_body, anchor, '''    if doneSeen {
        if err := experimentalFinishAttempt(b, c, requestID, selectedAttempt, prov.ProviderID); err != nil {
            log.Error("attempt completion failed after stream", "request_id", requestID, "err", err)
            return
        }
    }
'''+anchor)
    helper='''// Test-only attempt completion, not an authenticated production contract.
func experimentalFinishAttempt(b billingAPI, c *gin.Context, requestID string, ordinal int, provider uint64) error {
    finisher, ok := b.(interface { FinishUpstreamAttempt(context.Context, int64, string, int, uint64) error })
    if !ok { return status.Error(codes.Unavailable, "attempt completion unavailable") }
    ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
    defer cancel()
    return finisher.FinishUpstreamAttempt(ctx, c.GetInt64("tenant_id"), requestID, ordinal, provider)
}

'''
    gateway_body=replace_once(gateway_body, '// triedSlice 把', helper+'// triedSlice 把')
    if '--current-attempt-nonblocking' in sys.argv:
     old_nonstream='''            log.Error("attempt completion failed", "request_id", requestID, "err", err)
            abort(c, http.StatusServiceUnavailable, "attempt completion unavailable")
            return'''
     gateway_body=replace_once(gateway_body,old_nonstream,'            log.Error("attempt completion failed; continue billing", "request_id", requestID, "err", err)')
     old_stream='''            log.Error("attempt completion failed after stream", "request_id", requestID, "err", err)
            return'''
     gateway_body=replace_once(gateway_body,old_stream,'            log.Error("attempt completion failed after stream; continue billing", "request_id", requestID, "err", err)')
   generated=pathlib.Path(d)/'gateway-attempt-guard.go'
   generated.write_text(gateway_body)
   print('gateway_source_sha256='+hashlib.sha256(gateway.read_bytes()).hexdigest(),flush=True)
   print('gateway_candidate_sha256='+hashlib.sha256(generated.read_bytes()).hexdigest(),flush=True)
   replacements[str(gateway)]=str(generated)
  overlay.write_text(json.dumps({'Replace':replacements}))
  args+=['-overlay',str(overlay)]
 elif '--candidate' in sys.argv:
  # This historical whole-file overlay predates subscription snapshot fields.
  # Supply fields only to compile today's test package; no subscription behavior
  # is transplanted or validated by this cash-only experiment.
  source=root/'eval/project-load-2026-11-02/billing_execution_state_v4_candidate.go.txt'
  body=source.read_text()
  marker='\tPreQuota           int64'
  if body.count(marker) != 1 or 'ReservedTokens' in body or 'ReservedTier' in body:
   raise SystemExit('unexpected historical candidate layout; refuse to patch')
  body=body.replace(marker, '\tReservedTokens *int64\n\tReservedTier *string `gorm:"size:64"`\n'+marker, 1)
  candidate=pathlib.Path(d)/'candidate.go'
  candidate.write_text(body)
  print('historical_candidate_sha256='+hashlib.sha256(source.read_bytes()).hexdigest(), flush=True)
  print('compatibility_overlay_sha256='+hashlib.sha256(candidate.read_bytes()).hexdigest(), flush=True)
  overlay=pathlib.Path(d)/'overlay.json'
  overlay.write_text(json.dumps({'Replace':{str(root/'internal/billing/store/mysql.go'):str(candidate)}}))
  args+=['-overlay',str(overlay)]
 pattern='^Test(ExecutionUsageCommittedAfterScan|AtomicReserveOwnerAndRollback|ExperimentalSubscriptionIntentQuota|ExecutionReconcilerWithLeaseAndForwarding|ExecutionStrictRejectsLegacyBeforeReserve|ExecutionStrictCurrentGatewayRPCRejected|MySQLCandidateReserveIntentAtomic|MySQLCandidateForwardReverseRace|MySQLIntentCommitAckLoss|MySQLIntentInvalidRejectedBeforeReserve|MySQLIntentCrossTenantConflict|MySQLIntentOrphanOutboxDuplicateFailsClosed)$' if '--strict-candidate' in sys.argv else ('^TestExecutionStrictRejectsLegacyBeforeReserve$' if '--strict-red' in sys.argv else ('^Test(ExecutionUsageCommittedAfterScan|AtomicReserveOwnerAndRollback|ExperimentalSubscriptionIntentQuota|ExecutionCandidateLegacyCallsRejected|ExecutionReconcilerWithLeaseAndForwarding)$' if '--current-candidate' in sys.argv else '^TestExecutionUsageCommittedAfterScan$'))
 if '--current-completion' in sys.argv:
  if '--current-candidate' not in sys.argv: raise SystemExit('completion requires --current-candidate')
  pattern='^TestCompletionEvidenceOwnershipAndAtomicity$'
 if '--current-attempt-guard' in sys.argv or '--current-attempt-red' in sys.argv:
  if '--current-candidate' not in sys.argv: raise SystemExit('attempt-guard test requires --current-candidate')
  if '--current-attempt-finish' in sys.argv and '--current-attempt-guard' not in sys.argv: raise SystemExit('finish requires guard')
  pattern='^TestAttemptGuardBlocksUnknownFailover$'
 if '--current-attempt-failure' in sys.argv:
  if not all(flag in sys.argv for flag in ('--current-candidate','--current-attempt-guard','--current-attempt-finish','--current-completion')): raise SystemExit('attempt failure test requires complete candidate flags')
  pattern='^TestAttemptOutboxFailureAfterUpstreamResponse$'
 if '--current-auto-journal-kill' in sys.argv:
  if not all(flag in sys.argv for flag in ('--current-candidate','--strict-candidate','--current-completion','--current-attempt-guard','--current-attempt-finish','--current-attempt-nonblocking')): raise SystemExit('journal kill test requires complete candidate flags')
  pattern='^TestCompletionJournalHandlerKilledAfterSync$'
 if '--legacy-failover' in sys.argv:
  if '--current-candidate' not in sys.argv: raise SystemExit('--legacy-failover requires --current-candidate')
  pattern='^TestExecutionPermissionDoesNotGuardFailover$'
 if '--strict-gateway' in sys.argv: pattern='^TestRealGatewayWithExecutionBridge$/(complete|stream_complete)$'
 if '--strict-gateway-fault' in sys.argv: pattern='^TestRealGatewayWithExecutionBridge$/(truncated|stream_partial|stream_empty)$'
 if '--strict-kill' in sys.argv: pattern='^Test(AtomicCommitOwnerDiesBeforeSend|SentRequestOwnerKilledCannotRefundOnExpiry|AtomicReserveKilledConnectionAndReplay|StrictReserveCommitAckLoss)$'
 if '--strict-gate-load' in sys.argv: pattern='^TestExecutionGateDistribution$'
 if '--strict-network' in sys.argv: pattern='^TestNetworkGatewayInterruptedStream$'
 if '--strict-cycle' in sys.argv: pattern='^TestStrictBillingCycleDistribution$'
 if '--strict-usage-failure' in sys.argv: pattern='^TestRealGatewayWithExecutionBridge$/(usage_failed|stream_usage_failed)$'
 if '--strict-inbox' in sys.argv: pattern='^TestRealGatewayWithExecutionBridge$/(usage_failed|stream_usage_failed)$'
 if '--strict-inbox-fail' in sys.argv: pattern='^TestRealGatewayWithExecutionBridge$/(usage_failed|stream_usage_failed)$'
 if '--strict-local' in sys.argv or '--strict-local-fail' in sys.argv: pattern='^TestRealGatewayWithExecutionBridge$/(usage_failed|stream_usage_failed)$'
 repetitions='4' if '--strict-cycle' in sys.argv else '3'
 args+=['./eval/billing-comparison-2026-11-02','-run',pattern,'-count='+repetitions,'-v']
 env=dict(os.environ,TEST_EXECUTION_STATE='1')
 if '--current-completion' in sys.argv: env['TEST_COMPLETION_EVIDENCE']='1'
 if '--current-attempt-nonblocking' in sys.argv:
  if not all(flag in sys.argv for flag in ('--current-attempt-guard','--current-attempt-finish','--current-completion')): raise SystemExit('nonblocking needs guard, finish, completion')
  env['TEST_FINISH_NONBLOCKING']='1'
 if any(flag in sys.argv for flag in ('--strict-red','--strict-candidate','--strict-gateway','--strict-gateway-fault','--strict-kill','--strict-gate-load','--strict-network','--strict-usage-failure','--strict-inbox','--strict-inbox-fail','--strict-local','--strict-local-fail','--strict-cycle')): env['TEST_EXECUTION_STRICT']='1'
 if '--strict-gateway-fault' in sys.argv: env['TEST_GATEWAY_CONSERVATIVE']='1'
 if '--strict-network' in sys.argv: env.update(TEST_GATEWAY_CONSERVATIVE='1', TEST_STREAM_SAFE='1')
 if '--strict-usage-failure' in sys.argv: env.update(TEST_GATEWAY_CONSERVATIVE='1', TEST_LOST_KNOWN_USAGE='1')
 if '--strict-inbox' in sys.argv: env.update(TEST_GATEWAY_CONSERVATIVE='1', TEST_HANDLER_INBOX='1', TEST_HANDLER_INBOX_KILL='1')
 if '--strict-inbox-fail' in sys.argv: env.update(TEST_GATEWAY_CONSERVATIVE='1', TEST_HANDLER_INBOX='1', TEST_HANDLER_INBOX_WRITE_FAIL='1')
 if '--strict-local' in sys.argv or '--strict-local-fail' in sys.argv: env.update(TEST_GATEWAY_CONSERVATIVE='1', TEST_HANDLER_INBOX='1', TEST_HANDLER_INBOX_WRITE_FAIL='1', TEST_HANDLER_LOCAL_JOURNAL='1')
 if '--strict-local-fail' in sys.argv: env['TEST_HANDLER_LOCAL_FAIL']='1'
 raise SystemExit(subprocess.run(args,cwd=root,env=env).returncode)
