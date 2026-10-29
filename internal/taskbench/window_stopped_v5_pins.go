package taskbench

import "fmt"

// Exact R2 original top-level bytes; descendants are read separately.
func windowStoppedV5ArtifactPins() map[string]string {
	return map[string]string{
		"actual-manifest.json":                        "3e80c9fe4bc15fb096a5dbd5a61ca9ad79bb85f7dc98f8855b65a3b947dcb30b",
		"actual-owner-exit.json":                      "7d6229b7f61bcdf712dace2f7e22587c1f4a4b6e592131b2c7b358807de4b047",
		"campaign/checkpoint-000001.json":             "26fda354c5cfbfc252db5955726bb5142f00eda5c48d19495fa73d6922965d39",
		"campaign/checkpoint-000002.json":             "7085889251d8035891cec55deb23d90a0ebf18da697cc1a2515601cf361b0af0",
		"campaign/checkpoint-000003.json":             "215afa82b2e811ab3c163a00e9740fccb2b2c3dd13c84013ea699af72457816a",
		"campaign/claim-000001.json":                  "e439a45da5d9df4d2eec2f1fd56e7df197fe3bf8c21ba7e1f4599379b1e6eaa5",
		"campaign/claim-000002.json":                  "0641dbed0bad21d364eefb04e8b8f12445e3297210b09d6e5e71a6d0c7753a9d",
		"campaign/claim-000003.json":                  "3c1d153b51545d718f2e322ca6eaaa2ec7f6a9e538f5baf1b714a56aa232be49",
		"campaign/claim-000004.json":                  "8207bf55da28cfe920eac539fdf09e5cee1a61c5b54fe4b993647fcb4f47300a",
		"campaign/ledger-000001/accounting.jsonl":     "0e970d2401be26148bc2838ca9b3be4504951d2eacf3f713671d4f78a8468b86",
		"campaign/ledger-000002/accounting.jsonl":     "024bea13c99622dc172d5aaabcde6e3510088ceed4bcb0510783bcd8c861f8fd",
		"campaign/ledger-000003/accounting.jsonl":     "8e3e591e309fd89245bdeb7bcc75248b283b47ffd60c571d90d9c3a3fbd97667",
		"external-verifier-stop.json":                 "10ae8869d432bbbb0bc3aff1dbb8888309d20767e0371f82699d29085d4bbcc8",
		"records/startup.json":                        "94d263e8f7abd368f3ab1e5f6be25de0e12adaafb1a3b4689534433811ef73a1",
		"records/terminal.json":                       "783d8b9f72540368c3ffc1992d7fb4e117616e4a2333ded5d5aede64da107557",
		"records/window-000001-summary.json":          "60734d27c99acbdb26db2351d3d5d94ac8db8bbc844d49b44a9321f80fa655ca",
		"records/window-000001/payload-manifest.json": "0c8102b9d846ff6ec777c8818777796fb9d87d45e473b739ab9b6269c08e19c7",
		"records/window-000001/receipt.json":          "3276476afba8a5f70ae9593e617ebe341604bc0b36806cf785b6eee2c1b330d0",
		"records/window-000002-summary.json":          "1a0d615a72f25cf7d6cfbbc6b8a873fde4cbfc488597cbe3ba7645b30576e596",
		"records/window-000002/payload-manifest.json": "68c8ebc5de539276a37f83b21e8c5d8e2ce64b01d666b21c19fe2be6de248338",
		"records/window-000002/receipt.json":          "514e10d335826afbb43124f69e6f740875c86bf67468b5610f61ba2a592ec02f",
		"records/window-000003-summary.json":          "d20f0fe91154f5074732ad94a44da76b033486291bda200a294e850fedc683c4",
		"records/window-000003/payload-manifest.json": "7c205f97992c9fc6f8f01eee69df04c30245589f0444876edd303963339b6efe",
		"records/window-000003/receipt.json":          "f64ddadd19ed62f470e82dfd7f98a62feb7c607164f46bf28d9f8005c9bf7396",
	}
}

func expectedStoppedV5WindowBaseline() fullWindowBaseline {
	b := expectedStoppedV4WindowBaseline()
	for i := 1; i <= 4; i++ {
		b.ClaimSHA256[fmt.Sprintf("full1220-v5-20261029/window-%06d", i)] = windowStoppedV5ArtifactPins()[fmt.Sprintf("campaign/claim-%06d.json", i)]
	}
	b.PermanentModel, b.PermanentJEV = 42961430, 344493
	b.RemainingModel, b.RemainingJEV = 62038570, 655507
	b.ActualModelCalls, b.ActualJEVCalls = 2292, 585
	return b
}

func expectedStoppedV5ClaimMap() map[string]string {
	return expectedStoppedV5WindowBaseline().ClaimSHA256
}
