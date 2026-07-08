//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"fmt"
	"math/big"
	"math/rand"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	localec "github.com/xssnick/tonutils-go/tvm/internal/secp256k1"
	cellsliceop "github.com/xssnick/tonutils-go/tvm/op/cellslice"
	dictop "github.com/xssnick/tonutils-go/tvm/op/dict"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	ophelpers "github.com/xssnick/tonutils-go/tvm/op/helpers"
	mathop "github.com/xssnick/tonutils-go/tvm/op/math"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	tupleop "github.com/xssnick/tonutils-go/tvm/op/tuple"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

const (
	defaultDifferentialFuzzSeeds   = 128
	defaultParityProgramOps        = 24
	mixedDifferentialProgramWeight = 5
	versionMatrixFamilySeedStride  = 4096
	differentialFuzzGasLimit       = referenceDefaultMaxGas
	parityProgramChunkReserveBits  = 32
	maxSmallIndexForParityProgram  = (1 << 30) - 1

	expectedParityOpcodeInventoryEntries      = 915
	expectedParityOpcodeInventoryUniqueNames  = 885
	expectedParityDictOpcodeWitnessNames      = 143
	expectedParityMathOpcodeWitnessNames      = 255
	expectedParityCellSliceOpcodeWitnessNames = 163

	expectedSupportedDifferentialFuzzFamilyCount     = 28
	expectedSupportedDifferentialFuzzFamilyHash      = "56a8e5085c21d46e27c5280228fa9a19161352abcc9d5b19d3de202bad73e7e0"
	expectedVersionMatrixDifferentialFuzzFamilyCount = 26
	expectedVersionMatrixDifferentialFuzzFamilyHash  = "29fe2b2bb02d397ccbd05bdbb58602669c70fa7f618526df1dae1f7be22baea5"
	expectedMixedDifferentialFuzzFamilyCount         = 31
	expectedMixedDifferentialFuzzFamilyHash          = "5112dc6a3384e30803f9c7d1ff516c35416a2c01e7e8da0f1e0bce140eea89ca"
)

var expectedParityOpcodeCoverageBucketCounts = map[string]int{
	"dedicated_tuple":           33,
	"deterministic_exec":        111,
	"deterministic_ton_crypto":  45,
	"deterministic_ton_runtime": 66,
	"random_cell_slice":         166,
	"random_dict":               143,
	"random_math":               273,
	"random_stack":              78,
}

type parityWitnessManifestExpectation struct {
	count int
	hash  string
}

var expectedParityWitnessManifestExpectations = map[string]parityWitnessManifestExpectation{
	"requiredActionErrorGapCaseNames":       {12, "6aa8a32672946f60e0c3451fafe282602e934279cb1eeb5129855214355b7bd0"},
	"requiredActionGapTraceLabels":          {9, "e40427023952f2c0a389754e78ef564442db0eca2f12e3e9173076fa3515a6eb"},
	"requiredActionModeGapCaseNames":        {17, "8365cb79822a0f0cbdea339c8ff844e10a52db03bab6c11d3221ff93f62185bb"},
	"requiredCellSliceEdgeGapCaseNames":     {19, "cf9dcb210dead5a5f0d5e9b11e318262dc48d5b82f66eae30f2a8971cd4e95f0"},
	"requiredCellSliceGapCaseNames":         {27, "8b79ad9e1732e2cd6dee3dc25058fd0212389491f9047f8268518eff73853b14"},
	"requiredCellSliceProgramTraceLabels":   {7, "e66201bae6c213f6ff20c5cc36ffee6798f18afbf8840af726f10eeb597dded0"},
	"requiredCellSliceQuietGapCaseNames":    {52, "6dcb64c8f6ef8136f6bd5b79248feb285e5e979a2f7723ee07ce1382770ce363"},
	"requiredCirclCryptoGapCaseNames":       {39, "4adb641ae42ceb75e8398588f5e736faf19000ae2ffd7e323daa33574fed97f6"},
	"requiredCryptoEdgeGapCaseNames":        {31, "32764a1ab116f7dd2b603fac6ce2ce0775ade9984dadd45789abac5b44bdf76d"},
	"requiredCryptoGapCaseNames":            {14, "ff829b1a20a9ca8383acc2735c59107c88fe4b40ababbb2dc5670244edc23585"},
	"requiredDataSizeGapCaseNames":          {9, "6e3e5199a7456c22f065f6ecef250f0b742136e717abf1c3df54ea0c86a29b90"},
	"requiredDataSizeProgramTraceLabels":    {4, "df7673fc99960358465d9def9091c79231c02aea6c6ac63418466999b03ca523"},
	"requiredDictContinuationGapCaseNames":  {21, "f2a9d03b51b83f25ccb6fedcd98c42f5a7ccfca367e95d06f59ede32ed767004"},
	"requiredDictEdgeGapCaseNames":          {36, "0211c0d64f33822b6fd2a2ce8281dd458c744008dde4eb5b18a0f042eeaddb4e"},
	"requiredDictGapTraceLabels":            {63, "2c4de0a1b83a8c52d5b80abb180ec705f7de3db124fc6da4618ad87fce251aa5"},
	"requiredDictMissGapCaseNames":          {66, "1f9856f002df0976545c868341c7cec6398cb733e6e24cfba9b62639dbb62a14"},
	"requiredDictNearGapTraceLabels":        {12, "062acdf830f1d8c68c2f7d9d734a81e9dfd49c5920d24ab9fed67fac286ec325"},
	"requiredDictProgramTraceLabels":        {45, "fcba9d15672d65f5126c7af2d15a22babbbea36b91f720545a6201e080672877"},
	"requiredDictSuccessGapCaseNames":       {14, "6b644430c8fc5a993e89ca3317d1eb4071193577ac39314e9f6d5839789d9bfc"},
	"requiredExecGapCaseNames":              {168, "e3f2afdab6659571e08a6158fba25d5c231f80cb9ec9dcdf90c934f964bc7aa0"},
	"requiredExecRefDecodeGapCaseNames":     {12, "ed3d8fbf59ebc2bcf6386f67127489da147ef17b0802c65229dc2991c8c428dc"},
	"requiredHashGapCaseNames":              {20, "eabce7451333896a317e68f1e4d70d2a14df43cee9f91232cdc019bd30a98ad5"},
	"requiredInvalidMathGapCaseNames":       {8, "8a0e713afadab04c354be55bfe619f08594acbbde2fbc59d06edc07a449291d7"},
	"requiredInventoryResidualGapCaseNames": {38, "92e2961cdd5ffda7950683572254cd07c8621d95d37a5c8e069a055a471820df"},
	"requiredLibraryGapCaseNames":           {7, "9ea4213524f3ec5581b942730b2df340988e25a3648355b378b7695ed9de4e37"},
	"requiredMathCompoundTraceLabels":       {21, "1693f9bd08e5fec9fa028f1c8e7438621b8c45b8f6896660742f89fe23e6b6f5"},
	"requiredMathGapTraceLabels":            {36, "8432a060d595ee2bf1e7b1b9774ba2c2c6a5cfb12e28eed7af5f03600fbee2b3"},
	"requiredMathImmediateGapCaseNames":     {62, "47efebb69d22db471664b905ea4e81796a72ed8a5238c0f49bc030e02518d8a4"},
	"requiredMathProgramTraceLabels":        {27, "51b3b4dccbcb702beace253c75a4bc0f12dfcb9cb1cfcf95ed35b26db06122cd"},
	"requiredMathQuietCompoundTraceLabels":  {15, "153ddfe973151d31254587f4433502788ceb53140c0c990b7766f2f73abae367"},
	"requiredMathQuietLogicTraceLabels":     {29, "9108b5d0e3075f8214be54be56136513053793de3ce838062384fd7fd7859fb1"},
	"requiredMathShiftTraceLabels":          {20, "fb27bf0bf8d68eec0bfef21a76b062d3a3fe7e26fada87d725f269c365396dbd"},
	"requiredMsgAddressGapCaseNames":        {31, "b6a9caad455f3c9cad1de672c67831e23edf3e5f6a35610ce28392db0ea6d357"},
	"requiredMsgAddressProgramTraceLabels":  {16, "8124966c8519f8c188c963a0fb091532b05627dbd4bcefa3225846e1ffb43472"},
	"requiredQuietMathErrorGapCaseNames":    {22, "70b6df68d1c7f3ad3974bf01c58dcf268f413ad17677c6989feb31cc4d4d6461"},
	"requiredRunVMGapCaseNames":             {6, "c54405c34c64c9b24e45360d365e8a7b40bcb5ad89766418b4f5ff7c287f70c0"},
	"requiredStackDynamicDepthGapCaseNames": {22, "2d9cebaeb34927b478ff010bdcace64ec273ac8e88448bf3ab58b833881795d5"},
	"requiredStackGapCaseNames":             {30, "a4a03c1a6cd517fdc1ace188cf1e41557a37bc99d096e7f61986f5d63d4c0fb6"},
	"requiredStackOpcodeSpaceGapCaseNames":  {40, "70689dd0a525c1f92a38f1f5aa95e8220fd200293b2fe5943e23b6eada77a69e"},
	"requiredTonFuncGapCaseNames":           {61, "5896d8237e6110637cc6f3b51ff4d7cdf18ed81d5853c472e63167aef48d3e28"},
	"requiredTonFuncRuntimeGapCaseNames":    {36, "e3de93a510f951967bba38515810f8ca72725c5f22e3c58033ec855b6102899b"},
	"requiredTupleDynamicErrorGapCaseNames": {64, "bf62b52abce8b41114e2c3dc45de9a5f2036a14940af6b0bf3bc5696735169ac"},
	"requiredTupleGapCaseNames":             {7, "4947335e7c9372931320769db91024e5a8301e43c49a160f4a239a9daae20b2f"},
	"requiredTupleGapTraceLabels":           {19, "2f677e5e9808661d9b9720d529b9692266584cb81d7955566b0b1b728fbc1e6d"},
	"requiredTupleOpcodeSpaceGapCaseNames":  {38, "9f82cbfcc89f332c771cff09e4a9453e8a9a767f8fd1eacee87691baf412de60"},
	"requiredVarIntGapCaseNames":            {12, "16547fa9229c8370ddd1ce0035497e7e0d821dfd4575de1b77616472a35b2d6f"},
}

var expectedParityCryptoOpcodeWitnessCases = map[string][]string{
	"BLS_AGGREGATE":                    {"bls_aggregate"},
	"BLS_AGGREGATEVERIFY":              {"bls_aggregateverify_distinct_msgs_true"},
	"BLS_FASTAGGREGATEVERIFY":          {"bls_fastaggregateverify_true"},
	"BLS_G1_ADD":                       {"bls_g1_add"},
	"BLS_G1_INGROUP":                   {"bls_g1_ingroup_invalid_false"},
	"BLS_G1_ISZERO":                    {"bls_g1_iszero"},
	"BLS_G1_MUL":                       {"bls_g1_mul"},
	"BLS_G1_MULTIEXP":                  {"bls_g1_multiexp_two_terms"},
	"BLS_G1_NEG":                       {"bls_g1_neg"},
	"BLS_G1_SUB":                       {"bls_g1_sub"},
	"BLS_G1_ZERO":                      {"bls_g1_zero"},
	"BLS_G2_ADD":                       {"bls_g2_add"},
	"BLS_G2_INGROUP":                   {"bls_g2_ingroup_invalid_false"},
	"BLS_G2_ISZERO":                    {"bls_g2_iszero"},
	"BLS_G2_MUL":                       {"bls_g2_mul"},
	"BLS_G2_MULTIEXP":                  {"bls_g2_multiexp_two_terms"},
	"BLS_G2_NEG":                       {"bls_g2_neg"},
	"BLS_G2_SUB":                       {"bls_g2_sub"},
	"BLS_G2_ZERO":                      {"bls_g2_zero"},
	"BLS_MAP_TO_G1":                    {"bls_map_to_g1"},
	"BLS_MAP_TO_G2":                    {"bls_map_to_g2"},
	"BLS_PAIRING":                      {"bls_pairing_false"},
	"BLS_PUSHR":                        {"bls_pushr"},
	"BLS_VERIFY":                       {"bls_verify_true"},
	"CHKSIGNS":                         {"chksigns_success"},
	"CHKSIGNU":                         {"chksignu_success"},
	"ECRECOVER":                        {"ecrecover_success"},
	"HASHBU":                           {"hashbu_builder_with_ref"},
	"HASHEXT 0":                        {"hashext_bit_concat_sha256"},
	"P256_CHKSIGNS":                    {"p256_chksigns_success"},
	"P256_CHKSIGNU":                    {"p256_chksignu_success"},
	"RIST255_ADD":                      {"rist255_add_valid"},
	"RIST255_FROMHASH":                 {"rist255_fromhash"},
	"RIST255_MUL":                      {"rist255_mul"},
	"RIST255_MULBASE":                  {"rist255_mulbase"},
	"RIST255_PUSHL":                    {"rist255_pushl"},
	"RIST255_QADD":                     {"rist255_qadd_invalid"},
	"RIST255_QMUL":                     {"rist255_qmul_invalid"},
	"RIST255_QMULBASE":                 {"rist255_qmulbase"},
	"RIST255_QSUB":                     {"rist255_qsub_valid"},
	"RIST255_QVALIDATE":                {"rist255_qvalidate_invalid"},
	"RIST255_SUB":                      {"rist255_sub_valid"},
	"RIST255_VALIDATE":                 {"rist255_validate_valid"},
	"SECP256K1_XONLY_PUBKEY_TWEAK_ADD": {"secp256k1_xonly_pubkey_tweak_add_success"},
	"SHA256U":                          {"sha256u_empty"},
}

var expectedParityRuntimeOpcodeWitnessCases = map[string][]string{
	"ACCEPT":              {"accept"},
	"ADDRAND":             {"setrand_addrand_roundtrip"},
	"BALANCE":             {"balance_myaddr_configroot"},
	"BLOCKLT":             {"getparam_now_blocklt_ltime_aliases"},
	"CDATASIZE":           {"cdatasize_success_counts_ref"},
	"CDATASIZEQ":          {"cdatasizeq_bound_too_small_false"},
	"CHANGELIB":           {"changelib_mode_0"},
	"COMMIT":              {"commit"},
	"CONFIGDICT":          {"configdict"},
	"CONFIGOPTPARAM":      {"configoptparam_hit"},
	"CONFIGPARAM":         {"configparam_hit"},
	"CONFIGROOT":          {"balance_myaddr_configroot"},
	"DUEPAYMENT":          {"duepayment"},
	"GASCONSUMED":         {"gasconsumed"},
	"GETEXTRABALANCE":     {"getextrabalance_hit"},
	"GETFORWARDFEE":       {"getforwardfee"},
	"GETFORWARDFEESIMPLE": {"getforwardfeesimple"},
	"GETGASFEE":           {"getgasfee"},
	"GETGASFEESIMPLE":     {"getgasfeesimple"},
	"GETGLOB 1":           {"setglob_getglob_roundtrip"},
	"GETGLOBVAR":          {"setglobvar_getglobvar_roundtrip"},
	"GETORIGINALFWDFEE":   {"getoriginalfwdfee"},
	"GETPARAM 0":          {"getparam_now_blocklt_ltime_aliases"},
	"GETPARAMLONG 0":      {"getparamlong_randseed"},
	"GETPRECOMPILEDGAS":   {"getprecompiledgas"},
	"GETSTORAGEFEE":       {"getstoragefee"},
	"GLOBALID":            {"globalid"},
	"INCOMINGVALUE":       {"incomingvalue"},
	"INMSGPARAM 0":        {"inmsgparam_bounce"},
	"INMSGPARAMS":         {"inmsgparams"},
	"INMSG_BOUNCE":        {"inmsg_bounce"},
	"INMSG_BOUNCED":       {"inmsg_bounced"},
	"INMSG_FWDFEE":        {"inmsg_fwdfee"},
	"INMSG_LT":            {"inmsg_lt"},
	"INMSG_ORIGVALUE":     {"inmsg_origvalue"},
	"INMSG_SRC":           {"inmsg_src"},
	"INMSG_STATEINIT":     {"inmsg_stateinit"},
	"INMSG_UTIME":         {"inmsg_utime"},
	"INMSG_VALUE":         {"inmsg_value"},
	"INMSG_VALUEEXTRA":    {"inmsg_valueextra"},
	"LTIME":               {"getparam_now_blocklt_ltime_aliases"},
	"MYADDR":              {"balance_myaddr_configroot"},
	"MYCODE":              {"mycode"},
	"NOW":                 {"getparam_now_blocklt_ltime_aliases"},
	"PREVBLOCKSINFOTUPLE": {"prevblocksinfotuple"},
	"PREVKEYBLOCK":        {"prevkeyblock"},
	"PREVMCBLOCKS":        {"prevmcblocks"},
	"PREVMCBLOCKS_100":    {"prevmcblocks_100"},
	"RAND":                {"rand_bounded_updates_seed"},
	"RANDSEED":            {"getparamlong_randseed"},
	"RANDU256":            {"randu256_updates_seed"},
	"RAWRESERVE":          {"rawreserve_mode_0"},
	"RAWRESERVEX":         {"rawreservex_mode_0"},
	"SDATASIZE":           {"sdatasize_success_counts_refs"},
	"SDATASIZEQ":          {"sdatasizeq_bound_too_small_false"},
	"SENDMSG":             {"sendmsg_extout_fee_only"},
	"SENDRAWMSG":          {"action_chain_sendrawmsg_then_rawreserve_hash"},
	"SETCODE":             {"action_chain_setcode_then_setlibcode_hash"},
	"SETCPX":              {"setcpx_zero"},
	"SETGASLIMIT":         {"setgaslimit"},
	"SETGLOB 1":           {"setglob_getglob_roundtrip"},
	"SETGLOBVAR":          {"setglobvar_getglobvar_roundtrip"},
	"SETLIBCODE":          {"setlibcode_mode_0"},
	"SETRAND":             {"setrand_addrand_roundtrip"},
	"STORAGEFEES":         {"storagefees"},
	"UNPACKEDCONFIGTUPLE": {"unpackedconfigtuple"},
}

var expectedParityTupleOpcodeWitnessCases = map[string][]string{
	"0 EXPLODE":      {"explode_15"},
	"EXPLODEVAR":     {"explodevar_4"},
	"0 INDEX":        {"index_0"},
	"INDEX2 0,0":     {"index2_0_0"},
	"INDEX3 0,0,0":   {"index3_0_0_0"},
	"0 INDEXQ":       {"indexq_15"},
	"INDEXVAR":       {"indexvar_3"},
	"INDEXVARQ":      {"indexvarq_3"},
	"ISNULL":         {"isnull_null"},
	"ISTUPLE":        {"istuple_tuple"},
	"LAST":           {"last"},
	"NULLSWAPIF":     {"NULLSWAPIF"},
	"NULLSWAPIFNOT":  {"NULLSWAPIFNOT"},
	"NULLROTRIF":     {"NULLROTRIF"},
	"NULLROTRIFNOT":  {"NULLROTRIFNOT"},
	"NULLSWAPIF2":    {"NULLSWAPIF2"},
	"NULLSWAPIFNOT2": {"NULLSWAPIFNOT2"},
	"NULLROTRIF2":    {"NULLROTRIF2"},
	"NULLROTRIFNOT2": {"NULLROTRIFNOT2"},
	"QTLEN":          {"qtlen_tuple"},
	"0 SETINDEX":     {"setindex_15"},
	"0 SETINDEXQ":    {"setindexq_15"},
	"SETINDEXVAR":    {"setindexvar_3"},
	"SETINDEXVARQ":   {"setindexvarq_3"},
	"TLEN":           {"tlen"},
	"TPOP":           {"tpop"},
	"TPUSH":          {"tpush"},
	"0 TUPLE":        {"tuple_0"},
	"TUPLEVAR":       {"tuplevar_4"},
	"0 UNTUPLE":      {"untuple_0"},
	"0 UNPACKFIRST":  {"unpackfirst_15"},
	"UNTUPLEVAR":     {"untuplevar_4"},
	"UNPACKFIRSTVAR": {"unpackfirstvar_4"},
}

var expectedParityStackOpcodeWitnessCases = map[string][]string{
	"PUSHPOW2 1":       {"pushpow2"},
	"PUSHNAN":          {"pushnan"},
	"PUSHPOW2DEC 1":    {"pushpow2dec"},
	"PUSHNEGPOW2 1":    {"pushnegpow2"},
	"0 BLKDROP":        {"blkdrop_0"},
	"0,0 BLKDROP2":     {"blkdrop2_15_15"},
	"0, 0 BLKPUSH":     {"blkpush_1_0"},
	"1,1 BLKSWAP":      {"blkswap_1_1"},
	"BLKSWX":           {"blkswx_2_2"},
	"CHKDEPTH":         {"chkdepth_1022"},
	"DEPTH":            {"depth"},
	"DROP":             {"drop"},
	"2DROP":            {"2drop"},
	"DROPX":            {"dropx_0"},
	"DUMPSTK":          {"dumpstk_noop"},
	"DUMP s0":          {"dump_s0_noop"},
	"DEBUG 1":          {"debug_noop"},
	"DEBUGSTR 00":      {"debugstr_noop"},
	"STRDUMP":          {"strdump_slice_noop"},
	"DUP":              {"dup"},
	"2DUP":             {"2dup"},
	"0,0,0 XC2PU":      {"xc2pu_15_15_15"},
	"0,0,0 XCPUXC":     {"xcpuxc_15_15_15"},
	"0,0,0 XCPU2":      {"xcpu2_15_15_15"},
	"0,0,0 PUXC2":      {"puxc2_15_15_15"},
	"0,0,0 PUXCPU":     {"puxcpu_15_15_15"},
	"0,0,0 PU2XC":      {"pu2xc_15_15_15"},
	"0,0,0 PUSH3":      {"push3_15_15_15"},
	"NIP":              {"nip"},
	"ONLYTOPX":         {"onlytopx_0"},
	"ONLYX":            {"onlyx_0"},
	"OVER":             {"over"},
	"2OVER":            {"2over"},
	"PICK":             {"pick_0"},
	"s0 POP":           {"pop_short_15"},
	"s0 PUSH":          {"push_short_15"},
	"0,0 PUSH2":        {"push2_15_15"},
	"<???>  PUSHCONT":  {"pushcont_execute"},
	"PUSHINT":          {"pushint_small"},
	"??? PUSHREF":      {"pushref_payload"},
	"0[] PUSHREFSLICE": {"pushrefslice_payload"},
	"0[] PUSHSLICE":    {"pushslice_payload"},
	"PUSHNULL":         {"pushnull"},
	"0,0 PUXC":         {"puxc_15_15"},
	"2, 0 REVERSE":     {"reverse_17_15"},
	"REVX":             {"revx_empty"},
	"ROLL":             {"roll_0"},
	"ROLLREV":          {"rollrev_256"},
	"ROT":              {"rot"},
	"ROTREV":           {"rotrev"},
	"SWAP":             {"swap"},
	"2SWAP":            {"2swap"},
	"TUCK":             {"tuck"},
	"s0,s0 XCHG":       {"xchg_1_15"},
	"0 XCHG0":          {"xchg0_long_255"},
	"1 XCHG0":          {"xchg0_long_255"},
	"2 XCHG0":          {"xchg0_long_255"},
	"3 XCHG0":          {"xchg0_long_255"},
	"4 XCHG0":          {"xchg0_long_255"},
	"5 XCHG0":          {"xchg0_long_255"},
	"6 XCHG0":          {"xchg0_long_255"},
	"7 XCHG0":          {"xchg0_long_255"},
	"8 XCHG0":          {"xchg0_long_255"},
	"9 XCHG0":          {"xchg0_long_255"},
	"10 XCHG0":         {"xchg0_long_255"},
	"11 XCHG0":         {"xchg0_long_255"},
	"12 XCHG0":         {"xchg0_long_255"},
	"13 XCHG0":         {"xchg0_long_255"},
	"14 XCHG0":         {"xchg0_long_255"},
	"15 XCHG0":         {"xchg0_long_255"},
	"0,0 XCHG2":        {"xchg2_15_15"},
	"0,0,0 XCHG3":      {"xchg3_short_15_15_15"},
	"XCHGX":            {"xchgx_0"},
	"0,0 XCPU":         {"xcpu_15_15"},
}

var expectedParityExecOpcodeWitnessCases = map[string][]string{
	"AGAIN":            {"again_throw_bounded"},
	"AGAINBRK":         {"againbrk_break"},
	"AGAINEND":         {"againend_throw_bounded"},
	"AGAINENDBRK":      {"againendbrk_break"},
	"ATEXIT":           {"atexit_runs"},
	"ATEXITALT":        {"atexitalt_runs"},
	"BLESS":            {"bless_execute"},
	"BLESSARGS 0,-1":   {"blessargs_execute"},
	"BLESSVARARGS":     {"blessvarargs_execute"},
	"BOOLAND":          {"booland_composes_return_continuation"},
	"BOOLEVAL":         {"booleval_false_branch"},
	"BOOLOR":           {"boolor_composes_alt_continuation"},
	"CALLCC":           {"callcc_pushes_current_continuation"},
	"CALLCCARGS 0,-1":  {"callccargs_preserves_arg"},
	"CALLCCVARARGS":    {"callccvarargs_dynamic"},
	"CALLDICT 0":       {"calldict_short"},
	"CALLREF":          {"callref_pushes_value"},
	"CALLXARGS 0,-1":   {"callxargsp_all_returns"},
	"CALLXARGS 0,0":    {"callxargs_sum_then_tail"},
	"CALLXVARARGS":     {"callxvarargs_dynamic"},
	"COMPOSBOTH":       {"composboth_composes_return_and_alt"},
	"CONDSEL":          {"condsel_true"},
	"CONDSELCHK":       {"condselchk_true"},
	"EXECUTE":          {"execute_non_cont_typecheck"},
	"IF":               {"if_taken_call"},
	"IFBITJMP 0":       {"ifbitjmp_taken"},
	"IFBITJMPREF":      {"ifbitjmpref_taken"},
	"IFELSE":           {"ifelse_true_branch"},
	"IFELSEREF":        {"ifelseref_skips_invalid_ref_true_branch"},
	"IFJMP":            {"ifjmp_taken"},
	"IFJMPREF":         {"ifjmpref_taken"},
	"IFNOT":            {"ifnot_taken_call"},
	"IFNOTJMP":         {"ifnotjmp_taken"},
	"IFNOTJMPREF":      {"ifnotjmpref_taken"},
	"IFNOTREF":         {"ifnotref_skips_invalid_ref_true"},
	"IFNOTRET":         {"ifnotret_taken"},
	"IFNOTRETALT":      {"ifnotretalt_taken"},
	"IFREF":            {"ifref_skips_invalid_ref_false"},
	"IFREFELSE":        {"ifrefelse_skips_invalid_ref_false_branch"},
	"IFREFELSEREF":     {"ifrefelseref_true_branch"},
	"IFRET":            {"ifret_taken"},
	"IFRETALT":         {"ifretalt_taken"},
	"INVERT":           {"invert_ret"},
	"JMPDICT 0":        {"jmpdict"},
	"JMPREF":           {"jmpref_pushes_value"},
	"JMPREFDATA":       {"jmprefdata_exposes_remaining_code"},
	"JMPX":             {"jmpx_pushes_value"},
	"JMPXARGS 0":       {"jmpxargs_two_params"},
	"JMPXDATA":         {"jmpxdata_valid_remaining_slice"},
	"JMPXVARARGS":      {"jmpxvarargs_dynamic_params"},
	"NOP":              {"nop_keeps_stack"},
	"POPCTRX":          {"popctrx_c7_tuple_roundtrip"},
	"PREPAREDICT 0":    {"preparedict_execute"},
	"PUSHCTRX":         {"pushctrx_c6_range"},
	"REPEAT":           {"repeat_two_iterations"},
	"REPEATBRK":        {"repeatbrk_break"},
	"REPEATEND":        {"repeatend_one_iteration"},
	"REPEATENDBRK":     {"repeatendbrk_break"},
	"RET":              {"invert_ret"},
	"RETALT":           {"atexitalt_runs"},
	"RETARGS 0":        {"callxargs_sum_then_tail"},
	"RETBOOL":          {"retbool_return_branch"},
	"RETDATA":          {"retdata_remaining_slice"},
	"RETURNARGS 0":     {"returnargs_fixed_count"},
	"RETURNVARARGS":    {"returnvarargs_dynamic_count"},
	"RETVARARGS":       {"retvarargs_trim"},
	"RUNVM 0":          {"runvm_c7_return_one"},
	"RUNVMX":           {"runvmx_gas_bounds_return_one"},
	"SAMEALT":          {"samealt_copy_is_independent"},
	"SAMEALTSAVE":      {"samealtsave_preserves_previous_alt"},
	"SETCONTARGS 0,-1": {"setcontargs_capture_all"},
	"SETCONTCTRMANY 1": {"setcontctrmany_success"},
	"SETCONTCTRMANYX":  {"setcontctrmanyx_success"},
	"SETCONTCTRX":      {"setcontctrx_success"},
	"SETCONTVARARGS":   {"setcontvarargs_execute"},
	"SETCP 0":          {"setcp_zero"},
	"SETEXITALT":       {"setexitalt_runs"},
	"SETNUMVARARGS":    {"setnumvarargs_execute"},
	"THENRET":          {"thenret_runs"},
	"THENRETALT":       {"thenretalt_runs"},
	"THROW 0":          {"throw_short_uncaught"},
	"THROWANY":         {"throwany_uncaught"},
	"THROWARG 0":       {"throwarg_long_uncaught"},
	"THROWARGIF 0":     {"throwargif_taken"},
	"THROWARGIFNOT 0":  {"throwargifnot_taken"},
	"THROWIF 0":        {"throwif_taken"},
	"THROWIFNOT 0":     {"throwifnot_taken"},
	"TRY":              {"try_catches_throw"},
	"TRYARGS 0,0":      {"tryargs_returns_value"},
	"UNTIL":            {"until_one_iteration"},
	"UNTILBRK":         {"untilbrk_break"},
	"UNTILEND":         {"untilend_one_iteration"},
	"UNTILENDBRK":      {"untilendbrk_break"},
	"WHILE":            {"while_one_iteration_then_false"},
	"WHILEBRK":         {"whilebrk_break"},
	"WHILEEND":         {"whileend_one_iteration_then_false"},
	"WHILEENDBRK":      {"whileendbrk_break"},
	"c0 POP":           {"popctr_c0_noncont_typecheck"},
	"c0 POPSAVE":       {"popsavectr_c0_noncont_typecheck"},
	"c0 PUSH":          {"pushctr_c0_drop"},
	"c0 SAVEALTCTR":    {"savealtctr_c4_restore_on_retalt"},
	"c0 SAVEBOTHCTR":   {"savebothctr_c4_restore_on_ret"},
	"c0 SAVECTR":       {"savectr_c4_restore_on_ret"},
	"c0 SETALTCTR":     {"setaltctr_c4_restore_on_retalt"},
	"c0 SETCONTCTR":    {"setcontctr_success"},
	"c0 SETRETCTR":     {"setretctr_c4_restore_on_ret"},
}

type differentialFuzzCase struct {
	seed             uint64
	family           string
	op               string
	code             *cell.Cell
	stack            []any
	globalVersion    int
	globalVersionSet bool
	rawC7Versioned   bool
	gasLimit         int64
	c7               tuple.Tuple
	refCfg           *referenceGetMethodConfig
	refLibs          *cell.Cell
	goLibs           []*cell.Cell
}

func supportedDifferentialFuzzFamilies() []string {
	return []string{
		"mixed",
		"datasize",
		"slice_load",
		"slice_predicate",
		"slice_store",
		"math",
		"program",
		"msg_address_versioned",
		"program_versioned",
		"program_versioned_datasize",
		"program_versioned_actions",
		"program_versioned_libraries",
		"program_versioned_msg_address",
		"program_versioned_hash_varint",
		"program_versioned_prng",
		"program_versioned_math",
		"program_versioned_cellslice",
		"program_versioned_control",
		"program_versioned_exec",
		"program_versioned_dict",
		"program_versioned_stack",
		"program_versioned_tuple",
		"program_versioned_runvm",
		"program_versioned_runvm_rich_c7",
		"program_versioned_runtime",
		"program_versioned_c7",
		"program_versioned_rich_c7",
		"program_versioned_supercontract",
	}
}

func mixedDifferentialFuzzFamilies() []string {
	return []string{
		"datasize",
		"slice_load",
		"slice_predicate",
		"slice_store",
		"math",
		"program",
		"program",
		"program",
		"program",
		"program",
		"msg_address_versioned",
		"program_versioned",
		"program_versioned_datasize",
		"program_versioned_actions",
		"program_versioned_libraries",
		"program_versioned_msg_address",
		"program_versioned_hash_varint",
		"program_versioned_prng",
		"program_versioned_math",
		"program_versioned_cellslice",
		"program_versioned_control",
		"program_versioned_exec",
		"program_versioned_dict",
		"program_versioned_stack",
		"program_versioned_tuple",
		"program_versioned_runvm",
		"program_versioned_runvm_rich_c7",
		"program_versioned_runtime",
		"program_versioned_c7",
		"program_versioned_rich_c7",
		"program_versioned_supercontract",
	}
}

func versionMatrixDifferentialFuzzFamilies() []string {
	families := supportedDifferentialFuzzFamilies()
	out := make([]string, 0, len(families))
	for _, family := range families {
		switch family {
		case "mixed", "program":
			continue
		default:
			out = append(out, family)
		}
	}
	return out
}

func differentialFuzzFamiliesFromEnv(t *testing.T, envName string, families []string) []string {
	t.Helper()

	selected, err := differentialFuzzFamiliesFromRaw(envName, os.Getenv(envName), families)
	if err != nil {
		t.Fatal(err)
	}
	return selected
}

func differentialFuzzFamiliesFromRaw(envName, raw string, families []string) ([]string, error) {
	if raw == "" {
		return families, nil
	}
	allowed := map[string]struct{}{}
	for _, family := range families {
		allowed[family] = struct{}{}
	}

	var selected []string
	seen := map[string]struct{}{}
	for _, family := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	}) {
		if _, ok := allowed[family]; !ok {
			return nil, fmt.Errorf("%s contains unsupported family %q", envName, family)
		}
		if _, ok := seen[family]; ok {
			continue
		}
		seen[family] = struct{}{}
		selected = append(selected, family)
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("%s must list at least one family", envName)
	}
	return selected, nil
}

func pickMixedDifferentialFuzzFamily(t *testing.T, r *rand.Rand) string {
	t.Helper()

	families := mixedDifferentialFuzzFamilies()
	if len(families) == 0 {
		t.Fatal("mixed differential fuzz family list is empty")
	}
	return families[r.Intn(len(families))]
}

func TestTVMDifferentialFuzzSeeds(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	startInt := parityFuzzEnvInt(t, "TVM_PARITY_START", 0)
	if startInt < 0 {
		t.Fatal("TVM_PARITY_START must be non-negative")
	}
	start := uint64(startInt)
	seeds := parityFuzzEnvInt(t, "TVM_PARITY_SEEDS", defaultDifferentialFuzzSeeds)
	if seeds <= 0 {
		t.Skip("TVM_PARITY_SEEDS <= 0")
	}
	family := os.Getenv("TVM_PARITY_FAMILY")
	shard, shards := parityFuzzShardEnv(t, "TVM_PARITY_SHARD", "TVM_PARITY_SHARDS")

	runCount := 0
	for i := 0; i < seeds; i++ {
		if shards > 0 && i%shards != shard {
			continue
		}
		runCount++
		seed := start + uint64(i)
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			r := rand.New(rand.NewSource(int64(seed)))
			tc := generateDifferentialFuzzCaseWithFamily(t, r, seed, family)
			runDifferentialFuzzCase(t, tc)
		})
	}
	if runCount == 0 {
		t.Skipf("no seeds selected for shard %d/%d in %d requested seeds", shard, shards, seeds)
	}
}

func TestTVMDifferentialFuzzAllFamiliesAudit(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	startInt := parityFuzzEnvInt(t, "TVM_PARITY_AUDIT_START", 0)
	if startInt < 0 {
		t.Fatal("TVM_PARITY_AUDIT_START must be non-negative")
	}
	seeds := parityFuzzEnvInt(t, "TVM_PARITY_AUDIT_SEEDS", defaultDifferentialFuzzSeeds)
	if seeds <= 0 {
		t.Skip("TVM_PARITY_AUDIT_SEEDS <= 0")
	}
	shard, shards := parityFuzzShardEnv(t, "TVM_PARITY_AUDIT_SHARD", "TVM_PARITY_AUDIT_SHARDS")

	families := differentialFuzzFamiliesFromEnv(t, "TVM_PARITY_AUDIT_FAMILIES", supportedDifferentialFuzzFamilies())
	runCount := 0
	for familyIdx, family := range families {
		t.Run(family, func(t *testing.T) {
			for i := 0; i < seeds; i++ {
				globalIdx := familyIdx*seeds + i
				if shards > 0 && globalIdx%shards != shard {
					continue
				}

				runCount++
				seed := uint64(startInt + i)
				t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
					r := rand.New(rand.NewSource(int64(seed)))
					tc := generateDifferentialFuzzCaseWithFamily(t, r, seed, family)
					runDifferentialFuzzCase(t, tc)
				})
			}
		})
	}
	if runCount == 0 {
		t.Skipf("no audit seeds selected for shard %d/%d across %d families x %d seeds", shard, shards, len(families), seeds)
	}
}

func TestTVMDifferentialFuzzVersionMatrixAudit(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	startInt := parityFuzzEnvInt(t, "TVM_PARITY_VERSION_AUDIT_START", 0)
	if startInt < 0 {
		t.Fatal("TVM_PARITY_VERSION_AUDIT_START must be non-negative")
	}
	seeds := parityFuzzEnvInt(t, "TVM_PARITY_VERSION_AUDIT_SEEDS", defaultDifferentialFuzzSeeds)
	if seeds <= 0 {
		t.Skip("TVM_PARITY_VERSION_AUDIT_SEEDS <= 0")
	}
	shard, shards := parityFuzzShardEnv(t, "TVM_PARITY_VERSION_AUDIT_SHARD", "TVM_PARITY_VERSION_AUDIT_SHARDS")

	families := differentialFuzzFamiliesFromEnv(t, "TVM_PARITY_VERSION_AUDIT_FAMILIES", versionMatrixDifferentialFuzzFamilies())
	versionCount := MaxSupportedGlobalVersion - MinSupportedGlobalVersion + 1
	runCount := 0
	for familyIdx, family := range families {
		familyIdx, family := familyIdx, family
		t.Run(family, func(t *testing.T) {
			for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
				version := version
				t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
					for i := 0; i < seeds; i++ {
						globalIdx := (familyIdx*versionCount+version-MinSupportedGlobalVersion)*seeds + i
						if shards > 0 && globalIdx%shards != shard {
							continue
						}

						runCount++
						seed := differentialFuzzVersionMatrixAuditSeed(uint64(startInt), familyIdx, i, version)
						t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
							r := rand.New(rand.NewSource(int64(seed)))
							tc := generateDifferentialFuzzCaseWithFamily(t, r, seed, family)
							if got := differentialFuzzSeedVersion(seed); got != version {
								t.Fatalf("version matrix seed selected v%d, want v%d", got, version)
							}
							if !tc.globalVersionSet {
								t.Fatalf("version matrix family %q generated case without explicit global version", family)
							}
							if tc.globalVersion != version {
								t.Fatalf("version matrix generated v%d, want v%d", tc.globalVersion, version)
							}
							runDifferentialFuzzCase(t, tc)
						})
					}
				})
			}
		})
	}
	if runCount == 0 {
		t.Skipf("no version audit seeds selected for shard %d/%d across %d families x %d versions x %d seeds", shard, shards, len(families), versionCount, seeds)
	}
}

func TestTVMDifferentialFuzzFamiliesGenerateCases(t *testing.T) {
	for seed, family := range supportedDifferentialFuzzFamilies() {
		t.Run(family, func(t *testing.T) {
			r := rand.New(rand.NewSource(int64(seed + 1)))
			tc := generateDifferentialFuzzCaseWithFamily(t, r, uint64(seed+1), family)
			if tc.family == "" {
				t.Fatal("generated case has empty family")
			}
			if tc.op == "" {
				t.Fatal("generated case has empty op label")
			}
			if tc.code == nil {
				t.Fatal("generated case has nil code")
			}
		})
	}
}

func TestTVMDifferentialFuzzMixedCoversSupportedFamilies(t *testing.T) {
	want := map[string]int{}
	for _, family := range supportedDifferentialFuzzFamilies() {
		if family != "mixed" {
			want[family] = 0
		}
	}

	for _, family := range mixedDifferentialFuzzFamilies() {
		if _, ok := want[family]; !ok {
			t.Fatalf("mixed includes unsupported family %q", family)
		}
		want[family]++
	}

	for family, count := range want {
		if count == 0 {
			t.Fatalf("mixed does not include supported family %q", family)
		}
	}
	if want["program"] < mixedDifferentialProgramWeight {
		t.Fatalf("mixed program weight = %d, want at least %d", want["program"], mixedDifferentialProgramWeight)
	}
}

func TestTVMDifferentialFuzzVersionMatrixCoversVersionAwareFamilies(t *testing.T) {
	got := map[string]struct{}{}
	for _, family := range versionMatrixDifferentialFuzzFamilies() {
		if family == "mixed" || family == "program" {
			t.Fatalf("version matrix includes non-version-matrix family %q", family)
		}
		got[family] = struct{}{}
	}

	for _, family := range supportedDifferentialFuzzFamilies() {
		if family == "mixed" || family == "program" {
			continue
		}
		if _, ok := got[family]; !ok {
			t.Fatalf("version matrix does not include supported version-aware family %q", family)
		}
	}
}

func TestTVMDifferentialFuzzFamilyInventory(t *testing.T) {
	tests := []struct {
		name     string
		families []string
		count    int
		hash     string
	}{
		{
			name:     "supported",
			families: supportedDifferentialFuzzFamilies(),
			count:    expectedSupportedDifferentialFuzzFamilyCount,
			hash:     expectedSupportedDifferentialFuzzFamilyHash,
		},
		{
			name:     "version-matrix",
			families: versionMatrixDifferentialFuzzFamilies(),
			count:    expectedVersionMatrixDifferentialFuzzFamilyCount,
			hash:     expectedVersionMatrixDifferentialFuzzFamilyHash,
		},
		{
			name:     "mixed",
			families: mixedDifferentialFuzzFamilies(),
			count:    expectedMixedDifferentialFuzzFamilyCount,
			hash:     expectedMixedDifferentialFuzzFamilyHash,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.families) != tt.count {
				t.Fatalf("%s differential fuzz family count = %d, want %d", tt.name, len(tt.families), tt.count)
			}
			if got := parityWitnessManifestHash(tt.families); got != tt.hash {
				t.Fatalf("%s differential fuzz family hash = %s, want %s; families=%s", tt.name, got, tt.hash, strings.Join(tt.families, ", "))
			}
		})
	}
}

func TestTVMDifferentialFuzzVersionMatrixSeedsSelectRequestedVersion(t *testing.T) {
	for start := uint64(0); start < 5; start++ {
		for offset := 0; offset < 5; offset++ {
			for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
				seed := differentialFuzzVersionMatrixSeed(start, offset, version)
				if got := differentialFuzzSeedVersion(seed); got != version {
					t.Fatalf("seed start=%d offset=%d version=%d selected v%d", start, offset, version, got)
				}
			}
		}
	}
}

func TestTVMDifferentialFuzzVersionMatrixAuditSeedsIncludeFamily(t *testing.T) {
	families := versionMatrixDifferentialFuzzFamilies()
	if len(families) < 2 {
		t.Fatal("version matrix needs at least two families to verify family-aware seeds")
	}

	const seeds = 3
	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		seen := make(map[uint64]string, len(families)*seeds)
		for familyIdx, family := range families {
			for seedIdx := 0; seedIdx < seeds; seedIdx++ {
				seed := differentialFuzzVersionMatrixAuditSeed(0, familyIdx, seedIdx, version)
				if got := differentialFuzzSeedVersion(seed); got != version {
					t.Fatalf("%s seed %d selected v%d, want v%d", family, seed, got, version)
				}

				key := fmt.Sprintf("%s/seed_%d", family, seedIdx)
				if prev, ok := seen[seed]; ok {
					t.Fatalf("version matrix audit seed collision at v%d seed %d: %s and %s", version, seed, prev, key)
				}
				seen[seed] = key
			}
		}
	}
}

func TestTVMDifferentialFuzzVersionMatrixFamiliesSetExplicitVersions(t *testing.T) {
	for _, family := range versionMatrixDifferentialFuzzFamilies() {
		family := family
		t.Run(family, func(t *testing.T) {
			for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
				version := version
				t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
					seed := differentialFuzzVersionMatrixSeed(0, 0, version)
					r := rand.New(rand.NewSource(int64(seed)))
					tc := generateDifferentialFuzzCaseWithFamily(t, r, seed, family)
					if !tc.globalVersionSet {
						t.Fatalf("%s generated case without explicit global version", family)
					}
					if tc.globalVersion != version {
						t.Fatalf("%s generated v%d, want v%d", family, tc.globalVersion, version)
					}
				})
			}
		})
	}
}

func TestTVMDifferentialFuzzKnownReferenceMismatchReasonScope(t *testing.T) {
	tests := []struct {
		name    string
		tc      differentialFuzzCase
		version int
		want    bool
	}{
		{
			name:    "versioned control v14 duplicate",
			tc:      differentialFuzzCase{family: "program_versioned_control", op: "PUSH -> SAVECTR(4) -> SAVECTR(4)/v14"},
			version: 14,
			want:    true,
		},
		{
			name:    "generic versioned program v14 duplicate",
			tc:      differentialFuzzCase{family: "program_versioned", op: "PUSH -> SETCONTCTRMANY -> SETCONTCTRMANY/v14"},
			version: 14,
			want:    true,
		},
		{
			name:    "versioned control before v14",
			tc:      differentialFuzzCase{family: "program_versioned_control", op: "PUSH -> SAVECTR(4) -> SAVECTR(4)/v13"},
			version: 13,
		},
		{
			name:    "non program versioned family",
			tc:      differentialFuzzCase{family: "msg_address_versioned", op: "PUSH -> SAVECTR(4) -> SAVECTR(4)/v14"},
			version: 14,
		},
		{
			name:    "versioned program without control duplicate",
			tc:      differentialFuzzCase{family: "program_versioned_control", op: "PUSH -> ADD/v14"},
			version: 14,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := differentialFuzzKnownReferenceMismatchReason(tt.tc, tt.version) != ""
			if got != tt.want {
				t.Fatalf("known reference mismatch reason present = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTVMDifferentialFuzzVersionMatrixC7ParamFamiliesUseComparableContext(t *testing.T) {
	c7Families := map[string]struct{}{
		"program_versioned_actions":       {},
		"program_versioned_c7":            {},
		"program_versioned_msg_address":   {},
		"program_versioned_prng":          {},
		"program_versioned_rich_c7":       {},
		"program_versioned_runvm_rich_c7": {},
		"program_versioned_runtime":       {},
		"program_versioned_supercontract": {},
	}
	for family := range c7Families {
		family := family
		t.Run(family, func(t *testing.T) {
			for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
				seed := differentialFuzzVersionMatrixSeed(0, 0, version)
				r := rand.New(rand.NewSource(int64(seed)))
				tc := generateDifferentialFuzzCaseWithFamily(t, r, seed, family)
				if !differentialFuzzCaseHasComparableC7(tc) {
					t.Fatalf("%s v%d generated without comparable C7 context", family, version)
				}
			}
		})
	}

	for _, family := range versionMatrixDifferentialFuzzFamilies() {
		family := family
		t.Run("trace_"+family, func(t *testing.T) {
			for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
				for offset := 0; offset < 32; offset++ {
					seed := differentialFuzzVersionMatrixSeed(0, offset, version)
					r := rand.New(rand.NewSource(int64(seed)))
					tc := generateDifferentialFuzzCaseWithFamily(t, r, seed, family)
					if differentialFuzzTraceUsesParentC7(tc.op) && !differentialFuzzCaseHasComparableC7(tc) {
						t.Fatalf("%s v%d seed %d uses parent C7 without comparable context: %s", family, version, seed, tc.op)
					}
				}
			}
		})
	}
}

func differentialFuzzCaseHasComparableC7(tc differentialFuzzCase) bool {
	return tc.refCfg != nil || !tc.c7.IsNull()
}

func differentialFuzzTraceUsesParentC7(trace string) bool {
	for _, token := range []string{
		"BALANCE",
		"BLOCKLT",
		"CONFIGDICT",
		"CONFIGOPTPARAM(",
		"CONFIGPARAM(",
		"CONFIGROOT",
		"DUEPAYMENT",
		"GETFORWARDFEE",
		"GETFORWARDFEESIMPLE",
		"GETGASFEE",
		"GETGASFEESIMPLE",
		"GETORIGINALFWDFEE",
		"GETPARAM(",
		"GETPARAMLONG(",
		"GETPRECOMPILEDGAS",
		"GETSTORAGEFEE",
		"GLOBALID",
		"INCOMINGVALUE",
		"INMSG_",
		"INMSGPARAM",
		"LTIME",
		"MYADDR",
		"MYCODE",
		"NOW",
		"PREVBLOCKSINFOTUPLE",
		"PREVKEYBLOCK",
		"PREVMCBLOCKS",
		"RANDSEED",
		"STORAGEFEES",
		"UNPACKEDCONFIGTUPLE",
	} {
		if strings.Contains(trace, token) {
			return true
		}
	}
	return false
}

func TestTVMDifferentialFuzzFamilyEnvFilter(t *testing.T) {
	families := []string{"datasize", "math", "program_versioned_runvm_rich_c7"}

	got, err := differentialFuzzFamiliesFromRaw("TVM_TEST_FAMILIES", "", families)
	if err != nil {
		t.Fatalf("unset filter failed: %v", err)
	}
	if strings.Join(got, ",") != strings.Join(families, ",") {
		t.Fatalf("unset filter = %v, want %v", got, families)
	}

	got, err = differentialFuzzFamiliesFromRaw("TVM_TEST_FAMILIES", "math, datasize\nmath\tprogram_versioned_runvm_rich_c7", families)
	if err != nil {
		t.Fatalf("filtered families failed: %v", err)
	}
	if want := "math,datasize,program_versioned_runvm_rich_c7"; strings.Join(got, ",") != want {
		t.Fatalf("filtered families = %v, want %s", got, want)
	}

	if _, err = differentialFuzzFamiliesFromRaw("TVM_TEST_FAMILIES", "math,unknown", families); err == nil || !strings.Contains(err.Error(), "unsupported family") {
		t.Fatalf("unknown family error = %v", err)
	}
	if _, err = differentialFuzzFamiliesFromRaw("TVM_TEST_FAMILIES", ", \n\t", families); err == nil || !strings.Contains(err.Error(), "at least one family") {
		t.Fatalf("empty filter error = %v", err)
	}
}

func TestTVMDifferentialFuzzShardEnvParser(t *testing.T) {
	tests := []struct {
		name       string
		rawShard   string
		rawShards  string
		wantShard  int
		wantShards int
		wantErr    string
	}{
		{name: "unset"},
		{name: "valid", rawShard: "2", rawShards: "5", wantShard: 2, wantShards: 5},
		{name: "missing shard", rawShards: "5", wantErr: "must be set together"},
		{name: "missing shards", rawShard: "2", wantErr: "must be set together"},
		{name: "bad shards", rawShard: "0", rawShards: "x", wantErr: "positive integer"},
		{name: "zero shards", rawShard: "0", rawShards: "0", wantErr: "positive integer"},
		{name: "negative shard", rawShard: "-1", rawShards: "5", wantErr: "must be in [0,5)"},
		{name: "shard too large", rawShard: "5", rawShards: "5", wantErr: "must be in [0,5)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotShard, gotShards, err := parityFuzzShardFromEnv("TVM_TEST_SHARD", tt.rawShard, "TVM_TEST_SHARDS", tt.rawShards)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotShard != tt.wantShard || gotShards != tt.wantShards {
				t.Fatalf("shard parse = (%d, %d), want (%d, %d)", gotShard, gotShards, tt.wantShard, tt.wantShards)
			}
		})
	}
}

func TestTVMDifferentialFuzzAllFamiliesAuditShardPartition(t *testing.T) {
	families := supportedDifferentialFuzzFamilies()
	if len(families) == 0 {
		t.Fatal("supported differential fuzz family list is empty")
	}

	const seeds = 7
	want := make(map[string]struct{}, len(families)*seeds)
	for familyIdx, family := range families {
		for seedIdx := 0; seedIdx < seeds; seedIdx++ {
			want[fmt.Sprintf("%03d/%s/seed_%d", familyIdx, family, seedIdx)] = struct{}{}
		}
	}

	for _, shards := range []int{1, 2, 3, 7, 17} {
		seen := make(map[string]int, len(want))
		for shard := 0; shard < shards; shard++ {
			for familyIdx, family := range families {
				for seedIdx := 0; seedIdx < seeds; seedIdx++ {
					globalIdx := familyIdx*seeds + seedIdx
					if globalIdx%shards != shard {
						continue
					}
					key := fmt.Sprintf("%03d/%s/seed_%d", familyIdx, family, seedIdx)
					seen[key]++
				}
			}
		}

		assertDifferentialFuzzShardPartition(t, "all-families audit", shards, want, seen)
	}
}

func TestTVMDifferentialFuzzVersionMatrixAuditShardPartition(t *testing.T) {
	families := versionMatrixDifferentialFuzzFamilies()
	if len(families) == 0 {
		t.Fatal("version-matrix differential fuzz family list is empty")
	}

	const seeds = 5
	versionCount := MaxSupportedGlobalVersion - MinSupportedGlobalVersion + 1
	want := make(map[string]struct{}, len(families)*versionCount*seeds)
	for familyIdx, family := range families {
		for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
			for seedIdx := 0; seedIdx < seeds; seedIdx++ {
				want[fmt.Sprintf("%03d/%s/v%d/seed_%d", familyIdx, family, version, seedIdx)] = struct{}{}
			}
		}
	}

	for _, shards := range []int{1, 2, 3, 7, 17} {
		seen := make(map[string]int, len(want))
		for shard := 0; shard < shards; shard++ {
			for familyIdx, family := range families {
				for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
					for seedIdx := 0; seedIdx < seeds; seedIdx++ {
						globalIdx := (familyIdx*versionCount+version-MinSupportedGlobalVersion)*seeds + seedIdx
						if globalIdx%shards != shard {
							continue
						}

						seed := differentialFuzzVersionMatrixAuditSeed(0, familyIdx, seedIdx, version)
						if got := differentialFuzzSeedVersion(seed); got != version {
							t.Fatalf("version-matrix partition seed %d selected v%d, want v%d", seed, got, version)
						}
						key := fmt.Sprintf("%03d/%s/v%d/seed_%d", familyIdx, family, version, seedIdx)
						seen[key]++
					}
				}
			}
		}

		assertDifferentialFuzzShardPartition(t, "version-matrix audit", shards, want, seen)
	}
}

func TestTVMDifferentialFuzzVersionMatrixFamiliesGenerateConfiguredVersions(t *testing.T) {
	families := versionMatrixDifferentialFuzzFamilies()
	if len(families) == 0 {
		t.Fatal("version-matrix differential fuzz family list is empty")
	}

	for familyIdx, family := range families {
		for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
			for seedOffset := 0; seedOffset < 3; seedOffset++ {
				seed := differentialFuzzVersionMatrixSeed(uint64(familyIdx+1)*32, seedOffset, version)
				r := rand.New(rand.NewSource(int64(seed)))
				tc := generateDifferentialFuzzCaseWithFamily(t, r, seed, family)

				if got := differentialFuzzSeedVersion(seed); got != version {
					t.Fatalf("%s seed %d selected v%d, want v%d", family, seed, got, version)
				}
				if !tc.globalVersionSet {
					t.Fatalf("%s seed %d generated case without explicit global version", family, seed)
				}
				if tc.globalVersion != version {
					t.Fatalf("%s seed %d generated v%d, want v%d", family, seed, tc.globalVersion, version)
				}
			}
		}
	}
}

func assertDifferentialFuzzShardPartition(t *testing.T, name string, shards int, want map[string]struct{}, seen map[string]int) {
	t.Helper()

	if len(seen) != len(want) {
		t.Fatalf("%s %d-way sharding covered %d runs, want %d", name, shards, len(seen), len(want))
	}
	for key := range want {
		if seen[key] != 1 {
			t.Fatalf("%s %d-way sharding covered %s %d times", name, shards, key, seen[key])
		}
	}
	for key, count := range seen {
		if _, ok := want[key]; !ok {
			t.Fatalf("%s %d-way sharding produced unexpected run %s count=%d", name, shards, key, count)
		}
	}
}

func TestTVMDifferentialFuzzMixedSeedWindowReachesSupportedFamilies(t *testing.T) {
	missing := map[string]struct{}{}
	for _, family := range supportedDifferentialFuzzFamilies() {
		if family != "mixed" {
			missing[family] = struct{}{}
		}
	}

	for seed := uint64(0); seed < 512 && len(missing) > 0; seed++ {
		r := rand.New(rand.NewSource(int64(seed)))
		family := pickMixedDifferentialFuzzFamily(t, r)
		tc := generateDifferentialFuzzCaseWithFamily(t, r, seed, family)
		if tc.family == "" || tc.code == nil {
			t.Fatalf("mixed seed %d generated invalid case: %+v", seed, tc)
		}
		delete(missing, family)
	}

	if len(missing) == 0 {
		return
	}
	left := make([]string, 0, len(missing))
	for family := range missing {
		left = append(left, family)
	}
	sort.Strings(left)
	t.Fatalf("mixed seed window did not reach families: %s", strings.Join(left, ", "))
}

func TestTVMDifferentialFuzzFamiliesCrossEmulatorSmoke(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	versionMatrixFamilies := map[string]struct{}{}
	for _, family := range versionMatrixDifferentialFuzzFamilies() {
		versionMatrixFamilies[family] = struct{}{}
	}
	for familyIdx, family := range supportedDifferentialFuzzFamilies() {
		if _, ok := versionMatrixFamilies[family]; !ok {
			seed := uint64(familyIdx) * 16
			t.Run(fmt.Sprintf("%s/default/seed_%d", family, seed), func(t *testing.T) {
				r := rand.New(rand.NewSource(int64(seed)))
				tc := generateDifferentialFuzzCaseWithFamily(t, r, seed, family)
				runDifferentialFuzzCase(t, tc)
			})
			continue
		}

		for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
			seed := tvmFuzzGlobalVersionMatrixSeed(uint64(familyIdx)*16, 0, version)
			t.Run(fmt.Sprintf("%s/v%d/seed_%d", family, version, seed), func(t *testing.T) {
				r := rand.New(rand.NewSource(int64(seed)))
				tc := generateDifferentialFuzzCaseWithFamily(t, r, seed, family)
				if !tc.globalVersionSet {
					t.Fatalf("%s generated case without explicit global version", family)
				}
				if tc.globalVersion != version {
					t.Fatalf("%s generated v%d, want v%d", family, tc.globalVersion, version)
				}
				runDifferentialFuzzCase(t, tc)
			})
		}
	}
}

func TestTVMDifferentialFuzzVersionedMsgAddressC7ParamRegression(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	t.Setenv("TVM_PARITY_PROGRAM_OPS", "14")

	seed := uint64(2894)
	r := rand.New(rand.NewSource(int64(seed)))
	tc := generateDifferentialFuzzCaseWithFamily(t, r, seed, "program_versioned_msg_address")
	if tc.globalVersion != 14 {
		t.Fatalf("seed %d generated global version %d, want 14", seed, tc.globalVersion)
	}
	if !strings.Contains(tc.op, "PREVMCBLOCKS_100") {
		t.Fatalf("seed %d no longer reaches PREVMCBLOCKS_100: %s", seed, tc.op)
	}
	runDifferentialFuzzCase(t, tc)
}

type differentialFuzzVersionMatrixProgramSeed struct {
	familyRaw  uint16
	versionRaw uint8
	seedRaw    uint64
}

func differentialFuzzVersionMatrixProgramSeeds(families []string) []differentialFuzzVersionMatrixProgramSeed {
	seeds := make([]differentialFuzzVersionMatrixProgramSeed, 0, len(families)*tvmFuzzGlobalVersionCount()+1)
	for familyIdx := range families {
		for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
			seeds = append(seeds, differentialFuzzVersionMatrixProgramSeed{
				familyRaw:  uint16(familyIdx),
				versionRaw: uint8(version),
				seedRaw:    uint64(familyIdx+1)*versionMatrixFamilySeedStride + uint64(version),
			})
		}
	}
	return append(seeds, differentialFuzzVersionMatrixProgramSeed{
		familyRaw:  uint16(len(families) + 17),
		versionRaw: uint8(255),
		seedRaw:    uint64(1 << 40),
	})
}

func TestTVMDifferentialVersionMatrixProgramFuzzerSeedsCoverVersionsAndFamilies(t *testing.T) {
	families := versionMatrixDifferentialFuzzFamilies()
	if len(families) == 0 {
		t.Fatal("version matrix differential fuzz family list is empty")
	}

	versionSeen := map[int]struct{}{}
	familyVersionSeen := make([]map[int]struct{}, len(families))
	for idx := range familyVersionSeen {
		familyVersionSeen[idx] = map[int]struct{}{}
	}

	seeds := differentialFuzzVersionMatrixProgramSeeds(families)
	for _, seed := range seeds {
		familyIdx, version, _ := differentialFuzzVersionMatrixProgramCase(t, families, seed)

		versionSeen[version] = struct{}{}
		familyVersionSeen[familyIdx][version] = struct{}{}
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		if _, ok := versionSeen[version]; !ok {
			t.Fatalf("version matrix fuzzer baseline seeds do not cover v%d", version)
		}
	}
	for familyIdx, family := range families {
		for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
			if _, ok := familyVersionSeen[familyIdx][version]; !ok {
				t.Fatalf("version matrix fuzzer baseline seeds do not cover %s v%d", family, version)
			}
		}
	}
}

func TestTVMDifferentialVersionMatrixProgramFuzzerBaselineCrossEmulatorSmoke(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	families := versionMatrixDifferentialFuzzFamilies()
	if len(families) == 0 {
		t.Fatal("version matrix differential fuzz family list is empty")
	}

	for _, seed := range differentialFuzzVersionMatrixProgramSeeds(families) {
		seed := seed
		familyIdx := int(seed.familyRaw) % len(families)
		version := tvmFuzzGlobalVersionByte(seed.versionRaw)
		t.Run(fmt.Sprintf("%03d_%s/v%d/seed_%d", familyIdx, families[familyIdx], version, seed.seedRaw), func(t *testing.T) {
			_, _, tc := differentialFuzzVersionMatrixProgramCase(t, families, seed)
			runDifferentialFuzzCase(t, tc)
		})
	}
}

func differentialFuzzVersionMatrixProgramCase(t *testing.T, families []string, seed differentialFuzzVersionMatrixProgramSeed) (int, int, differentialFuzzCase) {
	t.Helper()

	familyIdx := int(seed.familyRaw) % len(families)
	version := tvmFuzzGlobalVersionByte(seed.versionRaw)
	matrixSeed := differentialFuzzVersionMatrixSeed(seed.seedRaw%(1<<40), familyIdx, version)
	r := rand.New(rand.NewSource(int64(matrixSeed)))
	tc := generateDifferentialFuzzCaseWithFamily(t, r, matrixSeed, families[familyIdx])

	if got := differentialFuzzSeedVersion(matrixSeed); got != version {
		t.Fatalf("version matrix fuzzer seed selected v%d, want v%d", got, version)
	}
	if !tc.globalVersionSet {
		t.Fatalf("version matrix fuzzer seed generated %s without explicit global version", families[familyIdx])
	}
	if tc.globalVersion != version {
		t.Fatalf("version matrix fuzzer seed generated %s v%d, want v%d", families[familyIdx], tc.globalVersion, version)
	}
	return familyIdx, version, tc
}

func FuzzTVMDifferentialVersionMatrixPrograms(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	families := versionMatrixDifferentialFuzzFamilies()
	if len(families) == 0 {
		f.Fatal("version matrix differential fuzz family list is empty")
	}

	for _, seed := range differentialFuzzVersionMatrixProgramSeeds(families) {
		f.Add(seed.familyRaw, seed.versionRaw, seed.seedRaw)
	}

	f.Fuzz(func(t *testing.T, familyRaw uint16, versionRaw uint8, seedRaw uint64) {
		_, _, tc := differentialFuzzVersionMatrixProgramCase(t, families, differentialFuzzVersionMatrixProgramSeed{
			familyRaw:  familyRaw,
			versionRaw: versionRaw,
			seedRaw:    seedRaw,
		})
		runDifferentialFuzzCase(t, tc)
	})
}

func TestTVMDifferentialFuzzExplicitGlobalVersionZero(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tc := differentialFuzzWithGlobalVersion(t, differentialFuzzCase{
		seed:   0,
		family: "version",
		op:     "explicit_v0_rejects_chashi",
		code:   codeFromBuilders(t, cellsliceop.CHASHI(0).Serialize()),
		stack:  []any{testEmptyCell()},
	}, 0)
	if !tc.globalVersionSet || tc.globalVersion != 0 {
		t.Fatalf("explicit v0 case lost global version: set=%v version=%d", tc.globalVersionSet, tc.globalVersion)
	}
	if tc.refCfg == nil {
		t.Fatal("explicit v0 case must use config-aware reference runner")
	}

	runDifferentialFuzzCase(t, tc)
}

func TestTVMDifferentialFuzzRawRichC7ExplicitGlobalVersionZero(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	runDifferentialFuzzCase(t, differentialFuzzCase{
		seed:             0,
		family:           "version",
		op:               "raw_rich_c7_explicit_v0_rejects_chashi",
		code:             codeFromBuilders(t, cellsliceop.CHASHI(0).Serialize()),
		stack:            []any{testEmptyCell()},
		globalVersion:    0,
		globalVersionSet: true,
		rawC7Versioned:   true,
		c7:               parityProgramVersionedRichC7(t, 0),
	})
}

func TestTVMDifferentialFuzzOpcodeInventoryIsClassified(t *testing.T) {
	if len(vm.List) == 0 {
		t.Fatal("vm.List is empty")
	}
	if len(vm.List) != expectedParityOpcodeInventoryEntries {
		t.Fatalf("registered opcode inventory size changed: got %d want %d", len(vm.List), expectedParityOpcodeInventoryEntries)
	}

	buckets := map[string]int{}
	seen := map[string]struct{}{}
	var missing []string
	for i, getter := range vm.List {
		op := getter()
		if op == nil {
			missing = append(missing, fmt.Sprintf("%03d <nil op>", i))
			continue
		}

		name := op.SerializeText()
		if name == "" {
			missing = append(missing, fmt.Sprintf("%03d <empty SerializeText>", i))
			continue
		}

		bucket, ok := parityOpcodeCoverageBucket(name)
		if !ok {
			missing = append(missing, fmt.Sprintf("%03d %s", i, name))
			continue
		}
		buckets[bucket]++
		seen[name] = struct{}{}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("unclassified TVM opcode inventory entries:\n%s", strings.Join(missing, "\n"))
	}
	if len(seen) != expectedParityOpcodeInventoryUniqueNames {
		t.Fatalf("classified opcode inventory unique name count changed: got %d want %d", len(seen), expectedParityOpcodeInventoryUniqueNames)
	}
	if len(buckets) != len(expectedParityOpcodeCoverageBucketCounts) {
		t.Fatalf("opcode inventory bucket set changed: got %d buckets want %d", len(buckets), len(expectedParityOpcodeCoverageBucketCounts))
	}
	for bucket, want := range expectedParityOpcodeCoverageBucketCounts {
		if got := buckets[bucket]; got != want {
			t.Fatalf("opcode inventory bucket %s count changed: got %d want %d", bucket, got, want)
		}
	}
	for bucket := range buckets {
		if _, ok := expectedParityOpcodeCoverageBucketCounts[bucket]; !ok {
			t.Fatalf("unexpected opcode inventory bucket %s with %d entries", bucket, buckets[bucket])
		}
	}
}

func TestTVMDifferentialFuzzWitnessManifestsAreStable(t *testing.T) {
	manifests := parityWitnessManifestLists()
	sourceNames := parityWitnessManifestNamesFromSource(t)
	if len(manifests) != len(expectedParityWitnessManifestExpectations) {
		t.Fatalf("witness manifest set size changed: got %d want %d", len(manifests), len(expectedParityWitnessManifestExpectations))
	}

	var failures []string
	if len(sourceNames) != len(expectedParityWitnessManifestExpectations) {
		failures = append(failures, fmt.Sprintf("source manifest set size got %d want %d", len(sourceNames), len(expectedParityWitnessManifestExpectations)))
	}
	for name, expected := range expectedParityWitnessManifestExpectations {
		if _, ok := sourceNames[name]; !ok {
			failures = append(failures, fmt.Sprintf("%s missing from source manifest scan", name))
		}
		items, ok := manifests[name]
		if !ok {
			failures = append(failures, fmt.Sprintf("%s missing", name))
			continue
		}
		if len(items) != expected.count {
			failures = append(failures, fmt.Sprintf("%s count got %d want %d", name, len(items), expected.count))
			continue
		}
		if got := parityWitnessManifestHash(items); got != expected.hash {
			failures = append(failures, fmt.Sprintf("%s hash got %s want %s", name, got, expected.hash))
		}
	}
	for name := range manifests {
		if _, ok := expectedParityWitnessManifestExpectations[name]; !ok {
			failures = append(failures, fmt.Sprintf("%s unexpected", name))
		}
		if _, ok := sourceNames[name]; !ok {
			failures = append(failures, fmt.Sprintf("%s missing from source manifest declarations", name))
		}
	}
	for name := range sourceNames {
		if _, ok := expectedParityWitnessManifestExpectations[name]; !ok {
			failures = append(failures, fmt.Sprintf("%s source manifest missing expectation", name))
		}
		if _, ok := manifests[name]; !ok {
			failures = append(failures, fmt.Sprintf("%s source manifest missing actual list", name))
		}
	}
	if len(failures) > 0 {
		sort.Strings(failures)
		t.Fatalf("witness manifest drift:\n%s", strings.Join(failures, "\n"))
	}
}

func TestTVMDifferentialFuzzWitnessManifestsHaveUniqueItems(t *testing.T) {
	manifests := parityWitnessManifestLists()

	var failures []string
	for name, items := range manifests {
		seen := map[string]int{}
		for idx, item := range items {
			if item == "" {
				failures = append(failures, fmt.Sprintf("%s[%d] is empty", name, idx))
				continue
			}
			if prev, ok := seen[item]; ok {
				failures = append(failures, fmt.Sprintf("%s duplicates %q at indexes %d and %d", name, item, prev, idx))
				continue
			}
			seen[item] = idx
		}
	}
	if len(failures) > 0 {
		sort.Strings(failures)
		t.Fatalf("witness manifest item drift:\n%s", strings.Join(failures, "\n"))
	}
}

func TestTVMDifferentialFuzzCryptoOpcodeInventoryHasWitnesses(t *testing.T) {
	inventory := parityOpcodeInventoryNamesByBucket(t, "deterministic_ton_crypto")
	if len(inventory) != len(expectedParityCryptoOpcodeWitnessCases) {
		t.Fatalf("crypto opcode witness inventory size changed: got %d want %d", len(inventory), len(expectedParityCryptoOpcodeWitnessCases))
	}

	witnessCases := parityNamedWitnessCases(
		requiredHashGapCaseNames,
		requiredCryptoGapCaseNames,
		requiredCryptoEdgeGapCaseNames,
		requiredCirclCryptoGapCaseNames,
	)

	var failures []string
	for opcode, cases := range expectedParityCryptoOpcodeWitnessCases {
		if _, ok := inventory[opcode]; !ok {
			failures = append(failures, fmt.Sprintf("%s missing from crypto opcode inventory", opcode))
			continue
		}
		for _, name := range cases {
			if _, ok := witnessCases[name]; !ok {
				failures = append(failures, fmt.Sprintf("%s witness case %s is missing", opcode, name))
			}
		}
	}
	for opcode := range inventory {
		if _, ok := expectedParityCryptoOpcodeWitnessCases[opcode]; !ok {
			failures = append(failures, fmt.Sprintf("%s missing crypto witness mapping", opcode))
		}
	}
	if len(failures) > 0 {
		sort.Strings(failures)
		t.Fatalf("crypto opcode witness drift:\n%s", strings.Join(failures, "\n"))
	}
}

func TestTVMDifferentialFuzzRuntimeOpcodeInventoryHasWitnesses(t *testing.T) {
	inventory := parityOpcodeInventoryNamesByBucket(t, "deterministic_ton_runtime")
	if len(inventory) != len(expectedParityRuntimeOpcodeWitnessCases) {
		t.Fatalf("runtime opcode witness inventory size changed: got %d want %d", len(inventory), len(expectedParityRuntimeOpcodeWitnessCases))
	}

	witnessCases := parityNamedWitnessCases(
		requiredTonFuncGapCaseNames,
		requiredTonFuncRuntimeGapCaseNames,
		requiredDataSizeGapCaseNames,
		requiredActionModeGapCaseNames,
		requiredActionErrorGapCaseNames,
		requiredInventoryResidualGapCaseNames,
	)

	var failures []string
	for opcode, cases := range expectedParityRuntimeOpcodeWitnessCases {
		if _, ok := inventory[opcode]; !ok {
			failures = append(failures, fmt.Sprintf("%s missing from runtime opcode inventory", opcode))
			continue
		}
		for _, name := range cases {
			if _, ok := witnessCases[name]; !ok {
				failures = append(failures, fmt.Sprintf("%s witness case %s is missing", opcode, name))
			}
		}
	}
	for opcode := range inventory {
		if _, ok := expectedParityRuntimeOpcodeWitnessCases[opcode]; !ok {
			failures = append(failures, fmt.Sprintf("%s missing runtime witness mapping", opcode))
		}
	}
	if len(failures) > 0 {
		sort.Strings(failures)
		t.Fatalf("runtime opcode witness drift:\n%s", strings.Join(failures, "\n"))
	}
}

func TestTVMDifferentialFuzzTupleOpcodeInventoryHasWitnesses(t *testing.T) {
	inventory := parityOpcodeInventoryNamesByBucket(t, "dedicated_tuple")
	if len(inventory) != len(expectedParityTupleOpcodeWitnessCases) {
		t.Fatalf("tuple opcode witness inventory size changed: got %d want %d", len(inventory), len(expectedParityTupleOpcodeWitnessCases))
	}

	witnessCases := parityNamedWitnessCases(
		requiredTupleGapCaseNames,
		requiredTupleOpcodeSpaceGapCaseNames,
		requiredTupleDynamicErrorGapCaseNames,
		requiredTupleGapTraceLabels,
	)

	var failures []string
	for opcode, cases := range expectedParityTupleOpcodeWitnessCases {
		if _, ok := inventory[opcode]; !ok {
			failures = append(failures, fmt.Sprintf("%s missing from tuple opcode inventory", opcode))
			continue
		}
		for _, name := range cases {
			if _, ok := witnessCases[name]; !ok {
				failures = append(failures, fmt.Sprintf("%s witness item %s is missing", opcode, name))
			}
		}
	}
	for opcode := range inventory {
		if _, ok := expectedParityTupleOpcodeWitnessCases[opcode]; !ok {
			failures = append(failures, fmt.Sprintf("%s missing tuple witness mapping", opcode))
		}
	}
	if len(failures) > 0 {
		sort.Strings(failures)
		t.Fatalf("tuple opcode witness drift:\n%s", strings.Join(failures, "\n"))
	}
}

func TestTVMDifferentialFuzzStackOpcodeInventoryHasWitnesses(t *testing.T) {
	inventory := parityOpcodeInventoryNamesByBucket(t, "random_stack")
	witnessCases := parityStackOpcodeWitnessItems()

	var failures []string
	if len(inventory) != len(expectedParityStackOpcodeWitnessCases) {
		failures = append(failures, fmt.Sprintf("stack opcode witness inventory size got %d want %d", len(inventory), len(expectedParityStackOpcodeWitnessCases)))
	}
	for opcode, cases := range expectedParityStackOpcodeWitnessCases {
		if _, ok := inventory[opcode]; !ok {
			failures = append(failures, fmt.Sprintf("%s missing from stack opcode inventory", opcode))
			continue
		}
		for _, name := range cases {
			if _, ok := witnessCases[name]; !ok {
				failures = append(failures, fmt.Sprintf("%s witness item %s is missing", opcode, name))
			}
		}
	}
	for opcode := range inventory {
		if _, ok := expectedParityStackOpcodeWitnessCases[opcode]; !ok {
			failures = append(failures, fmt.Sprintf("%s missing stack witness mapping", opcode))
		}
	}
	if len(failures) > 0 {
		sort.Strings(failures)
		t.Fatalf("stack opcode witness drift:\n%s", strings.Join(failures, "\n"))
	}
}

func TestTVMDifferentialFuzzExecOpcodeInventoryHasWitnesses(t *testing.T) {
	inventory := parityOpcodeInventoryNamesByBucket(t, "deterministic_exec")
	witnessCases := parityExecOpcodeWitnessItems()

	var failures []string
	if len(inventory) != len(expectedParityExecOpcodeWitnessCases) {
		failures = append(failures, fmt.Sprintf("exec opcode witness inventory size got %d want %d", len(inventory), len(expectedParityExecOpcodeWitnessCases)))
	}
	for opcode, cases := range expectedParityExecOpcodeWitnessCases {
		if _, ok := inventory[opcode]; !ok {
			failures = append(failures, fmt.Sprintf("%s missing from exec opcode inventory", opcode))
			continue
		}
		for _, name := range cases {
			if _, ok := witnessCases[name]; !ok {
				failures = append(failures, fmt.Sprintf("%s witness item %s is missing", opcode, name))
			}
		}
	}
	for opcode := range inventory {
		if _, ok := expectedParityExecOpcodeWitnessCases[opcode]; !ok {
			failures = append(failures, fmt.Sprintf("%s missing exec witness mapping", opcode))
		}
	}
	if len(failures) > 0 {
		sort.Strings(failures)
		t.Fatalf("exec opcode witness drift:\n%s", strings.Join(failures, "\n"))
	}
}

func TestTVMDifferentialFuzzDictOpcodeInventoryHasWitnesses(t *testing.T) {
	inventory := parityOpcodeInventoryNamesByBucket(t, "random_dict")
	witnessItems := parityDictOpcodeWitnessItems(t)

	var failures []string
	if len(inventory) != expectedParityDictOpcodeWitnessNames {
		failures = append(failures, fmt.Sprintf("dict opcode witness inventory size got %d want %d", len(inventory), expectedParityDictOpcodeWitnessNames))
	}
	for opcode := range inventory {
		key := parityDictOpcodeInventoryWitnessKey(opcode)
		if key == "" {
			failures = append(failures, fmt.Sprintf("%s failed to normalize dict witness key", opcode))
			continue
		}
		if !parityDictOpcodeHasWitness(key, witnessItems) {
			failures = append(failures, fmt.Sprintf("%s missing dict witness key %s", opcode, key))
		}
	}
	if len(failures) > 0 {
		sort.Strings(failures)
		t.Fatalf("dict opcode witness drift:\n%s", strings.Join(failures, "\n"))
	}
}

func TestTVMDifferentialFuzzCellSliceOpcodeInventoryHasWitnesses(t *testing.T) {
	inventory := parityOpcodeInventoryNamesByBucket(t, "random_cell_slice")
	witnessItems := parityCellSliceOpcodeWitnessItems(t)

	var failures []string
	if len(inventory) != expectedParityCellSliceOpcodeWitnessNames {
		failures = append(failures, fmt.Sprintf("cell/slice opcode witness inventory size got %d want %d", len(inventory), expectedParityCellSliceOpcodeWitnessNames))
	}
	for opcode := range inventory {
		key := parityCellSliceOpcodeInventoryWitnessKey(opcode)
		if key == "" {
			failures = append(failures, fmt.Sprintf("%s failed to normalize cell/slice witness key", opcode))
			continue
		}
		if !parityCellSliceOpcodeHasWitness(key, witnessItems) {
			failures = append(failures, fmt.Sprintf("%s missing cell/slice witness key %s", opcode, key))
		}
	}
	if len(failures) > 0 {
		sort.Strings(failures)
		t.Fatalf("cell/slice opcode witness drift:\n%s", strings.Join(failures, "\n"))
	}
}

func TestTVMDifferentialFuzzMathOpcodeInventoryHasWitnesses(t *testing.T) {
	inventory := parityOpcodeInventoryNamesByBucket(t, "random_math")
	witnessKeys := parityMathOpcodeWitnessItems(t)

	var failures []string
	if len(inventory) != expectedParityMathOpcodeWitnessNames {
		failures = append(failures, fmt.Sprintf("math opcode witness inventory size got %d want %d", len(inventory), expectedParityMathOpcodeWitnessNames))
	}
	for opcode := range inventory {
		key := parityMathOpcodeInventoryWitnessKey(opcode)
		if key == "" {
			failures = append(failures, fmt.Sprintf("%s failed to normalize math witness key", opcode))
			continue
		}
		if _, ok := witnessKeys[key]; !ok {
			failures = append(failures, fmt.Sprintf("%s missing math witness key %s", opcode, key))
		}
	}
	if len(failures) > 0 {
		sort.Strings(failures)
		t.Fatalf("math opcode witness drift:\n%s", strings.Join(failures, "\n"))
	}
}

func parityOpcodeInventoryNamesByBucket(t *testing.T, bucket string) map[string]struct{} {
	t.Helper()

	names := map[string]struct{}{}
	for i, getter := range vm.List {
		op := getter()
		if op == nil {
			t.Fatalf("vm.List[%d] returned nil op", i)
		}
		name := op.SerializeText()
		gotBucket, ok := parityOpcodeCoverageBucket(name)
		if !ok {
			t.Fatalf("vm.List[%d] %q has no coverage bucket", i, name)
		}
		if gotBucket == bucket {
			names[name] = struct{}{}
		}
	}
	return names
}

func parityNamedWitnessCases(lists ...[]string) map[string]struct{} {
	cases := map[string]struct{}{}
	for _, list := range lists {
		for _, name := range list {
			cases[name] = struct{}{}
		}
	}
	return cases
}

func parityStackOpcodeWitnessItems() map[string]struct{} {
	cases := parityNamedWitnessCases(
		requiredStackGapCaseNames,
		requiredStackOpcodeSpaceGapCaseNames,
		requiredStackDynamicDepthGapCaseNames,
		requiredInventoryResidualGapCaseNames,
		requiredMathImmediateGapCaseNames,
		requiredExecGapCaseNames,
	)
	for _, tc := range buildStackOpsOpcodeSpaceCases() {
		cases[tc.name] = struct{}{}
	}
	for _, tc := range buildStackOpsDynamicAndDepthEdgeCases() {
		cases[tc.name] = struct{}{}
	}
	return cases
}

func parityExecOpcodeWitnessItems() map[string]struct{} {
	return parityNamedWitnessCases(
		requiredExecGapCaseNames,
		requiredExecRefDecodeGapCaseNames,
		requiredRunVMGapCaseNames,
		requiredStackGapCaseNames,
		requiredInventoryResidualGapCaseNames,
		requiredTonFuncRuntimeGapCaseNames,
	)
}

func parityDictOpcodeWitnessItems(t *testing.T) []string {
	t.Helper()

	lists := [][]string{
		requiredDictProgramTraceLabels,
		requiredDictGapTraceLabels,
		requiredDictNearGapTraceLabels,
		requiredDictSuccessGapCaseNames,
		requiredDictMissGapCaseNames,
		requiredDictEdgeGapCaseNames,
		requiredDictContinuationGapCaseNames,
	}

	var items []string
	for _, list := range lists {
		items = append(items, list...)
	}
	for label := range parityProgramGeneratorLiteralTraceLabelsFromSource(t) {
		items = append(items, label)
	}
	return items
}

func parityDictOpcodeInventoryWitnessKey(name string) string {
	fields := strings.Fields(name)
	if len(fields) == 0 {
		return ""
	}
	for _, field := range fields {
		field = strings.Trim(field, ",")
		if strings.HasPrefix(field, "DICT") || strings.HasPrefix(field, "PFXDICT") {
			return strings.ToUpper(field)
		}
	}
	return strings.ToUpper(fields[0])
}

func parityDictOpcodeHasWitness(opcode string, items []string) bool {
	op := strings.ToLower(opcode)
	for _, item := range items {
		key := strings.ToLower(item)
		if idx := strings.IndexByte(key, '('); idx >= 0 {
			key = key[:idx]
		}
		if key == op || strings.HasPrefix(key, op+"_") {
			return true
		}
	}
	return false
}

func parityCellSliceOpcodeWitnessItems(t *testing.T) []string {
	t.Helper()

	lists := [][]string{
		requiredCellSliceProgramTraceLabels,
		requiredCellSliceGapCaseNames,
		requiredCellSliceQuietGapCaseNames,
		requiredCellSliceEdgeGapCaseNames,
		requiredDataSizeProgramTraceLabels,
		requiredDataSizeGapCaseNames,
		requiredMsgAddressProgramTraceLabels,
		requiredMsgAddressGapCaseNames,
		requiredVarIntGapCaseNames,
		requiredLibraryGapCaseNames,
		requiredInventoryResidualGapCaseNames,
		requiredHashGapCaseNames,
	}

	var items []string
	for _, list := range lists {
		items = append(items, list...)
	}
	for label := range parityProgramGeneratorLiteralTraceLabelsFromSource(t) {
		items = append(items, label)
	}
	return items
}

func parityCellSliceOpcodeInventoryWitnessKey(name string) string {
	fields := strings.Fields(name)
	for _, field := range fields {
		field = strings.Trim(field, ",")
		if field == "" || parityMathOpcodeNumberField(field) || strings.HasPrefix(field, "<") {
			continue
		}
		return parityCellSliceWitnessItemKey(field)
	}
	return ""
}

func parityCellSliceOpcodeHasWitness(opcode string, items []string) bool {
	op := strings.ToLower(opcode)
	for _, item := range items {
		key := strings.ToLower(parityCellSliceWitnessItemKey(item))
		if key == op || strings.HasPrefix(key, op+"_") {
			return true
		}
		if strings.HasPrefix(key, op) && len(key) > len(op) && key[len(op)] >= '0' && key[len(op)] <= '9' {
			return true
		}
	}
	return false
}

func parityCellSliceWitnessItemKey(item string) string {
	if idx := strings.IndexByte(item, '('); idx >= 0 {
		item = item[:idx]
	}
	key := strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(item, "-", "_"), " ", "_"), "__", "_"))
	replacements := []struct {
		from string
		to   string
	}{
		{"SDBEGINSCONSTQ", "SDBEGINSQ"},
		{"SDBEGINSCONST", "SDBEGINS"},
		{"PLDSLICEFIXQ", "PLDSLICEQ"},
		{"LDSLICEFIXQ", "LDSLICEQ"},
		{"PLDSLICEFIX", "PLDSLICE"},
		{"LDSLICEFIX", "LDSLICE"},
		{"PLDIFIXQ", "PLDIQ"},
		{"PLDUFIXQ", "PLDUQ"},
		{"LDIFIXQ", "LDIQ"},
		{"LDUFIXQ", "LDUQ"},
		{"PLDIFIX", "PLDI"},
		{"PLDUFIX", "PLDU"},
		{"LDIFIX", "LDI"},
		{"LDUFIX", "LDU"},
	}
	for _, repl := range replacements {
		if strings.HasPrefix(key, repl.from) {
			return repl.to + strings.TrimPrefix(key, repl.from)
		}
	}
	return key
}

func parityMathOpcodeWitnessItems(t *testing.T) map[string]struct{} {
	t.Helper()

	keys := map[string]struct{}{}
	addItem := func(item string) {
		for _, key := range parityMathWitnessItemKeys(item) {
			keys[key] = struct{}{}
		}
	}

	for label := range parityProgramGeneratorLiteralTraceLabelsFromSource(t) {
		addItem(label)
	}

	for item := range parityNamedWitnessCases(
		requiredMathProgramTraceLabels,
		requiredMathCompoundTraceLabels,
		requiredMathShiftTraceLabels,
		requiredMathGapTraceLabels,
		requiredMathQuietLogicTraceLabels,
		requiredMathQuietCompoundTraceLabels,
		requiredMathImmediateGapCaseNames,
		requiredInvalidMathGapCaseNames,
		requiredQuietMathErrorGapCaseNames,
	) {
		addItem(item)
	}

	return keys
}

func parityMathOpcodeInventoryWitnessKey(name string) string {
	fields := strings.Fields(name)
	if len(fields) == 0 {
		return ""
	}
	if len(fields) > 1 && parityMathOpcodeNumberField(fields[0]) {
		fields = fields[1:]
	}
	if len(fields) > 1 && parityMathOpcodeNumberField(fields[len(fields)-1]) {
		fields = fields[:len(fields)-1]
	}

	key := strings.ToUpper(strings.Join(fields, ""))
	key = strings.ReplaceAll(key, ",", "")
	key = strings.ReplaceAll(key, "_VAR", "")
	key = strings.ReplaceAll(key, "<INVALID>", "INVALID")
	key = strings.ReplaceAll(key, "#", "CODE")
	key = strings.ReplaceAll(key, "/", "")
	if key == "ISNAN" || key == "CHKNAN" {
		return key
	}
	key = parityMathTrimWitnessSuffixes(key)
	if grouped := parityMathGroupedWitnessKey(key); grouped != "" {
		return grouped
	}
	return key
}

func parityMathOpcodeNumberField(field string) bool {
	field = strings.Trim(field, ",")
	_, err := strconv.Atoi(field)
	return err == nil
}

func parityMathWitnessItemKeys(item string) []string {
	key := parityMathWitnessItemBaseKey(item)
	if key == "" {
		return nil
	}
	if key == "ISNAN" || key == "CHKNAN" {
		return []string{key}
	}
	if grouped := parityMathGroupedWitnessKey(key); grouped != "" {
		return []string{grouped}
	}

	key = parityMathTrimWitnessSuffixes(key)
	if grouped := parityMathGroupedWitnessKey(key); grouped != "" {
		return []string{grouped}
	}
	if key == "" {
		return nil
	}
	return []string{key}
}

func parityMathWitnessItemBaseKey(item string) string {
	if idx := strings.IndexByte(item, '('); idx >= 0 {
		item = item[:idx]
	}

	key := strings.ToUpper(item)
	key = strings.ReplaceAll(key, "_", "")
	key = strings.ReplaceAll(key, "-", "")
	key = strings.ReplaceAll(key, " ", "")
	key = strings.ReplaceAll(key, "<INVALID>", "INVALID")
	key = strings.ReplaceAll(key, "#", "CODE")
	key = strings.ReplaceAll(key, "/", "")
	return key
}

func parityMathGroupedWitnessKey(key string) string {
	switch {
	case strings.HasPrefix(key, "ADDCONST"):
		return "ADDINT"
	case strings.HasPrefix(key, "MULCONST"):
		return "MULINT"
	case key == "RSHIFTFLOOR":
		return "RSHIFT"
	case strings.HasPrefix(key, "QMODPOW2"),
		strings.HasPrefix(key, "QSHRMOD"),
		strings.HasPrefix(key, "QADDRSHIFTMOD"):
		return "QADDRSHIFTMOD"
	case strings.HasPrefix(key, "QRSHIFT"):
		return "QRSHIFT"
	case strings.HasPrefix(key, "QDIV"),
		strings.HasPrefix(key, "QMOD"),
		strings.HasPrefix(key, "QADDDIVMOD"):
		return "QADDDIVMOD"
	case strings.HasPrefix(key, "QMULMODPOW2"),
		strings.HasPrefix(key, "QMULSHRMOD"),
		strings.HasPrefix(key, "QMULRSHIFT"),
		strings.HasPrefix(key, "QMULADDRSHIFTMOD"):
		return "QMULADDRSHIFTMOD"
	case strings.HasPrefix(key, "QMULDIV"),
		strings.HasPrefix(key, "QMULMOD"),
		strings.HasPrefix(key, "QMULADDDIVMOD"):
		return "QMULADDDIVMOD"
	case strings.HasPrefix(key, "QLSHIFTDIV"),
		strings.HasPrefix(key, "QLSHIFTMOD"),
		strings.HasPrefix(key, "QLSHIFTADDDIVMOD"):
		return "QLSHIFTADDDIVMOD"
	case strings.HasPrefix(key, "QLSHIFTNEGATIVE"),
		strings.HasPrefix(key, "QLSHIFTCOUNT"):
		return "QLSHIFT"
	case strings.HasPrefix(key, "QPOW2OUTOFRANGE"):
		return "QPOW2"
	default:
		return ""
	}
}

func parityMathTrimWitnessSuffixes(key string) string {
	suffixes := []string{
		"NANALIAS",
		"IMMEDIATENAN",
		"IMMEDIATE",
		"FLOORALT",
		"CURRENT",
		"LEGACY",
		"FLOOR",
		"ROUND",
		"CEIL",
		"TRUE",
		"FALSE",
		"NAN",
		"FAIL",
	}

	for {
		trimmed := false
		for _, suffix := range suffixes {
			if strings.HasSuffix(key, suffix) && len(key) > len(suffix) {
				key = strings.TrimSuffix(key, suffix)
				trimmed = true
				break
			}
		}
		if !trimmed {
			return key
		}
	}
}

func parityWitnessManifestLists() map[string][]string {
	return map[string][]string{
		"requiredActionErrorGapCaseNames":       requiredActionErrorGapCaseNames,
		"requiredActionGapTraceLabels":          requiredActionGapTraceLabels,
		"requiredActionModeGapCaseNames":        requiredActionModeGapCaseNames,
		"requiredCellSliceEdgeGapCaseNames":     requiredCellSliceEdgeGapCaseNames,
		"requiredCellSliceGapCaseNames":         requiredCellSliceGapCaseNames,
		"requiredCellSliceProgramTraceLabels":   requiredCellSliceProgramTraceLabels,
		"requiredCellSliceQuietGapCaseNames":    requiredCellSliceQuietGapCaseNames,
		"requiredCirclCryptoGapCaseNames":       requiredCirclCryptoGapCaseNames,
		"requiredCryptoEdgeGapCaseNames":        requiredCryptoEdgeGapCaseNames,
		"requiredCryptoGapCaseNames":            requiredCryptoGapCaseNames,
		"requiredDataSizeGapCaseNames":          requiredDataSizeGapCaseNames,
		"requiredDataSizeProgramTraceLabels":    requiredDataSizeProgramTraceLabels,
		"requiredDictContinuationGapCaseNames":  requiredDictContinuationGapCaseNames,
		"requiredDictEdgeGapCaseNames":          requiredDictEdgeGapCaseNames,
		"requiredDictGapTraceLabels":            requiredDictGapTraceLabels,
		"requiredDictMissGapCaseNames":          requiredDictMissGapCaseNames,
		"requiredDictNearGapTraceLabels":        requiredDictNearGapTraceLabels,
		"requiredDictProgramTraceLabels":        requiredDictProgramTraceLabels,
		"requiredDictSuccessGapCaseNames":       requiredDictSuccessGapCaseNames,
		"requiredExecGapCaseNames":              requiredExecGapCaseNames,
		"requiredExecRefDecodeGapCaseNames":     requiredExecRefDecodeGapCaseNames,
		"requiredHashGapCaseNames":              requiredHashGapCaseNames,
		"requiredInvalidMathGapCaseNames":       requiredInvalidMathGapCaseNames,
		"requiredInventoryResidualGapCaseNames": requiredInventoryResidualGapCaseNames,
		"requiredLibraryGapCaseNames":           requiredLibraryGapCaseNames,
		"requiredMathCompoundTraceLabels":       requiredMathCompoundTraceLabels,
		"requiredMathGapTraceLabels":            requiredMathGapTraceLabels,
		"requiredMathImmediateGapCaseNames":     requiredMathImmediateGapCaseNames,
		"requiredMathProgramTraceLabels":        requiredMathProgramTraceLabels,
		"requiredMathQuietCompoundTraceLabels":  requiredMathQuietCompoundTraceLabels,
		"requiredMathQuietLogicTraceLabels":     requiredMathQuietLogicTraceLabels,
		"requiredMathShiftTraceLabels":          requiredMathShiftTraceLabels,
		"requiredMsgAddressGapCaseNames":        requiredMsgAddressGapCaseNames,
		"requiredMsgAddressProgramTraceLabels":  requiredMsgAddressProgramTraceLabels,
		"requiredQuietMathErrorGapCaseNames":    requiredQuietMathErrorGapCaseNames,
		"requiredRunVMGapCaseNames":             requiredRunVMGapCaseNames,
		"requiredStackDynamicDepthGapCaseNames": requiredStackDynamicDepthGapCaseNames,
		"requiredStackGapCaseNames":             requiredStackGapCaseNames,
		"requiredStackOpcodeSpaceGapCaseNames":  requiredStackOpcodeSpaceGapCaseNames,
		"requiredTonFuncGapCaseNames":           requiredTonFuncGapCaseNames,
		"requiredTonFuncRuntimeGapCaseNames":    requiredTonFuncRuntimeGapCaseNames,
		"requiredTupleDynamicErrorGapCaseNames": requiredTupleDynamicErrorGapCaseNames,
		"requiredTupleGapCaseNames":             requiredTupleGapCaseNames,
		"requiredTupleGapTraceLabels":           requiredTupleGapTraceLabels,
		"requiredTupleOpcodeSpaceGapCaseNames":  requiredTupleOpcodeSpaceGapCaseNames,
		"requiredVarIntGapCaseNames":            requiredVarIntGapCaseNames,
	}
}

func parityWitnessManifestNamesFromSource(t *testing.T) map[string]struct{} {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to resolve current test file")
	}
	src, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("failed to read current test file: %v", err)
	}

	names := map[string]struct{}{}
	for _, line := range strings.Split(string(src), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 4 || fields[0] != "var" || fields[2] != "=" || fields[3] != "[]string{" {
			continue
		}
		name := fields[1]
		if strings.HasPrefix(name, "required") &&
			(strings.HasSuffix(name, "CaseNames") || strings.HasSuffix(name, "TraceLabels")) {
			names[name] = struct{}{}
		}
	}
	return names
}

func parityWitnessManifestHash(items []string) string {
	sum := sha256.Sum256([]byte(strings.Join(items, "\n")))
	return fmt.Sprintf("%x", sum[:])
}

func parityOpcodeCoverageBucket(name string) (string, bool) {
	tokens := parityOpcodeTokens(name)
	switch {
	case parityOpcodeHasTokenPrefix(tokens, "DICT", "PFXDICT") ||
		parityOpcodeHasToken(tokens, "STDICT", "SKIPDICT", "LDDICTS", "PLDDICTS", "LDDICT", "PLDDICT", "LDDICTQ", "PLDDICTQ"):
		return "random_dict", true
	case parityOpcodeHasTokenPrefix(tokens, "c0") && parityOpcodeHasToken(tokens, "PUSH", "POP", "SETRETCTR", "SETALTCTR", "POPSAVE", "SAVEALTCTR", "SAVEBOTHCTR", "SAVECTR", "SETCONTCTR"):
		return "deterministic_exec", true
	case parityOpcodeHasToken(tokens,
		"XCHG", "PUSH", "POP", "DUP", "DROP", "SWAP", "ROT", "ROLL", "OVER", "NIP", "TUCK",
		"PICK", "BLKDROP", "BLKDROP2", "BLKPUSH", "BLKSWAP", "BLKSWX", "REVERSE", "REVX",
		"ONLYTOPX", "ONLYX", "DEPTH", "CHKDEPTH", "DUMP", "DUMPSTK", "DEBUG", "DEBUGSTR",
		"STRDUMP", "XC2PU", "XCPUXC", "XCPU2", "PUXC2", "PUXCPU", "PU2XC", "PUXC", "XCPU",
		"PUSH2", "PUSH3", "PUSHL", "POPL", "PUSHCONT", "PUSHINT", "PUSHREF", "PUSHREFSLICE",
		"PUSHSLICE", "PUSHNULL", "PUSHNAN", "PUSHPOW2", "PUSHPOW2DEC", "PUSHNEGPOW2",
		"PUSHSLICEINLINE", "DICTPUSHCONST", "PUSHCTR", "POPCTR", "2DROP", "DROPX", "2DUP",
		"2OVER", "ROLLREV", "ROTREV", "2SWAP", "XCHG0", "XCHG2", "XCHG3", "XCHGX",
	):
		return "random_stack", true
	case parityOpcodeHasToken(tokens,
		"ADD", "SUB", "MUL", "DIV", "MOD", "LSHIFT", "RSHIFT", "POW2", "AND", "OR", "XOR",
		"NOT", "NEGATE", "INC", "DEC", "ABS", "FITS", "UFITS", "BITSIZE", "MIN", "MAX",
		"CMP", "EQUAL", "LESS", "LEQ", "GREATER", "GEQ", "NEQ", "ISZERO", "ISPOS", "ISNEG",
		"ISNPOS", "ISNNEG", "ISNAN", "CHKNAN", "QINT", "QUFITS", "QFITS", "GTINT", "SGN",
		"ADDINT", "MULINT", "EQINT", "LESSINT", "NEQINT", "QEQINT", "QLESSINT", "QGTINT",
		"QNEQINT", "QADD", "QSUB", "QSUBR", "QNEGATE", "QINC", "QDEC", "QADDINT",
		"QMULINT", "QMUL", "QMIN", "QMAX", "QMINMAX", "QABS", "QSGN", "QLESS", "QEQUAL",
		"QLEQ", "QGREATER", "QNEQ", "QGEQ", "QCMP", "QNOT", "QAND", "QOR", "QXOR",
		"QLSHIFT", "QRSHIFT", "QPOW2", "DIVR", "DIVC", "DIVMOD", "DIVMODR", "DIVMODC",
		"MODR", "MODC", "MODPOW2", "MODPOW2R", "MODPOW2C", "MULDIV", "MULDIVR",
		"MULDIVC", "MULDIVMOD", "MULDIVMODR", "MULDIVMODC", "MULMOD", "MULMODR",
		"MULMODC", "MULMODPOW2_VAR", "MULMODPOW2R_VAR", "MULMODPOW2C_VAR", "MULRSHIFT",
		"MULRSHIFTR", "MULRSHIFTC", "MULRSHIFTMOD_VAR", "MULRSHIFTRMOD_VAR",
		"MULRSHIFTCMOD_VAR", "ADDDIVMOD", "ADDDIVMODR", "ADDDIVMODC", "MULADDDIVMOD",
		"MULADDDIVMODR", "MULADDDIVMODC", "ADDRSHIFTMOD", "ADDRSHIFTMODR", "ADDRSHIFTMODC",
		"LSHIFTDIV", "LSHIFTDIVR", "LSHIFTDIVC", "LSHIFTDIVMOD", "LSHIFTDIVMODR",
		"LSHIFTDIVMODC", "LSHIFTMOD", "LSHIFTMODR", "LSHIFTMODC", "RSHIFTR", "RSHIFTC",
		"RSHIFTMOD", "RSHIFTMODR", "RSHIFTMODC", "FITSX", "UFITSX", "UBITSIZE", "QFITSX",
		"QUFITSX", "QBITSIZE", "QUBITSIZE", "LSHIFTADDDIVMOD", "LSHIFTADDDIVMODC",
		"LSHIFTADDDIVMODR", "MINMAX", "MULADDRSHIFTCMOD", "MULADDRSHIFTMOD",
		"MULADDRSHIFTRMOD", "QADDDIVMOD", "QADDRSHIFTMOD", "QMULADDDIVMOD",
		"QMULADDRSHIFTMOD", "QLSHIFTADDDIVMOD", "SUBR",
	):
		return "random_math", true
	case parityOpcodeHasTokenPrefix(tokens,
		"QDIV", "QMOD", "QADDDIVMOD", "QSHRMOD", "QRSHIFT", "QMODPOW2", "QADDRSHIFTMOD",
		"QMULDIV", "QMULMOD", "QMULADDDIVMOD", "QMULSHRMOD", "QMULRSHIFT", "QMULMODPOW2",
		"QMULADDRSHIFTMOD", "QLSHIFTDIV", "QLSHIFTMOD", "QLSHIFTADDDIVMOD",
	):
		return "random_math", true
	case parityOpcodeHasTokenSuffix(tokens, "#", "#MOD") || parityOpcodeHasTokenContains(tokens, "/MOD<invalid>"):
		return "random_math", true
	case parityOpcodeHasToken(tokens,
		"NEWC", "ENDC", "CTOS", "XCTOS", "XLOAD", "ENDS", "ST", "LD", "PLD", "LDU", "LDI",
		"STU", "STI", "SREFS", "SBITS", "SREMPTY", "SEMPTY", "SDEMPTY", "SDFIRST",
		"SDLEXCMP", "SDEQ", "SDPFX", "SDPFXREV", "SDPPFX", "SDPPFXREV", "SDSKIP",
		"SDBEGIN", "SCUTFIRST", "SREMPTY", "BDEPTH", "BBITS", "BREFS", "BCHKBITS",
		"BCHKREFS", "BCHKBITREFS", "HASHSU", "HASHCU", "CHASHI", "CDEPTHI", "LDGRAMS",
		"STGRAMS", "LDVAR", "STVAR", "LDMSG", "PARSEMSG", "REWRITESTDADDR",
		"ENDXC", "BBITREFS", "BTOS", "SPLIT", "SCHKBITS", "SCHKREFS", "SCHKBITREFS",
		"SBITREFS", "CLEVEL", "CLEVELMASK", "SDCNT", "SDSFX", "SDPSFX", "BREMBITS",
		"BREMREFS", "BREMBITREFS", "DATASIZE", "SDEPTH", "CDEPTH", "CHASHIX", "CDEPTHIX",
		"LDREF", "LDREFRTOS", "LDSLICEX", "LDIX", "LDUX", "PLDIX", "PLDUX", "LDIXQ",
		"LDUXQ", "PLDIXQ", "PLDUXQ", "LDIQ", "LDUQ", "PLDIQ", "PLDUQ", "PLDUZ",
		"PLDSLICEX", "LDSLICEXQ", "PLDSLICEXQ", "LDSLICE", "PLDSLICE", "LDSLICEQ",
		"PLDSLICEQ", "LDILE4", "LDULE4", "LDILE8", "LDULE8", "PLDILE4", "PLDULE4",
		"PLDILE8", "PLDULE8", "LDILE4Q", "LDULE4Q", "LDILE8Q", "LDULE8Q", "PLDILE4Q",
		"PLDULE4Q", "PLDILE8Q", "PLDULE8Q", "STILE4", "STULE4", "STILE8", "STULE8",
		"STREF", "STBREF", "STSLICE", "STBREFR", "STREFR", "STSLICER", "STBR", "STREFQ",
		"STBREFQ", "STSLICEQ", "STBQ", "STREFRQ", "STBREFRQ", "STSLICERQ", "STBRQ",
		"BCHKBITSQ", "BCHKREFSQ", "BCHKBITREFSQ", "STZEROES", "STONES", "STSAME",
		"SDCUTFIRST", "SDCUTLAST", "SDSKIPLAST", "SDSUBSTR", "SSKIPFIRST", "SCUTLAST",
		"SSKIPLAST", "SUBSLICE", "PLDREFVAR", "PLDREFIDX", "LDZEROES", "LDONES", "LDSAME",
		"SDBEGINSX", "SDBEGINSXQ", "SDBEGINS", "SDSKIPFIRST", "ENDCST", "STREFCONST",
		"STSLICECONST", "SPLITQ", "XLOADQ", "SCHKBITSQ", "SCHKREFSQ", "SCHKBITREFSQ",
		"SDCNTLEAD0", "SDCNTLEAD1", "SDCNTTRAIL0", "SDCNTTRAIL1", "SDSFXREV", "SDPSFXREV",
		"PLDI", "PLDU", "STB", "STIX",
	):
		return "random_cell_slice", true
	case parityOpcodeHasTokenPrefix(tokens, "LDMSGADDR", "PARSEMSGADDR", "REWRITE", "LDSTDADDR", "LDOPTSTDADDR", "STSTDADDR", "STOPTSTDADDR", "LDVAR", "STVAR"):
		return "random_cell_slice", true
	case parityOpcodeHasToken(tokens,
		"EXECUTE", "JMP", "CALL", "RET", "RETURN", "THROW", "TRY", "IF", "WHILE", "UNTIL",
		"REPEAT", "AGAIN", "BLESS", "SETCONT", "SETNUM", "BOOL", "COMPOS", "ATEXIT",
		"ATEXITALT", "SETEXITALT", "THENRET", "INVERT", "SAMEALT", "RUNVM", "SETCP",
		"SAVE", "SAVECTR", "SAVEALTCTR", "SAVEBOTHCTR", "PUSHCTRX", "POPCTRX", "SETALTCTR",
		"CONDSEL", "CONDSELCHK", "NOP", "JMPX", "JMPXARGS", "JMPXDATA", "JMPXVARARGS",
		"CALLXARGS", "CALLCC", "CALLCCARGS", "CALLXVARARGS", "CALLCCVARARGS", "RETARGS",
		"RETVARARGS", "RETURNARGS", "RETURNVARARGS", "RETBOOL", "RETDATA", "RETALT",
		"IFRET", "IFRETALT", "IFNOTRET", "IFNOTRETALT", "IFJMP", "IFNOTJMP", "IFBITJMP",
		"IFNBITJMP", "IFBITJMPREF", "IFNBITJMPREF", "IFREF", "IFNOTREF", "IFJMPREF",
		"IFNOTJMPREF", "IFREFELSE", "IFELSEREF", "IFREFELSEREF", "CALLREF", "JMPREF",
		"JMPREFDATA", "CALLDICT", "JMPDICT", "PREPAREDICT", "BOOLEVAL", "BOOLAND",
		"BOOLOR", "COMPOSBOTH", "REPEATBRK", "UNTILBRK", "WHILEBRK", "REPEATENDBRK",
		"UNTILENDBRK", "WHILEENDBRK", "AGAINBRK", "AGAINENDBRK", "SETCONTARGS",
		"SETCONTVARARGS", "SETNUMVARARGS", "BLESSVARARGS", "BLESSARGS", "SETCONTCTR",
		"SETCONTCTRX", "SETCONTCTRMANY", "SETCONTCTRMANYX", "POPSAVE", "AGAINEND",
		"THENRETALT", "IFELSE", "IFNOT", "REPEATEND", "RUNVMX", "SAMEALTSAVE", "TRYARGS",
		"UNTILEND", "WHILEEND",
	):
		return "deterministic_exec", true
	case parityOpcodeHasTokenPrefix(tokens, "THROW") || parityOpcodeHasToken(tokens, "THROWANY", "THROWARGANY", "THROWANYIF", "THROWARGANYIF", "THROWANYIFNOT", "THROWARGANYIFNOT"):
		return "deterministic_exec", true
	case parityOpcodeHasToken(tokens,
		"SHA", "HASH", "HASHEXT", "CHKSIG", "ECRECOVER", "P256", "SECP256K1", "RIST255",
		"BLS", "SHA256U", "HASHEXT", "HASHBU", "CHKSIGNU", "CHKSIGNS",
	):
		return "deterministic_ton_crypto", true
	case parityOpcodeHasTokenPrefix(tokens, "BLS_", "P256_", "RIST255_", "SECP256K1_"):
		return "deterministic_ton_crypto", true
	case parityOpcodeHasToken(tokens,
		"NOW", "BLOCKLT", "LTIME", "RAND", "SETRAND", "ADDRAND", "GETRAND", "GETPARAM",
		"CONFIG", "MYADDR", "BALANCE", "MYCODE", "INCOMINGVALUE", "STORAGEFEES", "DUEPAYMENT",
		"GLOBALID", "PREVBLOCK", "PREVMC", "PREVKEY", "GETPRECOMPILEDGAS", "INMSG",
		"GETSTORAGEFEE", "GETGASFEE", "GETFORWARDFEE", "GETORIGINALFWDFEE", "GETEXTRABALANCE",
		"SEND", "RAWRESERVE", "SETCODE", "SETLIBCODE", "CHANGELIB", "GETGLOB", "SETGLOB",
		"ACCEPT", "SETGASLIMIT", "GASCONSUMED", "COMMIT", "SENDRAWMSG", "SENDMSG",
		"RANDU256", "RANDSEED", "CONFIGROOT", "CONFIGDICT", "CONFIGPARAM", "CONFIGOPTPARAM",
		"GETGLOBVAR", "SETGLOBVAR", "GETPARAMLONG", "RAWRESERVEX", "PREVBLOCKSINFOTUPLE",
		"UNPACKEDCONFIGTUPLE", "PREVMCBLOCKS", "PREVKEYBLOCK", "PREVMCBLOCKS_100",
		"INMSGPARAMS", "INMSG_BOUNCE", "INMSG_BOUNCED", "INMSG_SRC", "INMSG_FWDFEE",
		"INMSG_LT", "INMSG_UTIME", "INMSG_ORIGVALUE", "INMSG_VALUE", "INMSG_VALUEEXTRA",
		"INMSG_STATEINIT", "INMSGPARAM", "GETGASFEESIMPLE", "GETFORWARDFEESIMPLE",
		"SETCPX", "CDATASIZEQ", "CDATASIZE", "SDATASIZEQ", "SDATASIZE",
	):
		return "deterministic_ton_runtime", true
	case parityOpcodeHasToken(tokens,
		"TUPLE", "UNTUPLE", "INDEX", "SETINDEX", "EXPLODE", "TPUSH", "TPOP", "NULL",
		"ISNULL", "NULLSWAP", "UNPACKFIRST", "TLEN", "QTLEN", "ISTUPLE", "LAST", "FIRST",
		"SECOND", "THIRD", "UNPACK", "INDEXVAR", "INDEXVARQ", "SETINDEXVAR", "SETINDEXVARQ",
		"TUPLEVAR", "UNTUPLEVAR", "UNPACKFIRSTVAR", "EXPLODEVAR", "INDEXQ", "SETINDEXQ",
		"INDEX2", "INDEX3", "NULLSWAPIF", "NULLSWAPIFNOT", "NULLROTRIF", "NULLROTRIFNOT",
		"NULLSWAPIF2", "NULLSWAPIFNOT2", "NULLROTRIF2", "NULLROTRIFNOT2",
	):
		return "dedicated_tuple", true
	default:
		return "", false
	}
}

func parityOpcodeTokens(name string) []string {
	return strings.FieldsFunc(name, func(r rune) bool {
		switch r {
		case ' ', ',', '(', ')', '[', ']':
			return true
		default:
			return false
		}
	})
}

func parityOpcodeHasToken(tokens []string, names ...string) bool {
	for _, token := range tokens {
		for _, name := range names {
			if token == name {
				return true
			}
		}
	}
	return false
}

func parityOpcodeHasTokenPrefix(tokens []string, prefixes ...string) bool {
	for _, token := range tokens {
		for _, prefix := range prefixes {
			if strings.HasPrefix(token, prefix) {
				return true
			}
		}
	}
	return false
}

func parityOpcodeHasTokenSuffix(tokens []string, suffixes ...string) bool {
	for _, token := range tokens {
		for _, suffix := range suffixes {
			if strings.HasSuffix(token, suffix) {
				return true
			}
		}
	}
	return false
}

func parityOpcodeHasTokenContains(tokens []string, parts ...string) bool {
	for _, token := range tokens {
		for _, part := range parts {
			if strings.Contains(token, part) {
				return true
			}
		}
	}
	return false
}

func TestTVMDifferentialFuzzProgramGeneratorReachesResidualFamilies(t *testing.T) {
	const (
		seeds = 4096
		steps = 260
	)

	targets := map[string]struct{}{
		"NOP":                {},
		"DROP":               {},
		"DROP(*":             {},
		"DUP":                {},
		"OVER":               {},
		"SWAP":               {},
		"ROT":                {},
		"ROTREV":             {},
		"DEPTH":              {},
		"CHKDEPTH":           {},
		"XCPU(*":             {},
		"PUXC(*":             {},
		"XC2PU(*":            {},
		"XCPUXC(3,4,2)":      {},
		"XCPU2(4,3,2)":       {},
		"PUXC2(4,3,2)":       {},
		"PUXCPU(4,3,2)":      {},
		"PU2XC(4,3,2)":       {},
		"2DROP":              {},
		"2DUP":               {},
		"2OVER":              {},
		"2SWAP":              {},
		"NIP":                {},
		"TUCK":               {},
		"PICK":               {},
		"ROLL":               {},
		"ROLLREV":            {},
		"DROPX":              {},
		"XCHGX":              {},
		"REVX":               {},
		"ONLYTOPX":           {},
		"ONLYX":              {},
		"BLKSWX":             {},
		"PUSHINT(*":          {},
		"PUSHNULL":           {},
		"PUSHNULL(*":         {},
		"PUSHNAN":            {},
		"PUSHNAN(*":          {},
		"PUSHREF(*":          {},
		"PUSHSLICE(*":        {},
		"PUSHPOW2(*":         {},
		"PUSHPOW2DEC(*":      {},
		"PUSHNEGPOW2(*":      {},
		"PUSH(s*":            {},
		"POP(s*":             {},
		"XCHG(s*":            {},
		"REVERSE(*":          {},
		"BLKDROP(*":          {},
		"BLKDROP2(*":         {},
		"BLKPUSH(*":          {},
		"BLKSWAP(*":          {},
		"PUSH2(*":            {},
		"PUSH3(*":            {},
		"XCHG0(*":            {},
		"XCHG0L(*":           {},
		"XCHG2(*":            {},
		"XCHG3(*":            {},
		"PUSHL(*":            {},
		"POPL(*":             {},
		"PUSHSLICEINLINE":    {},
		"LDDICT":             {},
		"PLDDICT":            {},
		"STDICT":             {},
		"SKIPDICT":           {},
		"LDDICTS":            {},
		"PLDDICTS":           {},
		"LDDICTQ":            {},
		"PLDDICTQ":           {},
		"DICTPUSHCONST":      {},
		"DICTGET":            {},
		"DICTGETREF":         {},
		"DICTIGET":           {},
		"DICTUGET":           {},
		"DICTIGETREF":        {},
		"DICTSET":            {},
		"DICTISET":           {},
		"DICTUSET":           {},
		"DICTSETREF":         {},
		"DICTISETREF":        {},
		"DICTUSETREF":        {},
		"DICTSETGET":         {},
		"DICTISETGET":        {},
		"DICTUSETGET":        {},
		"DICTSETGETREF":      {},
		"DICTISETGETREF":     {},
		"DICTUSETGETREF":     {},
		"DICTREPLACE":        {},
		"DICTIREPLACE":       {},
		"DICTUREPLACE":       {},
		"DICTREPLACEREF":     {},
		"DICTIREPLACEREF":    {},
		"DICTUREPLACEREF":    {},
		"DICTADD":            {},
		"DICTIADD":           {},
		"DICTUADD":           {},
		"DICTADDREF":         {},
		"DICTIADDREF":        {},
		"DICTUADDREF":        {},
		"DICTDEL":            {},
		"DICTIDEL":           {},
		"DICTMIN":            {},
		"DICTMAX":            {},
		"DICTMINREF":         {},
		"DICTMAXREF":         {},
		"DICTIMIN":           {},
		"DICTUMAX":           {},
		"DICTIMINREF":        {},
		"DICTUMAXREF":        {},
		"DICTREMMIN":         {},
		"DICTREMMAX":         {},
		"DICTREMMINREF":      {},
		"DICTREMMAXREF":      {},
		"DICTIREMMIN":        {},
		"DICTUREMMAXREF":     {},
		"DICTUGETREF":        {},
		"DICTGETOPTREF":      {},
		"DICTIGETOPTREF":     {},
		"DICTUGETOPTREF":     {},
		"DICTDELGET":         {},
		"DICTUSETGETOPTREF":  {},
		"DICTUDELGETREF":     {},
		"DICTSETB":           {},
		"DICTUREPLACEB":      {},
		"DICTIADDB":          {},
		"DICTSETGETB":        {},
		"DICTUREPLACEGETB":   {},
		"DICTIADDGETB":       {},
		"DICTGETNEXT":        {},
		"DICTGETNEXTEQ":      {},
		"DICTGETPREV":        {},
		"DICTGETPREVEQ":      {},
		"DICTIGETNEXT":       {},
		"DICTIGETNEXTEQ":     {},
		"DICTIGETPREV":       {},
		"DICTIGETPREVEQ":     {},
		"DICTUGETNEXT":       {},
		"DICTUGETNEXTEQ":     {},
		"DICTUGETPREV":       {},
		"DICTUGETPREVEQ":     {},
		"DICTSUBDICTGET":     {},
		"DICTSUBDICTRPGET":   {},
		"DICTUSUBDICTGET":    {},
		"DICTUSUBDICTRPGET":  {},
		"DICTISUBDICTGET":    {},
		"DICTISUBDICTRPGET":  {},
		"DICTREPLACEGET":     {},
		"DICTIREPLACEGET":    {},
		"DICTREPLACEGETREF":  {},
		"DICTIREPLACEGETREF": {},
		"DICTUREPLACEGETREF": {},
		"DICTADDGETREF":      {},
		"DICTIADDGET":        {},
		"DICTIADDGETREF":     {},
		"DICTUADDGET":        {},
		"DICTDELGETREF":      {},
		"DICTIDELGET":        {},
		"DICTIDELGETREF":     {},
		"DICTSETGETOPTREF":   {},
		"DICTISETGETOPTREF":  {},
		"DICTIMAX":           {},
		"DICTIMAXREF":        {},
		"DICTIREMMAX":        {},
		"DICTUREMMAX":        {},
		"DICTIREMMINREF":     {},
		"DICTIREMMAXREF":     {},
		"DICTADDB":           {},
		"DICTISETB":          {},
		"DICTREPLACEB":       {},
		"DICTIREPLACEB":      {},
		"DICTADDGETB":        {},
		"DICTISETGETB":       {},
		"DICTUSETGETB":       {},
		"DICTREPLACEGETB":    {},
		"DICTIREPLACEGETB":   {},
		"PFXDICTGET":         {},
		"PFXDICTGETQ":        {},
		"PFXDICTREPLACE":     {},
		"PFXDICTADD":         {},
		"PFXDICTSET":         {},
		"PFXDICTDEL":         {},

		"GTINT(3)":                {},
		"QGTINT(4)":               {},
		"QGTINT(NaN)":             {},
		"SGN":                     {},
		"QSGN":                    {},
		"QSGN(NaN)":               {},
		"ISNAN":                   {},
		"NEGATE":                  {},
		"INC":                     {},
		"DEC":                     {},
		"ABS":                     {},
		"NOT":                     {},
		"BITSIZE":                 {},
		"ISNPOS":                  {},
		"ISZERO":                  {},
		"ISPOS":                   {},
		"ISNEG":                   {},
		"ADD":                     {},
		"SUB":                     {},
		"SUBR":                    {},
		"MUL":                     {},
		"AND":                     {},
		"OR":                      {},
		"XOR":                     {},
		"MIN":                     {},
		"MAX":                     {},
		"MINMAX":                  {},
		"LESS":                    {},
		"LEQ":                     {},
		"GREATER":                 {},
		"GEQ":                     {},
		"EQUAL":                   {},
		"NEQ":                     {},
		"CMP":                     {},
		"ISNNEG(0)":               {},
		"ISNNEG(-1)":              {},
		"ADDINT(-5)":              {},
		"MULINT(-3)":              {},
		"LESSINT(4)":              {},
		"EQINT(5)":                {},
		"NEQINT(8)":               {},
		"QADD":                    {},
		"QSUB":                    {},
		"QSUBR":                   {},
		"QNEGATE":                 {},
		"QINC":                    {},
		"QDEC":                    {},
		"QADDINT(4)":              {},
		"QMULINT(-2)":             {},
		"QMUL":                    {},
		"QMIN":                    {},
		"QMAX":                    {},
		"QMINMAX":                 {},
		"QABS":                    {},
		"UBITSIZE":                {},
		"QBITSIZE":                {},
		"QUBITSIZE":               {},
		"FITS(7)":                 {},
		"UFITS(8)":                {},
		"FITSX":                   {},
		"UFITSX":                  {},
		"POW2":                    {},
		"LSHIFT":                  {},
		"RSHIFT":                  {},
		"DIV":                     {},
		"MOD":                     {},
		"DIVMOD":                  {},
		"ADDCONST(6)":             {},
		"MULCONST(-4)":            {},
		"DIVR":                    {},
		"DIVC":                    {},
		"MODR":                    {},
		"MODC":                    {},
		"DIVMODR":                 {},
		"DIVMODC":                 {},
		"MULMOD":                  {},
		"MULMODR":                 {},
		"MULMODC":                 {},
		"MULDIV":                  {},
		"MULDIVR":                 {},
		"MULDIVC":                 {},
		"MULDIVMOD":               {},
		"MULDIVMODR":              {},
		"MULDIVMODC":              {},
		"ADDDIVMOD":               {},
		"ADDDIVMODR":              {},
		"ADDDIVMODC":              {},
		"MULADDDIVMOD":            {},
		"MULADDDIVMODR":           {},
		"MULADDDIVMODC":           {},
		"RSHIFTCODEFLOOR(3)":      {},
		"RSHIFTFLOOR":             {},
		"RSHIFTR":                 {},
		"RSHIFTC":                 {},
		"MODPOW2":                 {},
		"MODPOW2R":                {},
		"MODPOW2C":                {},
		"MULRSHIFT":               {},
		"MULRSHIFTR":              {},
		"MULRSHIFTC":              {},
		"LSHIFTDIV":               {},
		"LSHIFTDIVR":              {},
		"LSHIFTDIVC":              {},
		"LSHIFTDIVMOD":            {},
		"LSHIFTADDDIVMOD":         {},
		"LSHIFTADDDIVMODR":        {},
		"LSHIFTADDDIVMODC":        {},
		"MULADDRSHIFTMOD":         {},
		"MULADDRSHIFTRMOD":        {},
		"MULADDRSHIFTCMOD":        {},
		"QNOT":                    {},
		"QAND":                    {},
		"QOR":                     {},
		"QXOR":                    {},
		"QLSHIFT":                 {},
		"QRSHIFT":                 {},
		"QLSHIFTCODE(3)":          {},
		"QRSHIFTCODE(3)":          {},
		"QPOW2":                   {},
		"QLESS":                   {},
		"QEQUAL":                  {},
		"QLEQ":                    {},
		"QGREATER":                {},
		"QNEQ":                    {},
		"QGEQ":                    {},
		"QCMP":                    {},
		"QEQINT(5)":               {},
		"QLESSINT(4)":             {},
		"QNEQINT(4)":              {},
		"QFITS(6)":                {},
		"QFITS(3 fail)":           {},
		"QUFITS(8)":               {},
		"QFITSX":                  {},
		"QUFITSX(fail)":           {},
		"CHKNAN":                  {},
		"QDIV":                    {},
		"QMODR":                   {},
		"QADDDIVMODC":             {},
		"QMODPOW2R":               {},
		"QADDRSHIFTMODC":          {},
		"QMULDIV":                 {},
		"QMULMODR":                {},
		"QMULADDDIVMODC":          {},
		"QMULRSHIFT":              {},
		"QMULMODPOW2R":            {},
		"QMULADDRSHIFTMODC":       {},
		"QLSHIFTDIV":              {},
		"QLSHIFTMODR":             {},
		"QLSHIFTADDDIVMODC":       {},
		"RSHIFTMOD":               {},
		"RSHIFTMODR":              {},
		"RSHIFTMODC":              {},
		"RSHIFTCODEMOD(3)":        {},
		"RSHIFTRCODEMOD(3)":       {},
		"RSHIFTCCODEMOD(3)":       {},
		"LSHIFTMOD":               {},
		"LSHIFTMODR":              {},
		"LSHIFTMODC":              {},
		"LSHIFTDIVMODR":           {},
		"LSHIFTDIVMODC":           {},
		"ADDRSHIFTMOD":            {},
		"ADDRSHIFTMODR":           {},
		"ADDRSHIFTMODC":           {},
		"MULMODPOW2":              {},
		"MULMODPOW2R":             {},
		"MULMODPOW2C":             {},
		"MULRSHIFTMOD":            {},
		"MULRSHIFTRMOD":           {},
		"MULRSHIFTCMOD":           {},
		"MULMODPOW2CODE(3)":       {},
		"MULMODPOW2RCODE(3)":      {},
		"MULMODPOW2CCODE(3)":      {},
		"MULRSHIFTCODEMOD(3)":     {},
		"MULRSHIFTRCODEMOD(3)":    {},
		"MULRSHIFTCCODEMOD(3)":    {},
		"ADDRSHIFTCODEMOD(3)":     {},
		"ADDRSHIFTRCODEMOD(3)":    {},
		"ADDRSHIFTCCODEMOD(3)":    {},
		"MULADDRSHIFTCODEMOD(3)":  {},
		"MULADDRSHIFTRCODEMOD(3)": {},
		"MULADDRSHIFTCCODEMOD(3)": {},

		"NEWC":                 {},
		"NEWC(*":               {},
		"ENDC":                 {},
		"CTOS":                 {},
		"ENDS":                 {},
		"BBITS":                {},
		"BREFS":                {},
		"BDEPTH":               {},
		"LDREF":                {},
		"HASHCU":               {},
		"HASHCU(*":             {},
		"HASHSU":               {},
		"HASHBU":               {},
		"HASHEXT(0)":           {},
		"SHA256U":              {},
		"SBITS":                {},
		"SREFS":                {},
		"SBITREFS":             {},
		"SDEPTH":               {},
		"CDEPTH":               {},
		"CDEPTH(nil)":          {},
		"BCHKBITSIMM(8)":       {},
		"CHASHI(0)":            {},
		"CDEPTHI(0)":           {},
		"CHASHIX":              {},
		"CDEPTHIX":             {},
		"CLEVEL":               {},
		"CLEVELMASK":           {},
		"BBITREFS":             {},
		"BREMBITS":             {},
		"BREMREFS":             {},
		"BREMBITREFS":          {},
		"ENDXC":                {},
		"BTOS":                 {},
		"SPLIT":                {},
		"SPLITQ":               {},
		"SPLITQ(fail)":         {},
		"SCHKBITS":             {},
		"SCHKREFS":             {},
		"SCHKBITREFS":          {},
		"SCHKBITSQ":            {},
		"SCHKREFSQ":            {},
		"SCHKBITREFSQ":         {},
		"SCHKBITSQ(fail)":      {},
		"SCHKREFSQ(fail)":      {},
		"SCHKBITREFSQ(fail)":   {},
		"SDCNTLEAD0":           {},
		"SDCNTLEAD1":           {},
		"SDCNTTRAIL0":          {},
		"SDCNTTRAIL1":          {},
		"SDEMPTY":              {},
		"SDEQ":                 {},
		"SDLEXCMP":             {},
		"SDFIRST":              {},
		"SDPFX":                {},
		"SDPFXREV":             {},
		"SDPPFX":               {},
		"SDPPFXREV":            {},
		"SDSFX":                {},
		"SDSFXREV":             {},
		"SDPSFX":               {},
		"SDPSFXREV":            {},
		"SEMPTY":               {},
		"SREMPTY":              {},
		"SDCUTFIRST":           {},
		"SDSKIPFIRST":          {},
		"SDCUTLAST":            {},
		"SDSKIPLAST":           {},
		"SDSUBSTR":             {},
		"SCUTFIRST":            {},
		"SSKIPFIRST":           {},
		"SCUTLAST":             {},
		"SSKIPLAST":            {},
		"SUBSLICE":             {},
		"LDIX":                 {},
		"LDI8":                 {},
		"LDUX":                 {},
		"LDU8":                 {},
		"PLDIX":                {},
		"PLDUX":                {},
		"LDIXQ":                {},
		"LDUXQ(fail)":          {},
		"PLDIXQ":               {},
		"PLDUXQ(fail)":         {},
		"LDIFIX(9)":            {},
		"LDUFIX(9)":            {},
		"PLDIFIX(9)":           {},
		"PLDUFIX(9)":           {},
		"LDIQ":                 {},
		"PLDIQ":                {},
		"LDUFIXQ(9)":           {},
		"PLDUFIXQ(fail)":       {},
		"LDSLICEX":             {},
		"PLDSLICEX":            {},
		"LDSLICEXQ":            {},
		"LDSLICEXQ(fail)":      {},
		"PLDSLICEXQ":           {},
		"PLDSLICEXQ(fail)":     {},
		"LDSLICE(3)":           {},
		"LDSLICEFIX(3)":        {},
		"PLDSLICEFIX(3)":       {},
		"LDSLICEFIXQ(3)":       {},
		"LDSLICEFIXQ(fail)":    {},
		"PLDSLICEFIXQ(3)":      {},
		"PLDSLICEFIXQ(fail)":   {},
		"PLDUZ(32)":            {},
		"LDREFRTOS":            {},
		"PLDREFVAR":            {},
		"PLDREFIDX(0)":         {},
		"PLDREFIDX(1)":         {},
		"LDZEROES":             {},
		"LDZEROES(zero)":       {},
		"LDONES":               {},
		"LDONES(zero)":         {},
		"LDSAME":               {},
		"STI8":                 {},
		"STIX":                 {},
		"STU8":                 {},
		"STREF":                {},
		"STSLICE":              {},
		"STSLICE(*":            {},
		"STBREF":               {},
		"STBREFR":              {},
		"STREFR":               {},
		"STSLICER":             {},
		"STBR":                 {},
		"STREFQ":               {},
		"STREFQ(fail)":         {},
		"STREFRQ":              {},
		"STREFRQ(fail)":        {},
		"STBREFQ":              {},
		"STBREFQ(fail)":        {},
		"STBREFRQ":             {},
		"STBREFRQ(fail)":       {},
		"STSLICEQ":             {},
		"STSLICEQ(fail)":       {},
		"STSLICERQ":            {},
		"STSLICERQ(fail)":      {},
		"STBQ":                 {},
		"STBQ(fail)":           {},
		"STBRQ":                {},
		"STBRQ(fail)":          {},
		"BCHKBITS":             {},
		"BCHKREFS":             {},
		"BCHKREFSQ(fail)":      {},
		"STZEROES":             {},
		"STONES":               {},
		"STSAME":               {},
		"BCHKBITREFS":          {},
		"BCHKBITREFSQ":         {},
		"STB":                  {},
		"STILE4":               {},
		"STULE4":               {},
		"STILE8":               {},
		"STULE8":               {},
		"LDILE4":               {},
		"LDULE4":               {},
		"LDILE8":               {},
		"LDULE8":               {},
		"LDILE4Q":              {},
		"LDULE4Q":              {},
		"LDILE8Q":              {},
		"LDULE8Q(fail)":        {},
		"PLDILE4":              {},
		"PLDULE4":              {},
		"PLDILE8":              {},
		"PLDULE8":              {},
		"PLDILE4Q":             {},
		"PLDULE4Q":             {},
		"PLDILE8Q":             {},
		"PLDULE8Q(fail)":       {},
		"XCTOS(ordinary)":      {},
		"SDBEGINSX":            {},
		"SDBEGINSXQ":           {},
		"SDBEGINSXQ(fail)":     {},
		"SDBEGINSCONST":        {},
		"SDBEGINSCONSTQ":       {},
		"SDBEGINSCONSTQ(fail)": {},
		"LDGRAMS":              {},
		"STGRAMS":              {},
		"STREFCONST":           {},
		"STREF2CONST":          {},
		"STSLICECONST":         {},
		"ENDCST":               {},
		"LDMSGADDR":            {},
		"LDMSGADDRQ":           {},
		"LDSTDADDR":            {},
		"LDSTDADDRQ":           {},
		"LDOPTSTDADDR":         {},
		"LDOPTSTDADDRQ":        {},
		"PARSEMSGADDR":         {},
		"PARSEMSGADDRQ":        {},
		"REWRITESTDADDR":       {},
		"REWRITESTDADDRQ":      {},
		"REWRITEVARADDR":       {},
		"REWRITEVARADDRQ":      {},
		"STSTDADDR":            {},
		"STSTDADDRQ":           {},
		"STOPTSTDADDR":         {},
		"STOPTSTDADDRQ":        {},

		"CTOS(library)":    {},
		"XLOAD(library)":   {},
		"XLOADQ(library)":  {},
		"XCTOS(library)":   {},
		"XLOADQ(missing)":  {},
		"XLOADQ(ordinary)": {},

		"DUMPSTK":        {},
		"DUMP(0)":        {},
		"DUMP(15)":       {},
		"DEBUG(42)":      {},
		"STRDUMP(slice)": {},
		"STRDUMP(int)":   {},
		"DEBUGSTR":       {},

		"SHA256U(empty)":    {},
		"HASHBU(ref)":       {},
		"HASHEXT(0,concat)": {},
		"LDVARUINT32(zero)": {},
		"STVARUINT32(zero)": {},
		"STVARINT16(zero)":  {},

		"ACCEPT":                       {},
		"SETGASLIMIT":                  {},
		"GASCONSUMED":                  {},
		"COMMIT":                       {},
		"SETGLOB(20)":                  {},
		"GETGLOB(20)":                  {},
		"SETGLOBVAR(21)":               {},
		"GETGLOBVAR(21)":               {},
		"PUSHCTR(4)":                   {},
		"PUSHCTR(5)":                   {},
		"PUSHCTR(5/*":                  {},
		"POPCTR(4)":                    {},
		"POPCTR(5)":                    {},
		"PUSHCTRX(4)":                  {},
		"POPCTRX(4)":                   {},
		"SAVECTR(4)":                   {},
		"SAVEALTCTR(4)":                {},
		"SAVEBOTHCTR(4)":               {},
		"POPSAVECTR(4)":                {},
		"SETRETCTR(4)":                 {},
		"SETALTCTR(4)":                 {},
		"SENDRAWMSG":                   {},
		"RAWRESERVE":                   {},
		"RAWRESERVEX":                  {},
		"SETCODE":                      {},
		"SETLIBCODE":                   {},
		"CHANGELIB":                    {},
		"SENDMSG(fee-only)":            {},
		"SENDMSG(send)":                {},
		"SENDMSG(user-fwd-fee)":        {},
		"CONDSEL":                      {},
		"CONDSELCHK":                   {},
		"SETCP(0)":                     {},
		"SETCPX":                       {},
		"THROWIF(skip)":                {},
		"THROWIFNOT(skip)":             {},
		"INVERT":                       {},
		"IF(true)":                     {},
		"IF(false)":                    {},
		"IFNOT(true)":                  {},
		"IFNOT(false)":                 {},
		"IFELSE(true)":                 {},
		"IFELSE(false)":                {},
		"EXECUTE(push70)":              {},
		"EXECUTE(blessed_push80)":      {},
		"EXECUTE(blessargs)":           {},
		"PUSHCONT(*":                   {},
		"CALLREF(push71)":              {},
		"IFREF(push72,true)":           {},
		"IFREF(push72,false)":          {},
		"IFNOTREF(push73,true)":        {},
		"IFNOTREF(push73,false)":       {},
		"IFREFELSEREF(true74,false75)": {},
		"IFREFELSE(true76,false77)":    {},
		"IFELSEREF(true78,false79)":    {},
		"BLESS(push80)":                {},
		"BLESSARGS(1,0)":               {},
		"REPEAT(one)":                  {},
		"WHILE(one)":                   {},
		"UNTIL(one)":                   {},
		"TRY(caught)":                  {},
		"TRYARGS(1,1)":                 {},
		"RUNVM(0)":                     {},
		"RUNVMX(0)":                    {},
		"RUNVM(3)":                     {},
		"RUNVM(256)":                   {},
		"RUNVM(36)":                    {},
		"RUNVM(272)":                   {},
		"RUNVM(128)":                   {},
		"RUNVM(16/inmsgparams)":        {},

		"MYCODE":               {},
		"INCOMINGVALUE":        {},
		"STORAGEFEES":          {},
		"DUEPAYMENT":           {},
		"GLOBALID":             {},
		"PREVBLOCKSINFOTUPLE":  {},
		"PREVMCBLOCKS":         {},
		"PREVKEYBLOCK":         {},
		"PREVMCBLOCKS_100":     {},
		"GETPRECOMPILEDGAS":    {},
		"INMSG_BOUNCE":         {},
		"INMSG_SRC":            {},
		"INMSG_VALUE":          {},
		"GETGASFEE":            {},
		"GETSTORAGEFEE":        {},
		"GETFORWARDFEE":        {},
		"GETORIGINALFWDFEE":    {},
		"GETGASFEESIMPLE":      {},
		"GETFORWARDFEESIMPLE":  {},
		"NOW":                  {},
		"GETPARAM(3)":          {},
		"BLOCKLT":              {},
		"LTIME":                {},
		"RANDSEED":             {},
		"BALANCE":              {},
		"MYADDR":               {},
		"CONFIGROOT":           {},
		"CONFIGDICT":           {},
		"CONFIGPARAM(hit)":     {},
		"CONFIGPARAM(miss)":    {},
		"CONFIGOPTPARAM(hit)":  {},
		"CONFIGOPTPARAM(miss)": {},
		"GETPARAMLONG(6)":      {},
		"UNPACKEDCONFIGTUPLE":  {},
		"INMSGPARAMS":          {},
		"INMSGPARAM(0)":        {},
		"INMSGPARAM(2)":        {},
		"INMSGPARAM(7)":        {},
		"INMSGPARAM(8)":        {},
		"INMSGPARAM(9)":        {},
		"CDATASIZE":            {},
		"CDATASIZEQ":           {},
		"SDATASIZE":            {},
		"SDATASIZEQ":           {},
		"RANDU256":             {},
		"RAND":                 {},
		"SETRAND":              {},
		"ADDRAND":              {},
		"RAND(0)":              {},
		"RAND(negative)":       {},
		"RANDSEED(after_prng)": {},
		"SETRAND(max)":         {},
		"LDVARUINT32":          {},
		"LDVARINT16":           {},
		"LDVARINT32":           {},
		"STVARUINT32":          {},
		"STVARINT16":           {},
		"STVARINT32":           {},

		"TUPLE(*":        {},
		"UNTUPLE(*":      {},
		"UNPACKFIRST(*":  {},
		"INDEX(*":        {},
		"INDEXQ(*":       {},
		"SETINDEX(*":     {},
		"EXPLODE(*":      {},
		"ISNULL":         {},
		"ISTUPLE":        {},
		"QTLEN":          {},
		"TLEN":           {},
		"LAST":           {},
		"TPUSH":          {},
		"TPOP":           {},
		"TUPLEVAR":       {},
		"INDEXVAR":       {},
		"INDEXVARQ":      {},
		"UNTUPLEVAR":     {},
		"UNPACKFIRSTVAR": {},
		"EXPLODEVAR":     {},
		"INDEX2(0,1)":    {},
		"INDEX3(0,0,1)":  {},
		"SETINDEXQ(1)":   {},
		"SETINDEXVAR":    {},
		"SETINDEXVARQ":   {},
		"NULLSWAPIF":     {},
		"NULLSWAPIFNOT":  {},
		"NULLROTRIF":     {},
		"NULLSWAPIF2":    {},
		"NULLROTRIFNOT":  {},
		"NULLSWAPIFNOT2": {},
		"NULLROTRIF2":    {},
		"NULLROTRIFNOT2": {},
	}

	c7Targets := map[string]struct{}{}
	for _, target := range []string{
		"CONFIGPARAM(hit)",
		"CONFIGPARAM(miss)",
		"CONFIGOPTPARAM(hit)",
		"CONFIGOPTPARAM(miss)",
	} {
		c7Targets[target] = targets[target]
		delete(targets, target)
	}
	richC7Targets := map[string]struct{}{}
	for _, target := range []string{
		"UNPACKEDCONFIGTUPLE",
		"INMSGPARAMS",
		"INMSGPARAM(0)",
		"INMSGPARAM(2)",
		"INMSGPARAM(7)",
		"INMSGPARAM(8)",
		"INMSGPARAM(9)",
	} {
		richC7Targets[target] = targets[target]
		delete(targets, target)
	}
	runVMTargets := map[string]struct{}{}
	for _, target := range []string{
		"RUNVM(0)",
		"RUNVMX(0)",
		"RUNVM(3)",
		"RUNVM(256)",
		"RUNVM(36)",
		"RUNVM(272)",
		"RUNVM(128)",
		"RUNVM(16/inmsgparams)",
	} {
		runVMTargets[target] = targets[target]
	}
	delete(targets, "RUNVM(16/inmsgparams)")

	for seed := uint64(0); seed < seeds && len(targets) > 0; seed++ {
		g := newParityProgramGenerator(t, rand.New(rand.NewSource(int64(seed))))
		g.seedInitialStack()
		for i := 0; i < steps; i++ {
			if !g.emitRandomOp() {
				g.emitPushValueOp()
			}
		}

		for target := range targets {
			if parityProgramTraceHasOpcode(g.trace, target) {
				delete(targets, target)
			}
		}
	}

	if len(targets) > 0 {
		missing := make([]string, 0, len(targets))
		for target := range targets {
			missing = append(missing, target)
		}
		sort.Strings(missing)
		t.Fatalf("program generator did not reach residual opcode families over %d seeds x %d ops:\n%s",
			seeds, steps, strings.Join(missing, "\n"))
	}

	for seed := uint64(0); seed < seeds && len(c7Targets) > 0; seed++ {
		version := differentialFuzzSeedVersion(seed)
		refCfg := versionedC7ProgramRefConfig(t, version)
		g := newParityProgramGenerator(t, rand.New(rand.NewSource(int64(seed))))
		g.c7ConfigRoot = refCfg.ConfigRoot
		g.seedInitialStack()
		for i := 0; i < steps; i++ {
			if !g.emitRandomVersionedC7ProgramOp() {
				g.emitPushValueOp()
			}
		}

		for target := range c7Targets {
			if parityProgramTraceHasOpcode(g.trace, target) {
				delete(c7Targets, target)
			}
		}
	}

	if len(c7Targets) > 0 {
		missing := make([]string, 0, len(c7Targets))
		for target := range c7Targets {
			missing = append(missing, target)
		}
		sort.Strings(missing)
		t.Fatalf("versioned C7 program generator did not reach residual opcode families over %d seeds x %d ops:\n%s",
			seeds, steps, strings.Join(missing, "\n"))
	}

	for seed := uint64(0); seed < seeds && len(runVMTargets) > 0; seed++ {
		version := differentialFuzzSeedVersion(seed)
		g := newParityProgramGenerator(t, rand.New(rand.NewSource(int64(seed))))
		g.seedInitialStack()
		for i := 0; i < steps; i++ {
			if !g.emitRandomVersionedRunVMProgramOp(version) {
				g.emitPushValueOp()
			}
		}

		for target := range runVMTargets {
			if parityProgramTraceHasOpcode(g.trace, target) {
				delete(runVMTargets, target)
			}
		}
	}

	if len(runVMTargets) > 0 {
		missing := make([]string, 0, len(runVMTargets))
		for target := range runVMTargets {
			missing = append(missing, target)
		}
		sort.Strings(missing)
		t.Fatalf("versioned RUNVM program generator did not reach residual opcode families over %d seeds x %d ops:\n%s",
			seeds, steps, strings.Join(missing, "\n"))
	}

	for seed := uint64(0); seed < seeds && len(richC7Targets) > 0; seed++ {
		version := differentialFuzzSeedVersion(seed)
		configRoot := versionedC7ProgramConfigRoot(t, version)
		g := newParityProgramGenerator(t, rand.New(rand.NewSource(int64(seed))))
		g.c7 = parityProgramVersionedRichC7(t, version)
		g.c7ConfigRoot = configRoot
		g.seedInitialStack()
		for i := 0; i < steps; i++ {
			if !g.emitRandomVersionedRichC7ProgramOp() {
				g.emitPushValueOp()
			}
		}

		for target := range richC7Targets {
			if parityProgramTraceHasOpcode(g.trace, target) {
				delete(richC7Targets, target)
			}
		}
	}

	if len(richC7Targets) == 0 {
		return
	}

	missing := make([]string, 0, len(richC7Targets))
	for target := range richC7Targets {
		missing = append(missing, target)
	}
	sort.Strings(missing)
	t.Fatalf("versioned rich C7 program generator did not reach residual opcode families over %d seeds x %d ops:\n%s",
		seeds, steps, strings.Join(missing, "\n"))
}

func parityProgramTraceHasOpcode(trace []string, name string) bool {
	if strings.HasSuffix(name, "*") {
		prefix := strings.TrimSuffix(name, "*")
		for _, entry := range trace {
			if strings.HasPrefix(entry, prefix) {
				return true
			}
		}
		return false
	}
	for _, entry := range trace {
		if entry == name {
			return true
		}
	}
	return false
}

func parityProgramGeneratorLiteralTraceLabelsFromSource(t *testing.T) map[string]struct{} {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to resolve current test file")
	}

	src, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("failed to read current test file: %v", err)
	}

	labels := map[string]struct{}{}
	emitNeedles := []string{
		"g.emit(" + string('"'),
		"g.emitUnaryIntOp(" + string('"'),
		"g.emitUnaryIntToSmallOp(" + string('"'),
		"g.emitBinaryIntOp(" + string('"'),
		"g.emitBinaryIntToSmallOp(" + string('"'),
		"g.emitBuilderMetaOp(" + string('"'),
		"g.emitLoadMsgAddressOp(" + string('"'),
	}
	for _, line := range strings.Split(string(src), "\n") {
		if strings.Contains(line, "string('\"')") {
			continue
		}
		for _, needle := range emitNeedles {
			if idx := strings.Index(line, needle); idx >= 0 {
				if label, ok := parityProgramFirstQuotedString(line[idx:]); ok {
					labels[label] = struct{}{}
				}
			}
		}
	}
	return labels
}

func TestTVMDifferentialFuzzProgramGeneratorLiteralTraceLabelsAreTargeted(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to resolve current test file")
	}

	src, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("failed to read current test file: %v", err)
	}

	targets := map[string]struct{}{}
	emitLabels := map[string]struct{}{}
	targetStart := "targets := " + "map[string]struct{}{"
	emitNeedles := []string{
		"g.emit(" + string('"'),
		"g.emitUnaryIntOp(" + string('"'),
		"g.emitUnaryIntToSmallOp(" + string('"'),
		"g.emitBinaryIntOp(" + string('"'),
		"g.emitBinaryIntToSmallOp(" + string('"'),
		"g.emitBuilderMetaOp(" + string('"'),
		"g.emitLoadMsgAddressOp(" + string('"'),
	}
	inTargets := false
	for _, line := range strings.Split(string(src), "\n") {
		if strings.Contains(line, "string('\"')") {
			continue
		}
		if strings.Contains(line, targetStart) {
			inTargets = true
			continue
		}
		if inTargets {
			if strings.TrimSpace(line) == "}" {
				inTargets = false
				continue
			}
			if label, ok := parityProgramFirstQuotedString(line); ok {
				targets[label] = struct{}{}
			}
			continue
		}

		for _, needle := range emitNeedles {
			if idx := strings.Index(line, needle); idx >= 0 {
				if label, ok := parityProgramFirstQuotedString(line[idx:]); ok {
					emitLabels[label] = struct{}{}
				}
			}
		}
	}

	for _, label := range requiredDictGapTraceLabels {
		emitLabels[label] = struct{}{}
	}
	for _, label := range requiredDictNearGapTraceLabels {
		emitLabels[label] = struct{}{}
	}
	for _, label := range requiredDictProgramTraceLabels {
		emitLabels[label] = struct{}{}
	}
	for _, label := range requiredMathProgramTraceLabels {
		emitLabels[label] = struct{}{}
	}
	for _, label := range requiredMathCompoundTraceLabels {
		emitLabels[label] = struct{}{}
	}
	for _, label := range requiredMathShiftTraceLabels {
		emitLabels[label] = struct{}{}
	}
	for _, label := range requiredMathGapTraceLabels {
		emitLabels[label] = struct{}{}
	}
	for _, label := range requiredMathQuietLogicTraceLabels {
		emitLabels[label] = struct{}{}
	}
	for _, label := range requiredMathQuietCompoundTraceLabels {
		emitLabels[label] = struct{}{}
	}
	for _, label := range requiredMsgAddressProgramTraceLabels {
		emitLabels[label] = struct{}{}
	}
	for _, label := range requiredDataSizeProgramTraceLabels {
		emitLabels[label] = struct{}{}
	}
	for _, label := range requiredCellSliceProgramTraceLabels {
		emitLabels[label] = struct{}{}
	}

	if len(targets) == 0 {
		t.Fatal("failed to parse generator reachability targets")
	}
	if len(emitLabels) == 0 {
		t.Fatal("failed to parse generator trace labels")
	}

	var missing []string
	for label := range emitLabels {
		if !parityProgramTraceLabelIsTargeted(label, targets) {
			missing = append(missing, label)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("generator trace labels are missing reachability targets:\n%s", strings.Join(missing, "\n"))
	}
}

func parityProgramFirstQuotedString(line string) (string, bool) {
	start := strings.IndexByte(line, '"')
	if start < 0 {
		return "", false
	}
	end := strings.IndexByte(line[start+1:], '"')
	if end < 0 {
		return "", false
	}
	return line[start+1 : start+1+end], true
}

func parityProgramTraceLabelIsTargeted(label string, targets map[string]struct{}) bool {
	if _, ok := targets[label]; ok {
		return true
	}
	for target := range targets {
		if strings.HasSuffix(target, "*") && strings.HasPrefix(label, strings.TrimSuffix(target, "*")) {
			return true
		}
	}
	return false
}

var requiredExecGapCaseNames = []string{
	"jmpx_pushes_value",
	"try_catches_throw",
	"tryargs_returns_value",
	"tryargs_catches_throwarg",
	"throw_short_uncaught",
	"throw_long_uncaught",
	"throwarg_long_uncaught",
	"throwif_taken",
	"throwifnot_taken",
	"throwargif_taken",
	"throwargif_skip",
	"throwargifnot_taken",
	"throwargifnot_skip",
	"throwany_uncaught",
	"throwargany_uncaught",
	"throwanyif_taken",
	"throwanyif_skip",
	"throwanyif_skip_invalid_exc_range",
	"throwarganyif_taken",
	"throwarganyif_skip",
	"throwarganyif_skip_invalid_exc_range",
	"throwanyifnot_taken",
	"throwanyifnot_skip",
	"throwanyifnot_skip_invalid_exc_range",
	"throwarganyifnot_taken",
	"throwarganyifnot_skip",
	"throwarganyifnot_skip_invalid_exc_range",
	"execute_empty_stack_underflow",
	"execute_non_cont_typecheck",
	"callxargs_params_underflow",
	"jmpxargs_params_underflow",
	"callccvarargs_pass_all_return_all",
	"callccvarargs_bad_retvals_range",
	"retvarargs_all_from_called_continuation",
	"returnvarargs_zero_count_moves_all",
	"returnvarargs_bad_count_range",
	"returnargs_closure_overflow",
	"tryargs_zero_retvals_caught_throwarg",
	"setcontctr_success",
	"setcontctrx_success",
	"setcontctrmany_success",
	"setcontctrmanyx_success",
	"setcontctrmany_c6_range",
	"setcontctrmanyx_c6_range",
	"popctrx_c7_tuple_roundtrip",
	"pushctrx_c6_range",
	"popctrx_c6_range",
	"popctr_c0_noncont_typecheck",
	"popsavectr_c0_noncont_typecheck",
	"setretctr_c4_restore_on_ret",
	"setaltctr_c4_restore_on_retalt",
	"savectr_c4_restore_on_ret",
	"savealtctr_c4_restore_on_retalt",
	"savebothctr_c4_restore_on_ret",
	"savebothctr_c4_restore_on_retalt",
	"popsavectr_c4_restore_on_ret",
	"setretctr_c7_restore_on_ret",
	"setaltctr_c7_restore_on_retalt",
	"savectr_c7_restore_on_ret",
	"savealtctr_c7_restore_on_retalt",
	"savebothctr_c7_restore_on_ret",
	"savebothctr_c7_restore_on_retalt",
	"popsavectr_c7_restore_on_ret",
	"booleval_false_branch",
	"booleval_true_branch_resumes_tail",
	"invert_ret",
	"samealt_copy_is_independent",
	"if_taken_call",
	"if_not_taken",
	"ifnot_taken_call",
	"ifnot_not_taken",
	"ifelse_true_branch",
	"ifelse_false_branch",
	"ifjmp_taken",
	"ifjmp_not_taken",
	"ifnotjmp_taken",
	"ifnotjmp_not_taken",
	"ifbitjmp_not_taken_preserves_int",
	"ifbitjmp_taken",
	"ifbitjmp_high_negative_taken",
	"ifnbitjmp_taken",
	"ifnbitjmp_not_taken_preserves_int",
	"ifnbitjmp_high_negative_not_taken",
	"ifbitjmpref_taken",
	"ifbitjmpref_not_taken",
	"ifnbitjmpref_taken",
	"ifnbitjmpref_not_taken",
	"ifnotjmpref_not_taken",
	"ifref_skips_invalid_ref_false",
	"ifnotref_skips_invalid_ref_true",
	"ifjmpref_skips_invalid_ref_false",
	"ifnotjmpref_skips_invalid_ref_true",
	"ifrefelse_skips_invalid_ref_false_branch",
	"ifelseref_skips_invalid_ref_true_branch",
	"ifrefelseref_true_branch",
	"ifbitjmpref_skips_invalid_ref_not_taken",
	"ifretalt_continue",
	"ifretalt_taken",
	"ifnotretalt_continue",
	"ifnotretalt_taken",
	"repeat_two_iterations",
	"repeat_negative_count_skips",
	"repeatend_one_iteration",
	"while_first_condition_false",
	"while_one_iteration_then_false",
	"whileend_one_iteration_then_false",
	"until_one_iteration",
	"untilend_one_iteration",
	"again_throw_bounded",
	"againend_throw_bounded",
	"jmpxdata_valid_remaining_slice",
	"jmpxargs_two_params",
	"setcontargs_capture_all",
	"callxargs_trim",
	"callxargsp_all_returns",
	"callxargs_sum_then_tail",
	"callxvarargs_dynamic",
	"callxvarargs_dynamic_params_and_returns",
	"jmpxvarargs_dynamic_params",
	"retvarargs_trim",
	"retvarargs_from_called_continuation",
	"returnvarargs_dynamic_count",
	"returnargs_fixed_count",
	"retbool_return_branch",
	"retbool_alt_branch",
	"retdata_remaining_slice",
	"ifret_taken",
	"ifret_continue",
	"ifnotret_taken",
	"ifnotret_continue",
	"callcc_pushes_current_continuation",
	"callccargs_preserves_arg",
	"callccvarargs_dynamic",
	"setcontvarargs_execute",
	"setnumvarargs_execute",
	"bless_execute",
	"blessargs_execute",
	"blessvarargs_execute",
	"pushctr_c0_drop",
	"calldict_short",
	"calldict_long",
	"jmpdict",
	"preparedict_execute",
	"pushrefcont_execute",
	"callref_pushes_value",
	"jmpref_pushes_value",
	"jmprefdata_exposes_remaining_code",
	"ifjmpref_taken",
	"ifjmpref_not_taken",
	"ifnotjmpref_taken",
	"atexit_runs",
	"atexitalt_runs",
	"setexitalt_runs",
	"thenret_runs",
	"thenretalt_runs",
	"booland_composes_return_continuation",
	"boolor_composes_alt_continuation",
	"composboth_composes_return_and_alt",
	"samealt_copies_return_to_alt",
	"samealtsave_preserves_previous_alt",
	"repeatbrk_break",
	"repeatendbrk_break",
	"untilbrk_break",
	"untilendbrk_break",
	"whilebrk_break",
	"whileendbrk_break",
	"againbrk_break",
	"againendbrk_break",
}

var requiredExecRefDecodeGapCaseNames = []string{
	"callref_missing_ref_invalid",
	"jmpref_missing_ref_invalid",
	"jmprefdata_missing_ref_invalid",
	"ifref_missing_ref_invalid",
	"ifnotref_missing_ref_invalid",
	"ifjmpref_missing_ref_invalid",
	"ifnotjmpref_missing_ref_invalid",
	"ifrefelse_missing_ref_invalid",
	"ifelseref_missing_ref_invalid",
	"ifrefelseref_missing_refs_invalid",
	"ifbitjmpref_missing_ref_invalid",
	"ifnbitjmpref_missing_ref_invalid",
}

var requiredRunVMGapCaseNames = []string{
	"runvm_c7_return_one",
	"runvmx_gas_bounds_return_one",
	"runvm_data_actions_and_gas",
	"runvm_full_inputs_return_data_actions_gas",
	"runvm_mode512_rangecheck_no_stack",
	"runvmx_mode512_rangecheck_no_child",
}

var requiredDictGapTraceLabels = []string{
	"DICTIGETREF",
	"DICTUGETREF",
	"DICTISET",
	"DICTSETREF",
	"DICTISETREF",
	"DICTUSETREF",
	"DICTISETGET",
	"DICTUSETGET",
	"DICTISETGETREF",
	"DICTUSETGETREF",
	"DICTIREPLACE",
	"DICTUREPLACE",
	"DICTREPLACEREF",
	"DICTIREPLACEREF",
	"DICTUREPLACEREF",
	"DICTIADD",
	"DICTUADD",
	"DICTADDREF",
	"DICTIADDREF",
	"DICTUADDREF",
	"DICTIDEL",
	"DICTIGETOPTREF",
	"DICTMINREF",
	"DICTMAXREF",
	"DICTIMIN",
	"DICTUMAX",
	"DICTIMINREF",
	"DICTUMAXREF",
	"DICTREMMINREF",
	"DICTREMMAXREF",
	"DICTIREMMIN",
	"DICTUREMMAXREF",
	"DICTADDB",
	"DICTADDGETB",
	"DICTADDGETREF",
	"DICTDELGETREF",
	"DICTIADDGET",
	"DICTIADDGETREF",
	"DICTIDELGET",
	"DICTIDELGETREF",
	"DICTIMAX",
	"DICTIMAXREF",
	"DICTIREMMAX",
	"DICTIREMMAXREF",
	"DICTIREMMINREF",
	"DICTIREPLACEB",
	"DICTIREPLACEGET",
	"DICTIREPLACEGETB",
	"DICTIREPLACEGETREF",
	"DICTISETB",
	"DICTISETGETB",
	"DICTISETGETOPTREF",
	"DICTISUBDICTGET",
	"DICTISUBDICTRPGET",
	"DICTREPLACEB",
	"DICTREPLACEGET",
	"DICTREPLACEGETB",
	"DICTREPLACEGETREF",
	"DICTSETGETOPTREF",
	"DICTUADDGET",
	"DICTUREMMAX",
	"DICTUREPLACEGETREF",
	"DICTUSETGETB",
}

var requiredDictNearGapTraceLabels = []string{
	"DICTGETNEXT",
	"DICTGETNEXTEQ",
	"DICTGETPREV",
	"DICTGETPREVEQ",
	"DICTIGETNEXT",
	"DICTIGETNEXTEQ",
	"DICTIGETPREV",
	"DICTIGETPREVEQ",
	"DICTUGETNEXT",
	"DICTUGETNEXTEQ",
	"DICTUGETPREV",
	"DICTUGETPREVEQ",
}

var requiredDictProgramTraceLabels = []string{
	"LDDICT",
	"PLDDICT",
	"STDICT",
	"SKIPDICT",
	"LDDICTS",
	"PLDDICTS",
	"LDDICTQ",
	"PLDDICTQ",
	"DICTGET",
	"DICTGETREF",
	"DICTIGET",
	"DICTUGET",
	"DICTSET",
	"DICTUSET",
	"DICTDEL",
	"DICTMIN",
	"DICTMAX",
	"DICTGETOPTREF",
	"DICTUGETOPTREF",
	"DICTSETGET",
	"DICTSETGETREF",
	"DICTREPLACE",
	"DICTADD",
	"DICTREMMIN",
	"DICTREMMAX",
	"DICTGETNEXTEQ",
	"DICTSETB",
	"DICTUREPLACEB",
	"DICTIADDB",
	"DICTSETGETB",
	"DICTUREPLACEGETB",
	"DICTIADDGETB",
	"DICTDELGET",
	"DICTUDELGETREF",
	"DICTUSETGETOPTREF",
	"DICTSUBDICTGET",
	"DICTSUBDICTRPGET",
	"DICTUSUBDICTGET",
	"DICTUSUBDICTRPGET",
	"PFXDICTGET",
	"PFXDICTGETQ",
	"PFXDICTREPLACE",
	"PFXDICTADD",
	"PFXDICTSET",
	"PFXDICTDEL",
}

var requiredMathProgramTraceLabels = []string{
	"NEGATE",
	"INC",
	"DEC",
	"ABS",
	"NOT",
	"BITSIZE",
	"ISNPOS",
	"ISZERO",
	"ISPOS",
	"ISNEG",
	"ADD",
	"SUB",
	"SUBR",
	"MUL",
	"AND",
	"OR",
	"XOR",
	"MIN",
	"MAX",
	"MINMAX",
	"LESS",
	"LEQ",
	"GREATER",
	"GEQ",
	"EQUAL",
	"NEQ",
	"CMP",
}

var requiredMathCompoundTraceLabels = []string{
	"DIVR",
	"DIVC",
	"MODR",
	"MODC",
	"DIVMODR",
	"DIVMODC",
	"MULMOD",
	"MULMODR",
	"MULMODC",
	"MULDIV",
	"MULDIVR",
	"MULDIVC",
	"MULDIVMOD",
	"MULDIVMODR",
	"MULDIVMODC",
	"ADDDIVMOD",
	"ADDDIVMODR",
	"ADDDIVMODC",
	"MULADDDIVMOD",
	"MULADDDIVMODR",
	"MULADDDIVMODC",
}

var requiredMathShiftTraceLabels = []string{
	"RSHIFTFLOOR",
	"RSHIFTR",
	"RSHIFTC",
	"MODPOW2",
	"MODPOW2R",
	"MODPOW2C",
	"RSHIFTCODEFLOOR(3)",
	"MULRSHIFT",
	"MULRSHIFTR",
	"MULRSHIFTC",
	"LSHIFTDIV",
	"LSHIFTDIVR",
	"LSHIFTDIVC",
	"LSHIFTDIVMOD",
	"LSHIFTADDDIVMOD",
	"LSHIFTADDDIVMODR",
	"LSHIFTADDDIVMODC",
	"MULADDRSHIFTMOD",
	"MULADDRSHIFTRMOD",
	"MULADDRSHIFTCMOD",
}

var requiredMathGapTraceLabels = []string{
	"ISNNEG(0)",
	"ISNNEG(-1)",
	"ADDCONST(6)",
	"MULCONST(-4)",
	"RSHIFTMOD",
	"RSHIFTMODR",
	"RSHIFTMODC",
	"RSHIFTCODEMOD(3)",
	"RSHIFTRCODEMOD(3)",
	"RSHIFTCCODEMOD(3)",
	"LSHIFTMOD",
	"LSHIFTMODR",
	"LSHIFTMODC",
	"LSHIFTDIVMODR",
	"LSHIFTDIVMODC",
	"ADDRSHIFTMOD",
	"ADDRSHIFTMODR",
	"ADDRSHIFTMODC",
	"MULMODPOW2",
	"MULMODPOW2R",
	"MULMODPOW2C",
	"MULRSHIFTMOD",
	"MULRSHIFTRMOD",
	"MULRSHIFTCMOD",
	"MULMODPOW2CODE(3)",
	"MULMODPOW2RCODE(3)",
	"MULMODPOW2CCODE(3)",
	"MULRSHIFTCODEMOD(3)",
	"MULRSHIFTRCODEMOD(3)",
	"MULRSHIFTCCODEMOD(3)",
	"ADDRSHIFTCODEMOD(3)",
	"ADDRSHIFTRCODEMOD(3)",
	"ADDRSHIFTCCODEMOD(3)",
	"MULADDRSHIFTCODEMOD(3)",
	"MULADDRSHIFTRCODEMOD(3)",
	"MULADDRSHIFTCCODEMOD(3)",
}

var requiredMathQuietLogicTraceLabels = []string{
	"QNOT",
	"QAND",
	"QOR",
	"QXOR",
	"QLSHIFT",
	"QRSHIFT",
	"QLSHIFTCODE(3)",
	"QRSHIFTCODE(3)",
	"QPOW2",
	"QSGN",
	"QLESS",
	"QEQUAL",
	"QLEQ",
	"QGREATER",
	"QNEQ",
	"QGEQ",
	"QCMP",
	"QEQINT(5)",
	"QLESSINT(4)",
	"QGTINT(4)",
	"QNEQINT(4)",
	"QFITS(6)",
	"QFITS(3 fail)",
	"QUFITS(8)",
	"QFITSX",
	"QUFITSX(fail)",
	"QSGN(NaN)",
	"QGTINT(NaN)",
	"CHKNAN",
}

var requiredMathQuietCompoundTraceLabels = []string{
	"QDIV",
	"QMODR",
	"QADDDIVMODC",
	"QRSHIFT",
	"QMODPOW2R",
	"QADDRSHIFTMODC",
	"QMULDIV",
	"QMULMODR",
	"QMULADDDIVMODC",
	"QMULRSHIFT",
	"QMULMODPOW2R",
	"QMULADDRSHIFTMODC",
	"QLSHIFTDIV",
	"QLSHIFTMODR",
	"QLSHIFTADDDIVMODC",
}

var requiredDictSuccessGapCaseNames = []string{
	"dictugetoptref_hit",
	"dictusetgetoptref_replace_hit",
	"dictusetgetoptref_delete_hit",
	"dictmin_slice_hit",
	"dictumaxref_hit",
	"dicturemmaxref_hit",
	"dictusubdictget_success",
	"dictusubdictrpget_success",
	"pfxdictget_success",
	"pfxdictgetq_success",
	"pfxdictreplace_hit",
	"pfxdictadd_new",
	"pfxdictset_new",
	"pfxdictdel_hit",
}

var requiredDictMissGapCaseNames = []string{
	"dictget_miss_slice",
	"dictuget_miss_invalid_key",
	"dictugetref_miss",
	"dictiget_below_key_miss",
	"dictigetref_overflow_key_miss",
	"dictigetref_below_key_miss",
	"dictureplace_miss_false",
	"dictuadd_existing_false",
	"dictureplaceget_miss_false",
	"dictaddget_existing_returns_old",
	"dictuaddgetref_existing_returns_old",
	"dictudel_miss_false",
	"dictudelgetref_miss_false",
	"dictudel_invalid_key_rangecheck",
	"dictudelget_invalid_key_rangecheck",
	"dictidel_below_key_rangecheck",
	"dictidelget_invalid_key_rangecheck",
	"dictidelgetref_invalid_key_rangecheck",
	"dictidelget_below_key_rangecheck",
	"dictidelgetref_below_key_rangecheck",
	"dictugetoptref_miss_null",
	"dictugetoptref_invalid_key_null",
	"dictigetoptref_overflow_key_null",
	"dictigetoptref_below_key_null",
	"dictusetgetoptref_delete_miss_null",
	"dictisetgetoptref_overflow_key_rangecheck",
	"dictisetgetoptref_below_key_rangecheck",
	"dictusetgetoptref_overflow_key_rangecheck",
	"dictusetgetoptref_delete_overflow_key_rangecheck",
	"dicturemminref_key_len_rangecheck",
	"dictiremmaxref_key_len_rangecheck",
	"dictumin_empty_false",
	"dicturemmin_empty_false",
	"dictgetprev_miss_false",
	"dictugetprev_overflow_max",
	"dictugetnext_negative_returns_min",
	"dictugetnext_overflow_false",
	"dictigetprev_miss_false",
	"dictigetnext_below_min_returns_min",
	"dictgetnext_key_len_rangecheck",
	"dictgetprev_key_len_rangecheck",
	"dictigetnext_key_len_rangecheck",
	"dictigetprev_key_len_rangecheck",
	"dictugetnext_key_len_rangecheck",
	"dictugetprev_key_len_rangecheck",
	"pfxdictgetq_miss_false",
	"pfxdictgetq_nil_root_false",
	"pfxdictget_miss_cell_underflow",
	"pfxdictget_nil_root_cell_underflow",
	"pfxdictgetjmp_miss_keeps_input",
	"pfxdictgetjmp_nil_root_keeps_input",
	"pfxdictreplace_miss_false",
	"pfxdictadd_existing_false",
	"pfxdictdel_miss_false",
	"pfxdictset_oversized_key_false",
	"pfxdictset_oversized_key_nil_root_false",
	"pfxdictreplace_oversized_key_false",
	"pfxdictreplace_oversized_key_nil_root_false",
	"pfxdictadd_oversized_key_false",
	"pfxdictadd_oversized_key_nil_root_false",
	"pfxdictdel_oversized_key_false",
	"pfxdictdel_oversized_key_nil_root_false",
	"subdictuget_miss_dict_error",
	"dictureplaceb_miss_false",
	"dictuaddb_existing_false",
	"dictuaddgetb_existing_returns_old",
}

var requiredDictEdgeGapCaseNames = []string{
	"dictuset_negative_key_bad_value_typecheck_order",
	"dictusetb_negative_key_bad_builder_typecheck_order",
	"dictusetgetoptref_negative_key_bad_value_typecheck_order",
	"dictsetgetoptref_short_key_deferred_value_pop",
	"dictsetgetoptref_delete_short_key_deferred_value_pop",
	"dictsetgetoptref_update_plain_value_dict_error",
	"dictsetgetoptref_delete_plain_value_dict_error",
	"dictugetoptref_plain_value_dict_error",
	"dictudelgetref_plain_value_dict_error",
	"dictiminref_plain_value_dict_error",
	"dictimaxref_plain_value_dict_error",
	"dictiremminref_plain_value_dict_error",
	"dictiremmaxref_plain_value_dict_error",
	"dictuminref_plain_value_dict_error",
	"dictumaxref_plain_value_dict_error",
	"dicturemminref_plain_value_dict_error",
	"dicturemmaxref_plain_value_dict_error",
	"dictusetgetref_old_value_plain_slice",
	"dictset_library_root_underflow",
	"dictgetnext_short_key_underflow",
	"dictgetnexteq_short_key_underflow",
	"dictgetprev_short_key_underflow",
	"dictgetpreveq_short_key_underflow",
	"pfxdictgetq_input_with_refs_preserved",
	"dictsubdictget_slice_prefix_bits_range",
	"dictsubdictget_slice_prefix_underflow",
	"dictusubdictget_prefix_bits_range",
	"dictusubdictget_prefix_value_underflow",
	"subdictiget_prefix_bits_range",
	"subdictiget_prefix_value_underflow",
	"dictsubdictrpget_slice_prefix_bits_range",
	"dictsubdictrpget_slice_prefix_underflow",
	"dictusubdictrpget_prefix_bits_range",
	"dictusubdictrpget_prefix_value_underflow",
	"dictisubdictrpget_prefix_bits_range",
	"dictisubdictrpget_prefix_value_underflow",
}

var requiredActionGapTraceLabels = []string{
	"SENDRAWMSG",
	"RAWRESERVE",
	"RAWRESERVEX",
	"SETCODE",
	"SETLIBCODE",
	"CHANGELIB",
	"SENDMSG(fee-only)",
	"SENDMSG(send)",
	"SENDMSG(user-fwd-fee)",
}

var requiredActionModeGapCaseNames = []string{
	"rawreserve_mode_0",
	"rawreserve_mode_2",
	"rawreserve_mode_16",
	"rawreservex_mode_0",
	"rawreservex_mode_16",
	"setlibcode_mode_0",
	"setlibcode_mode_2",
	"setlibcode_mode_16",
	"changelib_mode_0",
	"changelib_mode_1",
	"changelib_mode_16",
	"sendmsg_mode64_incomingvalue",
	"sendmsg_mode128_balance",
	"sendmsg_extout_fee_only",
	"sendmsg_user_fwd_fee_lower_bound_gv13",
	"action_chain_sendrawmsg_then_rawreserve_hash",
	"action_chain_setcode_then_setlibcode_hash",
}

var requiredActionErrorGapCaseNames = []string{
	"rawreserve_negative_amount_range",
	"rawreservex_negative_amount_range",
	"rawreserve_missing_mode_underflow",
	"rawreservex_missing_extra_underflow",
	"setlibcode_missing_mode_underflow",
	"setlibcode_invalid_mode_range",
	"changelib_missing_mode_underflow",
	"changelib_negative_hash_range",
	"sendmsg_missing_mode_underflow",
	"sendmsg_invalid_mode_range",
	"sendrawmsg_missing_mode_underflow",
	"sendrawmsg_invalid_mode_range",
}

var requiredMathImmediateGapCaseNames = []string{
	"pushpow2",
	"pushpow2_nan_alias",
	"pushpow2dec",
	"pushnegpow2",
	"lessint_true",
	"eqint_true",
	"gtint_true",
	"neqint_false",
	"qaddint",
	"qmulint",
	"qeqint_true",
	"qlessint_true",
	"qgtint_nan",
	"qneqint_true",
	"fits_immediate",
	"ufits_immediate",
	"qfits_immediate_nan",
	"qufits_immediate",
	"lshift_code",
	"rshift_code_floor",
	"rshiftr_code_round",
	"rshiftc_code_ceil",
	"rshift_code_floor_alt",
	"lshift_code_nan_current",
	"rshift_code_nan_current",
	"qlshift_code",
	"qrshift_code",
	"qlshift_code_nan_current",
	"qrshift_code_nan_legacy",
	"rshift_code_mod_floor",
	"rshiftr_code_mod_round",
	"rshiftc_code_mod_ceil",
	"modpow2_code_floor",
	"modpow2r_code_round",
	"modpow2c_code_ceil",
	"mulrshift_code_floor",
	"mulrshiftr_code_round",
	"mulrshiftc_code_ceil",
	"mulrshift_code_mod_floor",
	"mulrshiftr_code_mod_round",
	"mulrshiftc_code_mod_ceil",
	"mulmodpow2_code_floor",
	"mulmodpow2r_code_round",
	"mulmodpow2c_code_ceil",
	"addrshift_code_mod_floor",
	"addrshiftr_code_mod_round",
	"addrshiftc_code_mod_ceil",
	"muladdrshift_code_mod_floor",
	"muladdrshiftr_code_mod_round",
	"muladdrshiftc_code_mod_ceil",
	"lshiftadddivmod_code_floor",
	"lshiftadddivmodr_code_round",
	"lshiftadddivmodc_code_ceil",
	"lshiftdiv_code_floor",
	"lshiftdivr_code_round",
	"lshiftdivc_code_ceil",
	"lshiftmod_code_floor",
	"lshiftmodr_code_round",
	"lshiftmodc_code_ceil",
	"lshiftdivmod_code_floor",
	"lshiftdivmodr_code_round",
	"lshiftdivmodc_code_ceil",
}

var requiredInvalidMathGapCaseNames = []string{
	"divmod_invalid",
	"shrmod_invalid",
	"shrcodemod_invalid",
	"muldivmod_invalid",
	"mulshrmod_invalid",
	"mulshrcodemod_invalid",
	"shldivmod_invalid",
	"shldivcodemod_invalid",
}

var requiredQuietMathErrorGapCaseNames = []string{
	"qdiv_zero_divisor_nan",
	"qmod_zero_divisor_nan",
	"qadddivmod_zero_divisor_double_nan",
	"qadddivmod_nan_addend_double_nan",
	"qmod_nan_operand_nan",
	"qaddrshiftmod_shift_over_256_legacy_rangecheck",
	"qaddrshiftmod_nan_shift_legacy_rangecheck",
	"qmuldiv_zero_divisor_nan",
	"qmuldiv_divisor_slice_typecheck",
	"qmulmodr_nan_factor_nan",
	"qmuladddivmod_zero_divisor_double_nan",
	"qmuladddivmod_nan_addend_double_nan",
	"qmuladdrshiftmod_negative_shift_double_nan",
	"qmuladdrshiftmod_nan_addend_double_nan",
	"qmulrshift_shift_over_256_nan",
	"qlshiftdiv_zero_divisor_nan",
	"qlshiftadddivmod_nan_addend_double_nan",
	"qlshiftadddivmod_negative_shift_double_nan",
	"qlshiftmod_shift_over_256_nan",
	"qlshift_negative_shift_nan",
	"qlshift_count_over_1023_nan",
	"qpow2_out_of_range_nan",
}

var requiredCellSliceGapCaseNames = []string{
	"ldule4q",
	"ldile8q",
	"pldule4q",
	"pldile8q",
	"bchkbitrefs",
	"stb",
	"stile4",
	"stule4",
	"stile8",
	"stule8",
	"bchkbitrefsq",
	"xctos_ordinary",
	"sdsfxrev",
	"sdpsfxrev",
	"sdbeginsx",
	"sdbeginsxq",
	"sdbeginsconst",
	"sdbeginsconstq",
	"ldgrams",
	"stgrams",
	"strefconst",
	"stref2const",
	"stsliceconst",
	"endcst",
	"stix_variable",
	"ldiq_fixed",
	"pldiq_fixed",
}

var requiredCellSliceQuietGapCaseNames = []string{
	"splitq_fail_preserves",
	"schkbitsq_true",
	"schkbitsq_false",
	"schkrefsq_true",
	"schkrefsq_false",
	"schkbitrefsq_true",
	"schkbitrefsq_false",
	"pldrefvar_idx1",
	"pldrefidx0",
	"pldrefidx1",
	"ldzeroes_nonzero",
	"ldzeroes_zero",
	"ldones_nonzero",
	"ldones_zero",
	"ldsame_one",
	"bchkbitsq_false",
	"bchkrefsq_false",
	"bchkbitrefsq_false",
	"ldixq_fail_preserves_slice",
	"pldixq_fail_only_flag",
	"lduxq_fail_preserves_slice",
	"plduxq_fail_only_flag",
	"ldslicexq_success",
	"ldslicexq_fail_preserves_slice",
	"ldslicexq_zero_width",
	"pldslicexq_success",
	"pldslicexq_fail_only_flag",
	"pldslicexq_zero_width",
	"ldslicefixq_fail_preserves_slice",
	"pldslicefixq_fail_only_flag",
	"ldule8q_fail_preserves_slice",
	"pldule8q_fail_only_flag",
	"ldile8q_success",
	"pldile8q_success",
	"strefq_success",
	"strefq_fail_preserves",
	"strefrq_success",
	"strefrq_fail_preserves",
	"stbrefq_success",
	"stbrefq_fail_preserves",
	"stbrefqr_success",
	"stbrefqr_fail_preserves",
	"stsliceq_success",
	"stsliceq_fail_preserves",
	"stslicerq_success",
	"stslicerq_fail_preserves",
	"stbq_success",
	"stbq_fail_preserves",
	"stbrq_success",
	"stbrq_fail_preserves",
	"stuxq_range_fail_status_plus_one",
	"stuxq_capacity_fail_status_minus_one",
}

var requiredCellSliceEdgeGapCaseNames = []string{
	"lduxq_width_257_rangecheck",
	"plduxq_width_257_rangecheck",
	"ldixq_width_258_rangecheck",
	"pldixq_width_258_rangecheck",
	"schkbitrefsq_refs5_rangecheck",
	"chashix_short_stack_bad_idx_order",
	"cdepthix_short_stack_bad_idx_order",
	"subslice_r2_range_precedes_slice_typecheck",
	"plduz_256_short_255_zero_extend",
	"pldrefidx3_four_refs",
	"pldrefvar_idx3_four_refs",
	"pldrefidx3_underflow",
	"ldrefrotos_no_ref_underflow",
	"bchkbitsimmq_true",
	"bchkbitsimmq_false",
	"stsliceconst_with_ref",
	"stsliceconst_bits_overflow",
	"stref2const_overflow",
	"endcst_dst_ref_overflow",
}

var requiredInventoryResidualGapCaseNames = []string{
	"nop_keeps_stack",
	"multi_xc2pu",
	"multi_xcpuxc",
	"multi_xcpu2",
	"multi_puxc2",
	"multi_puxcpu",
	"multi_puxc",
	"multi_xcpu",
	"gtint_true",
	"qgtint_nan",
	"sgn_negative",
	"qsgn_nan",
	"endxc_ordinary",
	"bbitrefs",
	"btos",
	"split_valid",
	"splitq_valid",
	"schkbits",
	"schkrefs",
	"schkbitrefs",
	"schkbitsq_false",
	"schkrefsq_false",
	"schkbitrefsq_false",
	"clevel",
	"clevelmask",
	"sdcntlead0",
	"sdcntlead1",
	"sdcnttrail0",
	"sdcnttrail1",
	"sdsfx",
	"sdpsfx",
	"brembits",
	"bremrefs",
	"brembitrefs",
	"accept",
	"setgaslimit",
	"gasconsumed",
	"commit",
}

var requiredLibraryGapCaseNames = []string{
	"ctos_library_resolution",
	"xload_library_resolution",
	"xloadq_library_resolution",
	"xctos_library_resolution",
	"xctos_library_special",
	"xload_library_missing_underflow",
	"xloadq_library_missing_false",
}

var requiredDictContinuationGapCaseNames = []string{
	"dictigetjmp_hit",
	"dictugetjmp_hit",
	"dictigetexec_hit",
	"dictugetexec_hit",
	"dictigetjmpz_hit",
	"dictugetjmpz_hit",
	"dictigetexecz_hit",
	"dictugetexecz_hit",
	"pfxdictgetjmp_hit",
	"pfxdictgetexec_hit",
	"pfxdictswitch_hit",
	"dictigetjmp_miss_falls_through",
	"dictugetexec_miss_falls_through",
	"dictigetjmpz_miss_keeps_index",
	"dictugetexecz_miss_keeps_index",
	"pfxdictgetjmp_miss_keeps_input",
	"pfxdictgetjmp_nil_root_keeps_input",
	"pfxdictgetexec_miss_cell_underflow",
	"pfxdictgetexec_nil_root_cell_underflow",
	"pfxdictswitch_miss_keeps_input",
	"pfxdictswitch_nil_root_flag_with_ref_underflow",
}

var requiredTonFuncGapCaseNames = []string{
	"mycode",
	"incomingvalue",
	"storagefees",
	"duepayment",
	"globalid",
	"unpackedconfigtuple",
	"configdict",
	"configparam_hit",
	"configoptparam_hit",
	"configoptparam_miss",
	"prevblocksinfotuple",
	"prevmcblocks",
	"prevkeyblock",
	"prevmcblocks_100",
	"getprecompiledgas",
	"inmsgparams",
	"inmsgparam_bounce",
	"inmsgparam_src",
	"inmsgparam_value",
	"inmsgparam_valueextra",
	"inmsgparam_stateinit",
	"inmsgparam_oob_range_error",
	"inmsg_bounce",
	"inmsg_bounced",
	"inmsg_src",
	"inmsg_fwdfee",
	"inmsg_lt",
	"inmsg_utime",
	"inmsg_origvalue",
	"inmsg_value",
	"inmsg_valueextra",
	"inmsg_stateinit",
	"getstoragefee",
	"getgasfee",
	"getgasfee_missing_gas_v8_partial_pop",
	"getgasfee_missing_gas_v9_precheck",
	"getforwardfee",
	"getforwardfee_missing_bits_v8_partial_pop",
	"getforwardfee_missing_bits_v9_precheck",
	"getoriginalfwdfee",
	"getoriginalfwdfee_missing_fee_v8_partial_pop",
	"getoriginalfwdfee_missing_fee_v9_precheck",
	"getforwardfeesimple",
	"getforwardfeesimple_missing_bits_v8_partial_pop",
	"getforwardfeesimple_missing_bits_v9_precheck",
	"getgasfeesimple",
	"getgasfeesimple_missing_gas_v8_partial_pop",
	"getgasfeesimple_missing_gas_v9_precheck",
	"getstoragefee_missing_delta_v8_partial_pop",
	"getstoragefee_missing_delta_v9_precheck",
	"getstoragefee_masterchain",
	"getgasfee_masterchain",
	"getforwardfee_masterchain",
	"getoriginalfwdfee_masterchain",
	"getforwardfeesimple_masterchain",
	"getgasfeesimple_masterchain",
	"getextrabalance_hit",
	"getextrabalance_miss",
	"getextrabalance_repeated_hit",
	"getextrabalance_nil_dict",
	"getextrabalance_malformed_value",
}

var requiredMsgAddressGapCaseNames = []string{
	"ldmsgaddrq_short_std",
	"ldmsgaddr_ext_success_rest",
	"ldstdaddr_std_success_rest",
	"ldoptstdaddr_std_success_rest",
	"ldoptstdaddrq_none_success_rest",
	"ldstdaddrq_var_fail",
	"ldoptstdaddrq_short_fail",
	"parsemsgaddr_std_success",
	"parsemsgaddr_none_success",
	"parsemsgaddr_ext_success",
	"parsemsgaddr_var_v9_success",
	"parsemsgaddr_var_v10_fail",
	"parsemsgaddrq_invalid_anycast",
	"rewritestdaddr_std_success",
	"rewritevaraddr_std_success",
	"rewritevaraddr_var20_fail",
	"rewritevaraddrq_short_anycast_var_v9_false",
	"rewritevaraddr_short_anycast_var_v9_underflow",
	"rewritestdaddrq_anycast_std_v9_success",
	"rewritevaraddrq_anycast_std_v9_success",
	"rewritestdaddrq_anycast_std_v10_false",
	"rewritestdaddrq_var_fail",
	"rewritestdaddrq_var20_fail",
	"rewritevaraddrq_ext_fail",
	"ststdaddrq_std_success_status_false",
	"ststdaddrq_var_fail",
	"stoptstdaddrq_none",
	"stoptstdaddrq_non_slice_v12_restores_value",
	"stoptstdaddrq_non_slice_v13_restores_value",
	"stoptstdaddrq_non_slice_v14_restores_value",
	"stoptstdaddrq_std_success_status_false",
}

var requiredMsgAddressProgramTraceLabels = []string{
	"LDMSGADDR",
	"LDMSGADDRQ",
	"LDSTDADDR",
	"LDSTDADDRQ",
	"LDOPTSTDADDR",
	"LDOPTSTDADDRQ",
	"PARSEMSGADDR",
	"PARSEMSGADDRQ",
	"REWRITESTDADDR",
	"REWRITESTDADDRQ",
	"REWRITEVARADDR",
	"REWRITEVARADDRQ",
	"STSTDADDR",
	"STSTDADDRQ",
	"STOPTSTDADDR",
	"STOPTSTDADDRQ",
}

var requiredDataSizeProgramTraceLabels = []string{
	"CDATASIZE",
	"CDATASIZEQ",
	"SDATASIZE",
	"SDATASIZEQ",
}

var requiredCellSliceProgramTraceLabels = []string{
	"BBITS",
	"BREFS",
	"BDEPTH",
	"LDI8",
	"LDU8",
	"STI8",
	"STU8",
}

var requiredVarIntGapCaseNames = []string{
	"ldvarint16_zero_len",
	"ldvarint16_negative_minimal",
	"ldvaruint32_zero_len",
	"ldvarint32_negative_two_bytes",
	"ldvaruint32_short_payload",
	"stvarint16_zero_len",
	"stvarint16_negative_minimal",
	"stvaruint32_zero_len",
	"stvarint32_negative_minimal",
	"stvaruint32_max_len",
	"stvaruint32_negative_rangecheck",
	"stvarint32_overflow_rangecheck",
}

var requiredDataSizeGapCaseNames = []string{
	"cdatasize_nil_success_zero",
	"cdatasize_success_counts_ref",
	"cdatasize_shared_ref_counts_once",
	"cdatasize_negative_bound_rangecheck",
	"cdatasizeq_bound_too_small_false",
	"sdatasize_success_counts_refs",
	"sdatasize_shared_ref_counts_once",
	"sdatasize_negative_bound_rangecheck",
	"sdatasizeq_bound_too_small_false",
}

var requiredTonFuncRuntimeGapCaseNames = []string{
	"getparam_now_blocklt_ltime_aliases",
	"getparamlong_randseed",
	"getparam_missing_params_tuple_range",
	"getparam_params_not_tuple_typecheck",
	"balance_myaddr_configroot",
	"randu256_updates_seed",
	"randu256_bad_seed_typecheck",
	"rand_bounded_updates_seed",
	"rand_zero_bound_updates_seed",
	"rand_negative_bound_updates_seed",
	"setrand_addrand_roundtrip",
	"setrand_max_seed_roundtrip",
	"setrand_negative_rangecheck",
	"setrand_c7_params_not_tuple_typecheck",
	"addrand_negative_rangecheck",
	"setglob_getglob_roundtrip",
	"setglobvar_getglobvar_roundtrip",
	"getglobvar_absent_null",
	"globalid_unpacked_config_short_tuple_range",
	"inmsgparam_short_tuple_range",
	"configparam_negative_key_hit",
	"configoptparam_negative_key_hit",
	"configparam_no_root_false",
	"configoptparam_no_root_null",
	"configparam_bad_root_type",
	"configoptparam_bad_root_type",
	"configparam_out_of_int32_false",
	"configparam_malformed_value_dict_error",
	"configoptparam_malformed_value_dict_error",
	"setcp_zero",
	"setcp_negative_unsupported",
	"setcp_positive_unsupported",
	"setcpx_zero",
	"setcpx_negative_unsupported",
	"setcpx_positive_unsupported",
	"setcpx_high_rangecheck",
}

var requiredHashGapCaseNames = []string{
	"sha256u_empty",
	"hashbu_builder_with_ref",
	"hashext_bit_concat_sha256",
	"hashextr_order_sha256",
	"hashext_dynamic_keccak256",
	"hashext_dynamic_unknown_hash_id",
	"hashext_dynamic_hash_id_rangecheck",
	"hashext_count_rangecheck",
	"hashext_dynamic_missing_count_v8_partial_pop",
	"hashext_dynamic_missing_count_v9_precheck",
	"hashext_sha512_tuple",
	"hashext_blake2b_tuple",
	"hashext_keccak512_tuple",
	"hashexta_keccak256_builder_input",
	"hashexta_append_non_builder_typecheck",
	"hashexta_dynamic_zero_count_builder_only",
	"hashextar_dynamic_sha512_append_reverse",
	"hashext_unaligned_bits_underflow",
	"sha256u_unaligned_bits_underflow",
	"hashexta_builder_overflow",
}

var requiredCryptoGapCaseNames = []string{
	"chksignu_success",
	"chksigns_success",
	"ecrecover_success",
	"ecrecover_invalid_v",
	"secp256k1_xonly_pubkey_tweak_add_success",
	"secp256k1_xonly_pubkey_tweak_add_tweak_ge_n",
	"p256_chksignu_success",
	"p256_chksignu_bad_key",
	"p256_chksigns_success",
	"p256_chksigns_bad_key",
	"chksignu_bad_signature_false",
	"chksigns_bad_signature_false",
	"p256_chksignu_bad_signature_false",
	"p256_chksigns_bad_signature_false",
}

var requiredCirclCryptoGapCaseNames = []string{
	"rist255_fromhash",
	"rist255_pushl",
	"rist255_validate_valid",
	"rist255_qvalidate_invalid",
	"rist255_add_valid",
	"rist255_qadd_invalid",
	"rist255_sub_valid",
	"rist255_qsub_valid",
	"rist255_qsub_invalid",
	"rist255_mul",
	"rist255_qmul_invalid",
	"rist255_mulbase",
	"rist255_qmulbase",
	"bls_pushr",
	"bls_verify_true",
	"bls_verify_invalid_pub_false",
	"bls_aggregate",
	"bls_fastaggregateverify_true",
	"bls_aggregateverify_distinct_msgs_true",
	"bls_g1_zero",
	"bls_g1_add",
	"bls_g1_sub",
	"bls_g1_neg",
	"bls_g1_mul",
	"bls_g1_multiexp_two_terms",
	"bls_g1_ingroup_invalid_false",
	"bls_g1_iszero",
	"bls_map_to_g1",
	"bls_g2_zero",
	"bls_g2_add",
	"bls_g2_sub",
	"bls_g2_neg",
	"bls_g2_mul",
	"bls_g2_multiexp_two_terms",
	"bls_g2_ingroup_invalid_false",
	"bls_g2_iszero",
	"bls_map_to_g2",
	"bls_pairing_false",
	"bls_pairing_invalid",
}

var requiredCryptoEdgeGapCaseNames = []string{
	"chksignu_short_signature_underflow",
	"chksigns_short_signature_underflow",
	"chksigns_unaligned_message_underflow",
	"chksignu_negative_hash_range",
	"chksignu_key_negative_range",
	"ecrecover_negative_hash_range",
	"ecrecover_negative_r_range",
	"ecrecover_v_out_of_uint8_range",
	"ecrecover_zero_signature_false",
	"secp256k1_tweak_negative_range",
	"secp256k1_key_negative_range",
	"secp256k1_invalid_key_false",
	"p256_chksigns_unaligned_message_underflow",
	"p256_chksignu_negative_hash_range",
	"p256_chksignu_short_signature_underflow",
	"p256_chksignu_short_key_underflow",
	"rist255_validate_invalid_range",
	"rist255_add_invalid_unknown",
	"bls_verify_short_pub_underflow",
	"bls_verify_short_sig_underflow",
	"bls_verify_invalid_sig_false",
	"bls_fastaggregateverify_empty_false",
	"bls_aggregateverify_empty_false",
	"bls_aggregateverify_invalid_sig_false",
	"bls_aggregate_invalid_signature_unknown",
	"bls_g1_add_invalid_point_unknown",
	"bls_g2_add_invalid_point_unknown",
	"bls_g1_ingroup_short_underflow",
	"bls_g2_ingroup_short_underflow",
	"bls_g1_multiexp_invalid_point_unknown",
	"bls_g2_multiexp_invalid_point_unknown",
}

var requiredTupleGapCaseNames = []string{
	"pushnull_isnull_true",
	"isnull_false",
	"istuple_true",
	"istuple_false",
	"qtlen_tuple",
	"qtlen_non_tuple",
	"indexq_oob_null",
}

const (
	expectedTupleOpcodeSpaceGapCases  = 243
	expectedTupleDynamicErrorGapCases = 64
)

var requiredTupleOpcodeSpaceGapCaseNames = []string{
	"pushnull",
	"isnull_null",
	"isnull_int",
	"tuple_0",
	"tuple_15",
	"index_0",
	"index_15",
	"untuple_0",
	"untuple_15",
	"unpackfirst_15",
	"explode_15",
	"setindex_15",
	"indexq_15",
	"setindexq_15",
	"tuplevar_4",
	"indexvar_3",
	"untuplevar_4",
	"unpackfirstvar_4",
	"explodevar_4",
	"setindexvar_3",
	"indexvarq_3",
	"setindexvarq_3",
	"tlen",
	"qtlen_tuple",
	"qtlen_non_tuple",
	"istuple_tuple",
	"istuple_non_tuple",
	"last",
	"tpush",
	"tpop",
	"nullop_6fa0_zero",
	"nullop_6fa0_nonzero",
	"nullop_6fa7_zero",
	"nullop_6fa7_nonzero",
	"index2_0_0",
	"index2_3_3",
	"index3_0_0_0",
	"index3_3_3_3",
}

var requiredTupleDynamicErrorGapCaseNames = []string{
	"tuplevar_0",
	"tuplevar_255",
	"indexvar_254",
	"untuplevar_255",
	"unpackfirstvar_255",
	"explodevar_255",
	"setindexvar_254",
	"indexvarq_254",
	"setindexvarq_254_nil_tuple_value",
	"setindexvarq_254_nil_tuple_nil_value",
	"setindexvarq_254_existing_nil_fill",
	"indexq_nil_tuple_null",
	"indexvarq_nil_tuple_null",
	"setindexq_nil_tuple_allocates",
	"setindexq_nil_tuple_nil_stays_null",
	"setindexq_existing_oob_extends",
	"setindexq_existing_oob_nil_preserves",
	"isnull_nan_false",
	"qtlen_nan_minus_one",
	"indexq_tuple_nan_value",
	"untuple_nan_null",
	"indexq_non_tuple",
	"setindexq_non_tuple",
	"index_out_of_range",
	"setindex_out_of_range",
	"tlen_non_tuple_typecheck",
	"untuple_wrong_arity",
	"unpackfirst_too_short",
	"explode_too_large",
	"tpush_empty_tuple_value",
	"tpush_len254_to255",
	"tpush_too_large",
	"tpop_singleton_to_empty",
	"tpop_empty",
	"last_empty",
	"index2_outer_oob_range",
	"index2_final_oob_range",
	"index2_intermediate_non_tuple",
	"index2_intermediate_oversized_tuple",
	"index3_second_level_non_tuple",
	"index3_final_oob_range",
	"nullswapif_non_integer",
	"indexvar_short_stack_preserves_error",
	"untuplevar_short_stack_preserves_error",
	"unpackfirstvar_short_stack_preserves_error",
	"explodevar_short_stack_preserves_error",
	"setindex_short_stack_preserves_error",
	"setindexq_short_stack_consumes_value",
	"setindexvar_short_stack_preserves_error",
	"indexvarq_short_stack_nan_idx",
	"setindexvarq_short_stack_nan_idx",
	"tpush_short_stack_preserves_error",
	"tuplevar_256_range",
	"indexvar_255_range",
	"untuplevar_256_range",
	"unpackfirstvar_256_range",
	"explodevar_256_range",
	"setindexvar_255_range",
	"indexvarq_255_range",
	"setindexvarq_255_range",
	"invalid_6f8e_gap",
	"invalid_6f9f_gap",
	"invalid_6fa8_gap",
	"invalid_6faf_gap",
}

var requiredStackGapCaseNames = []string{
	"xchg0l_depth17",
	"pushl_depth17",
	"popl_depth17",
	"pick_depth4",
	"roll_depth4",
	"rollrev_depth4",
	"dropx_three",
	"xchgx_depth3",
	"revx_3_1",
	"onlytopx_keep3",
	"onlyx_keep3",
	"blkswx_2_2",
	"multi_pu2xc",
	"push3",
	"pushint_small",
	"pushslice_payload",
	"pushrefslice_payload",
	"pushref_payload",
	"pushcont_execute",
	"pushnull",
	"pushnan",
	"dumpstk_noop",
	"dump_s0_noop",
	"debug_noop",
	"debugstr_noop",
	"strdump_slice_noop",
	"condsel_true",
	"condsel_false",
	"condselchk_true",
	"condselchk_mixed_int_slice_typecheck",
}

const (
	expectedStackOpcodeSpaceGapCases  = 39839
	expectedStackDynamicDepthGapCases = 22
)

var requiredStackOpcodeSpaceGapCaseNames = []string{
	"nop",
	"swap",
	"xchg0_short_15",
	"xchg_1_15",
	"xchg0_long_255",
	"xchg1_short_15",
	"dup",
	"over",
	"push_short_15",
	"drop",
	"nip",
	"pop_short_15",
	"xchg3_short_15_15_15",
	"xchg2_15_15",
	"xcpu_15_15",
	"puxc_15_15",
	"push2_15_15",
	"xchg3_ext_15_15_15",
	"xc2pu_15_15_15",
	"xcpuxc_15_15_15",
	"xcpu2_15_15_15",
	"puxc2_15_15_15",
	"puxcpu_15_15_15",
	"pu2xc_15_15_15",
	"push3_15_15_15",
	"blkswap_16_16",
	"push_long_255",
	"pop_long_255",
	"rot",
	"rotrev",
	"2swap",
	"2drop",
	"2dup",
	"2over",
	"reverse_17_15",
	"blkdrop_15",
	"blkpush_15_15",
	"tuck",
	"depth",
	"blkdrop2_15_15",
}

var requiredStackDynamicDepthGapCaseNames = []string{
	"pick_0",
	"pick_255",
	"pick_1020",
	"roll_0",
	"roll_256",
	"rollrev_256",
	"blkswx_empty_left",
	"blkswx_empty_right",
	"blkswx_510_511",
	"revx_empty",
	"revx_510_511",
	"dropx_0",
	"dropx_512",
	"xchgx_0",
	"xchgx_255",
	"xchgx_1021",
	"depth_1021",
	"chkdepth_1022",
	"onlytopx_0",
	"onlytopx_1022",
	"onlyx_0",
	"onlyx_1022",
}

var requiredTupleGapTraceLabels = []string{
	"TUPLEVAR",
	"INDEXVAR",
	"INDEXVARQ",
	"UNTUPLEVAR",
	"UNPACKFIRSTVAR",
	"EXPLODEVAR",
	"INDEX2(0,1)",
	"INDEX3(0,0,1)",
	"SETINDEXQ(1)",
	"SETINDEXVAR",
	"SETINDEXVARQ",
	"NULLSWAPIF",
	"NULLSWAPIFNOT",
	"NULLROTRIF",
	"NULLSWAPIF2",
	"NULLROTRIFNOT",
	"NULLSWAPIFNOT2",
	"NULLROTRIF2",
	"NULLROTRIFNOT2",
}

func requireNamedParityCases(t *testing.T, family string, names []string, required []string) {
	t.Helper()

	seen := map[string]struct{}{}
	for _, name := range names {
		seen[name] = struct{}{}
	}
	for _, name := range required {
		if _, ok := seen[name]; !ok {
			t.Fatalf("required %s parity case %q is missing", family, name)
		}
	}
}

func requireParityTraceLabels(t *testing.T, family string, labels []string, required []string) {
	t.Helper()

	seen := map[string]struct{}{}
	for _, label := range labels {
		seen[label] = struct{}{}
	}
	for _, req := range required {
		if _, ok := seen[req]; !ok {
			t.Fatalf("required %s parity trace label %q is missing", family, req)
		}
	}
}

func TestTVMDifferentialFuzzMathCompoundOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	empty := testEmptyCell()
	var allLabels []string
	for mode := 0; mode < 21; mode++ {
		t.Run(fmt.Sprintf("mode_%02d", mode), func(t *testing.T) {
			g := &parityProgramGenerator{
				r:    rand.New(rand.NewSource(int64(mode))),
				seed: append([]byte(nil), crossTestSeed...),
				regD: [2]*cell.Cell{
					empty,
					empty,
				},
			}
			if !g.emitMathCompoundOp(mode) {
				t.Fatalf("mode %d was not emitted", mode)
			}
			allLabels = append(allLabels, g.trace...)

			runDifferentialFuzzCase(t, differentialFuzzCase{
				seed:   uint64(mode),
				family: "math_compound",
				op:     strings.Join(g.trace, " -> "),
				code:   parityProgramCodeFromBuilders(t, g.ops...),
				c7:     prepareCrossTestC7(nil, empty),
			})
		})
	}
	requireParityTraceLabels(t, "math_compound", allLabels, requiredMathCompoundTraceLabels)
}

func TestTVMDifferentialFuzzMathShiftOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	empty := testEmptyCell()
	var allLabels []string
	for mode := 0; mode < 20; mode++ {
		t.Run(fmt.Sprintf("mode_%02d", mode), func(t *testing.T) {
			g := &parityProgramGenerator{
				r:    rand.New(rand.NewSource(int64(mode))),
				seed: append([]byte(nil), crossTestSeed...),
				regD: [2]*cell.Cell{
					empty,
					empty,
				},
			}
			if !g.emitMathShiftModOp(mode) {
				t.Fatalf("mode %d was not emitted", mode)
			}
			allLabels = append(allLabels, g.trace...)

			runDifferentialFuzzCase(t, differentialFuzzCase{
				seed:   uint64(mode),
				family: "math_shift",
				op:     strings.Join(g.trace, " -> "),
				code:   parityProgramCodeFromBuilders(t, g.ops...),
				c7:     prepareCrossTestC7(nil, empty),
			})
		})
	}
	requireParityTraceLabels(t, "math_shift", allLabels, requiredMathShiftTraceLabels)
}

func TestTVMDifferentialFuzzMathQuietLogicOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	empty := testEmptyCell()
	var allLabels []string
	for mode := 0; mode < 29; mode++ {
		t.Run(fmt.Sprintf("mode_%02d", mode), func(t *testing.T) {
			g := &parityProgramGenerator{
				r:    rand.New(rand.NewSource(int64(mode))),
				seed: append([]byte(nil), crossTestSeed...),
				regD: [2]*cell.Cell{
					empty,
					empty,
				},
			}
			if !g.emitMathQuietLogicOp(mode) {
				t.Fatalf("mode %d was not emitted", mode)
			}
			allLabels = append(allLabels, g.trace...)

			runDifferentialFuzzCase(t, differentialFuzzCase{
				seed:   uint64(mode),
				family: "math_quiet_logic",
				op:     strings.Join(g.trace, " -> "),
				code:   parityProgramCodeFromBuilders(t, g.ops...),
				c7:     prepareCrossTestC7(nil, empty),
			})
		})
	}
	requireParityTraceLabels(t, "math_quiet_logic", allLabels, requiredMathQuietLogicTraceLabels)
}

func TestTVMDifferentialFuzzMathGapOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	empty := testEmptyCell()
	var allLabels []string
	for mode := 0; mode < 36; mode++ {
		t.Run(fmt.Sprintf("mode_%02d", mode), func(t *testing.T) {
			g := &parityProgramGenerator{
				r:    rand.New(rand.NewSource(int64(mode))),
				seed: append([]byte(nil), crossTestSeed...),
				regD: [2]*cell.Cell{
					empty,
					empty,
				},
			}
			if !g.emitMathGapOp(mode) {
				t.Fatalf("mode %d was not emitted", mode)
			}
			allLabels = append(allLabels, g.trace...)

			runDifferentialFuzzCase(t, differentialFuzzCase{
				seed:   uint64(mode),
				family: "math_gap",
				op:     strings.Join(g.trace, " -> "),
				code:   parityProgramCodeFromBuilders(t, g.ops...),
				c7:     prepareCrossTestC7(nil, empty),
			})
		})
	}
	requireParityTraceLabels(t, "math_gap", allLabels, requiredMathGapTraceLabels)
}

func TestTVMDifferentialFuzzMathQuietCompoundOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	empty := testEmptyCell()
	var allLabels []string
	for mode := 0; mode < 15; mode++ {
		t.Run(fmt.Sprintf("mode_%02d", mode), func(t *testing.T) {
			g := &parityProgramGenerator{
				r:    rand.New(rand.NewSource(int64(mode))),
				seed: append([]byte(nil), crossTestSeed...),
				regD: [2]*cell.Cell{
					empty,
					empty,
				},
			}
			if !g.emitMathQuietCompoundOp(mode) {
				t.Fatalf("mode %d was not emitted", mode)
			}
			allLabels = append(allLabels, g.trace...)

			runDifferentialFuzzCase(t, differentialFuzzCase{
				seed:   uint64(mode),
				family: "math_quiet_compound",
				op:     strings.Join(g.trace, " -> "),
				code:   parityProgramCodeFromBuilders(t, g.ops...),
				c7:     prepareCrossTestC7(nil, empty),
			})
		})
	}
	requireParityTraceLabels(t, "math_quiet_compound", allLabels, requiredMathQuietCompoundTraceLabels)
}

func TestTVMDifferentialFuzzMathImmediateGapOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	shift := uint8(3)
	lshiftCodeOp := func(op byte) *cell.Builder {
		return parityProgramRawOp((0xA9<<16)|(uint64(op)<<8)|uint64(shift-1), 24)
	}
	oneArg := []any{big.NewInt(-17)}
	twoArgs := []any{big.NewInt(-7), big.NewInt(5)}
	addArgs := []any{big.NewInt(-17), big.NewInt(2)}
	mulAddArgs := []any{big.NewInt(-7), big.NewInt(5), big.NewInt(2)}
	lshiftDivCodeArgs := []any{big.NewInt(-7), big.NewInt(5)}
	lshiftAddDivModCodeArgs := []any{big.NewInt(-7), big.NewInt(2), big.NewInt(5)}

	cases := []struct {
		name  string
		code  *cell.Cell
		stack []any
	}{
		{"pushpow2", parityProgramCodeCell(mathop.PUSHPOW2(4).Serialize()), nil},
		{"pushpow2_nan_alias", parityProgramCodeCell(mathop.PUSHPOW2(0xff).Serialize()), nil},
		{"pushpow2dec", parityProgramCodeCell(mathop.PUSHPOW2DEC(4).Serialize()), nil},
		{"pushnegpow2", parityProgramCodeCell(mathop.PUSHNEGPOW2(4).Serialize()), nil},
		{"lessint_true", parityProgramCodeCell(mathop.LESSINT(4).Serialize()), []any{big.NewInt(3)}},
		{"eqint_true", parityProgramCodeCell(mathop.EQINT(5).Serialize()), []any{big.NewInt(5)}},
		{"gtint_true", parityProgramCodeCell(mathop.GTINT(3).Serialize()), []any{big.NewInt(8)}},
		{"neqint_false", parityProgramCodeCell(mathop.NEQINT(8).Serialize()), []any{big.NewInt(8)}},
		{"qaddint", parityProgramCodeCell(mathop.QADDINT(4).Serialize()), []any{big.NewInt(9)}},
		{"qmulint", parityProgramCodeCell(mathop.QMULINT(-2).Serialize()), []any{big.NewInt(9)}},
		{"qeqint_true", parityProgramCodeCell(mathop.QEQINT(5).Serialize()), []any{big.NewInt(5)}},
		{"qlessint_true", parityProgramCodeCell(mathop.QLESSINT(4).Serialize()), []any{big.NewInt(3)}},
		{"qgtint_nan", parityProgramCodeCell(mathop.QGTINT(4).Serialize()), []any{vm.NaN{}}},
		{"qneqint_true", parityProgramCodeCell(mathop.QNEQINT(4).Serialize()), []any{big.NewInt(5)}},
		{"fits_immediate", parityProgramCodeCell(mathop.FITS(6).Serialize()), []any{big.NewInt(63)}},
		{"ufits_immediate", parityProgramCodeCell(mathop.UFITS(7).Serialize()), []any{big.NewInt(255)}},
		{"qfits_immediate_nan", parityProgramCodeCell(mathop.QFITS(2).Serialize()), []any{big.NewInt(8)}},
		{"qufits_immediate", parityProgramCodeCell(mathop.QUFITS(7).Serialize()), []any{big.NewInt(255)}},
		{"lshift_code", parityProgramCodeCell(mathop.LSHIFTCODE(int8(shift)).Serialize()), []any{big.NewInt(-3)}},
		{"rshift_code_floor", parityProgramCodeCell(mathop.RSHIFTCODE(int8(shift)).Serialize()), oneArg},
		{"rshiftr_code_round", parityProgramCodeCell(mathop.RSHIFTRCODE(int8(shift)).Serialize()), oneArg},
		{"rshiftc_code_ceil", parityProgramCodeCell(mathop.RSHIFTCCODE(int8(shift)).Serialize()), oneArg},
		{"rshift_code_floor_alt", parityProgramCodeCell(mathop.RSHIFTCODEFLOOR(int8(shift)).Serialize()), oneArg},
		{"lshift_code_nan_current", parityProgramCodeCell(mathop.LSHIFTCODE(int8(shift)).Serialize()), []any{vm.NaN{}}},
		{"rshift_code_nan_current", parityProgramCodeCell(mathop.RSHIFTCODE(int8(shift)).Serialize()), []any{vm.NaN{}}},
		{"qlshift_code", parityProgramCodeCell(mathop.QLSHIFTCODE(int8(shift)).Serialize()), []any{big.NewInt(3)}},
		{"qrshift_code", parityProgramCodeCell(mathop.QRSHIFTCODE(int8(shift)).Serialize()), []any{big.NewInt(48)}},
		{"qlshift_code_nan_current", parityProgramCodeCell(mathop.QLSHIFTCODE(int8(shift)).Serialize()), []any{vm.NaN{}}},
		{"qrshift_code_nan_legacy", parityProgramCodeCell(mathop.QRSHIFTCODE(int8(shift)).Serialize()), []any{vm.NaN{}}},
		{"rshift_code_mod_floor", parityProgramCodeCell(mathop.RSHIFTCODEMOD(int8(shift)).Serialize()), oneArg},
		{"rshiftr_code_mod_round", parityProgramCodeCell(mathop.RSHIFTRCODEMOD(int8(shift)).Serialize()), oneArg},
		{"rshiftc_code_mod_ceil", parityProgramCodeCell(mathop.RSHIFTCCODEMOD(int8(shift)).Serialize()), oneArg},
		{"modpow2_code_floor", parityProgramCodeCell(mathop.MODPOW2CODE(int8(shift)).Serialize()), oneArg},
		{"modpow2r_code_round", parityProgramCodeCell(mathop.MODPOW2RCODE(int8(shift)).Serialize()), oneArg},
		{"modpow2c_code_ceil", parityProgramCodeCell(mathop.MODPOW2CCODE(int8(shift)).Serialize()), oneArg},
		{"mulrshift_code_floor", parityProgramCodeCell(mathop.MULRSHIFTCODE(int8(shift)).Serialize()), twoArgs},
		{"mulrshiftr_code_round", parityProgramCodeCell(mathop.MULRSHIFTRCODE(int8(shift)).Serialize()), twoArgs},
		{"mulrshiftc_code_ceil", parityProgramCodeCell(mathop.MULRSHIFTCCODE(int8(shift)).Serialize()), twoArgs},
		{"mulrshift_code_mod_floor", parityProgramCodeCell(mathop.MULRSHIFTCODEMOD(int8(shift)).Serialize()), twoArgs},
		{"mulrshiftr_code_mod_round", parityProgramCodeCell(mathop.MULRSHIFTRCODEMOD(int8(shift)).Serialize()), twoArgs},
		{"mulrshiftc_code_mod_ceil", parityProgramCodeCell(mathop.MULRSHIFTCCODEMOD(int8(shift)).Serialize()), twoArgs},
		{"mulmodpow2_code_floor", parityProgramCodeCell(mathop.MULMODPOW2CODE(int8(shift)).Serialize()), twoArgs},
		{"mulmodpow2r_code_round", parityProgramCodeCell(mathop.MULMODPOW2RCODE(int8(shift)).Serialize()), twoArgs},
		{"mulmodpow2c_code_ceil", parityProgramCodeCell(mathop.MULMODPOW2CCODE(int8(shift)).Serialize()), twoArgs},
		{"addrshift_code_mod_floor", parityProgramCodeCell(mathop.ADDRSHIFTCODEMOD(int8(shift)).Serialize()), addArgs},
		{"addrshiftr_code_mod_round", parityProgramCodeCell(mathop.ADDRSHIFTRCODEMOD(int8(shift)).Serialize()), addArgs},
		{"addrshiftc_code_mod_ceil", parityProgramCodeCell(mathop.ADDRSHIFTCCODEMOD(int8(shift)).Serialize()), addArgs},
		{"muladdrshift_code_mod_floor", parityProgramCodeCell(mathop.MULADDRSHIFTCODEMOD(int8(shift)).Serialize()), mulAddArgs},
		{"muladdrshiftr_code_mod_round", parityProgramCodeCell(mathop.MULADDRSHIFTRCODEMOD(int8(shift)).Serialize()), mulAddArgs},
		{"muladdrshiftc_code_mod_ceil", parityProgramCodeCell(mathop.MULADDRSHIFTCCODEMOD(int8(shift)).Serialize()), mulAddArgs},
		{"lshiftadddivmod_code_floor", parityProgramCodeCell(lshiftCodeOp(0xD0)), lshiftAddDivModCodeArgs},
		{"lshiftadddivmodr_code_round", parityProgramCodeCell(lshiftCodeOp(0xD1)), lshiftAddDivModCodeArgs},
		{"lshiftadddivmodc_code_ceil", parityProgramCodeCell(lshiftCodeOp(0xD2)), lshiftAddDivModCodeArgs},
		{"lshiftdiv_code_floor", parityProgramCodeCell(lshiftCodeOp(0xD4)), lshiftDivCodeArgs},
		{"lshiftdivr_code_round", parityProgramCodeCell(lshiftCodeOp(0xD5)), lshiftDivCodeArgs},
		{"lshiftdivc_code_ceil", parityProgramCodeCell(lshiftCodeOp(0xD6)), lshiftDivCodeArgs},
		{"lshiftmod_code_floor", parityProgramCodeCell(lshiftCodeOp(0xD8)), lshiftDivCodeArgs},
		{"lshiftmodr_code_round", parityProgramCodeCell(lshiftCodeOp(0xD9)), lshiftDivCodeArgs},
		{"lshiftmodc_code_ceil", parityProgramCodeCell(lshiftCodeOp(0xDA)), lshiftDivCodeArgs},
		{"lshiftdivmod_code_floor", parityProgramCodeCell(lshiftCodeOp(0xDC)), lshiftDivCodeArgs},
		{"lshiftdivmodr_code_round", parityProgramCodeCell(lshiftCodeOp(0xDD)), lshiftDivCodeArgs},
		{"lshiftdivmodc_code_ceil", parityProgramCodeCell(lshiftCodeOp(0xDE)), lshiftDivCodeArgs},
	}

	caseNames := make([]string, 0, len(cases))
	for _, tc := range cases {
		caseNames = append(caseNames, tc.name)
	}
	requireNamedParityCases(t, "math_immediate_gap", caseNames, requiredMathImmediateGapCaseNames)

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			globalVersion := 0
			switch tc.name {
			case "lshift_code_nan_current", "qlshift_code_nan_current":
				globalVersion = 14
			}

			runDifferentialFuzzCase(t, differentialFuzzCase{
				seed:             uint64(i),
				family:           "math_immediate_gap",
				op:               tc.name,
				code:             tc.code,
				stack:            tc.stack,
				globalVersion:    globalVersion,
				globalVersionSet: globalVersion != 0,
				refCfg:           differentialFuzzOptionalVersionRefConfig(t, globalVersion),
			})
		})
	}
}

func TestTVMDifferentialFuzzInvalidMathOpcodeGapOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	cases := []struct {
		name string
		raw  uint64
		bits uint
	}{
		{"divmod_invalid", 0xA903, 16},
		{"shrmod_invalid", 0xA927, 16},
		{"shrcodemod_invalid", 0xA93300, 24},
		{"muldivmod_invalid", 0xA987, 16},
		{"mulshrmod_invalid", 0xA9A7, 16},
		{"mulshrcodemod_invalid", 0xA9B300, 24},
		{"shldivmod_invalid", 0xA9C7, 16},
		{"shldivcodemod_invalid", 0xA9D300, 24},
	}

	caseNames := make([]string, 0, len(cases))
	for _, tc := range cases {
		caseNames = append(caseNames, tc.name)
	}
	requireNamedParityCases(t, "invalid_math_gap", caseNames, requiredInvalidMathGapCaseNames)

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runDifferentialFuzzCase(t, differentialFuzzCase{
				seed:   uint64(i),
				family: "invalid_math_gap",
				op:     tc.name,
				code:   parityProgramCodeCell(parityProgramRawOp(tc.raw, tc.bits)),
				c7:     prepareCrossTestC7(nil, testEmptyCell()),
			})
		})
	}
}

func TestTVMDifferentialFuzzQuietMathErrorGapOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	quietCompoundOp := func(prefix uint64, args uint8) *cell.Cell {
		return parityProgramCodeCell(parityProgramRawOp((prefix<<4)|uint64(args), 24))
	}

	cases := []struct {
		name  string
		code  *cell.Cell
		stack []any
	}{
		{
			name:  "qdiv_zero_divisor_nan",
			code:  quietCompoundOp(0xB7A90, 4),
			stack: []any{big.NewInt(-17), big.NewInt(0)},
		},
		{
			name:  "qmod_zero_divisor_nan",
			code:  quietCompoundOp(0xB7A90, 8),
			stack: []any{big.NewInt(-17), big.NewInt(0)},
		},
		{
			name:  "qadddivmod_zero_divisor_double_nan",
			code:  quietCompoundOp(0xB7A90, 0),
			stack: []any{big.NewInt(-17), big.NewInt(2), big.NewInt(0)},
		},
		{
			name:  "qadddivmod_nan_addend_double_nan",
			code:  quietCompoundOp(0xB7A90, 0),
			stack: []any{big.NewInt(-17), vm.NaN{}, big.NewInt(5)},
		},
		{
			name:  "qmod_nan_operand_nan",
			code:  quietCompoundOp(0xB7A90, 8),
			stack: []any{vm.NaN{}, big.NewInt(5)},
		},
		{
			name:  "qaddrshiftmod_shift_over_256_legacy_rangecheck",
			code:  quietCompoundOp(0xB7A92, 0),
			stack: []any{big.NewInt(-17), big.NewInt(2), big.NewInt(257)},
		},
		{
			name:  "qaddrshiftmod_nan_shift_legacy_rangecheck",
			code:  quietCompoundOp(0xB7A92, 0),
			stack: []any{big.NewInt(-17), big.NewInt(2), vm.NaN{}},
		},
		{
			name:  "qmuldiv_zero_divisor_nan",
			code:  quietCompoundOp(0xB7A98, 4),
			stack: []any{big.NewInt(-7), big.NewInt(5), big.NewInt(0)},
		},
		{
			name:  "qmuldiv_divisor_slice_typecheck",
			code:  quietCompoundOp(0xB7A98, 4),
			stack: []any{big.NewInt(-7), big.NewInt(5), cell.BeginCell().EndCell().MustBeginParse()},
		},
		{
			name:  "qmulmodr_nan_factor_nan",
			code:  quietCompoundOp(0xB7A98, 9),
			stack: []any{vm.NaN{}, big.NewInt(5), big.NewInt(4)},
		},
		{
			name:  "qmuladddivmod_zero_divisor_double_nan",
			code:  quietCompoundOp(0xB7A98, 0),
			stack: []any{big.NewInt(-7), big.NewInt(5), big.NewInt(2), big.NewInt(0)},
		},
		{
			name:  "qmuladddivmod_nan_addend_double_nan",
			code:  quietCompoundOp(0xB7A98, 0),
			stack: []any{big.NewInt(-7), big.NewInt(5), vm.NaN{}, big.NewInt(4)},
		},
		{
			name:  "qmuladdrshiftmod_negative_shift_double_nan",
			code:  quietCompoundOp(0xB7A9A, 0),
			stack: []any{big.NewInt(-7), big.NewInt(5), big.NewInt(2), big.NewInt(-1)},
		},
		{
			name:  "qmuladdrshiftmod_nan_addend_double_nan",
			code:  quietCompoundOp(0xB7A9A, 0),
			stack: []any{big.NewInt(-7), big.NewInt(5), vm.NaN{}, big.NewInt(2)},
		},
		{
			name:  "qmulrshift_shift_over_256_nan",
			code:  quietCompoundOp(0xB7A9A, 4),
			stack: []any{big.NewInt(-7), big.NewInt(5), big.NewInt(257)},
		},
		{
			name:  "qlshiftdiv_zero_divisor_nan",
			code:  quietCompoundOp(0xB7A9C, 4),
			stack: []any{big.NewInt(-17), big.NewInt(0), big.NewInt(2)},
		},
		{
			name:  "qlshiftadddivmod_nan_addend_double_nan",
			code:  quietCompoundOp(0xB7A9C, 0),
			stack: []any{big.NewInt(-17), vm.NaN{}, big.NewInt(5), big.NewInt(2)},
		},
		{
			name:  "qlshiftadddivmod_negative_shift_double_nan",
			code:  quietCompoundOp(0xB7A9C, 0),
			stack: []any{big.NewInt(-17), big.NewInt(2), big.NewInt(5), big.NewInt(-1)},
		},
		{
			name:  "qlshiftmod_shift_over_256_nan",
			code:  quietCompoundOp(0xB7A9C, 8),
			stack: []any{big.NewInt(-17), big.NewInt(5), big.NewInt(257)},
		},
		{
			name:  "qlshift_negative_shift_nan",
			code:  parityProgramCodeCell(mathop.QLSHIFT().Serialize()),
			stack: []any{big.NewInt(3), big.NewInt(-1)},
		},
		{
			name:  "qlshift_count_over_1023_nan",
			code:  parityProgramCodeCell(mathop.QLSHIFT().Serialize()),
			stack: []any{big.NewInt(3), big.NewInt(1024)},
		},
		{
			name:  "qpow2_out_of_range_nan",
			code:  parityProgramCodeCell(mathop.QPOW2().Serialize()),
			stack: []any{big.NewInt(1024)},
		},
	}

	caseNames := make([]string, 0, len(cases))
	for _, tc := range cases {
		caseNames = append(caseNames, tc.name)
	}
	requireNamedParityCases(t, "quiet_math_error_gap", caseNames, requiredQuietMathErrorGapCaseNames)

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runDifferentialFuzzCase(t, differentialFuzzCase{
				seed:   uint64(i),
				family: "quiet_math_error_gap",
				op:     tc.name,
				code:   tc.code,
				stack:  tc.stack,
				c7:     prepareCrossTestC7(nil, testEmptyCell()),
			})
		})
	}
}

func TestTVMDifferentialFuzzDictGapOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	empty := testEmptyCell()
	allLabels := make([]string, 0, 128)
	for mode := 0; mode < 63; mode++ {
		t.Run(fmt.Sprintf("mode_%02d", mode), func(t *testing.T) {
			g := &parityProgramGenerator{
				r:    rand.New(rand.NewSource(int64(mode))),
				seed: append([]byte(nil), crossTestSeed...),
				regD: [2]*cell.Cell{
					empty,
					empty,
				},
			}
			if !g.emitDictGapOp(mode) {
				t.Fatalf("mode %d was not emitted", mode)
			}
			allLabels = append(allLabels, g.trace...)

			runDifferentialFuzzCase(t, differentialFuzzCase{
				seed:   uint64(mode),
				family: "dict_gap",
				op:     strings.Join(g.trace, " -> "),
				code:   parityProgramCodeFromBuilders(t, g.ops...),
				c7:     prepareCrossTestC7(nil, empty),
			})
		})
	}
	requireParityTraceLabels(t, "dict_gap", allLabels, requiredDictGapTraceLabels)
}

func TestTVMDifferentialFuzzDictNearGapOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	empty := testEmptyCell()
	allLabels := make([]string, 0, 64)
	for mode := 0; mode < 12; mode++ {
		t.Run(fmt.Sprintf("mode_%02d", mode), func(t *testing.T) {
			g := &parityProgramGenerator{
				r:    rand.New(rand.NewSource(int64(mode))),
				seed: append([]byte(nil), crossTestSeed...),
				regD: [2]*cell.Cell{
					empty,
					empty,
				},
			}
			if !g.emitDictNearExtraOp(mode) {
				t.Fatalf("mode %d was not emitted", mode)
			}
			allLabels = append(allLabels, g.trace...)

			runDifferentialFuzzCase(t, differentialFuzzCase{
				seed:   uint64(mode),
				family: "dict_near_gap",
				op:     strings.Join(g.trace, " -> "),
				code:   parityProgramCodeFromBuilders(t, g.ops...),
				c7:     prepareCrossTestC7(nil, empty),
			})
		})
	}
	requireParityTraceLabels(t, "dict_near_gap", allLabels, requiredDictNearGapTraceLabels)
}

func TestTVMDifferentialFuzzDictSuccessGapOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	cases := []struct {
		name string
		emit func(*parityProgramGenerator) bool
	}{
		{"dictugetoptref_hit", func(g *parityProgramGenerator) bool { return g.emitDictGetOptRefOp(2) }},
		{"dictusetgetoptref_replace_hit", func(g *parityProgramGenerator) bool { return g.emitDictSetGetOptRefOp(false, 2) }},
		{"dictusetgetoptref_delete_hit", func(g *parityProgramGenerator) bool { return g.emitDictSetGetOptRefOp(true, 2) }},
		{"dictmin_slice_hit", func(g *parityProgramGenerator) bool { return g.emitDictMinMaxOp(false, false, 0) }},
		{"dictumaxref_hit", func(g *parityProgramGenerator) bool { return g.emitDictMinMaxOp(true, true, 2) }},
		{"dicturemmaxref_hit", func(g *parityProgramGenerator) bool { return g.emitDictRemMinMaxOp(true, true, 2) }},
		{"dictusubdictget_success", func(g *parityProgramGenerator) bool { return g.emitSubdictOp(false, 2) }},
		{"dictusubdictrpget_success", func(g *parityProgramGenerator) bool { return g.emitSubdictOp(true, 2) }},
		{"pfxdictget_success", func(g *parityProgramGenerator) bool { return g.emitPrefixDictGetOp(false) }},
		{"pfxdictgetq_success", func(g *parityProgramGenerator) bool { return g.emitPrefixDictGetOp(true) }},
		{"pfxdictreplace_hit", func(g *parityProgramGenerator) bool { return g.emitPrefixDictReplaceAddOp(false) }},
		{"pfxdictadd_new", func(g *parityProgramGenerator) bool { return g.emitPrefixDictReplaceAddOp(true) }},
		{"pfxdictset_new", func(g *parityProgramGenerator) bool { return g.emitPrefixDictSetDelOp(false) }},
		{"pfxdictdel_hit", func(g *parityProgramGenerator) bool { return g.emitPrefixDictSetDelOp(true) }},
	}

	caseNames := make([]string, 0, len(cases))
	for _, tc := range cases {
		caseNames = append(caseNames, tc.name)
	}
	requireNamedParityCases(t, "dict_success_gap", caseNames, requiredDictSuccessGapCaseNames)

	empty := testEmptyCell()
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := &parityProgramGenerator{
				r:    rand.New(rand.NewSource(int64(i))),
				seed: append([]byte(nil), crossTestSeed...),
				regD: [2]*cell.Cell{
					empty,
					empty,
				},
			}
			if !tc.emit(g) {
				t.Fatalf("%s was not emitted", tc.name)
			}

			runDifferentialFuzzCase(t, differentialFuzzCase{
				seed:   uint64(i),
				family: "dict_success_gap",
				op:     strings.Join(g.trace, " -> "),
				code:   parityProgramCodeFromBuilders(t, g.ops...),
				c7:     prepareCrossTestC7(nil, empty),
			})
		})
	}
}

func TestTVMDifferentialFuzzDictMissGapOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	plainRoot, _, _ := parityProgramDictRoot(false)
	refRoot, _, _ := parityProgramDictRoot(true)
	multiRoot, _ := parityProgramMultiDictRoot()
	signedRoot, _ := parityProgramSignedDictRoot()
	prefixRoot, _ := parityProgramPrefixDictRoot()
	subdictRoot := parityProgramSubdictRoot()

	valueSlice := func(value uint64, bits uint) *cell.Slice {
		return cell.BeginCell().MustStoreUInt(value, bits).EndCell().MustBeginParse()
	}
	valueBuilder := func(value uint64, bits uint) *cell.Builder {
		return cell.BeginCell().MustStoreUInt(value, bits)
	}
	refValue := func(value uint64, bits uint) *cell.Cell {
		return cell.BeginCell().MustStoreUInt(value, bits).EndCell()
	}
	code := func(raw uint64) *cell.Cell {
		return parityProgramCodeCell(parityProgramRawOp(raw, 16))
	}

	cases := []struct {
		name  string
		code  *cell.Cell
		stack []any
	}{
		{
			name:  "dictget_miss_slice",
			code:  code(0xF40A),
			stack: []any{parityProgramKeySlice(0x99, 8), plainRoot, int64(8)},
		},
		{
			name:  "dictuget_miss_invalid_key",
			code:  code(parityProgramDictValueOpcode(0xF40A, 2, false)),
			stack: []any{big.NewInt(-1), plainRoot, int64(8)},
		},
		{
			name:  "dictugetref_miss",
			code:  code(parityProgramDictValueOpcode(0xF40A, 2, true)),
			stack: []any{int64(0x99), refRoot, int64(8)},
		},
		{
			name:  "dictiget_below_key_miss",
			code:  code(parityProgramDictValueOpcode(0xF40A, 1, false)),
			stack: []any{big.NewInt(-129), signedRoot, int64(8)},
		},
		{
			name:  "dictigetref_overflow_key_miss",
			code:  code(parityProgramDictValueOpcode(0xF40A, 1, true)),
			stack: []any{big.NewInt(128), refRoot, int64(8)},
		},
		{
			name:  "dictigetref_below_key_miss",
			code:  code(parityProgramDictValueOpcode(0xF40A, 1, true)),
			stack: []any{big.NewInt(-129), refRoot, int64(8)},
		},
		{
			name:  "dictureplace_miss_false",
			code:  code(parityProgramDictValueOpcode(0xF422, 2, false)),
			stack: []any{valueSlice(0x55, 8), int64(0x33), (*cell.Cell)(nil), int64(8)},
		},
		{
			name:  "dictuadd_existing_false",
			code:  code(parityProgramDictValueOpcode(0xF432, 2, false)),
			stack: []any{valueSlice(0x99, 8), int64(0x12), plainRoot, int64(8)},
		},
		{
			name:  "dictureplaceget_miss_false",
			code:  code(parityProgramDictValueOpcode(0xF42A, 2, false)),
			stack: []any{valueSlice(0x66, 8), int64(0x44), (*cell.Cell)(nil), int64(8)},
		},
		{
			name:  "dictaddget_existing_returns_old",
			code:  code(parityProgramDictValueOpcode(0xF43A, 0, false)),
			stack: []any{valueSlice(0x77, 8), parityProgramKeySlice(0x12, 8), plainRoot, int64(8)},
		},
		{
			name:  "dictuaddgetref_existing_returns_old",
			code:  code(parityProgramDictValueOpcode(0xF43A, 2, true)),
			stack: []any{refValue(0xBB, 8), int64(0x12), refRoot, int64(8)},
		},
		{
			name:  "dictudel_miss_false",
			code:  code(parityProgramDictScalarOpcode(0xF459, 2)),
			stack: []any{int64(0x77), plainRoot, int64(8)},
		},
		{
			name:  "dictudelgetref_miss_false",
			code:  code(parityProgramDictValueOpcode(0xF462, 2, true)),
			stack: []any{int64(0x77), refRoot, int64(8)},
		},
		{
			name:  "dictudel_invalid_key_rangecheck",
			code:  code(parityProgramDictScalarOpcode(0xF459, 2)),
			stack: []any{big.NewInt(-1), plainRoot, int64(8)},
		},
		{
			name:  "dictudelget_invalid_key_rangecheck",
			code:  code(parityProgramDictValueOpcode(0xF462, 2, false)),
			stack: []any{big.NewInt(-1), plainRoot, int64(8)},
		},
		{
			name:  "dictidel_below_key_rangecheck",
			code:  code(parityProgramDictScalarOpcode(0xF459, 1)),
			stack: []any{big.NewInt(-129), plainRoot, int64(8)},
		},
		{
			name:  "dictidelget_invalid_key_rangecheck",
			code:  code(parityProgramDictValueOpcode(0xF462, 1, false)),
			stack: []any{big.NewInt(128), plainRoot, int64(8)},
		},
		{
			name:  "dictidelgetref_invalid_key_rangecheck",
			code:  code(parityProgramDictValueOpcode(0xF462, 1, true)),
			stack: []any{big.NewInt(128), refRoot, int64(8)},
		},
		{
			name:  "dictidelget_below_key_rangecheck",
			code:  code(parityProgramDictValueOpcode(0xF462, 1, false)),
			stack: []any{big.NewInt(-129), plainRoot, int64(8)},
		},
		{
			name:  "dictidelgetref_below_key_rangecheck",
			code:  code(parityProgramDictValueOpcode(0xF462, 1, true)),
			stack: []any{big.NewInt(-129), refRoot, int64(8)},
		},
		{
			name:  "dictugetoptref_miss_null",
			code:  code(parityProgramDictScalarOpcode(0xF469, 2)),
			stack: []any{int64(0x77), refRoot, int64(8)},
		},
		{
			name:  "dictugetoptref_invalid_key_null",
			code:  code(parityProgramDictScalarOpcode(0xF469, 2)),
			stack: []any{big.NewInt(-1), refRoot, int64(8)},
		},
		{
			name:  "dictigetoptref_overflow_key_null",
			code:  code(parityProgramDictScalarOpcode(0xF469, 1)),
			stack: []any{big.NewInt(128), refRoot, int64(8)},
		},
		{
			name:  "dictigetoptref_below_key_null",
			code:  code(parityProgramDictScalarOpcode(0xF469, 1)),
			stack: []any{big.NewInt(-129), refRoot, int64(8)},
		},
		{
			name:  "dictusetgetoptref_delete_miss_null",
			code:  code(parityProgramDictScalarOpcode(0xF46D, 2)),
			stack: []any{(*cell.Cell)(nil), int64(0x77), refRoot, int64(8)},
		},
		{
			name:  "dictisetgetoptref_overflow_key_rangecheck",
			code:  code(parityProgramDictScalarOpcode(0xF46D, 1)),
			stack: []any{testEmptyCell(), big.NewInt(128), refRoot, int64(8)},
		},
		{
			name:  "dictisetgetoptref_below_key_rangecheck",
			code:  code(parityProgramDictScalarOpcode(0xF46D, 1)),
			stack: []any{testEmptyCell(), big.NewInt(-129), refRoot, int64(8)},
		},
		{
			name:  "dictusetgetoptref_overflow_key_rangecheck",
			code:  code(parityProgramDictScalarOpcode(0xF46D, 2)),
			stack: []any{testEmptyCell(), big.NewInt(256), refRoot, int64(8)},
		},
		{
			name:  "dictusetgetoptref_delete_overflow_key_rangecheck",
			code:  code(parityProgramDictScalarOpcode(0xF46D, 2)),
			stack: []any{(*cell.Cell)(nil), big.NewInt(256), refRoot, int64(8)},
		},
		{
			name:  "dicturemminref_key_len_rangecheck",
			code:  code(parityProgramDictValueOpcode(0xF492, 2, true)),
			stack: []any{refRoot, int64(257)},
		},
		{
			name:  "dictiremmaxref_key_len_rangecheck",
			code:  code(parityProgramDictValueOpcode(0xF49A, 1, true)),
			stack: []any{refRoot, int64(258)},
		},
		{
			name:  "dictumin_empty_false",
			code:  code(parityProgramDictValueOpcode(0xF482, 2, false)),
			stack: []any{(*cell.Cell)(nil), int64(8)},
		},
		{
			name:  "dicturemmin_empty_false",
			code:  code(parityProgramDictValueOpcode(0xF492, 2, false)),
			stack: []any{(*cell.Cell)(nil), int64(8)},
		},
		{
			name:  "dictgetprev_miss_false",
			code:  code(0xF476),
			stack: []any{parityProgramKeySlice(0x05, 8), plainRoot, int64(8)},
		},
		{
			name:  "dictugetprev_overflow_max",
			code:  code(0xF47E),
			stack: []any{big.NewInt(0x1FF), multiRoot, int64(8)},
		},
		{
			name:  "dictugetnext_negative_returns_min",
			code:  code(0xF47C),
			stack: []any{big.NewInt(-1), multiRoot, int64(8)},
		},
		{
			name:  "dictugetnext_overflow_false",
			code:  code(0xF47C),
			stack: []any{big.NewInt(0x1FF), multiRoot, int64(8)},
		},
		{
			name:  "dictigetprev_miss_false",
			code:  code(0xF47A),
			stack: []any{big.NewInt(-3), signedRoot, int64(8)},
		},
		{
			name:  "dictigetnext_below_min_returns_min",
			code:  code(0xF478),
			stack: []any{big.NewInt(-200), signedRoot, int64(8)},
		},
		{
			name:  "dictgetnext_key_len_rangecheck",
			code:  code(0xF474),
			stack: []any{parityProgramKeySlice(0x10, 8), plainRoot, int64(1024)},
		},
		{
			name:  "dictgetprev_key_len_rangecheck",
			code:  code(0xF476),
			stack: []any{parityProgramKeySlice(0x80, 8), plainRoot, int64(1024)},
		},
		{
			name:  "dictigetnext_key_len_rangecheck",
			code:  code(0xF478),
			stack: []any{big.NewInt(1), signedRoot, int64(258)},
		},
		{
			name:  "dictigetprev_key_len_rangecheck",
			code:  code(0xF47A),
			stack: []any{big.NewInt(1), signedRoot, int64(258)},
		},
		{
			name:  "dictugetnext_key_len_rangecheck",
			code:  code(0xF47C),
			stack: []any{big.NewInt(1), plainRoot, int64(257)},
		},
		{
			name:  "dictugetprev_key_len_rangecheck",
			code:  code(0xF47E),
			stack: []any{big.NewInt(1), plainRoot, int64(257)},
		},
		{
			name:  "pfxdictgetq_miss_false",
			code:  code(0xF4A8),
			stack: []any{parityProgramKeySlice(0b0111, 4), prefixRoot, int64(4)},
		},
		{
			name:  "pfxdictgetq_nil_root_false",
			code:  code(0xF4A8),
			stack: []any{parityProgramKeySlice(0b1011, 4), (*cell.Cell)(nil), int64(4)},
		},
		{
			name:  "pfxdictget_miss_cell_underflow",
			code:  code(0xF4A9),
			stack: []any{parityProgramKeySlice(0b0111, 4), prefixRoot, int64(4)},
		},
		{
			name:  "pfxdictget_nil_root_cell_underflow",
			code:  code(0xF4A9),
			stack: []any{parityProgramKeySlice(0b1011, 4), (*cell.Cell)(nil), int64(4)},
		},
		{
			name:  "pfxdictgetjmp_miss_keeps_input",
			code:  code(0xF4AA),
			stack: []any{parityProgramKeySlice(0b0111, 4), prefixRoot, int64(4)},
		},
		{
			name:  "pfxdictgetjmp_nil_root_keeps_input",
			code:  code(0xF4AA),
			stack: []any{parityProgramKeySlice(0b1011, 4), (*cell.Cell)(nil), int64(4)},
		},
		{
			name:  "pfxdictreplace_miss_false",
			code:  code(0xF471),
			stack: []any{valueSlice(0xE, 4), parityProgramKeySlice(0b11, 2), (*cell.Cell)(nil), int64(4)},
		},
		{
			name:  "pfxdictadd_existing_false",
			code:  code(0xF472),
			stack: []any{valueSlice(0xE, 4), parityProgramKeySlice(0b10, 2), prefixRoot, int64(4)},
		},
		{
			name:  "pfxdictdel_miss_false",
			code:  code(0xF473),
			stack: []any{parityProgramKeySlice(0b01, 2), prefixRoot, int64(4)},
		},
		{
			name:  "pfxdictset_oversized_key_false",
			code:  code(0xF470),
			stack: []any{valueSlice(0xE, 4), parityProgramKeySlice(0b11, 2), prefixRoot, int64(1)},
		},
		{
			name:  "pfxdictset_oversized_key_nil_root_false",
			code:  code(0xF470),
			stack: []any{valueSlice(0xE, 4), parityProgramKeySlice(0b11, 2), (*cell.Cell)(nil), int64(1)},
		},
		{
			name:  "pfxdictreplace_oversized_key_false",
			code:  code(0xF471),
			stack: []any{valueSlice(0xE, 4), parityProgramKeySlice(0b11, 2), prefixRoot, int64(1)},
		},
		{
			name:  "pfxdictreplace_oversized_key_nil_root_false",
			code:  code(0xF471),
			stack: []any{valueSlice(0xE, 4), parityProgramKeySlice(0b11, 2), (*cell.Cell)(nil), int64(1)},
		},
		{
			name:  "pfxdictadd_oversized_key_false",
			code:  code(0xF472),
			stack: []any{valueSlice(0xE, 4), parityProgramKeySlice(0b11, 2), prefixRoot, int64(1)},
		},
		{
			name:  "pfxdictadd_oversized_key_nil_root_false",
			code:  code(0xF472),
			stack: []any{valueSlice(0xE, 4), parityProgramKeySlice(0b11, 2), (*cell.Cell)(nil), int64(1)},
		},
		{
			name:  "pfxdictdel_oversized_key_false",
			code:  code(0xF473),
			stack: []any{parityProgramKeySlice(0b11, 2), prefixRoot, int64(1)},
		},
		{
			name:  "pfxdictdel_oversized_key_nil_root_false",
			code:  code(0xF473),
			stack: []any{parityProgramKeySlice(0b11, 2), (*cell.Cell)(nil), int64(1)},
		},
		{
			name:  "subdictuget_miss_dict_error",
			code:  code(parityProgramDictScalarOpcode(0xF4B1, 2)),
			stack: []any{int64(0b111), int64(3), subdictRoot, int64(8)},
		},
		{
			name:  "dictureplaceb_miss_false",
			code:  code(parityProgramDictScalarOpcode(0xF449, 2)),
			stack: []any{valueBuilder(0x77, 8), int64(0x44), (*cell.Cell)(nil), int64(8)},
		},
		{
			name:  "dictuaddb_existing_false",
			code:  code(parityProgramDictScalarOpcode(0xF451, 2)),
			stack: []any{valueBuilder(0x88, 8), int64(0x12), plainRoot, int64(8)},
		},
		{
			name:  "dictuaddgetb_existing_returns_old",
			code:  code(parityProgramDictScalarOpcode(0xF455, 2)),
			stack: []any{valueBuilder(0x99, 8), int64(0x12), plainRoot, int64(8)},
		},
	}

	caseNames := make([]string, 0, len(cases))
	for _, tc := range cases {
		caseNames = append(caseNames, tc.name)
	}
	requireNamedParityCases(t, "dict_miss_gap", caseNames, requiredDictMissGapCaseNames)

	empty := testEmptyCell()
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runDifferentialFuzzCase(t, differentialFuzzCase{
				seed:   uint64(i),
				family: "dict_miss_gap",
				op:     tc.name,
				code:   tc.code,
				stack:  tc.stack,
				c7:     prepareCrossTestC7(nil, empty),
			})
		})
	}
}

func TestTVMDifferentialFuzzDictEdgeGapOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	plainRoot, _, _ := parityProgramDictRoot(false)
	refRoot, _, _ := parityProgramDictRoot(true)
	prefixRoot, _ := parityProgramPrefixDictRoot()
	subdictRoot := parityProgramSubdictRoot()
	valueSlice := func(value uint64, bits uint) *cell.Slice {
		return cell.BeginCell().MustStoreUInt(value, bits).EndCell().MustBeginParse()
	}
	prefixInputWithRef := cell.BeginCell().
		MustStoreUInt(0b1011, 4).
		MustStoreRef(testEmptyCell()).
		EndCell().
		MustBeginParse()
	code := func(raw uint64) *cell.Cell {
		return parityProgramCodeCell(parityProgramRawOp(raw, 16))
	}

	cases := []struct {
		name  string
		code  *cell.Cell
		stack []any
	}{
		{
			name:  "dictuset_negative_key_bad_value_typecheck_order",
			code:  code(parityProgramDictValueOpcode(0xF412, 2, false)),
			stack: []any{int64(1), big.NewInt(-1), (*cell.Cell)(nil), int64(8)},
		},
		{
			name:  "dictusetb_negative_key_bad_builder_typecheck_order",
			code:  code(parityProgramDictScalarOpcode(0xF441, 2)),
			stack: []any{int64(1), big.NewInt(-1), (*cell.Cell)(nil), int64(8)},
		},
		{
			name:  "dictusetgetoptref_negative_key_bad_value_typecheck_order",
			code:  code(parityProgramDictScalarOpcode(0xF46D, 2)),
			stack: []any{int64(1), big.NewInt(-1), refRoot, int64(8)},
		},
		{
			name:  "dictsetgetoptref_short_key_deferred_value_pop",
			code:  code(parityProgramDictScalarOpcode(0xF46D, 0)),
			stack: []any{testEmptyCell(), parityProgramKeySlice(0x1, 4), refRoot, int64(8)},
		},
		{
			name:  "dictsetgetoptref_delete_short_key_deferred_value_pop",
			code:  code(parityProgramDictScalarOpcode(0xF46D, 0)),
			stack: []any{(*cell.Cell)(nil), parityProgramKeySlice(0x1, 4), refRoot, int64(8)},
		},
		{
			name:  "dictsetgetoptref_update_plain_value_dict_error",
			code:  code(parityProgramDictScalarOpcode(0xF46D, 0)),
			stack: []any{testEmptyCell(), parityProgramKeySlice(0x12, 8), plainRoot, int64(8)},
		},
		{
			name:  "dictsetgetoptref_delete_plain_value_dict_error",
			code:  code(parityProgramDictScalarOpcode(0xF46D, 0)),
			stack: []any{(*cell.Cell)(nil), parityProgramKeySlice(0x12, 8), plainRoot, int64(8)},
		},
		{
			name:  "dictugetoptref_plain_value_dict_error",
			code:  code(parityProgramDictScalarOpcode(0xF469, 2)),
			stack: []any{int64(0x12), plainRoot, int64(8)},
		},
		{
			name:  "dictudelgetref_plain_value_dict_error",
			code:  code(parityProgramDictValueOpcode(0xF462, 2, true)),
			stack: []any{int64(0x12), plainRoot, int64(8)},
		},
		{
			name:  "dictiminref_plain_value_dict_error",
			code:  code(parityProgramDictValueOpcode(0xF482, 1, true)),
			stack: []any{plainRoot, int64(8)},
		},
		{
			name:  "dictimaxref_plain_value_dict_error",
			code:  code(parityProgramDictValueOpcode(0xF48A, 1, true)),
			stack: []any{plainRoot, int64(8)},
		},
		{
			name:  "dictiremminref_plain_value_dict_error",
			code:  code(parityProgramDictValueOpcode(0xF492, 1, true)),
			stack: []any{plainRoot, int64(8)},
		},
		{
			name:  "dictiremmaxref_plain_value_dict_error",
			code:  code(parityProgramDictValueOpcode(0xF49A, 1, true)),
			stack: []any{plainRoot, int64(8)},
		},
		{
			name:  "dictuminref_plain_value_dict_error",
			code:  code(parityProgramDictValueOpcode(0xF482, 2, true)),
			stack: []any{plainRoot, int64(8)},
		},
		{
			name:  "dictumaxref_plain_value_dict_error",
			code:  code(parityProgramDictValueOpcode(0xF48A, 2, true)),
			stack: []any{plainRoot, int64(8)},
		},
		{
			name:  "dicturemminref_plain_value_dict_error",
			code:  code(parityProgramDictValueOpcode(0xF492, 2, true)),
			stack: []any{plainRoot, int64(8)},
		},
		{
			name:  "dicturemmaxref_plain_value_dict_error",
			code:  code(parityProgramDictValueOpcode(0xF49A, 2, true)),
			stack: []any{plainRoot, int64(8)},
		},
		{
			name:  "dictusetgetref_old_value_plain_slice",
			code:  code(parityProgramDictValueOpcode(0xF41A, 2, true)),
			stack: []any{cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell(), int64(0x12), plainRoot, int64(8)},
		},
		{
			name:  "dictset_library_root_underflow",
			code:  code(0xF412),
			stack: []any{valueSlice(0x55, 8), parityProgramKeySlice(0x12, 8), mustLibraryCell(t), int64(8)},
		},
		{
			name:  "dictgetnext_short_key_underflow",
			code:  code(0xF474),
			stack: []any{parityProgramKeySlice(0x1, 4), (*cell.Cell)(nil), int64(8)},
		},
		{
			name:  "dictgetnexteq_short_key_underflow",
			code:  code(0xF475),
			stack: []any{parityProgramKeySlice(0x1, 4), (*cell.Cell)(nil), int64(8)},
		},
		{
			name:  "dictgetprev_short_key_underflow",
			code:  code(0xF476),
			stack: []any{parityProgramKeySlice(0x1, 4), (*cell.Cell)(nil), int64(8)},
		},
		{
			name:  "dictgetpreveq_short_key_underflow",
			code:  code(0xF477),
			stack: []any{parityProgramKeySlice(0x1, 4), (*cell.Cell)(nil), int64(8)},
		},
		{
			name:  "pfxdictgetq_input_with_refs_preserved",
			code:  code(0xF4A8),
			stack: []any{prefixInputWithRef, prefixRoot, int64(4)},
		},
		{
			name:  "dictsubdictget_slice_prefix_bits_range",
			code:  code(parityProgramDictScalarOpcode(0xF4B1, 0)),
			stack: []any{parityProgramKeySlice(0, 8), int64(9), subdictRoot, int64(8)},
		},
		{
			name:  "dictsubdictget_slice_prefix_underflow",
			code:  code(parityProgramDictScalarOpcode(0xF4B1, 0)),
			stack: []any{parityProgramKeySlice(0b10, 2), int64(4), subdictRoot, int64(8)},
		},
		{
			name:  "dictusubdictget_prefix_bits_range",
			code:  code(parityProgramDictScalarOpcode(0xF4B1, 2)),
			stack: []any{big.NewInt(0), int64(9), subdictRoot, int64(8)},
		},
		{
			name:  "dictusubdictget_prefix_value_underflow",
			code:  code(parityProgramDictScalarOpcode(0xF4B1, 2)),
			stack: []any{big.NewInt(0x10), int64(4), subdictRoot, int64(8)},
		},
		{
			name:  "subdictiget_prefix_bits_range",
			code:  code(parityProgramDictScalarOpcode(0xF4B1, 1)),
			stack: []any{big.NewInt(0), int64(9), subdictRoot, int64(8)},
		},
		{
			name:  "subdictiget_prefix_value_underflow",
			code:  code(parityProgramDictScalarOpcode(0xF4B1, 1)),
			stack: []any{big.NewInt(2), int64(1), subdictRoot, int64(8)},
		},
		{
			name:  "dictsubdictrpget_slice_prefix_bits_range",
			code:  code(parityProgramDictScalarOpcode(0xF4B5, 0)),
			stack: []any{parityProgramKeySlice(0, 8), int64(9), subdictRoot, int64(8)},
		},
		{
			name:  "dictsubdictrpget_slice_prefix_underflow",
			code:  code(parityProgramDictScalarOpcode(0xF4B5, 0)),
			stack: []any{parityProgramKeySlice(0b10, 2), int64(4), subdictRoot, int64(8)},
		},
		{
			name:  "dictusubdictrpget_prefix_bits_range",
			code:  code(parityProgramDictScalarOpcode(0xF4B5, 2)),
			stack: []any{big.NewInt(0), int64(9), subdictRoot, int64(8)},
		},
		{
			name:  "dictusubdictrpget_prefix_value_underflow",
			code:  code(parityProgramDictScalarOpcode(0xF4B5, 2)),
			stack: []any{big.NewInt(0x10), int64(4), subdictRoot, int64(8)},
		},
		{
			name:  "dictisubdictrpget_prefix_bits_range",
			code:  code(parityProgramDictScalarOpcode(0xF4B5, 1)),
			stack: []any{big.NewInt(0), int64(9), subdictRoot, int64(8)},
		},
		{
			name:  "dictisubdictrpget_prefix_value_underflow",
			code:  code(parityProgramDictScalarOpcode(0xF4B5, 1)),
			stack: []any{big.NewInt(2), int64(1), subdictRoot, int64(8)},
		},
	}

	caseNames := make([]string, 0, len(cases))
	for _, tc := range cases {
		caseNames = append(caseNames, tc.name)
	}
	requireNamedParityCases(t, "dict_edge_gap", caseNames, requiredDictEdgeGapCaseNames)

	empty := testEmptyCell()
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runDifferentialFuzzCase(t, differentialFuzzCase{
				seed:   uint64(i),
				family: "dict_edge_gap",
				op:     tc.name,
				code:   tc.code,
				stack:  tc.stack,
				c7:     prepareCrossTestC7(nil, empty),
			})
		})
	}
}

func TestTVMDifferentialFuzzCellSliceGapOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	cases := []struct {
		name string
		emit func(*parityProgramGenerator) bool
	}{
		{"ldule4q", func(g *parityProgramGenerator) bool { return g.emitCellSliceLittleEndianOp(12) }},
		{"ldile8q", func(g *parityProgramGenerator) bool { return g.emitCellSliceLittleEndianOp(13) }},
		{"pldule4q", func(g *parityProgramGenerator) bool { return g.emitCellSliceLittleEndianOp(14) }},
		{"pldile8q", func(g *parityProgramGenerator) bool { return g.emitCellSliceLittleEndianOp(15) }},
		{"bchkbitrefs", func(g *parityProgramGenerator) bool { return g.emitCellSliceBuilderAdvancedOp(33) }},
		{"stb", func(g *parityProgramGenerator) bool { return g.emitCellSliceBuilderAdvancedOp(34) }},
		{"stile4", func(g *parityProgramGenerator) bool { return g.emitCellSliceLittleEndianOp(16) }},
		{"stule4", func(g *parityProgramGenerator) bool { return g.emitCellSliceLittleEndianOp(17) }},
		{"stile8", func(g *parityProgramGenerator) bool { return g.emitCellSliceLittleEndianOp(18) }},
		{"stule8", func(g *parityProgramGenerator) bool { return g.emitCellSliceLittleEndianOp(19) }},
		{"bchkbitrefsq", func(g *parityProgramGenerator) bool { return g.emitCellSliceBuilderAdvancedOp(35) }},
		{"xctos_ordinary", func(g *parityProgramGenerator) bool { return g.emitCellSliceTransformOp(30) }},
		{"sdsfxrev", func(g *parityProgramGenerator) bool { return g.emitCellSlicePredicateOp(15) }},
		{"sdpsfxrev", func(g *parityProgramGenerator) bool { return g.emitCellSlicePredicateOp(17) }},
		{"sdbeginsx", func(g *parityProgramGenerator) bool { return g.emitCellSlicePrefixGramsOp(0) }},
		{"sdbeginsxq", func(g *parityProgramGenerator) bool { return g.emitCellSlicePrefixGramsOp(1) }},
		{"sdbeginsconst", func(g *parityProgramGenerator) bool { return g.emitCellSlicePrefixGramsOp(3) }},
		{"sdbeginsconstq", func(g *parityProgramGenerator) bool { return g.emitCellSlicePrefixGramsOp(4) }},
		{"ldgrams", func(g *parityProgramGenerator) bool { return g.emitCellSlicePrefixGramsOp(6) }},
		{"stgrams", func(g *parityProgramGenerator) bool { return g.emitCellSlicePrefixGramsOp(7) }},
		{"strefconst", func(g *parityProgramGenerator) bool { return g.emitCellSliceBuilderAdvancedOp(21) }},
		{"stref2const", func(g *parityProgramGenerator) bool { return g.emitCellSliceBuilderAdvancedOp(22) }},
		{"stsliceconst", func(g *parityProgramGenerator) bool { return g.emitCellSliceBuilderAdvancedOp(23) }},
		{"endcst", func(g *parityProgramGenerator) bool { return g.emitCellSliceBuilderAdvancedOp(24) }},
		{"stix_variable", func(g *parityProgramGenerator) bool { return g.emitCellSliceBuilderAdvancedOp(36) }},
		{"ldiq_fixed", func(g *parityProgramGenerator) bool { return g.emitCellSliceLoadFamilyOp(28) }},
		{"pldiq_fixed", func(g *parityProgramGenerator) bool { return g.emitCellSliceLoadFamilyOp(29) }},
	}

	caseNames := make([]string, 0, len(cases))
	for _, tc := range cases {
		caseNames = append(caseNames, tc.name)
	}
	requireNamedParityCases(t, "cellslice_gap", caseNames, requiredCellSliceGapCaseNames)

	empty := testEmptyCell()
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := &parityProgramGenerator{
				r:    rand.New(rand.NewSource(int64(i))),
				seed: append([]byte(nil), crossTestSeed...),
				regD: [2]*cell.Cell{
					empty,
					empty,
				},
			}
			if !tc.emit(g) {
				t.Fatalf("%s was not emitted", tc.name)
			}

			runDifferentialFuzzCase(t, differentialFuzzCase{
				seed:   uint64(i),
				family: "cellslice_gap",
				op:     strings.Join(g.trace, " -> "),
				code:   parityProgramCodeFromBuilders(t, g.ops...),
				c7:     prepareCrossTestC7(nil, empty),
			})
		})
	}
}

func TestTVMDifferentialFuzzCellSliceQuietGapOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	emitTransform := func(mode int) func(*parityProgramGenerator) bool {
		return func(g *parityProgramGenerator) bool { return g.emitCellSliceTransformOp(mode) }
	}
	emitLoadFamily := func(mode int) func(*parityProgramGenerator) bool {
		return func(g *parityProgramGenerator) bool { return g.emitCellSliceLoadFamilyOp(mode) }
	}
	emitLittleEndian := func(mode int) func(*parityProgramGenerator) bool {
		return func(g *parityProgramGenerator) bool { return g.emitCellSliceLittleEndianOp(mode) }
	}
	emitBuilderAdvanced := func(mode int) func(*parityProgramGenerator) bool {
		return func(g *parityProgramGenerator) bool { return g.emitCellSliceBuilderAdvancedOp(mode) }
	}

	shortSlice := parityProgramBitsSlice(0b1011, 4)
	sliceSrc := parityProgramBitsSlice(0b101101, 6)

	cases := []struct {
		name  string
		code  *cell.Cell
		stack []any
		emit  func(*parityProgramGenerator) bool
	}{
		{"splitq_fail_preserves", nil, nil, emitTransform(13)},
		{"schkbitsq_true", nil, nil, emitTransform(17)},
		{"schkbitsq_false", nil, nil, emitTransform(26)},
		{"schkrefsq_true", nil, nil, emitTransform(27)},
		{"schkrefsq_false", nil, nil, emitTransform(18)},
		{"schkbitrefsq_true", nil, nil, emitTransform(19)},
		{"schkbitrefsq_false", nil, nil, emitTransform(20)},
		{"pldrefvar_idx1", nil, nil, emitTransform(21)},
		{"pldrefidx0", nil, nil, emitTransform(22)},
		{"pldrefidx1", nil, nil, emitTransform(28)},
		{"ldzeroes_nonzero", nil, nil, emitTransform(23)},
		{"ldzeroes_zero", nil, nil, emitTransform(29)},
		{"ldones_nonzero", nil, nil, emitTransform(24)},
		{"ldones_zero", nil, nil, emitTransform(31)},
		{"ldsame_one", nil, nil, emitTransform(25)},
		{"bchkbitsq_false", parityProgramCodeCell(cellsliceop.BCHKBITSQ().Serialize()), []any{parityProgramFullBitsBuilder(), int64(1)}, nil},
		{"bchkrefsq_false", parityProgramCodeCell(cellsliceop.BCHKREFSQ().Serialize()), []any{parityProgramFullRefsBuilder(), int64(1)}, nil},
		{"bchkbitrefsq_false", parityProgramCodeCell(cellsliceop.BCHKBITREFSQ().Serialize()), []any{parityProgramFullRefsBuilder(), int64(0), int64(1)}, nil},
		{"ldixq_fail_preserves_slice", parityProgramCodeCell(cellsliceop.LDIXQ().Serialize()), []any{shortSlice.Copy(), int64(8)}, nil},
		{"pldixq_fail_only_flag", parityProgramCodeCell(cellsliceop.PLDIXQ().Serialize()), []any{shortSlice.Copy(), int64(8)}, nil},
		{"lduxq_fail_preserves_slice", nil, nil, emitLoadFamily(5)},
		{"plduxq_fail_only_flag", nil, nil, emitLoadFamily(7)},
		{"ldslicexq_success", nil, nil, emitLoadFamily(17)},
		{"ldslicexq_fail_preserves_slice", nil, nil, emitLoadFamily(18)},
		{"ldslicexq_zero_width", parityProgramCodeCell(cellsliceop.LDSLICEXQ().Serialize()), []any{sliceSrc.Copy(), int64(0)}, nil},
		{"pldslicexq_success", nil, nil, emitLoadFamily(19)},
		{"pldslicexq_fail_only_flag", nil, nil, emitLoadFamily(20)},
		{"pldslicexq_zero_width", parityProgramCodeCell(cellsliceop.PLDSLICEXQ().Serialize()), []any{sliceSrc.Copy(), int64(0)}, nil},
		{"ldslicefixq_fail_preserves_slice", nil, nil, emitLoadFamily(27)},
		{"pldslicefixq_fail_only_flag", nil, nil, emitLoadFamily(26)},
		{"ldule8q_fail_preserves_slice", nil, nil, emitLittleEndian(9)},
		{"pldule8q_fail_only_flag", nil, nil, emitLittleEndian(11)},
		{"ldile8q_success", nil, nil, emitLittleEndian(13)},
		{"pldile8q_success", nil, nil, emitLittleEndian(15)},
		{"strefq_success", nil, nil, emitBuilderAdvanced(5)},
		{"strefq_fail_preserves", nil, nil, emitBuilderAdvanced(6)},
		{"strefrq_success", nil, nil, emitBuilderAdvanced(7)},
		{"strefrq_fail_preserves", nil, nil, emitBuilderAdvanced(8)},
		{"stbrefq_success", nil, nil, emitBuilderAdvanced(9)},
		{"stbrefq_fail_preserves", nil, nil, emitBuilderAdvanced(10)},
		{"stbrefqr_success", nil, nil, emitBuilderAdvanced(11)},
		{"stbrefqr_fail_preserves", nil, nil, emitBuilderAdvanced(12)},
		{"stsliceq_success", nil, nil, emitBuilderAdvanced(13)},
		{"stsliceq_fail_preserves", nil, nil, emitBuilderAdvanced(14)},
		{"stslicerq_success", nil, nil, emitBuilderAdvanced(15)},
		{"stslicerq_fail_preserves", nil, nil, emitBuilderAdvanced(16)},
		{"stbq_success", nil, nil, emitBuilderAdvanced(17)},
		{"stbq_fail_preserves", nil, nil, emitBuilderAdvanced(18)},
		{"stbrq_success", nil, nil, emitBuilderAdvanced(19)},
		{"stbrq_fail_preserves", nil, nil, emitBuilderAdvanced(20)},
		{"stuxq_range_fail_status_plus_one", parityProgramCodeCell(parityProgramRawOp(0xCF05, 16)), []any{int64(256), cell.BeginCell(), int64(8)}, nil},
		{"stuxq_capacity_fail_status_minus_one", parityProgramCodeCell(parityProgramRawOp(0xCF05, 16)), []any{int64(1), parityProgramFullBitsBuilder(), int64(8)}, nil},
	}

	caseNames := make([]string, 0, len(cases))
	for _, tc := range cases {
		caseNames = append(caseNames, tc.name)
	}
	requireNamedParityCases(t, "cellslice_quiet_gap", caseNames, requiredCellSliceQuietGapCaseNames)

	empty := testEmptyCell()
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code := tc.code
			stack := tc.stack
			op := tc.name
			if tc.emit != nil {
				g := &parityProgramGenerator{
					r:    rand.New(rand.NewSource(int64(i))),
					seed: append([]byte(nil), crossTestSeed...),
					regD: [2]*cell.Cell{
						empty,
						empty,
					},
				}
				if !tc.emit(g) {
					t.Fatalf("%s was not emitted", tc.name)
				}
				code = parityProgramCodeFromBuilders(t, g.ops...)
				op = strings.Join(g.trace, " -> ")
			}

			runDifferentialFuzzCase(t, differentialFuzzCase{
				seed:   uint64(i),
				family: "cellslice_quiet_gap",
				op:     op,
				code:   code,
				stack:  stack,
				c7:     prepareCrossTestC7(nil, empty),
			})
		})
	}
}

func TestTVMDifferentialFuzzCellSliceEdgeGapOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	refA := cell.BeginCell().MustStoreUInt(0xA, 4).EndCell()
	refB := cell.BeginCell().MustStoreUInt(0xB, 4).EndCell()
	sliceWithRef := cell.BeginCell().
		MustStoreUInt(0b1011, 4).
		MustStoreRef(refA).
		ToSlice()

	cases := []struct {
		name  string
		code  *cell.Cell
		stack []any
	}{
		{
			name:  "lduxq_width_257_rangecheck",
			code:  parityProgramCodeCell(cellsliceop.LDUXQ().Serialize()),
			stack: []any{matrixSlice(t, 257, 0), int64(257)},
		},
		{
			name:  "plduxq_width_257_rangecheck",
			code:  parityProgramCodeCell(cellsliceop.PLDUXQ().Serialize()),
			stack: []any{matrixSlice(t, 257, 0), int64(257)},
		},
		{
			name:  "ldixq_width_258_rangecheck",
			code:  parityProgramCodeCell(cellsliceop.LDIXQ().Serialize()),
			stack: []any{matrixSlice(t, 257, 0), int64(258)},
		},
		{
			name:  "pldixq_width_258_rangecheck",
			code:  parityProgramCodeCell(cellsliceop.PLDIXQ().Serialize()),
			stack: []any{matrixSlice(t, 257, 0), int64(258)},
		},
		{
			name:  "schkbitrefsq_refs5_rangecheck",
			code:  parityProgramCodeCell(cellsliceop.SCHKBITREFSQ().Serialize()),
			stack: []any{matrixSlice(t, 0, 4), int64(0), int64(5)},
		},
		{
			name:  "chashix_short_stack_bad_idx_order",
			code:  parityProgramCodeCell(cellsliceop.CHASHIX().Serialize()),
			stack: []any{int64(4)},
		},
		{
			name:  "cdepthix_short_stack_bad_idx_order",
			code:  parityProgramCodeCell(cellsliceop.CDEPTHIX().Serialize()),
			stack: []any{int64(4)},
		},
		{
			name:  "subslice_r2_range_precedes_slice_typecheck",
			code:  parityProgramCodeCell(cellsliceop.SUBSLICE().Serialize()),
			stack: []any{int64(77), int64(0), int64(0), int64(0), int64(5)},
		},
		{
			name:  "plduz_256_short_255_zero_extend",
			code:  parityProgramCodeCell(cellsliceop.PLDUZ(256).Serialize()),
			stack: []any{matrixSlice(t, 255, 0)},
		},
		{
			name:  "pldrefidx3_four_refs",
			code:  parityProgramCodeCell(cellsliceop.PLDREFIDX(3).Serialize()),
			stack: []any{matrixSlice(t, 0, 4)},
		},
		{
			name:  "pldrefvar_idx3_four_refs",
			code:  parityProgramCodeCell(cellsliceop.PLDREFVAR().Serialize()),
			stack: []any{matrixSlice(t, 0, 4), int64(3)},
		},
		{
			name:  "pldrefidx3_underflow",
			code:  parityProgramCodeCell(cellsliceop.PLDREFIDX(3).Serialize()),
			stack: []any{matrixSlice(t, 0, 3)},
		},
		{
			name:  "ldrefrotos_no_ref_underflow",
			code:  parityProgramCodeCell(cellsliceop.LDREFRTOS().Serialize()),
			stack: []any{cell.BeginCell().ToSlice()},
		},
		{
			name:  "bchkbitsimmq_true",
			code:  parityProgramCodeCell(cellsliceop.BCHKBITSIMM(8, true).Serialize()),
			stack: []any{cell.BeginCell()},
		},
		{
			name:  "bchkbitsimmq_false",
			code:  parityProgramCodeCell(cellsliceop.BCHKBITSIMM(1, true).Serialize()),
			stack: []any{parityProgramFullBitsBuilder()},
		},
		{
			name:  "stsliceconst_with_ref",
			code:  parityProgramCodeCell(cellsliceop.STSLICECONST(sliceWithRef).Serialize()),
			stack: []any{cell.BeginCell()},
		},
		{
			name:  "stsliceconst_bits_overflow",
			code:  parityProgramCodeCell(cellsliceop.STSLICECONST(cell.BeginCell().MustStoreUInt(1, 1).ToSlice()).Serialize()),
			stack: []any{parityProgramFullBitsBuilder()},
		},
		{
			name:  "stref2const_overflow",
			code:  parityProgramCodeCell(cellsliceop.STREF2CONST(refA, refB).Serialize()),
			stack: []any{parityProgramFullRefsBuilder()},
		},
		{
			name:  "endcst_dst_ref_overflow",
			code:  parityProgramCodeCell(cellsliceop.ENDCST().Serialize()),
			stack: []any{parityProgramFullRefsBuilder(), cell.BeginCell()},
		},
	}

	caseNames := make([]string, 0, len(cases))
	for _, tc := range cases {
		caseNames = append(caseNames, tc.name)
	}
	requireNamedParityCases(t, "cellslice_edge_gap", caseNames, requiredCellSliceEdgeGapCaseNames)

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runDifferentialFuzzCase(t, differentialFuzzCase{
				seed:   uint64(i),
				family: "cellslice_edge_gap",
				op:     tc.name,
				code:   tc.code,
				stack:  tc.stack,
				c7:     prepareCrossTestC7(nil, testEmptyCell()),
			})
		})
	}
}

func TestTVMDifferentialFuzzInventoryResidualGapOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	ref := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	builder := func() *cell.Builder {
		return cell.BeginCell().MustStoreUInt(0xABCD, 16).MustStoreRef(ref)
	}
	slice := func() *cell.Slice {
		return cell.BeginCell().MustStoreUInt(0b00111100, 8).MustStoreRef(ref).ToSlice()
	}
	shortSlice := func() *cell.Slice {
		return cell.BeginCell().MustStoreUInt(0xC, 4).ToSlice()
	}
	suffix := func() *cell.Slice {
		return cell.BeginCell().MustStoreUInt(0xC, 4).ToSlice()
	}
	longer := func() *cell.Slice {
		return cell.BeginCell().MustStoreUInt(0xAC, 8).ToSlice()
	}
	ordinaryCell := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()

	cases := []struct {
		name  string
		code  *cell.Cell
		stack []any
	}{
		{
			name: "nop_keeps_stack",
			code: parityProgramCodeCell(stackop.NOP().Serialize()),
			stack: []any{
				int64(11),
			},
		},
		{
			name: "multi_xc2pu",
			code: parityProgramCodeCell(stackop.XC2PU(1, 2, 0).Serialize()),
			stack: []any{
				int64(10),
				int64(20),
				int64(30),
				int64(40),
			},
		},
		{
			name: "multi_xcpuxc",
			code: parityProgramCodeCell(stackop.XCPUXC(2, 1, 3).Serialize()),
			stack: []any{
				int64(10),
				int64(20),
				int64(30),
				int64(40),
			},
		},
		{
			name: "multi_xcpu2",
			code: parityProgramCodeCell(stackop.XCPU2(2, 1, 0).Serialize()),
			stack: []any{
				int64(10),
				int64(20),
				int64(30),
				int64(40),
			},
		},
		{
			name: "multi_puxc2",
			code: parityProgramCodeCell(stackop.PUXC2(1, 2, 0).Serialize()),
			stack: []any{
				int64(10),
				int64(20),
				int64(30),
				int64(40),
			},
		},
		{
			name: "multi_puxcpu",
			code: parityProgramCodeCell(stackop.PUXCPU(1, 2, 0).Serialize()),
			stack: []any{
				int64(10),
				int64(20),
				int64(30),
				int64(40),
			},
		},
		{
			name: "multi_puxc",
			code: parityProgramCodeCell(stackop.PUXC(1, 2).Serialize()),
			stack: []any{
				int64(10),
				int64(20),
				int64(30),
				int64(40),
			},
		},
		{
			name: "multi_xcpu",
			code: parityProgramCodeCell(stackop.XCPU(2, 1).Serialize()),
			stack: []any{
				int64(10),
				int64(20),
				int64(30),
				int64(40),
			},
		},
		{
			name: "gtint_true",
			code: parityProgramCodeCell(mathop.GTINT(7).Serialize()),
			stack: []any{
				int64(9),
			},
		},
		{
			name: "qgtint_nan",
			code: parityProgramCodeCell(mathop.QGTINT(7).Serialize()),
			stack: []any{
				vm.NaN{},
			},
		},
		{
			name: "sgn_negative",
			code: parityProgramCodeCell(mathop.SGN().Serialize()),
			stack: []any{
				int64(-77),
			},
		},
		{
			name: "qsgn_nan",
			code: parityProgramCodeCell(mathop.QSGN().Serialize()),
			stack: []any{
				vm.NaN{},
			},
		},
		{
			name: "endxc_ordinary",
			code: parityProgramCodeCell(cellsliceop.ENDXC().Serialize()),
			stack: []any{
				builder(),
				int64(0),
			},
		},
		{
			name: "bbitrefs",
			code: parityProgramCodeCell(cellsliceop.BBITREFS().Serialize()),
			stack: []any{
				builder(),
			},
		},
		{
			name: "btos",
			code: parityProgramCodeCell(cellsliceop.BTOS().Serialize()),
			stack: []any{
				builder(),
			},
		},
		{
			name: "split_valid",
			code: parityProgramCodeCell(cellsliceop.SPLIT().Serialize()),
			stack: []any{
				slice(),
				int64(4),
				int64(1),
			},
		},
		{
			name: "splitq_valid",
			code: parityProgramCodeCell(cellsliceop.SPLITQ().Serialize()),
			stack: []any{
				slice(),
				int64(4),
				int64(1),
			},
		},
		{
			name: "schkbits",
			code: parityProgramCodeCell(cellsliceop.SCHKBITS().Serialize()),
			stack: []any{
				slice(),
				int64(8),
			},
		},
		{
			name: "schkrefs",
			code: parityProgramCodeCell(cellsliceop.SCHKREFS().Serialize()),
			stack: []any{
				slice(),
				int64(1),
			},
		},
		{
			name: "schkbitrefs",
			code: parityProgramCodeCell(cellsliceop.SCHKBITREFS().Serialize()),
			stack: []any{
				slice(),
				int64(8),
				int64(1),
			},
		},
		{
			name: "schkbitsq_false",
			code: parityProgramCodeCell(cellsliceop.SCHKBITSQ().Serialize()),
			stack: []any{
				shortSlice(),
				int64(8),
			},
		},
		{
			name: "schkrefsq_false",
			code: parityProgramCodeCell(cellsliceop.SCHKREFSQ().Serialize()),
			stack: []any{
				shortSlice(),
				int64(1),
			},
		},
		{
			name: "schkbitrefsq_false",
			code: parityProgramCodeCell(cellsliceop.SCHKBITREFSQ().Serialize()),
			stack: []any{
				shortSlice(),
				int64(8),
				int64(1),
			},
		},
		{
			name: "clevel",
			code: parityProgramCodeCell(cellsliceop.CLEVEL().Serialize()),
			stack: []any{
				ordinaryCell,
			},
		},
		{
			name: "clevelmask",
			code: parityProgramCodeCell(cellsliceop.CLEVELMASK().Serialize()),
			stack: []any{
				ordinaryCell,
			},
		},
		{
			name: "sdcntlead0",
			code: parityProgramCodeCell(cellsliceop.SDCNTLEAD0().Serialize()),
			stack: []any{
				slice(),
			},
		},
		{
			name: "sdcntlead1",
			code: parityProgramCodeCell(cellsliceop.SDCNTLEAD1().Serialize()),
			stack: []any{
				cell.BeginCell().MustStoreUInt(0b11110000, 8).ToSlice(),
			},
		},
		{
			name: "sdcnttrail0",
			code: parityProgramCodeCell(cellsliceop.SDCNTTRAIL0().Serialize()),
			stack: []any{
				cell.BeginCell().MustStoreUInt(0b11110000, 8).ToSlice(),
			},
		},
		{
			name: "sdcnttrail1",
			code: parityProgramCodeCell(cellsliceop.SDCNTTRAIL1().Serialize()),
			stack: []any{
				cell.BeginCell().MustStoreUInt(0b00001111, 8).ToSlice(),
			},
		},
		{
			name: "sdsfx",
			code: parityProgramCodeCell(cellsliceop.SDSFX().Serialize()),
			stack: []any{
				suffix(),
				longer(),
			},
		},
		{
			name: "sdpsfx",
			code: parityProgramCodeCell(cellsliceop.SDPSFX().Serialize()),
			stack: []any{
				suffix(),
				longer(),
			},
		},
		{
			name: "brembits",
			code: parityProgramCodeCell(cellsliceop.BREMBITS().Serialize()),
			stack: []any{
				builder(),
			},
		},
		{
			name: "bremrefs",
			code: parityProgramCodeCell(cellsliceop.BREMREFS().Serialize()),
			stack: []any{
				builder(),
			},
		},
		{
			name: "brembitrefs",
			code: parityProgramCodeCell(cellsliceop.BREMBITREFS().Serialize()),
			stack: []any{
				builder(),
			},
		},
		{
			name: "accept",
			code: parityProgramCodeCell(funcsop.ACCEPT().Serialize()),
		},
		{
			name: "setgaslimit",
			code: parityProgramCodeCell(funcsop.SETGASLIMIT().Serialize()),
			stack: []any{
				int64(differentialFuzzGasLimit),
			},
		},
		{
			name: "gasconsumed",
			code: parityProgramCodeCell(funcsop.GASCONSUMED().Serialize()),
		},
		{
			name: "commit",
			code: parityProgramCodeCell(funcsop.COMMIT().Serialize()),
		},
	}

	caseNames := make([]string, 0, len(cases))
	for _, tc := range cases {
		caseNames = append(caseNames, tc.name)
	}
	requireNamedParityCases(t, "inventory_residual_gap", caseNames, requiredInventoryResidualGapCaseNames)

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runDifferentialFuzzCase(t, differentialFuzzCase{
				seed:     uint64(i),
				family:   "inventory_residual_gap",
				op:       tc.name,
				code:     tc.code,
				stack:    tc.stack,
				gasLimit: differentialFuzzGasLimit,
				c7:       prepareCrossTestC7(nil, testEmptyCell()),
			})
		})
	}
}

func TestTVMDifferentialFuzzLibraryGapOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	target := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	libraryRef := mustCrossLibraryCellForHash(t, target.Hash())
	libraryCollection := mustCrossLibraryCollection(t, target)
	missingLibraryRef := mustCrossLibraryCellForHash(t, cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell().Hash())
	cases := []struct {
		name    string
		code    *cell.Cell
		stack   []any
		refLibs *cell.Cell
	}{
		{
			name:    "ctos_library_resolution",
			code:    parityProgramCodeCell(cellsliceop.CTOS().Serialize()),
			stack:   []any{libraryRef},
			refLibs: libraryCollection,
		},
		{
			name:    "xload_library_resolution",
			code:    parityProgramCodeCell(cellsliceop.XLOAD().Serialize()),
			stack:   []any{libraryRef},
			refLibs: libraryCollection,
		},
		{
			name:    "xloadq_library_resolution",
			code:    parityProgramCodeCell(cellsliceop.XLOADQ().Serialize()),
			stack:   []any{libraryRef},
			refLibs: libraryCollection,
		},
		{
			name:    "xctos_library_resolution",
			code:    parityProgramCodeCell(cellsliceop.XCTOS().Serialize()),
			stack:   []any{libraryRef},
			refLibs: libraryCollection,
		},
		{
			name:  "xctos_library_special",
			code:  parityProgramCodeCell(cellsliceop.XCTOS().Serialize()),
			stack: []any{libraryRef},
		},
		{
			name:  "xload_library_missing_underflow",
			code:  parityProgramCodeCell(cellsliceop.XLOAD().Serialize()),
			stack: []any{missingLibraryRef},
		},
		{
			name:  "xloadq_library_missing_false",
			code:  parityProgramCodeCell(cellsliceop.XLOADQ().Serialize()),
			stack: []any{missingLibraryRef},
		},
	}

	caseNames := make([]string, 0, len(cases))
	for _, tc := range cases {
		caseNames = append(caseNames, tc.name)
	}
	requireNamedParityCases(t, "library_gap", caseNames, requiredLibraryGapCaseNames)

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runDifferentialFuzzCase(t, differentialFuzzCase{
				seed:     uint64(i),
				family:   "library_gap",
				op:       tc.name,
				code:     tc.code,
				stack:    tc.stack,
				gasLimit: differentialFuzzGasLimit,
				refLibs:  tc.refLibs,
			})
		})
	}
}

func TestTVMDifferentialFuzzStackGapOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	values := func(n int) []any {
		out := make([]any, n)
		for i := range out {
			out[i] = int64(i + 1)
		}
		return out
	}
	withIndex := func(n int, idxs ...int64) []any {
		out := values(n)
		for _, idx := range idxs {
			out = append(out, idx)
		}
		return out
	}
	refPayload := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	slicePayload := cell.BeginCell().MustStoreUInt(0xCA, 8).ToSlice()
	refSlicePayload := cell.BeginCell().MustStoreUInt(0xDC, 8).EndCell().MustBeginParse()
	pushContBody := parityProgramCodeCell(stackop.PUSHINT(big.NewInt(123)).Serialize())

	cases := []struct {
		name  string
		code  *cell.Cell
		stack []any
	}{
		{"xchg0l_depth17", parityProgramCodeCell(stackop.XCHG0L(17).Serialize()), values(20)},
		{"pushl_depth17", parityProgramCodeCell(stackop.PUSHL(17).Serialize()), values(20)},
		{"popl_depth17", parityProgramCodeCell(stackop.POPL(17).Serialize()), values(20)},
		{"pick_depth4", parityProgramCodeCell(stackop.PICK().Serialize()), withIndex(6, 4)},
		{"roll_depth4", parityProgramCodeCell(stackop.ROLL().Serialize()), withIndex(6, 4)},
		{"rollrev_depth4", parityProgramCodeCell(stackop.ROLLREV().Serialize()), withIndex(6, 4)},
		{"dropx_three", parityProgramCodeCell(stackop.DROPX().Serialize()), withIndex(6, 3)},
		{"xchgx_depth3", parityProgramCodeCell(stackop.XCHGX().Serialize()), withIndex(6, 3)},
		{"revx_3_1", parityProgramCodeCell(stackop.REVX().Serialize()), withIndex(6, 3, 1)},
		{"onlytopx_keep3", parityProgramCodeCell(stackop.ONLYTOPX().Serialize()), withIndex(6, 3)},
		{"onlyx_keep3", parityProgramCodeCell(stackop.ONLYX().Serialize()), withIndex(6, 3)},
		{"blkswx_2_2", parityProgramCodeCell(stackop.BLKSWX().Serialize()), withIndex(6, 2, 2)},
		{"multi_pu2xc", parityProgramCodeCell(stackop.PU2XC(1, 2, 0).Serialize()), values(6)},
		{"push3", parityProgramCodeCell(stackop.PUSH3(0, 1, 2).Serialize()), values(6)},
		{"pushint_small", parityProgramCodeCell(stackop.PUSHINT(big.NewInt(42)).Serialize()), nil},
		{"pushslice_payload", parityProgramCodeCell(stackop.PUSHSLICE(slicePayload).Serialize()), nil},
		{"pushrefslice_payload", parityProgramCodeCell(stackop.PUSHREFSLICE(refSlicePayload).Serialize()), nil},
		{"pushref_payload", parityProgramCodeCell(stackop.PUSHREF(refPayload).Serialize()), nil},
		{"pushcont_execute", parityProgramCodeCell(stackop.PUSHCONT(pushContBody).Serialize(), execop.EXECUTE().Serialize()), nil},
		{"pushnull", parityProgramCodeCell(tupleop.PUSHNULL().Serialize()), nil},
		{"pushnan", parityProgramCodeCell(mathop.PUSHNAN().Serialize()), nil},
		{"dumpstk_noop", parityProgramCodeCell(stackop.DUMPSTK().Serialize()), nil},
		{"dump_s0_noop", parityProgramCodeCell(stackop.DUMP(0).Serialize()), []any{int64(7)}},
		{"debug_noop", parityProgramCodeCell(stackop.DEBUG(1).Serialize()), nil},
		{"debugstr_noop", parityProgramCodeCell(stackop.DEBUGSTR([]byte{0}).Serialize()), nil},
		{"strdump_slice_noop", parityProgramCodeCell(stackop.STRDUMP().Serialize()), []any{slicePayload}},
		{"condsel_true", parityProgramCodeCell(stackop.CONDSEL().Serialize()), []any{int64(-1), int64(11), int64(22)}},
		{"condsel_false", parityProgramCodeCell(stackop.CONDSEL().Serialize()), []any{int64(0), int64(11), int64(22)}},
		{"condselchk_true", parityProgramCodeCell(execop.CONDSELCHK().Serialize()), []any{int64(-1), int64(11), int64(22)}},
		{
			"condselchk_mixed_int_slice_typecheck",
			parityProgramCodeCell(execop.CONDSELCHK().Serialize()),
			[]any{int64(-1), int64(11), cell.BeginCell().MustStoreUInt(0xAA, 8).ToSlice()},
		},
	}

	caseNames := make([]string, 0, len(cases))
	for _, tc := range cases {
		caseNames = append(caseNames, tc.name)
	}
	requireNamedParityCases(t, "stack_gap", caseNames, requiredStackGapCaseNames)

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runDifferentialFuzzCase(t, differentialFuzzCase{
				seed:     uint64(i),
				family:   "stack_gap",
				op:       tc.name,
				code:     tc.code,
				stack:    tc.stack,
				gasLimit: differentialFuzzGasLimit,
				c7:       prepareCrossTestC7(nil, testEmptyCell()),
			})
		})
	}
}

func TestTVMDifferentialFuzzStackOpcodeSpaceGapManifest(t *testing.T) {
	cases := buildStackOpsOpcodeSpaceCases()
	if len(cases) != expectedStackOpcodeSpaceGapCases {
		t.Fatalf("stack opcode-space parity case count mismatch: got %d want %d", len(cases), expectedStackOpcodeSpaceGapCases)
	}

	names := make([]string, 0, len(cases))
	for _, tc := range cases {
		names = append(names, tc.name)
	}
	requireNamedParityCases(t, "stack_opcode_space_gap", names, requiredStackOpcodeSpaceGapCaseNames)
}

func TestTVMDifferentialFuzzStackDynamicDepthGapOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	cases := buildStackOpsDynamicAndDepthEdgeCases()
	if len(cases) != expectedStackDynamicDepthGapCases {
		t.Fatalf("stack dynamic-depth parity case count mismatch: got %d want %d", len(cases), expectedStackDynamicDepthGapCases)
	}

	names := make([]string, 0, len(cases))
	for _, tc := range cases {
		names = append(names, tc.name)
	}
	requireNamedParityCases(t, "stack_dynamic_depth_gap", names, requiredStackDynamicDepthGapCaseNames)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runStackOpDepthParityCase(t, tc)
		})
	}
}

func TestTVMDifferentialFuzzActionGapOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	empty := testEmptyCell()
	allLabels := make([]string, 0, 32)
	for mode := 0; mode < 9; mode++ {
		t.Run(fmt.Sprintf("mode_%02d", mode), func(t *testing.T) {
			g := &parityProgramGenerator{
				r:    rand.New(rand.NewSource(int64(mode))),
				seed: append([]byte(nil), crossTestSeed...),
				regD: [2]*cell.Cell{
					empty,
					empty,
				},
			}
			if !g.emitActionRegisterOp(mode) {
				t.Fatalf("mode %d was not emitted", mode)
			}
			allLabels = append(allLabels, g.trace...)

			runDifferentialFuzzCase(t, differentialFuzzCase{
				seed:   uint64(mode),
				family: "action_gap",
				op:     strings.Join(g.trace, " -> "),
				code:   parityProgramCodeFromBuilders(t, g.ops...),
				c7:     feeTestC7(t),
			})
		})
	}
	requireParityTraceLabels(t, "action_gap", allLabels, requiredActionGapTraceLabels)
}

func TestTVMDifferentialFuzzActionModeGapOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	withActionHash := func(ops ...*cell.Builder) *cell.Cell {
		ops = append(ops, execop.PUSHCTR(5).Serialize(), cellsliceop.HASHCU().Serialize())
		return parityProgramCodeCell(ops...)
	}
	withSendMsgActionHash := func() *cell.Cell {
		return parityProgramCodeCell(
			funcsop.SENDMSG().Serialize(),
			stackop.DROP().Serialize(),
			execop.PUSHCTR(5).Serialize(),
			cellsliceop.HASHCU().Serialize(),
		)
	}

	internalMsg := parityProgramSendMsgInternalMessage()
	userFwdFeeMsg := parityProgramSendMsgUserFwdFeeInternalMessage()
	externalOutMsg := parityProgramSendMsgExternalOutMessage()
	sendMsgPrices := tlb.ConfigMsgForwardPrices{LumpPrice: 1}
	sendMsgFeeC7 := makeTonopsTestC7(t, tonopsTestC7Config{
		ConfigRoot:     tonopsCrossSendMsgConfig(t, 13, sendMsgPrices),
		UnpackedConfig: tonopsCrossSendMsgUnpackedConfig(t, sendMsgPrices),
	})
	code := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	extra := cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell()
	hash := new(big.Int).SetBytes(bytes.Repeat([]byte{0x22}, 32))

	cases := []struct {
		name  string
		code  *cell.Cell
		stack []any
		c7    tuple.Tuple
	}{
		{"rawreserve_mode_0", withActionHash(funcsop.RAWRESERVE().Serialize()), []any{int64(777), int64(0)}, tuple.Tuple{}},
		{"rawreserve_mode_2", withActionHash(funcsop.RAWRESERVE().Serialize()), []any{int64(777), int64(2)}, tuple.Tuple{}},
		{"rawreserve_mode_16", withActionHash(funcsop.RAWRESERVE().Serialize()), []any{int64(777), int64(16)}, tuple.Tuple{}},
		{"rawreservex_mode_0", withActionHash(funcsop.RAWRESERVEX().Serialize()), []any{int64(888), extra, int64(0)}, tuple.Tuple{}},
		{"rawreservex_mode_16", withActionHash(funcsop.RAWRESERVEX().Serialize()), []any{int64(888), extra, int64(16)}, tuple.Tuple{}},
		{"setlibcode_mode_0", withActionHash(funcsop.SETLIBCODE().Serialize()), []any{code, int64(0)}, tuple.Tuple{}},
		{"setlibcode_mode_2", withActionHash(funcsop.SETLIBCODE().Serialize()), []any{code, int64(2)}, tuple.Tuple{}},
		{"setlibcode_mode_16", withActionHash(funcsop.SETLIBCODE().Serialize()), []any{code, int64(16)}, tuple.Tuple{}},
		{"changelib_mode_0", withActionHash(funcsop.CHANGELIB().Serialize()), []any{hash, int64(0)}, tuple.Tuple{}},
		{"changelib_mode_1", withActionHash(funcsop.CHANGELIB().Serialize()), []any{hash, int64(1)}, tuple.Tuple{}},
		{"changelib_mode_16", withActionHash(funcsop.CHANGELIB().Serialize()), []any{hash, int64(16)}, tuple.Tuple{}},
		{"sendmsg_mode64_incomingvalue", withSendMsgActionHash(), []any{internalMsg, int64(64)}, tuple.Tuple{}},
		{"sendmsg_mode128_balance", withSendMsgActionHash(), []any{internalMsg, int64(128)}, tuple.Tuple{}},
		{"sendmsg_extout_fee_only", withSendMsgActionHash(), []any{externalOutMsg, int64(1024)}, tuple.Tuple{}},
		{"sendmsg_user_fwd_fee_lower_bound_gv13", parityProgramCodeCell(funcsop.SENDMSG().Serialize()), []any{userFwdFeeMsg, int64(1024)}, sendMsgFeeC7},
		{
			"action_chain_sendrawmsg_then_rawreserve_hash",
			withActionHash(funcsop.SENDRAWMSG().Serialize(), funcsop.RAWRESERVE().Serialize()),
			[]any{int64(777), int64(0), internalMsg, int64(1)},
			tuple.Tuple{},
		},
		{
			"action_chain_setcode_then_setlibcode_hash",
			withActionHash(funcsop.SETCODE().Serialize(), funcsop.SETLIBCODE().Serialize()),
			[]any{extra, int64(1), code},
			tuple.Tuple{},
		},
	}

	caseNames := make([]string, 0, len(cases))
	for _, tc := range cases {
		caseNames = append(caseNames, tc.name)
	}
	requireNamedParityCases(t, "action_mode_gap", caseNames, requiredActionModeGapCaseNames)

	c7 := feeTestC7(t)
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			caseC7 := c7
			if !tc.c7.IsNull() {
				caseC7 = tc.c7
			}
			runDifferentialFuzzCase(t, differentialFuzzCase{
				seed:     uint64(i),
				family:   "action_mode_gap",
				op:       tc.name,
				code:     tc.code,
				stack:    tc.stack,
				gasLimit: differentialFuzzGasLimit,
				c7:       caseC7,
			})
		})
	}
}

func TestTVMDifferentialFuzzActionErrorGapOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	sendMsg := parityProgramSendMsgInternalMessage()
	codeCell := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	extra := cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell()

	cases := []struct {
		name  string
		code  *cell.Cell
		stack []any
	}{
		{
			name:  "rawreserve_negative_amount_range",
			code:  parityProgramCodeCell(funcsop.RAWRESERVE().Serialize()),
			stack: []any{big.NewInt(-1), int64(0)},
		},
		{
			name:  "rawreservex_negative_amount_range",
			code:  parityProgramCodeCell(funcsop.RAWRESERVEX().Serialize()),
			stack: []any{big.NewInt(-1), extra, int64(0)},
		},
		{
			name:  "rawreserve_missing_mode_underflow",
			code:  parityProgramCodeCell(funcsop.RAWRESERVE().Serialize()),
			stack: []any{int64(777)},
		},
		{
			name:  "rawreservex_missing_extra_underflow",
			code:  parityProgramCodeCell(funcsop.RAWRESERVEX().Serialize()),
			stack: []any{int64(777), int64(0)},
		},
		{
			name:  "setlibcode_missing_mode_underflow",
			code:  parityProgramCodeCell(funcsop.SETLIBCODE().Serialize()),
			stack: []any{codeCell},
		},
		{
			name:  "setlibcode_invalid_mode_range",
			code:  parityProgramCodeCell(funcsop.SETLIBCODE().Serialize()),
			stack: []any{codeCell, int64(4)},
		},
		{
			name:  "changelib_missing_mode_underflow",
			code:  parityProgramCodeCell(funcsop.CHANGELIB().Serialize()),
			stack: []any{big.NewInt(1)},
		},
		{
			name:  "changelib_negative_hash_range",
			code:  parityProgramCodeCell(funcsop.CHANGELIB().Serialize()),
			stack: []any{big.NewInt(-1), int64(0)},
		},
		{
			name:  "sendmsg_missing_mode_underflow",
			code:  parityProgramCodeCell(funcsop.SENDMSG().Serialize()),
			stack: []any{sendMsg},
		},
		{
			name:  "sendmsg_invalid_mode_range",
			code:  parityProgramCodeCell(funcsop.SENDMSG().Serialize()),
			stack: []any{sendMsg, int64(256)},
		},
		{
			name:  "sendrawmsg_missing_mode_underflow",
			code:  parityProgramCodeCell(funcsop.SENDRAWMSG().Serialize()),
			stack: []any{sendMsg},
		},
		{
			name:  "sendrawmsg_invalid_mode_range",
			code:  parityProgramCodeCell(funcsop.SENDRAWMSG().Serialize()),
			stack: []any{sendMsg, int64(256)},
		},
	}

	caseNames := make([]string, 0, len(cases))
	for _, tc := range cases {
		caseNames = append(caseNames, tc.name)
	}
	requireNamedParityCases(t, "action_error_gap", caseNames, requiredActionErrorGapCaseNames)

	c7 := feeTestC7(t)
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runDifferentialFuzzCase(t, differentialFuzzCase{
				seed:   uint64(i),
				family: "action_error_gap",
				op:     tc.name,
				code:   tc.code,
				stack:  tc.stack,
				c7:     c7,
			})
		})
	}
}

func TestTVMDifferentialFuzzDictContinuationGapOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	signedRoot := parityProgramDictContinuationRoot(true, 70)
	unsignedRoot := parityProgramDictContinuationRoot(false, 71)
	prefixRoot := parityProgramPrefixContinuationRoot(72)
	prefixInput := parityProgramKeySlice(0b1011, 4)
	prefixMissInput := parityProgramKeySlice(0b0111, 4)
	empty := testEmptyCell()

	cases := []struct {
		name  string
		code  *cell.Cell
		stack []any
	}{
		{"dictigetjmp_hit", parityProgramCodeCell(parityProgramRawOp(0xF4A0, 16)), []any{int64(3), signedRoot, int64(8)}},
		{"dictugetjmp_hit", parityProgramCodeCell(parityProgramRawOp(0xF4A1, 16)), []any{int64(3), unsignedRoot, int64(8)}},
		{"dictigetexec_hit", parityProgramCodeCell(parityProgramRawOp(0xF4A2, 16)), []any{int64(3), signedRoot, int64(8)}},
		{"dictugetexec_hit", parityProgramCodeCell(parityProgramRawOp(0xF4A3, 16)), []any{int64(3), unsignedRoot, int64(8)}},
		{"dictigetjmpz_hit", parityProgramCodeCell(parityProgramRawOp(0xF4BC, 16)), []any{int64(3), signedRoot, int64(8)}},
		{"dictugetjmpz_hit", parityProgramCodeCell(parityProgramRawOp(0xF4BD, 16)), []any{int64(3), unsignedRoot, int64(8)}},
		{"dictigetexecz_hit", parityProgramCodeCell(parityProgramRawOp(0xF4BE, 16)), []any{int64(3), signedRoot, int64(8)}},
		{"dictugetexecz_hit", parityProgramCodeCell(parityProgramRawOp(0xF4BF, 16)), []any{int64(3), unsignedRoot, int64(8)}},
		{"pfxdictgetjmp_hit", parityProgramCodeCell(parityProgramRawOp(0xF4AA, 16)), []any{prefixInput.Copy(), prefixRoot, int64(4)}},
		{"pfxdictgetexec_hit", parityProgramCodeCell(parityProgramRawOp(0xF4AB, 16)), []any{prefixInput.Copy(), prefixRoot, int64(4)}},
		{"pfxdictswitch_hit", parityProgramCodeCell(dictop.PFXDICTSWITCH(prefixRoot, 4).Serialize()), []any{prefixInput.Copy()}},
		{
			"dictigetjmp_miss_falls_through",
			parityProgramCodeCell(
				parityProgramRawOp(0xF4A0, 16),
				stackop.PUSHINT(big.NewInt(73)).Serialize(),
			),
			[]any{int64(4), signedRoot, int64(8)},
		},
		{
			"dictugetexec_miss_falls_through",
			parityProgramCodeCell(
				parityProgramRawOp(0xF4A3, 16),
				stackop.PUSHINT(big.NewInt(74)).Serialize(),
			),
			[]any{int64(4), unsignedRoot, int64(8)},
		},
		{"dictigetjmpz_miss_keeps_index", parityProgramCodeCell(parityProgramRawOp(0xF4BC, 16)), []any{int64(4), signedRoot, int64(8)}},
		{"dictugetexecz_miss_keeps_index", parityProgramCodeCell(parityProgramRawOp(0xF4BF, 16)), []any{int64(4), unsignedRoot, int64(8)}},
		{"pfxdictgetjmp_miss_keeps_input", parityProgramCodeCell(parityProgramRawOp(0xF4AA, 16)), []any{prefixMissInput.Copy(), prefixRoot, int64(4)}},
		{"pfxdictgetjmp_nil_root_keeps_input", parityProgramCodeCell(parityProgramRawOp(0xF4AA, 16)), []any{prefixInput.Copy(), (*cell.Cell)(nil), int64(4)}},
		{"pfxdictgetexec_miss_cell_underflow", parityProgramCodeCell(parityProgramRawOp(0xF4AB, 16)), []any{prefixMissInput.Copy(), prefixRoot, int64(4)}},
		{"pfxdictgetexec_nil_root_cell_underflow", parityProgramCodeCell(parityProgramRawOp(0xF4AB, 16)), []any{prefixInput.Copy(), (*cell.Cell)(nil), int64(4)}},
		{"pfxdictswitch_miss_keeps_input", parityProgramCodeCell(dictop.PFXDICTSWITCH(prefixRoot, 4).Serialize()), []any{prefixMissInput.Copy()}},
		{"pfxdictswitch_nil_root_flag_with_ref_underflow", parityProgramPfxDictSwitchNilRootWithRef(4), []any{prefixInput.Copy()}},
	}

	caseNames := make([]string, 0, len(cases))
	for _, tc := range cases {
		caseNames = append(caseNames, tc.name)
	}
	requireNamedParityCases(t, "dict_continuation_gap", caseNames, requiredDictContinuationGapCaseNames)

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runDifferentialFuzzCase(t, differentialFuzzCase{
				seed:     uint64(i),
				family:   "dict_continuation_gap",
				op:       tc.name,
				code:     tc.code,
				stack:    tc.stack,
				gasLimit: differentialFuzzGasLimit,
				c7:       prepareCrossTestC7(nil, empty),
			})
		})
	}
}

func TestTVMDifferentialFuzzExecGapOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	body := func(builders ...*cell.Builder) *cell.Cell {
		return parityProgramCodeCell(builders...)
	}
	invalidRefBody := body(parityProgramRawOp(0xF2F6, 16))
	intBody := func(v int64) *cell.Cell {
		return body(stackop.PUSHINT(big.NewInt(v)).Serialize())
	}
	emptyBody := body()
	retAltBody := body(execop.RETALT().Serialize())
	sumBody := body(mathop.SUM().Serialize())
	sumReturnBody := body(
		mathop.SUM().Serialize(),
		execop.RETARGS(1).Serialize(),
	)
	retVarArgsBody := body(
		stackop.PUSHINT(big.NewInt(123)).Serialize(),
		stackop.PUSHINT(big.NewInt(1)).Serialize(),
		execop.RETVARARGS().Serialize(),
	)
	retVarArgsAllBody := body(
		stackop.PUSHINT(big.NewInt(123)).Serialize(),
		stackop.PUSHINT(big.NewInt(456)).Serialize(),
		stackop.PUSHINT(big.NewInt(-1)).Serialize(),
		execop.RETVARARGS().Serialize(),
	)
	returnOverflowBody := body(
		stackop.PUSHINT(big.NewInt(1)).Serialize(),
		execop.RETURNARGS(0).Serialize(),
		execop.RET().Serialize(),
	)
	falseCondBody := intBody(0)
	push99Body := intBody(99)
	dropThen7Body := body(
		stackop.DROP().Serialize(),
		stackop.PUSHINT(big.NewInt(7)).Serialize(),
	)
	ifBitJumpBody := body(
		stackop.DROP().Serialize(),
		stackop.PUSHINT(big.NewInt(99)).Serialize(),
	)
	throwHandlerBody := body(
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.PUSHINT(big.NewInt(123)).Serialize(),
	)
	tryArgsIncBody := body(
		mathop.INC().Serialize(),
		execop.RETARGS(1).Serialize(),
	)
	tryArgsThrowArgBody := body(parityProgramRawOp(0xF2C82A, 24))
	whileCounterCondBody := body(
		stackop.DUP().Serialize(),
		stackop.PUSHINT(big.NewInt(0)).Serialize(),
		mathop.GREATER().Serialize(),
	)
	whileCounterBody := body(mathop.DEC().Serialize())
	controlCellA := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	controlCellB := cell.BeginCell().MustStoreUInt(0xC0DE, 16).EndCell()
	pushC4Body := body(execop.PUSHCTR(4).Serialize())
	customC7Inner := tuple.NewTupleSized(4)
	mustSetTupleValue(t, &customC7Inner, 3, int64(777))
	customC7 := tuple.NewTupleValue(customC7Inner)
	otherC7Inner := tuple.NewTupleSized(4)
	mustSetTupleValue(t, &otherC7Inner, 3, int64(888))
	otherC7 := tuple.NewTupleValue(otherC7Inner)
	pushC7ParamBody := body(funcsop.GETPARAM(3).Serialize())

	cases := []struct {
		name  string
		code  *cell.Cell
		stack []any
	}{
		{
			name: "jmpxdata_valid_remaining_slice",
			code: body(
				stackop.PUSHCONT(body(cellsliceop.SBITS().Serialize())).Serialize(),
				execop.JMPXDATA().Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
		},
		{
			name: "jmpxargs_two_params",
			code: body(
				stackop.PUSHCONT(sumBody).Serialize(),
				execop.JMPXARGS(2).Serialize(),
			),
			stack: []any{int64(5), int64(6)},
		},
		{
			name: "setcontargs_capture_all",
			code: body(
				stackop.PUSHCONT(sumBody).Serialize(),
				execop.SETCONTARGS(2, -1).Serialize(),
				execop.EXECUTE().Serialize(),
			),
			stack: []any{int64(5), int64(6)},
		},
		{
			name: "callxargs_trim",
			code: body(
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.CALLXARGS(2, 1).Serialize(),
			),
			stack: []any{int64(11), int64(22)},
		},
		{
			name: "callxargsp_all_returns",
			code: body(
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.CALLXARGSP(1).Serialize(),
			),
			stack: []any{int64(55)},
		},
		{
			name: "callxargs_sum_then_tail",
			code: body(
				stackop.PUSHCONT(sumReturnBody).Serialize(),
				execop.CALLXARGS(2, 1).Serialize(),
				stackop.PUSHINT(big.NewInt(9)).Serialize(),
			),
			stack: []any{int64(5), int64(6)},
		},
		{
			name: "callxargs_params_underflow",
			code: body(
				stackop.PUSHCONT(sumBody).Serialize(),
				execop.CALLXARGS(2, 1).Serialize(),
			),
			stack: []any{int64(5)},
		},
		{
			name: "callxvarargs_dynamic",
			code: body(
				stackop.PUSHCONT(emptyBody).Serialize(),
				stackop.XCHG0(2).Serialize(),
				stackop.XCHG0(1).Serialize(),
				execop.CALLXVARARGS().Serialize(),
			),
			stack: []any{int64(11), int64(22), int64(2), int64(1)},
		},
		{
			name: "callxvarargs_dynamic_params_and_returns",
			code: body(
				stackop.PUSHCONT(sumReturnBody).Serialize(),
				stackop.PUSHINT(big.NewInt(2)).Serialize(),
				stackop.PUSHINT(big.NewInt(1)).Serialize(),
				execop.CALLXVARARGS().Serialize(),
				stackop.PUSHINT(big.NewInt(9)).Serialize(),
			),
			stack: []any{int64(5), int64(6)},
		},
		{
			name: "jmpxvarargs_dynamic_params",
			code: body(
				stackop.PUSHCONT(sumReturnBody).Serialize(),
				stackop.PUSHINT(big.NewInt(2)).Serialize(),
				execop.JMPXVARARGS().Serialize(),
			),
			stack: []any{int64(5), int64(6)},
		},
		{
			name: "jmpxargs_params_underflow",
			code: body(
				stackop.PUSHCONT(sumBody).Serialize(),
				execop.JMPXARGS(2).Serialize(),
			),
			stack: []any{int64(5)},
		},
		{
			name: "retvarargs_trim",
			code: body(execop.RETVARARGS().Serialize()),
			stack: []any{
				int64(11),
				int64(22),
				int64(1),
			},
		},
		{
			name: "retvarargs_from_called_continuation",
			code: body(
				stackop.PUSHCONT(retVarArgsBody).Serialize(),
				execop.CALLXARGS(0, 1).Serialize(),
				stackop.PUSHINT(big.NewInt(66)).Serialize(),
			),
		},
		{
			name: "retvarargs_all_from_called_continuation",
			code: body(
				stackop.PUSHCONT(retVarArgsAllBody).Serialize(),
				execop.CALLXARGSP(0).Serialize(),
				stackop.PUSHINT(big.NewInt(66)).Serialize(),
			),
		},
		{
			name: "returnvarargs_dynamic_count",
			code: body(
				stackop.PUSHINT(big.NewInt(1)).Serialize(),
				execop.RETURNVARARGS().Serialize(),
				stackop.PUSHINT(big.NewInt(44)).Serialize(),
				execop.RET().Serialize(),
			),
			stack: []any{
				int64(11),
				int64(22),
				int64(33),
			},
		},
		{
			name: "returnvarargs_zero_count_moves_all",
			code: body(
				stackop.PUSHINT(big.NewInt(0)).Serialize(),
				execop.RETURNVARARGS().Serialize(),
				stackop.PUSHINT(big.NewInt(44)).Serialize(),
				execop.RET().Serialize(),
			),
			stack: []any{
				int64(11),
				int64(22),
				int64(33),
			},
		},
		{
			name: "returnvarargs_bad_count_range",
			code: body(
				stackop.PUSHINT(big.NewInt(256)).Serialize(),
				execop.RETURNVARARGS().Serialize(),
				stackop.PUSHINT(big.NewInt(44)).Serialize(),
			),
			stack: []any{
				int64(11),
			},
		},
		{
			name: "returnargs_fixed_count",
			code: body(
				execop.RETURNARGS(2).Serialize(),
				stackop.PUSHINT(big.NewInt(44)).Serialize(),
				execop.RET().Serialize(),
			),
			stack: []any{
				int64(11),
				int64(22),
				int64(33),
			},
		},
		{
			name: "returnargs_closure_overflow",
			code: body(
				stackop.PUSHCONT(returnOverflowBody).Serialize(),
				execop.CALLXARGS(0, 0).Serialize(),
				stackop.PUSHINT(big.NewInt(44)).Serialize(),
			),
		},
		{
			name: "retbool_return_branch",
			code: body(
				execop.RETBOOL().Serialize(),
				stackop.PUSHINT(big.NewInt(44)).Serialize(),
			),
			stack: []any{int64(-1)},
		},
		{
			name: "retbool_alt_branch",
			code: body(
				execop.RETBOOL().Serialize(),
				stackop.PUSHINT(big.NewInt(44)).Serialize(),
			),
			stack: []any{int64(0)},
		},
		{
			name: "retdata_remaining_slice",
			code: body(
				execop.RETDATA().Serialize(),
				stackop.PUSHINT(big.NewInt(44)).Serialize(),
			),
		},
		{
			name: "ifret_taken",
			code: body(
				execop.IFRET().Serialize(),
				stackop.PUSHINT(big.NewInt(44)).Serialize(),
			),
			stack: []any{int64(-1)},
		},
		{
			name: "ifret_continue",
			code: body(
				execop.IFRET().Serialize(),
				stackop.PUSHINT(big.NewInt(44)).Serialize(),
			),
			stack: []any{int64(0)},
		},
		{
			name: "ifnotret_taken",
			code: body(
				execop.IFNOTRET().Serialize(),
				stackop.PUSHINT(big.NewInt(44)).Serialize(),
			),
			stack: []any{int64(0)},
		},
		{
			name: "ifnotret_continue",
			code: body(
				execop.IFNOTRET().Serialize(),
				stackop.PUSHINT(big.NewInt(44)).Serialize(),
			),
			stack: []any{int64(-1)},
		},
		{
			name: "try_catches_throw",
			code: body(
				stackop.PUSHCONT(body(parityProgramRawOp(0xF22A, 16))).Serialize(),
				stackop.PUSHCONT(throwHandlerBody).Serialize(),
				execop.TRY().Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
		},
		{
			name: "tryargs_returns_value",
			code: body(
				stackop.PUSHCONT(tryArgsIncBody).Serialize(),
				stackop.PUSHCONT(throwHandlerBody).Serialize(),
				execop.TRYARGS(1, 1).Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(41)},
		},
		{
			name: "tryargs_catches_throwarg",
			code: body(
				stackop.PUSHCONT(tryArgsThrowArgBody).Serialize(),
				stackop.PUSHCONT(throwHandlerBody).Serialize(),
				execop.TRYARGS(1, 1).Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(777)},
		},
		{
			name: "tryargs_zero_retvals_caught_throwarg",
			code: body(
				stackop.PUSHCONT(body(
					stackop.PUSHINT(big.NewInt(888)).Serialize(),
					parityProgramRawOp(0xF2C82A, 24),
				)).Serialize(),
				stackop.PUSHCONT(body(
					stackop.DROP().Serialize(),
					stackop.DROP().Serialize(),
					stackop.PUSHINT(big.NewInt(77)).Serialize(),
				)).Serialize(),
				execop.TRYARGS(0, 0).Serialize(),
				stackop.PUSHINT(big.NewInt(9)).Serialize(),
			),
			stack: []any{int64(11), int64(22)},
		},
		{
			name: "throw_short_uncaught",
			code: body(parityProgramRawOp(0xF22A, 16)),
		},
		{
			name: "throw_long_uncaught",
			code: body(parityProgramRawOp(0xF2C055, 24)),
		},
		{
			name:  "throwarg_long_uncaught",
			code:  body(parityProgramRawOp(0xF2C955, 24)),
			stack: []any{int64(1234)},
		},
		{
			name:  "throwif_taken",
			code:  body(parityProgramRawOp(0xF240|42, 16)),
			stack: []any{int64(-1)},
		},
		{
			name:  "throwifnot_taken",
			code:  body(parityProgramRawOp(0xF280|42, 16)),
			stack: []any{int64(0)},
		},
		{
			name:  "throwargif_taken",
			code:  body(parityProgramRawOp(0xF2D82A, 24)),
			stack: []any{int64(1234), int64(-1)},
		},
		{
			name:  "throwargif_skip",
			code:  body(parityProgramRawOp(0xF2D82A, 24), stackop.PUSHINT(big.NewInt(7)).Serialize()),
			stack: []any{int64(1234), int64(0)},
		},
		{
			name:  "throwargifnot_taken",
			code:  body(parityProgramRawOp(0xF2E82A, 24)),
			stack: []any{int64(1234), int64(0)},
		},
		{
			name:  "throwargifnot_skip",
			code:  body(parityProgramRawOp(0xF2E82A, 24), stackop.PUSHINT(big.NewInt(7)).Serialize()),
			stack: []any{int64(1234), int64(-1)},
		},
		{
			name:  "throwany_uncaught",
			code:  body(parityProgramRawOp(0xF2F0, 16)),
			stack: []any{int64(42)},
		},
		{
			name:  "throwargany_uncaught",
			code:  body(parityProgramRawOp(0xF2F1, 16)),
			stack: []any{int64(1234), int64(42)},
		},
		{
			name:  "throwanyif_taken",
			code:  body(parityProgramRawOp(0xF2F2, 16)),
			stack: []any{int64(42), int64(-1)},
		},
		{
			name:  "throwanyif_skip",
			code:  body(parityProgramRawOp(0xF2F2, 16), stackop.PUSHINT(big.NewInt(7)).Serialize()),
			stack: []any{int64(42), int64(0)},
		},
		{
			name:  "throwanyif_skip_invalid_exc_range",
			code:  body(parityProgramRawOp(0xF2F2, 16), stackop.PUSHINT(big.NewInt(7)).Serialize()),
			stack: []any{int64(0x10000), int64(0)},
		},
		{
			name:  "throwarganyif_taken",
			code:  body(parityProgramRawOp(0xF2F3, 16)),
			stack: []any{int64(1234), int64(42), int64(-1)},
		},
		{
			name:  "throwarganyif_skip",
			code:  body(parityProgramRawOp(0xF2F3, 16), stackop.PUSHINT(big.NewInt(7)).Serialize()),
			stack: []any{int64(1234), int64(42), int64(0)},
		},
		{
			name:  "throwarganyif_skip_invalid_exc_range",
			code:  body(parityProgramRawOp(0xF2F3, 16), stackop.PUSHINT(big.NewInt(7)).Serialize()),
			stack: []any{int64(1234), int64(0x10000), int64(0)},
		},
		{
			name:  "throwanyifnot_taken",
			code:  body(parityProgramRawOp(0xF2F4, 16)),
			stack: []any{int64(42), int64(0)},
		},
		{
			name:  "throwanyifnot_skip",
			code:  body(parityProgramRawOp(0xF2F4, 16), stackop.PUSHINT(big.NewInt(7)).Serialize()),
			stack: []any{int64(42), int64(-1)},
		},
		{
			name:  "throwanyifnot_skip_invalid_exc_range",
			code:  body(parityProgramRawOp(0xF2F4, 16), stackop.PUSHINT(big.NewInt(7)).Serialize()),
			stack: []any{int64(0x10000), int64(-1)},
		},
		{
			name:  "throwarganyifnot_taken",
			code:  body(parityProgramRawOp(0xF2F5, 16)),
			stack: []any{int64(1234), int64(42), int64(0)},
		},
		{
			name:  "throwarganyifnot_skip",
			code:  body(parityProgramRawOp(0xF2F5, 16), stackop.PUSHINT(big.NewInt(7)).Serialize()),
			stack: []any{int64(1234), int64(42), int64(-1)},
		},
		{
			name:  "throwarganyifnot_skip_invalid_exc_range",
			code:  body(parityProgramRawOp(0xF2F5, 16), stackop.PUSHINT(big.NewInt(7)).Serialize()),
			stack: []any{int64(1234), int64(0x10000), int64(-1)},
		},
		{
			name: "execute_empty_stack_underflow",
			code: body(execop.EXECUTE().Serialize()),
		},
		{
			name:  "execute_non_cont_typecheck",
			code:  body(execop.EXECUTE().Serialize()),
			stack: []any{int64(1)},
		},
		{
			name: "callcc_pushes_current_continuation",
			code: body(
				stackop.PUSHCONT(dropThen7Body).Serialize(),
				execop.CALLCC().Serialize(),
			),
		},
		{
			name: "callccargs_preserves_arg",
			code: body(
				stackop.PUSHCONT(dropThen7Body).Serialize(),
				execop.CALLCCARGS(1, 2).Serialize(),
			),
			stack: []any{int64(55)},
		},
		{
			name: "callccvarargs_dynamic",
			code: body(
				stackop.PUSHCONT(dropThen7Body).Serialize(),
				stackop.XCHG0(2).Serialize(),
				stackop.XCHG0(1).Serialize(),
				execop.CALLCCVARARGS().Serialize(),
			),
			stack: []any{int64(55), int64(1), int64(2)},
		},
		{
			name: "callccvarargs_pass_all_return_all",
			code: body(
				stackop.PUSHCONT(body(execop.EXECUTE().Serialize())).Serialize(),
				stackop.PUSHINT(big.NewInt(-1)).Serialize(),
				stackop.PUSHINT(big.NewInt(-1)).Serialize(),
				execop.CALLCCVARARGS().Serialize(),
				stackop.PUSHINT(big.NewInt(70)).Serialize(),
			),
			stack: []any{int64(41), int64(42)},
		},
		{
			name: "callccvarargs_bad_retvals_range",
			code: body(
				stackop.PUSHCONT(emptyBody).Serialize(),
				stackop.PUSHINT(big.NewInt(0)).Serialize(),
				stackop.PUSHINT(big.NewInt(255)).Serialize(),
				execop.CALLCCVARARGS().Serialize(),
				stackop.PUSHINT(big.NewInt(70)).Serialize(),
			),
		},
		{
			name: "setcontvarargs_execute",
			code: body(
				stackop.PUSHCONT(emptyBody).Serialize(),
				stackop.PUSHINT(big.NewInt(1)).Serialize(),
				stackop.PUSHINT(big.NewInt(0)).Serialize(),
				execop.SETCONTVARARGS().Serialize(),
				execop.EXECUTE().Serialize(),
			),
			stack: []any{int64(66)},
		},
		{
			name: "setnumvarargs_execute",
			code: body(
				stackop.PUSHCONT(emptyBody).Serialize(),
				stackop.PUSHINT(big.NewInt(1)).Serialize(),
				execop.SETNUMVARARGS().Serialize(),
				execop.EXECUTE().Serialize(),
			),
			stack: []any{int64(77)},
		},
		{
			name: "bless_execute",
			code: body(
				stackop.PUSHSLICE(intBody(80).MustBeginParse()).Serialize(),
				execop.BLESS().Serialize(),
				execop.EXECUTE().Serialize(),
			),
		},
		{
			name: "blessargs_execute",
			code: body(
				stackop.PUSHINT(big.NewInt(81)).Serialize(),
				stackop.PUSHSLICE(emptyBody.MustBeginParse()).Serialize(),
				execop.BLESSARGS(1, 0).Serialize(),
				execop.EXECUTE().Serialize(),
			),
		},
		{
			name: "pushctr_c0_drop",
			code: body(
				execop.PUSHCTR(0).Serialize(),
				stackop.DROP().Serialize(),
			),
		},
		{
			name: "setcontctrmany_c6_range",
			code: body(
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.SETCONTCTRMANY(1<<6).Serialize(),
			),
		},
		{
			name: "setcontctr_success",
			code: body(
				stackop.PUSHREF(controlCellA).Serialize(),
				stackop.PUSHCONT(pushC4Body).Serialize(),
				execop.SETCONTCTR(4).Serialize(),
				stackop.PUSHREF(controlCellB).Serialize(),
				execop.POPCTR(4).Serialize(),
				execop.EXECUTE().Serialize(),
			),
		},
		{
			name: "setcontctrx_success",
			code: body(
				stackop.PUSHREF(controlCellA).Serialize(),
				stackop.PUSHCONT(pushC4Body).Serialize(),
				stackop.PUSHINT(big.NewInt(4)).Serialize(),
				execop.SETCONTCTRX().Serialize(),
				stackop.PUSHREF(controlCellB).Serialize(),
				execop.POPCTR(4).Serialize(),
				execop.EXECUTE().Serialize(),
			),
		},
		{
			name: "setcontctrmany_success",
			code: body(
				stackop.PUSHREF(controlCellA).Serialize(),
				execop.POPCTR(4).Serialize(),
				stackop.PUSHCONT(pushC4Body).Serialize(),
				execop.SETCONTCTRMANY(1<<4).Serialize(),
				stackop.PUSHREF(controlCellB).Serialize(),
				execop.POPCTR(4).Serialize(),
				execop.EXECUTE().Serialize(),
			),
		},
		{
			name: "setcontctrmanyx_success",
			code: body(
				stackop.PUSHREF(controlCellA).Serialize(),
				execop.POPCTR(4).Serialize(),
				stackop.PUSHCONT(pushC4Body).Serialize(),
				stackop.PUSHINT(big.NewInt(1<<4)).Serialize(),
				execop.SETCONTCTRMANYX().Serialize(),
				stackop.PUSHREF(controlCellB).Serialize(),
				execop.POPCTR(4).Serialize(),
				execop.EXECUTE().Serialize(),
			),
		},
		{
			name: "setcontctrmanyx_c6_range",
			code: body(
				stackop.PUSHCONT(emptyBody).Serialize(),
				stackop.PUSHINT(big.NewInt(1<<6)).Serialize(),
				execop.SETCONTCTRMANYX().Serialize(),
			),
		},
		{
			name: "popctrx_c7_tuple_roundtrip",
			code: body(
				execop.POPCTRX().Serialize(),
				execop.PUSHCTR(7).Serialize(),
			),
			stack: []any{tuple.NewTupleValue(big.NewInt(11), big.NewInt(22)), int64(7)},
		},
		{
			name:  "pushctrx_c6_range",
			code:  body(execop.PUSHCTRX().Serialize()),
			stack: []any{int64(6)},
		},
		{
			name:  "popctrx_c6_range",
			code:  body(execop.POPCTRX().Serialize()),
			stack: []any{cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell(), int64(6)},
		},
		{
			name:  "popctr_c0_noncont_typecheck",
			code:  body(execop.POPCTR(0).Serialize()),
			stack: []any{int64(1)},
		},
		{
			name:  "popsavectr_c0_noncont_typecheck",
			code:  body(execop.POPSAVECTR(0).Serialize()),
			stack: []any{int64(1)},
		},
		{
			name: "setretctr_c4_restore_on_ret",
			code: body(
				stackop.PUSHCONT(pushC4Body).Serialize(),
				execop.POPCTR(0).Serialize(),
				stackop.PUSHREF(controlCellA).Serialize(),
				execop.SETRETCTR(4).Serialize(),
				stackop.PUSHREF(controlCellB).Serialize(),
				execop.POPCTR(4).Serialize(),
				execop.RET().Serialize(),
			),
		},
		{
			name: "setaltctr_c4_restore_on_retalt",
			code: body(
				stackop.PUSHCONT(pushC4Body).Serialize(),
				execop.POPCTR(1).Serialize(),
				stackop.PUSHREF(controlCellA).Serialize(),
				execop.SETALTCTR(4).Serialize(),
				stackop.PUSHREF(controlCellB).Serialize(),
				execop.POPCTR(4).Serialize(),
				execop.RETALT().Serialize(),
			),
		},
		{
			name: "savectr_c4_restore_on_ret",
			code: body(
				stackop.PUSHREF(controlCellA).Serialize(),
				execop.POPCTR(4).Serialize(),
				stackop.PUSHCONT(pushC4Body).Serialize(),
				execop.POPCTR(0).Serialize(),
				execop.SAVECTR(4).Serialize(),
				stackop.PUSHREF(controlCellB).Serialize(),
				execop.POPCTR(4).Serialize(),
				execop.RET().Serialize(),
			),
		},
		{
			name: "savealtctr_c4_restore_on_retalt",
			code: body(
				stackop.PUSHREF(controlCellA).Serialize(),
				execop.POPCTR(4).Serialize(),
				stackop.PUSHCONT(pushC4Body).Serialize(),
				execop.POPCTR(1).Serialize(),
				execop.SAVEALTCTR(4).Serialize(),
				stackop.PUSHREF(controlCellB).Serialize(),
				execop.POPCTR(4).Serialize(),
				execop.RETALT().Serialize(),
			),
		},
		{
			name: "savebothctr_c4_restore_on_ret",
			code: body(
				stackop.PUSHREF(controlCellA).Serialize(),
				execop.POPCTR(4).Serialize(),
				stackop.PUSHCONT(pushC4Body).Serialize(),
				execop.POPCTR(0).Serialize(),
				stackop.PUSHCONT(push99Body).Serialize(),
				execop.POPCTR(1).Serialize(),
				execop.SAVEBOTHCTR(4).Serialize(),
				stackop.PUSHREF(controlCellB).Serialize(),
				execop.POPCTR(4).Serialize(),
				execop.RET().Serialize(),
			),
		},
		{
			name: "savebothctr_c4_restore_on_retalt",
			code: body(
				stackop.PUSHREF(controlCellA).Serialize(),
				execop.POPCTR(4).Serialize(),
				stackop.PUSHCONT(push99Body).Serialize(),
				execop.POPCTR(0).Serialize(),
				stackop.PUSHCONT(pushC4Body).Serialize(),
				execop.POPCTR(1).Serialize(),
				execop.SAVEBOTHCTR(4).Serialize(),
				stackop.PUSHREF(controlCellB).Serialize(),
				execop.POPCTR(4).Serialize(),
				execop.RETALT().Serialize(),
			),
		},
		{
			name: "popsavectr_c4_restore_on_ret",
			code: body(
				stackop.PUSHREF(controlCellA).Serialize(),
				execop.POPCTR(4).Serialize(),
				stackop.PUSHCONT(pushC4Body).Serialize(),
				execop.POPCTR(0).Serialize(),
				stackop.PUSHREF(controlCellB).Serialize(),
				execop.POPSAVECTR(4).Serialize(),
				execop.RET().Serialize(),
			),
		},
		{
			name: "setretctr_c7_restore_on_ret",
			code: body(
				stackop.PUSHCONT(pushC7ParamBody).Serialize(),
				execop.POPCTR(0).Serialize(),
				execop.SETRETCTR(7).Serialize(),
				execop.RET().Serialize(),
			),
			stack: []any{customC7},
		},
		{
			name: "setaltctr_c7_restore_on_retalt",
			code: body(
				stackop.PUSHCONT(pushC7ParamBody).Serialize(),
				execop.POPCTR(1).Serialize(),
				execop.SETALTCTR(7).Serialize(),
				execop.RETALT().Serialize(),
			),
			stack: []any{customC7},
		},
		{
			name: "savectr_c7_restore_on_ret",
			code: body(
				stackop.PUSHCONT(pushC7ParamBody).Serialize(),
				execop.POPCTR(0).Serialize(),
				execop.POPCTR(7).Serialize(),
				execop.SAVECTR(7).Serialize(),
				execop.POPCTR(7).Serialize(),
				execop.RET().Serialize(),
			),
			stack: []any{otherC7, customC7},
		},
		{
			name: "savealtctr_c7_restore_on_retalt",
			code: body(
				stackop.PUSHCONT(pushC7ParamBody).Serialize(),
				execop.POPCTR(1).Serialize(),
				execop.POPCTR(7).Serialize(),
				execop.SAVEALTCTR(7).Serialize(),
				execop.POPCTR(7).Serialize(),
				execop.RETALT().Serialize(),
			),
			stack: []any{otherC7, customC7},
		},
		{
			name: "savebothctr_c7_restore_on_ret",
			code: body(
				stackop.PUSHCONT(pushC7ParamBody).Serialize(),
				execop.POPCTR(0).Serialize(),
				stackop.PUSHCONT(push99Body).Serialize(),
				execop.POPCTR(1).Serialize(),
				execop.POPCTR(7).Serialize(),
				execop.SAVEBOTHCTR(7).Serialize(),
				execop.POPCTR(7).Serialize(),
				execop.RET().Serialize(),
			),
			stack: []any{otherC7, customC7},
		},
		{
			name: "savebothctr_c7_restore_on_retalt",
			code: body(
				stackop.PUSHCONT(push99Body).Serialize(),
				execop.POPCTR(0).Serialize(),
				stackop.PUSHCONT(pushC7ParamBody).Serialize(),
				execop.POPCTR(1).Serialize(),
				execop.POPCTR(7).Serialize(),
				execop.SAVEBOTHCTR(7).Serialize(),
				execop.POPCTR(7).Serialize(),
				execop.RETALT().Serialize(),
			),
			stack: []any{otherC7, customC7},
		},
		{
			name: "popsavectr_c7_restore_on_ret",
			code: body(
				stackop.PUSHCONT(pushC7ParamBody).Serialize(),
				execop.POPCTR(0).Serialize(),
				execop.POPCTR(7).Serialize(),
				execop.POPSAVECTR(7).Serialize(),
				execop.RET().Serialize(),
			),
			stack: []any{otherC7, customC7},
		},
		{
			name: "blessvarargs_execute",
			code: body(
				stackop.PUSHINT(big.NewInt(1)).Serialize(),
				stackop.PUSHINT(big.NewInt(0)).Serialize(),
				execop.BLESSVARARGS().Serialize(),
				execop.EXECUTE().Serialize(),
			),
			stack: []any{
				int64(66),
				intBody(7).MustBeginParse(),
			},
		},
		{
			name: "calldict_short",
			code: body(
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.POPCTR(3).Serialize(),
				execop.CALLDICT(5).Serialize(),
			),
		},
		{
			name: "calldict_long",
			code: body(
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.POPCTR(3).Serialize(),
				execop.CALLDICT(300).Serialize(),
			),
		},
		{
			name: "jmpdict",
			code: body(
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.POPCTR(3).Serialize(),
				execop.JMPDICT(8).Serialize(),
			),
		},
		{
			name: "preparedict_execute",
			code: body(
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.POPCTR(3).Serialize(),
				execop.PREPAREDICT(9).Serialize(),
				execop.EXECUTE().Serialize(),
			),
		},
		{
			name: "booleval_false_branch",
			code: body(
				stackop.PUSHCONT(body(execop.RETALT().Serialize())).Serialize(),
				execop.BOOLEVAL().Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
		},
		{
			name: "booleval_true_branch_resumes_tail",
			code: body(
				stackop.PUSHCONT(body(execop.RET().Serialize())).Serialize(),
				execop.BOOLEVAL().Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
		},
		{
			name: "while_first_condition_false",
			code: body(
				stackop.PUSHCONT(falseCondBody).Serialize(),
				stackop.PUSHCONT(push99Body).Serialize(),
				execop.WHILE().Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
		},
		{
			name: "jmpx_pushes_value",
			code: body(
				stackop.PUSHCONT(intBody(110)).Serialize(),
				execop.JMPX().Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
		},
		{
			name: "if_taken_call",
			code: body(
				stackop.PUSHCONT(intBody(101)).Serialize(),
				execop.IF().Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(-1)},
		},
		{
			name: "if_not_taken",
			code: body(
				stackop.PUSHCONT(intBody(101)).Serialize(),
				execop.IF().Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(0)},
		},
		{
			name: "ifnot_taken_call",
			code: body(
				stackop.PUSHCONT(intBody(102)).Serialize(),
				execop.IFNOT().Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(0)},
		},
		{
			name: "ifnot_not_taken",
			code: body(
				stackop.PUSHCONT(intBody(102)).Serialize(),
				execop.IFNOT().Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(-1)},
		},
		{
			name: "ifelse_true_branch",
			code: body(
				stackop.PUSHCONT(intBody(103)).Serialize(),
				stackop.PUSHCONT(intBody(104)).Serialize(),
				execop.IFELSE().Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(-1)},
		},
		{
			name: "ifelse_false_branch",
			code: body(
				stackop.PUSHCONT(intBody(103)).Serialize(),
				stackop.PUSHCONT(intBody(104)).Serialize(),
				execop.IFELSE().Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(0)},
		},
		{
			name: "ifjmp_taken",
			code: body(
				stackop.PUSHCONT(intBody(105)).Serialize(),
				execop.IFJMP().Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(-1)},
		},
		{
			name: "ifjmp_not_taken",
			code: body(
				stackop.PUSHCONT(intBody(105)).Serialize(),
				execop.IFJMP().Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(0)},
		},
		{
			name: "ifnotjmp_taken",
			code: body(
				stackop.PUSHCONT(intBody(106)).Serialize(),
				execop.IFNOTJMP().Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(0)},
		},
		{
			name: "ifnotjmp_not_taken",
			code: body(
				stackop.PUSHCONT(intBody(106)).Serialize(),
				execop.IFNOTJMP().Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(-1)},
		},
		{
			name: "repeat_two_iterations",
			code: body(
				stackop.PUSHCONT(intBody(111)).Serialize(),
				execop.REPEAT().Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(2)},
		},
		{
			name: "while_one_iteration_then_false",
			code: body(
				stackop.PUSHCONT(whileCounterCondBody).Serialize(),
				stackop.PUSHCONT(whileCounterBody).Serialize(),
				execop.WHILE().Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(1)},
		},
		{
			name: "whileend_one_iteration_then_false",
			code: body(
				stackop.PUSHCONT(whileCounterCondBody).Serialize(),
				execop.WHILEEND().Serialize(),
				mathop.DEC().Serialize(),
			),
			stack: []any{int64(1)},
		},
		{
			name: "again_throw_bounded",
			code: body(
				stackop.PUSHCONT(body(parityProgramRawOp(0xF22A, 16))).Serialize(),
				execop.AGAIN().Serialize(),
			),
		},
		{
			name: "againend_throw_bounded",
			code: body(
				execop.AGAINEND().Serialize(),
				parityProgramRawOp(0xF22A, 16),
			),
		},
		{
			name: "until_one_iteration",
			code: body(
				stackop.PUSHCONT(body(
					stackop.PUSHINT(big.NewInt(107)).Serialize(),
					stackop.PUSHINT(big.NewInt(-1)).Serialize(),
				)).Serialize(),
				execop.UNTIL().Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
		},
		{
			name: "untilend_one_iteration",
			code: body(
				execop.UNTILEND().Serialize(),
				stackop.PUSHINT(big.NewInt(108)).Serialize(),
				stackop.PUSHINT(big.NewInt(-1)).Serialize(),
			),
		},
		{
			name: "repeatend_one_iteration",
			code: body(
				execop.REPEATEND().Serialize(),
				stackop.PUSHINT(big.NewInt(109)).Serialize(),
			),
			stack: []any{int64(1)},
		},
		{
			name: "repeat_negative_count_skips",
			code: body(
				stackop.PUSHCONT(push99Body).Serialize(),
				execop.REPEAT().Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(-1)},
		},
		{
			name: "ifbitjmp_not_taken_preserves_int",
			code: body(
				stackop.PUSHCONT(ifBitJumpBody).Serialize(),
				execop.IFBITJMP(1).Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(0)},
		},
		{
			name: "ifbitjmp_taken",
			code: body(
				stackop.PUSHCONT(ifBitJumpBody).Serialize(),
				execop.IFBITJMP(1).Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(2)},
		},
		{
			name: "ifbitjmp_high_negative_taken",
			code: body(
				stackop.PUSHCONT(ifBitJumpBody).Serialize(),
				execop.IFBITJMP(31).Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(-1)},
		},
		{
			name: "ifnbitjmp_taken",
			code: body(
				stackop.PUSHCONT(ifBitJumpBody).Serialize(),
				execop.IFNBITJMP(1).Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(0)},
		},
		{
			name: "ifnbitjmp_not_taken_preserves_int",
			code: body(
				stackop.PUSHCONT(ifBitJumpBody).Serialize(),
				execop.IFNBITJMP(1).Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(2)},
		},
		{
			name: "ifnbitjmp_high_negative_not_taken",
			code: body(
				stackop.PUSHCONT(ifBitJumpBody).Serialize(),
				execop.IFNBITJMP(31).Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(-1)},
		},
		{
			name: "pushrefcont_execute",
			code: body(
				stackop.PUSHREFCONT(intBody(81)).Serialize(),
				execop.EXECUTE().Serialize(),
			),
		},
		{
			name: "callref_pushes_value",
			code: body(execop.CALLREF(intBody(82)).Serialize()),
		},
		{
			name: "jmpref_pushes_value",
			code: body(execop.JMPREF(intBody(83)).Serialize()),
		},
		{
			name: "jmprefdata_exposes_remaining_code",
			code: body(
				execop.JMPREFDATA(ifBitJumpBody).Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
		},
		{
			name: "ifjmpref_taken",
			code: body(
				execop.IFJMPREF(intBody(84)).Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(-1)},
		},
		{
			name: "ifjmpref_not_taken",
			code: body(
				execop.IFJMPREF(intBody(85)).Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(0)},
		},
		{
			name: "ifnotjmpref_taken",
			code: body(
				execop.IFNOTJMPREF(intBody(86)).Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(0)},
		},
		{
			name: "ifnotjmpref_not_taken",
			code: body(
				execop.IFNOTJMPREF(intBody(86)).Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(-1)},
		},
		{
			name: "ifbitjmpref_taken",
			code: body(
				execop.IFBITJMPREF(1, ifBitJumpBody).Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(2)},
		},
		{
			name: "ifbitjmpref_not_taken",
			code: body(
				execop.IFBITJMPREF(1, ifBitJumpBody).Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(0)},
		},
		{
			name: "ifnbitjmpref_taken",
			code: body(
				execop.IFNBITJMPREF(1, ifBitJumpBody).Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(0)},
		},
		{
			name: "ifnbitjmpref_not_taken",
			code: body(
				execop.IFNBITJMPREF(1, ifBitJumpBody).Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(2)},
		},
		{
			name: "ifref_skips_invalid_ref_false",
			code: body(
				execop.IFREF(invalidRefBody).Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(0)},
		},
		{
			name: "ifnotref_skips_invalid_ref_true",
			code: body(
				execop.IFNOTREF(invalidRefBody).Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(-1)},
		},
		{
			name: "ifjmpref_skips_invalid_ref_false",
			code: body(
				execop.IFJMPREF(invalidRefBody).Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(0)},
		},
		{
			name: "ifnotjmpref_skips_invalid_ref_true",
			code: body(
				execop.IFNOTJMPREF(invalidRefBody).Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(-1)},
		},
		{
			name: "ifrefelse_skips_invalid_ref_false_branch",
			code: body(
				stackop.PUSHCONT(intBody(87)).Serialize(),
				execop.IFREFELSE(invalidRefBody).Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(0)},
		},
		{
			name: "ifelseref_skips_invalid_ref_true_branch",
			code: body(
				stackop.PUSHCONT(intBody(88)).Serialize(),
				execop.IFELSEREF(invalidRefBody).Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(-1)},
		},
		{
			name: "ifrefelseref_true_branch",
			code: body(
				execop.IFREFELSEREF(intBody(89), intBody(90)).Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(-1)},
		},
		{
			name: "ifbitjmpref_skips_invalid_ref_not_taken",
			code: body(
				execop.IFBITJMPREF(1, invalidRefBody).Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(0)},
		},
		{
			name: "atexit_runs",
			code: body(
				stackop.PUSHCONT(intBody(90)).Serialize(),
				execop.ATEXIT().Serialize(),
			),
		},
		{
			name: "atexitalt_runs",
			code: body(
				stackop.PUSHCONT(intBody(91)).Serialize(),
				execop.ATEXITALT().Serialize(),
				execop.RETALT().Serialize(),
			),
		},
		{
			name: "setexitalt_runs",
			code: body(
				stackop.PUSHCONT(intBody(92)).Serialize(),
				execop.SETEXITALT().Serialize(),
				execop.RETALT().Serialize(),
			),
		},
		{
			name: "thenret_runs",
			code: body(
				stackop.PUSHCONT(intBody(93)).Serialize(),
				execop.ATEXIT().Serialize(),
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.THENRET().Serialize(),
				execop.EXECUTE().Serialize(),
			),
		},
		{
			name: "thenretalt_runs",
			code: body(
				stackop.PUSHCONT(intBody(94)).Serialize(),
				execop.POPCTR(1).Serialize(),
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.THENRETALT().Serialize(),
				execop.EXECUTE().Serialize(),
			),
		},
		{
			name: "booland_composes_return_continuation",
			code: body(
				stackop.PUSHCONT(emptyBody).Serialize(),
				stackop.PUSHCONT(intBody(95)).Serialize(),
				execop.BOOLAND().Serialize(),
				execop.EXECUTE().Serialize(),
			),
		},
		{
			name: "boolor_composes_alt_continuation",
			code: body(
				stackop.PUSHCONT(retAltBody).Serialize(),
				stackop.PUSHCONT(intBody(96)).Serialize(),
				execop.BOOLOR().Serialize(),
				execop.EXECUTE().Serialize(),
			),
		},
		{
			name: "composboth_composes_return_and_alt",
			code: body(
				stackop.PUSHCONT(retAltBody).Serialize(),
				stackop.PUSHCONT(intBody(97)).Serialize(),
				execop.COMPOSBOTH().Serialize(),
				execop.EXECUTE().Serialize(),
			),
		},
		{
			name: "invert_ret",
			code: body(
				execop.INVERT().Serialize(),
				execop.RET().Serialize(),
			),
		},
		{
			name: "samealt_copies_return_to_alt",
			code: body(
				stackop.PUSHCONT(intBody(98)).Serialize(),
				execop.POPCTR(0).Serialize(),
				execop.SAMEALT().Serialize(),
				execop.RETALT().Serialize(),
			),
		},
		{
			name: "samealt_copy_is_independent",
			code: body(
				stackop.PUSHCONT(intBody(98)).Serialize(),
				execop.POPCTR(0).Serialize(),
				execop.SAMEALT().Serialize(),
				stackop.PUSHCONT(intBody(99)).Serialize(),
				execop.POPCTR(0).Serialize(),
				execop.RETALT().Serialize(),
			),
		},
		{
			name: "samealtsave_preserves_previous_alt",
			code: body(
				stackop.PUSHCONT(retAltBody).Serialize(),
				execop.POPCTR(0).Serialize(),
				stackop.PUSHCONT(intBody(99)).Serialize(),
				execop.POPCTR(1).Serialize(),
				execop.SAMEALTSAVE().Serialize(),
				execop.RET().Serialize(),
			),
		},
		{
			name: "repeatbrk_break",
			code: body(
				stackop.PUSHCONT(retAltBody).Serialize(),
				execop.REPEATBRK().Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(3)},
		},
		{
			name: "repeatendbrk_break",
			code: body(
				execop.REPEATENDBRK().Serialize(),
				execop.RETALT().Serialize(),
			),
			stack: []any{int64(3)},
		},
		{
			name: "untilbrk_break",
			code: body(
				stackop.PUSHCONT(retAltBody).Serialize(),
				execop.UNTILBRK().Serialize(),
				stackop.PUSHINT(big.NewInt(8)).Serialize(),
			),
		},
		{
			name: "untilendbrk_break",
			code: body(
				execop.UNTILENDBRK().Serialize(),
				execop.RETALT().Serialize(),
			),
		},
		{
			name: "whilebrk_break",
			code: body(
				stackop.PUSHCONT(intBody(1)).Serialize(),
				stackop.PUSHCONT(retAltBody).Serialize(),
				execop.WHILEBRK().Serialize(),
				stackop.PUSHINT(big.NewInt(9)).Serialize(),
			),
		},
		{
			name: "whileendbrk_break",
			code: body(
				stackop.PUSHCONT(intBody(1)).Serialize(),
				execop.WHILEENDBRK().Serialize(),
				execop.RETALT().Serialize(),
			),
		},
		{
			name: "againbrk_break",
			code: body(
				stackop.PUSHCONT(retAltBody).Serialize(),
				execop.AGAINBRK().Serialize(),
			),
		},
		{
			name: "againendbrk_break",
			code: body(
				execop.AGAINENDBRK().Serialize(),
				execop.RETALT().Serialize(),
			),
		},
		{
			name: "ifretalt_continue",
			code: body(
				execop.IFRETALT().Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(0)},
		},
		{
			name: "ifretalt_taken",
			code: body(
				execop.IFRETALT().Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(-1)},
		},
		{
			name: "ifnotretalt_continue",
			code: body(
				execop.IFNOTRETALT().Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(-1)},
		},
		{
			name: "ifnotretalt_taken",
			code: body(
				execop.IFNOTRETALT().Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(0)},
		},
	}

	caseNames := make([]string, 0, len(cases))
	for _, tc := range cases {
		caseNames = append(caseNames, tc.name)
	}
	requireNamedParityCases(t, "exec_gap", caseNames, requiredExecGapCaseNames)

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runDifferentialFuzzCase(t, differentialFuzzCase{
				seed:     uint64(i),
				family:   "exec_gap",
				op:       tc.name,
				code:     tc.code,
				stack:    tc.stack,
				gasLimit: differentialFuzzGasLimit,
				c7:       prepareCrossTestC7(nil, testEmptyCell()),
			})
		})
	}
}

func TestTVMDifferentialFuzzExecRefDecodeGapOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	cases := []cellParityCase{
		{name: "callref_missing_ref_invalid", code: parityProgramCodeCell(parityProgramRawOp(0xDB3C, 16)), exit: vmerr.CodeInvalidOpcode},
		{name: "jmpref_missing_ref_invalid", code: parityProgramCodeCell(parityProgramRawOp(0xDB3D, 16)), exit: vmerr.CodeInvalidOpcode},
		{name: "jmprefdata_missing_ref_invalid", code: parityProgramCodeCell(parityProgramRawOp(0xDB3E, 16)), exit: vmerr.CodeInvalidOpcode},
		{name: "ifref_missing_ref_invalid", code: parityProgramCodeCell(parityProgramRawOp(0xE300, 16)), exit: vmerr.CodeInvalidOpcode},
		{name: "ifnotref_missing_ref_invalid", code: parityProgramCodeCell(parityProgramRawOp(0xE301, 16)), exit: vmerr.CodeInvalidOpcode},
		{name: "ifjmpref_missing_ref_invalid", code: parityProgramCodeCell(parityProgramRawOp(0xE302, 16)), exit: vmerr.CodeInvalidOpcode},
		{name: "ifnotjmpref_missing_ref_invalid", code: parityProgramCodeCell(parityProgramRawOp(0xE303, 16)), exit: vmerr.CodeInvalidOpcode},
		{name: "ifrefelse_missing_ref_invalid", code: parityProgramCodeCell(parityProgramRawOp(0xE30D, 16)), exit: vmerr.CodeInvalidOpcode},
		{name: "ifelseref_missing_ref_invalid", code: parityProgramCodeCell(parityProgramRawOp(0xE30E, 16)), exit: vmerr.CodeInvalidOpcode},
		{name: "ifrefelseref_missing_refs_invalid", code: parityProgramCodeCell(parityProgramRawOp(0xE30F, 16)), exit: vmerr.CodeInvalidOpcode},
		{name: "ifbitjmpref_missing_ref_invalid", code: parityProgramCodeCell(parityProgramRawOp(0xE3C1, 16)), exit: vmerr.CodeInvalidOpcode},
		{name: "ifnbitjmpref_missing_ref_invalid", code: parityProgramCodeCell(parityProgramRawOp(0xE3E1, 16)), exit: vmerr.CodeInvalidOpcode},
	}

	names := make([]string, 0, len(cases))
	for _, tc := range cases {
		names = append(names, tc.name)
	}
	requireNamedParityCases(t, "exec_ref_decode_gap", names, requiredExecRefDecodeGapCaseNames)

	runCellParityCases(t, cases)
}

func TestTVMDifferentialFuzzRunVMGapOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	childC7 := tuple.NewTupleValue(tuple.NewTupleValue(big.NewInt(11), big.NewInt(22)))
	returnedData := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	returnedActions := cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell()
	initialData := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()

	body := func(builders ...*cell.Builder) *cell.Cell {
		return parityProgramCodeCell(builders...)
	}

	cases := []struct {
		name  string
		code  *cell.Cell
		stack []any
	}{
		{
			name: "runvm_c7_return_one",
			code: body(execop.RUNVM(16 | 256).Serialize()),
			stack: []any{
				int64(0),
				body(funcsop.GETPARAM(1).Serialize()).MustBeginParse(),
				int64(1),
				childC7,
			},
		},
		{
			name: "runvmx_gas_bounds_return_one",
			code: body(execop.RUNVMX().Serialize()),
			stack: []any{
				int64(0),
				body(
					stackop.PUSHINT(big.NewInt(7)).Serialize(),
					stackop.PUSHINT(big.NewInt(8)).Serialize(),
				).MustBeginParse(),
				int64(1),
				int64(10_000),
				int64(20_000),
				int64(8 | 64 | 256),
			},
		},
		{
			name: "runvm_data_actions_and_gas",
			code: body(execop.RUNVM(4 | 8 | 32).Serialize()),
			stack: []any{
				int64(0),
				body(
					stackop.PUSHREF(returnedData).Serialize(),
					execop.POPCTR(4).Serialize(),
					stackop.PUSHREF(returnedActions).Serialize(),
					execop.POPCTR(5).Serialize(),
				).MustBeginParse(),
				initialData,
				int64(10_000),
			},
		},
		{
			name: "runvm_full_inputs_return_data_actions_gas",
			code: body(execop.RUNVM(4 | 8 | 16 | 32 | 256).Serialize()),
			stack: []any{
				int64(0),
				body(
					funcsop.GETPARAM(1).Serialize(),
					stackop.PUSHREF(returnedData).Serialize(),
					execop.POPCTR(4).Serialize(),
					stackop.PUSHREF(returnedActions).Serialize(),
					execop.POPCTR(5).Serialize(),
				).MustBeginParse(),
				int64(1),
				initialData,
				childC7,
				int64(10_000),
			},
		},
		{
			name:  "runvm_mode512_rangecheck_no_stack",
			code:  body(execop.RUNVM(512).Serialize()),
			stack: nil,
		},
		{
			name:  "runvmx_mode512_rangecheck_no_child",
			code:  body(execop.RUNVMX().Serialize()),
			stack: []any{int64(512)},
		},
	}

	caseNames := make([]string, 0, len(cases))
	for _, tc := range cases {
		caseNames = append(caseNames, tc.name)
	}
	requireNamedParityCases(t, "runvm_gap", caseNames, requiredRunVMGapCaseNames)

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runDifferentialFuzzCase(t, differentialFuzzCase{
				seed:     uint64(i),
				family:   "runvm_gap",
				op:       tc.name,
				code:     tc.code,
				stack:    tc.stack,
				gasLimit: differentialFuzzGasLimit,
				c7:       prepareCrossTestC7(nil, testEmptyCell()),
			})
		})
	}
}

func TestTVMDifferentialFuzzTonFuncGapOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	configValue := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	globalVersionCell, err := tlb.ToCell(&tlb.GlobalVersion{Version: vm.DefaultGlobalVersion})
	if err != nil {
		t.Fatalf("failed to build global version config: %v", err)
	}
	configRoot := mustConfigDictCell(t, map[uint32]*cell.Cell{
		7:                                    configValue,
		uint32(tlb.ConfigParamGlobalVersion): globalVersionCell,
	})

	extraDict := cell.NewDict(32)
	if _, err = extraDict.SetBuilderWithMode(
		cell.BeginCell().MustStoreUInt(7, 32).EndCell(),
		cell.BeginCell().MustStoreVarUInt(12345, 32),
		cell.DictSetModeSet,
	); err != nil {
		t.Fatalf("failed to seed extra balance dict: %v", err)
	}
	malformedExtraDict := cell.NewDict(32)
	if err = malformedExtraDict.SetIntKey(big.NewInt(7), cell.BeginCell().MustStoreUInt(1, 5).EndCell()); err != nil {
		t.Fatalf("failed to seed malformed extra balance dict: %v", err)
	}

	baseC7 := makeTonopsTestC7(t, tonopsTestC7Config{
		ConfigRoot:     configRoot,
		UnpackedConfig: parityProgramTonFuncUnpackedConfig(t),
		IncomingValue:  tuple.NewTupleValue(big.NewInt(555), cell.BeginCell().MustStoreUInt(0xCD, 8).EndCell()),
		Balance:        tuple.NewTupleValue(big.NewInt(123456789), extraDict.AsCell()),
		StorageFees:    tonopsTestStorageFees,
		ExtraParams: map[int]any{
			13: tuple.NewTupleValue(big.NewInt(111), big.NewInt(222), big.NewInt(333)),
			15: int64(444),
			16: int64(555),
			17: makeInMsgParamsTuple(),
		},
	})
	nilExtraBalanceC7 := makeTonopsTestC7(t, tonopsTestC7Config{
		Balance: tuple.NewTupleValue(big.NewInt(123456789), nil),
	})
	malformedExtraBalanceC7 := makeTonopsTestC7(t, tonopsTestC7Config{
		Balance: tuple.NewTupleValue(big.NewInt(123456789), malformedExtraDict.AsCell()),
	})
	feeC7 := feeTestC7(t)

	cases := []struct {
		name  string
		code  *cell.Cell
		stack []any
		c7    tuple.Tuple
	}{
		{"mycode", parityProgramCodeCell(funcsop.MYCODE().Serialize()), nil, baseC7},
		{"incomingvalue", parityProgramCodeCell(funcsop.INCOMINGVALUE().Serialize()), nil, baseC7},
		{"storagefees", parityProgramCodeCell(funcsop.STORAGEFEES().Serialize()), nil, baseC7},
		{"duepayment", parityProgramCodeCell(funcsop.DUEPAYMENT().Serialize()), nil, baseC7},
		{"globalid", parityProgramCodeCell(funcsop.GLOBALID().Serialize()), nil, baseC7},
		{"unpackedconfigtuple", parityProgramCodeCell(funcsop.UNPACKEDCONFIGTUPLE().Serialize()), nil, baseC7},
		{"configdict", parityProgramCodeCell(funcsop.CONFIGDICT().Serialize()), nil, baseC7},
		{"configparam_hit", parityProgramCodeCell(funcsop.CONFIGPARAM().Serialize()), []any{int64(7)}, baseC7},
		{"configoptparam_hit", parityProgramCodeCell(funcsop.CONFIGOPTPARAM().Serialize()), []any{int64(7)}, baseC7},
		{"configoptparam_miss", parityProgramCodeCell(funcsop.CONFIGOPTPARAM().Serialize()), []any{int64(99)}, baseC7},
		{"prevblocksinfotuple", parityProgramCodeCell(funcsop.PREVBLOCKSINFOTUPLE().Serialize()), nil, baseC7},
		{"prevmcblocks", parityProgramCodeCell(funcsop.PREVMCBLOCKS().Serialize()), nil, baseC7},
		{"prevkeyblock", parityProgramCodeCell(funcsop.PREVKEYBLOCK().Serialize()), nil, baseC7},
		{"prevmcblocks_100", parityProgramCodeCell(funcsop.PREVMCBLOCKS_100().Serialize()), nil, baseC7},
		{"getprecompiledgas", parityProgramCodeCell(funcsop.GETPRECOMPILEDGAS().Serialize()), nil, baseC7},
		{"inmsgparams", parityProgramCodeCell(funcsop.INMSGPARAMS().Serialize()), nil, baseC7},
		{"inmsgparam_bounce", parityProgramCodeCell(funcsop.INMSGPARAM(0).Serialize()), nil, baseC7},
		{"inmsgparam_src", parityProgramCodeCell(funcsop.INMSGPARAM(2).Serialize()), nil, baseC7},
		{"inmsgparam_value", parityProgramCodeCell(funcsop.INMSGPARAM(7).Serialize()), nil, baseC7},
		{"inmsgparam_valueextra", parityProgramCodeCell(funcsop.INMSGPARAM(8).Serialize()), nil, baseC7},
		{"inmsgparam_stateinit", parityProgramCodeCell(funcsop.INMSGPARAM(9).Serialize()), nil, baseC7},
		{"inmsgparam_oob_range_error", parityProgramCodeCell(funcsop.INMSGPARAM(10).Serialize()), nil, baseC7},
		{"inmsg_bounce", parityProgramCodeCell(funcsop.INMSG_BOUNCE().Serialize()), nil, baseC7},
		{"inmsg_bounced", parityProgramCodeCell(funcsop.INMSG_BOUNCED().Serialize()), nil, baseC7},
		{"inmsg_src", parityProgramCodeCell(funcsop.INMSG_SRC().Serialize()), nil, baseC7},
		{"inmsg_fwdfee", parityProgramCodeCell(funcsop.INMSG_FWDFEE().Serialize()), nil, baseC7},
		{"inmsg_lt", parityProgramCodeCell(funcsop.INMSG_LT().Serialize()), nil, baseC7},
		{"inmsg_utime", parityProgramCodeCell(funcsop.INMSG_UTIME().Serialize()), nil, baseC7},
		{"inmsg_origvalue", parityProgramCodeCell(funcsop.INMSG_ORIGVALUE().Serialize()), nil, baseC7},
		{"inmsg_value", parityProgramCodeCell(funcsop.INMSG_VALUE().Serialize()), nil, baseC7},
		{"inmsg_valueextra", parityProgramCodeCell(funcsop.INMSG_VALUEEXTRA().Serialize()), nil, baseC7},
		{"inmsg_stateinit", parityProgramCodeCell(funcsop.INMSG_STATEINIT().Serialize()), nil, baseC7},
		{"getstoragefee", parityProgramCodeCell(funcsop.GETSTORAGEFEE().Serialize()), []any{int64(2), int64(3), int64(10), int64(0)}, feeC7},
		{"getgasfee", parityProgramCodeCell(funcsop.GETGASFEE().Serialize()), []any{int64(250), int64(0)}, feeC7},
		{"getforwardfee", parityProgramCodeCell(funcsop.GETFORWARDFEE().Serialize()), []any{int64(2), int64(8), int64(0)}, feeC7},
		{"getoriginalfwdfee", parityProgramCodeCell(funcsop.GETORIGINALFWDFEE().Serialize()), []any{big.NewInt(3200), int64(0)}, feeC7},
		{"getforwardfeesimple", parityProgramCodeCell(funcsop.GETFORWARDFEESIMPLE().Serialize()), []any{int64(2), int64(8), int64(0)}, feeC7},
		{"getgasfeesimple", parityProgramCodeCell(funcsop.GETGASFEESIMPLE().Serialize()), []any{int64(250), int64(0)}, feeC7},
		{"getstoragefee_masterchain", parityProgramCodeCell(funcsop.GETSTORAGEFEE().Serialize()), []any{int64(2), int64(3), int64(10), int64(-1)}, feeC7},
		{"getgasfee_masterchain", parityProgramCodeCell(funcsop.GETGASFEE().Serialize()), []any{int64(250), int64(-1)}, feeC7},
		{"getforwardfee_masterchain", parityProgramCodeCell(funcsop.GETFORWARDFEE().Serialize()), []any{int64(2), int64(8), int64(-1)}, feeC7},
		{"getoriginalfwdfee_masterchain", parityProgramCodeCell(funcsop.GETORIGINALFWDFEE().Serialize()), []any{big.NewInt(3200), int64(-1)}, feeC7},
		{"getforwardfeesimple_masterchain", parityProgramCodeCell(funcsop.GETFORWARDFEESIMPLE().Serialize()), []any{int64(2), int64(8), int64(-1)}, feeC7},
		{"getgasfeesimple_masterchain", parityProgramCodeCell(funcsop.GETGASFEESIMPLE().Serialize()), []any{int64(250), int64(-1)}, feeC7},
		{"getextrabalance_hit", parityProgramCodeCell(funcsop.GETEXTRABALANCE().Serialize()), []any{int64(7)}, baseC7},
		{"getextrabalance_miss", parityProgramCodeCell(funcsop.GETEXTRABALANCE().Serialize()), []any{int64(9)}, baseC7},
		{"getextrabalance_repeated_hit", parityProgramCodeCell(funcsop.GETEXTRABALANCE().Serialize(), stackop.PUSHINT(big.NewInt(7)).Serialize(), funcsop.GETEXTRABALANCE().Serialize()), []any{int64(7)}, baseC7},
		{"getextrabalance_nil_dict", parityProgramCodeCell(funcsop.GETEXTRABALANCE().Serialize()), []any{int64(7)}, nilExtraBalanceC7},
		{"getextrabalance_malformed_value", parityProgramCodeCell(funcsop.GETEXTRABALANCE().Serialize()), []any{int64(7)}, malformedExtraBalanceC7},
	}

	versionedCases := []struct {
		name          string
		code          *cell.Cell
		stack         []any
		globalVersion int
	}{
		{"getgasfee_missing_gas_v8_partial_pop", parityProgramCodeCell(funcsop.GETGASFEE().Serialize()), []any{int64(0)}, 8},
		{"getgasfee_missing_gas_v9_precheck", parityProgramCodeCell(funcsop.GETGASFEE().Serialize()), []any{int64(0)}, 9},
		{"getstoragefee_missing_delta_v8_partial_pop", parityProgramCodeCell(funcsop.GETSTORAGEFEE().Serialize()), []any{int64(0)}, 8},
		{"getstoragefee_missing_delta_v9_precheck", parityProgramCodeCell(funcsop.GETSTORAGEFEE().Serialize()), []any{int64(0)}, 9},
		{"getforwardfee_missing_bits_v8_partial_pop", parityProgramCodeCell(funcsop.GETFORWARDFEE().Serialize()), []any{int64(0)}, 8},
		{"getforwardfee_missing_bits_v9_precheck", parityProgramCodeCell(funcsop.GETFORWARDFEE().Serialize()), []any{int64(0)}, 9},
		{"getoriginalfwdfee_missing_fee_v8_partial_pop", parityProgramCodeCell(funcsop.GETORIGINALFWDFEE().Serialize()), []any{int64(0)}, 8},
		{"getoriginalfwdfee_missing_fee_v9_precheck", parityProgramCodeCell(funcsop.GETORIGINALFWDFEE().Serialize()), []any{int64(0)}, 9},
		{"getgasfeesimple_missing_gas_v8_partial_pop", parityProgramCodeCell(funcsop.GETGASFEESIMPLE().Serialize()), []any{int64(0)}, 8},
		{"getgasfeesimple_missing_gas_v9_precheck", parityProgramCodeCell(funcsop.GETGASFEESIMPLE().Serialize()), []any{int64(0)}, 9},
		{"getforwardfeesimple_missing_bits_v8_partial_pop", parityProgramCodeCell(funcsop.GETFORWARDFEESIMPLE().Serialize()), []any{int64(0)}, 8},
		{"getforwardfeesimple_missing_bits_v9_precheck", parityProgramCodeCell(funcsop.GETFORWARDFEESIMPLE().Serialize()), []any{int64(0)}, 9},
	}

	caseNames := make([]string, 0, len(cases)+len(versionedCases))
	for _, tc := range cases {
		caseNames = append(caseNames, tc.name)
	}
	for _, tc := range versionedCases {
		caseNames = append(caseNames, tc.name)
	}
	requireNamedParityCases(t, "ton_func_gap", caseNames, requiredTonFuncGapCaseNames)

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runDifferentialFuzzCase(t, differentialFuzzCase{
				seed:     uint64(i),
				family:   "ton_func_gap",
				op:       tc.name,
				code:     tc.code,
				stack:    tc.stack,
				gasLimit: differentialFuzzGasLimit,
				c7:       tc.c7,
			})
		})
	}
	for i, tc := range versionedCases {
		t.Run(tc.name, func(t *testing.T) {
			runDifferentialFuzzCase(t, differentialFuzzWithGlobalVersion(t, differentialFuzzCase{
				seed:     uint64(len(cases) + i),
				family:   "ton_func_gap",
				op:       tc.name,
				code:     tc.code,
				stack:    tc.stack,
				gasLimit: differentialFuzzGasLimit,
			}, tc.globalVersion))
		})
	}
}

func TestTVMDifferentialFuzzMsgAddressGapOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	extAddrSlice := cell.BeginCell().MustStoreAddr(address.NewAddressExt(0, 256, bytes.Repeat([]byte{0x33}, 32))).ToSlice()
	extAddrTailSlice := cell.BeginCell().
		MustStoreBuilder(extAddrSlice.ToBuilder()).
		MustStoreUInt(0xA, 4).
		ToSlice()
	varAddrSlice := cell.BeginCell().MustStoreAddr(address.NewAddressVar(0, tonopsTestAddr.Workchain(), 256, tonopsTestAddr.Data())).ToSlice()
	var20AddrSlice := cell.BeginCell().MustStoreAddr(address.NewAddressVar(0, tonopsTestAddr.Workchain(), 20, []byte{0xDE, 0xAD, 0xB0})).ToSlice()
	shortAnycastVarAddrSlice := cell.BeginCell().
		MustStoreUInt(0b11, 2).
		MustStoreBoolBit(true).
		MustStoreUInt(4, 5).
		MustStoreUInt(0b1010, 4).
		MustStoreUInt(2, 9).
		MustStoreInt(0, 32).
		MustStoreUInt(0, 2).
		ToSlice()
	anycastStdAddrSlice := cell.BeginCell().
		MustStoreUInt(0b10, 2).
		MustStoreBoolBit(true).
		MustStoreUInt(3, 5).
		MustStoreUInt(0b101, 3).
		MustStoreInt(0, 8).
		MustStoreUInt(0, 256).
		ToSlice()
	shortStdAddrSlice := cell.BeginCell().MustStoreUInt(0b10, 2).ToSlice()
	invalidAnycast := cell.BeginCell().
		MustStoreUInt(0b10, 2).
		MustStoreBoolBit(true).
		MustStoreUInt(31, 5).
		MustStoreSlice(bytes.Repeat([]byte{0xFF}, 4), 31).
		MustStoreInt(int64(tonopsTestAddr.Workchain()), 8).
		MustStoreSlice(tonopsTestAddr.Data(), 256).
		ToSlice()

	cases := []struct {
		name             string
		code             *cell.Cell
		stack            []any
		globalVersion    int
		globalVersionSet bool
	}{
		{
			name:  "ldmsgaddrq_short_std",
			code:  parityProgramCodeCell(funcsop.LDMSGADDRQ().Serialize()),
			stack: []any{shortStdAddrSlice.Copy()},
		},
		{
			name:  "ldmsgaddr_ext_success_rest",
			code:  parityProgramCodeCell(funcsop.LDMSGADDR().Serialize()),
			stack: []any{extAddrTailSlice.Copy()},
		},
		{
			name:  "ldstdaddr_std_success_rest",
			code:  parityProgramCodeCell(funcsop.LDSTDADDR().Serialize()),
			stack: []any{parityProgramStdAddrTailSlice()},
		},
		{
			name:  "ldoptstdaddr_std_success_rest",
			code:  parityProgramCodeCell(funcsop.LDOPTSTDADDR().Serialize()),
			stack: []any{parityProgramStdAddrTailSlice()},
		},
		{
			name:  "ldoptstdaddrq_none_success_rest",
			code:  parityProgramCodeCell(funcsop.LDOPTSTDADDRQ().Serialize()),
			stack: []any{parityProgramAddrNoneTailSlice()},
		},
		{
			name:  "ldstdaddrq_var_fail",
			code:  parityProgramCodeCell(funcsop.LDSTDADDRQ().Serialize()),
			stack: []any{varAddrSlice.Copy()},
		},
		{
			name:  "ldoptstdaddrq_short_fail",
			code:  parityProgramCodeCell(funcsop.LDOPTSTDADDRQ().Serialize()),
			stack: []any{cell.BeginCell().MustStoreUInt(1, 1).ToSlice()},
		},
		{
			name:  "parsemsgaddr_std_success",
			code:  parityProgramCodeCell(funcsop.PARSEMSGADDR().Serialize()),
			stack: []any{parityProgramStdAddrSlice()},
		},
		{
			name:  "parsemsgaddr_none_success",
			code:  parityProgramCodeCell(funcsop.PARSEMSGADDR().Serialize()),
			stack: []any{cell.BeginCell().MustStoreUInt(0, 2).ToSlice()},
		},
		{
			name:  "parsemsgaddr_ext_success",
			code:  parityProgramCodeCell(funcsop.PARSEMSGADDR().Serialize()),
			stack: []any{extAddrSlice.Copy()},
		},
		{
			name:             "parsemsgaddr_var_v9_success",
			code:             parityProgramCodeCell(funcsop.PARSEMSGADDR().Serialize()),
			stack:            []any{varAddrSlice.Copy()},
			globalVersion:    9,
			globalVersionSet: true,
		},
		{
			name:             "parsemsgaddr_var_v10_fail",
			code:             parityProgramCodeCell(funcsop.PARSEMSGADDR().Serialize()),
			stack:            []any{varAddrSlice.Copy()},
			globalVersion:    10,
			globalVersionSet: true,
		},
		{
			name:  "parsemsgaddrq_invalid_anycast",
			code:  parityProgramCodeCell(funcsop.PARSEMSGADDRQ().Serialize()),
			stack: []any{invalidAnycast.Copy()},
		},
		{
			name:  "rewritestdaddr_std_success",
			code:  parityProgramCodeCell(funcsop.REWRITESTDADDR().Serialize()),
			stack: []any{parityProgramStdAddrSlice()},
		},
		{
			name:  "rewritevaraddr_std_success",
			code:  parityProgramCodeCell(funcsop.REWRITEVARADDR().Serialize()),
			stack: []any{parityProgramStdAddrSlice()},
		},
		{
			name:  "rewritevaraddr_var20_fail",
			code:  parityProgramCodeCell(funcsop.REWRITEVARADDR().Serialize()),
			stack: []any{var20AddrSlice.Copy()},
		},
		{
			name:             "rewritevaraddrq_short_anycast_var_v9_false",
			code:             parityProgramCodeCell(funcsop.REWRITEVARADDRQ().Serialize()),
			stack:            []any{shortAnycastVarAddrSlice.Copy()},
			globalVersion:    9,
			globalVersionSet: true,
		},
		{
			name:             "rewritevaraddr_short_anycast_var_v9_underflow",
			code:             parityProgramCodeCell(funcsop.REWRITEVARADDR().Serialize()),
			stack:            []any{shortAnycastVarAddrSlice.Copy()},
			globalVersion:    9,
			globalVersionSet: true,
		},
		{
			name:             "rewritestdaddrq_anycast_std_v9_success",
			code:             parityProgramCodeCell(funcsop.REWRITESTDADDRQ().Serialize()),
			stack:            []any{anycastStdAddrSlice.Copy()},
			globalVersion:    9,
			globalVersionSet: true,
		},
		{
			name:             "rewritevaraddrq_anycast_std_v9_success",
			code:             parityProgramCodeCell(funcsop.REWRITEVARADDRQ().Serialize()),
			stack:            []any{anycastStdAddrSlice.Copy()},
			globalVersion:    9,
			globalVersionSet: true,
		},
		{
			name:             "rewritestdaddrq_anycast_std_v10_false",
			code:             parityProgramCodeCell(funcsop.REWRITESTDADDRQ().Serialize()),
			stack:            []any{anycastStdAddrSlice.Copy()},
			globalVersion:    10,
			globalVersionSet: true,
		},
		{
			name:  "rewritestdaddrq_var_fail",
			code:  parityProgramCodeCell(funcsop.REWRITESTDADDRQ().Serialize()),
			stack: []any{varAddrSlice.Copy()},
		},
		{
			name:  "rewritestdaddrq_var20_fail",
			code:  parityProgramCodeCell(funcsop.REWRITESTDADDRQ().Serialize()),
			stack: []any{var20AddrSlice.Copy()},
		},
		{
			name:  "rewritevaraddrq_ext_fail",
			code:  parityProgramCodeCell(funcsop.REWRITEVARADDRQ().Serialize()),
			stack: []any{extAddrSlice.Copy()},
		},
		{
			name:  "ststdaddrq_std_success_status_false",
			code:  parityProgramCodeCell(funcsop.STSTDADDRQ().Serialize()),
			stack: []any{parityProgramStdAddrSlice(), cell.BeginCell()},
		},
		{
			name:  "ststdaddrq_var_fail",
			code:  parityProgramCodeCell(funcsop.STSTDADDRQ().Serialize()),
			stack: []any{varAddrSlice.Copy(), cell.BeginCell()},
		},
		{
			name:  "stoptstdaddrq_none",
			code:  parityProgramCodeCell(funcsop.STOPTSTDADDRQ().Serialize()),
			stack: []any{nil, cell.BeginCell()},
		},
		{
			name:             "stoptstdaddrq_non_slice_v12_restores_value",
			code:             parityProgramCodeCell(funcsop.STOPTSTDADDRQ().Serialize(), stackop.DROP().Serialize(), stackop.DROP().Serialize(), tupleop.ISNULL().Serialize()),
			stack:            []any{int64(100), cell.BeginCell().MustStoreUInt(0xAB, 8)},
			globalVersion:    12,
			globalVersionSet: true,
		},
		{
			name:             "stoptstdaddrq_non_slice_v13_restores_value",
			code:             parityProgramCodeCell(funcsop.STOPTSTDADDRQ().Serialize(), stackop.DROP().Serialize(), stackop.DROP().Serialize(), tupleop.ISNULL().Serialize()),
			stack:            []any{int64(100), cell.BeginCell().MustStoreUInt(0xAB, 8)},
			globalVersion:    13,
			globalVersionSet: true,
		},
		{
			name:             "stoptstdaddrq_non_slice_v14_restores_value",
			code:             parityProgramCodeCell(funcsop.STOPTSTDADDRQ().Serialize(), stackop.DROP().Serialize(), stackop.DROP().Serialize(), tupleop.ISNULL().Serialize()),
			stack:            []any{int64(100), cell.BeginCell().MustStoreUInt(0xAB, 8)},
			globalVersion:    14,
			globalVersionSet: true,
		},
		{
			name:  "stoptstdaddrq_std_success_status_false",
			code:  parityProgramCodeCell(funcsop.STOPTSTDADDRQ().Serialize()),
			stack: []any{parityProgramStdAddrSlice(), cell.BeginCell()},
		},
	}

	caseNames := make([]string, 0, len(cases))
	for _, tc := range cases {
		caseNames = append(caseNames, tc.name)
	}
	requireNamedParityCases(t, "msg_address_gap", caseNames, requiredMsgAddressGapCaseNames)

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fuzzCase := differentialFuzzCase{
				seed:     uint64(i),
				family:   "msg_address_gap",
				op:       tc.name,
				code:     tc.code,
				stack:    tc.stack,
				gasLimit: differentialFuzzGasLimit,
				c7:       makeTonopsTestC7(t, tonopsTestC7Config{}),
			}
			if tc.globalVersionSet {
				fuzzCase.c7 = tuple.Tuple{}
				fuzzCase = differentialFuzzWithGlobalVersion(t, fuzzCase, tc.globalVersion)
			}
			runDifferentialFuzzCase(t, fuzzCase)
		})
	}

}

func TestTVMDifferentialFuzzVarIntGapOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	maxUnsigned31Bytes := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 248), big.NewInt(1))
	signedOverflow31Bytes := new(big.Int).Lsh(big.NewInt(1), 247)

	cases := []struct {
		name  string
		code  *cell.Cell
		stack []any
	}{
		{
			name:  "ldvarint16_zero_len",
			code:  parityProgramCodeCell(funcsop.LDVARINT16().Serialize()),
			stack: []any{cell.BeginCell().MustStoreUInt(0, 4).ToSlice()},
		},
		{
			name: "ldvarint16_negative_minimal",
			code: parityProgramCodeCell(funcsop.LDVARINT16().Serialize()),
			stack: []any{
				cell.BeginCell().
					MustStoreUInt(1, 4).
					MustStoreBigInt(big.NewInt(-2), 8).
					MustStoreUInt(0xA, 4).
					ToSlice(),
			},
		},
		{
			name:  "ldvaruint32_zero_len",
			code:  parityProgramCodeCell(funcsop.LDVARUINT32().Serialize()),
			stack: []any{cell.BeginCell().MustStoreUInt(0, 5).MustStoreUInt(0xA, 4).ToSlice()},
		},
		{
			name: "ldvarint32_negative_two_bytes",
			code: parityProgramCodeCell(funcsop.LDVARINT32().Serialize()),
			stack: []any{
				cell.BeginCell().
					MustStoreUInt(2, 5).
					MustStoreBigInt(big.NewInt(-2), 16).
					MustStoreUInt(0xA, 4).
					ToSlice(),
			},
		},
		{
			name:  "ldvaruint32_short_payload",
			code:  parityProgramCodeCell(funcsop.LDVARUINT32().Serialize()),
			stack: []any{cell.BeginCell().MustStoreUInt(2, 5).MustStoreUInt(0xAB, 8).ToSlice()},
		},
		{
			name:  "stvarint16_zero_len",
			code:  parityProgramCodeCell(funcsop.STVARINT16().Serialize()),
			stack: []any{cell.BeginCell(), int64(0)},
		},
		{
			name:  "stvarint16_negative_minimal",
			code:  parityProgramCodeCell(funcsop.STVARINT16().Serialize()),
			stack: []any{cell.BeginCell(), int64(-2)},
		},
		{
			name:  "stvaruint32_zero_len",
			code:  parityProgramCodeCell(funcsop.STVARUINT32().Serialize()),
			stack: []any{cell.BeginCell(), int64(0)},
		},
		{
			name:  "stvarint32_negative_minimal",
			code:  parityProgramCodeCell(funcsop.STVARINT32().Serialize()),
			stack: []any{cell.BeginCell(), int64(-2)},
		},
		{
			name:  "stvaruint32_max_len",
			code:  parityProgramCodeCell(funcsop.STVARUINT32().Serialize()),
			stack: []any{cell.BeginCell(), maxUnsigned31Bytes},
		},
		{
			name:  "stvaruint32_negative_rangecheck",
			code:  parityProgramCodeCell(funcsop.STVARUINT32().Serialize()),
			stack: []any{cell.BeginCell(), int64(-1)},
		},
		{
			name:  "stvarint32_overflow_rangecheck",
			code:  parityProgramCodeCell(funcsop.STVARINT32().Serialize()),
			stack: []any{cell.BeginCell(), signedOverflow31Bytes},
		},
	}

	caseNames := make([]string, 0, len(cases))
	for _, tc := range cases {
		caseNames = append(caseNames, tc.name)
	}
	requireNamedParityCases(t, "varint_gap", caseNames, requiredVarIntGapCaseNames)

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runDifferentialFuzzCase(t, differentialFuzzCase{
				seed:     uint64(i),
				family:   "varint_gap",
				op:       tc.name,
				code:     tc.code,
				stack:    tc.stack,
				gasLimit: differentialFuzzGasLimit,
				c7:       makeTonopsTestC7(t, tonopsTestC7Config{}),
			})
		})
	}
}

func TestTVMDifferentialFuzzDataSizeGapOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	root := parityProgramStorageStatCell()
	sharedRef := cell.BeginCell().MustStoreUInt(0xD, 4).EndCell()
	sharedRoot := cell.BeginCell().
		MustStoreRef(sharedRef).
		MustStoreRef(sharedRef).
		EndCell()

	cases := []struct {
		name  string
		code  *cell.Cell
		stack []any
	}{
		{
			name:  "cdatasize_nil_success_zero",
			code:  parityProgramCodeCell(funcsop.CDATASIZE().Serialize()),
			stack: []any{(*cell.Cell)(nil), int64(0)},
		},
		{
			name:  "cdatasize_success_counts_ref",
			code:  parityProgramCodeCell(funcsop.CDATASIZE().Serialize()),
			stack: []any{root, int64(10)},
		},
		{
			name:  "cdatasize_shared_ref_counts_once",
			code:  parityProgramCodeCell(funcsop.CDATASIZE().Serialize()),
			stack: []any{sharedRoot, int64(10)},
		},
		{
			name:  "cdatasize_negative_bound_rangecheck",
			code:  parityProgramCodeCell(funcsop.CDATASIZE().Serialize()),
			stack: []any{root, int64(-1)},
		},
		{
			name:  "cdatasizeq_bound_too_small_false",
			code:  parityProgramCodeCell(funcsop.CDATASIZEQ().Serialize()),
			stack: []any{root, int64(1)},
		},
		{
			name:  "sdatasize_success_counts_refs",
			code:  parityProgramCodeCell(funcsop.SDATASIZE().Serialize()),
			stack: []any{root.MustBeginParse(), int64(10)},
		},
		{
			name:  "sdatasize_shared_ref_counts_once",
			code:  parityProgramCodeCell(funcsop.SDATASIZE().Serialize()),
			stack: []any{sharedRoot.MustBeginParse(), int64(10)},
		},
		{
			name:  "sdatasize_negative_bound_rangecheck",
			code:  parityProgramCodeCell(funcsop.SDATASIZE().Serialize()),
			stack: []any{root.MustBeginParse(), int64(-1)},
		},
		{
			name:  "sdatasizeq_bound_too_small_false",
			code:  parityProgramCodeCell(funcsop.SDATASIZEQ().Serialize()),
			stack: []any{root.MustBeginParse(), int64(0)},
		},
	}

	caseNames := make([]string, 0, len(cases))
	for _, tc := range cases {
		caseNames = append(caseNames, tc.name)
	}
	requireNamedParityCases(t, "datasize_gap", caseNames, requiredDataSizeGapCaseNames)

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runDifferentialFuzzCase(t, differentialFuzzCase{
				seed:     uint64(i),
				family:   "datasize_gap",
				op:       tc.name,
				code:     tc.code,
				stack:    tc.stack,
				gasLimit: differentialFuzzGasLimit,
			})
		})
	}
}

func TestTVMDifferentialFuzzTonFuncRuntimeGapOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	runtimeC7 := makeTonopsTestC7(t, tonopsTestC7Config{})
	largeConfigIdx := new(big.Int).Lsh(big.NewInt(1), 40)
	maxRandSeed := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	negativeConfigValue := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	signedConfigRoot := mustConfigDictCell(t, map[uint32]*cell.Cell{
		^uint32(0): negativeConfigValue,
	})
	malformedConfig := cell.NewDict(32)
	if _, err := malformedConfig.SetBuilderWithMode(
		cell.BeginCell().MustStoreUInt(23, 32).EndCell(),
		cell.BeginCell().MustStoreUInt(1, 1).MustStoreRef(testEmptyCell()),
		cell.DictSetModeSet,
	); err != nil {
		t.Fatalf("failed to build malformed config dict: %v", err)
	}
	malformedConfigC7 := prepareCrossTestC7(malformedConfig, testEmptyCell())
	signedConfigC7 := makeTonopsTestC7(t, tonopsTestC7Config{ConfigRoot: signedConfigRoot})
	badConfigRootC7 := makeTonopsTestC7(t, tonopsTestC7Config{
		ExtraParams: map[int]any{
			9: int64(1),
		},
	})
	missingParamsC7 := tuple.NewTupleValue()
	paramsNotTupleC7 := tuple.NewTupleValue(big.NewInt(1))
	badRandSeedC7 := makeTonopsTestC7(t, tonopsTestC7Config{
		ExtraParams: map[int]any{
			6: testEmptyCell(),
		},
	})
	shortUnpackedConfigC7 := makeTonopsTestC7(t, tonopsTestC7Config{
		UnpackedConfig: tuple.NewTupleSized(1),
	})
	shortInMsgParamsC7 := makeTonopsTestC7(t, tonopsTestC7Config{
		ExtraParams: map[int]any{
			17: tuple.NewTupleSized(1),
		},
	})

	cases := []struct {
		name  string
		code  *cell.Cell
		stack []any
		c7    tuple.Tuple
	}{
		{
			name: "getparam_now_blocklt_ltime_aliases",
			code: parityProgramCodeCell(
				funcsop.GETPARAM(3).Serialize(),
				funcsop.NOW().Serialize(),
				funcsop.GETPARAM(4).Serialize(),
				funcsop.BLOCKLT().Serialize(),
				funcsop.GETPARAM(5).Serialize(),
				funcsop.LTIME().Serialize(),
			),
			c7: runtimeC7,
		},
		{
			name: "getparamlong_randseed",
			code: parityProgramCodeCell(
				funcsop.GETPARAMLONG(6).Serialize(),
				funcsop.RANDSEED().Serialize(),
			),
			c7: runtimeC7,
		},
		{
			name: "getparam_missing_params_tuple_range",
			code: parityProgramCodeCell(
				funcsop.GETPARAM(3).Serialize(),
			),
			c7: missingParamsC7,
		},
		{
			name: "getparam_params_not_tuple_typecheck",
			code: parityProgramCodeCell(
				funcsop.GETPARAM(3).Serialize(),
			),
			c7: paramsNotTupleC7,
		},
		{
			name: "balance_myaddr_configroot",
			code: parityProgramCodeCell(
				funcsop.BALANCE().Serialize(),
				funcsop.MYADDR().Serialize(),
				funcsop.CONFIGROOT().Serialize(),
			),
			c7: runtimeC7,
		},
		{
			name: "randu256_updates_seed",
			code: parityProgramCodeCell(
				funcsop.RANDU256().Serialize(),
				funcsop.RANDSEED().Serialize(),
			),
			c7: runtimeC7,
		},
		{
			name: "randu256_bad_seed_typecheck",
			code: parityProgramCodeCell(
				funcsop.RANDU256().Serialize(),
			),
			c7: badRandSeedC7,
		},
		{
			name: "rand_bounded_updates_seed",
			code: parityProgramCodeCell(
				funcsop.RAND().Serialize(),
				funcsop.RANDSEED().Serialize(),
			),
			stack: []any{int64(1000)},
			c7:    runtimeC7,
		},
		{
			name: "rand_zero_bound_updates_seed",
			code: parityProgramCodeCell(
				funcsop.RAND().Serialize(),
				funcsop.RANDSEED().Serialize(),
			),
			stack: []any{int64(0)},
			c7:    runtimeC7,
		},
		{
			name: "rand_negative_bound_updates_seed",
			code: parityProgramCodeCell(
				funcsop.RAND().Serialize(),
				funcsop.RANDSEED().Serialize(),
			),
			stack: []any{int64(-7)},
			c7:    runtimeC7,
		},
		{
			name: "setrand_addrand_roundtrip",
			code: parityProgramCodeCell(
				funcsop.SETRAND().Serialize(),
				funcsop.RANDSEED().Serialize(),
				stackop.PUSHINT(big.NewInt(0x5678)).Serialize(),
				funcsop.ADDRAND().Serialize(),
				funcsop.RANDSEED().Serialize(),
			),
			stack: []any{big.NewInt(0x1234)},
			c7:    runtimeC7,
		},
		{
			name: "setrand_max_seed_roundtrip",
			code: parityProgramCodeCell(
				funcsop.SETRAND().Serialize(),
				funcsop.RANDSEED().Serialize(),
			),
			stack: []any{maxRandSeed},
			c7:    runtimeC7,
		},
		{
			name: "setrand_negative_rangecheck",
			code: parityProgramCodeCell(
				funcsop.SETRAND().Serialize(),
				funcsop.RANDSEED().Serialize(),
			),
			stack: []any{int64(-1)},
			c7:    runtimeC7,
		},
		{
			name: "setrand_c7_params_not_tuple_typecheck",
			code: parityProgramCodeCell(
				funcsop.SETRAND().Serialize(),
			),
			stack: []any{big.NewInt(0x1234)},
			c7:    paramsNotTupleC7,
		},
		{
			name: "addrand_negative_rangecheck",
			code: parityProgramCodeCell(
				funcsop.ADDRAND().Serialize(),
				funcsop.RANDSEED().Serialize(),
			),
			stack: []any{int64(-1)},
			c7:    runtimeC7,
		},
		{
			name: "setglob_getglob_roundtrip",
			code: parityProgramCodeCell(
				funcsop.SETGLOB(20).Serialize(),
				funcsop.GETGLOB(20).Serialize(),
			),
			stack: []any{int64(1234)},
			c7:    runtimeC7,
		},
		{
			name: "setglobvar_getglobvar_roundtrip",
			code: parityProgramCodeCell(
				funcsop.SETGLOBVAR().Serialize(),
				stackop.PUSHINT(big.NewInt(21)).Serialize(),
				funcsop.GETGLOBVAR().Serialize(),
			),
			stack: []any{int64(1235), int64(21)},
			c7:    runtimeC7,
		},
		{
			name: "getglobvar_absent_null",
			code: parityProgramCodeCell(
				funcsop.GETGLOBVAR().Serialize(),
			),
			stack: []any{int64(254)},
			c7:    runtimeC7,
		},
		{
			name: "globalid_unpacked_config_short_tuple_range",
			code: parityProgramCodeCell(
				funcsop.GLOBALID().Serialize(),
			),
			c7: shortUnpackedConfigC7,
		},
		{
			name: "inmsgparam_short_tuple_range",
			code: parityProgramCodeCell(
				funcsop.INMSGPARAM(9).Serialize(),
			),
			c7: shortInMsgParamsC7,
		},
		{
			name:  "configparam_no_root_false",
			code:  parityProgramCodeCell(funcsop.CONFIGPARAM().Serialize()),
			stack: []any{int64(7)},
			c7:    runtimeC7,
		},
		{
			name:  "configparam_negative_key_hit",
			code:  parityProgramCodeCell(funcsop.CONFIGPARAM().Serialize()),
			stack: []any{int64(-1)},
			c7:    signedConfigC7,
		},
		{
			name:  "configoptparam_negative_key_hit",
			code:  parityProgramCodeCell(funcsop.CONFIGOPTPARAM().Serialize()),
			stack: []any{int64(-1)},
			c7:    signedConfigC7,
		},
		{
			name:  "configoptparam_no_root_null",
			code:  parityProgramCodeCell(funcsop.CONFIGOPTPARAM().Serialize()),
			stack: []any{int64(7)},
			c7:    runtimeC7,
		},
		{
			name:  "configparam_bad_root_type",
			code:  parityProgramCodeCell(funcsop.CONFIGPARAM().Serialize()),
			stack: []any{int64(7)},
			c7:    badConfigRootC7,
		},
		{
			name:  "configoptparam_bad_root_type",
			code:  parityProgramCodeCell(funcsop.CONFIGOPTPARAM().Serialize()),
			stack: []any{int64(7)},
			c7:    badConfigRootC7,
		},
		{
			name:  "configparam_out_of_int32_false",
			code:  parityProgramCodeCell(funcsop.CONFIGPARAM().Serialize()),
			stack: []any{largeConfigIdx},
			c7:    runtimeC7,
		},
		{
			name:  "configparam_malformed_value_dict_error",
			code:  parityProgramCodeCell(funcsop.CONFIGPARAM().Serialize()),
			stack: []any{int64(23)},
			c7:    malformedConfigC7,
		},
		{
			name:  "configoptparam_malformed_value_dict_error",
			code:  parityProgramCodeCell(funcsop.CONFIGOPTPARAM().Serialize()),
			stack: []any{int64(23)},
			c7:    malformedConfigC7,
		},
		{
			name: "setcp_zero",
			code: parityProgramCodeCell(
				funcsop.SETCP(0).Serialize(),
				stackop.PUSHINT(big.NewInt(1)).Serialize(),
			),
			c7: runtimeC7,
		},
		{
			name: "setcp_negative_unsupported",
			code: parityProgramCodeCell(
				funcsop.SETCP(-1).Serialize(),
				stackop.PUSHINT(big.NewInt(1)).Serialize(),
			),
			c7: runtimeC7,
		},
		{
			name: "setcp_positive_unsupported",
			code: parityProgramCodeCell(
				funcsop.SETCP(1).Serialize(),
				stackop.PUSHINT(big.NewInt(1)).Serialize(),
			),
			c7: runtimeC7,
		},
		{
			name: "setcpx_zero",
			code: parityProgramCodeCell(
				funcsop.SETCPX().Serialize(),
				stackop.PUSHINT(big.NewInt(2)).Serialize(),
			),
			stack: []any{int64(0)},
			c7:    runtimeC7,
		},
		{
			name: "setcpx_negative_unsupported",
			code: parityProgramCodeCell(
				funcsop.SETCPX().Serialize(),
				stackop.PUSHINT(big.NewInt(2)).Serialize(),
			),
			stack: []any{int64(-1)},
			c7:    runtimeC7,
		},
		{
			name: "setcpx_positive_unsupported",
			code: parityProgramCodeCell(
				funcsop.SETCPX().Serialize(),
				stackop.PUSHINT(big.NewInt(2)).Serialize(),
			),
			stack: []any{int64(1)},
			c7:    runtimeC7,
		},
		{
			name: "setcpx_high_rangecheck",
			code: parityProgramCodeCell(
				funcsop.SETCPX().Serialize(),
				stackop.PUSHINT(big.NewInt(2)).Serialize(),
			),
			stack: []any{int64(32768)},
			c7:    runtimeC7,
		},
	}

	caseNames := make([]string, 0, len(cases))
	for _, tc := range cases {
		caseNames = append(caseNames, tc.name)
	}
	requireNamedParityCases(t, "ton_func_runtime_gap", caseNames, requiredTonFuncRuntimeGapCaseNames)

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runDifferentialFuzzCase(t, differentialFuzzCase{
				seed:     uint64(i),
				family:   "ton_func_runtime_gap",
				op:       tc.name,
				code:     tc.code,
				stack:    tc.stack,
				gasLimit: differentialFuzzGasLimit,
				c7:       tc.c7,
			})
		})
	}
}

func TestTVMDifferentialFuzzHashGapOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	builderWithRef := cell.BeginCell().
		MustStoreUInt(0xCA, 8).
		MustStoreRef(cell.BeginCell().MustStoreUInt(0xFE, 8).EndCell())
	appendDst := cell.BeginCell().MustStoreUInt(0xAA, 8)
	builderInput := cell.BeginCell().MustStoreSlice([]byte("abc"), 24)
	crowdedBuilder := cell.BeginCell().MustStoreSlice(bytes.Repeat([]byte{0xAB}, 100), 800)

	cases := []struct {
		name             string
		code             *cell.Cell
		stack            []any
		globalVersion    int
		globalVersionSet bool
	}{
		{
			name: "sha256u_empty",
			code: parityProgramCodeCell(funcsop.SHA256U().Serialize()),
			stack: []any{
				cell.BeginCell().ToSlice(),
			},
		},
		{
			name: "hashbu_builder_with_ref",
			code: parityProgramCodeCell(funcsop.HASHBU().Serialize()),
			stack: []any{
				builderWithRef,
			},
		},
		{
			name: "hashext_bit_concat_sha256",
			code: parityProgramCodeCell(funcsop.HASHEXT(0).Serialize()),
			stack: []any{
				cell.BeginCell().MustStoreUInt(0xA, 4).ToSlice(),
				cell.BeginCell().MustStoreUInt(0xB, 4).ToSlice(),
				int64(2),
			},
		},
		{
			name: "hashextr_order_sha256",
			code: parityProgramCodeCell(funcsop.HASHEXT(1<<8 | 0).Serialize()),
			stack: []any{
				cell.BeginCell().MustStoreSlice([]byte("ab"), 16).ToSlice(),
				cell.BeginCell().MustStoreSlice([]byte("cd"), 16).ToSlice(),
				int64(2),
			},
		},
		{
			name: "hashext_dynamic_keccak256",
			code: parityProgramCodeCell(funcsop.HASHEXT(255).Serialize()),
			stack: []any{
				cell.BeginCell().MustStoreSlice([]byte("abc"), 24).ToSlice(),
				int64(1),
				int64(3),
			},
		},
		{
			name: "hashext_dynamic_unknown_hash_id",
			code: parityProgramCodeCell(funcsop.HASHEXT(255).Serialize()),
			stack: []any{
				cell.BeginCell().MustStoreSlice([]byte("abc"), 24).ToSlice(),
				int64(1),
				int64(5),
			},
		},
		{
			name: "hashext_dynamic_hash_id_rangecheck",
			code: parityProgramCodeCell(funcsop.HASHEXT(255).Serialize()),
			stack: []any{
				cell.BeginCell().MustStoreSlice([]byte("abc"), 24).ToSlice(),
				int64(1),
				int64(255),
			},
		},
		{
			name: "hashext_count_rangecheck",
			code: parityProgramCodeCell(funcsop.HASHEXT(0).Serialize()),
			stack: []any{
				cell.BeginCell().MustStoreSlice([]byte("abc"), 24).ToSlice(),
				int64(2),
			},
		},
		{
			name:             "hashext_dynamic_missing_count_v8_partial_pop",
			code:             parityProgramCodeCell(funcsop.HASHEXT(255).Serialize()),
			stack:            []any{int64(0)},
			globalVersion:    8,
			globalVersionSet: true,
		},
		{
			name:             "hashext_dynamic_missing_count_v9_precheck",
			code:             parityProgramCodeCell(funcsop.HASHEXT(255).Serialize()),
			stack:            []any{int64(0)},
			globalVersion:    9,
			globalVersionSet: true,
		},
		{
			name: "hashext_sha512_tuple",
			code: parityProgramCodeCell(funcsop.HASHEXT(1).Serialize()),
			stack: []any{
				cell.BeginCell().MustStoreSlice([]byte("abc"), 24).ToSlice(),
				int64(1),
			},
		},
		{
			name: "hashext_blake2b_tuple",
			code: parityProgramCodeCell(funcsop.HASHEXT(2).Serialize()),
			stack: []any{
				cell.BeginCell().MustStoreSlice([]byte("abc"), 24).ToSlice(),
				int64(1),
			},
		},
		{
			name: "hashext_keccak512_tuple",
			code: parityProgramCodeCell(funcsop.HASHEXT(4).Serialize()),
			stack: []any{
				cell.BeginCell().MustStoreSlice([]byte("abc"), 24).ToSlice(),
				int64(1),
			},
		},
		{
			name: "hashexta_keccak256_builder_input",
			code: parityProgramCodeCell(funcsop.HASHEXT(1<<9 | 3).Serialize()),
			stack: []any{
				appendDst,
				builderInput,
				int64(1),
			},
		},
		{
			name: "hashexta_append_non_builder_typecheck",
			code: parityProgramCodeCell(funcsop.HASHEXT(1 << 9).Serialize()),
			stack: []any{
				int64(123),
				int64(0),
			},
		},
		{
			name: "hashexta_dynamic_zero_count_builder_only",
			code: parityProgramCodeCell(funcsop.HASHEXT(1<<9 | 255).Serialize()),
			stack: []any{
				cell.BeginCell().MustStoreUInt(0xAA, 8),
				int64(0),
				int64(0),
			},
		},
		{
			name: "hashextar_dynamic_sha512_append_reverse",
			code: parityProgramCodeCell(funcsop.HASHEXT(1<<9 | 1<<8 | 255).Serialize()),
			stack: []any{
				cell.BeginCell().MustStoreUInt(0xAA, 8),
				cell.BeginCell().MustStoreSlice([]byte("ab"), 16).ToSlice(),
				cell.BeginCell().MustStoreSlice([]byte("cd"), 16).ToSlice(),
				int64(2),
				int64(1),
			},
		},
		{
			name: "hashext_unaligned_bits_underflow",
			code: parityProgramCodeCell(funcsop.HASHEXT(0).Serialize()),
			stack: []any{
				cell.BeginCell().MustStoreUInt(0xA, 4).ToSlice(),
				int64(1),
			},
		},
		{
			name: "sha256u_unaligned_bits_underflow",
			code: parityProgramCodeCell(funcsop.SHA256U().Serialize()),
			stack: []any{
				cell.BeginCell().MustStoreUInt(0xA, 4).ToSlice(),
			},
		},
		{
			name: "hashexta_builder_overflow",
			code: parityProgramCodeCell(funcsop.HASHEXT(1 << 9).Serialize()),
			stack: []any{
				crowdedBuilder,
				cell.BeginCell().MustStoreSlice([]byte("abc"), 24).ToSlice(),
				int64(1),
			},
		},
	}

	caseNames := make([]string, 0, len(cases))
	for _, tc := range cases {
		caseNames = append(caseNames, tc.name)
	}
	requireNamedParityCases(t, "hash_gap", caseNames, requiredHashGapCaseNames)

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fuzzCase := differentialFuzzCase{
				seed:     uint64(i),
				family:   "hash_gap",
				op:       tc.name,
				code:     tc.code,
				stack:    tc.stack,
				gasLimit: differentialFuzzGasLimit,
			}
			if tc.globalVersionSet {
				fuzzCase = differentialFuzzWithGlobalVersion(t, fuzzCase, tc.globalVersion)
			}
			runDifferentialFuzzCase(t, fuzzCase)
		})
	}
}

func TestTVMDifferentialFuzzCryptoGapOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	edKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x11}, ed25519.SeedSize))
	edPub := edKey.Public().(ed25519.PublicKey)
	edHash := bytes.Repeat([]byte{0x44}, 32)
	edSigU := ed25519.Sign(edKey, edHash)
	badEdSigU := ed25519.Sign(edKey, bytes.Repeat([]byte{0x45}, 32))
	edSliceData := []byte("ed25519-signed-slice")
	edSigS := ed25519.Sign(edKey, edSliceData)
	badEdSigS := ed25519.Sign(edKey, []byte("different-ed25519-slice"))

	ecrecoverHash := bytes.Repeat([]byte{0x42}, 32)
	ecrecoverV, ecrecoverR, ecrecoverS, _, ok := localec.SignRecoverable(bytes.Repeat([]byte{0x31}, 32), bytes.Repeat([]byte{0x57}, 32), ecrecoverHash)
	if !ok {
		t.Fatal("failed to build secp256k1 recovery fixture")
	}
	_, _, _, xonlyBasePub, ok := localec.SignRecoverable(bytes.Repeat([]byte{0x41}, 32), bytes.Repeat([]byte{0x67}, 32), bytes.Repeat([]byte{0x22}, 32))
	if !ok {
		t.Fatal("failed to build secp256k1 xonly fixture")
	}
	secpXOnlyKey := new(big.Int).SetBytes(xonlyBasePub[1:33])
	secpTooLargeTweak := new(big.Int).SetBytes(localec.CurveOrderBytes())

	p256D := big.NewInt(7)
	p256Scalar := p256D.FillBytes(make([]byte, 32))
	p256Curve := elliptic.P256()
	p256X, p256Y := p256Curve.ScalarBaseMult(p256Scalar)
	p256Priv := &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{
			Curve: p256Curve,
			X:     p256X,
			Y:     p256Y,
		},
		D: p256D,
	}
	p256HashBytes := bytes.Repeat([]byte{0x55}, 32)
	p256SigU := mustP256RawSignature(t, p256Priv, p256HashBytes)
	badP256SigU := mustP256RawSignature(t, p256Priv, bytes.Repeat([]byte{0x56}, 32))
	p256Pub := elliptic.MarshalCompressed(p256Curve, p256X, p256Y)
	p256SliceData := []byte("p256-signed-slice")
	p256SigS := mustP256RawSignature(t, p256Priv, p256SliceData)
	badP256SigS := mustP256RawSignature(t, p256Priv, []byte("different-p256-slice"))
	badP256Key := append([]byte{0x05}, bytes.Repeat([]byte{0x01}, 32)...)

	cases := []struct {
		name  string
		code  *cell.Cell
		stack []any
	}{
		{
			name: "chksignu_success",
			code: parityProgramCodeCell(funcsop.CHKSIGNU().Serialize()),
			stack: []any{
				new(big.Int).SetBytes(edHash),
				cell.BeginCell().MustStoreSlice(edSigU, 512).ToSlice(),
				new(big.Int).SetBytes(edPub),
			},
		},
		{
			name: "chksigns_success",
			code: parityProgramCodeCell(funcsop.CHKSIGNS().Serialize()),
			stack: []any{
				cell.BeginCell().MustStoreSlice(edSliceData, uint(len(edSliceData))*8).ToSlice(),
				cell.BeginCell().MustStoreSlice(edSigS, 512).ToSlice(),
				new(big.Int).SetBytes(edPub),
			},
		},
		{
			name: "chksignu_bad_signature_false",
			code: parityProgramCodeCell(funcsop.CHKSIGNU().Serialize()),
			stack: []any{
				new(big.Int).SetBytes(edHash),
				cell.BeginCell().MustStoreSlice(badEdSigU, 512).ToSlice(),
				new(big.Int).SetBytes(edPub),
			},
		},
		{
			name: "chksigns_bad_signature_false",
			code: parityProgramCodeCell(funcsop.CHKSIGNS().Serialize()),
			stack: []any{
				cell.BeginCell().MustStoreSlice(edSliceData, uint(len(edSliceData))*8).ToSlice(),
				cell.BeginCell().MustStoreSlice(badEdSigS, 512).ToSlice(),
				new(big.Int).SetBytes(edPub),
			},
		},
		{
			name: "ecrecover_success",
			code: parityProgramCodeCell(funcsop.ECRECOVER().Serialize()),
			stack: []any{
				new(big.Int).SetBytes(ecrecoverHash),
				int64(ecrecoverV),
				ecrecoverR,
				ecrecoverS,
			},
		},
		{
			name: "ecrecover_invalid_v",
			code: parityProgramCodeCell(funcsop.ECRECOVER().Serialize()),
			stack: []any{
				new(big.Int).SetBytes(ecrecoverHash),
				int64(4),
				ecrecoverR,
				ecrecoverS,
			},
		},
		{
			name: "secp256k1_xonly_pubkey_tweak_add_success",
			code: parityProgramCodeCell(funcsop.SECP256K1_XONLY_PUBKEY_TWEAK_ADD().Serialize()),
			stack: []any{
				secpXOnlyKey,
				int64(7),
			},
		},
		{
			name: "secp256k1_xonly_pubkey_tweak_add_tweak_ge_n",
			code: parityProgramCodeCell(funcsop.SECP256K1_XONLY_PUBKEY_TWEAK_ADD().Serialize()),
			stack: []any{
				secpXOnlyKey,
				secpTooLargeTweak,
			},
		},
		{
			name: "p256_chksignu_success",
			code: parityProgramCodeCell(funcsop.P256_CHKSIGNU().Serialize()),
			stack: []any{
				new(big.Int).SetBytes(p256HashBytes),
				cell.BeginCell().MustStoreSlice(p256SigU, 512).ToSlice(),
				cell.BeginCell().MustStoreSlice(p256Pub, 264).ToSlice(),
			},
		},
		{
			name: "p256_chksignu_bad_key",
			code: parityProgramCodeCell(funcsop.P256_CHKSIGNU().Serialize()),
			stack: []any{
				new(big.Int).SetBytes(p256HashBytes),
				cell.BeginCell().MustStoreSlice(p256SigU, 512).ToSlice(),
				cell.BeginCell().MustStoreSlice(badP256Key, 264).ToSlice(),
			},
		},
		{
			name: "p256_chksigns_success",
			code: parityProgramCodeCell(funcsop.P256_CHKSIGNS().Serialize()),
			stack: []any{
				cell.BeginCell().MustStoreSlice(p256SliceData, uint(len(p256SliceData))*8).ToSlice(),
				cell.BeginCell().MustStoreSlice(p256SigS, 512).ToSlice(),
				cell.BeginCell().MustStoreSlice(p256Pub, 264).ToSlice(),
			},
		},
		{
			name: "p256_chksigns_bad_key",
			code: parityProgramCodeCell(funcsop.P256_CHKSIGNS().Serialize()),
			stack: []any{
				cell.BeginCell().MustStoreSlice(p256SliceData, uint(len(p256SliceData))*8).ToSlice(),
				cell.BeginCell().MustStoreSlice(p256SigS, 512).ToSlice(),
				cell.BeginCell().MustStoreSlice(badP256Key, 264).ToSlice(),
			},
		},
		{
			name: "p256_chksignu_bad_signature_false",
			code: parityProgramCodeCell(funcsop.P256_CHKSIGNU().Serialize()),
			stack: []any{
				new(big.Int).SetBytes(p256HashBytes),
				cell.BeginCell().MustStoreSlice(badP256SigU, 512).ToSlice(),
				cell.BeginCell().MustStoreSlice(p256Pub, 264).ToSlice(),
			},
		},
		{
			name: "p256_chksigns_bad_signature_false",
			code: parityProgramCodeCell(funcsop.P256_CHKSIGNS().Serialize()),
			stack: []any{
				cell.BeginCell().MustStoreSlice(p256SliceData, uint(len(p256SliceData))*8).ToSlice(),
				cell.BeginCell().MustStoreSlice(badP256SigS, 512).ToSlice(),
				cell.BeginCell().MustStoreSlice(p256Pub, 264).ToSlice(),
			},
		},
	}

	caseNames := make([]string, 0, len(cases))
	for _, tc := range cases {
		caseNames = append(caseNames, tc.name)
	}
	requireNamedParityCases(t, "crypto_gap", caseNames, requiredCryptoGapCaseNames)

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runDifferentialFuzzCase(t, differentialFuzzCase{
				seed:     uint64(i),
				family:   "crypto_gap",
				op:       tc.name,
				code:     tc.code,
				stack:    tc.stack,
				gasLimit: differentialFuzzGasLimit,
			})
		})
	}
}

func TestTVMDifferentialFuzzCryptoEdgeGapOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	edKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x22}, ed25519.SeedSize))
	edPub := edKey.Public().(ed25519.PublicKey)
	edHash := sha256.Sum256([]byte("crypto-edge-ed25519"))
	edSig := ed25519.Sign(edKey, edHash[:])

	p256D := big.NewInt(9)
	p256Scalar := p256D.FillBytes(make([]byte, 32))
	p256Curve := elliptic.P256()
	p256X, p256Y := p256Curve.ScalarBaseMult(p256Scalar)
	p256Pub := elliptic.MarshalCompressed(p256Curve, p256X, p256Y)

	msg := []byte("crypto-edge-bls")
	pub1 := testBLSPubBytes(3)
	sig1 := testBLSSigBytes(3, msg)
	g1a := testBLSG1BytesForScalar(2)
	g2a := testBLSG2BytesForScalar(2)
	invalidRist := testInvalidRistrettoInt(t)
	invalidG1 := testInvalidBLSG1Bytes(t)
	invalidG2 := testInvalidBLSG2Bytes(t)

	shortSlice := cell.BeginCell().MustStoreUInt(0xAA, 8).ToSlice()
	unalignedSlice := cell.BeginCell().MustStoreUInt(1, 1).ToSlice()
	fullSig := cell.BeginCell().MustStoreSlice(bytes.Repeat([]byte{0x11}, 64), 512).ToSlice()
	p256Key := cell.BeginCell().MustStoreSlice(p256Pub, 264).ToSlice()
	hashInt := new(big.Int).SetBytes(edHash[:])

	cases := []struct {
		name  string
		code  *cell.Cell
		stack []any
	}{
		{
			name: "chksignu_short_signature_underflow",
			code: parityProgramCodeCell(funcsop.CHKSIGNU().Serialize()),
			stack: []any{
				hashInt,
				shortSlice,
				new(big.Int).SetBytes(edPub),
			},
		},
		{
			name: "chksigns_unaligned_message_underflow",
			code: parityProgramCodeCell(funcsop.CHKSIGNS().Serialize()),
			stack: []any{
				unalignedSlice,
				cell.BeginCell().MustStoreSlice(edSig, 512).ToSlice(),
				new(big.Int).SetBytes(edPub),
			},
		},
		{
			name: "chksigns_short_signature_underflow",
			code: parityProgramCodeCell(funcsop.CHKSIGNS().Serialize()),
			stack: []any{
				cell.BeginCell().MustStoreSlice([]byte("aligned"), 56).ToSlice(),
				shortSlice,
				new(big.Int).SetBytes(edPub),
			},
		},
		{
			name: "chksignu_negative_hash_range",
			code: parityProgramCodeCell(funcsop.CHKSIGNU().Serialize()),
			stack: []any{
				big.NewInt(-1),
				cell.BeginCell().MustStoreSlice(edSig, 512).ToSlice(),
				new(big.Int).SetBytes(edPub),
			},
		},
		{
			name: "chksignu_key_negative_range",
			code: parityProgramCodeCell(funcsop.CHKSIGNU().Serialize()),
			stack: []any{
				hashInt,
				cell.BeginCell().MustStoreSlice(edSig, 512).ToSlice(),
				big.NewInt(-1),
			},
		},
		{
			name: "ecrecover_negative_hash_range",
			code: parityProgramCodeCell(funcsop.ECRECOVER().Serialize()),
			stack: []any{
				big.NewInt(-1),
				int64(0),
				int64(0),
				int64(0),
			},
		},
		{
			name: "ecrecover_negative_r_range",
			code: parityProgramCodeCell(funcsop.ECRECOVER().Serialize()),
			stack: []any{
				hashInt,
				int64(0),
				big.NewInt(-1),
				int64(0),
			},
		},
		{
			name: "ecrecover_v_out_of_uint8_range",
			code: parityProgramCodeCell(funcsop.ECRECOVER().Serialize()),
			stack: []any{
				hashInt,
				int64(256),
				int64(0),
				int64(0),
			},
		},
		{
			name: "ecrecover_zero_signature_false",
			code: parityProgramCodeCell(funcsop.ECRECOVER().Serialize()),
			stack: []any{
				hashInt,
				int64(0),
				int64(0),
				int64(0),
			},
		},
		{
			name: "secp256k1_tweak_negative_range",
			code: parityProgramCodeCell(funcsop.SECP256K1_XONLY_PUBKEY_TWEAK_ADD().Serialize()),
			stack: []any{
				int64(1),
				big.NewInt(-1),
			},
		},
		{
			name: "secp256k1_key_negative_range",
			code: parityProgramCodeCell(funcsop.SECP256K1_XONLY_PUBKEY_TWEAK_ADD().Serialize()),
			stack: []any{
				big.NewInt(-1),
				int64(0),
			},
		},
		{
			name: "secp256k1_invalid_key_false",
			code: parityProgramCodeCell(funcsop.SECP256K1_XONLY_PUBKEY_TWEAK_ADD().Serialize()),
			stack: []any{
				int64(0),
				int64(0),
			},
		},
		{
			name: "p256_chksigns_unaligned_message_underflow",
			code: parityProgramCodeCell(funcsop.P256_CHKSIGNS().Serialize()),
			stack: []any{
				unalignedSlice,
				fullSig,
				p256Key,
			},
		},
		{
			name: "p256_chksignu_negative_hash_range",
			code: parityProgramCodeCell(funcsop.P256_CHKSIGNU().Serialize()),
			stack: []any{
				big.NewInt(-1),
				fullSig,
				p256Key,
			},
		},
		{
			name: "p256_chksignu_short_signature_underflow",
			code: parityProgramCodeCell(funcsop.P256_CHKSIGNU().Serialize()),
			stack: []any{
				hashInt,
				shortSlice,
				p256Key,
			},
		},
		{
			name: "p256_chksignu_short_key_underflow",
			code: parityProgramCodeCell(funcsop.P256_CHKSIGNU().Serialize()),
			stack: []any{
				hashInt,
				fullSig,
				shortSlice,
			},
		},
		{
			name: "rist255_validate_invalid_range",
			code: parityProgramCodeCell(funcsop.RIST255_VALIDATE().Serialize()),
			stack: []any{
				invalidRist,
			},
		},
		{
			name: "rist255_add_invalid_unknown",
			code: parityProgramCodeCell(funcsop.RIST255_ADD().Serialize()),
			stack: []any{
				int64(1),
				int64(1),
			},
		},
		{
			name: "bls_verify_short_pub_underflow",
			code: parityProgramCodeCell(funcsop.BLS_VERIFY().Serialize()),
			stack: []any{
				shortSlice,
				testSliceFromBytes(msg),
				testSliceFromBytes(sig1),
			},
		},
		{
			name: "bls_verify_short_sig_underflow",
			code: parityProgramCodeCell(funcsop.BLS_VERIFY().Serialize()),
			stack: []any{
				testSliceFromBytes(pub1),
				testSliceFromBytes(msg),
				shortSlice,
			},
		},
		{
			name: "bls_verify_invalid_sig_false",
			code: parityProgramCodeCell(funcsop.BLS_VERIFY().Serialize()),
			stack: []any{
				testSliceFromBytes(pub1),
				testSliceFromBytes(msg),
				testSliceFromBytes(invalidG2),
			},
		},
		{
			name: "bls_fastaggregateverify_empty_false",
			code: parityProgramCodeCell(funcsop.BLS_FASTAGGREGATEVERIFY().Serialize()),
			stack: []any{
				int64(0),
				testSliceFromBytes(msg),
				testSliceFromBytes(sig1),
			},
		},
		{
			name: "bls_aggregateverify_empty_false",
			code: parityProgramCodeCell(funcsop.BLS_AGGREGATEVERIFY().Serialize()),
			stack: []any{
				int64(0),
				testSliceFromBytes(sig1),
			},
		},
		{
			name: "bls_aggregateverify_invalid_sig_false",
			code: parityProgramCodeCell(funcsop.BLS_AGGREGATEVERIFY().Serialize()),
			stack: []any{
				testSliceFromBytes(pub1),
				testSliceFromBytes(msg),
				int64(1),
				testSliceFromBytes(invalidG2),
			},
		},
		{
			name: "bls_aggregate_invalid_signature_unknown",
			code: parityProgramCodeCell(funcsop.BLS_AGGREGATE().Serialize()),
			stack: []any{
				testSliceFromBytes(invalidG2),
				int64(1),
			},
		},
		{
			name: "bls_g1_add_invalid_point_unknown",
			code: parityProgramCodeCell(funcsop.BLS_G1_ADD().Serialize()),
			stack: []any{
				testSliceFromBytes(invalidG1),
				testSliceFromBytes(g1a),
			},
		},
		{
			name: "bls_g2_add_invalid_point_unknown",
			code: parityProgramCodeCell(funcsop.BLS_G2_ADD().Serialize()),
			stack: []any{
				testSliceFromBytes(invalidG2),
				testSliceFromBytes(g2a),
			},
		},
		{
			name: "bls_g1_ingroup_short_underflow",
			code: parityProgramCodeCell(funcsop.BLS_G1_INGROUP().Serialize()),
			stack: []any{
				shortSlice,
			},
		},
		{
			name: "bls_g2_ingroup_short_underflow",
			code: parityProgramCodeCell(funcsop.BLS_G2_INGROUP().Serialize()),
			stack: []any{
				shortSlice,
			},
		},
		{
			name: "bls_g1_multiexp_invalid_point_unknown",
			code: parityProgramCodeCell(funcsop.BLS_G1_MULTIEXP().Serialize()),
			stack: []any{
				testSliceFromBytes(invalidG1),
				int64(2),
				int64(1),
			},
		},
		{
			name: "bls_g2_multiexp_invalid_point_unknown",
			code: parityProgramCodeCell(funcsop.BLS_G2_MULTIEXP().Serialize()),
			stack: []any{
				testSliceFromBytes(invalidG2),
				int64(2),
				int64(1),
			},
		},
	}

	caseNames := make([]string, 0, len(cases))
	for _, tc := range cases {
		caseNames = append(caseNames, tc.name)
	}
	requireNamedParityCases(t, "crypto_edge_gap", caseNames, requiredCryptoEdgeGapCaseNames)

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runDifferentialFuzzCase(t, differentialFuzzCase{
				seed:     uint64(i),
				family:   "crypto_edge_gap",
				op:       tc.name,
				code:     tc.code,
				stack:    tc.stack,
				gasLimit: differentialFuzzGasLimit,
			})
		})
	}
}

func TestTVMDifferentialFuzzCirclCryptoGapOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	msg := []byte("ton-circl-bls-parity")
	msg2 := []byte("ton-circl-bls-parity-2")
	pub1 := testBLSPubBytes(3)
	pub2 := testBLSPubBytes(5)
	sig1 := testBLSSigBytes(3, msg)
	sig2 := testBLSSigBytes(5, msg)
	sig2Msg2 := testBLSSigBytes(5, msg2)
	aggSig := testBLSAggregateSigBytes(t, sig1, sig2)
	aggDistinctSig := testBLSAggregateSigBytes(t, sig1, sig2Msg2)

	g1a := testBLSG1BytesForScalar(2)
	g1b := testBLSG1BytesForScalar(3)
	g2a := testBLSG2BytesForScalar(2)
	g2b := testBLSG2BytesForScalar(3)
	fp := testBLSFPBytes(7)
	fp2 := testBLSFP2Bytes(11)
	invalidRist := testInvalidRistrettoInt(t)
	invalidG1 := testInvalidBLSG1Bytes(t)
	invalidG2 := testInvalidBLSG2Bytes(t)

	cases := []struct {
		name  string
		code  *cell.Cell
		stack []any
	}{
		{
			name: "rist255_fromhash",
			code: parityProgramCodeCell(funcsop.RIST255_FROMHASH().Serialize()),
			stack: []any{
				int64(1),
				int64(2),
			},
		},
		{
			name: "rist255_pushl",
			code: parityProgramCodeCell(funcsop.RIST255_PUSHL().Serialize()),
		},
		{
			name: "rist255_validate_valid",
			code: parityProgramCodeCell(funcsop.RIST255_VALIDATE().Serialize()),
			stack: []any{
				testRistrettoMulBaseInt(t, 1),
			},
		},
		{
			name: "rist255_qvalidate_invalid",
			code: parityProgramCodeCell(funcsop.RIST255_QVALIDATE().Serialize()),
			stack: []any{
				invalidRist,
			},
		},
		{
			name: "rist255_add_valid",
			code: parityProgramCodeCell(funcsop.RIST255_ADD().Serialize()),
			stack: []any{
				testRistrettoMulBaseInt(t, 1),
				testRistrettoMulBaseInt(t, 2),
			},
		},
		{
			name: "rist255_qadd_invalid",
			code: parityProgramCodeCell(funcsop.RIST255_QADD().Serialize()),
			stack: []any{
				testRistrettoMulBaseInt(t, 1),
				invalidRist,
			},
		},
		{
			name: "rist255_sub_valid",
			code: parityProgramCodeCell(funcsop.RIST255_SUB().Serialize()),
			stack: []any{
				testRistrettoMulBaseInt(t, 3),
				testRistrettoMulBaseInt(t, 1),
			},
		},
		{
			name: "rist255_qsub_valid",
			code: parityProgramCodeCell(funcsop.RIST255_QSUB().Serialize()),
			stack: []any{
				testRistrettoMulBaseInt(t, 3),
				testRistrettoMulBaseInt(t, 1),
			},
		},
		{
			name: "rist255_qsub_invalid",
			code: parityProgramCodeCell(funcsop.RIST255_QSUB().Serialize()),
			stack: []any{
				testRistrettoMulBaseInt(t, 3),
				invalidRist,
			},
		},
		{
			name: "rist255_mul",
			code: parityProgramCodeCell(funcsop.RIST255_MUL().Serialize()),
			stack: []any{
				testRistrettoMulBaseInt(t, 5),
				int64(3),
			},
		},
		{
			name: "rist255_qmul_invalid",
			code: parityProgramCodeCell(funcsop.RIST255_QMUL().Serialize()),
			stack: []any{
				invalidRist,
				int64(3),
			},
		},
		{
			name: "rist255_mulbase",
			code: parityProgramCodeCell(funcsop.RIST255_MULBASE().Serialize()),
			stack: []any{
				int64(11),
			},
		},
		{
			name: "rist255_qmulbase",
			code: parityProgramCodeCell(funcsop.RIST255_QMULBASE().Serialize()),
			stack: []any{
				int64(13),
			},
		},
		{
			name: "bls_pushr",
			code: parityProgramCodeCell(funcsop.BLS_PUSHR().Serialize()),
		},
		{
			name: "bls_verify_true",
			code: parityProgramCodeCell(funcsop.BLS_VERIFY().Serialize()),
			stack: []any{
				testSliceFromBytes(pub1),
				testSliceFromBytes(msg),
				testSliceFromBytes(sig1),
			},
		},
		{
			name: "bls_verify_invalid_pub_false",
			code: parityProgramCodeCell(funcsop.BLS_VERIFY().Serialize()),
			stack: []any{
				testSliceFromBytes(invalidG1),
				testSliceFromBytes(msg),
				testSliceFromBytes(sig1),
			},
		},
		{
			name: "bls_aggregate",
			code: parityProgramCodeCell(funcsop.BLS_AGGREGATE().Serialize()),
			stack: []any{
				testSliceFromBytes(sig1),
				testSliceFromBytes(sig2),
				int64(2),
			},
		},
		{
			name: "bls_fastaggregateverify_true",
			code: parityProgramCodeCell(funcsop.BLS_FASTAGGREGATEVERIFY().Serialize()),
			stack: []any{
				testSliceFromBytes(pub1),
				testSliceFromBytes(pub2),
				int64(2),
				testSliceFromBytes(msg),
				testSliceFromBytes(aggSig),
			},
		},
		{
			name: "bls_aggregateverify_distinct_msgs_true",
			code: parityProgramCodeCell(funcsop.BLS_AGGREGATEVERIFY().Serialize()),
			stack: []any{
				testSliceFromBytes(pub1),
				testSliceFromBytes(msg),
				testSliceFromBytes(pub2),
				testSliceFromBytes(msg2),
				int64(2),
				testSliceFromBytes(aggDistinctSig),
			},
		},
		{
			name: "bls_g1_zero",
			code: parityProgramCodeCell(funcsop.BLS_G1_ZERO().Serialize()),
		},
		{
			name: "bls_g1_add",
			code: parityProgramCodeCell(funcsop.BLS_G1_ADD().Serialize()),
			stack: []any{
				testSliceFromBytes(g1a),
				testSliceFromBytes(g1b),
			},
		},
		{
			name: "bls_g1_sub",
			code: parityProgramCodeCell(funcsop.BLS_G1_SUB().Serialize()),
			stack: []any{
				testSliceFromBytes(g1a),
				testSliceFromBytes(g1b),
			},
		},
		{
			name: "bls_g1_neg",
			code: parityProgramCodeCell(funcsop.BLS_G1_NEG().Serialize()),
			stack: []any{
				testSliceFromBytes(g1a),
			},
		},
		{
			name: "bls_g1_mul",
			code: parityProgramCodeCell(funcsop.BLS_G1_MUL().Serialize()),
			stack: []any{
				testSliceFromBytes(g1a),
				int64(7),
			},
		},
		{
			name: "bls_g1_multiexp_two_terms",
			code: parityProgramCodeCell(funcsop.BLS_G1_MULTIEXP().Serialize()),
			stack: []any{
				testSliceFromBytes(g1a),
				int64(5),
				testSliceFromBytes(g1b),
				int64(7),
				int64(2),
			},
		},
		{
			name: "bls_g1_ingroup_invalid_false",
			code: parityProgramCodeCell(funcsop.BLS_G1_INGROUP().Serialize()),
			stack: []any{
				testSliceFromBytes(invalidG1),
			},
		},
		{
			name: "bls_g1_iszero",
			code: parityProgramCodeCell(funcsop.BLS_G1_ISZERO().Serialize()),
			stack: []any{
				testSliceFromBytes(testBLSG1ZeroBytes()),
			},
		},
		{
			name: "bls_map_to_g1",
			code: parityProgramCodeCell(funcsop.BLS_MAP_TO_G1().Serialize()),
			stack: []any{
				testSliceFromBytes(fp),
			},
		},
		{
			name: "bls_g2_zero",
			code: parityProgramCodeCell(funcsop.BLS_G2_ZERO().Serialize()),
		},
		{
			name: "bls_g2_add",
			code: parityProgramCodeCell(funcsop.BLS_G2_ADD().Serialize()),
			stack: []any{
				testSliceFromBytes(g2a),
				testSliceFromBytes(g2b),
			},
		},
		{
			name: "bls_g2_sub",
			code: parityProgramCodeCell(funcsop.BLS_G2_SUB().Serialize()),
			stack: []any{
				testSliceFromBytes(g2a),
				testSliceFromBytes(g2b),
			},
		},
		{
			name: "bls_g2_neg",
			code: parityProgramCodeCell(funcsop.BLS_G2_NEG().Serialize()),
			stack: []any{
				testSliceFromBytes(g2a),
			},
		},
		{
			name: "bls_g2_mul",
			code: parityProgramCodeCell(funcsop.BLS_G2_MUL().Serialize()),
			stack: []any{
				testSliceFromBytes(g2a),
				int64(7),
			},
		},
		{
			name: "bls_g2_multiexp_two_terms",
			code: parityProgramCodeCell(funcsop.BLS_G2_MULTIEXP().Serialize()),
			stack: []any{
				testSliceFromBytes(g2a),
				int64(5),
				testSliceFromBytes(g2b),
				int64(7),
				int64(2),
			},
		},
		{
			name: "bls_g2_ingroup_invalid_false",
			code: parityProgramCodeCell(funcsop.BLS_G2_INGROUP().Serialize()),
			stack: []any{
				testSliceFromBytes(invalidG2),
			},
		},
		{
			name: "bls_g2_iszero",
			code: parityProgramCodeCell(funcsop.BLS_G2_ISZERO().Serialize()),
			stack: []any{
				testSliceFromBytes(testBLSG2ZeroBytes()),
			},
		},
		{
			name: "bls_map_to_g2",
			code: parityProgramCodeCell(funcsop.BLS_MAP_TO_G2().Serialize()),
			stack: []any{
				testSliceFromBytes(fp2),
			},
		},
		{
			name: "bls_pairing_false",
			code: parityProgramCodeCell(funcsop.BLS_PAIRING().Serialize()),
			stack: []any{
				testSliceFromBytes(testBLSG1BytesForScalar(1)),
				testSliceFromBytes(testBLSG2BytesForScalar(1)),
				int64(1),
			},
		},
		{
			name: "bls_pairing_invalid",
			code: parityProgramCodeCell(funcsop.BLS_PAIRING().Serialize()),
			stack: []any{
				testSliceFromBytes(invalidG1),
				testSliceFromBytes(testBLSG2BytesForScalar(1)),
				int64(1),
			},
		},
	}

	caseNames := make([]string, 0, len(cases))
	for _, tc := range cases {
		caseNames = append(caseNames, tc.name)
	}
	requireNamedParityCases(t, "circl_crypto_gap", caseNames, requiredCirclCryptoGapCaseNames)

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runDifferentialFuzzCase(t, differentialFuzzCase{
				seed:     uint64(i),
				family:   "circl_crypto_gap",
				op:       tc.name,
				code:     tc.code,
				stack:    tc.stack,
				gasLimit: differentialFuzzGasLimit,
			})
		})
	}
}

func TestTVMDifferentialFuzzTupleGapOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	two := tuple.NewTupleValue(big.NewInt(11), big.NewInt(22))
	cases := []struct {
		name  string
		code  *cell.Cell
		stack []any
	}{
		{
			name: "pushnull_isnull_true",
			code: parityProgramCodeCell(tupleop.PUSHNULL().Serialize(), tupleop.ISNULL().Serialize()),
		},
		{
			name:  "isnull_false",
			code:  parityProgramCodeCell(tupleop.ISNULL().Serialize()),
			stack: []any{int64(1)},
		},
		{
			name:  "istuple_true",
			code:  parityProgramCodeCell(tupleop.ISTUPLE().Serialize()),
			stack: []any{two},
		},
		{
			name:  "istuple_false",
			code:  parityProgramCodeCell(tupleop.ISTUPLE().Serialize()),
			stack: []any{int64(1)},
		},
		{
			name:  "qtlen_tuple",
			code:  parityProgramCodeCell(tupleop.QTLEN().Serialize()),
			stack: []any{two},
		},
		{
			name:  "qtlen_non_tuple",
			code:  parityProgramCodeCell(tupleop.QTLEN().Serialize()),
			stack: []any{int64(1)},
		},
		{
			name:  "indexq_oob_null",
			code:  parityProgramCodeCell(tupleop.INDEXQ(3).Serialize()),
			stack: []any{two},
		},
	}

	caseNames := make([]string, 0, len(cases))
	for _, tc := range cases {
		caseNames = append(caseNames, tc.name)
	}
	requireNamedParityCases(t, "tuple_gap", caseNames, requiredTupleGapCaseNames)

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runDifferentialFuzzCase(t, differentialFuzzCase{
				seed:     uint64(i),
				family:   "tuple_gap",
				op:       tc.name,
				code:     tc.code,
				stack:    tc.stack,
				gasLimit: differentialFuzzGasLimit,
			})
		})
	}

	empty := testEmptyCell()
	allLabels := make([]string, 0, 128)
	for mode := 0; mode < 19; mode++ {
		t.Run(fmt.Sprintf("extra_%02d", mode), func(t *testing.T) {
			g := &parityProgramGenerator{
				r:    rand.New(rand.NewSource(int64(mode))),
				seed: append([]byte(nil), crossTestSeed...),
				regD: [2]*cell.Cell{
					empty,
					empty,
				},
			}
			if !g.emitTupleExtraOpMode(mode) {
				t.Fatalf("mode %d was not emitted", mode)
			}
			allLabels = append(allLabels, g.trace...)

			runDifferentialFuzzCase(t, differentialFuzzCase{
				seed:     uint64(len(cases) + mode),
				family:   "tuple_gap",
				op:       strings.Join(g.trace, " -> "),
				code:     parityProgramCodeFromBuilders(t, g.ops...),
				gasLimit: differentialFuzzGasLimit,
				c7:       prepareCrossTestC7(nil, empty),
			})
		})
	}
	requireParityTraceLabels(t, "tuple_gap", allLabels, requiredTupleGapTraceLabels)
}

func TestTVMDifferentialFuzzTupleOpcodeSpaceGapOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	cases := buildTupleOpsOpcodeSpaceCases()
	requireTupleParityCases(t, "tuple_opcode_space_gap", cases, expectedTupleOpcodeSpaceGapCases, requiredTupleOpcodeSpaceGapCaseNames)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runTupleOpParityCase(t, tc)
		})
	}
}

func TestTVMDifferentialFuzzTupleDynamicErrorGapOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	cases := buildTupleOpsDynamicAndErrorCases()
	requireTupleParityCases(t, "tuple_dynamic_error_gap", cases, expectedTupleDynamicErrorGapCases, requiredTupleDynamicErrorGapCaseNames)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runTupleOpParityCase(t, tc)
		})
	}
}

func requireTupleParityCases(t *testing.T, family string, cases []tupleOpParityCase, expected int, required []string) {
	t.Helper()

	if len(cases) != expected {
		t.Fatalf("%s parity case count mismatch: got %d want %d", family, len(cases), expected)
	}

	names := make([]string, 0, len(cases))
	for _, tc := range cases {
		names = append(names, tc.name)
	}
	requireNamedParityCases(t, family, names, required)
}

func TestTVMDifferentialFuzzDebugGapOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	debugSlice := cell.BeginCell().MustStoreSlice([]byte("debug"), 40).ToSlice()
	refCell := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	cases := []struct {
		name  string
		code  *cell.Cell
		stack []any
	}{
		{
			name: "dumpstk_keeps_mixed_stack",
			code: parityProgramCodeCell(stackop.DUMPSTK().Serialize()),
			stack: []any{
				int64(11),
				debugSlice.Copy(),
				refCell,
			},
		},
		{
			name: "dump_top_keeps_stack",
			code: parityProgramCodeCell(stackop.DUMP(0).Serialize()),
			stack: []any{
				int64(22),
				refCell,
			},
		},
		{
			name: "dump_absent_keeps_stack",
			code: parityProgramCodeCell(stackop.DUMP(5).Serialize()),
			stack: []any{
				int64(33),
			},
		},
		{
			name: "debug_keeps_stack",
			code: parityProgramCodeCell(stackop.DEBUG(42).Serialize()),
			stack: []any{
				int64(44),
				debugSlice.Copy(),
			},
		},
		{
			name: "strdump_byte_slice_keeps_stack",
			code: parityProgramCodeCell(stackop.STRDUMP().Serialize()),
			stack: []any{
				debugSlice.Copy(),
			},
		},
		{
			name: "strdump_non_slice_keeps_stack",
			code: parityProgramCodeCell(stackop.STRDUMP().Serialize()),
			stack: []any{
				int64(55),
			},
		},
		{
			name: "debugstr_keeps_stack",
			code: parityProgramCodeCell(stackop.DEBUGSTR([]byte("hello")).Serialize()),
			stack: []any{
				refCell,
			},
		},
		{
			name: "debugstr_truncates_long_literal",
			code: parityProgramCodeCell(stackop.DEBUGSTR([]byte("0123456789abcdef-extra")).Serialize()),
		},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runDifferentialFuzzCase(t, differentialFuzzCase{
				seed:     uint64(i),
				family:   "debug_gap",
				op:       tc.name,
				code:     tc.code,
				stack:    tc.stack,
				gasLimit: differentialFuzzGasLimit,
			})
		})
	}
}

func parityFuzzEnvInt(t *testing.T, key string, fallback int) int {
	t.Helper()

	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatalf("%s must be an integer: %v", key, err)
	}
	return v
}

func parityFuzzShardEnv(t *testing.T, shardKey, shardsKey string) (int, int) {
	t.Helper()

	rawShard := os.Getenv(shardKey)
	rawShards := os.Getenv(shardsKey)
	shard, shards, err := parityFuzzShardFromEnv(shardKey, rawShard, shardsKey, rawShards)
	if err != nil {
		t.Fatal(err)
	}
	return shard, shards
}

func parityFuzzShardFromEnv(shardKey, rawShard, shardsKey, rawShards string) (int, int, error) {
	if rawShards == "" && rawShard == "" {
		return 0, 0, nil
	}
	if rawShards == "" || rawShard == "" {
		return 0, 0, fmt.Errorf("%s and %s must be set together", shardKey, shardsKey)
	}

	shards, err := strconv.Atoi(rawShards)
	if err != nil || shards <= 0 {
		return 0, 0, fmt.Errorf("%s must be a positive integer: %q", shardsKey, rawShards)
	}
	shard, err := strconv.Atoi(rawShard)
	if err != nil || shard < 0 || shard >= shards {
		return 0, 0, fmt.Errorf("%s must be in [0,%d): %q", shardKey, shards, rawShard)
	}
	return shard, shards, nil
}

func generateDifferentialFuzzCase(t *testing.T, r *rand.Rand, seed uint64) differentialFuzzCase {
	t.Helper()

	return generateDifferentialFuzzCaseWithFamily(t, r, seed, pickMixedDifferentialFuzzFamily(t, r))
}

func generateDifferentialFuzzCaseWithFamily(t *testing.T, r *rand.Rand, seed uint64, family string) differentialFuzzCase {
	t.Helper()

	switch family {
	case "", "mixed":
		return generateDifferentialFuzzCase(t, r, seed)
	case "datasize":
		return generateDataSizeFuzzCase(t, r, seed)
	case "slice_load":
		return generateSliceLoadFuzzCase(t, r, seed)
	case "slice_predicate":
		return generateSlicePredicateFuzzCase(t, r, seed)
	case "slice_store":
		return generateSliceStoreFuzzCase(t, r, seed)
	case "math":
		return generateMathFuzzCase(t, r, seed)
	case "program":
		return generateProgramFuzzCase(t, r, seed)
	case "msg_address_versioned":
		return generateVersionedMessageAddressFuzzCase(t, r, seed)
	case "program_versioned":
		return generateVersionedProgramFuzzCase(t, r, seed)
	case "program_versioned_datasize":
		return generateVersionedDataSizeProgramFuzzCase(t, r, seed)
	case "program_versioned_actions":
		return generateVersionedActionsProgramFuzzCase(t, r, seed)
	case "program_versioned_libraries":
		return generateVersionedLibrariesProgramFuzzCase(t, r, seed)
	case "program_versioned_msg_address":
		return generateVersionedMessageAddressProgramFuzzCase(t, r, seed)
	case "program_versioned_hash_varint":
		return generateVersionedHashVarIntProgramFuzzCase(t, r, seed)
	case "program_versioned_prng":
		return generateVersionedPRNGProgramFuzzCase(t, r, seed)
	case "program_versioned_math":
		return generateVersionedMathProgramFuzzCase(t, r, seed)
	case "program_versioned_cellslice":
		return generateVersionedCellSliceProgramFuzzCase(t, r, seed)
	case "program_versioned_control":
		return generateVersionedControlProgramFuzzCase(t, r, seed)
	case "program_versioned_exec":
		return generateVersionedExecProgramFuzzCase(t, r, seed)
	case "program_versioned_dict":
		return generateVersionedDictProgramFuzzCase(t, r, seed)
	case "program_versioned_stack":
		return generateVersionedStackProgramFuzzCase(t, r, seed)
	case "program_versioned_tuple":
		return generateVersionedTupleProgramFuzzCase(t, r, seed)
	case "program_versioned_runvm":
		return generateVersionedRunVMProgramFuzzCase(t, r, seed)
	case "program_versioned_runvm_rich_c7":
		return generateVersionedRunVMRichC7ProgramFuzzCase(t, r, seed)
	case "program_versioned_runtime":
		return generateVersionedRuntimeProgramFuzzCase(t, r, seed)
	case "program_versioned_c7":
		return generateVersionedC7ProgramFuzzCase(t, r, seed)
	case "program_versioned_rich_c7":
		return generateVersionedRichC7ProgramFuzzCase(t, r, seed)
	case "program_versioned_supercontract":
		return generateVersionedSupercontractProgramFuzzCase(t, r, seed)
	default:
		t.Fatalf("unsupported TVM_PARITY_FAMILY %q", family)
	}
	panic("unreachable")
}

func runDifferentialFuzzCase(t *testing.T, tc differentialFuzzCase) {
	t.Helper()

	code := prependRawMethodDrop(tc.code)
	gasLimit := tc.gasLimit
	if gasLimit == 0 {
		gasLimit = differentialFuzzGasLimit
	}

	goStack, err := buildCrossStack(tc.stack...)
	if err != nil {
		t.Fatalf("seed=%d family=%s op=%s: failed to build go stack: %v", tc.seed, tc.family, tc.op, err)
	}
	refStack, err := buildCrossStack(tc.stack...)
	if err != nil {
		t.Fatalf("seed=%d family=%s op=%s: failed to build reference stack: %v", tc.seed, tc.family, tc.op, err)
	}

	goLibs := tc.goLibs
	if len(goLibs) == 0 && tc.refLibs != nil {
		goLibs = []*cell.Cell{tc.refLibs}
	}

	globalVersion := tc.globalVersion
	if !tc.globalVersionSet && globalVersion == 0 {
		globalVersion = referenceRawRunGlobalVersion
	}
	if globalVersion != referenceRawRunGlobalVersion && tc.refCfg == nil && !tc.rawC7Versioned {
		t.Fatalf("seed=%d family=%s op=%s: versioned differential case v%d has no reference config", tc.seed, tc.family, tc.op, globalVersion)
	}

	c7 := tc.c7
	if tc.refCfg != nil {
		if !tc.c7.IsNull() {
			t.Fatalf("seed=%d family=%s op=%s: reference config path cannot use tuple c7; put comparable c7 fields in refCfg", tc.seed, tc.family, tc.op)
		}
		c7 = differentialFuzzC7FromRefConfig(t, code, *tc.refCfg, globalVersion)
	} else if c7.IsNull() {
		c7 = tuple.Tuple{}
	}

	goRes, err := runGoCrossCodeWithVersionGasAndLibs(code, testEmptyCell(), c7, goLibs, goStack, globalVersion, gasLimit)
	if err != nil {
		t.Fatalf("seed=%d family=%s op=%s: go tvm execution failed: %v", tc.seed, tc.family, tc.op, err)
	}
	var refRes *crossRunResult
	if tc.refCfg != nil {
		cfg := *tc.refCfg
		if tc.refLibs != nil {
			cfg.Libs = tc.refLibs
		}
		if gasLimit != 0 {
			cfg.GasLimit = gasLimit
		}
		refRes, err = runReferenceCrossCodeViaEmulator(code, testEmptyCell(), refStack, cfg)
	} else {
		refRes, err = runReferenceCrossCodeWithLibsAndGas(code, testEmptyCell(), c7, tc.refLibs, refStack, gasLimit)
	}
	if err != nil {
		t.Fatalf("seed=%d family=%s op=%s: reference tvm execution failed: %v", tc.seed, tc.family, tc.op, err)
	}

	if goRes.exitCode != refRes.exitCode {
		if reason := differentialFuzzKnownReferenceMismatchReason(tc, globalVersion); reason != "" {
			t.Skip(reason)
		}
		t.Fatalf("seed=%d family=%s op=%s: exit code mismatch: go=%d reference=%d",
			tc.seed, tc.family, tc.op, goRes.exitCode, refRes.exitCode)
	}
	if goRes.gasUsed != refRes.gasUsed {
		if reason := differentialFuzzKnownReferenceMismatchReason(tc, globalVersion); reason != "" {
			t.Skip(reason)
		}
		t.Fatalf("seed=%d family=%s op=%s: gas mismatch: go=%d reference=%d",
			tc.seed, tc.family, tc.op, goRes.gasUsed, refRes.gasUsed)
	}

	goStackCell, err := normalizeStackCell(goRes.stack)
	if err != nil {
		t.Fatalf("seed=%d family=%s op=%s: failed to normalize go stack: %v", tc.seed, tc.family, tc.op, err)
	}
	refStackCell, err := normalizeStackCell(refRes.stack)
	if err != nil {
		t.Fatalf("seed=%d family=%s op=%s: failed to normalize reference stack: %v", tc.seed, tc.family, tc.op, err)
	}
	if !bytes.Equal(goStackCell.Hash(), refStackCell.Hash()) {
		if reason := differentialFuzzKnownReferenceMismatchReason(tc, globalVersion); reason != "" {
			t.Skip(reason)
		}
		t.Fatalf("seed=%d family=%s op=%s: stack mismatch\ngo=%s\nreference=%s",
			tc.seed, tc.family, tc.op, goStackCell.Dump(), refStackCell.Dump())
	}
}

func differentialFuzzKnownReferenceMismatchReason(tc differentialFuzzCase, globalVersion int) string {
	if globalVersion < 14 || !strings.HasPrefix(tc.family, "program_versioned") || !differentialFuzzTraceHasV14SilentSaveListWrite(tc.op) {
		return ""
	}
	return "bundled reference emulator predates upstream control-register v14 silent duplicate save-list writes"
}

func differentialFuzzTraceHasV14SilentSaveListWrite(trace string) bool {
	for _, token := range []string{
		"SAVECTR(4)",
		"SAVEALTCTR(4)",
		"SETRETCTR(4)",
		"SETALTCTR(4)",
		"SETCONTCTR",
		"SETCONTCTRX",
		"SETCONTCTRMANY",
	} {
		if strings.Contains(trace, token) {
			return true
		}
	}
	return false
}

func differentialFuzzOptionalVersionRefConfig(t *testing.T, version int) *referenceGetMethodConfig {
	t.Helper()

	if version == 0 || version == referenceRawRunGlobalVersion {
		return nil
	}
	return differentialFuzzExplicitVersionRefConfig(t, version)
}

func differentialFuzzC7FromRefConfig(t *testing.T, code *cell.Cell, cfg referenceGetMethodConfig, globalVersion int) tuple.Tuple {
	t.Helper()

	if cfg.Address == nil {
		t.Fatal("reference config address is nil")
	}
	if cfg.Now == 0 {
		t.Fatal("reference config now must be set for deterministic Go c7")
	}

	balance := new(big.Int).SetUint64(cfg.Balance)
	if cfg.Balance == 0 {
		balance.SetUint64(referenceDefaultTonopsBalance)
	}

	seed := cfg.RandSeed
	if len(seed) == 0 {
		seed = referenceDefaultTonopsSeed
	}

	prepared := MustPrepareConfig(cfg.ConfigRoot)
	c7, err := buildEmulationC7(emulationC7Input{
		addr:           cfg.Address,
		code:           code,
		now:            cfg.Now,
		balance:        balance,
		seed:           new(big.Int).SetBytes(seed),
		configRoot:     prepared.Root(),
		prevBlocks:     cfg.PrevBlocks,
		unpackedConfig: messageUnpackedConfig(MessageEmulationConfig{Config: prepared, PrevBlocks: cfg.PrevBlocks}, cfg.Now),
		globalVersion:  uint32(globalVersion),
	})
	if err != nil {
		t.Fatalf("failed to build Go c7 from reference config: %v", err)
	}
	return c7
}

func differentialFuzzSeedVersion(seed uint64) int {
	return tvmFuzzGlobalVersionSeed(seed)
}

func differentialFuzzVersionMatrixSeed(start uint64, offset int, version int) uint64 {
	return tvmFuzzGlobalVersionMatrixSeed(start, offset, version)
}

func differentialFuzzVersionMatrixAuditSeed(start uint64, familyIdx, offset int, version int) uint64 {
	return differentialFuzzVersionMatrixSeed(start, familyIdx*versionMatrixFamilySeedStride+offset, version)
}

func differentialFuzzExplicitVersionRefConfig(t *testing.T, version int) *referenceGetMethodConfig {
	t.Helper()

	if version == referenceRawRunGlobalVersion {
		return nil
	}
	return tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version)))
}

func differentialFuzzWithGlobalVersion(t *testing.T, tc differentialFuzzCase, version int) differentialFuzzCase {
	t.Helper()

	tc.globalVersion = version
	tc.globalVersionSet = true
	tc.refCfg = differentialFuzzExplicitVersionRefConfig(t, version)
	tc.op = fmt.Sprintf("%s/v%d", tc.op, version)
	return tc
}

func differentialFuzzSimpleVersionedCase(t *testing.T, tc differentialFuzzCase) differentialFuzzCase {
	t.Helper()

	return differentialFuzzWithGlobalVersion(t, tc, differentialFuzzSeedVersion(tc.seed))
}

func generateDataSizeFuzzCase(t *testing.T, r *rand.Rand, seed uint64) differentialFuzzCase {
	t.Helper()

	ops := []struct {
		name     string
		builder  *cell.Builder
		sliceArg bool
		lowGasOK bool
	}{
		{"CDATASIZE", funcsop.CDATASIZE().Serialize(), false, true},
		{"CDATASIZEQ", funcsop.CDATASIZEQ().Serialize(), false, true},
		{"SDATASIZE", funcsop.SDATASIZE().Serialize(), true, false},
		{"SDATASIZEQ", funcsop.SDATASIZEQ().Serialize(), true, false},
	}
	op := ops[r.Intn(len(ops))]

	stack := []any{parityFuzzDataSizeArg(t, r, op.sliceArg), parityFuzzBound(t, r)}
	if r.Intn(8) == 0 {
		stack = stack[1:]
	}

	gasLimit := int64(0)
	if op.lowGasOK && r.Intn(24) == 0 {
		gasLimit = 120
	}

	return differentialFuzzSimpleVersionedCase(t, differentialFuzzCase{
		seed:     seed,
		family:   "datasize",
		op:       op.name + differentialFuzzStackSummary(stack),
		code:     codeFromBuilders(t, op.builder),
		stack:    stack,
		gasLimit: gasLimit,
	})
}

func generateSliceLoadFuzzCase(t *testing.T, r *rand.Rand, seed uint64) differentialFuzzCase {
	t.Helper()

	ops := []struct {
		name    string
		builder *cell.Builder
		width   bool
	}{
		{"LDIX", cellsliceop.LDIX().Serialize(), true},
		{"LDUX", cellsliceop.LDUX().Serialize(), true},
		{"PLDIX", cellsliceop.PLDIX().Serialize(), true},
		{"PLDUX", cellsliceop.PLDUX().Serialize(), true},
		{"LDIXQ", cellsliceop.LDIXQ().Serialize(), true},
		{"LDUXQ", cellsliceop.LDUXQ().Serialize(), true},
		{"LDSLICEX", cellsliceop.LDSLICEX().Serialize(), true},
		{"LDSLICEXQ", cellsliceop.LDSLICEXQ().Serialize(), true},
		{"PLDSLICEX", cellsliceop.PLDSLICEX().Serialize(), true},
		{"PLDSLICEXQ", cellsliceop.PLDSLICEXQ().Serialize(), true},
		{"LDI8", cellsliceop.LDI(8).Serialize(), false},
		{"LDU8", cellsliceop.LDU(8).Serialize(), false},
		{"PLDU8", cellsliceop.PLDU(8).Serialize(), false},
	}
	op := ops[r.Intn(len(ops))]

	var stack []any
	if op.width {
		stack = []any{parityFuzzMaybeSlice(t, r), parityFuzzWidth(r)}
		if r.Intn(10) == 0 {
			stack = stack[1:]
		}
	} else {
		stack = []any{parityFuzzMaybeSlice(t, r)}
		if r.Intn(10) == 0 {
			stack = nil
		}
	}

	return differentialFuzzSimpleVersionedCase(t, differentialFuzzCase{
		seed:   seed,
		family: "slice_load",
		op:     op.name,
		code:   codeFromBuilders(t, op.builder),
		stack:  stack,
	})
}

func generateSlicePredicateFuzzCase(t *testing.T, r *rand.Rand, seed uint64) differentialFuzzCase {
	t.Helper()

	unary := []struct {
		name    string
		builder *cell.Builder
	}{
		{"SEMPTY", cellsliceop.SEMPTY().Serialize()},
		{"SDEMPTY", cellsliceop.SDEMPTY().Serialize()},
		{"SREMPTY", cellsliceop.SREMPTY().Serialize()},
		{"SDFIRST", cellsliceop.SDFIRST().Serialize()},
		{"SDCNTLEAD0", cellsliceop.SDCNTLEAD0().Serialize()},
		{"SDCNTLEAD1", cellsliceop.SDCNTLEAD1().Serialize()},
		{"SDCNTTRAIL0", cellsliceop.SDCNTTRAIL0().Serialize()},
		{"SDCNTTRAIL1", cellsliceop.SDCNTTRAIL1().Serialize()},
		{"SBITS", cellsliceop.SBITS().Serialize()},
		{"SREFS", cellsliceop.SREFS().Serialize()},
	}
	binary := []struct {
		name    string
		builder *cell.Builder
	}{
		{"SDEQ", cellsliceop.SDEQ().Serialize()},
		{"SDLEXCMP", cellsliceop.SDLEXCMP().Serialize()},
		{"SDPFX", cellsliceop.SDPFX().Serialize()},
		{"SDPFXREV", cellsliceop.SDPFXREV().Serialize()},
		{"SDPPFX", cellsliceop.SDPPFX().Serialize()},
		{"SDPPFXREV", cellsliceop.SDPPFXREV().Serialize()},
		{"SDSFX", cellsliceop.SDSFX().Serialize()},
		{"SDSFXREV", cellsliceop.SDSFXREV().Serialize()},
		{"SDPSFX", cellsliceop.SDPSFX().Serialize()},
		{"SDPSFXREV", cellsliceop.SDPSFXREV().Serialize()},
		{"SDBEGINSX", cellsliceop.SDBEGINSX().Serialize()},
		{"SDBEGINSXQ", cellsliceop.SDBEGINSXQ().Serialize()},
	}

	if r.Intn(2) == 0 {
		op := unary[r.Intn(len(unary))]
		stack := []any{parityFuzzMaybeSlice(t, r)}
		if r.Intn(10) == 0 {
			stack = nil
		}
		return differentialFuzzSimpleVersionedCase(t, differentialFuzzCase{seed: seed, family: "slice_predicate", op: op.name + differentialFuzzStackSummary(stack), code: codeFromBuilders(t, op.builder), stack: stack})
	}

	op := binary[r.Intn(len(binary))]
	stack := []any{parityFuzzMaybeSlice(t, r), parityFuzzMaybeSlice(t, r)}
	if r.Intn(10) == 0 {
		stack = stack[:1]
	}
	return differentialFuzzSimpleVersionedCase(t, differentialFuzzCase{seed: seed, family: "slice_predicate", op: op.name + differentialFuzzStackSummary(stack), code: codeFromBuilders(t, op.builder), stack: stack})
}

func generateSliceStoreFuzzCase(t *testing.T, r *rand.Rand, seed uint64) differentialFuzzCase {
	t.Helper()

	ops := []struct {
		name    string
		builder *cell.Builder
		stack   func(*testing.T, *rand.Rand) []any
	}{
		{"STU8", cellsliceop.STU(8).Serialize(), func(t *testing.T, r *rand.Rand) []any {
			return []any{parityFuzzStoreInt(r), parityFuzzMaybeBuilder(t, r)}
		}},
		{"STI8", cellsliceop.STI(8).Serialize(), func(t *testing.T, r *rand.Rand) []any {
			return []any{parityFuzzStoreInt(r), parityFuzzMaybeBuilder(t, r)}
		}},
		{"STREF", cellsliceop.STREF().Serialize(), func(t *testing.T, r *rand.Rand) []any {
			return []any{parityFuzzMaybeCell(t, r), parityFuzzMaybeBuilder(t, r)}
		}},
		{"STREFQ", cellsliceop.STREFQ().Serialize(), func(t *testing.T, r *rand.Rand) []any {
			return []any{parityFuzzMaybeCell(t, r), parityFuzzMaybeBuilder(t, r)}
		}},
		{"STSLICE", cellsliceop.STSLICE().Serialize(), func(t *testing.T, r *rand.Rand) []any {
			return []any{parityFuzzMaybeSlice(t, r), parityFuzzMaybeBuilder(t, r)}
		}},
		{"STSLICEQ", cellsliceop.STSLICEQ().Serialize(), func(t *testing.T, r *rand.Rand) []any {
			return []any{parityFuzzMaybeSlice(t, r), parityFuzzMaybeBuilder(t, r)}
		}},
		{"BCHKBITS", cellsliceop.BCHKBITS().Serialize(), func(t *testing.T, r *rand.Rand) []any {
			return []any{parityFuzzMaybeBuilder(t, r), parityFuzzWidth(r)}
		}},
		{"BCHKBITSQ", cellsliceop.BCHKBITSQ().Serialize(), func(t *testing.T, r *rand.Rand) []any {
			return []any{parityFuzzMaybeBuilder(t, r), parityFuzzWidth(r)}
		}},
	}
	op := ops[r.Intn(len(ops))]
	stack := op.stack(t, r)
	if r.Intn(10) == 0 {
		stack = stack[:1]
	}

	return differentialFuzzSimpleVersionedCase(t, differentialFuzzCase{
		seed:   seed,
		family: "slice_store",
		op:     op.name,
		code:   codeFromBuilders(t, op.builder),
		stack:  stack,
	})
}

func generateMathFuzzCase(t *testing.T, r *rand.Rand, seed uint64) differentialFuzzCase {
	t.Helper()

	unaryStack := func(t *testing.T, r *rand.Rand) []any {
		stack := []any{parityFuzzMathValue(t, r)}
		if r.Intn(12) == 0 {
			return nil
		}
		return stack
	}
	binaryStack := func(t *testing.T, r *rand.Rand) []any {
		stack := []any{parityFuzzMathValue(t, r), parityFuzzMathValue(t, r)}
		if r.Intn(12) == 0 {
			return stack[:1]
		}
		return stack
	}
	shiftStack := func(t *testing.T, r *rand.Rand) []any {
		stack := []any{parityFuzzMathValue(t, r), parityFuzzMathShift(t, r)}
		if r.Intn(12) == 0 {
			return stack[:1]
		}
		return stack
	}
	powStack := func(t *testing.T, r *rand.Rand) []any {
		stack := []any{parityFuzzMathShift(t, r)}
		if r.Intn(12) == 0 {
			return nil
		}
		return stack
	}

	ops := []struct {
		name    string
		builder *cell.Builder
		stack   func(*testing.T, *rand.Rand) []any
	}{
		{"NEGATE", mathop.NEGATE().Serialize(), unaryStack},
		{"INC", mathop.INC().Serialize(), unaryStack},
		{"DEC", mathop.DEC().Serialize(), unaryStack},
		{"ABS", mathop.ABS().Serialize(), unaryStack},
		{"NOT", mathop.NOT().Serialize(), unaryStack},
		{"BITSIZE", mathop.BITSIZE().Serialize(), unaryStack},
		{"UBITSIZE", mathop.UBITSIZE().Serialize(), unaryStack},
		{"ISNAN", mathop.ISNAN().Serialize(), unaryStack},
		{"CHKNAN", mathop.CHKNAN().Serialize(), unaryStack},
		{"QNEGATE", mathop.QNEGATE().Serialize(), unaryStack},
		{"QABS", mathop.QABS().Serialize(), unaryStack},
		{"QNOT", mathop.QNOT().Serialize(), unaryStack},
		{"QSGN", mathop.QSGN().Serialize(), unaryStack},
		{"ADD", mathop.SUM().Serialize(), binaryStack},
		{"SUB", mathop.SUB().Serialize(), binaryStack},
		{"SUBR", mathop.SUBR().Serialize(), binaryStack},
		{"MUL", mathop.MUL().Serialize(), binaryStack},
		{"AND", mathop.AND().Serialize(), binaryStack},
		{"OR", mathop.OR().Serialize(), binaryStack},
		{"XOR", mathop.XOR().Serialize(), binaryStack},
		{"MIN", mathop.MIN().Serialize(), binaryStack},
		{"MAX", mathop.MAX().Serialize(), binaryStack},
		{"MINMAX", mathop.MINMAX().Serialize(), binaryStack},
		{"CMP", mathop.CMP().Serialize(), binaryStack},
		{"LESS", mathop.LESS().Serialize(), binaryStack},
		{"EQUAL", mathop.EQUAL().Serialize(), binaryStack},
		{"QAND", mathop.QAND().Serialize(), binaryStack},
		{"QOR", mathop.QOR().Serialize(), binaryStack},
		{"QXOR", mathop.QXOR().Serialize(), binaryStack},
		{"QMINMAX", mathop.QMINMAX().Serialize(), binaryStack},
		{"QCMP", mathop.QCMP().Serialize(), binaryStack},
		{"QLESS", mathop.QLESS().Serialize(), binaryStack},
		{"LSHIFT", mathop.LSHIFT().Serialize(), shiftStack},
		{"RSHIFT", mathop.RSHIFT().Serialize(), shiftStack},
		{"QLSHIFT", mathop.QLSHIFT().Serialize(), shiftStack},
		{"QRSHIFT", mathop.QRSHIFT().Serialize(), shiftStack},
		{"POW2", mathop.POW2().Serialize(), powStack},
		{"QPOW2", mathop.QPOW2().Serialize(), powStack},
	}
	op := ops[r.Intn(len(ops))]
	stack := op.stack(t, r)

	return differentialFuzzSimpleVersionedCase(t, differentialFuzzCase{
		seed:   seed,
		family: "math_basic",
		op:     op.name + differentialFuzzStackSummary(stack),
		code:   codeFromBuilders(t, op.builder),
		stack:  stack,
	})
}

func differentialFuzzStackSummary(values []any) string {
	if len(values) == 0 {
		return "[]"
	}

	parts := make([]string, len(values))
	for i, value := range values {
		switch v := value.(type) {
		case nil:
			parts[i] = "nil"
		case int64:
			parts[i] = fmt.Sprintf("%d", v)
		case *big.Int:
			parts[i] = v.String()
		case vm.NaN:
			parts[i] = "NaN"
		case *cell.Cell:
			parts[i] = fmt.Sprintf("cell(bits=%d,refs=%d)", v.BitsSize(), v.RefsNum())
		case *cell.Slice:
			parts[i] = fmt.Sprintf("slice(bits=%d,refs=%d)", v.BitsLeft(), v.RefsNum())
		case *cell.Builder:
			parts[i] = fmt.Sprintf("builder(bits=%d,refs=%d)", v.BitsUsed(), v.RefsUsed())
		default:
			parts[i] = fmt.Sprintf("%T", value)
		}
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func generateVersionedMessageAddressFuzzCase(t *testing.T, r *rand.Rand, seed uint64) differentialFuzzCase {
	t.Helper()

	version := differentialFuzzSeedVersion(seed)
	addr := parityVersionedMessageAddressSlice(seed, r.Intn(8), r.Intn(32) == 0)
	builder := cell.BeginCell()

	var (
		name  string
		op    *cell.Builder
		stack []any
	)
	switch r.Intn(17) {
	case 0:
		name, op, stack = "LDMSGADDR", funcsop.LDMSGADDR().Serialize(), []any{parityVersionedMessageAddressWithTail(addr)}
	case 1:
		name, op, stack = "LDMSGADDRQ", funcsop.LDMSGADDRQ().Serialize(), []any{parityVersionedMessageAddressWithTail(addr)}
	case 2:
		name, op, stack = "PARSEMSGADDR", funcsop.PARSEMSGADDR().Serialize(), []any{addr}
	case 3:
		name, op, stack = "PARSEMSGADDRQ", funcsop.PARSEMSGADDRQ().Serialize(), []any{addr}
	case 4:
		name, op, stack = "REWRITESTDADDR", funcsop.REWRITESTDADDR().Serialize(), []any{addr}
	case 5:
		name, op, stack = "REWRITESTDADDRQ", funcsop.REWRITESTDADDRQ().Serialize(), []any{addr}
	case 6:
		name, op, stack = "REWRITEVARADDR", funcsop.REWRITEVARADDR().Serialize(), []any{addr}
	case 7:
		name, op, stack = "REWRITEVARADDRQ", funcsop.REWRITEVARADDRQ().Serialize(), []any{addr}
	case 8:
		name, op, stack = "LDSTDADDR", funcsop.LDSTDADDR().Serialize(), []any{parityVersionedMessageAddressWithTail(addr)}
	case 9:
		name, op, stack = "LDSTDADDRQ", funcsop.LDSTDADDRQ().Serialize(), []any{parityVersionedMessageAddressWithTail(addr)}
	case 10:
		name, op, stack = "LDOPTSTDADDR", funcsop.LDOPTSTDADDR().Serialize(), []any{parityVersionedMessageAddressWithTail(addr)}
	case 11:
		name, op, stack = "LDOPTSTDADDRQ", funcsop.LDOPTSTDADDRQ().Serialize(), []any{parityVersionedMessageAddressWithTail(addr)}
	case 12:
		name, op, stack = "STSTDADDR", funcsop.STSTDADDR().Serialize(), []any{addr, builder}
	case 13:
		name, op, stack = "STSTDADDRQ", funcsop.STSTDADDRQ().Serialize(), []any{addr, builder}
	case 14:
		name, op, stack = "STOPTSTDADDR", funcsop.STOPTSTDADDR().Serialize(), []any{addr, builder}
	case 15:
		name, op, stack = "STOPTSTDADDRQ", funcsop.STOPTSTDADDRQ().Serialize(), []any{addr, builder}
	case 16:
		name, op, stack = "STOPTSTDADDR(nil)", funcsop.STOPTSTDADDR().Serialize(), []any{nil, builder}
	}

	tc := differentialFuzzCase{
		seed:   seed,
		family: "msg_address_versioned",
		op:     fmt.Sprintf("%s/%s", name, parityVersionedMessageAddressName(addr)),
		code:   codeFromBuilders(t, op),
		stack:  stack,
	}
	return differentialFuzzWithGlobalVersion(t, tc, version)
}

type parityProgramStackKind uint8

const (
	parityProgramInt parityProgramStackKind = iota
	parityProgramNaN
	parityProgramNull
	parityProgramTuple
	parityProgramCell
	parityProgramSlice
	parityProgramBuilder
)

type parityProgramStackValue struct {
	kind    parityProgramStackKind
	int     *big.Int
	tuple   []parityProgramStackValue
	cell    *cell.Cell
	slice   *cell.Slice
	builder *cell.Builder
}

type parityProgramGenerator struct {
	r             *rand.Rand
	stack         []parityProgramStackValue
	initial       []parityProgramStackValue
	ops           []*cell.Builder
	trace         []string
	seed          []byte
	regD          [2]*cell.Cell
	libTarget     *cell.Cell
	libRef        *cell.Cell
	missingLibRef *cell.Cell
	refLibs       *cell.Cell
	c7            tuple.Tuple
	c7ConfigRoot  *cell.Cell
}

func generateProgramFuzzCase(t *testing.T, r *rand.Rand, seed uint64) differentialFuzzCase {
	t.Helper()

	steps := parityFuzzEnvInt(t, "TVM_PARITY_PROGRAM_OPS", defaultParityProgramOps)
	if steps <= 0 {
		steps = defaultParityProgramOps
	}

	g := newParityProgramGenerator(t, r)
	g.seedInitialStack()
	for i := 0; i < steps; i++ {
		if !g.emitRandomOp() {
			g.emitPushValueOp()
		}
	}
	if len(g.stack) == 0 {
		g.emitPushValueOp()
	}

	return differentialFuzzCase{
		seed:    seed,
		family:  "program",
		op:      strings.Join(g.trace, " -> "),
		code:    parityProgramCodeFromBuilders(t, g.ops...),
		stack:   parityProgramHostStack(g.initial),
		c7:      g.c7,
		refLibs: g.refLibs,
	}
}

func generateVersionedProgramFuzzCase(t *testing.T, r *rand.Rand, seed uint64) differentialFuzzCase {
	t.Helper()

	steps := parityFuzzEnvInt(t, "TVM_PARITY_PROGRAM_OPS", defaultParityProgramOps)
	if steps <= 0 {
		steps = defaultParityProgramOps
	}

	g := newParityProgramGenerator(t, r)
	g.seedInitialStack()
	for i := 0; i < steps; i++ {
		if !g.emitRandomVersionedProgramOp() {
			g.emitPushValueOp()
		}
	}
	if len(g.stack) == 0 {
		g.emitPushValueOp()
	}

	version := differentialFuzzSeedVersion(seed)
	return differentialFuzzCase{
		seed:             seed,
		family:           "program_versioned",
		op:               fmt.Sprintf("%s/v%d", strings.Join(g.trace, " -> "), version),
		code:             parityProgramCodeFromBuilders(t, g.ops...),
		stack:            parityProgramHostStack(g.initial),
		globalVersion:    version,
		globalVersionSet: true,
		refCfg:           tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version))),
		refLibs:          g.refLibs,
	}
}

func generateVersionedDataSizeProgramFuzzCase(t *testing.T, r *rand.Rand, seed uint64) differentialFuzzCase {
	t.Helper()

	steps := parityFuzzEnvInt(t, "TVM_PARITY_PROGRAM_OPS", defaultParityProgramOps)
	if steps <= 0 {
		steps = defaultParityProgramOps
	}

	g := newParityProgramGenerator(t, r)
	g.seedInitialStack()
	for i := 0; i < steps; i++ {
		if !g.emitRandomVersionedDataSizeProgramOp() {
			g.emitPushValueOp()
		}
	}
	if len(g.stack) == 0 {
		g.emitPushValueOp()
	}

	version := differentialFuzzSeedVersion(seed)
	return differentialFuzzCase{
		seed:             seed,
		family:           "program_versioned_datasize",
		op:               fmt.Sprintf("%s/v%d", strings.Join(g.trace, " -> "), version),
		code:             parityProgramCodeFromBuilders(t, g.ops...),
		stack:            parityProgramHostStack(g.initial),
		globalVersion:    version,
		globalVersionSet: true,
		refCfg:           tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version))),
		refLibs:          g.refLibs,
	}
}

func generateVersionedLibrariesProgramFuzzCase(t *testing.T, r *rand.Rand, seed uint64) differentialFuzzCase {
	t.Helper()

	steps := parityFuzzEnvInt(t, "TVM_PARITY_PROGRAM_OPS", defaultParityProgramOps)
	if steps <= 0 {
		steps = defaultParityProgramOps
	}

	g := newParityProgramGenerator(t, r)
	g.seedInitialStack()
	for i := 0; i < steps; i++ {
		if !g.emitRandomVersionedLibrariesProgramOp() {
			g.emitPushValueOp()
		}
	}
	if len(g.stack) == 0 {
		g.emitPushValueOp()
	}

	version := differentialFuzzSeedVersion(seed)
	return differentialFuzzCase{
		seed:             seed,
		family:           "program_versioned_libraries",
		op:               fmt.Sprintf("%s/v%d", strings.Join(g.trace, " -> "), version),
		code:             parityProgramCodeFromBuilders(t, g.ops...),
		stack:            parityProgramHostStack(g.initial),
		globalVersion:    version,
		globalVersionSet: true,
		refCfg:           tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version))),
		refLibs:          g.refLibs,
	}
}

func generateVersionedMessageAddressProgramFuzzCase(t *testing.T, r *rand.Rand, seed uint64) differentialFuzzCase {
	t.Helper()

	steps := parityFuzzEnvInt(t, "TVM_PARITY_PROGRAM_OPS", defaultParityProgramOps)
	if steps <= 0 {
		steps = defaultParityProgramOps
	}

	version := differentialFuzzSeedVersion(seed)
	refCfg := versionedC7ProgramRefConfig(t, version)
	g := newParityProgramGenerator(t, r)
	g.c7ConfigRoot = refCfg.ConfigRoot
	g.seedInitialStack()
	for i := 0; i < steps; i++ {
		if !g.emitRandomVersionedMessageAddressProgramOp() {
			g.emitPushValueOp()
		}
	}
	if len(g.stack) == 0 {
		g.emitPushValueOp()
	}

	return differentialFuzzCase{
		seed:             seed,
		family:           "program_versioned_msg_address",
		op:               fmt.Sprintf("%s/v%d", strings.Join(g.trace, " -> "), version),
		code:             parityProgramCodeFromBuilders(t, g.ops...),
		stack:            parityProgramHostStack(g.initial),
		globalVersion:    version,
		globalVersionSet: true,
		refCfg:           refCfg,
		refLibs:          g.refLibs,
	}
}

func generateVersionedHashVarIntProgramFuzzCase(t *testing.T, r *rand.Rand, seed uint64) differentialFuzzCase {
	t.Helper()

	steps := parityFuzzEnvInt(t, "TVM_PARITY_PROGRAM_OPS", defaultParityProgramOps)
	if steps <= 0 {
		steps = defaultParityProgramOps
	}

	g := newParityProgramGenerator(t, r)
	g.seedInitialStack()
	for i := 0; i < steps; i++ {
		if !g.emitRandomVersionedHashVarIntProgramOp() {
			g.emitPushValueOp()
		}
	}
	if len(g.stack) == 0 {
		g.emitPushValueOp()
	}

	version := differentialFuzzSeedVersion(seed)
	return differentialFuzzCase{
		seed:             seed,
		family:           "program_versioned_hash_varint",
		op:               fmt.Sprintf("%s/v%d", strings.Join(g.trace, " -> "), version),
		code:             parityProgramCodeFromBuilders(t, g.ops...),
		stack:            parityProgramHostStack(g.initial),
		globalVersion:    version,
		globalVersionSet: true,
		refCfg:           tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version))),
		refLibs:          g.refLibs,
	}
}

func generateVersionedPRNGProgramFuzzCase(t *testing.T, r *rand.Rand, seed uint64) differentialFuzzCase {
	t.Helper()

	steps := parityFuzzEnvInt(t, "TVM_PARITY_PROGRAM_OPS", defaultParityProgramOps)
	if steps <= 0 {
		steps = defaultParityProgramOps
	}

	version := differentialFuzzSeedVersion(seed)
	refCfg := versionedC7ProgramRefConfig(t, version)
	g := newParityProgramGenerator(t, r)
	g.c7ConfigRoot = refCfg.ConfigRoot
	g.seedInitialStack()
	for i := 0; i < steps; i++ {
		if !g.emitRandomVersionedPRNGProgramOp() {
			g.emitPushValueOp()
		}
	}
	if len(g.stack) == 0 {
		g.emitPushValueOp()
	}

	return differentialFuzzCase{
		seed:             seed,
		family:           "program_versioned_prng",
		op:               fmt.Sprintf("%s/v%d", strings.Join(g.trace, " -> "), version),
		code:             parityProgramCodeFromBuilders(t, g.ops...),
		stack:            parityProgramHostStack(g.initial),
		globalVersion:    version,
		globalVersionSet: true,
		refCfg:           refCfg,
		refLibs:          g.refLibs,
	}
}

func generateVersionedMathProgramFuzzCase(t *testing.T, r *rand.Rand, seed uint64) differentialFuzzCase {
	t.Helper()

	steps := parityFuzzEnvInt(t, "TVM_PARITY_PROGRAM_OPS", defaultParityProgramOps)
	if steps <= 0 {
		steps = defaultParityProgramOps
	}

	g := newParityProgramGenerator(t, r)
	g.seedInitialStack()
	for i := 0; i < steps; i++ {
		if !g.emitRandomVersionedMathProgramOp() {
			g.emitPushValueOp()
		}
	}
	if len(g.stack) == 0 {
		g.emitPushValueOp()
	}

	version := differentialFuzzSeedVersion(seed)
	return differentialFuzzCase{
		seed:             seed,
		family:           "program_versioned_math",
		op:               fmt.Sprintf("%s/v%d", strings.Join(g.trace, " -> "), version),
		code:             parityProgramCodeFromBuilders(t, g.ops...),
		stack:            parityProgramHostStack(g.initial),
		globalVersion:    version,
		globalVersionSet: true,
		refCfg:           tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version))),
		refLibs:          g.refLibs,
	}
}

func generateVersionedCellSliceProgramFuzzCase(t *testing.T, r *rand.Rand, seed uint64) differentialFuzzCase {
	t.Helper()

	steps := parityFuzzEnvInt(t, "TVM_PARITY_PROGRAM_OPS", defaultParityProgramOps)
	if steps <= 0 {
		steps = defaultParityProgramOps
	}

	g := newParityProgramGenerator(t, r)
	g.seedInitialStack()
	for i := 0; i < steps; i++ {
		if !g.emitRandomVersionedCellSliceProgramOp() {
			g.emitPushValueOp()
		}
	}
	if len(g.stack) == 0 {
		g.emitPushValueOp()
	}

	version := differentialFuzzSeedVersion(seed)
	return differentialFuzzCase{
		seed:             seed,
		family:           "program_versioned_cellslice",
		op:               fmt.Sprintf("%s/v%d", strings.Join(g.trace, " -> "), version),
		code:             parityProgramCodeFromBuilders(t, g.ops...),
		stack:            parityProgramHostStack(g.initial),
		globalVersion:    version,
		globalVersionSet: true,
		refCfg:           tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version))),
		refLibs:          g.refLibs,
	}
}

func generateVersionedControlProgramFuzzCase(t *testing.T, r *rand.Rand, seed uint64) differentialFuzzCase {
	t.Helper()

	steps := parityFuzzEnvInt(t, "TVM_PARITY_PROGRAM_OPS", defaultParityProgramOps)
	if steps <= 0 {
		steps = defaultParityProgramOps
	}

	g := newParityProgramGenerator(t, r)
	g.seedInitialStack()
	for i := 0; i < steps; i++ {
		if !g.emitRandomVersionedControlProgramOp() {
			g.emitPushValueOp()
		}
	}
	if len(g.stack) == 0 {
		g.emitPushValueOp()
	}

	version := differentialFuzzSeedVersion(seed)
	return differentialFuzzCase{
		seed:             seed,
		family:           "program_versioned_control",
		op:               fmt.Sprintf("%s/v%d", strings.Join(g.trace, " -> "), version),
		code:             parityProgramCodeFromBuilders(t, g.ops...),
		stack:            parityProgramHostStack(g.initial),
		globalVersion:    version,
		globalVersionSet: true,
		refCfg:           tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version))),
		refLibs:          g.refLibs,
	}
}

func generateVersionedExecProgramFuzzCase(t *testing.T, r *rand.Rand, seed uint64) differentialFuzzCase {
	t.Helper()

	steps := parityFuzzEnvInt(t, "TVM_PARITY_PROGRAM_OPS", defaultParityProgramOps)
	if steps <= 0 {
		steps = defaultParityProgramOps
	}

	g := newParityProgramGenerator(t, r)
	g.seedInitialStack()
	for i := 0; i < steps; i++ {
		if !g.emitRandomVersionedExecProgramOp() {
			g.emitPushValueOp()
		}
	}
	if len(g.stack) == 0 {
		g.emitPushValueOp()
	}

	version := differentialFuzzSeedVersion(seed)
	return differentialFuzzCase{
		seed:             seed,
		family:           "program_versioned_exec",
		op:               fmt.Sprintf("%s/v%d", strings.Join(g.trace, " -> "), version),
		code:             parityProgramCodeFromBuilders(t, g.ops...),
		stack:            parityProgramHostStack(g.initial),
		globalVersion:    version,
		globalVersionSet: true,
		refCfg:           tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version))),
		refLibs:          g.refLibs,
	}
}

func generateVersionedDictProgramFuzzCase(t *testing.T, r *rand.Rand, seed uint64) differentialFuzzCase {
	t.Helper()

	steps := parityFuzzEnvInt(t, "TVM_PARITY_PROGRAM_OPS", defaultParityProgramOps)
	if steps <= 0 {
		steps = defaultParityProgramOps
	}

	g := newParityProgramGenerator(t, r)
	g.seedInitialStack()
	for i := 0; i < steps; i++ {
		if !g.emitRandomVersionedDictProgramOp() {
			g.emitPushValueOp()
		}
	}
	if len(g.stack) == 0 {
		g.emitPushValueOp()
	}

	version := differentialFuzzSeedVersion(seed)
	return differentialFuzzCase{
		seed:             seed,
		family:           "program_versioned_dict",
		op:               fmt.Sprintf("%s/v%d", strings.Join(g.trace, " -> "), version),
		code:             parityProgramCodeFromBuilders(t, g.ops...),
		stack:            parityProgramHostStack(g.initial),
		globalVersion:    version,
		globalVersionSet: true,
		refCfg:           tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version))),
		refLibs:          g.refLibs,
	}
}

func generateVersionedStackProgramFuzzCase(t *testing.T, r *rand.Rand, seed uint64) differentialFuzzCase {
	t.Helper()

	steps := parityFuzzEnvInt(t, "TVM_PARITY_PROGRAM_OPS", defaultParityProgramOps)
	if steps <= 0 {
		steps = defaultParityProgramOps
	}

	g := newParityProgramGenerator(t, r)
	g.seedInitialStack()
	for i := 0; i < steps; i++ {
		if !g.emitRandomVersionedStackProgramOp() {
			g.emitPushValueOp()
		}
	}
	if len(g.stack) == 0 {
		g.emitPushValueOp()
	}

	version := differentialFuzzSeedVersion(seed)
	return differentialFuzzCase{
		seed:             seed,
		family:           "program_versioned_stack",
		op:               fmt.Sprintf("%s/v%d", strings.Join(g.trace, " -> "), version),
		code:             parityProgramCodeFromBuilders(t, g.ops...),
		stack:            parityProgramHostStack(g.initial),
		globalVersion:    version,
		globalVersionSet: true,
		refCfg:           tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version))),
		refLibs:          g.refLibs,
	}
}

func generateVersionedTupleProgramFuzzCase(t *testing.T, r *rand.Rand, seed uint64) differentialFuzzCase {
	t.Helper()

	steps := parityFuzzEnvInt(t, "TVM_PARITY_PROGRAM_OPS", defaultParityProgramOps)
	if steps <= 0 {
		steps = defaultParityProgramOps
	}

	g := newParityProgramGenerator(t, r)
	g.seedInitialStack()
	for i := 0; i < steps; i++ {
		if !g.emitRandomVersionedTupleProgramOp() {
			g.emitPushValueOp()
		}
	}
	if len(g.stack) == 0 {
		g.emitPushValueOp()
	}

	version := differentialFuzzSeedVersion(seed)
	return differentialFuzzCase{
		seed:             seed,
		family:           "program_versioned_tuple",
		op:               fmt.Sprintf("%s/v%d", strings.Join(g.trace, " -> "), version),
		code:             parityProgramCodeFromBuilders(t, g.ops...),
		stack:            parityProgramHostStack(g.initial),
		globalVersion:    version,
		globalVersionSet: true,
		refCfg:           tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version))),
		refLibs:          g.refLibs,
	}
}

func generateVersionedRunVMProgramFuzzCase(t *testing.T, r *rand.Rand, seed uint64) differentialFuzzCase {
	t.Helper()

	steps := parityFuzzEnvInt(t, "TVM_PARITY_PROGRAM_OPS", defaultParityProgramOps)
	if steps <= 0 {
		steps = defaultParityProgramOps
	}

	version := differentialFuzzSeedVersion(seed)
	g := newParityProgramGenerator(t, r)
	g.seedInitialStack()
	for i := 0; i < steps; i++ {
		if !g.emitRandomVersionedRunVMProgramOp(version) {
			g.emitPushValueOp()
		}
	}
	if len(g.stack) == 0 {
		g.emitPushValueOp()
	}

	return differentialFuzzCase{
		seed:             seed,
		family:           "program_versioned_runvm",
		op:               fmt.Sprintf("%s/v%d", strings.Join(g.trace, " -> "), version),
		code:             parityProgramCodeFromBuilders(t, g.ops...),
		stack:            parityProgramHostStack(g.initial),
		globalVersion:    version,
		globalVersionSet: true,
		refCfg:           tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version))),
		refLibs:          g.refLibs,
	}
}

func generateVersionedRunVMRichC7ProgramFuzzCase(t *testing.T, r *rand.Rand, seed uint64) differentialFuzzCase {
	t.Helper()

	steps := parityFuzzEnvInt(t, "TVM_PARITY_PROGRAM_OPS", defaultParityProgramOps)
	if steps <= 0 {
		steps = defaultParityProgramOps
	}

	version := differentialFuzzSeedVersion(seed)
	configRoot := versionedC7ProgramConfigRoot(t, version)
	g := newParityProgramGenerator(t, r)
	g.c7 = parityProgramVersionedRichC7(t, version)
	g.c7ConfigRoot = configRoot
	g.seedInitialStack()
	for i := 0; i < steps; i++ {
		if !g.emitRandomVersionedRunVMProgramOp(version) {
			g.emitPushValueOp()
		}
	}
	if len(g.stack) == 0 {
		g.emitPushValueOp()
	}

	return differentialFuzzCase{
		seed:             seed,
		family:           "program_versioned_runvm_rich_c7",
		op:               fmt.Sprintf("%s/v%d", strings.Join(g.trace, " -> "), version),
		code:             parityProgramCodeFromBuilders(t, g.ops...),
		stack:            parityProgramHostStack(g.initial),
		globalVersion:    version,
		globalVersionSet: true,
		rawC7Versioned:   true,
		c7:               g.c7,
		refLibs:          g.refLibs,
	}
}

func generateVersionedRuntimeProgramFuzzCase(t *testing.T, r *rand.Rand, seed uint64) differentialFuzzCase {
	t.Helper()

	steps := parityFuzzEnvInt(t, "TVM_PARITY_PROGRAM_OPS", defaultParityProgramOps)
	if steps <= 0 {
		steps = defaultParityProgramOps
	}

	version := differentialFuzzSeedVersion(seed)
	configRoot := versionedC7ProgramConfigRoot(t, version)
	g := newParityProgramGenerator(t, r)
	g.c7 = parityProgramVersionedRichC7(t, version)
	g.c7ConfigRoot = configRoot
	g.seedInitialStack()
	for i := 0; i < steps; i++ {
		if !g.emitRandomVersionedRuntimeProgramOp() {
			g.emitPushValueOp()
		}
	}
	if len(g.stack) == 0 {
		g.emitPushValueOp()
	}

	return differentialFuzzCase{
		seed:             seed,
		family:           "program_versioned_runtime",
		op:               fmt.Sprintf("%s/v%d", strings.Join(g.trace, " -> "), version),
		code:             parityProgramCodeFromBuilders(t, g.ops...),
		stack:            parityProgramHostStack(g.initial),
		globalVersion:    version,
		globalVersionSet: true,
		rawC7Versioned:   true,
		c7:               g.c7,
		refLibs:          g.refLibs,
	}
}

func generateVersionedActionsProgramFuzzCase(t *testing.T, r *rand.Rand, seed uint64) differentialFuzzCase {
	t.Helper()

	steps := parityFuzzEnvInt(t, "TVM_PARITY_PROGRAM_OPS", defaultParityProgramOps)
	if steps <= 0 {
		steps = defaultParityProgramOps
	}

	version := differentialFuzzSeedVersion(seed)
	configRoot := versionedC7ProgramConfigRoot(t, version)
	g := newParityProgramGenerator(t, r)
	g.c7 = parityProgramVersionedRichC7(t, version)
	g.c7ConfigRoot = configRoot
	g.seedInitialStack()
	for i := 0; i < steps; i++ {
		if !g.emitRandomVersionedActionsProgramOp() {
			g.emitPushValueOp()
		}
	}
	if len(g.stack) == 0 {
		g.emitPushValueOp()
	}

	return differentialFuzzCase{
		seed:             seed,
		family:           "program_versioned_actions",
		op:               fmt.Sprintf("%s/v%d", strings.Join(g.trace, " -> "), version),
		code:             parityProgramCodeFromBuilders(t, g.ops...),
		stack:            parityProgramHostStack(g.initial),
		globalVersion:    version,
		globalVersionSet: true,
		rawC7Versioned:   true,
		c7:               g.c7,
		refLibs:          g.refLibs,
	}
}

func generateVersionedC7ProgramFuzzCase(t *testing.T, r *rand.Rand, seed uint64) differentialFuzzCase {
	t.Helper()

	steps := parityFuzzEnvInt(t, "TVM_PARITY_PROGRAM_OPS", defaultParityProgramOps)
	if steps <= 0 {
		steps = defaultParityProgramOps
	}

	version := differentialFuzzSeedVersion(seed)
	refCfg := versionedC7ProgramRefConfig(t, version)
	g := newParityProgramGenerator(t, r)
	g.c7ConfigRoot = refCfg.ConfigRoot
	g.seedInitialStack()
	for i := 0; i < steps; i++ {
		if !g.emitRandomVersionedC7ProgramOp() {
			g.emitPushValueOp()
		}
	}
	if len(g.stack) == 0 {
		g.emitPushValueOp()
	}

	return differentialFuzzCase{
		seed:             seed,
		family:           "program_versioned_c7",
		op:               fmt.Sprintf("%s/v%d", strings.Join(g.trace, " -> "), version),
		code:             parityProgramCodeFromBuilders(t, g.ops...),
		stack:            parityProgramHostStack(g.initial),
		globalVersion:    version,
		globalVersionSet: true,
		refCfg:           refCfg,
		refLibs:          g.refLibs,
	}
}

func generateVersionedRichC7ProgramFuzzCase(t *testing.T, r *rand.Rand, seed uint64) differentialFuzzCase {
	t.Helper()

	steps := parityFuzzEnvInt(t, "TVM_PARITY_PROGRAM_OPS", defaultParityProgramOps)
	if steps <= 0 {
		steps = defaultParityProgramOps
	}

	version := differentialFuzzSeedVersion(seed)
	configRoot := versionedC7ProgramConfigRoot(t, version)
	g := newParityProgramGenerator(t, r)
	g.c7 = parityProgramVersionedRichC7(t, version)
	g.c7ConfigRoot = configRoot
	g.seedInitialStack()
	for i := 0; i < steps; i++ {
		if !g.emitRandomVersionedRichC7ProgramOp() {
			g.emitPushValueOp()
		}
	}
	if len(g.stack) == 0 {
		g.emitPushValueOp()
	}

	return differentialFuzzCase{
		seed:             seed,
		family:           "program_versioned_rich_c7",
		op:               fmt.Sprintf("%s/v%d", strings.Join(g.trace, " -> "), version),
		code:             parityProgramCodeFromBuilders(t, g.ops...),
		stack:            parityProgramHostStack(g.initial),
		globalVersion:    version,
		globalVersionSet: true,
		rawC7Versioned:   true,
		c7:               g.c7,
		refLibs:          g.refLibs,
	}
}

func generateVersionedSupercontractProgramFuzzCase(t *testing.T, r *rand.Rand, seed uint64) differentialFuzzCase {
	t.Helper()

	steps := parityFuzzEnvInt(t, "TVM_PARITY_PROGRAM_OPS", defaultParityProgramOps)
	if steps <= 0 {
		steps = defaultParityProgramOps
	}

	version := differentialFuzzSeedVersion(seed)
	configRoot := versionedC7ProgramConfigRoot(t, version)
	g := newParityProgramGenerator(t, r)
	g.c7 = parityProgramVersionedRichC7(t, version)
	g.c7ConfigRoot = configRoot
	g.seedInitialStack()
	for i := 0; i < steps; i++ {
		if !g.emitRandomVersionedSupercontractProgramOp(version) {
			g.emitPushValueOp()
		}
	}
	if len(g.stack) == 0 {
		g.emitPushValueOp()
	}

	return differentialFuzzCase{
		seed:             seed,
		family:           "program_versioned_supercontract",
		op:               fmt.Sprintf("%s/v%d", strings.Join(g.trace, " -> "), version),
		code:             parityProgramCodeFromBuilders(t, g.ops...),
		stack:            parityProgramHostStack(g.initial),
		globalVersion:    version,
		globalVersionSet: true,
		rawC7Versioned:   true,
		c7:               g.c7,
		refLibs:          g.refLibs,
	}
}

func versionedC7ProgramRefConfig(t *testing.T, version int) *referenceGetMethodConfig {
	t.Helper()

	cfg := tonopsCrossRefConfig(versionedC7ProgramConfigRoot(t, version))
	cfg.PrevBlocks = versionedC7ProgramPrevBlocksTuple()
	return cfg
}

func versionedC7ProgramConfigRoot(t *testing.T, version int) *cell.Cell {
	t.Helper()

	versionCell, err := tlb.ToCell(&tlb.GlobalVersion{Version: uint32(version)})
	if err != nil {
		t.Fatalf("failed to build global version config: %v", err)
	}
	globalIDCell, err := tlb.ToCell(&tlb.GlobalIDConfig{GlobalID: tonopsTestGlobalID})
	if err != nil {
		t.Fatalf("failed to build global id config: %v", err)
	}

	return mustConfigDictCell(t, map[uint32]*cell.Cell{
		7:                                    versionedC7ProgramConfigValue(),
		uint32(tlb.ConfigParamGlobalID):      globalIDCell,
		uint32(tlb.ConfigParamGlobalVersion): versionCell,
	})
}

func parityProgramVersionedRichC7(t *testing.T, version int) tuple.Tuple {
	t.Helper()

	configRoot := versionedC7ProgramConfigRoot(t, version)
	return makeTonopsTestC7(t, tonopsTestC7Config{
		ConfigRoot:     configRoot,
		LegacyConfig:   configRoot,
		UnpackedConfig: parityProgramTonFuncUnpackedConfig(t),
		ExtraParams: map[int]any{
			13: versionedC7ProgramPrevBlocksTuple(),
			15: int64(444),
			16: int64(555),
			17: makeInMsgParamsTuple(),
		},
	})
}

func versionedC7ProgramConfigValue() *cell.Cell {
	return cell.BeginCell().MustStoreUInt(0xC7C7, 16).EndCell()
}

func versionedC7ProgramPrevBlocksTuple() tuple.Tuple {
	return tuple.NewTupleValue(big.NewInt(111), big.NewInt(222), big.NewInt(333))
}

func versionedC7ProgramPrevBlocksValue() parityProgramStackValue {
	return parityProgramStackValue{
		kind: parityProgramTuple,
		tuple: []parityProgramStackValue{
			parityProgramIntValue(big.NewInt(111)),
			parityProgramIntValue(big.NewInt(222)),
			parityProgramIntValue(big.NewInt(333)),
		},
	}
}

func parityProgramInMsgParamsValue() parityProgramStackValue {
	return parityProgramStackValue{
		kind: parityProgramTuple,
		tuple: []parityProgramStackValue{
			parityProgramIntValue(big.NewInt(1)),
			parityProgramIntValue(big.NewInt(0)),
			{kind: parityProgramSlice, slice: cell.BeginCell().MustStoreAddr(address.MustParseAddr("EQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAM9c")).ToSlice()},
			parityProgramIntValue(big.NewInt(11)),
			parityProgramIntValue(big.NewInt(22)),
			parityProgramIntValue(big.NewInt(33)),
			{
				kind: parityProgramTuple,
				tuple: []parityProgramStackValue{
					parityProgramIntValue(big.NewInt(44)),
					{kind: parityProgramNull},
				},
			},
			{
				kind: parityProgramTuple,
				tuple: []parityProgramStackValue{
					parityProgramIntValue(big.NewInt(55)),
					{kind: parityProgramNull},
				},
			},
			{kind: parityProgramNull},
			{kind: parityProgramNull},
		},
	}
}

func parityProgramRunVMInMsgParamsValue() parityProgramStackValue {
	return parityProgramStackValue{
		kind: parityProgramTuple,
		tuple: []parityProgramStackValue{
			parityProgramIntValue(big.NewInt(1)),
			parityProgramIntValue(big.NewInt(2)),
		},
	}
}

func parityProgramRunVMInMsgParamsChildC7InnerValue() parityProgramStackValue {
	items := make([]parityProgramStackValue, 18)
	for i := range items {
		items[i] = parityProgramStackValue{kind: parityProgramNull}
	}
	items[17] = parityProgramRunVMInMsgParamsValue()
	return parityProgramStackValue{kind: parityProgramTuple, tuple: items}
}

func parityProgramRunVMInMsgParamsChildC7Value() parityProgramStackValue {
	return parityProgramStackValue{
		kind:  parityProgramTuple,
		tuple: []parityProgramStackValue{parityProgramRunVMInMsgParamsChildC7InnerValue()},
	}
}

func newParityProgramGenerator(t *testing.T, r *rand.Rand) *parityProgramGenerator {
	t.Helper()

	empty := testEmptyCell()
	libTarget := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	missingTarget := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	return &parityProgramGenerator{
		r:             r,
		seed:          append([]byte(nil), tonopsTestSeed...),
		libTarget:     libTarget,
		libRef:        mustCrossLibraryCellForHash(t, libTarget.Hash()),
		missingLibRef: mustCrossLibraryCellForHash(t, missingTarget.Hash()),
		refLibs:       mustCrossLibraryCollection(t, libTarget),
		c7:            parityProgramRichC7(t),
		regD: [2]*cell.Cell{
			empty,
			empty,
		},
	}
}

func parityProgramRichC7(t *testing.T) tuple.Tuple {
	t.Helper()

	return makeTonopsTestC7(t, tonopsTestC7Config{
		UnpackedConfig: parityProgramTonFuncUnpackedConfig(t),
		ExtraParams: map[int]any{
			13: tuple.NewTupleValue(big.NewInt(111), big.NewInt(222), big.NewInt(333)),
			15: int64(444),
			16: int64(555),
			17: makeInMsgParamsTuple(),
		},
	})
}

func parityProgramCodeFromBuilders(t *testing.T, builders ...*cell.Builder) *cell.Cell {
	t.Helper()

	var chunks [][]*cell.Builder
	var chunk []*cell.Builder
	bits := uint(0)
	refs := 0
	flush := func() {
		if len(chunk) == 0 {
			return
		}
		chunks = append(chunks, chunk)
		chunk = nil
		bits = 0
		refs = 0
	}

	for _, builder := range builders {
		needBits := builder.BitsUsed()
		needRefs := builder.RefsUsed()
		if len(chunk) > 0 && (bits+needBits+parityProgramChunkReserveBits >= 1024 || refs+needRefs+1 > 4) {
			flush()
		}
		chunk = append(chunk, builder)
		bits += needBits
		refs += needRefs
	}
	flush()

	var next *cell.Cell
	for i := len(chunks) - 1; i >= 0; i-- {
		builder := cell.BeginCell()
		for _, instr := range chunks[i] {
			builder.MustStoreBuilder(instr)
		}
		if next != nil {
			builder.MustStoreBuilder(execop.JMPREF(next).Serialize())
		}
		next = builder.EndCell()
	}
	if next == nil {
		return cell.BeginCell().EndCell()
	}
	return next
}

func (g *parityProgramGenerator) seedInitialStack() {
	depth := 2 + g.r.Intn(5)
	g.stack = make([]parityProgramStackValue, 0, depth)
	for i := 0; i < depth; i++ {
		g.stack = append(g.stack, g.randomInitialValue(0))
	}
	g.initial = parityProgramCloneStack(g.stack)
}

func (g *parityProgramGenerator) randomInitialValue(depth int) parityProgramStackValue {
	if depth < 2 && g.r.Intn(5) == 0 {
		ln := g.r.Intn(4)
		items := make([]parityProgramStackValue, ln)
		for i := range items {
			items[i] = g.randomInitialValue(depth + 1)
		}
		return parityProgramStackValue{kind: parityProgramTuple, tuple: items}
	}
	switch g.r.Intn(12) {
	case 0:
		return parityProgramStackValue{kind: parityProgramNull}
	case 1:
		return parityProgramStackValue{kind: parityProgramCell, cell: g.randomCell()}
	case 2:
		return parityProgramStackValue{kind: parityProgramSlice, slice: g.randomCell().MustBeginParse()}
	case 3:
		return parityProgramStackValue{kind: parityProgramBuilder, builder: g.randomBuilder()}
	default:
		return parityProgramIntValue(g.smallInt())
	}
}

func (g *parityProgramGenerator) emitRandomOp() bool {
	for i := 0; i < 32; i++ {
		switch g.r.Intn(32) {
		case 0, 1:
			return g.emitPushValueOp()
		case 2, 3, 4, 5:
			if g.emitStackOp() {
				return true
			}
		case 6, 7, 8, 9:
			if g.emitMathOp() {
				return true
			}
		case 10, 11, 12:
			if g.emitCellSliceOp() {
				return true
			}
		case 13, 14, 15:
			if g.emitDictOp() {
				return true
			}
		case 16, 17:
			if g.emitControlOp() {
				return true
			}
		case 18, 19:
			if g.emitFuncParamOp() {
				return true
			}
		case 20, 21:
			if g.emitMessageAddressOp() {
				return true
			}
		case 22, 23, 24:
			if g.emitRuntimeControlOp() {
				return true
			}
		case 25:
			if g.emitRunVMOp() {
				return true
			}
		default:
			if g.emitTupleOp() {
				return true
			}
		}
	}
	return false
}

func (g *parityProgramGenerator) emitRandomVersionedProgramOp() bool {
	for i := 0; i < 32; i++ {
		switch g.r.Intn(28) {
		case 0, 1:
			return g.emitPushValueOp()
		case 2, 3, 4, 5:
			if g.emitStackOp() {
				return true
			}
		case 6, 7, 8, 9:
			if g.emitMathOp() {
				return true
			}
		case 10, 11, 12, 13:
			if g.emitCellSliceOp() {
				return true
			}
		case 14, 15, 16, 17:
			if g.emitDictOp() {
				return true
			}
		case 18, 19:
			if g.emitControlOp() {
				return true
			}
		case 20, 21, 22:
			if g.emitVersionedPureFuncOp() {
				return true
			}
		default:
			if g.emitTupleOp() {
				return true
			}
		}
	}
	return false
}

func (g *parityProgramGenerator) emitRandomVersionedDataSizeProgramOp() bool {
	for i := 0; i < 32; i++ {
		switch g.r.Intn(24) {
		case 0, 1:
			return g.emitPushValueOp()
		case 2, 3:
			if g.emitStackOp() {
				return true
			}
		case 4:
			if g.emitMathOp() {
				return true
			}
		case 5:
			if g.emitCellSliceOp() {
				return true
			}
		case 6:
			if g.emitDictOp() {
				return true
			}
		case 7:
			if g.emitControlOp() {
				return true
			}
		case 8, 9, 10:
			if g.emitDataSizeProgramOp(false, false) {
				return true
			}
		case 11, 12, 13:
			if g.emitDataSizeProgramOp(false, true) {
				return true
			}
		case 14, 15, 16:
			if g.emitDataSizeProgramOp(true, false) {
				return true
			}
		case 17, 18, 19:
			if g.emitDataSizeProgramOp(true, true) {
				return true
			}
		case 20:
			if g.emitVersionedPureFuncOp() {
				return true
			}
		default:
			if g.emitTupleOp() {
				return true
			}
		}
	}
	return false
}

func (g *parityProgramGenerator) emitRandomVersionedLibrariesProgramOp() bool {
	for i := 0; i < 32; i++ {
		switch g.r.Intn(24) {
		case 0, 1:
			return g.emitPushValueOp()
		case 2, 3:
			if g.emitStackOp() {
				return true
			}
		case 4:
			if g.emitMathOp() {
				return true
			}
		case 5, 6:
			if g.emitCellSliceOp() {
				return true
			}
		case 7:
			if g.emitDictOp() {
				return true
			}
		case 8:
			if g.emitControlOp() {
				return true
			}
		case 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21:
			if g.emitLibraryResolutionOp(g.r.Intn(6)) {
				return true
			}
		default:
			if g.emitTupleOp() {
				return true
			}
		}
	}
	return false
}

func (g *parityProgramGenerator) emitRandomVersionedMessageAddressProgramOp() bool {
	for i := 0; i < 32; i++ {
		switch g.r.Intn(24) {
		case 0, 1:
			return g.emitPushValueOp()
		case 2, 3:
			if g.emitStackOp() {
				return true
			}
		case 4:
			if g.emitMathOp() {
				return true
			}
		case 5:
			if g.emitCellSliceOp() {
				return true
			}
		case 6:
			if g.emitDictOp() {
				return true
			}
		case 7:
			if g.emitControlOp() {
				return true
			}
		case 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20:
			if g.emitMessageAddressOp() {
				return true
			}
		case 21:
			if g.emitVersionedC7ParamOp() {
				return true
			}
		case 22:
			if g.emitVersionedPureFuncOp() {
				return true
			}
		default:
			if g.emitTupleOp() {
				return true
			}
		}
	}
	return false
}

func (g *parityProgramGenerator) emitRandomVersionedHashVarIntProgramOp() bool {
	for i := 0; i < 32; i++ {
		switch g.r.Intn(24) {
		case 0, 1:
			return g.emitPushValueOp()
		case 2, 3:
			if g.emitStackOp() {
				return true
			}
		case 4:
			if g.emitMathOp() {
				return true
			}
		case 5:
			if g.emitCellSliceOp() {
				return true
			}
		case 6:
			if g.emitDictOp() {
				return true
			}
		case 7:
			if g.emitControlOp() {
				return true
			}
		case 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20:
			if g.emitFuncHashVarIntOp(g.r.Intn(15)) {
				return true
			}
		case 21:
			if g.emitVersionedPureFuncOp() {
				return true
			}
		default:
			if g.emitTupleOp() {
				return true
			}
		}
	}
	return false
}

func (g *parityProgramGenerator) emitRandomVersionedPRNGProgramOp() bool {
	for i := 0; i < 32; i++ {
		switch g.r.Intn(24) {
		case 0, 1:
			return g.emitPushValueOp()
		case 2, 3:
			if g.emitStackOp() {
				return true
			}
		case 4:
			if g.emitMathOp() {
				return true
			}
		case 5:
			if g.emitCellSliceOp() {
				return true
			}
		case 6:
			if g.emitDictOp() {
				return true
			}
		case 7:
			if g.emitControlOp() {
				return true
			}
		case 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20:
			if g.emitPrngOp(g.r.Intn(7)) {
				return true
			}
		case 21:
			if g.emitVersionedC7ParamOp() {
				return true
			}
		case 22:
			if g.emitVersionedPureFuncOp() {
				return true
			}
		default:
			if g.emitTupleOp() {
				return true
			}
		}
	}
	return false
}

func (g *parityProgramGenerator) emitRandomVersionedMathProgramOp() bool {
	for i := 0; i < 32; i++ {
		switch g.r.Intn(24) {
		case 0, 1, 2:
			return g.emitPushValueOp()
		case 3, 4:
			if g.emitStackOp() {
				return true
			}
		case 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17:
			if g.emitMathOp() {
				return true
			}
		case 18:
			if g.emitCellSliceOp() {
				return true
			}
		case 19:
			if g.emitDictOp() {
				return true
			}
		case 20:
			if g.emitControlOp() {
				return true
			}
		case 21:
			if g.emitVersionedPureFuncOp() {
				return true
			}
		default:
			if g.emitTupleOp() {
				return true
			}
		}
	}
	return false
}

func (g *parityProgramGenerator) emitRandomVersionedCellSliceProgramOp() bool {
	for i := 0; i < 32; i++ {
		switch g.r.Intn(24) {
		case 0, 1:
			return g.emitPushValueOp()
		case 2, 3:
			if g.emitStackOp() {
				return true
			}
		case 4:
			if g.emitMathOp() {
				return true
			}
		case 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19:
			if g.emitCellSliceOp() {
				return true
			}
		case 20:
			if g.emitDictOp() {
				return true
			}
		case 21:
			if g.emitControlOp() {
				return true
			}
		case 22:
			if g.emitVersionedPureFuncOp() {
				return true
			}
		default:
			if g.emitTupleOp() {
				return true
			}
		}
	}
	return false
}

func (g *parityProgramGenerator) emitRandomVersionedControlProgramOp() bool {
	for i := 0; i < 32; i++ {
		switch g.r.Intn(24) {
		case 0, 1:
			return g.emitPushValueOp()
		case 2, 3:
			if g.emitStackOp() {
				return true
			}
		case 4:
			if g.emitMathOp() {
				return true
			}
		case 5:
			if g.emitCellSliceOp() {
				return true
			}
		case 6:
			if g.emitDictOp() {
				return true
			}
		case 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20:
			if g.emitControlOp() {
				return true
			}
		case 21:
			if g.emitVersionedPureFuncOp() {
				return true
			}
		default:
			if g.emitTupleOp() {
				return true
			}
		}
	}
	return false
}

func (g *parityProgramGenerator) emitRandomVersionedExecProgramOp() bool {
	for i := 0; i < 32; i++ {
		switch g.r.Intn(24) {
		case 0, 1:
			return g.emitPushValueOp()
		case 2, 3:
			if g.emitStackOp() {
				return true
			}
		case 4:
			if g.emitMathOp() {
				return true
			}
		case 5:
			if g.emitCellSliceOp() {
				return true
			}
		case 6:
			if g.emitDictOp() {
				return true
			}
		case 7:
			if g.emitControlOp() {
				return true
			}
		case 8, 9, 10, 11, 12, 13:
			if g.emitExecBranchLoopOp(g.r.Intn(11)) {
				return true
			}
		case 14, 15, 16, 17, 18, 19, 20, 21:
			if g.emitContinuationControlOp(g.r.Intn(12)) {
				return true
			}
		case 22:
			if g.emitVersionedPureFuncOp() {
				return true
			}
		default:
			if g.emitTupleOp() {
				return true
			}
		}
	}
	return false
}

func (g *parityProgramGenerator) emitRandomVersionedDictProgramOp() bool {
	for i := 0; i < 32; i++ {
		switch g.r.Intn(24) {
		case 0, 1:
			return g.emitPushValueOp()
		case 2, 3:
			if g.emitStackOp() {
				return true
			}
		case 4:
			if g.emitMathOp() {
				return true
			}
		case 5:
			if g.emitCellSliceOp() {
				return true
			}
		case 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19:
			if g.emitDictOp() {
				return true
			}
		case 20:
			if g.emitControlOp() {
				return true
			}
		case 21:
			if g.emitVersionedPureFuncOp() {
				return true
			}
		default:
			if g.emitTupleOp() {
				return true
			}
		}
	}
	return false
}

func (g *parityProgramGenerator) emitRandomVersionedStackProgramOp() bool {
	for i := 0; i < 32; i++ {
		switch g.r.Intn(24) {
		case 0, 1:
			return g.emitPushValueOp()
		case 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15:
			if g.emitStackOp() {
				return true
			}
		case 16, 17:
			if g.emitMathOp() {
				return true
			}
		case 18:
			if g.emitCellSliceOp() {
				return true
			}
		case 19:
			if g.emitDictOp() {
				return true
			}
		case 20:
			if g.emitControlOp() {
				return true
			}
		case 21:
			if g.emitVersionedPureFuncOp() {
				return true
			}
		default:
			if g.emitTupleOp() {
				return true
			}
		}
	}
	return false
}

func (g *parityProgramGenerator) emitRandomVersionedTupleProgramOp() bool {
	for i := 0; i < 32; i++ {
		switch g.r.Intn(24) {
		case 0, 1, 2:
			return g.emitPushValueOp()
		case 3, 4:
			if g.emitStackOp() {
				return true
			}
		case 5:
			if g.emitMathOp() {
				return true
			}
		case 6:
			if g.emitCellSliceOp() {
				return true
			}
		case 7:
			if g.emitDictOp() {
				return true
			}
		case 8:
			if g.emitControlOp() {
				return true
			}
		case 9:
			if g.emitVersionedPureFuncOp() {
				return true
			}
		default:
			if g.emitTupleOp() {
				return true
			}
		}
	}
	return false
}

func (g *parityProgramGenerator) emitRandomVersionedRunVMProgramOp(version int) bool {
	for i := 0; i < 32; i++ {
		switch g.r.Intn(32) {
		case 0, 1:
			return g.emitPushValueOp()
		case 2, 3, 4:
			if g.emitStackOp() {
				return true
			}
		case 5, 6, 7:
			if g.emitMathOp() {
				return true
			}
		case 8, 9:
			if g.emitCellSliceOp() {
				return true
			}
		case 10, 11:
			if g.emitDictOp() {
				return true
			}
		case 12, 13:
			if g.emitControlOp() {
				return true
			}
		case 14, 15, 16, 17, 18, 19:
			if g.emitRunVMOp() {
				return true
			}
		case 20, 21, 22, 23, 24, 25:
			if g.emitRunVMVersionedChildOp(version) {
				return true
			}
		case 26, 27:
			if g.emitVersionedPureFuncOp() {
				return true
			}
		default:
			if g.emitTupleOp() {
				return true
			}
		}
	}
	return false
}

func (g *parityProgramGenerator) emitRandomVersionedRuntimeProgramOp() bool {
	for i := 0; i < 32; i++ {
		switch g.r.Intn(36) {
		case 0, 1:
			return g.emitPushValueOp()
		case 2, 3, 4:
			if g.emitStackOp() {
				return true
			}
		case 5, 6:
			if g.emitMathOp() {
				return true
			}
		case 7, 8:
			if g.emitCellSliceOp() {
				return true
			}
		case 9:
			if g.emitDictOp() {
				return true
			}
		case 10, 11, 12:
			if g.emitControlOp() {
				return true
			}
		case 13, 14, 15, 16, 17, 18, 19, 20, 21:
			if g.emitRuntimeControlOp() {
				return true
			}
		case 22, 23, 24:
			if g.emitVersionedRichC7ParamOp() {
				return true
			}
		case 25, 26:
			if g.emitVersionedC7ParamOp() {
				return true
			}
		case 27, 28:
			if g.emitVersionedPureFuncOp() {
				return true
			}
		default:
			if g.emitTupleOp() {
				return true
			}
		}
	}
	return false
}

func (g *parityProgramGenerator) emitRandomVersionedActionsProgramOp() bool {
	for i := 0; i < 32; i++ {
		switch g.r.Intn(24) {
		case 0, 1:
			return g.emitPushValueOp()
		case 2, 3:
			if g.emitStackOp() {
				return true
			}
		case 4:
			if g.emitMathOp() {
				return true
			}
		case 5:
			if g.emitCellSliceOp() {
				return true
			}
		case 6:
			if g.emitDictOp() {
				return true
			}
		case 7:
			if g.emitControlOp() {
				return true
			}
		case 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20:
			if g.emitActionRegisterOp(g.r.Intn(9)) {
				return true
			}
		case 21:
			if g.emitRuntimeControlOp() {
				return true
			}
		default:
			if g.emitTupleOp() {
				return true
			}
		}
	}
	return false
}

func (g *parityProgramGenerator) emitRandomVersionedC7ProgramOp() bool {
	for i := 0; i < 32; i++ {
		switch g.r.Intn(32) {
		case 0, 1:
			return g.emitPushValueOp()
		case 2, 3, 4:
			if g.emitStackOp() {
				return true
			}
		case 5, 6, 7:
			if g.emitMathOp() {
				return true
			}
		case 8, 9:
			if g.emitCellSliceOp() {
				return true
			}
		case 10, 11:
			if g.emitDictOp() {
				return true
			}
		case 12, 13:
			if g.emitControlOp() {
				return true
			}
		case 14, 15, 16, 17, 18, 19, 20, 21:
			if g.emitVersionedC7ParamOp() {
				return true
			}
		case 22, 23, 24:
			if g.emitVersionedPureFuncOp() {
				return true
			}
		default:
			if g.emitTupleOp() {
				return true
			}
		}
	}
	return false
}

func (g *parityProgramGenerator) emitRandomVersionedRichC7ProgramOp() bool {
	for i := 0; i < 32; i++ {
		switch g.r.Intn(36) {
		case 0, 1:
			return g.emitPushValueOp()
		case 2, 3, 4:
			if g.emitStackOp() {
				return true
			}
		case 5, 6, 7:
			if g.emitMathOp() {
				return true
			}
		case 8, 9:
			if g.emitCellSliceOp() {
				return true
			}
		case 10, 11:
			if g.emitDictOp() {
				return true
			}
		case 12, 13:
			if g.emitControlOp() {
				return true
			}
		case 14, 15, 16, 17, 18, 19, 20, 21:
			if g.emitVersionedRichC7ParamOp() {
				return true
			}
		case 22, 23, 24, 25:
			if g.emitVersionedC7ParamOp() {
				return true
			}
		case 26, 27, 28:
			if g.emitVersionedPureFuncOp() {
				return true
			}
		default:
			if g.emitTupleOp() {
				return true
			}
		}
	}
	return false
}

func (g *parityProgramGenerator) emitRandomVersionedSupercontractProgramOp(version int) bool {
	for i := 0; i < 32; i++ {
		switch g.r.Intn(64) {
		case 0, 1, 2, 3:
			return g.emitPushValueOp()
		case 4, 5, 6:
			if g.emitStackOp() {
				return true
			}
		case 7, 8, 9, 10:
			if g.emitMathOp() {
				return true
			}
		case 11, 12, 13, 14:
			if g.emitCellSliceOp() {
				return true
			}
		case 15, 16, 17:
			if g.emitDictOp() {
				return true
			}
		case 18, 19:
			if g.emitControlOp() {
				return true
			}
		case 20, 21, 22:
			if g.emitVersionedPureFuncOp() {
				return true
			}
		case 23, 24:
			if g.emitRuntimeControlOp() {
				return true
			}
		case 25, 26:
			if g.emitVersionedC7ParamOp() {
				return true
			}
		case 27, 28, 29:
			if g.emitVersionedRichC7ParamOp() {
				return true
			}
		case 30, 31:
			if g.emitActionRegisterOp(g.r.Intn(9)) {
				return true
			}
		case 32, 33:
			if g.emitRunVMOp() {
				return true
			}
		case 34, 35:
			if g.emitRunVMVersionedChildOp(version) {
				return true
			}
		case 36, 37:
			if g.emitLibraryResolutionOp(g.r.Intn(6)) {
				return true
			}
		case 38, 39, 40:
			if g.emitMessageAddressOp() {
				return true
			}
		case 41, 42, 43:
			if g.emitFuncHashVarIntOp(g.r.Intn(15)) {
				return true
			}
		case 44, 45:
			if g.emitPrngOp(g.r.Intn(7)) {
				return true
			}
		case 46, 47, 48:
			if g.emitDataSizeProgramOp(g.r.Intn(2) == 0, g.r.Intn(2) == 0) {
				return true
			}
		default:
			if g.emitTupleOp() {
				return true
			}
		}
	}
	return false
}

func (g *parityProgramGenerator) emitVersionedPureFuncOp() bool {
	switch g.r.Intn(8) {
	case 0, 1, 2:
		return g.emitMessageAddressOp()
	case 3:
		return g.emitFuncHashVarIntOp(g.r.Intn(15))
	case 4:
		return g.emitDataSizeProgramOp(false, false)
	case 5:
		return g.emitDataSizeProgramOp(false, true)
	case 6:
		return g.emitDataSizeProgramOp(true, false)
	default:
		return g.emitDataSizeProgramOp(true, true)
	}
}

func (g *parityProgramGenerator) emitVersionedC7ParamOp() bool {
	switch g.r.Intn(24) {
	case 0:
		g.emit("NOW", funcsop.NOW().Serialize())
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(tonopsTestTime.Unix())))
	case 1:
		g.emit("GETPARAM(3)", funcsop.GETPARAM(3).Serialize())
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(tonopsTestTime.Unix())))
	case 2:
		g.emit("RANDSEED", funcsop.RANDSEED().Serialize())
		g.stack = append(g.stack, parityProgramIntValue(new(big.Int).SetBytes(g.seed)))
	case 3:
		g.emit("GETPARAMLONG(6)", funcsop.GETPARAMLONG(6).Serialize())
		g.stack = append(g.stack, parityProgramIntValue(new(big.Int).SetBytes(g.seed)))
	case 4:
		g.emit("BALANCE", funcsop.BALANCE().Serialize())
		g.stack = append(g.stack, parityProgramStackValue{
			kind: parityProgramTuple,
			tuple: []parityProgramStackValue{
				parityProgramIntValue(tonopsTestBalance),
				{kind: parityProgramNull},
			},
		})
	case 5:
		g.emit("MYADDR", funcsop.MYADDR().Serialize())
		g.stack = append(g.stack, parityProgramStackValue{
			kind:  parityProgramSlice,
			slice: cell.BeginCell().MustStoreAddr(tonopsTestAddr).ToSlice(),
		})
	case 6:
		g.emit("CONFIGROOT", funcsop.CONFIGROOT().Serialize())
		g.stack = append(g.stack, parityProgramMaybeCellValue(g.c7ConfigRoot))
	case 7:
		g.emit("CONFIGDICT", funcsop.CONFIGDICT().Serialize())
		g.stack = append(g.stack, parityProgramMaybeCellValue(g.c7ConfigRoot), parityProgramIntValue(big.NewInt(32)))
	case 8:
		base := len(g.stack)
		g.emitPushInt("configparam_hit_idx", big.NewInt(7))
		g.emit("CONFIGPARAM(hit)", funcsop.CONFIGPARAM().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramCell, cell: versionedC7ProgramConfigValue()}, parityProgramBoolValue(true))
	case 9:
		base := len(g.stack)
		g.emitPushInt("configparam_miss_idx", big.NewInt(12345))
		g.emit("CONFIGPARAM(miss)", funcsop.CONFIGPARAM().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(false))
	case 10:
		base := len(g.stack)
		g.emitPushInt("configoptparam_hit_idx", big.NewInt(7))
		g.emit("CONFIGOPTPARAM(hit)", funcsop.CONFIGOPTPARAM().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramCell, cell: versionedC7ProgramConfigValue()})
	case 11:
		base := len(g.stack)
		g.emitPushInt("configoptparam_miss_idx", big.NewInt(12345))
		g.emit("CONFIGOPTPARAM(miss)", funcsop.CONFIGOPTPARAM().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramNull})
	case 12:
		g.emit("PREVBLOCKSINFOTUPLE", funcsop.PREVBLOCKSINFOTUPLE().Serialize())
		g.stack = append(g.stack, versionedC7ProgramPrevBlocksValue())
	case 13:
		g.emit("PREVMCBLOCKS", funcsop.PREVMCBLOCKS().Serialize())
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(111)))
	case 14:
		g.emit("PREVKEYBLOCK", funcsop.PREVKEYBLOCK().Serialize())
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(222)))
	case 15:
		g.emit("PREVMCBLOCKS_100", funcsop.PREVMCBLOCKS_100().Serialize())
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(333)))
	case 16, 17, 18, 19, 20, 21, 22:
		return g.emitPrngOp(g.r.Intn(7))
	default:
		return g.emitVersionedPureFuncOp()
	}
	return true
}

func (g *parityProgramGenerator) emitVersionedRichC7ParamOp() bool {
	switch g.r.Intn(28) {
	case 0, 1, 2, 3, 4, 5, 6, 7, 8, 9:
		return g.emitTonFuncContextOp(g.r.Intn(19))
	case 10:
		g.emit("UNPACKEDCONFIGTUPLE", funcsop.UNPACKEDCONFIGTUPLE().Serialize())
		g.stack = append(g.stack, parityProgramStackValue{
			kind: parityProgramTuple,
			tuple: []parityProgramStackValue{
				parityProgramStackValue{kind: parityProgramSlice, slice: makeStoragePricesSlice(100, 3, 5, 7, 11)},
				parityProgramStackValue{kind: parityProgramSlice, slice: cell.BeginCell().MustStoreUInt(uint64(uint32(tonopsTestGlobalID)), 32).ToSlice()},
				parityProgramStackValue{kind: parityProgramSlice, slice: makeGasPricesSlice(100, 77, 200, 1000, 1200, 50, 2000, 3000, 4000, true)},
				parityProgramStackValue{kind: parityProgramSlice, slice: makeGasPricesSlice(100, 55, 150, 900, 900, 40, 1800, 2800, 3800, true)},
				parityProgramStackValue{kind: parityProgramSlice, slice: makeMsgPricesSlice(1000, 200, 300, 500, 1000, 2000)},
				parityProgramStackValue{kind: parityProgramSlice, slice: makeMsgPricesSlice(900, 120, 220, 400, 800, 1200)},
				parityProgramStackValue{kind: parityProgramSlice, slice: makeSizeLimitsSlice(1<<20, 128)},
			},
		})
	case 11:
		g.emit("INMSGPARAMS", funcsop.INMSGPARAMS().Serialize())
		g.stack = append(g.stack, parityProgramInMsgParamsValue())
	case 12:
		g.emit("INMSGPARAM(0)", funcsop.INMSGPARAM(0).Serialize())
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(1)))
	case 13:
		g.emit("INMSGPARAM(2)", funcsop.INMSGPARAM(2).Serialize())
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: cell.BeginCell().MustStoreAddr(address.MustParseAddr("EQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAM9c")).ToSlice()})
	case 14:
		g.emit("INMSGPARAM(7)", funcsop.INMSGPARAM(7).Serialize())
		g.stack = append(g.stack, parityProgramStackValue{
			kind: parityProgramTuple,
			tuple: []parityProgramStackValue{
				parityProgramIntValue(big.NewInt(55)),
				{kind: parityProgramNull},
			},
		})
	case 15:
		g.emit("INMSGPARAM(8)", funcsop.INMSGPARAM(8).Serialize())
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramNull})
	case 16:
		g.emit("INMSGPARAM(9)", funcsop.INMSGPARAM(9).Serialize())
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramNull})
	default:
		return g.emitTonFuncContextOp(10 + g.r.Intn(3))
	}
	return true
}

func (g *parityProgramGenerator) emitCellSliceOp() bool {
	switch g.r.Intn(25) {
	case 0:
		g.emit("NEWC", cellsliceop.NEWC().Serialize())
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: cell.BeginCell()})
	case 1:
		if len(g.stack) == 0 || g.stack[len(g.stack)-1].kind != parityProgramBuilder {
			return false
		}
		g.emit("ENDC", cellsliceop.ENDC().Serialize())
		g.stack[len(g.stack)-1] = parityProgramStackValue{kind: parityProgramCell, cell: g.stack[len(g.stack)-1].builder.Copy().EndCell()}
	case 2:
		if len(g.stack) == 0 || g.stack[len(g.stack)-1].kind != parityProgramCell {
			return false
		}
		g.emit("CTOS", cellsliceop.CTOS().Serialize())
		g.stack[len(g.stack)-1] = parityProgramStackValue{kind: parityProgramSlice, slice: g.stack[len(g.stack)-1].cell.MustBeginParse()}
	case 3:
		if len(g.stack) == 0 || g.stack[len(g.stack)-1].kind != parityProgramSlice {
			return false
		}
		sl := g.stack[len(g.stack)-1].slice
		if sl.BitsLeft() != 0 || sl.RefsNum() != 0 {
			return false
		}
		g.emit("ENDS", cellsliceop.ENDS().Serialize())
		g.stack = g.stack[:len(g.stack)-1]
	case 4:
		if len(g.stack) == 0 || g.stack[len(g.stack)-1].kind != parityProgramCell {
			return false
		}
		g.emit("HASHCU", cellsliceop.HASHCU().Serialize())
		hash := new(big.Int).SetBytes(g.stack[len(g.stack)-1].cell.Hash())
		g.stack[len(g.stack)-1] = parityProgramIntValue(hash)
	case 5:
		if len(g.stack) == 0 || g.stack[len(g.stack)-1].kind != parityProgramSlice {
			return false
		}
		g.emit("HASHSU", cellsliceop.HASHSU().Serialize())
		hash := new(big.Int).SetBytes(g.stack[len(g.stack)-1].slice.ToBuilder().EndCell().Hash())
		g.stack[len(g.stack)-1] = parityProgramIntValue(hash)
	case 6:
		return g.emitLoadRefOp()
	case 7:
		return g.emitLoadIntOp(false)
	case 8:
		return g.emitLoadIntOp(true)
	case 9:
		return g.emitStoreIntOp(false)
	case 10:
		return g.emitStoreIntOp(true)
	case 11:
		return g.emitStoreRefOp()
	case 12:
		return g.emitStoreSliceOp()
	case 13:
		return g.emitBuilderMetaOp("BBITS", cellsliceop.BBITS().Serialize(), func(b *cell.Builder) int64 {
			return int64(b.BitsUsed())
		})
	case 14:
		return g.emitBuilderMetaOp("BREFS", cellsliceop.BREFS().Serialize(), func(b *cell.Builder) int64 {
			return int64(b.RefsUsed())
		})
	case 15:
		return g.emitBuilderMetaOp("BDEPTH", cellsliceop.BDEPTH().Serialize(), func(b *cell.Builder) int64 {
			return int64(b.Depth())
		})
	case 16:
		return g.emitLibraryResolutionOp(g.r.Intn(6))
	default:
		return g.emitCellSliceMetaOp()
	}
	return true
}

func (g *parityProgramGenerator) emitLibraryResolutionOp(mode int) bool {
	if g.libTarget == nil || g.libRef == nil || g.missingLibRef == nil {
		return false
	}

	base := len(g.stack)
	switch mode {
	case 0:
		g.emitPushCell("library_ref_ctos", g.libRef)
		g.emit("CTOS(library)", cellsliceop.CTOS().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: g.libTarget.MustBeginParse()})
	case 1:
		g.emitPushCell("library_ref_xload", g.libRef)
		g.emit("XLOAD(library)", cellsliceop.XLOAD().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramCell, cell: g.libTarget})
	case 2:
		g.emitPushCell("library_ref_xloadq", g.libRef)
		g.emit("XLOADQ(library)", cellsliceop.XLOADQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramCell, cell: g.libTarget}, parityProgramBoolValue(true))
	case 3:
		g.emitPushCell("library_ref_xctos", g.libRef)
		g.emit("XCTOS(library)", cellsliceop.XCTOS().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: g.libTarget.MustBeginParse()}, parityProgramBoolValue(false))
	case 4:
		g.emitPushCell("library_ref_missing_xloadq", g.missingLibRef)
		g.emit("XLOADQ(missing)", cellsliceop.XLOADQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(false))
	default:
		g.emitPushCell("xloadq_ordinary", g.libTarget)
		g.emit("XLOADQ(ordinary)", cellsliceop.XLOADQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramCell, cell: g.libTarget}, parityProgramBoolValue(true))
	}
	return true
}

func (g *parityProgramGenerator) emitCellSliceMetaOp() bool {
	base := len(g.stack)
	root := parityProgramStorageStatCell()

	switch g.r.Intn(36) {
	case 0:
		sl := root.MustBeginParse()
		g.emitPushSlice("sbits_src", sl)
		g.emit("SBITS", cellsliceop.SBITS().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(sl.BitsLeft()))))
	case 1:
		sl := root.MustBeginParse()
		g.emitPushSlice("srefs_src", sl)
		g.emit("SREFS", cellsliceop.SREFS().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(sl.RefsNum()))))
	case 2:
		sl := root.MustBeginParse()
		g.emitPushSlice("sbitrefs_src", sl)
		g.emit("SBITREFS", cellsliceop.SBITREFS().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(sl.BitsLeft()))), parityProgramIntValue(big.NewInt(int64(sl.RefsNum()))))
	case 3:
		sl := root.MustBeginParse()
		g.emitPushSlice("sdepth_src", sl)
		g.emit("SDEPTH", cellsliceop.SDEPTH().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(sl.Depth()))))
	case 4:
		g.emitPushCell("cdepth_src", root)
		g.emit("CDEPTH", cellsliceop.CDEPTH().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(root.Depth()))))
	case 5:
		g.emitPushNull("cdepth_nil")
		g.emit("CDEPTH(nil)", cellsliceop.CDEPTH().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(0)))
	case 6:
		g.emitPushCell("clevel_src", root)
		g.emit("CLEVEL", cellsliceop.CLEVEL().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(root.Level()))))
	case 7:
		g.emitPushCell("clevelmask_src", root)
		g.emit("CLEVELMASK", cellsliceop.CLEVELMASK().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(root.LevelMask().Mask))))
	case 8:
		g.emitPushCell("chashi_src", root)
		g.emit("CHASHI(0)", cellsliceop.CHASHI(0).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(new(big.Int).SetBytes(root.Hash(0))))
	case 9:
		g.emitPushCell("cdepthi_src", root)
		g.emit("CDEPTHI(0)", cellsliceop.CDEPTHI(0).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(root.Depth(0)))))
	case 10:
		g.emitPushCell("chashix_src", root)
		g.emitPushInt("chashix_idx", big.NewInt(0))
		g.emit("CHASHIX", cellsliceop.CHASHIX().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(new(big.Int).SetBytes(root.Hash(0))))
	case 11:
		g.emitPushCell("cdepthix_src", root)
		g.emitPushInt("cdepthix_idx", big.NewInt(0))
		g.emit("CDEPTHIX", cellsliceop.CDEPTHIX().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(root.Depth(0)))))
	case 12:
		builder := parityProgramMetaBuilder()
		g.emitPushBuilder("bbitrefs_src", builder)
		g.emit("BBITREFS", cellsliceop.BBITREFS().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(builder.BitsUsed()))), parityProgramIntValue(big.NewInt(int64(builder.RefsUsed()))))
	case 13:
		builder := parityProgramMetaBuilder()
		g.emitPushBuilder("brembits_src", builder)
		g.emit("BREMBITS", cellsliceop.BREMBITS().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(builder.BitsLeft()))))
	case 14:
		builder := parityProgramMetaBuilder()
		g.emitPushBuilder("bremrefs_src", builder)
		g.emit("BREMREFS", cellsliceop.BREMREFS().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(builder.RefsLeft()))))
	case 15:
		builder := parityProgramMetaBuilder()
		g.emitPushBuilder("brembitrefs_src", builder)
		g.emit("BREMBITREFS", cellsliceop.BREMBITREFS().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(builder.BitsLeft()))), parityProgramIntValue(big.NewInt(int64(builder.RefsLeft()))))
	case 16:
		builder := parityProgramMetaBuilder()
		g.emitPushBuilder("bchkbitsimm_src", builder)
		g.emit("BCHKBITSIMM(8)", cellsliceop.BCHKBITSIMM(8, false).Serialize())
		g.stack = g.stack[:base]
	case 17:
		builder := parityProgramMetaBuilder()
		g.emitPushBuilder("bchkbitrefsq_src", builder)
		g.emitPushInt("bchkbitrefsq_bits", big.NewInt(8))
		g.emitPushInt("bchkbitrefsq_refs", big.NewInt(1))
		g.emit("BCHKBITREFSQ", cellsliceop.BCHKBITREFSQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-1)))
	default:
		switch g.r.Intn(6) {
		case 0:
			return g.emitCellSlicePredicateOp(g.r.Intn(18))
		case 1:
			return g.emitCellSliceTransformOp(g.r.Intn(32))
		case 2:
			return g.emitCellSliceLoadFamilyOp(g.r.Intn(30))
		case 3:
			return g.emitCellSliceLittleEndianOp(g.r.Intn(20))
		default:
			if g.r.Intn(3) == 0 {
				return g.emitCellSlicePrefixGramsOp(g.r.Intn(8))
			}
			return g.emitCellSliceBuilderAdvancedOp(g.r.Intn(37))
		}
	}
	return true
}

func (g *parityProgramGenerator) emitCellSlicePredicateOp(mode int) bool {
	base := len(g.stack)
	empty := cell.BeginCell().ToSlice()
	refOnly := cell.BeginCell().MustStoreRef(cell.BeginCell().EndCell()).ToSlice()
	full := parityProgramBitsSlice(0b10110, 5)
	fullAlt := parityProgramBitsSlice(0b10111, 5)
	prefix := parityProgramBitsSlice(0b101, 3)
	suffix := parityProgramBitsSlice(0b110, 3)

	switch mode {
	case 0:
		g.emitPushSlice("sempty_src", empty)
		g.emit("SEMPTY", cellsliceop.SEMPTY().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(true))
	case 1:
		g.emitPushSlice("sdempty_src", refOnly)
		g.emit("SDEMPTY", cellsliceop.SDEMPTY().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(true))
	case 2:
		g.emitPushSlice("srempty_src", full)
		g.emit("SREMPTY", cellsliceop.SREMPTY().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(true))
	case 3:
		g.emitPushSlice("sdfirst_src", full)
		g.emit("SDFIRST", cellsliceop.SDFIRST().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(true))
	case 4:
		src := parityProgramBitsSlice(0b00101, 5)
		g.emitPushSlice("sdcntlead0_src", src)
		g.emit("SDCNTLEAD0", cellsliceop.SDCNTLEAD0().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(src.CountLeading(false)))))
	case 5:
		src := parityProgramBitsSlice(0b11101, 5)
		g.emitPushSlice("sdcntlead1_src", src)
		g.emit("SDCNTLEAD1", cellsliceop.SDCNTLEAD1().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(src.CountLeading(true)))))
	case 6:
		src := parityProgramBitsSlice(0b10100, 5)
		g.emitPushSlice("sdcnttrail0_src", src)
		g.emit("SDCNTTRAIL0", cellsliceop.SDCNTTRAIL0().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(src.CountTrailing(false)))))
	case 7:
		src := parityProgramBitsSlice(0b10111, 5)
		g.emitPushSlice("sdcnttrail1_src", src)
		g.emit("SDCNTTRAIL1", cellsliceop.SDCNTTRAIL1().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(src.CountTrailing(true)))))
	case 8:
		g.emitPushSlice("sdeq_a", full)
		g.emitPushSlice("sdeq_b", parityProgramBitsSlice(0b10110, 5))
		g.emit("SDEQ", cellsliceop.SDEQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(true))
	case 9:
		g.emitPushSlice("sdlexcmp_a", full)
		g.emitPushSlice("sdlexcmp_b", fullAlt)
		g.emit("SDLEXCMP", cellsliceop.SDLEXCMP().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(full.LexCompare(fullAlt)))))
	case 10:
		g.emitPushSlice("sdpfx_prefix", prefix)
		g.emitPushSlice("sdpfx_full", full)
		g.emit("SDPFX", cellsliceop.SDPFX().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(prefix.IsPrefixOf(full)))
	case 11:
		g.emitPushSlice("sdpfxrev_full", full)
		g.emitPushSlice("sdpfxrev_prefix", prefix)
		g.emit("SDPFXREV", cellsliceop.SDPFXREV().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(prefix.IsPrefixOf(full)))
	case 12:
		g.emitPushSlice("sdppfx_prefix", prefix)
		g.emitPushSlice("sdppfx_full", full)
		g.emit("SDPPFX", cellsliceop.SDPPFX().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(prefix.IsProperPrefixOf(full)))
	case 13:
		g.emitPushSlice("sdppfxrev_full", full)
		g.emitPushSlice("sdppfxrev_prefix", prefix)
		g.emit("SDPPFXREV", cellsliceop.SDPPFXREV().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(prefix.IsProperPrefixOf(full)))
	case 14:
		g.emitPushSlice("sdsfx_suffix", suffix)
		g.emitPushSlice("sdsfx_full", full)
		g.emit("SDSFX", cellsliceop.SDSFX().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(suffix.IsSuffixOf(full)))
	case 15:
		g.emitPushSlice("sdsfxrev_full", full)
		g.emitPushSlice("sdsfxrev_suffix", suffix)
		g.emit("SDSFXREV", cellsliceop.SDSFXREV().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(suffix.IsSuffixOf(full)))
	case 16:
		g.emitPushSlice("sdpsfx_suffix", suffix)
		g.emitPushSlice("sdpsfx_full", full)
		g.emit("SDPSFX", cellsliceop.SDPSFX().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(suffix.IsProperSuffixOf(full)))
	default:
		g.emitPushSlice("sdpsfxrev_full", full)
		g.emitPushSlice("sdpsfxrev_suffix", suffix)
		g.emit("SDPSFXREV", cellsliceop.SDPSFXREV().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(suffix.IsProperSuffixOf(full)))
	}
	return true
}

func (g *parityProgramGenerator) emitCellSliceTransformOp(mode int) bool {
	base := len(g.stack)
	bitsOnly := parityProgramBitsSlice(0b101101, 6)
	refSrc, ref0, ref1 := parityProgramRefSlice()
	mutate := func(src *cell.Slice, fn func(*cell.Slice) bool) *cell.Slice {
		cp := src.Copy()
		if !fn(cp) {
			panic("invalid parity program slice transform fixture")
		}
		return cp
	}

	switch mode {
	case 0:
		rest := refSrc.Copy()
		ref, err := rest.LoadRefCell()
		if err != nil {
			panic(err)
		}
		g.emitPushSlice("ldrefrotos_src", refSrc)
		g.emit("LDREFRTOS", cellsliceop.LDREFRTOS().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramStackValue{kind: parityProgramSlice, slice: rest},
			parityProgramStackValue{kind: parityProgramSlice, slice: ref.MustBeginParse()},
		)
	case 1:
		want := mutate(bitsOnly, func(sl *cell.Slice) bool { return sl.OnlyFirst(3, 0) })
		g.emitPushSlice("sdcutfirst_src", bitsOnly)
		g.emitPushInt("sdcutfirst_bits", big.NewInt(3))
		g.emit("SDCUTFIRST", cellsliceop.SDCUTFIRST().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: want})
	case 2:
		want := mutate(bitsOnly, func(sl *cell.Slice) bool { return sl.SkipFirst(2, 0) })
		g.emitPushSlice("sdskipfirst_src", bitsOnly)
		g.emitPushInt("sdskipfirst_bits", big.NewInt(2))
		g.emit("SDSKIPFIRST", cellsliceop.SDSKIPFIRST().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: want})
	case 3:
		want := mutate(bitsOnly, func(sl *cell.Slice) bool { return sl.OnlyLast(4, 0) })
		g.emitPushSlice("sdcutlast_src", bitsOnly)
		g.emitPushInt("sdcutlast_bits", big.NewInt(4))
		g.emit("SDCUTLAST", cellsliceop.SDCUTLAST().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: want})
	case 4:
		want := mutate(bitsOnly, func(sl *cell.Slice) bool { return sl.SkipLast(2, 0) })
		g.emitPushSlice("sdskiplast_src", bitsOnly)
		g.emitPushInt("sdskiplast_bits", big.NewInt(2))
		g.emit("SDSKIPLAST", cellsliceop.SDSKIPLAST().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: want})
	case 5:
		want := mutate(bitsOnly, func(sl *cell.Slice) bool {
			return sl.SkipFirst(1, 0) && sl.OnlyFirst(4, 0)
		})
		g.emitPushSlice("sdsubstr_src", bitsOnly)
		g.emitPushInt("sdsubstr_offset", big.NewInt(1))
		g.emitPushInt("sdsubstr_bits", big.NewInt(4))
		g.emit("SDSUBSTR", cellsliceop.SDSUBSTR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: want})
	case 6:
		want := mutate(refSrc, func(sl *cell.Slice) bool { return sl.OnlyFirst(4, 1) })
		g.emitPushSlice("scutfirst_src", refSrc)
		g.emitPushInt("scutfirst_bits", big.NewInt(4))
		g.emitPushInt("scutfirst_refs", big.NewInt(1))
		g.emit("SCUTFIRST", cellsliceop.SCUTFIRST().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: want})
	case 7:
		want := mutate(refSrc, func(sl *cell.Slice) bool { return sl.SkipFirst(2, 1) })
		g.emitPushSlice("sskipfirst_src", refSrc)
		g.emitPushInt("sskipfirst_bits", big.NewInt(2))
		g.emitPushInt("sskipfirst_refs", big.NewInt(1))
		g.emit("SSKIPFIRST", cellsliceop.SSKIPFIRST().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: want})
	case 8:
		want := mutate(refSrc, func(sl *cell.Slice) bool { return sl.OnlyLast(3, 1) })
		g.emitPushSlice("scutlast_src", refSrc)
		g.emitPushInt("scutlast_bits", big.NewInt(3))
		g.emitPushInt("scutlast_refs", big.NewInt(1))
		g.emit("SCUTLAST", cellsliceop.SCUTLAST().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: want})
	case 9:
		want := mutate(refSrc, func(sl *cell.Slice) bool { return sl.SkipLast(2, 1) })
		g.emitPushSlice("sskiplast_src", refSrc)
		g.emitPushInt("sskiplast_bits", big.NewInt(2))
		g.emitPushInt("sskiplast_refs", big.NewInt(1))
		g.emit("SSKIPLAST", cellsliceop.SSKIPLAST().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: want})
	case 10:
		want := mutate(refSrc, func(sl *cell.Slice) bool {
			return sl.SkipFirst(1, 1) && sl.OnlyFirst(4, 1)
		})
		g.emitPushSlice("subslice_src", refSrc)
		g.emitPushInt("subslice_l1", big.NewInt(1))
		g.emitPushInt("subslice_r1", big.NewInt(1))
		g.emitPushInt("subslice_l2", big.NewInt(4))
		g.emitPushInt("subslice_r2", big.NewInt(1))
		g.emit("SUBSLICE", cellsliceop.SUBSLICE().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: want})
	case 11, 12:
		first := mutate(refSrc, func(sl *cell.Slice) bool { return sl.OnlyFirst(3, 1) })
		rest := mutate(refSrc, func(sl *cell.Slice) bool { return sl.SkipFirst(3, 1) })
		quiet := mode == 12
		g.emitPushSlice("split_src", refSrc)
		g.emitPushInt("split_bits", big.NewInt(3))
		g.emitPushInt("split_refs", big.NewInt(1))
		if quiet {
			g.emit("SPLITQ", cellsliceop.SPLITQ().Serialize())
		} else {
			g.emit("SPLIT", cellsliceop.SPLIT().Serialize())
		}
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramStackValue{kind: parityProgramSlice, slice: first},
			parityProgramStackValue{kind: parityProgramSlice, slice: rest},
		)
		if quiet {
			g.stack = append(g.stack, parityProgramBoolValue(true))
		}
	case 13:
		g.emitPushSlice("splitq_fail_src", refSrc)
		g.emitPushInt("splitq_fail_bits", big.NewInt(7))
		g.emitPushInt("splitq_fail_refs", big.NewInt(0))
		g.emit("SPLITQ(fail)", cellsliceop.SPLITQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramStackValue{kind: parityProgramSlice, slice: refSrc.Copy()},
			parityProgramBoolValue(false),
		)
	case 14:
		g.emitPushSlice("schkbits_src", bitsOnly)
		g.emitPushInt("schkbits_bits", big.NewInt(6))
		g.emit("SCHKBITS", cellsliceop.SCHKBITS().Serialize())
		g.stack = g.stack[:base]
	case 15:
		g.emitPushSlice("schkrefs_src", refSrc)
		g.emitPushInt("schkrefs_refs", big.NewInt(2))
		g.emit("SCHKREFS", cellsliceop.SCHKREFS().Serialize())
		g.stack = g.stack[:base]
	case 16:
		g.emitPushSlice("schkbitrefs_src", refSrc)
		g.emitPushInt("schkbitrefs_bits", big.NewInt(6))
		g.emitPushInt("schkbitrefs_refs", big.NewInt(2))
		g.emit("SCHKBITREFS", cellsliceop.SCHKBITREFS().Serialize())
		g.stack = g.stack[:base]
	case 17:
		g.emitPushSlice("schkbitsq_src", bitsOnly)
		g.emitPushInt("schkbitsq_bits", big.NewInt(6))
		g.emit("SCHKBITSQ", cellsliceop.SCHKBITSQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(true))
	case 18:
		g.emitPushSlice("schkrefsq_src", refSrc)
		g.emitPushInt("schkrefsq_refs", big.NewInt(3))
		g.emit("SCHKREFSQ(fail)", cellsliceop.SCHKREFSQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(false))
	case 19:
		g.emitPushSlice("schkbitrefsq_src", refSrc)
		g.emitPushInt("schkbitrefsq_bits", big.NewInt(6))
		g.emitPushInt("schkbitrefsq_refs", big.NewInt(2))
		g.emit("SCHKBITREFSQ", cellsliceop.SCHKBITREFSQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(true))
	case 20:
		g.emitPushSlice("schkbitrefsq_fail_src", refSrc)
		g.emitPushInt("schkbitrefsq_fail_bits", big.NewInt(7))
		g.emitPushInt("schkbitrefsq_fail_refs", big.NewInt(2))
		g.emit("SCHKBITREFSQ(fail)", cellsliceop.SCHKBITREFSQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(false))
	case 21:
		g.emitPushSlice("pldrefvar_src", refSrc)
		g.emitPushInt("pldrefvar_idx", big.NewInt(1))
		g.emit("PLDREFVAR", cellsliceop.PLDREFVAR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramCell, cell: ref1})
	case 22:
		g.emitPushSlice("pldrefidx_src", refSrc)
		g.emit("PLDREFIDX(0)", cellsliceop.PLDREFIDX(0).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramCell, cell: ref0})
	case 23:
		src := parityProgramBitsSlice(0b001011, 6)
		want := mutate(src, func(sl *cell.Slice) bool { return sl.SkipFirst(uint(sl.CountLeading(false)), 0) })
		g.emitPushSlice("ldzeroes_src", src)
		g.emit("LDZEROES", cellsliceop.LDZEROES().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramIntValue(big.NewInt(2)),
			parityProgramStackValue{kind: parityProgramSlice, slice: want},
		)
	case 24:
		src := parityProgramBitsSlice(0b111010, 6)
		want := mutate(src, func(sl *cell.Slice) bool { return sl.SkipFirst(uint(sl.CountLeading(true)), 0) })
		g.emitPushSlice("ldones_src", src)
		g.emit("LDONES", cellsliceop.LDONES().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramIntValue(big.NewInt(3)),
			parityProgramStackValue{kind: parityProgramSlice, slice: want},
		)
	case 25:
		src := parityProgramBitsSlice(0b111010, 6)
		count := src.CountLeading(true)
		want := mutate(src, func(sl *cell.Slice) bool { return sl.SkipFirst(uint(count), 0) })
		g.emitPushSlice("ldsame_src", src)
		g.emitPushInt("ldsame_bit", big.NewInt(1))
		g.emit("LDSAME", cellsliceop.LDSAME().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramIntValue(big.NewInt(int64(count))),
			parityProgramStackValue{kind: parityProgramSlice, slice: want},
		)
	case 26:
		g.emitPushSlice("schkbitsq_fail_src", bitsOnly)
		g.emitPushInt("schkbitsq_fail_bits", big.NewInt(7))
		g.emit("SCHKBITSQ(fail)", cellsliceop.SCHKBITSQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(false))
	case 27:
		g.emitPushSlice("schkrefsq_ok_src", refSrc)
		g.emitPushInt("schkrefsq_ok_refs", big.NewInt(2))
		g.emit("SCHKREFSQ", cellsliceop.SCHKREFSQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(true))
	case 28:
		g.emitPushSlice("pldrefidx1_src", refSrc)
		g.emit("PLDREFIDX(1)", cellsliceop.PLDREFIDX(1).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramCell, cell: ref1})
	case 29:
		src := parityProgramBitsSlice(0b101011, 6)
		count := src.CountLeading(false)
		want := src.Copy()
		g.emitPushSlice("ldzeroes_zero_src", src)
		g.emit("LDZEROES(zero)", cellsliceop.LDZEROES().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramIntValue(big.NewInt(int64(count))),
			parityProgramStackValue{kind: parityProgramSlice, slice: want},
		)
	case 30:
		cl := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
		g.emitPushCell("xctos_ordinary", cl)
		g.emit("XCTOS(ordinary)", cellsliceop.XCTOS().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramStackValue{kind: parityProgramSlice, slice: cl.MustBeginParse()},
			parityProgramBoolValue(false),
		)
	default:
		src := parityProgramBitsSlice(0b010111, 6)
		count := src.CountLeading(true)
		want := src.Copy()
		g.emitPushSlice("ldones_zero_src", src)
		g.emit("LDONES(zero)", cellsliceop.LDONES().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramIntValue(big.NewInt(int64(count))),
			parityProgramStackValue{kind: parityProgramSlice, slice: want},
		)
	}
	return true
}

func (g *parityProgramGenerator) emitCellSliceLoadFamilyOp(mode int) bool {
	base := len(g.stack)
	intSrc := parityProgramBitsSlice(0xABCD, 16)
	shortSrc := parityProgramBitsSlice(0b1011, 4)
	sliceSrc := parityProgramBitsSlice(0b101101, 6)
	loadInt := func(src *cell.Slice, bits uint, unsigned, preload bool) (*big.Int, *cell.Slice) {
		cp := src.Copy()
		var (
			val *big.Int
			err error
		)
		if unsigned {
			if preload {
				val, err = cp.PreloadBigUInt(bits)
			} else {
				val, err = cp.LoadBigUInt(bits)
			}
		} else if preload {
			val, err = cp.PreloadBigInt(bits)
		} else {
			val, err = cp.LoadBigInt(bits)
		}
		if err != nil {
			panic(err)
		}
		return val, cp
	}
	loadSlice := func(src *cell.Slice, bits uint, preload bool) (*cell.Slice, *cell.Slice) {
		cp := src.Copy()
		var (
			part *cell.Slice
			err  error
		)
		if preload {
			part, err = cp.PreloadSubslice(bits, 0)
		} else {
			part, err = cp.FetchSubslice(bits, 0)
		}
		if err != nil {
			panic(err)
		}
		return part, cp
	}

	switch mode {
	case 0:
		val, rest := loadInt(intSrc, 8, false, false)
		g.emitPushSlice("ldix_src", intSrc)
		g.emitPushInt("ldix_bits", big.NewInt(8))
		g.emit("LDIX", cellsliceop.LDIX().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val), parityProgramStackValue{kind: parityProgramSlice, slice: rest})
	case 1:
		val, rest := loadInt(intSrc, 12, true, false)
		g.emitPushSlice("ldux_src", intSrc)
		g.emitPushInt("ldux_bits", big.NewInt(12))
		g.emit("LDUX", cellsliceop.LDUX().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val), parityProgramStackValue{kind: parityProgramSlice, slice: rest})
	case 2:
		val, _ := loadInt(intSrc, 8, false, true)
		g.emitPushSlice("pldix_src", intSrc)
		g.emitPushInt("pldix_bits", big.NewInt(8))
		g.emit("PLDIX", cellsliceop.PLDIX().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val))
	case 3:
		val, _ := loadInt(intSrc, 12, true, true)
		g.emitPushSlice("pldux_src", intSrc)
		g.emitPushInt("pldux_bits", big.NewInt(12))
		g.emit("PLDUX", cellsliceop.PLDUX().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val))
	case 4:
		val, rest := loadInt(intSrc, 8, false, false)
		g.emitPushSlice("ldixq_src", intSrc)
		g.emitPushInt("ldixq_bits", big.NewInt(8))
		g.emit("LDIXQ", cellsliceop.LDIXQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val), parityProgramStackValue{kind: parityProgramSlice, slice: rest}, parityProgramBoolValue(true))
	case 5:
		g.emitPushSlice("lduxq_fail_src", shortSrc)
		g.emitPushInt("lduxq_fail_bits", big.NewInt(8))
		g.emit("LDUXQ(fail)", cellsliceop.LDUXQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: shortSrc.Copy()}, parityProgramBoolValue(false))
	case 6:
		val, _ := loadInt(intSrc, 8, false, true)
		g.emitPushSlice("pldixq_src", intSrc)
		g.emitPushInt("pldixq_bits", big.NewInt(8))
		g.emit("PLDIXQ", cellsliceop.PLDIXQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val), parityProgramBoolValue(true))
	case 7:
		g.emitPushSlice("plduxq_fail_src", shortSrc)
		g.emitPushInt("plduxq_fail_bits", big.NewInt(8))
		g.emit("PLDUXQ(fail)", cellsliceop.PLDUXQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(false))
	case 8:
		val, rest := loadInt(intSrc, 9, false, false)
		g.emitPushSlice("ldifix_src", intSrc)
		g.emit("LDIFIX(9)", cellsliceop.LDIFIX(9, false, false, false).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val), parityProgramStackValue{kind: parityProgramSlice, slice: rest})
	case 9:
		val, rest := loadInt(intSrc, 9, true, false)
		g.emitPushSlice("ldufix_src", intSrc)
		g.emit("LDUFIX(9)", cellsliceop.LDUFIX(9, false, false, false).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val), parityProgramStackValue{kind: parityProgramSlice, slice: rest})
	case 10:
		val, _ := loadInt(intSrc, 9, false, true)
		g.emitPushSlice("pldifix_src", intSrc)
		g.emit("PLDIFIX(9)", cellsliceop.PLDIFIX(9, false, false, false).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val))
	case 11:
		val, _ := loadInt(intSrc, 9, true, true)
		g.emitPushSlice("pldufix_src", intSrc)
		g.emit("PLDUFIX(9)", cellsliceop.PLDUFIX(9, false, false, false).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val))
	case 12:
		val, rest := loadInt(intSrc, 9, true, false)
		g.emitPushSlice("ldufixq_src", intSrc)
		g.emit("LDUFIXQ(9)", cellsliceop.LDUFIX(9, true, false, false).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val), parityProgramStackValue{kind: parityProgramSlice, slice: rest}, parityProgramBoolValue(true))
	case 13:
		g.emitPushSlice("pldufixq_fail_src", shortSrc)
		g.emit("PLDUFIXQ(fail)", cellsliceop.PLDUFIX(9, true, false, false).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(false))
	case 14:
		loadBits := shortSrc.BitsLeft()
		val, _ := loadInt(shortSrc, loadBits, true, true)
		val = new(big.Int).Lsh(val, 32-loadBits)
		g.emitPushSlice("plduz_src", shortSrc)
		g.emit("PLDUZ(32)", cellsliceop.PLDUZ(32).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: shortSrc.Copy()}, parityProgramIntValue(val))
	case 15:
		part, rest := loadSlice(sliceSrc, 3, false)
		g.emitPushSlice("ldslicex_src", sliceSrc)
		g.emitPushInt("ldslicex_bits", big.NewInt(3))
		g.emit("LDSLICEX", cellsliceop.LDSLICEX().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: part}, parityProgramStackValue{kind: parityProgramSlice, slice: rest})
	case 16:
		part, _ := loadSlice(sliceSrc, 3, true)
		g.emitPushSlice("pldslicex_src", sliceSrc)
		g.emitPushInt("pldslicex_bits", big.NewInt(3))
		g.emit("PLDSLICEX", cellsliceop.PLDSLICEX().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: part})
	case 17:
		part, rest := loadSlice(sliceSrc, 3, false)
		g.emitPushSlice("ldslicexq_src", sliceSrc)
		g.emitPushInt("ldslicexq_bits", big.NewInt(3))
		g.emit("LDSLICEXQ", cellsliceop.LDSLICEXQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramStackValue{kind: parityProgramSlice, slice: part},
			parityProgramStackValue{kind: parityProgramSlice, slice: rest},
			parityProgramBoolValue(true),
		)
	case 18:
		g.emitPushSlice("ldslicexq_fail_src", shortSrc)
		g.emitPushInt("ldslicexq_fail_bits", big.NewInt(7))
		g.emit("LDSLICEXQ(fail)", cellsliceop.LDSLICEXQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: shortSrc.Copy()}, parityProgramBoolValue(false))
	case 19:
		part, _ := loadSlice(sliceSrc, 3, true)
		g.emitPushSlice("pldslicexq_src", sliceSrc)
		g.emitPushInt("pldslicexq_bits", big.NewInt(3))
		g.emit("PLDSLICEXQ", cellsliceop.PLDSLICEXQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: part}, parityProgramBoolValue(true))
	case 20:
		g.emitPushSlice("pldslicexq_fail_src", shortSrc)
		g.emitPushInt("pldslicexq_fail_bits", big.NewInt(7))
		g.emit("PLDSLICEXQ(fail)", cellsliceop.PLDSLICEXQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(false))
	case 21:
		part, rest := loadSlice(sliceSrc, 3, false)
		g.emitPushSlice("ldslice_src", sliceSrc)
		g.emit("LDSLICE(3)", cellsliceop.LDSLICE(3).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: part}, parityProgramStackValue{kind: parityProgramSlice, slice: rest})
	case 22:
		part, rest := loadSlice(sliceSrc, 3, false)
		g.emitPushSlice("ldslicefix_src", sliceSrc)
		g.emit("LDSLICEFIX(3)", cellsliceop.LDSLICEFIX(3, false, false).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: part}, parityProgramStackValue{kind: parityProgramSlice, slice: rest})
	case 23:
		part, _ := loadSlice(sliceSrc, 3, true)
		g.emitPushSlice("pldslicefix_src", sliceSrc)
		g.emit("PLDSLICEFIX(3)", cellsliceop.PLDSLICEFIX(3, false, false).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: part})
	case 24:
		part, rest := loadSlice(sliceSrc, 3, false)
		g.emitPushSlice("ldslicefixq_src", sliceSrc)
		g.emit("LDSLICEFIXQ(3)", cellsliceop.LDSLICEFIX(3, true, false).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramStackValue{kind: parityProgramSlice, slice: part},
			parityProgramStackValue{kind: parityProgramSlice, slice: rest},
			parityProgramBoolValue(true),
		)
	case 25:
		part, _ := loadSlice(sliceSrc, 3, true)
		g.emitPushSlice("pldslicefixq_src", sliceSrc)
		g.emit("PLDSLICEFIXQ(3)", cellsliceop.PLDSLICEFIX(3, true, false).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: part}, parityProgramBoolValue(true))
	case 26:
		g.emitPushSlice("pldslicefixq_fail_src", shortSrc)
		g.emit("PLDSLICEFIXQ(fail)", cellsliceop.PLDSLICEFIX(7, true, false).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(false))
	case 27:
		g.emitPushSlice("ldslicefixq_fail_src", shortSrc)
		g.emit("LDSLICEFIXQ(fail)", cellsliceop.LDSLICEFIX(7, true, false).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: shortSrc.Copy()}, parityProgramBoolValue(false))
	case 28:
		val, rest := loadInt(intSrc, 9, false, false)
		g.emitPushSlice("ldiq_src", intSrc)
		g.emit("LDIQ", cellsliceop.LDIFIX(9, true, false, false).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val), parityProgramStackValue{kind: parityProgramSlice, slice: rest}, parityProgramBoolValue(true))
	default:
		val, _ := loadInt(intSrc, 9, false, true)
		g.emitPushSlice("pldiq_src", intSrc)
		g.emit("PLDIQ", cellsliceop.PLDIFIX(9, true, false, false).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val), parityProgramBoolValue(true))
	}
	return true
}

func (g *parityProgramGenerator) emitCellSliceLittleEndianOp(mode int) bool {
	base := len(g.stack)
	src4 := parityProgramByteSlice([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xA5})
	src8 := parityProgramByteSlice([]byte{0x08, 0x07, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01, 0xA5})
	short := parityProgramByteSlice([]byte{0x01, 0x02, 0x03, 0x04})
	load := func(src *cell.Slice, bytesLen int, unsigned, preload bool) (*big.Int, *cell.Slice) {
		cp := src.Copy()
		var (
			data []byte
			err  error
		)
		if preload {
			data, err = cp.PreloadSlice(uint(bytesLen * 8))
		} else {
			data, err = cp.LoadSlice(uint(bytesLen * 8))
		}
		if err != nil {
			panic(err)
		}
		return parityProgramDecodeLEInt(data, unsigned), cp
	}
	store := func(v *big.Int, bytesLen int, unsigned bool) *cell.Builder {
		data := parityProgramEncodeLEInt(v, bytesLen, unsigned)
		builder := cell.BeginCell()
		if err := builder.StoreSlice(data, uint(bytesLen*8)); err != nil {
			panic(err)
		}
		return builder
	}

	switch mode {
	case 0:
		val, rest := load(src4, 4, false, false)
		g.emitPushSlice("ldile4_src", src4)
		g.emit("LDILE4", cellsliceop.LDILE4().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val), parityProgramStackValue{kind: parityProgramSlice, slice: rest})
	case 1:
		val, rest := load(src4, 4, true, false)
		g.emitPushSlice("ldule4_src", src4)
		g.emit("LDULE4", cellsliceop.LDULE4().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val), parityProgramStackValue{kind: parityProgramSlice, slice: rest})
	case 2:
		val, rest := load(src8, 8, false, false)
		g.emitPushSlice("ldile8_src", src8)
		g.emit("LDILE8", cellsliceop.LDILE8().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val), parityProgramStackValue{kind: parityProgramSlice, slice: rest})
	case 3:
		val, rest := load(src8, 8, true, false)
		g.emitPushSlice("ldule8_src", src8)
		g.emit("LDULE8", cellsliceop.LDULE8().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val), parityProgramStackValue{kind: parityProgramSlice, slice: rest})
	case 4:
		val, _ := load(src4, 4, false, true)
		g.emitPushSlice("pldile4_src", src4)
		g.emit("PLDILE4", cellsliceop.PLDILE4().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val))
	case 5:
		val, _ := load(src4, 4, true, true)
		g.emitPushSlice("pldule4_src", src4)
		g.emit("PLDULE4", cellsliceop.PLDULE4().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val))
	case 6:
		val, _ := load(src8, 8, false, true)
		g.emitPushSlice("pldile8_src", src8)
		g.emit("PLDILE8", cellsliceop.PLDILE8().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val))
	case 7:
		val, _ := load(src8, 8, true, true)
		g.emitPushSlice("pldule8_src", src8)
		g.emit("PLDULE8", cellsliceop.PLDULE8().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val))
	case 8:
		val, rest := load(src4, 4, false, false)
		g.emitPushSlice("ldile4q_src", src4)
		g.emit("LDILE4Q", cellsliceop.LDILE4Q().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val), parityProgramStackValue{kind: parityProgramSlice, slice: rest}, parityProgramBoolValue(true))
	case 9:
		g.emitPushSlice("ldule8q_fail_src", short)
		g.emit("LDULE8Q(fail)", cellsliceop.LDULE8Q().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: short.Copy()}, parityProgramBoolValue(false))
	case 10:
		val, _ := load(src4, 4, false, true)
		g.emitPushSlice("pldile4q_src", src4)
		g.emit("PLDILE4Q", cellsliceop.PLDILE4Q().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val), parityProgramBoolValue(true))
	case 11:
		g.emitPushSlice("pldule8q_fail_src", short)
		g.emit("PLDULE8Q(fail)", cellsliceop.PLDULE8Q().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(false))
	case 12:
		val, rest := load(src4, 4, true, false)
		g.emitPushSlice("ldule4q_src", src4)
		g.emit("LDULE4Q", cellsliceop.LDULE4Q().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val), parityProgramStackValue{kind: parityProgramSlice, slice: rest}, parityProgramBoolValue(true))
	case 13:
		val, rest := load(src8, 8, false, false)
		g.emitPushSlice("ldile8q_src", src8)
		g.emit("LDILE8Q", cellsliceop.LDILE8Q().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val), parityProgramStackValue{kind: parityProgramSlice, slice: rest}, parityProgramBoolValue(true))
	case 14:
		val, _ := load(src4, 4, true, true)
		g.emitPushSlice("pldule4q_src", src4)
		g.emit("PLDULE4Q", cellsliceop.PLDULE4Q().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val), parityProgramBoolValue(true))
	case 15:
		val, _ := load(src8, 8, false, true)
		g.emitPushSlice("pldile8q_src", src8)
		g.emit("PLDILE8Q", cellsliceop.PLDILE8Q().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val), parityProgramBoolValue(true))
	case 16:
		v := big.NewInt(-2)
		want := store(v, 4, false)
		g.emitPushInt("stile4_val", v)
		g.emitPushBuilder("stile4_builder", cell.BeginCell())
		g.emit("STILE4", cellsliceop.STILE4().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want})
	case 17:
		v := big.NewInt(0x01020304)
		want := store(v, 4, true)
		g.emitPushInt("stule4_val", v)
		g.emitPushBuilder("stule4_builder", cell.BeginCell())
		g.emit("STULE4", cellsliceop.STULE4().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want})
	case 18:
		v := big.NewInt(-2)
		want := store(v, 8, false)
		g.emitPushInt("stile8_val", v)
		g.emitPushBuilder("stile8_builder", cell.BeginCell())
		g.emit("STILE8", cellsliceop.STILE8().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want})
	default:
		v := new(big.Int).SetUint64(0x0102030405060708)
		want := store(v, 8, true)
		g.emitPushInt("stule8_val", v)
		g.emitPushBuilder("stule8_builder", cell.BeginCell())
		g.emit("STULE8", cellsliceop.STULE8().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want})
	}
	return true
}

func (g *parityProgramGenerator) emitCellSliceBuilderAdvancedOp(mode int) bool {
	base := len(g.stack)
	ref := cell.BeginCell().MustStoreUInt(0xC, 4).EndCell()
	srcBuilder := cell.BeginCell().MustStoreUInt(0xA, 4)
	dstBuilder := cell.BeginCell().MustStoreUInt(0x5, 3)
	srcSlice := parityProgramBitsSlice(0b1011, 4)
	fullRefs := parityProgramFullRefsBuilder()
	fullBits := parityProgramFullBitsBuilder()
	storeRef := func(dst *cell.Builder, cl *cell.Cell) *cell.Builder {
		cp := dst.Copy()
		if err := cp.StoreRefUncheckedDepth(cl); err != nil {
			panic(err)
		}
		return cp
	}
	storeBuilder := func(dst, src *cell.Builder) *cell.Builder {
		cp := dst.Copy()
		if err := cp.StoreBuilderUncheckedDepth(src.Copy()); err != nil {
			panic(err)
		}
		return cp
	}
	storeSlice := func(dst *cell.Builder, sl *cell.Slice) *cell.Builder {
		cp := dst.Copy()
		if err := cp.StoreBuilderUncheckedDepth(sl.ToBuilder()); err != nil {
			panic(err)
		}
		return cp
	}
	endBuilder := func(src *cell.Builder) *cell.Cell {
		cl, err := src.Copy().EndCellSpecial(false)
		if err != nil {
			panic(err)
		}
		return cl
	}

	switch mode {
	case 0:
		want := storeRef(dstBuilder, endBuilder(srcBuilder))
		g.emitPushBuilder("stbref_src", srcBuilder)
		g.emitPushBuilder("stbref_dst", dstBuilder)
		g.emit("STBREF", cellsliceop.STBREF().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want})
	case 1:
		want := storeRef(dstBuilder, endBuilder(srcBuilder))
		g.emitPushBuilder("stbrefr_dst", dstBuilder)
		g.emitPushBuilder("stbrefr_src", srcBuilder)
		g.emit("STBREFR", cellsliceop.STBREFR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want})
	case 2:
		want := storeRef(dstBuilder, ref)
		g.emitPushBuilder("strefr_dst", dstBuilder)
		g.emitPushCell("strefr_ref", ref)
		g.emit("STREFR", cellsliceop.STREFR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want})
	case 3:
		want := storeSlice(dstBuilder, srcSlice)
		g.emitPushBuilder("stslicer_dst", dstBuilder)
		g.emitPushSlice("stslicer_src", srcSlice)
		g.emit("STSLICER", cellsliceop.STSLICER().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want})
	case 4:
		want := storeBuilder(dstBuilder, srcBuilder)
		g.emitPushBuilder("stbr_dst", dstBuilder)
		g.emitPushBuilder("stbr_src", srcBuilder)
		g.emit("STBR", cellsliceop.STBR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want})
	case 5:
		want := storeRef(dstBuilder, ref)
		g.emitPushCell("strefq_ref", ref)
		g.emitPushBuilder("strefq_dst", dstBuilder)
		g.emit("STREFQ", cellsliceop.STREFQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want}, parityProgramIntValue(big.NewInt(0)))
	case 6:
		g.emitPushCell("strefq_fail_ref", ref)
		g.emitPushBuilder("strefq_fail_dst", fullRefs)
		g.emit("STREFQ(fail)", cellsliceop.STREFQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramStackValue{kind: parityProgramCell, cell: ref},
			parityProgramStackValue{kind: parityProgramBuilder, builder: fullRefs.Copy()},
			parityProgramIntValue(big.NewInt(-1)),
		)
	case 7:
		want := storeRef(dstBuilder, ref)
		g.emitPushBuilder("strefrq_dst", dstBuilder)
		g.emitPushCell("strefrq_ref", ref)
		g.emit("STREFRQ", cellsliceop.STREFRQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want}, parityProgramIntValue(big.NewInt(0)))
	case 8:
		g.emitPushBuilder("strefrq_fail_dst", fullRefs)
		g.emitPushCell("strefrq_fail_ref", ref)
		g.emit("STREFRQ(fail)", cellsliceop.STREFRQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramStackValue{kind: parityProgramBuilder, builder: fullRefs.Copy()},
			parityProgramStackValue{kind: parityProgramCell, cell: ref},
			parityProgramIntValue(big.NewInt(-1)),
		)
	case 9:
		want := storeRef(dstBuilder, endBuilder(srcBuilder))
		g.emitPushBuilder("stbrefq_src", srcBuilder)
		g.emitPushBuilder("stbrefq_dst", dstBuilder)
		g.emit("STBREFQ", cellsliceop.STBREFQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want}, parityProgramIntValue(big.NewInt(0)))
	case 10:
		g.emitPushBuilder("stbrefq_fail_src", srcBuilder)
		g.emitPushBuilder("stbrefq_fail_dst", fullRefs)
		g.emit("STBREFQ(fail)", cellsliceop.STBREFQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramStackValue{kind: parityProgramBuilder, builder: srcBuilder.Copy()},
			parityProgramStackValue{kind: parityProgramBuilder, builder: fullRefs.Copy()},
			parityProgramIntValue(big.NewInt(-1)),
		)
	case 11:
		want := storeRef(dstBuilder, endBuilder(srcBuilder))
		g.emitPushBuilder("stbrefrq_dst", dstBuilder)
		g.emitPushBuilder("stbrefrq_src", srcBuilder)
		g.emit("STBREFRQ", cellsliceop.STBREFRQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want}, parityProgramIntValue(big.NewInt(0)))
	case 12:
		g.emitPushBuilder("stbrefrq_fail_dst", fullRefs)
		g.emitPushBuilder("stbrefrq_fail_src", srcBuilder)
		g.emit("STBREFRQ(fail)", cellsliceop.STBREFRQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramStackValue{kind: parityProgramBuilder, builder: fullRefs.Copy()},
			parityProgramStackValue{kind: parityProgramBuilder, builder: srcBuilder.Copy()},
			parityProgramIntValue(big.NewInt(-1)),
		)
	case 13:
		want := storeSlice(dstBuilder, srcSlice)
		g.emitPushSlice("stsliceq_src", srcSlice)
		g.emitPushBuilder("stsliceq_dst", dstBuilder)
		g.emit("STSLICEQ", cellsliceop.STSLICEQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want}, parityProgramIntValue(big.NewInt(0)))
	case 14:
		g.emitPushSlice("stsliceq_fail_src", srcSlice)
		g.emitPushBuilder("stsliceq_fail_dst", fullBits)
		g.emit("STSLICEQ(fail)", cellsliceop.STSLICEQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramStackValue{kind: parityProgramSlice, slice: srcSlice.Copy()},
			parityProgramStackValue{kind: parityProgramBuilder, builder: fullBits.Copy()},
			parityProgramIntValue(big.NewInt(-1)),
		)
	case 15:
		want := storeSlice(dstBuilder, srcSlice)
		g.emitPushBuilder("stslicerq_dst", dstBuilder)
		g.emitPushSlice("stslicerq_src", srcSlice)
		g.emit("STSLICERQ", cellsliceop.STSLICERQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want}, parityProgramIntValue(big.NewInt(0)))
	case 16:
		g.emitPushBuilder("stslicerq_fail_dst", fullBits)
		g.emitPushSlice("stslicerq_fail_src", srcSlice)
		g.emit("STSLICERQ(fail)", cellsliceop.STSLICERQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramStackValue{kind: parityProgramBuilder, builder: fullBits.Copy()},
			parityProgramStackValue{kind: parityProgramSlice, slice: srcSlice.Copy()},
			parityProgramIntValue(big.NewInt(-1)),
		)
	case 17:
		want := storeBuilder(dstBuilder, srcBuilder)
		g.emitPushBuilder("stbq_src", srcBuilder)
		g.emitPushBuilder("stbq_dst", dstBuilder)
		g.emit("STBQ", cellsliceop.STBQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want}, parityProgramIntValue(big.NewInt(0)))
	case 18:
		g.emitPushBuilder("stbq_fail_src", srcBuilder)
		g.emitPushBuilder("stbq_fail_dst", fullBits)
		g.emit("STBQ(fail)", cellsliceop.STBQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramStackValue{kind: parityProgramBuilder, builder: srcBuilder.Copy()},
			parityProgramStackValue{kind: parityProgramBuilder, builder: fullBits.Copy()},
			parityProgramIntValue(big.NewInt(-1)),
		)
	case 19:
		want := storeBuilder(dstBuilder, srcBuilder)
		g.emitPushBuilder("stbrq_dst", dstBuilder)
		g.emitPushBuilder("stbrq_src", srcBuilder)
		g.emit("STBRQ", cellsliceop.STBRQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want}, parityProgramIntValue(big.NewInt(0)))
	case 20:
		g.emitPushBuilder("stbrq_fail_dst", fullBits)
		g.emitPushBuilder("stbrq_fail_src", srcBuilder)
		g.emit("STBRQ(fail)", cellsliceop.STBRQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramStackValue{kind: parityProgramBuilder, builder: fullBits.Copy()},
			parityProgramStackValue{kind: parityProgramBuilder, builder: srcBuilder.Copy()},
			parityProgramIntValue(big.NewInt(-1)),
		)
	case 21:
		want := storeRef(dstBuilder, ref)
		g.emitPushBuilder("strefconst_dst", dstBuilder)
		g.emit("STREFCONST", cellsliceop.STREFCONST(ref).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want})
	case 22:
		ref2 := cell.BeginCell().MustStoreUInt(0xD, 4).EndCell()
		want := storeRef(storeRef(dstBuilder, ref), ref2)
		g.emitPushBuilder("stref2const_dst", dstBuilder)
		g.emit("STREF2CONST", cellsliceop.STREF2CONST(ref, ref2).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want})
	case 23:
		want := storeSlice(dstBuilder, srcSlice)
		g.emitPushBuilder("stsliceconst_dst", dstBuilder)
		g.emit("STSLICECONST", cellsliceop.STSLICECONST(srcSlice).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want})
	case 24:
		want := storeRef(dstBuilder, endBuilder(srcBuilder))
		g.emitPushBuilder("endcst_dst", dstBuilder)
		g.emitPushBuilder("endcst_src", srcBuilder)
		g.emit("ENDCST", cellsliceop.ENDCST().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want})
	case 25:
		cl, err := srcBuilder.Copy().EndCellSpecial(false)
		if err != nil {
			panic(err)
		}
		g.emitPushBuilder("endxc_builder", srcBuilder)
		g.emitPushInt("endxc_special", big.NewInt(0))
		g.emit("ENDXC", cellsliceop.ENDXC().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramCell, cell: cl})
	case 26:
		cl := endBuilder(srcBuilder)
		g.emitPushBuilder("btos_builder", srcBuilder)
		g.emit("BTOS", cellsliceop.BTOS().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: cl.MustBeginParse()})
	case 27:
		g.emitPushBuilder("bchkbits_src", dstBuilder)
		g.emitPushInt("bchkbits_bits", big.NewInt(8))
		g.emit("BCHKBITS", cellsliceop.BCHKBITS().Serialize())
		g.stack = g.stack[:base]
	case 28:
		g.emitPushBuilder("bchkrefs_src", dstBuilder)
		g.emitPushInt("bchkrefs_refs", big.NewInt(1))
		g.emit("BCHKREFS", cellsliceop.BCHKREFS().Serialize())
		g.stack = g.stack[:base]
	case 29:
		g.emitPushBuilder("bchkrefsq_fail_src", fullRefs)
		g.emitPushInt("bchkrefsq_fail_refs", big.NewInt(1))
		g.emit("BCHKREFSQ(fail)", cellsliceop.BCHKREFSQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(false))
	case 30:
		want := dstBuilder.Copy()
		if err := want.StoreSameBit(false, 3); err != nil {
			panic(err)
		}
		g.emitPushBuilder("stzeroes_builder", dstBuilder)
		g.emitPushInt("stzeroes_bits", big.NewInt(3))
		g.emit("STZEROES", cellsliceop.STZEROES().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want})
	case 31:
		want := dstBuilder.Copy()
		if err := want.StoreSameBit(true, 4); err != nil {
			panic(err)
		}
		g.emitPushBuilder("stones_builder", dstBuilder)
		g.emitPushInt("stones_bits", big.NewInt(4))
		g.emit("STONES", cellsliceop.STONES().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want})
	case 32:
		want := dstBuilder.Copy()
		if err := want.StoreSameBit(true, 5); err != nil {
			panic(err)
		}
		g.emitPushBuilder("stsame_builder", dstBuilder)
		g.emitPushInt("stsame_bits", big.NewInt(5))
		g.emitPushInt("stsame_bit", big.NewInt(1))
		g.emit("STSAME", cellsliceop.STSAME().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want})
	case 33:
		g.emitPushBuilder("bchkbitrefs_src", dstBuilder)
		g.emitPushInt("bchkbitrefs_bits", big.NewInt(8))
		g.emitPushInt("bchkbitrefs_refs", big.NewInt(1))
		g.emit("BCHKBITREFS", cellsliceop.BCHKBITREFS().Serialize())
		g.stack = g.stack[:base]
	case 34:
		want := storeBuilder(dstBuilder, srcBuilder)
		g.emitPushBuilder("stb_src", srcBuilder)
		g.emitPushBuilder("stb_dst", dstBuilder)
		g.emit("STB", cellsliceop.STB().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want})
	case 35:
		g.emitPushBuilder("bchkbitrefsq_src", dstBuilder)
		g.emitPushInt("bchkbitrefsq_bits", big.NewInt(8))
		g.emitPushInt("bchkbitrefsq_refs", big.NewInt(1))
		g.emit("BCHKBITREFSQ", cellsliceop.BCHKBITREFSQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(true))
	default:
		value := big.NewInt(-2)
		bits := uint(4)
		want := cell.BeginCell()
		if err := want.StoreBigInt(value, bits); err != nil {
			panic(err)
		}
		g.emitPushInt("stix_value", value)
		g.emitPushBuilder("stix_builder", cell.BeginCell())
		g.emitPushInt("stix_bits", big.NewInt(int64(bits)))
		g.emit("STIX", parityProgramRawOp(0xCF00, 16))
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want})
	}
	return true
}

func (g *parityProgramGenerator) emitCellSlicePrefixGramsOp(mode int) bool {
	base := len(g.stack)
	full := parityProgramBitsSlice(0b101101, 6)
	prefix := parityProgramBitsSlice(0b101, 3)
	wrongPrefix := parityProgramBitsSlice(0b111, 3)
	restAfterPrefix := func(src *cell.Slice, bits uint) *cell.Slice {
		cp := src.Copy()
		if err := cp.SkipBits(bits); err != nil {
			panic(err)
		}
		return cp
	}

	switch mode {
	case 0:
		g.emitPushSlice("sdbeginsx_full", full)
		g.emitPushSlice("sdbeginsx_prefix", prefix)
		g.emit("SDBEGINSX", cellsliceop.SDBEGINSX().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: restAfterPrefix(full, prefix.BitsLeft())})
	case 1:
		g.emitPushSlice("sdbeginsxq_full", full)
		g.emitPushSlice("sdbeginsxq_prefix", prefix)
		g.emit("SDBEGINSXQ", cellsliceop.SDBEGINSXQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: restAfterPrefix(full, prefix.BitsLeft())}, parityProgramBoolValue(true))
	case 2:
		g.emitPushSlice("sdbeginsxq_fail_full", full)
		g.emitPushSlice("sdbeginsxq_fail_prefix", wrongPrefix)
		g.emit("SDBEGINSXQ(fail)", cellsliceop.SDBEGINSXQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: full.Copy()}, parityProgramBoolValue(false))
	case 3:
		g.emitPushSlice("sdbeginsconst_full", full)
		g.emit("SDBEGINSCONST", cellsliceop.SDBEGINSCONST(prefix, false).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: restAfterPrefix(full, prefix.BitsLeft())})
	case 4:
		g.emitPushSlice("sdbeginsconstq_full", full)
		g.emit("SDBEGINSCONSTQ", cellsliceop.SDBEGINSCONST(prefix, true).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: restAfterPrefix(full, prefix.BitsLeft())}, parityProgramBoolValue(true))
	case 5:
		g.emitPushSlice("sdbeginsconstq_fail_full", full)
		g.emit("SDBEGINSCONSTQ(fail)", cellsliceop.SDBEGINSCONST(wrongPrefix, true).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: full.Copy()}, parityProgramBoolValue(false))
	case 6:
		amount := big.NewInt(123456789)
		src := cell.BeginCell().MustStoreBigCoins(amount).MustStoreUInt(0xA, 4).ToSlice()
		rest := src.Copy()
		coins, err := rest.LoadBigCoins()
		if err != nil {
			panic(err)
		}
		g.emitPushSlice("ldgrams_src", src)
		g.emit("LDGRAMS", cellsliceop.LDGRAMS().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(coins), parityProgramStackValue{kind: parityProgramSlice, slice: rest})
	default:
		amount := big.NewInt(987654321)
		want := cell.BeginCell().MustStoreUInt(0xA, 4)
		if err := want.StoreBigCoins(amount); err != nil {
			panic(err)
		}
		g.emitPushBuilder("stgrams_builder", cell.BeginCell().MustStoreUInt(0xA, 4))
		g.emitPushInt("stgrams_amount", amount)
		g.emit("STGRAMS", cellsliceop.STGRAMS().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want})
	}
	return true
}

func (g *parityProgramGenerator) emitDictOp() bool {
	switch g.r.Intn(57) {
	case 0:
		return g.emitLoadDictOp(false)
	case 1:
		return g.emitLoadDictOp(true)
	case 2:
		return g.emitDictGetOp(false, false, false)
	case 3:
		return g.emitDictGetOp(true, false, false)
	case 4:
		return g.emitDictGetOp(false, true, false)
	case 5:
		return g.emitDictGetOp(false, true, true)
	case 6:
		return g.emitDictSetOp(false, 0)
	case 7:
		return g.emitDictSetOp(false, 2)
	case 8:
		return g.emitDictDeleteOp(0)
	case 9:
		return g.emitDictMinMaxOp(false, false, 0)
	case 10:
		return g.emitDictMinMaxOp(true, false, 0)
	case 11:
		return g.emitPrefixDictGetQOp()
	case 12:
		return g.emitDictGetOptRefOp(0)
	case 13:
		return g.emitDictGetOptRefOp(2)
	case 14:
		return g.emitDictSetGetOp(false, 0)
	case 15:
		return g.emitDictSetGetOp(true, 0)
	case 16:
		return g.emitDictReplaceAddOp(false, false, 0)
	case 17:
		return g.emitDictReplaceAddOp(true, false, 0)
	case 18:
		return g.emitDictRemMinMaxOp(false, false, 0)
	case 19:
		return g.emitDictRemMinMaxOp(true, false, 0)
	case 20:
		return g.emitDictNearOp()
	case 21:
		return g.emitStoreDictOp()
	case 22:
		return g.emitSkipDictOp()
	case 23:
		return g.emitLoadDictSliceOp(false, false)
	case 24:
		return g.emitLoadDictSliceOp(true, false)
	case 25:
		return g.emitLoadDictQuietOp(false)
	case 26:
		return g.emitLoadDictQuietOp(true)
	case 27:
		return g.emitPrefixDictGetOp(false)
	case 28:
		return g.emitPrefixDictReplaceAddOp(false)
	case 29:
		return g.emitPrefixDictReplaceAddOp(true)
	case 30:
		return g.emitDictBuilderSetOp(cell.DictSetModeSet, 0)
	case 31:
		return g.emitDictBuilderSetOp(cell.DictSetModeReplace, 2)
	case 32:
		return g.emitDictBuilderSetOp(cell.DictSetModeAdd, 1)
	case 33:
		return g.emitDictBuilderSetGetOp(cell.DictSetModeSet, 0)
	case 34:
		return g.emitDictBuilderSetGetOp(cell.DictSetModeReplace, 2)
	case 35:
		return g.emitDictBuilderSetGetOp(cell.DictSetModeAdd, 1)
	case 36:
		return g.emitDictDeleteGetOp(false, 0)
	case 37:
		return g.emitDictDeleteGetOp(true, 2)
	case 38:
		return g.emitDictSetGetOptRefOp(false, 2)
	case 39:
		return g.emitDictSetGetOptRefOp(true, 2)
	case 40, 41, 42, 43:
		return g.emitDictNearExtraOp(g.r.Intn(12))
	case 44:
		return g.emitSubdictOp(false, 0)
	case 45:
		return g.emitSubdictOp(true, 0)
	case 46:
		return g.emitSubdictOp(false, 2)
	case 47:
		return g.emitPrefixDictSetDelOp(g.r.Intn(2) == 0)
	case 48:
		return g.emitSubdictOp(true, 2)
	default:
		return g.emitDictGapOp(g.r.Intn(63))
	}
}

func (g *parityProgramGenerator) emitDictGapOp(mode int) bool {
	switch mode {
	case 0:
		return g.emitDictGetOp(true, true, false)
	case 1:
		return g.emitDictGetOp(true, true, true)
	case 2:
		return g.emitDictSetOp(false, 1)
	case 3:
		return g.emitDictSetOp(true, 0)
	case 4:
		return g.emitDictSetOp(true, 1)
	case 5:
		return g.emitDictSetOp(true, 2)
	case 6:
		return g.emitDictSetGetOp(false, 1)
	case 7:
		return g.emitDictSetGetOp(false, 2)
	case 8:
		return g.emitDictSetGetOp(true, 1)
	case 9:
		return g.emitDictSetGetOp(true, 2)
	case 10:
		return g.emitDictReplaceAddOp(false, false, 1)
	case 11:
		return g.emitDictReplaceAddOp(false, false, 2)
	case 12:
		return g.emitDictReplaceAddOp(false, true, 0)
	case 13:
		return g.emitDictReplaceAddOp(false, true, 1)
	case 14:
		return g.emitDictReplaceAddOp(false, true, 2)
	case 15:
		return g.emitDictReplaceAddOp(true, false, 1)
	case 16:
		return g.emitDictReplaceAddOp(true, false, 2)
	case 17:
		return g.emitDictReplaceAddOp(true, true, 0)
	case 18:
		return g.emitDictReplaceAddOp(true, true, 1)
	case 19:
		return g.emitDictReplaceAddOp(true, true, 2)
	case 20:
		return g.emitDictDeleteOp(1)
	case 21:
		return g.emitDictGetOptRefOp(1)
	case 22:
		return g.emitDictMinMaxOp(false, true, 0)
	case 23:
		return g.emitDictMinMaxOp(true, true, 0)
	case 24:
		return g.emitDictMinMaxOp(false, false, 1)
	case 25:
		return g.emitDictMinMaxOp(true, false, 2)
	case 26:
		return g.emitDictMinMaxOp(false, true, 1)
	case 27:
		return g.emitDictMinMaxOp(true, true, 2)
	case 28:
		return g.emitDictRemMinMaxOp(false, true, 0)
	case 29:
		return g.emitDictRemMinMaxOp(true, true, 0)
	case 30:
		return g.emitDictRemMinMaxOp(false, false, 1)
	case 31:
		return g.emitDictRemMinMaxOp(true, true, 2)
	case 32:
		return g.emitDictBuilderSetOp(cell.DictSetModeAdd, 0)
	case 33:
		return g.emitDictBuilderSetGetOp(cell.DictSetModeAdd, 0)
	case 34:
		return g.emitDictBuilderSetOp(cell.DictSetModeReplace, 0)
	case 35:
		return g.emitDictBuilderSetOp(cell.DictSetModeReplace, 1)
	case 36:
		return g.emitDictBuilderSetOp(cell.DictSetModeSet, 1)
	case 37:
		return g.emitDictBuilderSetGetOp(cell.DictSetModeReplace, 0)
	case 38:
		return g.emitDictBuilderSetGetOp(cell.DictSetModeReplace, 1)
	case 39:
		return g.emitDictBuilderSetGetOp(cell.DictSetModeSet, 1)
	case 40:
		return g.emitDictBuilderSetGetOp(cell.DictSetModeSet, 2)
	case 41:
		return g.emitDictReplaceAddGetOp(true, true, 0)
	case 42:
		return g.emitDictReplaceAddGetOp(true, false, 1)
	case 43:
		return g.emitDictReplaceAddGetOp(true, true, 1)
	case 44:
		return g.emitDictReplaceAddGetOp(true, false, 2)
	case 45:
		return g.emitDictReplaceAddGetOp(false, false, 0)
	case 46:
		return g.emitDictReplaceAddGetOp(false, true, 0)
	case 47:
		return g.emitDictReplaceAddGetOp(false, false, 1)
	case 48:
		return g.emitDictReplaceAddGetOp(false, true, 1)
	case 49:
		return g.emitDictReplaceAddGetOp(false, true, 2)
	case 50:
		return g.emitDictDeleteGetOp(true, 0)
	case 51:
		return g.emitDictDeleteGetOp(false, 1)
	case 52:
		return g.emitDictDeleteGetOp(true, 1)
	case 53:
		return g.emitDictSetGetOptRefOp(false, 0)
	case 54:
		return g.emitDictSetGetOptRefOp(false, 1)
	case 55:
		return g.emitDictMinMaxOp(true, false, 1)
	case 56:
		return g.emitDictMinMaxOp(true, true, 1)
	case 57:
		return g.emitDictRemMinMaxOp(true, false, 1)
	case 58:
		return g.emitDictRemMinMaxOp(true, false, 2)
	case 59:
		return g.emitDictRemMinMaxOp(false, true, 1)
	case 60:
		return g.emitDictRemMinMaxOp(true, true, 1)
	case 61:
		return g.emitSubdictOp(false, 1)
	case 62:
		return g.emitSubdictOp(true, 1)
	default:
		return false
	}
}

func (g *parityProgramGenerator) emitControlOp() bool {
	switch g.r.Intn(13) {
	case 0:
		return g.emitCondSelectOp(false)
	case 1:
		return g.emitCondSelectOp(true)
	case 2:
		g.emit("SETCP(0)", funcsop.SETCP(0).Serialize())
	case 3:
		base := len(g.stack)
		g.emitPushInt("setcpx_cp", big.NewInt(0))
		g.emit("SETCPX", funcsop.SETCPX().Serialize())
		g.stack = g.stack[:base]
	case 4:
		base := len(g.stack)
		g.emitPushInt("throwif_false", big.NewInt(0))
		g.emit("THROWIF(skip)", parityProgramRawOp(0xF240|42, 16))
		g.stack = g.stack[:base]
	case 5:
		base := len(g.stack)
		g.emitPushInt("throwifnot_true", big.NewInt(-1))
		g.emit("THROWIFNOT(skip)", parityProgramRawOp(0xF280|42, 16))
		g.stack = g.stack[:base]
	case 6, 7:
		return g.emitExecBranchLoopOp(g.r.Intn(11))
	case 8:
		g.emit("INVERT", execop.INVERT().Serialize())
	default:
		return g.emitContinuationControlOp(g.r.Intn(12))
	}
	return true
}

func (g *parityProgramGenerator) emitExecBranchLoopOp(mode int) bool {
	base := len(g.stack)
	intBody := func(v int64) *cell.Cell {
		return parityProgramCodeCell(stackop.PUSHINT(big.NewInt(v)).Serialize())
	}
	boolBody := func(v int64) *cell.Cell {
		return parityProgramCodeCell(
			stackop.PUSHINT(big.NewInt(v)).Serialize(),
			stackop.PUSHINT(big.NewInt(-1)).Serialize(),
		)
	}
	whileCondBody := parityProgramCodeCell(
		stackop.DUP().Serialize(),
		stackop.PUSHINT(big.NewInt(0)).Serialize(),
		mathop.GREATER().Serialize(),
	)
	whileBody := parityProgramCodeCell(mathop.DEC().Serialize())
	throwHandlerBody := parityProgramCodeCell(
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.PUSHINT(big.NewInt(128)).Serialize(),
	)
	pushResult := func(v int64) {
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(v)))
	}
	clearStack := func() {
		g.stack = g.stack[:base]
	}

	switch mode {
	case 0:
		g.emitPushInt("if_true_cond", big.NewInt(-1))
		g.emit("PUSHCONT(if_true_120)", stackop.PUSHCONT(intBody(120)).Serialize())
		g.emit("IF(true)", execop.IF().Serialize())
		pushResult(120)
	case 1:
		g.emitPushInt("if_false_cond", big.NewInt(0))
		g.emit("PUSHCONT(if_false_120)", stackop.PUSHCONT(intBody(120)).Serialize())
		g.emit("IF(false)", execop.IF().Serialize())
		clearStack()
	case 2:
		g.emitPushInt("ifnot_true_cond", big.NewInt(0))
		g.emit("PUSHCONT(ifnot_true_121)", stackop.PUSHCONT(intBody(121)).Serialize())
		g.emit("IFNOT(true)", execop.IFNOT().Serialize())
		pushResult(121)
	case 3:
		g.emitPushInt("ifnot_false_cond", big.NewInt(-1))
		g.emit("PUSHCONT(ifnot_false_121)", stackop.PUSHCONT(intBody(121)).Serialize())
		g.emit("IFNOT(false)", execop.IFNOT().Serialize())
		clearStack()
	case 4:
		g.emitPushInt("ifelse_true_cond", big.NewInt(-1))
		g.emit("PUSHCONT(ifelse_true_122)", stackop.PUSHCONT(intBody(122)).Serialize())
		g.emit("PUSHCONT(ifelse_false_123)", stackop.PUSHCONT(intBody(123)).Serialize())
		g.emit("IFELSE(true)", execop.IFELSE().Serialize())
		pushResult(122)
	case 5:
		g.emitPushInt("ifelse_false_cond", big.NewInt(0))
		g.emit("PUSHCONT(ifelse_true_122)", stackop.PUSHCONT(intBody(122)).Serialize())
		g.emit("PUSHCONT(ifelse_false_123)", stackop.PUSHCONT(intBody(123)).Serialize())
		g.emit("IFELSE(false)", execop.IFELSE().Serialize())
		pushResult(123)
	case 6:
		g.emitPushInt("repeat_one_count", big.NewInt(1))
		g.emit("PUSHCONT(repeat_one_124)", stackop.PUSHCONT(intBody(124)).Serialize())
		g.emit("REPEAT(one)", execop.REPEAT().Serialize())
		pushResult(124)
	case 7:
		g.emit("PUSHCONT(until_one_125)", stackop.PUSHCONT(boolBody(125)).Serialize())
		g.emit("UNTIL(one)", execop.UNTIL().Serialize())
		pushResult(125)
	case 8:
		g.emitPushInt("while_one_counter", big.NewInt(1))
		g.emit("PUSHCONT(while_one_cond)", stackop.PUSHCONT(whileCondBody).Serialize())
		g.emit("PUSHCONT(while_one_body)", stackop.PUSHCONT(whileBody).Serialize())
		g.emit("WHILE(one)", execop.WHILE().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(0)))
	case 9:
		g.emit("PUSHCONT(try_throw)", stackop.PUSHCONT(parityProgramCodeCell(parityProgramRawOp(0xF22A, 16))).Serialize())
		g.emit("PUSHCONT(try_handler)", stackop.PUSHCONT(throwHandlerBody).Serialize())
		g.emit("TRY(caught)", execop.TRY().Serialize())
		pushResult(128)
	default:
		g.emitPushInt("tryargs_param", big.NewInt(41))
		g.emit("PUSHCONT(tryargs_inc)", stackop.PUSHCONT(parityProgramCodeCell(
			mathop.INC().Serialize(),
			execop.RETARGS(1).Serialize(),
		)).Serialize())
		g.emit("PUSHCONT(tryargs_handler)", stackop.PUSHCONT(throwHandlerBody).Serialize())
		g.emit("TRYARGS(1,1)", execop.TRYARGS(1, 1).Serialize())
		pushResult(42)
	}
	return true
}

func (g *parityProgramGenerator) emitContinuationControlOp(mode int) bool {
	base := len(g.stack)
	intBody := func(v int64) *cell.Cell {
		return parityProgramCodeCell(stackop.PUSHINT(big.NewInt(v)).Serialize())
	}
	pushResult := func(v int64) {
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(v)))
	}
	clearStack := func() {
		g.stack = g.stack[:base]
	}

	switch mode {
	case 0:
		g.emit("PUSHCONT(push70)", stackop.PUSHCONT(intBody(70)).Serialize())
		g.emit("EXECUTE(push70)", execop.EXECUTE().Serialize())
		pushResult(70)
	case 1:
		g.emit("CALLREF(push71)", execop.CALLREF(intBody(71)).Serialize())
		pushResult(71)
	case 2:
		g.emitPushInt("ifref_true_cond", big.NewInt(-1))
		g.emit("IFREF(push72,true)", execop.IFREF(intBody(72)).Serialize())
		pushResult(72)
	case 3:
		g.emitPushInt("ifref_false_cond", big.NewInt(0))
		g.emit("IFREF(push72,false)", execop.IFREF(intBody(72)).Serialize())
		clearStack()
	case 4:
		g.emitPushInt("ifnotref_true_cond", big.NewInt(0))
		g.emit("IFNOTREF(push73,true)", execop.IFNOTREF(intBody(73)).Serialize())
		pushResult(73)
	case 5:
		g.emitPushInt("ifnotref_false_cond", big.NewInt(-1))
		g.emit("IFNOTREF(push73,false)", execop.IFNOTREF(intBody(73)).Serialize())
		clearStack()
	case 6:
		g.emitPushInt("ifrefelseref_true_cond", big.NewInt(-1))
		g.emit("IFREFELSEREF(true74,false75)", execop.IFREFELSEREF(intBody(74), intBody(75)).Serialize())
		pushResult(74)
	case 7:
		g.emitPushInt("ifrefelseref_false_cond", big.NewInt(0))
		g.emit("IFREFELSEREF(true74,false75)", execop.IFREFELSEREF(intBody(74), intBody(75)).Serialize())
		pushResult(75)
	case 8:
		g.emitPushInt("ifrefelse_true_cond", big.NewInt(-1))
		g.emit("PUSHCONT(ifrefelse_false77)", stackop.PUSHCONT(intBody(77)).Serialize())
		g.emit("IFREFELSE(true76,false77)", execop.IFREFELSE(intBody(76)).Serialize())
		pushResult(76)
	case 9:
		g.emitPushInt("ifelseref_false_cond", big.NewInt(0))
		g.emit("PUSHCONT(ifelseref_true78)", stackop.PUSHCONT(intBody(78)).Serialize())
		g.emit("IFELSEREF(true78,false79)", execop.IFELSEREF(intBody(79)).Serialize())
		pushResult(79)
	case 10:
		body := intBody(80)
		g.emitPushSlice("bless_code", body.MustBeginParse())
		g.emit("BLESS(push80)", execop.BLESS().Serialize())
		g.emit("EXECUTE(blessed_push80)", execop.EXECUTE().Serialize())
		pushResult(80)
	default:
		body := parityProgramCodeCell()
		g.emitPushInt("blessargs_copied", big.NewInt(81))
		g.emitPushSlice("blessargs_code", body.MustBeginParse())
		g.emit("BLESSARGS(1,0)", execop.BLESSARGS(1, 0).Serialize())
		g.emit("EXECUTE(blessargs)", execop.EXECUTE().Serialize())
		pushResult(81)
	}
	return true
}

func (g *parityProgramGenerator) emitRunVMOp() bool {
	base := len(g.stack)
	pushChild := func(name string, builders ...*cell.Builder) {
		g.emitPushInt(name+"_stack_size", big.NewInt(0))
		g.emitPushSlice(name+"_code", parityProgramCodeCell(builders...).MustBeginParse())
	}

	switch g.r.Intn(7) {
	case 0:
		pushChild("runvm_basic", stackop.PUSHINT(big.NewInt(90)).Serialize())
		g.emit("RUNVM(0)", execop.RUNVM(0).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(90)), parityProgramIntValue(big.NewInt(0)))
	case 1:
		pushChild("runvmx_basic", stackop.PUSHINT(big.NewInt(91)).Serialize())
		g.emitPushInt("runvmx_mode", big.NewInt(0))
		g.emit("RUNVMX(0)", execop.RUNVMX().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(91)), parityProgramIntValue(big.NewInt(0)))
	case 2:
		pushChild("runvm_same_c3_push_zero", stackop.DROP().Serialize())
		g.emit("RUNVM(3)", execop.RUNVM(3).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(0)))
	case 3:
		pushChild(
			"runvm_return_one",
			stackop.PUSHINT(big.NewInt(92)).Serialize(),
			stackop.PUSHINT(big.NewInt(93)).Serialize(),
		)
		g.emitPushInt("runvm_return_one_count", big.NewInt(1))
		g.emit("RUNVM(256)", execop.RUNVM(256).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(93)), parityProgramIntValue(big.NewInt(0)))
	case 4:
		dataCell := cell.BeginCell().MustStoreUInt(0xCA, 8).EndCell()
		actionsCell := cell.BeginCell().MustStoreUInt(0xCB, 8).EndCell()
		pushChild(
			"runvm_data_actions",
			stackop.PUSHREF(dataCell).Serialize(),
			execop.POPCTR(4).Serialize(),
			stackop.PUSHREF(actionsCell).Serialize(),
			execop.POPCTR(5).Serialize(),
			funcsop.COMMIT().Serialize(),
		)
		g.emitPushCell("runvm_initial_data", cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell())
		g.emit("RUNVM(36)", execop.RUNVM(4|32).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramIntValue(big.NewInt(0)),
			parityProgramStackValue{kind: parityProgramCell, cell: dataCell},
			parityProgramStackValue{kind: parityProgramCell, cell: actionsCell},
		)
	case 5:
		childC7 := parityProgramStackValue{
			kind: parityProgramTuple,
			tuple: []parityProgramStackValue{
				{
					kind: parityProgramTuple,
					tuple: []parityProgramStackValue{
						parityProgramIntValue(big.NewInt(11)),
						parityProgramIntValue(big.NewInt(22)),
					},
				},
			},
		}
		pushChild("runvm_c7_return_one", funcsop.GETPARAM(1).Serialize())
		g.emitPushInt("runvm_c7_return_one_count", big.NewInt(1))
		g.emitPushTuple("runvm_c7_return_one_c7", childC7)
		g.emit("RUNVM(272)", execop.RUNVM(16|256).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(22)), parityProgramIntValue(big.NewInt(0)))
	default:
		pushChild("runvm_isolated", stackop.PUSHINT(big.NewInt(94)).Serialize())
		g.emit("RUNVM(128)", execop.RUNVM(128).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(94)), parityProgramIntValue(big.NewInt(0)))
	}
	return true
}

func (g *parityProgramGenerator) emitRunVMVersionedChildOp(version int) bool {
	base := len(g.stack)

	g.emitPushInt("runvm_child_inmsgparams_stack_size", big.NewInt(0))
	g.emitPushSlice("runvm_child_inmsgparams_code", parityProgramCodeCell(funcsop.INMSGPARAMS().Serialize()).MustBeginParse())
	g.emitPushRunVMInMsgParamsChildC7("runvm_child_inmsgparams_c7")
	g.emit("RUNVM(16/inmsgparams)", execop.RUNVM(16).Serialize())

	g.stack = g.stack[:base]
	if version >= 11 {
		g.stack = append(g.stack, parityProgramRunVMInMsgParamsValue(), parityProgramIntValue(big.NewInt(0)))
	} else {
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(vmerr.CodeInvalidOpcode)))
	}
	return true
}

func (g *parityProgramGenerator) emitPushRunVMInMsgParamsChildC7(name string) {
	base := len(g.stack)
	params := parityProgramRunVMInMsgParamsValue()
	inner := parityProgramRunVMInMsgParamsChildC7InnerValue()
	c7 := parityProgramRunVMInMsgParamsChildC7Value()

	g.emitPushNull(name + "_inner")
	g.emitPushTuple(name+"_params", params)
	g.emitPushInt(name+"_params_idx", big.NewInt(17))
	g.emit("SETINDEXVARQ", tupleop.SETINDEXVARQ().Serialize())
	g.stack = g.stack[:base]
	g.stack = append(g.stack, inner)

	g.emit(fmt.Sprintf("TUPLE(%s:1)", name), tupleop.TUPLE(1).Serialize())
	g.stack = g.stack[:base]
	g.stack = append(g.stack, c7)
}

func (g *parityProgramGenerator) emitFuncParamOp() bool {
	switch g.r.Intn(31) {
	case 0:
		g.emit("NOW", funcsop.NOW().Serialize())
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(crossTestTime.Unix())))
	case 1:
		g.emit("GETPARAM(3)", funcsop.GETPARAM(3).Serialize())
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(crossTestTime.Unix())))
	case 2:
		g.emit("BLOCKLT", funcsop.BLOCKLT().Serialize())
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(tonopsTestBlockLT)))
	case 3:
		g.emit("LTIME", funcsop.LTIME().Serialize())
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(tonopsTestLogicalTime)))
	case 4:
		g.emit("RANDSEED", funcsop.RANDSEED().Serialize())
		g.stack = append(g.stack, parityProgramIntValue(new(big.Int).SetBytes(g.seed)))
	case 5:
		g.emit("BALANCE", funcsop.BALANCE().Serialize())
		g.stack = append(g.stack, parityProgramStackValue{
			kind: parityProgramTuple,
			tuple: []parityProgramStackValue{
				parityProgramIntValue(crossTestBalance),
				{kind: parityProgramNull},
			},
		})
	case 6:
		g.emit("MYADDR", funcsop.MYADDR().Serialize())
		g.stack = append(g.stack, parityProgramStackValue{
			kind:  parityProgramSlice,
			slice: cell.BeginCell().MustStoreAddr(crossTestAddr).ToSlice(),
		})
	case 7:
		g.emit("CONFIGROOT", funcsop.CONFIGROOT().Serialize())
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramNull})
	case 8:
		g.emit("CONFIGDICT", funcsop.CONFIGDICT().Serialize())
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramNull}, parityProgramIntValue(big.NewInt(32)))
	case 9:
		g.emit("GETPARAMLONG(6)", funcsop.GETPARAMLONG(6).Serialize())
		g.stack = append(g.stack, parityProgramIntValue(new(big.Int).SetBytes(g.seed)))
	case 10:
		return g.emitPrngOp(0)
	case 11:
		return g.emitPrngOp(1)
	case 12:
		return g.emitPrngOp(2)
	case 13:
		return g.emitPrngOp(3)
	case 14:
		return g.emitPrngOp(4)
	case 15:
		return g.emitPrngOp(5)
	case 16:
		return g.emitPrngOp(6)
	case 17:
		return g.emitDataSizeProgramOp(false, false)
	case 18:
		return g.emitDataSizeProgramOp(false, true)
	case 19:
		return g.emitDataSizeProgramOp(true, false)
	case 20:
		return g.emitDataSizeProgramOp(true, true)
	case 21:
		return g.emitRuntimeControlOp()
	case 22:
		return g.emitGlobalVarOp(false)
	case 23:
		return g.emitGlobalVarOp(true)
	case 24:
		return g.emitControlRegisterOp(g.r.Intn(12))
	case 25, 26:
		return g.emitTonFuncContextOp(g.r.Intn(19))
	default:
		return g.emitFuncHashVarIntOp(g.r.Intn(15))
	}
	return true
}

func (g *parityProgramGenerator) emitTonFuncContextOp(mode int) bool {
	base := len(g.stack)
	prevBlocks := parityProgramStackValue{
		kind: parityProgramTuple,
		tuple: []parityProgramStackValue{
			parityProgramIntValue(big.NewInt(111)),
			parityProgramIntValue(big.NewInt(222)),
			parityProgramIntValue(big.NewInt(333)),
		},
	}
	incomingValue := parityProgramStackValue{
		kind: parityProgramTuple,
		tuple: []parityProgramStackValue{
			parityProgramIntValue(big.NewInt(7777)),
			{kind: parityProgramNull},
		},
	}
	inMsgSrc := cell.BeginCell().MustStoreAddr(address.MustParseAddr("EQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAM9c")).ToSlice()
	inMsgValue := parityProgramStackValue{
		kind: parityProgramTuple,
		tuple: []parityProgramStackValue{
			parityProgramIntValue(big.NewInt(55)),
			{kind: parityProgramNull},
		},
	}

	switch mode {
	case 0:
		g.emit("MYCODE", funcsop.MYCODE().Serialize())
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramCell, cell: cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()})
	case 1:
		g.emit("INCOMINGVALUE", funcsop.INCOMINGVALUE().Serialize())
		g.stack = append(g.stack, incomingValue)
	case 2:
		g.emit("STORAGEFEES", funcsop.STORAGEFEES().Serialize())
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(tonopsTestStorageFees)))
	case 3:
		g.emit("DUEPAYMENT", funcsop.DUEPAYMENT().Serialize())
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(444)))
	case 4:
		g.emit("GLOBALID", funcsop.GLOBALID().Serialize())
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(tonopsTestGlobalID))))
	case 5:
		g.emit("PREVBLOCKSINFOTUPLE", funcsop.PREVBLOCKSINFOTUPLE().Serialize())
		g.stack = append(g.stack, prevBlocks)
	case 6:
		g.emit("PREVMCBLOCKS", funcsop.PREVMCBLOCKS().Serialize())
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(111)))
	case 7:
		g.emit("PREVKEYBLOCK", funcsop.PREVKEYBLOCK().Serialize())
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(222)))
	case 8:
		g.emit("PREVMCBLOCKS_100", funcsop.PREVMCBLOCKS_100().Serialize())
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(333)))
	case 9:
		g.emit("GETPRECOMPILEDGAS", funcsop.GETPRECOMPILEDGAS().Serialize())
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(555)))
	case 10:
		g.emit("INMSG_BOUNCE", funcsop.INMSG_BOUNCE().Serialize())
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(1)))
	case 11:
		g.emit("INMSG_SRC", funcsop.INMSG_SRC().Serialize())
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: inMsgSrc})
	case 12:
		g.emit("INMSG_VALUE", funcsop.INMSG_VALUE().Serialize())
		g.stack = append(g.stack, inMsgValue)
	case 13:
		g.emitPushInt("getgasfee_gas", big.NewInt(250))
		g.emitPushInt("getgasfee_mc", big.NewInt(0))
		g.emit("GETGASFEE", funcsop.GETGASFEE().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(56)))
	case 14:
		g.emitPushInt("getstoragefee_cells", big.NewInt(2))
		g.emitPushInt("getstoragefee_bits", big.NewInt(3))
		g.emitPushInt("getstoragefee_delta", big.NewInt(10))
		g.emitPushInt("getstoragefee_mc", big.NewInt(0))
		g.emit("GETSTORAGEFEE", funcsop.GETSTORAGEFEE().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(1)))
	case 15:
		g.emitPushInt("getforwardfee_cells", big.NewInt(2))
		g.emitPushInt("getforwardfee_bits", big.NewInt(8))
		g.emitPushInt("getforwardfee_mc", big.NewInt(0))
		g.emit("GETFORWARDFEE", funcsop.GETFORWARDFEE().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(901)))
	case 16:
		g.emitPushInt("getoriginalfwdfee_fee", big.NewInt(3200))
		g.emitPushInt("getoriginalfwdfee_mc", big.NewInt(0))
		g.emit("GETORIGINALFWDFEE", funcsop.GETORIGINALFWDFEE().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(3239)))
	case 17:
		g.emitPushInt("getgasfeesimple_gas", big.NewInt(250))
		g.emitPushInt("getgasfeesimple_mc", big.NewInt(0))
		g.emit("GETGASFEESIMPLE", funcsop.GETGASFEESIMPLE().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(1)))
	default:
		g.emitPushInt("getforwardfeesimple_cells", big.NewInt(2))
		g.emitPushInt("getforwardfeesimple_bits", big.NewInt(8))
		g.emitPushInt("getforwardfeesimple_mc", big.NewInt(0))
		g.emit("GETFORWARDFEESIMPLE", funcsop.GETFORWARDFEESIMPLE().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(1)))
	}
	return true
}

func (g *parityProgramGenerator) emitRuntimeControlOp() bool {
	switch g.r.Intn(12) {
	case 0:
		g.emit("ACCEPT", funcsop.ACCEPT().Serialize())
	case 1:
		base := len(g.stack)
		g.emitPushInt("setgaslimit_limit", big.NewInt(differentialFuzzGasLimit))
		g.emit("SETGASLIMIT", funcsop.SETGASLIMIT().Serialize())
		g.stack = g.stack[:base]
	case 2:
		base := len(g.stack)
		g.emit("GASCONSUMED", funcsop.GASCONSUMED().Serialize())
		g.emit("DROP(gasconsumed)", stackop.DROP().Serialize())
		g.stack = g.stack[:base]
	case 3:
		g.emit("COMMIT", funcsop.COMMIT().Serialize())
	case 4:
		return g.emitGlobalVarOp(false)
	case 5:
		return g.emitGlobalVarOp(true)
	case 6:
		return g.emitActionRegisterOp(g.r.Intn(9))
	default:
		return g.emitControlRegisterOp(g.r.Intn(12))
	}
	return true
}

func (g *parityProgramGenerator) emitGlobalVarOp(variable bool) bool {
	base := len(g.stack)
	value := big.NewInt(int64(1200 + g.r.Intn(200)))
	if variable {
		g.emitPushInt("setglobvar_value", value)
		g.emitPushInt("setglobvar_idx", big.NewInt(21))
		g.emit("SETGLOBVAR(21)", funcsop.SETGLOBVAR().Serialize())
		g.emitPushInt("getglobvar_idx", big.NewInt(21))
		g.emit("GETGLOBVAR(21)", funcsop.GETGLOBVAR().Serialize())
	} else {
		g.emitPushInt("setglob20_value", value)
		g.emit("SETGLOB(20)", funcsop.SETGLOB(20).Serialize())
		g.emit("GETGLOB(20)", funcsop.GETGLOB(20).Serialize())
	}
	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramIntValue(value))
	return true
}

func (g *parityProgramGenerator) emitControlRegisterOp(mode int) bool {
	base := len(g.stack)
	dataCell := cell.BeginCell().MustStoreUInt(uint64(0xA0+mode), 8).EndCell()
	actionCell := cell.BeginCell().MustStoreUInt(uint64(0xB0+mode), 8).EndCell()

	switch mode {
	case 0:
		g.emit("PUSHCTR(4)", execop.PUSHCTR(4).Serialize())
		g.emit("DROP(pushctr4)", stackop.DROP().Serialize())
	case 1:
		g.emit("PUSHCTR(5)", execop.PUSHCTR(5).Serialize())
		g.emit("DROP(pushctr5)", stackop.DROP().Serialize())
	case 2:
		g.emitPushCell("popctr4_data", dataCell)
		g.emit("POPCTR(4)", execop.POPCTR(4).Serialize())
		g.regD[0] = dataCell
	case 3:
		g.emitPushCell("popctr5_actions", actionCell)
		g.emit("POPCTR(5)", execop.POPCTR(5).Serialize())
		g.regD[1] = actionCell
	case 4:
		g.emitPushInt("pushctrx_idx", big.NewInt(4))
		g.emit("PUSHCTRX(4)", execop.PUSHCTRX().Serialize())
		g.emit("DROP(pushctrx4)", stackop.DROP().Serialize())
	case 5:
		g.emitPushCell("popctrx4_data", dataCell)
		g.emitPushInt("popctrx4_idx", big.NewInt(4))
		g.emit("POPCTRX(4)", execop.POPCTRX().Serialize())
		g.regD[0] = dataCell
	case 6:
		g.emit("SAVECTR(4)", execop.SAVECTR(4).Serialize())
	case 7:
		g.emit("SAVEALTCTR(4)", execop.SAVEALTCTR(4).Serialize())
	case 8:
		g.emit("SAVEBOTHCTR(4)", execop.SAVEBOTHCTR(4).Serialize())
	case 9:
		g.emitPushCell("popsavectr4_data", dataCell)
		g.emit("POPSAVECTR(4)", execop.POPSAVECTR(4).Serialize())
		g.regD[0] = dataCell
	case 10:
		g.emitPushCell("setretctr4_data", dataCell)
		g.emit("SETRETCTR(4)", execop.SETRETCTR(4).Serialize())
	default:
		g.emitPushCell("setaltctr4_data", dataCell)
		g.emit("SETALTCTR(4)", execop.SETALTCTR(4).Serialize())
	}

	g.stack = g.stack[:base]
	return true
}

func (g *parityProgramGenerator) emitSendRawMsgRegisterOp() bool {
	return g.emitActionRegisterOp(0)
}

func (g *parityProgramGenerator) emitActionRegisterOp(mode int) bool {
	base := len(g.stack)
	nextAction := func(build func(*cell.Builder)) *cell.Cell {
		b := cell.BeginCell().MustStoreRef(g.regD[1])
		build(b)
		return b.EndCell()
	}
	pushActionHash := func(name string, nextActions *cell.Cell) {
		g.emit("PUSHCTR(5/"+name+")", execop.PUSHCTR(5).Serialize())
		g.emit("HASHCU("+name+"_c5)", cellsliceop.HASHCU().Serialize())
		g.regD[1] = nextActions
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(new(big.Int).SetBytes(nextActions.Hash())))
	}
	storeBigCoins := func(b *cell.Builder, amount *big.Int) {
		if err := b.StoreBigCoins(amount); err != nil {
			panic(err)
		}
	}
	storeMaybeRef := func(b *cell.Builder, cl *cell.Cell) {
		if err := b.StoreMaybeRefUncheckedDepth(cl); err != nil {
			panic(err)
		}
	}
	storeRef := func(b *cell.Builder, cl *cell.Cell) {
		if err := b.StoreRefUncheckedDepth(cl); err != nil {
			panic(err)
		}
	}

	switch mode {
	case 0:
		msg := parityProgramOutboundInternalMessage()
		actionMode := uint8(g.r.Intn(4))
		nextActions := nextAction(func(b *cell.Builder) {
			b.MustStoreUInt(0x0ec3c86d, 32).MustStoreUInt(uint64(actionMode), 8)
			storeRef(b, msg)
		})

		g.emitPushCell("sendrawmsg_msg", msg)
		g.emitPushInt("sendrawmsg_mode", big.NewInt(int64(actionMode)))
		g.emit("SENDRAWMSG", funcsop.SENDRAWMSG().Serialize())
		pushActionHash("sendrawmsg", nextActions)
	case 1:
		amount := big.NewInt(777)
		actionMode := uint8(3)
		nextActions := nextAction(func(b *cell.Builder) {
			b.MustStoreUInt(0x36e6b809, 32).MustStoreUInt(uint64(actionMode), 8)
			storeBigCoins(b, amount)
			storeMaybeRef(b, nil)
		})

		g.emitPushInt("rawreserve_amount", amount)
		g.emitPushInt("rawreserve_mode", big.NewInt(int64(actionMode)))
		g.emit("RAWRESERVE", funcsop.RAWRESERVE().Serialize())
		pushActionHash("rawreserve", nextActions)
	case 2:
		amount := big.NewInt(888)
		extra := cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell()
		actionMode := uint8(4)
		nextActions := nextAction(func(b *cell.Builder) {
			b.MustStoreUInt(0x36e6b809, 32).MustStoreUInt(uint64(actionMode), 8)
			storeBigCoins(b, amount)
			storeMaybeRef(b, extra)
		})

		g.emitPushInt("rawreservex_amount", amount)
		g.emitPushCell("rawreservex_extra", extra)
		g.emitPushInt("rawreservex_mode", big.NewInt(int64(actionMode)))
		g.emit("RAWRESERVEX", funcsop.RAWRESERVEX().Serialize())
		pushActionHash("rawreservex", nextActions)
	case 3:
		code := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
		nextActions := nextAction(func(b *cell.Builder) {
			b.MustStoreUInt(0xAD4DE08E, 32)
			storeRef(b, code)
		})

		g.emitPushCell("setcode_code", code)
		g.emit("SETCODE", funcsop.SETCODE().Serialize())
		pushActionHash("setcode", nextActions)
	case 4:
		code := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
		actionMode := uint8(1)
		nextActions := nextAction(func(b *cell.Builder) {
			b.MustStoreUInt(0x26FA1DD4, 32).MustStoreUInt(uint64(actionMode)*2+1, 8)
			storeRef(b, code)
		})

		g.emitPushCell("setlibcode_code", code)
		g.emitPushInt("setlibcode_mode", big.NewInt(int64(actionMode)))
		g.emit("SETLIBCODE", funcsop.SETLIBCODE().Serialize())
		pushActionHash("setlibcode", nextActions)
	case 5:
		hash := new(big.Int).SetBytes(bytes.Repeat([]byte{0x22}, 32))
		actionMode := uint8(2)
		nextActions := nextAction(func(b *cell.Builder) {
			b.MustStoreUInt(0x26FA1DD4, 32).MustStoreUInt(uint64(actionMode)*2, 8).MustStoreBigUInt(hash, 256)
		})

		g.emitPushInt("changelib_hash", hash)
		g.emitPushInt("changelib_mode", big.NewInt(int64(actionMode)))
		g.emit("CHANGELIB", funcsop.CHANGELIB().Serialize())
		pushActionHash("changelib", nextActions)
	case 6:
		msg := parityProgramSendMsgInternalMessage()
		g.emitPushCell("sendmsg_fee_msg", msg)
		g.emitPushInt("sendmsg_fee_mode", big.NewInt(1024))
		g.emit("SENDMSG(fee-only)", funcsop.SENDMSG().Serialize())
		g.emit("DROP(sendmsg_fee)", stackop.DROP().Serialize())
		pushActionHash("sendmsg_fee", g.regD[1])
	case 7:
		msg := parityProgramSendMsgInternalMessage()
		actionMode := uint8(1)
		nextActions := nextAction(func(b *cell.Builder) {
			b.MustStoreUInt(0x0ec3c86d, 32).MustStoreUInt(uint64(actionMode), 8)
			storeRef(b, msg)
		})

		g.emitPushCell("sendmsg_send_msg", msg)
		g.emitPushInt("sendmsg_send_mode", big.NewInt(int64(actionMode)))
		g.emit("SENDMSG(send)", funcsop.SENDMSG().Serialize())
		g.emit("DROP(sendmsg_send_fee)", stackop.DROP().Serialize())
		pushActionHash("sendmsg_send", nextActions)
	default:
		msg := parityProgramSendMsgLargeUserFwdFeeInternalMessage()
		g.emitPushCell("sendmsg_user_fwd_fee_msg", msg)
		g.emitPushInt("sendmsg_user_fwd_fee_mode", big.NewInt(1024))
		g.emit("SENDMSG(user-fwd-fee)", funcsop.SENDMSG().Serialize())
		g.emit("DROP(sendmsg_user_fwd_fee)", stackop.DROP().Serialize())
		pushActionHash("sendmsg_user_fwd_fee", g.regD[1])
	}
	return true
}

func parityProgramOutboundInternalMessage() *cell.Cell {
	msg, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     address.NewAddressNone(),
		DstAddr:     crossTestAddr,
		Amount:      tlb.FromNanoTONU(1),
		Body:        cell.BeginCell().MustStoreUInt(0xB0, 8).EndCell(),
	})
	if err != nil {
		panic(err)
	}
	return msg
}

func parityProgramSendMsgInternalMessage() *cell.Cell {
	msg, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     crossTestAddr,
		DstAddr:     crossTestAddr,
		Amount:      tlb.FromNanoTONU(1),
		IHRFee:      tlb.FromNanoTONU(0),
		FwdFee:      tlb.FromNanoTONU(0),
		CreatedLT:   1,
		CreatedAt:   uint32(crossTestTime.Unix()),
		Body:        cell.BeginCell().MustStoreUInt(0xB1, 8).EndCell(),
	})
	if err != nil {
		panic(err)
	}
	return msg
}

func parityProgramSendMsgUserFwdFeeInternalMessage() *cell.Cell {
	msg, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     crossTestAddr,
		DstAddr:     crossTestAddr,
		Amount:      tlb.FromNanoTONU(1),
		IHRFee:      tlb.FromNanoTONU(0),
		FwdFee:      tlb.FromNanoTONU(500),
		CreatedLT:   1,
		CreatedAt:   uint32(crossTestTime.Unix()),
		Body:        cell.BeginCell().MustStoreUInt(0xB3, 8).EndCell(),
	})
	if err != nil {
		panic(err)
	}
	return msg
}

func parityProgramSendMsgLargeUserFwdFeeInternalMessage() *cell.Cell {
	msg, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     crossTestAddr,
		DstAddr:     crossTestAddr,
		Amount:      tlb.FromNanoTONU(1),
		IHRFee:      tlb.FromNanoTONU(0),
		FwdFee:      tlb.FromNanoTONU(50_000),
		CreatedLT:   1,
		CreatedAt:   uint32(crossTestTime.Unix()),
		Body:        cell.BeginCell().MustStoreUInt(0xB4, 8).EndCell(),
	})
	if err != nil {
		panic(err)
	}
	return msg
}

func parityProgramSendMsgExternalOutMessage() *cell.Cell {
	msg, err := tlb.ToCell(&tlb.ExternalMessageOut{
		SrcAddr:   crossTestAddr,
		DstAddr:   address.NewAddressExt(0, 256, bytes.Repeat([]byte{0x33}, 32)),
		CreatedLT: 1,
		CreatedAt: uint32(crossTestTime.Unix()),
		Body:      cell.BeginCell().MustStoreUInt(0xB2, 8).EndCell(),
	})
	if err != nil {
		panic(err)
	}
	return msg
}

func parityProgramTonFuncUnpackedConfig(t *testing.T) tuple.Tuple {
	t.Helper()

	unpacked := tuple.NewTupleSized(7)
	mustSetTupleValue(t, &unpacked, 0, makeStoragePricesSlice(100, 3, 5, 7, 11))
	mustSetTupleValue(t, &unpacked, 1, cell.BeginCell().MustStoreUInt(uint64(uint32(tonopsTestGlobalID)), 32).ToSlice())
	mustSetTupleValue(t, &unpacked, 2, makeGasPricesSlice(100, 77, 200, 1000, 1200, 50, 2000, 3000, 4000, true))
	mustSetTupleValue(t, &unpacked, 3, makeGasPricesSlice(100, 55, 150, 900, 900, 40, 1800, 2800, 3800, true))
	mustSetTupleValue(t, &unpacked, 4, makeMsgPricesSlice(1000, 200, 300, 500, 1000, 2000))
	mustSetTupleValue(t, &unpacked, 5, makeMsgPricesSlice(900, 120, 220, 400, 800, 1200))
	mustSetTupleValue(t, &unpacked, 6, makeSizeLimitsSlice(1<<20, 128))
	return unpacked
}

func (g *parityProgramGenerator) emitDataSizeProgramOp(sliceArg, quiet bool) bool {
	base := len(g.stack)
	root := parityProgramStorageStatCell()
	cells, bits, refs := int64(2), int64(8), int64(1)
	if g.r.Intn(3) == 0 {
		root = parityProgramSharedRefStorageStatCell()
		cells, bits, refs = 2, 4, 2
	}
	if sliceArg {
		g.emitPushSlice("sdatasize_src", root.MustBeginParse())
	} else {
		g.emitPushCell("cdatasize_src", root)
	}
	g.emitPushInt("datasize_bound", big.NewInt(10))

	name := "CDATASIZE"
	op := funcsop.CDATASIZE().Serialize()
	if sliceArg {
		name = "SDATASIZE"
		op = funcsop.SDATASIZE().Serialize()
		cells--
	}
	if quiet {
		name += "Q"
		if sliceArg {
			op = funcsop.SDATASIZEQ().Serialize()
		} else {
			op = funcsop.CDATASIZEQ().Serialize()
		}
	}
	g.emit(name, op)

	g.stack = g.stack[:base]
	g.stack = append(g.stack,
		parityProgramIntValue(big.NewInt(cells)),
		parityProgramIntValue(big.NewInt(bits)),
		parityProgramIntValue(big.NewInt(refs)),
	)
	if quiet {
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-1)))
	}
	return true
}

func (g *parityProgramGenerator) emitPrngOp(mode int) bool {
	switch mode {
	case 0:
		value := g.nextRandU256()
		g.emit("RANDU256", funcsop.RANDU256().Serialize())
		g.stack = append(g.stack, parityProgramIntValue(value))
		g.emitRandSeedProbe()
	case 1:
		bound := big.NewInt(int64(1 + g.r.Intn(1000)))
		value := g.nextRandU256()
		value.Mul(value, bound)
		value.Rsh(value, 256)
		g.emitPushInt("rand_bound", bound)
		g.emit("RAND", funcsop.RAND().Serialize())
		g.stack = g.stack[:len(g.stack)-1]
		g.stack = append(g.stack, parityProgramIntValue(value))
		g.emitRandSeedProbe()
	case 2:
		seed := big.NewInt(int64(g.r.Intn(1000)))
		g.emitPushInt("setrand_seed", seed)
		g.emit("SETRAND", funcsop.SETRAND().Serialize())
		g.stack = g.stack[:len(g.stack)-1]
		g.seed = parityProgramUint256Bytes(seed)
		g.emitRandSeedProbe()
	case 3:
		mix := big.NewInt(int64(g.r.Intn(1000)))
		mixBytes := parityProgramUint256Bytes(mix)
		buf := make([]byte, 64)
		copy(buf, g.seed)
		copy(buf[32:], mixBytes)
		sum := sha256.Sum256(buf)
		g.emitPushInt("addrand_seed", mix)
		g.emit("ADDRAND", funcsop.ADDRAND().Serialize())
		g.stack = g.stack[:len(g.stack)-1]
		g.seed = append(g.seed[:0], sum[:]...)
		g.emitRandSeedProbe()
	case 4:
		value := g.nextRandU256()
		value.Mul(value, big.NewInt(0))
		value.Rsh(value, 256)
		g.emitPushInt("rand_zero_bound", big.NewInt(0))
		g.emit("RAND(0)", funcsop.RAND().Serialize())
		g.stack = g.stack[:len(g.stack)-1]
		g.stack = append(g.stack, parityProgramIntValue(value))
		g.emitRandSeedProbe()
	case 5:
		bound := big.NewInt(-7)
		value := g.nextRandU256()
		value.Mul(value, bound)
		value.Rsh(value, 256)
		g.emitPushInt("rand_negative_bound", bound)
		g.emit("RAND(negative)", funcsop.RAND().Serialize())
		g.stack = g.stack[:len(g.stack)-1]
		g.stack = append(g.stack, parityProgramIntValue(value))
		g.emitRandSeedProbe()
	default:
		seed := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
		g.emitPushInt("setrand_max_seed", seed)
		g.emit("SETRAND(max)", funcsop.SETRAND().Serialize())
		g.stack = g.stack[:len(g.stack)-1]
		g.seed = parityProgramUint256Bytes(seed)
		g.emitRandSeedProbe()
	}
	return true
}

func (g *parityProgramGenerator) emitRandSeedProbe() {
	g.emit("RANDSEED(after_prng)", funcsop.RANDSEED().Serialize())
	g.stack = append(g.stack, parityProgramIntValue(new(big.Int).SetBytes(g.seed)))
}

func (g *parityProgramGenerator) nextRandU256() *big.Int {
	sum := sha512.Sum512(g.seed)
	g.seed = append(g.seed[:0], sum[:32]...)
	return new(big.Int).SetBytes(sum[32:])
}

func (g *parityProgramGenerator) emitFuncHashVarIntOp(mode int) bool {
	switch mode {
	case 0:
		data := []byte{0xAB, 0xCD}
		sum := sha256.Sum256(data)
		g.emitPushSlice("sha256u_src", cell.BeginCell().MustStoreSlice(data, uint(len(data)*8)).ToSlice())
		g.emit("SHA256U", funcsop.SHA256U().Serialize())
		g.stack = g.stack[:len(g.stack)-1]
		g.stack = append(g.stack, parityProgramIntValue(new(big.Int).SetBytes(sum[:])))
	case 1:
		builder := cell.BeginCell().MustStoreUInt(0xAB, 8)
		hash := builder.Copy().EndCell().Hash()
		g.emitPushBuilder("hashbu_src", builder)
		g.emit("HASHBU", funcsop.HASHBU().Serialize())
		g.stack = g.stack[:len(g.stack)-1]
		g.stack = append(g.stack, parityProgramIntValue(new(big.Int).SetBytes(hash)))
	case 2:
		data := []byte{0x11, 0x22, 0x33}
		sum := sha256.Sum256(data)
		base := len(g.stack)
		g.emitPushSlice("hashext_src", cell.BeginCell().MustStoreSlice(data, uint(len(data)*8)).ToSlice())
		g.emitPushInt("hashext_count", big.NewInt(1))
		g.emit("HASHEXT(0)", funcsop.HASHEXT(0).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(new(big.Int).SetBytes(sum[:])))
	case 3:
		sum := sha256.Sum256(nil)
		g.emitPushSlice("sha256u_empty_src", cell.BeginCell().ToSlice())
		g.emit("SHA256U(empty)", funcsop.SHA256U().Serialize())
		g.stack = g.stack[:len(g.stack)-1]
		g.stack = append(g.stack, parityProgramIntValue(new(big.Int).SetBytes(sum[:])))
	case 4:
		ref := cell.BeginCell().MustStoreUInt(0x5A, 8).EndCell()
		builder := cell.BeginCell().MustStoreUInt(0xAB, 8).MustStoreRef(ref)
		hash := builder.Copy().EndCell().Hash()
		g.emitPushBuilder("hashbu_ref_src", builder)
		g.emit("HASHBU(ref)", funcsop.HASHBU().Serialize())
		g.stack = g.stack[:len(g.stack)-1]
		g.stack = append(g.stack, parityProgramIntValue(new(big.Int).SetBytes(hash)))
	case 5:
		sum := sha256.Sum256([]byte{0xAB})
		base := len(g.stack)
		g.emitPushSlice("hashext_concat_hi", cell.BeginCell().MustStoreUInt(0xA, 4).ToSlice())
		g.emitPushSlice("hashext_concat_lo", cell.BeginCell().MustStoreUInt(0xB, 4).ToSlice())
		g.emitPushInt("hashext_concat_count", big.NewInt(2))
		g.emit("HASHEXT(0,concat)", funcsop.HASHEXT(0).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(new(big.Int).SetBytes(sum[:])))
	case 6:
		src := parityProgramVarIntSlice(big.NewInt(-2), 4, true)
		base := len(g.stack)
		g.emitPushSlice("ldvarint16_src", src)
		g.emit("LDVARINT16", funcsop.LDVARINT16().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-2)), parityProgramStackValue{kind: parityProgramSlice, slice: cell.BeginCell().EndCell().MustBeginParse()})
	case 7:
		src := parityProgramVarIntSlice(big.NewInt(17), 5, false)
		base := len(g.stack)
		g.emitPushSlice("ldvaruint32_src", src)
		g.emit("LDVARUINT32", funcsop.LDVARUINT32().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(17)), parityProgramStackValue{kind: parityProgramSlice, slice: cell.BeginCell().EndCell().MustBeginParse()})
	case 8:
		src := parityProgramVarIntSlice(big.NewInt(-2), 5, true)
		base := len(g.stack)
		g.emitPushSlice("ldvarint32_src", src)
		g.emit("LDVARINT32", funcsop.LDVARINT32().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-2)), parityProgramStackValue{kind: parityProgramSlice, slice: cell.BeginCell().EndCell().MustBeginParse()})
	case 9:
		base := len(g.stack)
		builder := cell.BeginCell()
		next := builder.Copy()
		next.MustStoreUInt(1, 4).MustStoreInt(-2, 8)
		g.emitPushBuilder("stvarint16_builder", builder)
		g.emitPushInt("stvarint16_value", big.NewInt(-2))
		g.emit("STVARINT16", funcsop.STVARINT16().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: next})
	case 10:
		base := len(g.stack)
		builder := cell.BeginCell()
		next := builder.Copy()
		next.MustStoreUInt(1, 5).MustStoreUInt(17, 8)
		g.emitPushBuilder("stvaruint32_builder", builder)
		g.emitPushInt("stvaruint32_value", big.NewInt(17))
		g.emit("STVARUINT32", funcsop.STVARUINT32().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: next})
	case 11:
		src := cell.BeginCell().MustStoreUInt(0, 5).MustStoreUInt(0xA, 4).ToSlice()
		base := len(g.stack)
		g.emitPushSlice("ldvaruint32_zero_src", src)
		g.emit("LDVARUINT32(zero)", funcsop.LDVARUINT32().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(0)), parityProgramStackValue{kind: parityProgramSlice, slice: parityProgramTailSlice()})
	case 12:
		base := len(g.stack)
		builder := cell.BeginCell()
		next := builder.Copy()
		next.MustStoreUInt(0, 5)
		g.emitPushBuilder("stvaruint32_zero_builder", builder)
		g.emitPushInt("stvaruint32_zero_value", big.NewInt(0))
		g.emit("STVARUINT32(zero)", funcsop.STVARUINT32().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: next})
	case 13:
		base := len(g.stack)
		builder := cell.BeginCell()
		next := builder.Copy()
		next.MustStoreUInt(0, 4)
		g.emitPushBuilder("stvarint16_zero_builder", builder)
		g.emitPushInt("stvarint16_zero_value", big.NewInt(0))
		g.emit("STVARINT16(zero)", funcsop.STVARINT16().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: next})
	default:
		base := len(g.stack)
		builder := cell.BeginCell()
		next := builder.Copy()
		next.MustStoreUInt(1, 5).MustStoreInt(-2, 8)
		g.emitPushBuilder("stvarint32_builder", builder)
		g.emitPushInt("stvarint32_value", big.NewInt(-2))
		g.emit("STVARINT32", funcsop.STVARINT32().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: next})
	}
	return true
}

func (g *parityProgramGenerator) emitMessageAddressOp() bool {
	switch g.r.Intn(16) {
	case 0:
		return g.emitLoadMsgAddressOp("LDMSGADDR", funcsop.LDMSGADDR().Serialize(), false, false, false)
	case 1:
		return g.emitLoadMsgAddressOp("LDMSGADDRQ", funcsop.LDMSGADDRQ().Serialize(), false, true, false)
	case 2:
		return g.emitLoadMsgAddressOp("LDSTDADDR", funcsop.LDSTDADDR().Serialize(), true, false, false)
	case 3:
		return g.emitLoadMsgAddressOp("LDSTDADDRQ", funcsop.LDSTDADDRQ().Serialize(), true, true, false)
	case 4:
		return g.emitLoadMsgAddressOp("LDOPTSTDADDR", funcsop.LDOPTSTDADDR().Serialize(), true, false, true)
	case 5:
		return g.emitLoadMsgAddressOp("LDOPTSTDADDRQ", funcsop.LDOPTSTDADDRQ().Serialize(), true, true, true)
	case 6:
		return g.emitParseMsgAddressOp(false)
	case 7:
		return g.emitParseMsgAddressOp(true)
	case 8:
		return g.emitRewriteStdAddressOp(false, false)
	case 9:
		return g.emitRewriteStdAddressOp(false, true)
	case 10:
		return g.emitRewriteStdAddressOp(true, false)
	case 11:
		return g.emitRewriteStdAddressOp(true, true)
	case 12:
		return g.emitStoreStdAddressOp(false, false, false)
	case 13:
		return g.emitStoreStdAddressOp(false, true, false)
	case 14:
		return g.emitStoreStdAddressOp(true, false, g.r.Intn(2) == 0)
	default:
		return g.emitStoreStdAddressOp(true, true, g.r.Intn(2) == 0)
	}
}

func (g *parityProgramGenerator) emitLoadMsgAddressOp(name string, op *cell.Builder, stdOnly, quiet, optStd bool) bool {
	base := len(g.stack)
	if optStd && g.r.Intn(3) == 0 {
		src := parityProgramAddrNoneTailSlice()
		g.emitPushSlice(name+"_none", src)
		g.emit(name, op)
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramStackValue{kind: parityProgramNull},
			parityProgramStackValue{kind: parityProgramSlice, slice: parityProgramTailSlice()},
		)
		if quiet {
			g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-1)))
		}
		return true
	}

	if !stdOnly {
		switch g.r.Intn(3) {
		case 0:
			src := parityProgramAddrNoneTailSlice()
			g.emitPushSlice(name+"_addr_none", src)
			g.emit(name, op)
			g.stack = g.stack[:base]
			g.stack = append(g.stack,
				parityProgramStackValue{kind: parityProgramSlice, slice: parityProgramAddrNoneSlice()},
				parityProgramStackValue{kind: parityProgramSlice, slice: parityProgramTailSlice()},
			)
			if quiet {
				g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-1)))
			}
			return true
		case 1:
			src := parityProgramExtAddrTailSlice()
			g.emitPushSlice(name+"_ext", src)
			g.emit(name, op)
			g.stack = g.stack[:base]
			g.stack = append(g.stack,
				parityProgramStackValue{kind: parityProgramSlice, slice: parityProgramExtAddrSlice()},
				parityProgramStackValue{kind: parityProgramSlice, slice: parityProgramTailSlice()},
			)
			if quiet {
				g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-1)))
			}
			return true
		}
	}

	src := parityProgramStdAddrTailSlice()
	g.emitPushSlice(name+"_std", src)
	g.emit(name, op)
	g.stack = g.stack[:base]
	g.stack = append(g.stack,
		parityProgramStackValue{kind: parityProgramSlice, slice: parityProgramStdAddrSlice()},
		parityProgramStackValue{kind: parityProgramSlice, slice: parityProgramTailSlice()},
	)
	if quiet {
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-1)))
	}
	return true
}

func (g *parityProgramGenerator) emitParseMsgAddressOp(quiet bool) bool {
	base := len(g.stack)
	name := "PARSEMSGADDR"
	op := funcsop.PARSEMSGADDR().Serialize()
	if quiet {
		name = "PARSEMSGADDRQ"
		op = funcsop.PARSEMSGADDRQ().Serialize()
	}
	var parsed parityProgramStackValue
	switch g.r.Intn(3) {
	case 0:
		g.emitPushSlice(name+"_none", parityProgramAddrNoneSlice())
		parsed = parityProgramParsedNoneAddrTuple()
	case 1:
		g.emitPushSlice(name+"_ext", parityProgramExtAddrSlice())
		parsed = parityProgramParsedExtAddrTuple()
	default:
		g.emitPushSlice(name+"_std", parityProgramStdAddrSlice())
		parsed = parityProgramParsedStdAddrTuple()
	}
	g.emit(name, op)
	g.stack = g.stack[:base]
	g.stack = append(g.stack, parsed)
	if quiet {
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-1)))
	}
	return true
}

func (g *parityProgramGenerator) emitRewriteStdAddressOp(varAddr, quiet bool) bool {
	base := len(g.stack)
	name := "REWRITESTDADDR"
	op := funcsop.REWRITESTDADDR().Serialize()
	if varAddr {
		name = "REWRITEVARADDR"
		op = funcsop.REWRITEVARADDR().Serialize()
	}
	if quiet {
		name += "Q"
		if varAddr {
			op = funcsop.REWRITEVARADDRQ().Serialize()
		} else {
			op = funcsop.REWRITESTDADDRQ().Serialize()
		}
	}
	g.emitPushSlice(name+"_std", parityProgramStdAddrSlice())
	g.emit(name, op)
	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(crossTestAddr.Workchain()))))
	if varAddr {
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: parityProgramStdAddrDataSlice()})
	} else {
		g.stack = append(g.stack, parityProgramIntValue(new(big.Int).SetBytes(crossTestAddr.Data())))
	}
	if quiet {
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-1)))
	}
	return true
}

func (g *parityProgramGenerator) emitStoreStdAddressOp(opt, quiet, none bool) bool {
	base := len(g.stack)
	name := "STSTDADDR"
	op := funcsop.STSTDADDR().Serialize()
	if opt {
		name = "STOPTSTDADDR"
		op = funcsop.STOPTSTDADDR().Serialize()
	}
	if quiet {
		name += "Q"
		if opt {
			op = funcsop.STOPTSTDADDRQ().Serialize()
		} else {
			op = funcsop.STSTDADDRQ().Serialize()
		}
	}

	builder := cell.BeginCell()
	next := cell.BeginCell()
	if opt && none {
		g.emitPushNull(name + "_none")
		next.MustStoreUInt(0, 2)
	} else {
		g.emitPushSlice(name+"_std", parityProgramStdAddrSlice())
		next.MustStoreBuilder(parityProgramStdAddrSlice().ToBuilder())
	}
	g.emitPushBuilder(name+"_builder", builder)
	g.emit(name, op)

	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: next})
	if quiet {
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(0)))
	}
	return true
}

func (g *parityProgramGenerator) emitPushBuilder(name string, builder *cell.Builder) {
	base := len(g.stack)
	if builder.BitsUsed() == 0 && builder.RefsUsed() == 0 {
		g.emit("NEWC("+name+")", cellsliceop.NEWC().Serialize())
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: cell.BeginCell()})
		return
	}

	g.emitPushSlice(name+"_contents", builder.ToSlice())
	g.emit("NEWC("+name+")", cellsliceop.NEWC().Serialize())
	g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: cell.BeginCell()})
	g.emit("STSLICE("+name+")", cellsliceop.STSLICE().Serialize())
	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: builder.Copy()})
}

func (g *parityProgramGenerator) emitDictKey(name string, kind int, value uint64, bits uint) {
	if kind == 0 {
		g.emitPushSlice(name, parityProgramKeySlice(value, bits))
		return
	}
	g.emitPushInt(name, big.NewInt(int64(value)))
}

func parityProgramVarIntSlice(value *big.Int, lenBits uint, signed bool) *cell.Slice {
	b := cell.BeginCell().MustStoreUInt(1, lenBits)
	if signed {
		b.MustStoreBigInt(value, 8)
	} else {
		b.MustStoreBigUInt(value, 8)
	}
	return b.ToSlice()
}

func parityProgramUint256Bytes(v *big.Int) []byte {
	out := make([]byte, 32)
	v.FillBytes(out)
	return out
}

func (g *parityProgramGenerator) emitCondSelectOp(checked bool) bool {
	base := len(g.stack)
	cond := int64(0)
	selected := parityProgramIntValue(big.NewInt(22))
	if g.r.Intn(2) == 0 {
		cond = -1
		selected = parityProgramIntValue(big.NewInt(11))
	}

	g.emitPushInt("cond", big.NewInt(cond))
	g.emitPushInt("cond_x", big.NewInt(11))
	g.emitPushInt("cond_y", big.NewInt(22))
	if checked {
		g.emit("CONDSELCHK", execop.CONDSELCHK().Serialize())
	} else {
		g.emit("CONDSEL", stackop.CONDSEL().Serialize())
	}

	g.stack = g.stack[:base]
	g.stack = append(g.stack, selected)
	return true
}

func (g *parityProgramGenerator) emitLoadDictOp(preload bool) bool {
	root, _, _ := parityProgramDictRoot(false)
	src := cell.BeginCell().MustStoreMaybeRef(root).EndCell().MustBeginParse()
	base := len(g.stack)
	g.emitPushSlice("dict_container", src)
	if preload {
		g.emit("PLDDICT", parityProgramRawOp(0xF405, 16))
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramCell, cell: root})
		return true
	}
	g.emit("LDDICT", parityProgramRawOp(0xF404, 16))
	g.stack = g.stack[:base]
	g.stack = append(g.stack,
		parityProgramStackValue{kind: parityProgramCell, cell: root},
		parityProgramStackValue{kind: parityProgramSlice, slice: cell.BeginCell().EndCell().MustBeginParse()},
	)
	return true
}

func (g *parityProgramGenerator) emitStoreDictOp() bool {
	root, _, _ := parityProgramDictRoot(false)
	base := len(g.stack)
	builder := cell.BeginCell()
	next := cell.BeginCell().MustStoreMaybeRef(root)

	g.emitPushCell("stdict_root", root)
	g.emitPushBuilder("stdict_builder", builder)
	g.emit("STDICT", parityProgramRawOp(0xF400, 16))

	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: next})
	return true
}

func (g *parityProgramGenerator) emitSkipDictOp() bool {
	root, _, _ := parityProgramDictRoot(false)
	base := len(g.stack)
	src := cell.BeginCell().MustStoreMaybeRef(root).MustStoreUInt(0xA, 4).ToSlice()
	rest := parityProgramTailSlice()

	g.emitPushSlice("skipdict_src", src)
	g.emit("SKIPDICT", parityProgramRawOp(0xF401, 16))

	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: rest})
	return true
}

func (g *parityProgramGenerator) emitLoadDictSliceOp(preload, quiet bool) bool {
	root, _, _ := parityProgramDictRoot(false)
	base := len(g.stack)
	dictSlice := cell.BeginCell().MustStoreMaybeRef(root).ToSlice()
	src := cell.BeginCell().MustStoreMaybeRef(root).MustStoreUInt(0xA, 4).ToSlice()
	name := "LDDICTS"
	opcode := uint64(0xF402)
	if preload {
		name = "PLDDICTS"
		opcode = 0xF403
	}

	g.emitPushSlice(strings.ToLower(name)+"_src", src)
	g.emit(name, parityProgramRawOp(opcode, 16))

	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: dictSlice})
	if !preload {
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: parityProgramTailSlice()})
	}
	if quiet {
		g.stack = append(g.stack, parityProgramBoolValue(true))
	}
	return true
}

func (g *parityProgramGenerator) emitLoadDictQuietOp(preload bool) bool {
	root, _, _ := parityProgramDictRoot(false)
	src := cell.BeginCell().MustStoreMaybeRef(root).MustStoreUInt(0xA, 4).ToSlice()
	base := len(g.stack)
	name := "LDDICTQ"
	opcode := uint64(0xF406)
	if preload {
		name = "PLDDICTQ"
		opcode = 0xF407
	}

	g.emitPushSlice(strings.ToLower(name)+"_src", src)
	g.emit(name, parityProgramRawOp(opcode, 16))

	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramCell, cell: root})
	if !preload {
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: parityProgramTailSlice()})
	}
	g.stack = append(g.stack, parityProgramBoolValue(true))
	return true
}

func (g *parityProgramGenerator) emitDictGetOp(byRef, intKey, unsigned bool) bool {
	root, value, ref := parityProgramDictRoot(byRef)
	base := len(g.stack)

	key := uint64(0x12)
	opcode := uint64(0xF40A)
	kind := 0
	if byRef {
		opcode++
	}
	switch {
	case intKey && unsigned:
		kind = 2
		opcode += 4
		g.emitPushInt("dict_key_u", big.NewInt(int64(key)))
	case intKey:
		kind = 1
		opcode += 2
		g.emitPushInt("dict_key_i", big.NewInt(int64(key)))
	default:
		g.emitPushSlice("dict_key", parityProgramKeySlice(key, 8))
	}
	g.emitPushCell("dict_root", root)
	g.emitPushInt("dict_bits", big.NewInt(8))
	g.emit(parityProgramDictValueName(kind, "GET", byRef), parityProgramRawOp(opcode, 16))

	g.stack = g.stack[:base]
	if byRef {
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramCell, cell: ref})
	} else {
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: value.MustBeginParse()})
	}
	g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-1)))
	return true
}

func (g *parityProgramGenerator) emitDictSetOp(byRef bool, kind int) bool {
	base := len(g.stack)
	value := cell.BeginCell().MustStoreUInt(0xA5, 8).EndCell()
	key := uint64(0x21)
	opcode := parityProgramDictValueOpcode(0xF412, kind, byRef)

	dict := cell.NewDict(8)
	var err error
	if byRef {
		ref := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
		g.emitPushCell("dict_set_ref", ref)
		_, err = dict.SetBuilderWithMode(parityProgramDictKeyCellForKind(kind, key, 8), cell.BeginCell().MustStoreRef(ref), cell.DictSetModeSet)
	} else {
		g.emitPushSlice("dict_set_value", value.MustBeginParse())
		_, err = dict.SetWithMode(parityProgramDictKeyCellForKind(kind, key, 8), value, cell.DictSetModeSet)
	}
	if err != nil {
		panic(err)
	}
	g.emitDictKey("dict_set_key", kind, key, 8)
	g.emitPushNull("dict_set_root")
	g.emitPushInt("dict_set_bits", big.NewInt(8))
	g.emit(parityProgramDictValueName(kind, "SET", byRef), parityProgramRawOp(opcode, 16))

	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramMaybeCellValue(dict.AsCell()))
	return true
}

func (g *parityProgramGenerator) emitDictSetGetOp(byRef bool, kind int) bool {
	root, oldValue, oldRef := parityProgramDictRoot(byRef)
	base := len(g.stack)
	newValue := cell.BeginCell().MustStoreUInt(0x99, 8).EndCell()
	newRef := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	key := uint64(0x12)
	opcode := parityProgramDictValueOpcode(0xF41A, kind, byRef)

	dict := cell.NewDict(8)
	var err error
	if byRef {
		g.emitPushCell("dict_setget_ref", newRef)
		_, err = dict.SetBuilderWithMode(parityProgramDictKeyCellForKind(kind, key, 8), cell.BeginCell().MustStoreRef(newRef), cell.DictSetModeSet)
	} else {
		g.emitPushSlice("dict_setget_value", newValue.MustBeginParse())
		_, err = dict.SetWithMode(parityProgramDictKeyCellForKind(kind, key, 8), newValue, cell.DictSetModeSet)
	}
	if err != nil {
		panic(err)
	}
	g.emitDictKey("dict_setget_key", kind, key, 8)
	g.emitPushCell("dict_setget_root", root)
	g.emitPushInt("dict_setget_bits", big.NewInt(8))
	g.emit(parityProgramDictValueName(kind, "SETGET", byRef), parityProgramRawOp(opcode, 16))

	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramMaybeCellValue(dict.AsCell()))
	if byRef {
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramCell, cell: oldRef})
	} else {
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: oldValue.MustBeginParse()})
	}
	g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-1)))
	return true
}

func (g *parityProgramGenerator) emitDictReplaceAddOp(add bool, byRef bool, kind int) bool {
	root, _, _ := parityProgramDictRoot(byRef)
	base := len(g.stack)
	value := cell.BeginCell().MustStoreUInt(0x77, 8).EndCell()
	ref := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	key := uint64(0x12)
	opcode := uint64(0xF422)
	name := "REPLACE"
	if add {
		key = 0x21
		opcode = 0xF432
		name = "ADD"
	}
	opcode = parityProgramDictValueOpcode(opcode, kind, byRef)

	dict := parityProgramRootAsDict(root, 8)
	var err error
	if byRef {
		g.emitPushCell("dict_replace_add_ref", ref)
		_, err = dict.SetBuilderWithMode(parityProgramDictKeyCellForKind(kind, key, 8), cell.BeginCell().MustStoreRef(ref), cell.DictSetModeSet)
	} else {
		g.emitPushSlice("dict_replace_add_value", value.MustBeginParse())
		_, err = dict.SetWithMode(parityProgramDictKeyCellForKind(kind, key, 8), value, cell.DictSetModeSet)
	}
	if err != nil {
		panic(err)
	}

	g.emitDictKey("dict_replace_add_key", kind, key, 8)
	g.emitPushCell("dict_replace_add_root", root)
	g.emitPushInt("dict_replace_add_bits", big.NewInt(8))
	g.emit(parityProgramDictValueName(kind, name, byRef), parityProgramRawOp(opcode, 16))

	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramMaybeCellValue(dict.AsCell()), parityProgramIntValue(big.NewInt(-1)))
	return true
}

func (g *parityProgramGenerator) emitDictReplaceAddGetOp(add bool, byRef bool, kind int) bool {
	root, oldValue, oldRef := parityProgramDictRoot(byRef)
	base := len(g.stack)
	newValue := cell.BeginCell().MustStoreUInt(0x88, 8).EndCell()
	newRef := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	key := uint64(0x12)
	name := "REPLACEGET"
	opcode := uint64(0xF42A)
	mode := cell.DictSetModeReplace
	if add {
		key = 0x21
		name = "ADDGET"
		opcode = 0xF43A
		mode = cell.DictSetModeAdd
	}

	dict := parityProgramRootAsDict(root, 8)
	keyCell := parityProgramDictKeyCellForKind(kind, key, 8)
	var old *cell.Slice
	var err error
	if byRef {
		g.emitPushCell("dict_replace_add_get_ref", newRef)
		old, _, err = dict.LoadValueAndSetBuilderWithMode(keyCell, cell.BeginCell().MustStoreRef(newRef), mode)
	} else {
		g.emitPushSlice("dict_replace_add_get_value", newValue.MustBeginParse())
		old, _, err = dict.LoadValueAndSetBuilderWithMode(keyCell, newValue.ToBuilder(), mode)
	}
	if err != nil {
		panic(err)
	}

	g.emitDictKey("dict_replace_add_get_key", kind, key, 8)
	g.emitPushCell("dict_replace_add_get_root", root)
	g.emitPushInt("dict_replace_add_get_bits", big.NewInt(8))
	g.emit(parityProgramDictValueName(kind, name, byRef), parityProgramRawOp(parityProgramDictValueOpcode(opcode, kind, byRef), 16))

	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramMaybeCellValue(dict.AsCell()))
	if old != nil {
		if byRef {
			g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramCell, cell: oldRef})
		} else {
			g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: oldValue.MustBeginParse()})
		}
		g.stack = append(g.stack, parityProgramBoolValue(mode != cell.DictSetModeAdd))
	} else {
		g.stack = append(g.stack, parityProgramBoolValue(mode == cell.DictSetModeAdd))
	}
	return true
}

func (g *parityProgramGenerator) emitDictDeleteOp(kind int) bool {
	root, _, _ := parityProgramDictRoot(false)
	base := len(g.stack)
	key := uint64(0x12)
	opcode := parityProgramDictScalarOpcode(0xF459, kind)
	g.emitDictKey("dict_del_key", kind, key, 8)
	g.emitPushCell("dict_del_root", root)
	g.emitPushInt("dict_del_bits", big.NewInt(8))
	g.emit(parityProgramDictScalarName(kind, "DEL"), parityProgramRawOp(opcode, 16))

	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramNull}, parityProgramIntValue(big.NewInt(-1)))
	return true
}

func (g *parityProgramGenerator) emitDictGetOptRefOp(kind int) bool {
	root, _, ref := parityProgramDictRoot(true)
	base := len(g.stack)
	key := uint64(0x12)
	opcode := parityProgramDictScalarOpcode(0xF469, kind)
	g.emitDictKey("dict_getoptref_key", kind, key, 8)
	g.emitPushCell("dict_getoptref_root", root)
	g.emitPushInt("dict_getoptref_bits", big.NewInt(8))
	g.emit(parityProgramDictScalarName(kind, "GETOPTREF"), parityProgramRawOp(opcode, 16))

	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramCell, cell: ref})
	return true
}

func (g *parityProgramGenerator) emitDictMinMaxOp(fetchMax bool, byRef bool, kind int) bool {
	root, value, ref := parityProgramDictRoot(byRef)
	base := len(g.stack)
	g.emitPushCell("dict_minmax_root", root)
	g.emitPushInt("dict_minmax_bits", big.NewInt(8))
	name := "MIN"
	opcode := uint64(0xF482)
	key := uint64(0x12)
	if fetchMax {
		name = "MAX"
		opcode = 0xF48A
	}
	opcode = parityProgramDictValueOpcode(opcode, kind, byRef)
	g.emit(parityProgramDictValueName(kind, name, byRef), parityProgramRawOp(opcode, 16))

	g.stack = g.stack[:base]
	if byRef {
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramCell, cell: ref})
	} else {
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: value.MustBeginParse()})
	}
	g.stack = append(g.stack, parityProgramDictKeyValue(kind, key, 8), parityProgramIntValue(big.NewInt(-1)))
	return true
}

func (g *parityProgramGenerator) emitDictRemMinMaxOp(fetchMax bool, byRef bool, kind int) bool {
	root, value, ref := parityProgramDictRoot(byRef)
	base := len(g.stack)
	g.emitPushCell("dict_rem_minmax_root", root)
	g.emitPushInt("dict_rem_minmax_bits", big.NewInt(8))
	name := "REMMIN"
	opcode := uint64(0xF492)
	key := uint64(0x12)
	if fetchMax {
		name = "REMMAX"
		opcode = 0xF49A
	}
	opcode = parityProgramDictValueOpcode(opcode, kind, byRef)
	g.emit(parityProgramDictValueName(kind, name, byRef), parityProgramRawOp(opcode, 16))

	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramNull})
	if byRef {
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramCell, cell: ref})
	} else {
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: value.MustBeginParse()})
	}
	g.stack = append(g.stack, parityProgramDictKeyValue(kind, key, 8), parityProgramIntValue(big.NewInt(-1)))
	return true
}

func (g *parityProgramGenerator) emitDictNearOp() bool {
	root, value, _ := parityProgramDictRoot(false)
	base := len(g.stack)
	key := uint64(0x12)
	g.emitPushSlice("dict_near_key", parityProgramKeySlice(key, 8))
	g.emitPushCell("dict_near_root", root)
	g.emitPushInt("dict_near_bits", big.NewInt(8))
	g.emit("DICTGETNEXTEQ", parityProgramRawOp(0xF475, 16))

	g.stack = g.stack[:base]
	g.stack = append(g.stack,
		parityProgramStackValue{kind: parityProgramSlice, slice: value.MustBeginParse()},
		parityProgramStackValue{kind: parityProgramSlice, slice: parityProgramKeySlice(key, 8)},
		parityProgramIntValue(big.NewInt(-1)),
	)
	return true
}

func (g *parityProgramGenerator) emitDictBuilderSetOp(mode cell.DictSetMode, kind int) bool {
	base := len(g.stack)
	key := uint64(0x12)
	root, _, _ := parityProgramDictRoot(false)
	if mode == cell.DictSetModeAdd {
		key = 0x21
	}
	value := cell.BeginCell().MustStoreUInt(0x66, 8)
	dict := parityProgramRootAsDict(root, 8)
	changed, err := dict.SetBuilderWithMode(parityProgramDictKeyCellForKind(kind, key, 8), value.Copy(), mode)
	if err != nil {
		panic(err)
	}

	g.emitPushBuilder("dict_setb_value", value)
	g.emitDictKey("dict_setb_key", kind, key, 8)
	g.emitPushCell("dict_setb_root", root)
	g.emitPushInt("dict_setb_bits", big.NewInt(8))
	name, opcode := parityProgramDictBuilderSetNameOpcode(mode, kind)
	g.emit(name, parityProgramRawOp(opcode, 16))

	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramMaybeCellValue(dict.AsCell()))
	if mode != cell.DictSetModeSet {
		g.stack = append(g.stack, parityProgramBoolValue(changed))
	}
	return true
}

func (g *parityProgramGenerator) emitDictBuilderSetGetOp(mode cell.DictSetMode, kind int) bool {
	base := len(g.stack)
	key := uint64(0x12)
	root, oldValue, _ := parityProgramDictRoot(false)
	if mode == cell.DictSetModeAdd {
		key = 0x21
	}
	value := cell.BeginCell().MustStoreUInt(0x66, 8)
	dict := parityProgramRootAsDict(root, 8)
	old, _, err := dict.LoadValueAndSetBuilderWithMode(parityProgramDictKeyCellForKind(kind, key, 8), value.Copy(), mode)
	if err != nil {
		panic(err)
	}

	g.emitPushBuilder("dict_setgetb_value", value)
	g.emitDictKey("dict_setgetb_key", kind, key, 8)
	g.emitPushCell("dict_setgetb_root", root)
	g.emitPushInt("dict_setgetb_bits", big.NewInt(8))
	name, opcode := parityProgramDictBuilderSetGetNameOpcode(mode, kind)
	g.emit(name, parityProgramRawOp(opcode, 16))

	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramMaybeCellValue(dict.AsCell()))
	if old != nil {
		g.stack = append(g.stack,
			parityProgramStackValue{kind: parityProgramSlice, slice: oldValue.MustBeginParse()},
			parityProgramBoolValue(mode != cell.DictSetModeAdd),
		)
	} else {
		g.stack = append(g.stack, parityProgramBoolValue(mode == cell.DictSetModeAdd))
	}
	return true
}

func (g *parityProgramGenerator) emitDictDeleteGetOp(byRef bool, kind int) bool {
	root, value, ref := parityProgramDictRoot(byRef)
	base := len(g.stack)
	key := uint64(0x12)

	g.emitDictKey("dict_delget_key", kind, key, 8)
	g.emitPushCell("dict_delget_root", root)
	g.emitPushInt("dict_delget_bits", big.NewInt(8))
	g.emit(parityProgramDictValueName(kind, "DELGET", byRef), parityProgramRawOp(parityProgramDictValueOpcode(0xF462, kind, byRef), 16))

	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramNull})
	if byRef {
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramCell, cell: ref})
	} else {
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: value.MustBeginParse()})
	}
	g.stack = append(g.stack, parityProgramBoolValue(true))
	return true
}

func (g *parityProgramGenerator) emitDictSetGetOptRefOp(del bool, kind int) bool {
	root, _, oldRef := parityProgramDictRoot(true)
	base := len(g.stack)
	key := uint64(0x12)
	newRef := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	dict := cell.NewDict(8)
	if !del {
		if _, err := dict.SetBuilderWithMode(parityProgramDictKeyCellForKind(kind, key, 8), cell.BeginCell().MustStoreRef(newRef), cell.DictSetModeSet); err != nil {
			panic(err)
		}
	}

	if del {
		g.emitPushNull("dict_setgetoptref_nil")
	} else {
		g.emitPushCell("dict_setgetoptref_new", newRef)
	}
	g.emitDictKey("dict_setgetoptref_key", kind, key, 8)
	g.emitPushCell("dict_setgetoptref_root", root)
	g.emitPushInt("dict_setgetoptref_bits", big.NewInt(8))
	g.emit(parityProgramDictScalarName(kind, "SETGETOPTREF"), parityProgramRawOp(parityProgramDictScalarOpcode(0xF46D, kind), 16))

	g.stack = g.stack[:base]
	g.stack = append(g.stack,
		parityProgramMaybeCellValue(dict.AsCell()),
		parityProgramStackValue{kind: parityProgramCell, cell: oldRef},
	)
	return true
}

func (g *parityProgramGenerator) emitDictNearExtraOp(mode int) bool {
	base := len(g.stack)
	variants := []struct {
		kind      int
		fetchNext bool
		allowEq   bool
	}{
		{kind: 0, fetchNext: true},
		{kind: 0, fetchNext: true, allowEq: true},
		{kind: 0},
		{kind: 0, allowEq: true},
		{kind: 1, fetchNext: true},
		{kind: 1, fetchNext: true, allowEq: true},
		{kind: 1},
		{kind: 1, allowEq: true},
		{kind: 2, fetchNext: true},
		{kind: 2, fetchNext: true, allowEq: true},
		{kind: 2},
		{kind: 2, allowEq: true},
	}
	v := variants[mode%len(variants)]
	inputKey := int64(0x20)
	wantKey := int64(0x20)
	var root *cell.Cell
	var value *cell.Cell
	if v.kind == 1 {
		values := map[int64]*cell.Cell{}
		root, values = parityProgramSignedDictRoot()
		if v.fetchNext {
			inputKey = -2
			wantKey = 3
			if v.allowEq {
				wantKey = -2
			}
		} else {
			inputKey = 3
			wantKey = -2
			if v.allowEq {
				wantKey = 3
			}
		}
		value = values[wantKey]
		g.emitPushInt("dict_near_key", big.NewInt(inputKey))
	} else {
		values := map[uint64]*cell.Cell{}
		root, values = parityProgramMultiDictRoot()
		if v.fetchNext && !v.allowEq {
			wantKey = 0x30
		}
		if !v.fetchNext && !v.allowEq {
			wantKey = 0x10
		}
		value = values[uint64(wantKey)]
		if v.kind == 0 {
			g.emitPushSlice("dict_near_key", parityProgramKeySlice(uint64(inputKey), 8))
		} else {
			g.emitPushInt("dict_near_key", big.NewInt(inputKey))
		}
	}

	g.emitPushCell("dict_near_root", root)
	g.emitPushInt("dict_near_bits", big.NewInt(8))
	g.emit(parityProgramDictNearName(v.kind, v.fetchNext, v.allowEq), parityProgramRawOp(0xF474+uint64(mode%len(variants)), 16))

	wantKeyValue := parityProgramIntValue(big.NewInt(wantKey))
	if v.kind == 0 {
		wantKeyValue = parityProgramStackValue{kind: parityProgramSlice, slice: parityProgramKeySlice(uint64(wantKey), 8)}
	}
	g.stack = g.stack[:base]
	g.stack = append(g.stack,
		parityProgramStackValue{kind: parityProgramSlice, slice: value.MustBeginParse()},
		wantKeyValue,
		parityProgramBoolValue(true),
	)
	return true
}

func (g *parityProgramGenerator) emitSubdictOp(removePrefix bool, kind int) bool {
	root := parityProgramSubdictRoot()
	base := len(g.stack)
	prefixBits := uint(3)
	prefix := uint64(0b101)
	dict := parityProgramRootAsDict(root, 8)
	ok, err := dict.CutPrefixSubdict(parityProgramDictKeyCellForKind(kind, prefix, prefixBits), removePrefix)
	if err != nil || !ok {
		panic("invalid subdict fixture")
	}

	g.emitDictKey("subdict_prefix", kind, prefix, prefixBits)
	g.emitPushInt("subdict_prefix_bits", big.NewInt(int64(prefixBits)))
	g.emitPushCell("subdict_root", root)
	g.emitPushInt("subdict_key_bits", big.NewInt(8))
	suffix := "SUBDICTGET"
	opcode := uint64(0xF4B1)
	if removePrefix {
		suffix = "SUBDICTRPGET"
		opcode = 0xF4B5
	}
	g.emit(parityProgramDictScalarName(kind, suffix), parityProgramRawOp(parityProgramDictScalarOpcode(opcode, kind), 16))

	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramMaybeCellValue(dict.AsCell()))
	return true
}

func (g *parityProgramGenerator) emitPrefixDictGetQOp() bool {
	root, value := parityProgramPrefixDictRoot()
	base := len(g.stack)
	input := parityProgramKeySlice(0b1011, 4)
	g.emitPushSlice("pfx_input", input)
	g.emitPushCell("pfx_root", root)
	g.emitPushInt("pfx_bits", big.NewInt(4))
	g.emit("PFXDICTGETQ", parityProgramRawOp(0xF4A8, 16))

	g.stack = g.stack[:base]
	g.stack = append(g.stack,
		parityProgramStackValue{kind: parityProgramSlice, slice: parityProgramKeySlice(0b10, 2)},
		parityProgramStackValue{kind: parityProgramSlice, slice: value.MustBeginParse()},
		parityProgramStackValue{kind: parityProgramSlice, slice: parityProgramKeySlice(0b11, 2)},
		parityProgramIntValue(big.NewInt(-1)),
	)
	return true
}

func (g *parityProgramGenerator) emitPrefixDictGetOp(quiet bool) bool {
	root, value := parityProgramPrefixDictRoot()
	base := len(g.stack)
	input := parityProgramKeySlice(0b1011, 4)
	name := "PFXDICTGET"
	opcode := uint64(0xF4A9)
	if quiet {
		name = "PFXDICTGETQ"
		opcode = 0xF4A8
	}

	g.emitPushSlice(strings.ToLower(name)+"_input", input)
	g.emitPushCell(strings.ToLower(name)+"_root", root)
	g.emitPushInt(strings.ToLower(name)+"_bits", big.NewInt(4))
	g.emit(name, parityProgramRawOp(opcode, 16))

	g.stack = g.stack[:base]
	g.stack = append(g.stack,
		parityProgramStackValue{kind: parityProgramSlice, slice: parityProgramKeySlice(0b10, 2)},
		parityProgramStackValue{kind: parityProgramSlice, slice: value.MustBeginParse()},
		parityProgramStackValue{kind: parityProgramSlice, slice: parityProgramKeySlice(0b11, 2)},
	)
	if quiet {
		g.stack = append(g.stack, parityProgramBoolValue(true))
	}
	return true
}

func (g *parityProgramGenerator) emitPrefixDictReplaceAddOp(add bool) bool {
	root, oldValue := parityProgramPrefixDictRoot()
	base := len(g.stack)
	key := uint64(0b10)
	value := cell.BeginCell().MustStoreUInt(0xE, 4).EndCell()
	name := "PFXDICTREPLACE"
	opcode := uint64(0xF471)
	if add {
		key = 0b11
		name = "PFXDICTADD"
		opcode = 0xF472
	}

	dict := cell.NewPrefixDict(4)
	if _, err := dict.SetWithMode(parityProgramKeyCell(0b10, 2), oldValue, cell.DictSetModeSet); err != nil {
		panic(err)
	}
	if _, err := dict.SetWithMode(parityProgramKeyCell(key, 2), value, cell.DictSetModeSet); err != nil {
		panic(err)
	}

	g.emitPushSlice(strings.ToLower(name)+"_value", value.MustBeginParse())
	g.emitPushSlice(strings.ToLower(name)+"_key", parityProgramKeySlice(key, 2))
	g.emitPushCell(strings.ToLower(name)+"_root", root)
	g.emitPushInt(strings.ToLower(name)+"_bits", big.NewInt(4))
	g.emit(name, parityProgramRawOp(opcode, 16))

	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramMaybeCellValue(dict.AsCell()), parityProgramBoolValue(true))
	return true
}

func (g *parityProgramGenerator) emitPrefixDictSetDelOp(del bool) bool {
	root, value := parityProgramPrefixDictRoot()
	base := len(g.stack)
	key := parityProgramKeySlice(0b10, 2)
	if del {
		g.emitPushSlice("pfx_del_key", key)
		g.emitPushCell("pfx_del_root", root)
		g.emitPushInt("pfx_del_bits", big.NewInt(4))
		g.emit("PFXDICTDEL", parityProgramRawOp(0xF473, 16))
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramNull}, parityProgramIntValue(big.NewInt(-1)))
		return true
	}

	dict := cell.NewPrefixDict(4)
	if _, err := dict.SetWithMode(parityProgramKeyCell(0b10, 2), value, cell.DictSetModeSet); err != nil {
		panic(err)
	}
	g.emitPushSlice("pfx_set_value", value.MustBeginParse())
	g.emitPushSlice("pfx_set_key", key)
	g.emitPushNull("pfx_set_root")
	g.emitPushInt("pfx_set_bits", big.NewInt(4))
	g.emit("PFXDICTSET", parityProgramRawOp(0xF470, 16))
	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramMaybeCellValue(dict.AsCell()), parityProgramIntValue(big.NewInt(-1)))
	return true
}

func (g *parityProgramGenerator) emitLoadRefOp() bool {
	if len(g.stack) == 0 || g.stack[len(g.stack)-1].kind != parityProgramSlice {
		return false
	}
	sl := g.stack[len(g.stack)-1].slice.Copy()
	if sl.RefsNum() == 0 {
		return false
	}
	ref, err := sl.LoadRefCell()
	if err != nil {
		return false
	}
	g.emit("LDREF", cellsliceop.LDREF().Serialize())
	g.stack[len(g.stack)-1] = parityProgramStackValue{kind: parityProgramCell, cell: ref}
	g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: sl})
	return true
}

func (g *parityProgramGenerator) emitLoadIntOp(signed bool) bool {
	if len(g.stack) == 0 || g.stack[len(g.stack)-1].kind != parityProgramSlice {
		return false
	}
	sl := g.stack[len(g.stack)-1].slice.Copy()
	if sl.BitsLeft() < 8 {
		return false
	}
	var val *big.Int
	var err error
	name := "LDU8"
	op := cellsliceop.LDU(8).Serialize()
	if signed {
		name = "LDI8"
		op = cellsliceop.LDI(8).Serialize()
		val, err = sl.LoadBigInt(8)
	} else {
		val, err = sl.LoadBigUInt(8)
	}
	if err != nil {
		return false
	}
	g.emit(name, op)
	g.stack[len(g.stack)-1] = parityProgramIntValue(val)
	g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: sl})
	return true
}

func (g *parityProgramGenerator) emitStoreIntOp(signed bool) bool {
	if len(g.stack) < 2 || g.stack[len(g.stack)-1].kind != parityProgramBuilder || g.stack[len(g.stack)-2].kind != parityProgramInt {
		return false
	}
	val := g.stack[len(g.stack)-2].int
	b := g.stack[len(g.stack)-1].builder.Copy()
	if !b.CanExtendBy(8, 0) {
		return false
	}

	var err error
	name := "STU8"
	op := cellsliceop.STU(8).Serialize()
	if signed {
		if val.Cmp(big.NewInt(-128)) < 0 || val.Cmp(big.NewInt(127)) > 0 {
			return false
		}
		name = "STI8"
		op = cellsliceop.STI(8).Serialize()
		err = b.StoreBigInt(val, 8)
	} else {
		if val.Sign() < 0 || val.Cmp(big.NewInt(255)) > 0 {
			return false
		}
		err = b.StoreBigUInt(val, 8)
	}
	if err != nil {
		return false
	}
	g.emit(name, op)
	g.stack = g.stack[:len(g.stack)-2]
	g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: b})
	return true
}

func (g *parityProgramGenerator) emitStoreRefOp() bool {
	if len(g.stack) < 2 || g.stack[len(g.stack)-1].kind != parityProgramBuilder || g.stack[len(g.stack)-2].kind != parityProgramCell {
		return false
	}
	b := g.stack[len(g.stack)-1].builder.Copy()
	if !b.CanExtendBy(0, 1) {
		return false
	}
	if err := b.StoreRefUncheckedDepth(g.stack[len(g.stack)-2].cell); err != nil {
		return false
	}
	g.emit("STREF", cellsliceop.STREF().Serialize())
	g.stack = g.stack[:len(g.stack)-2]
	g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: b})
	return true
}

func (g *parityProgramGenerator) emitStoreSliceOp() bool {
	if len(g.stack) < 2 || g.stack[len(g.stack)-1].kind != parityProgramBuilder || g.stack[len(g.stack)-2].kind != parityProgramSlice {
		return false
	}
	b := g.stack[len(g.stack)-1].builder.Copy()
	if err := b.StoreBuilderUncheckedDepth(g.stack[len(g.stack)-2].slice.ToBuilder()); err != nil {
		return false
	}
	g.emit("STSLICE", cellsliceop.STSLICE().Serialize())
	g.stack = g.stack[:len(g.stack)-2]
	g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: b})
	return true
}

func (g *parityProgramGenerator) emitBuilderMetaOp(name string, op *cell.Builder, fn func(*cell.Builder) int64) bool {
	if len(g.stack) == 0 || g.stack[len(g.stack)-1].kind != parityProgramBuilder {
		return false
	}
	val := fn(g.stack[len(g.stack)-1].builder)
	g.emit(name, op)
	g.stack[len(g.stack)-1] = parityProgramIntValue(big.NewInt(val))
	return true
}

func (g *parityProgramGenerator) emitPushValueOp() bool {
	switch g.r.Intn(8) {
	case 0:
		g.emit("PUSHNULL", tupleop.PUSHNULL().Serialize())
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramNull})
	case 1:
		g.emit("PUSHNAN", mathop.PUSHNAN().Serialize())
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramNaN})
	case 2:
		value := uint8(g.r.Intn(12))
		g.emit(fmt.Sprintf("PUSHPOW2(%d)", value+1), mathop.PUSHPOW2(value).Serialize())
		g.stack = append(g.stack, parityProgramIntValue(new(big.Int).Lsh(big.NewInt(1), uint(value+1))))
	case 3:
		value := uint8(g.r.Intn(12))
		g.emit(fmt.Sprintf("PUSHPOW2DEC(%d)", value+1), mathop.PUSHPOW2DEC(value).Serialize())
		v := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(value+1)), big.NewInt(1))
		g.stack = append(g.stack, parityProgramIntValue(v))
	case 4:
		value := uint8(g.r.Intn(12))
		g.emit(fmt.Sprintf("PUSHNEGPOW2(%d)", value+1), mathop.PUSHNEGPOW2(value).Serialize())
		g.stack = append(g.stack, parityProgramIntValue(new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), uint(value+1)))))
	default:
		v := g.smallInt()
		g.emit(fmt.Sprintf("PUSHINT(%s)", v.String()), stackop.PUSHINT(v).Serialize())
		g.stack = append(g.stack, parityProgramIntValue(v))
	}
	return true
}

func (g *parityProgramGenerator) emitStackOp() bool {
	switch g.r.Intn(24) {
	case 0:
		g.emit("NOP", stackop.NOP().Serialize())
		return true
	case 1:
		if len(g.stack) < 1 {
			return false
		}
		g.emit("DROP", stackop.DROP().Serialize())
		g.stack = g.stack[:len(g.stack)-1]
	case 2:
		if len(g.stack) < 1 {
			return false
		}
		g.emit("DUP", stackop.DUP().Serialize())
		g.stack = append(g.stack, parityProgramCloneValue(g.stack[len(g.stack)-1]))
	case 3:
		if len(g.stack) < 2 {
			return false
		}
		g.emit("OVER", stackop.OVER().Serialize())
		g.stack = append(g.stack, parityProgramCloneValue(g.stack[len(g.stack)-2]))
	case 4:
		if len(g.stack) < 2 {
			return false
		}
		g.emit("SWAP", stackop.SWAP().Serialize())
		g.swapDepth(0, 1)
	case 5:
		if len(g.stack) < 3 {
			return false
		}
		g.emit("ROT", stackop.ROT().Serialize())
		n := len(g.stack)
		a, b, c := g.stack[n-3], g.stack[n-2], g.stack[n-1]
		g.stack[n-3], g.stack[n-2], g.stack[n-1] = b, c, a
	case 6:
		if len(g.stack) < 3 {
			return false
		}
		g.emit("ROTREV", stackop.ROTREV().Serialize())
		n := len(g.stack)
		a, b, c := g.stack[n-3], g.stack[n-2], g.stack[n-1]
		g.stack[n-3], g.stack[n-2], g.stack[n-1] = c, a, b
	case 7:
		if len(g.stack) < 2 {
			return false
		}
		g.emit("2DUP", stackop.DUP2().Serialize())
		n := len(g.stack)
		g.stack = append(g.stack, parityProgramCloneValue(g.stack[n-2]), parityProgramCloneValue(g.stack[n-1]))
	case 8:
		if len(g.stack) < 2 {
			return false
		}
		g.emit("TUCK", stackop.TUCK().Serialize())
		n := len(g.stack)
		a, b := g.stack[n-2], g.stack[n-1]
		g.stack[n-2], g.stack[n-1] = b, a
		g.stack = append(g.stack, parityProgramCloneValue(b))
	case 9:
		if len(g.stack) < 1 {
			return false
		}
		idx := g.randomStackIndex(16)
		g.emit(fmt.Sprintf("PUSH(s%d)", idx), stackop.PUSH(uint8(idx)).Serialize())
		g.stack = append(g.stack, parityProgramCloneValue(g.stack[len(g.stack)-1-idx]))
	case 10:
		if len(g.stack) < 1 {
			return false
		}
		idx := g.randomStackIndex(16)
		g.emit(fmt.Sprintf("POP(s%d)", idx), stackop.POP(uint8(idx)).Serialize())
		g.stack[len(g.stack)-1-idx] = parityProgramCloneValue(g.stack[len(g.stack)-1])
		g.stack = g.stack[:len(g.stack)-1]
	case 11:
		if len(g.stack) < 2 {
			return false
		}
		a := g.randomStackIndex(16)
		b := g.randomStackIndex(16)
		if a == b {
			return false
		}
		g.emit(fmt.Sprintf("XCHG(s%d,s%d)", a, b), stackop.XCHG(uint8(a), uint8(b)).Serialize())
		g.swapDepth(a, b)
	case 12:
		if !g.emitReverseOp() {
			return false
		}
	default:
		return g.emitStackExtraOp()
	}
	return true
}

func (g *parityProgramGenerator) emitStackExtraOp() bool {
	switch g.r.Intn(30) {
	case 0:
		g.emit("DEPTH", stackop.DEPTH().Serialize())
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(len(g.stack)))))
	case 1:
		base := len(g.stack)
		count := base
		if count > 8 {
			count = g.r.Intn(9)
		}
		g.emitPushInt("chkdepth_count", big.NewInt(int64(count)))
		g.emit("CHKDEPTH", stackop.CHKDEPTH().Serialize())
		g.stack = g.stack[:base]
	case 2:
		if len(g.stack) < 2 {
			return false
		}
		g.emit("NIP", stackop.NIP().Serialize())
		top := parityProgramCloneValue(g.stack[len(g.stack)-1])
		g.stack[len(g.stack)-2] = top
		g.stack = g.stack[:len(g.stack)-1]
	case 3:
		if len(g.stack) < 2 {
			return false
		}
		g.emit("2DROP", stackop.DROP2().Serialize())
		g.stack = g.stack[:len(g.stack)-2]
	case 4:
		if len(g.stack) < 4 {
			return false
		}
		g.emit("2OVER", stackop.OVER2().Serialize())
		n := len(g.stack)
		g.stack = append(g.stack, parityProgramCloneValue(g.stack[n-4]), parityProgramCloneValue(g.stack[n-3]))
	case 5:
		if len(g.stack) < 4 {
			return false
		}
		g.emit("2SWAP", stackop.SWAP2().Serialize())
		n := len(g.stack)
		a, b, c, d := g.stack[n-4], g.stack[n-3], g.stack[n-2], g.stack[n-1]
		g.stack[n-4], g.stack[n-3], g.stack[n-2], g.stack[n-1] = c, d, a, b
	case 6:
		if len(g.stack) == 0 {
			return false
		}
		base := len(g.stack)
		idx := g.randomStackIndex(8)
		val := parityProgramCloneValue(g.stack[base-1-idx])
		g.emitPushInt("pick_idx", big.NewInt(int64(idx)))
		g.emit("PICK", stackop.PICK().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, val)
	case 7:
		if len(g.stack) == 0 {
			return false
		}
		base := len(g.stack)
		idx := g.randomStackIndex(8)
		g.emitPushInt("roll_idx", big.NewInt(int64(idx)))
		g.emit("ROLL", stackop.ROLL().Serialize())
		g.stack = g.stack[:base]
		g.moveDepthToTop(idx)
	case 8:
		if len(g.stack) == 0 {
			return false
		}
		base := len(g.stack)
		idx := g.randomStackIndex(8)
		g.emitPushInt("rollrev_idx", big.NewInt(int64(idx)))
		g.emit("ROLLREV", stackop.ROLLREV().Serialize())
		g.stack = g.stack[:base]
		g.moveTopToDepth(idx)
	case 9:
		if len(g.stack) == 0 {
			return false
		}
		base := len(g.stack)
		count := g.r.Intn(base + 1)
		if count > 4 {
			count = g.r.Intn(5)
		}
		g.emitPushInt("dropx_count", big.NewInt(int64(count)))
		g.emit("DROPX", stackop.DROPX().Serialize())
		g.stack = g.stack[:base-count]
	case 10:
		if len(g.stack) == 0 {
			return false
		}
		base := len(g.stack)
		idx := g.randomStackIndex(8)
		g.emitPushInt("xchgx_idx", big.NewInt(int64(idx)))
		g.emit("XCHGX", stackop.XCHGX().Serialize())
		g.stack = g.stack[:base]
		g.swapDepth(0, idx)
	case 11:
		if len(g.stack) < 2 {
			return false
		}
		maxX := len(g.stack)
		if maxX > 5 {
			maxX = 5
		}
		y := g.r.Intn(maxX - 1)
		x := 1 + g.r.Intn(maxX-y)
		if x+y > len(g.stack) {
			return false
		}
		base := len(g.stack)
		g.emitPushInt("revx_x", big.NewInt(int64(x)))
		g.emitPushInt("revx_y", big.NewInt(int64(y)))
		g.emit("REVX", stackop.REVX().Serialize())
		g.stack = g.stack[:base]
		g.reverseDepthRange(x+y, y)
	case 12:
		base := len(g.stack)
		if base > maxSmallIndexForParityProgram {
			return false
		}
		g.emitPushInt("onlytopx_count", big.NewInt(int64(base)))
		g.emit("ONLYTOPX", stackop.ONLYTOPX().Serialize())
		g.stack = g.stack[:base]
	case 13:
		base := len(g.stack)
		if base > maxSmallIndexForParityProgram {
			return false
		}
		g.emitPushInt("onlyx_count", big.NewInt(int64(base)))
		g.emit("ONLYX", stackop.ONLYX().Serialize())
		g.stack = g.stack[:base]
	case 14:
		if len(g.stack) == 0 {
			return false
		}
		max := len(g.stack)
		if max > 4 {
			max = 4
		}
		count := g.r.Intn(max + 1)
		g.emit(fmt.Sprintf("BLKDROP(%d)", count), stackop.BLKDROP(uint8(count)).Serialize())
		g.stack = g.stack[:len(g.stack)-count]
	case 15:
		if len(g.stack) < 2 {
			return false
		}
		i := 1 + g.r.Intn(2)
		j := g.r.Intn(3)
		if i+j > len(g.stack) {
			return false
		}
		g.emit(fmt.Sprintf("BLKDROP2(%d,%d)", i, j), stackop.BLKDROP2(uint8(i), uint8(j)).Serialize())
		g.dropMany(i, j)
	case 16:
		if len(g.stack) == 0 {
			return false
		}
		i := 1 + g.r.Intn(2)
		j := g.randomStackIndex(4)
		g.emit(fmt.Sprintf("BLKPUSH(%d,%d)", i, j), stackop.BLKPUSH(uint8(i), uint8(j)).Serialize())
		for x := 0; x < i; x++ {
			g.stack = append(g.stack, parityProgramCloneValue(g.stack[len(g.stack)-1-j]))
		}
	case 17:
		if len(g.stack) < 2 {
			return false
		}
		i := 1 + g.r.Intn(2)
		j := 1 + g.r.Intn(2)
		if i+j > len(g.stack) {
			return false
		}
		g.emit(fmt.Sprintf("BLKSWAP(%d,%d)", i, j), stackop.BLKSWAP(uint8(i), uint8(j)).Serialize())
		g.blockSwap(i, j)
	case 18:
		if len(g.stack) < 2 {
			return false
		}
		i := 1 + g.r.Intn(2)
		j := 1 + g.r.Intn(2)
		if i+j > len(g.stack) {
			return false
		}
		base := len(g.stack)
		g.emitPushInt("blkswx_x", big.NewInt(int64(i)))
		g.emitPushInt("blkswx_y", big.NewInt(int64(j)))
		g.emit("BLKSWX", stackop.BLKSWX().Serialize())
		g.stack = g.stack[:base]
		g.blockSwap(i, j)
	case 19:
		if len(g.stack) == 0 {
			return false
		}
		i := g.randomStackIndex(8)
		j := g.randomStackIndex(8)
		first := parityProgramCloneValue(g.stack[len(g.stack)-1-i])
		second := parityProgramCloneValue(g.stack[len(g.stack)-1-j])
		g.emit(fmt.Sprintf("PUSH2(%d,%d)", i, j), stackop.PUSH2(uint8(i), uint8(j)).Serialize())
		g.stack = append(g.stack, first, second)
	case 20:
		if len(g.stack) == 0 {
			return false
		}
		i := g.randomStackIndex(8)
		g.emit(fmt.Sprintf("XCHG0(%d)", i), stackop.XCHG0(uint8(i)).Serialize())
		g.swapDepth(0, i)
	case 21:
		if len(g.stack) == 0 {
			return false
		}
		i := g.randomStackIndex(32)
		g.emit(fmt.Sprintf("XCHG0L(%d)", i), stackop.XCHG0L(uint8(i)).Serialize())
		g.swapDepth(0, i)
	case 22:
		if len(g.stack) < 2 {
			return false
		}
		i := g.randomStackIndex(8)
		j := g.randomStackIndex(8)
		g.emit(fmt.Sprintf("XCHG2(%d,%d)", i, j), stackop.XCHG2(uint8(i), uint8(j)).Serialize())
		g.swapDepth(1, i)
		g.swapDepth(0, j)
	case 23:
		if len(g.stack) < 3 {
			return false
		}
		i := g.randomStackIndex(8)
		j := g.randomStackIndex(8)
		k := g.randomStackIndex(8)
		g.emit(fmt.Sprintf("XCHG3(%d,%d,%d)", i, j, k), stackop.XCHG3(uint8(i), uint8(j), uint8(k)).Serialize())
		g.swapDepth(2, i)
		g.swapDepth(1, j)
		g.swapDepth(0, k)
	case 24:
		if len(g.stack) == 0 {
			return false
		}
		i := g.randomStackIndex(256)
		g.emit(fmt.Sprintf("PUSHL(%d)", i), stackop.PUSHL(uint8(i)).Serialize())
		g.stack = append(g.stack, parityProgramCloneValue(g.stack[len(g.stack)-1-i]))
	case 25:
		if len(g.stack) == 0 {
			return false
		}
		i := g.randomStackIndex(256)
		g.emit(fmt.Sprintf("POPL(%d)", i), stackop.POPL(uint8(i)).Serialize())
		g.stack[len(g.stack)-1-i] = parityProgramCloneValue(g.stack[len(g.stack)-1])
		g.stack = g.stack[:len(g.stack)-1]
	case 26:
		src := cell.BeginCell().
			MustStoreUInt(0xA5, 8).
			MustStoreRef(cell.BeginCell().MustStoreUInt(0x5A, 8).EndCell()).
			ToSlice()
		g.emit("PUSHSLICEINLINE", stackop.PUSHSLICEINLINE(src).Serialize())
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: src})
	case 27:
		root, _, _ := parityProgramDictRoot(false)
		g.emit("DICTPUSHCONST", stackop.DICTPUSHCONST(root).Serialize())
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramCell, cell: root}, parityProgramIntValue(big.NewInt(0)))
	case 28:
		return g.emitDebugOp(g.r.Intn(7))
	default:
		return g.emitStackExtraPushExchangeOp()
	}
	return true
}

func (g *parityProgramGenerator) emitDebugOp(mode int) bool {
	switch mode {
	case 0:
		g.emit("DUMPSTK", stackop.DUMPSTK().Serialize())
	case 1:
		g.emit("DUMP(0)", stackop.DUMP(0).Serialize())
	case 2:
		g.emit("DUMP(15)", stackop.DUMP(15).Serialize())
	case 3:
		g.emit("DEBUG(42)", stackop.DEBUG(42).Serialize())
	case 4:
		g.emitPushSlice("strdump_slice", cell.BeginCell().MustStoreSlice([]byte("trace"), 40).ToSlice())
		g.emit("STRDUMP(slice)", stackop.STRDUMP().Serialize())
	case 5:
		g.emitPushInt("strdump_int", big.NewInt(7))
		g.emit("STRDUMP(int)", stackop.STRDUMP().Serialize())
	default:
		g.emit("DEBUGSTR", stackop.DEBUGSTR([]byte("parity-fuzz")).Serialize())
	}
	return true
}

func (g *parityProgramGenerator) emitStackExtraPushExchangeOp() bool {
	pushDepth := func(depth int) {
		g.stack = append(g.stack, parityProgramCloneValue(g.stack[len(g.stack)-1-depth]))
	}

	switch g.r.Intn(9) {
	case 0:
		if len(g.stack) == 0 {
			return false
		}
		i := g.randomStackIndex(8)
		j := g.randomStackIndex(8)
		g.emit(fmt.Sprintf("XCPU(%d,%d)", i, j), stackop.XCPU(uint8(i), uint8(j)).Serialize())
		g.swapDepth(0, i)
		g.stack = append(g.stack, parityProgramCloneValue(g.stack[len(g.stack)-1-j]))
	case 1:
		if len(g.stack) == 0 {
			return false
		}
		i := g.randomStackIndex(8)
		j := g.randomStackIndex(8)
		g.emit(fmt.Sprintf("PUXC(%d,%d)", i, j), stackop.PUXC(uint8(i), uint8(j)).Serialize())
		val := parityProgramCloneValue(g.stack[len(g.stack)-1-i])
		g.stack = append(g.stack, val)
		g.swapDepth(0, 1)
		g.swapDepth(0, j)
	case 2:
		if len(g.stack) == 0 {
			return false
		}
		i := g.randomStackIndex(8)
		j := g.randomStackIndex(8)
		k := g.randomStackIndex(8)
		first := parityProgramCloneValue(g.stack[len(g.stack)-1-i])
		second := parityProgramCloneValue(g.stack[len(g.stack)-1-j])
		third := parityProgramCloneValue(g.stack[len(g.stack)-1-k])
		g.emit(fmt.Sprintf("PUSH3(%d,%d,%d)", i, j, k), stackop.PUSH3(uint8(i), uint8(j), uint8(k)).Serialize())
		g.stack = append(g.stack, first, second, third)
	case 3:
		if len(g.stack) < 2 {
			return false
		}
		i := g.randomStackIndex(8)
		j := g.randomStackIndex(8)
		k := g.randomStackIndex(8)
		g.emit(fmt.Sprintf("XC2PU(%d,%d,%d)", i, j, k), stackop.XC2PU(uint8(i), uint8(j), uint8(k)).Serialize())
		g.swapDepth(1, i)
		g.swapDepth(0, j)
		g.stack = append(g.stack, parityProgramCloneValue(g.stack[len(g.stack)-1-k]))
	case 4:
		if len(g.stack) < 5 {
			return false
		}
		g.emit("XCPUXC(3,4,2)", stackop.XCPUXC(3, 4, 2).Serialize())
		g.swapDepth(1, 3)
		pushDepth(4)
		g.swapDepth(0, 1)
		g.swapDepth(0, 2)
	case 5:
		if len(g.stack) < 5 {
			return false
		}
		g.emit("XCPU2(4,3,2)", stackop.XCPU2(4, 3, 2).Serialize())
		g.swapDepth(0, 4)
		pushDepth(3)
		pushDepth(3)
	case 6:
		if len(g.stack) < 5 {
			return false
		}
		g.emit("PUXC2(4,3,2)", stackop.PUXC2(4, 3, 2).Serialize())
		pushDepth(4)
		g.swapDepth(2, 0)
		g.swapDepth(1, 3)
		g.swapDepth(0, 2)
	case 7:
		if len(g.stack) < 5 {
			return false
		}
		g.emit("PUXCPU(4,3,2)", stackop.PUXCPU(4, 3, 2).Serialize())
		pushDepth(4)
		g.swapDepth(0, 1)
		g.swapDepth(0, 3)
		pushDepth(2)
	default:
		if len(g.stack) < 5 {
			return false
		}
		g.emit("PU2XC(4,3,2)", stackop.PU2XC(4, 3, 2).Serialize())
		pushDepth(4)
		g.swapDepth(1, 0)
		pushDepth(3)
		g.swapDepth(1, 0)
		g.swapDepth(0, 2)
	}
	return true
}

func (g *parityProgramGenerator) emitReverseOp() bool {
	if len(g.stack) < 2 {
		return false
	}
	maxY := len(g.stack) - 2
	if maxY > 4 {
		maxY = 4
	}
	y := g.r.Intn(maxY + 1)
	maxX := len(g.stack) - y
	if maxX > 5 {
		maxX = 5
	}
	if maxX < 2 {
		return false
	}
	x := 2 + g.r.Intn(maxX-1)
	g.emit(fmt.Sprintf("REVERSE(%d,%d)", x, y), stackop.REVERSE(uint8(x), uint8(y)).Serialize())
	start := len(g.stack) - x - y
	end := len(g.stack) - y
	for l, r := start, end-1; l < r; l, r = l+1, r-1 {
		g.stack[l], g.stack[r] = g.stack[r], g.stack[l]
	}
	return true
}

func (g *parityProgramGenerator) emitMathOp() bool {
	if len(g.stack) == 0 {
		return false
	}
	if g.stack[len(g.stack)-1].kind == parityProgramNaN {
		g.emit("ISNAN", mathop.ISNAN().Serialize())
		g.stack[len(g.stack)-1] = parityProgramIntValue(big.NewInt(-1))
		return true
	}

	switch g.r.Intn(24) {
	case 0:
		return g.emitUnaryIntOp("NEGATE", mathop.NEGATE().Serialize(), func(x *big.Int) *big.Int { return new(big.Int).Neg(x) })
	case 1:
		return g.emitUnaryIntOp("INC", mathop.INC().Serialize(), func(x *big.Int) *big.Int { return new(big.Int).Add(x, big.NewInt(1)) })
	case 2:
		return g.emitUnaryIntOp("DEC", mathop.DEC().Serialize(), func(x *big.Int) *big.Int { return new(big.Int).Sub(x, big.NewInt(1)) })
	case 3:
		return g.emitUnaryIntOp("ABS", mathop.ABS().Serialize(), func(x *big.Int) *big.Int { return new(big.Int).Abs(x) })
	case 4:
		return g.emitUnaryIntOp("NOT", mathop.NOT().Serialize(), func(x *big.Int) *big.Int { return new(big.Int).Not(x) })
	case 5:
		return g.emitUnaryIntToSmallOp("BITSIZE", mathop.BITSIZE().Serialize())
	case 6:
		return g.emitUnaryIntToSmallOp("ISNPOS", mathop.ISNPOS().Serialize())
	case 7:
		return g.emitUnaryIntToSmallOp("ISZERO", mathop.ISZERO().Serialize())
	case 8:
		return g.emitUnaryIntToSmallOp("ISPOS", mathop.ISPOS().Serialize())
	case 9:
		return g.emitUnaryIntToSmallOp("ISNEG", mathop.ISNEG().Serialize())
	case 10:
		return g.emitBinaryIntOp("ADD", mathop.SUM().Serialize(), func(x, y *big.Int) []parityProgramStackValue {
			return []parityProgramStackValue{parityProgramIntValue(new(big.Int).Add(x, y))}
		})
	case 11:
		return g.emitBinaryIntOp("SUB", mathop.SUB().Serialize(), func(x, y *big.Int) []parityProgramStackValue {
			return []parityProgramStackValue{parityProgramIntValue(new(big.Int).Sub(x, y))}
		})
	case 12:
		return g.emitBinaryIntOp("SUBR", mathop.SUBR().Serialize(), func(x, y *big.Int) []parityProgramStackValue {
			return []parityProgramStackValue{parityProgramIntValue(new(big.Int).Sub(y, x))}
		})
	case 13:
		return g.emitBinaryIntOp("MUL", mathop.MUL().Serialize(), func(x, y *big.Int) []parityProgramStackValue {
			return []parityProgramStackValue{parityProgramIntValue(new(big.Int).Mul(x, y))}
		})
	case 14:
		return g.emitBinaryIntOp("AND", mathop.AND().Serialize(), func(x, y *big.Int) []parityProgramStackValue {
			return []parityProgramStackValue{parityProgramIntValue(new(big.Int).And(x, y))}
		})
	case 15:
		return g.emitBinaryIntOp("OR", mathop.OR().Serialize(), func(x, y *big.Int) []parityProgramStackValue {
			return []parityProgramStackValue{parityProgramIntValue(new(big.Int).Or(x, y))}
		})
	case 16:
		return g.emitBinaryIntOp("XOR", mathop.XOR().Serialize(), func(x, y *big.Int) []parityProgramStackValue {
			return []parityProgramStackValue{parityProgramIntValue(new(big.Int).Xor(x, y))}
		})
	case 17:
		return g.emitMathCompareOrMinMax()
	default:
		return g.emitMathExtraOp()
	}
}

func (g *parityProgramGenerator) emitMathCompareOrMinMax() bool {
	switch g.r.Intn(10) {
	case 0:
		return g.emitBinaryIntOp("MIN", mathop.MIN().Serialize(), func(x, y *big.Int) []parityProgramStackValue {
			if x.Cmp(y) <= 0 {
				return []parityProgramStackValue{parityProgramIntValue(x)}
			}
			return []parityProgramStackValue{parityProgramIntValue(y)}
		})
	case 1:
		return g.emitBinaryIntOp("MAX", mathop.MAX().Serialize(), func(x, y *big.Int) []parityProgramStackValue {
			if x.Cmp(y) >= 0 {
				return []parityProgramStackValue{parityProgramIntValue(x)}
			}
			return []parityProgramStackValue{parityProgramIntValue(y)}
		})
	case 2:
		return g.emitBinaryIntOp("MINMAX", mathop.MINMAX().Serialize(), func(x, y *big.Int) []parityProgramStackValue {
			if x.Cmp(y) <= 0 {
				return []parityProgramStackValue{parityProgramIntValue(x), parityProgramIntValue(y)}
			}
			return []parityProgramStackValue{parityProgramIntValue(y), parityProgramIntValue(x)}
		})
	case 3:
		return g.emitBinaryIntToSmallOp("LESS", mathop.LESS().Serialize())
	case 4:
		return g.emitBinaryIntToSmallOp("LEQ", mathop.LEQ().Serialize())
	case 5:
		return g.emitBinaryIntToSmallOp("GREATER", mathop.GREATER().Serialize())
	case 6:
		return g.emitBinaryIntToSmallOp("GEQ", mathop.GEQ().Serialize())
	case 7:
		return g.emitBinaryIntToSmallOp("EQUAL", mathop.EQUAL().Serialize())
	case 8:
		return g.emitBinaryIntToSmallOp("NEQ", mathop.NEQ().Serialize())
	default:
		return g.emitBinaryIntToSmallOp("CMP", mathop.CMP().Serialize())
	}
}

func (g *parityProgramGenerator) emitMathExtraOp() bool {
	base := len(g.stack)
	switch g.r.Intn(7) {
	case 0:
		return g.emitMathCompoundOp(g.r.Intn(21))
	case 1:
		return g.emitMathQuietLogicOp(g.r.Intn(29))
	case 2:
		return g.emitMathShiftModOp(g.r.Intn(20))
	case 3:
		return g.emitMathQuietCompoundOp(g.r.Intn(15))
	case 4:
		return g.emitMathGapOp(g.r.Intn(36))
	}

	switch g.r.Intn(32) {
	case 0:
		g.emitPushInt("addint_x", big.NewInt(12))
		g.emit("ADDINT(-5)", mathop.ADDINT(-5).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(7)))
	case 1:
		g.emitPushInt("mulint_x", big.NewInt(7))
		g.emit("MULINT(-3)", mathop.MULINT(-3).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-21)))
	case 2:
		g.emitPushInt("lessint_x", big.NewInt(3))
		g.emit("LESSINT(4)", mathop.LESSINT(4).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-1)))
	case 3:
		g.emitPushInt("eqint_x", big.NewInt(5))
		g.emit("EQINT(5)", mathop.EQINT(5).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-1)))
	case 4:
		g.emitPushInt("gtint_x", big.NewInt(8))
		g.emit("GTINT(3)", mathop.GTINT(3).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-1)))
	case 5:
		g.emitPushInt("neqint_x", big.NewInt(8))
		g.emit("NEQINT(8)", mathop.NEQINT(8).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(0)))
	case 6:
		g.emitPushInt("qadd_x", big.NewInt(10))
		g.emitPushInt("qadd_y", big.NewInt(20))
		g.emit("QADD", mathop.QADD().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(30)))
	case 7:
		g.emitPushInt("qsub_x", big.NewInt(10))
		g.emitPushInt("qsub_y", big.NewInt(20))
		g.emit("QSUB", mathop.QSUB().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-10)))
	case 8:
		g.emitPushInt("qsubr_x", big.NewInt(10))
		g.emitPushInt("qsubr_y", big.NewInt(20))
		g.emit("QSUBR", mathop.QSUBR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(10)))
	case 9:
		g.emitPushInt("qnegate_x", big.NewInt(-9))
		g.emit("QNEGATE", mathop.QNEGATE().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(9)))
	case 10:
		g.emitPushInt("qinc_x", big.NewInt(9))
		g.emit("QINC", mathop.QINC().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(10)))
	case 11:
		g.emitPushInt("qdec_x", big.NewInt(9))
		g.emit("QDEC", mathop.QDEC().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(8)))
	case 12:
		g.emitPushInt("qaddint_x", big.NewInt(9))
		g.emit("QADDINT(4)", mathop.QADDINT(4).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(13)))
	case 13:
		g.emitPushInt("qmulint_x", big.NewInt(9))
		g.emit("QMULINT(-2)", mathop.QMULINT(-2).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-18)))
	case 14:
		g.emitPushInt("qmul_x", big.NewInt(6))
		g.emitPushInt("qmul_y", big.NewInt(7))
		g.emit("QMUL", mathop.QMUL().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(42)))
	case 15:
		g.emitPushInt("qmin_x", big.NewInt(6))
		g.emitPushInt("qmin_y", big.NewInt(7))
		g.emit("QMIN", mathop.QMIN().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(6)))
	case 16:
		g.emitPushInt("qmax_x", big.NewInt(6))
		g.emitPushInt("qmax_y", big.NewInt(7))
		g.emit("QMAX", mathop.QMAX().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(7)))
	case 17:
		g.emitPushInt("qminmax_x", big.NewInt(7))
		g.emitPushInt("qminmax_y", big.NewInt(6))
		g.emit("QMINMAX", mathop.QMINMAX().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(6)), parityProgramIntValue(big.NewInt(7)))
	case 18:
		g.emitPushInt("qabs_x", big.NewInt(-7))
		g.emit("QABS", mathop.QABS().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(7)))
	case 19:
		g.emitPushInt("sgn_x", big.NewInt(-7))
		g.emit("SGN", mathop.SGN().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-1)))
	case 20:
		g.emitPushInt("ubitsize_x", big.NewInt(255))
		g.emit("UBITSIZE", mathop.UBITSIZE().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(8)))
	case 21:
		g.emitPushInt("qbitsize_x", big.NewInt(-128))
		g.emit("QBITSIZE", mathop.QBITSIZE().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(8)))
	case 22:
		g.emitPushInt("qubitsize_x", big.NewInt(255))
		g.emit("QUBITSIZE", mathop.QUBITSIZE().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(8)))
	case 23:
		g.emitPushInt("fits_x", big.NewInt(63))
		g.emit("FITS(7)", mathop.FITS(6).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(63)))
	case 24:
		g.emitPushInt("ufits_x", big.NewInt(255))
		g.emit("UFITS(8)", mathop.UFITS(7).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(255)))
	case 25:
		g.emitPushInt("fitsx_x", big.NewInt(63))
		g.emitPushInt("fitsx_bits", big.NewInt(7))
		g.emit("FITSX", mathop.FITSX().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(63)))
	case 26:
		g.emitPushInt("ufitsx_x", big.NewInt(255))
		g.emitPushInt("ufitsx_bits", big.NewInt(8))
		g.emit("UFITSX", mathop.UFITSX().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(255)))
	case 27:
		g.emitPushInt("pow2_bits", big.NewInt(5))
		g.emit("POW2", mathop.POW2().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(32)))
	case 28:
		g.emitPushInt("lshift_x", big.NewInt(3))
		g.emitPushInt("lshift_bits", big.NewInt(4))
		g.emit("LSHIFT", mathop.LSHIFT().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(48)))
	case 29:
		g.emitPushInt("rshift_x", big.NewInt(48))
		g.emitPushInt("rshift_bits", big.NewInt(4))
		g.emit("RSHIFT", mathop.RSHIFT().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(3)))
	case 30:
		g.emitPushInt("div_x", big.NewInt(17))
		g.emitPushInt("div_y", big.NewInt(5))
		g.emit("DIV", mathop.DIV().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(3)))
	default:
		g.emitPushInt("divmod_x", big.NewInt(17))
		g.emitPushInt("divmod_y", big.NewInt(5))
		if g.r.Intn(2) == 0 {
			g.emit("MOD", mathop.MOD().Serialize())
			g.stack = g.stack[:base]
			g.stack = append(g.stack, parityProgramIntValue(big.NewInt(2)))
			return true
		}
		g.emit("DIVMOD", mathop.DIVMOD().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(3)), parityProgramIntValue(big.NewInt(2)))
	}
	return true
}

func (g *parityProgramGenerator) emitMathGapOp(mode int) bool {
	base := len(g.stack)
	x := big.NewInt(-17)
	w := big.NewInt(2)
	y := big.NewInt(5)
	mx := big.NewInt(-7)
	my := big.NewInt(5)
	shift := big.NewInt(2)
	codeShift := big.NewInt(3)
	divisor := func(bits *big.Int) *big.Int {
		return new(big.Int).Lsh(big.NewInt(1), uint(bits.Uint64()))
	}
	round := func(v, d *big.Int, rounding int) (*big.Int, *big.Int) {
		v = new(big.Int).Set(v)
		d = new(big.Int).Set(d)
		switch rounding {
		case 0:
			return ophelpers.DivFloor(v, d)
		case 1:
			q := ophelpers.DivRound(v, d)
			return q, new(big.Int).Sub(v, new(big.Int).Mul(d, q))
		default:
			q := ophelpers.DivCeil(v, d)
			return q, new(big.Int).Sub(v, new(big.Int).Mul(d, q))
		}
	}
	pushQR := func(q, r *big.Int) {
		g.stack = append(g.stack, parityProgramIntValue(q), parityProgramIntValue(r))
	}
	pushRShiftMod := func(name string, op *cell.Builder, rounding int) {
		q, r := round(x, divisor(shift), rounding)
		g.emitPushInt(name+"_x", x)
		g.emitPushInt(name+"_shift", shift)
		g.emit(name, op)
		g.stack = g.stack[:base]
		pushQR(q, r)
	}
	pushRShiftCodeMod := func(name string, op *cell.Builder, rounding int) {
		q, r := round(x, divisor(codeShift), rounding)
		g.emitPushInt(name+"_x", x)
		g.emit(name, op)
		g.stack = g.stack[:base]
		pushQR(q, r)
	}
	pushLShiftDivMod := func(name string, op *cell.Builder, rounding int, retModOnly bool) {
		dividend := new(big.Int).Mul(mx, divisor(shift))
		q, r := round(dividend, y, rounding)
		g.emitPushInt(name+"_x", mx)
		g.emitPushInt(name+"_y", y)
		g.emitPushInt(name+"_shift", shift)
		g.emit(name, op)
		g.stack = g.stack[:base]
		if retModOnly {
			g.stack = append(g.stack, parityProgramIntValue(r))
			return
		}
		pushQR(q, r)
	}
	pushAddRShiftMod := func(name string, op *cell.Builder, rounding int) {
		dividend := new(big.Int).Add(x, w)
		q, r := round(dividend, divisor(shift), rounding)
		g.emitPushInt(name+"_x", x)
		g.emitPushInt(name+"_w", w)
		g.emitPushInt(name+"_shift", shift)
		g.emit(name, op)
		g.stack = g.stack[:base]
		pushQR(q, r)
	}
	pushMulPow2Mod := func(name string, op *cell.Builder, rounding int) {
		dividend := new(big.Int).Mul(mx, my)
		_, r := round(dividend, divisor(shift), rounding)
		g.emitPushInt(name+"_x", mx)
		g.emitPushInt(name+"_y", my)
		g.emitPushInt(name+"_shift", shift)
		g.emit(name, op)
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(r))
	}
	pushMulPow2CodeMod := func(name string, op *cell.Builder, rounding int) {
		dividend := new(big.Int).Mul(mx, my)
		_, r := round(dividend, divisor(codeShift), rounding)
		g.emitPushInt(name+"_x", mx)
		g.emitPushInt(name+"_y", my)
		g.emit(name, op)
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(r))
	}
	pushMulRShiftMod := func(name string, op *cell.Builder, rounding int) {
		dividend := new(big.Int).Mul(mx, my)
		q, r := round(dividend, divisor(shift), rounding)
		g.emitPushInt(name+"_x", mx)
		g.emitPushInt(name+"_y", my)
		g.emitPushInt(name+"_shift", shift)
		g.emit(name, op)
		g.stack = g.stack[:base]
		pushQR(q, r)
	}
	pushMulRShiftCodeMod := func(name string, op *cell.Builder, rounding int) {
		dividend := new(big.Int).Mul(mx, my)
		q, r := round(dividend, divisor(codeShift), rounding)
		g.emitPushInt(name+"_x", mx)
		g.emitPushInt(name+"_y", my)
		g.emit(name, op)
		g.stack = g.stack[:base]
		pushQR(q, r)
	}
	pushAddRShiftCodeMod := func(name string, op *cell.Builder, rounding int) {
		dividend := new(big.Int).Add(x, w)
		q, r := round(dividend, divisor(codeShift), rounding)
		g.emitPushInt(name+"_x", x)
		g.emitPushInt(name+"_w", w)
		g.emit(name, op)
		g.stack = g.stack[:base]
		pushQR(q, r)
	}
	pushMulAddRShiftCodeMod := func(name string, op *cell.Builder, rounding int) {
		dividend := new(big.Int).Add(new(big.Int).Mul(mx, my), w)
		q, r := round(dividend, divisor(codeShift), rounding)
		g.emitPushInt(name+"_x", mx)
		g.emitPushInt(name+"_y", my)
		g.emitPushInt(name+"_w", w)
		g.emit(name, op)
		g.stack = g.stack[:base]
		pushQR(q, r)
	}

	switch mode {
	case 0:
		g.emitPushInt("isnneg_zero", big.NewInt(0))
		g.emit("ISNNEG(0)", mathop.ISNNEG().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(true))
	case 1:
		g.emitPushInt("isnneg_negative", big.NewInt(-1))
		g.emit("ISNNEG(-1)", mathop.ISNNEG().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(false))
	case 2:
		g.emitPushInt("addconst_x", big.NewInt(11))
		g.emit("ADDCONST(6)", mathop.ADDCONST(6).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(17)))
	case 3:
		g.emitPushInt("mulconst_x", big.NewInt(7))
		g.emit("MULCONST(-4)", mathop.MULCONST(-4).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-28)))
	case 4:
		pushRShiftMod("RSHIFTMOD", mathop.RSHIFTMOD().Serialize(), 0)
	case 5:
		pushRShiftMod("RSHIFTMODR", mathop.RSHIFTMODR().Serialize(), 1)
	case 6:
		pushRShiftMod("RSHIFTMODC", mathop.RSHIFTMODC().Serialize(), 2)
	case 7:
		pushRShiftCodeMod("RSHIFTCODEMOD(3)", mathop.RSHIFTCODEMOD(3).Serialize(), 0)
	case 8:
		pushRShiftCodeMod("RSHIFTRCODEMOD(3)", mathop.RSHIFTRCODEMOD(3).Serialize(), 1)
	case 9:
		pushRShiftCodeMod("RSHIFTCCODEMOD(3)", mathop.RSHIFTCCODEMOD(3).Serialize(), 2)
	case 10:
		pushLShiftDivMod("LSHIFTMOD", mathop.LSHIFTMOD().Serialize(), 0, true)
	case 11:
		pushLShiftDivMod("LSHIFTMODR", mathop.LSHIFTMODR().Serialize(), 1, true)
	case 12:
		pushLShiftDivMod("LSHIFTMODC", mathop.LSHIFTMODC().Serialize(), 2, true)
	case 13:
		pushLShiftDivMod("LSHIFTDIVMODR", mathop.LSHIFTDIVMODR().Serialize(), 1, false)
	case 14:
		pushLShiftDivMod("LSHIFTDIVMODC", mathop.LSHIFTDIVMODC().Serialize(), 2, false)
	case 15:
		pushAddRShiftMod("ADDRSHIFTMOD", mathop.ADDRSHIFTMOD().Serialize(), 0)
	case 16:
		pushAddRShiftMod("ADDRSHIFTMODR", mathop.ADDRSHIFTMODR().Serialize(), 1)
	case 17:
		pushAddRShiftMod("ADDRSHIFTMODC", mathop.ADDRSHIFTMODC().Serialize(), 2)
	case 18:
		pushMulPow2Mod("MULMODPOW2", mathop.MULMODPOW2_VAR().Serialize(), 0)
	case 19:
		pushMulPow2Mod("MULMODPOW2R", mathop.MULMODPOW2R_VAR().Serialize(), 1)
	case 20:
		pushMulPow2Mod("MULMODPOW2C", mathop.MULMODPOW2C_VAR().Serialize(), 2)
	case 21:
		pushMulRShiftMod("MULRSHIFTMOD", mathop.MULRSHIFTMOD_VAR().Serialize(), 0)
	case 22:
		pushMulRShiftMod("MULRSHIFTRMOD", mathop.MULRSHIFTRMOD_VAR().Serialize(), 1)
	case 23:
		pushMulRShiftMod("MULRSHIFTCMOD", mathop.MULRSHIFTCMOD_VAR().Serialize(), 2)
	case 24:
		pushMulPow2CodeMod("MULMODPOW2CODE(3)", mathop.MULMODPOW2CODE(3).Serialize(), 0)
	case 25:
		pushMulPow2CodeMod("MULMODPOW2RCODE(3)", mathop.MULMODPOW2RCODE(3).Serialize(), 1)
	case 26:
		pushMulPow2CodeMod("MULMODPOW2CCODE(3)", mathop.MULMODPOW2CCODE(3).Serialize(), 2)
	case 27:
		pushMulRShiftCodeMod("MULRSHIFTCODEMOD(3)", mathop.MULRSHIFTCODEMOD(3).Serialize(), 0)
	case 28:
		pushMulRShiftCodeMod("MULRSHIFTRCODEMOD(3)", mathop.MULRSHIFTRCODEMOD(3).Serialize(), 1)
	case 29:
		pushMulRShiftCodeMod("MULRSHIFTCCODEMOD(3)", mathop.MULRSHIFTCCODEMOD(3).Serialize(), 2)
	case 30:
		pushAddRShiftCodeMod("ADDRSHIFTCODEMOD(3)", mathop.ADDRSHIFTCODEMOD(3).Serialize(), 0)
	case 31:
		pushAddRShiftCodeMod("ADDRSHIFTRCODEMOD(3)", mathop.ADDRSHIFTRCODEMOD(3).Serialize(), 1)
	case 32:
		pushAddRShiftCodeMod("ADDRSHIFTCCODEMOD(3)", mathop.ADDRSHIFTCCODEMOD(3).Serialize(), 2)
	case 33:
		pushMulAddRShiftCodeMod("MULADDRSHIFTCODEMOD(3)", mathop.MULADDRSHIFTCODEMOD(3).Serialize(), 0)
	case 34:
		pushMulAddRShiftCodeMod("MULADDRSHIFTRCODEMOD(3)", mathop.MULADDRSHIFTRCODEMOD(3).Serialize(), 1)
	default:
		pushMulAddRShiftCodeMod("MULADDRSHIFTCCODEMOD(3)", mathop.MULADDRSHIFTCCODEMOD(3).Serialize(), 2)
	}
	return true
}

func (g *parityProgramGenerator) emitMathCompoundOp(mode int) bool {
	base := len(g.stack)
	divResult := func(x, y *big.Int, rounding int) (*big.Int, *big.Int) {
		x = new(big.Int).Set(x)
		y = new(big.Int).Set(y)
		switch rounding {
		case 0:
			return ophelpers.DivFloor(x, y)
		case 1:
			q := ophelpers.DivRound(x, y)
			return q, new(big.Int).Sub(x, new(big.Int).Mul(y, q))
		default:
			q := ophelpers.DivCeil(x, y)
			return q, new(big.Int).Sub(x, new(big.Int).Mul(y, q))
		}
	}
	pushDivArgs := func(x, y *big.Int) {
		g.emitPushInt("compound_x", x)
		g.emitPushInt("compound_y", y)
	}
	pushMulDivArgs := func(x, y, z *big.Int) {
		g.emitPushInt("compound_x", x)
		g.emitPushInt("compound_y", y)
		g.emitPushInt("compound_z", z)
	}
	pushAddDivArgs := func(x, w, z *big.Int) {
		g.emitPushInt("compound_x", x)
		g.emitPushInt("compound_w", w)
		g.emitPushInt("compound_z", z)
	}
	pushMulAddDivArgs := func(x, y, w, z *big.Int) {
		g.emitPushInt("compound_x", x)
		g.emitPushInt("compound_y", y)
		g.emitPushInt("compound_w", w)
		g.emitPushInt("compound_z", z)
	}
	qrValues := func(q, r *big.Int) []parityProgramStackValue {
		return []parityProgramStackValue{parityProgramIntValue(q), parityProgramIntValue(r)}
	}

	x := big.NewInt(-17)
	y := big.NewInt(5)
	mx := big.NewInt(-7)
	my := big.NewInt(5)
	z := big.NewInt(4)
	w := big.NewInt(2)

	switch mode {
	case 0:
		q, _ := divResult(x, y, 1)
		pushDivArgs(x, y)
		g.emit("DIVR", mathop.DIVR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(q))
	case 1:
		q, _ := divResult(x, y, 2)
		pushDivArgs(x, y)
		g.emit("DIVC", mathop.DIVC().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(q))
	case 2:
		_, r := divResult(x, y, 1)
		pushDivArgs(x, y)
		g.emit("MODR", mathop.MODR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(r))
	case 3:
		_, r := divResult(x, y, 2)
		pushDivArgs(x, y)
		g.emit("MODC", mathop.MODC().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(r))
	case 4:
		q, r := divResult(x, y, 1)
		pushDivArgs(x, y)
		g.emit("DIVMODR", mathop.DIVMODR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, qrValues(q, r)...)
	case 5:
		q, r := divResult(x, y, 2)
		pushDivArgs(x, y)
		g.emit("DIVMODC", mathop.DIVMODC().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, qrValues(q, r)...)
	case 6:
		product := new(big.Int).Mul(mx, my)
		q, _ := divResult(product, z, 0)
		pushMulDivArgs(mx, my, z)
		g.emit("MULDIV", mathop.MULDIV().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(q))
	case 7:
		product := new(big.Int).Mul(mx, my)
		q, _ := divResult(product, z, 1)
		pushMulDivArgs(mx, my, z)
		g.emit("MULDIVR", mathop.MULDIVR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(q))
	case 8:
		product := new(big.Int).Mul(mx, my)
		q, _ := divResult(product, z, 2)
		pushMulDivArgs(mx, my, z)
		g.emit("MULDIVC", mathop.MULDIVC().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(q))
	case 9:
		product := new(big.Int).Mul(mx, my)
		q, r := divResult(product, z, 0)
		pushMulDivArgs(mx, my, z)
		g.emit("MULDIVMOD", mathop.MULDIVMOD().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, qrValues(q, r)...)
	case 10:
		product := new(big.Int).Mul(mx, my)
		q, r := divResult(product, z, 1)
		pushMulDivArgs(mx, my, z)
		g.emit("MULDIVMODR", mathop.MULDIVMODR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, qrValues(q, r)...)
	case 11:
		product := new(big.Int).Mul(mx, my)
		q, r := divResult(product, z, 2)
		pushMulDivArgs(mx, my, z)
		g.emit("MULDIVMODC", mathop.MULDIVMODC().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, qrValues(q, r)...)
	case 12:
		sum := new(big.Int).Add(x, w)
		q, r := divResult(sum, y, 0)
		pushAddDivArgs(x, w, y)
		g.emit("ADDDIVMOD", mathop.ADDDIVMOD().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, qrValues(q, r)...)
	case 13:
		sum := new(big.Int).Add(x, w)
		q, r := divResult(sum, y, 1)
		pushAddDivArgs(x, w, y)
		g.emit("ADDDIVMODR", mathop.ADDDIVMODR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, qrValues(q, r)...)
	case 14:
		sum := new(big.Int).Add(x, w)
		q, r := divResult(sum, y, 2)
		pushAddDivArgs(x, w, y)
		g.emit("ADDDIVMODC", mathop.ADDDIVMODC().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, qrValues(q, r)...)
	case 15:
		sum := new(big.Int).Add(new(big.Int).Mul(mx, my), w)
		q, r := divResult(sum, z, 0)
		pushMulAddDivArgs(mx, my, w, z)
		g.emit("MULADDDIVMOD", mathop.MULADDDIVMOD().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, qrValues(q, r)...)
	case 16:
		sum := new(big.Int).Add(new(big.Int).Mul(mx, my), w)
		q, r := divResult(sum, z, 1)
		pushMulAddDivArgs(mx, my, w, z)
		g.emit("MULADDDIVMODR", mathop.MULADDDIVMODR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, qrValues(q, r)...)
	case 17:
		sum := new(big.Int).Add(new(big.Int).Mul(mx, my), w)
		q, r := divResult(sum, z, 2)
		pushMulAddDivArgs(mx, my, w, z)
		g.emit("MULADDDIVMODC", mathop.MULADDDIVMODC().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, qrValues(q, r)...)
	case 18:
		product := new(big.Int).Mul(mx, my)
		_, r := divResult(product, z, 0)
		pushMulDivArgs(mx, my, z)
		g.emit("MULMOD", mathop.MULMOD().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(r))
	case 19:
		product := new(big.Int).Mul(mx, my)
		_, r := divResult(product, z, 1)
		pushMulDivArgs(mx, my, z)
		g.emit("MULMODR", mathop.MULMODR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(r))
	default:
		product := new(big.Int).Mul(mx, my)
		_, r := divResult(product, z, 2)
		pushMulDivArgs(mx, my, z)
		g.emit("MULMODC", mathop.MULMODC().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(r))
	}
	return true
}

func (g *parityProgramGenerator) emitMathQuietLogicOp(mode int) bool {
	base := len(g.stack)

	switch mode {
	case 0:
		g.emitPushInt("qnot_x", big.NewInt(5))
		g.emit("QNOT", mathop.QNOT().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-6)))
	case 1:
		g.emitPushInt("qand_x", big.NewInt(0x0F))
		g.emitPushInt("qand_y", big.NewInt(0x33))
		g.emit("QAND", mathop.QAND().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(0x03)))
	case 2:
		g.emitPushInt("qor_x", big.NewInt(0x0F))
		g.emitPushInt("qor_y", big.NewInt(0x30))
		g.emit("QOR", mathop.QOR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(0x3F)))
	case 3:
		g.emitPushInt("qxor_x", big.NewInt(0x0F))
		g.emitPushInt("qxor_y", big.NewInt(0x33))
		g.emit("QXOR", mathop.QXOR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(0x3C)))
	case 4:
		g.emitPushInt("qlshift_x", big.NewInt(3))
		g.emitPushInt("qlshift_bits", big.NewInt(4))
		g.emit("QLSHIFT", mathop.QLSHIFT().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(48)))
	case 5:
		g.emitPushInt("qrshift_x", big.NewInt(48))
		g.emitPushInt("qrshift_bits", big.NewInt(4))
		g.emit("QRSHIFT", mathop.QRSHIFT().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(3)))
	case 6:
		g.emitPushInt("qlshiftcode_x", big.NewInt(3))
		g.emit("QLSHIFTCODE(3)", mathop.QLSHIFTCODE(3).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(24)))
	case 7:
		g.emitPushInt("qrshiftcode_x", big.NewInt(48))
		g.emit("QRSHIFTCODE(3)", mathop.QRSHIFTCODE(3).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(6)))
	case 8:
		g.emitPushInt("qpow2_bits", big.NewInt(5))
		g.emit("QPOW2", mathop.QPOW2().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(32)))
	case 9:
		g.emitPushInt("qsgn_x", big.NewInt(-7))
		g.emit("QSGN", mathop.QSGN().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-1)))
	case 10:
		g.emitPushInt("qless_x", big.NewInt(3))
		g.emitPushInt("qless_y", big.NewInt(7))
		g.emit("QLESS", mathop.QLESS().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(true))
	case 11:
		g.emitPushInt("qequal_x", big.NewInt(5))
		g.emitPushInt("qequal_y", big.NewInt(5))
		g.emit("QEQUAL", mathop.QEQUAL().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(true))
	case 12:
		g.emitPushInt("qleq_x", big.NewInt(5))
		g.emitPushInt("qleq_y", big.NewInt(5))
		g.emit("QLEQ", mathop.QLEQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(true))
	case 13:
		g.emitPushInt("qgreater_x", big.NewInt(8))
		g.emitPushInt("qgreater_y", big.NewInt(3))
		g.emit("QGREATER", mathop.QGREATER().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(true))
	case 14:
		g.emitPushInt("qneq_x", big.NewInt(8))
		g.emitPushInt("qneq_y", big.NewInt(3))
		g.emit("QNEQ", mathop.QNEQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(true))
	case 15:
		g.emitPushInt("qgeq_x", big.NewInt(8))
		g.emitPushInt("qgeq_y", big.NewInt(8))
		g.emit("QGEQ", mathop.QGEQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(true))
	case 16:
		g.emitPushInt("qcmp_x", big.NewInt(3))
		g.emitPushInt("qcmp_y", big.NewInt(7))
		g.emit("QCMP", mathop.QCMP().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-1)))
	case 17:
		g.emitPushInt("qeqint_x", big.NewInt(5))
		g.emit("QEQINT(5)", mathop.QEQINT(5).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(true))
	case 18:
		g.emitPushInt("qlessint_x", big.NewInt(3))
		g.emit("QLESSINT(4)", mathop.QLESSINT(4).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(true))
	case 19:
		g.emitPushInt("qgtint_x", big.NewInt(5))
		g.emit("QGTINT(4)", mathop.QGTINT(4).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(true))
	case 20:
		g.emitPushInt("qneqint_x", big.NewInt(5))
		g.emit("QNEQINT(4)", mathop.QNEQINT(4).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(true))
	case 21:
		g.emitPushInt("qfits_x", big.NewInt(63))
		g.emit("QFITS(6)", mathop.QFITS(6).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(63)))
	case 22:
		g.emitPushInt("qfits_fail_x", big.NewInt(8))
		g.emit("QFITS(3 fail)", mathop.QFITS(2).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramNaN})
	case 23:
		g.emitPushInt("qufits_x", big.NewInt(255))
		g.emit("QUFITS(8)", mathop.QUFITS(7).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(255)))
	case 24:
		g.emitPushInt("qfitsx_x", big.NewInt(63))
		g.emitPushInt("qfitsx_bits", big.NewInt(7))
		g.emit("QFITSX", mathop.QFITSX().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(63)))
	case 25:
		g.emitPushInt("qufitsx_fail_x", big.NewInt(-1))
		g.emitPushInt("qufitsx_fail_bits", big.NewInt(8))
		g.emit("QUFITSX(fail)", mathop.QUFITSX().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramNaN})
	case 26:
		g.emitPushNaN("qsgn_nan")
		g.emit("QSGN(NaN)", mathop.QSGN().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramNaN})
	case 27:
		g.emitPushNaN("qgtint_nan")
		g.emit("QGTINT(NaN)", mathop.QGTINT(4).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramNaN})
	default:
		g.emitPushInt("chknan_x", big.NewInt(7))
		g.emit("CHKNAN", mathop.CHKNAN().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(7)))
	}
	return true
}

func (g *parityProgramGenerator) emitMathShiftModOp(mode int) bool {
	base := len(g.stack)
	x := big.NewInt(-17)
	mx := big.NewInt(-7)
	my := big.NewInt(5)
	w := big.NewInt(2)
	shift := big.NewInt(2)
	divisor := func(s *big.Int) *big.Int {
		return new(big.Int).Lsh(big.NewInt(1), uint(s.Uint64()))
	}
	round := func(v, d *big.Int, mode int) (*big.Int, *big.Int) {
		switch mode {
		case 0:
			return ophelpers.DivFloor(new(big.Int).Set(v), new(big.Int).Set(d))
		case 1:
			q := ophelpers.DivRound(new(big.Int).Set(v), new(big.Int).Set(d))
			return q, new(big.Int).Sub(new(big.Int).Set(v), new(big.Int).Mul(new(big.Int).Set(d), q))
		default:
			q := ophelpers.DivCeil(new(big.Int).Set(v), new(big.Int).Set(d))
			return q, new(big.Int).Sub(new(big.Int).Set(v), new(big.Int).Mul(new(big.Int).Set(d), q))
		}
	}
	pushXShift := func(name string) {
		g.emitPushInt(name+"_x", x)
		g.emitPushInt(name+"_shift", shift)
	}
	pushMulShift := func(name string) {
		g.emitPushInt(name+"_x", mx)
		g.emitPushInt(name+"_y", my)
		g.emitPushInt(name+"_shift", shift)
	}
	pushLShiftDiv := func(name string) {
		g.emitPushInt(name+"_x", mx)
		g.emitPushInt(name+"_y", my)
		g.emitPushInt(name+"_shift", shift)
	}
	pushLShiftAddDivMod := func(name string) {
		g.emitPushInt(name+"_x", mx)
		g.emitPushInt(name+"_w", w)
		g.emitPushInt(name+"_z", my)
		g.emitPushInt(name+"_shift", shift)
	}
	pushMulAddRShiftMod := func(name string) {
		g.emitPushInt(name+"_x", mx)
		g.emitPushInt(name+"_y", my)
		g.emitPushInt(name+"_w", w)
		g.emitPushInt(name+"_shift", shift)
	}

	switch mode {
	case 0:
		pushXShift("rshiftfloor")
		g.emit("RSHIFTFLOOR", mathop.RSHIFTFLOOR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(new(big.Int).Rsh(new(big.Int).Set(x), uint(shift.Uint64()))))
	case 1:
		q, _ := round(x, divisor(shift), 1)
		pushXShift("rshiftr")
		g.emit("RSHIFTR", mathop.RSHIFTR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(q))
	case 2:
		q, _ := round(x, divisor(shift), 2)
		pushXShift("rshiftc")
		g.emit("RSHIFTC", mathop.RSHIFTC().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(q))
	case 3:
		_, r := round(x, divisor(shift), 0)
		pushXShift("modpow2")
		g.emit("MODPOW2", mathop.MODPOW2().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(r))
	case 4:
		_, r := round(x, divisor(shift), 1)
		pushXShift("modpow2r")
		g.emit("MODPOW2R", mathop.MODPOW2R().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(r))
	case 5:
		_, r := round(x, divisor(shift), 2)
		pushXShift("modpow2c")
		g.emit("MODPOW2C", mathop.MODPOW2C().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(r))
	case 6:
		g.emitPushInt("rshiftcodefloor_x", x)
		g.emit("RSHIFTCODEFLOOR(3)", mathop.RSHIFTCODEFLOOR(3).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(new(big.Int).Rsh(new(big.Int).Set(x), 3)))
	case 7:
		product := new(big.Int).Mul(mx, my)
		q, _ := round(product, divisor(shift), 0)
		pushMulShift("mulrshift")
		g.emit("MULRSHIFT", mathop.MULRSHIFT().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(q))
	case 8:
		product := new(big.Int).Mul(mx, my)
		q, _ := round(product, divisor(shift), 1)
		pushMulShift("mulrshiftr")
		g.emit("MULRSHIFTR", mathop.MULRSHIFTR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(q))
	case 9:
		product := new(big.Int).Mul(mx, my)
		q, _ := round(product, divisor(shift), 2)
		pushMulShift("mulrshiftc")
		g.emit("MULRSHIFTC", mathop.MULRSHIFTC().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(q))
	case 10:
		dividend := new(big.Int).Mul(mx, divisor(shift))
		q, _ := round(dividend, my, 0)
		pushLShiftDiv("lshiftdiv")
		g.emit("LSHIFTDIV", mathop.LSHIFTDIV().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(q))
	case 11:
		dividend := new(big.Int).Mul(mx, divisor(shift))
		q, _ := round(dividend, my, 1)
		pushLShiftDiv("lshiftdivr")
		g.emit("LSHIFTDIVR", mathop.LSHIFTDIVR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(q))
	case 12:
		dividend := new(big.Int).Mul(mx, divisor(shift))
		q, _ := round(dividend, my, 2)
		pushLShiftDiv("lshiftdivc")
		g.emit("LSHIFTDIVC", mathop.LSHIFTDIVC().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(q))
	case 13:
		dividend := new(big.Int).Mul(mx, divisor(shift))
		q, r := round(dividend, my, 0)
		pushLShiftDiv("lshiftdivmod")
		g.emit("LSHIFTDIVMOD", mathop.LSHIFTDIVMOD().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(q), parityProgramIntValue(r))
	case 14:
		dividend := new(big.Int).Add(new(big.Int).Mul(mx, divisor(shift)), w)
		q, r := round(dividend, my, 0)
		pushLShiftAddDivMod("lshiftadddivmod")
		g.emit("LSHIFTADDDIVMOD", mathop.LSHIFTADDDIVMOD().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(q), parityProgramIntValue(r))
	case 15:
		dividend := new(big.Int).Add(new(big.Int).Mul(mx, divisor(shift)), w)
		q, r := round(dividend, my, 1)
		pushLShiftAddDivMod("lshiftadddivmodr")
		g.emit("LSHIFTADDDIVMODR", mathop.LSHIFTADDDIVMODR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(q), parityProgramIntValue(r))
	case 16:
		dividend := new(big.Int).Add(new(big.Int).Mul(mx, divisor(shift)), w)
		q, r := round(dividend, my, 2)
		pushLShiftAddDivMod("lshiftadddivmodc")
		g.emit("LSHIFTADDDIVMODC", mathop.LSHIFTADDDIVMODC().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(q), parityProgramIntValue(r))
	case 17:
		dividend := new(big.Int).Add(new(big.Int).Mul(mx, my), w)
		q, r := round(dividend, divisor(shift), 0)
		pushMulAddRShiftMod("muladdrshiftmod")
		g.emit("MULADDRSHIFTMOD", mathop.MULADDRSHIFTMOD().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(q), parityProgramIntValue(r))
	case 18:
		dividend := new(big.Int).Add(new(big.Int).Mul(mx, my), w)
		q, r := round(dividend, divisor(shift), 1)
		pushMulAddRShiftMod("muladdrshiftrmod")
		g.emit("MULADDRSHIFTRMOD", mathop.MULADDRSHIFTRMOD().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(q), parityProgramIntValue(r))
	default:
		dividend := new(big.Int).Add(new(big.Int).Mul(mx, my), w)
		q, r := round(dividend, divisor(shift), 2)
		pushMulAddRShiftMod("muladdrshiftcmod")
		g.emit("MULADDRSHIFTCMOD", mathop.MULADDRSHIFTCMOD().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(q), parityProgramIntValue(r))
	}
	return true
}

func (g *parityProgramGenerator) emitMathQuietCompoundOp(mode int) bool {
	base := len(g.stack)
	x := big.NewInt(-17)
	y := big.NewInt(5)
	mx := big.NewInt(-7)
	my := big.NewInt(5)
	w := big.NewInt(2)
	z := big.NewInt(4)
	shift := big.NewInt(2)
	round := func(v, d *big.Int, args uint8) (*big.Int, *big.Int) {
		switch args & 3 {
		case 0:
			return ophelpers.DivFloor(new(big.Int).Set(v), new(big.Int).Set(d))
		case 1:
			q := ophelpers.DivRound(new(big.Int).Set(v), new(big.Int).Set(d))
			return q, new(big.Int).Sub(new(big.Int).Set(v), new(big.Int).Mul(new(big.Int).Set(d), q))
		default:
			q := ophelpers.DivCeil(new(big.Int).Set(v), new(big.Int).Set(d))
			return q, new(big.Int).Sub(new(big.Int).Set(v), new(big.Int).Mul(new(big.Int).Set(d), q))
		}
	}
	emit := func(name string, prefix uint64, args uint8) {
		g.emit(name, parityProgramRawOp((prefix<<4)|uint64(args), 24))
	}
	pushSelected := func(args uint8, q, r *big.Int) {
		d := (args >> 2) & 3
		if d == 0 {
			d = 3
		}
		if d&1 != 0 {
			g.stack = append(g.stack, parityProgramIntValue(q))
		}
		if d&2 != 0 {
			g.stack = append(g.stack, parityProgramIntValue(r))
		}
	}
	divider := func(bits *big.Int) *big.Int {
		return new(big.Int).Lsh(big.NewInt(1), uint(bits.Uint64()))
	}

	switch mode {
	case 0:
		args := uint8(4)
		q, r := round(x, y, args)
		g.emitPushInt("qdiv_x", x)
		g.emitPushInt("qdiv_y", y)
		emit("QDIV", 0xB7A90, args)
		g.stack = g.stack[:base]
		pushSelected(args, q, r)
	case 1:
		args := uint8(9)
		q, r := round(x, y, args)
		g.emitPushInt("qmodr_x", x)
		g.emitPushInt("qmodr_y", y)
		emit("QMODR", 0xB7A90, args)
		g.stack = g.stack[:base]
		pushSelected(args, q, r)
	case 2:
		args := uint8(2)
		dividend := new(big.Int).Add(new(big.Int).Set(x), w)
		q, r := round(dividend, y, args)
		g.emitPushInt("qadddivmodc_x", x)
		g.emitPushInt("qadddivmodc_w", w)
		g.emitPushInt("qadddivmodc_y", y)
		emit("QADDDIVMODC", 0xB7A90, args)
		g.stack = g.stack[:base]
		pushSelected(args, q, r)
	case 3:
		args := uint8(4)
		q, r := round(x, divider(shift), args)
		g.emitPushInt("qrshift_x", x)
		g.emitPushInt("qrshift_shift", shift)
		emit("QRSHIFT", 0xB7A92, args)
		g.stack = g.stack[:base]
		pushSelected(args, q, r)
	case 4:
		args := uint8(9)
		q, r := round(x, divider(shift), args)
		g.emitPushInt("qmodpow2r_x", x)
		g.emitPushInt("qmodpow2r_shift", shift)
		emit("QMODPOW2R", 0xB7A92, args)
		g.stack = g.stack[:base]
		pushSelected(args, q, r)
	case 5:
		args := uint8(2)
		dividend := new(big.Int).Add(new(big.Int).Set(x), w)
		q, r := round(dividend, divider(shift), args)
		g.emitPushInt("qaddrshiftmodc_x", x)
		g.emitPushInt("qaddrshiftmodc_w", w)
		g.emitPushInt("qaddrshiftmodc_shift", shift)
		emit("QADDRSHIFTMODC", 0xB7A92, args)
		g.stack = g.stack[:base]
		pushSelected(args, q, r)
	case 6:
		args := uint8(4)
		dividend := new(big.Int).Mul(mx, my)
		q, r := round(dividend, z, args)
		g.emitPushInt("qmuldiv_x", mx)
		g.emitPushInt("qmuldiv_y", my)
		g.emitPushInt("qmuldiv_z", z)
		emit("QMULDIV", 0xB7A98, args)
		g.stack = g.stack[:base]
		pushSelected(args, q, r)
	case 7:
		args := uint8(9)
		dividend := new(big.Int).Mul(mx, my)
		q, r := round(dividend, z, args)
		g.emitPushInt("qmulmodr_x", mx)
		g.emitPushInt("qmulmodr_y", my)
		g.emitPushInt("qmulmodr_z", z)
		emit("QMULMODR", 0xB7A98, args)
		g.stack = g.stack[:base]
		pushSelected(args, q, r)
	case 8:
		args := uint8(2)
		dividend := new(big.Int).Add(new(big.Int).Mul(mx, my), w)
		q, r := round(dividend, z, args)
		g.emitPushInt("qmuladddivmodc_x", mx)
		g.emitPushInt("qmuladddivmodc_y", my)
		g.emitPushInt("qmuladddivmodc_w", w)
		g.emitPushInt("qmuladddivmodc_z", z)
		emit("QMULADDDIVMODC", 0xB7A98, args)
		g.stack = g.stack[:base]
		pushSelected(args, q, r)
	case 9:
		args := uint8(4)
		dividend := new(big.Int).Mul(mx, my)
		q, r := round(dividend, divider(shift), args)
		g.emitPushInt("qmulrshift_x", mx)
		g.emitPushInt("qmulrshift_y", my)
		g.emitPushInt("qmulrshift_shift", shift)
		emit("QMULRSHIFT", 0xB7A9A, args)
		g.stack = g.stack[:base]
		pushSelected(args, q, r)
	case 10:
		args := uint8(9)
		dividend := new(big.Int).Mul(mx, my)
		q, r := round(dividend, divider(shift), args)
		g.emitPushInt("qmulmodpow2r_x", mx)
		g.emitPushInt("qmulmodpow2r_y", my)
		g.emitPushInt("qmulmodpow2r_shift", shift)
		emit("QMULMODPOW2R", 0xB7A9A, args)
		g.stack = g.stack[:base]
		pushSelected(args, q, r)
	case 11:
		args := uint8(2)
		dividend := new(big.Int).Add(new(big.Int).Mul(mx, my), w)
		q, r := round(dividend, divider(shift), args)
		g.emitPushInt("qmuladdrshiftmodc_x", mx)
		g.emitPushInt("qmuladdrshiftmodc_y", my)
		g.emitPushInt("qmuladdrshiftmodc_w", w)
		g.emitPushInt("qmuladdrshiftmodc_shift", shift)
		emit("QMULADDRSHIFTMODC", 0xB7A9A, args)
		g.stack = g.stack[:base]
		pushSelected(args, q, r)
	case 12:
		args := uint8(4)
		dividend := new(big.Int).Lsh(new(big.Int).Set(x), uint(shift.Uint64()))
		q, r := round(dividend, y, args)
		g.emitPushInt("qlshiftdiv_x", x)
		g.emitPushInt("qlshiftdiv_y", y)
		g.emitPushInt("qlshiftdiv_shift", shift)
		emit("QLSHIFTDIV", 0xB7A9C, args)
		g.stack = g.stack[:base]
		pushSelected(args, q, r)
	case 13:
		args := uint8(9)
		dividend := new(big.Int).Lsh(new(big.Int).Set(x), uint(shift.Uint64()))
		q, r := round(dividend, y, args)
		g.emitPushInt("qlshiftmodr_x", x)
		g.emitPushInt("qlshiftmodr_y", y)
		g.emitPushInt("qlshiftmodr_shift", shift)
		emit("QLSHIFTMODR", 0xB7A9C, args)
		g.stack = g.stack[:base]
		pushSelected(args, q, r)
	default:
		args := uint8(2)
		dividend := new(big.Int).Lsh(new(big.Int).Set(x), uint(shift.Uint64()))
		dividend.Add(dividend, w)
		q, r := round(dividend, y, args)
		g.emitPushInt("qlshiftadddivmodc_x", x)
		g.emitPushInt("qlshiftadddivmodc_w", w)
		g.emitPushInt("qlshiftadddivmodc_y", y)
		g.emitPushInt("qlshiftadddivmodc_shift", shift)
		emit("QLSHIFTADDDIVMODC", 0xB7A9C, args)
		g.stack = g.stack[:base]
		pushSelected(args, q, r)
	}
	return true
}

func (g *parityProgramGenerator) emitTupleOp() bool {
	switch g.r.Intn(20) {
	case 0:
		if len(g.stack) == 0 {
			return false
		}
		g.emit("ISNULL", tupleop.ISNULL().Serialize())
		g.stack[len(g.stack)-1] = parityProgramIntValue(big.NewInt(0))
	case 1:
		if len(g.stack) == 0 {
			return false
		}
		g.emit("ISTUPLE", tupleop.ISTUPLE().Serialize())
		g.stack[len(g.stack)-1] = parityProgramIntValue(big.NewInt(0))
	case 2:
		if len(g.stack) == 0 {
			return false
		}
		g.emit("QTLEN", tupleop.QTLEN().Serialize())
		g.stack[len(g.stack)-1] = parityProgramIntValue(big.NewInt(0))
	case 3:
		n := g.r.Intn(5)
		if n > len(g.stack) {
			n = len(g.stack)
		}
		g.emit(fmt.Sprintf("TUPLE(%d)", n), tupleop.TUPLE(uint8(n)).Serialize())
		items := parityProgramCloneStack(g.stack[len(g.stack)-n:])
		g.stack = g.stack[:len(g.stack)-n]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramTuple, tuple: items})
	case 4:
		top := g.topTuple()
		if top == nil || len(top.tuple) > 15 {
			return false
		}
		n := len(top.tuple)
		g.emit(fmt.Sprintf("UNTUPLE(%d)", n), tupleop.UNTUPLE(uint8(n)).Serialize())
		items := parityProgramCloneStack(top.tuple)
		g.stack = g.stack[:len(g.stack)-1]
		g.stack = append(g.stack, items...)
	case 5:
		top := g.topTuple()
		if top == nil {
			return false
		}
		n := 0
		if len(top.tuple) > 0 {
			max := len(top.tuple)
			if max > 15 {
				max = 15
			}
			n = g.r.Intn(max + 1)
		}
		g.emit(fmt.Sprintf("UNPACKFIRST(%d)", n), tupleop.UNPACKFIRST(uint8(n)).Serialize())
		items := parityProgramCloneStack(top.tuple[:n])
		g.stack = g.stack[:len(g.stack)-1]
		g.stack = append(g.stack, items...)
	case 6:
		top := g.topTuple()
		if top == nil || len(top.tuple) == 0 {
			return false
		}
		max := len(top.tuple)
		if max > 16 {
			max = 16
		}
		idx := g.r.Intn(max)
		g.emit(fmt.Sprintf("INDEX(%d)", idx), tupleop.INDEX(uint8(idx)).Serialize())
		item := parityProgramCloneValue(top.tuple[idx])
		g.stack[len(g.stack)-1] = item
	case 7:
		if len(g.stack) == 0 {
			return false
		}
		idx := g.r.Intn(6)
		g.emit(fmt.Sprintf("INDEXQ(%d)", idx), tupleop.INDEXQ(uint8(idx)).Serialize())
		top := g.stack[len(g.stack)-1]
		if top.kind == parityProgramTuple && idx < len(top.tuple) {
			g.stack[len(g.stack)-1] = parityProgramCloneValue(top.tuple[idx])
		} else {
			g.stack[len(g.stack)-1] = parityProgramStackValue{kind: parityProgramNull}
		}
	case 8:
		top := g.topTuple()
		if top == nil {
			return false
		}
		g.emit("TLEN", tupleop.TLEN().Serialize())
		g.stack[len(g.stack)-1] = parityProgramIntValue(big.NewInt(int64(len(top.tuple))))
	case 9:
		top := g.topTuple()
		if top == nil || len(top.tuple) == 0 {
			return false
		}
		g.emit("LAST", tupleop.LAST().Serialize())
		g.stack[len(g.stack)-1] = parityProgramCloneValue(top.tuple[len(top.tuple)-1])
	case 10:
		top := g.topTuple()
		if top == nil || len(top.tuple) > 15 {
			return false
		}
		max := len(top.tuple)
		g.emit(fmt.Sprintf("EXPLODE(%d)", max), tupleop.EXPLODE(uint8(max)).Serialize())
		items := parityProgramCloneStack(top.tuple)
		g.stack = g.stack[:len(g.stack)-1]
		g.stack = append(g.stack, items...)
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(max))))
	case 11:
		if len(g.stack) < 2 || g.stack[len(g.stack)-2].kind != parityProgramTuple || len(g.stack[len(g.stack)-2].tuple) >= 254 {
			return false
		}
		g.emit("TPUSH", tupleop.TPUSH().Serialize())
		val := parityProgramCloneValue(g.stack[len(g.stack)-1])
		tup := parityProgramCloneValue(g.stack[len(g.stack)-2])
		tup.tuple = append(tup.tuple, val)
		g.stack = g.stack[:len(g.stack)-2]
		g.stack = append(g.stack, tup)
	case 12:
		top := g.topTuple()
		if top == nil || len(top.tuple) == 0 {
			return false
		}
		g.emit("TPOP", tupleop.TPOP().Serialize())
		tup := parityProgramCloneValue(*top)
		val := parityProgramCloneValue(tup.tuple[len(tup.tuple)-1])
		tup.tuple = tup.tuple[:len(tup.tuple)-1]
		g.stack[len(g.stack)-1] = tup
		g.stack = append(g.stack, val)
	case 13:
		if len(g.stack) < 2 || g.stack[len(g.stack)-2].kind != parityProgramTuple || len(g.stack[len(g.stack)-2].tuple) == 0 {
			return false
		}
		max := len(g.stack[len(g.stack)-2].tuple)
		if max > 16 {
			max = 16
		}
		idx := g.r.Intn(max)
		g.emit(fmt.Sprintf("SETINDEX(%d)", idx), tupleop.SETINDEX(uint8(idx)).Serialize())
		val := parityProgramCloneValue(g.stack[len(g.stack)-1])
		tup := parityProgramCloneValue(g.stack[len(g.stack)-2])
		tup.tuple[idx] = val
		g.stack = g.stack[:len(g.stack)-2]
		g.stack = append(g.stack, tup)
	default:
		return g.emitTupleExtraOp()
	}
	return true
}

func (g *parityProgramGenerator) emitTupleExtraOp() bool {
	return g.emitTupleExtraOpMode(g.r.Intn(19))
}

func (g *parityProgramGenerator) emitTupleExtraOpMode(mode int) bool {
	base := len(g.stack)
	two := parityProgramStackValue{
		kind: parityProgramTuple,
		tuple: []parityProgramStackValue{
			parityProgramIntValue(big.NewInt(11)),
			parityProgramIntValue(big.NewInt(22)),
		},
	}
	three := parityProgramStackValue{
		kind: parityProgramTuple,
		tuple: []parityProgramStackValue{
			parityProgramIntValue(big.NewInt(11)),
			parityProgramIntValue(big.NewInt(22)),
			parityProgramIntValue(big.NewInt(33)),
		},
	}

	switch mode % 19 {
	case 0:
		g.emitPushInt("tuplevar_0", big.NewInt(11))
		g.emitPushInt("tuplevar_1", big.NewInt(22))
		g.emitPushInt("tuplevar_count", big.NewInt(2))
		g.emit("TUPLEVAR", tupleop.TUPLEVAR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, two)
	case 1:
		g.emitPushTuple("tuple_indexvar", two)
		g.emitPushInt("indexvar_idx", big.NewInt(1))
		g.emit("INDEXVAR", tupleop.INDEXVAR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(22)))
	case 2:
		g.emitPushTuple("tuple_indexvarq", two)
		g.emitPushInt("indexvarq_idx", big.NewInt(1))
		g.emit("INDEXVARQ", tupleop.INDEXVARQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(22)))
	case 3:
		g.emitPushTuple("tuple_untuplevar", two)
		g.emitPushInt("untuplevar_count", big.NewInt(2))
		g.emit("UNTUPLEVAR", tupleop.UNTUPLEVAR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(11)), parityProgramIntValue(big.NewInt(22)))
	case 4:
		g.emitPushTuple("tuple_unpackfirstvar", three)
		g.emitPushInt("unpackfirstvar_count", big.NewInt(2))
		g.emit("UNPACKFIRSTVAR", tupleop.UNPACKFIRSTVAR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(11)), parityProgramIntValue(big.NewInt(22)))
	case 5:
		g.emitPushTuple("tuple_explodevar", two)
		g.emitPushInt("explodevar_max", big.NewInt(2))
		g.emit("EXPLODEVAR", tupleop.EXPLODEVAR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(11)), parityProgramIntValue(big.NewInt(22)), parityProgramIntValue(big.NewInt(2)))
	case 6:
		nested := parityProgramStackValue{
			kind:  parityProgramTuple,
			tuple: []parityProgramStackValue{two},
		}
		g.emitPushTuple("tuple_index2", nested)
		g.emit("INDEX2(0,1)", tupleop.INDEX2(0, 1).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(22)))
	case 7:
		nested := parityProgramStackValue{
			kind: parityProgramTuple,
			tuple: []parityProgramStackValue{
				{kind: parityProgramTuple, tuple: []parityProgramStackValue{two}},
			},
		}
		g.emitPushTuple("tuple_index3", nested)
		g.emit("INDEX3(0,0,1)", tupleop.INDEX3(0, 0, 1).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(22)))
	case 8:
		g.emitPushTuple("tuple_setindexq", two)
		g.emitPushInt("setindexq_value", big.NewInt(99))
		g.emit("SETINDEXQ(1)", tupleop.SETINDEXQ(1).Serialize())
		next := parityProgramCloneValue(two)
		next.tuple[1] = parityProgramIntValue(big.NewInt(99))
		g.stack = g.stack[:base]
		g.stack = append(g.stack, next)
	case 9:
		g.emitPushTuple("tuple_setindexvar", two)
		g.emitPushInt("setindexvar_value", big.NewInt(99))
		g.emitPushInt("setindexvar_idx", big.NewInt(1))
		g.emit("SETINDEXVAR", tupleop.SETINDEXVAR().Serialize())
		next := parityProgramCloneValue(two)
		next.tuple[1] = parityProgramIntValue(big.NewInt(99))
		g.stack = g.stack[:base]
		g.stack = append(g.stack, next)
	case 10:
		g.emitPushTuple("tuple_setindexvarq", two)
		g.emitPushInt("setindexvarq_value", big.NewInt(99))
		g.emitPushInt("setindexvarq_idx", big.NewInt(1))
		g.emit("SETINDEXVARQ", tupleop.SETINDEXVARQ().Serialize())
		next := parityProgramCloneValue(two)
		next.tuple[1] = parityProgramIntValue(big.NewInt(99))
		g.stack = g.stack[:base]
		g.stack = append(g.stack, next)
	case 11:
		g.emitPushInt("nullswapif_cond", big.NewInt(1))
		g.emit("NULLSWAPIF", tupleop.NULLSWAPIF().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramNull}, parityProgramIntValue(big.NewInt(1)))
	case 12:
		g.emitPushInt("nullswapifnot_cond", big.NewInt(0))
		g.emit("NULLSWAPIFNOT", tupleop.NULLSWAPIFNOT().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramNull}, parityProgramIntValue(big.NewInt(0)))
	case 13:
		g.emitPushInt("nullrotrif_payload", big.NewInt(7))
		g.emitPushInt("nullrotrif_cond", big.NewInt(1))
		g.emit("NULLROTRIF", tupleop.NULLROTRIF().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramNull}, parityProgramIntValue(big.NewInt(7)), parityProgramIntValue(big.NewInt(1)))
	case 14:
		g.emitPushInt("nullswapif2_cond", big.NewInt(1))
		g.emit("NULLSWAPIF2", tupleop.NULLSWAPIF2().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramNull}, parityProgramStackValue{kind: parityProgramNull}, parityProgramIntValue(big.NewInt(1)))
	case 15:
		g.emitPushInt("nullrotrifnot_payload", big.NewInt(7))
		g.emitPushInt("nullrotrifnot_cond", big.NewInt(0))
		g.emit("NULLROTRIFNOT", tupleop.NULLROTRIFNOT().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramNull}, parityProgramIntValue(big.NewInt(7)), parityProgramIntValue(big.NewInt(0)))
	case 16:
		g.emitPushInt("nullswapifnot2_cond", big.NewInt(0))
		g.emit("NULLSWAPIFNOT2", tupleop.NULLSWAPIFNOT2().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramNull}, parityProgramStackValue{kind: parityProgramNull}, parityProgramIntValue(big.NewInt(0)))
	case 17:
		g.emitPushInt("nullrotrif2_payload", big.NewInt(7))
		g.emitPushInt("nullrotrif2_cond", big.NewInt(1))
		g.emit("NULLROTRIF2", tupleop.NULLROTRIF2().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramNull}, parityProgramStackValue{kind: parityProgramNull}, parityProgramIntValue(big.NewInt(7)), parityProgramIntValue(big.NewInt(1)))
	default:
		g.emitPushInt("nullrotrifnot2_payload", big.NewInt(7))
		g.emitPushInt("nullrotrifnot2_cond", big.NewInt(0))
		g.emit("NULLROTRIFNOT2", tupleop.NULLROTRIFNOT2().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramNull}, parityProgramStackValue{kind: parityProgramNull}, parityProgramIntValue(big.NewInt(7)), parityProgramIntValue(big.NewInt(0)))
	}
	return true
}

func (g *parityProgramGenerator) emitUnaryIntOp(name string, op *cell.Builder, fn func(*big.Int) *big.Int) bool {
	if len(g.stack) == 0 || g.stack[len(g.stack)-1].kind != parityProgramInt {
		return false
	}
	next := parityProgramIntValue(fn(g.stack[len(g.stack)-1].int))
	if !parityProgramValueIsSafe(next) {
		return false
	}
	g.emit(name, op)
	g.stack[len(g.stack)-1] = next
	return true
}

func (g *parityProgramGenerator) emitUnaryIntToSmallOp(name string, op *cell.Builder) bool {
	if len(g.stack) == 0 || g.stack[len(g.stack)-1].kind != parityProgramInt {
		return false
	}
	g.emit(name, op)
	g.stack[len(g.stack)-1] = parityProgramIntValue(big.NewInt(0))
	return true
}

func (g *parityProgramGenerator) emitBinaryIntOp(name string, op *cell.Builder, fn func(x, y *big.Int) []parityProgramStackValue) bool {
	if len(g.stack) < 2 || g.stack[len(g.stack)-1].kind != parityProgramInt || g.stack[len(g.stack)-2].kind != parityProgramInt {
		return false
	}
	x := new(big.Int).Set(g.stack[len(g.stack)-2].int)
	y := new(big.Int).Set(g.stack[len(g.stack)-1].int)
	next := fn(x, y)
	if !parityProgramStackIsSafe(next) {
		return false
	}
	g.emit(name, op)
	g.stack = g.stack[:len(g.stack)-2]
	g.stack = append(g.stack, next...)
	return true
}

func (g *parityProgramGenerator) emitBinaryIntToSmallOp(name string, op *cell.Builder) bool {
	return g.emitBinaryIntOp(name, op, func(_, _ *big.Int) []parityProgramStackValue {
		return []parityProgramStackValue{parityProgramIntValue(big.NewInt(0))}
	})
}

func (g *parityProgramGenerator) emit(name string, op *cell.Builder) {
	g.ops = append(g.ops, op)
	g.trace = append(g.trace, name)
}

func (g *parityProgramGenerator) emitPushInt(name string, v *big.Int) {
	g.emit(fmt.Sprintf("PUSHINT(%s:%s)", name, v.String()), stackop.PUSHINT(v).Serialize())
	g.stack = append(g.stack, parityProgramIntValue(v))
}

func (g *parityProgramGenerator) emitPushNaN(name string) {
	g.emit("PUSHNAN("+name+")", mathop.PUSHNAN().Serialize())
	g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramNaN})
}

func (g *parityProgramGenerator) emitPushNull(name string) {
	g.emit("PUSHNULL("+name+")", tupleop.PUSHNULL().Serialize())
	g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramNull})
}

func (g *parityProgramGenerator) emitPushCell(name string, cl *cell.Cell) {
	g.emit("PUSHREF("+name+")", stackop.PUSHREF(cl).Serialize())
	g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramCell, cell: cl})
}

func (g *parityProgramGenerator) emitPushSlice(name string, sl *cell.Slice) {
	g.emit("PUSHSLICE("+name+")", stackop.PUSHSLICE(sl).Serialize())
	g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: sl.Copy()})
}

func (g *parityProgramGenerator) emitPushTuple(name string, tup parityProgramStackValue) {
	base := len(g.stack)
	for i := range tup.tuple {
		g.emitPushLiteral(fmt.Sprintf("%s_%d", name, i), tup.tuple[i])
	}
	g.emit(fmt.Sprintf("TUPLE(%s:%d)", name, len(tup.tuple)), tupleop.TUPLE(uint8(len(tup.tuple))).Serialize())
	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramCloneValue(tup))
}

func (g *parityProgramGenerator) emitPushLiteral(name string, val parityProgramStackValue) {
	switch val.kind {
	case parityProgramInt:
		g.emitPushInt(name, val.int)
	case parityProgramNull:
		g.emitPushNull(name)
	case parityProgramTuple:
		g.emitPushTuple(name, val)
	default:
		panic("unsupported parity program tuple literal")
	}
}

func (g *parityProgramGenerator) topTuple() *parityProgramStackValue {
	if len(g.stack) == 0 || g.stack[len(g.stack)-1].kind != parityProgramTuple {
		return nil
	}
	return &g.stack[len(g.stack)-1]
}

func (g *parityProgramGenerator) randomStackIndex(limit int) int {
	n := len(g.stack)
	if n > limit {
		n = limit
	}
	return g.r.Intn(n)
}

func (g *parityProgramGenerator) swapDepth(a, b int) {
	ai := len(g.stack) - 1 - a
	bi := len(g.stack) - 1 - b
	g.stack[ai], g.stack[bi] = g.stack[bi], g.stack[ai]
}

func (g *parityProgramGenerator) moveDepthToTop(depth int) {
	idx := len(g.stack) - 1 - depth
	val := g.stack[idx]
	copy(g.stack[idx:], g.stack[idx+1:])
	g.stack[len(g.stack)-1] = val
}

func (g *parityProgramGenerator) moveTopToDepth(depth int) {
	if depth == 0 {
		return
	}
	idx := len(g.stack) - 1 - depth
	val := g.stack[len(g.stack)-1]
	copy(g.stack[idx+1:], g.stack[idx:len(g.stack)-1])
	g.stack[idx] = val
}

func (g *parityProgramGenerator) reverseDepthRange(from, to int) {
	for l, r := len(g.stack)-from, len(g.stack)-to-1; l < r; l, r = l+1, r-1 {
		g.stack[l], g.stack[r] = g.stack[r], g.stack[l]
	}
}

func (g *parityProgramGenerator) dropMany(num, offs int) {
	if num == 0 {
		return
	}
	end := len(g.stack)
	copy(g.stack[end-(num+offs):end-num], g.stack[end-offs:end])
	g.stack = g.stack[:end-num]
}

func (g *parityProgramGenerator) blockSwap(x, y int) {
	if x == 0 || y == 0 {
		return
	}
	g.reverseDepthRange(x+y, y)
	g.reverseDepthRange(y, 0)
	g.reverseDepthRange(x+y, 0)
}

func (g *parityProgramGenerator) smallInt() *big.Int {
	edges := []int64{-17, -8, -3, -1, 0, 1, 2, 3, 7, 15, 31}
	if g.r.Intn(4) == 0 {
		return big.NewInt(int64(g.r.Intn(41) - 20))
	}
	return big.NewInt(edges[g.r.Intn(len(edges))])
}

func (g *parityProgramGenerator) randomCell() *cell.Cell {
	return g.randomBuilder().EndCell()
}

func (g *parityProgramGenerator) randomBuilder() *cell.Builder {
	bits := uint([]int{0, 1, 8, 16, 32}[g.r.Intn(5)])
	b := cell.BeginCell()
	if bits > 0 {
		b.MustStoreSlice(matrixPattern(bits), bits)
	}
	for i := 0; i < g.r.Intn(3); i++ {
		b.MustStoreRef(cell.BeginCell().MustStoreUInt(uint64(g.r.Intn(256)), 8).EndCell())
	}
	return b
}

func parityProgramRawOp(value uint64, bits uint) *cell.Builder {
	return cell.BeginCell().MustStoreUInt(value, bits)
}

func parityProgramPfxDictSwitchNilRootWithRef(bits uint64) *cell.Cell {
	return cell.BeginCell().
		MustStoreSlice([]byte{0xF4, 0xAC}, 13).
		MustStoreBoolBit(false).
		MustStoreRef(cell.BeginCell().EndCell()).
		MustStoreUInt(bits, 10).
		EndCell()
}

func parityProgramCodeCell(builders ...*cell.Builder) *cell.Cell {
	code := cell.BeginCell()
	for _, builder := range builders {
		code.MustStoreBuilder(builder)
	}
	return code.EndCell()
}

func parityProgramKeyCell(value uint64, bits uint) *cell.Cell {
	return cell.BeginCell().MustStoreUInt(value, bits).EndCell()
}

func parityProgramKeySlice(value uint64, bits uint) *cell.Slice {
	return parityProgramKeyCell(value, bits).MustBeginParse()
}

func parityProgramBitsSlice(value uint64, bits uint) *cell.Slice {
	return cell.BeginCell().MustStoreUInt(value, bits).ToSlice()
}

func parityProgramByteSlice(data []byte) *cell.Slice {
	return cell.BeginCell().MustStoreSlice(data, uint(len(data)*8)).ToSlice()
}

func parityProgramDecodeLEInt(data []byte, unsigned bool) *big.Int {
	if unsigned {
		if len(data) == 4 {
			return new(big.Int).SetUint64(uint64(binary.LittleEndian.Uint32(data)))
		}
		return new(big.Int).SetUint64(binary.LittleEndian.Uint64(data))
	}
	if len(data) == 4 {
		return big.NewInt(int64(int32(binary.LittleEndian.Uint32(data))))
	}
	return big.NewInt(int64(binary.LittleEndian.Uint64(data)))
}

func parityProgramEncodeLEInt(v *big.Int, bytesLen int, unsigned bool) []byte {
	out := make([]byte, bytesLen)
	if unsigned {
		if bytesLen == 4 {
			binary.LittleEndian.PutUint32(out, uint32(v.Uint64()))
		} else {
			binary.LittleEndian.PutUint64(out, v.Uint64())
		}
		return out
	}
	if bytesLen == 4 {
		binary.LittleEndian.PutUint32(out, uint32(int32(v.Int64())))
	} else {
		binary.LittleEndian.PutUint64(out, uint64(v.Int64()))
	}
	return out
}

func parityProgramRefSlice() (*cell.Slice, *cell.Cell, *cell.Cell) {
	first := cell.BeginCell().MustStoreUInt(0xA, 4).EndCell()
	second := cell.BeginCell().MustStoreUInt(0xB, 4).EndCell()
	return cell.BeginCell().
		MustStoreUInt(0b101101, 6).
		MustStoreRef(first).
		MustStoreRef(second).
		ToSlice(), first, second
}

func parityProgramDictRoot(byRef bool) (*cell.Cell, *cell.Cell, *cell.Cell) {
	key := parityProgramKeyCell(0x12, 8)
	value := cell.BeginCell().MustStoreUInt(0x34, 8).EndCell()
	ref := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	dict := cell.NewDict(8)

	var err error
	if byRef {
		_, err = dict.SetBuilderWithMode(key, cell.BeginCell().MustStoreRef(ref), cell.DictSetModeSet)
	} else {
		_, err = dict.SetWithMode(key, value, cell.DictSetModeSet)
	}
	if err != nil {
		panic(err)
	}
	return dict.AsCell(), value, ref
}

func parityProgramDictContinuationRoot(signed bool, result int64) *cell.Cell {
	value := parityProgramCodeCell(stackop.PUSHINT(big.NewInt(result)).Serialize())
	dict := cell.NewDict(8)
	var err error
	if signed {
		err = dict.SetIntKey(big.NewInt(3), value)
	} else {
		_, err = dict.SetWithMode(parityProgramKeyCell(3, 8), value, cell.DictSetModeSet)
	}
	if err != nil {
		panic(err)
	}
	return dict.AsCell()
}

func parityProgramPrefixContinuationRoot(result int64) *cell.Cell {
	value := parityProgramCodeCell(stackop.PUSHINT(big.NewInt(result)).Serialize())
	dict := cell.NewPrefixDict(4)
	if _, err := dict.SetWithMode(parityProgramKeyCell(0b10, 2), value, cell.DictSetModeSet); err != nil {
		panic(err)
	}
	return dict.AsCell()
}

func parityProgramRootAsDict(root *cell.Cell, bits uint) *cell.Dictionary {
	if root == nil {
		return cell.NewDict(bits)
	}
	return root.AsDict(bits)
}

func parityProgramDictKeyCellForKind(kind int, value uint64, bits uint) *cell.Cell {
	if kind == 1 {
		return cell.BeginCell().MustStoreBigInt(big.NewInt(int64(value)), bits).EndCell()
	}
	return parityProgramKeyCell(value, bits)
}

func parityProgramDictScalarOpcode(base uint64, kind int) uint64 {
	switch kind {
	case 1:
		return base + 1
	case 2:
		return base + 2
	default:
		return base
	}
}

func parityProgramDictValueOpcode(base uint64, kind int, byRef bool) uint64 {
	offset := uint64(0)
	switch kind {
	case 1:
		offset = 2
	case 2:
		offset = 4
	}
	if byRef {
		offset++
	}
	return base + offset
}

func parityProgramDictScalarName(kind int, suffix string) string {
	switch kind {
	case 1:
		return "DICTI" + suffix
	case 2:
		return "DICTU" + suffix
	default:
		return "DICT" + suffix
	}
}

func parityProgramDictValueName(kind int, suffix string, byRef bool) string {
	name := parityProgramDictScalarName(kind, suffix)
	if byRef {
		name += "REF"
	}
	return name
}

func parityProgramDictNearName(kind int, fetchNext, allowEq bool) string {
	suffix := "GETPREV"
	if fetchNext {
		suffix = "GETNEXT"
	}
	if allowEq {
		suffix += "EQ"
	}
	return parityProgramDictScalarName(kind, suffix)
}

func parityProgramDictKeyValue(kind int, value uint64, bits uint) parityProgramStackValue {
	if kind == 0 {
		return parityProgramStackValue{kind: parityProgramSlice, slice: parityProgramKeySlice(value, bits)}
	}
	return parityProgramIntValue(big.NewInt(int64(value)))
}

func parityProgramDictBuilderSetNameOpcode(mode cell.DictSetMode, kind int) (string, uint64) {
	switch mode {
	case cell.DictSetModeReplace:
		return parityProgramDictScalarName(kind, "REPLACEB"), parityProgramDictScalarOpcode(0xF449, kind)
	case cell.DictSetModeAdd:
		return parityProgramDictScalarName(kind, "ADDB"), parityProgramDictScalarOpcode(0xF451, kind)
	default:
		return parityProgramDictScalarName(kind, "SETB"), parityProgramDictScalarOpcode(0xF441, kind)
	}
}

func parityProgramDictBuilderSetGetNameOpcode(mode cell.DictSetMode, kind int) (string, uint64) {
	switch mode {
	case cell.DictSetModeReplace:
		return parityProgramDictScalarName(kind, "REPLACEGETB"), parityProgramDictScalarOpcode(0xF44D, kind)
	case cell.DictSetModeAdd:
		return parityProgramDictScalarName(kind, "ADDGETB"), parityProgramDictScalarOpcode(0xF455, kind)
	default:
		return parityProgramDictScalarName(kind, "SETGETB"), parityProgramDictScalarOpcode(0xF445, kind)
	}
}

func parityProgramMultiDictRoot() (*cell.Cell, map[uint64]*cell.Cell) {
	values := map[uint64]*cell.Cell{
		0x10: cell.BeginCell().MustStoreUInt(0x10, 8).EndCell(),
		0x20: cell.BeginCell().MustStoreUInt(0x20, 8).EndCell(),
		0x30: cell.BeginCell().MustStoreUInt(0x30, 8).EndCell(),
	}
	dict := cell.NewDict(8)
	for key, value := range values {
		if _, err := dict.SetWithMode(parityProgramKeyCell(key, 8), value, cell.DictSetModeSet); err != nil {
			panic(err)
		}
	}
	return dict.AsCell(), values
}

func parityProgramSignedDictRoot() (*cell.Cell, map[int64]*cell.Cell) {
	values := map[int64]*cell.Cell{
		-2: cell.BeginCell().MustStoreUInt(0x22, 8).EndCell(),
		3:  cell.BeginCell().MustStoreUInt(0x33, 8).EndCell(),
	}
	dict := cell.NewDict(8)
	for key, value := range values {
		if err := dict.SetIntKey(big.NewInt(key), value); err != nil {
			panic(err)
		}
	}
	return dict.AsCell(), values
}

func parityProgramSubdictRoot() *cell.Cell {
	dict := cell.NewDict(8)
	items := map[uint64]uint64{
		0b01100000: 0x60,
		0b10100000: 0xA0,
		0b10110000: 0xB0,
	}
	for key, value := range items {
		if _, err := dict.SetWithMode(parityProgramKeyCell(key, 8), cell.BeginCell().MustStoreUInt(value, 8).EndCell(), cell.DictSetModeSet); err != nil {
			panic(err)
		}
	}
	return dict.AsCell()
}

func parityProgramPrefixDictRoot() (*cell.Cell, *cell.Cell) {
	value := cell.BeginCell().MustStoreUInt(0xD, 4).EndCell()
	dict := cell.NewPrefixDict(4)
	if _, err := dict.SetWithMode(parityProgramKeyCell(0b10, 2), value, cell.DictSetModeSet); err != nil {
		panic(err)
	}
	return dict.AsCell(), value
}

func parityProgramStorageStatCell() *cell.Cell {
	return cell.BeginCell().
		MustStoreUInt(0xAB, 8).
		MustStoreRef(cell.BeginCell().EndCell()).
		EndCell()
}

func parityProgramSharedRefStorageStatCell() *cell.Cell {
	ref := cell.BeginCell().MustStoreUInt(0xD, 4).EndCell()
	return cell.BeginCell().
		MustStoreRef(ref).
		MustStoreRef(ref).
		EndCell()
}

func parityProgramMetaBuilder() *cell.Builder {
	return cell.BeginCell().
		MustStoreUInt(0xAB, 8).
		MustStoreRef(cell.BeginCell().EndCell())
}

func parityProgramFullRefsBuilder() *cell.Builder {
	return cell.BeginCell().
		MustStoreRef(cell.BeginCell().MustStoreUInt(0, 1).EndCell()).
		MustStoreRef(cell.BeginCell().MustStoreUInt(1, 1).EndCell()).
		MustStoreRef(cell.BeginCell().MustStoreUInt(0, 1).EndCell()).
		MustStoreRef(cell.BeginCell().MustStoreUInt(1, 1).EndCell())
}

func parityProgramFullBitsBuilder() *cell.Builder {
	builder := cell.BeginCell()
	if err := builder.StoreSameBit(false, 1023); err != nil {
		panic(err)
	}
	return builder
}

func parityProgramAddrNoneSlice() *cell.Slice {
	return cell.BeginCell().MustStoreUInt(0, 2).ToSlice()
}

func parityProgramExtAddrSlice() *cell.Slice {
	return cell.BeginCell().MustStoreAddr(address.NewAddressExt(0, 256, bytes.Repeat([]byte{0x33}, 32))).ToSlice()
}

func parityProgramExtAddrDataSlice() *cell.Slice {
	return cell.BeginCell().MustStoreSlice(bytes.Repeat([]byte{0x33}, 32), 256).ToSlice()
}

func parityProgramExtAddrTailSlice() *cell.Slice {
	return cell.BeginCell().
		MustStoreBuilder(parityProgramExtAddrSlice().ToBuilder()).
		MustStoreUInt(0xA, 4).
		ToSlice()
}

func parityProgramStdAddrSlice() *cell.Slice {
	return cell.BeginCell().MustStoreAddr(crossTestAddr).ToSlice()
}

func parityProgramStdAddrDataSlice() *cell.Slice {
	return cell.BeginCell().MustStoreSlice(crossTestAddr.Data(), 256).ToSlice()
}

func parityProgramTailSlice() *cell.Slice {
	return cell.BeginCell().MustStoreUInt(0xA, 4).ToSlice()
}

func parityProgramStdAddrTailSlice() *cell.Slice {
	return cell.BeginCell().
		MustStoreBuilder(parityProgramStdAddrSlice().ToBuilder()).
		MustStoreUInt(0xA, 4).
		ToSlice()
}

func parityProgramAddrNoneTailSlice() *cell.Slice {
	return cell.BeginCell().MustStoreUInt(0, 2).MustStoreUInt(0xA, 4).ToSlice()
}

func parityVersionedMessageAddressSlice(seed uint64, mode int, short bool) *cell.Slice {
	if short {
		return cell.BeginCell().MustStoreUInt(0b10, 2).ToSlice()
	}

	switch mode % 8 {
	case 0:
		return parityProgramAddrNoneSlice()
	case 1:
		return parityProgramExtAddrSlice()
	case 2:
		return parityProgramStdAddrSlice()
	case 3:
		return parityVersionedStdAnycastAddrSlice(seed, 3)
	case 4:
		return parityVersionedVarAddrSlice(seed, false, 0, 256)
	case 5:
		return parityVersionedVarAddrSlice(seed, true, 5, 20)
	case 6:
		return parityVersionedStdAnycastAddrSlice(seed, 31)
	default:
		return cell.BeginCell().MustStoreUInt(0b11, 2).MustStoreBoolBit(true).MustStoreUInt(31, 5).ToSlice()
	}
}

func parityVersionedMessageAddressName(addr *cell.Slice) string {
	cl := addr.ToBuilder().EndCell()
	hash := cl.Hash()
	return fmt.Sprintf("addr_bits_%d_%x", addr.BitsLeft(), hash[:4])
}

func parityVersionedMessageAddressWithTail(addr *cell.Slice) *cell.Slice {
	return cell.BeginCell().
		MustStoreBuilder(addr.ToBuilder()).
		MustStoreUInt(0xA, 4).
		ToSlice()
}

func parityVersionedStdAnycastAddrSlice(seed uint64, depth uint) *cell.Slice {
	builder := cell.BeginCell().
		MustStoreUInt(0b10, 2).
		MustStoreBoolBit(true).
		MustStoreUInt(uint64(depth), 5)
	if depth <= 30 {
		builder.MustStoreSlice(parityVersionedAddrData(seed), depth)
	}
	return builder.
		MustStoreInt(int64(crossTestAddr.Workchain()), 8).
		MustStoreSlice(crossTestAddr.Data(), 256).
		ToSlice()
}

func parityVersionedVarAddrSlice(seed uint64, anycast bool, depth uint, bits uint) *cell.Slice {
	builder := cell.BeginCell().
		MustStoreUInt(0b11, 2).
		MustStoreBoolBit(anycast)
	if anycast {
		builder.MustStoreUInt(uint64(depth), 5)
		if depth <= 30 {
			builder.MustStoreSlice(parityVersionedAddrData(seed), depth)
		}
	}
	return builder.
		MustStoreUInt(uint64(bits), 9).
		MustStoreInt(int64(crossTestAddr.Workchain()), 32).
		MustStoreSlice(parityVersionedAddrData(seed+1), bits).
		ToSlice()
}

func parityVersionedAddrData(seed uint64) []byte {
	data := make([]byte, 32)
	binary.BigEndian.PutUint64(data, seed)
	binary.BigEndian.PutUint64(data[8:], seed*0x9e3779b97f4a7c15)
	copy(data[16:], crossTestAddr.Data()[:16])
	return data
}

func parityProgramParsedNoneAddrTuple() parityProgramStackValue {
	return parityProgramStackValue{
		kind:  parityProgramTuple,
		tuple: []parityProgramStackValue{parityProgramIntValue(big.NewInt(0))},
	}
}

func parityProgramParsedExtAddrTuple() parityProgramStackValue {
	return parityProgramStackValue{
		kind: parityProgramTuple,
		tuple: []parityProgramStackValue{
			parityProgramIntValue(big.NewInt(1)),
			{kind: parityProgramSlice, slice: parityProgramExtAddrDataSlice()},
		},
	}
}

func parityProgramParsedStdAddrTuple() parityProgramStackValue {
	return parityProgramStackValue{
		kind: parityProgramTuple,
		tuple: []parityProgramStackValue{
			parityProgramIntValue(big.NewInt(2)),
			{kind: parityProgramNull},
			parityProgramIntValue(big.NewInt(int64(crossTestAddr.Workchain()))),
			{kind: parityProgramSlice, slice: parityProgramStdAddrDataSlice()},
		},
	}
}

func parityProgramIntValue(v *big.Int) parityProgramStackValue {
	return parityProgramStackValue{kind: parityProgramInt, int: new(big.Int).Set(v)}
}

func parityProgramBoolValue(v bool) parityProgramStackValue {
	if v {
		return parityProgramIntValue(big.NewInt(-1))
	}
	return parityProgramIntValue(big.NewInt(0))
}

func parityProgramMaybeCellValue(cl *cell.Cell) parityProgramStackValue {
	if cl == nil {
		return parityProgramStackValue{kind: parityProgramNull}
	}
	return parityProgramStackValue{kind: parityProgramCell, cell: cl}
}

func parityProgramStackIsSafe(stack []parityProgramStackValue) bool {
	for _, val := range stack {
		if !parityProgramValueIsSafe(val) {
			return false
		}
	}
	return true
}

func parityProgramValueIsSafe(val parityProgramStackValue) bool {
	if val.kind == parityProgramInt && val.int.BitLen() > 220 {
		return false
	}
	return true
}

func parityProgramCloneStack(src []parityProgramStackValue) []parityProgramStackValue {
	dst := make([]parityProgramStackValue, len(src))
	for i := range src {
		dst[i] = parityProgramCloneValue(src[i])
	}
	return dst
}

func parityProgramCloneValue(src parityProgramStackValue) parityProgramStackValue {
	dst := parityProgramStackValue{kind: src.kind}
	if src.int != nil {
		dst.int = new(big.Int).Set(src.int)
	}
	if src.tuple != nil {
		dst.tuple = parityProgramCloneStack(src.tuple)
	}
	if src.cell != nil {
		dst.cell = src.cell
	}
	if src.slice != nil {
		dst.slice = src.slice.Copy()
	}
	if src.builder != nil {
		dst.builder = src.builder.Copy()
	}
	return dst
}

func parityProgramHostStack(src []parityProgramStackValue) []any {
	stack := make([]any, len(src))
	for i := range src {
		stack[i] = parityProgramHostValue(src[i])
	}
	return stack
}

func parityProgramHostValue(src parityProgramStackValue) any {
	switch src.kind {
	case parityProgramInt:
		return new(big.Int).Set(src.int)
	case parityProgramNaN:
		return vm.NaN{}
	case parityProgramNull:
		return nil
	case parityProgramTuple:
		items := make([]any, len(src.tuple))
		for i := range src.tuple {
			items[i] = parityProgramHostValue(src.tuple[i])
		}
		return tuple.NewTupleValue(items...)
	case parityProgramCell:
		return src.cell
	case parityProgramSlice:
		return src.slice.Copy()
	case parityProgramBuilder:
		return src.builder.Copy()
	default:
		panic("unknown parity program stack kind")
	}
}

func parityFuzzDataSizeArg(t *testing.T, r *rand.Rand, sliceArg bool) any {
	t.Helper()

	if sliceArg {
		if r.Intn(4) == 0 {
			return parityFuzzWrongValue(t, r)
		}
		return parityFuzzSlice(t, r)
	}
	if r.Intn(5) == 0 {
		return parityFuzzWrongValue(t, r)
	}
	if r.Intn(5) == 0 {
		return nil
	}
	return parityFuzzGraphCell(t, r)
}

func parityFuzzBound(t *testing.T, r *rand.Rand) any {
	t.Helper()

	switch r.Intn(10) {
	case 0:
		return vm.NaN{}
	case 1:
		return int64(-1)
	case 2:
		return new(big.Int).Lsh(big.NewInt(1), 200)
	case 3:
		return parityFuzzWrongValue(t, r)
	default:
		return int64([]int{0, 1, 2, 3, 10}[r.Intn(5)])
	}
}

func parityFuzzWidth(r *rand.Rand) any {
	switch r.Intn(12) {
	case 0:
		return vm.NaN{}
	case 1:
		return int64(-1)
	case 2:
		return int64(257)
	case 3:
		return int64(258)
	case 4:
		return int64(1024)
	default:
		return int64([]int{0, 1, 7, 8, 16, 255, 256}[r.Intn(7)])
	}
}

func parityFuzzStoreInt(r *rand.Rand) any {
	switch r.Intn(9) {
	case 0:
		return vm.NaN{}
	case 1:
		return int64(-129)
	case 2:
		return int64(-1)
	case 3:
		return int64(0)
	case 4:
		return int64(1)
	case 5:
		return int64(255)
	case 6:
		return int64(256)
	default:
		return big.NewInt(int64(r.Intn(256)))
	}
}

func parityFuzzMathValue(t *testing.T, r *rand.Rand) any {
	t.Helper()

	switch r.Intn(16) {
	case 0:
		return vm.NaN{}
	case 1:
		return parityFuzzWrongValue(t, r)
	case 2:
		return int64(-1)
	case 3:
		return int64(0)
	case 4:
		return int64(1)
	case 5:
		return int64(255)
	case 6:
		return int64(-256)
	case 7:
		return new(big.Int).Lsh(big.NewInt(1), 200)
	case 8:
		return new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 200))
	default:
		return big.NewInt(int64(r.Intn(4097) - 2048))
	}
}

func parityFuzzMathShift(t *testing.T, r *rand.Rand) any {
	t.Helper()

	switch r.Intn(12) {
	case 0:
		return vm.NaN{}
	case 1:
		return parityFuzzWrongValue(t, r)
	case 2:
		return int64(-1)
	case 3:
		return int64(0)
	case 4:
		return int64(1)
	case 5:
		return int64(1023)
	case 6:
		return int64(1024)
	default:
		return int64(r.Intn(1050) - 10)
	}
}

func parityFuzzMaybeCell(t *testing.T, r *rand.Rand) any {
	t.Helper()

	if r.Intn(5) == 0 {
		return parityFuzzWrongValue(t, r)
	}
	return parityFuzzGraphCell(t, r)
}

func parityFuzzMaybeSlice(t *testing.T, r *rand.Rand) any {
	t.Helper()

	if r.Intn(5) == 0 {
		return parityFuzzWrongValue(t, r)
	}
	return parityFuzzSlice(t, r)
}

func parityFuzzMaybeBuilder(t *testing.T, r *rand.Rand) any {
	t.Helper()

	if r.Intn(5) == 0 {
		return parityFuzzWrongValue(t, r)
	}
	return matrixBuilder(t, uint([]int{0, 1, 8, 1016, 1023}[r.Intn(5)]), r.Intn(5))
}

func parityFuzzWrongValue(t *testing.T, r *rand.Rand) any {
	t.Helper()

	switch r.Intn(5) {
	case 0:
		return int64(777)
	case 1:
		return parityFuzzSlice(t, r)
	case 2:
		return parityFuzzGraphCell(t, r)
	case 3:
		return cell.BeginCell()
	default:
		return nil
	}
}

func parityFuzzSlice(t *testing.T, r *rand.Rand) *cell.Slice {
	t.Helper()

	return parityFuzzGraphCell(t, r).MustBeginParse()
}

func parityFuzzGraphCell(t *testing.T, r *rand.Rand) *cell.Cell {
	t.Helper()

	bits := uint([]int{0, 1, 7, 8, 16, 255, 512, 1023}[r.Intn(8)])
	refs := r.Intn(5)
	b := cell.BeginCell()
	if bits > 0 {
		b.MustStoreSlice(matrixPattern(bits), bits)
	}

	if refs == 0 {
		return b.EndCell()
	}

	shared := cell.BeginCell().MustStoreUInt(uint64(r.Intn(256)), 8).EndCell()
	for i := 0; i < refs; i++ {
		if i%2 == 0 {
			b.MustStoreRef(shared)
		} else {
			b.MustStoreRef(cell.BeginCell().MustStoreUInt(uint64(i), 8).EndCell())
		}
	}
	return b.EndCell()
}
