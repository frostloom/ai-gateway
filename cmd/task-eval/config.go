package main

import (
	"fmt"
	"net"
	"net/url"
	"os"
)

type runConfig struct {
	Base, Out, Version, JevMode, Model, Source, AgentBinary, CasesFile string
	AllowWrites                                                        bool
	PrepareOnly                                                        bool
	PreparedFile, RoutingMode                                          string
	MaxTasks                                                           int
}

func validateRun(c runConfig) error {
	if !c.PrepareOnly && !c.AllowWrites {
		return fmt.Errorf("pass -allow-simulated-writes to permit writes to newly created simulation tenants")
	}
	u, err := url.Parse(c.Base)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return fmt.Errorf("agent must be an http loopback origin")
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("agent must be a loopback origin")
	}
	if c.Out == "" || c.Version == "" || c.Model == "" || c.Source == "" || c.AgentBinary == "" {
		return fmt.Errorf("out, version, model, source and agent-binary are required")
	}
	if c.JevMode != "off" && c.JevMode != "on" {
		return fmt.Errorf("jev must be off or on")
	}
	if c.RoutingMode != "" && c.RoutingMode != plannedRouting(c.JevMode) {
		return fmt.Errorf("routing-mode must match jev: off=model, on=jev_selective")
	}
	for _, path := range []string{c.Out, c.Out + ".md", c.Out + ".running"} {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("output or run lock already exists: %s", path)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
