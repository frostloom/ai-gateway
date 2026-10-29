package taskbench

import "fmt"

const windowStoppedV7PreviousSHA = "ccf528106789daf05a61c8d8f4e5f72d3a29eea65d78a6f81afb85f4e1ea45ad"
const windowStoppedV7DescriptorSHA = "2a78a0188ffd5072f75aeb51c9d9d54c5fdd46edc18a521571aa8aaccf38e1df"
const windowStoppedV7AuditSHA = "1650ef1e944bcb1831bf098abc3ddff38d3d49c5fa760638d61ce3149352d7a0"
const windowStoppedV7InventorySHA = "e91797058a0bf95e47ae7d9884839a48aee8fdaa112e9a6ed801195b55a6bdcf"
const windowStoppedV7ManifestSHA = "830325d797a3964f93c4dc5c43e8fa116a6747a0cbbb264344a2d0540aa4d2b2"
const windowStoppedV7ScopeSHA = "8946fee7acc5c91408b8b23166ff3fed5173d2aeca24bff2332b71f10915d1e4"
const windowStoppedV7ExecutionSHA = "f89b0a7fa025004dd330c954975702226eb32bd81025b5a15effa37afb9fd713"

func windowStoppedV7ClaimPins() [11]string {
	return [11]string{
		"7d13baa87b84c4dfebf8dd177c554be25d2284bfa3823fe3fe1e163bb4e96669",
		"2fc146ef2b553c7ea4686c97f7e1e4990debd316bad0d3fdd5be2e8a9c300c79",
		"682755e963ed11df07b731ececc1c7fbf476f63f8c0d84400253eddb95ce66cb",
		"9210bd78b22d936d97b14786b9a3b352e1e07386cc61f349dda1195bd04791f2",
		"040ac83572a094d084c5db8f80739943f266dc6d819ce136d7a71ecd627057d4",
		"6cddf9f3de0979b948fa584afcdabaaaba5065ea831a3a865c0cce08f9a8b71b",
		"c52d890ae1bc273c5f44f0213cb7e938a83f4da82b43d23766182baa60bc2a08",
		"71b39e542d08afb7e5eb1585d51495aa82400a7aaf24805249dc4bc5034db763",
		"3916b0b65ecf18b2939102a7a7af167195863e5a0dfb44f1792cec291e54cbe7",
		"0707b5957be1eb73c7de6bf463910cc37f7254d1446749b095c3b50ec4cdbdb3",
		"711d6afa300a0ce51b6865cb5f47b902ab6956f0fa6a85cf21476be3f894c590",
	}
}
func expectedStoppedV7WindowBaseline() fullWindowBaseline {
	b := expectedStoppedV6WindowBaseline()
	for i, sha := range windowStoppedV7ClaimPins() {
		b.ClaimSHA256[fmt.Sprintf("full1220-v7-20261030/window-%06d", i+1)] = sha
	}
	b.PermanentModel, b.PermanentJEV = 51200203, 399529
	b.RemainingModel, b.RemainingJEV = b.TotalModel-b.PermanentModel, b.TotalJEV-b.PermanentJEV
	b.ActualModelCalls, b.ActualJEVCalls = 3489, 890
	return b
}
