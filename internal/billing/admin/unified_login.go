package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"gorm.io/gorm"
)

func resolveLoginRole(admin, user bool) string {
	if admin == user {
		return ""
	}
	if admin {
		return "admin"
	}
	return "user"
}

func registerUnifiedLogin(mux *http.ServeMux, st *store.Store) {
	mux.HandleFunc("/auth/unified-login", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !requirePOST(w, r) {
			return
		}
		b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
		if err != nil {
			writeErr(w, 400, "请求过大")
			return
		}
		var input struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if json.Unmarshal(b, &input) != nil {
			writeErr(w, 400, "请求格式错误")
			return
		}
		name := strings.TrimSpace(input.Username)
		_, aerr := st.GetAdminByUsername(r.Context(), name)
		_, uerr := st.GetPortalUser(r.Context(), strings.ToLower(name))
		if (aerr != nil && !errors.Is(aerr, gorm.ErrRecordNotFound)) || (uerr != nil && !errors.Is(uerr, gorm.ErrRecordNotFound)) {
			writeErr(w, 500, "登录暂时不可用")
			return
		}
		role := resolveLoginRole(aerr == nil, uerr == nil)
		if role == "" {
			writeErr(w, 401, "用户名或密码错误，或账号需要管理员处理")
			return
		}
		input.Username = name
		if role == "user" {
			input.Username = strings.ToLower(name)
		}
		body, _ := json.Marshal(input)
		req := r.Clone(r.Context())
		req.URL.Path = "/auth/login"
		if role == "admin" {
			req.URL.Path = "/admin/auth/login"
		}
		req.Body = io.NopCloser(bytes.NewReader(body))
		req.ContentLength = int64(len(body))
		mux.ServeHTTP(w, req)
	})
}
