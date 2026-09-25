// Package jev TypeSafe Jev「System One」判定模型客户端（P3）。
//
// Jev 不做文本生成：输入一段 state（文本/JSON）+ 一组类型化问题，返回每个问题的
// 类型化答案 + 概率/置信度（约 0.1s/次）。定位 = 给网关的判断类环节提速降本：
// 意图识别、写操作守门、风险评分。
//
// 契约以官方 OpenAPI 为准（https://api.typesafe.ai/openapi.json，v0.2.0）：
//
//	POST /v1/systemone
//	{"model": "jev-latest", "state": <string|object|array>,
//	 "questions": {"<name>": {"type": "noul|choice|score", ...}}}
//
// 三种问题的字段与答案形状各不相同（这是最容易踩错的地方，之前就照错的形状写过）：
//
//	noul   : instructions + criteria{true,false}  → {"type":"noul","noul":0.98}
//	choice : instructions + criteria{opt:desc}    → {"type":"choice","choice":"x","confidence":0.9,"probabilities":{...}}
//	score  : instructions + criteria[有序档位]     → {"type":"score","score":1.9,"confidence":0.87,"legend":{...},"probabilities":{...}}
//
// 注意命名差异：请求里问题的描述字段叫 instructions（不是 question），
// 选项/档位叫 criteria（不是 choices），noul 的判定值直接叫 noul（不是 probability）。
package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultEndpoint 官方 System One 端点（旧版代码默认的 /v1/chat/completions 是错的，会 404）。
const DefaultEndpoint = "https://api.typesafe.ai/v1/systemone"

// DefaultModel 模型别名；GET /v1/models 可列出可用名。
const DefaultModel = "jev-latest"

// QuestionType 问题类型。
type QuestionType string

const (
	QNoul   QuestionType = "noul"   // 是/否二元判断 → 为真的概率
	QChoice QuestionType = "choice" // 多选分类 → 选中项 + 各项概率
	QScore  QuestionType = "score"  // 有序量表打分 → 期望分数 + 分布
)

// Question 一个类型化问题。
//
// 一个字段兼容三种类型（与官方 oneOf 结构对齐）：
//   - Noul  : Criteria 用 map{"true":..., "false":...} 说明什么算真/假
//   - Choice: Criteria 用 map{选项名: 判定标准}
//   - Score : Criteria 用 []string 有序档位（下标即分值）
type Question struct {
	Type         QuestionType `json:"type"`
	Instructions string       `json:"instructions,omitempty"`
	Criteria     any          `json:"criteria,omitempty"`
}

// Answer 单个问题的答案（三种类型的字段并集）。
//
// 判定时统一走 Confidence()：noul 用 noul 概率，choice/score 用 confidence。
type Answer struct {
	Type string `json:"type"`

	// noul
	Noul float64 `json:"noul,omitempty"`

	// choice
	Choice        string             `json:"choice,omitempty"`
	Confidence    float64            `json:"confidence,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`

	// score
	Score float64            `json:"score,omitempty"`
	Legend map[string]string `json:"legend,omitempty"`
}

// Conf 取该答案的置信度（0~1），供阈值判定统一使用。
//
// noul 的语义是「为真的概率」，本身就是置信度；
// choice/score 另有 confidence 字段（选中的把握程度）。
// （方法名不叫 Confidence，避免与 JSON 字段同名冲突。）
func (a Answer) Conf() float64 {
	if a.Type == string(QNoul) || (a.Type == "" && a.Noul != 0) {
		return a.Noul
	}
	if a.Confidence != 0 {
		return a.Confidence
	}
	return a.Noul
}

// Probability 取某个选项/档位的概率（choice/score 用）。
func (a Answer) Probability(name string) float64 {
	if a.Probabilities == nil {
		return 0
	}
	return a.Probabilities[name]
}

// Result 一次评估的全部答案（按问题名索引）。
type Result struct {
	Model   string
	Answers map[string]Answer
	Raw     string // 原始响应（调试/审计用）
}

// Evaluator 评估面：生产是 HTTP Client，测试用 Fake。
type Evaluator interface {
	Evaluate(ctx context.Context, state any, questions map[string]Question) (*Result, error)
}

// ---------- HTTP 客户端 ----------

// Client Jev HTTP 客户端。
type Client struct {
	endpoint string
	apiKey   string
	model    string
	cli      *http.Client
}

func NewClient(endpoint, apiKey, model string) *Client {
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	if model == "" {
		model = DefaultModel
	}
	return &Client{
		endpoint: endpoint,
		apiKey:   apiKey,
		model:    model,
		cli:      &http.Client{Timeout: 10 * time.Second},
	}
}

// Evaluate 发一次判定：state + questions → 类型化答案。
func (c *Client) Evaluate(ctx context.Context, state any, questions map[string]Question) (*Result, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("JEV_API_KEY 未配置，判定不可用")
	}
	if len(questions) == 0 {
		return nil, fmt.Errorf("questions 为空")
	}
	body, err := json.Marshal(map[string]any{
		"model":     c.model,
		"state":     state,
		"questions": questions,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.cli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jev 请求失败: %w", err)
	}
	defer func() { io.Copy(io.Discard, resp.Body); resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jev HTTP %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	return decodeResult(raw)
}

// decodeResult 解码 SystemOneResponse。
//
// 官方形状固定为 {"model":..,"answers":{name:{...}},"usage":{..}}，
// 同时容忍「直接把 answers 摊平在顶层」的非标准返回。
func decodeResult(raw []byte) (*Result, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, fmt.Errorf("jev 响应解析失败: %w", err)
	}

	out := &Result{Raw: string(raw)}
	if m, ok := probe["model"]; ok {
		_ = json.Unmarshal(m, &out.Model)
	}

	answers := map[string]Answer{}
	if inner, ok := probe["answers"]; ok {
		if err := json.Unmarshal(inner, &answers); err != nil {
			return nil, fmt.Errorf("jev answers 解析失败: %w", err)
		}
	} else {
		// 非标准：逐键当答案解，跳过元数据字段
		for name, rawAns := range probe {
			if name == "model" || name == "usage" || name == "type" {
				continue
			}
			var a Answer
			if err := json.Unmarshal(rawAns, &a); err != nil || a.Type == "" {
				continue
			}
			answers[name] = a
		}
	}
	if len(answers) == 0 {
		return nil, fmt.Errorf("jev 响应无答案: %s", truncate(string(raw), 160))
	}
	out.Answers = answers
	return out, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
