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
 "gorm.io/gorm"
 "gorm.io/gorm/logger"
)

func TestIsolatedSummaryRefundAmount(t *testing.T){
 if os.Getenv("TEST_SUMMARY_REFUND_AMOUNT")!="1"{t.Skip("isolated only")}
 db,r:=isolatedDB(t);db.Logger=db.Logger.LogMode(logger.Silent);s:=store.NewStore(db,r);c:=candidate{db:db};ctx:=context.Background()
 if err:=s.AutoMigrate();err!=nil{t.Fatal(err)};if err:=c.migrate();err!=nil{t.Fatal(err)}
 id:=fmt.Sprintf("summary-amount-%d",time.Now().UnixNano());tenant:=store.Tenant{Name:id,InitialQuota:500}
 if err:=db.Create(&tenant).Error;err!=nil{t.Fatal(err)};if err:=db.Create(&candidateAccount{ID:tenant.ID,Initial:500,Balance:500}).Error;err!=nil{t.Fatal(err)};if err:=r.Set(ctx,store.BalanceKey(tenant.ID),500,0).Err();err!=nil{t.Fatal(err)}
 bad:=c;bad.fail=true;if _,err:=bad.refundSummaryAmount(ctx,tenant.ID,2,id+"-refund");err==nil{t.Fatal("expected rollback")}
 var wg sync.WaitGroup;results:=make(chan int64,16);errs:=make(chan error,16);for i:=0;i<16;i++{wg.Add(1);go func(){defer wg.Done();v,e:=c.refundSummaryAmount(ctx,tenant.ID,2,id+"-refund");if e!=nil{errs<-e;return};results<-v.RefundCent}()};wg.Wait();close(results);close(errs);for e:=range errs{t.Error(e)};var refunded int64;for v:=range results{refunded+=v};if refunded!=200{t.Fatalf("total refunded=%d",refunded)}
 balance,err:=c.balance(ctx,tenant.ID);if err!=nil{t.Fatal(err)};ledger,err:=store.LedgerBalance(ctx,db,tenant.ID);if err!=nil||balance!=300||ledger!=300{t.Fatalf("refund summary=%d ledger=%d err=%v; expected 300/300",balance,ledger,err)}
 if _,err=c.refundSummaryAmount(ctx,tenant.ID,1,id+"-refund");err==nil{t.Fatal("different amount accepted for same key")}
 var entries int64;if err=db.Model(&store.Topup{}).Where("ref_type=? AND ref_id=?",store.TopupRefund,id+"-refund").Count(&entries).Error;err!=nil||entries!=1{t.Fatalf("refund entries=%d err=%v",entries,err)}
 t.Log("16 concurrent retries returned total refund 200; rollback, conflict, ledger and summary verified")
}

func TestIsolatedSummaryRefundAmountPartialAndConflict(t *testing.T){
 if os.Getenv("TEST_SUMMARY_REFUND_AMOUNT")!="1"{t.Skip("isolated only")}
 db,r:=isolatedDB(t);db.Logger=db.Logger.LogMode(logger.Silent);s:=store.NewStore(db,r);c:=candidate{db:db};ctx:=context.Background();if err:=s.AutoMigrate();err!=nil{t.Fatal(err)};if err:=c.migrate();err!=nil{t.Fatal(err)}
 id:=fmt.Sprintf("summary-partial-amount-%d",time.Now().UnixNano());a:=store.Tenant{Name:id+"-a",InitialQuota:150};b:=store.Tenant{Name:id+"-b",InitialQuota:500}
 for _,v:=range []*store.Tenant{&a,&b}{if err:=db.Create(v).Error;err!=nil{t.Fatal(err)};if err:=db.Create(&candidateAccount{ID:v.ID,Initial:v.InitialQuota,Balance:v.InitialQuota}).Error;err!=nil{t.Fatal(err)}}
 key:=id+"-partial";first,err:=c.refundSummaryAmount(ctx,a.ID,2,key);if err!=nil||first.RefundCent!=150||first.AmountCent!=200{t.Fatal(first,err)}
 again,err:=c.refundSummaryAmount(ctx,a.ID,2,key);if err!=nil||again.RefundCent!=0{t.Fatal(again,err)}
 if _,err=c.refundSummaryAmount(ctx,b.ID,2,key);err==nil{t.Fatal("cross-tenant idempotency conflict accepted")};if _,err=c.refundSummaryAmount(ctx,a.ID,1,key);err==nil{t.Fatal("changed amount accepted")};if _,err=c.refundSummaryAmount(ctx,b.ID,0,id+"-zero");err==nil{t.Fatal("zero refund accepted")}
 if err=c.audit(ctx);err!=nil{t.Fatal(err)};t.Log("partial refund 150/200, retry zero, cross-tenant and changed-amount conflicts rejected")
}

