#!/usr/bin/env python3
"""Test-only reconciler: settle usage aged 30s without shortening unknown grace."""
import json
import pathlib
import sys
root=pathlib.Path(__file__).resolve().parents[2]
out=pathlib.Path(sys.argv[1]); out.mkdir(parents=True,exist_ok=True)
src=root/'internal/billing/reconciler/reconciler.go'
s=src.read_text()
a='Where("phase = ? AND status = ? AND created_at < ?",\n\t\t\t\tstore.PhaseReserve, store.StatusPending, cutoff)'
b='Where("phase = ? AND status = ? AND (created_at < ? OR (usage_reported_at IS NOT NULL AND usage_reported_at < ?))",\n\t\t\t\tstore.PhaseReserve, store.StatusPending, cutoff, time.Now().Add(-30*time.Second))'
if s.count(a)!=1: raise RuntimeError('reconciler query anchor changed')
candidate=out/'reconciler.go';candidate.write_text(s.replace(a,b))
(out/'overlay.json').write_text(json.dumps({'Replace':{str(src):str(candidate)}}))
