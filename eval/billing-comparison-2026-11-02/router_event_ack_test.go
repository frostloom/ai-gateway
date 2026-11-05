package comparison

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"gorm.io/gorm/logger"
)

func TestRouterEventCommitAckAndCrossProviderConflict(t *testing.T) {
	if os.Getenv("TEST_ROUTER_EVENT_ACK") != "1" {
		t.Skip("isolated only")
	}
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.AutoMigrate(&store.Provider{}, &routerTestEvent{}); err != nil {
		t.Fatal(err)
	}
	for _, injected := range []error{errCommitAckLost, driver.ErrBadConn} {
		id := fmt.Sprintf("event-ack-%d", time.Now().UnixNano())
		p := store.Provider{Name: id, BaseURL: "http://127.0.0.1:1", Models: "[]", Weight: 1}
		other := store.Provider{Name: id + "-other", BaseURL: "http://127.0.0.1:1", Models: "[]", Weight: 1}
		if err := db.Create(&p).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&other).Error; err != nil {
			t.Fatal(err)
		}
		broken := recoveryDBWithCommitError(t, injected)
		if err := testRouterApply(context.Background(), broken, id, p.ID, false, false); !errors.Is(err, injected) {
			t.Fatalf("expected injected=%v got=%v", injected, err)
		}
		if err := testRouterApply(context.Background(), db, id, p.ID, false, false); err != nil {
			t.Fatal(err)
		}
		if err := testRouterApply(context.Background(), db, id, other.ID, false, false); !errors.Is(err, errRouterConflict) {
			t.Fatalf("cross-provider conflict=%v", err)
		}
		var actual, otherActual store.Provider
		if err := db.First(&actual, p.ID).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.First(&otherActual, other.ID).Error; err != nil {
			t.Fatal(err)
		}
		var n int64
		if err := db.Model(&routerTestEvent{}).Where("id=?", id).Count(&n).Error; err != nil || n != 1 || actual.ConsecutiveFail != 1 || otherActual.ConsecutiveFail != 0 {
			t.Fatalf("events=%d first=%d other=%d err=%v", n, actual.ConsecutiveFail, otherActual.ConsecutiveFail, err)
		}
		t.Logf("commit_succeeded_injected_error=%v retry_no_duplicate=true cross_provider_conflict=true events=%d first_count=%d other_count=%d", injected, n, actual.ConsecutiveFail, otherActual.ConsecutiveFail)
	}
}