func TestIsolatedSummaryRefundAmountCrossTenantRace(t *testing.T){
 if os.Getenv("TEST_SUMMARY_REFUND_AMOUNT")!="1"{t.Skip("isolated only")}
 db,r:=isolatedDB(t);db.Logger=db.Logger.LogMode(logger.Silent);s:=store.NewStore(db,r);c:=candidate{db:db};ctx:=context.Background();if err:=s.AutoMigrate();err!=nil{t.Fatal(err)};if err:=c.migrate();err!=nil{t.Fatal(err)}
 id:=fmt.Sprintf("summary-keyrace-%d",time.Now().UnixNano());a:=store.Tenant{Name:id+"-a",InitialQuota:500};b:=store.Tenant{Name:id+"-b",InitialQuota:500};for _,v:=range []*store.Tenant{&a,&b}{if err:=db.Create(v).Error;err!=nil{t.Fatal(err)};if err:=db.Create(&candidateAccount{ID:v.ID,Initial:500,Balance:500}).Error;err!=nil{t.Fatal(err)}}
 entered:=make(chan struct{},2);release:=make(chan struct{});key:=id+"-same-key"
 if err:=db.Callback().Create().Before("gorm:create").Register("test:align_refund_insert",func(tx *gorm.DB){if e,ok:=tx.Statement.Dest.(*store.Topup);ok&&e.RefID==key{entered<-struct{}{};<-release}});err!=nil{t.Fatal(err)};defer db.Callback().Create().Remove("test:align_refund_insert")
 start:=make(chan struct{});var wg sync.WaitGroup;var result [2]*store.RefundResult;var errs [2]error;for i,tenant:=range []uint64{a.ID,b.ID}{wg.Add(1);go func(i int,tenant uint64){defer wg.Done();<-start;result[i],errs[i]=c.refundSummaryAmount(ctx,tenant,2,key)}(i,tenant)};close(start)
 aligned:=true;for i:=0;i<2;i++{select{case <-entered:case <-time.After(5*time.Second):aligned=false}};close(release);wg.Wait();if !aligned{t.Fatal("both transactions did not reach insert barrier")}
 success:=0;for i:=range errs{if errs[i]==nil&&result[i]!=nil&&result[i].RefundCent==200{success++}};if success!=1{t.Fatalf("success=%d results=%v errors=%v",success,result,errs)}
 av,err:=c.balance(ctx,a.ID);if err!=nil{t.Fatal(err)};bv,err:=c.balance(ctx,b.ID);if err!=nil||av+bv!=800{t.Fatalf("balances=%d,%d err=%v",av,bv,err)};if err=c.audit(ctx);err!=nil{t.Fatal(err)}
 t.Logf("cross-tenant same key: one refund; balances=%d,%d; errors=%v",av,bv,errs)
}

func TestIsolatedSummaryRefundAmountConnectionKill(t *testing.T){
 if os.Getenv("TEST_SUMMARY_REFUND_AMOUNT")!="1"{t.Skip("isolated only")}
 db,r:=isolatedDB(t);db.Logger=db.Logger.LogMode(logger.Silent);s:=store.NewStore(db,r);c:=candidate{db:db};ctx:=context.Background();if err:=s.AutoMigrate();err!=nil{t.Fatal(err)};if err:=c.migrate();err!=nil{t.Fatal(err)}
 id:=fmt.Sprintf("summary-amount-kill-%d",time.Now().UnixNano());tenant:=store.Tenant{Name:id,InitialQuota:500};if err:=db.Create(&tenant).Error;err!=nil{t.Fatal(err)};if err:=db.Create(&candidateAccount{ID:tenant.ID,Initial:500,Balance:500}).Error;err!=nil{t.Fatal(err)}
 var fired atomic.Bool;var killErr error
 if err:=db.Callback().Update().Before("gorm:update").Register("test:kill_amount_refund_connection",func(tx *gorm.DB){if tx.Statement.Table!="candidate_accounts"||!fired.CompareAndSwap(false,true){return};var connID uint64;if killErr=tx.Session(&gorm.Session{NewDB:true}).Raw("SELECT CONNECTION_ID()").Scan(&connID).Error;killErr==nil{killErr=db.Exec(fmt.Sprintf("KILL CONNECTION %d",connID)).Error}});err!=nil{t.Fatal(err)}
 _,err:=c.refundSummaryAmount(ctx,tenant.ID,2,id+"-refund");db.Callback().Update().Remove("test:kill_amount_refund_connection");if !fired.Load()||killErr!=nil||err==nil{t.Fatalf("fired=%v kill=%v refund=%v",fired.Load(),killErr,err)}
 bal,err:=c.balance(ctx,tenant.ID);if err!=nil{t.Fatal(err)};ledger,err:=store.LedgerBalance(ctx,db,tenant.ID);if err!=nil||bal!=500||ledger!=500{t.Fatalf("after kill balance=%d ledger=%d err=%v",bal,ledger,err)}
 var n int64;if err=db.Model(&store.Topup{}).Where("ref_id=?",id+"-refund").Count(&n).Error;err!=nil||n!=0{t.Fatalf("after kill topups=%d err=%v",n,err)}
 result,err:=c.refundSummaryAmount(ctx,tenant.ID,2,id+"-refund");if err!=nil||result.RefundCent!=200{t.Fatal(result,err)};if err=db.Model(&store.Topup{}).Where("ref_id=?",id+"-refund").Count(&n).Error;err!=nil||n!=1{t.Fatalf("after retry topups=%d err=%v",n,err)}
 bal,err=c.balance(ctx,tenant.ID);if err!=nil{t.Fatal(err)};ledger,err=store.LedgerBalance(ctx,db,tenant.ID);if err!=nil||bal!=300||ledger!=300{t.Fatalf("after retry balance=%d ledger=%d err=%v",bal,ledger,err)}
 t.Log("actual MySQL connection killed after topup insert before commit; transaction rolled back; retry refunded once")
}
