package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/frostloom/ai-gateway/internal/localpaired"
)

func main() {
	auth, err := loadAuth()
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	root := localpaired.CampaignDir
	if len(os.Args) == 2 && os.Args[1] != "" {
		root = os.Args[1]
	}
	if err := localpaired.RunLive(context.Background(), root, auth); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func loadAuth() (localpaired.LiveAuth, error) {
	raw, err := os.ReadFile(os.ExpandEnv("$HOME/.codex/auth.json"))
	if err != nil {
		return localpaired.LiveAuth{}, fmt.Errorf("chatgpt auth file unreadable")
	}
	var file struct {
		Tokens struct {
			AccessToken string `json:"access_token"`
			AccountID   string `json:"account_id"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return localpaired.LiveAuth{}, fmt.Errorf("chatgpt auth file unreadable")
	}
	env, err := loadEnvFile(".env")
	if err != nil {
		return localpaired.LiveAuth{}, err
	}
	langfuse, err := loadEnvFile("deploy/langfuse/.env.agent")
	if err != nil {
		return localpaired.LiveAuth{}, err
	}
	return localpaired.LiveAuth{
		Token:       file.Tokens.AccessToken,
		AccountID:   file.Tokens.AccountID,
		JEVKey:      env["JEV_API_KEY"],
		JEVEndpoint: env["JEV_ENDPOINT"],
		JEVModel:    env["JEV_MODEL"],
		ProxyURL:    env["JEV_HTTPS_PROXY"],
		Langfuse:    langfuse,
	}, nil
}

func loadEnvFile(path string) (map[string]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("env file unreadable")
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"'`)
	}
	return out, nil
}
