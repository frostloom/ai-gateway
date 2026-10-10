// Package config 通用环境变量读取（docker-compose 注入，本地开发可 .env）。
package config

import (
	"net"
	"os"
	"strconv"
)

// ListenAddr 返回服务监听地址。默认只绑 127.0.0.1。
// LISTEN_HOST=0.0.0.0 时才对外。
func ListenAddr(port string) string {
	return net.JoinHostPort(Getenv("LISTEN_HOST", "127.0.0.1"), port)
}

// Getenv 读字符串环境变量，缺省返回 def。
func Getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// GetenvInt 读 int 环境变量，非法/缺省返回 def。
func GetenvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// GetenvInt64 读 int64 环境变量，非法/缺省返回 def。
func GetenvInt64(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}

// GetenvFloat 读 float64 环境变量，非法/缺省返回 def。
func GetenvFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}
