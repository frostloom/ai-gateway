package comparison

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/pkg/metrics"
	"github.com/frostloom/ai-gateway/internal/router"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func routerMetric(t *testing.T, operation string) float64 {
	t.Helper()
	w := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	if w.Code != 200 {
		t.Fatalf("metrics status=%d", w.Code)
	}
	prefix := fmt.Sprintf(`router_persistence_errors_total{operation="%s"} `, operation)
	for _, line := range strings.Split(w.Body.String(), "\n") {
		if strings.HasPrefix(line, prefix) {
			n, err := strconv.ParseFloat(strings.TrimPrefix(line, prefix), 64)
			if err != nil {
				t.Fatal(err)
			}
			return n
		}
	}
	return 0
}
func TestRouterPersistenceErrorMetrics(t *testing.T) {
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.AutoMigrate(&store.Provider{}); err != nil {
		t.Fatal(err)
	}
	p := store.Provider{Name: fmt.Sprintf("metric-%d", time.Now().UnixNano()), BaseURL: "http://127.0.0.1:1", Models: "[]", Weight: 1}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	ops := []string{"increment", "reset", "ban"}
	before := make(map[string]float64)
	for _, op := range ops {
		before[op] = routerMetric(t, op)
	}
	name := fmt.Sprintf("inject-router-metric-%d", p.ID)
	if err := db.Callback().Update().Before("gorm:update").Register(name, func(tx *gorm.DB) { tx.AddError(errors.New("injected update failure")) }); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Update().Remove(name)
	reg := router.New(db, slog.New(slog.DiscardHandler))
	reg.FailThreshold = 5
	reg.AutobanAfter = 2
	reg.Report(p.ID, false)
	reg.Report(p.ID, true)
	reg.Report(p.ID, false)
	reg.Report(p.ID, false)
	for op, want := range map[string]float64{"increment": 2, "reset": 1, "ban": 1} {
		got := routerMetric(t, op) - before[op]
		if got != want {
			t.Errorf("operation=%s got=%v want=%v", op, got, want)
		}
	}
	var row store.Provider
	if err := db.First(&row, p.ID).Error; err != nil || row.ConsecutiveFail != 0 || row.Status != 0 {
		t.Fatalf("injected db row=%+v err=%v", row, err)
	}
}
