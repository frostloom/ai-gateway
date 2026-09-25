// 管理员认证端点（newapi 式：首次初始化 / 之后登录）—— HttpOnly cookie 保持会话。
// 设计：billing 校验账号并签发随机 token，Set-Cookie(agw_admin) 给浏览器；
// gateway 的 /admin/auth/* 转发本段，并把 Set-Cookie 透传给前端（见 gateway handler/proxy）。
package admin

import (
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/frostloom/ai-gateway/internal/billing/store"
)

// cookieName HttpOnly 会话 cookie 名。Path=/ 全站可带；SameSite=Lax 防跨站 POST 误带。
const cookieName = "agw_admin"

// sessionTTL 会话有效期。
const sessionTTL = 12 * time.Hour

// registerAuth 挂载 /admin/auth/* 路由（mux 由 Handler 传入）。
func registerAuth(mux *http.ServeMux, st *store.Store, log *slog.Logger) {
	mux.HandleFunc("/admin/auth/initialized", func(w http.ResponseWriter, r *http.Request) {
		n, err := st.CountAdmins(r.Context())
		if err != nil {
			log.Error("auth initialized", "err", err)
			writeErr(w, http.StatusInternalServerError, "internal error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"initialized": n > 0})
	})

	mux.HandleFunc("/admin/auth/setup", func(w http.ResponseWriter, r *http.Request) {
		if !requirePOST(w, r) {
			return
		}
		// 仅当系统还没有任何管理员时才允许初始化。
		n, err := st.CountAdmins(r.Context())
		if err != nil {
			log.Error("auth setup", "err", err)
			writeErr(w, http.StatusInternalServerError, "internal error")
			return
		}
		if n > 0 {
			writeErr(w, http.StatusConflict, "already initialized")
			return
		}
		var in struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if !parseBody(w, r, &in) {
			return
		}
		if in.Username == "" || len(in.Password) < 6 {
			writeErr(w, http.StatusBadRequest, "用户名必填，密码至少 6 位")
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
		if err != nil {
			log.Error("auth setup bcrypt", "err", err)
			writeErr(w, http.StatusInternalServerError, "password hash failed")
			return
		}
		u := &store.AdminUser{Username: in.Username, PasswordHash: string(hash)}
		if err := st.CreateAdmin(r.Context(), u); err != nil {
			log.Error("auth setup create", "err", err)
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		issueSession(w, r, st, log, u.Username)
	})

	mux.HandleFunc("/admin/auth/login", func(w http.ResponseWriter, r *http.Request) {
		if !requirePOST(w, r) {
			return
		}
		var in struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if !parseBody(w, r, &in) {
			return
		}
		u, err := st.GetAdminByUsername(r.Context(), in.Username)
		if err != nil || u.Status != 0 {
			// 用户不存在 / 禁用：统一 401，不泄露具体原因。
			writeErr(w, http.StatusUnauthorized, "用户名或密码错误")
			return
		}
		if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(in.Password)) != nil {
			writeErr(w, http.StatusUnauthorized, "用户名或密码错误")
			return
		}
		issueSession(w, r, st, log, u.Username)
	})

	mux.HandleFunc("/admin/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if !requirePOST(w, r) {
			return
		}
		if tok := sessionToken(r); tok != "" {
			if err := st.DeleteSession(r.Context(), tok); err != nil {
				log.Error("auth logout", "err", err)
			}
		}
		// 清 cookie（Max-Age<0 立即过期）。
		http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", HttpOnly: true, MaxAge: -1})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	mux.HandleFunc("/admin/auth/me", func(w http.ResponseWriter, r *http.Request) {
		tok := sessionToken(r)
		if tok == "" {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		se, err := st.GetSessionByToken(r.Context(), tok)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"username": se.Username})
	})
}

// issueSession 签发会话：生成随机 token 落库 + 设 HttpOnly cookie。
func issueSession(w http.ResponseWriter, r *http.Request, st *store.Store, log *slog.Logger, username string) {
	tok, err := randomToken()
	if err != nil {
		log.Error("auth token", "err", err)
		writeErr(w, http.StatusInternalServerError, "token generate failed")
		return
	}
	se := &store.AdminSession{Token: tok, Username: username, ExpiresAt: time.Now().Add(sessionTTL)}
	if err := st.CreateSession(r.Context(), se); err != nil {
		log.Error("auth session", "err", err)
		writeErr(w, http.StatusInternalServerError, "session create failed")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: tok, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, MaxAge: int(sessionTTL.Seconds()),
	})
	writeJSON(w, http.StatusOK, map[string]any{"username": username})
}

// sessionToken 从 cookie 或 X-Admin-Token 头取会话 token（gateway 把请求头的 cookie 透传过来）。
func sessionToken(r *http.Request) string {
	if ck, err := r.Cookie(cookieName); err == nil && ck.Value != "" {
		return ck.Value
	}
	if h := strings.TrimSpace(r.Header.Get("X-Admin-Token")); h != "" {
		return h
	}
	return ""
}

// randomToken 32 字节随机 → base64url（无填充），长度 ~43 字符，满足 size:64。
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
