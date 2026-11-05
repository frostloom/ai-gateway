package comparison

import (
	"context"
	"encoding/json"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"gorm.io/gorm"
	"os"
	"path/filepath"
	"strings"
)

type localScanResult struct {
	Succeeded int
	Failed    int
}

func scanLocalUsage(ctx context.Context, db *gorm.DB, s *store.Store, dir string) (localScanResult, error) {
	var result localScanResult
	entries, err := os.ReadDir(dir)
	if err != nil {
		return result, err
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() > 65536 {
			result.Failed++
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			result.Failed++
			continue
		}
		var x inboxUsage
		if err := json.Unmarshal(data, &x); err != nil || x.RequestID == "" || filepath.Base(localUsagePath(dir, x.RequestID)) != entry.Name() || x.State != "" {
			result.Failed++
			continue
		}
		if err := putUsageInbox(ctx, db, x); err != nil {
			result.Failed++
			continue
		}
		if err := replayUsageInbox(ctx, db, s, x.RequestID); err != nil {
			result.Failed++
			continue
		}
		result.Succeeded++
	}
	return result, nil
}
