package comparison

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	redis "github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestReversingProcessChild(t *testing.T) {
	if os.Getenv("REVERSING_CHILD") != "1" {
		t.Skip("helper only")
	}
	db, e := gorm.Open(mysql.Open(os.Getenv("COMPARISON_MYSQL_DSN")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if e != nil {
		t.Fatal(e)
	}
	r := redis.NewClient(&redis.Options{Addr: os.Getenv("COMPARISON_REDIS_ADDR")})
	defer r.Close()
	s := store.NewStore(db, r)
	id := os.Getenv("REVERSING_REQUEST")
	ctx := context.Background()
	if ok, e := markReversing(ctx, db, id); e != nil || !ok {
		t.Fatal(ok, e)
	}
	if os.Getenv("REVERSING_STAGE") == "refund_committed" {
		if x, e := s.Reverse(ctx, &pb.ReverseRequest{RequestId: id, Reason: "test:known-not-forwarded"}); e != nil || x == nil || !x.Reversed {
			t.Fatal(x, e)
		}
	}
	fmt.Println("REVERSING_CHILD_READY")
	time.Sleep(time.Minute)
}
func TestReversingKilledWorker(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	g := sqlIdemGate{db: db}
	if e := s.AutoMigrate(); e != nil {
		t.Fatal(e)
	}
	if e := g.migrate(); e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	base := fmt.Sprintf("kill-reversing-%d", time.Now().UnixNano())
	m := store.Model{Vendor: "test", ModelID: base, Name: "mock", InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if e := db.Create(&m).Error; e != nil {
		t.Fatal(e)
	}
	tenant := store.Tenant{Name: base, InitialQuota: 1000}
	if e := db.Create(&tenant).Error; e != nil {
		t.Fatal(e)
	}
	if e := r.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); e != nil {
		t.Fatal(e)
	}
	for _, stage := range []string{"marked", "refund_committed"} {
		t.Run(stage, func(t *testing.T) {
			id := base + "-" + stage
			if state, e := g.claim(ctx, id, "same", "owner", time.Minute); e != nil || state != gateOwned {
				t.Fatal(state, e)
			}
			b, e := s.Reserve(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: base, EstPromptTokens: 84})
			if e != nil || b.Code != pb.ReserveCode_RESERVE_OK {
				t.Fatal(b, e)
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestReversingProcessChild$")
			cmd.Env = append(os.Environ(), "REVERSING_CHILD=1", "REVERSING_REQUEST="+id, "REVERSING_STAGE="+stage)
			stdout, e := cmd.StdoutPipe()
			if e != nil {
				t.Fatal(e)
			}
			cmd.Stderr = os.Stderr
			if e = cmd.Start(); e != nil {
				t.Fatal(e)
			}
			ready := make(chan bool, 1)
			go func() {
				scanner := bufio.NewScanner(stdout)
				for scanner.Scan() {
					if strings.TrimSpace(scanner.Text()) == "REVERSING_CHILD_READY" {
						ready <- true
						return
					}
				}
				ready <- false
			}()
			select {
			case ok := <-ready:
				if !ok {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
					t.Fatal("child exited before ready")
				}
			case <-time.After(30 * time.Second):
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				t.Fatal("child readiness timeout")
			}
			if e := cmd.Process.Kill(); e != nil {
				_ = cmd.Wait()
				t.Fatal(e)
			}
			_ = cmd.Wait()
			var intent sqlIdemIntent
			if e := db.Where("request_id=?", id).First(&intent).Error; e != nil || intent.State != "reversing" {
				t.Fatalf("intent=%v err=%v", intent, e)
			}
			if ok, e := coordinatedForward(ctx, db, id, "owner"); e != nil || ok {
				t.Fatalf("forward after death=%v %v", ok, e)
			}
			var wg sync.WaitGroup
			results := make(chan error, 8)
			for i := 0; i < 8; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					ok, e := finishReversing(ctx, db, s, id)
					if e != nil || !ok {
						results <- fmt.Errorf("ok=%v err=%v", ok, e)
					}
				}()
			}
			wg.Wait()
			close(results)
			for e := range results {
				t.Fatalf("parallel recovery: %v", e)
			}
			if ok, e := finishReversing(ctx, db, s, id); e != nil || !ok {
				t.Fatalf("repeat=%v %v", ok, e)
			}
			if e := db.Where("request_id=?", id).First(&intent).Error; e != nil || intent.State != "reversed" {
				t.Fatalf("final intent=%v err=%v", intent, e)
			}
			var row store.Bill
			if e := db.Where("request_id=? AND phase=?", id, store.PhaseReserve).First(&row).Error; e != nil {
				t.Fatal(e)
			}
			var events int64
			if e := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{id + ":reserve", id + ":reverse"}).Count(&events).Error; e != nil {
				t.Fatal(e)
			}
			live, e := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
			if e != nil {
				t.Fatal(e)
			}
			ledger, e := store.LedgerBalance(ctx, db, tenant.ID)
			if e != nil || row.Status != store.StatusReversed || live != 1000 || live != ledger || events != 2 {
				t.Fatalf("status=%s live=%d ledger=%d events=%d err=%v", row.Status, live, ledger, events, e)
			}
		})
	}
}
