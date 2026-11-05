#!/usr/bin/env python3
"""Construct a test-only overlay from the exercised single-query candidate."""
import json
import pathlib
import sys

root = pathlib.Path(__file__).resolve().parents[2]
out = pathlib.Path(sys.argv[1])
out.mkdir(parents=True, exist_ok=True)
test_source = (root / 'eval/billing-comparison-2026-11-02/auth_single_query_test.go').read_text()
start = test_source.index('func singleQueryAuth(')
end = test_source.index('\nfunc TestAuthSingleQueryMatchesAuthority', start)
candidate = test_source[start:end]
candidate = candidate.replace('func singleQueryAuth(ctx context.Context, db *gorm.DB, hash string) (*store.APIKey, error)',
                              'func (s *Store) validateAPIKeyAuthority(ctx context.Context, hash string) (*APIKey, error)', 1)
candidate = candidate.replace('store.APIKey', 'APIKey').replace('db.WithContext(ctx)', 's.db.WithContext(ctx)')
if 'func singleQueryAuth' in candidate or 'store.APIKey' in candidate or 'BINARY api_keys.name = BINARY ?' not in candidate:
    raise ValueError('candidate source mismatch')
source_path = root / 'internal/billing/store/mysql.go'
source = source_path.read_text()
start = source.index('func (s *Store) validateAPIKeyAuthority(')
end = source.index('\n// ---------- 观测', start)
output = out / 'mysql.go'
output.write_text(source[:start] + candidate + '\n' + source[end:])
(out / 'overlay.json').write_text(json.dumps({'Replace': {str(source_path): str(output)}}))
