package taskbench

import "fmt"

func validateV11ClaimDeclaration(d fullWindowClaimDeclaration) error {
	if !comparisonDigest(d.ScopeSHA256) || !comparisonDigest(d.PolicySHA256) || d.ScheduleSHA256 != windowV10ScheduleSHA || len(d.Windows) != 511 || d.ModelTotal != 123827032 || d.JEVTotal != 2037474 {
		return fmt.Errorf("v11 exact full declaration required")
	}
	caps := make([]fullWindowCaps, len(d.Windows))
	var model, jev int64
	ordinal, mc, jc := 1, 0, 0
	for i, w := range d.Windows {
		if w.ID != fmt.Sprintf("window-%03d", i+1) || w.FirstOrdinal != ordinal || w.LastOrdinal < w.FirstOrdinal || w.LastOrdinal-w.FirstOrdinal >= 12 || w.LastOrdinal > 3660 || w.ModelCap < 1 || w.ModelCap > d.ModelTotal-model || w.JEVCap < 1 || w.JEVCap > d.JEVTotal-jev || w.MaxJEVCalls < 1 || w.MaxJEVCalls > 12 || w.MaxModelCalls != 10*w.MaxJEVCalls {
			return fmt.Errorf("v11 full ordered windows required")
		}
		caps[i] = w.fullWindowCaps
		model += w.ModelCap
		jev += w.JEVCap
		mc += w.MaxModelCalls
		jc += w.MaxJEVCalls
		ordinal = w.LastOrdinal + 1
	}
	if windowV8DeclarationSHA(caps) != windowV9CapsSHA || ordinal != 3661 || model != d.ModelTotal || jev != d.JEVTotal || mc != 58860 || jc != 5886 {
		return fmt.Errorf("v11 fixed full caps required")
	}
	return nil
}
