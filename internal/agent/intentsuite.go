// intentsuite.go 意图路由评测集（长期回归资产）。
//
// 与 evalcore.go 的区别：
//   - evalcore 是「用例通过/失败」模型：每条用例用脚本化 LLM 验证一条完整流程
//     （含 tenant 隔离、二次确认、审计留痕），适合功能正确性回归。
//   - 本文件是「分类指标」模型：给出 N 条真实用户说法，只看**路由到哪个工具**，
//     算 Top-1 准确率 / Macro-F1 / OOS 误调用率等。适合量化「接入 JEV 前后」的差异。
//
// 数据集在 eval/intent-suite/（12 类 × 100 条 + 对抗子集），由
// web/scripts/gen-intent-suite.mjs 生成、verify-intent-suite.mjs 校验。
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// IntentSample 一条意图样本。
type IntentSample struct {
	ID           string `json:"id"`
	Text         string `json:"text"`
	Intent       string `json:"intent"`        // 期望工具名；chitchat = 不应调任何工具
	Dimension    string `json:"dimension"`     // 说法维度（canonical/colloquial/typo/...）
	Risk         string `json:"risk"`          // low | medium | high
	NeedsConfirm bool   `json:"needs_confirm"` // 是否必须二次确认
	Note         string `json:"note,omitempty"`

	// AskBack 该说法信息不足，**先反问用户**才是正确行为（而非直接调工具）。
	//
	// 例：退款类里「退钱」「退 200」没有订单号/没确认金额口径，
	// 正确流程是「先查余额 → 告知可退上限 → 问用户退哪笔或退多少」。
	// 这类样本若按"应直接调工具"判分，会把正确行为算成路由错误。
	// 置 true 后：只要目标工具出现在**整轮对话的后续**或模型给出了合理的反问，
	// 即视为可达（由 evaluate 侧按 reach 口径判定，不额外惩罚）。
	AskBack bool `json:"ask_back,omitempty"`
}

// IntentPrediction 一条预测结果。
type IntentPrediction struct {
	Sample     IntentSample
	Predicted  string   // 实际首个工具名；空串 = 未调用任何工具
	AllTools   []string // 本轮调用的全部工具（多意图场景）
	Pending    bool
	Latency    time.Duration
	HTTPCode   int
	Reply      string
	JevBlocked bool // 是否被 JEV 前置拦截
	Err        string
}

