package taskbench

const RechargeConfirmID = "recharge-confirm-20261023"
const RechargeDiagnosticRetestID = "recharge-confirm-diagnostic-retest-20261023"
const RechargePriorClaimSHA = "2c9d64b98bd985a24da5aed31c049d63311ebdd7f8dc4c862600ccd77e80dcbc"
const RechargeDiagnosticSourceRevision = "5c2f669413c0787ba1bd3a429c9552d628e1a44c"
const RechargeDiagnosticManifestSHA = "8731a95fc1efd01abd76d6448335f86f84ffaa1137165a9bbc446cfffd0cf8b3"
const RechargeBudgetCeilingSHA = "4669a31fe6f74f7f6901ed9a4dcb114016d7ed4aac85be949d9b0b6e74c0c00b"

// These two fixed scopes reserve 675840/5376 micro currency units cumulatively,
// even on failure. The diagnostic scope requires the exact consumed prior
// claim. The cancellation successor accounts for both permanent reservations.
func checkedRechargeScope(schema, id string) (cashRetestScope, bool) {
	if (schema != "recharge-confirm-authorization.v1" || id != RechargeConfirmID) && (schema != "recharge-confirm-authorization.v2" || id != RechargeDiagnosticRetestID) {
		return checkedRechargeCancelScope(schema, id)
	}
	scope := cashRetestScope{
		ID: id, BillingSchema: "recharge-confirm-billing-attestation.v1", ReceiptSchema: "authorized-recharge-confirm.v1",
		CaseKey: "recharge-002/confirm", ModelCalls: 10, ModelMicroCNY: 337920,
		CustomerRevision:     "056a760033186e4b768b320e50ec7b7186a32157",
		RuntimeContractSHA:   "35c117c3dcee33a783e074712d6836236dd1f6a9d4a798fdfd01f0c92e95bce1",
		BudgetCeilingSHA:     RechargeBudgetCeilingSHA,
		OfflineManifestSHA:   "3b6ffcf29430361abfff62cbabcd0cd14ba8a4158d7281cb304a50442e4c0637",
		OfflineAcceptanceSHA: "29c6a248afa1650b08ab9e3bbaed7301280ec1bb7fa1a87f969321d56237ae4d",
	}
	if id == RechargeDiagnosticRetestID {
		scope.BillingSchema = "recharge-confirm-billing-attestation.v2"
		scope.ReceiptSchema = "authorized-recharge-confirm.v2"
		scope.PriorJournalSHA = "c7008d7345bee7a04783dff93fae86586135c6e95fffe7210edda93a15b591ea"
		scope.PriorReviewSHA = "53199923545c6dc8c0e4d24671fa4135c6277377eb4dfb63510f3dcb8bbb9208"
		scope.DiagnosticSourceRevision = RechargeDiagnosticSourceRevision
		scope.DiagnosticManifestSHA = RechargeDiagnosticManifestSHA
	}
	return scope, true
}
