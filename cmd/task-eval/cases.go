package main

import (
	"encoding/json"
	"os"

	"github.com/frostloom/ai-gateway/internal/taskbench"
)

func loadCases(path string) ([]taskbench.Case, error) {
	if path == "" {
		return taskbench.DefaultCases(), nil
	}
	var cases []taskbench.Case
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(data, &cases)
	return cases, err
}
