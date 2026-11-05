package comparison

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestSQLGateChildProcess(t *testing.T) {
	if os.Getenv("IDEM_GATE_CHILD") != "1" {
		t.Skip("helper only")
	}
	db, e := gorm.Open(mysql.Open(os.Getenv("COMPARISON_MYSQL_DSN")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if e != nil {
		t.Fatal(e)
	}
	g := sqlIdemGate{db: db}
	ctx := context.Background()
	key := os.Getenv("IDEM_GATE_REQUEST")
	if state, e := g.claim(ctx, key, "fingerprint", "crashed-owner", time.Second); e != nil || state != gateOwned {
		t.Fatal(state, e)
	}
	if os.Getenv("IDEM_GATE_STAGE") == "forwarding" {
		if ok, e := g.beginForward(ctx, key, "crashed-owner"); e != nil || !ok {
			t.Fatal(ok, e)
		}
	}
	fmt.Println("GATE_READY")
	time.Sleep(time.Minute)
}

func TestSQLGateKilledOwner(t *testing.T) {
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	g := sqlIdemGate{db: db}
	if e := g.migrate(); e != nil {
		t.Fatal(e)
	}
	for _, stage := range []string{"claimed", "forwarding"} {
		t.Run(stage, func(t *testing.T) {
			key := fmt.Sprintf("process-%s-%d", stage, time.Now().UnixNano())
			defer db.Where("request_id = ?", key).Delete(&sqlIdemIntent{})
			cmd := exec.Command(os.Args[0], "-test.run=^TestSQLGateChildProcess$")
			cmd.Env = append(os.Environ(), "IDEM_GATE_CHILD=1", "IDEM_GATE_REQUEST="+key, "IDEM_GATE_STAGE="+stage)
			stdout, e := cmd.StdoutPipe()
			if e != nil {
				t.Fatal(e)
			}
			cmd.Stderr = os.Stderr
			if e := cmd.Start(); e != nil {
				t.Fatal(e)
			}
			ready := make(chan bool, 1)
			go func() {
				scan := bufio.NewScanner(stdout)
				for scan.Scan() {
					if strings.TrimSpace(scan.Text()) == "GATE_READY" {
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
					t.Fatal("child exited before readiness")
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
			// Use actual DB time to await the one-second lease, not a test-only update.
			time.Sleep(1200 * time.Millisecond)
			state, e := g.claim(context.Background(), key, "fingerprint", "replacement", 3*time.Second)
			if e != nil {
				t.Fatal(e)
			}
			if stage == "claimed" && state != gateOwned {
				t.Fatalf("claimed owner was not recoverable: %s", state)
			}
			if stage == "forwarding" && state != gateForwardUnknown {
				t.Fatalf("forwarding owner was retried: %s", state)
			}
		})
	}
}
