// Package observability exports only explicitly allowed agent metadata.
package observability

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	CaptureContent                                                bool
	Enabled                                                       bool
	Endpoint, PublicKey, SecretKey, HashKey, Release, Environment string
	SampleRate                                                    float64
	QueueSize                                                     int
	ExportTimeout                                                 time.Duration
}

func ConfigFromEnv(get func(string) string) (Config, error) {
	c := Config{SampleRate: 1, QueueSize: 256, ExportTimeout: 2 * time.Second, Environment: "production"}
	enabled := get("AGENT_LANGFUSE_ENABLED")
	if enabled == "" || enabled == "0" || enabled == "false" {
		return c, nil
	}
	if enabled != "1" && enabled != "true" {
		return c, errors.New("invalid AGENT_LANGFUSE_ENABLED")
	}
	c.Enabled = true
	capture := get("AGENT_LANGFUSE_CAPTURE_CONTENT")
	if capture != "" && capture != "false" && capture != "0" && capture != "true" && capture != "1" {
		return c, errors.New("invalid content capture flag")
	}
	c.CaptureContent = capture == "true" || capture == "1"
	c.Endpoint = get("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")
	c.PublicKey = get("LANGFUSE_PUBLIC_KEY")
	c.SecretKey = get("LANGFUSE_SECRET_KEY")
	c.HashKey = get("AGENT_LANGFUSE_HASH_KEY")
	c.Release = get("AGENT_RELEASE")
	if c.Release == "" {
		c.Release = "unknown"
	}
	if env := get("AGENT_LANGFUSE_ENVIRONMENT"); env != "" {
		c.Environment = env
	}
	if sample := get("AGENT_LANGFUSE_SAMPLE_RATE"); sample != "" {
		v, err := strconv.ParseFloat(sample, 64)
		if err != nil {
			return c, errors.New("invalid telemetry sample rate")
		}
		c.SampleRate = v
	}
	return c, c.validate()
}

func (c Config) validate() error {
	u, err := url.Parse(c.Endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/api/public/otel/v1/traces" {
		return errors.New("invalid Langfuse OTLP trace endpoint")
	}
	if strings.TrimSpace(c.PublicKey) == "" || strings.TrimSpace(c.SecretKey) == "" || strings.ContainsAny(c.PublicKey+c.SecretKey, "\r\n:") {
		return errors.New("missing or invalid Langfuse project keys")
	}
	if len(c.HashKey) < 32 {
		return errors.New("telemetry identity hash key must be at least 32 bytes")
	}
	if c.Environment != "production" && c.Environment != "evaluation" {
		return errors.New("telemetry environment must be production or evaluation")
	}
	if !(c.SampleRate >= 0 && c.SampleRate <= 1) {
		return errors.New("invalid telemetry sample rate")
	}
	if c.QueueSize < 0 || c.QueueSize > 4096 || c.ExportTimeout < 0 || c.ExportTimeout > 10*time.Second {
		return errors.New("invalid telemetry queue or timeout limit")
	}
	return nil
}
