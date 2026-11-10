package store

import "testing"

func TestSupportedCatalog(t *testing.T) {
	models := []Model{{ModelID: "a"}, {ModelID: "b"}, {ModelID: "c"}}
	providers := []Provider{{Status: 0, Weight: 1, BaseURL: "https://example.com", Models: `["a"]`}, {Status: 1, Weight: 1, BaseURL: "https://example.com", Models: `["b"]`}, {Status: 0, Models: `["c"]`}}
	got := supportedCatalog(models, providers)
	if len(got) != 1 || got[0].ModelID != "a" {
		t.Fatalf("unexpected catalog %+v", got)
	}
	providers[0].Models = `["*"]`
	if len(supportedCatalog(models, providers)) != 0 {
		t.Fatal("routing wildcard must not advertise unconfigured models")
	}
	providers[0].Weight = 0
	if len(supportedCatalog(models, providers)) != 0 {
		t.Fatal("zero-weight upstream advertised")
	}
	providers[0].Weight = 1
	providers[0].Models = `[" a "]`
	if len(supportedCatalog(models, providers)) != 0 {
		t.Fatal("model name normalization diverges from router")
	}
	providers[0].Models = `invalid`
	if len(supportedCatalog(models, providers)) != 0 {
		t.Fatal("malformed config must not advertise models")
	}
}

func TestValidateConsolePlan(t *testing.T) {
	good := Plan{Name: "月卡", PlanType: PlanTypeRecurring, PriceMoney: 29, ValidityDays: 30, RefreshHours: 5, TierQuota: `{"轻量":10000}`}
	if err := ValidateConsolePlan(&good); err != nil {
		t.Fatal(err)
	}
	for _, quota := range []string{`null`, `[]`, `{}`, `{"x":-1}`, `{"x":1.5}`, `{"":100}`, `{"x":0}`} {
		p := good
		p.TierQuota = quota
		if ValidateConsolePlan(&p) == nil {
			t.Errorf("accepted quota %s", quota)
		}
	}
	p := good
	p.PriceMoney = -1
	if ValidateConsolePlan(&p) == nil {
		t.Fatal("accepted negative price")
	}
}
