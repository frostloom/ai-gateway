// loadtest：M5 压测 + 计费一致性断言。
//
// 并发 -c 个 goroutine 持续 -d 打非流式 /v1/chat/completions，统计
// P50/P95/P99 / 平均 / 吞吐 / 错误分布；结束后做三类断言：
//  1. 无错误（全部 200 —— 出现 402/429/5xx 会列出来）
//  2. Redis 余额 == 账本求和（防超卖/防丢钱）：balance = initial - Σsettled - Σpending
//  3. settle 无重复 delta：同一 request_id 的 settle 行 ≤ 1（幂等唯一索引兜底）
//
// 跑法：bash scripts/loadtest/run.sh（它会同时把 reconciler 拉起来，压测期间并发跑对账）
package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"

	"github.com/frostloom/ai-gateway/internal/billing/store"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// errKindOf 把客户端侧错误分类，压测报告里要能分清「超时」和「拨号失败」——
// 前者说明服务端处理不过来（饱和），后者是端口/连接问题（环境故障）。
func errKindOf(err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "context deadline exceeded"),
		strings.Contains(s, "Client.Timeout"):
		return "timeout"
	case strings.Contains(s, "connection refused"),
		strings.Contains(s, "Only one usage of each socket address"),
		strings.Contains(s, "connection reset"):
		return "dial-fail"
	default:
		return "other"
	}
}

