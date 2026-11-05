#!/usr/bin/env python3
"""Test-only MySQL snapshot portal view with explicit unchecked projection."""
import json,pathlib,sys
root=pathlib.Path(__file__).resolve().parents[2];out=pathlib.Path(sys.argv[1]);out.mkdir(parents=True,exist_ok=True)
source=root/'internal/billing/store/plan.go';s=source.read_text()
imports='\t"context"\n';field='\tConsistent         bool          `json:"consistent"`'
if s.count(imports)!=1 or s.count(field)!=1:raise ValueError('portal source anchors changed')
s=s.replace(imports,imports+'\t"database/sql"\n',1)
pos=s.index(field);end=s.index('\n',pos)
s=s[:end]+ '\n\tProjectionChecked *bool `json:"projection_checked,omitempty"` // TEST ONLY' +s[end:]
a=s.index('func (s *Store) MeOverview(');b=s.index('\nfunc (s *Store) meOverviewCash(',a)
new='''func (s *Store) MeOverview(ctx context.Context, tenantID uint64) (*MeView, error) {
 var view *MeView
 err:=s.db.WithContext(ctx).Transaction(func(tx *gorm.DB)error{
  var tenant Tenant
  if err:=tx.Select("id").First(&tenant,tenantID).Error;err!=nil{return err}
  snapshot:=*s;snapshot.db=tx;snapshot.cash=nil
  ledger,err:=LedgerBalance(ctx,tx,tenantID);if err!=nil{return err}
  sub,err:=snapshot.GetActiveSubscription(ctx,tenantID);if err!=nil{return err}
  plans,err:=snapshot.ListPlans(ctx);if err!=nil{return err}
  cons,err:=snapshot.ConsumptionStats(ctx,tenantID,7);if err!=nil{return err}
  byModel,err:=snapshot.ConsumptionByModel(ctx,tenantID,7);if err!=nil{return err}
  topups,err:=snapshot.ListTopups(ctx,tenantID,20);if err!=nil{return err}
  allowance,err:=snapshot.SubscriptionAllowance(ctx,sub);if err!=nil{return err}
  refreshHours:=0
  if sub!=nil {p,err:=snapshot.GetPlan(ctx,sub.PlanID);if err!=nil{return err};if p!=nil{refreshHours=p.RefreshHours}}
  checked:=false
  view=&MeView{Balance:ledger,BalanceCent:ledger,LedgerBalance:ledger,Consistent:false,ProjectionChecked:&checked,Subscription:sub,Allowance:allowance,RefreshHours:refreshHours,Plans:plans,Consumption:cons,ConsumptionByModel:byModel,Topups:topups}
  return nil
 },&sql.TxOptions{Isolation:sql.LevelRepeatableRead,ReadOnly:true})
 if err!=nil{return nil,err};return view,nil
}
'''
s=s[:a]+new+s[b:]
dest=out/'plan.go';dest.write_text(s);(out/'overlay.json').write_text(json.dumps({'Replace':{str(source):str(dest)}}))
