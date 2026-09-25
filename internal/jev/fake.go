// 假 Jev 评估器：脚本化返回，供单测/离线评测（无网络、零成本）。
package jev

import (
	"context"
	"fmt"
	"sync"
)

// FakeScript 按问题名返回固定答案。
type FakeScript struct {
	Answers map[string]Answer
}

// Fake 假 Evaluator：按 script 逐次弹出答案（耗尽报错，暴露预期外调用）。
type Fake struct {
	mu     sync.Mutex
	script []FakeScript
}

// NewFake 构造假评估器；每个 Evaluate 消耗一条 script。
func NewFake(scripts ...FakeScript) *Fake {
	return &Fake{script: scripts}
}

// AnswerNoul 便捷构造 noul 答案（prob = 为真的概率，本身就是置信度）。
func AnswerNoul(prob float64) Answer {
	return Answer{Type: string(QNoul), Noul: prob}
}

// AnswerChoice 便捷构造 choice 答案。
func AnswerChoice(value string, confidence float64) Answer {
	return Answer{
		Type:          string(QChoice),
		Choice:        value,
		Confidence:    confidence,
		Probabilities: map[string]float64{value: confidence},
	}
}

// AnswerScore 便捷构造 score 答案。
func AnswerScore(score, confidence float64, legend []string) Answer {
	lg := map[string]string{}
	for i, s := range legend {
		lg[fmt.Sprint(i)] = s
	}
	return Answer{Type: string(QScore), Score: score, Confidence: confidence, Legend: lg}
}

func (f *Fake) Evaluate(_ context.Context, _ any, questions map[string]Question) (*Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.script) == 0 {
		return nil, fmt.Errorf("假 Jev 脚本耗尽（出现预期外评估调用，问题: %v）", questionNames(questions))
	}
	sc := f.script[0]
	f.script = f.script[1:]
	return &Result{Answers: sc.Answers}, nil
}

func questionNames(questions map[string]Question) []string {
	out := make([]string, 0, len(questions))
	for n := range questions {
		out = append(out, n)
	}
	return out
}