func main() {
	url := flag.String("url", "http://127.0.0.1:18080", "gateway base url")
	key := flag.String("key", "sk-demo-8f3a2b1c9d4e5f60", "api key")
	concurrency := flag.Int("c", 100, "concurrent workers")
	duration := flag.Duration("d", 30*time.Second, "duration")
	model := flag.String("model", "deepseek-v4-flash", "model name")
	tenant := flag.Int64("tenant", 1, "断言针对的租户 id（余额/账本都只算这个租户）")
	rebuild := flag.Bool("rebuild", true, "断言前跑一次 reconciler -once -no-sweep 重建投影")
	flag.Parse()

	mysqlDSN := env("MYSQL_DSN", "root:root@tcp(127.0.0.1:3307)/ai_gateway?parseTime=True&loc=Local")
	redisAddr := env("REDIS_ADDR", "127.0.0.1:6381")

	body, _ := json.Marshal(map[string]any{
		"model":    *model,
		"messages": []map[string]string{{"role": "user", "content": "hello load test"}},
		"stream":   false,
	})

	tr := &http.Transport{MaxIdleConnsPerHost: *concurrency, MaxIdleConns: *concurrency * 2}
	cli := &http.Client{Transport: tr, Timeout: 10 * time.Second}

	testStart := time.Now()
	var (
		wg        sync.WaitGroup
		done      = time.Now().Add(*duration)
		reqs      int64
		errCount  int64
		statusMu  sync.Mutex
		statusCnt = map[int]int64{} // http_code -> count（并发下必须加锁，Load-then-Store 会丢计数）
		errKind   = map[string]int64{}
		errSample = map[string]string{}
		errAt     = map[string]map[int]int64{}
		latMu     sync.Mutex
		lats      []float64
	)
	record := func(code int, lat time.Duration) {
		if code != http.StatusOK {
			atomic.AddInt64(&errCount, 1)
		}
		atomic.AddInt64(&reqs, 1)
		statusMu.Lock()
		statusCnt[code]++
		statusMu.Unlock()
		latMu.Lock()
		lats = append(lats, lat.Seconds()*1000) // ms
		latMu.Unlock()
	}

	// 热身：gateway 刚被 run.sh 重启时，300 个 worker 同时建连会冲爆 accept 队列，
	// 前 1 秒出现成百上千次 connection refused（实测 300 并发冷启动 @0s 拒绝 1.7k 次，
	// 之后 59 秒零拒绝）。先跑 2 秒小并发把连接池/accept 回路跑热，再进计时。
	fmt.Printf("==> 热身 2s（gateway 刚重启，冷启动直接满并发会假性 refused）\n")
	warmDone := time.Now().Add(2 * time.Second)
	var warmWG sync.WaitGroup
	for i := 0; i < 16; i++ {
		warmWG.Add(1)
		go func() {
			defer warmWG.Done()
			for time.Now().Before(warmDone) {
				req, _ := http.NewRequest(http.MethodPost, *url+"/v1/chat/completions", bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+*key)
				if resp, err := cli.Do(req); err == nil {
					_, _ = io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
				}
			}
		}()
	}
	warmWG.Wait()

	for i := 0; i < *concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(done) {
				start := time.Now()
				req, _ := http.NewRequest(http.MethodPost, *url+"/v1/chat/completions", bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+*key)
				resp, err := cli.Do(req)
				if err != nil {
					atomic.AddInt64(&errCount, 1)
					atomic.AddInt64(&reqs, 1)
					statusMu.Lock()
					statusCnt[-1]++ // -1 = 客户端侧错误（超时/断连/拨号失败）
					kind := errKindOf(err)
					errKind[kind]++
					if errSample[kind] == "" {
						errSample[kind] = err.Error()
					}
					if errAt[kind] == nil {
						errAt[kind] = map[int]int64{}
					}
					errAt[kind][int(time.Since(testStart).Seconds())]++
					statusMu.Unlock()
					continue
				}
				code := resp.StatusCode
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				record(code, time.Since(start))
			}
		}()
	}
	wg.Wait()

	// ---------- 统计 ----------
	sort.Float64s(lats)
	percentile := func(p float64) float64 {
		if len(lats) == 0 {
			return 0
		}
		idx := int(float64(len(lats)-1) * p)
		return lats[idx]
	}
	elapsed := (*duration).Seconds()
	total := atomic.LoadInt64(&reqs)
	bad := atomic.LoadInt64(&errCount)
	avg := 0.0
	for _, l := range lats {
		avg += l
	}
	if len(lats) > 0 {
		avg /= float64(len(lats))
	}
	fmt.Printf("\n===== 压测结果 =====\n")
	fmt.Printf("并发 %d × %v：总请求 %d，吞吐 %.1f req/s，错误 %d\n",
		*concurrency, *duration, total, float64(total)/elapsed, bad)
	fmt.Printf("延迟(ms)：P50=%.1f  P95=%.1f  P99=%.1f  平均=%.1f\n",
		percentile(0.50), percentile(0.95), percentile(0.99), avg)
	statusMu.Lock()
	for code, n := range statusCnt {
		if code == -1 {
			fmt.Printf("  客户端侧错误 = %d\n", n)
		} else {
			fmt.Printf("  HTTP %d = %d\n", code, n)
		}
	}
	for kind, n := range errKind {
		fmt.Printf("    错误类型 %-10s = %d  样例: %s\n", kind, n, errSample[kind])
		secs := make([]int, 0, len(errAt[kind]))
		for s := range errAt[kind] {
			secs = append(secs, s)
		}
		sort.Ints(secs)
		if len(secs) > 0 {
			fmt.Printf("      分布: 首次@%ds 末次@%ds 出现秒数=%d", secs[0], secs[len(secs)-1], len(secs))
			if len(secs) <= 12 {
				for _, s := range secs {
					fmt.Printf(" %ds:%d", s, errAt[kind][s])
				}
			}
			fmt.Println()
		}
	}
	statusMu.Unlock()

	// ---------- 断言 1：无错误 ----------
	pass, fail := 0, 0
	ok1 := bad == 0
	if ok1 {
		pass++
		fmt.Printf("[PASS] 请求全部 200，无 402/429/5xx/连接错误\n")
	} else {
		fail++
		fmt.Printf("[FAIL] 存在 %d 个非 200 响应\n", bad)
	}

	// ---------- 断言 2：Redis 余额 == 账本求和 ----------
	if *rebuild {
		// 压测期间只跑 sweep（安全），这里在流量停止后跑一次 rebuild 收敛投影。
		// 原因：rebuild 与在途 Reserve/Settle 竞态会漏掉 pre 预占，必须流量安静时跑。
		if out, err := exec.Command("./bin/reconciler.exe", "-once", "-no-sweep", "-grace", "1").CombinedOutput(); err != nil {
			log.Fatalf("rebuild balance failed: %v\n%s", err, out)
		}
	}
	db, err := sql.Open("mysql", mysqlDSN)
	if err != nil {
		log.Fatal("open mysql: ", err)
	}
	defer db.Close()

	var initial int64
	if err := db.QueryRow("SELECT initial_quota FROM tenants WHERE id=?", *tenant).Scan(&initial); err != nil {
		log.Fatal("query tenants: ", err)
	}
	var settled, pending, topups int64
	// 注意：必须按租户过滤！seed 造了多租户数据，若不过滤会把其他租户的 settled 也算进来，
	// 对不上本租户的 Redis 余额（这是 seed 工具揪出的 loadtest 真实 bug）。
	_ = db.QueryRow("SELECT COALESCE(SUM(actual_quota),0) FROM bills WHERE tenant_id=? AND phase='settle' AND status='settled'", *tenant).Scan(&settled)
	_ = db.QueryRow("SELECT COALESCE(SUM(pre_quota),0) FROM bills WHERE tenant_id=? AND phase='reserve' AND status='pending'", *tenant).Scan(&pending)
	// M7：余额公式含 Σtopups（充值/订阅入账正项）
	_ = db.QueryRow("SELECT COALESCE(SUM(amount),0) FROM topups WHERE tenant_id=?", *tenant).Scan(&topups)
	expected := initial + topups - settled - pending

	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer rdb.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	bal, err := rdb.Get(ctx, store.BalanceKey(uint64(*tenant))).Int64()
	if err != nil {
		log.Fatal("get redis balance: ", err)
	}
	fmt.Printf("[账本] initial=%d + topups=%d - settled=%d - pending=%d = %d ; Redis balance=%d\n",
		initial, topups, settled, pending, expected, bal)
	ok2 := bal == expected
	if ok2 {
		pass++
		fmt.Printf("[PASS] Redis 余额 == 账本求和（并发 100 下不超卖、不丢钱）\n")
	} else {
		fail++
		fmt.Printf("[FAIL] 余额不一致：期望 %d，实际 %d\n", expected, bal)
	}

	// ---------- 断言 3：settle 无重复 delta ----------
	var dup int
	_ = db.QueryRow("SELECT COUNT(*) FROM (SELECT request_id FROM bills WHERE phase='settle' GROUP BY request_id HAVING COUNT(*)>1) t").Scan(&dup)
	fmt.Printf("[幂等] 重复 settle 的 request_id 数量 = %d\n", dup)
	ok3 := dup == 0
	if ok3 {
		pass++
		fmt.Printf("[PASS] settle 恰好一次，无重复 delta（幂等唯一索引兜底）\n")
	} else {
		fail++
		fmt.Printf("[FAIL] 存在 %d 个重复 settle\n", dup)
	}

	fmt.Printf("\nloadtest: %d PASS / %d FAIL\n", pass, fail)
	if fail > 0 {
		os.Exit(1)
	}
}
