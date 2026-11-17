// Package main 实现 L2 离线判定基准：把 campaign packets 还原成完整对话请求体，
// 逐例走生产 autoroute.Selector.Decide 路径调用 JEV，落盘每例分数、概率、时延、
// 兜底与原始用量。输出 JSONL 支持断点续跑（已存在的 case+rep 自动跳过）。
//
// 用法：
//
//	autoroute-bench -records eval/campaigns/2026-11-10-langfuse-r2/records \
//	  -labels labels.json -out eval/autoroute/l2-2026-11-17 \
//	  -subset 100 -reps 4 -workers 3
//
// 付费运行需环境变量 JEV_API_KEY（可选 JEV_HTTPS_PROXY）；-dry-run 使用确定性
// 桩评估器验证管道，不联网不计费。
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/frostloom/ai-gateway/internal/autoroute"
	"github.com/frostloom/ai-gateway/internal/jev"
)

func main() {
	var (
		recordsDir = flag.String("records", "", "campaign records 目录（每例一个 JSON，含 packet 路径）")
		prompts    = flag.String("prompts", "", "异构提示词 JSONL（id,category,expect,user），与 -records 二选一")
		labelsPath = flag.String("labels", "", "难度标签 JSONL（case_id,label），可选")
		outDir     = flag.String("out", "", "输出目录（写 results.jsonl 与 stability.jsonl）")
		subset     = flag.Int("subset", 100, "稳定性抽样条数")
		reps       = flag.Int("reps", 4, "抽样条目额外重复次数")
		workers    = flag.Int("workers", 3, "并发数")
		limit      = flag.Int("limit", 0, "只跑前 N 例（0=全部），冒烟用")
		dryRun     = flag.Bool("dry-run", false, "确定性桩评估器，不联网")
		timeoutMS  = flag.Int("timeout-ms", 2000, "单次判定预算（与生产默认一致）")
	)
	flag.Parse()
	if (*recordsDir == "" && *prompts == "") || *outDir == "" {
		fmt.Fprintln(os.Stderr, "usage: autoroute-bench -records DIR | -prompts FILE -out DIR [flags]")
		os.Exit(2)
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		log.Error("mkdir", "err", err)
		os.Exit(1)
	}
	var cases []benchCase
	var labels map[string]string
	if *prompts != "" {
		var err error
		cases, labels, err = loadPrompts(*prompts)
		if err != nil {
			log.Error("load prompts", "err", err)
			os.Exit(1)
		}
	} else {
		loaded, err := loadRecords(*recordsDir)
		if err != nil {
			log.Error("load records", "err", err)
			os.Exit(1)
		}
		sort.Slice(loaded, func(i, j int) bool { return loaded[i].ID < loaded[j].ID })
		cases = loaded
		labels = loadLabels(*labelsPath)
	}
	if *limit > 0 && *limit <= len(cases) {
		cases = cases[:*limit]
	}

	var eval jev.Evaluator
	if *dryRun {
		eval = &stubEval{}
	} else {
		endpoint := os.Getenv("JEV_ENDPOINT")
		if endpoint == "" {
			endpoint = jev.DefaultEndpoint
		}
		model := os.Getenv("JEV_MODEL")
		if model == "" {
			model = jev.DefaultModel
		}
		client, err := jev.NewEvaluationClient(endpoint, os.Getenv("JEV_API_KEY"), model, jev.ProxyTransport())
		if err != nil {
			log.Error("jev client", "err", err)
			os.Exit(1)
		}
		eval = client
	}

	cfg := defaultBenchConfig(*timeoutMS)
	rec := &recorder{inner: eval}
	selector := autoroute.NewSelector(cfg, rec)
	outPath := filepath.Join(*outDir, "results.jsonl")
	done := loadDone(outPath)

	runPass := func(rep int, list []benchCase, tag string) {
		sem := make(chan struct{}, *workers)
		var wg sync.WaitGroup
		var mu sync.Mutex
		out, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			log.Error("open output", "err", err)
			os.Exit(1)
		}
		defer out.Close()
		for _, c := range list {
			key := fmt.Sprintf("%s|%d", c.ID, rep)
			if done[key] {
				continue
			}
			wg.Add(1)
			sem <- struct{}{}
			go func(c benchCase) {
				defer wg.Done()
				defer func() { <-sem }()
				rec.setCurrent(c.ID)
				start := time.Now()
				dec := selector.Decide(context.Background(), time.Now(), c.Body)
				raw, evalErr := rec.takeLast()
				row := resultRow{
					Case: c.ID, Rep: rep, Label: labels[c.ID], Tag: tag, Category: c.Category,
					Judged: dec.Judged, Score: dec.Score, Tier: tierAt(dec.Score, cfg.Threshold),
					Applied: dec.Applied, ServedModel: dec.ServedModel, WouldBeModel: dec.WouldBeModel,
					Fallback: dec.Fallback, LatencyMS: dec.LatencyMS, WallMS: ms(time.Since(start)),
					EvalErrKind: evalErr,
				}
				if raw != "" {
					row.Usage = usageOf(raw)
				}
				mu.Lock()
				line, _ := json.Marshal(row)
				fmt.Fprintf(out, "%s\n", line)
				mu.Unlock()
			}(c)
		}
		wg.Wait()
	}

	runPass(0, cases, "main")
	if *subset > 0 && *reps > 0 && len(cases) > 0 {
		stride := len(cases) / *subset
		if stride < 1 {
			stride = 1
		}
		var sampled []benchCase
		for i := 0; i < len(cases); i += stride {
			sampled = append(sampled, cases[i])
		}
		for rep := 1; rep <= *reps; rep++ {
			runPass(rep, sampled, "stability")
		}
	}
	log.Info("bench finished", "cases", len(cases), "out", outPath)
}

