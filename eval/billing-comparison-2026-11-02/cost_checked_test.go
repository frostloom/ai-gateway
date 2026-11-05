package comparison

import (
	"math"
	"math/big"
	"math/rand"
	"testing"

	"github.com/frostloom/ai-gateway/internal/pkg/quota"
)

func TestCostCheckedAgainstBigInt(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 5000; i++ {
		p, c, in, out := rng.Int63(), rng.Int63(), rng.Int63(), rng.Int63()
		a := new(big.Int).Mul(big.NewInt(p), big.NewInt(in))
		b := new(big.Int).Mul(big.NewInt(c), big.NewInt(out))
		a.Add(a, b).Quo(a, big.NewInt(1000000))
		got, err := quota.CostCentChecked(p, c, in, out)
		if a.IsInt64() {
			if err != nil || got != a.Int64() {
				t.Fatalf("i=%d got=%d err=%v want=%v", i, got, err, a)
			}
		} else if err == nil {
			t.Fatalf("i=%d expected overflow got=%d", i, got)
		}
	}
	for i := 0; i < 5000; i++ {
		p, c, in, out := rng.Int63n(1000000001), rng.Int63n(1000000001), rng.Int63n(10000000001), rng.Int63n(10000000001)
		a := new(big.Int).Mul(big.NewInt(p), big.NewInt(in))
		b := new(big.Int).Mul(big.NewInt(c), big.NewInt(out))
		a.Add(a, b).Quo(a, big.NewInt(1000000))
		got, err := quota.CostCentChecked(p, c, in, out)
		if err != nil || got != a.Int64() {
			t.Fatalf("representable i=%d got=%d err=%v want=%v", i, got, err, a)
		}
	}
	for _, tc := range []struct{ p, c, in, out int64 }{{-1, 0, 1, 1}, {1, -1, 1, 1}, {1, 1, -1, 1}, {1, 1, 1, -1}} {
		if _, err := quota.CostCentChecked(tc.p, tc.c, tc.in, tc.out); err == nil {
			t.Fatal("negative accepted", tc)
		}
	}
	if got, err := quota.CostCentChecked(1000000000, 0, 10000000000, 1); err != nil || got != 10000000000000 {
		t.Fatal(got, err)
	}
	if _, err := quota.CostCentChecked(math.MaxInt64, math.MaxInt64, math.MaxInt64, math.MaxInt64); err == nil {
		t.Fatal("overflow accepted")
	}
	if _, err := quota.ReserveCostCentChecked(math.MaxInt64, 0, 1000000, 0); err == nil {
		t.Fatal("reserve overflow accepted")
	}
	if got, err := quota.ReserveCostCentChecked(8334, 0, 1000000, 1000000); err != nil || got != 10000 {
		t.Fatal(got, err)
	}
}
