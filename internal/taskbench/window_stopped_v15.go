package taskbench

import (
	"context"
	"fmt"
)

const windowStoppedV15ClaimPrefix = "full3660-v15-root2-20261107-once"

// These fixed original claim hashes are DATA, not live execution authority.
var windowStoppedV15ClaimHashes = [...]string{
	"c1e761b9995149371299539ca7877767335df3e72cc81fffbeef2fd59d23c89c",
	"92bf818feea2ee1bd082702a1283f73c2fae64b29c3eec7d44715ff3130cbb9b",
	"08929597154e258dcc795f7c248c2f277af38b2eeb8dda68e81e6f62d420693c",
	"f5216c858a20ccaef66641daabc75f2e8146513fefd1b5248fe7f5e9d1e04cdb",
	"77e09de72b21a03557dd469a3c10cc010b0dfe3d1baebf9c6511f893e3bddb45",
	"3461865f908d7693727b7ac273702818fecc99a8fad4f188d49782aa86fbc4f3",
	"3c78421e88514555c7cc2d5ec72fe386e5501cce683e0073703333f45ddfb365",
	"b42f451e764693a4efa6545043b0a5158603a81c2186b9c308ee78db2a920f42",
	"2cdd41de042d218d094c8d8b8fd904e022a2c38cb7deabcc801c43a4b0e38801",
	"492823e20f11040817f5747895027d9a02ebcead52ebf056ade5d667396139b1",
	"bf5f39ea3e5a62a99b0b42d0b8fd82c09d94e9752fbfea35fd0a2a7db506abfe",
	"f8cf28d7fdbc414744027f85abc9f71ba705f2c7536acd5993306080b4fedc34",
	"1858ee8648ed98a94f267e078d8a8997ae33947c77bc53e2b43de0fb63df763f",
	"7626222cb49b90df0a444d418ec109ee8dc629b58e2e7bfb43e45fe11b941411",
	"fc5ee3558b67c4500cc86bb409bb5c0a6b76c42862e66f236a14d7a89f9a613b",
	"9576498362c349fd92ba904edbd4d15e94ecac9a2fe26400aad5e7d90149416d",
	"ef6c8a51b0ec9f40be6365ea59ffde14f5235a7f02bd0cf771b339d9cd5b3e55",
	"97fb321903aa4f8a317532ab08e69e120ee7be11d2f7fe3561ca9fe4657e04f3",
	"7b6c8956a6bfe1828b75c8f39af35eaa11a5f75b6072f4e82425ead6cefba7d3",
	"fc1b4958fb380c9c0391307b508a74bff8a3c558e1e0835a4387118ab1ee272a",
	"e300594b3c55663a2f0e4fd4c3a2709571d4b6d455fd05fb0e219c3c7e92ae45",
	"3004e9961ec5c3694c171958283d6b6561c0dff18359ac0bc5df3511ff829cf2",
	"08836bad9ea91b5e46e1f18580cc0218ab4610b44906acb9c311eb3a282a1c84",
	"60dbd9238bd08adc9c904ab47dee3fdb6195550ed3bc3863dd63f19db58a699a",
	"0427536ef0f0422987bd3753124b0126c5ee276128c43b342ffb4a07be816bb0",
	"3df5d9413d9c73534cc7f096779c40de25529046f61b29dacb9c95b5ecb030cf",
	"22758bd35a5344073c5c03031df421fbff9565410bd4029a8f6a9b35f06b54aa",
	"e19131a2f38a78d6eb17c5d84782b8016d1ca667be28f1e988845d0ab2d116cb",
	"42295904b3530e13ad153ffcf5c39a908bdbd0aedbeee78ca0d479d62a7e8deb",
	"1c0a9ddc2b9c2441c950784c8cab71d9f2703726a47045d4d8c07da797b65fa2",
	"e83a2989d4f6f564c5b7327bf5b9fb84f4e266f4c251a9699ff95d6c3050025d",
	"ea1be33ff9d61223aee80512c890d41fffa87ede69cb0629f60c22d5e2eef859",
	"b1700f7210b458546990a9bc6c5c0bc88d03935519fd0dc865037e8a397d009c",
	"1b5ab8619c915de4974336395f97108d7d246305c8f2f12392222de04db17d96",
	"50efc562eade1024bc00c3680757c25dbfd056f9fecd524a58910997e821daf3",
	"a45af91532e37f5b08e60060bd304e07940e644e8190aa6ee88a16e71027d8ae",
	"b6ed2ea34d6b35fea34c0d7818b81ab46216923da8e4901e298834d4ed737e11",
	"cc2a06603d00ac3db35b39a13af477fdf8036a353e3bf608e7e810611b334515",
	"8cf8983fc995dd768ff63c2680b6fd9932d552e92e77d1327b37b9c0f09b4f56",
	"30d5eeb442c35794baeb87f1eef70fc1ad7ac855ec527c7160983cb338df68c0",
	"b7fe9b374193013e5712398dde397e0907363b845b67b3e880c4239f18d7d3ea",
	"54d8e000911aa81edda982582a2ae6873893224b6beb2dd4447b1fc0f514a30a",
	"b42a61d55c63401b550a7bc7386d799b5d8a044e7e3a2e277515000c46edae5c",
	"863f5c8ced822f81a6eaa04d52af0d5d6b028fcf60fce216d76439e1b63a3929",
	"15424fd828fa02efd8b7da972a46afb9714c27fb67d7c1a78c5827b19bbd71d3",
	"4a2b3aa591714bdca7a8e1e5a5fb0fe1aa91878ac9019b712428cfde371f87e0",
	"1d72ac8b79b1b2e1297e74b0250c3ad2dadcac4220bbd4fda14786b21f12fcc0",
	"15c98f74f44ecc13f9ede5c812fb000ced341d791dce922a81a22de6cbe6809e",
	"7abae895d0020a8daa9098e3a63cb5b9086c5d72f5a6a1a582573a2d2aade808",
	"762078056e4782444a26ae63b17922e04a3358e9608537d7b7ead63df8414d6c",
	"b3dcee9741cf674c97b67e775d794c3801c84056470f57531f5cf0fc874def64",
	"f2538629fe04c8ae9c1903cf4a1db4906436638614fa1f47c2d422d9c1c0a22b",
	"65878c1998fcb0a76cc88f8be0f3c82c34e37c43ab83a2c0908f7089a14f0c3d",
	"a1e03cc77aff61d389f5a76cc3d3b8291a570747510c28b5c50d941af3ef06e5",
	"05e0dbdf8458e89597108882aafa620ec7d222eecb4cb55dfbce5217751d522b",
	"4034f8614bc0c3e954f310cb12276d80eee227f80b80489537c7d7c7579d31bf",
	"779c15343d1a349945ac0469ccbbb17cc6a38e8de40f5ca42d41afaebb88ed1f",
	"34b97ca97fa70e19e71b638ff9d184e340502af65286ba2aa34583450cd886e6",
	"f9fa7f6539a15e18acd0dedbcb0defcf7c56e9a524479eae2db83290a5efbd97",
	"38169c06ddc1454925644a052663d0c3739b88251d23d50aefd9cd93c7973265",
	"f1d6add58b1a5e0d232476df3757be79beb68b3391e49f139b200cbd537d8d11",
	"4c71f038a437fdbe8a46a77f0be25a6e41a371d0e2997117da07c6fa6457beed",
	"0cea8a77616f02dcac607e46cc8e5e431712dcfb2c0537cf8291a3eaf6297430",
	"90202ce61a83a2c1637cc3ad80b6946867e330f5dc72b28b102fadfec2323317",
	"dcac92c783613dd57e08d3301be6a4ef0483a9a749c90b0a9a7189ec1ec672bd",
	"28ad483a8a71061fd7b0f14b96c91649c3ae0222ac77b7e95ae3124f5facb679",
	"ad4b74f44ba35392eebd2ed1a9dd7fc8132fa3167df37345bec0e3bcd34cb809",
	"873ac2575d81639d2ad5cd448310c2b8c3a5483c1a7980997a2d532070021a89",
	"18f5c48feb7f305a72b7f3babeffc1aeae56f51d0220bcb8cf7e9222a21d366e",
	"fa953cde8f8f1466128dd10ce62d5f513d07ed13480914663f254c951cf18002",
	"a65bdd6fbbd75cdd7d99a2c02029440f75f2f8bab476d53406bd5c99d6fee315",
}

