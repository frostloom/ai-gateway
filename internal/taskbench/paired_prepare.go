package taskbench

import (
	"encoding/json"
	"fmt"
	"reflect"
)

type PairAnswerStandard struct {
	Key          string `json:"key"`
	Focus        string `json:"focus"`
	ReviewStatus string `json:"review_status"`
	Facts        string `json:"facts"`
}
type SmallPairPlan struct {
	Schema           string               `json:"schema"`
	Suite            ConversationSuite    `json:"suite"`
	Standards        []PairAnswerStandard `json:"answer_standards"`
	MaxCases         int                  `json:"max_cases"`
	ProposedMaxCalls int                  `json:"proposed_max_calls"`
	MaxOutputTokens  int                  `json:"max_output_tokens"`
	ModelCalls       int                  `json:"model_calls"`
	ReadyForPaidRun  bool                 `json:"ready_for_paid_run"`
	Limitations      []string             `json:"limitations"`
}

var smallPairSelection = []struct{ key, focus string }{
	{"balance-001/answer_complete", "现金余额与账本一致；高置信直接读候选"},
	{"plans-001/answer_complete", "套餐目录名称、价格、刷新小时和额度"},
	{"bills-001/answer_complete", "默认最近三笔，金额/token/时间；不是完整月份"},
	{"consumption-001/answer_complete", "近七日现金扣费，不能当token使用量"},
	{"subscription-001/answer_complete", "当前订阅、到期、续费与窗口剩余额度"},
	{"balance-020/explicit_scope_followup", "歧义应追问后读取；实际JEV置信度与回退单独观察"},
	{"recharge-001/supplement_then_confirm", "必要追问金额、预览、确认后才执行"},
	{"recharge-002/confirm", "明确充值先预览，确认后一次执行"},
	{"recharge-002/cancel", "取消清除pending，不执行"},
	{"recharge-002/duplicate_confirm", "第二次确认不能重复扣款"},
	{"recharge-002/cancel_then_confirm", "取消后确认不能执行旧申请"},
	{"refund-001/balance_cap", "必要追问和确认，退款不超过现金余额"},
}

func PrepareSmallPair(source ConversationSuite) (SmallPairPlan, error) {
	p := SmallPairPlan{Schema: "small-paired-plan.v1", Suite: ConversationSuite{DefinitionSHA256: source.DefinitionSHA256, ManifestSHA256: source.ManifestSHA256}, MaxCases: 12, ProposedMaxCalls: 24, MaxOutputTokens: 128, Limitations: []string{
		"Development examples, not independently blind gold or population accuracy.",
		"Expected behavior is the exact frozen rules/assertions; answer truth requires runtime oracle and independent semantic review.",
		"No model endpoint, credentials, currency tariff or paid authorization is discovered by prepare.",
		"24 attempts is a small pilot ceiling, not enough to guarantee all 24 arms complete; unrun arms stay in denominator.",
		"Low-confidence fallback is observed from real JEV telemetry, never forced or assumed in this sample.",
	}}
	byKey := map[string]ConversationCase{}
	for _, c := range source.Cases {
		if _, ok := byKey[c.Key()]; ok {
			return p, fmt.Errorf("duplicate source case")
		}
		byKey[c.Key()] = c
	}
	for _, choice := range smallPairSelection {
		c, ok := byKey[choice.key]
		if !ok {
			return p, fmt.Errorf("representative case absent: %s", choice.key)
		}
		if err := validateConversation(c); err != nil {
			return p, err
		}
		if !c.Scenario.FullTaskMetricEligible || c.Scenario.MeasurementScope != "complete_task" {
			return p, fmt.Errorf("representative case not full task")
		}
		p.Suite.Cases = append(p.Suite.Cases, c)
		p.Standards = append(p.Standards, PairAnswerStandard{choice.key, choice.focus, "development_requires_independent_semantic_review", "Use independent tenant/time-bound snapshots; absent fact authority remains not_evaluated. Necessary clarification and preview are legitimate intermediate actions."})
	}
	// Return a detached copy; callers cannot mutate the frozen source in memory.
	b, err := json.Marshal(p)
	var detached SmallPairPlan
	if err == nil {
		err = json.Unmarshal(b, &detached)
	}
	return detached, err
}
func ValidateSmallPair(source ConversationSuite, p SmallPairPlan) error {
	want, err := PrepareSmallPair(source)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(want, p) {
		return fmt.Errorf("small paired plan changed from frozen preparation")
	}
	return nil
}
