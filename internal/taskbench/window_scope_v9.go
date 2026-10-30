package taskbench

import (
	"encoding/json"
	"fmt"
	"reflect"
)

const windowV9BaselineSHA = "2f87853fe1a8df139a2093e1a34d91eb8758cd6ea547755e02d80bb98d3b2a3c"
const windowV9CapsSHA = "e75a0f693700f12d95bb7bf29a2c32d6d7f11e0890433fca3ad49fbf407a244a"

// V9 reuses the scalar liability format; it confers no execution authority.
func expectedWindowV9BudgetDeclaration() windowV8BudgetDeclaration {
	return windowV8BudgetDeclaration{
		Schema: "full-window-budget-declaration.v1", HumanAuthorized: false,
		BaselineSHA256: windowV9BaselineSHA, BaseCapsSHA256: windowV9CapsSHA,
		RetryPolicySHA256: windowV8DeclarationSHA(expectedWindowV8RetryDeclaration()),
		Parent:            windowV8Amounts{180000000, 2500000}, Historical: windowV8Amounts{52654504, 408701},
		Risk: windowV8Amounts{2023424, 65}, Base: windowV8Amounts{123827032, 2037474},
		MaximumExtras: windowV8Amounts{1495040, 53760}, MaximumReserved: windowV8Amounts{180000000, 2500000},
	}
}

func prepareFullWindowScopeV9(s ConversationSuite, p PairedProfiles, caps []fullWindowCaps, b fullWindowBindings, baselineSHA string) (fullWindowScope, error) {
	if baselineSHA != windowV9BaselineSHA || len(caps) != 511 {
		return fullWindowScope{}, fmt.Errorf("v9 exact stopped46 and511 caps required")
	}
	raw, err := json.Marshal(caps)
	if err != nil || conversationHash(raw) != windowV9CapsSHA {
		return fullWindowScope{}, fmt.Errorf("v9 fixed cap vector required")
	}
	// Use the original schedule constructor for all non-window slot identities and
	// frozen suite/profile checks. Its temporary windows do not escape.
	seed := make([]fullWindowCaps, 37)
	for i := range seed {
		seed[i] = fullWindowCaps{1, 1}
	}
	out, err := prepareFullWindowScopeWithBounds(s, p, 100, seed, b, 37, 37)
	if err != nil {
		return fullWindowScope{}, err
	}
	out.Schema = "full-window-scope.v9"
	out.Width = 12
	out.BaselineEvidenceSHA256 = baselineSHA
	out.Windows = nil
	turns := 0
	for i := range out.Slots {
		slot := &out.Slots[i]
		if slot.UserTurns < 1 || 2*slot.UserTurns > 24 {
			return fullWindowScope{}, fmt.Errorf("v9 case exceeds fixed defined-turn bound")
		}
		if len(out.Windows) == 0 || i-out.Windows[len(out.Windows)-1].FirstOrdinal+1 >= 12 || turns+2*slot.UserTurns > 24 {
			if len(out.Windows) >= len(caps) {
				return fullWindowScope{}, fmt.Errorf("v9 unexpected partition count")
			}
			out.Windows = append(out.Windows, fullWindowDeclaration{ID: fmt.Sprintf("window-%03d", len(out.Windows)+1), FirstOrdinal: i + 1, fullWindowCaps: caps[len(out.Windows)]})
			turns = 0
		}
		w := &out.Windows[len(out.Windows)-1]
		slot.WindowID = w.ID
		w.LastOrdinal = i + 1
		w.MaxModelCalls += 10 * slot.UserTurns
		w.MaxJEVCalls += slot.UserTurns
		turns += 2 * slot.UserTurns
	}
	if len(out.Windows) != 511 {
		return fullWindowScope{}, fmt.Errorf("v9 unexpected partition count")
	}
	raw, err = json.Marshal(out.Slots)
	if err != nil {
		return fullWindowScope{}, err
	}
	out.ScheduleSHA256 = conversationHash(raw)
	budget, retry := expectedWindowV9BudgetDeclaration(), expectedWindowV8RetryDeclaration()
	out.BudgetDeclaration = &budget
	out.RetryDeclaration = &retry
	out.BudgetDeclarationSHA256 = windowV8DeclarationSHA(budget)
	out.RetryPolicySHA256 = windowV8DeclarationSHA(retry)
	return out, nil
}

func validateWindowV9Declarations(scope fullWindowScope) error {
	budget, retry := expectedWindowV9BudgetDeclaration(), expectedWindowV8RetryDeclaration()
	if scope.BudgetApprovalSHA256 != "" || !reflect.DeepEqual(scope.BudgetDeclaration, &budget) || !reflect.DeepEqual(scope.RetryDeclaration, &retry) || scope.BudgetDeclarationSHA256 != windowV8DeclarationSHA(budget) || scope.RetryPolicySHA256 != windowV8DeclarationSHA(retry) {
		return fmt.Errorf("v9 fixed candidate declarations required; no human authority")
	}
	return nil
}