// The concrete constructor is deliberately fail-closed until original bundle,
// source, lock, metadata and signature validation have been integrated.
type windowStoppedV15Baseline struct {
	base                                                      fullWindowBaseline
	previous                                                  windowStoppedV14Baseline
	evidencePath, evidenceSHA256, originalRoot, historicalDir string
}

func expectedStoppedV15WindowBaseline() fullWindowBaseline {
	b := expectedStoppedV14WindowBaseline()
	b.ClaimSHA256 = cloneWindowHistoricalClaims(b.ClaimSHA256)
	b.PermanentModel += 18088007
	b.PermanentJEV += 288216
	b.ActualModelCalls += 1939
	b.ActualJEVCalls += 566
	b.RemainingModel = b.TotalModel - b.PermanentModel
	b.RemainingJEV = b.TotalJEV - b.PermanentJEV
	for i, h := range windowStoppedV15ClaimHashes {
		b.ClaimSHA256[fmt.Sprintf("%s/window-%06d", windowStoppedV15ClaimPrefix, i+1)] = h
	}
	return b
}

func freezeWindowStoppedV15Predecessor(s windowStoppedV14Baseline) windowStoppedV14Baseline {
	s.base.ClaimSHA256 = cloneWindowHistoricalClaims(s.base.ClaimSHA256)
	s.previous = freezeWindowStoppedV14Predecessor(s.previous)
	return s
}

func loadWindowStoppedV15BaselineContext(ctx context.Context, path, sha string) (windowStoppedV15Baseline, error) {
	if ctx == nil {
		return windowStoppedV15Baseline{}, fmt.Errorf("stopped249 context required")
	}
	if err := ctx.Err(); err != nil {
		return windowStoppedV15Baseline{}, err
	}
	return windowStoppedV15Baseline{}, fmt.Errorf("stopped249 constructor not integrated")
}