// ---------- 数据装载 ----------

type benchCase struct {
	ID       string
	Body     []byte
	Category string
}

// loadPrompts 读异构提示词 JSONL，期望档（expect）即构造标签。
func loadPrompts(path string) ([]benchCase, map[string]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var cases []benchCase
	labels := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		var p struct {
			ID       string `json:"id"`
			Category string `json:"category"`
			Expect   string `json:"expect"`
			User     string `json:"user"`
		}
		if json.Unmarshal([]byte(line), &p) != nil || p.ID == "" || p.User == "" {
			continue
		}
		body, err := json.Marshal(map[string]any{
			"model":    "auto",
			"messages": []map[string]string{{"role": "user", "content": p.User}},
		})
		if err != nil {
			return nil, nil, err
		}
		cases = append(cases, benchCase{ID: p.ID, Body: body, Category: p.Category})
		labels[p.ID] = p.Expect
	}
	if len(cases) == 0 {
		return nil, nil, fmt.Errorf("no prompts loaded")
	}
	return cases, labels, nil
}

type packetFile struct {
	Turns []struct {
		User  string `json:"User"`
		Reply string `json:"Reply"`
	} `json:"Turns"`
}

type recordFile struct {
	ID    string `json:"id"`
	Arms  []struct {
		Packet string `json:"packet"`
	} `json:"arms"`
}

func loadRecords(dir string) ([]benchCase, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	var out []benchCase
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var rec recordFile
		if err := json.Unmarshal(raw, &rec); err != nil || rec.ID == "" || len(rec.Arms) == 0 || rec.Arms[0].Packet == "" {
			continue
		}
		body, err := buildBody(filepath.Join(filepath.Dir(dir), rec.Arms[0].Packet))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", rec.ID, err)
		}
		out = append(out, benchCase{ID: rec.ID, Body: body})
	}
	return out, nil
}

// buildBody 把 packet 还原成网关视角的完整对话请求体。
func buildBody(packetPath string) ([]byte, error) {
	raw, err := os.ReadFile(packetPath)
	if err != nil {
		return nil, err
	}
	var p packetFile
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	msgs := make([]map[string]string, 0, len(p.Turns)*2)
	for _, t := range p.Turns {
		msgs = append(msgs, map[string]string{"role": "user", "content": t.User})
		if t.Reply != "" {
			msgs = append(msgs, map[string]string{"role": "assistant", "content": t.Reply})
		}
	}
	return json.Marshal(map[string]any{"model": "auto", "messages": msgs})
}

