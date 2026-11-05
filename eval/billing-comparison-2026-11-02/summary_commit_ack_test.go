package comparison

import (
 "context"
 "database/sql/driver"
 "errors"
 "fmt"
 "os"
 "os/exec"
 "strconv"
 "sync"
 "testing"
 "time"

 "github.com/frostloom/ai-gateway/internal/billing/store"
 pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
 "gorm.io/gorm/logger"
)

func TestIsolatedSummaryCommitAckLoss(t *testing.T) {
 if os.Getenv("TEST_SUMMARY_ACK")!="1"{t.Skip("isolated only")}
 db,r:=isolatedDB(t);db.Logger=db.Logger.LogMode(logger.Silent);s:=store.NewStore(db,r);reliable:=candidate{db:db};ctx:=context.Background()
 if err:=s.AutoMigrate();err!=nil{t.Fatal(err)};if err:=reliable.migrate();err!=nil{t.Fatal(err)}
 for _,mode:=range []string{"reserve","usage","settle","reverse","pay","refund_order","refund_amount"}{t.Run(mode,func(t *testing.T){
  id:=fmt.Sprintf("summary-ack-%s-%d",mode,time.Now().UnixNano());tenant:=store.Tenant{Name:id,InitialQuota:500};model:=store.Model{Vendor:"test",ModelID:id,Name:id,InputPriceCent:1000000,OutputPriceCent:1000000,Tags:"[]"}
  if err:=db.Create(&tenant).Error;err!=nil{t.Fatal(err)};if err:=db.Create(&model).Error;err!=nil{t.Fatal(err)};if err:=db.Create(&candidateAccount{ID:tenant.ID,Initial:500,Balance:500}).Error;err!=nil{t.Fatal(err)}
  req:=&pb.ReserveRequest{RequestId:id,TenantId:int64(tenant.ID),ApiKeyId:1,Model:id,EstPromptTokens:84};usage:=&pb.ReportUsageRequest{RequestId:id,PromptTokens:40}
  if mode=="usage"||mode=="settle"||mode=="reverse" {out,err:=reliable.reserve(ctx,req);if err!=nil||out.Code!=pb.ReserveCode_RESERVE_OK{t.Fatal(out,err)}}
  if mode=="settle" {if err:=reliable.usage(ctx,usage);err!=nil{t.Fatal(err)}}
  orderNo:=id+"-order"
  if mode=="pay"||mode=="refund_order" {order,err:=s.CreateRechargeOrder(ctx,tenant.ID,1,orderNo);if err!=nil{t.Fatal(err)};if mode=="refund_order"{if _,err=reliable.paySummary(ctx,order.OrderNo);err!=nil{t.Fatal(err)}}}
  injected:=errCommitAckLost;if os.Getenv("TEST_ACK_BAD_CONNECTION")=="1"{injected=driver.ErrBadConn}
  broken:=candidate{db:recoveryDBWithCommitError(t,injected)}
  call:=func(c candidate)error{switch mode {
   case "reserve":v,e:=c.reserve(ctx,req);if e==nil&&(v==nil||v.Code!=pb.ReserveCode_RESERVE_OK){return fmt.Errorf("invalid reserve replay: %v",v)};return e
   case "usage":return c.usage(ctx,usage)
   case "settle":ok,e:=c.settle(ctx,id);if e==nil&&!ok{return errors.New("settle replay not settled")};return e
   case "reverse":ok,e:=c.reverse(ctx,id);if e==nil&&!ok{return errors.New("reverse replay not reversed")};return e
   case "pay":v,e:=c.paySummary(ctx,orderNo);if e==nil&&(v==nil||v.Status!=store.RechargePaid){return fmt.Errorf("invalid paid replay: %v",v)};return e
   case "refund_order":v,e:=c.refundSummaryOrder(ctx,tenant.ID,orderNo);if e==nil&&v!=0{return fmt.Errorf("repeat refund=%d, want zero",v)};return e
   case "refund_amount":v,e:=c.refundSummaryAmount(ctx,tenant.ID,2,id+"-refund");if e==nil&&(v==nil||v.RefundCent!=0){return fmt.Errorf("invalid refund replay: %v",v)};return e
   };panic("unknown mode")}
  if err:=call(broken);!errors.Is(err,injected){t.Fatalf("expected injected commit error %v, got %v",injected,err)}
  want:=int64(500);switch mode{case "reserve","usage":want=400;case "settle":want=460;case "pay":want=600;case "refund_amount":want=300}
  balance,err:=reliable.balance(ctx,tenant.ID);if err!=nil{t.Fatal(err)};ledger,err:=store.LedgerBalance(ctx,db,tenant.ID);if err!=nil||balance!=want||ledger!=want{t.Fatalf("committed mode=%s summary=%d ledger=%d err=%v want=%d",mode,balance,ledger,err,want)}
  if err=call(reliable);err!=nil{t.Fatalf("retry %s: %v",mode,err)}
  balance,err=reliable.balance(ctx,tenant.ID);if err!=nil{t.Fatal(err)};ledger,err=store.LedgerBalance(ctx,db,tenant.ID);if err!=nil||balance!=want||ledger!=want{t.Fatalf("retry mode=%s summary=%d ledger=%d err=%v want=%d",mode,balance,ledger,err,want)}
  if mode=="reserve"||mode=="usage"||mode=="settle"||mode=="reverse" {phase:=mode;var n int64;if err=db.Model(&store.OutboxEvent{}).Where("event_key=?",id+":"+phase).Count(&n).Error;err!=nil||n!=1{t.Fatalf("phase=%s outbox=%d err=%v",phase,n,err)}}
  if mode=="pay"||mode=="refund_order"||mode=="refund_amount" {ref:=orderNo;typ:=store.TopupRecharge;if mode=="refund_order"{typ=store.TopupRefund};if mode=="refund_amount"{ref=id+"-refund";typ=store.TopupRefund};var n int64;if err=db.Model(&store.Topup{}).Where("ref_type=? AND ref_id=?",typ,ref).Count(&n).Error;err!=nil||n!=1{t.Fatalf("ref=%s topups=%d err=%v",ref,n,err)}}
  t.Logf("mode=%s commit succeeded then injected=%v; retry preserved summary=ledger=%d and one event or topup",mode,injected,want)
 })}
}

