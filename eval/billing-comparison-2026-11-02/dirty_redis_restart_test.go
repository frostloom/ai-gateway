package comparison

import (
 "context"
 "fmt"
 "os"
 "os/exec"
 "strings"
 "testing"
 "time"
 "github.com/frostloom/ai-gateway/internal/billing/store"
 pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
 "gorm.io/gorm/logger"
)

func TestIsolatedDirtyProjectionRedisRollback(t *testing.T) {
 if os.Getenv("TEST_DIRTY_REDIS_RESTART")!="1" {t.Skip("isolated restart only")}
 container:=os.Getenv("TEST_OWNED_REDIS_CONTAINER");if !strings.HasPrefix(container,"projection-restart-redis-")||strings.ContainsAny(container," /\\") {t.Fatal("owned Redis container required")}
 db,r:=isolatedDB(t);db.Logger=db.Logger.LogMode(logger.Silent);s:=store.NewStore(db,r);ctx:=context.Background();if err:=s.AutoMigrate();err!=nil {t.Fatal(err)}
 id:=fmt.Sprintf("redis-rollback-%d",time.Now().UnixNano());tenant:=store.Tenant{Name:id,InitialQuota:500};model:=store.Model{Vendor:"test",ModelID:id,Name:id,InputPriceCent:1000000,OutputPriceCent:1000000,Tags:"[]"}
 if err:=db.Create(&tenant).Error;err!=nil{t.Fatal(err)};if err:=db.Create(&model).Error;err!=nil{t.Fatal(err)};if err:=r.Set(ctx,store.BalanceKey(tenant.ID),500,0).Err();err!=nil{t.Fatal(err)}
 if err:=r.ConfigSet(ctx,"save","").Err();err!=nil{t.Fatal(err)}
 conf,err:=r.ConfigGet(ctx,"appendonly").Result();if err!=nil||conf["appendonly"]!="no"{t.Fatalf("appendonly config=%v err=%v",conf,err)}
 res,err:=s.Reserve(ctx,&pb.ReserveRequest{RequestId:id,TenantId:int64(tenant.ID),ApiKeyId:1,Model:id,EstPromptTokens:84});if err!=nil||res.Code!=pb.ReserveCode_RESERVE_OK{t.Fatal(res,err)}
 if err=s.ReportUsage(ctx,&pb.ReportUsageRequest{RequestId:id,PromptTokens:40});err!=nil{t.Fatal(err)}
 before,err:=s.GetBalance(ctx,tenant.ID);if err!=nil||before!=400{t.Fatalf("before snapshot %d %v",before,err)}
 if err=r.Save(ctx).Err();err!=nil{t.Fatal(err)}
 settled,err:=s.Settle(ctx,&pb.SettleRequest{RequestId:id});if err!=nil||!settled.Settled{t.Fatal(settled,err)}
 live,err:=s.GetBalance(ctx,tenant.ID);if err!=nil||live!=460{t.Fatalf("before restart %d %v",live,err)}
 stop:=exec.Command("docker","kill","--signal","KILL",container);if out,err:=stop.CombinedOutput();err!=nil{t.Fatalf("kill owned Redis: %v: %s",err,out)}
 start:=exec.Command("docker","start",container);if out,err:=start.CombinedOutput();err!=nil{t.Fatalf("restart owned Redis: %v: %s",err,out)}
 deadline:=time.Now().Add(30*time.Second);for {pingCtx,cancel:=context.WithTimeout(ctx,time.Second);e:=r.Ping(pingCtx).Err();cancel();if e==nil{break};if time.Now().After(deadline){t.Fatalf("Redis restart timeout: %v",e)};time.Sleep(200*time.Millisecond)}
 raw,err:=r.Get(ctx,store.BalanceKey(tenant.ID)).Int64();if err!=nil{t.Fatal(err)}
 dirty,err:=r.Exists(ctx,fmt.Sprintf("billing:cash-dirty:%d",tenant.ID)).Result();if err!=nil{t.Fatal(err)}
 live,err=s.GetBalance(ctx,tenant.ID);if err!=nil{t.Fatal(err)};ledger,err:=store.LedgerBalance(ctx,db,tenant.ID);if err!=nil{t.Fatal(err)}
 expectedQuery:=int64(400);if os.Getenv("TEST_EXPECT_MYSQL_BALANCE")=="1"{expectedQuery=460}
 if raw!=400||dirty!=0||live!=expectedQuery||ledger!=460 {t.Fatalf("unexpected rollback raw=%d dirty=%d query=%d ledger=%d expected_query=%d",raw,dirty,live,ledger,expectedQuery)}
 corrected,err:=s.RebuildBalance(ctx,tenant.ID);if err!=nil||corrected!=460{t.Fatalf("rebuild=%d err=%v",corrected,err)}
 t.Logf("actual Redis SIGKILL/restart from pre-settlement RDB: raw=%d dirty=%d query=%d ledger=%d; explicit rebuild=%d",raw,dirty,live,ledger,corrected)
}

func TestIsolatedMySQLBalanceWhenRedisDown(t *testing.T) {
 if os.Getenv("TEST_DIRTY_REDIS_RESTART")!="1" {t.Skip("isolated outage only")}
 container:=os.Getenv("TEST_OWNED_REDIS_CONTAINER");if !strings.HasPrefix(container,"projection-restart-redis-")||strings.ContainsAny(container," /\\"){t.Fatal("owned Redis container required")}
 db,r:=isolatedDB(t);db.Logger=db.Logger.LogMode(logger.Silent);s:=store.NewStore(db,r);ctx:=context.Background();if err:=s.AutoMigrate();err!=nil{t.Fatal(err)}
 id:=fmt.Sprintf("redis-down-%d",time.Now().UnixNano());tenant:=store.Tenant{Name:id,InitialQuota:500};model:=store.Model{Vendor:"test",ModelID:id,Name:id,InputPriceCent:1000000,OutputPriceCent:1000000,Tags:"[]"}
 if err:=db.Create(&tenant).Error;err!=nil{t.Fatal(err)};if err:=db.Create(&model).Error;err!=nil{t.Fatal(err)};if err:=r.Set(ctx,store.BalanceKey(tenant.ID),500,0).Err();err!=nil{t.Fatal(err)}
 result,err:=s.Reserve(ctx,&pb.ReserveRequest{RequestId:id,TenantId:int64(tenant.ID),ApiKeyId:1,Model:id,EstPromptTokens:84});if err!=nil||result.Code!=pb.ReserveCode_RESERVE_OK{t.Fatal(result,err)}
 if out,err:=exec.Command("docker","kill","--signal","KILL",container).CombinedOutput();err!=nil{t.Fatalf("kill owned Redis: %v %s",err,out)}
 readctx,cancel:=context.WithTimeout(ctx,3*time.Second);defer cancel();balance,err:=s.GetBalance(readctx,tenant.ID)
 if err!=nil||balance!=400{t.Fatalf("balance with Redis down=%d err=%v, want 400",balance,err)}
 t.Logf("Redis container killed; MySQL snapshot balance=%d",balance)
}
