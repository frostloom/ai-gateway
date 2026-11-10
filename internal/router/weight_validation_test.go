package router

import "testing"

func TestPickWeightedInvalidWeights(t *testing.T) {
	for _, rows := range [][]Provider{nil, {{Weight: 0}}, {{Weight: -1}}, {{Weight: 1000001}}} {
		if got := pickWeighted(rows); got != nil {
			t.Errorf("invalid weights selected %+v", got)
		}
	}
	rows := []Provider{{ID: 1, Weight: -1}, {ID: 2, Weight: 1}}
	if got := pickWeighted(rows); got == nil || got.ID != 2 {
		t.Fatal("valid provider not selected")
	}
}
