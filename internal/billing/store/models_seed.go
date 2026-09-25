// 商品目录种子：36 个国内主流模型 SKU（2026-05/06 价格快照，admin 可改，不自动同步官网）。
// 这是目录的单一来源：seed 脚本（SeedModels）与测试共用；schema.sql 只建表 DDL，数据由此填充。
//
// 价格单位：分/百万 token（¥1 = 100 分）。免费模型（glm-4-flash/spark-lite/...）价格 = 0，
// 调它们不扣费（但同样走预占/结算，bill 记录 token 用量）。
package store

import (
	"encoding/json"
)

// DefaultModels 36 SKU 目录（单一来源，seed 与测试共用）。
func DefaultModels() []Model {
	raw := []struct {
		vendor, modelID, name string
		in, out               int64 // 分/百万 token
		ctx                   int
		tags                  []string
	}{
		// ---- DeepSeek ----
		{"DeepSeek", "deepseek-v4-flash", "DeepSeek V4 Flash", 100, 200, 1_000_000, []string{"文本", "轻量"}},
		{"DeepSeek", "deepseek-v4-flash-thinking", "DeepSeek V4 Flash Thinking", 100, 400, 128_000, []string{"推理"}},
		{"DeepSeek", "deepseek-v4-pro", "DeepSeek V4 Pro", 300, 600, 128_000, []string{"旗舰"}},
		{"DeepSeek", "deepseek-v4-pro-thinking", "DeepSeek V4 Pro Thinking", 300, 1_200, 128_000, []string{"推理", "旗舰"}},
		// ---- 通义千问 ----
		{"通义千问", "qwen3-max", "通义千问 Qwen3 Max", 250, 1_000, 256_000, []string{"旗舰"}},
		{"通义千问", "qwen3-plus", "通义千问 Qwen3 Plus", 80, 480, 128_000, []string{"文本"}},
		{"通义千问", "qwen3-turbo", "通义千问 Qwen3 Turbo", 30, 60, 128_000, []string{"轻量"}},
		{"通义千问", "qwen3-long", "通义千问 Qwen3 Long", 50, 200, 1_000_000, []string{"长文本"}},
		{"通义千问", "qwen3-vl-max", "通义千问 Qwen3 VL Max", 1_900, 1_900, 32_000, []string{"视觉"}},
		{"通义千问", "qwen-max", "通义千问 Qwen Max", 240, 960, 32_000, []string{"文本"}},
		{"通义千问", "qwen-plus", "通义千问 Qwen Plus", 80, 200, 128_000, []string{"文本"}},
		{"通义千问", "qwen-turbo", "通义千问 Qwen Turbo", 30, 60, 1_000_000, []string{"轻量"}},
		// ---- 智谱 ----
		{"智谱", "glm-4-plus", "智谱 GLM-4 Plus", 500, 500, 128_000, []string{"旗舰"}},
		{"智谱", "glm-4-air", "智谱 GLM-4 Air", 60, 60, 128_000, []string{"轻量"}},
		{"智谱", "glm-4-flash", "智谱 GLM-4 Flash", 0, 0, 128_000, []string{"免费"}},
		{"智谱", "glm-4v-plus", "智谱 GLM-4V Plus", 1_000, 1_000, 128_000, []string{"视觉"}},
		// ---- Kimi ----
		{"Kimi", "kimi-k2", "Kimi K2", 400, 2_000, 128_000, []string{"文本"}},
		{"Kimi", "kimi-k2-thinking", "Kimi K2 Thinking", 400, 4_000, 128_000, []string{"推理"}},
		{"Kimi", "kimi-k2.6", "Kimi K2.6", 650, 2_700, 128_000, []string{"旗舰"}},
		{"Kimi", "kimi-k2-long", "Kimi K2 Long", 400, 2_000, 1_000_000, []string{"长文本"}},
		// ---- 豆包 ----
		{"豆包", "doubao-seed-2.1-pro", "豆包 Seed 2.1 Pro", 600, 3_000, 256_000, []string{"旗舰"}},
		{"豆包", "doubao-seed-2.1-turbo", "豆包 Seed 2.1 Turbo", 300, 1_500, 256_000, []string{"文本"}},
		{"豆包", "doubao-seed-2.0-lite", "豆包 Seed 2.0 Lite", 60, 366, 256_000, []string{"轻量"}},
		// ---- MiniMax ----
		{"MiniMax", "minimax-m3", "MiniMax M3", 440, 1_760, 256_000, []string{"旗舰"}},
		{"MiniMax", "minimax-m2.7", "MiniMax M2.7", 210, 840, 256_000, []string{"文本"}},
		// ---- 讯飞星火 ----
		{"讯飞星火", "spark-4.0-max", "讯飞星火 4.0 Max", 3_000, 3_000, 128_000, []string{"旗舰"}},
		{"讯飞星火", "spark-lite", "讯飞星火 Lite", 0, 0, 128_000, []string{"免费"}},
		// ---- 百度文心 ----
		{"百度文心", "ernie-5.1", "百度文心 ERNIE 5.1", 800, 2_400, 128_000, []string{"旗舰"}},
		{"百度文心", "ernie-speed-128k", "百度文心 ERNIE Speed 128K", 0, 0, 128_000, []string{"免费"}},
		{"百度文心", "ernie-speed-8k", "百度文心 ERNIE Speed 8K", 0, 0, 8_000, []string{"免费"}},
		// ---- 腾讯混元 ----
		{"腾讯混元", "hunyuan-turbo-s", "腾讯混元 Turbo S", 80, 200, 256_000, []string{"文本"}},
		{"腾讯混元", "hunyuan-t1", "腾讯混元 T1", 100, 400, 256_000, []string{"推理"}},
		{"腾讯混元", "hunyuan-lite", "腾讯混元 Lite", 0, 0, 256_000, []string{"免费"}},
		// ---- 阶跃星辰 ----
		{"阶跃星辰", "step-2", "阶跃星辰 Step-2", 3_800, 7_600, 16_000, []string{"旗舰"}},
		{"阶跃星辰", "step-1.5v", "阶跃星辰 Step-1.5V", 1_900, 3_800, 16_000, []string{"视觉"}},
		// ---- 智谱（追加） ----
		{"智谱", "glm-4.7-flash", "智谱 GLM-4.7 Flash", 0, 0, 200_000, []string{"免费"}},
	}
	models := make([]Model, 0, len(raw))
	for _, r := range raw {
		tags, _ := json.Marshal(r.tags)
		models = append(models, Model{
			Vendor:          r.vendor,
			ModelID:         r.modelID,
			Name:            r.name,
			InputPriceCent:  r.in,
			OutputPriceCent: r.out,
			ContextLen:      r.ctx,
			Tags:            string(tags),
		})
	}
	return models
}
