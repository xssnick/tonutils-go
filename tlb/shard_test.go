package tlb

import (
	"encoding/hex"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"testing"
)

func TestShardState_LoadFromCell(t *testing.T) {
	tag1 := 0x5f327da5
	//tag2 := 0x9023afe2

	tests := []struct {
		name            string
		tag             int
		leftAndRightRef bool
	}{
		{"blockType1 (tag 0x5f327da5)", tag1, true},
		//{"blockType2 (tag 0x9023afe2)", tag2, false},
	}

	type testStruct struct {
		State any `tlb:"[ShardStateUnsplit,ShardStateSplit]"`
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var testShard testStruct
			var tBuilder cell.Builder
			err := tBuilder.StoreUInt(uint64(test.tag), 32)
			if err != nil {
				t.Fatal(err)
			}
			cellBytes, err := hex.DecodeString("b5ee9c724102b00100132d00245b9023afe2ffffff1100ffffffff00000000000000000173ed4400000001634e93e700001d3677a8f1440173ed4160052303010455cc26aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaac23305e8350c57b37e049b060200480101b20e36a3b36a4cdee601106c642e90718b0a58daf200753dbb3189f956b494b6000102330000000000000000ffffffffffffffff81fa7454b05a2ea2a828636b00480101db29f7a5808e1a673feb2258d777f3005642991b8025b1f92bcd7c498a8bd8ed00020048010124871f46ee0eb1ae00a27d5c29f6cdbcc378c1f4f1380805ff2297c9ed9fcf22000102bf000100f59f3900058edb600003a6cef335e0880000e99c6da994200b9dfdf25ea78c41c94e4584ff2623b917b789f5d0e6a859861410542cb94e76fdc6efde77dc19b6aa8de0d4ad05651468c38cb9f64e1f8cf1351de6d5a7d98d56d6ded0be5c070201200c080201209d09020120620a0201200b9f004801016925c827cdb72656785a860c0ed1b94c1ff9f0614b9e2ed1b0aa1ee8fbb395aa000c0201200e0d00480101912d60694234d59e4645f5d2ebd90e081979a3f6eaf4124bec3980e4547a5094000e0201201f0f0201201e100201201211004801010c275d6749b7c9102256e4abafdecda16a1697f20d26cd0e711bd9d58ef4a2f2000a0201201d130201201c14020120161500480101b13de2fa76c60764833d05264a2e1081609e2dfa9a04bf8d6c5e1162ac7d47cb00070201201b1702012026180201201a1900480101a96f5d75bc79b8d1640e680704965baaa2245e4b3a5ad8e980ef5a3568409418000200480101d52b65c44fcc1a90bbbf8cc01e8ab9c7b6c51f95c2735d6de72a669c1135a836000300480101027742b12159d2d1310044b4a94e1eea928905b045871a52b552b4b4841288400006004801017a3b4493fefcfd2275fa2f6ab01a8db5d70a443fb48dfb00e152545adfb97cbb000700480101c169f7745c95d5f3f6b4e550c15978aaf563631f3a9e6ddaaa361ed4042e4f050009004801018dfe3c99df194f8fec2b5b64b5ef08296b853794a29497c7c425ca62a44695e6000c020120212000480101348a81067d100edaf90feeb18db50c3c315a07c6c0944b52368c30e76a6f40df000b0201207c22004801018540d2166efad6f7a81289ddf3983d3ed177993dce47ccb150f2fcc287428d53000a02138207e9d152c168ba8ab0246303130103f4e8a960b45d455827256300480101827773c365eccfd6cb46a3f783a09f1aba77ce1a0a4d62048569e9b845e955f8016b00480101df4611dc79f46dc700809e0c3140796be5ca9572c3f3fab70ddbe6a5460bf4900003031301022a87a0b197c8877834286302130100cbc4fd8a34cb65a82a2900480101cf20bddca78403c3e40e2ab1b3eeb526c425b5efb531e6c5bd3d52019c25125c002602130100596b57d9c1932d885b2b021301003f0bad3989c468482d2c004801011d818de56750d053b2a227f0da85ba37fb1869d0c774629d04e2668e1204c0a3001b021100e0b187aea6583a68332e021100e0a7528c0ef95128302f00480101e00721ae4b2be2ed708fa68e6b7bec2759a107504812069b3b8aa9acea346c660015020f00c141a6498c4d083231020f00c02225548664a8726c00480101747c06e45f53ca1dc7d23f39db07d12f2e67fa595a36fa41d26683ab42d1a304001400480101174c3878604468b08b7b76f5349c41201ae28b81ca1d94794ab11a692e4668250018031301015ec2a32762fd21d858356302130100f62101db096d33a8573602130100dd08f96dc461bcc8383700480101b90ed7fc04a4971294b12a078ec8189e8fdba184de6e23043922a774ae403ee2002402130100a6df074312541a084839021100f6b69431011179483b3a00480101e2a96bbff9be849635722263833d77a90f0a832b410f8b73bca56041fd7e21970016021100f6a2a63eab3d4e483d3c0048010130dd0d5ef5796dc4c101fbf5b4b083599e509d0f738b07a8dbfad6b5ae53aecb0012021100ea5905b0b329bd883f3e0048010130219e3c8c788af6da8a296da6f3e9925c909eed9821a0ae1911c38f56f7b37e000b021100ea58fcd0996ec1484740020f00c035987df0cb0842410048010118dd0a8040c21a2cfb6c0acf4ad636dc67ef3ab0a3e102f1b43ad500c55728d00007019bbd62f8f7bea30f8ab5e9f16c3fb8642b118f56ed1bdc49600dbe5220c8b1af9e040c474f803d1a0544cba813425adf3253dd3727a789c9e418b5e788a4cab5df805caee92b00000e9b3bd478a1c043036fcff34517c7bdf5187c55af4f8b61fdc321588c7ab768dee24b006df29106458d7cf21881f48000000000000074d9dea3c5110311d3e017f046454400480101986c49971b96062e1fba4410e27249c8d73b0a9380f7ffd44640167e68b215e80003004811fd096c0000000000000000000000000000000000000000000000000000000000000000004801017269fb9feb45d719ebdbc3b0816b987bab06f43378dc84dc84d5572790548214000200480101e2bc337ece7f3af5171f3265f44c612fc2fcba87f4b4563dc7fdc3285dd6a44d000802130100902873121142a0c856490213010090175c7b3161aee8554a021301008fb45fdc97d252084c4b00480101c7c146bea2ced23475861d11146c0560a46c3d243563fda0e32bf8c34229d2670013021301008f689e5fb80803e84e4d00480101ef1aa8b2068cf6a8eadef8197235a5d5976865a32a3ad1fe80db069ddb8cc2fe0011021301008f677b3e09283ac8544f021301008f677a70024cc7085350021301008f6779cbfeb8f18852510048010150725eee52e86432f846698a08ac153a67bc9ad9c160130af907c3bef05f2948000701a1bcd99999999999999999999999999999999999999999999999999999999999982011ecef393af6d61933fe8eada66c79771a5de359b379b70bfe4414022dee90877585c9818f39dda000003a6cef51e28564004801018a51fe69422dbf7e028fb1dcac5a62064eefeb4c080793e78a24ef22334b307c001000480101c2ef35325f62d0b4cc17d1f5d083894100c3c478504d70b6eb8d3cf26e604ff4001100480101cc6ead611f9fa7c0598d8f88d658fe0b91f5f9c9635c872154234c16c722970c001400480101eda54e0b0237690499c3e159ab800469fdbcb3c162d42181c2c298acd4e98f310015004801012bd772e408a34578028922281a3e5b5384970a6a6dd741b1cfa3b80a3e5ec57d00230313010068a1a14c598fee385a596300480101df3229c929cdae91378fd16242bda5a97246c54da4e9700d7427829825ee7b5d001900480101357b3e386bb95837e17d8fc7dd37f292efdc3301f6e109d8b33eb8a02afbb95d002200480101ff7081e66c7f0d6e868021316b0189b9e67b61284c176cd74f78fa6baa18a025001a0213c3c000074d9de66bc120615d0211480000e9b3bccd7824605e0211200003a6cef335e0905f6d0048010165b0a85a0fdea0c76a2a98445623ea62427099a6318624794dea416f1bdc6f5c0015004801014b01ebcf5425735461aa8b83bae89e70fa21e95d2ee85e57b05dad26c1d6d530001600480101258d602eaa21d621634dcf86692aeae308ff3cf888f3edafc6a5b21848d732f90018004801016bc4ad2e5c909f6f452be243edc65694f7e6db5f2fc615f69756954a60a563a2000c00480101a5a7d24057d8643b2527709d986cda3846adcb3eddc32d28ec21f69e17dbaaef0001027bcff33333333333333333333333333333333333333333333333333333333333333334081ac1664bc000000000000074d9dea3c50e011ecef393af6d6196d06a650355ec039e4242ff8cc69bf4260c44ddc7b820f838fa85ad1828d2b83ace409d6c02a3b89a505ac592d94a7c4d6968660179a0634dfa13634f7a130000800006226ee3dc107c1c7d42d68c14695c1d67204eb60151dc4d282d62c96ca53e26c0100ee542c8b882e30ec339334e5ca06700480101b8ad45439ed0f9f1ffb12362a0c0a6f522734feed11dda077d5f6067f1305170000b00480101336df3bd068890e3f26c1a8f5e77c4bf7cc3c81fc88006ab614b6db436472626000700480101ff06225996392d9e78d92fef981828f3459892841111b2d352901236d506cb65000b004801016217f872c99fafcb870f2c11a362f59339be95095f70d00b9cff2f6dcd69d3dd000e00480101de5adf45c03a745fc9d3a418d9e2ba096db9c1aaa77bc0898b6d9ebb611d2b40000500480101eb38ef90c590bedb3ce31140d2d4176d43db6b7aab35df685afc4ccf2a383209000b0211200003a6cef335e090716e02116200003a6cef335e09706f0211200003a6cef335e0909e8a00480101fde4f74a9866e3de066d6d27e3b1fe107053ecce8b54d8b05ebf4a3b0789c26b001100480101b5b64686c719580155341cb7347af0405dec7158c283ad30833b07325bdc48a50014020f00c0221de12e91087b73020f4030085e7768002a7a74020f00c02170f2272c287675004801010143b3d2dd671b2559543155e003f847022e510b3a57afabbca05d4069c327ef000d019dbceaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa818042d76318e19f365e6f7e0780eb2bc9f0c382940ef38b61bb6257ad617eb36fe8c9f56964f2800003a6cef51e287770277cff55555555555555555555555555555555555555555555555555555555555555554085ac1288e0000000000000074d9dea3c5118042d76318e195d07978014900000027cbb9d1062954439a83a91f27835fb9d2e3e798910356650c3c493c9462346468409b0048010164a43970f2007a1da6d6fc81773cc095d1cc270e81359e471f3b03469abeb7b5000c00480101a248b81f22333cc28f6b6744e4298aefcd9b6f2dc5d7c99e1da1b28c37f3aa0c00070048010179a2e20b8a926ab2fe83108ff00f2fbced9958047008e5cb5fdf8c798aab638500100201207e7d0048010110b3b5e79df7c963efb443120853eb1bf9377e78020993bf79d5aaa9b02d1a6a0008020120807f004801016f610eec3a1e4dc9bdacbda0e586e7a8f6b4734b6599ecc0f8c5d0e9666d0ed300080201208281004801019100c451439a1cfdcf444d77bc78d03f19ca5e71b1f8fdae5e9e0ccf3e8214a00007020120848300480101e0140ab9f7e276e1143af00713243e470dfc2c93c02b124622926fd33551a71b00060201208685004801014ef684da255649795b7830d1100f419d8f8a0eeb9ed6ed3610ba20b5d815deed000302012088870048010155cdb8f72801ef11ba562172ed2626c88208eddcf4a0c8f6d5447a785d02b79000020201209c8900480101b6eb72df89b91190ab85640f1ef9817bf00e49c5c11e8fd173b5b382ca4a104700010211200003a6cef335e0909a8b0211200003a6cef335e090998c0211000003a6cef335e090988d0211400000e9b3bccd7824978e0211000003a6cef335e090968f0211400000e9b3bccd782495900211400000e9b3bccd782494910211d000003a6cef335e0993920048010177c2748c31a7f78c56862aa9d06df60981de7aaa59e67d4d0360a2903384fe15000100480101523e62a3a95932c2a65f2314a8a818f82f48644967cc31dcfda9954109d8b551000100480101322f03bbddf42b900d602199315f5d4befa1a9282a2a6c845f3db6ccd2b6bfc000060048010175d211346d824c33aff56800c12e0b320854590aadfd85e3f909502cdb6ec3c1000800480101d744ca7d3ce6fe4538b3fa6a138971ca129c227d8a6736a9cd1d33c2f1fd06cc000a004801016d16afa0d70d41df6abe49636527c0b566bd3b722b731eba03433d7efbcb3908000b0048010191c44865f6767ab41750fbf5117df2d8be3110925c7993aa2e03780673c31f32000d0048010122da148fcc6a6a317ae3c41ee888034019cbfa89e57f306b85601dd2045d6daa000e0048010187c846be2bc06a266ae017ae9a13c66cf156125edd95b8bd4f6cfe3c903e3b35000f00480101374e198a900e08edc634a5f2ad73e388b0a3019d24269fae8046024e437476b1001000480101f25a1e1d7f11115186543ff6eb95e3d9b98f71d2c959af6b0dad6b63cd1e6d690001004801019deed5e9cd5995ad6c97a06276c939029a1d05a6de03b6c724a4b5567e9adb7a000e00480101f7a4391731a8136b142d214311bd2f8c162938f27185d22de576a045a13b1e160010020120a1a00048010197d9c97586b5cf9a93f5077cf1e13c91f7a4d5b240601e4d08030ab62cd17707000b020120afa2020120aea3020120ada4020120aca5020120a7a60048010178a2f12e152f91343bff8aeda8ca7bab1039578fb6b03832c150f22786d0500c0004020120a9a800480101b26a0cc496805853f303d8a00ae9fc7f7b20dc7cab6d1d1c21f5b86469874a840002020120abaa00480101a31f27b17ffa79bcaf0e47f55dffa054f825e019e447026255e7e1a8d74887010002004801016f2780ba9d3cdce8eee34a23d893d90800da0ac1be8a973c513909136b7f636b000200480101e86bec3c2e5a0c5b9bad30e9b0efd5c74409fece4efd571f8fe02eccbbd0af1a000400480101497deb7f82cc061521c9f6bf58ddd3043ecb1dbaea13352ecb73bb53236a9dd8000600480101d83f99b6b2deca33e45337ea0fa4788a5590c2a9f88654c24c1e4b5282ec7787000800480101f613c63e75ce90bdb3aadf01297ba9a958588392473ea542ef8654f281d2854f0009f6ea5a07")
			if err != nil {
				t.Fatal(err)
			}
			_cell, err := cell.FromBOC(cellBytes)
			if err != nil {
				t.Fatal(err)
			}

			err = tBuilder.StoreRef(_cell)
			if err != nil {
				t.Fatal(err)
			}
			if test.name == "blockType1 (tag 0x5f327da5)" {
				err = tBuilder.StoreRef(_cell)
				if err != nil {
					t.Fatal(err)
				}
			}

			err = LoadFromCell(&testShard, tBuilder.EndCell().BeginParse())
			if err != nil {
				t.Fatal(err)
			}

			if testShard.State.(ShardStateSplit).Left.Seqno != 24374596 {
				t.Fatal("incorrect result")
			}
		})
	}
}

