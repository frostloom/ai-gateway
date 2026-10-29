package taskbench

// Immutable financial/source authority only. Scores, mutable documentation and
// candidate allocation tools are deliberately not dependencies of this proof.
func windowStoppedV6ArtifactPins() map[string]string {
	return map[string]string{
		"actual-manifest.json":                         "de6d781c1fbf879c9f76b0510f508df4dc29d25f6c71fd14bb617503b88128bf",
		"actual-manifest.json.binary-attestation.json": "bb627a1bee0bf2e2aecf8bd6f72032d1c3a7ec819b44a3792b09199b891a4ad3",
		"actual-owner-exit.json":                       "a86eaf3bd2951b7d520e8757f8524aa4f74ea32fb6ce2e56aef268090c83fa6b",
		"campaign/claim-000001.json":                   "60cfded2cc923d066b6d33d16657bc544ba00ca693fe95d48397b915531f8445",
		"campaign/ledger-000001/accounting.jsonl":      "d77baa8e7e2662f4c4bda5b3b28eb0d492cab53c030d8ce640c7870c63bf5ce3",
		"records/startup.json":                         "94d263e8f7abd368f3ab1e5f6be25de0e12adaafb1a3b4689534433811ef73a1",
		"records/terminal.json":                        "d9995e178d780487f49723070009794e40739b8784d56d2b18004c15357826c8",
		"records/window-000001-summary.json":           "d4f6d47b866e0d9123a4c0225e170c3a69bbe74952ce0740e3e279144de84269",
		"records/window-000001/receipt.json":           "6ba8783cb36cc4e7c7ed9b73dbfc1d59541cb8b4e32421e16fcfebba94ce4d71",
		"records/window-000001/payload-manifest.json":  "53ae056cb88b04ee116e9cac290f10d3c0d0fdb429aebfeeea8f4e4af90c8c9c",
	}
}
func expectedStoppedV6WindowBaseline() fullWindowBaseline {
	b := expectedStoppedV5WindowBaseline()
	b.ClaimSHA256["full1220-v6-20261029/window-000001"] = windowStoppedV6ArtifactPins()["campaign/claim-000001.json"]
	b.TotalModel, b.TotalJEV = 130000000, 1000000
	b.PermanentModel, b.PermanentJEV = 43465965, 349080
	b.RemainingModel, b.RemainingJEV = 86534035, 650920
	b.ActualModelCalls, b.ActualJEVCalls = 2339, 607
	return b
}
