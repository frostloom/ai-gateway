package comparison

import (
 "context"
 "fmt"
 "os"
 "os/exec"
 "strings"
 "syscall"
 "testing"
 "time"

 "github.com/frostloom/ai-gateway/internal/billing/store"
 pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
 "gorm.io/gorm"
 "gorm.io/gorm/logger"
)

func TestTerminalProcessKilledAfterRedisDelta(t *testing.T) {
 if os.Getenv("TEST_TERMINAL_PROCESS_CRASH")!="1" {t.Skip("isolated crash injection only")}
 db,r:=isolatedDB(t);db.Logger=db.Logger.LogMode(logger.Silent);s:=store.NewStore(db,r);ctx:=context.Background()
 if id:=os.Getenv("CRASH_CHILD_REQUEST");id!="" {
  if err:=db.Callback().Create().Before("gorm:create").Register("test:kill_self_after_delta",func(tx *gorm.DB){
   if tx.Statement.Table=="outbox_events" {fmt.Fprintln(os.Stderr,"CRASH_AT_OUTBOX_BEFORE_COMMIT");_=syscall.Kill(os.Getpid(),syscall.SIGKILL)}
  });err!=nil{t.Fatal(err)}
  if os.Getenv("CRASH_CHILD_MODE")=="settle" {_,err:=s.Settle(ctx,&pb.SettleRequest{RequestId:id});t.Fatalf("child unexpectedly survived: %v",err)}
  _,err:=s.Reverse(ctx,&pb.ReverseRequest{RequestId:id});t.Fatalf("child unexpectedly survived: %v",err)
 }
 if err:=s.AutoMigrate();err!=nil{t.Fatal(err)}
 for _,mode:=range []string{"settle","reverse"}{t.Run(mode,func(t *testing.T){
  id:=fmt.Sprintf("process-crash-%s-%d",mode,time.Now().UnixNano());tenant:=store.Tenant{Name:id,InitialQuota:500};model:=store.Model{Vendor:"test",ModelID:id,Name:id,InputPriceCent:1000000,OutputPriceCent:1000000,Tags:"[]"}
  if err:=db.Create(&tenant).Error;err!=nil{t.Fatal(err)};if err:=db.Create(&model).Error;err!=nil{t.Fatal(err)}
  if err:=r.Set(ctx,store.BalanceKey(tenant.ID),500,0).Err();err!=nil{t.Fatal(err)}
  req:=&pb.ReserveRequest{RequestId:id,TenantId:int64(tenant.ID),ApiKeyId:1,Model:id,EstPromptTokens:84}
  if out,err:=s.Reserve(ctx,req);err!=nil||out.Code!=pb.ReserveCode_RESERVE_OK{t.Fatal(out,err)}
  if mode=="settle"{if err:=s.ReportUsage(ctx,&pb.ReportUsageRequest{RequestId:id,PromptTokens:40});err!=nil{t.Fatal(err)}}
  bin,err:=os.Executable();if err!=nil{t.Fatal(err)}
  cmd:=exec.Command(bin,"-test.run=^TestTerminalProcessKilledAfterRedisDelta$")
  cmd.Env=append(os.Environ(),"CRASH_CHILD_REQUEST="+id,"CRASH_CHILD_MODE="+mode)
  output,err:=cmd.CombinedOutput();exit,ok:=err.(*exec.ExitError);if !ok{t.Fatalf("child did not fail: err=%v output=%s",err,output)}
  status,ok:=exit.Sys().(syscall.WaitStatus);if !ok||!status.Signaled()||status.Signal()!=syscall.SIGKILL||!strings.Contains(string(output),"CRASH_AT_OUTBOX_BEFORE_COMMIT"){t.Fatalf("wrong child exit %v: %s",err,output)}
  if os.Getenv("TEST_DROP_DIRTY_MARKER")=="1" {if err:=r.Del(ctx,fmt.Sprintf("billing:cash-dirty:%d",tenant.ID)).Err();err!=nil{t.Fatal(err)}}
  live,err:=s.GetBalance(ctx,tenant.ID);if err!=nil{t.Fatal(err)};ledger,err:=store.LedgerBalance(ctx,db,tenant.ID);if err!=nil{t.Fatal(err)}
  expected:=int64(500);if mode=="settle"{expected=460};if os.Getenv("TEST_EXPECT_CONSISTENT_READ")=="1"{expected=400};if live!=expected||ledger!=400{t.Fatalf("crash balance=%d ledger=%d expected=%d/400 child=%s",live,ledger,expected,output)}
  var row store.Bill;if err=db.Where("request_id=? AND phase=?",id,store.PhaseReserve).First(&row).Error;err!=nil||row.Status!=store.StatusPending{t.Fatalf("reserve state=%s err=%v",row.Status,err)}
  guard,err:=r.Exists(ctx,store.DeltaGuardKey(id)).Result();if err!=nil||guard!=1{t.Fatalf("guard=%d err=%v",guard,err)}
  newReq:=&pb.ReserveRequest{RequestId:id+"-other",TenantId:int64(tenant.ID),ApiKeyId:1,Model:id,EstPromptTokens:84}
  if out,err:=s.Reserve(ctx,newReq);err!=nil||out.Code!=pb.ReserveCode_RESERVE_OK{t.Fatal(out,err)}
  fresh,err:=s.GetBalance(ctx,tenant.ID);if err!=nil{t.Fatal(err)};if fresh!=300{t.Fatalf("other reserve after crash balance=%d, want 300",fresh)}
  if mode=="settle"{out,err:=s.Settle(ctx,&pb.SettleRequest{RequestId:id});if err!=nil||!out.Settled{t.Fatal(out,err)}}else{out,err:=s.Reverse(ctx,&pb.ReverseRequest{RequestId:id});if err!=nil||!out.Reversed{t.Fatal(out,err)}}
  live,err=s.GetBalance(ctx,tenant.ID);if err!=nil{t.Fatal(err)};ledger,err=store.LedgerBalance(ctx,db,tenant.ID);if err!=nil{t.Fatal(err)}
  want:=int64(400);if mode=="settle"{want=360};if live!=want||ledger!=want{t.Fatalf("after retry balance=%d ledger=%d want=%d",live,ledger,want)}
  var events int64;if err=db.Model(&store.OutboxEvent{}).Where("event_key=?",id+":"+mode).Count(&events).Error;err!=nil||events!=1{t.Fatalf("terminal events=%d err=%v",events,err)}
  t.Logf("mode=%s SIGKILL observed initial_query=%d ledger=400 other_reserve=300 retry_balance=%d terminal_events=%d",mode,expected,live,events)
 })}
}
