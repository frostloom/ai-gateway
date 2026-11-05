package comparison

import (
 "context"
 "fmt"
 "os"
 "sync"
 "sync/atomic"
 "testing"
 "time"
 "github.com/frostloom/ai-gateway/internal/billing/store"
 pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
 "gorm.io/gorm"
 "gorm.io/gorm/logger"
)

// Diagnostic compares two different accounts with equivalent synthetic plans.
func TestIsolatedSummarySubscriptionMismatch(t *testing.T){
 if os.Getenv("TEST_SUMMARY_SUBSCRIPTION")!="1"{t.Skip("isolated only")}
 db,r:=isolatedDB(t);db.Logger=db.Logger.LogMode(logger.Silent);s:=store.NewStore(db,r);c:=candidate{db:db};ctx:=context.Background();if err:=s.AutoMigrate();err!=nil{t.Fatal(err)};if err:=c.migrate();err!=nil{t.Fatal(err)}
 id:=fmt.Sprintf("summary-sub-%d",time.Now().UnixNano());model:=store.Model{Vendor:"test",ModelID:id,Name:id,InputPriceCent:1000000,OutputPriceCent:1000000,Tags:`["文本"]`};if err:=db.Create(&model).Error;err!=nil{t.Fatal(err)}
 plan:=store.Plan{Name:id,PlanType:store.PlanTypeRecurring,PriceMoney:1,ValidityDays:30,RefreshHours:5,TierQuota:`{"文本":300}`};if err:=db.Create(&plan).Error;err!=nil{t.Fatal(err)}
 tenants:=[]store.Tenant{{Name:id+"-current",InitialQuota:0},{Name:id+"-candidate",InitialQuota:0}}
 for i:=range tenants {v:=&tenants[i];if err:=db.Create(v).Error;err!=nil{t.Fatal(err)};if err:=r.Set(ctx,store.BalanceKey(v.ID),0,0).Err();err!=nil{t.Fatal(err)};if _,err:=s.SubscribePlan(ctx,v.ID,plan.ID);err!=nil{t.Fatal(err)}}
 if err:=db.Create(&candidateAccount{ID:tenants[1].ID,Initial:0,Balance:0}).Error;err!=nil{t.Fatal(err)}
 req:=func(rid string,tenant uint64)*pb.ReserveRequest{return &pb.ReserveRequest{RequestId:rid,TenantId:int64(tenant),ApiKeyId:1,Model:id,EstPromptTokens:100}}
 current,err:=s.Reserve(ctx,req(id+"-current",tenants[0].ID));if err!=nil||current.Code!=pb.ReserveCode_RESERVE_OK{t.Fatal(current,err)}
 var row store.Bill;if err:=db.Where("request_id=? AND phase=?",id+"-current",store.PhaseReserve).First(&row).Error;err!=nil{t.Fatal(err)};if row.Funding!=store.FundingSubscription||row.PreQuota!=0{t.Fatalf("funding=%s pre=%d",row.Funding,row.PreQuota)}
 candidateResult,err:=c.reserve(ctx,req(id+"-candidate",tenants[1].ID));if err!=nil||candidateResult.Code!=pb.ReserveCode_RESERVE_INSUFFICIENT{t.Fatalf("candidate=%v err=%v",candidateResult,err)}
 t.Logf("current subscription accepted with zero cash and pre=0; candidate returned %s",candidateResult.Code)
}

