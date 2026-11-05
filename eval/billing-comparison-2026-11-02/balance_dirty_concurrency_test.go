package comparison

import (
 "context"
 "fmt"
 "os"
 "sync/atomic"
 "testing"
 "time"
 "github.com/frostloom/ai-gateway/internal/billing/store"
 pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
 "gorm.io/gorm"
 "gorm.io/gorm/logger"
)

func TestIsolatedBalanceReadDuringTerminalWrite(t *testing.T){
 if os.Getenv("TEST_DIRTY_CONCURRENCY")!="1" {t.Skip("isolated only")}
 db,r:=isolatedDB(t);db.Logger=db.Logger.LogMode(logger.Silent);s:=store.NewStore(db,r);ctx:=context.Background();if err:=s.AutoMigrate();err!=nil{t.Fatal(err)}
 id:=fmt.Sprintf("dirty-read-%d",time.Now().UnixNano());tenant:=store.Tenant{Name:id,InitialQuota:500};model:=store.Model{Vendor:"test",ModelID:id,Name:id,InputPriceCent:1000000,OutputPriceCent:1000000,Tags:"[]"}
 if err:=db.Create(&tenant).Error;err!=nil{t.Fatal(err)};if err:=db.Create(&model).Error;err!=nil{t.Fatal(err)};if err:=r.Set(ctx,store.BalanceKey(tenant.ID),500,0).Err();err!=nil{t.Fatal(err)}
 out,err:=s.Reserve(ctx,&pb.ReserveRequest{RequestId:id,TenantId:int64(tenant.ID),ApiKeyId:1,Model:id,EstPromptTokens:84});if err!=nil||out.Code!=pb.ReserveCode_RESERVE_OK{t.Fatal(out,err)}
 if err=s.ReportUsage(ctx,&pb.ReportUsageRequest{RequestId:id,PromptTokens:40});err!=nil{t.Fatal(err)}
 entered:=make(chan struct{});release:=make(chan struct{});var fired atomic.Bool
 if err=db.Callback().Create().Before("gorm:create").Register("test:block_terminal_outbox",func(tx *gorm.DB){if tx.Statement.Table=="outbox_events"&&fired.CompareAndSwap(false,true){close(entered);<-release}});err!=nil{t.Fatal(err)}
 defer db.Callback().Create().Remove("test:block_terminal_outbox")
 done:=make(chan error,1);go func(){_,err:=s.Settle(ctx,&pb.SettleRequest{RequestId:id});done<-err}()
 select{case <-entered:case <-time.After(5*time.Second):close(release);t.Fatal("did not reach post-Redis cutpoint")}
 readctx,cancel:=context.WithTimeout(ctx,200*time.Millisecond);defer cancel();value,readErr:=s.GetBalance(readctx,tenant.ID)
 close(release);if err=<-done;err!=nil{t.Fatal(err)}
 if readErr==nil {t.Fatalf("read returned uncommitted projection %d while terminal transaction paused",value)}
 value,err=s.GetBalance(ctx,tenant.ID);if err!=nil||value!=460{t.Fatalf("after commit value=%d err=%v",value,err)}
 t.Logf("during write read returned error=%v; after commit balance=%d",readErr,value)
}
