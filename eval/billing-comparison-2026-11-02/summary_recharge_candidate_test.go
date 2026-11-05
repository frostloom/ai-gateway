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

func TestIsolatedSummaryRechargeConsistent(t *testing.T) {
 if os.Getenv("TEST_SUMMARY_RECHARGE")!="1" {t.Skip("isolated only")}
 db,r:=isolatedDB(t);db.Logger=db.Logger.LogMode(logger.Silent);s:=store.NewStore(db,r);c:=candidate{db:db};ctx:=context.Background()
 if err:=s.AutoMigrate();err!=nil{t.Fatal(err)};if err:=c.migrate();err!=nil{t.Fatal(err)}
 id:=fmt.Sprintf("summary-pay-%d",time.Now().UnixNano());tenant:=store.Tenant{Name:id,InitialQuota:500}
 if err:=db.Create(&tenant).Error;err!=nil{t.Fatal(err)};if err:=db.Create(&candidateAccount{ID:tenant.ID,Initial:500,Balance:500}).Error;err!=nil{t.Fatal(err)};if err:=r.Set(ctx,store.BalanceKey(tenant.ID),500,0).Err();err!=nil{t.Fatal(err)}
 order,err:=s.CreateRechargeOrder(ctx,tenant.ID,1,id+"-order");if err!=nil{t.Fatal(err)}
 bad:=c;bad.fail=true
 if _,err=bad.paySummary(ctx,order.OrderNo);err==nil{t.Fatal("expected pay rollback")}
 var pending store.RechargeOrder;if err=db.First(&pending,order.ID).Error;err!=nil||pending.Status!=store.RechargePending{t.Fatalf("after rollback order=%s err=%v",pending.Status,err)}
 var count int64;if err=db.Model(&store.Topup{}).Where("ref_id=?",order.OrderNo).Count(&count).Error;err!=nil||count!=0{t.Fatalf("after rollback topups=%d err=%v",count,err)}
 var wg sync.WaitGroup;errs:=make(chan error,16);for i:=0;i<16;i++{wg.Add(1);go func(){defer wg.Done();_,e:=c.paySummary(ctx,order.OrderNo);if e!=nil{errs<-e}}()};wg.Wait();close(errs);for e:=range errs{t.Error(e)}
 summary,err:=c.balance(ctx,tenant.ID);if err!=nil{t.Fatal(err)};ledger,err:=store.LedgerBalance(ctx,db,tenant.ID);if err!=nil{t.Fatal(err)}
 if summary!=600||ledger!=600 {t.Fatalf("after pay summary=%d ledger=%d; both must equal 600",summary,ledger)}
 if err=db.Model(&store.Topup{}).Where("ref_id=? AND ref_type=?",order.OrderNo,store.TopupRecharge).Count(&count).Error;err!=nil||count!=1{t.Fatalf("pay topups=%d err=%v",count,err)}
 if _,err=bad.refundSummaryOrder(ctx,tenant.ID,order.OrderNo);err==nil{t.Fatal("expected refund rollback")}
 summary,err=c.balance(ctx,tenant.ID);if err!=nil||summary!=600{t.Fatalf("after refund rollback balance=%d err=%v",summary,err)}
 if err=db.First(&pending,order.ID).Error;err!=nil||pending.Status!=store.RechargePaid{t.Fatalf("after refund rollback order=%s err=%v",pending.Status,err)}
 refund,err:=c.refundSummaryOrder(ctx,tenant.ID,order.OrderNo);if err!=nil||refund!=100{t.Fatalf("refund=%d err=%v",refund,err)}
 refund,err=c.refundSummaryOrder(ctx,tenant.ID,order.OrderNo);if err!=nil||refund!=0{t.Fatalf("duplicate refund=%d err=%v",refund,err)}
 summary,err=c.balance(ctx,tenant.ID);if err!=nil{t.Fatal(err)};ledger,err=store.LedgerBalance(ctx,db,tenant.ID);if err!=nil||summary!=500||ledger!=500{t.Fatalf("after refund summary=%d ledger=%d err=%v",summary,ledger,err)}
 if err=db.Model(&store.Topup{}).Where("ref_id=? AND ref_type=?",order.OrderNo,store.TopupRefund).Count(&count).Error;err!=nil||count!=1{t.Fatalf("refund topups=%d err=%v",count,err)}
 t.Log("16 concurrent payments converged; injected pay/refund transactions rolled back; duplicate refund did not repeat; ledger and summary match")
}

