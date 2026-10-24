package main

import (
	"fmt"
	"net/http"
	"time"
)

func healthcheck(endpoint string) error {
	c := http.Client{Timeout: 2 * time.Second}
	r, e := c.Get(endpoint)
	if e != nil {
		return e
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return fmt.Errorf("health status %d", r.StatusCode)
	}
	return nil
}
