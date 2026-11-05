package comparison

import (
 "context"
 "database/sql"
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

func TestIsolatedSummaryHistoryMixed(t *testing.T){
 if os.Getenv("TEST_SUMMARY_HISTORY_MIXED")!="1"{t.Skip("isolated only")}
 db,_:=isolatedDB(t);db.Logger=db.Logger.LogMode(logger.Silent);c:=candidate{db:db};ctx:=context.Background();if err:=c.migrate();err!=nil{t.Fatal(err)}
 modelID:=fmt.Sprintf("summary-mixed-%d",time.Now().UnixNano());m:=store.Model{Vendor:"test",ModelID:modelID,Name:modelID,InputPriceCent:1000000,OutputPriceCent:1000000,Tags:"[]"};if err:=db.Create(&m).Error;err!=nil{t.Fatal(err)}
 ids:=make([]uint64,4);for i:=range ids {tenant:=store.Tenant{Name:fmt.Sprintf("%s-%d",modelID,i),InitialQuota:100000};if err:=db.Create(&tenant).Error;err!=nil{t.Fatal(err)};ids[i]=tenant.ID
  for start:=0;start<1000;start+=250 {rows:=make([]store.Bill,0,250);for j:=start;j<start+250;j++{v:=int64(1);rows=append(rows,store.Bill{TenantID:tenant.ID,RequestID:fmt.Sprintf("history-%d-%d",tenant.ID,j),Phase:store.PhaseSettle,Status:store.StatusSettled,Funding:store.FundingBalance,Model:"synthetic",ActualQuota:&v})};if err:=db.CreateInBatches(rows,250).Error;err!=nil{t.Fatal(err)}}
  if err:=db.Create(&candidateAccount{ID:tenant.ID,Initial:100000,Balance:99000}).Error;err!=nil{t.Fatal(err)}
 }
 errs:=make(chan error,512);stop:=make(chan struct{});readerDone:=make(chan struct{});var checked atomic.Int64
 go func(){defer close(readerDone);for {select{case <-stop:return;default:}
  for _,id:=range ids {err:=db.WithContext(ctx).Transaction(func(tx *gorm.DB)error{var a candidateAccount;if err:=tx.First(&a,id).Error;err!=nil{return err};ledger,err:=store.LedgerBalance(ctx,tx,id);if err!=nil{return err};if a.Balance!=ledger{return fmt.Errorf("snapshot tenant=%d balance=%d ledger=%d",id,a.Balance,ledger)};return nil},&sql.TxOptions{Isolation:sql.LevelRepeatableRead,ReadOnly:true});if err!=nil{errs<-err;return};checked.Add(1)}
 }}()
 const workers,perWorker=8,10;var wg sync.WaitGroup
 for w:=0;w<workers;w++ {wg.Add(1);go func(w int){defer wg.Done();for i:=0;i<perWorker;i++ {
  id:=fmt.Sprintf("mix-%s-%d-%d",modelID,w,i);req:=&pb.ReserveRequest{RequestId:id,TenantId:int64(ids[w%len(ids)]),ApiKeyId:1,Model:modelID,EstPromptTokens:84}
  first,e:=c.reserve(ctx,req);if e!=nil||first.Code!=pb.ReserveCode_RESERVE_OK{errs<-fmt.Errorf("reserve %s %v %v",id,first,e);return}
  repeat,e:=c.reserve(ctx,req);if e!=nil||repeat.BillId!=first.BillId{errs<-fmt.Errorf("reserve retry %s %v %v",id,repeat,e);return}
  usage:=&pb.ReportUsageRequest{RequestId:id,PromptTokens:50};if e=c.usage(ctx,usage);e!=nil{errs<-e;return};if e=c.usage(ctx,usage);e!=nil{errs<-e;return}
  var ok bool;if i%2==0{ok,e=c.settle(ctx,id)}else{ok,e=c.reverse(ctx,id)};if e!=nil||!ok{errs<-fmt.Errorf("terminal %s %v %v",id,ok,e);return}
  if i%2==0{ok,e=c.settle(ctx,id)}else{ok,e=c.reverse(ctx,id)};if e!=nil||!ok{errs<-fmt.Errorf("terminal retry %s %v %v",id,ok,e);return}
 }}(w)}
 wg.Wait();close(stop);<-readerDone;close(errs);for e:=range errs{t.Error(e)}
 if checked.Load()==0{t.Error("no concurrent snapshots checked")};if err:=c.audit(ctx);err!=nil{t.Error(err)}
 var reserve,settle,reverse,events int64;db.Model(&store.Bill{}).Where("request_id LIKE ? AND phase=?","mix-"+modelID+"%",store.PhaseReserve).Count(&reserve);db.Model(&store.Bill{}).Where("request_id LIKE ? AND phase=?","mix-"+modelID+"%",store.PhaseSettle).Count(&settle);db.Model(&store.OutboxEvent{}).Where("event_key LIKE ? AND event_key LIKE ?","mix-"+modelID+"%","%:reverse").Count(&reverse);db.Model(&store.OutboxEvent{}).Where("event_key LIKE ?","mix-"+modelID+"%").Count(&events)
 if reserve!=80||settle!=40||reverse!=40||events!=240{t.Fatalf("reserve=%d settle=%d reverse_events=%d outbox=%d",reserve,settle,reverse,events)}
 t.Logf("synthetic history=4000 accounts=4 mixed_requests=%d settle=%d reverse=%d outbox=%d consistent_snapshot_checks=%d",reserve,settle,reverse,events,checked.Load())
}

// This diagnostic intentionally combines the isolated candidate with the
// project's existing recharge path to expose the missing integration.
func TestIsolatedSummaryRechargeMismatch(t *testing.T){
 if os.Getenv("TEST_SUMMARY_HISTORY_MIXED")!="1"{t.Skip("isolated only")}
 db,r:=isolatedDB(t);db.Logger=db.Logger.LogMode(logger.Silent);c:=candidate{db:db};s:=store.NewStore(db,r);ctx:=context.Background()
 if err:=s.AutoMigrate();err!=nil{t.Fatal(err)};if err:=c.migrate();err!=nil{t.Fatal(err)}
 id:=fmt.Sprintf("summary-recharge-%d",time.Now().UnixNano());tenant:=store.Tenant{Name:id,InitialQuota:500};model:=store.Model{Vendor:"test",ModelID:id,Name:id,InputPriceCent:1000000,OutputPriceCent:1000000,Tags:"[]"}
 if err:=db.Create(&tenant).Error;err!=nil{t.Fatal(err)};if err:=db.Create(&model).Error;err!=nil{t.Fatal(err)};if err:=db.Create(&candidateAccount{ID:tenant.ID,Initial:500,Balance:500}).Error;err!=nil{t.Fatal(err)};if err:=r.Set(ctx,store.BalanceKey(tenant.ID),500,0).Err();err!=nil{t.Fatal(err)}
 order,err:=s.CreateRechargeOrder(ctx,tenant.ID,1,id+"-order");if err!=nil{t.Fatal(err)};if _,err=s.PayRecharge(ctx,order.OrderNo);err!=nil{t.Fatal(err)}
 summary,err:=c.balance(ctx,tenant.ID);if err!=nil{t.Fatal(err)};ledger,err:=store.LedgerBalance(ctx,db,tenant.ID);if err!=nil{t.Fatal(err)}
 result,err:=c.reserve(ctx,&pb.ReserveRequest{RequestId:id+"-request",TenantId:int64(tenant.ID),ApiKeyId:1,Model:id,EstPromptTokens:500});if err!=nil{t.Fatal(err)}
 if summary!=500||ledger!=600||result.Code!=pb.ReserveCode_RESERVE_INSUFFICIENT{t.Fatalf("unexpected diagnostic summary=%d ledger=%d result=%v",summary,ledger,result)}
 t.Logf("existing simulated recharge committed; candidate summary=%d ledger=%d; 600-cent reserve rejected",summary,ledger)
}
