package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"time"

	redis "github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// One pinned session owns both the tenant lock and any nested request lock.
// Its Redis token fences delayed workers after MySQL releases a lost session.
type cashSession struct {
	tenant  uint64
	conn    *sql.Conn
	root    *gorm.DB
	token   string
	release func()
	caller  context.Context
}

func cashFenceKey(tenant uint64) string { return fmt.Sprintf("billing:cash-owner:%d", tenant) }

func (s *Store) lockCash(ctx context.Context, tenant uint64) (*Store, func(), error) {
	if s.cash != nil {
		if s.cash.tenant != tenant {
			return nil, nil, fmt.Errorf("cash lock tenant mismatch")
		}
		return s, func() {}, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	name := fmt.Sprintf("billing:cash:%d", tenant)
	conn, unlock, err := s.lockNamed(ctx, name)
	if err != nil {
		return nil, nil, err
	}
	sqlctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	release := func() { unlock(); cancel() }
	locked := *s
	locked.db = s.db.Session(&gorm.Session{NewDB: true}).WithContext(sqlctx)
	locked.db.Statement.ConnPool = conn
	locked.cash = &cashSession{tenant: tenant, conn: conn, root: s.db, token: rand.Text(), release: release, caller: ctx}
	// Publish BEFORE verifying the SQL session. A delayed acquirer that lost
	// its session may invalidate another worker, but can never write balances.
	if err := s.rdb.Set(ctx, cashFenceKey(tenant), locked.cash.token, 0).Err(); err != nil {
		release()
		return nil, nil, err
	}
	var owns bool
	if err := conn.QueryRowContext(sqlctx, "SELECT IS_USED_LOCK(?) = CONNECTION_ID()", name).Scan(&owns); err != nil || !owns {
		release()
		if err == nil {
			err = fmt.Errorf("cash session no longer owns tenant lock")
		}
		return nil, nil, err
	}
	return &locked, release, nil
}

func (s *Store) cashSQLContext() context.Context { return s.db.Statement.Context }

// Cancellation may stop a new financial operation. Once Redis/SQL writes
// begin, the bounded detached SQL context lets it finish or recover safely.
func (s *Store) cashMutationAllowed() error { return s.cash.caller.Err() }

func (s *Store) runCashLua(ctx context.Context, source string, keys []string, args ...any) (any, error) {
	if s.cash != nil {
		source = `if redis.call('GET', KEYS[#KEYS]) ~= ARGV[#ARGV] then return redis.error_reply('stale cash owner') end
` + source
		keys = append(keys, cashFenceKey(s.cash.tenant))
		args = append(args, s.cash.token)
	}
	return redis.NewScript(source).Run(ctx, s.rdb, keys, args...).Result()
}

// Optional guard cleanup and absolute projection repair are one Redis action.
// Never add a refund after a lock handoff: a rebuild may already have restored it.
func (s *Store) rebuildCash(ctx context.Context, tenant uint64, guard, owner string) (int64, error) {
	var row Tenant
	if err := s.db.WithContext(ctx).First(&row, tenant).Error; err != nil {
		return 0, err
	}
	if row.Status != 0 {
		return 0, fmt.Errorf("tenant disabled: %d", tenant)
	}
	bal, err := LedgerBalance(ctx, s.db, tenant)
	if err != nil {
		return 0, err
	}
	_, err = s.runCashLua(ctx, `
if ARGV[2] ~= '' then
  local current = redis.call('GET', KEYS[2])
  if current and current ~= ARGV[2] then return redis.error_reply('cash guard owner changed') end
  redis.call('DEL', KEYS[2])
end
redis.call('SET', KEYS[1], ARGV[1])
return 1`, []string{BalanceKey(tenant), guard}, bal, owner)
	return bal, err
}

func (s *Store) repairOrphanGuard(ctx context.Context, tenant uint64, key string) error {
	owner, err := s.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = s.rebuildCash(ctx, tenant, key, owner)
	return err
}
