package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"
)

// RuntimeMetadata identifies the actual shared model/execution contract. It
// contains no credentials. Dataset/fixture hashes belong to the evaluator.
type RuntimeMetadata struct {
	Model            string             `json:"model"`
	Protocol         string             `json:"protocol"`
	EndpointSHA256   string             `json:"endpoint_sha256"`
	PromptSHA256     string             `json:"prompt_sha256"`
	ToolsSHA256      string             `json:"tools_sha256"`
	ContractSHA256   string             `json:"contract_sha256"`
	RoutingMode      string             `json:"routing_mode"`
	RoutingRevision  string             `json:"routing_revision"`
	Thresholds       map[string]float64 `json:"thresholds"`
	MaxOutputTokens  int                `json:"max_output_tokens"`
	ThinkingMode     string             `json:"thinking_mode,omitempty"`
	ReplyRepairLimit int                `json:"reply_repair_limit"`
}

const runtimeContract = "customer-contract-v4;tenant-auth;read-only-four-first-independent-no-params;intent-shape>=0.90;legit>=0.85;risk<0.5;risk_conf>=0.8;shared-preview-confirm;no-pending-overwrite;empty-pending-confirm-noop;finite-recharge-completion-needs-execution;finite-explicit-own-recharge-preview-application;reply-repair-max-one;explicit-own-cash-read-recovery-once-after-repair;no-numeric-inference;one-tool-per-iteration;loop-max-five"

// EvaluationRuntimeMetadata captures configured hashes and bounds before an
// evaluation sends any model request. It performs no network/environment access
// and returns a fresh metadata value without provider credentials.
func (h *Handler) EvaluationRuntimeMetadata() *RuntimeMetadata { return h.runtimeMetadata() }

func contentSHA(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (h *Handler) runtimeMetadata() *RuntimeMetadata {
	metadata := &RuntimeMetadata{Model: "unknown", Protocol: "unknown", RoutingMode: string(h.RoutingMode()),
		RoutingRevision: "selective-context-prefilter-v1",
		PromptSHA256:    contentSHA([]byte(systemPrompt)), ContractSHA256: contentSHA([]byte(runtimeContract)),
		ReplyRepairLimit: 1, Thresholds: map[string]float64{"intent_min": .90, "shape_min": .90,
			"legit_min": .85, "risk_max_exclusive": .5, "risk_confidence_min": .8}}
	if h.jevFastPathMin > 0 {
		metadata.Thresholds["intent_min"] = h.jevFastPathMin
		metadata.Thresholds["shape_min"] = h.jevFastPathMin
	}
	if h.tb != nil {
		raw, _ := json.Marshal(h.tb.All())
		metadata.ToolsSHA256 = contentSHA(raw)
	}
	if model, ok := h.llm.(*LLMClient); ok {
		metadata.Model, metadata.Protocol, metadata.MaxOutputTokens = model.Model(), model.Protocol(), model.maxTokens
		metadata.EndpointSHA256 = endpointSHA(model.baseURL)
		if model.evaluationDisablesThinking() {
			metadata.ThinkingMode = "disabled"
		}
	}
	return metadata
}

func endpointSHA(base string) string {
	u, err := url.Parse(base)
	if err != nil {
		return ""
	}
	u.User, u.Fragment = nil, ""
	u.Scheme, u.Host = strings.ToLower(u.Scheme), strings.ToLower(u.Host)
	u.Path = strings.TrimRight(u.Path, "/")
	query := u.Query()
	for key := range query {
		switch strings.ToLower(key) {
		case "key", "api_key", "api-key", "token", "access_token", "authorization", "signature":
			query.Del(key)
		}
	}
	u.RawQuery = query.Encode()
	return contentSHA([]byte(u.String()))
}
