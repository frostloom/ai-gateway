#!/usr/bin/env python3
"""Test only: every GetBalance rebuilds under the existing tenant lock."""
import hashlib,json,pathlib
root=pathlib.Path(__file__).resolve().parents[2]
src=root/'internal/billing/store/mysql.go';text=src.read_text()
assert hashlib.sha256(text.encode()).hexdigest()=='e4a53b0eaca810e385a7336c0ace3b05723313b362342b6094b822bee0d8f98c'
old='func (s *Store) GetBalance(ctx context.Context, tenantID uint64) (int64, error) {\n\treturn s.balanceOrEnsure(ctx, tenantID)\n}'
new='func (s *Store) GetBalance(ctx context.Context, tenantID uint64) (int64, error) {\n\treturn s.RebuildBalance(ctx, tenantID)\n}'
assert text.count(old)==1
dest=root/'eval/billing-comparison-2026-11-02/consistent_balance_candidate.go.txt';dest.write_text(text.replace(old,new,1))
overlay=dest.parent/'consistent-balance-overlay.json';overlay.write_text(json.dumps({'Replace':{str(src):str(dest)}})+'\n')
print('candidate_sha256='+hashlib.sha256(dest.read_bytes()).hexdigest())
