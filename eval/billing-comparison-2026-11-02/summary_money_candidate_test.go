package comparison

import (
 "context"
 "crypto/rand"
 "errors"
 "fmt"
 "time"
 "github.com/frostloom/ai-gateway/internal/billing/store"
 "github.com/frostloom/ai-gateway/internal/pkg/quota"
 mysql "github.com/go-sql-driver/mysql"
 "gorm.io/gorm"
 "gorm.io/gorm/clause"
)

// Test-only partial candidate. All writers must use this protocol; the
// existing Store recharge path does not update candidate_accounts.
func (c candidate) paySummary(ctx context.Context,orderNo string)(*store.RechargeOrder,error){
 var initial store.RechargeOrder
 if err:=c.db.WithContext(ctx).Where("order_no=?",orderNo).First(&initial).Error;err!=nil{return nil,err}
 var result store.RechargeOrder
 err:=c.db.WithContext(ctx).Transaction(func(tx *gorm.DB)error{
  var account candidateAccount;if err:=tx.Clauses(clause.Locking{Strength:"UPDATE"}).First(&account,initial.TenantID).Error;err!=nil{return err}
  if err:=tx.Clauses(clause.Locking{Strength:"UPDATE"}).Where("order_no=?",orderNo).First(&result).Error;err!=nil{return err}
  if result.TenantID!=initial.TenantID{return errors.New("tenant changed")};if result.Status==store.RechargePaid{return nil};if result.Status!=store.RechargePending{return fmt.Errorf("order status %s",result.Status)}
  if result.AmountMoney<=0||result.AmountMoney>quota.MaxCashLua/quota.CentPerYuan||result.AmountCent!=result.AmountMoney*quota.CentPerYuan||account.Balance<0||account.Balance>quota.MaxCashLua-result.AmountCent{return errors.New("amount out of range")}
  now:=time.Now();entry:=store.Topup{TenantID:account.ID,Source:store.TopupRecharge,Amount:result.AmountCent,RefType:store.TopupRecharge,RefID:orderNo,CreatedAt:now}
  if err:=tx.Create(&entry).Error;err!=nil{return err}
  if err:=tx.Model(&account).Update("balance",gorm.Expr("balance + ?",result.AmountCent)).Error;err!=nil{return err}
  changed:=tx.Model(&result).Where("status=?",store.RechargePending).Updates(map[string]any{"status":store.RechargePaid,"paid_at":now});if changed.Error!=nil{return changed.Error};if changed.RowsAffected!=1{return errors.New("order status changed")}
  if c.fail{return errors.New("injected rollback")};result.Status=store.RechargePaid;result.PaidAt=&now;return nil
 })
 if err!=nil{return nil,err};return &result,nil
}

func (c candidate) refundSummaryAmount(ctx context.Context,tenantID uint64,amountMoney int64,idemKey string)(*store.RefundResult,error){
 if amountMoney<=0||amountMoney>quota.MaxCashLua/quota.CentPerYuan{return nil,errors.New("refund amount out of range")}
 if idemKey==""{idemKey=rand.Text()};target:=amountMoney*quota.CentPerYuan
 verify:=func(entry store.Topup)error{if entry.TenantID!=tenantID||entry.Source!=store.TopupRefund||entry.RequestedCent==nil||*entry.RequestedCent!=target||entry.Amount>=0||entry.Amount < -target{return errors.New("refund idempotency conflict")};return nil}
 var result *store.RefundResult
 err:=c.db.WithContext(ctx).Transaction(func(tx *gorm.DB)error{
  var account candidateAccount;if err:=tx.Clauses(clause.Locking{Strength:"UPDATE"}).First(&account,tenantID).Error;err!=nil{return err}
  var existing store.Topup;err:=tx.Where("ref_type=? AND ref_id=?",store.TopupRefund,idemKey).First(&existing).Error
  if err==nil {if err=verify(existing);err!=nil{return err};result=&store.RefundResult{AmountCent:target,RefundCent:0,BalanceAfter:account.Balance};return nil};if !errors.Is(err,gorm.ErrRecordNotFound){return err}
  refund:=target;if refund>account.Balance{refund=account.Balance};if refund<=0{return errors.New("no refundable balance")}
  entry:=store.Topup{TenantID:tenantID,Source:store.TopupRefund,Amount:-refund,RequestedCent:&target,RefType:store.TopupRefund,RefID:idemKey,CreatedAt:time.Now()};if err:=tx.Create(&entry).Error;err!=nil{return err}
  if err:=tx.Model(&account).Update("balance",gorm.Expr("balance - ?",refund)).Error;err!=nil{return err};if c.fail{return errors.New("injected rollback")}
  result=&store.RefundResult{AmountCent:target,RefundCent:refund,BalanceAfter:account.Balance-refund};return nil
 })
 var duplicate *mysql.MySQLError
 if errors.As(err,&duplicate)&&duplicate.Number==1062 {
  var existing store.Topup;if e:=c.db.WithContext(ctx).Where("ref_type=? AND ref_id=?",store.TopupRefund,idemKey).First(&existing).Error;e!=nil{return nil,e};if e:=verify(existing);e!=nil{return nil,e}
  balance,e:=c.balance(ctx,tenantID);if e!=nil{return nil,e};return &store.RefundResult{AmountCent:target,RefundCent:0,BalanceAfter:balance},nil
 }
 if err!=nil{return nil,err};return result,nil
}

func (c candidate) refundSummaryOrder(ctx context.Context,tenantID uint64,orderNo string)(int64,error){
 var refund int64
 err:=c.db.WithContext(ctx).Transaction(func(tx *gorm.DB)error{
  var account candidateAccount;if err:=tx.Clauses(clause.Locking{Strength:"UPDATE"}).First(&account,tenantID).Error;err!=nil{return err}
  var order store.RechargeOrder;if err:=tx.Clauses(clause.Locking{Strength:"UPDATE"}).Where("order_no=?",orderNo).First(&order).Error;err!=nil{return err}
  if order.TenantID!=tenantID{return errors.New("wrong tenant")};if order.Status==store.RechargeRefunded{return nil};if order.Status!=store.RechargePaid{return errors.New("order not paid")}
  refund=order.AmountCent;if refund>account.Balance{refund=account.Balance};if refund<=0{return errors.New("no refundable balance")}
  entry:=store.Topup{TenantID:tenantID,Source:store.TopupRefund,Amount:-refund,RefType:store.TopupRefund,RefID:orderNo,CreatedAt:time.Now()};if err:=tx.Create(&entry).Error;err!=nil{return err}
  if err:=tx.Model(&account).Update("balance",gorm.Expr("balance - ?",refund)).Error;err!=nil{return err}
  changed:=tx.Model(&order).Where("status=?",store.RechargePaid).Update("status",store.RechargeRefunded);if changed.Error!=nil{return changed.Error};if changed.RowsAffected!=1{return errors.New("order status changed")}
  if c.fail{return errors.New("injected rollback")};return nil
 })
 if err!=nil{return 0,err};return refund,nil
}
