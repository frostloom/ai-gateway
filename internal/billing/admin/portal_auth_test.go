package admin

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"golang.org/x/crypto/bcrypt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestPublicAccountHTTP(t *testing.T) {
	sqlDB, err := sql.Open("mysql", "root:root@tcp(127.0.0.1:3307)/")
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	if _, err = sqlDB.Exec("CREATE DATABASE IF NOT EXISTS ai_gateway_auth_test"); err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(mysql.Open("root:root@tcp(127.0.0.1:3307)/ai_gateway_auth_test?parseTime=true"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	conn, _ := db.DB()
	defer conn.Close()
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6381", DB: 2})
	defer rdb.Close()
	st := store.NewStore(db, rdb)
	if err := st.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	h := Handler(st, slog.Default())
	name := fmt.Sprintf("http%d", time.Now().UnixNano())
	request := func(path string, body any, ck *http.Cookie) *httptest.ResponseRecorder {
		method := "GET"
		var raw []byte
		if body != nil {
			method = "POST"
			raw, _ = json.Marshal(body)
		}
		r := httptest.NewRequest(method, path, bytes.NewReader(raw))
		if ck != nil {
			r.AddCookie(ck)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	credentials := map[string]string{"username": name, "password": "ValidPassword123!"}
	hash, _ := bcrypt.GenerateFromPassword([]byte("AdminPassword123!"), bcrypt.DefaultCost)
	adminName := " legacy-admin-" + name + " "
	if err := st.CreateAdmin(context.Background(), &store.AdminUser{Username: adminName, PasswordHash: string(hash)}); err != nil {
		t.Fatal(err)
	}
	adminLogin := request("/admin/auth/login", map[string]string{"username": adminName, "password": "AdminPassword123!"}, nil)
	if adminLogin.Code != 200 {
		t.Fatal("existing admin login failed")
	}
	adminCookie := adminLogin.Result().Cookies()[0]
	if got := request("/admin/auth/me", nil, adminCookie); got.Code != 200 {
		t.Fatal("admin session rejected")
	}
	if got := request("/auth/me", nil, adminCookie); got.Code != 401 {
		t.Fatal("admin cookie impersonates user")
	}
	bad := request("/auth/register", map[string]string{"username": name, "password": "ValidPassword123!", "role": "admin"}, nil)
	if bad.Code != 403 {
		t.Fatalf("role escalation accepted: %d", bad.Code)
	}
	reg := request("/auth/register", credentials, nil)
	if reg.Code != 200 {
		t.Fatalf("register %d: %s", reg.Code, reg.Body.String())
	}
	cookies := reg.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatal("missing secure session cookie")
	}
	if bytes.Contains(reg.Body.Bytes(), []byte("password")) || bytes.Contains(reg.Body.Bytes(), []byte(cookies[0].Value)) {
		t.Fatal("credential leaked in JSON")
	}
	if got := request("/auth/me", nil, cookies[0]); got.Code != 200 {
		t.Fatal("session not restored")
	}
	if got := request("/admin/auth/me", nil, cookies[0]); got.Code != 401 {
		t.Fatal("user became admin")
	}
	if got := request("/auth/register", credentials, nil); got.Code != 409 {
		t.Fatal("duplicate registration accepted")
	}
	if got := request("/auth/login", map[string]string{"username": name, "password": "WrongPassword!"}, nil); got.Code != 401 {
		t.Fatal("wrong password accepted")
	}
	if got := request("/auth/logout", map[string]string{}, cookies[0]); got.Code != 200 {
		t.Fatal("logout failed")
	}
	if got := request("/auth/me", nil, cookies[0]); got.Code != 401 {
		t.Fatal("revoked session accepted")
	}
	if got := request("/auth/login", credentials, nil); got.Code != 200 {
		t.Fatal("password login failed")
	}
}
