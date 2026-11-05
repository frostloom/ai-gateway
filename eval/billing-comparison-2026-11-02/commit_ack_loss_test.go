package comparison

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"os"
	"sync/atomic"
	"testing"

	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var errCommitAckLost = errors.New("injected commit acknowledgement loss")

type ackLossConnector struct {
	driver.Connector
	fail      *atomic.Bool
	commitErr error
}

func (c ackLossConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &ackLossConn{Conn: conn, fail: c.fail, commitErr: c.commitErr}, nil
}

type ackLossConn struct {
	driver.Conn
	fail      *atomic.Bool
	commitErr error
}

func (c *ackLossConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	tx, err := c.Conn.(driver.ConnBeginTx).BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &ackLossTx{Tx: tx, fail: c.fail, commitErr: c.commitErr}, nil
}
func (c *ackLossConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if e, ok := c.Conn.(driver.ExecerContext); ok {
		return e.ExecContext(ctx, q, args)
	}
	return nil, driver.ErrSkip
}
func (c *ackLossConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if e, ok := c.Conn.(driver.QueryerContext); ok {
		return e.QueryContext(ctx, q, args)
	}
	return nil, driver.ErrSkip
}
func (c *ackLossConn) CheckNamedValue(v *driver.NamedValue) error {
	if e, ok := c.Conn.(driver.NamedValueChecker); ok {
		return e.CheckNamedValue(v)
	}
	return driver.ErrSkip
}

type ackLossTx struct {
	driver.Tx
	fail      *atomic.Bool
	commitErr error
}

func (t *ackLossTx) Commit() error {
	if err := t.Tx.Commit(); err != nil {
		return err
	}
	if t.fail.CompareAndSwap(true, false) {
		return t.commitErr
	}
	return nil
}

func recoveryDBWithLostCommitAck(t *testing.T) *gorm.DB {
	return recoveryDBWithCommitError(t, errCommitAckLost)
}
func recoveryDBWithCommitError(t *testing.T, commitErr error) *gorm.DB {
	t.Helper()
	cfg, err := mysqldriver.ParseDSN(os.Getenv("COMPARISON_MYSQL_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	connector, err := mysqldriver.NewConnector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var fail atomic.Bool
	fail.Store(true)
	sqldb := sql.OpenDB(ackLossConnector{Connector: connector, fail: &fail, commitErr: commitErr})
	t.Cleanup(func() { _ = sqldb.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqldb, SkipInitializeWithVersion: true}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db
}
