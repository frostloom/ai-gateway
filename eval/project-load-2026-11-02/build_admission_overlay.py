#!/usr/bin/env python3
"""Create a test-only gateway entrypoint from the exercised local gate."""
import pathlib
import sys

root = pathlib.Path(__file__).resolve().parents[2]
out = pathlib.Path(sys.argv[1])
source = (root / 'cmd/gateway/main.go').read_text()
test = (root / 'eval/billing-comparison-2026-11-02/admission_fairness_test.go').read_text()
start = test.index('func experimentalTenantGate(')
end = test.index('\nfunc TestAdmissionTenantFairnessRealBilling', start)
gate = test[start:end]
def replace_once(text, old, new):
    if text.count(old) != 1:
        raise ValueError('unexpected source anchor: ' + old[:80])
    return text.replace(old, new, 1)
source = replace_once(source, '\t"os"\n', '\t"os"\n\t"sync"\n')
source = replace_once(source, '\tv1 := r.Group("/v1")\n', '\tadmission := experimentalTenantGate(64, 16)\n\tv1 := r.Group("/v1")\n')
source = replace_once(source, '\tv1.Use(middleware.APIKey(bc))\n', '\tv1.Use(middleware.APIKey(bc))\n\tv1.Use(admission)\n')
source = replace_once(source, 'portal.POST("/completions", middleware.Idempotency(rdb)', 'portal.POST("/completions", admission, middleware.Idempotency(rdb)')
out.parent.mkdir(parents=True, exist_ok=True)
out.write_text(source + '\n' + gate + '\n')
