// Package autoroute 实现 model:"auto" 路由：配置解析、state 提取、JEV 熔断与选档决策。
// 本文件只做环境变量配置层：默认值、解析与 fail-fast 校验（shadow/on 态），off 态仅校验 mode。
package autoroute

import (
	"fmt"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/frostloom/ai-gateway/internal/jev"
)

// 环境变量名（spec 配置表）。
const (
	EnvMode                     = "AUTO_ROUTE_MODE"
	EnvTiers                    = "AUTO_ROUTE_TIERS"
	EnvThreshold                = "AUTO_ROUTE_THRESHOLD"
	EnvFailMode                 = "AUTO_ROUTE_FAIL_MODE"
	EnvShadowServe              = "AUTO_ROUTE_SHADOW_SERVE"
	EnvDegradeStrongUnavailable = "AUTO_ROUTE_DEGRADE_STRONG_UNAVAILABLE"
	EnvUpgradeCheapUnavailable  = "AUTO_ROUTE_UPGRADE_CHEAP_UNAVAILABLE"
	EnvBreakerFailures          = "AUTO_ROUTE_BREAKER_FAILURES"
	EnvBreakerCooldownS         = "AUTO_ROUTE_BREAKER_COOLDOWN_S"
	EnvTimeoutMS                = "AUTO_ROUTE_TIMEOUT_MS"
	EnvMaxInputBytes            = "AUTO_ROUTE_MAX_INPUT_BYTES"
	EnvJEVEndpoint              = "JEV_ENDPOINT"
	EnvJEVAPIKey                = "JEV_API_KEY"
	EnvJEVModel                 = "JEV_MODEL"
)

// Mode auto 路由运行模式。
type Mode string

const (
	ModeOff    Mode = "off"    // auto 请求确定性 400
	ModeShadow Mode = "shadow" // 只判定记录，按固定档实际服务
	ModeOn     Mode = "on"     // 判定即改写
)

// 解析与校验边界。
const (
	maxThreshold     = 3
	maxTimeoutMS     = 60000
	maxMaxInputBytes = 1048576
)

// Config auto 路由配置（全部来自环境变量）。
type Config struct {
	Mode                     Mode
	CheapTier                string  // 便宜档模型名
	StrongTier               string  // 强档模型名
	Threshold                float64 // score ≥ 阈值选强档
	FailMode                 string  // JEV 失败兜底档：cheap/strong
	ShadowServe              string  // shadow 模式实际服务档位
	TimeoutMS                int     // 单次判定预算
	MaxInputBytes            int     // 发给 JEV 的 state 上限
	BreakerFailures          int     // 连续失败 N 次进熔断
	BreakerCooldownS         int     // 熔断冷却秒数
	DegradeStrongUnavailable bool    // 强档无 provider 时自动降便宜档
	UpgradeCheapUnavailable  bool    // 便宜档无 provider 时自动升强档
	JEVEndpoint              string
	JEVAPIKey                string
	JEVModel                 string
}

// defaultConfig 全部默认值（spec 配置表）。
func defaultConfig() Config {
	return Config{
		Mode:                     ModeOff,
		CheapTier:                "deepseek-v4-flash",
		StrongTier:               "deepseek-v4-pro",
		Threshold:                0.8, // L2b 异构基准最优一致率（90%）；2026-11-17 依已批准的阈值回调改自 1.5
		FailMode:                 "cheap",
		ShadowServe:              "deepseek-v4-flash",
		TimeoutMS:                2000,
		MaxInputBytes:            8192,
		BreakerFailures:          3,
		BreakerCooldownS:         30,
		DegradeStrongUnavailable: true,
		UpgradeCheapUnavailable:  false,
		JEVEndpoint:              jev.DefaultEndpoint,
		JEVModel:                 jev.DefaultModel,
	}
}

