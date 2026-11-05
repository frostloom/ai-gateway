package main

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	dsn, addr, port := os.Getenv("TEST_MYSQL_DSN"), os.Getenv("TEST_REDIS_ADDR"), os.Getenv("TEST_MOCK_PORT")
	if dsn == "" || addr == "" || port == "" {
		log.Fatal("isolated settings required")
	}
	db, e := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if e != nil {
		log.Fatal(e)
	}
	r := redis.NewClient(&redis.Options{Addr: addr})
	defer r.Close()
	ctx := context.Background()
	tenantCount := 8
	if raw := os.Getenv("TEST_TENANTS"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 1024 {
			log.Fatal("TEST_TENANTS must be 1..1024")
		}
		tenantCount = n
	}
	if os.Getenv("TEST_MODE") == "fixture" || os.Getenv("TEST_MODE") == "target" {
		data, err := os.ReadFile(os.Getenv("TEST_FIXTURE_FILE"))
		if err != nil {
			log.Fatal(err)
		}
		var fixture struct {
			UsageID string `json:"usage_request_id"`
			EmptyID string `json:"empty_request_id"`
		}
		if err := json.Unmarshal(data, &fixture); err != nil {
			log.Fatal(err)
		}
		ids := []string{fixture.UsageID, fixture.EmptyID}
		if os.Getenv("TEST_MODE") == "target" {
			if !strings.HasPrefix(fixture.UsageID, "idem:recon-http-") || fixture.EmptyID != "" {
				log.Fatal("invalid target fixture ID")
			}
			ids = ids[:1]
		} else if !strings.HasPrefix(fixture.UsageID, "grace-usage-") || !strings.HasPrefix(fixture.EmptyID, "grace-empty-") {
			log.Fatal("invalid synthetic fixture IDs")
		}
		rows := make(map[string]any)
		for _, id := range ids {
			var bill store.Bill
			if err := db.Where("request_id=? AND phase=? AND tenant_id=?", id, store.PhaseReserve, 1).Take(&bill).Error; err != nil {
				log.Fatal(err)
			}
			events := make(map[string]int64)
			for _, phase := range []string{"reserve", "usage", "settle", "reverse"} {
				var count int64
				if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":"+phase).Count(&count).Error; err != nil {
					log.Fatal(err)
				}
				events[phase] = count
			}
			var settleRows int64
			if err := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", id, store.PhaseSettle).Count(&settleRows).Error; err != nil {
				log.Fatal(err)
			}
			rows[id] = map[string]any{"status": bill.Status, "usage_reported": bill.UsageReportedAt != nil, "prompt_tokens": bill.PromptTokens, "completion_tokens": bill.CompletionTokens, "created_at": bill.CreatedAt, "settle_rows": settleRows, "events": events}
		}
		balance, err := r.Get(ctx, store.BalanceKey(1)).Int64()
		if err != nil {
			log.Fatal(err)
		}
		ledger, err := store.LedgerBalance(ctx, db, 1)
		if err != nil {
			log.Fatal(err)
		}
		if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"records": rows, "balance": balance, "ledger": ledger}); err != nil {
			log.Fatal(err)
		}
		return
	}
	if os.Getenv("TEST_MODE") == "trace" {
		path := os.Getenv("TEST_TRACE_PATH")
		if path == "" {
			log.Fatal("TEST_TRACE_PATH required")
		}
		f, e := os.Open(path)
		if e != nil {
			log.Fatal(e)
		}
		defer f.Close()
		reader := csv.NewReader(f)
		if _, e = reader.Read(); e != nil {
			log.Fatal(e)
		}
		var bills []store.Bill
		if e = db.Find(&bills).Error; e != nil {
			log.Fatal(e)
		}
		reserves := map[string]store.Bill{}
		settles := map[string]int{}
		for _, bill := range bills {
			if bill.Phase == store.PhaseReserve {
				reserves[bill.RequestID] = bill
			}
			if bill.Phase == store.PhaseSettle {
				settles[bill.RequestID]++
			}
		}
		groups := map[string]int{}
		anomalies := make([]map[string]any, 0)
		anomaliesTruncated := 0
		total := 0
		for {
			row, e := reader.Read()
			if e == io.EOF {
				break
			}
			if e != nil || len(row) != 3 {
				log.Fatalf("bad CSV: %v", e)
			}
			bill, found := reserves[row[0]]
			state := "missing"
			usage := "none"
			if found {
				state = bill.Status
				if bill.UsageReportedAt != nil {
					usage = "reported"
				}
			}
			key := fmt.Sprintf("http=%s valid=%s reserve=%s usage=%s settle_rows=%d", row[1], row[2], state, usage, settles[row[0]])
			groups[key]++
			// Record only unusual synthetic requests; no keys or request bodies.
			if (row[1] == "200" && (row[2] != "true" || state != store.StatusSettled || usage != "reported" || settles[row[0]] != 1)) || (row[1] != "200" && found) {
				if len(anomalies) < 10000 {
					entry := map[string]any{"request_id": row[0], "http": row[1], "valid": row[2], "reserve": state, "usage": usage, "settle_rows": settles[row[0]]}
					if found {
						entry["bill_id"] = bill.ID
						entry["tenant_id"] = bill.TenantID
					}
					anomalies = append(anomalies, entry)
				} else {
					anomaliesTruncated++
				}
			}
			total++
		}
		result := map[string]any{"requests": total, "groups": groups, "anomalies": anomalies, "anomalies_truncated": anomaliesTruncated}
		data, _ := json.Marshal(result)
		fmt.Println(string(data))
		return
	}
	if os.Getenv("TEST_MODE") == "audit" {
		var bills []store.Bill
		if e := db.Find(&bills).Error; e != nil {
			log.Fatal(e)
		}
		var pending, settled, reversed int
		for _, b := range bills {
			if b.Phase == store.PhaseReserve {
				switch b.Status {
				case store.StatusPending:
					pending++
				case store.StatusSettled:
					settled++
				case store.StatusReversed:
					reversed++
				}
			}
		}
		var sessions int64
		if e := db.Model(&store.AgentSession{}).Count(&sessions).Error; e != nil {
			log.Fatal(e)
		}
		var events, unsent, delivered, attempts int64
		for _, item := range []struct {
			query *gorm.DB
			dst   *int64
		}{
			{db.Model(&store.OutboxEvent{}), &events},
			{db.Model(&store.OutboxEvent{}).Where("sent_at IS NULL"), &unsent},
			{db.Model(&store.OutboxEvent{}).Where("attempts > 0"), &attempts},
			{db.Model(&store.AuditEvent{}), &delivered},
		} {
			if e := item.query.Count(item.dst).Error; e != nil {
				log.Fatal(e)
			}
		}
		issues := []string{}
		missing := []string{}
		for i := 1; i <= tenantCount; i++ {
			live, e := r.Get(ctx, store.BalanceKey(uint64(i))).Int64()
			if e == redis.Nil && os.Getenv("TEST_AUDIT_ALLOW_MISSING") == "1" {
				ledger, ledgerErr := store.LedgerBalance(ctx, db, uint64(i))
				if ledgerErr != nil {
					log.Fatal(ledgerErr)
				}
				missing = append(missing, fmt.Sprintf("tenant=%d ledger=%d", i, ledger))
				continue
			}
			if e != nil {
				log.Fatal(e)
			}
			ledger, e := store.LedgerBalance(ctx, db, uint64(i))
			if e != nil {
				log.Fatal(e)
			}
			if live != ledger {
				issues = append(issues, fmt.Sprintf("tenant=%d live=%d ledger=%d", i, live, ledger))
			}
		}
		x := map[string]any{"agent_sessions": sessions, "pending": pending, "settled": settled, "reversed": reversed, "outbox": events, "outbox_unsent": unsent, "outbox_attempted_error": attempts, "audit_events": delivered, "balance_mismatches": issues, "missing_balance_keys": missing}
		v, _ := json.Marshal(x)
		fmt.Println(string(v))
		if len(issues) > 0 {
			os.Exit(1)
		}
		return
	}
	m := store.Model{Vendor: "isolated", ModelID: "test-model", Name: "isolated model", InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if e := db.Create(&m).Error; e != nil {
		log.Fatal(e)
	}
	p := store.Provider{Name: "isolated-mock", BaseURL: "http://127.0.0.1:" + port, Models: `["test-model"]`, Weight: 1}
	if e := db.Create(&p).Error; e != nil {
		log.Fatal(e)
	}
	for i := 1; i <= tenantCount; i++ {
		tenant := store.Tenant{ID: uint64(i), Name: fmt.Sprintf("test-%d", i), InitialQuota: 1000000000}
		if e := db.Create(&tenant).Error; e != nil {
			log.Fatal(e)
		}
		key := fmt.Sprintf("sk-isolated-%03d", i)
		sum := sha256.Sum256([]byte(key))
		api := store.APIKey{TenantID: tenant.ID, KeyHash: hex.EncodeToString(sum[:]), KeyPrefix: key[:8], Name: "isolated"}
		if os.Getenv("TEST_BROWSER_MIX") == "1" && i%2 == 0 {
			api.Name = "__browser_session"
		}
		if e := db.Create(&api).Error; e != nil {
			log.Fatal(e)
		}
		if api.Name == "__browser_session" {
			user := store.PortalUser{Username: fmt.Sprintf("isolated-user-%d", i), PasswordHash: "synthetic", TenantID: tenant.ID}
			if e := db.Create(&user).Error; e != nil {
				log.Fatal(e)
			}
			if e := db.Create(&store.PortalSession{UserID: user.ID, APIKeyID: api.ID}).Error; e != nil {
				log.Fatal(e)
			}
		}
		if e := r.Set(ctx, store.BalanceKey(tenant.ID), tenant.InitialQuota, 0).Err(); e != nil {
			log.Fatal(e)
		}
	}
	fmt.Printf("seeded %d isolated tenants and one loopback mock provider\n", tenantCount)
}
