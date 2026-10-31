package taskbench

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// This only checks a declaration. Its dynamic ScopeSHA256/PolicySHA256 must be
// bound to the independently validated frozen scope by the actual runtime.
// No file, financial claim, provider capability, or permission is created here.
func validateV10ClaimDeclaration(d fullWindowClaimDeclaration, composition *windowCompletionDeclarationV10) error {
	expected := expectedWindowCompletionDeclarationV10()
	if !reflect.DeepEqual(composition, &expected) {
		return fmt.Errorf("v10 fixed completion required")
	}
	for _, h := range []string{d.ScopeSHA256, d.PolicySHA256} {
		if !comparisonDigest(h) {
			return fmt.Errorf("missing claim identity")
		}
	}
	if d.ScheduleSHA256 != windowV10ScheduleSHA || len(d.Windows) != 456 || d.ModelTotal != 110191257 || d.JEVTotal != 1818872 {
		return fmt.Errorf("v10 exact suffix claim totals required")
	}
	raw, err := json.Marshal(d.Windows)
	if err != nil || conversationHash(raw) != windowV10SuffixWindowsSHA {
		return fmt.Errorf("v10 fixed suffix window declarations required")
	}
	var model, jev int64
	ordinal, modelCalls, jevCalls := 386, 0, 0
	for i, w := range d.Windows {
		if w.ID != fmt.Sprintf("window-%03d", i+56) || w.FirstOrdinal != ordinal || w.LastOrdinal < w.FirstOrdinal || w.LastOrdinal-w.FirstOrdinal >= 12 || w.LastOrdinal > 3660 || w.ModelCap < 1 || w.ModelCap > d.ModelTotal-model || w.JEVCap < 1 || w.JEVCap > d.JEVTotal-jev || w.MaxModelCalls < 1 || w.MaxModelCalls > 52520-modelCalls || w.MaxJEVCalls < 1 || w.MaxJEVCalls > 5252-jevCalls || w.MaxModelCalls != 10*w.MaxJEVCalls {
			return fmt.Errorf("invalid v10 ordered suffix declaration")
		}
		model += w.ModelCap
		jev += w.JEVCap
		ordinal = w.LastOrdinal + 1
		modelCalls += w.MaxModelCalls
		jevCalls += w.MaxJEVCalls
	}
	if ordinal != 3661 || model != d.ModelTotal || jev != d.JEVTotal || modelCalls != 52520 || jevCalls != 5252 {
		return fmt.Errorf("incomplete v10 suffix totals")
	}
	return nil
}
