package taskbench

import "fmt"

const windowStoppedV8DescriptorSHA = windowV9BaselineSHA
const windowStoppedV8ApprovalSHA = "22a9ec8c97ded148879f4a30abaa2a0e8370371ecd7fecb56a85988f89943a57"
const windowStoppedV8ManifestSHA = "a6e9eee26e6cdd044a05c2b23fb2fdfb4410643f3147970b10dc8139d4c98527"
const windowStoppedV8AuditSHA = "27390161e46f4effdefb6e4407c6924f365481992757a9950981a28a766230ca"
const windowStoppedV8InventorySHA = "1a6d25b3c97ac8d56e54dbdc24e28db19c491f5a0c94cc0f31d84d2131c58218"
const windowStoppedV8SourceConfirmationSHA = "fdd9e7096fb09e35d24983d32eb9317e2fc54294e6fb6a9d3690461458e9c3ea"
const windowStoppedV8OwnerExitSHA = "24454c796b205dbf91ddc534aad4b210911bbb6d5ac37aba9305887366dfa9de"

func windowStoppedV8ClaimPins() [2]string {
	return [2]string{"fd552b1caff16c068f6f7636062e945ca28faaa4abf04040539447b7baf43f1f", "aa745b44830a8417dcdd39f208c58faadccc44c0184745682040833287e435b8"}
}
func expectedStoppedV8WindowBaseline() fullWindowBaseline {
	b := expectedStoppedV7WindowBaseline()
	b.TotalModel, b.TotalJEV = 200000000, 4000000
	for i, sha := range windowStoppedV8ClaimPins() {
		b.ClaimSHA256[fmt.Sprintf("full1220-v8-20261030/window-%06d", i+1)] = sha
	}
	b.PermanentModel, b.PermanentJEV = 52654504, 408701
	b.RemainingModel, b.RemainingJEV = b.TotalModel-b.PermanentModel, b.TotalJEV-b.PermanentJEV
	b.ActualModelCalls, b.ActualJEVCalls = 3557, 918
	return b
}
