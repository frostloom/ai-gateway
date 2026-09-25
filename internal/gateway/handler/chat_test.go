package handler

import "testing"

func TestParseUsageFrame(t *testing.T) {
	cases := []struct {
		name string
		line string
		want *usage // nil 表示不应解析出 usage
	}{
		{
			name: "终帧带 usage（choices 空）",
			line: `data: {"id":"x","choices":[],"usage":{"prompt_tokens":7,"completion_tokens":100,"total_tokens":107}}`,
			want: &usage{PromptTokens: 7, CompletionTokens: 100},
		},
		{
			name: "内容增量帧（带 choices，无 usage）",
			line: `data: {"id":"x","choices":[{"index":0,"delta":{"content":"你好"}}]}`,
			want: nil,
		},
		{
			name: "role 帧",
			line: `data: {"id":"x","choices":[{"index":0,"delta":{"role":"assistant"}}]}`,
			want: nil,
		},
		{
			name: "DONE 哨兵",
			line: "data: [DONE]",
			want: nil,
		},
		{
			name: "非 data 行（空行分隔）",
			line: "",
			want: nil,
		},
		{
			name: "坏 JSON",
			line: "data: {not json",
			want: nil,
		},
		{
			name: "带 choices 但空 usage 指针",
			line: `data: {"id":"x","choices":[{"index":0,"delta":{"content":"a"}}],"usage":null}`,
			want: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseUsageFrame(tc.line)
			if tc.want == nil {
				if got != nil {
					t.Fatalf("parseUsageFrame(%q) = %+v, want nil", tc.line, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("parseUsageFrame(%q) = nil, want %+v", tc.line, tc.want)
			}
			if got.PromptTokens != tc.want.PromptTokens || got.CompletionTokens != tc.want.CompletionTokens {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}
