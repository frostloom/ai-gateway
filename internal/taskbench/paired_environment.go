package taskbench

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	driver "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type PairedFixtureOptions struct {
	AllowFixtureCreation bool
	AdminDSN             string // explicit local MySQL administration DSN, no selected database
	ProfilesDir          string
}

// PairedFixtureEnvironment owns only its randomly created DB and private Redis.
// It never attaches to an existing application database or Redis service.
type PairedFixtureEnvironment struct {
	admin    *sql.DB
	db       *gorm.DB
	rdb      *redis.Client
	redis    *miniredis.Miniredis
	st       *store.Store
	name     string
	profiles PairedProfiles
	plans    map[string]store.Plan
	closed   bool
}

func OpenPairedFixtureEnvironment(ctx context.Context, o PairedFixtureOptions) (*PairedFixtureEnvironment, error) {
	if !o.AllowFixtureCreation {
		return nil, fmt.Errorf("fixture creation disabled")
	}
	p, err := LoadPairedProfiles(o.ProfilesDir)
	if err != nil {
		return nil, err
	}
	c, err := driver.ParseDSN(o.AdminDSN)
	if err != nil || c.DBName != "" || c.Net != "tcp" {
		return nil, fmt.Errorf("explicit local admin DSN without database required")
	}
	host, _, err := net.SplitHostPort(c.Addr)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return nil, fmt.Errorf("fixture MySQL must be loopback")
	}
	c.Timeout = 2 * time.Second
	c.ReadTimeout = 10 * time.Second
	c.WriteTimeout = 10 * time.Second
	a, err := sql.Open("mysql", c.FormatDSN())
	if err != nil {
		return nil, fmt.Errorf("fixture admin connection unavailable")
	}
	e := &PairedFixtureEnvironment{admin: a, profiles: p}
	name := "ai_gateway_pair_" + randomID()
	fail := func(err error) (*PairedFixtureEnvironment, error) { return nil, errors.Join(err, e.Close()) }
	if err = a.PingContext(ctx); err != nil {
		return fail(fmt.Errorf("fixture MySQL unavailable"))
	}
	if _, err = a.ExecContext(ctx, "CREATE DATABASE "+name+" CHARACTER SET utf8mb4"); err != nil {
		// Creation failed: this instance owns no schema and must not drop one.
		e.name = ""
		return fail(fmt.Errorf("isolated fixture database creation failed"))
	}
	e.name = name
	c.DBName = e.name
	c.ParseTime = true
	c.Loc = time.Local
	e.db, err = gorm.Open(mysql.Open(c.FormatDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return fail(fmt.Errorf("isolated fixture connection unavailable"))
	}
	pool, err := e.db.DB()
	if err != nil {
		return fail(err)
	}
	pool.SetMaxOpenConns(4)
	e.redis, err = miniredis.Run()
	if err != nil {
		return fail(err)
	}
	e.rdb = redis.NewClient(&redis.Options{Addr: e.redis.Addr()})
	e.st = store.NewStore(e.db, e.rdb)
	if err = e.st.AutoMigrate(); err != nil {
		return fail(fmt.Errorf("fixture migration failed"))
	}
	e.plans, err = seedPairedCatalogue(ctx, e.db, p.archive)
	if err != nil {
		return fail(fmt.Errorf("fixture catalogue creation failed"))
	}
	return e, nil
}

func (e *PairedFixtureEnvironment) Close() error {
	if e == nil || e.closed {
		return nil
	}
	e.closed = true
	var errs []error
	if e.rdb != nil {
		errs = append(errs, e.rdb.Close())
	}
	if e.redis != nil {
		e.redis.Close()
	}
	if e.db != nil {
		if p, err := e.db.DB(); err == nil {
			errs = append(errs, p.Close())
		} else {
			errs = append(errs, err)
		}
	}
	if e.admin != nil {
		if e.name != "" {
			if !strings.HasPrefix(e.name, "ai_gateway_pair_") || strings.ContainsAny(e.name, "` ;'\"\\") {
				errs = append(errs, fmt.Errorf("unsafe owned database identity"))
			} else {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				_, err := e.admin.ExecContext(ctx, "DROP DATABASE "+e.name)
				errs = append(errs, err)
				var remaining int
				err = e.admin.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name = ?", e.name).Scan(&remaining)
				if err != nil || remaining != 0 {
					errs = append(errs, fmt.Errorf("fixture cleanup not verified"))
				}
				cancel()
			}
		}
		errs = append(errs, e.admin.Close())
	}
	return errors.Join(errs...)
}
