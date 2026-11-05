#!/usr/bin/env python3
"""Recreate pre-single-query working-tree auth solely for paired load tests."""
import json
import pathlib
import sys

root = pathlib.Path(__file__).resolve().parents[2]
out = pathlib.Path(sys.argv[1]); out.mkdir(parents=True, exist_ok=True)
path = root / 'internal/billing/store/mysql.go'
source = path.read_text()
start = source.index('func (s *Store) validateAPIKeyAuthority(')
end = source.index('\n// ---------- 观测', start)
current = source[start:end]
if not (('SessionCount int64' in current and 'BINARY api_keys.name' in current) or ('JOIN tenants ON tenants.id = api_keys.tenant_id' in current and 'Model(&PortalSession{})' in current)):
    raise ValueError('auth anchor changed')
legacy = '''func (s *Store) validateAPIKeyAuthority(ctx context.Context, keyHash string) (*APIKey, error) {
    var k APIKey
    err := s.db.WithContext(ctx).
        Joins("JOIN tenants ON tenants.id = api_keys.tenant_id").
        Where("api_keys.key_hash = ? AND tenants.status = ?", keyHash, 0).
        First(&k).Error
    if errors.Is(err, gorm.ErrRecordNotFound) { return nil, nil }
    if err != nil { return nil, err }
    if k.Status != 0 || (k.ExpiresAt != nil && time.Now().After(*k.ExpiresAt)) { return nil, nil }
    if k.Name == browserKeyName {
        var n int64
        err := s.db.WithContext(ctx).Model(&PortalSession{}).
            Joins("JOIN portal_users ON portal_users.id = portal_sessions.user_id").
            Joins("JOIN tenants ON tenants.id = portal_users.tenant_id").
            Where("portal_sessions.api_key_id = ? AND portal_users.status = 0 AND tenants.status = 0 AND tenants.id = ?", k.ID, k.TenantID).Count(&n).Error
        if err != nil { return nil, err }
        if n != 1 { return nil, nil }
    }
    return &k, nil
}
'''
output=out/'mysql.go'; output.write_text(source[:start]+legacy+source[end:])
(out/'overlay.json').write_text(json.dumps({'Replace':{str(path):str(output)}}))
