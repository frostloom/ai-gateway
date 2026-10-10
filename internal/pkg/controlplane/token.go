// Package controlplane 是进程之间的共享令牌。
// 网关用它访问 router、billing admin 和 billing gRPC。
// 令牌为空时一律拒绝，避免未配置时把控制面留在打开状态。
package controlplane

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	// Header 是 HTTP 控制面令牌头。
	Header = "X-Control-Token"
	// MetadataKey 是 gRPC metadata 键（小写）。
	MetadataKey = "x-control-token"
	// Env 是各进程读取的环境变量名。
	Env = "CONTROL_PLANE_TOKEN"
)

// Require 读取令牌。未设置时退出，避免服务在无令牌时监听。
func Require(log *slog.Logger) string {
	tok := strings.TrimSpace(os.Getenv(Env))
	if tok == "" {
		log.Error("CONTROL_PLANE_TOKEN is required")
		os.Exit(1)
	}
	return tok
}

// Valid 判断调用方出示的令牌是否与服务端配置一致。
// 空配置或空出示都拒绝。长度不同时直接拒绝（subtle 比较要求等长）。
func Valid(presented, secret string) bool {
	if secret == "" || presented == "" || len(presented) != len(secret) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(presented), []byte(secret)) == 1
}

// WrapHTTP 保护 handler。open 中的路径（如 /healthz、/metrics）不校验令牌。
func WrapHTTP(secret string, open map[string]struct{}, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := open[r.URL.Path]; ok {
			next.ServeHTTP(w, r)
			return
		}
		if !Valid(r.Header.Get(Header), secret) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"control plane token required"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// UnaryServer 校验入站 gRPC 令牌。
func UnaryServer(secret string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		md, _ := metadata.FromIncomingContext(ctx)
		presented := ""
		if vs := md.Get(MetadataKey); len(vs) > 0 {
			presented = vs[0]
		}
		if !Valid(presented, secret) {
			return nil, status.Error(codes.Unauthenticated, "control plane token required")
		}
		return handler(ctx, req)
	}
}

// UnaryClient 把令牌写入出站 gRPC metadata。
func UnaryClient(secret string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		ctx = metadata.AppendToOutgoingContext(ctx, MetadataKey, secret)
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// Dial 连接 billing gRPC，并附带控制面令牌。
func Dial(addr, secret string) (*grpc.ClientConn, error) {
	return grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(UnaryClient(secret)),
	)
}
