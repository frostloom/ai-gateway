package taskbench

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/pkg/config"
	"github.com/frostloom/ai-gateway/internal/pkg/redisx"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"os"
	"strings"
	"time"
)

type LiveBackend struct {
	db  *gorm.DB
	rdb *redis.Client
	st  *store.Store
}

func NewLiveFromEnv() (*LiveBackend, error) {
	db, err := gorm.Open(mysql.Open(config.Getenv("MYSQL_DSN", "root:root@tcp(127.0.0.1:3307)/ai_gateway?charset=utf8mb4&parseTime=True&loc=Local")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, fmt.Errorf("mysql connection unavailable")
	}
	rdb := redisx.New(config.Getenv("REDIS_ADDR", "127.0.0.1:6381"), config.Getenv("REDIS_PASSWORD", ""))
	return &LiveBackend{db: db, rdb: rdb, st: store.NewStore(db, rdb)}, nil
}
func (b *LiveBackend) Close() error {
	redisErr := b.rdb.Close()
	sqlDB, err := b.db.DB()
	if err != nil {
		return err
	}
	if err = sqlDB.Close(); err != nil {
		return err
	}
	return redisErr
}
func ResolvePlans(plans []store.Plan) (store.Plan, store.Plan, error) {
	var standard, flagship []store.Plan
	for _, p := range plans {
		if p.Status != 0 || p.PlanType != store.PlanTypeRecurring {
			continue
		}
		if p.Name == "月度·标准" {
			standard = append(standard, p)
		}
		if p.Name == "月度·旗舰" {
			flagship = append(flagship, p)
		}
	}
	if len(standard) != 1 || len(flagship) != 1 {
		return store.Plan{}, store.Plan{}, fmt.Errorf("expected unique active recurring 月度·标准 and 月度·旗舰 plans; found %d and %d", len(standard), len(flagship))
	}
	return standard[0], flagship[0], nil
}
func (b *LiveBackend) Setup(ctx context.Context, runID string, c Case) (Fixture, error) {
	f := Fixture{}
	plans, err := b.st.ListPlans(ctx)
	if err != nil {
		return f, err
	}
	f.Standard, f.Flagship, err = ResolvePlans(plans)
	if err != nil {
		return f, err
	}
	// UUID and case suffix fit the schema's 64-character unique tenant name.
	name := "taskeval-" + runID + "-" + c.ID
	if len(name) > 64 {
		caseHash := sha256.Sum256([]byte(c.ID))
		name = "taskeval-" + runID + "-" + hex.EncodeToString(caseHash[:])[:12]
	}
	tenant := store.Tenant{Name: name}
	if err = b.db.WithContext(ctx).Create(&tenant).Error; err != nil {
		return f, err
	}
	f.TenantID = tenant.ID
	f.TenantName = name
	key := "sk-task-" + randomID() + randomID()
	hash := sha256.Sum256([]byte(key))
	expiry := time.Now().Add(24 * time.Hour)
	api := store.APIKey{TenantID: tenant.ID, KeyHash: hex.EncodeToString(hash[:]), KeyPrefix: key[:8], Name: "taskbench fixture", ExpiresAt: &expiry}
	if err = b.db.WithContext(ctx).Create(&api).Error; err != nil {
		return f, err
	}
	f.APIKey = key
	if err = b.rdb.Set(ctx, store.BalanceKey(tenant.ID), 0, 0).Err(); err != nil {
		return f, err
	}
	order, err := b.st.CreateRechargeOrder(ctx, tenant.ID, 5000, "fixture-"+runID+"-"+c.ID)
	if err != nil {
		return f, err
	}
	if _, err = b.st.PayRecharge(ctx, order.OrderNo); err != nil {
		return f, err
	}
	f.OrderNo = order.OrderNo
	if c.InitialSubscription {
		if _, err = b.st.SubscribePlan(ctx, tenant.ID, f.Standard.ID); err != nil {
			return f, err
		}
	}
	if c.InitiallyCancelled {
		if _, err = b.st.CancelSubscription(ctx, tenant.ID); err != nil {
			return f, err
		}
	}
	return f, nil
}
func (b *LiveBackend) Snapshot(ctx context.Context, f Fixture, session string) (Snapshot, error) {
	out := Snapshot{Orders: []store.RechargeOrder{}, Subscriptions: []store.Subscription{}, Topups: []store.Topup{}, Audits: []store.AgentAuditLog{}}
	me, err := b.st.MeOverview(ctx, f.TenantID)
	if err != nil {
		return out, err
	}
	out.BalanceCent = me.BalanceCent
	out.LedgerBalance = me.LedgerBalance
	out.Consistent = me.Consistent
	db := b.db.WithContext(ctx)
	if err = db.Where("tenant_id = ?", f.TenantID).Order("id asc").Find(&out.Orders).Error; err != nil {
		return out, err
	}
	if err = db.Where("tenant_id = ?", f.TenantID).Order("id asc").Find(&out.Subscriptions).Error; err != nil {
		return out, err
	}
	if err = db.Where("tenant_id = ?", f.TenantID).Order("id asc").Find(&out.Topups).Error; err != nil {
		return out, err
	}
	if err = db.Where("tenant_id = ?", f.TenantID).Order("id asc").Find(&out.Audits).Error; err != nil {
		return out, err
	}
	return out, nil
}
func (b *LiveBackend) Pending(ctx context.Context, session string) (Pending, error) {
	if strings.TrimSpace(session) == "" {
		return Pending{}, fmt.Errorf("empty session ID")
	}
	raw, err := b.rdb.Get(ctx, "agent:session:"+session).Bytes()
	if err != nil {
		return Pending{}, err
	}
	var v struct {
		Pending *Pending `json:"pending_action"`
	}
	if err = json.Unmarshal(raw, &v); err != nil {
		return Pending{}, err
	}
	if v.Pending == nil {
		return Pending{}, fmt.Errorf("server pending state absent")
	}
	return *v.Pending, nil
}
func WriteReport(path string, r Report) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err = f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
