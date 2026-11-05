#!/usr/bin/env python3
"""Build test-only balance reads, with and without a MySQL snapshot."""
import hashlib,json,pathlib
root=pathlib.Path(__file__).resolve().parents[2];src=root/'internal/billing/store/mysql.go';base=src.read_text();out=root/'eval/billing-comparison-2026-11-02'
assert hashlib.sha256(base.encode()).hexdigest()=='e4a53b0eaca810e385a7336c0ace3b05723313b362342b6094b822bee0d8f98c'
old='func (s *Store) GetBalance(ctx context.Context, tenantID uint64) (int64, error) {\n\treturn s.balanceOrEnsure(ctx, tenantID)\n}'
assert base.count(old)==1
variants={'unsafe':'''func (s *Store) GetBalance(ctx context.Context, tenantID uint64) (int64, error) {
 return LedgerBalance(ctx, s.db, tenantID)
}''','snapshot':'''func (s *Store) GetBalance(ctx context.Context, tenantID uint64) (int64, error) {
 var balance int64
 err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
  var tenant Tenant
  if err := tx.Select("id").First(&tenant, tenantID).Error; err != nil { return err }
  var err error
  balance, err = LedgerBalance(ctx, tx, tenantID)
  return err
 }, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
 return balance, err
}'''}
for name,body in variants.items():
 dest=out/('mysql_balance_'+name+'.go.txt');dest.write_text(base.replace(old,body,1))
 (out/('mysql-balance-'+name+'-overlay.json')).write_text(json.dumps({'Replace':{str(src):str(dest)}})+'\n')
 print(name+'='+hashlib.sha256(dest.read_bytes()).hexdigest())