func TestShardIdent_IsSibling(t *testing.T) {
	type fields struct {
		_           Magic
		WorkchainID int32
		ShardPrefix uint64
	}
	type args struct {
		with ShardIdent
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   bool
	}{
		{
			name: "true",
			fields: fields{
				WorkchainID: 0,
				ShardPrefix: 0xC000000000000000,
			},
			args: args{
				with: ShardIdent{
					WorkchainID: 0,
					ShardPrefix: 0x4000000000000000,
				},
			},
			want: true,
		},
		{
			name: "same",
			fields: fields{
				WorkchainID: 0,
				ShardPrefix: 0x4000000000000000,
			},
			args: args{
				with: ShardIdent{
					WorkchainID: 0,
					ShardPrefix: 0x4000000000000000,
				},
			},
			want: false,
		},
		{
			name: "next",
			fields: fields{
				WorkchainID: 0,
				ShardPrefix: 0x6000000000000000,
			},
			args: args{
				with: ShardIdent{
					WorkchainID: 0,
					ShardPrefix: 0x4000000000000000,
				},
			},
			want: false,
		},
		{
			name: "diff wc",
			fields: fields{
				WorkchainID: -1,
				ShardPrefix: 0xC000000000000000,
			},
			args: args{
				with: ShardIdent{
					WorkchainID: 0,
					ShardPrefix: 0x4000000000000000,
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := ShardIdent{
				WorkchainID: tt.fields.WorkchainID,
				ShardPrefix: tt.fields.ShardPrefix,
			}
			if got := s.IsSibling(tt.args.with); got != tt.want {
				t.Errorf("IsSibling() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShardIdent_IsParent(t *testing.T) {
	type fields struct {
		_           Magic
		WorkchainID int32
		ShardPrefix uint64
	}
	type args struct {
		with ShardIdent
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   bool
	}{
		{
			name: "parent",
			fields: fields{
				WorkchainID: 0,
				ShardPrefix: 0x8000000000000000,
			},
			args: args{
				with: ShardIdent{
					WorkchainID: 0,
					ShardPrefix: 0xC000000000000000,
				},
			},
			want: true,
		},
		{
			name: "child",
			fields: fields{
				WorkchainID: 0,
				ShardPrefix: 0xC000000000000000,
			},
			args: args{
				with: ShardIdent{
					WorkchainID: 0,
					ShardPrefix: 0x8000000000000000,
				},
			},
			want: false,
		},
		{
			name: "grand child",
			fields: fields{
				WorkchainID: 0,
				ShardPrefix: 0x8000000000000000,
			},
			args: args{
				with: ShardIdent{
					WorkchainID: 0,
					ShardPrefix: 0xE000000000000000,
				},
			},
			want: false,
		},
		{
			name: "diff wc",
			fields: fields{
				WorkchainID: 0,
				ShardPrefix: 0x8000000000000000,
			},
			args: args{
				with: ShardIdent{
					WorkchainID: -1,
					ShardPrefix: 0xC000000000000000,
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := ShardIdent{
				WorkchainID: tt.fields.WorkchainID,
				ShardPrefix: tt.fields.ShardPrefix,
			}
			if got := s.IsParent(tt.args.with); got != tt.want {
				t.Errorf("IsParent() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShardIdent_IsAncestor(t *testing.T) {
	type fields struct {
		_           Magic
		WorkchainID int32
		ShardPrefix uint64
	}
	type args struct {
		with ShardIdent
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   bool
	}{
		{
			name: "ancestor",
			fields: fields{
				WorkchainID: 0,
				ShardPrefix: 0x8000000000000000,
			},
			args: args{
				with: ShardIdent{
					WorkchainID: 0,
					ShardPrefix: 0xE000000000000000,
				},
			},
			want: true,
		},
		{
			name: "diff wc",
			fields: fields{
				WorkchainID: 0,
				ShardPrefix: 0x8000000000000000,
			},
			args: args{
				with: ShardIdent{
					WorkchainID: -1,
					ShardPrefix: 0xE000000000000000,
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := ShardIdent{
				WorkchainID: tt.fields.WorkchainID,
				ShardPrefix: tt.fields.ShardPrefix,
			}
			if got := s.IsAncestor(tt.args.with); got != tt.want {
				t.Errorf("IsAncestor() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShardIdent_GetShardID(t *testing.T) {
	type fields struct {
		ShardPrefix uint64
	}
	tests := []struct {
		name   string
		fields fields
		want   ShardID
	}{
		{
			name: "ok",
			fields: fields{
				ShardPrefix: 0xE000000000000000,
			},
			want: 0xE000000000000000,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := ShardIdent{
				ShardPrefix: tt.fields.ShardPrefix,
			}
			if got := s.GetShardID(); got != tt.want {
				t.Errorf("GetShardID() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShardID_GetChild(t *testing.T) {
	type args struct {
		left bool
	}
	tests := []struct {
		name string
		s    ShardID
		args args
		want ShardID
	}{
		{
			name: "ok",
			s:    0xE000000000000000,
			args: args{
				true,
			},
			want: 0xD000000000000000,
		},
		{
			name: "ok",
			s:    0xE000000000000000,
			args: args{
				false,
			},
			want: 0xF000000000000000,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.GetChild(tt.args.left); got != tt.want {
				t.Errorf("GetChild() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShardID_ContainsAddress(t *testing.T) {
	type args struct {
		addr *address.Address
	}
	tests := []struct {
		name string
		s    ShardID
		args args
		want bool
	}{
		{
			name: "ok",
			s:    0xA000000000000000,
			args: args{
				address.MustParseAddr("EQCN6j4gO7D_9OBkWQy_BkW1peVqA0ikvcSgCd9yj1yxu7VD"),
			},
			want: true,
		},
		{
			name: "ok 2",
			s:    0x6000000000000000,
			args: args{
				address.MustParseAddr("EQBTmKoKwypDGJFXf9FNwNdKG9Ei5C9KdKd85_ALPLRJbIR1"),
			},
			want: true,
		},
		{
			name: "ok 3",
			s:    0x8000000000000000,
			args: args{
				address.MustParseAddr("EQBTmKoKwypDGJFXf9FNwNdKG9Ei5C9KdKd85_ALPLRJbIR1"),
			},
			want: true,
		},
		{
			name: "ok 3",
			s:    0x8000000000000000,
			args: args{
				address.MustParseAddr("EQCN6j4gO7D_9OBkWQy_BkW1peVqA0ikvcSgCd9yj1yxu7VD"),
			},
			want: true,
		},
		{
			name: "no",
			s:    0x6000000000000000,
			args: args{
				address.MustParseAddr("EQCN6j4gO7D_9OBkWQy_BkW1peVqA0ikvcSgCd9yj1yxu7VD"),
			},
			want: false,
		},
		{
			name: "no 2",
			s:    0xA000000000000000,
			args: args{
				address.MustParseAddr("EQBTmKoKwypDGJFXf9FNwNdKG9Ei5C9KdKd85_ALPLRJbIR1"),
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.ContainsAddress(tt.args.addr); got != tt.want {
				t.Errorf("ContainsAddress() = %v, want %v", got, tt.want)
			}
		})
	}
}
