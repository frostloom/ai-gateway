// autoroute 配置解析测试：空环境默认值、off 态宽松、shadow/on 态 fail-fast 校验、布尔解析与兜底档位。
package autoroute

import (
	"testing"

	"github.com/frostloom/ai-gateway/internal/jev"
)

// envLookup 把 map 包装成 getenv；缺失键返回空串，模拟未设置的环境变量。
func envLookup(env map[string]string) func(string) string {
	return func(key string) string { return env[key] }
}

func TestConfigFromEnvDefaults(t *testing.T) {
	cfg, err := ConfigFromEnv(envLookup(map[string]string{}))
	if err != nil {
		t.Fatalf("空环境不应报错: %v", err)
	}
	if cfg.Mode != ModeOff {
		t.Errorf("Mode = %q, 期望 %q", cfg.Mode, ModeOff)
	}
	if cfg.CheapTier != "deepseek-v4-flash" {
		t.Errorf("CheapTier = %q, 期望 deepseek-v4-flash", cfg.CheapTier)
	}
	if cfg.StrongTier != "deepseek-v4-pro" {
		t.Errorf("StrongTier = %q, 期望 deepseek-v4-pro", cfg.StrongTier)
	}
	if cfg.Threshold != 0.8 {
		t.Errorf("Threshold = %v, 期望 0.8", cfg.Threshold)
	}
	if cfg.FailMode != "cheap" {
		t.Errorf("FailMode = %q, 期望 cheap", cfg.FailMode)
	}
	if cfg.TimeoutMS != 2000 {
		t.Errorf("TimeoutMS = %d, 期望 2000", cfg.TimeoutMS)
	}
	if cfg.MaxInputBytes != 8192 {
		t.Errorf("MaxInputBytes = %d, 期望 8192", cfg.MaxInputBytes)
	}
	if cfg.BreakerFailures != 3 {
		t.Errorf("BreakerFailures = %d, 期望 3", cfg.BreakerFailures)
	}
	if cfg.BreakerCooldownS != 30 {
		t.Errorf("BreakerCooldownS = %d, 期望 30", cfg.BreakerCooldownS)
	}
	if !cfg.DegradeStrongUnavailable {
		t.Errorf("DegradeStrongUnavailable = false, 期望 true")
	}
	if cfg.UpgradeCheapUnavailable {
		t.Errorf("UpgradeCheapUnavailable = true, 期望 false")
	}
	if cfg.ShadowServe != cfg.CheapTier {
		t.Errorf("ShadowServe = %q, 期望便宜档 %q", cfg.ShadowServe, cfg.CheapTier)
	}
	if cfg.JEVModel != "jev-latest" {
		t.Errorf("JEVModel = %q, 期望 jev-latest", cfg.JEVModel)
	}
	if cfg.JEVEndpoint != jev.DefaultEndpoint {
		t.Errorf("JEVEndpoint = %q, 期望 %q", cfg.JEVEndpoint, jev.DefaultEndpoint)
	}
	if cfg.FallbackTier() != cfg.CheapTier {
		t.Errorf("默认 FailMode=cheap 时 FallbackTier 应为便宜档, 得到 %q", cfg.FallbackTier())
	}
}

func TestConfigFromEnvOffIgnoresOtherValues(t *testing.T) {
	env := map[string]string{
		"AUTO_ROUTE_MODE": "off",
		// 其余全部非法：off 态不解析、不校验、不要求 JEV_API_KEY
		"AUTO_ROUTE_TIERS":           "only-one",
		"AUTO_ROUTE_THRESHOLD":       "not-a-number",
		"AUTO_ROUTE_FAIL_MODE":       "sideways",
		"AUTO_ROUTE_TIMEOUT_MS":      "0",
		"AUTO_ROUTE_MAX_INPUT_BYTES": "999999999",
		"JEV_ENDPOINT":               "https://user:pass@api.typesafe.ai/v1/systemone",
		// JEV_API_KEY 故意缺失
	}
	cfg, err := ConfigFromEnv(envLookup(env))
	if err != nil {
		t.Fatalf("off 态其余非法值不应报错: %v", err)
	}
	if cfg.Mode != ModeOff {
		t.Errorf("Mode = %q, 期望 %q", cfg.Mode, ModeOff)
	}
}