type subscriptionRaceContextKey struct{}
// A diagnostic: accepted concurrent usage above quota is the observed risk,
// so a PASS here means the issue was reproduced, not that the design is safe.
func TestIsolatedSummarySubscriptionUsageRace(t *testing.T){
 if os.Getenv("TEST_SUMMARY_SUBSCRIPTION")!="1"{t.Skip("isolated only")}
 for _,mode:=range []string{"current","candidate"}{t.Run(mode,func(t *testing.T){
  db,r:=isolatedDB(t);db.Logger=db.Logger.LogMode(logger.Silent);s:=store.NewStore(db,r);c:=candidate{db:db};ctx:=context.Background();if err:=s.AutoMigrate();err!=nil{t.Fatal(err)};if err:=c.migrate();err!=nil{t.Fatal(err)}
  id:=fmt.Sprintf("summary-sub-race-%s-%d",mode,time.Now().UnixNano());tenant:=store.Tenant{Name:id,InitialQuota:0};model:=store.Model{Vendor:"test",ModelID:id,Name:id,InputPriceCent:1000000,OutputPriceCent:1000000,Tags:`["文本"]`};plan:=store.Plan{Name:id,PlanType:store.PlanTypeRecurring,PriceMoney:1,ValidityDays:30,RefreshHours:5,TierQuota:`{"文本":300}`}
  if err:=db.Create(&tenant).Error;err!=nil{t.Fatal(err)};if err:=db.Create(&model).Error;err!=nil{t.Fatal(err)};if err:=db.Create(&plan).Error;err!=nil{t.Fatal(err)};if err:=r.Set(ctx,store.BalanceKey(tenant.ID),0,0).Err();err!=nil{t.Fatal(err)};if _,err:=s.SubscribePlan(ctx,tenant.ID,plan.ID);err!=nil{t.Fatal(err)};if err:=db.Create(&candidateAccount{ID:tenant.ID,Initial:0,Balance:0}).Error;err!=nil{t.Fatal(err)}
  first:=&pb.ReserveRequest{RequestId:id+"-first",TenantId:int64(tenant.ID),ApiKeyId:1,Model:id,EstPromptTokens:100};second:=&pb.ReserveRequest{RequestId:id+"-second",TenantId:int64(tenant.ID),ApiKeyId:1,Model:id,EstPromptTokens:100}
  reserve:=func(ctx context.Context,req *pb.ReserveRequest)(*pb.ReserveResponse,error){if mode=="current"{return s.Reserve(ctx,req)};return c.reserveSubscription(ctx,req)}
  one,err:=reserve(ctx,first);if err!=nil||one.Code!=pb.ReserveCode_RESERVE_OK{t.Fatal(one,err)}
  entered:=make(chan struct{});release:=make(chan struct{});var fired atomic.Bool
  if err=db.Callback().Row().After("gorm:row").Register("test:pause_subscription_sum",func(tx *gorm.DB){if tx.Statement.Table=="bills"&&tx.Statement.Context.Value(subscriptionRaceContextKey{})!=nil&&fired.CompareAndSwap(false,true){close(entered);<-release}});err!=nil{t.Fatal(err)};defer db.Callback().Row().Remove("test:pause_subscription_sum")
  type result struct{value *pb.ReserveResponse;err error};done:=make(chan result,1);go func(){v,e:=reserve(context.WithValue(ctx,subscriptionRaceContextKey{},true),second);done<-result{v,e}}()
  select{case <-entered:case <-time.After(5*time.Second):close(release);t.Fatal("did not reach subscription sum")}
  usage:=&pb.ReportUsageRequest{RequestId:first.RequestId,PromptTokens:300};if mode=="current"{err=s.ReportUsage(ctx,usage)}else{err=c.usage(ctx,usage)};close(release);v:=<-done;if err!=nil{t.Fatal(err)};if v.err!=nil||v.value.Code!=pb.ReserveCode_RESERVE_OK{t.Fatalf("expected observed stale acceptance result=%v err=%v",v.value,v.err)}
  var firstRow,secondRow store.Bill;if err=db.Where("request_id=? AND phase=?",first.RequestId,store.PhaseReserve).First(&firstRow).Error;err!=nil{t.Fatal(err)};if err=db.Where("request_id=? AND phase=?",second.RequestId,store.PhaseReserve).First(&secondRow).Error;err!=nil{t.Fatal(err)}
  if firstRow.PromptTokens==nil||*firstRow.PromptTokens!=300||secondRow.ReservedTokens==nil||*secondRow.ReservedTokens!=100{t.Fatalf("usage=%+v reserved=%+v",firstRow.PromptTokens,secondRow.ReservedTokens)}
  t.Logf("mode=%s accepted second estimate=100 after first usage=300 committed; effective 400 > quota 300",mode)
 })}
}

