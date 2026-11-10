package localpaired

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/frostloom/ai-gateway/internal/agent"
	"github.com/frostloom/ai-gateway/internal/billing/admin"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/jev"
	"github.com/frostloom/ai-gateway/internal/observability"
	goredis "github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const liveDatabase = "ai_gateway_local_paired_20261110"
const liveBalance int64 = 2000000

type LiveAuth struct {
	Token       string
	AccountID   string
	JEVKey      string
	JEVEndpoint string
	JEVModel    string
	ProxyURL    string
	Langfuse    map[string]string
}

func RunLive(ctx context.Context, root string, auth LiveAuth) error {
	root = filepath.Clean(root)
	if pathRefused(root) {
		return fmt.Errorf("refusing protected path")
	}
	if strings.TrimSpace(auth.Token) == "" || strings.TrimSpace(auth.AccountID) == "" {
		return fmt.Errorf("chatgpt credentials missing")
	}
	if strings.TrimSpace(auth.JEVKey) == "" {
		return fmt.Errorf("jev credentials missing")
	}
	if err := claimRun(root); err != nil {
		return err
	}
	defer os.Remove(filepath.Join(root, "running"))

	logFile, err := os.OpenFile(filepath.Join(root, "run.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer logFile.Close()
	logger := slog.New(slog.NewTextHandler(logFile, nil))
	logger.Info("start", "root", root, "customer_model", CustomerModel)
	cfg, err := observability.ConfigFromEnv(func(key string) string { return auth.Langfuse[key] })
	if err != nil {
		return err
	}
	if !cfg.Enabled {
		return fmt.Errorf("langfuse export disabled")
	}
	cfg.ExportTimeout = 5 * time.Second
	observer, err := observability.New(ctx, cfg)
	if err != nil {
		return err
	}
	defer observer.Shutdown(context.Background())
	sent, err := backfillLangfuse(root, cfg)
	if err != nil {
		logger.Error("langfuse backfill", "err", err.Error())
	} else {
		logger.Info("langfuse backfill", "sent", sent)
	}

	cases, err := loadCases(root)
	if err != nil {
		return err
	}
	db, rdb, st, closeDB, err := openLiveStore()
	if err != nil {
		return err
	}
	defer closeDB()

	httpClient, err := liveHTTPClient(auth.ProxyURL)
	if err != nil {
		return err
	}
	proxy, closeProxy, err := startChatProxy(ResponsesClient{
		Endpoint:  CodexResponsesEndpoint,
		Token:     auth.Token,
		AccountID: auth.AccountID,
		Model:     CustomerModel,
		HTTP:      httpClient,
	})
	if err != nil {
		return err
	}
	defer closeProxy()

	token := "local-paired-internal"
	adminServer := httptest.NewServer(admin.Handler(st, logger, token))
	defer adminServer.Close()
	llm := agent.NewLLMClient(proxy, "local-paired", CustomerModel)
	tools := agent.NewToolbox(agent.NewAdminClientWithToken(adminServer.URL, token))
	modelHandler := agent.NewHandler(nil, tools, llm, st, rdb, logger)
	if err := modelHandler.SetRoutingMode("model"); err != nil {
		return err
	}
	modelHandler.SetAdminAccess(token, 0)
	modelHandler.SetObserver(observer)
	selective := agent.NewHandler(nil, tools, llm, st, rdb, logger)
	if err := selective.SetRoutingMode("jev_selective"); err != nil {
		return err
	}
	selective.SetAdminAccess(token, 0)
	if auth.ProxyURL != "" {
		_ = os.Setenv("JEV_HTTPS_PROXY", auth.ProxyURL)
	}
	selective.SetJev(jev.NewClient(auth.JEVEndpoint, auth.JEVKey, auth.JEVModel))
	selective.SetObserver(observer)

	bundle := Bundle{Root: root}
	done := 0
	failed := 0
	consecutive := 0
	for i, item := range cases {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		recordPath := filepath.Join(root, "records", item.ID+".json")
		if _, err := os.Stat(recordPath); err == nil {
			done++
			consecutive = 0
			continue
		}
		err := runOne(ctx, bundle, db, rdb, modelHandler, selective, token, item)
		flushCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		flushErr := observer.ForceFlush(flushCtx)
		cancel()
		stats := observer.Stats()
		if flushErr != nil || stats.Failed > 0 {
			logger.Error("langfuse", "err", flushErr, "exported", stats.Exported, "failed", stats.Failed)
		}
		if err != nil {
			failed++
			consecutive++
			logger.Error("case", "id", item.ID, "index", i+1, "total", len(cases), "err", err.Error())
			_ = os.WriteFile(filepath.Join(root, "records", item.ID+".error"), []byte(err.Error()+"\n"), 0o644)
			if consecutive >= 5 {
				writeStatus(root, len(cases), done, failed, "stopped")
				return err
			}
			continue
		}
		consecutive = 0
		done++
		_ = os.Remove(filepath.Join(root, "records", item.ID+".error"))
		logger.Info("case", "id", item.ID, "index", i+1, "total", len(cases))
		if done%10 == 0 {
			writeStatus(root, len(cases), done, failed, "running")
		}
	}
	writeStatus(root, len(cases), done, failed, "finished")
	logger.Info("finished", "done", done, "failed", failed, "total", len(cases))
	return nil
}

func runOne(ctx context.Context, bundle Bundle, db *gorm.DB, rdb *goredis.Client, model, selective http.Handler, token string, item storedCase) error {
	modelTenant, err := seedTenant(ctx, db, rdb)
	if err != nil {
		return err
	}
	selectiveTenant, err := seedTenant(ctx, db, rdb)
	if err != nil {
		return err
	}
	sessions := map[string]string{}
	pending := map[string]bool{}
	return bundle.ExecuteCase(ctx, item, nil, func(ctx context.Context, arm string, turn Turn) (Outcome, error) {
		handler := model
		tenant := modelTenant
		if arm == "jev_selective" {
			handler = selective
			tenant = selectiveTenant
		}
		before, _ := rdb.Get(ctx, store.BalanceKey(tenant)).Int64()
		outcome, sessionID, stillPending, err := askAgent(handler, token, tenant, sessions[arm], pending[arm], before, rdb, turn)
		if err != nil {
			return Outcome{}, err
		}
		sessions[arm] = sessionID
		pending[arm] = stillPending
		return outcome, nil
	}, func(context.Context, string, []byte) error {
		return nil
	})
}

func askAgent(handler http.Handler, token string, tenant uint64, sessionID string, pendingBefore bool, balanceBefore int64, rdb *goredis.Client, turn Turn) (Outcome, string, bool, error) {
	payload, _ := json.Marshal(map[string]string{"session_id": sessionID, "message": turn.User})
	req := httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(string(payload)))
	req.Header.Set("X-Agent-Internal", token)
	req.Header.Set("X-Agent-Tenant", strconv.FormatUint(tenant, 10))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var body struct {
		Reply     string `json:"reply"`
		Error     string `json:"error"`
		SessionID string `json:"session_id"`
		Pending   bool   `json:"pending_confirm"`
		Trace     []struct {
			Tool   string `json:"tool"`
			Result string `json:"result"`
		} `json:"trace"`
		Telemetry struct {
			LLMCalls            int     `json:"llm_calls"`
			JevCalls            int     `json:"jev_calls"`
			JevFastPath         bool    `json:"jev_fast_path"`
			JevShapeConfidence  float64 `json:"jev_shape_confidence"`
			JevTopicConfidence  float64 `json:"jev_topic_confidence"`
			JevIntentConfidence float64 `json:"jev_intent_confidence"`
		} `json:"telemetry"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		return Outcome{}, "", false, fmt.Errorf("agent response: %s", rec.Body.String())
	}
	if rec.Code >= 400 {
		return Outcome{}, body.SessionID, body.Pending, fmt.Errorf("agent HTTP %d: %s", rec.Code, body.Error)
	}
	tools := make([]ToolRecord, 0, len(body.Trace))
	for _, item := range body.Trace {
		if item.Tool == "" {
			continue
		}
		tools = append(tools, ToolRecord{Name: item.Tool, Result: item.Result})
	}
	after, _ := rdb.Get(req.Context(), store.BalanceKey(tenant)).Int64()
	tel := body.Telemetry
	return Outcome{
		Reply:           body.Reply,
		Tools:           tools,
		ModelCalls:      tel.LLMCalls,
		JEVCalls:        tel.JevCalls,
		HighConfidence:  tel.JevFastPath || tel.JevTopicConfidence >= 0.90,
		PendingBefore:   pendingBefore,
		PendingAfter:    body.Pending,
		DatabaseChanged: after != balanceBefore,
		Trace:           append([]byte(nil), rec.Body.Bytes()...),
	}, body.SessionID, body.Pending, nil
}

var liveTenants atomic.Int64

func seedTenant(ctx context.Context, db *gorm.DB, rdb *goredis.Client) (uint64, error) {
	tenant := store.Tenant{
		Name:         fmt.Sprintf("lp-%d-%d", time.Now().UnixNano(), liveTenants.Add(1)),
		InitialQuota: liveBalance,
	}
	if err := db.WithContext(ctx).Create(&tenant).Error; err != nil {
		return 0, err
	}
	if err := rdb.Set(ctx, store.BalanceKey(tenant.ID), liveBalance, 0).Err(); err != nil {
		return 0, err
	}
	return tenant.ID, nil
}

func openLiveStore() (*gorm.DB, *goredis.Client, *store.Store, func(), error) {
	adminDSN := "root:root@tcp(127.0.0.1:3307)/?timeout=2s&parseTime=True&loc=Local"
	adminDB, err := gorm.Open(mysql.Open(adminDSN), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if err := adminDB.Exec("CREATE DATABASE IF NOT EXISTS " + liveDatabase + " CHARACTER SET utf8mb4").Error; err != nil {
		return nil, nil, nil, nil, err
	}
	sqlDB, _ := adminDB.DB()
	if sqlDB != nil {
		sqlDB.Close()
	}
	dsn := "root:root@tcp(127.0.0.1:3307)/" + liveDatabase + "?parseTime=True&loc=Local&timeout=2s"
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		return nil, nil, nil, nil, err
	}
	mini, err := miniredis.Run()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	rdb := goredis.NewClient(&goredis.Options{Addr: mini.Addr()})
	st := store.NewStore(db, rdb)
	if err := st.AutoMigrate(); err != nil {
		mini.Close()
		return nil, nil, nil, nil, err
	}
	return db, rdb, st, func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
		_ = rdb.Close()
		mini.Close()
	}, nil
}

func liveHTTPClient(proxyURL string) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if strings.TrimSpace(proxyURL) != "" {
		parsed, err := url.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("proxy url invalid")
		}
		transport.Proxy = http.ProxyURL(parsed)
	}
	return &http.Client{Timeout: 150 * time.Second, Transport: transport}, nil
}

func startChatProxy(client ResponsesClient) (string, func(), error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "read request", http.StatusBadRequest)
			return
		}
		out, err := client.RoundTripChat(r.Context(), raw)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(out)
	})}
	go server.Serve(ln)
	return "http://" + ln.Addr().String(), func() { _ = server.Close() }, nil
}

func loadCases(root string) ([]storedCase, error) {
	raw, err := os.ReadFile(filepath.Join(root, "cases.json"))
	if err != nil {
		return nil, err
	}
	var cases []storedCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		return nil, err
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("question bank is empty")
	}
	return cases, nil
}

func backfillLangfuse(root string, cfg observability.Config) (int, error) {
	endpoint, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return 0, err
	}
	endpoint.Path = "/api/public/ingestion"
	endpoint.RawQuery, endpoint.Fragment = "", ""
	sentPath := filepath.Join(root, "langfuse-sent.txt")
	already := map[string]struct{}{}
	if raw, err := os.ReadFile(sentPath); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			if line != "" {
				already[line] = struct{}{}
			}
		}
	}
	paths, err := filepath.Glob(filepath.Join(root, "records", "*.json"))
	if err != nil {
		return 0, err
	}
	auth := base64.StdEncoding.EncodeToString([]byte(cfg.PublicKey + ":" + cfg.SecretKey))
	sent := 0
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return sent, err
		}
		var record storedRecord
		if err := json.Unmarshal(raw, &record); err != nil {
			return sent, err
		}
		for _, arm := range record.Arms {
			if len(arm.Traces) == 0 {
				continue
			}
			traceID := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(arm.Traces[0]), "trace-"), ".bin")
			if _, ok := already[traceID]; ok {
				continue
			}
			packetRaw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(arm.Packet)))
			if err != nil {
				return sent, err
			}
			var packet struct {
				Turns []struct {
					User  string `json:"User"`
					Reply string `json:"Reply"`
				} `json:"Turns"`
			}
			if err := json.Unmarshal(packetRaw, &packet); err != nil {
				return sent, err
			}
			users := make([]string, 0, len(packet.Turns))
			replies := make([]string, 0, len(packet.Turns))
			for _, turn := range packet.Turns {
				users = append(users, turn.User)
				replies = append(replies, turn.Reply)
			}
			body, err := json.Marshal(map[string]any{"batch": []map[string]any{{
				"id":        traceID + ":create",
				"type":      "trace-create",
				"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
				"body": map[string]any{
					"id":        traceID,
					"name":      record.ID,
					"sessionId": record.ID + ":" + arm.Arm,
					"input":     users,
					"output":    replies,
					"metadata": map[string]any{
						"arm":         arm.Arm,
						"model_calls": arm.ModelCalls,
						"jev_calls":   arm.JEVCalls,
						"pass":        arm.Pass,
					},
				},
			}}})
			if err != nil {
				return sent, err
			}
			req, err := http.NewRequest(http.MethodPost, endpoint.String(), strings.NewReader(string(body)))
			if err != nil {
				return sent, err
			}
			req.Header.Set("Authorization", "Basic "+auth)
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return sent, err
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusMultiStatus {
				return sent, fmt.Errorf("langfuse ingestion HTTP %d", resp.StatusCode)
			}
			if err := os.WriteFile(sentPath, []byte(strings.Join(append(keys(already), traceID), "\n")+"\n"), 0o644); err != nil {
				return sent, err
			}
			already[traceID] = struct{}{}
			sent++
		}
	}
	return sent, nil
}

func keys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	return out
}

func claimRun(root string) error {
	path := filepath.Join(root, "running")
	raw, err := os.ReadFile(path)
	if err == nil {
		pid, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
		if pid > 0 && pidAlive(pid) {
			return fmt.Errorf("campaign already running")
		}
	}
	return os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o644)
}

func pidAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

func writeStatus(root string, total, done, failed int, state string) {
	raw, _ := json.MarshalIndent(map[string]any{
		"state":  state,
		"total":  total,
		"done":   done,
		"failed": failed,
		"at":     time.Now().Format(time.RFC3339),
	}, "", "  ")
	_ = os.WriteFile(filepath.Join(root, "status.json"), append(raw, '\n'), 0o644)
}
