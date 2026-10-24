package taskbench

const RechargeCancelID = "recharge-cancel-20261024"
const RechargeDiagnosticClaimSHA = "23047a143039f719531cfe403f1769abda7a6091b2ee51cecf6184eb4768e654"

// This third fixed allocation retains both consumed confirmation reservations.
// It is a new case, not permission to retry either previous evaluation.
func checkedRechargeCancelScope(schema, id string) (cashRetestScope, bool) {
	if schema != "recharge-cancel-authorization.v1" || id != RechargeCancelID {
		return cashRetestScope{}, false
	}
	return cashRetestScope{
		ID: id, BillingSchema: "recharge-cancel-billing-attestation.v1", ReceiptSchema: "authorized-recharge-cancel.v1",
		CaseKey: "recharge-002/cancel", ModelCalls: 10, ModelMicroCNY: 337920,
		CustomerRevision:         "056a760033186e4b768b320e50ec7b7186a32157",
		RuntimeContractSHA:       "35c117c3dcee33a783e074712d6836236dd1f6a9d4a798fdfd01f0c92e95bce1",
		BudgetCeilingSHA:         RechargeBudgetCeilingSHA,
		OfflineManifestSHA:       "0458f4ce65f17983185611a8affb236332d48a4b48bd388d803db64673d03f79",
		OfflineAcceptanceSHA:     "f3a3d5b62b88dd6de7ce08aaf34443627696c12f674f26ff457eb471262122f0",
		DiagnosticSourceRevision: RechargeDiagnosticSourceRevision,
		DiagnosticManifestSHA:    RechargeDiagnosticManifestSHA,
	}, true
}
