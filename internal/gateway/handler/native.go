package handler

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/frostloom/ai-gateway/internal/pkg/upstream"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	"github.com/frostloom/ai-gateway/internal/pkg/metrics"
	"github.com/frostloom/ai-gateway/internal/pkg/tokens"
	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
)

// Native 接收 Claude 的 Messages 或 Codex 的 Responses，原样转给上游对应入口。
// 请求没写 stream 时默认打开 SSE。计费仍走同一条预扣和结算。
func Native(b billingAPI, r routingAPI, log *slog.Logger, kind string, holdUnknown bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, 8<<20))
		if err != nil || len(body) == 0 {
			abort(c, http.StatusBadRequest, "invalid request body")
			return
		}
		prepared, model, routeModel, stream, maxTokens, err := prepareNativeBody(body)
		if err != nil || model == "" {
			abort(c, http.StatusBadRequest, "model required")
			return
		}

		routeStart := time.Now()
		prov, err := r.Route(c.Request.Context(), routeModel)
		metrics.RequestHop("route", time.Since(routeStart))
		if err != nil {
			abort(c, http.StatusServiceUnavailable, "routing unavailable")
			return
		}
		if prov == nil {
			abort(c, http.StatusServiceUnavailable, "no available provider for model")
			return
		}
		protocolBase := prov.ResponsesBaseURL
		if kind == "messages" {
			protocolBase = prov.MessagesBaseURL
		}
		upstreamURL := nativeUpstream(prov.BaseURL, kind, protocolBase)
		if upstreamURL == "" {
			abort(c, http.StatusServiceUnavailable, "routing unavailable")
			return
		}

		tenantID := c.GetInt64("tenant_id")
		apiKeyID := c.GetInt64("api_key_id")
		requestID := c.GetString("request_id")
		est := tokens.Count(routeModel, string(prepared))
		if est < 1 {
			est = 1
		}
		rc, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		digest := sha256.Sum256(prepared)
		rc = controlplane.WithReserveContent(rc, hex.EncodeToString(digest[:]))
		rr, err := b.Reserve(rc, &billingv1.ReserveRequest{
			RequestId:           requestID,
			TenantId:            tenantID,
			ApiKeyId:            apiKeyID,
			ProviderId:          int64(prov.ProviderID),
			Model:               routeModel,
			EstPromptTokens:     int64(est),
			MaxCompletionTokens: maxTokens,
		})
		cancel()
		if err != nil {
			if status.Code(err) == codes.FailedPrecondition {
				abort(c, http.StatusConflict, "billing request id conflict")
				return
			}
			if status.Code(err) == codes.InvalidArgument {
				abort(c, http.StatusBadRequest, "model not listed or disabled: "+routeModel)
				return
			}
			abort(c, http.StatusServiceUnavailable, "billing unavailable")
			return
		}
		if rr == nil || rr.Code != billingv1.ReserveCode_RESERVE_OK || rr.BillId <= 0 {
			if rr != nil && rr.Code == billingv1.ReserveCode_RESERVE_INSUFFICIENT {
				abort(c, http.StatusPaymentRequired, "insufficient balance")
				return
			}
			abort(c, http.StatusServiceUnavailable, "billing unavailable")
			return
		}
		log.Info("reserved", "request_id", requestID, "tenant_id", tenantID, "model", routeModel, "wire", kind, "stream", stream)

		reqCtx := c.Request.Context()
		if !stream {
			var stop context.CancelFunc
			reqCtx, stop = context.WithTimeout(reqCtx, 2*time.Minute)
			defer stop()
		}
		upReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, upstreamURL, bytes.NewReader(prepared))
		if err != nil {
			reverseWithBackground(b, log, requestID, rr.BillId, "build upstream request failed")
			abort(c, http.StatusBadGateway, "upstream error")
			return
		}
		setUpstreamHeaders(upReq, c, stream, prov.APIKey)
		if kind == "messages" {
			if prov.APIKey != "" {
				upReq.Header.Set("x-api-key", prov.APIKey)
			}
			version := c.GetHeader("anthropic-version")
			if version == "" {
				version = "2023-06-01"
			}
			upReq.Header.Set("anthropic-version", version)
			if beta := c.GetHeader("anthropic-beta"); beta != "" {
				upReq.Header.Set("anthropic-beta", beta)
			}
		}

		cli := upstreamClient
		if stream {
			cli = streamClient
		}
		upResp, err := cli.Do(upReq)
		if err != nil {
			if holdUnknown {
				reportProvider(c.Request.Context(), r, prov, false)
				log.Warn("upstream outcome unknown; bill left pending", "request_id", requestID, "err", err)
				abort(c, http.StatusBadGateway, "upstream outcome unknown")
				return
			}
			reverseWithBackground(b, log, requestID, rr.BillId, "upstream dial failed")
			abort(c, http.StatusBadGateway, "upstream error")
			return
		}
		defer upResp.Body.Close()
		if upResp.StatusCode >= 500 {
			upResp.Body.Close()
			reportProvider(c.Request.Context(), r, prov, false)
			reverseWithBackground(b, log, requestID, rr.BillId, "upstream status "+upResp.Status)
			abort(c, http.StatusBadGateway, "upstream rejected request")
			return
		}
		if upResp.StatusCode/100 != 2 {
			statusCode := upResp.StatusCode
			errBody := readLimitedBody(upResp.Body)
			reportProvider(c.Request.Context(), r, prov, true)
			reverseWithBackground(b, log, requestID, rr.BillId, "upstream status "+upResp.Status)
			writeUpstreamBody(c, statusCode, errBody)
			return
		}
		reportProvider(c.Request.Context(), r, prov, true)
		if !stream {
			payload, readErr := io.ReadAll(io.LimitReader(upResp.Body, 8<<20))
			if readErr != nil {
				log.Error("upstream response incomplete", "request_id", requestID, "err", readErr)
				abort(c, http.StatusBadGateway, "upstream response incomplete")
				return
			}
			if in, out, ok := nativeUsageFromJSON(payload); ok {
				settleAndLog(c, b, log, requestID, rr.BillId, in, out, int64(prov.ProviderID))
			} else if !holdUnknown {
				reverseWithBackground(b, log, requestID, rr.BillId, "upstream response lacks usage")
			} else {
				log.Warn("upstream response lacks usage; bill left pending", "request_id", requestID)
			}
			c.Data(upResp.StatusCode, "application/json", payload)
			return
		}

		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Status(http.StatusOK)
		flusher, _ := c.Writer.(http.Flusher)
		acc := nativeUsageAcc{}
		sc := bufio.NewScanner(upResp.Body)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Text()
			if _, err := c.Writer.Write([]byte(line + "\n")); err != nil {
				break
			}
			if flusher != nil {
				flusher.Flush()
			}
			acc.observeLine(line)
		}
		if err := sc.Err(); err != nil && !acc.ready() {
			log.Error("upstream response incomplete", "request_id", requestID, "err", err)
			return
		}
		if acc.ready() {
			settleAndLog(c, b, log, requestID, rr.BillId, acc.in, acc.out, int64(prov.ProviderID))
			return
		}
		if holdUnknown {
			log.Warn("stream lacks usage; bill left pending", "request_id", requestID)
			return
		}
		reverseWithBackground(b, log, requestID, rr.BillId, "stream lacks usage")
	}
}