func TestIsolatedSummarySubscriptionReserve(t *testing.T){
 if os.Getenv("TEST_SUMMARY_SUBSCRIPTION")!="1"{t.Skip("isolated only")}
 db,r:=isolatedDB(t);db.Logger=db.Logger.LogMode(logger.Silent);s:=store.NewStore(db,r);c:=candidate{db:db};ctx:=context.Background();if err:=s.AutoMigrate();err!=nil{t.Fatal(err)};if err:=c.migrate();err!=nil{t.Fatal(err)}
 id:=fmt.Sprintf("summary-sub-reserve-%d",time.Now().UnixNano());model:=store.Model{Vendor:"test",ModelID:id,Name:id,InputPriceCent:1000000,OutputPriceCent:1000000,Tags:`["文本"]`};if err:=db.Create(&model).Error;err!=nil{t.Fatal(err)}
 tenant:=store.Tenant{Name:id,InitialQuota:0};if err:=db.Create(&tenant).Error;err!=nil{t.Fatal(err)};if err:=db.Create(&candidateAccount{ID:tenant.ID,Initial:0,Balance:0}).Error;err!=nil{t.Fatal(err)};if err:=r.Set(ctx,store.BalanceKey(tenant.ID),0,0).Err();err!=nil{t.Fatal(err)}
 plan:=store.Plan{Name:id,PlanType:store.PlanTypeRecurring,PriceMoney:1,ValidityDays:30,RefreshHours:5,TierQuota:`{"文本":300}`};if err:=db.Create(&plan).Error;err!=nil{t.Fatal(err)};if _,err:=s.SubscribePlan(ctx,tenant.ID,plan.ID);err!=nil{t.Fatal(err)}
 bad:=c;bad.fail=true;if _,err:=bad.reserveSubscription(ctx,&pb.ReserveRequest{RequestId:id+"-rollback",TenantId:int64(tenant.ID),ApiKeyId:1,Model:id,EstPromptTokens:100});err==nil{t.Fatal("expected subscription transaction rollback")}
 var rollbackBills,rollbackEvents int64;if err:=db.Model(&store.Bill{}).Where("request_id=?",id+"-rollback").Count(&rollbackBills).Error;err!=nil{t.Fatal(err)};if err:=db.Model(&store.OutboxEvent{}).Where("event_key=?",id+"-rollback:reserve").Count(&rollbackEvents).Error;err!=nil{t.Fatal(err)};if rollbackBills!=0||rollbackEvents!=0{t.Fatalf("rollback bill=%d event=%d",rollbackBills,rollbackEvents)}
 type result struct{id string;value *pb.ReserveResponse;err error};results:=make(chan result,8);var wg sync.WaitGroup
 for i:=0;i<8;i++{wg.Add(1);go func(i int){defer wg.Done();rid:=fmt.Sprintf("%s-%d",id,i);v,e:=c.reserveSubscription(ctx,&pb.ReserveRequest{RequestId:rid,TenantId:int64(tenant.ID),ApiKeyId:1,Model:id,EstPromptTokens:100});results<-result{rid,v,e}}(i)};wg.Wait();close(results)
 ok,quota:=0,0;winner:="";for v:=range results{if v.err!=nil{t.Error(v.err);continue};switch v.value.Code{case pb.ReserveCode_RESERVE_OK:ok++;winner=v.id;case pb.ReserveCode_RESERVE_SUB_QUOTA:quota++;default:t.Errorf("unexpected code=%v",v.value.Code)}}
 if ok!=3||quota!=5{t.Fatalf("accepted=%d quota=%d; want 3/5",ok,quota)}
 replay:=&pb.ReserveRequest{RequestId:winner,TenantId:int64(tenant.ID),ApiKeyId:1,Model:id,EstPromptTokens:100};repeat,err:=c.reserveSubscription(ctx,replay);if err!=nil||repeat.Code!=pb.ReserveCode_RESERVE_OK{t.Fatal(repeat,err)};replay.EstPromptTokens++;if _,err=c.reserveSubscription(ctx,replay);err==nil{t.Fatal("different estimate replay accepted")}
 var rows,events int64;if err=db.Model(&store.Bill{}).Where("tenant_id=? AND phase=? AND funding=? AND pre_quota=0",tenant.ID,store.PhaseReserve,store.FundingSubscription).Count(&rows).Error;err!=nil{t.Fatal(err)};if err=db.Model(&store.OutboxEvent{}).Where("event_key LIKE ?",id+"-%:reserve").Count(&events).Error;err!=nil{t.Fatal(err)};balance,err:=c.balance(ctx,tenant.ID);if err!=nil||balance!=0||rows!=3||events!=3{t.Fatalf("balance=%d bills=%d outbox=%d err=%v",balance,rows,events,err)}
 t.Logf("8 concurrent subscriptions accepted=%d quota=%d; reserve bills=%d outbox=%d cash=%d; replay and rollback checked",ok,quota,rows,events,balance)
}

