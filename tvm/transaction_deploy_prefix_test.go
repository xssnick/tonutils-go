package tvm

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

// TON ac605b3b21d010a4e97b1ff15560c5be1a0e398b, transaction.cpp:
// check_in_msg_state_hash calls Account::recompute_tmp_addr before v10;
// compute_state continues to serialize account.my_addr, not the temporary one.
func TestTransactionDeploymentPrefixAddress(t *testing.T) {
	for _, version := range []uint32{0, 9, 10} {
		for _, depth := range []uint64{0, 1, 7, 30, 31} {
			for _, badSuffix := range []bool{false, true} {
				t.Run(fmt.Sprintf("v%d/depth%d/bad_suffix%t", version, depth, badSuffix), func(t *testing.T) {
					state := &tlb.StateInit{Depth: &depth, Code: cell.BeginCell().EndCell()}
					stateCell, err := tlb.ToCell(state)
					if err != nil {
						t.Fatal(err)
					}
					addrData := append([]byte(nil), stateCell.Hash()...)
					if depth > 0 {
						addrData[0] ^= 0x80
					}
					if badSuffix {
						addrData[31] ^= 1
					}
					addr := address.NewAddress(0, 0, addrData)
					acc := &transactionRuntimeAccount{addr: addr, status: tlb.AccountStatusUninit}
					message := &tlb.Message{MsgType: tlb.MsgTypeInternal, Msg: &tlb.InternalMessage{DstAddr: addr, StateInit: state}}
					cfg := transactionTestConfigWithGlobalVersion(t, version)
					next, activated, skip, err := transactionPrepareComputeAccount(acc, acc.status, false, message, false, cfg, false, false)
					if err != nil {
						t.Fatal(err)
					}
					wantSkip := badSuffix || depth > 30 || version >= 10 && depth > transactionGetSizeLimits(cfg).maxAccFixedPrefixLength
					if wantSkip {
						if activated || skip == nil || skip.Type != tlb.ComputeSkipReasonBadState || next.addrVM != nil {
							t.Fatalf("bad deployment changed execution address: activated=%t skip=%+v", activated, skip)
						}
						return
					}
					if !activated || skip != nil {
						t.Fatalf("valid deployment skipped: activated=%t skip=%+v", activated, skip)
					}
					want := addr
					if depth > 0 && version < 10 {
						want = address.NewAddress(0, 0, stateCell.Hash()).WithAnycast(address.NewAnycast(uint(depth), addrData))
					}
					gotCell := cell.BeginCell().MustStoreAddr(next.vmAddress(version)).EndCell()
					wantCell := cell.BeginCell().MustStoreAddr(want).EndCell()
					if !bytes.Equal(gotCell.Hash(), wantCell.Hash()) {
						t.Fatal("incorrect temporary deployment address")
					}
					if acc.addrVM != nil || next.rawAddress() != addr || next.exactAddress() != addr {
						t.Fatal("temporary address changed stored or effective account identity")
					}
					if version >= 10 && next.addrVM != nil {
						t.Fatal("modern deployment retained a temporary anycast address")
					}
				})
			}
		}
	}
}