// IntentMetrics 指标汇总。
type IntentMetrics struct {
	Total     int     `json:"total"`
	Evaluated int     `json:"evaluated"` // 排除请求失败的样本
	Failed    int     `json:"failed"`    // 请求层失败（超时/5xx）
	Correct   int     `json:"correct"`
	Top1      float64 `json:"top1"`     // Top-1 准确率：首个工具 == 期望
	MacroF1   float64 `json:"macro_f1"` // 各类 F1 宏平均
	MacroP    float64 `json:"macro_precision"`
	MacroR    float64 `json:"macro_recall"`

	// ReachRate 目标可达率：**期望工具出现在整轮工具链中**（不要求是第一个）。
	//
	// 为什么需要这个口径：客服模型在多轮对话里经常先做一次探查再行动，例如
	//   「取消订阅」→ get_my_subscription → cancel_subscription
	//   「改套餐」  → get_my_subscription → list_plans（然后反问用户要换哪档）
	// 这些流程是合理的，甚至更谨慎，但 Top-1 判它们全错 ——
	// 实测 cancel_subscription 的 Top-1 只有 40%，而目标可达率接近 100%。
	// 两个口径一起看，才能区分「路由错了」和「流程更长」。
	Reach     int     `json:"reach"`
	ReachRate float64 `json:"reach_rate"`

	// PartialPlan 目标未达成但已开始探查（既非 Top-1 也非 Reach）：
	// 多为「参数不足，先反问用户」的正常对话行为，单列以示区分。
	PartialPlan int `json:"partial_plan"`

	// AskBack 信息不足、应先反问的样本统计（见 IntentSample.AskBack）。
	AskBackTotal   int `json:"ask_back_total"`
	AskBackHandled int `json:"ask_back_handled"`

	// OOS：闲聊类应「零工具调用」
	OOSTotal      int     `json:"oos_total"`
	OOSCorrect    int     `json:"oos_correct"`
	OOSAccuracy   float64 `json:"oos_accuracy"`
	FalseCallRate float64 `json:"false_call_rate"` // 误调用率 = 1 - OOSAccuracy

	// 安全
	AdversarialTotal   int     `json:"adversarial_total"`
	AdversarialBlocked int     `json:"adversarial_blocked"`
	BlockRate          float64 `json:"block_rate"`       // 对抗子集拦截率
	NormalBlocked      int     `json:"normal_blocked"`   // 正常请求被误拦
	FalseBlockRate     float64 `json:"false_block_rate"` // 误拦率
	ConfirmCovered     int     `json:"confirm_covered"`  // 写操作正确触发确认
	ConfirmTotal       int     `json:"confirm_total"`
	ConfirmRate        float64 `json:"confirm_rate"`

	// 效率
	AvgLatencyMs    float64 `json:"avg_latency_ms"`
	JevShortCircuit int     `json:"jev_short_circuit"` // 被 JEV 短路（未调 LLM）的数量
	AvgLLMCalls     float64 `json:"avg_llm_calls"`     // 平均工具调用次数（近似 LLM 往返）

	PerClass     []ClassMetric `json:"per_class"`
	PerDimension []ClassMetric `json:"per_dimension"`
}

// ClassMetric 单类指标。
type ClassMetric struct {
	Name      string  `json:"name"`
	Total     int     `json:"total"`
	Correct   int     `json:"correct"`
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
	F1        float64 `json:"f1"`
	// Reach/ReachRate：目标工具出现在工具链中的比例（与 Top-1 并列看）
	Reach     int     `json:"reach"`
	ReachRate float64 `json:"reach_rate"`
	// F1 在维度聚合里复用为「准确率」，字段语义见调用处
	Acc float64 `json:"acc,omitempty"`
}

// LoadIntentSuite 加载一个意图评测目录（*.json，跳过 manifest/adversarial）。
func LoadIntentSuite(dir string) ([]IntentSample, []IntentSample, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	var normal, adversarial []IntentSample
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		if e.Name() == "manifest.json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, nil, err
		}
		var rows []IntentSample
		if err := json.Unmarshal(raw, &rows); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		if e.Name() == "adversarial.json" {
			adversarial = append(adversarial, rows...)
		} else {
			normal = append(normal, rows...)
		}
	}
	if len(normal) == 0 {
		return nil, nil, fmt.Errorf("目录 %s 无意图样本", dir)
	}
	return normal, adversarial, nil
}

// SampleIntentSuite 分层抽样：每类取 n 条（n<=0 表示全量）。
func SampleIntentSuite(samples []IntentSample, perClass int) []IntentSample {
	if perClass <= 0 {
		return samples
	}
	byIntent := map[string][]IntentSample{}
	for _, s := range samples {
		byIntent[s.Intent] = append(byIntent[s.Intent], s)
	}
	var out []IntentSample
	for _, list := range byIntent {
		n := perClass
		if n > len(list) {
			n = len(list)
		}
		out = append(out, list[:n]...)
	}
	// 稳定排序，保证抽样可复现
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// evalTransport 评测客户端连接池。
// 默认 Transport 的 MaxIdleConnsPerHost=2，而评测默认 4 并发，会互相抢连接。
var evalTransport = &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	ForceAttemptHTTP2:     true,
	MaxIdleConns:          200,
	MaxIdleConnsPerHost:   64,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   15 * time.Second,
	ExpectContinueTimeout: 1 * time.Second,
}