func loadLabels(path string) map[string]string {
	out := map[string]string{}
	if path == "" {
		return out
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(raw), "\n") {
		var row struct {
			CaseID string `json:"case_id"`
			Label  string `json:"label"`
		}
		if json.Unmarshal([]byte(line), &row) == nil && row.CaseID != "" {
			out[row.CaseID] = row.Label
		}
	}
	return out
}

func loadDone(path string) map[string]bool {
	out := map[string]bool{}
	raw, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(raw), "\n") {
		var row resultRow
		if json.Unmarshal([]byte(line), &row) == nil && row.Case != "" {
			out[fmt.Sprintf("%s|%d", row.Case, row.Rep)] = true
		}
	}
	return out
}

// ---------- 录制评估器 ----------

type recorder struct {
	inner    jev.Evaluator
	mu       sync.Mutex
	current  string
	lastRaw  string
	lastErrK string
}

func (r *recorder) setCurrent(caseID string) {
	r.mu.Lock()
	r.current, r.lastRaw, r.lastErrK = caseID, "", ""
	r.mu.Unlock()
}

func (r *recorder) takeLast() (string, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastRaw, r.lastErrK
}

func (r *recorder) Evaluate(ctx context.Context, state any, questions map[string]jev.Question) (*jev.Result, error) {
	res, err := r.inner.Evaluate(ctx, state, questions)
	r.mu.Lock()
	if err != nil {
		r.lastErrK = jev.FailureDetails(err).Kind
		r.lastRaw = ""
	} else if res != nil {
		r.lastRaw = res.Raw
	}
	r.mu.Unlock()
	return res, err
}

// stubEval 确定性桩：对 state 序列化字节取哈希映射到 [0,3)，干跑验证管道用。
type stubEval struct{}

func (s *stubEval) Evaluate(_ context.Context, state any, _ map[string]jev.Question) (*jev.Result, error) {
	raw, _ := json.Marshal(state)
	h := sha256.Sum256(raw)
	score := float64(int(h[0])%300) / 100
	body := fmt.Sprintf(`{"model":"stub","answers":{"complexity":{"type":"score","score":%.2f,"confidence":0.9}},"usage":{"input_tokens":10}}`, score)
	var res jev.Result
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		return nil, err
	}
	res.Raw = body
	return &res, nil
}

// ---------- 结果行 ----------

type resultRow struct {
	Case        string             `json:"case"`
	Rep         int                `json:"rep"`
	Tag         string             `json:"tag"`
	Category    string             `json:"category,omitempty"`
	Label       string             `json:"label,omitempty"`
	Judged      bool               `json:"judged"`
	Score       float64            `json:"score"`
	Tier        string             `json:"tier"`
	Applied     bool               `json:"applied"`
	ServedModel string             `json:"served_model"`
	WouldBeModel string            `json:"would_be_model,omitempty"`
	Fallback    string             `json:"fallback,omitempty"`
	LatencyMS   float64            `json:"latency_ms"`
	WallMS      float64            `json:"wall_ms"`
	EvalErrKind string             `json:"eval_err_kind,omitempty"`
	Usage       map[string]float64 `json:"usage,omitempty"`
}

func tierAt(score, threshold float64) string {
	if score >= threshold {
		return "strong"
	}
	return "cheap"
}

func usageOf(raw string) map[string]float64 {
	var probe struct {
		Usage map[string]float64 `json:"usage"`
	}
	if json.Unmarshal([]byte(raw), &probe) != nil || probe.Usage == nil {
		return nil
	}
	return probe.Usage
}

func defaultBenchConfig(timeoutMS int) autoroute.Config {
	cfg, err := autoroute.ConfigFromEnv(func(string) string { return "" })
	if err != nil {
		panic(err)
	}
	cfg.Mode = autoroute.ModeOn
	cfg.TimeoutMS = timeoutMS
	cfg.JEVAPIKey = "bench"
	return cfg
}

func ms(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000
}
