package store

import (
	"context"
	"errors"
	"fmt"
	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"os"
	"strings"
	"testing"
	"time"
)

func TestValidateModelImport(t *testing.T) {
	contextLen := 8192
	zero := int64(0)
	paid := int64(123)
	negative := int64(-1)
	excessive := int64(1000000001)
	valid := ModelImportItem{ModelID: "test", ContextLen: &contextLen, InputPriceCent: &zero, OutputPriceCent: &paid}
	if err := ValidateModelImport([]ModelImportItem{valid}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*ModelImportItem)
	}{

		{"negative", func(i *ModelImportItem) { i.InputPriceCent = &negative }},
		{"excessive", func(i *ModelImportItem) { i.InputPriceCent = &excessive }},
		{"context", func(i *ModelImportItem) { bad := 0; i.ContextLen = &bad }},
		{"wildcard", func(i *ModelImportItem) { i.ModelID = "*" }},
		{"blank id", func(i *ModelImportItem) { i.ModelID = " " }},
		{"long id", func(i *ModelImportItem) { i.ModelID = string(make([]byte, 65)) }},
		{"control", func(i *ModelImportItem) { i.Name = "bad\nname" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := valid
			tc.change(&item)
			if ValidateModelImport([]ModelImportItem{item}) == nil {
				t.Fatal("accepted invalid item")
			}
		})
	}
	if err := ValidateModelImport([]ModelImportItem{{ModelID: "existing-only"}}); err != nil {
		t.Fatal(err)
	}
	if ValidateModelImport(nil) == nil {
		t.Fatal("accepted empty import")
	}
	if ValidateModelImport([]ModelImportItem{valid, valid}) == nil {
		t.Fatal("accepted duplicate ids")
	}
	if ValidateModelImport(make([]ModelImportItem, 101)) == nil {
		t.Fatal("accepted over 100 items")
	}
}

// TestImportProviderModelsAtomic is opt-in and accepts only a dedicated test
// database. It never uses the shared reserve test database or Redis.
func TestImportProviderModelsAtomic(t *testing.T) {
	dsn := os.Getenv("MODEL_IMPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("MODEL_IMPORT_TEST_DSN is required")
	}
	cfg, err := mysqldriver.ParseDSN(dsn)
	if err != nil || !strings.HasPrefix(cfg.DBName, "ai_gateway_import_test_") {
		t.Fatal("use a dedicated ai_gateway_import_test_* database")
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
	if err := db.AutoMigrate(&Provider{}, &Model{}); err != nil {
		t.Fatal(err)
	}
	st := NewStore(db, nil)
	ctx := context.Background()
	stamp := fmt.Sprintf("import-%d", time.Now().UnixNano())
	p := Provider{Name: stamp, BaseURL: "http://localhost/v1", UpstreamKey: "test", Models: `["*"]`, Status: 0}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	ids := []string{stamp + "-one", stamp + "-two", stamp + "-other"}
	defer db.Where("model_id IN ?", ids).Delete(&Model{})
	defer db.Delete(&p)
	contextLen := 8192
	price := int64(0)
	item := func(id string) ModelImportItem {
		return ModelImportItem{ModelID: id, ContextLen: &contextLen, InputPriceCent: &price, OutputPriceCent: &price}
	}
	snapshot, err := st.GetProviderForDiscovery(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	// A wildcard alone must never grant import authority.
	if _, err := st.ImportProviderModels(ctx, *snapshot, []ModelImportItem{item(ids[0])}, nil); err == nil {
		t.Fatal("wildcard authorized unknown model")
	}
	// Failure in a later selected item must roll back both earlier SKU and routing.
	if _, err := st.ImportProviderModels(ctx, *snapshot, []ModelImportItem{item(ids[0]), {ModelID: ids[1]}}, map[string]bool{ids[0]: true, ids[1]: true}); err == nil {
		t.Fatal("missing prices accepted")
	}
	var count int64
	db.Model(&Model{}).Where("model_id = ?", ids[0]).Count(&count)
	if count != 0 {
		t.Fatal("partial SKU survived rollback")
	}
	current, err := st.GetProviderForDiscovery(ctx, p.ID)
	if err != nil || current.Models != snapshot.Models {
		t.Fatal("routing survived rollback")
	}
	result, err := st.ImportProviderModels(ctx, *snapshot, []ModelImportItem{item(ids[0])}, map[string]bool{ids[0]: true})
	if err != nil || len(result.Created) != 1 || result.Created[0].Tags != "[]" || result.Created[0].Status != 0 {
		t.Fatalf("created=%+v err=%v", result, err)
	}
	db.Model(&Model{}).Where("model_id = ?", ids[0]).Updates(map[string]any{"input_price_cent": 321, "status": 1})
	current, err = st.GetProviderForDiscovery(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	result, err = st.ImportProviderModels(ctx, *current, []ModelImportItem{{ModelID: ids[0]}}, nil)
	if err != nil || len(result.Created) != 0 || len(result.Skipped) != 1 || result.Skipped[0].InputPriceCent != 321 || result.Skipped[0].Status != 1 {
		t.Fatalf("retry=%+v err=%v", result, err)
	}
	current, err = st.GetProviderForDiscovery(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	db.Model(&Provider{}).Where("id = ?", p.ID).Update("upstream_key", "changed")
	if _, err := st.ImportProviderModels(ctx, *current, []ModelImportItem{item(ids[1])}, map[string]bool{ids[1]: true}); !errors.Is(err, ErrProviderChanged) {
		t.Fatalf("stale snapshot error=%v", err)
	}
	for _, status := range []int8{1, 2, ProviderDeleted} {
		db.Model(&Provider{}).Where("id = ?", p.ID).Update("status", status)
		if _, err := st.ImportProviderModels(ctx, *current, []ModelImportItem{item(ids[1])}, map[string]bool{ids[1]: true}); err == nil {
			t.Fatalf("imported into status %d", status)
		}
	}
}