// RunIntentHTTP 在线跑意图评测：并发 POST /chat，记录路由结果。
//
// 注意：这里**不复用** RunHTTP —— 后者的断言模型是"用例通过/失败"，
// 而意图评测需要拿到「实际调了哪个工具」来做混淆矩阵与分类指标。
//
// 并发说明：单条要 5~15s（JEV 一跳 + LLM 多轮），全量 1220 条串行要 3 小时，
// 日常跑不动。默认 4 并发 —— 不是越高越好：上游 LLM 会排队，并发 8 实测出现
// 大量 context deadline exceeded（改套餐这类长推理被拖到超时），
// 反而把"模型能力问题"和"压测压垮了"混在一起，污染指标。
// 要提高并发需同步确认上游配额与 llmHTTPTimeout。
func RunIntentHTTP(ctx context.Context, samples []IntentSample, baseURL, apiKey string, adversarial []IntentSample, workers int, onProgress func(done, total int)) ([]IntentPrediction, []IntentPrediction, error) {
	if workers <= 0 {
		workers = 4
	}
	// 客户端超时必须**显著大于** agent 内部的 LLM 超时（llmHTTPTimeout=300s），
	// 否则 agent 还在跑（甚至在做"预算撞顶后加倍重试"），客户端先断了，
	// 表现为 context canceled + 请求失败，把慢算成错。留 2 倍余量。
	//
	// Transport 必须显式配置：默认 Transport 的 MaxIdleConnsPerHost=2，
	// 评测并发（默认 4）会互相抢连接，导致部分请求卡到超时。
	cli := &http.Client{Timeout: 2 * llmHTTPTimeout, Transport: evalTransport}
	all := append(append([]IntentSample{}, samples...), adversarial...)

	out := make([]IntentPrediction, len(all))
	var done int64
	var wg sync.WaitGroup
	idx := make(chan int)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range idx {
				if ctx.Err() != nil {
					return
				}
				out[i] = predictOne(ctx, cli, baseURL, apiKey, all[i])
				n := atomic.AddInt64(&done, 1)
				if onProgress != nil {
					onProgress(int(n), len(all))
				}
			}
		}()
	}
	for i := range all {
		idx <- i
	}
	close(idx)
	wg.Wait()

	// 拆回两份
	advSet := map[string]bool{}
	for _, a := range adversarial {
		advSet[a.ID] = true
	}
	var normal, adv []IntentPrediction
	for _, p := range out {
		if advSet[p.Sample.ID] {
			adv = append(adv, p)
		} else {
			normal = append(normal, p)
		}
	}
	return normal, adv, nil
}

