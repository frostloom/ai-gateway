#!/usr/bin/env python3
"""Test-only per-provider serialization based on the current router source."""
import json
import pathlib
import sys

root = pathlib.Path(__file__).resolve().parents[2]
out = pathlib.Path(sys.argv[1])
out.mkdir(parents=True, exist_ok=True)
source = root / 'internal/router/registry.go'
content = source.read_text()
anchor = '\tmu            sync.Mutex\n'
if content.count(anchor) != 1:
    raise ValueError('registry lock anchor changed')
content = content.replace(anchor, anchor + '\treportLocks   sync.Map // experimental; lifecycle is not solved\n', 1)
start = content.index('func (r *Registry) Report(')
end = content.index('\n// available 按熔断状态', start)
replacement = '''func (r *Registry) Report(providerID uint64, ok bool) {
    value, _ := r.reportLocks.LoadOrStore(providerID, &sync.Mutex{})
    providerLock := value.(*sync.Mutex)
    providerLock.Lock()
    defer providerLock.Unlock()
    r.mu.Lock()
    b := r.breaker(providerID)
    now := time.Now()
    if ok {
        b.consecutiveFail = 0
        if b.state == breakerHalfOpen {
            b.state = breakerClosed
            b.probeInFlight = false
            r.logState(providerID, b, "recover")
        }
        r.mu.Unlock()
        r.resetDBFails(providerID)
        return
    }
    b.consecutiveFail++
    if b.state == breakerHalfOpen { b.probeInFlight = false }
    if b.consecutiveFail >= r.AutobanAfter {
        b.state = breakerOpen
        b.nextClosedAt = time.Now().Add(24 * time.Hour)
        failures := b.consecutiveFail
        r.logState(providerID, b, "autoban")
        r.mu.Unlock()
        if r.db != nil {
            if err := r.db.Table("providers").Where("id = ? AND status = ?", providerID, 0).Updates(map[string]any{"status": 2, "consecutive_fail": failures}).Error; err != nil {
                metrics.RouterPersistenceError("ban")
                r.log.Error("autoban persist", "provider_id", providerID, "err", err)
            }
            r.mu.Lock()
            r.clearCache()
            r.mu.Unlock()
        }
        return
    }
    if b.consecutiveFail >= r.FailThreshold && b.state != breakerOpen {
        b.state = breakerOpen
        b.nextClosedAt = now.Add(r.Cooldown)
        r.logState(providerID, b, "open")
    }
    r.mu.Unlock()
    r.persistFail(providerID)
}
'''
candidate = out / 'registry.go'
candidate.write_text(content[:start] + replacement + content[end:])
(out / 'overlay.json').write_text(json.dumps({'Replace': {str(source): str(candidate)}}))
