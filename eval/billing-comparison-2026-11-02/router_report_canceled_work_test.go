package comparison

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	"github.com/frostloom/ai-gateway/internal/router"
	"gorm.io/gorm"
)

type reportObservedBody struct {
	io.Reader
	read chan struct{}
	done atomic.Bool
}

func (b *reportObservedBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	if n > 0 && b.done.CompareAndSwap(false, true) {
		close(b.read)
	}
	return n, err
}

func TestCanceledRouterReportStillPersists(t *testing.T) {
	db, _ := isolatedDB(t)
	if err := db.AutoMigrate(&store.Provider{}); err != nil {
		t.Fatal(err)
	}
	p := store.Provider{Name: fmt.Sprintf("cancel-report-%d", time.Now().UnixNano()), BaseURL: "http://127.0.0.1:1", Models: `["model"]`, Weight: 1}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	reg := router.New(db, slog.New(slog.DiscardHandler))
	entered, release := make(chan struct{}), make(chan struct{})
	var writes atomic.Int32
	callback := fmt.Sprintf("eval:cancel-report-%d", time.Now().UnixNano())
	if err := db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "providers" && writes.Add(1) == 1 {
			close(entered)
			<-release
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Update().Remove(callback)
	first := make(chan error, 1)
	go func() { first <- reg.Report(p.ID, true) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("first report did not enter write")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	body := &reportObservedBody{Reader: strings.NewReader(fmt.Sprintf(`{"provider_id":%d,"ok":false}`, p.ID)), read: make(chan struct{})}
	req := httptest.NewRequest(http.MethodPost, "/report", body).WithContext(ctx)
	req.Header.Set(controlplane.Header, "test-token")
	resp := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		router.NewHTTPHandler(reg, "test-token", slog.New(slog.DiscardHandler)).ServeHTTP(resp, req)
		close(done)
	}()
	select {
	case <-body.read:
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("report body not read")
	}
	close(release)
	select {
	case err := <-first:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("first report stuck")
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("canceled report stuck")
	}
	var got store.Provider
	if err := db.First(&got, p.ID).Error; err != nil {
		t.Fatal(err)
	}
	if resp.Code != 200 || got.ConsecutiveFail != 1 || writes.Load() != 2 {
		t.Fatalf("status=%d db_fail=%d writes=%d", resp.Code, got.ConsecutiveFail, writes.Load())
	}
	t.Log("pre-canceled report was read while previous write blocked and subsequently persisted; recorder is not a real disconnected client")
}
