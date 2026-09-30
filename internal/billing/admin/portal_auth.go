package admin

import (
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"golang.org/x/crypto/bcrypt"
)

const portalCookie = "agw_user"

var portalUsername = regexp.MustCompile(`^[a-z0-9_-]{3,32}$`)

func registerPortalAuth(mux *http.ServeMux, st *store.Store, log *slog.Logger) {
	slots := make(chan struct{}, 4)
	for _, action := range []string{"register", "login"} {
		mux.HandleFunc("/auth/"+action, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			if !requirePOST(w, r) {
				return
			}
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			default:
				writeErr(w, 429, "请求过于频繁，请稍后重试")
				return
			}
			var in struct {
				Username string `json:"username"`
				Password string `json:"password"`
				Role     string `json:"role"`
			}
			r.Body = http.MaxBytesReader(w, r.Body, 4096)
			if !parseBody(w, r, &in) {
				return
			}
			in.Username = strings.ToLower(strings.TrimSpace(in.Username))
			if !portalUsername.MatchString(in.Username) || len(in.Password) < 8 || len(in.Password) > 72 {
				writeErr(w, 400, "用户名须为 3–32 位字母、数字、下划线或短横线；密码须为 8–72 字节")
				return
			}
			if in.Role != "" && in.Role != "user" {
				writeErr(w, 403, "公开注册和用户登录仅支持普通用户")
				return
			}
			var u *store.PortalUser
			if action == "register" {
				hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
				if err != nil {
					writeErr(w, 500, "注册失败")
					return
				}
				u, err = st.RegisterPortalUser(r.Context(), in.Username, string(hash))
				if err != nil {
					log.Warn("register user failed", "err", err)
					writeErr(w, 409, "用户名已被使用，请更换后重试")
					return
				}
			} else {
				var err error
				u, err = st.GetPortalUser(r.Context(), in.Username)
				if err != nil || u.Status != 0 || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(in.Password)) != nil {
					writeErr(w, 401, "用户名或密码错误")
					return
				}
			}
			token, err := randomToken()
			if err != nil {
				writeErr(w, 500, "创建会话失败")
				return
			}
			if err := st.CreatePortalSession(r.Context(), u, token, time.Now().Add(sessionTTL)); err != nil {
				writeErr(w, 500, "创建会话失败，请重新登录")
				return
			}
			if old, err := r.Cookie(portalCookie); err == nil {
				_ = st.RevokePortalSession(r.Context(), old.Value)
			}
			http.SetCookie(w, &http.Cookie{Name: portalCookie, Value: token, Path: "/", HttpOnly: true, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https", SameSite: http.SameSiteLaxMode, MaxAge: int(sessionTTL.Seconds())})
			writeJSON(w, 200, map[string]any{"id": u.ID, "username": u.Username, "role": "user", "tenant_id": u.TenantID})
		})
	}
	mux.HandleFunc("/auth/me", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet {
			writeErr(w, 405, "method not allowed")
			return
		}
		ck, err := r.Cookie(portalCookie)
		if err != nil {
			writeErr(w, 401, "请先登录")
			return
		}
		u, key, err := st.PortalIdentity(r.Context(), ck.Value)
		if err != nil {
			writeErr(w, 401, "登录已过期，请重新登录")
			return
		}
		writeJSON(w, 200, map[string]any{"id": u.ID, "username": u.Username, "role": "user", "tenant_id": u.TenantID, "api_key_id": key.ID})
	})
	mux.HandleFunc("/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !requirePOST(w, r) {
			return
		}
		if ck, err := r.Cookie(portalCookie); err == nil {
			if err := st.RevokePortalSession(r.Context(), ck.Value); err != nil {
				writeErr(w, 500, "退出失败，请重试")
				return
			}
		}
		http.SetCookie(w, &http.Cookie{Name: portalCookie, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
		writeJSON(w, 200, map[string]any{"ok": true})
	})
}
