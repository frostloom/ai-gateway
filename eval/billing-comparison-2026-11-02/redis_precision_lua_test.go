package comparison

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	redis "github.com/redis/go-redis/v9"
)

func TestCashLuaSafeIntegerBoundaries(t *testing.T) {
	_, r := isolatedDB(t)
	ctx := context.Background()
	id := fmt.Sprintf("lua-boundary-%d", time.Now().UnixNano())
	balance := id + ":balance"
	guard := id + ":guard"
	limit := int64(99999999999999)
	read := func(name string) *redis.Script {
		p := filepath.Join("..", "..", "internal", "billing", "store", "lua", name)
		b, e := os.ReadFile(p)
		if e != nil {
			t.Fatal(e)
		}
		return redis.NewScript(string(b))
	}
	reserve, settle, compensate := read("reserve.lua"), read("settle.lua"), read("compensate.lua")
	if err := r.Set(ctx, balance, limit, 0).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := reserve.Run(ctx, r, []string{balance, guard}, int64(1), "owner").Result(); err != nil {
		t.Fatal(err)
	}
	if v, err := r.Get(ctx, balance).Int64(); err != nil || v != limit-1 {
		t.Fatal(v, err)
	}
	if _, err := compensate.Run(ctx, r, []string{balance, guard}, int64(1), "owner").Result(); err != nil {
		t.Fatal(err)
	}
	if v, err := r.Get(ctx, balance).Int64(); err != nil || v != limit {
		t.Fatal(v, err)
	}
	if err := r.Del(ctx, guard).Err(); err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, balance, 0, 0).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := settle.Run(ctx, r, []string{balance, guard}, -limit).Result(); err != nil {
		t.Fatal(err)
	}
	if v, err := r.Get(ctx, balance).Int64(); err != nil || v != limit {
		t.Fatal(v, err)
	}
	if v, err := r.Get(ctx, guard).Result(); err != nil || v != "v2:-99999999999999" {
		t.Fatal(v, err)
	}
	if _, err := settle.Run(ctx, r, []string{balance, guard}, -limit).Result(); err != nil {
		t.Fatal(err)
	}
	if v, err := r.Get(ctx, balance).Int64(); err != nil || v != limit {
		t.Fatal(v, err)
	}
	if err := r.Del(ctx, guard).Err(); err != nil {
		t.Fatal(err)
	}
	amount := int64(12345678901234)
	if _, err := settle.Run(ctx, r, []string{balance, guard}, amount).Result(); err != nil {
		t.Fatal(err)
	}
	marker, err := r.Get(ctx, guard).Result()
	if err != nil || marker != "v2:12345678901234" {
		t.Fatal(marker, err)
	}
	if _, err := settle.Run(ctx, r, []string{balance, guard}, amount).Result(); err != nil {
		t.Fatal(err)
	}
	if v, err := r.Get(ctx, balance).Int64(); err != nil || v != limit-amount {
		t.Fatal(v, err)
	}
	if err := r.Del(ctx, guard).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := settle.Run(ctx, r, []string{balance, guard}, -amount-1).Result(); err == nil {
		t.Fatal("refund beyond safe maximum accepted")
	}
	if n, err := r.Exists(ctx, guard).Result(); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	if v, err := r.Get(ctx, balance).Int64(); err != nil || v != limit-amount {
		t.Fatal(v, err)
	}
	if err := r.Set(ctx, guard, "v2:1e+14", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := settle.Run(ctx, r, []string{balance, guard}, int64(1)).Result(); err == nil {
		t.Fatal("unsafe legacy guard accepted")
	}
	if err := r.Set(ctx, balance, limit, 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, guard, "owner", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := compensate.Run(ctx, r, []string{balance, guard}, int64(1), "owner").Result(); err == nil {
		t.Fatal("unsafe compensation accepted")
	}
	if v, err := r.Get(ctx, guard).Result(); err != nil || v != "owner" {
		t.Fatal(v, err)
	}
	if v, err := r.Get(ctx, balance).Int64(); err != nil || v != limit {
		t.Fatal(v, err)
	}
	t.Log("14-digit operations and replay exact; unsafe refund and legacy scientific guard fail closed")
}
