package taskbench

const RechargeConfirmID = "recharge-confirm-20261023"
const RechargeBudgetCeilingSHA = "4669a31fe6f74f7f6901ed9a4dcb114016d7ed4aac85be949d9b0b6e74c0c00b"

// This is the only executable scope allocated against the new cumulative
// ceiling. Its permanent claim reserves 337920/2688 micro currency units even
// on failure. Future allocations must account for this reservation first.
func checkedRechargeScope(schema, id string) (cashRetestScope, bool) {
	if schema != "recharge-confirm-authorization.v1" || id != RechargeConfirmID {
		return cashRetestScope{}, false
	}
	return cashRetestScope{
		ID: id, BillingSchema: "recharge-confirm-billing-attestation.v1", ReceiptSchema: "authorized-recharge-confirm.v1",
		CaseKey: "recharge-002/confirm", ModelCalls: 10, ModelMicroCNY: 337920,
		CustomerRevision:     "056a760033186e4b768b320e50ec7b7186a32157",
		RuntimeContractSHA:   "35c117c3dcee33a783e074712d6836236dd1f6a9d4a798fdfd01f0c92e95bce1",
		BudgetCeilingSHA:     RechargeBudgetCeilingSHA,
		OfflineManifestSHA:   "3b6ffcf29430361abfff62cbabcd0cd14ba8a4158d7281cb304a50442e4c0637",
		OfflineAcceptanceSHA: "29c6a248afa1650b08ab9e3bbaed7301280ec1bb7fa1a87f969321d56237ae4d",
	}, true
}
