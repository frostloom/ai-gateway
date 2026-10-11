package main

import "testing"

func TestPreparedReadDatasetUsesSupportedTaskSchema(t *testing.T) {
	cases, err := loadCases("../../eval/task-suites/2026-10-11-read-fixtures-dev-v1/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 214 {
		t.Fatalf("cases=%d want214", len(cases))
	}
	for _, c := range cases {
		if !c.Read || c.Tool != "get_balance" && c.Tool != "get_my_subscription" {
			t.Fatalf("unsupported prepared case: %+v", c)
		}
	}
}