func TestIsolatedSummarySubscriptionSettle(t *testing.T){
 if os.Getenv("TEST_SUMMARY_SUBSCRIPTION")!="1"{t.Skip("isolated only")}
 db,r:=isolatedDB(t);db.Logger=db.Logger.LogMode(logger.Silent);s:=store.NewStore(db,r);c:=candidate{db:db};ctx:=context.Background();if err:=s.AutoMigrate();err!=nil{t.Fatal(err)};if err:=c.migrate();err!=nil{t.Fatal(err)}
 id:=fmt.Sprintf("summary-sub-settle-%d",time.Now().UnixNano());model:=store.Model{Vendor:"test",ModelID:id,Name:id,InputPriceCent:1000000,OutputPriceCent:1000000,Tags:`["文本"]`};if err:=db.Create(&model).Error;err!=nil{t.Fatal(err)}
 tenant:=store.Tenant{Name:id,InitialQuota:500};if err:=db.Create(&tenant).Error;err!=nil{t.Fatal(err)};if err:=db.Create(&candidateAccount{ID:tenant.ID,Initial:500,Balance:500}).Error;err!=nil{t.Fatal(err)}
 plan:=store.Plan{Name:id,PlanType:store.PlanTypeRecurring,PriceMoney:1,ValidityDays:30,RefreshHours:5,TierQuota:`{"文本":300}`};if err:=db.Create(&plan).Error;err!=nil{t.Fatal(err)};if _,err:=s.SubscribePlan(ctx,tenant.ID,plan.ID);err!=nil{t.Fatal(err)}
 req:=&pb.ReserveRequest{RequestId:id+"-request",TenantId:int64(tenant.ID),ApiKeyId:1,Model:id,EstPromptTokens:100};reserved,err:=c.reserveSubscription(ctx,req);if err!=nil||reserved.Code!=pb.ReserveCode_RESERVE_OK{t.Fatal(reserved,err)}
 if err=c.usage(ctx,&pb.ReportUsageRequest{RequestId:req.RequestId,PromptTokens:50});err!=nil{t.Fatal(err)}
 bad:=c;bad.fail=true;if _,err=bad.terminalSubscription(ctx,req.RequestId,true);err==nil{t.Fatal("expected subscription settle rollback")};var n int64;if err=db.Model(&store.OutboxEvent{}).Where("event_key=?",req.RequestId+":settle").Count(&n).Error;err!=nil||n!=0{t.Fatalf("rollback events=%d err=%v",n,err)}
 settled,err:=c.terminalSubscription(ctx,req.RequestId,true);if err!=nil||!settled{t.Fatal(settled,err)};settled,err=c.terminalSubscription(ctx,req.RequestId,true);if err!=nil||!settled{t.Fatal(settled,err)};reversed,err:=c.terminalSubscription(ctx,req.RequestId,false);if err!=nil||reversed{t.Fatal(reversed,err)}
 balance,err:=c.balance(ctx,tenant.ID);if err!=nil||balance!=500{t.Fatalf("subscription settle changed cash balance=%d err=%v",balance,err)}
 var row store.Bill;if err=db.Where("request_id=? AND phase=?",req.RequestId,store.PhaseSettle).First(&row).Error;err!=nil||row.ActualQuota==nil||*row.ActualQuota!=0||row.DeltaQuota==nil||*row.DeltaQuota!=0{t.Fatalf("settle row=%+v err=%v",row,err)};if err=db.Model(&store.OutboxEvent{}).Where("event_key=?",req.RequestId+":settle").Count(&n).Error;err!=nil||n!=1{t.Fatalf("settle events=%d err=%v",n,err)}
 req.RequestId=id+"-reverse";reserved,err=c.reserveSubscription(ctx,req);if err!=nil||reserved.Code!=pb.ReserveCode_RESERVE_OK{t.Fatal(reserved,err)};reversed,err=c.terminalSubscription(ctx,req.RequestId,false);if err!=nil||!reversed{t.Fatal(reversed,err)};reversed,err=c.terminalSubscription(ctx,req.RequestId,false);if err!=nil||!reversed{t.Fatal(reversed,err)};if err=db.Model(&store.OutboxEvent{}).Where("event_key=?",req.RequestId+":reverse").Count(&n).Error;err!=nil||n!=1{t.Fatalf("reverse events=%d err=%v",n,err)}
 balance,err=c.balance(ctx,tenant.ID);if err!=nil||balance!=500{t.Fatalf("subscription reverse changed balance=%d err=%v",balance,err)};t.Log("subscription settle and reverse: cash unchanged, zero settle, unique events, replay and rollback checked")
}