func TestIsolatedSummaryRefundReserveRace(t *testing.T){
 if os.Getenv("TEST_SUMMARY_RECHARGE")!="1"{t.Skip("isolated only")}
 db,r:=isolatedDB(t);db.Logger=db.Logger.LogMode(logger.Silent);s:=store.NewStore(db,r);c:=candidate{db:db};ctx:=context.Background();if err:=s.AutoMigrate();err!=nil{t.Fatal(err)};if err:=c.migrate();err!=nil{t.Fatal(err)}
 id:=fmt.Sprintf("summary-refund-race-%d",time.Now().UnixNano());m:=store.Model{Vendor:"test",ModelID:id,Name:id,InputPriceCent:1000000,OutputPriceCent:1000000,Tags:"[]"};if err:=db.Create(&m).Error;err!=nil{t.Fatal(err)}
 var reserveWins,refundWins int
 for i:=0;i<8;i++ {
  tenant:=store.Tenant{Name:fmt.Sprintf("%s-%d",id,i),InitialQuota:500};if err:=db.Create(&tenant).Error;err!=nil{t.Fatal(err)};if err:=db.Create(&candidateAccount{ID:tenant.ID,Initial:500,Balance:500}).Error;err!=nil{t.Fatal(err)}
  order,err:=s.CreateRechargeOrder(ctx,tenant.ID,1,fmt.Sprintf("%s-order-%d",id,i));if err!=nil{t.Fatal(err)};if _,err=c.paySummary(ctx,order.OrderNo);err!=nil{t.Fatal(err)}
  req:=&pb.ReserveRequest{RequestId:fmt.Sprintf("%s-req-%d",id,i),TenantId:int64(tenant.ID),ApiKeyId:1,Model:id,EstPromptTokens:500}
  var reserve *pb.ReserveResponse;var reserveErr,refundErr error;var refund int64;start:=make(chan struct{});var wg sync.WaitGroup;wg.Add(2)
  go func(){defer wg.Done();<-start;reserve,reserveErr=c.reserve(ctx,req)}();go func(){defer wg.Done();<-start;refund,refundErr=c.refundSummaryOrder(ctx,tenant.ID,order.OrderNo)}();close(start);wg.Wait()
  if reserveErr!=nil||reserve==nil{t.Fatalf("reserve error=%v",reserveErr)}
  if reserve.Code==pb.ReserveCode_RESERVE_OK {reserveWins++;if refundErr==nil{t.Fatalf("reserve and refund both succeeded: refund=%d",refund)}} else if reserve.Code==pb.ReserveCode_RESERVE_INSUFFICIENT {refundWins++;if refundErr!=nil||refund!=100{t.Fatalf("refund=%d err=%v",refund,refundErr)}}else{t.Fatalf("unexpected reserve=%v",reserve.Code)}
  summary,err:=c.balance(ctx,tenant.ID);if err!=nil{t.Fatal(err)};ledger,err:=store.LedgerBalance(ctx,db,tenant.ID);if err!=nil||summary!=ledger{t.Fatalf("summary=%d ledger=%d err=%v",summary,ledger,err)}
 }
 t.Logf("8 competing reserve/refund attempts: reserve wins=%d refund wins=%d; all accounts reconciled",reserveWins,refundWins)
}

func TestIsolatedSummaryRefundPartial(t *testing.T){
 if os.Getenv("TEST_SUMMARY_RECHARGE")!="1"{t.Skip("isolated only")}
 db,r:=isolatedDB(t);db.Logger=db.Logger.LogMode(logger.Silent);s:=store.NewStore(db,r);c:=candidate{db:db};ctx:=context.Background();if err:=s.AutoMigrate();err!=nil{t.Fatal(err)};if err:=c.migrate();err!=nil{t.Fatal(err)}
 id:=fmt.Sprintf("summary-partial-%d",time.Now().UnixNano());tenant:=store.Tenant{Name:id,InitialQuota:50};other:=store.Tenant{Name:id+"-other",InitialQuota:50};model:=store.Model{Vendor:"test",ModelID:id,Name:id,InputPriceCent:1000000,OutputPriceCent:1000000,Tags:"[]"}
 for _,v:=range []*store.Tenant{&tenant,&other}{if err:=db.Create(v).Error;err!=nil{t.Fatal(err)};if err:=db.Create(&candidateAccount{ID:v.ID,Initial:50,Balance:50}).Error;err!=nil{t.Fatal(err)}};if err:=db.Create(&model).Error;err!=nil{t.Fatal(err)}
 order,err:=s.CreateRechargeOrder(ctx,tenant.ID,1,id+"-order");if err!=nil{t.Fatal(err)};if _,err=c.paySummary(ctx,order.OrderNo);err!=nil{t.Fatal(err)}
 if _,err=c.refundSummaryOrder(ctx,other.ID,order.OrderNo);err==nil{t.Fatal("cross-tenant refund accepted")}
 res,err:=c.reserve(ctx,&pb.ReserveRequest{RequestId:id+"-req",TenantId:int64(tenant.ID),ApiKeyId:1,Model:id,EstPromptTokens:84});if err!=nil||res.Code!=pb.ReserveCode_RESERVE_OK{t.Fatal(res,err)}
 refund,err:=c.refundSummaryOrder(ctx,tenant.ID,order.OrderNo);if err!=nil||refund!=50{t.Fatalf("partial refund=%d err=%v",refund,err)}
 repeat,err:=c.refundSummaryOrder(ctx,tenant.ID,order.OrderNo);if err!=nil||repeat!=0{t.Fatalf("repeat refund=%d err=%v",repeat,err)}
 bal,err:=c.balance(ctx,tenant.ID);if err!=nil{t.Fatal(err)};ledger,err:=store.LedgerBalance(ctx,db,tenant.ID);if err!=nil||bal!=0||ledger!=0{t.Fatalf("balance=%d ledger=%d err=%v",bal,ledger,err)}
 t.Log("partial refund 50, cross-tenant request rejected, duplicate refunded 0; summary and ledger 0")
}