// ConfigFromEnv 从环境变量解析配置。getenv 为 nil 时用 os.Getenv。
//
// off 态只校验 mode 本身（其余配置不解析不校验，保持默认）；
// shadow/on 态全部配置 fail-fast：任何非法值（含缺 JEV_API_KEY、端点形状非法）直接报错。
func ConfigFromEnv(getenv func(string) string) (Config, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	cfg := defaultConfig()

	rawMode := getenv(EnvMode)
	switch Mode(rawMode) {
	case "":
		// 未设置 → 默认 off
	case ModeOff, ModeShadow, ModeOn:
		cfg.Mode = Mode(rawMode)
	default:
		return Config{}, fmt.Errorf("autoroute: 未知 %s %q（仅支持 off/shadow/on）", EnvMode, rawMode)
	}
	if cfg.Mode == ModeOff {
		return cfg, nil
	}

	// ---- 以下仅 shadow/on：全量解析 + fail-fast ----

	cheap, strong, err := parseTiers(getenv(EnvTiers), cfg.CheapTier, cfg.StrongTier)
	if err != nil {
		return Config{}, err
	}
	cfg.CheapTier, cfg.StrongTier = cheap, strong

	if raw := getenv(EnvThreshold); raw != "" {
		v, err := strconv.ParseFloat(raw, 64)
		// NaN 能通过 ParseFloat 且不落进任何区间比较，必须显式拒绝
		//（否则静默路由全走便宜档）；±Inf 已被范围检查覆盖。
		if err != nil || math.IsNaN(v) || v <= 0 || v > maxThreshold {
			return Config{}, fmt.Errorf("autoroute: %s 需为 (0,%d] 的数字: %q", EnvThreshold, maxThreshold, raw)
		}
		cfg.Threshold = v
	}

	if raw := getenv(EnvFailMode); raw != "" {
		if raw != "cheap" && raw != "strong" {
			return Config{}, fmt.Errorf("autoroute: %s 仅支持 cheap/strong: %q", EnvFailMode, raw)
		}
		cfg.FailMode = raw
	}

	if raw := getenv(EnvShadowServe); raw != "" {
		if raw != cfg.CheapTier && raw != cfg.StrongTier {
			return Config{}, fmt.Errorf("autoroute: %s 需为两档之一 (%s/%s): %q", EnvShadowServe, cfg.CheapTier, cfg.StrongTier, raw)
		}
		cfg.ShadowServe = raw
	} else {
		cfg.ShadowServe = cfg.CheapTier
	}

	n, err := parseIntGE(getenv, EnvTimeoutMS, 1, cfg.TimeoutMS, maxTimeoutMS)
	if err != nil {
		return Config{}, err
	}
	cfg.TimeoutMS = n

	n, err = parseIntGE(getenv, EnvMaxInputBytes, 1, cfg.MaxInputBytes, maxMaxInputBytes)
	if err != nil {
		return Config{}, err
	}
	cfg.MaxInputBytes = n

	n, err = parseIntGE(getenv, EnvBreakerFailures, 1, cfg.BreakerFailures, 0)
	if err != nil {
		return Config{}, err
	}
	cfg.BreakerFailures = n

	n, err = parseIntGE(getenv, EnvBreakerCooldownS, 1, cfg.BreakerCooldownS, 0)
	if err != nil {
		return Config{}, err
	}
	cfg.BreakerCooldownS = n

	b, err := parseBoolFlag(getenv, EnvDegradeStrongUnavailable, cfg.DegradeStrongUnavailable)
	if err != nil {
		return Config{}, err
	}
	cfg.DegradeStrongUnavailable = b

	b, err = parseBoolFlag(getenv, EnvUpgradeCheapUnavailable, cfg.UpgradeCheapUnavailable)
	if err != nil {
		return Config{}, err
	}
	cfg.UpgradeCheapUnavailable = b

	if raw := getenv(EnvJEVEndpoint); raw != "" {
		cfg.JEVEndpoint = raw
	}
	cfg.JEVAPIKey = getenv(EnvJEVAPIKey)
	if raw := getenv(EnvJEVModel); raw != "" {
		cfg.JEVModel = raw
	}

	// 端点形状 / key / model 非空校验复用 jev.NewEvaluationClient 的规则
	//（https 任意主机、http 仅 loopback、禁 userinfo/query/fragment）。
	if _, err := jev.NewEvaluationClient(cfg.JEVEndpoint, cfg.JEVAPIKey, cfg.JEVModel, http.DefaultTransport); err != nil {
		return Config{}, fmt.Errorf("autoroute: JEV 配置校验失败: %w", err)
	}
	return cfg, nil
}

// parseTiers 解析「便宜,强」两档；未设置时用默认值。
func parseTiers(raw, defCheap, defStrong string) (string, string, error) {
	if raw == "" {
		return defCheap, defStrong, nil
	}
	parts := strings.Split(raw, ",")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("autoroute: %s 需为「便宜,强」两档: %q", EnvTiers, raw)
	}
	cheap, strong := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	if cheap == "" || strong == "" {
		return "", "", fmt.Errorf("autoroute: %s 需为「便宜,强」两档: %q", EnvTiers, raw)
	}
	if cheap == strong {
		return "", "", fmt.Errorf("autoroute: %s 两档不能相同: %q", EnvTiers, raw)
	}
	return cheap, strong, nil
}

// parseIntGE 解析整数环境变量：≥ min；max > 0 时还需 ≤ max。
func parseIntGE(getenv func(string) string, key string, min, def, max int) (int, error) {
	raw := getenv(key)
	if raw == "" {
		return def, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("autoroute: %s 不是合法整数: %q", key, raw)
	}
	if v < min || (max > 0 && v > max) {
		if max > 0 {
			return 0, fmt.Errorf("autoroute: %s 需在 [%d,%d]: %q", key, min, max, raw)
		}
		return 0, fmt.Errorf("autoroute: %s 需 ≥%d: %q", key, min, raw)
	}
	return v, nil
}

// parseBoolFlag 解析 0/1 开关环境变量（其余值报错）。
func parseBoolFlag(getenv func(string) string, key string, def bool) (bool, error) {
	raw := getenv(key)
	if raw == "" {
		return def, nil
	}
	switch raw {
	case "0":
		return false, nil
	case "1":
		return true, nil
	}
	return def, fmt.Errorf("autoroute: %s 仅接受 0/1: %q", key, raw)
}

// FallbackTier JEV 判定不可用时的兜底档位：FailMode=="strong" 取强档，否则便宜档。
func (c Config) FallbackTier() string {
	if c.FailMode == "strong" {
		return c.StrongTier
	}
	return c.CheapTier
}
