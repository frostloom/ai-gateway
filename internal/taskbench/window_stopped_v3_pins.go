package taskbench

import "fmt"

// Exact original bytes; descendant records and native bodies are re-read too.
func windowStoppedV3ArtifactPins() map[string]string {
	return map[string]string{
		"actual-manifest.json":                        "0243bbc33f2985ed0e942235b15311525dab1d082e4713570a6c5521688640d9",
		"actual-owner-exit.json":                      "cee0c996ba85c5675acbed1b2bcd2875ff398ebbf3b3702b32318bbf16b4b6f3",
		"campaign/checkpoint-000001.json":             "5a8e55c89513cbc3f67b91e321ca8c7ae4e5101a89991b75d2f9d6f7b0b504f9",
		"campaign/checkpoint-000002.json":             "e23f4d5a57cd43697c83537e9d96cd031017a19ee2104fd7856088a5c596325e",
		"campaign/checkpoint-000003.json":             "f594bc5b10228916ca81d7693cba0509cc726f53334bc6c36cbdca1b389f14af",
		"campaign/checkpoint-000004.json":             "94d58e0a04be9db6e9cd8fa8747090d23c694b5169c282bcd483f5350a357714",
		"campaign/checkpoint-000005.json":             "35ef572cefb7190520c9428768052a55dbbccb73313fce2c18ab2e8d21f8140f",
		"campaign/checkpoint-000006.json":             "075337179c89969c664f68d76912ead9becb260009932d1b0f888518b5341661",
		"campaign/checkpoint-000007.json":             "d0a6d1a540eaf217fc7dd4b7a3fc4f2aeb6a8a6ccaf7f476a9f71eacb2e2d84c",
		"campaign/checkpoint-000008.json":             "f7621bfa7a02673f596c43c6eb0fa430b205f678716bf87ca53237979b696b3a",
		"campaign/claim-000001.json":                  "5cb4f10bd990e073b71ac039d7d09670d732de72cb2831422444dc0e525373e1",
		"campaign/claim-000002.json":                  "b46344d00c277ce8c3d7a4ad317fd23d97f77a2f0e5d50960422affd51535dd4",
		"campaign/claim-000003.json":                  "37d8e559963bf116dbafe8c18f059e77125f8be8cc604fe5d6d4b2b822f26088",
		"campaign/claim-000004.json":                  "d9cb74a2376dc827744cc929eda9e39547b03ed0a20fa0ea5e8bb8aca0ba262d",
		"campaign/claim-000005.json":                  "735f6ad0fb8a3173f6d1db6fc012260fd9040c4357b00b16824ab28df4c95998",
		"campaign/claim-000006.json":                  "9ab2594ef283fa01446e55d7c5598397076c9ef0ee9e88cbd5e5efb2a1ca5493",
		"campaign/claim-000007.json":                  "503ca8aaa9a4ae807dc4bd98b196414222083092751e5177ca92ce12361df926",
		"campaign/claim-000008.json":                  "54c227dccb982dcf80b19d9603b6d8eba6765a365463cbaf23ff5259547a1cc7",
		"campaign/claim-000009.json":                  "0f698f0bf29550cee7052d9ca04f94ef76592942f858b4d84e44a7651f5df6b1",
		"campaign/ledger-000001/accounting.jsonl":     "1fb724834d86e2cd0d71a92afaccb4c1eddcfaaf4df995ac2ba10a994a819adc",
		"campaign/ledger-000002/accounting.jsonl":     "55a01c3549bfa84a8476afe6eb72fc7b092789f42862671c248bed8ec56cfced",
		"campaign/ledger-000003/accounting.jsonl":     "1716799ecebc2d98383b4ef58b57eab82b421b37dc6ae9092321cdf2c7e432fb",
		"campaign/ledger-000004/accounting.jsonl":     "dd14d761eef862dbebc43f9727ea965b7838e4605ecbf78e8ed7c914b299bbce",
		"campaign/ledger-000005/accounting.jsonl":     "cc0c39dcc22f6da6bde19275ad2e34d48500723cc66e4e4886dfef64797b150a",
		"campaign/ledger-000006/accounting.jsonl":     "6b3fff62c463ff10493a3a187d3f28081a02ca5494615fac9a2d2c63a05f9302",
		"campaign/ledger-000007/accounting.jsonl":     "69aa2154ccd311d5f40ffdc31f67014f9f301f53f652d9cbe042cebb0254088e",
		"campaign/ledger-000008/accounting.jsonl":     "a844e020c68164afdd79dffd1ac70217f1555070330a3e7ff8280e88701faea8",
		"campaign/ledger-000009/accounting.jsonl":     "fb0bdf6154e6cbf1aab774efc7cd81b36c9fac7db2e9f46b9d1a010a26134755",
		"records/startup.json":                        "94d263e8f7abd368f3ab1e5f6be25de0e12adaafb1a3b4689534433811ef73a1",
		"records/terminal.json":                       "6305f2d0998e3617e005e2b706f18084bcfcf333e4bd4825b188da5f5a5c326a",
		"records/window-000001-summary.json":          "947205c27cf6301851da7abb17f1aa6519bb0871d1fe65826e07456780fd6a3b",
		"records/window-000001/payload-manifest.json": "41c6b8d86527de6076226e26375b13e729c72c52d295a1f4d833f55f92c628b2",
		"records/window-000001/receipt.json":          "03e305cf75b7208bf27a0bf13934852e1c67a5dfa40e55cfc59c9140637d2955",
		"records/window-000002-summary.json":          "571ee2ed134fe1652995ca45501b6c1370fe50ad67edce13d9f7f85426959598",
		"records/window-000002/payload-manifest.json": "bba4769139bfd2d3e1a4abe6e382b60f784845ec95a9a2a93b98ed7865dccddd",
		"records/window-000002/receipt.json":          "047f3e70540e0268185282d7edf8713066cb030aeb36da75fb71f3ec7bce4117",
		"records/window-000003-summary.json":          "77494abcd64315cc33c3d842f0ef8d486af7af7ba054f1ce82fe3b3457c14c0a",
		"records/window-000003/payload-manifest.json": "c4abb54059ef0c7a52bb98c6f2ab1955843dffcc4bcac09ac59dbc753bf966e4",
		"records/window-000003/receipt.json":          "f7c5179310021218573d78373738298bcb3e729475fb8e112b6127f280fd3d05",
		"records/window-000004-summary.json":          "4286319546f52ef1be0f57d1920544c3ef7e9d542734ccdb99dc0f0df85fad14",
		"records/window-000004/payload-manifest.json": "30427cfbbeaa4b072e31b2ae0390bae21074dba159fbe3d9261080219f0ec082",
		"records/window-000004/receipt.json":          "bd2424c561d6d9d95321147d1c6c2c97e981a505adeb659b619a81f66f1cb8a6",
		"records/window-000005-summary.json":          "f9551d7c041cc36038b872440315740cb56f55c5258284dc79e1046faa2ebd0a",
		"records/window-000005/payload-manifest.json": "4d32a5a8fcb179a4497cf7da3bee9260341d9555a2b6e478f1e5d91b006aca3e",
		"records/window-000005/receipt.json":          "432068bad2c59431ea7880269db2676f56eee68517250abb224701cc94ffb3b2",
		"records/window-000006-summary.json":          "f1d911127e5763d550c89f3c63f67733fe952535c81dda60de8e5c2d869c33e9",
		"records/window-000006/payload-manifest.json": "2e77f0820994658e1551e94222ad859567dc05904c39f3b90d2fcd2b1398597b",
		"records/window-000006/receipt.json":          "4ea6209ae7a84ef55a9cd7254a4a935b62502c6c5bf413adf5b09cf9436f93c2",
		"records/window-000007-summary.json":          "4f1cbe5478e29b4d866b378e4b2a81b91a446950b2e150ce2a7be992852c4d54",
		"records/window-000007/payload-manifest.json": "21efedc41831058593e6bd3aae725c97c5b47dd398dc2f296ab385a417c74e89",
		"records/window-000007/receipt.json":          "857e7f40d93b610174748e519758dcd79f4d818eb2984bc81152ed352df4bf59",
		"records/window-000008-summary.json":          "125e901160abb4b4d0de109db503695d03229fe7c1c56ab06e8b162b7a9afc25",
		"records/window-000008/payload-manifest.json": "2512ff4642b87af9a5175ca527d2535ae856c366ec747c47694b740c1c71816d",
		"records/window-000008/receipt.json":          "26743e1362e46d848a49cce83d7ec87345636b4a8364c7e3f5e37aa57b998ad3",
		"records/window-000009-summary.json":          "dfb0b36859aff1df53d9f5871bd6ec420bc1c470bf98b9b088197ff271133370",
		"records/window-000009/payload-manifest.json": "040abf42372bd82d78c70f2122c241da49fbdb55f307a90f173014080dd4ef1a",
		"records/window-000009/receipt.json":          "1cac2d5e326ce743ddf499b036a4ae87f11f5572041631f7da0ddf91f40a20b0",
	}
}

func expectedStoppedV3WindowBaseline() fullWindowBaseline {
	b := expectedStoppedV2WindowBaseline()
	for i := 1; i <= 9; i++ {
		b.ClaimSHA256[fmt.Sprintf("full1220-v3-20261028/window-%06d", i)] = windowStoppedV3ArtifactPins()[fmt.Sprintf("campaign/claim-%06d.json", i)]
	}
	b.TotalModel, b.TotalJEV = 105000000, 1000000
	b.PermanentModel, b.PermanentJEV = 38888170, 308003
	b.RemainingModel, b.RemainingJEV = 66111830, 691997
	b.ActualModelCalls, b.ActualJEVCalls = 1791, 445
	return b
}