func TestIsolatedSummarySubscriptionWindowAndLegacy(t *testing.T){
 if os.Getenv("TEST_SUMMARY_SUBSCRIPTION")!="1"{t.Skip("isolated only")}
 db,r:=isolatedDB(t);db.Logger=db.Logger.LogMode(logger.Silent);s:=store.NewStore(db,r);c:=candidate{db:db};ctx:=context.Background();if err:=s.AutoMigrate();err!=nil{t.Fatal(err)};if err:=c.migrate();err!=nil{t.Fatal(err)}
 id:=fmt.Sprintf("summary-sub-window-%d",time.Now().UnixNano());tenant:=store.Tenant{Name:id,InitialQuota:0};model:=store.Model{Vendor:"test",ModelID:id,Name:id,InputPriceCent:1000000,OutputPriceCent:1000000,Tags:`["文本"]`};plan:=store.Plan{Name:id,PlanType:store.PlanTypeRecurring,PriceMoney:1,ValidityDays:30,RefreshHours:5,TierQuota:`{"文本":300}`}
 if err:=db.Create(&tenant).Error;err!=nil{t.Fatal(err)};if err:=db.Create(&model).Error;err!=nil{t.Fatal(err)};if err:=db.Create(&plan).Error;err!=nil{t.Fatal(err)};if _,err:=s.SubscribePlan(ctx,tenant.ID,plan.ID);err!=nil{t.Fatal(err)};if err:=db.Create(&candidateAccount{ID:tenant.ID,Initial:0,Balance:0}).Error;err!=nil{t.Fatal(err)}
 first:=&pb.ReserveRequest{RequestId:id+"-first",TenantId:int64(tenant.ID),ApiKeyId:1,Model:id,EstPromptTokens:100};res,err:=c.reserveSubscription(ctx,first);if err!=nil||res.Code!=pb.ReserveCode_RESERVE_OK{t.Fatal(res,err)}
 if err=db.Model(&store.Bill{}).Where("request_id=? AND phase=?",first.RequestId,store.PhaseReserve).Update("created_at",time.Now().Add(-6*time.Hour)).Error;err!=nil{t.Fatal(err)}
 next:=&pb.ReserveRequest{RequestId:id+"-next",TenantId:int64(tenant.ID),ApiKeyId:1,Model:id,EstPromptTokens:250};res,err=c.reserveSubscription(ctx,next);if err!=nil||res.Code!=pb.ReserveCode_RESERVE_SUB_QUOTA{t.Fatalf("old unresolved pending must consume quota: %v %v",res,err)}
 if err=c.usage(ctx,&pb.ReportUsageRequest{RequestId:first.RequestId,PromptTokens:50});err!=nil{t.Fatal(err)};settled,err:=c.terminalSubscription(ctx,first.RequestId,true);if err!=nil||!settled{t.Fatal(settled,err)}
 next.EstPromptTokens=300;res,err=c.reserveSubscription(ctx,next);if err!=nil||res.Code!=pb.ReserveCode_RESERVE_OK{t.Fatalf("old settled usage should expire: %v %v",res,err)};rev,err:=c.terminalSubscription(ctx,next.RequestId,false);if err!=nil||!rev{t.Fatal(rev,err)}
 tier:="文本";legacy:=store.Bill{RequestID:id+"-legacy",Phase:store.PhaseReserve,Status:store.StatusPending,TenantID:tenant.ID,APIKeyID:1,Model:id,Funding:store.FundingSubscription,ReservedTier:&tier,CreatedAt:time.Now().Add(-6*time.Hour)};if err=db.Create(&legacy).Error;err!=nil{t.Fatal(err)}
 newReq:=&pb.ReserveRequest{RequestId:id+"-after-legacy",TenantId:int64(tenant.ID),ApiKeyId:1,Model:id,EstPromptTokens:1};if _,err=c.reserveSubscription(ctx,newReq);err==nil{t.Fatal("unknown legacy reservation must fail closed")}
 cash,err:=c.balance(ctx,tenant.ID);if err!=nil||cash!=0{t.Fatalf("cash=%d err=%v",cash,err)};t.Log("old unknown pending counted; settled usage expired; unresolved legacy reservation refused")
}
