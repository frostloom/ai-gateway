package router

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

var testChannelActive atomic.Bool
var registerChannelDriver sync.Once

type channelStatusDriver struct{}
type channelStatusConn struct{}
type channelStatusRows struct{ done bool }

func (channelStatusDriver) Open(string) (driver.Conn, error)  { return channelStatusConn{}, nil }
func (channelStatusConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (channelStatusConn) Close() error                        { return nil }
func (channelStatusConn) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }
func (channelStatusConn) QueryContext(_ context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
	return &channelStatusRows{}, nil
}
func (*channelStatusRows) Columns() []string { return []string{"id"} }
func (*channelStatusRows) Close() error      { return nil }
func (r *channelStatusRows) Next(v []driver.Value) error {
	if r.done || !testChannelActive.Load() {
		return io.EOF
	}
	r.done = true
	v[0] = int64(17)
	return nil
}

func TestCachedChannelIsExcludedAfterDeletion(t *testing.T) {
	registerChannelDriver.Do(func() { sql.Register("channel-status-test", channelStatusDriver{}) })
	sqldb, err := sql.Open("channel-status-test", "")
	if err != nil {
		t.Fatal(err)
	}
	defer sqldb.Close()
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqldb, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	r := New(db, slog.New(slog.DiscardHandler))
	r.ActiveRecheck = 0
	r.cache["m"] = &cachedRoute{providers: []Provider{{ID: 17, Name: "test", Weight: 1}}, expireAt: time.Now().Add(time.Minute)}
	testChannelActive.Store(true)
	p, err := r.Route("m")
	if err != nil || p == nil {
		t.Fatalf("active channel was not selected: %v", err)
	}
	testChannelActive.Store(false)
	p, err = r.Route("m")
	if err != nil || p != nil {
		t.Fatalf("deleted cached channel was selected: %+v %v", p, err)
	}
}
