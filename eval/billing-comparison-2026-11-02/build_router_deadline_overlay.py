#!/usr/bin/env python3
"""Test-only polling mutex overlay; generated from current working tree."""
import json
import pathlib
import sys

root = pathlib.Path(__file__).resolve().parents[2]
out = pathlib.Path(sys.argv[1])
out.mkdir(parents=True, exist_ok=True)
path = root / 'internal/router/registry.go'
src = path.read_text()

def replace_once(old, new):
    global src
    if src.count(old) != 1:
        raise ValueError('ambiguous or missing overlay anchor')
    src = src.replace(old, new, 1)

replace_once('''\t// 选择和占住探测位必须在同一把锁里。否则两个并发 Route 都会看到
\t// probeInFlight=false，HALF-OPEN 会放出多个探测。
\tr.mu.Lock()
''','''\t// 选择和占住探测位必须在同一把锁里。否则两个并发 Route 都会看到
\t// probeInFlight=false，HALF-OPEN 会放出多个探测。
\tif err := r.lockRouteContext(ctx); err != nil { return nil, err }
''')
replace_once('''func (r *Registry) snapshot(ctx context.Context, model string) ([]Provider, error) {
\tr.mu.Lock()
''','''func (r *Registry) snapshot(ctx context.Context, model string) ([]Provider, error) {
\tif err := r.lockRouteContext(ctx); err != nil { return nil, err }
''')
replace_once('''// ---------- snapshot（M2 原逻辑） ----------
''','''// lockRouteContext is a test-only candidate; polling cost is not established.
func (r *Registry) lockRouteContext(ctx context.Context) error {
    for {
        if err := ctx.Err(); err != nil { return err }
        if r.mu.TryLock() { return nil }
        timer := time.NewTimer(time.Millisecond)
        select {
        case <-ctx.Done():
            timer.Stop()
            return ctx.Err()
        case <-timer.C:
        }
    }
}

// ---------- snapshot（M2 原逻辑） ----------
''')
generated = out / 'registry.go'
generated.write_text(src)
(out / 'overlay.json').write_text(json.dumps({'Replace': {str(path): str(generated)}}))
