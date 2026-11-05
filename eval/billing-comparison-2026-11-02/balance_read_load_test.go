package comparison

import (
 "context"
 "fmt"
 "os"
 "sort"
 "sync"
 "testing"
 "time"
 "github.com/frostloom/ai-gateway/internal/billing/store"
 "gorm.io/gorm/logger"
)

func TestIsolatedBalanceReadLoad(t *testing.T) {
 if os.Getenv("TEST_BALANCE_READ_LOAD")!="1" {t.Skip("isolated load only")}
 db,r:=isolatedDB(t);db.Logger=db.Logger.LogMode(logger.Silent);s:=store.NewStore(db,r);ctx:=context.Background();if err:=s.AutoMigrate();err!=nil{t.Fatal(err)}
 ids:=make([]uint64,64);for i:=range ids {tenant:=store.Tenant{Name:fmt.Sprintf("balance-load-%d-%d",time.Now().UnixNano(),i),InitialQuota:500};if err:=db.Create(&tenant).Error;err!=nil{t.Fatal(err)};ids[i]=tenant.ID;if err:=r.Set(ctx,store.BalanceKey(tenant.ID),500,0).Err();err!=nil{t.Fatal(err)}}
 const workers,perWorker=32,40
 for rep:=1;rep<=2;rep++ {
  durations:=make(chan time.Duration,workers*perWorker);errs:=make(chan error,workers*perWorker);var wg sync.WaitGroup;started:=time.Now()
  for w:=0;w<workers;w++ {wg.Add(1);go func(w int){defer wg.Done();for i:=0;i<perWorker;i++{start:=time.Now();v,err:=s.GetBalance(ctx,ids[(w+i*workers)%len(ids)]);durations<-time.Since(start);if err!=nil {errs<-err}else if v!=500{errs<-fmt.Errorf("unexpected balance %d",v)}}}(w)}
  wg.Wait();elapsed:=time.Since(started);close(durations);close(errs);var samples []time.Duration;for d:=range durations{samples=append(samples,d)};errorCount:=0;for err:=range errs{errorCount++;t.Error(err)}
  sort.Slice(samples,func(i,j int)bool{return samples[i]<samples[j]});var total time.Duration;for _,d:=range samples{total+=d}
  fmt.Printf("BALANCE_READ rep=%d count=%d workers=%d tenants=%d seconds=%.3f ops_sec=%.1f mean_ms=%.3f p95_ms=%.3f p99_ms=%.3f errors=%d\n",rep,len(samples),workers,len(ids),elapsed.Seconds(),float64(len(samples))/elapsed.Seconds(),float64(total.Microseconds())/float64(len(samples))/1000,float64(samples[len(samples)*95/100].Microseconds())/1000,float64(samples[len(samples)*99/100].Microseconds())/1000,errorCount)
 }
}

func TestIsolatedDisabledTenantBalanceRead(t *testing.T) {
 if os.Getenv("TEST_BALANCE_READ_LOAD")!="1" {t.Skip("isolated only")}
 db,r:=isolatedDB(t);db.Logger=db.Logger.LogMode(logger.Silent);s:=store.NewStore(db,r);ctx:=context.Background();if err:=s.AutoMigrate();err!=nil{t.Fatal(err)}
 tenant:=store.Tenant{Name:fmt.Sprintf("disabled-balance-%d",time.Now().UnixNano()),InitialQuota:500,Status:1}
 if err:=db.Create(&tenant).Error;err!=nil{t.Fatal(err)};if err:=r.Set(ctx,store.BalanceKey(tenant.ID),500,0).Err();err!=nil{t.Fatal(err)}
 got,err:=s.GetBalance(ctx,tenant.ID);if err!=nil||got!=500{t.Fatalf("disabled tenant balance=%d err=%v, want 500",got,err)}
}

func TestIsolatedMissingTenantBalanceRead(t *testing.T) {
 if os.Getenv("TEST_BALANCE_READ_LOAD")!="1" {t.Skip("isolated only")}
 db,r:=isolatedDB(t);s:=store.NewStore(db,r);if err:=s.AutoMigrate();err!=nil{t.Fatal(err)}
 got,err:=s.GetBalance(context.Background(),999999999);if err==nil{t.Fatalf("nonexistent tenant returned balance=%d without error",got)}
}
