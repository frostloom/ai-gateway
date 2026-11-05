package comparison

import (
 "context"
 "errors"
 "fmt"
 "math"
 "time"
 "github.com/frostloom/ai-gateway/internal/billing/store"
 pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
 "google.golang.org/grpc/codes"
 "google.golang.org/grpc/status"
 "gorm.io/gorm"
 "gorm.io/gorm/clause"
)

// Test-only subscription-specific entry. It deliberately rejects non-subscription
// cases instead of claiming to implement dispatch for all models and plans.
func (c candidate) reserveSubscription(ctx context.Context,req *pb.ReserveRequest)(out *pb.ReserveResponse,err error){
 if req.TenantId<=0||req.EstPromptTokens<0||req.MaxCompletionTokens<0||req.MaxCompletionTokens>math.MaxInt64-req.EstPromptTokens{return nil,status.Error(codes.InvalidArgument,"invalid estimate")}
 err=c.db.WithContext(ctx).Transaction(func(tx *gorm.DB)error{
  var account candidateAccount;if e:=tx.Clauses(clause.Locking{Strength:"UPDATE"}).First(&account,uint64(req.TenantId)).Error;e!=nil{return e}
  var tenant store.Tenant;if e:=tx.First(&tenant,account.ID).Error;e!=nil{return e};if tenant.Status!=0{return status.Error(codes.PermissionDenied,"tenant unavailable")}
  var old store.Bill;e:=tx.Where("request_id=? AND phase=?",req.RequestId,store.PhaseReserve).First(&old).Error
  if e==nil {if old.ReserveFingerprint==nil||*old.ReserveFingerprint!=fp(req)||old.Status!=store.StatusPending||old.UsageReportedAt!=nil{return status.Error(codes.FailedPrecondition,"invalid reserve replay")};out=&pb.ReserveResponse{Code:pb.ReserveCode_RESERVE_OK,BillId:int64(old.ID),BalanceAfter:account.Balance};return nil};if !errors.Is(e,gorm.ErrRecordNotFound){return e}
  var model store.Model;if e=tx.Where("model_id=? AND status=0",req.Model).First(&model).Error;e!=nil{return e};tier:=store.TierFromTags(model.TagsList());if tier==""||tier=="免费"{return errors.New("not a subscription tier")}
  lookup:=store.NewStore(tx,nil);sub,e:=lookup.GetActiveSubscription(ctx,account.ID);if e!=nil{return e};if sub==nil||sub.PlanType!=store.PlanTypeRecurring{return errors.New("no eligible subscription")}
  plan,e:=lookup.GetPlan(ctx,sub.PlanID);if e!=nil{return e};if plan==nil||plan.Status!=0{return errors.New("subscription plan unavailable")};quota:=plan.TierQuotaFor(tier);hours:=plan.RefreshHours;if hours<=0{hours=5};since:=time.Now().Add(-time.Duration(hours)*time.Hour)
  type group struct{Model string;ReservedTier *string;Consumed int64;Unknown int64};var groups []group
  e=tx.Model(&store.Bill{}).Where("tenant_id = ? AND phase = ? AND funding = ? AND status IN (?, ?) AND (created_at >= ? OR (status = ? AND usage_reported_at IS NULL))",account.ID,store.PhaseReserve,store.FundingSubscription,store.StatusPending,store.StatusSettled,since,store.StatusPending).
   Select("model, reserved_tier, COALESCE(SUM(CASE WHEN usage_reported_at IS NULL THEN COALESCE(reserved_tokens,0) ELSE COALESCE(prompt_tokens,0)+COALESCE(completion_tokens,0) END),0) AS consumed, SUM(CASE WHEN (usage_reported_at IS NULL AND reserved_tokens IS NULL) OR (usage_reported_at IS NOT NULL AND (prompt_tokens IS NULL OR completion_tokens IS NULL)) THEN 1 ELSE 0 END) AS unknown").Group("model, reserved_tier").Scan(&groups).Error;if e!=nil{return e}
  var used int64;for _,g:=range groups{if g.Unknown>0{return errors.New("unresolved legacy reservation")};groupTier:="";if g.ReservedTier!=nil{groupTier=*g.ReservedTier}else{m,e:=lookup.GetModelByModelID(ctx,g.Model);if e!=nil{return e};if m==nil{return errors.New("reservation model unavailable")};groupTier=store.TierFromTags(m.TagsList())};if groupTier==""{return errors.New("reservation tier unavailable")};if groupTier==tier{if g.Consumed<0||used>math.MaxInt64-g.Consumed{return errors.New("invalid consumed")};used+=g.Consumed}}
  estimate:=req.EstPromptTokens+req.MaxCompletionTokens;if quota<0||estimate>quota||used>quota-estimate{out=&pb.ReserveResponse{Code:pb.ReserveCode_RESERVE_SUB_QUOTA};return nil}
  fingerprint:=fp(req);row:=store.Bill{RequestID:req.RequestId,ReserveFingerprint:&fingerprint,Phase:store.PhaseReserve,Status:store.StatusPending,TenantID:account.ID,APIKeyID:uint64(req.ApiKeyId),Model:req.Model,Funding:store.FundingSubscription,PreQuota:0,ReservedTokens:&estimate,ReservedTier:&tier,InPriceCent:&model.InputPriceCent,OutPriceCent:&model.OutputPriceCent,CreatedAt:time.Now()}
  if req.ProviderId>0{pid:=uint64(req.ProviderId);row.ProviderID=&pid};if e=tx.Create(&row).Error;e!=nil{return e};if e=candidateEvent(tx,&row,store.PhaseReserve,0);e!=nil{return e};if c.fail{return fmt.Errorf("injected subscription rollback")};out=&pb.ReserveResponse{Code:pb.ReserveCode_RESERVE_OK,BillId:int64(row.ID),BalanceAfter:account.Balance};return nil
 });return
}

