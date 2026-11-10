package channelintegration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/frostloom/ai-gateway/internal/billing/admin"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	"github.com/frostloom/ai-gateway/internal/router"
	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestIsolatedChannelAndIdentity(t *testing.T) {
	dsn := os.Getenv("CHANNEL_AUTH_TEST_DSN")
	if dsn == "" {
		t.Skip("requires explicitly supplied disposable MySQL database")
	}
	config, err := mysqldriver.ParseDSN(dsn)
	if err != nil || config.DBName != "isolated_test" || config.Net != "tcp" || !strings.HasPrefix(config.Addr, "127.0.0.1:") {
		t.Fatal("CHANNEL_AUTH_TEST_DSN must point to the disposable local isolated_test database")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	st := store.NewStore(db, nil)
	if err := st.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	p := &store.Provider{Name: "test-channel", BaseURL: "https://example.invalid", UpstreamKey: "test-secret", Models: `["model-test"]`, Weight: 1}
	if err := st.UpsertProvider(ctx, p); err != nil {
		t.Fatal(err)
	}
	id := p.ID
	bill := store.Bill{RequestID: "isolated-request", Phase: "settle", Status: "settled", ProviderID: &id}
	if err := db.Create(&bill).Error; err != nil {
		t.Fatal(err)
	}
	reg := router.New(db, slog.New(slog.DiscardHandler))
	reg.ActiveRecheck = 0
	if got, err := reg.Route("model-test"); err != nil || got == nil {
		t.Fatalf("initial route: %v", err)
	}
	if err := st.DeleteProvider(ctx, id); err != nil {
		t.Fatal(err)
	}
	if got, err := reg.Route("model-test"); err != nil || got != nil {
		t.Fatalf("cached route after deletion: %v %+v", err, got)
	}
	var archived store.Provider
	if err := db.First(&archived, id).Error; err != nil || archived.Status != store.ProviderDeleted || archived.UpstreamKey != "" {
		t.Fatalf("archive/key: %v %+v", err, archived)
	}
	var preserved store.Bill
	if err := db.First(&preserved, bill.ID).Error; err != nil || preserved.ProviderID == nil || *preserved.ProviderID != id {
		t.Fatalf("bill not retained: %v", err)
	}
	list, err := st.ListProvidersWithMargin(ctx)
	if err != nil || len(list) != 0 {
		t.Fatalf("archived channel visible: %v %+v", err, list)
	}
	if err := st.UpsertProvider(ctx, &store.Provider{Name: p.Name, BaseURL: "https://new.invalid", Models: `["model-test"]`, Weight: 1}); err == nil {
		t.Fatal("archived name was restored")
	}
	if got, err := reg.Route("model-test"); err != nil || got != nil {
		t.Fatalf("archived name revived: %v", err)
	}

	h := admin.Handler(st, slog.New(slog.DiscardHandler), "integration-only")
	second := &store.Provider{Name: "http-delete", BaseURL: "https://example.invalid", Models: `["m"]`, Weight: 1}
	if err := st.UpsertProvider(ctx, second); err != nil {
		t.Fatal(err)
	}
	deleteReq := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/admin/providers?id=%d", second.ID), nil)
	deleteReq.Header.Set(controlplane.Header, "integration-only")
	deleteResp := httptest.NewRecorder()
	h.ServeHTTP(deleteResp, deleteReq)
	if deleteResp.Code != http.StatusOK {
		t.Fatalf("delete API: %d %s", deleteResp.Code, deleteResp.Body.String())
	}
	if found, err := st.ListProvidersWithMargin(ctx); err != nil || len(found) != 0 {
		t.Fatalf("delete API left visible channels: %v", err)
	}
	request := func(path string, body map[string]string) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
		req.Header.Set(controlplane.Header, "integration-only")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	setup := request("/admin/auth/setup", map[string]string{"username": "AdminExample", "password": "Password123!"})
	if setup.Code != 200 {
		t.Fatalf("admin setup %d: %s", setup.Code, setup.Body.String())
	}
	regUser := request("/auth/register", map[string]string{"username": "normal_user", "password": "Password123!"})
	if regUser.Code != 200 {
		t.Fatalf("user register %d: %s", regUser.Code, regUser.Body.String())
	}
	for _, tc := range []struct{ name, role, cookie string }{{"AdminExample", "admin", "agw_admin"}, {"normal_user", "user", "agw_user"}} {
		w := request("/auth/unified-login", map[string]string{"username": tc.name, "password": "Password123!"})
		var body map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if w.Code != 200 || body["role"] != tc.role {
			t.Fatalf("unified %s: %d %s", tc.name, w.Code, w.Body.String())
		}
		found := false
		for _, c := range w.Result().Cookies() {
			if c.Name == tc.cookie && c.HttpOnly {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing %s cookie", tc.cookie)
		}
	}
	if got := request("/auth/register", map[string]string{"username": "adminexample", "password": "Password123!"}); got.Code != 409 {
		t.Fatalf("cross-role duplicate: %d", got.Code)
	}
	if got := request("/auth/unified-login", map[string]string{"username": "normal_user", "password": "wrongpass"}); got.Code != 401 {
		t.Fatalf("bad password: %d", got.Code)
	}
	// Race both account-creation paths using the same normalized username.
	var wg sync.WaitGroup
	wg.Add(2)
	errs := make(chan error, 2)
	go func() {
		defer wg.Done()
		errs <- st.CreateAdmin(ctx, &store.AdminUser{Username: "race_name", PasswordHash: "hash"})
	}()
	go func() { defer wg.Done(); _, e := st.RegisterPortalUser(ctx, "race_name", "hash"); errs <- e }()
	wg.Wait()
	close(errs)
	successes := 0
	for e := range errs {
		if e == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent name claims succeeded %d times", successes)
	}
	var ac, uc int64
	db.Model(&store.AdminUser{}).Where("username = ?", "race_name").Count(&ac)
	db.Model(&store.PortalUser{}).Where("username = ?", "race_name").Count(&uc)
	if ac+uc != 1 {
		t.Fatal(fmt.Sprintf("duplicate accounts: admin=%d user=%d", ac, uc))
	}
}
