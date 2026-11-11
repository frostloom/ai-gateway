package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/frostloom/ai-gateway/internal/supportconfig"
	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// This test accepts only an explicitly isolated database and has no Redis or
// provider requests. The caller owns creating and removing the disposable DB.
func TestCustomerServiceConfigAtomicAndEncrypted(t *testing.T) {
	dsn := os.Getenv("CUSTOMER_SERVICE_TEST_DSN")
	if dsn == "" {
		t.Skip("CUSTOMER_SERVICE_TEST_DSN required")
	}
	cfg, err := mysqldriver.ParseDSN(dsn)
	if err != nil || !strings.HasPrefix(cfg.DBName, "ai_gateway_service_test_") {
		t.Fatal("use an isolated ai_gateway_service_test_* database")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	if err := db.AutoMigrate(&CustomerServiceSettings{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DELETE FROM customer_service_settings").Error; err != nil {
		t.Fatal(err)
	}
	st := NewStore(db, nil)
	ctx := context.Background()
	cipher, err := supportconfig.NewCipher(base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := st.LoadCustomerServiceConfig(ctx, cipher)
	if err != nil || loaded != nil {
		t.Fatal("initial state not empty", err)
	}
	in := supportconfig.Update{LLM: supportconfig.LLMUpdate{BaseURL: "https://llm.example/v1", Model: "old-model", Protocol: "openai", MaxOutputTokens: 8192}, JEV: supportconfig.JEVUpdate{Enabled: true, Endpoint: "https://jev.example/v1/systemone", Model: "jev-test"}}
	fallback := supportconfig.Config{LLM: supportconfig.LLMConfig{BaseURL: in.LLM.BaseURL, APIKey: "env-llm-private"}, JEV: supportconfig.JEVConfig{Endpoint: in.JEV.Endpoint, APIKey: "env-jev-private"}}
	broken := in
	broken.LLM.MaxOutputTokens = 0
	if _, err := st.SaveCustomerServiceConfig(ctx, cipher, broken, fallback); !errors.Is(err, ErrCustomerServiceInvalid) {
		t.Fatal("invalid save accepted", err)
	}
	var rows int64
	db.Model(&CustomerServiceSettings{}).Count(&rows)
	if rows != 0 {
		t.Fatal("invalid save committed placeholder")
	}
	first, err := st.SaveCustomerServiceConfig(ctx, cipher, in, fallback)
	if err != nil || first.Revision != 1 || first.LLM.APIKey != fallback.LLM.APIKey || first.JEV.APIKey != fallback.JEV.APIKey {
		t.Fatal("environment credentials not retained", err)
	}
	var row CustomerServiceSettings
	if err := db.First(&row, 1).Error; err != nil {
		t.Fatal(err)
	}
	if row.LLMKeyCiphertext == fallback.LLM.APIKey || row.JEVKeyCiphertext == fallback.JEV.APIKey {
		t.Fatal("plaintext persisted")
	}
	raw, _ := json.Marshal(row)
	if strings.Contains(string(raw), "ciphertext") || strings.Contains(string(raw), "private") {
		t.Fatal("row JSON exposed secret")
	}
	loaded, err = st.LoadCustomerServiceConfig(ctx, cipher)
	if err != nil || loaded.LLM.APIKey != first.LLM.APIKey || loaded.Revision != 1 {
		t.Fatal("readback failed", err)
	}
	wrong, _ := supportconfig.NewCipher(base64.StdEncoding.EncodeToString([]byte(strings.Repeat("z", 32))))
	if _, err := st.LoadCustomerServiceConfig(ctx, wrong); !errors.Is(err, supportconfig.ErrEncryptionUnavailable) {
		t.Fatal("wrong master accepted", err)
	}
	if _, err := st.SaveCustomerServiceConfig(ctx, cipher, in, fallback); !errors.Is(err, ErrCustomerServiceConflict) {
		t.Fatal("stale revision accepted", err)
	}
	in.Revision = 1
	in.LLM.Model = "new-model"
	changedOrigin := in
	changedOrigin.LLM.BaseURL = "https://other.example/v1"
	if _, err := st.SaveCustomerServiceConfig(ctx, cipher, changedOrigin); !errors.Is(err, ErrCustomerServiceInvalid) {
		t.Fatal("retained key sent to new origin", err)
	}
	in.LLM.APIKey = "replacement-llm-private"
	second, err := st.SaveCustomerServiceConfig(ctx, cipher, in)
	if err != nil || second.Revision != 2 || second.LLM.APIKey != in.LLM.APIKey || second.JEV.APIKey != fallback.JEV.APIKey {
		t.Fatal("key replacement failed", err)
	}
	in.Revision = 2
	in.LLM.APIKey = ""
	in.JEV.Enabled = false
	// Concurrent admins starting from one revision must produce one winner.
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := st.SaveCustomerServiceConfig(ctx, cipher, in); results <- err }()
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrCustomerServiceConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("concurrent save success=%d conflict=%d", success, conflict)
	}
	loaded, err = st.LoadCustomerServiceConfig(ctx, cipher)
	if err != nil || loaded.Revision != 3 || loaded.JEV.Enabled || loaded.JEV.APIKey != fallback.JEV.APIKey {
		t.Fatal("disabled JEV or retained key lost", err)
	}
	if _, err := st.LoadCustomerServiceConfig(ctx, nil); !errors.Is(err, supportconfig.ErrEncryptionUnavailable) {
		t.Fatal("missing master accepted", err)
	}
}
