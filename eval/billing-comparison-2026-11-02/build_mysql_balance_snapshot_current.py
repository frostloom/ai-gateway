#!/usr/bin/env python3
"""Build a test-only MySQL snapshot GetBalance overlay from current source."""
import json,pathlib,sys
root=pathlib.Path(__file__).resolve().parents[2];out=pathlib.Path(sys.argv[1]);out.mkdir(parents=True,exist_ok=True)
source=root/'internal/billing/store/mysql.go';text=source.read_text()
old='func (s *Store) GetBalance(ctx context.Context, tenantID uint64) (int64, error) {\n\treturn s.balanceOrEnsure(ctx, tenantID)\n}'
if text.count(old)!=1:raise ValueError('GetBalance source anchor changed')
new='''func (s *Store) GetBalance(ctx context.Context, tenantID uint64) (int64, error) {
 var balance int64
 err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
  var tenant Tenant
  if err:=tx.Select("id").First(&tenant,tenantID).Error;err!=nil{return err}
  var err error
  balance,err=LedgerBalance(ctx,tx,tenantID)
  return err
 },&sql.TxOptions{Isolation:sql.LevelRepeatableRead,ReadOnly:true})
 return balance,err
}'''
candidate=out/'mysql.go';candidate.write_text(text.replace(old,new,1));(out/'overlay.json').write_text(json.dumps({'Replace':{str(source):str(candidate)}}))
