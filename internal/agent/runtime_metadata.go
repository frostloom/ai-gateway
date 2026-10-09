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
	Thresholds       map[string]float64 `json:"thresholds"`
	MaxOutputTokens  int                `json:"max_output_tokens"`
	ReplyRepairLimit int                `json:"reply_repair_limit"`
}

const runtimeContract = "customer-contract-v2;tenant-auth;read-only-four-first-independent-no-params;intent-shape>=0.90;legit>=0.85;risk<0.5;risk_conf>=0.8;shared-preview-confirm;no-pending-overwrite;reply-repair-max-one;no-numeric-inference;one-tool-per-iteration;loop-max-five"

func contentSHA(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (h *Handler) runtimeMetadata() *RuntimeMetadata {
	metadata := &RuntimeMetadata{Model: "unknown", Protocol: "unknown", RoutingMode: string(h.RoutingMode()),
		PromptSHA256: contentSHA([]byte(systemPrompt)), ContractSHA256: contentSHA([]byte(runtimeContract)),
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
