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

func TestIsolatedBalanceHistoryLoad(t *testing.T){
 if os.Getenv("TEST_BALANCE_HISTORY")!="1"{t.Skip("isolated only")}
 db,r:=isolatedDB(t);db.Logger=db.Logger.LogMode(logger.Silent);s:=store.NewStore(db,r);ctx:=context.Background();if err:=s.AutoMigrate();err!=nil{t.Fatal(err)}
 if os.Getenv("TEST_SUMMARY_COMPARE")=="1" {if err:=db.AutoMigrate(&candidateAccount{});err!=nil{t.Fatal(err)}}
 type scenario struct{n int; tenant store.Tenant};cases:=[]scenario{{n:0},{n:5000},{n:20000}}
 for i:=range cases {
  c:=&cases[i];c.tenant=store.Tenant{Name:fmt.Sprintf("history-%d-%d",time.Now().UnixNano(),c.n),InitialQuota:100000};if err:=db.Create(&c.tenant).Error;err!=nil{t.Fatal(err)}
  for start:=0;start<c.n;start+=500{end:=start+500;if end>c.n{end=c.n};batch:=make([]store.Bill,0,end-start);for j:=start;j<end;j++{actual:=int64(1);batch=append(batch,store.Bill{TenantID:c.tenant.ID,RequestID:fmt.Sprintf("h%d-%d",c.tenant.ID,j),Phase:store.PhaseSettle,Status:store.StatusSettled,Funding:store.FundingBalance,Model:"synthetic",ActualQuota:&actual})};if err:=db.CreateInBatches(batch,500).Error;err!=nil{t.Fatal(err)}}
  expected:=int64(100000-c.n);if err:=r.Set(ctx,store.BalanceKey(c.tenant.ID),expected,0).Err();err!=nil{t.Fatal(err)}
  if os.Getenv("TEST_SUMMARY_COMPARE")=="1" {if err:=db.Create(&candidateAccount{ID:c.tenant.ID,Initial:100000,Balance:expected}).Error;err!=nil{t.Fatal(err)}}
  got,err:=s.GetBalance(ctx,c.tenant.ID);if err!=nil||got!=expected{t.Fatalf("seed count=%d got=%d want=%d err=%v",c.n,got,expected,err)}
 }
 for _,stage:=range []string{"original_index","covering_index"} {
  if stage=="covering_index" {if err:=db.Exec("CREATE INDEX idx_eval_balance_cover ON bills (tenant_id, phase, status, actual_quota)").Error;err!=nil{t.Fatal(err)};if os.Getenv("TEST_ANALYZE_INDEX")=="1" {if err:=db.Exec("ANALYZE TABLE bills").Error;err!=nil{t.Fatal(err)}}}
  for _,c:=range cases {
   var plan string;if err:=db.Raw("EXPLAIN FORMAT=JSON SELECT COALESCE(SUM(actual_quota),0) FROM bills WHERE tenant_id=? AND phase=? AND status=?",c.tenant.ID,store.PhaseSettle,store.StatusSettled).Scan(&plan).Error;err!=nil{t.Fatal(err)};fmt.Printf("HISTORY_PLAN stage=%s rows=%d json=%s\n",stage,c.n,plan)
   if stage=="covering_index" && os.Getenv("TEST_FORCE_INDEX")=="1" {
    query:="SELECT COALESCE(SUM(actual_quota),0) FROM bills FORCE INDEX (idx_eval_balance_cover) WHERE tenant_id=? AND phase=? AND status=?"
    var forced string;if err:=db.Raw("EXPLAIN FORMAT=JSON "+query,c.tenant.ID,store.PhaseSettle,store.StatusSettled).Scan(&forced).Error;err!=nil{t.Fatal(err)};fmt.Printf("HISTORY_FORCED_PLAN rows=%d json=%s\n",c.n,forced)
    for _,forcedQuery:=range []bool{false,true} {q:="SELECT COALESCE(SUM(actual_quota),0) FROM bills WHERE tenant_id=? AND phase=? AND status=?";if forcedQuery{q=query};start:=time.Now();for i:=0;i<20;i++ {var total int64;if err:=db.Raw(q,c.tenant.ID,store.PhaseSettle,store.StatusSettled).Scan(&total).Error;err!=nil||total!=int64(c.n){t.Fatalf("sum=%d err=%v",total,err)}};fmt.Printf("HISTORY_SUM rows=%d forced=%t count=20 mean_ms=%.3f\n",c.n,forcedQuery,float64(time.Since(start).Microseconds())/20000)}
   }
   modes:=[]string{"snapshot"};if os.Getenv("TEST_SUMMARY_COMPARE")=="1"{modes=append(modes,"summary")}
   for _,mode:=range modes {for rep:=1;rep<=2;rep++ {
    const workers,perWorker=8,8;durations:=make(chan time.Duration,workers*perWorker);errs:=make(chan error,workers*perWorker);var wg sync.WaitGroup;started:=time.Now()
    for w:=0;w<workers;w++{wg.Add(1);go func(){defer wg.Done();for i:=0;i<perWorker;i++{begin:=time.Now();var value int64;var err error;if mode=="summary"{value,err=(candidate{db:db}).balance(ctx,c.tenant.ID)}else{value,err=s.GetBalance(ctx,c.tenant.ID)};durations<-time.Since(begin);if err!=nil{errs<-err}else if value!=int64(100000-c.n){errs<-fmt.Errorf("incorrect balance %d",value)}}}()}
    wg.Wait();elapsed:=time.Since(started);close(durations);close(errs);var samples []time.Duration;for d:=range durations{samples=append(samples,d)};errors:=0;for e:=range errs{errors++;t.Error(e)};sort.Slice(samples,func(i,j int)bool{return samples[i]<samples[j]});var sum time.Duration;for _,d:=range samples{sum+=d}
    fmt.Printf("HISTORY_READ mode=%s stage=%s rows=%d rep=%d count=%d workers=%d seconds=%.3f ops_sec=%.1f mean_ms=%.3f p95_ms=%.3f p99_ms=%.3f errors=%d\n",mode,stage,c.n,rep,len(samples),workers,elapsed.Seconds(),float64(len(samples))/elapsed.Seconds(),float64(sum.Microseconds())/float64(len(samples))/1000,float64(samples[len(samples)*95/100].Microseconds())/1000,float64(samples[len(samples)*99/100].Microseconds())/1000,errors)
   }}
  }
 }
}
