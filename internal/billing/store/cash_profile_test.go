package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
	redis "github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// Test-only driver timing includes prepared-statement fallback and row reads.
// SQL lock timings include network round trips, not just server-side waiting.
// Categories never contain SQL arguments, request identifiers or credentials.
type cashLoadProfile struct {
	enabled atomic.Bool
	mu      sync.Mutex
	samples map[string][]float64
	errors  map[string]int
	db      *sql.DB
	start   sql.DBStats
}

var cashProfileSequence atomic.Uint64

// Hold an actual result open to check that timing includes row lifetime and
// counts the mysql prepared-statement fallback exactly once.
func TestCashProfileRowTiming(t *testing.T) {
	if os.Getenv("BILLING_CASH_PROFILE") != "1" {
		t.Skip("set BILLING_CASH_PROFILE=1 to check the opt-in profiler")
	}
	s, closeStore := newTestStore(t)
	defer closeStore()
	p := attachCashLoadProfile(t, s)
	p.begin()
	rows, err := p.db.QueryContext(context.Background(), "SELECT ?", 42)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("missing calibration row", rows.Err())
	}
	var value int
	if err := rows.Scan(&value); err != nil || value != 42 {
		t.Fatalf("calibration value=%d err=%v", value, err)
	}
	time.Sleep(40 * time.Millisecond) // Deliberate timer calibration, not race synchronization.
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	result := p.finish()
	metrics := result["metrics"].(map[string]any)
	read := metrics["sql.read"].(map[string]any)
	prepare := metrics["mysql.prepare"].(map[string]any)
	if read["count"] != 1 || read["errors"] != 0 || read["total_ms"].(float64) < 35 || prepare["count"] != 1 {
		t.Fatalf("unexpected calibration metrics: %+v", metrics)
	}
}