func TestConfigFromEnvShadowOnValidation(t *testing.T) {
	// env 为 nil 表示移除 JEV_API_KEY（缺 key 用例），其余为在 mode+key 基础上的覆盖。
	cases := []struct {
		name string
		env  map[string]string
	}{
		{"缺JEV_API_KEY", nil},
		{"TIERS只有一档", map[string]string{"AUTO_ROUTE_TIERS": "deepseek-v4-flash"}},
		{"TIERS两档相同", map[string]string{"AUTO_ROUTE_TIERS": "same,same"}},
		{"TIERS三段", map[string]string{"AUTO_ROUTE_TIERS": "a,b,c"}},
		{"THRESHOLD非数字", map[string]string{"AUTO_ROUTE_THRESHOLD": "abc"}},
		{"THRESHOLD等于零", map[string]string{"AUTO_ROUTE_THRESHOLD": "0"}},
		{"THRESHOLD负数", map[string]string{"AUTO_ROUTE_THRESHOLD": "-0.5"}},
		{"THRESHOLD超过3", map[string]string{"AUTO_ROUTE_THRESHOLD": "3.01"}},
		{"THRESHOLD是NaN", map[string]string{"AUTO_ROUTE_THRESHOLD": "NaN"}}, // ParseFloat 成功且两个区间比较都放行，必须显式拒绝
		{"FAIL_MODE非法", map[string]string{"AUTO_ROUTE_FAIL_MODE": "sideways"}},
		{"SHADOW_SERVE不在两档", map[string]string{"AUTO_ROUTE_SHADOW_SERVE": "other-model"}},
		{"TIMEOUT_MS等于零", map[string]string{"AUTO_ROUTE_TIMEOUT_MS": "0"}},
		{"TIMEOUT_MS超上限", map[string]string{"AUTO_ROUTE_TIMEOUT_MS": "60001"}},
		{"TIMEOUT_MS非数字", map[string]string{"AUTO_ROUTE_TIMEOUT_MS": "2s"}},
		{"MAX_INPUT_BYTES等于零", map[string]string{"AUTO_ROUTE_MAX_INPUT_BYTES": "0"}},
		{"MAX_INPUT_BYTES超上限", map[string]string{"AUTO_ROUTE_MAX_INPUT_BYTES": "1048577"}},
		{"BREAKER_FAILURES低于1", map[string]string{"AUTO_ROUTE_BREAKER_FAILURES": "0"}},
		{"BREAKER_COOLDOWN_S低于1", map[string]string{"AUTO_ROUTE_BREAKER_COOLDOWN_S": "0"}},
		{"JEV_ENDPOINT带userinfo", map[string]string{"JEV_ENDPOINT": "https://user:pass@api.typesafe.ai/v1/systemone"}},
		{"DEGRADE值非法", map[string]string{"AUTO_ROUTE_DEGRADE_STRONG_UNAVAILABLE": "yes"}},
	}
	for _, mode := range []Mode{ModeShadow, ModeOn} {
		for _, tc := range cases {
			env := map[string]string{
				"AUTO_ROUTE_MODE": string(mode),
				"JEV_API_KEY":     "test-key",
			}
			for k, v := range tc.env {
				env[k] = v
			}
			if tc.env == nil {
				delete(env, "JEV_API_KEY")
			}
			t.Run(string(mode)+"/"+tc.name, func(t *testing.T) {
				if _, err := ConfigFromEnv(envLookup(env)); err == nil {
					t.Fatalf("%s 态下 %s 应报错", mode, tc.name)
				}
			})
		}
	}
}

func TestConfigFromEnvUnknownMode(t *testing.T) {
	extras := []map[string]string{
		nil, // 干净环境
		{"AUTO_ROUTE_TIERS": "only-one", "AUTO_ROUTE_THRESHOLD": "abc"}, // 其余非法仍以 mode 错误报出
	}
	for _, extra := range extras {
		env := map[string]string{"AUTO_ROUTE_MODE": "bogus"}
		for k, v := range extra {
			env[k] = v
		}
		if _, err := ConfigFromEnv(envLookup(env)); err == nil {
			t.Fatalf("未知 AUTO_ROUTE_MODE=%q 应报错（含 off 态）", env["AUTO_ROUTE_MODE"])
		}
	}
}

func TestConfigFromEnvBoolFlags(t *testing.T) {
	base := func(extra map[string]string) map[string]string {
		env := map[string]string{"AUTO_ROUTE_MODE": "shadow", "JEV_API_KEY": "test-key"}
		for k, v := range extra {
			env[k] = v
		}
		return env
	}
	cases := []struct {
		name        string
		env         map[string]string
		wantDegrade bool
		wantUpgrade bool
	}{
		{"degrade=1,upgrade=0", base(map[string]string{
			"AUTO_ROUTE_DEGRADE_STRONG_UNAVAILABLE": "1",
			"AUTO_ROUTE_UPGRADE_CHEAP_UNAVAILABLE":  "0",
		}), true, false},
		{"degrade=0,upgrade=1", base(map[string]string{
			"AUTO_ROUTE_DEGRADE_STRONG_UNAVAILABLE": "0",
			"AUTO_ROUTE_UPGRADE_CHEAP_UNAVAILABLE":  "1",
		}), false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := ConfigFromEnv(envLookup(tc.env))
			if err != nil {
				t.Fatalf("不应报错: %v", err)
			}
			if cfg.DegradeStrongUnavailable != tc.wantDegrade {
				t.Errorf("DegradeStrongUnavailable = %v, 期望 %v", cfg.DegradeStrongUnavailable, tc.wantDegrade)
			}
			if cfg.UpgradeCheapUnavailable != tc.wantUpgrade {
				t.Errorf("UpgradeCheapUnavailable = %v, 期望 %v", cfg.UpgradeCheapUnavailable, tc.wantUpgrade)
			}
		})
	}
}