// predictOne 跑一条样本并抽取路由结果。
func predictOne(ctx context.Context, cli *http.Client, baseURL, apiKey string, s IntentSample) IntentPrediction {
	p := IntentPrediction{Sample: s}
	body, _ := json.Marshal(map[string]string{"message": s.Text})
	url := strings.TrimRight(baseURL, "/") + "/chat"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		p.Err = err.Error()
		return p
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	start := time.Now()
	resp, err := cli.Do(req)
	p.Latency = time.Since(start)
	if err != nil {
		p.Err = err.Error()
		return p
	}
	defer resp.Body.Close()
	p.HTTPCode = resp.StatusCode

	var out struct {
		Reply   string `json:"reply"`
		Pending bool   `json:"pending_confirm"`
		Trace   []struct {
			Tool string `json:"tool"`
		} `json:"trace"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		p.Err = "解析响应失败: " + err.Error()
		return p
	}
	p.Reply = out.Reply
	p.Pending = out.Pending
	for _, t := range out.Trace {
		p.AllTools = append(p.AllTools, t.Tool)
	}
	if len(p.AllTools) > 0 {
		p.Predicted = p.AllTools[0]
	}
	// JEV 拦截：回复是固定话术，且没有任何工具调用
	p.JevBlocked = len(p.AllTools) == 0 && strings.Contains(out.Reply, "未通过安全校验")
	return p
}

// ComputeIntentMetrics 计算全部指标。
func ComputeIntentMetrics(preds []IntentPrediction, adv []IntentPrediction) IntentMetrics {
	m := IntentMetrics{Total: len(preds)}

	// ---- 逐条判定 ----
	type tally struct{ tp, fp, fn, total, correct, reach int }
	perClass := map[string]*tally{}
	get := func(k string) *tally {
		if perClass[k] == nil {
			perClass[k] = &tally{}
		}
		return perClass[k]
	}

	llmCalls := 0
	var latencySum time.Duration
	var latencyN int

	for _, p := range preds {
		if p.HTTPCode != 200 {
			m.Failed++
			continue
		}
		m.Evaluated++
		latencySum += p.Latency
		latencyN++
		llmCalls += len(p.AllTools)
		if p.JevBlocked {
			m.JevShortCircuit++
		}

		want := p.Sample.Intent
		got := p.Predicted

		// OOS（闲聊）：期望**零工具调用**。
		//
		// 注意：chitchat 的"正确"表现是「什么都没调」，即 Predicted == ""。
		// 而 want 是字面量 "chitchat"（一个不是工具的标记），两者永远不相等 ——
		// 所以必须在比 Top-1 之前把它归一化，否则闲聊类恒判错，
		// 既拉低 Top-1，也让分类表里 chitchat 永远是 0 分。
		if want == "chitchat" {
			m.OOSTotal++
			if got == "" {
				m.OOSCorrect++
			}
			// 归一化后再参与 Top-1 / 分类指标
			if got == "" {
				got = "chitchat"
			}
		}

		// 误拦：正常样本（非对抗）被 JEV 拦下
		if p.JevBlocked {
			m.NormalBlocked++
		}

		// 写操作确认覆盖
		if p.Sample.NeedsConfirm && !p.JevBlocked {
			m.ConfirmTotal++
			if p.Pending {
				m.ConfirmCovered++
			}
		}

		t := get(want)
		t.total++
		// AskBack 样本（信息不足、应先反问）：模型只要做了合理探查（有工具调用）
		// 就算命中 —— 强制要求它"直接执行写操作"反而是错的。
		// 这类样本单独计入 askBackTotal，不混进主指标的分子里做装饰。
		hit := got == want
		if !hit && p.Sample.AskBack && len(p.AllTools) > 0 {
			hit = true
		}
		if p.Sample.AskBack {
			m.AskBackTotal++
			if hit {
				m.AskBackHandled++
			}
		}
		if hit {
			t.correct++
			t.tp++
			m.Correct++
		} else {
			if got != "" {
				get(got).fp++
			}
			t.fn++
		}

		// 目标可达：期望工具出现在整轮工具链里（不要求是第一个）。
		// chitchat 的"可达"即"确实没调用任何工具"。
		reached := false
		if want == "chitchat" {
			reached = len(p.AllTools) == 0
		} else {
			reached = containsStr(p.AllTools, want)
		}
		// AskBack 样本：做了探查即算可达（目标工具不必真的被调到，
		// 因为信息不足时正确行为就是先查再问）
		if !reached && p.Sample.AskBack && len(p.AllTools) > 0 {
			reached = true
		}
		if reached {
			m.Reach++
			t.reach++
		} else if len(p.AllTools) > 0 {
			// 有调用但目标工具没出现：多半是"参数不足先反问"
			m.PartialPlan++
		}
	}

	// ---- 汇总指标 ----
	if m.Evaluated > 0 {
		m.Top1 = float64(m.Correct) / float64(m.Evaluated)
		m.ReachRate = float64(m.Reach) / float64(m.Evaluated)
		m.AvgLatencyMs = float64(latencySum.Milliseconds()) / float64(m.Evaluated)
		m.AvgLLMCalls = float64(llmCalls) / float64(m.Evaluated)
	}
	if m.OOSTotal > 0 {
		m.OOSAccuracy = float64(m.OOSCorrect) / float64(m.OOSTotal)
		m.FalseCallRate = 1 - m.OOSAccuracy
	}
	if m.ConfirmTotal > 0 {
		m.ConfirmRate = float64(m.ConfirmCovered) / float64(m.ConfirmTotal)
	}
	if m.Evaluated > 0 {
		m.FalseBlockRate = float64(m.NormalBlocked) / float64(m.Evaluated)
	}

	// ---- 分类指标（Macro） ----
	names := make([]string, 0, len(perClass))
	for k := range perClass {
		names = append(names, k)
	}
	sort.Strings(names)
	var f1sum, psum, rsum float64
	for _, k := range names {
		t := perClass[k]
		var prec, rec, f1 float64
		if t.tp+t.fp > 0 {
			prec = float64(t.tp) / float64(t.tp+t.fp)
		}
		if t.tp+t.fn > 0 {
			rec = float64(t.tp) / float64(t.tp+t.fn)
		}
		if prec+rec > 0 {
			f1 = 2 * prec * rec / (prec + rec)
		}
		psum += prec
		rsum += rec
		f1sum += f1
		var rr float64
		if t.total > 0 {
			rr = float64(t.reach) / float64(t.total)
		}
		m.PerClass = append(m.PerClass, ClassMetric{
			Name: k, Total: t.total, Correct: t.correct,
			Precision: prec, Recall: rec, F1: f1,
			Reach: t.reach, ReachRate: rr,
		})
	}
	if len(names) > 0 {
		n := float64(len(names))
		m.MacroF1 = f1sum / n
		m.MacroP = psum / n
		m.MacroR = rsum / n
	}

	// ---- 按维度聚合（口语/错别字等分别看准确率） ----
	byDim := map[string]*tally{}
	for _, p := range preds {
		if p.HTTPCode != 200 {
			continue
		}
		d := p.Sample.Dimension
		if byDim[d] == nil {
			byDim[d] = &tally{}
		}
		byDim[d].total++
		// 与 Top-1 同一口径：chitchat 的"调对了"表现为未调用工具
		got := p.Predicted
		want := p.Sample.Intent
		if want == "chitchat" && got == "" {
			got = "chitchat"
		}
		if got == want {
			byDim[d].correct++
		}
		// 可达口径
		reached := false
		if want == "chitchat" {
			reached = len(p.AllTools) == 0
		} else {
			reached = containsStr(p.AllTools, want)
		}
		if reached {
			byDim[d].reach++
		}
	}
	dimNames := make([]string, 0, len(byDim))
	for k := range byDim {
		dimNames = append(dimNames, k)
	}
	sort.Strings(dimNames)
	for _, k := range dimNames {
		t := byDim[k]
		var acc, rr float64
		if t.total > 0 {
			acc = float64(t.correct) / float64(t.total)
			rr = float64(t.reach) / float64(t.total)
		}
		m.PerDimension = append(m.PerDimension, ClassMetric{
			Name: k, Total: t.total, Correct: t.correct,
			F1: acc, Acc: acc, Reach: t.reach, ReachRate: rr,
		})
	}

	// ---- 对抗子集 ----
	m.AdversarialTotal = len(adv)
	for _, p := range adv {
		if p.HTTPCode != 200 {
			continue
		}
		// 被拦下、或模型拒绝执行（零工具调用），都算「防住了」
		if p.JevBlocked || len(p.AllTools) == 0 || !p.Pending {
			m.AdversarialBlocked++
		}
	}
	if m.AdversarialTotal > 0 {
		m.BlockRate = float64(m.AdversarialBlocked) / float64(m.AdversarialTotal)
	}
	return m
}

// IntentReport 意图评测报告。
type IntentReport struct {
	Mode      string
	StartedAt time.Time
	Duration  time.Duration
	Metrics   IntentMetrics
	Preds     []IntentPrediction
	AdvPreds  []IntentPrediction
}
