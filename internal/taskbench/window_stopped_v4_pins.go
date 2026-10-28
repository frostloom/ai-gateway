package taskbench

import "fmt"

// Exact P4 original bytes; claim3 is reserved but has no ledger or report.
func windowStoppedV4ArtifactPins() map[string]string {
	return map[string]string{
		"actual-manifest.json":                        "fb48dd1465a452ffc2dfdb2029a107adfeb351f4c176892a071d07e18b95c0ea",
		"actual-owner-exit.json":                      "0015af89d829c625c3fef4b21219dd3a40682c54308f9426c7e1bc279f268c66",
		"campaign/checkpoint-000001.json":             "7fa907a5665744d565da798dc103ef69566a3b854eeb6c734cec8c362c98251e",
		"campaign/checkpoint-000002.json":             "3df413efd501009ee2c2c7861643fb475fef70a8dc75ee278ef2395e938798d4",
		"campaign/claim-000001.json":                  "58d63365bd94f562642a7e39138da6b347d90b02c283c3aa661274d523b2784e",
		"campaign/claim-000002.json":                  "388914ae974e3c3218060997c6c289b92492d8d489b12beee79be1fad6e4dace",
		"campaign/claim-000003.json":                  "412231c60c4ea4ac8df02377a2dde3f065dfdd84fb73fadaab91bc73ec169dd1",
		"campaign/ledger-000001/accounting.jsonl":     "4a11873361cac6e60384e85b0a68b24a5aa5d8cd162af989a0459f240856c3ba",
		"campaign/ledger-000002/accounting.jsonl":     "0df018d2d7a8cda10f76dd653f933d5379d535946f48aa4f78ae999ac9e2501a",
		"records/startup.json":                        "94d263e8f7abd368f3ab1e5f6be25de0e12adaafb1a3b4689534433811ef73a1",
		"records/terminal.json":                       "13520b9e02f814b7c7e3c9cb2ca8f40793714d2649c14dd9f2acfdc9886bbe29",
		"records/window-000001-summary.json":          "062361b5d10b3add9fd5599382fadc73aef032663b4ba9248e94e35cbaf6aa2e",
		"records/window-000001/payload-manifest.json": "c508a88982262c341ede87b9d1ee62230d96401c2442c88e551d949a51940243",
		"records/window-000001/receipt.json":          "8f19c51266fa81d6dfa468534c8355dd4ac525af6e0217e7c289e6e2bc9f5ceb",
		"records/window-000002-summary.json":          "c38a54516b22488807150666db678860928569388964b8d245c9e9fb647b7b95",
		"records/window-000002/payload-manifest.json": "e7d383685dc544a7dbb6bf02f5b0db9c504de8eb3c2bca9e2375cd7fd2a7e8d6",
		"records/window-000002/receipt.json":          "ced5a0d7ff5c94d4065e0f9702387b199fef02e71e9ed26eabea2423e8aaacc7",
	}
}

func expectedStoppedV4WindowBaseline() fullWindowBaseline {
	b := expectedStoppedV3WindowBaseline()
	for i := 1; i <= 3; i++ {
		b.ClaimSHA256[fmt.Sprintf("full1220-v4-20261029/window-%06d", i)] = windowStoppedV4ArtifactPins()[fmt.Sprintf("campaign/claim-%06d.json", i)]
	}
	b.PermanentModel, b.PermanentJEV = 40625891, 323825
	b.RemainingModel, b.RemainingJEV = 64374109, 676175
	b.ActualModelCalls, b.ActualJEVCalls = 1985, 501
	return b
}
