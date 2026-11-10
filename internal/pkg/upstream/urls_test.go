package upstream

import "testing"

func TestChatAndModelsPreserveAPIPrefix(t *testing.T) {
	for _, tc := range []struct{ base, chat, models string }{
		{"https://api.example.com", "https://api.example.com/v1/chat/completions", "https://api.example.com/v1/models"},
		{"https://api.example.com/v1/", "https://api.example.com/v1/chat/completions", "https://api.example.com/v1/models"},
		{"https://open.bigmodel.cn/api/coding/paas/v4", "https://open.bigmodel.cn/api/coding/paas/v4/chat/completions", "https://open.bigmodel.cn/api/coding/paas/v4/models"},
		{"https://api.example.com/custom/v4/chat/completions", "https://api.example.com/custom/v4/chat/completions", "https://api.example.com/custom/v4/models"},
	} {
		chat, e := ChatURL(tc.base)
		if e != nil || chat != tc.chat {
			t.Fatalf("%s chat=%s err=%v", tc.base, chat, e)
		}
		models, e := ModelsURL(tc.base)
		if e != nil || models != tc.models {
			t.Fatalf("%s models=%s err=%v", tc.base, models, e)
		}
	}
	for _, bad := range []string{"", "file:///etc/passwd", "https://user:secret@example.com", "https://example.com?key=secret", "https://example.com#fragment"} {
		if _, err := ChatURL(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestNativePathsAndExplicitProtocolBases(t *testing.T) {
	for _, tc := range []struct{ base, override, kind, want string }{
		{"https://api.deepseek.com", "", "messages", "https://api.deepseek.com/anthropic/v1/messages"},
		{"https://api.deepseek.com/v1", "", "responses", "https://api.deepseek.com/responses"},
		{"https://open.bigmodel.cn/api/coding/paas/v4", "", "messages", "https://open.bigmodel.cn/api/anthropic/v1/messages"},
		{"https://open.bigmodel.cn/api/paas/v4", "", "messages", "https://open.bigmodel.cn/api/anthropic/v1/messages"},
		{"https://api.anthropic.com", "", "messages", "https://api.anthropic.com/v1/messages"},
		{"https://gateway.example/prefix/v1", "", "messages", "https://gateway.example/prefix/v1/messages"},
		{"https://api.openai.com/v1", "", "responses", "https://api.openai.com/v1/responses"},
		{"https://api.openai.com", "", "responses", "https://api.openai.com/v1/responses"},
		{"https://chat.example/v1", "https://native.example/custom/anthropic", "messages", "https://native.example/custom/anthropic/v1/messages"},
		{"https://chat.example/v1", "https://native.example/custom/v1/messages", "messages", "https://native.example/custom/v1/messages"},
		{"https://chat.example/v1", "https://native.example/api/v1", "responses", "https://native.example/api/v1/responses"},
	} {
		got, err := NativeURL(tc.base, tc.override, tc.kind)
		if err != nil || got != tc.want {
			t.Errorf("%+v got=%s err=%v", tc, got, err)
		}
	}
}