func prepareNativeBody(body []byte) (prepared []byte, model, routeModel string, stream bool, maxTokens int64, err error) {
	var raw map[string]json.RawMessage
	if err = json.Unmarshal(body, &raw); err != nil || raw == nil {
		return nil, "", "", false, 0, err
	}
	model = jsonString(raw["model"])
	routeModel = canonicalModel(model)
	if routeModel == "" {
		return nil, "", "", false, 0, err
	}
	if _, ok := raw["stream"]; !ok {
		raw["stream"] = []byte("true")
	}
	stream = strings.TrimSpace(string(raw["stream"])) == "true"
	if n := jsonInt(raw["max_tokens"]); n > 0 {
		maxTokens = n
	} else if n := jsonInt(raw["max_output_tokens"]); n > 0 {
		maxTokens = n
	}
	prepared, err = json.Marshal(raw)
	return prepared, model, routeModel, stream, maxTokens, err
}

func canonicalModel(model string) string {
	model = strings.TrimSpace(model)
	if i := strings.LastIndex(model, "["); i > 0 && strings.HasSuffix(model, "]") {
		model = model[:i]
	}
	return model
}

func nativeUpstream(base, kind string, protocolBase ...string) string {
	override := ""
	if len(protocolBase) > 0 {
		override = protocolBase[0]
	}
	endpoint, err := upstream.NativeURL(base, override, kind)
	if err != nil {
		return ""
	}
	return endpoint
}

func jsonString(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

func jsonInt(raw json.RawMessage) int64 {
	if len(raw) == 0 {
		return 0
	}
	var n int64
	if json.Unmarshal(raw, &n) == nil {
		return n
	}
	var f float64
	if json.Unmarshal(raw, &f) == nil {
		return int64(f)
	}
	return 0
}

type nativeUsageAcc struct {
	in, out int64
	hasIn   bool
	hasOut  bool
}

func (a *nativeUsageAcc) ready() bool { return a.hasIn && a.hasOut }

func (a *nativeUsageAcc) observeLine(line string) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "data:") {
		return
	}
	payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
	if payload == "" || payload == "[DONE]" {
		return
	}
	a.observe(json.RawMessage(payload))
}

func (a *nativeUsageAcc) observe(payload json.RawMessage) {
	var frame map[string]json.RawMessage
	if json.Unmarshal(payload, &frame) != nil {
		return
	}
	a.takeUsage(frame["usage"])
	if msg, ok := frame["message"]; ok {
		var message map[string]json.RawMessage
		if json.Unmarshal(msg, &message) == nil {
			a.takeUsage(message["usage"])
		}
	}
	if resp, ok := frame["response"]; ok {
		var response map[string]json.RawMessage
		if json.Unmarshal(resp, &response) == nil {
			a.takeUsage(response["usage"])
		}
	}
}

func (a *nativeUsageAcc) takeUsage(raw json.RawMessage) {
	if len(raw) == 0 || string(raw) == "null" {
		return
	}
	var usage map[string]json.RawMessage
	if json.Unmarshal(raw, &usage) != nil {
		return
	}
	if n, ok := firstInt(usage, "input_tokens", "prompt_tokens"); ok && n >= 0 {
		a.in, a.hasIn = n, true
	}
	if n, ok := firstInt(usage, "output_tokens", "completion_tokens"); ok && n >= 0 {
		a.out, a.hasOut = n, true
	}
}

func firstInt(obj map[string]json.RawMessage, keys ...string) (int64, bool) {
	for _, key := range keys {
		if raw, ok := obj[key]; ok && len(raw) > 0 && string(raw) != "null" {
			return jsonInt(raw), true
		}
	}
	return 0, false
}

func nativeUsageFromJSON(payload []byte) (int64, int64, bool) {
	acc := nativeUsageAcc{}
	acc.observe(payload)
	if !acc.ready() {
		return 0, 0, false
	}
	return acc.in, acc.out, true
}
