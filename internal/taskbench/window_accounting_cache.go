package taskbench

import "fmt"

// settlementCost uses a complete validated cache pair only when explicitly
// enabled by the provider rate. Admission always uses the unchanged cost().
// Rate and token bounds keep the weighted numerator within int64.
func (p windowProviderLimits) settlementCost(u *windowUsage) (int64, error) {
	if err := p.validate(false); err != nil {
		return 0, err
	}
	if p.CacheHitInputRate < 0 || p.CacheHitInputRate > p.InputRate || !windowUsageValid(p, u) {
		return 0, fmt.Errorf("invalid bounded settlement usage/rate")
	}
	if p.CacheHitInputRate == 0 || u.CacheHit == nil {
		return p.cost(*u.Input, *u.Output), nil
	}
	input := (*u.CacheMiss*p.InputRate + *u.CacheHit*p.CacheHitInputRate + 999999) / 1000000
	output := (*u.Output*p.OutputRate + 999999) / 1000000
	return input + output, nil
}