func TestIsolatedSummaryRechargeConnectionKill(t *testing.T){
 if os.Getenv("TEST_SUMMARY_RECHARGE")!="1"{t.Skip("isolated only")}
 db,r:=isolatedDB(t);db.Logger=db.Logger.LogMode(logger.Silent);s:=store.NewStore(db,r);c:=candidate{db:db};ctx:=context.Background();if err:=s.AutoMigrate();err!=nil{t.Fatal(err)};if err:=c.migrate();err!=nil{t.Fatal(err)}
 id:=fmt.Sprintf("summary-kill-%d",time.Now().UnixNano());tenant:=store.Tenant{Name:id,InitialQuota:500};if err:=db.Create(&tenant).Error;err!=nil{t.Fatal(err)};if err:=db.Create(&candidateAccount{ID:tenant.ID,Initial:500,Balance:500}).Error;err!=nil{t.Fatal(err)}
 order,err:=s.CreateRechargeOrder(ctx,tenant.ID,1,id+"-order");if err!=nil{t.Fatal(err)}
 var fired atomic.Bool;var killErr error
 if err=db.Callback().Update().Before("gorm:update").Register("test:kill_summary_pay_connection",func(tx *gorm.DB){if tx.Statement.Table!="recharge_orders"||!fired.CompareAndSwap(false,true){return};var connID uint64;if killErr=tx.Session(&gorm.Session{NewDB:true}).Raw("SELECT CONNECTION_ID()").Scan(&connID).Error;killErr==nil{killErr=db.Exec(fmt.Sprintf("KILL CONNECTION %d",connID)).Error}});err!=nil{t.Fatal(err)}
 _,err=c.paySummary(ctx,order.OrderNo);db.Callback().Update().Remove("test:kill_summary_pay_connection")
 if !fired.Load()||killErr!=nil||err==nil{t.Fatalf("fired=%v killErr=%v payErr=%v",fired.Load(),killErr,err)}
 var got store.RechargeOrder;if err=db.First(&got,order.ID).Error;err!=nil||got.Status!=store.RechargePending{t.Fatalf("order=%s err=%v",got.Status,err)}
 var n int64;if err=db.Model(&store.Topup{}).Where("ref_id=?",order.OrderNo).Count(&n).Error;err!=nil||n!=0{t.Fatalf("topups=%d err=%v",n,err)}
 bal,err:=c.balance(ctx,tenant.ID);if err!=nil{t.Fatal(err)};ledger,err:=store.LedgerBalance(ctx,db,tenant.ID);if err!=nil||bal!=500||ledger!=500{t.Fatalf("after kill balance=%d ledger=%d err=%v",bal,ledger,err)}
 if _,err=c.paySummary(ctx,order.OrderNo);err!=nil{t.Fatal(err)};bal,err=c.balance(ctx,tenant.ID);if err!=nil{t.Fatal(err)};ledger,err=store.LedgerBalance(ctx,db,tenant.ID);if err!=nil||bal!=600||ledger!=600{t.Fatalf("after retry balance=%d ledger=%d err=%v",bal,ledger,err)}
 if err=db.First(&got,order.ID).Error;err!=nil||got.Status!=store.RechargePaid{t.Fatalf("after retry order=%s err=%v",got.Status,err)};if err=db.Model(&store.Topup{}).Where("ref_id=? AND ref_type=?",order.OrderNo,store.TopupRecharge).Count(&n).Error;err!=nil||n!=1{t.Fatalf("after retry topups=%d err=%v",n,err)}
 t.Log("actual SQL connection killed before commit: order/ledger/summary rolled back; retry committed once")
}
