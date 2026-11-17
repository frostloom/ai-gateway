// autoroute body 改写测试：model 字段替换、其余键逐字节保留（嵌套对象/数组/数字精度）、
// 特殊字符转义与无效 JSON/非对象/缺 model 拒绝。
package autoroute

import (
	"bytes"
	"encoding/json"
	"testing"
)

// rawKeys 解析 JSON 对象为键→原始字节映射，失败时终止测试。
func rawKeys(t *testing.T, body []byte) map[string]json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("解析 JSON 对象失败: %v", err)
	}
	return m
}

func TestRewriteModelReplacesModelAndPreservesOtherKeys(t *testing.T) {
	// temperature 为超精度数字、nums 含大整数与指数写法，验证原始字节不被重排。
	original := []byte(`{"model":"deepseek-v4-flash","stream":true,` +
		`"temperature":0.30000000000000001,"nums":[12345678901234567890,1e-7],` +
		`"options":{"max_tokens":128,"stop":["\n","User:"]},` +
		`"messages":[{"role":"user","content":"你好"}]}`)
	const want = "deepseek-v4-pro"
	out, err := RewriteModel(original, want)
	if err != nil {
		t.Fatalf("RewriteModel 失败: %v", err)
	}
	src, dst := rawKeys(t, original), rawKeys(t, out)
	if len(dst) != len(src) {
		t.Fatalf("键数量变化: 原 %d 个，改写后 %d 个", len(src), len(dst))
	}
	for key, wantRaw := range src {
		if key == "model" {
			continue
		}
		if got := dst[key]; !bytes.Equal(got, wantRaw) {
			t.Fatalf("键 %q 字节被改动: 原 %s，改写后 %s", key, wantRaw, got)
		}
	}
	var gotModel string
	if err := json.Unmarshal(dst["model"], &gotModel); err != nil {
		t.Fatalf("改写后 model 不是字符串: %v", err)
	}
	if gotModel != want {
		t.Fatalf("model 未改写: got %q", gotModel)
	}
}

func TestRewriteModelEscapesSpecialCharacters(t *testing.T) {
	original := []byte(`{"model":"m","x":1}`)
	want := "deep\"seek\\v4\n\t中文"
	out, err := RewriteModel(original, want)
	if err != nil {
		t.Fatalf("RewriteModel 失败: %v", err)
	}
	var got string
	if err := json.Unmarshal(rawKeys(t, out)["model"], &got); err != nil {
		t.Fatalf("改写后 model 不是字符串: %v", err)
	}
	if got != want {
		t.Fatalf("特殊字符未正确转义/还原: got %q", got)
	}
}

func TestRewriteModelReplacesNonStringModelValue(t *testing.T) {
	original := []byte(`{"model":123,"k":"v"}`)
	out, err := RewriteModel(original, "new")
	if err != nil {
		t.Fatalf("RewriteModel 失败: %v", err)
	}
	dst := rawKeys(t, out)
	var got string
	if err := json.Unmarshal(dst["model"], &got); err != nil {
		t.Fatalf("改写后 model 不是字符串: %v", err)
	}
	if got != "new" || !bytes.Equal(dst["k"], json.RawMessage(`"v"`)) {
		t.Fatalf("非字符串 model 未替换或其余键被改动: %s", out)
	}
}

func TestRewriteModelRejects(t *testing.T) {
	cases := []struct {
		name string
		body []byte
	}{
		{"无效 JSON", []byte(`{"model":"m",`)},
		{"空 body", nil},
		{"顶层数组", []byte(`["m"]`)},
		{"顶层字符串", []byte(`"m"`)},
		{"顶层 null", []byte(`null`)},
		{"缺 model 键", []byte(`{"messages":[]}`)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if out, err := RewriteModel(c.body, "x"); err == nil {
				t.Fatalf("期望报错，得到输出 %s", out)
			}
		})
	}
}
