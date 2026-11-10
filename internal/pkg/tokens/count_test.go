package tokens

import "testing"

func TestCountUsesNewAPIWeights(t *testing.T) {
	if Count("deepseek-flash", "") != 0 {
		t.Fatal("empty")
	}
	// unknown：一个英文词 1.02，向上取整为 2。两个汉字 1.70，向上取整为 2。
	if got := Count("deepseek-flash", "hi"); got != 2 {
		t.Fatalf("hi = %d", got)
	}
	if got := Count("deepseek-flash", "你好"); got != 2 {
		t.Fatalf("你好 = %d", got)
	}
	// user + 换行 + 你好：1.02 + 0.50 + 1.70 = 3.22，向上取整为 4。
	if got := Count("deepseek-flash", "user\n你好"); got != 4 {
		t.Fatalf("message = %d", got)
	}
	if Count("deepseek-v4", "你好") != Count("deepseek-flash", "你好") {
		t.Fatal("deepseek family drifted")
	}
	if Count("gemini-3.7-flash", "你好") == Count("deepseek-flash", "你好") && Count("claude-opus-4-7", "你好") == Count("deepseek-flash", "你好") {
		t.Fatal("families collapsed")
	}
}
