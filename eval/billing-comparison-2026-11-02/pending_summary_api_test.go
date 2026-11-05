package comparison

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/admin"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	gatewayhandler "github.com/frostloom/ai-gateway/internal/gateway/handler"
	"github.com/frostloom/ai-gateway/internal/gateway/middleware"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	"github.com/gin-gonic/gin"
)

func TestPendingSummaryAdminSnapshot(t *testing.T) {
	db, redis := isolatedDB(t)
	st := store.NewStore(db, redis)
	if err := st.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	tag := fmt.Sprintf("pending-summary-%d", time.Now().UnixNano())
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	marker := base.Add(3 * time.Minute)
	rows := []store.Bill{
		{RequestID: tag + "-a", Phase: store.PhaseReserve, Status: store.StatusPending, TenantID: 1, APIKeyID: 1, Funding: store.FundingBalance, CreatedAt: base},
		{RequestID: tag + "-b", Phase: store.PhaseReserve, Status: store.StatusPending, TenantID: 2, APIKeyID: 2, Funding: store.FundingSubscription, CreatedAt: base.Add(time.Minute)},
		{RequestID: tag + "-c", Phase: store.PhaseReserve, Status: store.StatusPending, TenantID: 2, APIKeyID: 2, Funding: store.FundingBalance, CreatedAt: base.Add(2 * time.Minute), UsageReportedAt: &marker},
		{RequestID: tag + "-d", Phase: store.PhaseReserve, Status: store.StatusSettled, TenantID: 1, APIKeyID: 1, Funding: store.FundingBalance, CreatedAt: base.Add(-time.Hour)},
		{RequestID: tag + "-e", Phase: store.PhaseSettle, Status: store.StatusPending, TenantID: 1, APIKeyID: 1, Funding: store.FundingBalance, CreatedAt: base.Add(-time.Hour)},
	}
	for i := range rows {
		if err := db.Create(&rows[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	defer db.Where("request_id LIKE ?", tag+"%").Delete(&store.Bill{})
	handler := admin.Handler(st, slog.New(slog.DiscardHandler), "isolated-token")
	request := func(method, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/admin/pending-summary", nil)
		if token != "" {
			r.Header.Set(controlplane.Header, token)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if got := request(http.MethodGet, ""); got.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d", got.Code)
	}
	if got := request(http.MethodPost, "isolated-token"); got.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status=%d", got.Code)
	}
	got := request(http.MethodGet, "isolated-token")
	if got.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", got.Code, got.Body.String())
	}
	var summary struct {
		Total         int64      `json:"pending_total"`
		Missing       int64      `json:"usage_marker_missing"`
		Present       int64      `json:"usage_marker_present"`
		MissingOldest *time.Time `json:"missing_oldest_created_at"`
		PresentOldest *time.Time `json:"present_oldest_created_at"`
		Observed      time.Time  `json:"observed_at"`
	}
	if err := json.Unmarshal(got.Body.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Total != 3 || summary.Missing != 2 || summary.Present != 1 || summary.MissingOldest == nil || !summary.MissingOldest.Equal(base) || summary.PresentOldest == nil || !summary.PresentOldest.Equal(base.Add(2*time.Minute)) || summary.Observed.IsZero() {
		t.Fatalf("summary=%+v", summary)
	}
	// The real gateway admin group must enforce its own session middleware and
	// forward the control token to the real billing admin handler.
	billingServer := httptest.NewServer(handler)
	defer billingServer.Close()
	gatewayhandler.SetControlToken("isolated-token")
	defer gatewayhandler.SetControlToken("")
	engine := gin.New()
	group := engine.Group("/admin")
	group.Use(middleware.AdminSession(billingServer.URL, "synthetic-admin", "isolated-token", slog.New(slog.DiscardHandler)))
	gatewayhandler.RegisterAdmin(group, billingServer.URL, "http://127.0.0.1:1", "http://127.0.0.1:1", "http://127.0.0.1:1", slog.New(slog.DiscardHandler))
	gatewayRequest := func(token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/admin/pending-summary", nil)
		if token != "" {
			r.Header.Set("X-Admin-Token", token)
		}
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, r)
		return w
	}
	if response := gatewayRequest(""); response.Code != http.StatusUnauthorized {
		t.Fatalf("gateway unauthorized=%d", response.Code)
	}
	if response := gatewayRequest("synthetic-admin"); response.Code != http.StatusOK {
		t.Fatalf("gateway status=%d body=%s", response.Code, response.Body.String())
	}
	if err := db.Where("request_id LIKE ?", tag+"%").Delete(&store.Bill{}).Error; err != nil {
		t.Fatal(err)
	}
	got = request(http.MethodGet, "isolated-token")
	if got.Code != http.StatusOK {
		t.Fatalf("empty status=%d", got.Code)
	}
	if err := json.Unmarshal(got.Body.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Total != 0 || summary.Missing != 0 || summary.Present != 0 || summary.MissingOldest != nil || summary.PresentOldest != nil {
		t.Fatalf("empty=%+v", summary)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	if response := request(http.MethodGet, "isolated-token"); response.Code != http.StatusInternalServerError {
		t.Fatalf("unavailable database status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestPendingSummaryAdminQueryTimeBounded(t *testing.T) {
	db, redis := isolatedDB(t)
	st := store.NewStore(db, redis)
	if err := st.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	conn, err := sqlDB.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err = conn.ExecContext(context.Background(), "LOCK TABLES bills WRITE"); err != nil {
		t.Fatal(err)
	}
	locked := true
	defer func() {
		if locked {
			_, _ = conn.ExecContext(context.Background(), "UNLOCK TABLES")
		}
	}()
	mux := admin.Handler(st, slog.New(slog.DiscardHandler), "isolated-token")
	done := make(chan int, 1)
	go func() {
		req := httptest.NewRequest(http.MethodGet, "/admin/pending-summary", nil)
		req.Header.Set(controlplane.Header, "isolated-token")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		done <- w.Code
	}()
	var result int
	select {
	case result = <-done:
	case <-time.After(3500 * time.Millisecond):
		result = 0
	}
	if _, err = conn.ExecContext(context.Background(), "UNLOCK TABLES"); err != nil {
		t.Fatal(err)
	}
	locked = false
	if result == 0 {
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("request did not finish after unlock")
		}
		t.Fatal("query remained blocked beyond budget")
	}
	if result != http.StatusInternalServerError {
		t.Fatalf("status=%d", result)
	}
}
