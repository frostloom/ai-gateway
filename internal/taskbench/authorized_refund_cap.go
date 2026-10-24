package taskbench

const RefundCapID = "refund-cap-20261024"
const RechargeSupplementClaimSHA = "824e1e44882b1043bba833ec6af5b88980d6d89b4ce8e4023fd12c68c98fb11c"

// Fifth fixed allocation retains four consumed recharge reservations.
// Original refund balance-cap case; no permission to retry previous scopes.
func checkedRefundCapScope(schema, id string) (cashRetestScope, bool) {
	if schema != "refund-cap-authorization.v1" || id != RefundCapID {
		return cashRetestScope{}, false
	}
	return cashRetestScope{
		ID: id, BillingSchema: "refund-cap-billing-attestation.v1", ReceiptSchema: "authorized-refund-cap.v1",
		CaseKey: "refund-001/balance_cap", ModelCalls: 12, ModelMicroCNY: 405504,
		CustomerRevision:         "50e3c8dab14b4b6cc25322fd340a274e6970200b",
		RuntimeContractSHA:       "a95cacd041830012619a0f4d7fd8208c184aa9e8d45b7ee05afaa239a30e1e8f",
		BudgetCeilingSHA:         RechargeBudgetCeilingSHA,
		OfflineManifestSHA:       "e7918bc7106f72e9de55136d5200045c99da35dfdf99835e707bab6b9a8bc907",
		OfflineAcceptanceSHA:     "7e8f4b9abe2398d87e43fac5f5574536190917588275679970a647c0e1e02fdb",
		DiagnosticSourceRevision: RechargeDiagnosticSourceRevision,
		DiagnosticManifestSHA:    RechargeDiagnosticManifestSHA,
	}, true
}