func TestSummaryAckReplayChild(t *testing.T){
 if os.Getenv("SUMMARY_ACK_CHILD")!="1"{t.Skip("isolated child only")}
 db,_:=isolatedDB(t);c:=candidate{db:db};ctx:=context.Background();tenant,err:=strconv.ParseUint(os.Getenv("SUMMARY_ACK_TENANT"),10,64);if err!=nil{t.Fatal(err)}
 switch os.Getenv("SUMMARY_ACK_MODE") {
 case "pay":v,e:=c.paySummary(ctx,os.Getenv("SUMMARY_ACK_KEY"));if e!=nil||v==nil||v.Status!=store.RechargePaid{t.Fatal(v,e)}
 case "refund_amount":v,e:=c.refundSummaryAmount(ctx,tenant,2,os.Getenv("SUMMARY_ACK_KEY"));if e!=nil||v==nil||v.RefundCent!=0{t.Fatal(v,e)}
 default:t.Fatal("unknown child mode")
 }
}

func TestIsolatedSummaryCommitAckLossMultiProcess(t *testing.T){
 if os.Getenv("TEST_SUMMARY_ACK")!="1"{t.Skip("isolated only")}
 db,r:=isolatedDB(t);db.Logger=db.Logger.LogMode(logger.Silent);s:=store.NewStore(db,r);c:=candidate{db:db};ctx:=context.Background();if err:=s.AutoMigrate();err!=nil{t.Fatal(err)};if err:=c.migrate();err!=nil{t.Fatal(err)}
 for _,mode:=range []string{"pay","refund_amount"}{t.Run(mode,func(t *testing.T){
  id:=fmt.Sprintf("summary-multiprocess-%s-%d",mode,time.Now().UnixNano());tenant:=store.Tenant{Name:id,InitialQuota:500};if err:=db.Create(&tenant).Error;err!=nil{t.Fatal(err)};if err:=db.Create(&candidateAccount{ID:tenant.ID,Initial:500,Balance:500}).Error;err!=nil{t.Fatal(err)}
  key:=id+"-key";if mode=="pay"{if _,err:=s.CreateRechargeOrder(ctx,tenant.ID,1,key);err!=nil{t.Fatal(err)}}
  broken:=candidate{db:recoveryDBWithLostCommitAck(t)};var err error;if mode=="pay"{_,err=broken.paySummary(ctx,key)}else{_,err=broken.refundSummaryAmount(ctx,tenant.ID,2,key)};if !errors.Is(err,errCommitAckLost){t.Fatalf("expected injected ack error: %v",err)}
  binary,err:=os.Executable();if err!=nil{t.Fatal(err)};childCtx,cancel:=context.WithTimeout(ctx,30*time.Second);defer cancel();var wg sync.WaitGroup;outputs:=make(chan error,4)
  for i:=0;i<4;i++{wg.Add(1);go func(){defer wg.Done();cmd:=exec.CommandContext(childCtx,binary,"-test.run=^TestSummaryAckReplayChild$");cmd.Env=append(os.Environ(),"SUMMARY_ACK_CHILD=1","SUMMARY_ACK_MODE="+mode,"SUMMARY_ACK_TENANT="+strconv.FormatUint(tenant.ID,10),"SUMMARY_ACK_KEY="+key);out,e:=cmd.CombinedOutput();if e!=nil{outputs<-fmt.Errorf("child error %v: %s",e,out)}}()};wg.Wait();close(outputs);for e:=range outputs{t.Error(e)}
  want:=int64(600);typ:=store.TopupRecharge;if mode=="refund_amount"{want=300;typ=store.TopupRefund};balance,err:=c.balance(ctx,tenant.ID);if err!=nil{t.Fatal(err)};ledger,err:=store.LedgerBalance(ctx,db,tenant.ID);if err!=nil||balance!=want||ledger!=want{t.Fatalf("balance=%d ledger=%d err=%v want=%d",balance,ledger,err,want)}
  var n int64;if err=db.Model(&store.Topup{}).Where("ref_type=? AND ref_id=?",typ,key).Count(&n).Error;err!=nil||n!=1{t.Fatalf("topup count=%d err=%v",n,err)}
  t.Logf("mode=%s committed before injected error; four independent processes retried; balance=%d topups=%d",mode,balance,n)
 })}
}