func TestConfigFromEnvCustom(t *testing.T) {
	env := map[string]string{
		"AUTO_ROUTE_MODE":                       "on",
		"AUTO_ROUTE_TIERS":                      "cheap-a,strong-b",
		"AUTO_ROUTE_THRESHOLD":                  "3", // 上边界合法（>3 才拒）
		"AUTO_ROUTE_FAIL_MODE":                  "strong",
		"AUTO_ROUTE_SHADOW_SERVE":               "strong-b",
		"AUTO_ROUTE_TIMEOUT_MS":                 "60000",
		"AUTO_ROUTE_MAX_INPUT_BYTES":            "1048576",
		"AUTO_ROUTE_BREAKER_FAILURES":           "1",
		"AUTO_ROUTE_BREAKER_COOLDOWN_S":         "1",
		"AUTO_ROUTE_DEGRADE_STRONG_UNAVAILABLE": "0",
		"AUTO_ROUTE_UPGRADE_CHEAP_UNAVAILABLE":  "1",
		"JEV_API_KEY":                           "test-key",
		"JEV_ENDPOINT":                          "http://127.0.0.1:9999/v1/systemone", // http 仅 loopback 合法
		"JEV_MODEL":                             "jev-pinned",
	}
	cfg, err := ConfigFromEnv(envLookup(env))
	if err != nil {
		t.Fatalf("全量合法配置不应报错: %v", err)
	}
	if cfg.Mode != ModeOn || cfg.CheapTier != "cheap-a" || cfg.StrongTier != "strong-b" {
		t.Errorf("mode/档位不符: %q %q %q", cfg.Mode, cfg.CheapTier, cfg.StrongTier)
	}
	if cfg.Threshold != 3 || cfg.FailMode != "strong" || cfg.ShadowServe != "strong-b" {
		t.Errorf("threshold/failmode/shadowserve 不符: %v %q %q", cfg.Threshold, cfg.FailMode, cfg.ShadowServe)
	}
	if cfg.TimeoutMS != 60000 || cfg.MaxInputBytes != 1048576 {
		t.Errorf("timeout/maxinput 不符: %d %d", cfg.TimeoutMS, cfg.MaxInputBytes)
	}
	if cfg.BreakerFailures != 1 || cfg.BreakerCooldownS != 1 {
		t.Errorf("breaker 参数不符: %d %d", cfg.BreakerFailures, cfg.BreakerCooldownS)
	}
	if cfg.DegradeStrongUnavailable || !cfg.UpgradeCheapUnavailable {
		t.Errorf("开关不符: degrade=%v upgrade=%v", cfg.DegradeStrongUnavailable, cfg.UpgradeCheapUnavailable)
	}
	if cfg.JEVEndpoint != "http://127.0.0.1:9999/v1/systemone" || cfg.JEVAPIKey != "test-key" || cfg.JEVModel != "jev-pinned" {
		t.Errorf("JEV 配置不符: %q %q %q", cfg.JEVEndpoint, cfg.JEVAPIKey, cfg.JEVModel)
	}
	if cfg.FallbackTier() != "strong-b" {
		t.Errorf("FailMode=strong 时 FallbackTier 应为强档, 得到 %q", cfg.FallbackTier())
	}
}

// 未设置 SHADOW_SERVE 时默认取自定义后的便宜档。
func TestConfigFromEnvShadowServeDefaultsToCheap(t *testing.T) {
	env := map[string]string{
		"AUTO_ROUTE_MODE":  "shadow",
		"AUTO_ROUTE_TIERS": "cheap-a,strong-b",
		"JEV_API_KEY":      "test-key",
	}
	cfg, err := ConfigFromEnv(envLookup(env))
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if cfg.ShadowServe != "cheap-a" {
		t.Errorf("ShadowServe = %q, 期望便宜档 cheap-a", cfg.ShadowServe)
	}
}

func TestFallbackTier(t *testing.T) {
	cfg := Config{CheapTier: "c", StrongTier: "s"}
	for _, tc := range []struct {
		failMode string
		want     string
	}{
		{"cheap", "c"},
		{"strong", "s"},
		{"other", "c"}, // 非 strong 一律便宜档
	} {
		cfg.FailMode = tc.failMode
		if got := cfg.FallbackTier(); got != tc.want {
			t.Errorf("FailMode=%q: FallbackTier = %q, 期望 %q", tc.failMode, got, tc.want)
		}
	}
}
