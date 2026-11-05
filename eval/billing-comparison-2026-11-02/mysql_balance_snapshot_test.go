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

type snapshotTestKey struct{}
func TestIsolatedBalanceSnapshotDuringSettle(t *testing.T) {
 if os.Getenv("TEST_BALANCE_SNAPSHOT")!="1" {t.Skip("isolated only")}
 db,r:=isolatedDB(t);db.Logger=db.Logger.LogMode(logger.Silent);s:=store.NewStore(db,r);ctx:=context.Background();if err:=s.AutoMigrate();err!=nil{t.Fatal(err)}
 id:=fmt.Sprintf("snapshot-balance-%d",time.Now().UnixNano());tenant:=store.Tenant{Name:id,InitialQuota:500};model:=store.Model{Vendor:"test",ModelID:id,Name:id,InputPriceCent:1000000,OutputPriceCent:1000000,Tags:"[]"}
 if err:=db.Create(&tenant).Error;err!=nil{t.Fatal(err)};if err:=db.Create(&model).Error;err!=nil{t.Fatal(err)};if err:=r.Set(ctx,store.BalanceKey(tenant.ID),500,0).Err();err!=nil{t.Fatal(err)}
 out,err:=s.Reserve(ctx,&pb.ReserveRequest{RequestId:id,TenantId:int64(tenant.ID),ApiKeyId:1,Model:id,EstPromptTokens:84});if err!=nil||out.Code!=pb.ReserveCode_RESERVE_OK{t.Fatal(out,err)}
 if err=s.ReportUsage(ctx,&pb.ReportUsageRequest{RequestId:id,PromptTokens:40});err!=nil{t.Fatal(err)}
 entered:=make(chan struct{});release:=make(chan struct{});var fired atomic.Bool
 if err=db.Callback().Row().After("gorm:row").Register("test:pause_after_pending_sum",func(tx *gorm.DB){if tx.Statement.Table=="bills"&&tx.Statement.Context.Value(snapshotTestKey{})!=nil&&fired.CompareAndSwap(false,true){close(entered);<-release}});err!=nil{t.Fatal(err)}
 defer db.Callback().Row().Remove("test:pause_after_pending_sum")
 type result struct{v int64;err error};done:=make(chan result,1);go func(){v,e:=s.GetBalance(context.WithValue(ctx,snapshotTestKey{},true),tenant.ID);done<-result{v,e}}()
 select{case <-entered:case <-time.After(5*time.Second):close(release);t.Fatal("balance query did not reach pending sum")}
 settled,err:=s.Settle(ctx,&pb.SettleRequest{RequestId:id});close(release);read:=<-done
 if err!=nil||!settled.Settled{t.Fatal(settled,err)}
 if read.err!=nil||read.v!=400{t.Fatalf("concurrent balance=%d err=%v; want consistent pre-settle 400",read.v,read.err)}
 now,err:=s.GetBalance(ctx,tenant.ID);if err!=nil||now!=460{t.Fatalf("after settlement=%d err=%v",now,err)}
 t.Logf("concurrent snapshot=%d pre-settle; later balance=%d",read.v,now)
}