// Test-only terminal operation for subscription rows: no cash delta. This
// does not replace or route the existing cash terminal entry points.
func (c candidate) terminalSubscription(ctx context.Context,id string,settle bool)(ok bool,err error){
 var initial store.Bill;if err=c.db.WithContext(ctx).Where("request_id=? AND phase=?",id,store.PhaseReserve).First(&initial).Error;err!=nil{return false,err}
 err=c.db.WithContext(ctx).Transaction(func(tx *gorm.DB)error{
  var account candidateAccount;if e:=tx.Clauses(clause.Locking{Strength:"UPDATE"}).First(&account,initial.TenantID).Error;e!=nil{return e}
  var row store.Bill;if e:=tx.Clauses(clause.Locking{Strength:"UPDATE"}).Where("request_id=? AND phase=?",id,store.PhaseReserve).First(&row).Error;e!=nil{return e}
  if row.Funding!=store.FundingSubscription||row.PreQuota!=0{return errors.New("not a zero-cash subscription reservation")}
  if row.Status!=store.StatusPending {ok=(settle&&row.Status==store.StatusSettled)||(!settle&&row.Status==store.StatusReversed);return nil}
  phase:=store.PhaseReverse;if settle {
   if row.UsageReportedAt==nil||row.PromptTokens==nil||row.CompletionTokens==nil{return status.Error(codes.FailedPrecondition,"usage not reported")}
   zero:=int64(0);sr:=row;sr.ID=0;sr.Phase=store.PhaseSettle;sr.Status=store.StatusSettled;sr.ActualQuota=&zero;sr.DeltaQuota=&zero;sr.ReserveFingerprint=nil
   if e:=tx.Create(&sr).Error;e!=nil{return e};if e:=candidateEvent(tx,&sr,store.PhaseSettle,0);e!=nil{return e};phase=store.PhaseSettle;row.Status=store.StatusSettled
  }else{if e:=candidateEvent(tx,&row,store.PhaseReverse,0);e!=nil{return e};row.Status=store.StatusReversed}
  changed:=tx.Model(&row).Where("status=?",store.StatusPending).Update("status",row.Status);if changed.Error!=nil{return changed.Error};if changed.RowsAffected!=1{return errors.New("terminal state changed")};if c.fail{return fmt.Errorf("injected %s rollback",phase)};ok=true;return nil
 });return
}