func attachCashLoadProfile(t *testing.T, s *Store) *cashLoadProfile {
	t.Helper()
	p := &cashLoadProfile{}
	name := fmt.Sprintf("cash-profile-%d", cashProfileSequence.Add(1))
	sql.Register(name, cashProfileDriver{p: p})
	db, err := sql.Open(name, testMySQLHost+testMySQLDB+"?charset=utf8mb4&parseTime=True&loc=Local")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(40)
	db.SetMaxIdleConns(10)
	bound, err := gorm.Open(mysql.New(mysql.Config{Conn: db}), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	s.db, p.db = bound, db
	s.rdb.AddHook(p)
	return p
}

func (p *cashLoadProfile) observe(key string, began time.Time, err error) {
	if !p.enabled.Load() || err == driver.ErrSkip {
		return
	}
	ms := float64(time.Since(began)) / float64(time.Millisecond)
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.enabled.Load() {
		return
	}
	p.samples[key] = append(p.samples[key], ms)
	if err != nil {
		p.errors[key]++
	}
}

func (p *cashLoadProfile) begin() {
	p.samples, p.errors = map[string][]float64{}, map[string]int{}
	p.start = p.db.Stats()
	p.enabled.Store(true)
}

func (p *cashLoadProfile) finish() map[string]any {
	p.enabled.Store(false) // Workers have drained; exclude final verification SQL.
	p.mu.Lock()
	defer p.mu.Unlock()
	end := p.db.Stats()
	metrics := map[string]any{}
	for key, samples := range p.samples {
		sort.Float64s(samples)
		sum := 0.0
		for _, sample := range samples {
			sum += sample
		}
		metrics[key] = map[string]any{
			"count": len(samples), "errors": p.errors[key], "total_ms": sum,
			"mean_ms": sum / float64(len(samples)), "p95_ms": samples[(len(samples)-1)*95/100],
			"max_ms": samples[len(samples)-1],
		}
	}
	return map[string]any{
		"metrics": metrics, "pool_wait_count": end.WaitCount - p.start.WaitCount,
		"pool_wait_ms": float64(end.WaitDuration-p.start.WaitDuration) / float64(time.Millisecond),
	}
}

func cashSQLCategory(query string, args []driver.NamedValue) string {
	q := strings.ToUpper(strings.ReplaceAll(query, "`", ""))
	switch {
	case strings.Contains(q, "GET_LOCK("):
		if len(args) > 0 && strings.HasPrefix(fmt.Sprint(args[0].Value), "billing:cash:") {
			return "sql.lock.tenant"
		}
		return "sql.lock.request"
	case strings.Contains(q, "RELEASE_LOCK("):
		return "sql.lock.release"
	case strings.Contains(q, "IS_USED_LOCK("):
		return "sql.lock.verify"
	case strings.Contains(q, "SUM("):
		return "sql.ledger_aggregate"
	case strings.HasPrefix(q, "SELECT INITIAL_QUOTA"):
		return "sql.ledger_initial"
	case strings.HasPrefix(q, "SELECT"):
		return "sql.read"
	default:
		return "sql.write"
	}
}

type cashProfileDriver struct{ p *cashLoadProfile }

func (d cashProfileDriver) Open(dsn string) (driver.Conn, error) {
	c, err := d.OpenConnector(dsn)
	if err != nil {
		return nil, err
	}
	return c.Connect(context.Background())
}

func (d cashProfileDriver) OpenConnector(dsn string) (driver.Connector, error) {
	c, err := (&drivermysql.MySQLDriver{}).OpenConnector(dsn)
	if err != nil {
		return nil, err
	}
	return &cashProfileConnector{Connector: c, d: d}, nil
}

type cashProfileConnector struct {
	driver.Connector
	d cashProfileDriver
}

func (c *cashProfileConnector) Driver() driver.Driver { return c.d }

func (c *cashProfileConnector) Connect(ctx context.Context) (driver.Conn, error) {
	began := time.Now()
	conn, err := c.Connector.Connect(ctx)
	c.d.p.observe("mysql.connect", began, err)
	if err != nil {
		return nil, err
	}
	return c.d.wrap(conn), nil
}

func (d cashProfileDriver) wrap(c driver.Conn) driver.Conn {
	// Forward the optional interfaces implemented by the pinned mysql driver.
	return &cashProfileConn{
		Conn: c, ConnPrepareContext: c.(driver.ConnPrepareContext), ConnBeginTx: c.(driver.ConnBeginTx),
		QueryerContext: c.(driver.QueryerContext), ExecerContext: c.(driver.ExecerContext),
		NamedValueChecker: c.(driver.NamedValueChecker), SessionResetter: c.(driver.SessionResetter),
		Validator: c.(driver.Validator), Pinger: c.(driver.Pinger), p: d.p,
	}
}

type cashProfileConn struct {
	driver.Conn
	driver.ConnPrepareContext
	driver.ConnBeginTx
	driver.QueryerContext
	driver.ExecerContext
	driver.NamedValueChecker
	driver.SessionResetter
	driver.Validator
	driver.Pinger
	p *cashLoadProfile
}

func (c *cashProfileConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	began := time.Now()
	s, err := c.ConnPrepareContext.PrepareContext(ctx, query)
	c.p.observe("mysql.prepare", began, err)
	if err != nil {
		return nil, err
	}
	return &cashProfileStmt{Stmt: s, StmtQueryContext: s.(driver.StmtQueryContext), StmtExecContext: s.(driver.StmtExecContext), NamedValueChecker: s.(driver.NamedValueChecker), query: query, p: c.p}, nil
}

func (c *cashProfileConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	began := time.Now()
	rows, err := c.QueryerContext.QueryContext(ctx, query, args)
	return c.p.rows(rows, err, cashSQLCategory(query, args), began)
}

func (c *cashProfileConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	began := time.Now()
	r, err := c.ExecerContext.ExecContext(ctx, query, args)
	c.p.observe(cashSQLCategory(query, args), began, err)
	return r, err
}

func (c *cashProfileConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	began := time.Now()
	tx, err := c.ConnBeginTx.BeginTx(ctx, opts)
	c.p.observe("mysql.begin", began, err)
	if err != nil {
		return nil, err
	}
	return &cashProfileTx{Tx: tx, p: c.p}, nil
}

type cashProfileStmt struct {
	driver.Stmt
	driver.StmtQueryContext
	driver.StmtExecContext
	driver.NamedValueChecker
	query string
	p     *cashLoadProfile
}

func (s *cashProfileStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	began := time.Now()
	rows, err := s.StmtQueryContext.QueryContext(ctx, args)
	return s.p.rows(rows, err, cashSQLCategory(s.query, args), began)
}

func (s *cashProfileStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	began := time.Now()
	r, err := s.StmtExecContext.ExecContext(ctx, args)
	s.p.observe(cashSQLCategory(s.query, args), began, err)
	return r, err
}

func (s *cashProfileStmt) Close() error {
	began := time.Now()
	err := s.Stmt.Close()
	s.p.observe("mysql.statement_close", began, err)
	return err
}

type cashProfileTx struct {
	driver.Tx
	p *cashLoadProfile
}

func (t *cashProfileTx) Commit() error {
	began := time.Now()
	err := t.Tx.Commit()
	t.p.observe("mysql.commit", began, err)
	return err
}

func (t *cashProfileTx) Rollback() error {
	began := time.Now()
	err := t.Tx.Rollback()
	t.p.observe("mysql.rollback", began, err)
	return err
}

type cashProfileRows struct {
	driver.Rows
	driver.RowsNextResultSet
	driver.RowsColumnTypeDatabaseTypeName
	driver.RowsColumnTypeNullable
	driver.RowsColumnTypePrecisionScale
	driver.RowsColumnTypeScanType
	p     *cashLoadProfile
	key   string
	began time.Time
	once  sync.Once
	err   error
}

func (p *cashLoadProfile) rows(rows driver.Rows, err error, key string, began time.Time) (driver.Rows, error) {
	if err != nil {
		p.observe(key, began, err)
		return nil, err
	}
	return &cashProfileRows{
		Rows: rows, RowsNextResultSet: rows.(driver.RowsNextResultSet),
		RowsColumnTypeDatabaseTypeName: rows.(driver.RowsColumnTypeDatabaseTypeName),
		RowsColumnTypeNullable:         rows.(driver.RowsColumnTypeNullable),
		RowsColumnTypePrecisionScale:   rows.(driver.RowsColumnTypePrecisionScale),
		RowsColumnTypeScanType:         rows.(driver.RowsColumnTypeScanType),
		p:                              p, key: key, began: began,
	}, nil
}

func (r *cashProfileRows) Columns() []string { return r.Rows.Columns() }

func (r *cashProfileRows) Next(dest []driver.Value) error {
	err := r.Rows.Next(dest)
	if err != nil && err != io.EOF {
		r.err = err
	}
	return err
}

func (r *cashProfileRows) Close() error {
	err := r.Rows.Close()
	r.once.Do(func() {
		if err != nil {
			r.err = err
		}
		r.p.observe(r.key, r.began, r.err)
	})
	return err
}

func (p *cashLoadProfile) DialHook(next redis.DialHook) redis.DialHook { return next }
func (p *cashLoadProfile) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		began := time.Now()
		err := next(ctx, cmd)
		key, observedErr := "redis."+cmd.Name(), err
		if err == redis.Nil || redis.HasErrorPrefix(err, "NOSCRIPT") {
			key, observedErr = key+".miss", nil
		}
		p.observe(key, began, observedErr)
		return err
	}
}
func (p *cashLoadProfile) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		began := time.Now()
		err := next(ctx, cmds)
		p.observe("redis.pipeline", began, err)
		return err
	}
}
