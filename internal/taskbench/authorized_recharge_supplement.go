package taskbench

const RechargeSupplementID = "recharge-supplement-20261024"
const RechargeCancelClaimSHA = "a9ce7a22d79e4e42e8ded0b932c34648df3db0d0574ad0710c098d2b02cbc8f4"

// This fourth fixed allocation retains all three consumed recharge reservations.
// It is a separate frozen clarification case, not a retry of previous scopes.
func checkedRechargeSupplementScope(schema, id string) (cashRetestScope, bool) {
	if schema != "recharge-supplement-authorization.v1" || id != RechargeSupplementID {
		return checkedRefundCapScope(schema, id)
	}
	return cashRetestScope{
		ID: id, BillingSchema: "recharge-supplement-billing-attestation.v1", ReceiptSchema: "authorized-recharge-supplement.v1",
		CaseKey: "recharge-001/supplement_then_confirm", ModelCalls: 12, ModelMicroCNY: 405504,
		CustomerRevision:         "262a47f25572ad77ef38f68d00fb8703d0f45e53",
		RuntimeContractSHA:       "624a53c69b2fece1f705332ca208ee854be0588decad986d4dee42355ad46477",
		BudgetCeilingSHA:         RechargeBudgetCeilingSHA,
		OfflineManifestSHA:       "6c8a04a705931816c74425effb17a4856c507ba48ce77dcad17d31182119b578",
		OfflineAcceptanceSHA:     "f06c40c02c99f182c86d845e74750b50135a4dfed07c107ad0ed48de2a686845",
		DiagnosticSourceRevision: RechargeDiagnosticSourceRevision,
		DiagnosticManifestSHA:    RechargeDiagnosticManifestSHA,
	}, true
}
