package main

import "testing"

func TestLatencyByOutcome(t *testing.T) {
	success, failure := latencyByOutcome([]sample{{valid: true, lat: 10}, {valid: true, lat: 20}, {valid: false, lat: 1}, {valid: false, lat: 2}, {valid: false, lat: 3}})
	if success == nil || success.Count != 2 || success.MeanMS != 15 || success.P95MS != 20 || success.P99MS != 20 {
		t.Fatalf("success=%+v", success)
	}
	if failure == nil || failure.Count != 3 || failure.MeanMS != 2 || failure.P95MS != 3 || failure.P99MS != 3 {
		t.Fatalf("failure=%+v", failure)
	}
	success, failure = latencyByOutcome([]sample{{valid: true, lat: 7}})
	if success == nil || success.Count != 1 || success.P95MS != 7 || failure != nil {
		t.Fatalf("success=%+v failure=%+v", success, failure)
	}
	success, failure = latencyByOutcome(nil)
	if success != nil || failure != nil {
		t.Fatalf("empty success=%+v failure=%+v", success, failure)
	}
}

func TestValidStream(t *testing.T) {
	cases := []struct {
		name, body string
		want       bool
	}{
		{"complete", "data: {\"choices\":[]}\n\ndata: {\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":3}}\n\ndata: [DONE]\n\n", true},
		{"missing_done", "data: {\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":3}}\n\n", false},
		{"missing_usage", "data: [DONE]\n\n", false},
		{"incomplete_usage", "data: {\"usage\":{\"prompt_tokens\":2}}\n\ndata: [DONE]\n\n", false},
		{"negative_usage", "data: {\"usage\":{\"prompt_tokens\":-1,\"completion_tokens\":3}}\n\ndata: [DONE]\n\n", false},
		{"content_is_not_done", "data: {\"choices\":[{\"text\":\"[DONE]\"}]}\n\n", false},
		{"invalid_json", "data: {\n\ndata: [DONE]\n\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := validStream([]byte(tc.body)); got != tc.want {
				t.Fatalf("got=%v want=%v", got, tc.want)
			}
		})
	}
}
