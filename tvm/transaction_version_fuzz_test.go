package tvm

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	vmcore "github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

var transactionFuzzAllVersions = func() []uint32 {
	versions := make([]uint32, 0, MaxSupportedGlobalVersion-MinSupportedGlobalVersion+1)
	for version := uint32(MinSupportedGlobalVersion); version <= uint32(MaxSupportedGlobalVersion); version++ {
		versions = append(versions, version)
	}
	return versions
}()

func transactionFuzzGlobalVersion(raw byte) uint32 {
	return uint32(tvmFuzzGlobalVersionByte(raw))
}

func TestTransactionFuzzGlobalVersionCoversSupportedRange(t *testing.T) {
	wantLen := MaxSupportedGlobalVersion - MinSupportedGlobalVersion + 1
	if len(transactionFuzzAllVersions) != wantLen {
		t.Fatalf("transaction fuzz versions len = %d, want %d", len(transactionFuzzAllVersions), wantLen)
	}
	if MaxSupportedGlobalVersion > 255 {
		t.Fatalf("transaction fuzz raw byte cannot cover max global version %d", MaxSupportedGlobalVersion)
	}

	for i, got := range transactionFuzzAllVersions {
		want := uint32(MinSupportedGlobalVersion + i)
		if got != want {
			t.Fatalf("transaction fuzz version[%d] = %d, want %d", i, got, want)
		}
		if mapped := transactionFuzzGlobalVersion(byte(want)); mapped != want {
			t.Fatalf("version seed %d mapped to %d, want %d", want, mapped, want)
		}
	}
}

func FuzzTransactionVersionedAnycastAccountSerialization(f *testing.F) {
	f.Add(byte(1), []byte{0x80}, bytes.Repeat([]byte{0x11}, 32))
	f.Add(byte(3), []byte{0xA0}, bytes.Repeat([]byte{0x22}, 32))
	f.Add(byte(30), []byte{0xFF, 0xFF, 0xFF, 0xFC}, bytes.Repeat([]byte{0x33}, 32))

	f.Fuzz(func(t *testing.T, depthByte byte, prefixInput []byte, addrInput []byte) {
		depth := uint(depthByte%30) + 1
		prefixLen := int((depth + 7) / 8)
		if len(prefixInput) < prefixLen || len(addrInput) < 32 {
			return
		}

		prefix := append([]byte(nil), prefixInput[:prefixLen]...)
		if rem := depth % 8; rem != 0 {
			prefix[len(prefix)-1] &= 0xFF << (8 - rem)
		}
		addrData := append([]byte(nil), addrInput[:32]...)
		addr := address.NewAddress(0, 0, addrData).WithAnycast(address.NewAnycast(depth, prefix))

		rewritten, err := transactionRewrittenAccountAddressData(addr)
		if err != nil {
			t.Fatal(err)
		}

		idAddr, err := transactionAccountIDAddr(addr)
		if err != nil {
			t.Fatal(err)
		}
		if idAddr.Anycast() != nil || !bytes.Equal(idAddr.Data(), rewritten) {
			t.Fatalf("account id addr mismatch: anycast=%v data=%x want %x", idAddr.Anycast(), idAddr.Data(), rewritten)
		}

		seed, err := transactionEmulationSeed(bytes.Repeat([]byte{0x44}, 32), addr, 8)
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.New()
		hash.Write(bytes.Repeat([]byte{0x44}, 32))
		hash.Write(rewritten)
		if seed.Cmp(new(big.Int).SetBytes(hash.Sum(nil))) != 0 {
			t.Fatal("transaction seed did not use rewritten account address")
		}

		checkAccountSerializationVersion(t, addr, addrData, rewritten, depth, 8)
		checkAccountSerializationVersion(t, addr, addrData, rewritten, depth, 9)
		checkAccountSerializationVersion(t, addr, addrData, rewritten, depth, 10)
	})
}

func FuzzTransactionVersionedOutboundAnycastDestination(f *testing.F) {
	for _, version := range transactionFuzzAllVersions {
		f.Add(byte(version), byte(1), []byte{0x80})
	}
	f.Add(byte(MinSupportedGlobalVersion), byte(1), []byte{0x00})
	f.Add(byte(9), byte(1), []byte{0x00})
	f.Add(byte(9), byte(3), []byte{0xE0})
	f.Add(byte(10), byte(1), []byte{0x80})
	f.Add(byte(MaxSupportedGlobalVersion), byte(30), []byte{0xFF, 0xFF, 0xFF, 0xFC})

	f.Fuzz(func(t *testing.T, rawVersion, rawDepth byte, rawPrefix []byte) {
		version := transactionFuzzGlobalVersion(rawVersion)
		depth := uint(rawDepth%30) + 1
		prefixLen := int((depth + 7) / 8)
		if len(rawPrefix) < prefixLen {
			return
		}
		prefix := append([]byte(nil), rawPrefix[:prefixLen]...)
		if rem := depth % 8; rem != 0 {
			prefix[len(prefix)-1] &= 0xFF << (8 - rem)
		}

		dst := tonopsTestAddr.WithAnycast(address.NewAnycast(depth, prefix))
		msgCell := buildTransactionOutboundInternalCellWithAddresses(t, address.NewAddressNone(), dst, 100, cell.BeginCell().EndCell())
		normalized, err := transactionNormalizeOutboundMessage(msgCell, tonopsTestAddr, 10, uint32(tonopsTestTime.Unix()), transactionTestConfigWithGlobalVersion(t, version))
		if version >= 10 {
			if !errors.Is(err, errTransactionInvalidDestination) {
				t.Fatalf("v%d normalize error = %v, want invalid destination", version, err)
			}
			return
		}
		if err != nil {
			t.Fatalf("v%d normalize failed: %v", version, err)
		}

		var msg tlb.Message
		if err = transactionParseCell(&msg, normalized); err != nil {
			t.Fatal(err)
		}
		gotAnycast := msg.AsInternal().DstAddr.Anycast()
		if gotAnycast == nil {
			t.Fatalf("v%d normalized destination has no anycast", version)
		}
		if gotAnycast.Depth() != depth {
			t.Fatalf("v%d anycast depth = %d, want %d", version, gotAnycast.Depth(), depth)
		}
		wantPrefix := transactionAddressPrefix(tonopsTestAddr.Data(), depth)
		if !bytes.Equal(gotAnycast.Prefix(), wantPrefix) {
			t.Fatalf("v%d anycast prefix = %x, want %x", version, gotAnycast.Prefix(), wantPrefix)
		}
	})
}

func FuzzTransactionVersionedInternalAndBounceAnycastValidation(f *testing.F) {
	for _, version := range transactionFuzzAllVersions {
		f.Add(byte(version), byte(1), []byte{0x80})
	}
	f.Add(byte(0), byte(1), []byte{0x00})
	f.Add(byte(9), byte(3), []byte{0xE0})
	f.Add(byte(10), byte(1), []byte{0x80})
	f.Add(byte(MaxSupportedGlobalVersion), byte(30), []byte{0xFF, 0xFF, 0xFF, 0xFC})

	f.Fuzz(func(t *testing.T, rawVersion, rawDepth byte, rawPrefix []byte) {
		version := transactionFuzzGlobalVersion(rawVersion)
		depth := uint(rawDepth%30) + 1
		prefixLen := int((depth + 7) / 8)
		if len(rawPrefix) < prefixLen {
			return
		}
		prefix := append([]byte(nil), rawPrefix[:prefixLen]...)
		if rem := depth % 8; rem != 0 {
			prefix[len(prefix)-1] &= 0xFF << (8 - rem)
		}

		dst := tonopsTestAddr.WithAnycast(address.NewAnycast(depth, prefix))
		cfg := transactionTestConfigWithGlobalVersion(t, version)
		wantPrefix := transactionAddressPrefix(tonopsTestAddr.Data(), depth)

		internal, ok := transactionValidateAndNormalizeInternalDestAddr(dst, cfg, tonopsTestAddr)
		if version >= 10 {
			if ok || internal != nil {
				t.Fatalf("v%d internal anycast validation succeeded, want rejection", version)
			}
		} else {
			if !ok {
				t.Fatalf("v%d internal anycast validation failed", version)
			}
			checkTransactionFuzzNormalizedAnycast(t, version, "internal", internal, depth, wantPrefix)
		}

		bounce, ok := transactionValidateAndNormalizeBounceDestAddr(dst, cfg, tonopsTestAddr)
		if !ok {
			t.Fatalf("v%d bounce anycast validation failed", version)
		}
		checkTransactionFuzzNormalizedAnycast(t, version, "bounce", bounce, depth, wantPrefix)
	})
}

func checkTransactionFuzzNormalizedAnycast(t *testing.T, version uint32, name string, addr *address.Address, depth uint, wantPrefix []byte) {
	t.Helper()

	if addr.Type() != address.StdAddress {
		t.Fatalf("v%d %s normalized type = %d, want std", version, name, addr.Type())
	}
	if addr.Workchain() != tonopsTestAddr.Workchain() {
		t.Fatalf("v%d %s normalized workchain = %d, want %d", version, name, addr.Workchain(), tonopsTestAddr.Workchain())
	}

	gotAnycast := addr.Anycast()
	if gotAnycast == nil {
		t.Fatalf("v%d %s normalized address has no anycast", version, name)
	}
	if gotAnycast.Depth() != depth {
		t.Fatalf("v%d %s anycast depth = %d, want %d", version, name, gotAnycast.Depth(), depth)
	}
	if !bytes.Equal(gotAnycast.Prefix(), wantPrefix) {
		t.Fatalf("v%d %s anycast prefix = %x, want %x", version, name, gotAnycast.Prefix(), wantPrefix)
	}
}

func FuzzTransactionVersionedActionFineStartsAtV4(f *testing.F) {
	f.Add(uint64(10), byte(6))
	f.Add(uint64(1), byte(1))
	f.Add(uint64(32), byte(31))

	priceCell, err := tlb.ToCell(&tlb.ConfigMsgForwardPrices{CellPrice: 4 << 16})
	if err != nil {
		f.Fatalf("failed to build message forward prices: %v", err)
	}

	f.Fuzz(func(t *testing.T, rawRemaining uint64, rawExtraCells byte) {
		remaining := rawRemaining%32 + 1
		bodyCells := int(remaining) + int(rawExtraCells%32) + 4
		msgCell := buildTransactionOutboundInternalCellWithBody(t, remaining, transactionTestCellChain(bodyCells))
		v3Cfg := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
			tlb.ConfigParamGlobalVersion:               transactionTestGlobalVersionCell(t, 3),
			tlb.ConfigParamMsgForwardPricesBasechain:   priceCell,
			tlb.ConfigParamMsgForwardPricesMasterchain: priceCell,
		})
		v4Cfg := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
			tlb.ConfigParamGlobalVersion:               transactionTestGlobalVersionCell(t, 4),
			tlb.ConfigParamMsgForwardPricesBasechain:   priceCell,
			tlb.ConfigParamMsgForwardPricesMasterchain: priceCell,
		})

		v3 := applyTransactionSendActionForTestWithParams(t,
			tlb.ActionSendMsg{Mode: 0, Msg: msgCell},
			v3Cfg,
			new(big.Int).SetUint64(remaining),
			nil,
			transactionZeroCurrencyBalance(),
		)
		if v3.phase == nil || v3.phase.Success || v3.phase.ResultCode != 37 {
			t.Fatalf("unexpected v3 action phase: %+v", v3.phase)
		}
		if v3.phase.TotalActionFees != nil {
			t.Fatalf("v3 action fees = %s, want nil", v3.phase.TotalActionFees.Nano())
		}
		if v3.balance.Uint64() != remaining {
			t.Fatalf("v3 balance = %s, want %d", v3.balance, remaining)
		}

		v4 := applyTransactionSendActionForTestWithParams(t,
			tlb.ActionSendMsg{Mode: 0, Msg: msgCell},
			v4Cfg,
			new(big.Int).SetUint64(remaining),
			nil,
			transactionZeroCurrencyBalance(),
		)
		if v4.phase == nil || v4.phase.Success || v4.phase.ResultCode != 40 {
			t.Fatalf("unexpected v4 action phase: %+v", v4.phase)
		}
		if v4.phase.TotalActionFees == nil || v4.phase.TotalActionFees.Nano().Uint64() != remaining {
			t.Fatalf("v4 action fees = %v, want %d", v4.phase.TotalActionFees, remaining)
		}
		if v4.balance.Sign() != 0 {
			t.Fatalf("v4 balance = %s, want 0", v4.balance)
		}
	})
}

func FuzzTransactionVersionedMessageFeeWrappersUseTailUsage(f *testing.F) {
	f.Add(byte(0x03), byte(0), byte(8), byte(12), byte(0), byte(4), uint64(10), uint64(2), uint32(0), uint16(0), uint64(100))
	f.Add(byte(0x33), byte(7), byte(0), byte(31), byte(3), byte(7), uint64(11), uint64(17), uint32(1<<15), uint16(1<<15), uint64(1))
	f.Add(byte(0x8f), byte(14), byte(16), byte(5), byte(2), byte(3), uint64(0), uint64(0), uint32(0), uint16(0xffff), uint64(0))
	f.Add(byte(0x48), byte(9), byte(63), byte(63), byte(1), byte(6), uint64(123), uint64(45), uint32(0xffff), uint16(0x4000), uint64(7))

	f.Fuzz(func(t *testing.T, rawFlags, rawVersion, rawRootBits, rawLeafBits, rawBranchBits, rawCellUnits byte, rawLump, rawBitPrice uint64, rawIHRFactor uint32, rawFirstFrac uint16, rawAvailable uint64) {
		leaf := transactionFuzzPayloadCell(rawFlags, uint(rawLeafBits%64))
		branch := cell.BeginCell()
		if bits := uint(rawBranchBits % 32); bits > 0 {
			branch.MustStoreUInt(transactionFuzzPayloadValue(rawFlags>>1, bits), bits)
		}
		branchCell := branch.MustStoreRef(leaf).EndCell()

		var refs []*cell.Cell
		if rawFlags&1 != 0 {
			refs = append(refs, leaf)
		}
		if rawFlags&2 != 0 {
			refs = append(refs, branchCell)
		}
		if rawFlags&4 != 0 {
			refs = append(refs, leaf)
		}

		root := cell.BeginCell()
		if bits := uint(rawRootBits % 64); bits > 0 {
			root.MustStoreUInt(transactionFuzzPayloadValue(rawFlags, bits), bits)
		}
		for _, ref := range refs {
			root.MustStoreRef(ref)
		}
		msgCell := root.EndCell()

		collector := newTransactionUsageCollector()
		tailUsage := transactionUsage{}
		for _, ref := range refs {
			refUsage, err := collector.addCell(ref, false)
			if err != nil {
				t.Fatal(err)
			}
			tailUsage = transactionAddUsage(tailUsage, refUsage)
		}

		cellUnits := uint64(rawCellUnits % 8)
		basePrices := tlb.ConfigMsgForwardPrices{
			LumpPrice: rawLump % 1000,
			BitPrice:  (rawBitPrice%1000 + 1) << 8,
			CellPrice: cellUnits << 16,
			IHRFactor: rawIHRFactor & 0xffff,
			FirstFrac: rawFirstFrac,
		}
		masterPrices := tlb.ConfigMsgForwardPrices{
			LumpPrice: basePrices.LumpPrice + 17,
			BitPrice:  basePrices.BitPrice + 23,
			CellPrice: basePrices.CellPrice + 2<<16,
			IHRFactor: (basePrices.IHRFactor + 0x1111) & 0xffff,
			FirstFrac: basePrices.FirstFrac ^ 0x5555,
		}

		var available *big.Int
		if rawFlags&0x40 == 0 {
			available = new(big.Int).SetUint64(rawAvailable % 1000)
		}

		srcAddr := tonopsTestAddr
		dstAddr := tonopsTestAddr
		if rawFlags&0x10 != 0 {
			srcAddr = address.NewAddress(0, 0xff, bytes.Repeat([]byte{0x91}, 32))
		}
		if rawFlags&0x20 != 0 {
			dstAddr = address.NewAddress(0, 0xff, bytes.Repeat([]byte{0xA7}, 32))
		}

		for _, version := range transactionFuzzAllVersions {
			var prices *tlb.ConfigMsgForwardPrices
			cfg := transactionTestConfigWithGlobalVersion(t, version)
			if rawFlags&0x08 == 0 {
				baseCell, err := tlb.ToCell(&basePrices)
				if err != nil {
					t.Fatal(err)
				}
				masterCell, err := tlb.ToCell(&masterPrices)
				if err != nil {
					t.Fatal(err)
				}
				cfg = transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
					tlb.ConfigParamGlobalVersion:               transactionTestGlobalVersionCell(t, version),
					tlb.ConfigParamMsgForwardPricesBasechain:   baseCell,
					tlb.ConfigParamMsgForwardPricesMasterchain: masterCell,
				})
				prices = &basePrices
				if transactionIsMasterchain(srcAddr) || transactionIsMasterchain(dstAddr) {
					prices = &masterPrices
				}
			}

			wantForward := big.NewInt(0)
			if prices != nil {
				wantForward = prices.ComputeForwardFee(tailUsage.cells, tailUsage.bits)
			}
			gotForward, err := transactionComputeForwardFeeForMessage(cfg, srcAddr, dstAddr, msgCell)
			if err != nil {
				t.Fatalf("v%d forward fee failed: %v", version, err)
			}
			if gotForward.Cmp(wantForward) != 0 {
				t.Fatalf("v%d forward fee = %s, want %s for usage %+v", version, gotForward, wantForward, tailUsage)
			}
			if got := transactionComputeForwardFeeForUsage(cfg, srcAddr, dstAddr, tailUsage); got.Cmp(wantForward) != 0 {
				t.Fatalf("v%d forward fee by usage = %s, want %s", version, got, wantForward)
			}

			wantFine := big.NewInt(0)
			if prices != nil {
				finePerCell := (prices.CellPrice >> 16) / 4
				if finePerCell > 0 {
					fineCells := tailUsage.cells
					if available != nil {
						maxFineCells := new(big.Int).Div(new(big.Int).Set(available), new(big.Int).SetUint64(finePerCell))
						if !maxFineCells.IsUint64() {
							fineCells = 0
						} else if maxFineCells.Uint64() < fineCells {
							fineCells = maxFineCells.Uint64()
						}
					}
					wantFine = new(big.Int).Mul(new(big.Int).SetUint64(finePerCell), new(big.Int).SetUint64(fineCells))
				}
			}
			gotFine, err := transactionComputeActionFine(cfg, srcAddr, dstAddr, msgCell, available)
			if err != nil {
				t.Fatalf("v%d action fine failed: %v", version, err)
			}
			if gotFine.Cmp(wantFine) != 0 {
				t.Fatalf("v%d action fine = %s, want %s for usage %+v available %v", version, gotFine, wantFine, tailUsage, available)
			}
			if got := transactionComputeActionFineForUsage(cfg, srcAddr, dstAddr, tailUsage, available); got.Cmp(wantFine) != 0 {
				t.Fatalf("v%d action fine by usage = %s, want %s", version, got, wantFine)
			}

			wantIHR := big.NewInt(0)
			ihrDisabled := rawFlags&0x80 != 0
			if prices != nil && !ihrDisabled && wantForward.Sign() > 0 && prices.IHRFactor != 0 {
				wantIHR.SetUint64(uint64(prices.IHRFactor))
				wantIHR.Mul(wantIHR, wantForward)
				wantIHR.Rsh(wantIHR, 16)
			}
			if got := transactionComputeIHRFee(cfg, srcAddr, dstAddr, wantForward, ihrDisabled); got.Cmp(wantIHR) != 0 {
				t.Fatalf("v%d IHR fee = %s, want %s", version, got, wantIHR)
			}
			if got := transactionComputeIHRFee(cfg, srcAddr, dstAddr, nil, false); got.Sign() != 0 {
				t.Fatalf("v%d nil forward IHR fee = %s, want 0", version, got)
			}

			wantFirst := big.NewInt(0)
			if prices != nil && wantForward.Sign() > 0 && prices.FirstFrac != 0 {
				wantFirst.SetUint64(uint64(prices.FirstFrac))
				wantFirst.Mul(wantFirst, wantForward)
				wantFirst.Rsh(wantFirst, 16)
			}
			if got := transactionFirstPartForwardFee(cfg, srcAddr, dstAddr, wantForward); got.Cmp(wantFirst) != 0 {
				t.Fatalf("v%d first forward fee = %s, want %s", version, got, wantFirst)
			}
		}
	})
}

func FuzzTransactionVersionedBasicSuccessAcrossGlobalVersions(f *testing.F) {
	for _, version := range transactionFuzzAllVersions {
		f.Add(byte(version), byte(0x11), byte(0x22))
	}
	f.Add(byte(0), byte(0x11), byte(0x22))
	f.Add(byte(7), byte(0x33), byte(0x44))
	f.Add(byte(MaxSupportedGlobalVersion), byte(0x55), byte(0x66))

	f.Fuzz(func(t *testing.T, rawVersion, dataTag, bodyTag byte) {
		version := transactionFuzzGlobalVersion(rawVersion)
		now := uint32(tonopsTestTime.Unix())
		origData := cell.BeginCell().MustStoreUInt(uint64(dataTag), 8).EndCell()
		newData := cell.BeginCell().MustStoreUInt(uint64(dataTag)^0xff, 8).EndCell()
		code := makeTransactionInternalSuccessCode(t, newData)
		msg, err := tlb.ToCell(&tlb.InternalMessage{
			IHRDisabled: true,
			Bounce:      true,
			SrcAddr:     internalEmulationSrcAddr,
			DstAddr:     tonopsTestAddr,
			Amount:      tlb.FromNanoTONU(1_000_000_000),
			Body:        cell.BeginCell().MustStoreUInt(uint64(bodyTag), 8).EndCell(),
		})
		if err != nil {
			t.Fatalf("v%d failed to build message: %v", version, err)
		}
		cfg := transactionTestConfigWithGlobalVersion(t, version)
		shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)

		res, err := NewTVM().EmulateTransaction(shard, msg, TransactionEmulationConfig{
			Address:     tonopsTestAddr,
			Now:         now,
			BlockLT:     transactionTestLogicalTime,
			LogicalTime: transactionTestLogicalTime,
			RandSeed:    append([]byte(nil), tonopsTestSeed...),
			ConfigRoot:  cfg.Root,
		})
		if err != nil {
			t.Fatalf("v%d transaction emulation failed: %v", version, err)
		}
		if res.TransactionCell == nil || res.ShardAccountCell == nil || res.AccountState == nil {
			t.Fatalf("v%d transaction result has nil cells/state: %+v", version, res)
		}
		if res.AccountState.StateInit == nil || res.AccountState.StateInit.Data == nil || !bytes.Equal(res.AccountState.StateInit.Data.Hash(), newData.Hash()) {
			t.Fatalf("v%d final data mismatch", version)
		}

		var tx tlb.Transaction
		if err = tlb.LoadFromCell(&tx, res.TransactionCell.MustBeginParse()); err != nil {
			t.Fatalf("v%d failed to decode transaction: %v", version, err)
		}
		desc, ok := tx.Description.(tlb.TransactionDescriptionOrdinary)
		if !ok {
			t.Fatalf("v%d transaction description = %T, want ordinary", version, tx.Description)
		}
		compute, ok := desc.ComputePhase.Phase.(tlb.ComputePhaseVM)
		if !ok || !compute.Success {
			t.Fatalf("v%d compute phase = %+v, want successful VM", version, desc.ComputePhase)
		}
		if desc.ActionPhase == nil || !desc.ActionPhase.Success || !desc.ActionPhase.Valid || desc.Aborted {
			t.Fatalf("v%d unexpected action/abort: action=%+v aborted=%t", version, desc.ActionPhase, desc.Aborted)
		}
	})
}

func FuzzTransactionVersionedComputePhaseExitArgAndBits256(f *testing.F) {
	f.Add(byte(0), byte(0), int64(0), byte(0x11))
	f.Add(byte(1), byte(1), int64(1), byte(0x22))
	f.Add(byte(31), byte(2), int64(-1), byte(0x33))
	f.Add(byte(32), byte(3), int64(1<<31-1), byte(0x44))
	f.Add(byte(33), byte(4), int64(-1<<31), byte(0x55))
	f.Add(byte(48), byte(5), int64(1<<31), byte(0x66))
	f.Add(byte(7), byte(6), int64(-1<<31-1), byte(0x77))
	f.Add(byte(64), byte(7), int64(17), byte(0x88))
	f.Add(byte(9), byte(7), int64(0), byte(0x99))
	f.Add(byte(10), byte(7), int64(1<<31-1), byte(0xAA))
	f.Add(byte(11), byte(7), int64(-1<<31), byte(0xBB))
	f.Add(byte(12), byte(7), int64(1<<31), byte(0xCC))
	f.Add(byte(13), byte(7), int64(-1<<31-1), byte(0xDD))

	f.Fuzz(func(t *testing.T, rawLen, rawCase byte, rawExitArg int64, rawSeed byte) {
		raw := transactionFuzzHashBytes(rawLen, rawSeed)
		gotBits := transactionBits256(raw)
		wantBits := transactionNormalizeBits256(raw)
		if !bytes.Equal(gotBits, wantBits) {
			t.Fatalf("transactionBits256(%x) = %x, want %x", raw, gotBits, wantBits)
		}
		if len(gotBits) != 32 {
			t.Fatalf("transactionBits256 len = %d, want 32", len(gotBits))
		}

		res, wantArg := transactionFuzzComputeExitArgResult(t, rawCase, rawExitArg)
		gotArg := transactionComputeExitArg(res)
		if !transactionFuzzInt32PtrEqual(gotArg, wantArg) {
			t.Fatalf("case=%d exit arg = %v, want %v", rawCase%8, transactionFuzzInt32PtrString(gotArg), transactionFuzzInt32PtrString(wantArg))
		}
		if res == nil {
			return
		}

		for _, version := range transactionFuzzAllVersions {
			phase := buildTransactionComputePhase(transactionBuildDescriptionParams{
				msg: &tlb.Message{
					MsgType: tlb.MsgTypeInternal,
					Msg:     &tlb.InternalMessage{},
				},
				computeResult: res,
				computeGas: vmcore.Gas{
					Limit: int64(version) + 100,
				},
				gasFees: big.NewInt(int64(version)),
			})
			vmPhase, ok := phase.Phase.(tlb.ComputePhaseVM)
			if !ok {
				t.Fatalf("v%d compute phase = %T, want VM", version, phase.Phase)
			}
			if !transactionFuzzInt32PtrEqual(vmPhase.Details.ExitArg, wantArg) {
				t.Fatalf("v%d compute phase exit arg = %v, want %v", version, transactionFuzzInt32PtrString(vmPhase.Details.ExitArg), transactionFuzzInt32PtrString(wantArg))
			}
			if gotLimit := vmPhase.Details.GasLimit.Int64(); gotLimit != int64(version)+100 {
				t.Fatalf("v%d gas limit = %d, want %d", version, gotLimit, int64(version)+100)
			}
		}
	})
}

func FuzzTransactionVersionedTickTockSuccessAcrossGlobalVersions(f *testing.F) {
	for _, version := range transactionFuzzAllVersions {
		f.Add(byte(version), false, byte(0x11), byte(0x22))
		f.Add(byte(version), true, byte(0x33), byte(0x44))
	}
	f.Add(byte(0), false, byte(0x11), byte(0x22))
	f.Add(byte(7), true, byte(0x33), byte(0x44))
	f.Add(byte(MaxSupportedGlobalVersion), false, byte(0x55), byte(0x66))

	f.Fuzz(func(t *testing.T, rawVersion byte, isTock bool, dataTag, bodyTag byte) {
		version := transactionFuzzGlobalVersion(rawVersion)
		now := uint32(tonopsTestTime.Unix())
		origData := cell.BeginCell().MustStoreUInt(uint64(dataTag), 8).EndCell()
		tickData := cell.BeginCell().MustStoreUInt(uint64(dataTag)^0x11, 8).EndCell()
		tockData := cell.BeginCell().MustStoreUInt(uint64(dataTag)^uint64(bodyTag)^0x22, 8).EndCell()
		code := transactionFuzzTickTockStateOnlyCode(t, tickData, tockData)
		cfg := transactionTestConfigWithGlobalVersion(t, version)
		shard, err := buildTickTockShardAccountForTest(t, tickTockTestAddr, code, origData, tickTockTestBalance)
		if err != nil {
			t.Fatalf("v%d failed to build tick/tock shard: %v", version, err)
		}

		res, err := NewTVM().EmulateTickTockTransaction(shard, isTock, TransactionEmulationConfig{
			Now:        now,
			Balance:    new(big.Int).SetUint64(tickTockTestBalance),
			RandSeed:   append([]byte(nil), tonopsTestSeed...),
			ConfigRoot: cfg.Root,
		})
		if err != nil {
			t.Fatalf("v%d tick/tock emulation failed: %v", version, err)
		}
		if res.AccountState == nil || res.AccountState.StateInit == nil || res.AccountState.StateInit.Data == nil {
			t.Fatalf("v%d tick/tock result has nil account state: %+v", version, res)
		}
		wantData := tickData
		if isTock {
			wantData = tockData
		}
		if !bytes.Equal(res.AccountState.StateInit.Data.Hash(), wantData.Hash()) {
			t.Fatalf("v%d tick/tock final data mismatch", version)
		}

		var tx tlb.Transaction
		if err = tlb.LoadFromCell(&tx, res.TransactionCell.MustBeginParse()); err != nil {
			t.Fatalf("v%d failed to decode tick/tock transaction: %v", version, err)
		}
		desc, ok := tx.Description.(tlb.TransactionDescriptionTickTock)
		if !ok {
			t.Fatalf("v%d transaction description = %T, want tick/tock", version, tx.Description)
		}
		if desc.IsTock != isTock {
			t.Fatalf("v%d tick/tock flag = %t, want %t", version, desc.IsTock, isTock)
		}
		compute, ok := desc.ComputePhase.Phase.(tlb.ComputePhaseVM)
		if !ok || !compute.Success {
			t.Fatalf("v%d tick/tock compute phase = %+v, want successful VM", version, desc.ComputePhase)
		}
		if desc.ActionPhase == nil || !desc.ActionPhase.Success || !desc.ActionPhase.Valid || desc.Aborted {
			t.Fatalf("v%d unexpected tick/tock action/abort: action=%+v aborted=%t", version, desc.ActionPhase, desc.Aborted)
		}
	})
}

func FuzzTickTockBuildProofLibraryCodeCellStartupV9Boundary(f *testing.F) {
	f.Add(byte(0xA4), byte(0xB4), byte(0xC4), false)
	f.Add(byte(0xA5), byte(0xB5), byte(0xC5), true)
	f.Add(byte(0), byte(1), byte(2), false)
	f.Add(byte(0xff), byte(0x80), byte(0x7f), true)

	f.Fuzz(func(t *testing.T, origTag, tickTag, tockTag byte, isTock bool) {
		origData := cell.BeginCell().MustStoreUInt(uint64(origTag), 8).EndCell()
		tickData := cell.BeginCell().MustStoreUInt(uint64(tickTag), 8).EndCell()
		tockData := cell.BeginCell().MustStoreUInt(uint64(tockTag), 8).EndCell()
		targetCode := transactionFuzzTickTockStateOnlyCode(t, tickData, tockData)
		code := mustLibraryCellForHash(t, targetCode.Hash())
		libraries := mustLibraryCollection(t, targetCode)
		shard, err := buildTickTockShardAccountForTest(t, tickTockTestAddr, code, origData, tickTockTestBalance)
		if err != nil {
			t.Fatalf("failed to build tick/tock shard: %v", err)
		}

		missing := runTickTockBuildProofLibraryStartup(t, 9, shard, isTock)
		if missing.ExitCode != vmerr.CodeCellUnderflow {
			t.Fatalf("tick/tock proof missing v9 library exit = %d, want cell underflow", missing.ExitCode)
		}

		legacy := runTickTockBuildProofLibraryStartup(t, 8, shard, isTock, libraries)
		direct := runTickTockBuildProofLibraryStartup(t, 9, shard, isTock, libraries)
		wantData := tickData
		if isTock {
			wantData = tockData
		}
		assertTransactionBuildProofLibraryStartupResult(t, "tick/tock v8", legacy, wantData)
		assertTransactionBuildProofLibraryStartupResult(t, "tick/tock v9", direct, wantData)

		if legacy.Steps <= direct.Steps {
			t.Fatalf("tick/tock proof v8 steps = %d, v9 steps = %d; v8 should include an implicit jump through the code ref", legacy.Steps, direct.Steps)
		}
		if legacy.GasUsed <= direct.GasUsed {
			t.Fatalf("tick/tock proof v8 gas = %d, v9 gas = %d; v8 should charge the implicit code ref jump", legacy.GasUsed, direct.GasUsed)
		}
	})
}

func runTickTockBuildProofLibraryStartup(t *testing.T, version uint32, shard *tlb.ShardAccount, isTock bool, libraries ...*cell.Cell) *TransactionExecutionResult {
	t.Helper()

	res, err := NewTVM().EmulateTickTockTransaction(shard, isTock, TransactionEmulationConfig{
		Now:        uint32(tonopsTestTime.Unix()),
		Balance:    new(big.Int).SetUint64(tickTockTestBalance),
		RandSeed:   append([]byte(nil), tonopsTestSeed...),
		ConfigRoot: transactionTestConfigWithGlobalVersion(t, version).Root,
		Gas: vmcore.NewGas(vmcore.GasConfig{
			Max:   DefaultTickTockTransactionGasMax,
			Limit: DefaultTickTockTransactionGasMax,
		}),
		BuildProof: true,
		Libraries:  libraries,
	})
	if err != nil {
		t.Fatalf("tick/tock proof library startup v%d failed: %v", version, err)
	}
	if res == nil {
		t.Fatalf("tick/tock proof library startup v%d returned nil result", version)
	}
	if res.Proof == nil {
		t.Fatalf("tick/tock proof library startup v%d proof is nil", version)
	}
	if _, err = cell.UnwrapProof(res.Proof, shard.Account.Hash()); err != nil {
		t.Fatalf("tick/tock proof library startup v%d proof is invalid: %v", version, err)
	}
	return res
}

func FuzzTickTockChksigAlwaysSucceedPerRun(f *testing.F) {
	for _, version := range transactionFuzzAllVersions {
		f.Add(byte(version), false, byte(0), byte(0x11))
		f.Add(byte(version), true, byte(1), byte(0x22))
	}
	f.Add(byte(0), false, byte(0), byte(0x11))
	f.Add(byte(3), true, byte(2), byte(0x22))
	f.Add(byte(4), false, byte(3), byte(0x33))
	f.Add(byte(MaxSupportedGlobalVersion), true, byte(1), byte(0x44))

	f.Fuzz(func(t *testing.T, rawVersion byte, isTock bool, rawKind, sigTag byte) {
		version := transactionFuzzGlobalVersion(rawVersion)
		tt := executionConfigSignatureCases[int(rawKind)%len(executionConfigSignatureCases)]

		signature := make([]byte, 64)
		signature[0] = sigTag
		signature[31] = sigTag ^ 0x55
		signature[63] = sigTag ^ 0xff

		code := makeTickTockChksigAlwaysVariantCode(t, tt, signature)
		shard, err := buildTickTockShardAccountForTest(t, tickTockTestAddr, code, cell.BeginCell().EndCell(), tickTockTestBalance)
		if err != nil {
			t.Fatalf("failed to build tick/tock shard: %v", err)
		}
		cfg := TransactionEmulationConfig{
			Now:        uint32(tonopsTestTime.Unix()),
			Balance:    new(big.Int).SetUint64(tickTockTestBalance),
			RandSeed:   append([]byte(nil), tonopsTestSeed...),
			ConfigRoot: transactionTestConfigWithGlobalVersion(t, version).Root,
		}

		if tt.minVersion > 0 && version < uint32(tt.minVersion) {
			for _, always := range []bool{false, true, false} {
				res := runTickTockChksigAlwaysVariant(t, NewTVM(), shard, isTock, cfg, tt, always)
				if res.exit != vmerr.CodeInvalidOpcode {
					t.Fatalf("%s v%d always=%t exit=%d, want invalid opcode", tt.name, version, always, res.exit)
				}
			}
			return
		}

		assertMessageChksigAlwaysVariant(t, tt, version, false, runTickTockChksigAlwaysVariant(t, NewTVM(), shard, isTock, cfg, tt, false), false)
		assertMessageChksigAlwaysVariant(t, tt, version, true, runTickTockChksigAlwaysVariant(t, NewTVM(), shard, isTock, cfg, tt, true), true)
		assertMessageChksigAlwaysVariant(t, tt, version, false, runTickTockChksigAlwaysVariant(t, NewTVM(), shard, isTock, cfg, tt, false), false)
	})
}

func transactionFuzzTickTockStateOnlyCode(t *testing.T, tickData, tockData *cell.Cell) *cell.Cell {
	t.Helper()

	tockRef := codeFromBuilders(t,
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.PUSHREF(tockData).Serialize(),
		execop.POPCTR(4).Serialize(),
	)

	return codeFromBuilders(t,
		stackop.DROP().Serialize(),
		execop.IFJMPREF(tockRef).Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.PUSHREF(tickData).Serialize(),
		execop.POPCTR(4).Serialize(),
	)
}

func FuzzMessageEmulationVersionedInMsgParamsDirectMessages(f *testing.F) {
	for _, version := range transactionFuzzAllVersions {
		f.Add(byte(version), true, byte(0x11), byte(0x22))
		f.Add(byte(version), false, byte(0x33), byte(0x44))
	}
	f.Add(byte(0), true, byte(0x11), byte(0x22))
	f.Add(byte(10), true, byte(0x33), byte(0x44))
	f.Add(byte(11), false, byte(0x55), byte(0x66))
	f.Add(byte(MaxSupportedGlobalVersion), false, byte(0x77), byte(0x88))

	f.Fuzz(func(t *testing.T, rawVersion byte, external bool, dataTag, bodyTag byte) {
		version := transactionFuzzGlobalVersion(rawVersion)
		now := uint32(tonopsTestTime.Unix())
		origData := cell.BeginCell().MustStoreUInt(uint64(dataTag), 8).EndCell()
		newData := cell.BeginCell().MustStoreUInt(uint64(dataTag)^uint64(bodyTag)^0xff, 8).EndCell()
		body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 8).EndCell()
		code := makeDirectMessageInMsgParamsCode(t, newData, external)
		cfg := transactionTestConfigWithGlobalVersion(t, version)

		var res *MessageExecutionResult
		var err error
		if external {
			res, err = NewTVM().EmulateExternalMessage(code, origData, &tlb.ExternalMessage{
				DstAddr: tonopsTestAddr,
				Body:    body,
			}, EmulateExternalMessageConfig{
				Address:    tonopsTestAddr,
				Now:        now,
				Balance:    new(big.Int).SetUint64(walletSendTestBalance),
				RandSeed:   append([]byte(nil), walletSendTestSeed...),
				ConfigRoot: cfg.Root,
			})
		} else {
			amount := uint64(bodyTag) + 1
			res, err = NewTVM().EmulateInternalMessage(code, origData, body, amount, EmulateInternalMessageConfig{
				Address:    tonopsTestAddr,
				Now:        now,
				Balance:    new(big.Int).SetUint64(walletSendTestBalance),
				RandSeed:   append([]byte(nil), walletSendTestSeed...),
				ConfigRoot: cfg.Root,
				Gas: vmcore.NewGas(vmcore.GasConfig{
					Max:   DefaultInternalMessageGasMax,
					Limit: int64(amount) * InternalMessageGasAmountFactor,
				}),
			})
		}
		if err != nil {
			t.Fatalf("v%d direct message emulation failed: %v", version, err)
		}

		if version < 11 {
			if res.ExitCode != vmerr.CodeInvalidOpcode {
				t.Fatalf("v%d exit code = %d, want invalid opcode", version, res.ExitCode)
			}
			if external && res.Accepted {
				t.Fatalf("v%d external message accepted before INMSGPARAMS gate", version)
			}
			if !bytes.Equal(res.Data.Hash(), origData.Hash()) {
				t.Fatalf("v%d failed direct message changed data", version)
			}
			if res.Actions != nil {
				t.Fatalf("v%d failed direct message produced actions", version)
			}
			return
		}

		if !res.Accepted {
			t.Fatalf("v%d direct message was not accepted", version)
		}
		if res.ExitCode != 0 {
			t.Fatalf("v%d exit code = %d, want success", version, res.ExitCode)
		}
		if !res.Committed {
			t.Fatalf("v%d direct message was not committed", version)
		}
		if !bytes.Equal(res.Data.Hash(), newData.Hash()) {
			t.Fatalf("v%d direct message final data mismatch", version)
		}
		if messageResultHasNonEmptyActions(res.Actions) {
			t.Fatalf("v%d direct message produced unexpected actions", version)
		}
	})
}

func FuzzMessageEmulationGlobalVersionFallbackAndConfigOverride(f *testing.F) {
	for _, version := range transactionFuzzAllVersions {
		f.Add(byte(version), byte(0), false, true)
		f.Add(byte(0), byte(version), true, false)
	}
	f.Add(byte(3), byte(14), false, true)
	f.Add(byte(4), byte(0), false, false)
	f.Add(byte(14), byte(3), true, true)
	f.Add(byte(3), byte(14), true, false)
	f.Add(byte(0), byte(4), true, true)
	f.Add(byte(MaxSupportedGlobalVersion), byte(0), false, false)

	f.Fuzz(func(t *testing.T, rawMachineVersion, rawConfigVersion byte, useConfigRoot bool, external bool) {
		machineVersion := int(transactionFuzzGlobalVersion(rawMachineVersion))
		configVersion := transactionFuzzGlobalVersion(rawConfigVersion)
		effectiveVersion := machineVersion

		var configRoot *cell.Cell
		if useConfigRoot {
			configRoot = transactionTestConfigWithGlobalVersion(t, configVersion).Root
			effectiveVersion = int(configVersion)
		}

		machine, err := NewTVM().WithGlobalVersion(machineVersion)
		if err != nil {
			t.Fatalf("WithGlobalVersion(%d): %v", machineVersion, err)
		}

		code := makeMessageGasConsumedCode(t)
		data := cell.BeginCell().EndCell()
		body := cell.BeginCell().MustStoreUInt(uint64(rawMachineVersion)^uint64(rawConfigVersion), 8).EndCell()
		cfg := MessageEmulationConfig{
			Address:    tonopsTestAddr,
			Now:        uint32(tonopsTestTime.Unix()),
			Balance:    new(big.Int).SetUint64(walletSendTestBalance),
			RandSeed:   append([]byte(nil), walletSendTestSeed...),
			ConfigRoot: configRoot,
		}

		var res *MessageExecutionResult
		if external {
			res, err = machine.EmulateExternalMessage(code, data, &tlb.ExternalMessage{
				DstAddr: tonopsTestAddr,
				Body:    body,
			}, cfg)
		} else {
			res, err = machine.EmulateInternalMessage(code, data, body, uint64(rawConfigVersion)+1, cfg)
		}
		if err != nil {
			if !useConfigRoot && errors.Is(err, errConfigRootRequired) {
				return
			}
			t.Fatalf("message emulation machine_v=%d config_v=%d use_config=%t external=%t failed: %v", machineVersion, configVersion, useConfigRoot, external, err)
		}
		if !useConfigRoot {
			t.Fatalf("message emulation machine_v=%d config_v=%d external=%t succeeded without config root", machineVersion, configVersion, external)
		}

		if effectiveVersion < 4 {
			if res.ExitCode != vmerr.CodeInvalidOpcode {
				t.Fatalf("effective v%d exit code = %d, want invalid opcode", effectiveVersion, res.ExitCode)
			}
			return
		}

		if res.ExitCode != 0 {
			t.Fatalf("effective v%d exit code = %d, want success", effectiveVersion, res.ExitCode)
		}
		if _, err = res.Stack.PopInt(); err != nil {
			t.Fatalf("effective v%d failed to pop GASCONSUMED result: %v", effectiveVersion, err)
		}
	})
}

func FuzzMessageEmulationBuildProofGlobalVersionFallbackAndConfigOverride(f *testing.F) {
	for _, version := range transactionFuzzAllVersions {
		f.Add(byte(version), byte(0), false, true)
		f.Add(byte(0), byte(version), true, false)
	}
	f.Add(byte(3), byte(14), false, true)
	f.Add(byte(14), byte(3), true, true)
	f.Add(byte(3), byte(14), true, false)
	f.Add(byte(4), byte(0), false, false)
	f.Add(byte(MaxSupportedGlobalVersion), byte(0), true, true)

	f.Fuzz(func(t *testing.T, rawMachineVersion, rawConfigVersion byte, useConfigRoot, external bool) {
		machineVersion := int(transactionFuzzGlobalVersion(rawMachineVersion))
		configVersion := transactionFuzzGlobalVersion(rawConfigVersion)
		effectiveVersion := uint32(machineVersion)

		var configRoot *cell.Cell
		if useConfigRoot {
			configRoot = transactionTestConfigWithGlobalVersion(t, configVersion).Root
			effectiveVersion = configVersion
		}

		machine, err := NewTVM().WithGlobalVersion(machineVersion)
		if err != nil {
			t.Fatalf("WithGlobalVersion(%d): %v", machineVersion, err)
		}

		code := makeMessageGasConsumedCode(t)
		data := cell.BeginCell().EndCell()
		accountRoot := executionProofAccountStateRoot(t, tlb.AccountState{
			IsValid:     true,
			Address:     tonopsTestAddr,
			StorageInfo: executionProofStorageInfo(),
			AccountStorage: tlb.AccountStorage{
				Status:  tlb.AccountStatusActive,
				Balance: tlb.FromNanoTONU(walletSendTestBalance),
				StateInit: &tlb.StateInit{
					Code: code,
					Data: data,
				},
			},
		})
		body := cell.BeginCell().MustStoreUInt(uint64(rawMachineVersion)^uint64(rawConfigVersion)^0xA5, 8).EndCell()
		cfg := MessageEmulationConfig{
			Now:         uint32(tonopsTestTime.Unix()),
			Balance:     new(big.Int).SetUint64(walletSendTestBalance),
			RandSeed:    append([]byte(nil), walletSendTestSeed...),
			ConfigRoot:  configRoot,
			BuildProof:  true,
			AccountRoot: accountRoot,
		}

		var res *MessageExecutionResult
		if external {
			res, err = machine.EmulateExternalMessage(nil, nil, &tlb.ExternalMessage{
				DstAddr: tonopsTestAddr,
				Body:    body,
			}, cfg)
		} else {
			res, err = machine.EmulateInternalMessage(nil, nil, body, uint64(rawConfigVersion)+1, cfg)
		}
		if err != nil {
			if !useConfigRoot && errors.Is(err, errConfigRootRequired) {
				return
			}
			t.Fatalf("message proof emulation machine_v=%d config_v=%d use_config=%t external=%t failed: %v", machineVersion, configVersion, useConfigRoot, external, err)
		}
		if !useConfigRoot {
			t.Fatalf("message proof emulation machine_v=%d config_v=%d external=%t succeeded without config root", machineVersion, configVersion, external)
		}
		if res.Proof == nil {
			t.Fatalf("message proof emulation effective v%d proof is nil", effectiveVersion)
		}
		if _, err = cell.UnwrapProof(res.Proof, accountRoot.Hash()); err != nil {
			t.Fatalf("message proof emulation effective v%d proof is invalid: %v", effectiveVersion, err)
		}

		assertGasConsumedVersionResult(t, "message proof", effectiveVersion, res.ExitCode, res.Stack)
	})
}

func FuzzMessageEmulationBuildProofLibraryCodeCellStartupV9Boundary(f *testing.F) {
	f.Add(byte(0xA4), byte(0xB4), byte(0xC4), false)
	f.Add(byte(0xA5), byte(0xB5), byte(0xC5), true)
	f.Add(byte(0), byte(1), byte(2), false)
	f.Add(byte(0xff), byte(0x80), byte(0x7f), true)

	f.Fuzz(func(t *testing.T, origTag, newTag, bodyTag byte, external bool) {
		origData := cell.BeginCell().MustStoreUInt(uint64(origTag), 8).EndCell()
		newData := cell.BeginCell().MustStoreUInt(uint64(newTag), 8).EndCell()
		body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 8).EndCell()
		targetCode := transactionFuzzMessageLibrarySetDataCode(t, newData, external)
		code := mustLibraryCellForHash(t, targetCode.Hash())
		libraries := mustLibraryCollection(t, targetCode)
		accountRoot := executionProofAccountStateRoot(t, tlb.AccountState{
			IsValid:     true,
			Address:     tonopsTestAddr,
			StorageInfo: executionProofStorageInfo(),
			AccountStorage: tlb.AccountStorage{
				Status:  tlb.AccountStatusActive,
				Balance: tlb.FromNanoTONU(walletSendTestBalance),
				StateInit: &tlb.StateInit{
					Code: code,
					Data: origData,
				},
			},
		})

		missing := runMessageBuildProofLibraryStartup(t, 9, external, accountRoot, body)
		if missing.ExitCode != vmerr.CodeCellUnderflow {
			t.Fatalf("message proof missing v9 library exit = %d, want cell underflow", missing.ExitCode)
		}

		legacy := runMessageBuildProofLibraryStartup(t, 8, external, accountRoot, body, libraries)
		direct := runMessageBuildProofLibraryStartup(t, 9, external, accountRoot, body, libraries)
		assertMessageBuildProofLibraryStartupResult(t, "v8", legacy, newData)
		assertMessageBuildProofLibraryStartupResult(t, "v9", direct, newData)

		if legacy.Steps <= direct.Steps {
			t.Fatalf("message proof v8 steps = %d, v9 steps = %d; v8 should include an implicit jump through the code ref", legacy.Steps, direct.Steps)
		}
		if legacy.GasUsed <= direct.GasUsed {
			t.Fatalf("message proof v8 gas = %d, v9 gas = %d; v8 should charge the implicit code ref jump", legacy.GasUsed, direct.GasUsed)
		}
	})
}

func transactionFuzzMessageLibrarySetDataCode(t *testing.T, newData *cell.Cell, external bool) *cell.Cell {
	t.Helper()

	builders := []*cell.Builder{}
	if external {
		builders = append(builders, funcsop.ACCEPT().Serialize())
	}
	builders = append(builders,
		stackop.PUSHREF(newData).Serialize(),
		execop.POPCTR(4).Serialize(),
	)
	return codeFromBuilders(t, builders...)
}

func runMessageBuildProofLibraryStartup(t *testing.T, version uint32, external bool, accountRoot, body *cell.Cell, libraries ...*cell.Cell) *MessageExecutionResult {
	t.Helper()

	cfg := MessageEmulationConfig{
		Now:         uint32(tonopsTestTime.Unix()),
		Balance:     new(big.Int).SetUint64(walletSendTestBalance),
		RandSeed:    append([]byte(nil), walletSendTestSeed...),
		ConfigRoot:  transactionTestConfigWithGlobalVersion(t, version).Root,
		BuildProof:  true,
		AccountRoot: accountRoot,
		Libraries:   libraries,
	}

	var res *MessageExecutionResult
	var err error
	if external {
		cfg.Gas = vmcore.NewGas(vmcore.GasConfig{
			Max:    DefaultExternalMessageGasMax,
			Credit: DefaultExternalMessageGasCredit,
		})
		res, err = NewTVM().EmulateExternalMessage(nil, nil, &tlb.ExternalMessage{
			DstAddr: tonopsTestAddr,
			Body:    body,
		}, cfg)
	} else {
		cfg.Gas = vmcore.NewGas(vmcore.GasConfig{
			Max:   DefaultInternalMessageGasMax,
			Limit: int64(internalMessageTestAmount) * InternalMessageGasAmountFactor,
		})
		res, err = NewTVM().EmulateInternalMessage(nil, nil, body, internalMessageTestAmount, cfg)
	}
	if err != nil {
		t.Fatalf("message proof library startup v%d external=%t failed: %v", version, external, err)
	}
	if res == nil {
		t.Fatalf("message proof library startup v%d external=%t returned nil result", version, external)
	}
	if res.Proof == nil {
		t.Fatalf("message proof library startup v%d external=%t proof is nil", version, external)
	}
	if _, err = cell.UnwrapProof(res.Proof, accountRoot.Hash()); err != nil {
		t.Fatalf("message proof library startup v%d external=%t proof is invalid: %v", version, external, err)
	}
	return res
}

func assertMessageBuildProofLibraryStartupResult(t *testing.T, name string, res *MessageExecutionResult, wantData *cell.Cell) {
	t.Helper()

	if res.ExitCode != 0 {
		t.Fatalf("%s message proof library startup exit = %d, want success", name, res.ExitCode)
	}
	if res.Data == nil || !bytes.Equal(res.Data.Hash(), wantData.Hash()) {
		t.Fatalf("%s message proof library startup data mismatch", name)
	}
}

func FuzzTransactionEmulationBuildProofLibraryCodeCellStartupV9Boundary(f *testing.F) {
	f.Add(byte(0xA4), byte(0xB4), byte(0xC4))
	f.Add(byte(0), byte(1), byte(2))
	f.Add(byte(0xff), byte(0x80), byte(0x7f))

	f.Fuzz(func(t *testing.T, origTag, newTag, bodyTag byte) {
		now := uint32(tonopsTestTime.Unix())
		origData := cell.BeginCell().MustStoreUInt(uint64(origTag), 8).EndCell()
		newData := cell.BeginCell().MustStoreUInt(uint64(newTag), 8).EndCell()
		targetCode := makeTransactionInternalSuccessCode(t, newData)
		code := mustLibraryCellForHash(t, targetCode.Hash())
		libraries := mustLibraryCollection(t, targetCode)
		shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)
		msgCell, err := tlb.ToCell(&tlb.InternalMessage{
			IHRDisabled: true,
			Bounce:      true,
			SrcAddr:     internalEmulationSrcAddr,
			DstAddr:     tonopsTestAddr,
			Amount:      tlb.FromNanoTONU(1_000_000_000),
			Body:        cell.BeginCell().MustStoreUInt(uint64(bodyTag), 8).EndCell(),
		})
		if err != nil {
			t.Fatalf("failed to build transaction message: %v", err)
		}

		missing := runTransactionBuildProofLibraryStartup(t, 9, shard, msgCell)
		if missing.ExitCode != vmerr.CodeCellUnderflow {
			t.Fatalf("transaction proof missing v9 library exit = %d, want cell underflow", missing.ExitCode)
		}

		legacy := runTransactionBuildProofLibraryStartup(t, 8, shard, msgCell, libraries)
		direct := runTransactionBuildProofLibraryStartup(t, 9, shard, msgCell, libraries)
		assertTransactionBuildProofLibraryStartupResult(t, "v8", legacy, newData)
		assertTransactionBuildProofLibraryStartupResult(t, "v9", direct, newData)

		if legacy.Steps <= direct.Steps {
			t.Fatalf("transaction proof v8 steps = %d, v9 steps = %d; v8 should include an implicit jump through the code ref", legacy.Steps, direct.Steps)
		}
		if legacy.GasUsed <= direct.GasUsed {
			t.Fatalf("transaction proof v8 gas = %d, v9 gas = %d; v8 should charge the implicit code ref jump", legacy.GasUsed, direct.GasUsed)
		}
	})
}

func runTransactionBuildProofLibraryStartup(t *testing.T, version uint32, shard *tlb.ShardAccount, msgCell *cell.Cell, libraries ...*cell.Cell) *TransactionExecutionResult {
	t.Helper()

	res, err := NewTVM().EmulateTransaction(shard, msgCell, TransactionEmulationConfig{
		Address:     tonopsTestAddr,
		Now:         uint32(tonopsTestTime.Unix()),
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  transactionTestConfigWithGlobalVersion(t, version).Root,
		BuildProof:  true,
		Libraries:   libraries,
	})
	if err != nil {
		t.Fatalf("transaction proof library startup v%d failed: %v", version, err)
	}
	if res == nil {
		t.Fatalf("transaction proof library startup v%d returned nil result", version)
	}
	if res.Proof == nil {
		t.Fatalf("transaction proof library startup v%d proof is nil", version)
	}
	if _, err = cell.UnwrapProof(res.Proof, shard.Account.Hash()); err != nil {
		t.Fatalf("transaction proof library startup v%d proof is invalid: %v", version, err)
	}
	return res
}

func assertTransactionBuildProofLibraryStartupResult(t *testing.T, name string, res *TransactionExecutionResult, wantData *cell.Cell) {
	t.Helper()

	if res.ExitCode != 0 {
		t.Fatalf("%s transaction proof library startup exit = %d, want success", name, res.ExitCode)
	}
	if res.AccountState == nil || res.AccountState.StateInit == nil || res.AccountState.StateInit.Data == nil {
		t.Fatalf("%s transaction proof library startup missing account data", name)
	}
	if !bytes.Equal(res.AccountState.StateInit.Data.Hash(), wantData.Hash()) {
		t.Fatalf("%s transaction proof library startup data mismatch", name)
	}
}

func FuzzMessageEmulationBuildProofChksigAlwaysSucceedPerRun(f *testing.F) {
	for version := byte(MinSupportedGlobalVersion); version <= byte(MaxSupportedGlobalVersion); version++ {
		for kind := byte(0); kind < byte(len(executionConfigSignatureCases)); kind++ {
			f.Add(version, kind, version^kind^0x71, false)
			f.Add(version, kind, version^kind^0x8E, true)
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion, rawKind, sigTag byte, external bool) {
		version := transactionFuzzGlobalVersion(rawVersion)
		tt := executionConfigSignatureCases[int(rawKind)%len(executionConfigSignatureCases)]

		signature := make([]byte, 64)
		signature[0] = sigTag
		signature[31] = sigTag ^ 0x33
		signature[63] = sigTag ^ 0xff

		code := makeMessageChksigAlwaysVariantCode(t, tt, signature)
		data := cell.BeginCell().EndCell()
		accountRoot := executionProofAccountStateRoot(t, tlb.AccountState{
			IsValid:     true,
			Address:     tonopsTestAddr,
			StorageInfo: executionProofStorageInfo(),
			AccountStorage: tlb.AccountStorage{
				Status:  tlb.AccountStatusActive,
				Balance: tlb.FromNanoTONU(walletSendTestBalance),
				StateInit: &tlb.StateInit{
					Code: code,
					Data: data,
				},
			},
		})
		body := cell.BeginCell().MustStoreUInt(uint64(sigTag), 8).EndCell()
		cfg := MessageEmulationConfig{
			Now:         uint32(tonopsTestTime.Unix()),
			Balance:     new(big.Int).SetUint64(walletSendTestBalance),
			RandSeed:    append([]byte(nil), walletSendTestSeed...),
			ConfigRoot:  transactionTestConfigWithGlobalVersion(t, version).Root,
			BuildProof:  true,
			AccountRoot: accountRoot,
		}

		if int(version) < tt.minVersion {
			for _, always := range []bool{false, true, false} {
				res := runMessageBuildProofChksigAlwaysVariant(t, NewTVM(), accountRoot, body, cfg, tt, external, always)
				if res.exit != vmerr.CodeInvalidOpcode {
					t.Fatalf("%s proof v%d external=%t always=%t exit=%d, want invalid opcode", tt.name, version, external, always, res.exit)
				}
			}
			return
		}

		assertMessageChksigAlwaysVariant(t, tt, version, false, runMessageBuildProofChksigAlwaysVariant(t, NewTVM(), accountRoot, body, cfg, tt, external, false), false)
		assertMessageChksigAlwaysVariant(t, tt, version, true, runMessageBuildProofChksigAlwaysVariant(t, NewTVM(), accountRoot, body, cfg, tt, external, true), true)
		assertMessageChksigAlwaysVariant(t, tt, version, false, runMessageBuildProofChksigAlwaysVariant(t, NewTVM(), accountRoot, body, cfg, tt, external, false), false)
	})
}

func runMessageBuildProofChksigAlwaysVariant(t *testing.T, machine *TVM, accountRoot, body *cell.Cell, cfg MessageEmulationConfig, tt executionConfigSignatureCase, external bool, always bool) messageChksigAlwaysVariantResult {
	t.Helper()

	cfg.ChksigAlwaysSucceed = always
	var res *MessageExecutionResult
	var err error
	if external {
		res, err = machine.EmulateExternalMessage(nil, nil, &tlb.ExternalMessage{
			DstAddr: tonopsTestAddr,
			Body:    body,
		}, cfg)
	} else {
		res, err = machine.EmulateInternalMessage(nil, nil, body, internalMessageTestAmount, cfg)
	}
	var execRes *ExecutionResult
	if res != nil {
		execRes = &res.ExecutionResult
	}
	exit := exitCodeFromResult(execRes, err)
	if exit == -1 {
		t.Fatalf("message proof %s external=%t always=%t failed: %v", tt.name, external, always, err)
	}
	if res == nil {
		t.Fatalf("message proof %s external=%t always=%t returned nil result", tt.name, external, always)
	}
	if res.Proof == nil {
		t.Fatalf("message proof %s external=%t always=%t proof is nil", tt.name, external, always)
	}
	if _, err = cell.UnwrapProof(res.Proof, accountRoot.Hash()); err != nil {
		t.Fatalf("message proof %s external=%t always=%t proof is invalid: %v", tt.name, external, always, err)
	}
	if !vmcore.IsSuccessExitCode(exit) {
		return messageChksigAlwaysVariantResult{exit: exit}
	}
	got, err := res.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop message proof %s external=%t always=%t result: %v", tt.name, external, always, err)
	}
	return messageChksigAlwaysVariantResult{exit: exit, ok: got}
}

func FuzzTransactionEmulationBuildProofChksigAlwaysSucceedPerRun(f *testing.F) {
	for version := byte(MinSupportedGlobalVersion); version <= byte(MaxSupportedGlobalVersion); version++ {
		for kind := byte(0); kind < byte(len(executionConfigSignatureCases)); kind++ {
			f.Add(version, kind, version^kind^0x42)
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion, rawKind, sigTag byte) {
		version := transactionFuzzGlobalVersion(rawVersion)
		tt := executionConfigSignatureCases[int(rawKind)%len(executionConfigSignatureCases)]

		signature := make([]byte, 64)
		signature[0] = sigTag
		signature[31] = sigTag ^ 0x33
		signature[63] = sigTag ^ 0xff

		code := makeMessageChksigAlwaysVariantCode(t, tt, signature)
		shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, cell.BeginCell().EndCell(), walletSendTestBalance, uint32(tonopsTestTime.Unix()))
		msgCell, err := tlb.ToCell(&tlb.ExternalMessage{
			DstAddr: tonopsTestAddr,
			Body:    cell.BeginCell().MustStoreUInt(uint64(sigTag), 8).EndCell(),
		})
		if err != nil {
			t.Fatalf("failed to build external message: %v", err)
		}

		cfg := TransactionEmulationConfig{
			Address:     tonopsTestAddr,
			Now:         uint32(tonopsTestTime.Unix()),
			BlockLT:     transactionTestLogicalTime,
			LogicalTime: transactionTestLogicalTime,
			RandSeed:    append([]byte(nil), tonopsTestSeed...),
			ConfigRoot:  transactionTestConfigWithGlobalVersion(t, version).Root,
			BuildProof:  true,
			Gas: vmcore.NewGas(vmcore.GasConfig{
				Max:    walletSendTestGasMax,
				Credit: walletSendTestCredit,
			}),
		}

		if int(version) < tt.minVersion {
			for _, always := range []bool{false, true, false} {
				res := runTransactionBuildProofChksigAlwaysVariant(t, NewTVM(), shard, msgCell, cfg, tt, always)
				if res.exit != vmerr.CodeInvalidOpcode {
					t.Fatalf("%s transaction proof v%d always=%t exit=%d, want invalid opcode", tt.name, version, always, res.exit)
				}
			}
			return
		}

		assertMessageChksigAlwaysVariant(t, tt, version, false, runTransactionBuildProofChksigAlwaysVariant(t, NewTVM(), shard, msgCell, cfg, tt, false), false)
		assertMessageChksigAlwaysVariant(t, tt, version, true, runTransactionBuildProofChksigAlwaysVariant(t, NewTVM(), shard, msgCell, cfg, tt, true), true)
		assertMessageChksigAlwaysVariant(t, tt, version, false, runTransactionBuildProofChksigAlwaysVariant(t, NewTVM(), shard, msgCell, cfg, tt, false), false)
	})
}

func runTransactionBuildProofChksigAlwaysVariant(t *testing.T, machine *TVM, shard *tlb.ShardAccount, msgCell *cell.Cell, cfg TransactionEmulationConfig, tt executionConfigSignatureCase, always bool) messageChksigAlwaysVariantResult {
	t.Helper()

	accountHash := shard.Account.Hash()
	cfg.ChksigAlwaysSucceed = always
	res, err := machine.EmulateTransaction(shard, msgCell, cfg)
	var execRes *ExecutionResult
	if res != nil {
		execRes = &res.ExecutionResult
	}
	exit := exitCodeFromResult(execRes, err)
	if exit == -1 {
		t.Fatalf("transaction proof %s always=%t failed: %v", tt.name, always, err)
	}
	if res == nil {
		t.Fatalf("transaction proof %s always=%t returned nil result", tt.name, always)
	}
	if res.Proof == nil {
		t.Fatalf("transaction proof %s always=%t proof is nil", tt.name, always)
	}
	if _, err = cell.UnwrapProof(res.Proof, accountHash); err != nil {
		t.Fatalf("transaction proof %s always=%t proof is invalid: %v", tt.name, always, err)
	}
	if !vmcore.IsSuccessExitCode(exit) {
		return messageChksigAlwaysVariantResult{exit: exit}
	}
	got, err := res.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop transaction proof %s result always=%t: %v", tt.name, always, err)
	}
	return messageChksigAlwaysVariantResult{exit: exit, ok: got}
}

func FuzzTransactionEmulationGlobalVersionFallbackAndConfigOverride(f *testing.F) {
	for _, version := range transactionFuzzAllVersions {
		f.Add(byte(version), byte(0), false)
		f.Add(byte(0), byte(version), true)
	}
	f.Add(byte(3), byte(14), false)
	f.Add(byte(14), byte(3), true)
	f.Add(byte(3), byte(14), true)
	f.Add(byte(4), byte(0), false)
	f.Add(byte(MaxSupportedGlobalVersion), byte(0), true)

	f.Fuzz(func(t *testing.T, rawMachineVersion, rawConfigVersion byte, useConfigRoot bool) {
		machineVersion := int(transactionFuzzGlobalVersion(rawMachineVersion))
		configVersion := transactionFuzzGlobalVersion(rawConfigVersion)
		effectiveVersion := uint32(vmcore.DefaultGlobalVersion)

		var configRoot *cell.Cell
		if useConfigRoot {
			configRoot = transactionTestConfigWithGlobalVersion(t, configVersion).Root
			effectiveVersion = configVersion
		}

		machine, err := NewTVM().WithGlobalVersion(machineVersion)
		if err != nil {
			t.Fatalf("WithGlobalVersion(%d): %v", machineVersion, err)
		}

		code := makeMessageGasConsumedCode(t)
		shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, cell.BeginCell().EndCell(), walletSendTestBalance, uint32(tonopsTestTime.Unix()))
		msgCell, err := tlb.ToCell(&tlb.ExternalMessage{
			DstAddr: tonopsTestAddr,
			Body:    cell.BeginCell().MustStoreUInt(uint64(rawMachineVersion)^uint64(rawConfigVersion), 8).EndCell(),
		})
		if err != nil {
			t.Fatalf("failed to build external message: %v", err)
		}

		res, err := machine.EmulateTransaction(shard, msgCell, TransactionEmulationConfig{
			Address:     tonopsTestAddr,
			Now:         uint32(tonopsTestTime.Unix()),
			BlockLT:     transactionTestLogicalTime,
			LogicalTime: transactionTestLogicalTime,
			RandSeed:    append([]byte(nil), tonopsTestSeed...),
			ConfigRoot:  configRoot,
			Gas: vmcore.NewGas(vmcore.GasConfig{
				Max:    walletSendTestGasMax,
				Credit: walletSendTestCredit,
			}),
		})
		if err != nil {
			if !useConfigRoot && errors.Is(err, errConfigRootRequired) {
				return
			}
			t.Fatalf("transaction emulation machine_v=%d config_v=%d use_config=%t failed: %v", machineVersion, configVersion, useConfigRoot, err)
		}
		if !useConfigRoot {
			t.Fatalf("transaction emulation machine_v=%d config_v=%d succeeded without config root", machineVersion, configVersion)
		}

		assertGasConsumedVersionResult(t, "transaction", effectiveVersion, res.ExitCode, res.Stack)
	})
}

func FuzzTransactionEmulationGlobalVersionPerRun(f *testing.F) {
	for version := byte(MinSupportedGlobalVersion); version <= byte(MaxSupportedGlobalVersion); version++ {
		f.Add(version, version^0x5c)
	}

	f.Fuzz(func(t *testing.T, rawVersion, bodyTag byte) {
		version := transactionFuzzGlobalVersion(rawVersion)
		machine, err := NewTVM().WithGlobalVersion(MaxSupportedGlobalVersion)
		if err != nil {
			t.Fatalf("WithGlobalVersion(%d): %v", MaxSupportedGlobalVersion, err)
		}

		code := makeMessageGasConsumedCode(t)
		shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, cell.BeginCell().EndCell(), walletSendTestBalance, uint32(tonopsTestTime.Unix()))
		msgCell, err := tlb.ToCell(&tlb.ExternalMessage{
			DstAddr: tonopsTestAddr,
			Body:    cell.BeginCell().MustStoreUInt(uint64(bodyTag), 8).EndCell(),
		})
		if err != nil {
			t.Fatalf("failed to build external message: %v", err)
		}

		baseCfg := TransactionEmulationConfig{
			Address:     tonopsTestAddr,
			Now:         uint32(tonopsTestTime.Unix()),
			BlockLT:     transactionTestLogicalTime,
			LogicalTime: transactionTestLogicalTime,
			RandSeed:    append([]byte(nil), tonopsTestSeed...),
			ConfigRoot:  transactionTestConfigWithGlobalVersion(t, uint32(MaxSupportedGlobalVersion)).Root,
			Gas: vmcore.NewGas(vmcore.GasConfig{
				Max:    walletSendTestGasMax,
				Credit: walletSendTestCredit,
			}),
		}
		versionCfg := baseCfg
		versionCfg.ConfigRoot = transactionTestConfigWithGlobalVersion(t, version).Root

		res, err := machine.EmulateTransaction(shard, msgCell, versionCfg)
		if err != nil {
			t.Fatalf("transaction emulation per-run v%d failed: %v", version, err)
		}
		assertGasConsumedVersionResult(t, "transaction per-run", version, res.ExitCode, res.Stack)

		leakCheck, err := machine.EmulateTransaction(shard, msgCell, baseCfg)
		if err != nil {
			t.Fatalf("transaction emulation leak-check failed: %v", err)
		}
		assertGasConsumedVersionResult(t, "transaction leak-check", MaxSupportedGlobalVersion, leakCheck.ExitCode, leakCheck.Stack)
	})
}

func FuzzTickTockGlobalVersionFallbackAndConfigOverride(f *testing.F) {
	for _, version := range transactionFuzzAllVersions {
		f.Add(byte(version), byte(0), false, false)
		f.Add(byte(0), byte(version), true, true)
	}
	f.Add(byte(3), byte(14), false, false)
	f.Add(byte(14), byte(3), true, true)
	f.Add(byte(3), byte(14), true, true)
	f.Add(byte(4), byte(0), false, false)
	f.Add(byte(MaxSupportedGlobalVersion), byte(0), true, false)

	f.Fuzz(func(t *testing.T, rawMachineVersion, rawConfigVersion byte, useConfigRoot, isTock bool) {
		machineVersion := int(transactionFuzzGlobalVersion(rawMachineVersion))
		configVersion := transactionFuzzGlobalVersion(rawConfigVersion)
		effectiveVersion := uint32(vmcore.DefaultGlobalVersion)

		var configRoot *cell.Cell
		if useConfigRoot {
			configRoot = transactionTestConfigWithGlobalVersion(t, configVersion).Root
			effectiveVersion = configVersion
		}

		machine, err := NewTVM().WithGlobalVersion(machineVersion)
		if err != nil {
			t.Fatalf("WithGlobalVersion(%d): %v", machineVersion, err)
		}

		shard, err := buildTickTockShardAccountForTest(t, tickTockTestAddr, makeTickTockGasConsumedCode(t), cell.BeginCell().EndCell(), tickTockTestBalance)
		if err != nil {
			t.Fatalf("failed to build tick/tock shard: %v", err)
		}
		res, err := machine.EmulateTickTockTransaction(shard, isTock, TransactionEmulationConfig{
			Now:        uint32(tonopsTestTime.Unix()),
			Balance:    new(big.Int).SetUint64(tickTockTestBalance),
			RandSeed:   append([]byte(nil), tonopsTestSeed...),
			ConfigRoot: configRoot,
			Gas: vmcore.NewGas(vmcore.GasConfig{
				Max:   DefaultTickTockTransactionGasMax,
				Limit: DefaultTickTockTransactionGasMax,
			}),
		})
		if err != nil {
			if !useConfigRoot && errors.Is(err, errConfigRootRequired) {
				return
			}
			t.Fatalf("tick/tock emulation machine_v=%d config_v=%d use_config=%t is_tock=%t failed: %v", machineVersion, configVersion, useConfigRoot, isTock, err)
		}
		if !useConfigRoot {
			t.Fatalf("tick/tock emulation machine_v=%d config_v=%d is_tock=%t succeeded without config root", machineVersion, configVersion, isTock)
		}

		assertGasConsumedVersionResult(t, "tick/tock", effectiveVersion, res.ExitCode, res.Stack)
	})
}

func FuzzTickTockGlobalVersionPerRun(f *testing.F) {
	for version := byte(MinSupportedGlobalVersion); version <= byte(MaxSupportedGlobalVersion); version++ {
		f.Add(version, false)
		f.Add(version, true)
	}

	f.Fuzz(func(t *testing.T, rawVersion byte, isTock bool) {
		version := transactionFuzzGlobalVersion(rawVersion)
		machine, err := NewTVM().WithGlobalVersion(MaxSupportedGlobalVersion)
		if err != nil {
			t.Fatalf("WithGlobalVersion(%d): %v", MaxSupportedGlobalVersion, err)
		}

		shard, err := buildTickTockShardAccountForTest(t, tickTockTestAddr, makeTickTockGasConsumedCode(t), cell.BeginCell().EndCell(), tickTockTestBalance)
		if err != nil {
			t.Fatalf("failed to build tick/tock shard: %v", err)
		}
		baseCfg := TransactionEmulationConfig{
			Now:        uint32(tonopsTestTime.Unix()),
			Balance:    new(big.Int).SetUint64(tickTockTestBalance),
			RandSeed:   append([]byte(nil), tonopsTestSeed...),
			ConfigRoot: transactionTestConfigWithGlobalVersion(t, uint32(MaxSupportedGlobalVersion)).Root,
			Gas: vmcore.NewGas(vmcore.GasConfig{
				Max:   DefaultTickTockTransactionGasMax,
				Limit: DefaultTickTockTransactionGasMax,
			}),
		}
		versionCfg := baseCfg
		versionCfg.ConfigRoot = transactionTestConfigWithGlobalVersion(t, version).Root

		res, err := machine.EmulateTickTockTransaction(shard, isTock, versionCfg)
		if err != nil {
			t.Fatalf("tick/tock emulation per-run v%d is_tock=%t failed: %v", version, isTock, err)
		}
		assertGasConsumedVersionResult(t, "tick/tock per-run", version, res.ExitCode, res.Stack)

		leakCheck, err := machine.EmulateTickTockTransaction(shard, isTock, baseCfg)
		if err != nil {
			t.Fatalf("tick/tock emulation leak-check is_tock=%t failed: %v", isTock, err)
		}
		assertGasConsumedVersionResult(t, "tick/tock leak-check", MaxSupportedGlobalVersion, leakCheck.ExitCode, leakCheck.Stack)
	})
}

func FuzzCheckExternalMessageAcceptedGlobalVersionFallbackAndConfigOverride(f *testing.F) {
	for _, version := range transactionFuzzAllVersions {
		f.Add(byte(version), byte(0), false)
		f.Add(byte(0), byte(version), true)
	}
	f.Add(byte(3), byte(14), false)
	f.Add(byte(14), byte(3), true)
	f.Add(byte(3), byte(14), true)
	f.Add(byte(4), byte(0), false)
	f.Add(byte(MaxSupportedGlobalVersion), byte(0), true)

	f.Fuzz(func(t *testing.T, rawMachineVersion, rawConfigVersion byte, useConfigRoot bool) {
		machineVersion := int(transactionFuzzGlobalVersion(rawMachineVersion))
		configVersion := transactionFuzzGlobalVersion(rawConfigVersion)
		effectiveVersion := uint32(vmcore.DefaultGlobalVersion)

		var configRoot *cell.Cell
		if useConfigRoot {
			configRoot = transactionTestConfigWithGlobalVersion(t, configVersion).Root
			effectiveVersion = configVersion
		}

		machine, err := NewTVM().WithGlobalVersion(machineVersion)
		if err != nil {
			t.Fatalf("WithGlobalVersion(%d): %v", machineVersion, err)
		}

		code := makeCheckExternalAcceptedGasConsumedCode(t)
		origData := cell.BeginCell().EndCell()
		shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, uint32(tonopsTestTime.Unix()))
		msg := &tlb.ExternalMessage{
			DstAddr: tonopsTestAddr,
			Body:    cell.BeginCell().MustStoreUInt(uint64(rawMachineVersion)^uint64(rawConfigVersion), 8).EndCell(),
		}
		msgCell, err := tlb.ToCell(msg)
		if err != nil {
			t.Fatalf("failed to build external message: %v", err)
		}

		accepted, err := machine.CheckExternalMessageAccepted(shard, mustParseTransactionTestAccount(t, shard), msgCell, msg, CheckExternalMessageAcceptedConfig{
			Now:         uint32(tonopsTestTime.Unix()),
			BlockLT:     transactionTestLogicalTime,
			LogicalTime: transactionTestLogicalTime,
			RandSeed:    append([]byte(nil), tonopsTestSeed...),
			ConfigRoot:  configRoot,
		})
		if err != nil {
			if !useConfigRoot && errors.Is(err, errConfigRootRequired) {
				return
			}
			t.Fatalf("check external accepted machine_v=%d config_v=%d use_config=%t failed: %v", machineVersion, configVersion, useConfigRoot, err)
		}
		if !useConfigRoot {
			t.Fatalf("check external accepted machine_v=%d config_v=%d succeeded without config root", machineVersion, configVersion)
		}

		want := effectiveVersion >= 4
		if accepted != want {
			t.Fatalf("effective v%d accepted=%t, want %t", effectiveVersion, accepted, want)
		}
	})
}

func FuzzCheckExternalMessageAcceptedGlobalVersionPerRun(f *testing.F) {
	for version := byte(MinSupportedGlobalVersion); version <= byte(MaxSupportedGlobalVersion); version++ {
		f.Add(version, version^0x9d)
	}

	f.Fuzz(func(t *testing.T, rawVersion, bodyTag byte) {
		version := transactionFuzzGlobalVersion(rawVersion)
		machine, err := NewTVM().WithGlobalVersion(MaxSupportedGlobalVersion)
		if err != nil {
			t.Fatalf("WithGlobalVersion(%d): %v", MaxSupportedGlobalVersion, err)
		}

		code := makeCheckExternalAcceptedGasConsumedCode(t)
		origData := cell.BeginCell().EndCell()
		shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, uint32(tonopsTestTime.Unix()))
		msg := &tlb.ExternalMessage{
			DstAddr: tonopsTestAddr,
			Body:    cell.BeginCell().MustStoreUInt(uint64(bodyTag), 8).EndCell(),
		}
		msgCell, err := tlb.ToCell(msg)
		if err != nil {
			t.Fatalf("failed to build external message: %v", err)
		}

		baseCfg := CheckExternalMessageAcceptedConfig{
			Now:         uint32(tonopsTestTime.Unix()),
			BlockLT:     transactionTestLogicalTime,
			LogicalTime: transactionTestLogicalTime,
			RandSeed:    append([]byte(nil), tonopsTestSeed...),
			ConfigRoot:  transactionTestConfigWithGlobalVersion(t, uint32(MaxSupportedGlobalVersion)).Root,
		}
		versionCfg := baseCfg
		versionCfg.ConfigRoot = transactionTestConfigWithGlobalVersion(t, version).Root

		accepted, err := machine.CheckExternalMessageAccepted(shard, mustParseTransactionTestAccount(t, shard), msgCell, msg, versionCfg)
		if err != nil {
			t.Fatalf("check external accepted per-run v%d failed: %v", version, err)
		}
		if want := version >= 4; accepted != want {
			t.Fatalf("check external accepted per-run v%d accepted=%t, want %t", version, accepted, want)
		}

		accepted, err = machine.CheckExternalMessageAccepted(shard, mustParseTransactionTestAccount(t, shard), msgCell, msg, baseCfg)
		if err != nil {
			t.Fatalf("check external accepted leak-check failed: %v", err)
		}
		if !accepted {
			t.Fatal("check external accepted leak-check rejected default-version message")
		}
	})
}

func FuzzCheckExternalMessageAcceptedChksigAlwaysSucceedPerRun(f *testing.F) {
	for version := byte(MinSupportedGlobalVersion); version <= byte(MaxSupportedGlobalVersion); version++ {
		for kind := byte(0); kind < byte(len(executionConfigSignatureCases)); kind++ {
			f.Add(version, kind, version^kind^0xC3)
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion, rawKind, sigTag byte) {
		version := transactionFuzzGlobalVersion(rawVersion)
		tt := executionConfigSignatureCases[int(rawKind)%len(executionConfigSignatureCases)]

		signature := make([]byte, 64)
		signature[0] = sigTag
		signature[31] = sigTag ^ 0x33
		signature[63] = sigTag ^ 0xff

		code := makeCheckExternalAcceptedChksigAlwaysCode(t, tt, signature, codeFromBuilders(t, funcsop.ACCEPT().Serialize()))
		shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, cell.BeginCell().EndCell(), walletSendTestBalance, uint32(tonopsTestTime.Unix()))
		msg := &tlb.ExternalMessage{
			DstAddr: tonopsTestAddr,
			Body:    cell.BeginCell().MustStoreUInt(uint64(sigTag), 8).EndCell(),
		}
		msgCell, err := tlb.ToCell(msg)
		if err != nil {
			t.Fatalf("failed to build external message: %v", err)
		}

		cfg := CheckExternalMessageAcceptedConfig{
			Now:         uint32(tonopsTestTime.Unix()),
			BlockLT:     transactionTestLogicalTime,
			LogicalTime: transactionTestLogicalTime,
			RandSeed:    append([]byte(nil), tonopsTestSeed...),
			ConfigRoot:  transactionTestConfigWithGlobalVersion(t, version).Root,
		}
		account := mustParseTransactionTestAccount(t, shard)
		machine := NewTVM()

		if int(version) < tt.minVersion {
			for _, always := range []bool{false, true, false} {
				if accepted := runCheckExternalAcceptedChksigAlwaysVariant(t, machine, shard, account, msgCell, msg, cfg, tt, always); accepted {
					t.Fatalf("%s check accepted v%d always=%t accepted=true, want false before opcode activation", tt.name, version, always)
				}
			}
			return
		}

		if accepted := runCheckExternalAcceptedChksigAlwaysVariant(t, machine, shard, account, msgCell, msg, cfg, tt, false); accepted {
			t.Fatalf("%s check accepted v%d always=false accepted=true, want false", tt.name, version)
		}
		if accepted := runCheckExternalAcceptedChksigAlwaysVariant(t, machine, shard, account, msgCell, msg, cfg, tt, true); !accepted {
			t.Fatalf("%s check accepted v%d always=true accepted=false, want true", tt.name, version)
		}
		if accepted := runCheckExternalAcceptedChksigAlwaysVariant(t, machine, shard, account, msgCell, msg, cfg, tt, false); accepted {
			t.Fatalf("%s check accepted v%d always=false accepted=true after configured run", tt.name, version)
		}
	})
}

func runCheckExternalAcceptedChksigAlwaysVariant(t *testing.T, machine *TVM, shard *tlb.ShardAccount, account *tlb.AccountState, msgCell *cell.Cell, msg *tlb.ExternalMessage, cfg CheckExternalMessageAcceptedConfig, tt executionConfigSignatureCase, always bool) bool {
	t.Helper()

	cfg.ChksigAlwaysSucceed = always
	accepted, err := machine.CheckExternalMessageAccepted(shard, account, msgCell, msg, cfg)
	if err != nil {
		t.Fatalf("CheckExternalMessageAccepted %s always=%t failed: %v", tt.name, always, err)
	}
	return accepted
}

func makeMessageGasConsumedCode(t *testing.T) *cell.Cell {
	t.Helper()

	return codeFromBuilders(t,
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		funcsop.GASCONSUMED().Serialize(),
	)
}

func makeCheckExternalAcceptedGasConsumedCode(t *testing.T) *cell.Cell {
	t.Helper()

	return codeFromBuilders(t,
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		funcsop.GASCONSUMED().Serialize(),
		stackop.DROP().Serialize(),
		funcsop.ACCEPT().Serialize(),
	)
}

func makeTickTockGasConsumedCode(t *testing.T) *cell.Cell {
	t.Helper()

	return codeFromBuilders(t,
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		funcsop.GASCONSUMED().Serialize(),
	)
}

func assertGasConsumedVersionResult(t *testing.T, name string, effectiveVersion uint32, exit int64, stack *vmcore.Stack) {
	t.Helper()

	if effectiveVersion < 4 {
		if exit != vmerr.CodeInvalidOpcode {
			t.Fatalf("%s effective v%d exit=%d, want invalid opcode", name, effectiveVersion, exit)
		}
		return
	}
	if exit != 0 {
		t.Fatalf("%s effective v%d exit=%d, want success", name, effectiveVersion, exit)
	}
	if stack == nil {
		t.Fatalf("%s effective v%d stack is nil", name, effectiveVersion)
	}
	if _, err := stack.PopInt(); err != nil {
		t.Fatalf("%s effective v%d failed to pop GASCONSUMED result: %v", name, effectiveVersion, err)
	}
}

func FuzzMessageEmulationChksigAlwaysSucceedPerRun(f *testing.F) {
	for version := byte(MinSupportedGlobalVersion); version <= byte(MaxSupportedGlobalVersion); version++ {
		for kind := byte(0); kind < byte(len(executionConfigSignatureCases)); kind++ {
			f.Add(version, kind, version^kind^0x11)
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion, rawKind, sigTag byte) {
		version := transactionFuzzGlobalVersion(rawVersion)
		tt := executionConfigSignatureCases[int(rawKind)%len(executionConfigSignatureCases)]
		signature := make([]byte, 64)
		signature[0] = sigTag
		signature[63] = sigTag ^ 0xff
		code := makeMessageChksigAlwaysVariantCode(t, tt, signature)
		data := cell.BeginCell().EndCell()
		body := cell.BeginCell().MustStoreUInt(uint64(sigTag), 8).EndCell()
		cfg := EmulateExternalMessageConfig{
			Address:    tonopsTestAddr,
			Now:        uint32(tonopsTestTime.Unix()),
			Balance:    new(big.Int).SetUint64(walletSendTestBalance),
			RandSeed:   append([]byte(nil), walletSendTestSeed...),
			ConfigRoot: transactionTestConfigWithGlobalVersion(t, version).Root,
		}
		msg := &tlb.ExternalMessage{
			DstAddr: tonopsTestAddr,
			Body:    body,
		}
		machine := NewTVM()

		if int(version) < tt.minVersion {
			for _, always := range []bool{false, true} {
				res := runMessageChksigAlwaysVariant(t, machine, code, data, msg, cfg, tt, always)
				if res.exit != vmerr.CodeInvalidOpcode {
					t.Fatalf("%s v%d always=%t exit=%d, want invalid opcode", tt.name, version, always, res.exit)
				}
			}
			return
		}

		defaultRes := runMessageChksigAlwaysVariant(t, machine, code, data, msg, cfg, tt, false)
		assertMessageChksigAlwaysVariant(t, tt, version, false, defaultRes, false)

		configuredRes := runMessageChksigAlwaysVariant(t, machine, code, data, msg, cfg, tt, true)
		assertMessageChksigAlwaysVariant(t, tt, version, true, configuredRes, true)

		leakCheck := runMessageChksigAlwaysVariant(t, machine, code, data, msg, cfg, tt, false)
		assertMessageChksigAlwaysVariant(t, tt, version, false, leakCheck, false)
	})
}

func FuzzMessageEmulationGlobalVersionPerRun(f *testing.F) {
	for version := byte(MinSupportedGlobalVersion); version <= byte(MaxSupportedGlobalVersion); version++ {
		f.Add(version, byte(0), version^0x28)
		f.Add(version, byte(1), version^0x82)
	}

	f.Fuzz(func(t *testing.T, rawVersion, rawPath, sigTag byte) {
		version := transactionFuzzGlobalVersion(rawVersion)
		tt := executionConfigSignatureCases[2]
		signature := make([]byte, 64)
		signature[0] = sigTag
		signature[63] = sigTag ^ 0xff
		code := makeMessageChksigAlwaysVariantCode(t, tt, signature)
		data := cell.BeginCell().EndCell()
		body := cell.BeginCell().MustStoreUInt(uint64(sigTag), 8).EndCell()
		machine, err := NewTVM().WithGlobalVersion(MaxSupportedGlobalVersion)
		if err != nil {
			t.Fatalf("WithGlobalVersion(%d): %v", MaxSupportedGlobalVersion, err)
		}

		baseCfg := MessageEmulationConfig{
			Address:             tonopsTestAddr,
			Now:                 uint32(tonopsTestTime.Unix()),
			Balance:             new(big.Int).Set(tonopsTestBalance),
			RandSeed:            append([]byte(nil), tonopsTestSeed...),
			ConfigRoot:          transactionTestConfigWithGlobalVersion(t, uint32(MaxSupportedGlobalVersion)).Root,
			ChksigAlwaysSucceed: true,
			Gas: vmcore.NewGas(vmcore.GasConfig{
				Max:   DefaultInternalMessageGasMax,
				Limit: int64(internalMessageTestAmount) * InternalMessageGasAmountFactor,
			}),
		}
		versionCfg := baseCfg
		versionCfg.ConfigRoot = transactionTestConfigWithGlobalVersion(t, version).Root

		configured := runMessageGlobalVersionPath(t, &machine, rawPath, code, data, body, versionCfg, tt)
		if int(version) < tt.minVersion {
			if configured.exit != vmerr.CodeInvalidOpcode {
				t.Fatalf("%s message path=%d per-run v%d exit=%d, want invalid opcode", tt.name, rawPath%2, version, configured.exit)
			}
		} else {
			assertMessageChksigAlwaysVariant(t, tt, version, true, configured, true)
		}

		leakCheck := runMessageGlobalVersionPath(t, &machine, rawPath, code, data, body, baseCfg, tt)
		assertMessageChksigAlwaysVariant(t, tt, uint32(MaxSupportedGlobalVersion), true, leakCheck, true)
	})
}

func FuzzInternalMessageEmulationChksigAlwaysSucceedPerRun(f *testing.F) {
	for version := byte(MinSupportedGlobalVersion); version <= byte(MaxSupportedGlobalVersion); version++ {
		for kind := byte(0); kind < byte(len(executionConfigSignatureCases)); kind++ {
			f.Add(version, kind, version^kind^0x44)
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion, rawKind, sigTag byte) {
		version := transactionFuzzGlobalVersion(rawVersion)
		tt := executionConfigSignatureCases[int(rawKind)%len(executionConfigSignatureCases)]
		signature := make([]byte, 64)
		signature[0] = sigTag
		signature[63] = sigTag ^ 0xff
		code := makeMessageChksigAlwaysVariantCode(t, tt, signature)
		data := cell.BeginCell().EndCell()
		body := cell.BeginCell().MustStoreUInt(uint64(sigTag), 8).EndCell()
		cfg := EmulateInternalMessageConfig{
			Address:    tonopsTestAddr,
			Now:        uint32(tonopsTestTime.Unix()),
			Balance:    new(big.Int).Set(tonopsTestBalance),
			RandSeed:   append([]byte(nil), tonopsTestSeed...),
			ConfigRoot: transactionTestConfigWithGlobalVersion(t, version).Root,
			Gas: vmcore.NewGas(vmcore.GasConfig{
				Max:   DefaultInternalMessageGasMax,
				Limit: int64(internalMessageTestAmount) * InternalMessageGasAmountFactor,
			}),
		}
		machine := NewTVM()

		if int(version) < tt.minVersion {
			for _, always := range []bool{false, true} {
				res := runInternalMessageChksigAlwaysVariant(t, machine, code, data, body, cfg, tt, always)
				if res.exit != vmerr.CodeInvalidOpcode {
					t.Fatalf("%s internal v%d always=%t exit=%d, want invalid opcode", tt.name, version, always, res.exit)
				}
			}
			return
		}

		defaultRes := runInternalMessageChksigAlwaysVariant(t, machine, code, data, body, cfg, tt, false)
		assertMessageChksigAlwaysVariant(t, tt, version, false, defaultRes, false)

		configuredRes := runInternalMessageChksigAlwaysVariant(t, machine, code, data, body, cfg, tt, true)
		assertMessageChksigAlwaysVariant(t, tt, version, true, configuredRes, true)

		leakCheck := runInternalMessageChksigAlwaysVariant(t, machine, code, data, body, cfg, tt, false)
		assertMessageChksigAlwaysVariant(t, tt, version, false, leakCheck, false)
	})
}

func makeMessageChksigAlwaysCode(t *testing.T, signature []byte) *cell.Cell {
	t.Helper()

	return makeMessageChksigAlwaysVariantCode(t, executionConfigSignatureCases[0], signature)
}

func makeMessageChksigAlwaysVariantCode(t *testing.T, tt executionConfigSignatureCase, signature []byte) *cell.Cell {
	t.Helper()

	builders := []*cell.Builder{
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
	}
	if tt.fromSlice {
		builders = append(builders, stackop.PUSHSLICE(cell.BeginCell().MustStoreSlice([]byte{0x10, 0x20, 0x30, 0x40}, 32).ToSlice()).Serialize())
	} else {
		builders = append(builders, stackop.PUSHINT(big.NewInt(0)).Serialize())
	}

	builders = append(builders, stackop.PUSHSLICE(cell.BeginCell().MustStoreSlice(signature, 512).ToSlice()).Serialize())
	if tt.p256 {
		key := make([]byte, 33)
		key[0] = 0x05
		builders = append(builders, stackop.PUSHSLICE(cell.BeginCell().MustStoreSlice(key, 264).ToSlice()).Serialize())
	} else {
		builders = append(builders, stackop.PUSHINT(big.NewInt(2)).Serialize())
	}

	builders = append(builders, chksigAlwaysVariantOpcode(t, tt))
	return codeFromBuilders(t, builders...)
}

func chksigAlwaysVariantOpcode(t *testing.T, tt executionConfigSignatureCase) *cell.Builder {
	t.Helper()

	switch tt.name {
	case "CHKSIGNU":
		return funcsop.CHKSIGNU().Serialize()
	case "CHKSIGNS":
		return funcsop.CHKSIGNS().Serialize()
	case "P256_CHKSIGNU":
		return funcsop.P256_CHKSIGNU().Serialize()
	case "P256_CHKSIGNS":
		return funcsop.P256_CHKSIGNS().Serialize()
	default:
		t.Fatalf("unsupported signature op %q", tt.name)
		return nil
	}
}

type messageChksigAlwaysVariantResult struct {
	exit int64
	ok   bool
}

func runMessageChksigAlwaysVariant(t *testing.T, machine *TVM, code, data *cell.Cell, msg *tlb.ExternalMessage, cfg EmulateExternalMessageConfig, tt executionConfigSignatureCase, always bool) messageChksigAlwaysVariantResult {
	t.Helper()

	cfg.ChksigAlwaysSucceed = always
	res, err := machine.EmulateExternalMessage(code, data, msg, cfg)
	var execRes *ExecutionResult
	if res != nil {
		execRes = &res.ExecutionResult
	}
	exit := exitCodeFromResult(execRes, err)
	if exit == -1 {
		t.Fatalf("EmulateExternalMessage %s always=%v failed: %v", tt.name, always, err)
	}
	if !vmcore.IsSuccessExitCode(exit) {
		return messageChksigAlwaysVariantResult{exit: exit}
	}
	got, err := res.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop %s result always=%v: %v", tt.name, always, err)
	}
	return messageChksigAlwaysVariantResult{exit: exit, ok: got}
}

func runInternalMessageChksigAlwaysVariant(t *testing.T, machine *TVM, code, data, body *cell.Cell, cfg EmulateInternalMessageConfig, tt executionConfigSignatureCase, always bool) messageChksigAlwaysVariantResult {
	t.Helper()

	cfg.ChksigAlwaysSucceed = always
	res, err := machine.EmulateInternalMessage(code, data, body, internalMessageTestAmount, cfg)
	var execRes *ExecutionResult
	if res != nil {
		execRes = &res.ExecutionResult
	}
	exit := exitCodeFromResult(execRes, err)
	if exit == -1 {
		t.Fatalf("EmulateInternalMessage %s always=%v failed: %v", tt.name, always, err)
	}
	if !vmcore.IsSuccessExitCode(exit) {
		return messageChksigAlwaysVariantResult{exit: exit}
	}
	got, err := res.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop internal %s result always=%v: %v", tt.name, always, err)
	}
	return messageChksigAlwaysVariantResult{exit: exit, ok: got}
}

func runMessageGlobalVersionPath(t *testing.T, machine *TVM, rawPath byte, code, data, body *cell.Cell, cfg MessageEmulationConfig, tt executionConfigSignatureCase) messageChksigAlwaysVariantResult {
	t.Helper()

	if rawPath%2 == 0 {
		msg := &tlb.ExternalMessage{
			DstAddr: tonopsTestAddr,
			Body:    body,
		}
		return runMessageChksigAlwaysVariant(t, machine, code, data, msg, cfg, tt, cfg.ChksigAlwaysSucceed)
	}
	return runInternalMessageChksigAlwaysVariant(t, machine, code, data, body, cfg, tt, cfg.ChksigAlwaysSucceed)
}

func assertMessageChksigAlwaysVariant(t *testing.T, tt executionConfigSignatureCase, version uint32, always bool, res messageChksigAlwaysVariantResult, want bool) {
	t.Helper()

	if !vmcore.IsSuccessExitCode(res.exit) {
		t.Fatalf("%s v%d always=%t exit=%d, want success", tt.name, version, always, res.exit)
	}
	if res.ok != want {
		t.Fatalf("%s v%d always=%t result=%t, want %t", tt.name, version, always, res.ok, want)
	}
}

func FuzzCheckExternalMessageAcceptedVersionedInMsgParams(f *testing.F) {
	for _, version := range transactionFuzzAllVersions {
		f.Add(byte(version), byte(0x11), byte(0x22))
	}
	f.Add(byte(0), byte(0x11), byte(0x22))
	f.Add(byte(10), byte(0x33), byte(0x44))
	f.Add(byte(11), byte(0x55), byte(0x66))
	f.Add(byte(MaxSupportedGlobalVersion), byte(0x77), byte(0x88))

	f.Fuzz(func(t *testing.T, rawVersion, dataTag, bodyTag byte) {
		version := transactionFuzzGlobalVersion(rawVersion)
		now := uint32(tonopsTestTime.Unix())
		origData := cell.BeginCell().MustStoreUInt(uint64(dataTag), 8).EndCell()
		newData := cell.BeginCell().MustStoreUInt(uint64(dataTag)^uint64(bodyTag)^0xff, 8).EndCell()
		body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 8).EndCell()
		code := makeDirectMessageInMsgParamsCode(t, newData, true)
		cfg := transactionTestConfigWithGlobalVersion(t, version)
		shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)
		msg := &tlb.ExternalMessage{
			DstAddr: tonopsTestAddr,
			Body:    body,
		}
		msgCell, err := tlb.ToCell(msg)
		if err != nil {
			t.Fatalf("v%d failed to build external message: %v", version, err)
		}

		var account tlb.AccountState
		if err = tlb.Parse(&account, shard.Account); err != nil {
			t.Fatalf("v%d failed to parse account: %v", version, err)
		}

		accepted, err := NewTVM().CheckExternalMessageAccepted(shard, &account, msgCell, msg, CheckExternalMessageAcceptedConfig{
			Now:         now,
			BlockLT:     transactionTestLogicalTime,
			LogicalTime: transactionTestLogicalTime,
			RandSeed:    append([]byte(nil), tonopsTestSeed...),
			ConfigRoot:  cfg.Root,
		})
		if err != nil {
			t.Fatalf("v%d check external message accepted failed: %v", version, err)
		}
		if accepted != (version >= 11) {
			t.Fatalf("v%d accepted = %t, want %t", version, accepted, version >= 11)
		}

		full, err := NewTVM().EmulateTransaction(shard, msgCell, TransactionEmulationConfig{
			Address:      tonopsTestAddr,
			Now:          now,
			BlockLT:      transactionTestLogicalTime,
			LogicalTime:  transactionTestLogicalTime,
			RandSeed:     append([]byte(nil), tonopsTestSeed...),
			ConfigRoot:   cfg.Root,
			StopOnAccept: true,
		})
		if err != nil {
			t.Fatalf("v%d stop-on-accept transaction failed: %v", version, err)
		}
		if accepted != full.Accepted {
			t.Fatalf("v%d check accepted = %t, full transaction accepted = %t", version, accepted, full.Accepted)
		}
	})
}

func FuzzCheckExternalMessageAcceptedInboundDestinationVersionGate(f *testing.F) {
	for _, version := range transactionFuzzAllVersions {
		f.Add(byte(version), byte(0), byte(0x11))
		f.Add(byte(version), byte(1), byte(0x22))
	}
	f.Add(byte(9), byte(0), byte(0x11))
	f.Add(byte(9), byte(1), byte(0x22))
	f.Add(byte(10), byte(1), byte(0x33))
	f.Add(byte(10), byte(2), byte(0x44))
	f.Add(byte(MaxSupportedGlobalVersion), byte(3), byte(0x55))

	f.Fuzz(func(t *testing.T, rawVersion, rawDest, bodyTag byte) {
		version := transactionFuzzGlobalVersion(rawVersion)
		now := uint32(tonopsTestTime.Unix())
		origData := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
		newData := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
		body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 8).EndCell()
		code := makeTransactionExternalSuccessCode(t, newData)
		cfg := transactionTestConfigWithGlobalVersion(t, version)
		shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)

		dst := tonopsTestAddr
		validFromV10 := true
		switch rawDest % 4 {
		case 1:
			dst = tonopsTestAddr.WithAnycast(address.NewAnycast(1, []byte{tonopsTestAddr.Data()[0] & 0x80}))
			validFromV10 = false
		case 2:
			dst = address.NewAddressNone()
			validFromV10 = false
		case 3:
			dst = address.NewAddressVar(0, tonopsTestAddr.Workchain(), tonopsTestAddr.BitsLen(), tonopsTestAddr.Data())
			validFromV10 = false
		}

		msg := &tlb.ExternalMessage{
			DstAddr: dst,
			Body:    body,
		}
		msgCell, err := tlb.ToCell(msg)
		if err != nil {
			t.Fatalf("v%d failed to build external message: %v", version, err)
		}

		accepted, err := NewTVM().CheckExternalMessageAccepted(shard, mustParseTransactionTestAccount(t, shard), msgCell, msg, CheckExternalMessageAcceptedConfig{
			Now:         now,
			BlockLT:     transactionTestLogicalTime,
			LogicalTime: transactionTestLogicalTime,
			RandSeed:    append([]byte(nil), tonopsTestSeed...),
			ConfigRoot:  cfg.Root,
		})
		if version >= 10 && !validFromV10 {
			if err == nil {
				t.Fatalf("v%d accepted invalid inbound external destination", version)
			}
			return
		}
		if err != nil {
			t.Fatalf("v%d check external message accepted failed: %v", version, err)
		}
		if !accepted {
			t.Fatalf("v%d valid inbound external destination was not accepted", version)
		}
	})
}

func messageResultHasNonEmptyActions(actions *cell.Cell) bool {
	return actions != nil && (actions.BitsSize() > 0 || actions.RefsNum() > 0)
}

func makeDirectMessageInMsgParamsCode(t *testing.T, newData *cell.Cell, external bool) *cell.Cell {
	t.Helper()

	builders := []*cell.Builder{
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		funcsop.INMSGPARAMS().Serialize(),
		stackop.DROP().Serialize(),
	}
	if external {
		builders = append(builders, funcsop.ACCEPT().Serialize())
	}
	builders = append(builders,
		stackop.PUSHREF(newData).Serialize(),
		execop.POPCTR(4).Serialize(),
	)
	return codeFromBuilders(t, builders...)
}

func FuzzTransactionVersionedNonCanonicalForwardFee(f *testing.F) {
	f.Add(byte(1), []byte{0x00})
	f.Add(byte(2), []byte{0x00, 0x01})
	f.Add(byte(3), []byte{0x00, 0x00, 0x02})

	f.Fuzz(func(t *testing.T, lenByte byte, payload []byte) {
		ln := uint(lenByte % 16)
		if len(payload) < int(ln) {
			return
		}

		msgCell := transactionTestRelaxedInternalMessageWithFwdFee(func(b *cell.Builder) {
			b.MustStoreBigCoins(big.NewInt(1))
		}, func(b *cell.Builder) {
			b.MustStoreBigCoins(big.NewInt(0))
		}, func(b *cell.Builder) {
			b.MustStoreUInt(uint64(ln), 4)
			if ln > 0 {
				b.MustStoreSlice(payload[:ln], ln*8)
			}
		})

		validation, err := transactionValidateRelaxedActionMessageCurrencies(msgCell)
		if err != nil || validation.fwdFeeCanonical {
			return
		}

		priceCell := buildTransactionMsgForwardPricesCell(t, 100, 1<<15)
		v7Cfg := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
			tlb.ConfigParamGlobalVersion:               transactionTestGlobalVersionCell(t, 7),
			tlb.ConfigParamMsgForwardPricesBasechain:   priceCell,
			tlb.ConfigParamMsgForwardPricesMasterchain: priceCell,
		})
		v8Cfg := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
			tlb.ConfigParamGlobalVersion:               transactionTestGlobalVersionCell(t, 8),
			tlb.ConfigParamMsgForwardPricesBasechain:   priceCell,
			tlb.ConfigParamMsgForwardPricesMasterchain: priceCell,
		})

		v7 := applyTransactionSendActionForTestWithParams(t,
			tlb.ActionSendMsg{Mode: 2, Msg: msgCell},
			v7Cfg,
			big.NewInt(1_000_000_000),
			nil,
			transactionZeroCurrencyBalance(),
		)
		if v7.phase == nil || v7.phase.ResultCode != 34 || v7.phase.SkippedActions != 0 {
			t.Fatalf("unexpected v7 action phase: %+v", v7.phase)
		}

		v8 := applyTransactionSendActionForTestWithParams(t,
			tlb.ActionSendMsg{Mode: 0, Msg: msgCell},
			v8Cfg,
			big.NewInt(1_000_000_000),
			nil,
			transactionZeroCurrencyBalance(),
		)
		if v8.phase == nil || v8.phase.ResultCode != 37 || !v8.phase.NoFunds {
			t.Fatalf("unexpected v8 action phase: %+v", v8.phase)
		}

		v8Skip := applyTransactionSendActionForTestWithParams(t,
			tlb.ActionSendMsg{Mode: 2, Msg: msgCell},
			v8Cfg,
			big.NewInt(1_000_000_000),
			nil,
			transactionZeroCurrencyBalance(),
		)
		if v8Skip.phase == nil || !v8Skip.phase.Success || v8Skip.phase.ResultCode != 0 || v8Skip.phase.SkippedActions != 1 || v8Skip.phase.MessagesCreated != 0 {
			t.Fatalf("unexpected v8 skip action phase: %+v", v8Skip.phase)
		}
	})
}

func FuzzTransactionVersionedCustomFeeLowerBoundStopsAtV8(f *testing.F) {
	f.Add(uint64(100), uint32(1<<16), uint64(200), uint64(300), byte(0x10))
	f.Add(uint64(100), uint32(1<<15), uint64(50), uint64(25), byte(0x20))
	f.Add(uint64(1), uint32(0), uint64(0), uint64(0), byte(0x30))
	f.Add(uint64(999), uint32(0xffff), uint64(1500), uint64(1500), byte(0x40))

	f.Fuzz(func(t *testing.T, rawFwd uint64, rawIHRFactor uint32, rawUserFwd, rawUserIHR uint64, bodyTag byte) {
		computedFwd := rawFwd%1000 + 1
		ihrFactor := rawIHRFactor & 0xffff
		userFwd := rawUserFwd % 2000
		userIHR := rawUserIHR % 2000
		computedIHR := (computedFwd * uint64(ihrFactor)) >> 16
		amount := uint64(10_000_000)

		priceCell, err := tlb.ToCell(&tlb.ConfigMsgForwardPrices{
			LumpPrice: computedFwd,
			IHRFactor: ihrFactor,
		})
		if err != nil {
			t.Fatalf("build forward prices: %v", err)
		}
		msgCell, err := tlb.ToCell(&tlb.InternalMessage{
			IHRDisabled: false,
			SrcAddr:     address.NewAddressNone(),
			DstAddr:     tonopsTestAddr,
			Amount:      tlb.FromNanoTONU(amount),
			IHRFee:      tlb.FromNanoTONU(userIHR),
			FwdFee:      tlb.FromNanoTONU(userFwd),
			Body:        cell.BeginCell().MustStoreUInt(uint64(bodyTag), 8).EndCell(),
		})
		if err != nil {
			t.Fatalf("build outbound message: %v", err)
		}

		for _, tc := range []struct {
			version uint32
			fwdFee  uint64
			ihrFee  uint64
		}{
			{version: 7, fwdFee: max(computedFwd, userFwd), ihrFee: max(computedIHR, userIHR)},
			{version: 8, fwdFee: computedFwd, ihrFee: computedIHR},
		} {
			cfg := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
				tlb.ConfigParamGlobalVersion:               transactionTestGlobalVersionCell(t, tc.version),
				tlb.ConfigParamMsgForwardPricesBasechain:   priceCell,
				tlb.ConfigParamMsgForwardPricesMasterchain: priceCell,
			})

			res := applyTransactionSendActionForTestWithParams(t,
				tlb.ActionSendMsg{Mode: 0, Msg: msgCell},
				cfg,
				big.NewInt(1_000_000_000),
				nil,
				transactionZeroCurrencyBalance(),
			)
			if res.phase == nil || !res.phase.Success || res.phase.ResultCode != 0 || res.phase.MessagesCreated != 1 || len(res.outMsgs) != 1 {
				t.Fatalf("v%d unexpected action phase: %+v out=%d", tc.version, res.phase, len(res.outMsgs))
			}

			total := tc.fwdFee + tc.ihrFee
			if got := transactionBigOrZero(transactionCoinsNano(res.phase.TotalFwdFees)).Uint64(); got != total {
				t.Fatalf("v%d total fwd fees = %d, want %d", tc.version, got, total)
			}

			var outMsg tlb.Message
			if err = transactionParseCell(&outMsg, res.outMsgs[0]); err != nil {
				t.Fatalf("v%d parse outbound message: %v", tc.version, err)
			}
			internal := outMsg.AsInternal()
			if got := internal.FwdFee.Nano().Uint64(); got != tc.fwdFee {
				t.Fatalf("v%d outbound fwd fee = %d, want %d", tc.version, got, tc.fwdFee)
			}
			if got := internal.IHRFee.Nano().Uint64(); got != tc.ihrFee {
				t.Fatalf("v%d outbound IHR fee = %d, want %d", tc.version, got, tc.ihrFee)
			}
			if got := internal.Amount.Nano().Uint64(); got != amount-total {
				t.Fatalf("v%d outbound amount = %d, want %d", tc.version, got, amount-total)
			}
		}
	})
}

func FuzzTransactionVersionedSendExtraFlagsBoundaries(f *testing.F) {
	f.Add(byte(0), byte(0), byte(0), uint64(100))
	f.Add(byte(0), byte(1), byte(3), uint64(200))
	f.Add(byte(1), byte(0), byte(4), uint64(300))
	f.Add(byte(1), byte(1), byte(0xFF), uint64(400))
	f.Add(byte(2), byte(0), byte(0), uint64(500))
	f.Add(byte(3), byte(1), byte(7), uint64(600))

	f.Fuzz(func(t *testing.T, rawCase, rawMode, rawFlag byte, rawAmount uint64) {
		mode := uint8(0)
		if rawMode&1 != 0 {
			mode = 2
		}
		amount := rawAmount % 1_000_000

		var wantV12Flag uint64
		wantV12Err := false
		msgCell := transactionTestRelaxedInternalMessageWithFwdFee(func(b *cell.Builder) {
			b.MustStoreBigCoins(new(big.Int).SetUint64(amount))
		}, func(b *cell.Builder) {
			switch rawCase % 4 {
			case 0:
				wantV12Flag = uint64(rawFlag % 4)
				b.MustStoreBigCoins(new(big.Int).SetUint64(wantV12Flag))
			case 1:
				wantV12Flag = uint64(rawFlag%252) + 4
				b.MustStoreBigCoins(new(big.Int).SetUint64(wantV12Flag))
				wantV12Err = true
			case 2:
				b.MustStoreUInt(1, 4).MustStoreUInt(0, 8)
				wantV12Err = true
			default:
				b.MustStoreUInt(2, 4).MustStoreUInt(uint64(rawFlag), 16)
				wantV12Err = true
			}
		}, func(b *cell.Builder) {
			b.MustStoreBigCoins(big.NewInt(0))
		})

		v11 := applyTransactionSendActionForTestWithParams(t,
			tlb.ActionSendMsg{Mode: mode, Msg: msgCell},
			transactionFuzzSendExtraFlagsConfig(t, 11),
			big.NewInt(1_000_000_000),
			nil,
			transactionZeroCurrencyBalance(),
		)
		checkTransactionFuzzSendExtraFlagsPhase(t, 11, mode, v11, true, false)
		checkTransactionFuzzSendExtraFlagsOutMsg(t, 11, v11.outMsgs[0], 0)

		v12 := applyTransactionSendActionForTestWithParams(t,
			tlb.ActionSendMsg{Mode: mode, Msg: msgCell},
			transactionFuzzSendExtraFlagsConfig(t, 12),
			big.NewInt(1_000_000_000),
			nil,
			transactionZeroCurrencyBalance(),
		)
		if wantV12Err {
			checkTransactionFuzzSendExtraFlagsPhase(t, 12, mode, v12, false, true)
			return
		}

		checkTransactionFuzzSendExtraFlagsPhase(t, 12, mode, v12, true, false)
		checkTransactionFuzzSendExtraFlagsOutMsg(t, 12, v12.outMsgs[0], wantV12Flag)
	})
}

func FuzzTransactionActionGlobalVersionFallbackInvalidSource(f *testing.F) {
	for _, version := range transactionFuzzAllVersions {
		f.Add(byte(2), byte(version), true, byte(0x11))
	}
	f.Add(byte(0), byte(0), true, byte(0x11))
	f.Add(byte(1), byte(0), true, byte(0x22))
	f.Add(byte(2), byte(12), true, byte(0x33))
	f.Add(byte(2), byte(13), true, byte(0x44))
	f.Add(byte(2), byte(MaxSupportedGlobalVersion), false, byte(0x55))

	f.Fuzz(func(t *testing.T, rawConfigKind, rawVersion byte, ignoreErrors bool, payload byte) {
		version := uint32(transactionFuzzGlobalVersion(rawVersion))
		var cfg transactionConfig

		switch rawConfigKind % 4 {
		case 0:
			cfg = transactionConfigFromBlockchainConfig(tlb.BlockchainConfig{})
		case 1:
			cfg = transactionConfigFromBlockchainConfig(tlb.BlockchainConfig{Root: buildTransactionConfigRoot(t, map[uint32]*cell.Cell{})})
		case 2:
			cfg = transactionTestConfigWithGlobalVersion(t, version)
		default:
			cfg = transactionConfigFromBlockchainConfig(tlb.BlockchainConfig{Root: buildTransactionConfigRoot(t, map[uint32]*cell.Cell{
				tlb.ConfigParamGlobalVersion: cell.BeginCell().MustStoreUInt(uint64(payload&1), 1).EndCell(),
			})})
		}
		effectiveVersion := cfg.globalVersion()

		mode := uint8(0)
		if ignoreErrors {
			mode = 2
		}
		msg := buildTransactionOutboundInternalCellWithAddresses(
			t,
			address.NewAddressExt(0, 256, bytes.Repeat([]byte{payload}, 32)),
			tonopsTestAddr,
			1000,
			cell.BeginCell().MustStoreUInt(uint64(payload), 8).EndCell(),
		)

		res := applyTransactionSendActionForTestWithParams(t,
			tlb.ActionSendMsg{Mode: mode, Msg: msg},
			cfg,
			big.NewInt(1_000_000_000),
			nil,
			transactionZeroCurrencyBalance(),
		)
		if res.phase == nil {
			t.Fatalf("effective v%d missing action phase", effectiveVersion)
		}

		if ignoreErrors && effectiveVersion >= 13 {
			if !res.phase.Success || !res.phase.Valid || res.phase.ResultCode != 0 || res.phase.SkippedActions != 1 || res.phase.MessagesCreated != 0 {
				t.Fatalf("effective v%d invalid source skip phase = %+v", effectiveVersion, res.phase)
			}
			return
		}
		if res.phase.Success || !res.phase.Valid || res.phase.ResultCode != 35 || res.phase.SkippedActions != 0 || res.phase.MessagesCreated != 0 {
			t.Fatalf("effective v%d invalid source error phase = %+v", effectiveVersion, res.phase)
		}
	})
}

func FuzzTransactionVersionedInvalidSourceMode2(f *testing.F) {
	for _, version := range transactionFuzzAllVersions {
		f.Add(byte(version), version >= 13)
	}
	f.Add(byte(MinSupportedGlobalVersion), false)
	f.Add(byte(12), false)
	f.Add(byte(12), true)
	f.Add(byte(13), true)
	f.Add(byte(MaxSupportedGlobalVersion), true)

	f.Fuzz(func(t *testing.T, rawVersion byte, ignoreErrors bool) {
		version := transactionFuzzGlobalVersion(rawVersion)
		mode := uint8(0)
		if ignoreErrors {
			mode = 2
		}
		msg := buildTransactionOutboundInternalCellWithAddresses(
			t,
			address.NewAddressExt(0, 256, bytes.Repeat([]byte{0x22}, 32)),
			tonopsTestAddr,
			1000,
			cell.BeginCell().MustStoreUInt(0xB0, 8).EndCell(),
		)

		res := applyTransactionSendActionForTestWithParams(t,
			tlb.ActionSendMsg{Mode: mode, Msg: msg},
			transactionTestConfigWithGlobalVersion(t, version),
			big.NewInt(1_000_000_000),
			nil,
			transactionZeroCurrencyBalance(),
		)

		if res.phase == nil {
			t.Fatalf("v%d missing action phase", version)
		}
		if ignoreErrors && version >= 13 {
			if !res.phase.Success || !res.phase.Valid || res.phase.ResultCode != 0 || res.phase.SkippedActions != 1 || res.phase.MessagesCreated != 0 {
				t.Fatalf("v%d invalid source skip phase = %+v", version, res.phase)
			}
			return
		}
		if res.phase.Success || !res.phase.Valid || res.phase.ResultCode != 35 || res.phase.SkippedActions != 0 || res.phase.MessagesCreated != 0 {
			t.Fatalf("v%d invalid source error phase = %+v", version, res.phase)
		}
	})
}

func FuzzTransactionVersionedInvalidDestinationMode2(f *testing.F) {
	for _, version := range transactionFuzzAllVersions {
		f.Add(byte(version), byte(1), true)
	}
	f.Add(byte(MinSupportedGlobalVersion), byte(0), true)
	f.Add(byte(7), byte(0), true)
	f.Add(byte(7), byte(1), true)
	f.Add(byte(8), byte(0), true)
	f.Add(byte(12), byte(1), false)
	f.Add(byte(MaxSupportedGlobalVersion), byte(1), true)

	f.Fuzz(func(t *testing.T, rawVersion, rawCase byte, ignoreErrors bool) {
		version := transactionFuzzGlobalVersion(rawVersion)
		mode := uint8(0)
		if ignoreErrors {
			mode = 2
		}

		dst := address.NewAddress(0, 1, bytes.Repeat([]byte{rawCase}, 32))
		acceptMessages := false
		if rawCase%2 != 0 {
			dst = address.NewAddressVar(0, 1, 255, bytes.Repeat([]byte{rawCase}, 32))
			acceptMessages = true
		}
		msg := buildTransactionOutboundInternalCellWithAddresses(
			t,
			address.NewAddressNone(),
			dst,
			1000,
			cell.BeginCell().MustStoreUInt(0xB0, 8).EndCell(),
		)

		res := applyTransactionSendActionForTestWithParams(t,
			tlb.ActionSendMsg{Mode: mode, Msg: msg},
			transactionFuzzWorkchainConfig(t, version, 1, acceptMessages),
			big.NewInt(1_000_000_000),
			nil,
			transactionZeroCurrencyBalance(),
		)

		if res.phase == nil {
			t.Fatalf("v%d missing action phase", version)
		}
		if ignoreErrors {
			wantSkipped := uint16(0)
			if version >= 8 {
				wantSkipped = 1
			}
			if !res.phase.Success || !res.phase.Valid || res.phase.ResultCode != 0 || res.phase.SkippedActions != wantSkipped || res.phase.MessagesCreated != 0 {
				t.Fatalf("v%d invalid destination skip phase = %+v, want skipped=%d", version, res.phase, wantSkipped)
			}
			return
		}
		if res.phase.Success || !res.phase.Valid || res.phase.ResultCode != 36 || res.phase.SkippedActions != 0 || res.phase.MessagesCreated != 0 {
			t.Fatalf("v%d invalid destination error phase = %+v", version, res.phase)
		}
	})
}

func FuzzTransactionVersionedVarDestinationNormalization(f *testing.F) {
	for _, version := range transactionFuzzAllVersions {
		f.Add(byte(version), int32(0), uint16(256), byte(0x11))
	}
	f.Add(byte(0), int32(0), uint16(256), byte(0x11))
	f.Add(byte(9), int32(-128), uint16(256), byte(0x22))
	f.Add(byte(10), int32(127), uint16(256), byte(0x33))
	f.Add(byte(MaxSupportedGlobalVersion), int32(128), uint16(256), byte(0x44))
	f.Add(byte(MaxSupportedGlobalVersion), int32(0), uint16(255), byte(0x55))

	f.Fuzz(func(t *testing.T, rawVersion byte, workchain int32, rawBits uint16, fill byte) {
		version := transactionFuzzGlobalVersion(rawVersion)
		bits := uint(rawBits)
		if bits != 255 && bits != 256 {
			bits = uint(rawBits%2) + 255
		}
		data := bytes.Repeat([]byte{fill}, 32)
		dst := address.NewAddressVar(0, workchain, bits, data)
		msgCell := buildTransactionOutboundInternalCellWithAddresses(
			t,
			address.NewAddressNone(),
			dst,
			100,
			cell.BeginCell().EndCell(),
		)

		normalized, err := transactionNormalizeOutboundMessage(msgCell, tonopsTestAddr, 10, uint32(tonopsTestTime.Unix()), transactionTestConfigWithGlobalVersion(t, version))
		if workchain == address.MasterchainID && bits != 256 {
			if !errors.Is(err, errTransactionInvalidDestination) {
				t.Fatalf("v%d masterchain var bits=%d normalize error = %v, want invalid destination", version, bits, err)
			}
			return
		}
		if err != nil {
			t.Fatalf("v%d var destination normalize failed: %v", version, err)
		}

		var msg tlb.Message
		if err = transactionParseCell(&msg, normalized); err != nil {
			t.Fatal(err)
		}
		got := msg.AsInternal().DstAddr
		wantStd := bits == 256 && workchain >= -128 && workchain < 128
		wantData := transactionFuzzAddressDataForBits(data, bits)
		if wantStd {
			if got.Type() != address.StdAddress || got.Workchain() != workchain || got.BitsLen() != 256 || !bytes.Equal(got.Data(), wantData) {
				t.Fatalf("v%d var destination normalized to type=%v wc=%d bits=%d data=%x, want std wc=%d data=%x", version, got.Type(), got.Workchain(), got.BitsLen(), got.Data(), workchain, wantData)
			}
			return
		}
		if got.Type() != address.VarAddress || got.Workchain() != workchain || got.BitsLen() != bits || !bytes.Equal(got.Data(), wantData) {
			t.Fatalf("v%d var destination = type=%v wc=%d bits=%d data=%x, want var wc=%d bits=%d data=%x", version, got.Type(), got.Workchain(), got.BitsLen(), got.Data(), workchain, bits, wantData)
		}
	})
}

func FuzzTransactionVersionedInternalAddressWorkchainDescriptors(f *testing.F) {
	for _, version := range transactionFuzzAllVersions {
		f.Add(byte(version), byte(0), uint16(256), byte(0x11))
	}
	f.Add(byte(0), byte(0), uint16(256), byte(0x11))
	f.Add(byte(9), byte(1), uint16(255), byte(0x22))
	f.Add(byte(10), byte(2), uint16(256), byte(0x33))
	f.Add(byte(MaxSupportedGlobalVersion), byte(3), uint16(255), byte(0x44))

	f.Fuzz(func(t *testing.T, rawVersion, rawCase byte, rawBits uint16, fill byte) {
		version := transactionFuzzGlobalVersion(rawVersion)
		workchain := int32(rawCase%2) + 1
		bits := uint(256)
		if rawBits%2 != 0 {
			bits = 255
		}
		data := bytes.Repeat([]byte{fill}, 32)
		dst := address.NewAddressVar(0, workchain, bits, data)

		got, ok := transactionValidateAndNormalizeInternalDestAddr(dst, transactionTestConfigWithGlobalVersion(t, version), tonopsTestAddr)
		if !ok {
			t.Fatalf("v%d partial config rejected workchain=%d bits=%d", version, workchain, bits)
		}
		checkTransactionFuzzValidatedInternalAddress(t, version, "partial", got, workchain, bits, data)

		got, ok = transactionValidateAndNormalizeInternalDestAddr(dst, transactionFuzzWorkchainConfig(t, version, workchain, true), tonopsTestAddr)
		if bits != 256 {
			if ok || got != nil {
				t.Fatalf("v%d basic descriptor accepted workchain=%d bits=%d", version, workchain, bits)
			}
		} else {
			if !ok {
				t.Fatalf("v%d basic descriptor rejected workchain=%d bits=%d", version, workchain, bits)
			}
			checkTransactionFuzzValidatedInternalAddress(t, version, "basic", got, workchain, bits, data)
		}

		got, ok = transactionValidateAndNormalizeInternalDestAddr(dst, transactionFuzzWorkchainConfig(t, version, workchain, false), tonopsTestAddr)
		if ok || got != nil {
			t.Fatalf("v%d disabled descriptor accepted workchain=%d bits=%d", version, workchain, bits)
		}

		got, ok = transactionValidateAndNormalizeInternalDestAddr(dst, transactionFuzzWorkchainConfig(t, version, workchain+2, true), tonopsTestAddr)
		if ok || got != nil {
			t.Fatalf("v%d missing descriptor accepted workchain=%d bits=%d", version, workchain, bits)
		}
	})
}

func checkTransactionFuzzValidatedInternalAddress(t *testing.T, version uint32, name string, got *address.Address, workchain int32, bits uint, data []byte) {
	t.Helper()

	if bits == 256 && workchain >= -128 && workchain < 128 {
		if got.Type() != address.StdAddress || got.Workchain() != workchain || got.BitsLen() != 256 || !bytes.Equal(got.Data(), data) {
			t.Fatalf("v%d %s address = type=%v wc=%d bits=%d data=%x, want std wc=%d data=%x", version, name, got.Type(), got.Workchain(), got.BitsLen(), got.Data(), workchain, data)
		}
		return
	}
	if got.Type() != address.VarAddress || got.Workchain() != workchain || got.BitsLen() != bits || !bytes.Equal(got.Data(), data) {
		t.Fatalf("v%d %s address = type=%v wc=%d bits=%d data=%x, want var wc=%d bits=%d data=%x", version, name, got.Type(), got.Workchain(), got.BitsLen(), got.Data(), workchain, bits, data)
	}
}

func FuzzTransactionVersionedInboundIHRFeeCredit(f *testing.F) {
	f.Add(uint64(0), uint64(1))
	f.Add(uint64(100), uint64(23))
	f.Add(uint64(1_000_000), uint64(999_999))

	f.Fuzz(func(t *testing.T, rawAmount, rawIHRFee uint64) {
		amount := rawAmount % 1_000_000
		ihrFee := rawIHRFee % 1_000_000
		msg := &tlb.Message{
			MsgType: tlb.MsgTypeInternal,
			Msg: &tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(amount),
				IHRFee:      tlb.FromNanoTONU(ihrFee),
			},
		}

		for _, version := range transactionFuzzAllVersions {
			want := amount + ihrFee
			if version >= 12 {
				want = amount
			}
			prepared, err := transactionPrepareInitialPhases(&transactionRuntimeAccount{
				addr:    tonopsTestAddr,
				status:  tlb.AccountStatusActive,
				balance: big.NewInt(0),
				storageInfo: tlb.StorageInfo{
					StorageExtra: tlb.StorageExtraNone{},
				},
			}, msg, big.NewInt(0), big.NewInt(0), uint32(tonopsTestTime.Unix()), transactionTestConfigWithGlobalVersion(t, version), transactionStorageDueLimits{})
			if err != nil {
				t.Fatal(err)
			}
			if got := prepared.msgBalance.grams.Uint64(); got != want {
				t.Fatalf("v%d message balance = %d, want %d", version, got, want)
			}
			if got := prepared.creditPhase.Credit.Coins.Nano().Uint64(); got != want {
				t.Fatalf("v%d credit phase = %d, want %d", version, got, want)
			}
		}
	})
}

func FuzzTransactionVersionedGasLimitBoundaries(f *testing.F) {
	f.Add(uint64(200), uint64(10_000_000_000_000), byte(0), uint32(0))
	f.Add(uint64(0), uint64(100), byte(1), uint32(1))
	f.Add(uint64(5000), uint64(777), byte(0), uint32(2))

	f.Fuzz(func(t *testing.T, rawSpecialMsg, rawBalance uint64, rawAddrCase byte, rawNowDelta uint32) {
		specialPrices := &tlb.ConfigGasLimitsPrices{
			HasFlatPricing:          true,
			FlatGasLimit:            10,
			FlatGasPrice:            100,
			HasSeparateSpecialLimit: true,
			GasPrice:                1 << 16,
			GasLimit:                1_000_000,
			SpecialGasLimit:         1_000,
			BlockGasLimit:           1_000_000,
		}
		specialGasCell, err := tlb.ToCell(specialPrices)
		if err != nil {
			t.Fatalf("failed to build special gas config: %v", err)
		}
		msgBalance := new(big.Int).SetUint64(rawSpecialMsg % 10_000)
		for _, version := range transactionFuzzAllVersions {
			want := min(transactionGasInt(transactionGasBoughtFor(specialPrices, msgBalance)), transactionGasInt(specialPrices.SpecialGasLimit))
			if version >= 5 {
				want = transactionGasInt(specialPrices.SpecialGasLimit)
			}

			cfg := transactionFuzzGasConfig(t, version, specialGasCell)
			gas := transactionMessageGas(TransactionEmulationConfig{}, cfg, tonopsTestAddr, big.NewInt(10_000), msgBalance, tlb.MsgTypeInternal, true)
			if gas.Max != transactionGasInt(specialPrices.SpecialGasLimit) || gas.Limit != want || gas.Remaining != want {
				t.Fatalf("special v%d gas = %+v, want max=%d limit=%d", version, gas, specialPrices.SpecialGasLimit, want)
			}
		}

		overridePrices := &tlb.ConfigGasLimitsPrices{
			GasPrice:      1,
			GasLimit:      1_000_000,
			BlockGasLimit: 100_000_000,
		}
		overrideGasCell, err := tlb.ToCell(overridePrices)
		if err != nil {
			t.Fatalf("failed to build override gas config: %v", err)
		}
		overrideCases := []struct {
			addr        *address.Address
			fromVersion uint32
			until       uint32
			limit       uint64
		}{
			{
				addr:        address.MustParseRawAddr("0:FFBFD8F5AE5B2E1C7C3614885CB02145483DFAEE575F0DD08A72C366369211CD"),
				fromVersion: 5,
				until:       1_709_164_800,
				limit:       70_000_000,
			},
			{
				addr:        address.MustParseRawAddr("0:5E4A5F9DBA638789E6770C990D2959237ACA3BC19D15A734782C26CB19343CC6"),
				fromVersion: 9,
				until:       1_740_787_200,
				limit:       70_000_000,
			},
		}
		override := overrideCases[int(rawAddrCase>>1)%len(overrideCases)]
		addr := override.addr
		if rawAddrCase&1 != 0 {
			data := append([]byte(nil), override.addr.Data()...)
			data[31] ^= 1
			addr = address.NewAddress(0, byte(override.addr.Workchain()), data)
		}

		balance := new(big.Int).SetUint64(rawBalance%10_000_000 + 1)
		activeNow := override.until - 1 - rawNowDelta%2
		expiredNow := override.until + rawNowDelta%2
		for _, now := range []uint32{activeNow, expiredNow} {
			for _, version := range transactionFuzzAllVersions {
				limit := overridePrices.GasLimit
				if rawAddrCase&1 == 0 && version >= override.fromVersion && now < override.until {
					limit = override.limit
				}

				cfg := transactionFuzzGasConfig(t, version, overrideGasCell)
				got := transactionGasBoughtForAccount(cfg, overridePrices, balance, addr, now)
				want := transactionGasBoughtForLimit(overridePrices, balance, limit)
				if got != want {
					t.Fatalf("historical v%d now=%d gas = %d, want %d", version, now, got, want)
				}
			}
		}
	})
}

func FuzzTransactionVersionedTickTockGasBoundaries(f *testing.F) {
	f.Add(byte(0), uint64(100), uint64(10), uint64(100), uint64(1<<16), uint64(1000), uint32(0))
	f.Add(byte(1), uint64(1), uint64(0), uint64(0), uint64(1), uint64(0), uint32(1))
	f.Add(byte(2), uint64(10_000_000), uint64(50), uint64(7), uint64(333), uint64(70_000_000), uint32(2))
	f.Add(byte(7), uint64(999_999), uint64(3), uint64(11), uint64(1<<17), uint64(500), uint32(3))

	f.Fuzz(func(t *testing.T, rawFlags byte, rawBalance, rawFlatLimit, rawFlatPrice, rawGasPrice, rawGasLimit uint64, rawNowDelta uint32) {
		explicit := vmcore.Gas{
			Max:       int64(rawBalance%1000 + 1),
			Limit:     int64(rawFlatLimit % 1000),
			Credit:    int64(rawFlags & 7),
			Base:      int64(rawFlatPrice % 1000),
			Remaining: int64(rawGasPrice % 1000),
		}
		if got := transactionTickTockGas(TransactionEmulationConfig{Gas: explicit}, transactionConfig{}, tonopsTestAddr, big.NewInt(0), false); got != explicit {
			t.Fatalf("explicit ticktock gas = %+v, want %+v", got, explicit)
		}

		fallback := transactionTickTockGas(TransactionEmulationConfig{}, transactionConfig{}, tonopsTestAddr, big.NewInt(0), false)
		if fallback.Max != DefaultTickTockTransactionGasMax || fallback.Limit != DefaultTickTockTransactionGasMax || fallback.Credit != 0 || fallback.Remaining != DefaultTickTockTransactionGasMax {
			t.Fatalf("fallback ticktock gas = %+v", fallback)
		}

		gasLimit := rawGasLimit%2_000_000 + 1
		flatLimit := rawFlatLimit % (gasLimit + 1)
		prices := &tlb.ConfigGasLimitsPrices{
			HasFlatPricing:          true,
			FlatGasLimit:            flatLimit,
			FlatGasPrice:            rawFlatPrice % 10_000,
			HasSeparateSpecialLimit: true,
			GasPrice:                rawGasPrice%100_000 + 1,
			GasLimit:                gasLimit,
			SpecialGasLimit:         rawGasLimit%10_000 + 1,
			BlockGasLimit:           gasLimit + 10_000,
		}
		if prices.GasLimit <= prices.FlatGasLimit {
			if got := transactionMaxGasThresholdForLimit(prices, prices.GasLimit); got.Uint64() != transactionGasFlatPrice(prices) {
				t.Fatalf("flat threshold = %s, want flat price %d", got, prices.FlatGasPrice)
			}
		}
		if transactionGasFlatPrice(nil) != 0 {
			t.Fatal("nil gas prices flat price should be zero")
		}

		gasCell, err := tlb.ToCell(prices)
		if err != nil {
			t.Fatalf("failed to build gas config: %v", err)
		}

		override := transactionGasLimitOverrides[int(rawFlags>>1)%len(transactionGasLimitOverrides)]
		addr := tonopsTestAddr
		if rawFlags&1 == 0 {
			addr = override.addr
		}
		now := override.until - 1 - rawNowDelta%2
		if rawFlags&0x40 != 0 {
			now = override.until + rawNowDelta%2
		}
		balance := new(big.Int).SetUint64(rawBalance % 10_000_000)

		for _, version := range transactionFuzzAllVersions {
			cfg := transactionFuzzGasConfig(t, version, gasCell)

			limit := prices.GasLimit
			if rawFlags&1 == 0 && version >= override.fromVersion && now < override.until {
				limit = override.limit
			}
			wantLimit := transactionGasBoughtForLimit(prices, balance, limit)
			got := transactionTickTockGas(TransactionEmulationConfig{Now: now}, cfg, addr, balance, false)
			if got.Max != transactionGasInt(wantLimit) || got.Limit != transactionGasInt(wantLimit) || got.Credit != 0 || got.Remaining != transactionGasInt(wantLimit) {
				t.Fatalf("v%d ticktock gas = %+v, want limit %d", version, got, wantLimit)
			}

			got = transactionTickTockGas(TransactionEmulationConfig{Now: now}, cfg, addr, balance, true)
			wantSpecial := transactionGasInt(prices.SpecialGasLimit)
			if got.Max != wantSpecial || got.Limit != wantSpecial || got.Credit != 0 || got.Remaining != wantSpecial {
				t.Fatalf("v%d special ticktock gas = %+v, want %d", version, got, wantSpecial)
			}
		}
	})
}

func FuzzTransactionVersionedPrecompiledGasConfig(f *testing.F) {
	for _, version := range transactionFuzzAllVersions {
		f.Add(byte(version), uint64(7), uint64(10), false)
	}
	f.Add(byte(MinSupportedGlobalVersion), uint64(7), uint64(10), false)
	f.Add(byte(13), uint64(7), uint64(10), false)
	f.Add(byte(13), uint64(11), uint64(10), false)
	f.Add(byte(MaxSupportedGlobalVersion), uint64(10), uint64(10), true)
	f.Add(byte(MaxSupportedGlobalVersion), uint64(0), uint64(0), false)

	f.Fuzz(func(t *testing.T, rawVersion byte, rawUsage, rawLimit uint64, credit bool) {
		version := transactionFuzzGlobalVersion(rawVersion)
		usage := rawUsage % 1_000
		limit := rawLimit % 1_000
		code := cell.BeginCell().MustStoreUInt(0xA3, 8).EndCell()
		cfg := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
			tlb.ConfigParamGlobalVersion:             transactionTestGlobalVersionCell(t, version),
			tlb.ConfigParamPrecompiledContracts:      buildTransactionV13PrecompiledConfig(t, code, usage),
			tlb.ConfigParamGasPricesBasechain:        buildTransactionGasLimitsCell(t, 100, 500),
			tlb.ConfigParamGasPricesMasterchain:      buildTransactionGasLimitsCell(t, 100, 500),
			tlb.ConfigParamMsgForwardPricesBasechain: buildTransactionMsgForwardPricesCell(t, 0, 0),
		})
		gas := vmcore.Gas{
			Max:       int64(limit),
			Limit:     int64(limit),
			Base:      int64(limit),
			Remaining: int64(limit),
		}
		if credit {
			gas.Credit = 1
		}

		nextGas, gotUsage, skip, err := transactionApplyPrecompiledGasConfig(TransactionEmulationConfig{}, cfg, code, gas)
		if err != nil {
			t.Fatal(err)
		}
		if gotUsage == nil || gotUsage.Uint64() != usage {
			t.Fatalf("v%d precompiled usage = %v, want %d", version, gotUsage, usage)
		}
		if usage > limit {
			if skip == nil || skip.Type != tlb.ComputeSkipReasonNoGas {
				t.Fatalf("v%d skip = %v, want no_gas", version, skip)
			}
			if nextGas != gas {
				t.Fatalf("v%d gas changed on no_gas: got %+v want %+v", version, nextGas, gas)
			}
			return
		}
		if skip != nil {
			t.Fatalf("v%d skip = %v, want nil", version, skip)
		}
		if limit == 0 {
			if nextGas != gas {
				t.Fatalf("v%d fallback gas with zero limit = %+v, want unchanged %+v", version, nextGas, gas)
			}
			return
		}
		wantLimit := int64(limit)
		wantCredit := int64(0)
		if credit {
			wantCredit = wantLimit
		}
		if nextGas.Limit != wantLimit || nextGas.Max != wantLimit || nextGas.Base != wantLimit+wantCredit || nextGas.Remaining != wantLimit+wantCredit || nextGas.Credit != wantCredit {
			t.Fatalf("v%d fallback gas = %+v, want limit=%d credit=%d", version, nextGas, wantLimit, wantCredit)
		}
	})
}

func FuzzTransactionVersionedPrecompiledGasUsageBoundaries(f *testing.F) {
	f.Add(byte(0), uint64(0), byte(0x11))
	f.Add(byte(1), uint64(7), byte(0x22))
	f.Add(byte(2), uint64(7), byte(0x33))
	f.Add(byte(3), uint64(7), byte(0x44))
	f.Add(byte(4), uint64(9), byte(0x55))
	f.Add(byte(5), uint64(11), byte(0x66))
	f.Add(byte(6), uint64(13), byte(0x77))
	f.Add(byte(7), uint64(17), byte(0x88))
	f.Add(byte(8), uint64(19), byte(0x99))
	f.Add(byte(9), uint64(23), byte(0xAA))

	f.Fuzz(func(t *testing.T, rawCase byte, rawUsage uint64, rawSeed byte) {
		code := cell.BeginCell().MustStoreUInt(uint64(rawSeed), 8).EndCell()
		otherCode := cell.BeginCell().MustStoreUInt(uint64(rawSeed)^0xff, 8).EndCell()
		usage := int64(rawUsage%1_000 + 1)
		limit := int64(rawUsage%1_000 + 10)
		gas := vmcore.Gas{Max: limit + 50, Limit: limit, Base: limit, Remaining: limit}
		cfg := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
			tlb.ConfigParamPrecompiledContracts: buildTransactionV13PrecompiledConfig(t, code, uint64(usage)),
		})

		switch rawCase % 10 {
		case 0:
			got, ok, err := transactionPrecompiledGasUsage(nil)
			if err != nil || ok || got != 0 {
				t.Fatalf("nil precompiled usage = %d ok=%t err=%v, want empty success", got, ok, err)
			}
		case 1:
			got, ok, err := transactionPrecompiledGasUsage(big.NewInt(usage))
			if err != nil || !ok || got != usage {
				t.Fatalf("valid precompiled usage = %d ok=%t err=%v, want %d", got, ok, err, usage)
			}
		case 2:
			if _, _, err := transactionPrecompiledGasUsage(big.NewInt(-usage)); err == nil {
				t.Fatal("negative precompiled usage should fail")
			}
		case 3:
			if _, _, err := transactionPrecompiledGasUsage(new(big.Int).Lsh(big.NewInt(1), 80)); err == nil {
				t.Fatal("overflow precompiled usage should fail")
			}
		case 4:
			nextGas, gotUsage, skip, err := transactionApplyPrecompiledGasConfig(TransactionEmulationConfig{
				IncomingValue: tuple.NewTupleValue(big.NewInt(1), nil),
			}, cfg, code, gas)
			if err != nil || gotUsage != nil || skip != nil || nextGas != gas {
				t.Fatalf("explicit C7 precompiled config = gas %+v usage %v skip %v err %v, want untouched", nextGas, gotUsage, skip, err)
			}
		case 5:
			nextGas, gotUsage, skip, err := transactionApplyPrecompiledGasConfig(TransactionEmulationConfig{}, cfg, nil, gas)
			if err != nil || gotUsage != nil || skip != nil || nextGas != gas {
				t.Fatalf("nil-code precompiled config = gas %+v usage %v skip %v err %v, want untouched", nextGas, gotUsage, skip, err)
			}
		case 6:
			nextGas, gotUsage, skip, err := transactionApplyPrecompiledGasConfig(TransactionEmulationConfig{}, cfg, otherCode, gas)
			if err != nil || gotUsage != nil || skip != nil || nextGas != gas {
				t.Fatalf("missing-code precompiled config = gas %+v usage %v skip %v err %v, want untouched", nextGas, gotUsage, skip, err)
			}
		case 7:
			if _, gotUsage, skip, err := transactionApplyPrecompiledGasConfig(TransactionEmulationConfig{
				PrecompiledGasUsage: big.NewInt(-usage),
			}, cfg, code, gas); err == nil || gotUsage == nil || skip != nil {
				t.Fatalf("negative explicit usage = usage %v skip %v err %v, want error with usage", gotUsage, skip, err)
			}
		case 8:
			if _, gotUsage, skip, err := transactionApplyPrecompiledGasConfig(TransactionEmulationConfig{
				PrecompiledGasUsage: new(big.Int).Lsh(big.NewInt(1), 80),
			}, cfg, code, gas); err == nil || gotUsage == nil || skip != nil {
				t.Fatalf("overflow explicit usage = usage %v skip %v err %v, want error with usage", gotUsage, skip, err)
			}
		case 9:
			explicit := big.NewInt(limit + 1)
			nextGas, gotUsage, skip, err := transactionApplyPrecompiledGasConfig(TransactionEmulationConfig{
				PrecompiledGasUsage: explicit,
			}, cfg, code, gas)
			if err != nil || gotUsage != explicit || skip == nil || skip.Type != tlb.ComputeSkipReasonNoGas || nextGas != gas {
				t.Fatalf("explicit no-gas usage = gas %+v usage %v skip %v err %v", nextGas, gotUsage, skip, err)
			}

			res := &MessageExecutionResult{ExecutionResult: ExecutionResult{ExitCode: 0, GasUsed: 5, Steps: 7}}
			if err = transactionApplyPrecompiledGasUsage(res, big.NewInt(usage)); err != nil {
				t.Fatalf("apply precompiled usage failed: %v", err)
			}
			if res.GasUsed != usage || res.Steps != 0 {
				t.Fatalf("applied precompiled usage gas=%d steps=%d, want %d/0", res.GasUsed, res.Steps, usage)
			}

			outOfGas := &MessageExecutionResult{ExecutionResult: ExecutionResult{ExitCode: ^int64(vmerr.CodeOutOfGas), GasUsed: 5, Steps: 7}}
			if err = transactionApplyPrecompiledGasUsage(outOfGas, big.NewInt(usage)); err != nil {
				t.Fatalf("apply out-of-gas precompiled usage failed: %v", err)
			}
			if outOfGas.GasUsed != 5 || outOfGas.Steps != 7 {
				t.Fatalf("out-of-gas precompiled usage changed gas=%d steps=%d", outOfGas.GasUsed, outOfGas.Steps)
			}
		}
	})
}

func FuzzTransactionVersionedFailedActionMessageBalance(f *testing.F) {
	f.Add(byte(0), uint64(500), uint64(1), false, uint64(0))
	f.Add(byte(1), uint64(500), uint64(1), true, uint64(11))
	f.Add(byte(2), uint64(777), uint64(0), true, uint64(23))
	f.Add(byte(3), uint64(999), uint64(9), false, uint64(0))

	modes := []uint8{0, 64, 65, 128}

	f.Fuzz(func(t *testing.T, rawMode byte, rawMsgBalance, rawFirstAmount uint64, hasExtra bool, rawExtraAmount uint64) {
		mode := modes[int(rawMode)%len(modes)]
		var extra *cell.Dictionary
		wantExtra := uint64(0)
		if hasExtra {
			wantExtra = rawExtraAmount%1_000 + 1
			extra = makeTransactionExtraCurrencies(t, 7, wantExtra)
		}
		msgBalance, err := transactionCurrencyFromParts(new(big.Int).SetUint64(rawMsgBalance%1_000), extra)
		if err != nil {
			t.Fatal(err)
		}
		wantOriginalMsgBalance := msgBalance.copy()
		firstAmount := rawFirstAmount % 10
		actions := buildTransactionActionList(t,
			tlb.ActionSendMsg{Mode: mode, Msg: buildTransactionOutboundInternalCell(t, firstAmount)},
			tlb.ActionSendMsg{Mode: 16, Msg: buildTransactionOutboundInternalCell(t, 2_000_000)},
		)
		acc := &transactionRuntimeAccount{
			addr:    tonopsTestAddr,
			status:  tlb.AccountStatusActive,
			code:    cell.BeginCell().EndCell(),
			data:    cell.BeginCell().EndCell(),
			balance: big.NewInt(1_000_000),
		}

		for _, version := range transactionFuzzAllVersions {
			res, err := transactionApplyActions(acc, &MessageExecutionResult{
				Accepted: true,
				ExecutionResult: ExecutionResult{
					ExitCode:  0,
					Data:      cell.BeginCell().EndCell(),
					Actions:   actions,
					Committed: true,
				},
			}, uint64(transactionTestLogicalTime), uint32(tonopsTestTime.Unix()), transactionTestConfigWithGlobalVersion(t, version), big.NewInt(1_000_000), nil, msgBalance, big.NewInt(0))
			if err != nil {
				t.Fatalf("apply actions v%d failed: %v", version, err)
			}
			wantCode := int32(34)
			wantBounce := false
			if version >= 4 {
				wantCode = 37
				wantBounce = true
			}
			if res.phase == nil || res.phase.Success || res.phase.ResultCode != wantCode || res.bounce != wantBounce {
				t.Fatalf("unexpected action failure v%d: phase=%+v bounce=%t", version, res.phase, res.bounce)
			}

			want := msgBalance.grams.Uint64()
			if mode&0xc0 != 0 && version < 14 {
				want = 0
			}
			if got := res.msgBalanceRemaining.grams.Uint64(); got != want {
				t.Fatalf("v%d mode=%d message balance remaining = %d, want %d", version, mode, got, want)
			}
			if hasExtra {
				got := res.msgBalanceRemaining.extra[7]
				if got == nil || got.Uint64() != wantExtra {
					t.Fatalf("v%d mode=%d message balance extra = %v, want 7:%d", version, mode, res.msgBalanceRemaining.extra, wantExtra)
				}
			} else if len(res.msgBalanceRemaining.extra) != 0 {
				t.Fatalf("v%d mode=%d message balance extra = %v, want empty", version, mode, res.msgBalanceRemaining.extra)
			}
			assertTransactionFuzzMessageBalanceUnchanged(t, msgBalance, wantOriginalMsgBalance, version, "failed-action")
		}
	})
}

func FuzzTransactionVersionedStateLimitFailureMessageBalance(f *testing.F) {
	for version := uint32(MinSupportedGlobalVersion); version <= uint32(MaxSupportedGlobalVersion); version++ {
		f.Add(uint8(version), uint8(0), uint64(500), uint64(1), false, uint64(0))
		f.Add(uint8(version), uint8(1), uint64(777), uint64(9), true, uint64(11))
		f.Add(uint8(version), uint8(2), uint64(999), uint64(5), true, uint64(23))
	}

	modes := []uint8{64, 65, 128}

	f.Fuzz(func(t *testing.T, rawVersion, rawMode byte, rawMsgBalance, rawFirstAmount uint64, hasExtra bool, rawExtraAmount uint64) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		mode := modes[int(rawMode)%len(modes)]
		msgBalanceAmount := rawMsgBalance%1_000_000 + 1_000
		firstAmount := rawFirstAmount%100 + 1
		var extra *cell.Dictionary
		wantExtra := uint64(0)
		if hasExtra {
			wantExtra = rawExtraAmount%1_000 + 1
			extra = makeTransactionExtraCurrencies(t, 7, wantExtra)
		}
		msgBalance, err := transactionCurrencyFromParts(new(big.Int).SetUint64(msgBalanceAmount), extra)
		if err != nil {
			t.Fatal(err)
		}
		wantOriginalMsgBalance := msgBalance.copy()
		data := cell.BeginCell().EndCell()
		oversizedCode := cell.BeginCell().
			MustStoreUInt(0xDD, 8).
			MustStoreRef(cell.BeginCell().EndCell()).
			EndCell()
		actions := buildTransactionActionList(t,
			tlb.ActionSendMsg{Mode: mode, Msg: buildTransactionOutboundInternalCell(t, firstAmount)},
			tlb.ActionSetCode{NewCode: oversizedCode},
		)
		acc := &transactionRuntimeAccount{
			addr:    tonopsTestAddr,
			status:  tlb.AccountStatusActive,
			code:    cell.BeginCell().EndCell(),
			data:    data,
			balance: big.NewInt(10_000_000),
		}

		res, err := transactionApplyActions(acc, &MessageExecutionResult{
			Accepted: true,
			ExecutionResult: ExecutionResult{
				ExitCode:  0,
				Data:      data,
				Actions:   actions,
				Committed: true,
			},
		}, uint64(transactionTestLogicalTime), uint32(tonopsTestTime.Unix()), transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
			tlb.ConfigParamGlobalVersion: transactionTestGlobalVersionCell(t, version),
			tlb.ConfigParamSizeLimits:    buildTransactionSizeLimitsCell(t, 1<<21, 1<<13, 1000, 1, 1),
		}), big.NewInt(10_000_000), nil, msgBalance, big.NewInt(0))
		if err != nil {
			t.Fatalf("apply actions v%d failed: %v", version, err)
		}
		if res.phase == nil || res.phase.Success || res.phase.ResultCode != 50 || res.phase.MessagesCreated != 1 || len(res.outMsgs) != 0 {
			t.Fatalf("unexpected state-limit failure v%d: phase=%+v out=%d", version, res.phase, len(res.outMsgs))
		}

		want := uint64(0)
		if version >= 14 {
			want = msgBalanceAmount
		}
		if got := res.msgBalanceRemaining.grams.Uint64(); got != want {
			t.Fatalf("v%d mode=%d state-limit message balance remaining = %d, want %d", version, mode, got, want)
		}
		if hasExtra {
			got := res.msgBalanceRemaining.extra[7]
			if got == nil || got.Uint64() != wantExtra {
				t.Fatalf("v%d mode=%d state-limit message balance extra = %v, want 7:%d", version, mode, res.msgBalanceRemaining.extra, wantExtra)
			}
		} else if len(res.msgBalanceRemaining.extra) != 0 {
			t.Fatalf("v%d mode=%d state-limit message balance extra = %v, want empty", version, mode, res.msgBalanceRemaining.extra)
		}
		assertTransactionFuzzMessageBalanceUnchanged(t, msgBalance, wantOriginalMsgBalance, version, "state-limit")
	})
}

func FuzzTransactionApplyActionsMessageBalanceInputIsolation(f *testing.F) {
	for version := uint32(MinSupportedGlobalVersion); version <= uint32(MaxSupportedGlobalVersion); version++ {
		f.Add(uint8(version), byte(0), uint64(500), false, uint64(0))
		f.Add(uint8(version), byte(1), uint64(501), true, uint64(11))
		f.Add(uint8(version), byte(2), uint64(502), true, uint64(23))
		f.Add(uint8(version), byte(3), uint64(503), false, uint64(0))
		f.Add(uint8(version), byte(4), uint64(504), true, uint64(37))
	}

	f.Fuzz(func(t *testing.T, rawVersion, rawCase byte, rawMsgBalance uint64, hasExtra bool, rawExtraAmount uint64) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		msgBalanceAmount := rawMsgBalance%1_000_000 + 1
		var extra *cell.Dictionary
		wantExtra := uint64(0)
		if hasExtra {
			wantExtra = rawExtraAmount%1_000 + 1
			extra = makeTransactionExtraCurrencies(t, 7, wantExtra)
		}
		msgBalance, err := transactionCurrencyFromParts(new(big.Int).SetUint64(msgBalanceAmount), extra)
		if err != nil {
			t.Fatal(err)
		}
		wantOriginalMsgBalance := msgBalance.copy()

		data := cell.BeginCell().EndCell()
		res := &MessageExecutionResult{
			Accepted: true,
			ExecutionResult: ExecutionResult{
				ExitCode:  0,
				Data:      data,
				Actions:   cell.BeginCell().EndCell(),
				Committed: true,
			},
		}
		context := "empty-success"
		switch rawCase % 5 {
		case 0:
			context = "compute-skip"
			res.Accepted = false
			res.Committed = false
		case 1:
			context = "malformed-actions"
			res.Actions = cell.BeginCell().MustStoreUInt(1, 1).EndCell()
		case 2:
			context = "special-actions"
			res.Actions = buildTransactionActionList(t,
				tlb.ActionReserveCurrency{Mode: 0, Currency: tlb.CurrencyCollection{Coins: tlb.FromNanoTONU(1)}},
				tlb.ActionSetCode{NewCode: cell.BeginCell().MustStoreUInt(uint64(rawMsgBalance&0xff), 8).EndCell()},
			)
		case 3:
			context = "mode64-send"
			res.Actions = buildTransactionActionList(t,
				tlb.ActionSendMsg{Mode: 64, Msg: buildTransactionOutboundInternalCell(t, 0)},
			)
		case 4:
			context = "mode128-send"
			res.Actions = buildTransactionActionList(t,
				tlb.ActionSendMsg{Mode: 128, Msg: buildTransactionOutboundInternalCell(t, 0)},
			)
		}

		acc := &transactionRuntimeAccount{
			addr:    tonopsTestAddr,
			status:  tlb.AccountStatusActive,
			code:    cell.BeginCell().EndCell(),
			data:    data,
			balance: big.NewInt(2_000_000),
		}
		out, err := transactionApplyActions(acc, res, uint64(transactionTestLogicalTime), uint32(tonopsTestTime.Unix()), transactionTestConfigWithGlobalVersion(t, version), big.NewInt(2_000_000), nil, msgBalance, big.NewInt(0))
		if err != nil {
			t.Fatalf("apply actions v%d %s failed: %v", version, context, err)
		}
		assertTransactionFuzzMessageBalanceUnchanged(t, msgBalance, wantOriginalMsgBalance, version, context)

		switch rawCase % 5 {
		case 0:
			if out.phase != nil {
				t.Fatalf("v%d compute-skip phase = %+v, want nil", version, out.phase)
			}
			assertTransactionFuzzMessageBalanceUnchanged(t, out.msgBalanceRemaining, wantOriginalMsgBalance, version, "compute-skip result")
		case 1:
			if out.phase == nil || out.phase.Success || out.phase.ResultCode != 32 {
				t.Fatalf("unexpected malformed action phase v%d: %+v", version, out.phase)
			}
			assertTransactionFuzzMessageBalanceUnchanged(t, out.msgBalanceRemaining, wantOriginalMsgBalance, version, "malformed-actions result")
		case 2:
			if out.phase == nil || !out.phase.Success || out.phase.ResultCode != 0 {
				t.Fatalf("unexpected special action phase v%d: %+v", version, out.phase)
			}
			assertTransactionFuzzMessageBalanceUnchanged(t, out.msgBalanceRemaining, wantOriginalMsgBalance, version, "special-actions result")
		case 3, 4:
			if out.phase == nil || !out.phase.Success || out.phase.ResultCode != 0 || out.phase.MessagesCreated != 1 {
				t.Fatalf("unexpected send action phase v%d %s: %+v", version, context, out.phase)
			}
			if out.msgBalanceRemaining.grams.Sign() != 0 {
				t.Fatalf("v%d %s result message grams = %s, want 0", version, context, out.msgBalanceRemaining.grams)
			}
			if hasExtra {
				got := out.msgBalanceRemaining.extra[7]
				if got == nil || got.Uint64() != wantExtra {
					t.Fatalf("v%d %s result message extra = %v, want 7:%d", version, context, out.msgBalanceRemaining.extra, wantExtra)
				}
			} else if len(out.msgBalanceRemaining.extra) != 0 {
				t.Fatalf("v%d %s result message extra = %v, want empty", version, context, out.msgBalanceRemaining.extra)
			}
		}
	})
}

func FuzzTransactionVersionedMalformedActionLoading(f *testing.F) {
	f.Add(byte(2), byte(0), byte(0), byte(0))
	f.Add(byte(16), byte(0), byte(1), byte(0))
	f.Add(byte(18), byte(3), byte(1), byte(7))
	f.Add(byte(0), byte(2), byte(3), byte(9))
	f.Add(byte(255), byte(3), byte(0), byte(11))
	f.Add(byte(0), byte(5), byte(0), byte(13))

	f.Fuzz(func(t *testing.T, rawMode, rawKind, rawFlags, rawPayload byte) {
		kind := rawKind % 6
		if kind == 5 {
			root := cell.BeginCell().EndCell()
			for i := 0; i < 256; i++ {
				root = transactionFuzzActionSetCodeNode(t, root, byte(i))
			}

			for _, version := range transactionFuzzAllVersions {
				loaded, err := transactionLoadActions(root, version)
				if err != nil {
					t.Fatalf("v%d load failed: %v", version, err)
				}
				if loaded.resultCode != 33 || loaded.totalActions != 0 || loaded.skippedActions != 0 || loaded.bounce {
					t.Fatalf("unexpected overlong action list v%d: %+v", version, loaded)
				}
				transactionFuzzCheckActionResultArg(t, loaded.resultArg, 256)
			}
			return
		}

		root := cell.BeginCell().EndCell()
		malformedIndex := 0
		totalActions := 1
		if rawFlags&1 != 0 && kind != 4 {
			root = transactionFuzzActionSetCodeNode(t, root, rawPayload)
			malformedIndex = 1
			totalActions++
		}
		root = transactionFuzzMalformedActionNode(root, rawMode, kind)
		if rawFlags&2 != 0 {
			root = transactionFuzzActionSetCodeNode(t, root, rawPayload+1)
			totalActions++
		}

		for _, version := range transactionFuzzAllVersions {
			loaded, err := transactionLoadActions(root, version)
			if err != nil {
				t.Fatalf("v%d load failed: %v", version, err)
			}

			if kind == 4 {
				wantArg := 0
				if rawFlags&2 != 0 {
					wantArg = 1
				}
				if loaded.resultCode != 32 || loaded.totalActions != 0 || loaded.skippedActions != 0 || loaded.bounce {
					t.Fatalf("unexpected broken-chain action list v%d: %+v", version, loaded)
				}
				transactionFuzzCheckActionResultArg(t, loaded.resultArg, wantArg)
				continue
			}

			isMalformedSend := kind <= 1
			if isMalformedSend && version >= 8 && rawMode&2 != 0 {
				if loaded.resultCode != 0 || loaded.totalActions != uint16(totalActions) || loaded.skippedActions != 1 || loaded.bounce {
					t.Fatalf("unexpected skipped malformed send v%d mode=%d kind=%d: %+v", version, rawMode, kind, loaded)
				}
				if len(loaded.actions) != totalActions {
					t.Fatalf("v%d loaded actions len = %d, want %d", version, len(loaded.actions), totalActions)
				}
				for i, action := range loaded.actions {
					if i == malformedIndex {
						if !action.skipped || action.action != nil {
							t.Fatalf("v%d action %d = %+v, want skipped", version, i, action)
						}
						continue
					}
					transactionFuzzCheckSetCodeAction(t, action, i)
				}
				continue
			}

			wantBounce := isMalformedSend && version >= 4 && rawMode&16 != 0
			if loaded.resultCode != 34 || loaded.totalActions != uint16(totalActions) || loaded.skippedActions != 0 || loaded.bounce != wantBounce || len(loaded.actions) != 0 {
				t.Fatalf("unexpected malformed action v%d mode=%d kind=%d: %+v", version, rawMode, kind, loaded)
			}
			transactionFuzzCheckActionResultArg(t, loaded.resultArg, malformedIndex)
		}
	})
}

func FuzzTransactionVersionedReserveActionBoundaries(f *testing.F) {
	f.Add(byte(0), uint64(100), uint64(40), uint64(7), uint64(11), uint64(1))
	f.Add(byte(1), uint64(100), uint64(0), uint64(10), uint64(1), uint64(0))
	f.Add(byte(1), uint64(0), uint64(0), uint64(1), uint64(255), uint64(0))
	f.Add(byte(2), uint64(100), uint64(40), uint64(11), uint64(0), uint64(7))
	f.Add(byte(3), uint64(100), uint64(40), uint64(11), uint64(0), uint64(7))

	f.Fuzz(func(t *testing.T, rawCase byte, rawBase, rawAmount, rawExtraHave, rawExtraDelta, rawSpare uint64) {
		switch rawCase % 4 {
		case 0:
			remainingGrams := rawBase%1_000_000 + 1
			amount := rawAmount % (remainingGrams + 1)
			action := tlb.ActionReserveCurrency{
				Mode: 16,
				Currency: tlb.CurrencyCollection{
					Coins: tlb.FromNanoTONU(amount),
				},
			}

			for _, version := range transactionFuzzAllVersions {
				wantOK := version >= 4
				original := transactionFuzzCurrencyBalance(remainingGrams, nil)
				remaining := original.copy()
				reserved := transactionZeroCurrencyBalance()

				res, err := transactionProcessReserveAction(action, original, remaining, reserved, version)
				if err != nil {
					t.Fatal(err)
				}
				if !wantOK {
					checkTransactionFuzzReserveFailure(t, version, res, remaining, reserved, 34, false, remainingGrams)
					continue
				}
				if res.resultCode != 0 || !res.bounceOnFail {
					t.Fatalf("v%d reserve result = %+v, want success with bounce", version, res)
				}
				if got := remaining.grams.Uint64(); got != remainingGrams-amount {
					t.Fatalf("v%d remaining grams = %d, want %d", version, got, remainingGrams-amount)
				}
				if got := reserved.grams.Uint64(); got != amount {
					t.Fatalf("v%d reserved grams = %d, want %d", version, got, amount)
				}
			}
		case 1:
			remainingGrams := rawBase % 1_000_000
			reserveGrams := rawAmount % (remainingGrams + 1)
			extraHave := rawExtraHave%1_000 + 1
			extraNeed := extraHave + rawExtraDelta%1_000 + 1
			action := tlb.ActionReserveCurrency{
				Mode: 2,
				Currency: tlb.CurrencyCollection{
					Coins:           tlb.FromNanoTONU(reserveGrams),
					ExtraCurrencies: makeTransactionExtraCurrencies(t, 7, extraNeed),
				},
			}

			for _, version := range transactionFuzzAllVersions {
				code := int32(38)
				if version == 9 {
					code = 0
				} else if version >= 10 {
					code = 34
				}
				original := transactionFuzzCurrencyBalance(remainingGrams, map[uint32]uint64{7: extraHave})
				remaining := original.copy()
				reserved := transactionZeroCurrencyBalance()

				res, err := transactionProcessReserveAction(action, original, remaining, reserved, version)
				if err != nil {
					t.Fatal(err)
				}
				if code != 0 {
					checkTransactionFuzzReserveFailure(t, version, res, remaining, reserved, code, false, remainingGrams)
					if got := transactionFuzzExtraAmount(remaining, 7); got != extraHave {
						t.Fatalf("v%d remaining extra = %d, want %d", version, got, extraHave)
					}
					continue
				}
				if res.resultCode != 0 {
					t.Fatalf("v%d reserve result code = %d, want success", version, res.resultCode)
				}
				if got := remaining.grams.Uint64(); got != remainingGrams-reserveGrams {
					t.Fatalf("v%d remaining grams = %d, want %d", version, got, remainingGrams-reserveGrams)
				}
				if got := reserved.grams.Uint64(); got != reserveGrams {
					t.Fatalf("v%d reserved grams = %d, want %d", version, got, reserveGrams)
				}
				if got := transactionFuzzExtraAmount(reserved, 7); got != extraHave {
					t.Fatalf("v%d reserved extra = %d, want %d", version, got, extraHave)
				}
				if got := transactionFuzzExtraAmount(remaining, 7); got != 0 {
					t.Fatalf("v%d remaining extra = %d, want 0", version, got)
				}
			}
		case 2:
			originalGrams := rawBase % 1_000_000
			actionGrams := rawAmount % 1_000_000
			spareGrams := rawSpare % 1_000
			remainingGrams := originalGrams + actionGrams + spareGrams
			extraHave := rawExtraHave%1_000 + 1
			action := tlb.ActionReserveCurrency{
				Mode: 4,
				Currency: tlb.CurrencyCollection{
					Coins: tlb.FromNanoTONU(actionGrams),
				},
			}

			checkTransactionFuzzReserveOriginalExtraBoundary(t, action, originalGrams, remainingGrams, originalGrams+actionGrams, extraHave)
		case 3:
			originalGrams := rawBase % 1_000_000
			amount := uint64(0)
			if originalGrams > 0 {
				amount = rawAmount % (originalGrams + 1)
			}
			spareGrams := rawSpare % 1_000
			remainingGrams := originalGrams + spareGrams
			extraHave := rawExtraHave%1_000 + 1
			action := tlb.ActionReserveCurrency{
				Mode: 12,
				Currency: tlb.CurrencyCollection{
					Coins: tlb.FromNanoTONU(amount),
				},
			}

			checkTransactionFuzzReserveOriginalExtraBoundary(t, action, originalGrams, remainingGrams, originalGrams-amount, extraHave)
		}
	})
}

func FuzzTransactionVersionedChangeLibraryActionBoundaries(f *testing.F) {
	f.Add(byte(0), uint64(1))
	f.Add(byte(1), uint64(2))
	f.Add(byte(2), uint64(3))
	f.Add(byte(3), uint64(4))
	f.Add(byte(4), uint64(5))
	f.Add(byte(5), uint64(6))
	f.Add(byte(6), uint64(7))

	f.Fuzz(func(t *testing.T, rawCase byte, rawSeed uint64) {
		lib := transactionTestCellChain(int(rawSeed%3) + 1)

		for _, version := range transactionFuzzAllVersions {
			action := tlb.ActionChangeLibrary{}
			var current *cell.Dictionary
			wantCode := int32(0)
			wantBounce := version >= 4
			wantStored := false
			wantPublic := false
			wantDeleted := false

			switch rawCase % 7 {
			case 0:
				action = tlb.ActionChangeLibrary{Mode: 16, LibRef: tlb.LibRefRef{Library: lib}}
				current = buildTransactionV13LibraryDict(t, lib, false)
				wantDeleted = version >= 4
			case 1:
				action = tlb.ActionChangeLibrary{Mode: 17, LibRef: tlb.LibRefRef{Library: lib}}
				wantStored = version >= 4
			case 2:
				action = tlb.ActionChangeLibrary{Mode: 18, LibRef: tlb.LibRefRef{Library: lib}}
				wantStored = version >= 4
				wantPublic = version >= 4
			case 3:
				action = tlb.ActionChangeLibrary{Mode: 19, LibRef: tlb.LibRefRef{Library: lib}}
				wantCode = 34
			case 4:
				action = tlb.ActionChangeLibrary{Mode: 17, LibRef: tlb.LibRefHash{LibHash: lib.Hash()}}
				wantCode = 41
			case 5:
				action = tlb.ActionChangeLibrary{Mode: 18, LibRef: tlb.LibRefHash{LibHash: lib.Hash()}}
				current = buildTransactionV13LibraryDict(t, lib, false)
				wantStored = version >= 4
				wantPublic = version >= 4
			case 6:
				action = tlb.ActionChangeLibrary{Mode: 16, LibRef: struct{}{}}
				wantCode = 34
			}
			if version < 4 {
				wantCode = 34
				wantBounce = false
				wantStored = false
				wantDeleted = false
			}

			res, err := transactionProcessChangeLibraryAction(action, current, transactionTestConfigWithGlobalVersion(t, version), version)
			if err != nil {
				t.Fatalf("v%d change library failed: %v", version, err)
			}
			if res.resultCode != wantCode || res.bounceOnFail != wantBounce {
				t.Fatalf("v%d case=%d change library result = %+v, want code=%d bounce=%t", version, rawCase%7, res, wantCode, wantBounce)
			}
			if wantCode != 0 {
				if res.nextLibraries != nil {
					t.Fatalf("v%d case=%d next libraries present on failure", version, rawCase%7)
				}
				continue
			}
			if wantDeleted {
				checkTransactionFuzzChangeLibraryMissing(t, version, res.nextLibraries, lib)
				continue
			}
			if wantStored {
				checkTransactionFuzzChangeLibraryStored(t, version, res.nextLibraries, lib, wantPublic)
			}
		}
	})
}

func FuzzTransactionVersionedLibraryHashNormalizationAndDictEquality(f *testing.F) {
	f.Add(byte(0), byte(0x11), byte(0x22), byte(0))
	f.Add(byte(31), byte(0x33), byte(0x44), byte(1))
	f.Add(byte(32), byte(0x55), byte(0x66), byte(2))
	f.Add(byte(33), byte(0x77), byte(0x88), byte(3))
	f.Add(byte(48), byte(0x99), byte(0xAA), byte(4))

	f.Fuzz(func(t *testing.T, rawLen, rawSeed, rawLibSeed, rawFlags byte) {
		raw := transactionFuzzHashBytes(rawLen, rawSeed)
		normalized := transactionNormalizeBits256(raw)
		if len(normalized) != 32 {
			t.Fatalf("normalized hash len = %d, want 32", len(normalized))
		}
		switch {
		case len(raw) == 32:
			if !bytes.Equal(normalized, raw) {
				t.Fatalf("32-byte normalized hash = %x, want %x", normalized, raw)
			}
		case len(raw) > 32:
			if !bytes.Equal(normalized, raw[len(raw)-32:]) {
				t.Fatalf("long normalized hash = %x, want suffix %x", normalized, raw[len(raw)-32:])
			}
		default:
			if !bytes.Equal(normalized[32-len(raw):], raw) {
				t.Fatalf("short normalized hash tail = %x, want %x", normalized[32-len(raw):], raw)
			}
			for i, b := range normalized[:32-len(raw)] {
				if b != 0 {
					t.Fatalf("short normalized hash prefix byte %d = %x, want zero", i, b)
				}
			}
		}

		empty := cell.NewDict(256)
		lib := transactionFuzzPayloadCell(rawLibSeed, uint(rawLibSeed%16)+1)
		otherLib := transactionFuzzPayloadCell(rawLibSeed^0xff, uint(rawLibSeed%16)+1)
		public := rawFlags&1 != 0
		libs := buildTransactionV13LibraryDict(t, lib, public)
		sameLibs := buildTransactionV13LibraryDict(t, lib, public)
		differentModeLibs := buildTransactionV13LibraryDict(t, lib, !public)
		otherLibs := buildTransactionV13LibraryDict(t, otherLib, public)

		checkTransactionFuzzDictEqual(t, nil, nil, true)
		checkTransactionFuzzDictEqual(t, nil, empty, true)
		checkTransactionFuzzDictEqual(t, empty, nil, true)
		checkTransactionFuzzDictEqual(t, empty, cell.NewDict(256), true)
		checkTransactionFuzzDictEqual(t, libs, nil, false)
		checkTransactionFuzzDictEqual(t, nil, libs, false)
		checkTransactionFuzzDictEqual(t, libs, libs, true)
		checkTransactionFuzzDictEqual(t, libs, libs.Copy(), true)
		checkTransactionFuzzDictEqual(t, libs, sameLibs, true)
		checkTransactionFuzzDictEqual(t, libs, differentModeLibs, false)
		checkTransactionFuzzDictEqual(t, libs, otherLibs, false)

		longHash := append(transactionFuzzHashBytes(4, rawSeed^0x5a), lib.Hash()...)
		for _, version := range transactionFuzzAllVersions {
			res, err := transactionProcessChangeLibraryAction(
				tlb.ActionChangeLibrary{Mode: 18, LibRef: tlb.LibRefHash{LibHash: longHash}},
				libs,
				transactionTestConfigWithGlobalVersion(t, version),
				version,
			)
			if err != nil {
				t.Fatalf("v%d long-hash change library failed: %v", version, err)
			}
			if version < 4 {
				if res.resultCode != 34 || res.bounceOnFail || res.nextLibraries != nil {
					t.Fatalf("v%d long-hash change library = %+v, want invalid mode", version, res)
				}
				continue
			}
			if res.resultCode != 0 || !res.bounceOnFail {
				t.Fatalf("v%d long-hash change library = %+v, want success with bounce flag", version, res)
			}
			checkTransactionFuzzChangeLibraryStored(t, version, res.nextLibraries, lib, true)
		}
	})
}

func FuzzTransactionVersionedReserveOriginalExtraSendBoundary(f *testing.F) {
	f.Add(byte(0), uint64(1000), uint64(3), uint64(1), uint64(0))
	f.Add(byte(1), uint64(2000), uint64(11), uint64(7), uint64(40))
	f.Add(byte(1), uint64(1), uint64(1), uint64(1), uint64(0))

	f.Fuzz(func(t *testing.T, rawMode byte, rawBalance, rawExtraHave, rawExtraSend, rawReserveAmount uint64) {
		originalGrams := rawBalance%1_000_000 + 1
		extraHave := rawExtraHave%1_000 + 1
		extraSend := rawExtraSend%extraHave + 1
		mode := uint8(4)
		reserveAmount := uint64(0)
		if rawMode&1 != 0 {
			mode = 12
			reserveAmount = rawReserveAmount % (originalGrams + 1)
		}

		outMsg := buildTransactionOutboundInternalCellWithExtra(t, 0, makeTransactionExtraCurrencies(t, 7, extraSend))
		actions := []any{
			tlb.ActionReserveCurrency{
				Mode: mode,
				Currency: tlb.CurrencyCollection{
					Coins: tlb.FromNanoTONU(reserveAmount),
				},
			},
			tlb.ActionSendMsg{
				Mode: 0,
				Msg:  outMsg,
			},
		}

		for _, version := range transactionFuzzAllVersions {
			res := applyTransactionActionsForTestWithParams(
				t,
				actions,
				transactionFuzzSendExtraFlagsConfig(t, version),
				new(big.Int).SetUint64(originalGrams),
				makeTransactionExtraCurrencies(t, 7, extraHave),
				transactionZeroCurrencyBalance(),
			)

			if version < 10 {
				if res.phase == nil || res.phase.Success || !res.phase.Valid || res.phase.ResultCode != 38 || !res.phase.NoFunds || res.phase.MessagesCreated != 0 || len(res.outMsgs) != 0 {
					t.Fatalf("v%d action phase = %+v out_msgs=%d, want extra-currency no-funds", version, res.phase, len(res.outMsgs))
				}
				continue
			}

			if res.phase == nil || !res.phase.Success || !res.phase.Valid || res.phase.ResultCode != 0 || res.phase.MessagesCreated != 1 || len(res.outMsgs) != 1 {
				t.Fatalf("v%d action phase = %+v out_msgs=%d, want one outbound message", version, res.phase, len(res.outMsgs))
			}
			if got := res.phase.TotalMsgSize.Cells.Uint64(); got != 1 {
				t.Fatalf("v%d total message cells = %d, want root cell only", version, got)
			}

			var msg tlb.Message
			if err := transactionParseCell(&msg, res.outMsgs[0]); err != nil {
				t.Fatalf("failed to parse v%d outbound message: %v", version, err)
			}
			gotExtra, err := transactionLoadExtraCurrencies(msg.AsInternal().ExtraCurrencies)
			if err != nil {
				t.Fatalf("failed to load v%d outbound extra currencies: %v", version, err)
			}
			if got := gotExtra[7].Uint64(); got != extraSend {
				t.Fatalf("v%d outbound extra currency = %d, want %d", version, got, extraSend)
			}
		}
	})
}

func FuzzTransactionVersionedSendExtraCurrencySizeBoundary(f *testing.F) {
	f.Add(uint64(1000), uint64(3), uint64(1))
	f.Add(uint64(1), uint64(11), uint64(7))

	f.Fuzz(func(t *testing.T, rawBalance, rawExtraHave, rawExtraSend uint64) {
		balance := rawBalance%1_000_000 + 1
		extraHave := rawExtraHave%1_000 + 1
		extraSend := rawExtraSend%extraHave + 1
		outMsg := buildTransactionOutboundInternalCellWithExtra(t, 0, makeTransactionExtraCurrencies(t, 7, extraSend))

		for _, version := range transactionFuzzAllVersions {
			wantCells := uint64(2)
			if version >= 10 {
				wantCells = 1
			}

			res := applyTransactionSendActionForTestWithParams(
				t,
				tlb.ActionSendMsg{Mode: 0, Msg: outMsg},
				transactionFuzzSendExtraFlagsConfig(t, version),
				new(big.Int).SetUint64(balance),
				makeTransactionExtraCurrencies(t, 7, extraHave),
				transactionZeroCurrencyBalance(),
			)
			if res.phase == nil || !res.phase.Success || !res.phase.Valid || res.phase.ResultCode != 0 || res.phase.MessagesCreated != 1 || len(res.outMsgs) != 1 {
				t.Fatalf("v%d action phase = %+v out_msgs=%d, want one outbound message", version, res.phase, len(res.outMsgs))
			}
			if got := res.phase.TotalMsgSize.Cells.Uint64(); got != wantCells {
				t.Fatalf("v%d total message cells = %d, want %d", version, got, wantCells)
			}
		}
	})
}

func FuzzTransactionVersionedOutboundFeeUsageLayout(f *testing.F) {
	for _, version := range transactionFuzzAllVersions {
		f.Add(byte(version), byte(0), uint64(1), uint64(7))
	}
	f.Add(byte(0), byte(0), uint64(1), uint64(7))
	f.Add(byte(9), byte(3), uint64(2), uint64(11))
	f.Add(byte(10), byte(2), uint64(3), uint64(13))
	f.Add(byte(MaxSupportedGlobalVersion), byte(7), uint64(4), uint64(17))

	f.Fuzz(func(t *testing.T, rawVersion, rawFlags byte, rawCells, rawExtra uint64) {
		version := transactionFuzzGlobalVersion(rawVersion)
		bodyInRef := rawFlags&1 != 0
		stateInitInRef := rawFlags&2 != 0
		withState := rawFlags&4 != 0
		withExtra := rawFlags&8 == 0
		cellCount := int(rawCells%4) + 1

		body := transactionTestCellChain(cellCount)
		var stateInit *tlb.StateInit
		if withState {
			stateInit = &tlb.StateInit{
				Code: transactionTestCellChain(cellCount + 1),
				Data: transactionTestCellChain((cellCount % 3) + 1),
			}
		}

		var extra *cell.Dictionary
		if withExtra {
			extra = makeTransactionExtraCurrencies(t, uint32(rawExtra%31)+1, rawExtra%1_000+1)
		}

		layout := transactionOutboundLayout{stateInitInRef: stateInitInRef, bodyInRef: bodyInRef}
		msg := &tlb.InternalMessage{
			IHRDisabled:     true,
			SrcAddr:         address.NewAddressNone(),
			DstAddr:         tonopsTestAddr,
			Amount:          tlb.FromNanoTONU(1),
			ExtraCurrencies: extra,
			Body:            body,
			StateInit:       stateInit,
		}
		cfg := transactionTestConfigWithGlobalVersion(t, version)

		got, err := transactionOutboundInternalMessageFeeUsage(cfg, msg, layout)
		if err != nil {
			t.Fatalf("v%d fee usage failed: %v", version, err)
		}

		want, err := transactionFuzzExpectedOutboundFeeUsage(version, msg, layout)
		if err != nil {
			t.Fatalf("v%d expected fee usage failed: %v", version, err)
		}
		if got != want {
			t.Fatalf("v%d layout=%+v with_state=%t with_extra=%t fee usage = %+v, want %+v", version, layout, withState, withExtra, got, want)
		}

		v9Want, err := transactionFuzzExpectedOutboundFeeUsage(9, msg, layout)
		if err != nil {
			t.Fatal(err)
		}
		v10Want, err := transactionFuzzExpectedOutboundFeeUsage(10, msg, layout)
		if err != nil {
			t.Fatal(err)
		}
		if extra != nil && !extra.IsEmpty() {
			extraUsage, err := transactionCollectUsage(extra.AsCell())
			if err != nil {
				t.Fatal(err)
			}
			wantDelta := transactionAddUsage(v10Want, extraUsage)
			if v9Want != wantDelta {
				t.Fatalf("v9/v10 extra usage delta = %+v/%+v, want v10 + %+v", v9Want, v10Want, extraUsage)
			}
		} else if v9Want != v10Want {
			t.Fatalf("empty extra usage changed across v10: v9=%+v v10=%+v", v9Want, v10Want)
		}
	})
}

func FuzzTransactionVersionedInboundExternalValidation(f *testing.F) {
	f.Add(byte(0), byte(0), byte(1), byte(1), byte(0))
	f.Add(byte(1), byte(0), byte(9), byte(1), byte(0))
	f.Add(byte(2), byte(0), byte(1), byte(3), byte(0))
	f.Add(byte(3), byte(0), byte(1), byte(4), byte(0))
	f.Add(byte(4), byte(0), byte(1), byte(3), byte(0))
	f.Add(byte(5), byte(0), byte(8), byte(2), byte(0))

	f.Fuzz(func(t *testing.T, rawCase, rawDst, rawBits, rawCells, rawPayload byte) {
		msg := &tlb.Message{
			MsgType: tlb.MsgTypeExternalIn,
			Msg: &tlb.ExternalMessage{
				DstAddr: tonopsTestAddr,
			},
		}

		msgCell := cell.BeginCell().EndCell()
		cfg := transactionFuzzInboundExternalConfig(t, 10, 1<<21, 1<<13, 512)
		v9Cfg := transactionFuzzInboundExternalConfig(t, 9, 1<<21, 1<<13, 512)
		wantV9Err := false
		wantV10Err := false

		switch rawCase % 6 {
		case 0:
			msg.AsExternalIn().DstAddr = transactionFuzzInvalidInboundExternalDst(rawDst)
			wantV10Err = true
		case 1:
			bits := uint(rawBits%64) + 1
			msgCell = transactionFuzzMessageTailCell(transactionFuzzPayloadCell(rawPayload, bits))
			cfg = transactionFuzzInboundExternalConfig(t, 10, uint32(bits-1), 1<<13, 512)
			v9Cfg = transactionFuzzInboundExternalConfig(t, 9, uint32(bits-1), 1<<13, 512)
			wantV9Err = true
			wantV10Err = true
		case 2:
			cells := int(rawCells%4) + 1
			msgCell = transactionFuzzMessageTailCell(transactionTestCellChain(cells))
			cfg = transactionFuzzInboundExternalConfig(t, 10, 1<<21, uint32(cells-1), 512)
			v9Cfg = transactionFuzzInboundExternalConfig(t, 9, 1<<21, uint32(cells-1), 512)
			wantV9Err = true
			wantV10Err = true
		case 3:
			cells := int(rawCells%8) + 2
			msgCell = transactionFuzzMessageTailCell(transactionTestCellChain(cells))
			cfg = transactionFuzzInboundExternalConfig(t, 10, 1<<21, 1<<13, uint16(cells-1))
			v9Cfg = transactionFuzzInboundExternalConfig(t, 9, 1<<21, 1<<13, uint16(cells-1))
			wantV9Err = true
			wantV10Err = true
		case 4:
			proof, err := transactionFuzzNestedMerkleProofs(int(rawCells%3)+3, rawPayload)
			if err != nil {
				t.Fatal(err)
			}
			msgCell = transactionFuzzMessageTailCell(proof)
			cfg = transactionFuzzInboundExternalConfig(t, 10, 1<<21, 1<<13, 512)
			v9Cfg = transactionFuzzInboundExternalConfig(t, 9, 1<<21, 1<<13, 512)
			wantV9Err = true
			wantV10Err = true
		case 5:
			bits := uint(rawBits % 32)
			cells := int(rawCells % 3)
			if cells == 0 {
				msgCell = transactionFuzzMessageTailCell(transactionFuzzPayloadCell(rawPayload, bits))
				cfg = transactionFuzzInboundExternalConfig(t, 10, 1<<21, 1<<13, 512)
			} else {
				msgCell = transactionFuzzMessageTailCell(transactionTestCellChain(cells))
				cfg = transactionFuzzInboundExternalConfig(t, 10, 1<<21, 1<<13, 512)
			}
		}

		err := transactionValidateInboundExternalMessage(msgCell, msg, v9Cfg)
		if wantV9Err && err == nil {
			t.Fatal("v9 inbound external validation succeeded, want error")
		}
		if !wantV9Err && err != nil {
			t.Fatalf("v9 inbound external validation error = %v, want nil", err)
		}

		err = transactionValidateInboundExternalMessage(msgCell, msg, cfg)
		if wantV10Err && err == nil {
			t.Fatal("v10 inbound external validation succeeded, want error")
		}
		if !wantV10Err && err != nil {
			t.Fatalf("v10 inbound external validation error = %v, want nil", err)
		}
	})
}

func FuzzTransactionVersionedBounceMessageUsage(f *testing.F) {
	for _, version := range transactionFuzzAllVersions {
		f.Add(byte(version), byte(1), byte(0), byte(0), byte(2), byte(8), byte(4))
	}
	f.Add(byte(MinSupportedGlobalVersion), byte(1), byte(0), byte(0), byte(2), byte(8), byte(4))
	f.Add(byte(12), byte(1), byte(0), byte(0), byte(2), byte(8), byte(4))
	f.Add(byte(13), byte(1), byte(0), byte(0), byte(2), byte(8), byte(4))
	f.Add(byte(12), byte(1), byte(0x07), byte(0xAB), byte(7), byte(9), byte(3))
	f.Add(byte(13), byte(1), byte(0x07), byte(0xAB), byte(7), byte(9), byte(3))
	f.Add(byte(12), byte(2), byte(0x0F), byte(0x55), byte(5), byte(6), byte(7))
	f.Add(byte(13), byte(2), byte(0x0F), byte(0x55), byte(5), byte(6), byte(7))
	f.Add(byte(MaxSupportedGlobalVersion), byte(2), byte(0x0F), byte(0x55), byte(5), byte(6), byte(7))

	f.Fuzz(func(t *testing.T, rawVersion, rawExtra, rawRefs, rawPayload, rawLeafBits, rawBranchBits, rawExtraAmount byte) {
		version := transactionFuzzGlobalVersion(rawVersion)
		var extra *cell.Dictionary
		if rawExtra%3 != 0 {
			extra = makeTransactionExtraCurrencies(t, uint32(rawExtra%31)+1, uint64(rawExtraAmount)+1)
		}

		leaf := transactionFuzzPayloadCell(rawPayload, uint(rawLeafBits%16))
		branch := cell.BeginCell()
		if bits := uint(rawBranchBits % 16); bits > 0 {
			branch.MustStoreUInt(transactionFuzzPayloadValue(rawPayload>>4, bits), bits)
		}
		branchCell := branch.MustStoreRef(leaf).EndCell()

		var refs []*cell.Cell
		if rawRefs&1 != 0 {
			refs = append(refs, leaf)
		}
		if rawRefs&2 != 0 {
			refs = append(refs, branchCell)
		}
		if rawRefs&4 != 0 && extra != nil {
			refs = append(refs, extra.AsCell())
		}
		if rawRefs&8 != 0 {
			refs = append(refs, leaf)
		}

		body := cell.BeginCell()
		if bits := uint(rawPayload % 17); bits > 0 {
			body.MustStoreUInt(transactionFuzzPayloadValue(rawPayload, bits), bits)
		}
		for _, ref := range refs {
			body.MustStoreRef(ref)
		}

		want, err := transactionFuzzExpectedBounceUsage(version, extra, refs)
		if err != nil {
			t.Fatal(err)
		}
		got, err := transactionBounceMessageUsage(&tlb.InternalMessage{ExtraCurrencies: extra}, body.EndCell(), transactionTestConfigWithGlobalVersion(t, version))
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("v%d bounce usage = %+v, want %+v", version, got, want)
		}
	})
}

func FuzzTransactionVersionedBounceExtraFlags(f *testing.F) {
	for _, version := range transactionFuzzAllVersions {
		f.Add(byte(version), byte(1), byte(0))
	}
	f.Add(byte(MinSupportedGlobalVersion), byte(1), byte(0))
	f.Add(byte(11), byte(1), byte(0))
	f.Add(byte(12), byte(1), byte(1))
	f.Add(byte(13), byte(3), byte(2))
	f.Add(byte(MaxSupportedGlobalVersion), byte(7), byte(3))

	f.Fuzz(func(t *testing.T, rawVersion, rawIHRFee, rawBody byte) {
		version := transactionFuzzGlobalVersion(rawVersion)
		amount := big.NewInt(1_000_000)
		body := transactionFuzzPayloadCell(rawBody, uint(rawBody%64))
		msg := &tlb.Message{
			MsgType: tlb.MsgTypeInternal,
			Msg: &tlb.InternalMessage{
				IHRDisabled: true,
				Bounce:      true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTON(amount),
				IHRFee:      tlb.FromNanoTONU(uint64(rawIHRFee)),
				Body:        body,
			},
		}
		priceCell := buildTransactionMsgForwardPricesCell(t, 0, 0)
		cfg := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
			tlb.ConfigParamGlobalVersion:               transactionTestGlobalVersionCell(t, version),
			tlb.ConfigParamMsgForwardPricesBasechain:   priceCell,
			tlb.ConfigParamMsgForwardPricesMasterchain: priceCell,
		})

		bounce, err := transactionPrepareBouncePhase(
			msg,
			amount,
			nil,
			&transactionCurrencyBalance{grams: new(big.Int).Set(amount), extra: map[uint32]*big.Int{}},
			big.NewInt(0),
			big.NewInt(0),
			uint64(transactionTestLogicalTime),
			uint32(tonopsTestTime.Unix()),
			0,
			cfg,
			nil,
			nil,
			nil,
		)
		if err != nil {
			t.Fatalf("v%d bounce failed: %v", version, err)
		}
		if bounce == nil || bounce.outMsg == nil {
			t.Fatalf("v%d missing bounce output", version)
		}

		var out tlb.Message
		if err = tlb.Parse(&out, bounce.outMsg); err != nil {
			t.Fatalf("v%d failed to parse bounce output: %v", version, err)
		}
		got := out.AsInternal().IHRFee.Nano().Uint64()
		want := uint64(0)
		if version >= 12 {
			want = uint64(rawIHRFee) & 3
		}
		if got != want {
			t.Fatalf("v%d bounce ihr_fee = %d, want %d", version, got, want)
		}
	})
}

func FuzzTransactionVersionedDetailedBounceBodyPhaseExit(f *testing.F) {
	for _, version := range transactionFuzzAllVersions {
		f.Add(byte(version), byte(1), byte(0), byte(0), int32(13), uint32(5), uint32(7), byte(16), byte(0xAA))
	}
	f.Add(byte(MinSupportedGlobalVersion), byte(1), byte(0), byte(0), int32(13), uint32(5), uint32(7), byte(16), byte(0xAA))
	f.Add(byte(11), byte(0), byte(1), byte(0), int32(13), uint32(5), uint32(7), byte(16), byte(0xAA))
	f.Add(byte(11), byte(1), byte(1), byte(0), int32(13), uint32(5), uint32(7), byte(16), byte(0xAB))
	f.Add(byte(12), byte(0), byte(1), byte(0), int32(13), uint32(5), uint32(7), byte(16), byte(0xAC))
	f.Add(byte(12), byte(2), byte(1), byte(0), int32(13), uint32(5), uint32(7), byte(16), byte(0xAD))
	f.Add(byte(12), byte(1), byte(0), byte(0), int32(13), uint32(5), uint32(7), byte(16), byte(0xAA))
	f.Add(byte(12), byte(1), byte(0), byte(1), int32(13), uint32(5), uint32(7), byte(16), byte(0xA1))
	f.Add(byte(12), byte(1), byte(0), byte(2), int32(13), uint32(5), uint32(7), byte(16), byte(0xA2))
	f.Add(byte(12), byte(3), byte(1), byte(3), int32(-14), uint32(9), uint32(11), byte(17), byte(0xBB))
	f.Add(byte(13), byte(1), byte(0), byte(4), int32(0), uint32(1), uint32(2), byte(5), byte(0xCC))
	f.Add(byte(MaxSupportedGlobalVersion), byte(3), byte(1), byte(5), int32(42), uint32(99), uint32(100), byte(31), byte(0xDD))
	f.Add(byte(12), byte(1), byte(0), byte(6), int32(37), uint32(123), uint32(45), byte(7), byte(0xEE))
	f.Add(byte(12), byte(1), byte(0), byte(7), int32(0), uint32(11), uint32(22), byte(9), byte(0x77))
	f.Add(byte(12), byte(1), byte(0), byte(8), int32(34), uint32(0), uint32(0), byte(0), byte(0x11))

	f.Fuzz(func(t *testing.T, rawVersion, rawFlags, rawCapabilities, rawCase byte, rawExit int32, rawGas, rawSteps uint32, rawBodyBits, rawPayload byte) {
		version := transactionFuzzGlobalVersion(rawVersion)
		extraFlags := uint64(rawFlags & 3)
		bodyBits := uint(rawBodyBits % 32)
		bodyBuilder := cell.BeginCell()
		if bodyBits > 0 {
			bodyBuilder.MustStoreUInt(transactionFuzzPayloadValue(rawPayload, bodyBits), bodyBits)
		}
		bodyBuilder.MustStoreRef(transactionFuzzPayloadCell(rawPayload^0x5a, uint(rawPayload%8)+1))
		originalBody := bodyBuilder.EndCell()
		in := &tlb.InternalMessage{
			IHRDisabled: true,
			Bounce:      true,
			Bounced:     rawFlags&0x80 != 0,
			SrcAddr:     internalEmulationSrcAddr,
			DstAddr:     tonopsTestAddr,
			Amount:      tlb.FromNanoTONU(123456),
			IHRFee:      tlb.FromNanoTONU(extraFlags),
			CreatedLT:   777,
			CreatedAt:   888,
			Body:        originalBody,
		}
		skip, compute, action, wantPhase, wantExit, wantStats := transactionFuzzBounceExitCase(rawCase, rawExit, rawGas, rawSteps)
		cfg := transactionTestConfigWithGlobalVersion(t, version)
		hasLegacyCapability := rawCapabilities&1 != 0
		if hasLegacyCapability {
			cfg = transactionTestConfigWithGlobalVersionAndCapabilities(t, version, 4)
		}

		body, err := transactionBuildBounceBody(in, cfg, skip, compute, action)
		if err != nil {
			t.Fatalf("v%d build bounce body failed: %v", version, err)
		}

		flags := uint64(0)
		if version >= 12 {
			flags = extraFlags
		}
		if flags&1 == 0 {
			if hasLegacyCapability {
				transactionFuzzCheckLegacyBounceBody(t, body, originalBody, bodyBits)
				return
			}
			if body.BitsSize() != 0 || body.RefsNum() != 0 {
				t.Fatalf("v%d flags=%d legacy bounce body = %s, want empty without capability 4", version, flags, body.Dump())
			}
			return
		}

		transactionFuzzCheckDetailedBounceBody(t, body, originalBody, bodyBits, flags&2 != 0, wantPhase, wantExit, wantStats, rawGas, rawSteps)
	})
}

func FuzzTransactionVersionedStoragePhaseLifecycle(f *testing.F) {
	for _, version := range transactionFuzzAllVersions {
		f.Add(byte(version), byte(0), uint64(1000), uint64(77), uint64(77), uint64(1000), uint64(1000), false, false, uint64(0))
	}
	f.Add(byte(MinSupportedGlobalVersion), byte(0), uint64(1000), uint64(77), uint64(77), uint64(1000), uint64(1000), false, false, uint64(0))
	f.Add(byte(1), byte(0), uint64(1000), uint64(77), uint64(77), uint64(1000), uint64(1000), false, false, uint64(0))
	f.Add(byte(4), byte(0), uint64(1000), uint64(77), uint64(77), uint64(1000), uint64(1000), false, false, uint64(0))
	f.Add(byte(2), byte(0), uint64(50), uint64(100), uint64(0), uint64(1_000_000), uint64(1_000_000), false, false, uint64(0))
	f.Add(byte(6), byte(1), uint64(50), uint64(500), uint64(0), uint64(1_000_000), uint64(100), false, false, uint64(0))
	f.Add(byte(6), byte(1), uint64(50), uint64(500), uint64(0), uint64(1_000_000), uint64(100), true, false, uint64(0))
	f.Add(byte(7), byte(0), uint64(0), uint64(50), uint64(0), uint64(10), uint64(1_000_000), false, true, uint64(100))
	f.Add(byte(MaxSupportedGlobalVersion), byte(0), uint64(0), uint64(50), uint64(0), uint64(10), uint64(1_000_000), false, true, uint64(100))

	statuses := []tlb.AccountStatus{
		tlb.AccountStatusActive,
		tlb.AccountStatusUninit,
		tlb.AccountStatusFrozen,
		tlb.AccountStatusNonExist,
	}

	f.Fuzz(func(t *testing.T, rawVersion, rawStatus byte, balanceRaw, storageFeeRaw, oldDueRaw, freezeDueRaw, deleteDueRaw uint64, hasExtra, adjustMsgValue bool, msgBalanceRaw uint64) {
		version := transactionFuzzGlobalVersion(rawVersion)
		status := statuses[int(rawStatus)%len(statuses)]
		balance := balanceRaw % 1_000_000
		storageFee := storageFeeRaw % 1_000_000
		oldDue := oldDueRaw % 1_000_000
		freezeDue := freezeDueRaw % 1_000_000
		deleteDue := deleteDueRaw % 1_000_000
		msgBalance := msgBalanceRaw % 1_000_000

		var extra *cell.Dictionary
		if hasExtra {
			extra = makeTransactionExtraCurrencies(t, 7, 11)
		}

		duePayment := transactionCoinsPtr(new(big.Int).SetUint64(oldDue))
		acc := &transactionRuntimeAccount{
			addr:            tonopsTestAddr,
			status:          status,
			balance:         new(big.Int).SetUint64(balance),
			extraCurrencies: extra,
			storageInfo: tlb.StorageInfo{
				StorageExtra: tlb.StorageExtraNone{},
				DuePayment:   duePayment,
			},
		}
		prepared := &transactionPreparedPhases{
			balance:         new(big.Int).SetUint64(balance),
			extraCurrencies: extra,
			msgBalance: &transactionCurrencyBalance{
				grams: new(big.Int).SetUint64(msgBalance),
				extra: map[uint32]*big.Int{},
			},
			status: status,
		}

		prepared.applyStoragePhase(acc, new(big.Int).SetUint64(storageFee), uint32(tonopsTestTime.Unix()), version, transactionStorageDueLimits{
			freezeDue: new(big.Int).SetUint64(freezeDue),
			deleteDue: new(big.Int).SetUint64(deleteDue),
		}, adjustMsgValue)

		collected := storageFee
		if collected > balance {
			collected = balance
		}
		due := uint64(0)
		if storageFee > balance {
			due = storageFee - balance
		}
		wantBalance := balance - collected
		wantDuePayment := oldDue
		wantStatus := status
		wantDeleted := false
		wantDestroyed := false
		wantStatusChange := tlb.AccStatusChangeUnchanged

		if storageFee > 0 {
			if storageFee <= balance {
				if version >= 7 {
					wantDuePayment = 0
				}
			} else {
				switch status {
				case tlb.AccountStatusUninit, tlb.AccountStatusFrozen, tlb.AccountStatusNonExist:
					if due > deleteDue && !hasExtra {
						wantDeleted = true
						wantDestroyed = version >= 13
						wantStatus = tlb.AccountStatusNonExist
						wantStatusChange = tlb.AccStatusChangeDeleted
					}
				case tlb.AccountStatusActive:
					if due > freezeDue {
						wantStatus = tlb.AccountStatusFrozen
						wantStatusChange = tlb.AccStatusChangeFrozen
					}
				}
				if version >= 4 {
					wantDuePayment = due
				}
			}
		}

		if prepared.storagePhase == nil {
			t.Fatal("storage phase is nil")
		}
		if got := prepared.storagePhase.StorageFeesCollected.Nano().Uint64(); got != collected {
			t.Fatalf("v%d collected = %d, want %d", version, got, collected)
		}
		checkTransactionFuzzCoinsPtr(t, "storage phase due", prepared.storagePhase.StorageFeesDue, due)
		if got := prepared.balance.Uint64(); got != wantBalance {
			t.Fatalf("v%d balance = %d, want %d", version, got, wantBalance)
		}
		checkTransactionFuzzCoinsPtr(t, "persisted due payment", prepared.duePayment, wantDuePayment)
		if prepared.status != wantStatus {
			t.Fatalf("v%d status = %s, want %s", version, prepared.status, wantStatus)
		}
		if prepared.deleted != wantDeleted {
			t.Fatalf("v%d deleted = %t, want %t", version, prepared.deleted, wantDeleted)
		}
		if prepared.destroyed != wantDestroyed {
			t.Fatalf("v%d destroyed = %t, want %t", version, prepared.destroyed, wantDestroyed)
		}
		if prepared.storagePhase.StatusChange.Type != wantStatusChange {
			t.Fatalf("v%d status change = %s, want %s", version, prepared.storagePhase.StatusChange.Type, wantStatusChange)
		}
		if prepared.lastPaid != uint32(tonopsTestTime.Unix()) {
			t.Fatalf("v%d last paid = %d, want %d", version, prepared.lastPaid, uint32(tonopsTestTime.Unix()))
		}

		wantMsgBalance := msgBalance
		if adjustMsgValue && wantMsgBalance > wantBalance {
			wantMsgBalance = wantBalance
		}
		if got := prepared.msgBalance.grams.Uint64(); got != wantMsgBalance {
			t.Fatalf("v%d message balance = %d, want %d", version, got, wantMsgBalance)
		}
	})
}

func FuzzTransactionVersionedFrozenFinalStateNormalization(f *testing.F) {
	for _, version := range transactionFuzzAllVersions {
		f.Add(byte(version), byte(0), byte(0), byte(0x11), byte(0x22))
	}
	f.Add(byte(MinSupportedGlobalVersion), byte(0), byte(0), byte(0x11), byte(0x22))
	f.Add(byte(12), byte(0), byte(0), byte(0x11), byte(0x22))
	f.Add(byte(13), byte(0), byte(0), byte(0x33), byte(0x44))
	f.Add(byte(13), byte(1), byte(0), byte(0x55), byte(0x66))
	f.Add(byte(12), byte(2), byte(5), byte(0x77), byte(0x88))
	f.Add(byte(MaxSupportedGlobalVersion), byte(3), byte(8), byte(0x99), byte(0xaa))

	f.Fuzz(func(t *testing.T, rawVersion, rawCase, rawDepth, codeTag, dataTag byte) {
		version := transactionFuzzGlobalVersion(rawVersion)
		depth := uint64(rawDepth % 9)
		var stateDepth *uint64
		if rawCase&4 != 0 {
			stateDepth = &depth
		}

		code := cell.BeginCell().MustStoreUInt(uint64(codeTag), 8).EndCell()
		data := cell.BeginCell().MustStoreUInt(uint64(dataTag), 8).EndCell()
		stateInit := &tlb.StateInit{
			Depth: transactionCloneUint64(stateDepth),
			Code:  code,
			Data:  data,
		}
		stateCell, err := buildTransactionStateInitCell(stateInit)
		if err != nil {
			t.Fatal(err)
		}

		computedHash := stateCell.Hash()
		addrData := append([]byte(nil), computedHash...)
		useProvidedHash := rawCase&1 == 0
		mismatch := rawCase&2 != 0
		var stateHash []byte
		if useProvidedHash {
			stateHash = append([]byte(nil), computedHash...)
			if mismatch {
				flipTransactionFuzzBit(stateHash, 0)
			}
		} else if mismatch {
			flipTransactionFuzzBit(addrData, 0)
		}

		txStatus, accountStatus, nextHash, err := transactionNormalizeFrozenFinalState(&transactionRuntimeAccount{
			addr:       address.NewAddress(0, 0, addrData),
			stateDepth: stateDepth,
		}, tlb.AccountStatusFrozen, code, data, nil, stateHash, transactionTestConfigWithGlobalVersion(t, version))
		if err != nil {
			t.Fatal(err)
		}

		if !mismatch {
			wantTxStatus := tlb.AccountStatus(tlb.AccountStatusFrozen)
			if version >= 13 {
				wantTxStatus = tlb.AccountStatusUninit
			}
			if txStatus != wantTxStatus || accountStatus != tlb.AccountStatusUninit || nextHash != nil {
				t.Fatalf("v%d case=%d frozen match = tx:%s account:%s hash:%x, want tx:%s account:uninit hash:nil", version, rawCase, txStatus, accountStatus, nextHash, wantTxStatus)
			}
			return
		}

		wantHash := stateHash
		if !useProvidedHash {
			wantHash = computedHash
		}
		if txStatus != tlb.AccountStatusFrozen || accountStatus != tlb.AccountStatusFrozen || !bytes.Equal(nextHash, wantHash) {
			t.Fatalf("v%d case=%d frozen mismatch = tx:%s account:%s hash:%x, want frozen/frozen hash:%x", version, rawCase, txStatus, accountStatus, nextHash, wantHash)
		}
	})
}

func FuzzTransactionVersionedStorageExtraDictHash(f *testing.F) {
	for _, version := range transactionFuzzAllVersions {
		f.Add(byte(version), byte(2), byte(0))
	}
	f.Add(byte(MinSupportedGlobalVersion), byte(2), byte(0))
	f.Add(byte(10), byte(2), byte(0))
	f.Add(byte(11), byte(2), byte(2))
	f.Add(byte(12), byte(6), byte(4))
	f.Add(byte(MaxSupportedGlobalVersion), byte(6), byte(4))

	f.Fuzz(func(t *testing.T, rawVersion, rawThreshold, rawRefs byte) {
		version := transactionFuzzGlobalVersion(rawVersion)
		threshold := uint32(rawThreshold%8) + 1
		refs := int(rawRefs % 5)
		data := cell.BeginCell().MustStoreUInt(uint64(rawRefs), 8)
		for i := 0; i < refs; i++ {
			data.MustStoreRef(cell.BeginCell().MustStoreUInt(uint64(i+1), uint(1+i%7)).EndCell())
		}

		cfg := transactionFuzzStorageDictConfig(t, version, threshold)
		accountCell, accountState, _, err := buildTransactionAccountCell(
			&transactionRuntimeAccount{
				addr:    tonopsTestAddr,
				status:  tlb.AccountStatusActive,
				balance: big.NewInt(1000),
			},
			tlb.AccountStatusActive,
			big.NewInt(1000),
			nil,
			1,
			uint32(tonopsTestTime.Unix()),
			nil,
			cell.BeginCell().EndCell(),
			data.EndCell(),
			nil,
			nil,
			cfg,
			nil,
		)
		if err != nil {
			t.Fatal(err)
		}

		wantInfo := version >= 11 && accountState.StorageInfo.StorageUsed.CellsUsed.Uint64() >= uint64(threshold)
		checkTransactionFuzzStorageExtra(t, version, "state", accountState.StorageInfo.StorageExtra, wantInfo)

		var parsed tlb.AccountState
		if err = tlb.Parse(&parsed, accountCell); err != nil {
			t.Fatal(err)
		}
		checkTransactionFuzzStorageExtra(t, version, "serialized", parsed.StorageInfo.StorageExtra, wantInfo)
	})
}

func FuzzTransactionVersionedComputeStateInitBoundaries(f *testing.F) {
	for _, version := range transactionFuzzAllVersions {
		f.Add(byte(version), byte(0), byte(9), byte(0), false, false, false, byte(0x11), byte(0x22))
	}
	f.Add(byte(MinSupportedGlobalVersion), byte(0), byte(9), byte(0), false, false, false, byte(0x11), byte(0x22))
	f.Add(byte(9), byte(0), byte(9), byte(0), false, false, false, byte(0x11), byte(0x22))
	f.Add(byte(10), byte(0), byte(9), byte(0), false, false, false, byte(0x11), byte(0x22))
	f.Add(byte(10), byte(1), byte(8), byte(0), false, false, false, byte(0x33), byte(0x44))
	f.Add(byte(9), byte(0), byte(7), byte(1), false, false, false, byte(0x55), byte(0x66))
	f.Add(byte(10), byte(0), byte(7), byte(1), false, false, false, byte(0x55), byte(0x66))
	f.Add(byte(7), byte(2), byte(0), byte(0), false, true, true, byte(0x77), byte(0x88))
	f.Add(byte(8), byte(2), byte(0), byte(0), false, true, true, byte(0x77), byte(0x88))
	f.Add(byte(8), byte(2), byte(0), byte(0), false, false, true, byte(0x99), byte(0xaa))
	f.Add(byte(MaxSupportedGlobalVersion), byte(2), byte(0), byte(0), false, false, true, byte(0x99), byte(0xaa))

	statuses := []tlb.AccountStatus{
		tlb.AccountStatusUninit,
		tlb.AccountStatusNonExist,
		tlb.AccountStatusFrozen,
	}

	f.Fuzz(func(t *testing.T, rawVersion, rawStatus, rawDepth, rawMismatch byte, suspended, frozenAddrMismatch, frozenHashMismatch bool, codeTag, dataTag byte) {
		version := transactionFuzzGlobalVersion(rawVersion)
		status := statuses[int(rawStatus)%len(statuses)]
		stateInit := &tlb.StateInit{
			Code: cell.BeginCell().MustStoreUInt(uint64(codeTag), 8).EndCell(),
			Data: cell.BeginCell().MustStoreUInt(uint64(dataTag), 8).EndCell(),
		}

		if status == tlb.AccountStatusFrozen {
			stateCell, err := tlb.ToCell(stateInit)
			if err != nil {
				t.Fatal(err)
			}
			hash := stateCell.Hash()
			addrData := append([]byte(nil), hash...)
			if frozenAddrMismatch {
				flipTransactionFuzzBit(addrData, 0)
			}
			stateHash := append([]byte(nil), hash...)
			if frozenHashMismatch {
				flipTransactionFuzzBit(stateHash, 7)
			}
			msg := &tlb.Message{
				MsgType: tlb.MsgTypeExternalIn,
				Msg: &tlb.ExternalMessage{
					DstAddr:   address.NewAddress(0, 0, addrData),
					StateInit: stateInit,
				},
			}
			_, usedState, skip, err := transactionPrepareComputeAccount(&transactionRuntimeAccount{
				addr:      msg.AsExternalIn().DstAddr,
				status:    status,
				stateHash: stateHash,
			}, status, false, msg, false, transactionTestConfigWithGlobalVersion(t, version))
			if err != nil {
				t.Fatal(err)
			}

			wantBadState := frozenHashMismatch || (version < 8 && frozenAddrMismatch)
			checkTransactionComputeBoundaryResult(t, version, usedState, skip, !wantBadState, tlb.ComputeSkipReasonBadState)
			return
		}

		depth := uint64(rawDepth % 13)
		stateInit.Depth = &depth
		stateCell, err := tlb.ToCell(stateInit)
		if err != nil {
			t.Fatal(err)
		}
		addrData := append([]byte(nil), stateCell.Hash()...)
		mismatchKind := rawMismatch % 3
		if mismatchKind == 1 && depth > 0 {
			flipTransactionFuzzBit(addrData, depth-1)
		}
		if mismatchKind == 2 {
			flipTransactionFuzzBit(addrData, depth)
		}

		msg := &tlb.Message{
			MsgType: tlb.MsgTypeInternal,
			Msg: &tlb.InternalMessage{
				DstAddr:   address.NewAddress(0, 0, addrData),
				StateInit: stateInit,
			},
		}
		_, usedState, skip, err := transactionPrepareComputeAccount(&transactionRuntimeAccount{
			addr:   msg.AsInternal().DstAddr,
			status: status,
		}, status, false, msg, suspended, transactionTestConfigWithGlobalVersion(t, version))
		if err != nil {
			t.Fatal(err)
		}

		if suspended {
			checkTransactionComputeBoundaryResult(t, version, usedState, skip, false, tlb.ComputeSkipReasonSuspended)
			return
		}

		wantBadState := mismatchKind == 2 || (version >= 10 && depth > transactionGetSizeLimits(transactionConfig{}).maxAccFixedPrefixLength)
		checkTransactionComputeBoundaryResult(t, version, usedState, skip, !wantBadState, tlb.ComputeSkipReasonBadState)
	})
}

func FuzzTransactionVersionedStateInitDepthPersistence(f *testing.F) {
	for _, version := range transactionFuzzAllVersions {
		f.Add(byte(version), byte(0), byte(0), byte(0), byte(0x11), byte(0x22))
	}
	f.Add(byte(MinSupportedGlobalVersion), byte(0), byte(0), byte(0), byte(0x11), byte(0x22))
	f.Add(byte(9), byte(0), byte(0), byte(0), byte(0x11), byte(0x22))
	f.Add(byte(9), byte(1), byte(5), byte(1), byte(0x33), byte(0x44))
	f.Add(byte(10), byte(0), byte(0), byte(0), byte(0x55), byte(0x66))
	f.Add(byte(10), byte(1), byte(5), byte(1), byte(0x77), byte(0x88))
	f.Add(byte(MaxSupportedGlobalVersion), byte(0), byte(8), byte(1), byte(0x99), byte(0xaa))

	statuses := []tlb.AccountStatus{
		tlb.AccountStatusUninit,
		tlb.AccountStatusNonExist,
	}
	maxDepth := transactionGetSizeLimits(transactionConfig{}).maxAccFixedPrefixLength

	f.Fuzz(func(t *testing.T, rawVersion, rawStatus, rawDepth, rawPrefix byte, codeTag, dataTag byte) {
		version := transactionFuzzGlobalVersion(rawVersion)
		status := statuses[int(rawStatus)%len(statuses)]
		depth := uint64(rawDepth) % (maxDepth + 1)
		stateInit := &tlb.StateInit{
			Depth: &depth,
			Code:  cell.BeginCell().MustStoreUInt(uint64(codeTag), 8).EndCell(),
			Data:  cell.BeginCell().MustStoreUInt(uint64(dataTag), 8).EndCell(),
		}

		stateCell, err := tlb.ToCell(stateInit)
		if err != nil {
			t.Fatal(err)
		}
		addrData := append([]byte(nil), stateCell.Hash()...)
		if depth > 0 && rawPrefix%2 != 0 {
			flipTransactionFuzzBit(addrData, depth-1)
		}
		msg := &tlb.Message{
			MsgType: tlb.MsgTypeInternal,
			Msg: &tlb.InternalMessage{
				DstAddr:   address.NewAddress(0, 0, addrData),
				StateInit: stateInit,
			},
		}

		next, usedState, skip, err := transactionPrepareComputeAccount(&transactionRuntimeAccount{
			addr:   msg.AsInternal().DstAddr,
			status: status,
		}, status, false, msg, false, transactionTestConfigWithGlobalVersion(t, version))
		if err != nil {
			t.Fatal(err)
		}
		if skip != nil || !usedState || next == nil || next.status != tlb.AccountStatusActive {
			t.Fatalf("v%d status=%s depth=%d activated=%t next=%+v skip=%+v, want active", version, status, depth, usedState, next, skip)
		}

		var wantDepth *uint64
		if version >= 10 && depth > 0 {
			v := depth
			wantDepth = &v
		}
		if !transactionUint64PtrEqual(next.stateDepth, wantDepth) {
			t.Fatalf("v%d depth=%d next state depth = %v, want %v", version, depth, next.stateDepth, wantDepth)
		}
	})
}

func FuzzTransactionVersionedNoStateSkipReasonBoundaries(f *testing.F) {
	for _, version := range transactionFuzzAllVersions {
		f.Add(byte(version), byte(0), byte(0), false, byte(0x11), byte(0x22))
	}
	f.Add(byte(0), byte(0), byte(0), false, byte(0x11), byte(0x22))
	f.Add(byte(4), byte(1), byte(1), true, byte(0x33), byte(0x44))
	f.Add(byte(8), byte(2), byte(2), true, byte(0x55), byte(0x66))
	f.Add(byte(13), byte(3), byte(0), false, byte(0x77), byte(0x88))
	f.Add(byte(MaxSupportedGlobalVersion), byte(3), byte(0), false, byte(0x77), byte(0x88))

	statuses := []tlb.AccountStatus{
		tlb.AccountStatusActive,
		tlb.AccountStatusUninit,
		tlb.AccountStatusFrozen,
		tlb.AccountStatusNonExist,
	}

	f.Fuzz(func(t *testing.T, rawVersion, rawStatus, rawMsgKind byte, withStateInit bool, codeTag, dataTag byte) {
		version := transactionFuzzGlobalVersion(rawVersion)
		status := statuses[int(rawStatus)%len(statuses)]

		var stateInit *tlb.StateInit
		if withStateInit {
			stateInit = &tlb.StateInit{
				Code: cell.BeginCell().MustStoreUInt(uint64(codeTag), 8).EndCell(),
				Data: cell.BeginCell().MustStoreUInt(uint64(dataTag), 8).EndCell(),
			}
		}

		var msg *tlb.Message
		switch rawMsgKind % 4 {
		case 0:
			msg = nil
		case 1:
			msg = &tlb.Message{
				MsgType: tlb.MsgTypeInternal,
				Msg: &tlb.InternalMessage{
					DstAddr:   tonopsTestAddr,
					StateInit: stateInit,
				},
			}
		case 2:
			msg = &tlb.Message{
				MsgType: tlb.MsgTypeExternalIn,
				Msg: &tlb.ExternalMessage{
					DstAddr:   tonopsTestAddr,
					StateInit: stateInit,
				},
			}
		default:
			msg = &tlb.Message{
				MsgType: tlb.MsgTypeExternalOut,
				Msg: &tlb.ExternalMessageOut{
					SrcAddr:   tonopsTestAddr,
					StateInit: stateInit,
				},
			}
		}

		extractedState := transactionMessageStateInit(msg)
		wantExtracted := stateInit
		if msg == nil || msg.MsgType == tlb.MsgTypeExternalOut {
			wantExtracted = nil
		}
		if extractedState != wantExtracted {
			t.Fatalf("v%d msg kind=%d extracted state = %p, want %p", version, rawMsgKind%4, extractedState, wantExtracted)
		}

		acc := &transactionRuntimeAccount{
			addr:   tonopsTestAddr,
			status: status,
			code:   cell.BeginCell().EndCell(),
		}
		if status == tlb.AccountStatusActive && rawStatus&0x80 != 0 {
			acc.code = nil
		}

		_, usedState, skip, err := transactionPrepareComputeAccount(acc, status, true, msg, false, transactionTestConfigWithGlobalVersion(t, version))
		if err != nil {
			t.Fatalf("v%d deleted compute account failed: %v", version, err)
		}
		wantSkip := transactionNoStateSkipReason(wantExtracted)
		checkTransactionComputeBoundaryResult(t, version, usedState, skip, false, wantSkip)

		if status == tlb.AccountStatusActive {
			_, usedState, skip, err = transactionPrepareComputeAccount(acc, status, false, msg, false, transactionTestConfigWithGlobalVersion(t, version))
			if err != nil {
				t.Fatalf("v%d active compute account failed: %v", version, err)
			}
			if acc.code == nil {
				checkTransactionComputeBoundaryResult(t, version, usedState, skip, false, tlb.ComputeSkipReasonNoState)
			} else if msg != nil && msg.MsgType == tlb.MsgTypeExternalIn && wantExtracted != nil {
				stateCell, err := tlb.ToCell(wantExtracted)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(stateCell.Hash(), acc.addr.Data()) {
					checkTransactionComputeBoundaryResult(t, version, usedState, skip, false, tlb.ComputeSkipReasonBadState)
					return
				}
				if skip != nil || usedState {
					t.Fatalf("v%d active external state init skip=%+v usedState=%t, want matching active account", version, skip, usedState)
				}
			} else if skip != nil || usedState {
				t.Fatalf("v%d active compute skip=%+v usedState=%t, want ordinary active account", version, skip, usedState)
			}
		}
	})
}

func FuzzTransactionVersionedStateInitLibraryValidation(f *testing.F) {
	for _, version := range transactionFuzzAllVersions {
		f.Add(byte(version), byte(0), byte(0), false)
	}
	f.Add(byte(0), byte(0), byte(0), false)
	f.Add(byte(1), byte(2), byte(2), true)
	f.Add(byte(7), byte(3), byte(1), false)
	f.Add(byte(13), byte(4), byte(2), true)
	f.Add(byte(MaxSupportedGlobalVersion), byte(4), byte(2), true)

	f.Fuzz(func(t *testing.T, rawVersion, rawLibCase, rawMsgKind byte, ignoreErrors bool) {
		version := transactionFuzzGlobalVersion(rawVersion)
		lib := transactionTestCellChain(int(rawLibCase%3) + 1)
		libDict := cell.NewDict(256)
		key := cell.BeginCell().MustStoreSlice(lib.Hash(), 256).EndCell()
		value := transactionFuzzStateInitLibraryValue(rawLibCase, lib)
		if err := libDict.Set(key, value); err != nil {
			t.Fatalf("failed to store library value: %v", err)
		}

		stateInit := &tlb.StateInit{
			Lib: libDict,
		}
		msg := &tlb.Message{
			MsgType: tlb.MsgTypeInternal,
			Msg: &tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     address.NewAddressNone(),
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(1),
				StateInit:   stateInit,
			},
		}
		if rawMsgKind%3 == 1 {
			msg = &tlb.Message{
				MsgType: tlb.MsgTypeExternalIn,
				Msg: &tlb.ExternalMessage{
					DstAddr:   tonopsTestAddr,
					StateInit: stateInit,
				},
			}
		} else if rawMsgKind%3 == 2 {
			msg = &tlb.Message{
				MsgType: tlb.MsgTypeExternalOut,
				Msg: &tlb.ExternalMessageOut{
					SrcAddr:   tonopsTestAddr,
					StateInit: stateInit,
				},
			}
		}

		wantValid := rawLibCase%5 == 0 || rawLibCase%5 == 4
		err := transactionValidateMessageStateInitLibs(msg)
		if rawMsgKind%3 == 2 {
			if err != nil {
				t.Fatalf("v%d external-out validator error = %v, want nil", version, err)
			}
			return
		}
		if wantValid && err != nil {
			t.Fatalf("v%d valid library entry failed: %v", version, err)
		}
		if !wantValid && err == nil {
			t.Fatalf("v%d invalid library entry was accepted", version)
		}
		if rawMsgKind%3 != 0 {
			return
		}

		msgCell, err := tlb.ToCell(msg.Msg)
		if err != nil {
			t.Fatalf("v%d failed to serialize outbound message: %v", version, err)
		}
		mode := uint8(0)
		if ignoreErrors {
			mode = 2
		}
		res := applyTransactionSendActionForTestWithParams(
			t,
			tlb.ActionSendMsg{Mode: mode, Msg: msgCell},
			transactionFuzzSendExtraFlagsConfig(t, version),
			big.NewInt(1_000_000),
			nil,
			transactionZeroCurrencyBalance(),
		)
		if res.phase == nil {
			t.Fatalf("v%d missing action phase", version)
		}

		if wantValid {
			if !res.phase.Success || !res.phase.Valid || res.phase.ResultCode != 0 || res.phase.SkippedActions != 0 || res.phase.MessagesCreated != 1 {
				t.Fatalf("v%d valid library action phase = %+v", version, res.phase)
			}
			return
		}
		if ignoreErrors && version >= 13 {
			if !res.phase.Success || !res.phase.Valid || res.phase.ResultCode != 0 || res.phase.SkippedActions != 1 || res.phase.MessagesCreated != 0 {
				t.Fatalf("v%d invalid library skipped phase = %+v", version, res.phase)
			}
			return
		}
		if res.phase.Success || res.phase.Valid || res.phase.ResultCode != 34 || res.phase.SkippedActions != 0 || res.phase.MessagesCreated != 0 {
			t.Fatalf("v%d invalid library action phase = %+v", version, res.phase)
		}
	})
}

func FuzzTransactionVersionedMasterchainPublicLibraryDeploy(f *testing.F) {
	for _, version := range transactionFuzzAllVersions {
		f.Add(byte(version), byte(0x03), byte(0xA2), byte(0x11), byte(0x22))
	}
	f.Add(byte(MinSupportedGlobalVersion), byte(0x03), byte(0xA2), byte(0x11), byte(0x22))
	f.Add(byte(12), byte(0x03), byte(0xA2), byte(0x11), byte(0x22))
	f.Add(byte(13), byte(0x03), byte(0xB3), byte(0x33), byte(0x44))
	f.Add(byte(MaxSupportedGlobalVersion), byte(0x01), byte(0xC4), byte(0x55), byte(0x66))
	f.Add(byte(13), byte(0x02), byte(0xD5), byte(0x77), byte(0x88))
	f.Add(byte(13), byte(0x07), byte(0xE6), byte(0x99), byte(0xAA))
	f.Add(byte(13), byte(0x0B), byte(0xF7), byte(0xBB), byte(0xCC))

	statuses := []tlb.AccountStatus{
		tlb.AccountStatusUninit,
		tlb.AccountStatusNonExist,
	}

	f.Fuzz(func(t *testing.T, rawVersion, rawFlags, libTag, codeTag, dataTag byte) {
		version := transactionFuzzGlobalVersion(rawVersion)
		status := statuses[int(rawFlags>>3)%len(statuses)]
		isMasterchain := rawFlags&1 != 0
		isPublicLibrary := rawFlags&2 != 0
		hasCode := rawFlags&4 == 0
		workchain := 0
		if isMasterchain {
			workchain = 0xFF
		}

		lib := cell.BeginCell().MustStoreUInt(uint64(libTag), 8).EndCell()
		stateInit := &tlb.StateInit{
			Data: cell.BeginCell().MustStoreUInt(uint64(dataTag), 8).EndCell(),
			Lib:  buildTransactionV13LibraryDict(t, lib, isPublicLibrary),
		}
		if hasCode {
			stateInit.Code = cell.BeginCell().MustStoreUInt(uint64(codeTag), 8).EndCell()
		}
		addr := stateInit.CalcAddress(workchain)
		msg := &tlb.Message{
			MsgType: tlb.MsgTypeInternal,
			Msg: &tlb.InternalMessage{
				DstAddr:   addr,
				StateInit: stateInit,
			},
		}

		_, usedState, skip, err := transactionPrepareComputeAccount(&transactionRuntimeAccount{
			addr:   addr,
			status: status,
		}, status, false, msg, false, transactionTestConfigWithGlobalVersion(t, version))
		if err != nil {
			t.Fatal(err)
		}

		wantActive := !(status == tlb.AccountStatusUninit && isMasterchain && isPublicLibrary)
		checkTransactionComputeBoundaryResult(t, version, usedState, skip, wantActive, tlb.ComputeSkipReasonBadState)
	})
}

func FuzzTransactionVersionedAccountStateLimitBoundaries(f *testing.F) {
	for _, version := range transactionFuzzAllVersions {
		f.Add(byte(version), byte(1), byte(2), byte(1), byte(4), byte(1), byte(0), byte(1))
	}
	f.Add(byte(MinSupportedGlobalVersion), byte(1), byte(2), byte(1), byte(4), byte(1), byte(0), byte(1))
	f.Add(byte(11), byte(1), byte(2), byte(1), byte(4), byte(1), byte(0), byte(1))
	f.Add(byte(12), byte(1), byte(2), byte(1), byte(4), byte(1), byte(0), byte(1))
	f.Add(byte(12), byte(0), byte(2), byte(1), byte(1), byte(4), byte(0), byte(1))
	f.Add(byte(12), byte(3), byte(3), byte(2), byte(0), byte(0), byte(0), byte(1))
	f.Add(byte(12), byte(13), byte(1), byte(1), byte(4), byte(4), byte(1), byte(0))
	f.Add(byte(12), byte(5), byte(1), byte(1), byte(4), byte(4), byte(1), byte(0))
	f.Add(byte(MaxSupportedGlobalVersion), byte(5), byte(1), byte(1), byte(4), byte(4), byte(1), byte(0))

	f.Fuzz(func(t *testing.T, rawVersion, rawFlags, rawCodeCells, rawDataCells, rawMaxAcc, rawMaxMC, rawLibCells, rawMaxPublic byte) {
		version := transactionFuzzGlobalVersion(rawVersion)
		isMasterchain := rawFlags&1 != 0
		isSpecial := rawFlags&2 != 0
		hasLibrary := rawFlags&4 != 0
		publicLibrary := rawFlags&8 != 0
		sameLibraries := rawFlags&16 != 0
		workchain := byte(0)
		if isMasterchain {
			workchain = 0xFF
		}

		code := transactionTestCellChain(int(rawCodeCells%4) + 1)
		var data *cell.Cell
		if rawDataCells%4 != 0 {
			data = transactionTestCellChain(int(rawDataCells%4) + 1)
		}

		maxAccCells := uint32(rawMaxAcc % 6)
		maxMCCells := uint32(rawMaxMC % 6)
		maxPublicLibraries := uint32(rawMaxPublic % 3)
		var libs *cell.Dictionary
		if hasLibrary {
			lib := transactionTestCellChain(int(rawLibCells%4) + 1)
			libs = buildTransactionV13LibraryDict(t, lib, publicLibrary)
		}
		cfg := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
			tlb.ConfigParamGlobalVersion: transactionTestGlobalVersionCell(t, version),
			tlb.ConfigParamSizeLimits:    buildTransactionSizeLimitsCellWithPublicLibraries(t, 1<<21, 1<<13, 1000, maxAccCells, maxMCCells, maxPublicLibraries),
		})
		acc := &transactionRuntimeAccount{
			addr:      address.NewAddress(0, workchain, bytes.Repeat([]byte{0x51}, 32)),
			code:      cell.BeginCell().EndCell(),
			data:      cell.BeginCell().EndCell(),
			isSpecial: isSpecial,
		}
		if sameLibraries {
			acc.libraries = libs
		}

		got, err := transactionAccountStateExceedsLimits(acc, code, data, libs, cfg)
		if err != nil {
			t.Fatal(err)
		}

		var libCell *cell.Cell
		if libs != nil && !libs.IsEmpty() {
			libCell = libs.AsCell()
		}
		usage, err := transactionCollectUniqueUsage(code, data, libCell)
		if err != nil {
			t.Fatal(err)
		}
		maxCells := uint64(maxAccCells)
		if isMasterchain && version >= 12 {
			maxCells = uint64(maxMCCells)
		}
		publicLibrariesExceeded := isMasterchain && !transactionDictEqual(acc.libraries, libs) && transactionPublicLibrariesCount(libs) > uint64(maxPublicLibraries)
		want := !isSpecial && (usage.cells > maxCells || publicLibrariesExceeded)
		if got != want {
			t.Fatalf("v%d master=%t special=%t usage=%d max_acc=%d max_mc=%d public=%d max_public=%d same_libs=%t exceeds=%t want %t", version, isMasterchain, isSpecial, usage.cells, maxAccCells, maxMCCells, transactionPublicLibrariesCount(libs), maxPublicLibraries, sameLibraries, got, want)
		}
	})
}

func FuzzTransactionVersionedAccountStateLimitShortCircuits(f *testing.F) {
	for _, version := range transactionFuzzAllVersions {
		f.Add(byte(version), byte(0x04), byte(3), byte(2), byte(0), byte(0), byte(0))
	}
	f.Add(byte(MinSupportedGlobalVersion), byte(0x04), byte(3), byte(2), byte(0), byte(0), byte(0))
	f.Add(byte(11), byte(0x04), byte(3), byte(2), byte(0), byte(0), byte(0))
	f.Add(byte(12), byte(0x05), byte(3), byte(2), byte(0), byte(0), byte(0))
	f.Add(byte(12), byte(0x02), byte(3), byte(2), byte(0), byte(0), byte(0))
	f.Add(byte(MaxSupportedGlobalVersion), byte(0x19), byte(1), byte(1), byte(1), byte(0), byte(0))

	f.Fuzz(func(t *testing.T, rawVersion, rawFlags, rawCodeCells, rawDataCells, rawMaxAcc, rawMaxMC, rawMaxPublic byte) {
		version := transactionFuzzGlobalVersion(rawVersion)
		isMasterchain := rawFlags&1 != 0
		isSpecial := rawFlags&2 != 0
		sameState := rawFlags&4 != 0
		publicLibrary := rawFlags&8 != 0
		hasLibrary := rawFlags&16 != 0

		workchain := byte(0)
		if isMasterchain {
			workchain = 0xFF
		}
		code := transactionTestCellChain(int(rawCodeCells%4) + 1)
		data := transactionTestCellChain(int(rawDataCells%4) + 1)
		var libs *cell.Dictionary
		if hasLibrary {
			libs = buildTransactionV13LibraryDict(t, transactionTestCellChain(2), publicLibrary)
		}

		maxAccCells := uint32(rawMaxAcc % 3)
		maxMCCells := uint32(rawMaxMC % 3)
		maxPublicLibraries := uint32(rawMaxPublic % 2)
		cfg := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
			tlb.ConfigParamGlobalVersion: transactionTestGlobalVersionCell(t, version),
			tlb.ConfigParamSizeLimits:    buildTransactionSizeLimitsCellWithPublicLibraries(t, 1<<21, 1<<13, 1000, maxAccCells, maxMCCells, maxPublicLibraries),
		})

		acc := &transactionRuntimeAccount{
			addr:      address.NewAddress(0, workchain, bytes.Repeat([]byte{0x61}, 32)),
			code:      cell.BeginCell().EndCell(),
			data:      cell.BeginCell().EndCell(),
			isSpecial: isSpecial,
		}
		if sameState {
			acc.code = code
			acc.data = data
			acc.libraries = libs
		}

		got, err := transactionAccountStateExceedsLimits(acc, code, data, libs, cfg)
		if err != nil {
			t.Fatal(err)
		}
		if sameState || isSpecial {
			if got {
				t.Fatalf("v%d master=%t special=%t same=%t exceeded limits, want false", version, isMasterchain, isSpecial, sameState)
			}
			return
		}

		var libCell *cell.Cell
		if libs != nil && !libs.IsEmpty() {
			libCell = libs.AsCell()
		}
		usage, err := transactionCollectUniqueUsage(code, data, libCell)
		if err != nil {
			t.Fatal(err)
		}
		maxCells := uint64(maxAccCells)
		if isMasterchain && version >= 12 {
			maxCells = uint64(maxMCCells)
		}
		publicLibrariesExceeded := isMasterchain && transactionPublicLibrariesCount(libs) > uint64(maxPublicLibraries)
		want := usage.cells > maxCells || publicLibrariesExceeded
		if got != want {
			t.Fatalf("v%d master=%t usage=%d max_acc=%d max_mc=%d public=%d max_public=%d exceeded=%t want %t", version, isMasterchain, usage.cells, maxAccCells, maxMCCells, transactionPublicLibrariesCount(libs), maxPublicLibraries, got, want)
		}
	})
}

func transactionFuzzExpectedBounceUsage(version uint32, extra *cell.Dictionary, refs []*cell.Cell) (transactionUsage, error) {
	collector := newTransactionUsageCollector()
	usage := transactionUsage{}
	if version < 13 && extra != nil && !extra.IsEmpty() {
		extraUsage, err := collector.addCell(extra.AsCell(), false)
		if err != nil {
			return transactionUsage{}, err
		}
		usage = transactionAddUsage(usage, extraUsage)
	}
	for _, ref := range refs {
		refUsage, err := collector.addCell(ref, false)
		if err != nil {
			return transactionUsage{}, err
		}
		usage = transactionAddUsage(usage, refUsage)
	}
	return usage, nil
}

func transactionFuzzBounceExitCase(rawCase byte, rawExit int32, gasUsed, steps uint32) (*tlb.ComputeSkipReason, *MessageExecutionResult, *tlb.ActionPhase, uint8, int32, bool) {
	switch rawCase % 9 {
	case 0:
		return &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonNoState}, nil, nil, 0, -1, false
	case 1:
		return &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonBadState}, nil, nil, 0, -2, false
	case 2:
		return &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonNoGas}, nil, nil, 0, -3, false
	case 3:
		return &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonSuspended}, nil, nil, 0, -4, false
	case 4:
		return &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonType("UNKNOWN")}, nil, nil, 0, 0, false
	case 5:
		return nil, &MessageExecutionResult{Accepted: true, ExecutionResult: ExecutionResult{ExitCode: int64(rawExit), GasUsed: int64(gasUsed), Steps: steps}}, nil, 1, rawExit, true
	case 6:
		return nil, &MessageExecutionResult{Accepted: true, ExecutionResult: ExecutionResult{ExitCode: 0, GasUsed: int64(gasUsed), Steps: steps, Committed: true}}, &tlb.ActionPhase{ResultCode: rawExit}, 2, rawExit, true
	case 7:
		return nil, &MessageExecutionResult{Accepted: true, ExecutionResult: ExecutionResult{ExitCode: 0, GasUsed: int64(gasUsed), Steps: steps, Committed: true}}, nil, 2, 0, true
	default:
		return nil, nil, &tlb.ActionPhase{ResultCode: rawExit}, 2, rawExit, false
	}
}

func transactionFuzzCheckDetailedBounceBody(t *testing.T, body, originalBody *cell.Cell, bodyBits uint, fullOriginal bool, wantPhase uint8, wantExit int32, wantStats bool, wantGasUsed, wantSteps uint32) {
	t.Helper()

	sl := body.MustBeginParse()
	if tag, err := sl.LoadUInt(32); err != nil || tag != 0xfffffffe {
		t.Fatalf("detailed bounce tag = %#x err=%v, want 0xfffffffe", tag, err)
	}
	originalRef, err := sl.LoadRefCell()
	if err != nil {
		t.Fatalf("load original body ref: %v", err)
	}
	infoRef, err := sl.LoadRefCell()
	if err != nil {
		t.Fatalf("load original info ref: %v", err)
	}
	if phase, err := sl.LoadUInt(8); err != nil || uint8(phase) != wantPhase {
		t.Fatalf("bounce phase = %d err=%v, want %d", phase, err, wantPhase)
	}
	exitCode, err := sl.LoadInt(32)
	if err != nil || int32(exitCode) != wantExit {
		t.Fatalf("bounce exit = %d err=%v, want %d", exitCode, err, wantExit)
	}
	hasStats, err := sl.LoadBoolBit()
	if err != nil {
		t.Fatalf("load compute stats flag: %v", err)
	}
	if hasStats != wantStats {
		t.Fatalf("compute stats flag = %t, want %t", hasStats, wantStats)
	}
	if hasStats {
		gasUsed, err := sl.LoadUInt(32)
		if err != nil {
			t.Fatalf("load gas used: %v", err)
		}
		steps, err := sl.LoadUInt(32)
		if err != nil {
			t.Fatalf("load steps: %v", err)
		}
		if uint32(gasUsed) != wantGasUsed || uint32(steps) != wantSteps {
			t.Fatalf("compute stats = gas %d steps %d, want gas %d steps %d", gasUsed, steps, wantGasUsed, wantSteps)
		}
	}
	if sl.BitsLeft() != 0 || sl.RefsNum() != 0 {
		t.Fatalf("detailed bounce body has trailing bits=%d refs=%d", sl.BitsLeft(), sl.RefsNum())
	}

	origSl := originalRef.MustBeginParse()
	wantOrig := originalBody.MustBeginParse()
	if origSl.BitsLeft() != bodyBits {
		t.Fatalf("original body bits = %d, want %d", origSl.BitsLeft(), bodyBits)
	}
	if bodyBits > 0 && !bytes.Equal(origSl.MustLoadSlice(bodyBits), wantOrig.MustLoadSlice(bodyBits)) {
		t.Fatal("original body bits mismatch")
	}
	wantRefs := uint(0)
	if fullOriginal {
		wantRefs = originalBody.RefsNum()
	}
	if originalRef.RefsNum() != wantRefs {
		t.Fatalf("original body refs = %d, want %d", originalRef.RefsNum(), wantRefs)
	}

	infoSl := infoRef.MustBeginParse()
	var value tlb.CurrencyCollection
	if err := tlb.LoadFromCell(&value, infoSl); err != nil {
		t.Fatalf("load bounced value: %v", err)
	}
	if got := value.Coins.Nano().Uint64(); got != 123456 {
		t.Fatalf("bounced value = %d, want 123456", got)
	}
	createdLT, err := infoSl.LoadUInt(64)
	if err != nil {
		t.Fatalf("load created lt: %v", err)
	}
	createdAt, err := infoSl.LoadUInt(32)
	if err != nil {
		t.Fatalf("load created at: %v", err)
	}
	if createdLT != 777 || createdAt != 888 {
		t.Fatalf("original info lt/at = %d/%d, want 777/888", createdLT, createdAt)
	}
}

func transactionFuzzCheckLegacyBounceBody(t *testing.T, body, originalBody *cell.Cell, bodyBits uint) {
	t.Helper()

	sl := body.MustBeginParse()
	if tag, err := sl.LoadUInt(32); err != nil || tag != 0xffffffff {
		t.Fatalf("legacy bounce tag = %#x err=%v, want 0xffffffff", tag, err)
	}
	if sl.BitsLeft() != bodyBits || sl.RefsNum() != 0 {
		t.Fatalf("legacy bounce tail = bits %d refs %d, want bits %d refs 0", sl.BitsLeft(), sl.RefsNum(), bodyBits)
	}
	if bodyBits == 0 {
		return
	}

	want := originalBody.MustBeginParse()
	if !bytes.Equal(sl.MustLoadSlice(bodyBits), want.MustLoadSlice(bodyBits)) {
		t.Fatal("legacy bounce body bits mismatch")
	}
}

func transactionFuzzStateInitLibraryValue(raw byte, lib *cell.Cell) *cell.Cell {
	switch raw % 5 {
	case 0:
		return cell.BeginCell().MustStoreBoolBit(true).MustStoreRef(lib).EndCell()
	case 1:
		return cell.BeginCell().MustStoreRef(lib).EndCell()
	case 2:
		return cell.BeginCell().MustStoreBoolBit(true).EndCell()
	case 3:
		return cell.BeginCell().MustStoreBoolBit(false).MustStoreRef(lib).MustStoreBoolBit(true).EndCell()
	default:
		return cell.BeginCell().MustStoreBoolBit(false).MustStoreRef(lib).EndCell()
	}
}

func transactionFuzzActionSetCodeNode(t *testing.T, prev *cell.Cell, rawPayload byte) *cell.Cell {
	t.Helper()

	next, err := tlb.ToCell(tlb.OutList{
		Prev: prev,
		Out: tlb.ActionSetCode{
			NewCode: cell.BeginCell().MustStoreUInt(uint64(rawPayload), 8).EndCell(),
		},
	})
	if err != nil {
		t.Fatalf("failed to build action node: %v", err)
	}
	return next
}

func transactionFuzzMalformedActionNode(prev *cell.Cell, mode, kind byte) *cell.Cell {
	if kind == 4 {
		return cell.BeginCell().
			MustStoreUInt(0x0ec3c86d, 32).
			MustStoreUInt(uint64(mode), 8).
			EndCell()
	}

	node := cell.BeginCell().MustStoreRef(prev)
	switch kind {
	case 0:
		node.MustStoreUInt(0x0ec3c86d, 32).MustStoreUInt(uint64(mode), 8)
	case 1:
		node.MustStoreUInt(0x0ec3c86d, 32).MustStoreUInt(uint64(mode), 8).MustStoreUInt(1, 1)
	case 2:
		node.MustStoreUInt(0xffffffff, 32).MustStoreUInt(uint64(mode), 8)
	case 3:
		node.MustStoreUInt(0x0ec3c86d, 32).MustStoreUInt(uint64(mode&0x7f), 7)
	default:
		node.MustStoreUInt(0xad4de08e, 32)
	}
	return node.EndCell()
}

func transactionFuzzCheckSetCodeAction(t *testing.T, entry transactionActionEntry, idx int) {
	t.Helper()

	if entry.skipped {
		t.Fatalf("action %d is skipped, want set-code", idx)
	}
	if _, ok := entry.action.(tlb.ActionSetCode); !ok {
		t.Fatalf("action %d = %#v, want set-code", idx, entry.action)
	}
}

func transactionFuzzCheckActionResultArg(t *testing.T, got *int32, wantIdx int) {
	t.Helper()

	if wantIdx == 0 {
		if got != nil {
			t.Fatalf("result arg = %d, want nil", *got)
		}
		return
	}
	if got == nil {
		t.Fatalf("result arg = nil, want %d", wantIdx)
	}
	if *got != int32(wantIdx) {
		t.Fatalf("result arg = %d, want %d", *got, wantIdx)
	}
}

func transactionFuzzExpectedOutboundFeeUsage(version uint32, msg *tlb.InternalMessage, layout transactionOutboundLayout) (transactionUsage, error) {
	collector := newTransactionUsageCollector()
	usage := transactionUsage{}

	if msg.StateInit != nil {
		stateCell, err := tlb.ToCell(msg.StateInit)
		if err != nil {
			return transactionUsage{}, err
		}
		stateUsage, err := collector.addCell(stateCell, !layout.stateInitInRef)
		if err != nil {
			return transactionUsage{}, err
		}
		usage = transactionAddUsage(usage, stateUsage)
	}

	if msg.Body != nil {
		bodyUsage, err := collector.addCell(msg.Body, !layout.bodyInRef)
		if err != nil {
			return transactionUsage{}, err
		}
		usage = transactionAddUsage(usage, bodyUsage)
	}

	if version < 10 && msg.ExtraCurrencies != nil {
		extraUsage, err := collector.addCell(msg.ExtraCurrencies.AsCell(), false)
		if err != nil {
			return transactionUsage{}, err
		}
		usage = transactionAddUsage(usage, extraUsage)
	}

	return usage, nil
}

func transactionFuzzPayloadCell(payload byte, bits uint) *cell.Cell {
	b := cell.BeginCell()
	if bits > 0 {
		b.MustStoreUInt(transactionFuzzPayloadValue(payload, bits), bits)
	}
	return b.EndCell()
}

func transactionFuzzPayloadValue(payload byte, bits uint) uint64 {
	value := uint64(payload)
	if bits < 8 {
		value &= (1 << bits) - 1
	}
	return value
}

func checkTransactionComputeBoundaryResult(t *testing.T, version uint32, usedState bool, skip *tlb.ComputeSkipReason, wantActive bool, wantSkip tlb.ComputeSkipReasonType) {
	t.Helper()

	if wantActive {
		if skip != nil || !usedState {
			t.Fatalf("v%d compute skip=%+v usedState=%t, want activation", version, skip, usedState)
		}
		return
	}
	if usedState {
		t.Fatalf("v%d usedState = true, want false", version)
	}
	if skip == nil || skip.Type != wantSkip {
		t.Fatalf("v%d compute skip=%+v, want %s", version, skip, wantSkip)
	}
}

func checkTransactionFuzzCoinsPtr(t *testing.T, name string, got *tlb.Coins, want uint64) {
	t.Helper()

	if want == 0 {
		if got != nil {
			t.Fatalf("%s = %s, want nil", name, got.Nano())
		}
		return
	}
	if got == nil || got.Nano().Uint64() != want {
		t.Fatalf("%s = %v, want %d", name, got, want)
	}
}

func checkTransactionFuzzStorageExtra(t *testing.T, version uint32, name string, got any, wantInfo bool) {
	t.Helper()

	switch v := got.(type) {
	case tlb.StorageExtraInfo:
		if len(v.DictHash) != 32 {
			t.Fatalf("v%d %s storage extra hash len = %d, want 32", version, name, len(v.DictHash))
		}
		if !wantInfo {
			t.Fatalf("v%d %s storage extra is info, want none", version, name)
		}
	case tlb.StorageExtraNone:
		if wantInfo {
			t.Fatalf("v%d %s storage extra is none, want info", version, name)
		}
	default:
		t.Fatalf("v%d %s storage extra = %T, want none or info", version, name, got)
	}
}

func checkTransactionFuzzReserveFailure(t *testing.T, version uint32, res *transactionReserveActionResult, remaining, reserved *transactionCurrencyBalance, wantCode int32, wantBounce bool, wantRemaining uint64) {
	t.Helper()

	if res.resultCode != wantCode || res.bounceOnFail != wantBounce {
		t.Fatalf("v%d reserve result = %+v, want code %d bounce %t", version, res, wantCode, wantBounce)
	}
	if got := remaining.grams.Uint64(); got != wantRemaining {
		t.Fatalf("v%d remaining grams = %d, want %d", version, got, wantRemaining)
	}
	if reserved.grams.Sign() != 0 || !reserved.extraEmpty() {
		t.Fatalf("v%d reserved balance = grams %s extra %v, want empty", version, reserved.grams, reserved.extra)
	}
}

func checkTransactionFuzzChangeLibraryMissing(t *testing.T, version uint32, libs *cell.Dictionary, lib *cell.Cell) {
	t.Helper()

	if libs == nil || libs.IsEmpty() {
		return
	}
	if _, err := libs.LoadValueByIntKey(new(big.Int).SetBytes(lib.Hash())); err == nil {
		t.Fatalf("v%d library %x is still present", version, lib.Hash())
	} else if !errors.Is(err, cell.ErrNoSuchKeyInDict) {
		t.Fatalf("v%d library lookup failed: %v", version, err)
	}
}

func checkTransactionFuzzChangeLibraryStored(t *testing.T, version uint32, libs *cell.Dictionary, lib *cell.Cell, wantPublic bool) {
	t.Helper()

	if libs == nil {
		t.Fatalf("v%d libraries are nil", version)
	}
	stored, err := libs.LoadValueByIntKey(new(big.Int).SetBytes(lib.Hash()))
	if err != nil {
		t.Fatalf("v%d library was not stored: %v", version, err)
	}
	isPublic, err := stored.LoadBoolBit()
	if err != nil {
		t.Fatalf("v%d failed to load public flag: %v", version, err)
	}
	if isPublic != wantPublic {
		t.Fatalf("v%d public flag = %t, want %t", version, isPublic, wantPublic)
	}
	storedRef, err := stored.LoadRefCell()
	if err != nil {
		t.Fatalf("v%d failed to load library ref: %v", version, err)
	}
	if storedRef.HashKey() != lib.HashKey() {
		t.Fatalf("v%d library hash = %x, want %x", version, storedRef.Hash(), lib.Hash())
	}
}

func transactionFuzzHashBytes(rawLen, rawSeed byte) []byte {
	ln := int(rawLen % 49)
	out := make([]byte, ln)
	for i := range out {
		out[i] = byte(i*31) ^ rawSeed
	}
	return out
}

func transactionFuzzComputeExitArgResult(t *testing.T, rawCase byte, rawExitArg int64) (*MessageExecutionResult, *int32) {
	t.Helper()

	if rawCase%8 == 0 {
		return nil, nil
	}

	res := &MessageExecutionResult{
		Accepted: rawCase%8 != 7,
		ExecutionResult: ExecutionResult{
			ExitCode: int64(rawCase) + 10,
			GasUsed:  int64(rawCase) + 20,
			Steps:    uint32(rawCase) + 30,
		},
	}
	stack := vmcore.NewStack()
	res.Stack = stack

	switch rawCase % 8 {
	case 1:
		if err := stack.PushSmallInt(rawExitArg); err != nil {
			t.Fatalf("push successful exit arg: %v", err)
		}
		res.Committed = true
		return res, nil
	case 2:
		res.Stack = nil
		return res, nil
	case 3:
		return res, nil
	case 4:
		if err := stack.PushCell(cell.BeginCell().EndCell()); err != nil {
			t.Fatalf("push non-int exit arg: %v", err)
		}
		return res, nil
	case 5:
		if err := stack.PushOwnedValue((*big.Int)(nil)); err != nil {
			t.Fatalf("push nil int exit arg: %v", err)
		}
		return res, nil
	case 6:
		if err := stack.PushOwnedValue(new(big.Int).Lsh(big.NewInt(1), 80)); err != nil {
			t.Fatalf("push huge exit arg: %v", err)
		}
		return res, nil
	default:
		if err := stack.PushSmallInt(rawExitArg); err != nil {
			t.Fatalf("push exit arg: %v", err)
		}
		return res, transactionFuzzExpectedComputeExitArg(rawExitArg)
	}
}

func transactionFuzzExpectedComputeExitArg(raw int64) *int32 {
	const (
		minInt32 = int64(-1 << 31)
		maxInt32 = int64(1<<31 - 1)
	)
	if raw == 0 || raw < minInt32 || raw > maxInt32 {
		return nil
	}
	v := int32(raw)
	return &v
}

func TestTransactionFuzzExpectedComputeExitArgBoundaries(t *testing.T) {
	const (
		minInt32 = int64(-1 << 31)
		maxInt32 = int64(1<<31 - 1)
	)

	tests := []struct {
		raw  int64
		want *int32
	}{
		{raw: -1 << 63},
		{raw: minInt32 - 1},
		{raw: minInt32, want: transactionFuzzInt32Ptr(int32(minInt32))},
		{raw: -1, want: transactionFuzzInt32Ptr(-1)},
		{raw: 0},
		{raw: 1, want: transactionFuzzInt32Ptr(1)},
		{raw: maxInt32, want: transactionFuzzInt32Ptr(int32(maxInt32))},
		{raw: maxInt32 + 1},
		{raw: 1<<63 - 1},
	}
	for _, tt := range tests {
		got := transactionFuzzExpectedComputeExitArg(tt.raw)
		if !transactionFuzzInt32PtrEqual(got, tt.want) {
			t.Fatalf("raw exit arg %d mapped to %s, want %s", tt.raw, transactionFuzzInt32PtrString(got), transactionFuzzInt32PtrString(tt.want))
		}
	}
}

func transactionFuzzInt32Ptr(v int32) *int32 {
	return &v
}

func transactionFuzzInt32PtrEqual(a, b *int32) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func transactionFuzzInt32PtrString(v *int32) string {
	if v == nil {
		return "nil"
	}
	return big.NewInt(int64(*v)).String()
}

func checkTransactionFuzzDictEqual(t *testing.T, a, b *cell.Dictionary, want bool) {
	t.Helper()

	if got := transactionDictEqual(a, b); got != want {
		t.Fatalf("transactionDictEqual(%s, %s) = %t, want %t", transactionFuzzDictLabel(a), transactionFuzzDictLabel(b), got, want)
	}
	if got := transactionDictEqual(b, a); got != want {
		t.Fatalf("transactionDictEqual symmetry (%s, %s) = %t, want %t", transactionFuzzDictLabel(b), transactionFuzzDictLabel(a), got, want)
	}
}

func transactionFuzzDictLabel(dict *cell.Dictionary) string {
	if dict == nil {
		return "nil"
	}
	if dict.IsEmpty() {
		return "empty"
	}
	return "non-empty"
}

func checkTransactionFuzzReserveOriginalExtraBoundary(t *testing.T, action tlb.ActionReserveCurrency, originalGrams, remainingGrams, wantReservedGrams, extraHave uint64) {
	t.Helper()

	for _, version := range transactionFuzzAllVersions {
		original := transactionFuzzCurrencyBalance(originalGrams, map[uint32]uint64{7: extraHave})
		remaining := transactionFuzzCurrencyBalance(remainingGrams, map[uint32]uint64{7: extraHave})
		reserved := transactionZeroCurrencyBalance()

		res, err := transactionProcessReserveAction(action, original, remaining, reserved, version)
		if err != nil {
			t.Fatal(err)
		}
		if res.resultCode != 0 {
			t.Fatalf("v%d reserve result code = %d, want success", version, res.resultCode)
		}
		if got := reserved.grams.Uint64(); got != wantReservedGrams {
			t.Fatalf("v%d reserved grams = %d, want %d", version, got, wantReservedGrams)
		}
		if got := remaining.grams.Uint64(); got != remainingGrams-wantReservedGrams {
			t.Fatalf("v%d remaining grams = %d, want %d", version, got, remainingGrams-wantReservedGrams)
		}

		wantReservedExtra := uint64(0)
		wantRemainingExtra := extraHave
		if version < 10 {
			wantReservedExtra = extraHave
			wantRemainingExtra = 0
		}
		if got := transactionFuzzExtraAmount(reserved, 7); got != wantReservedExtra {
			t.Fatalf("v%d reserved extra = %d, want %d", version, got, wantReservedExtra)
		}
		if got := transactionFuzzExtraAmount(remaining, 7); got != wantRemainingExtra {
			t.Fatalf("v%d remaining extra = %d, want %d", version, got, wantRemainingExtra)
		}
	}
}

func transactionFuzzCurrencyBalance(grams uint64, extra map[uint32]uint64) *transactionCurrencyBalance {
	out := &transactionCurrencyBalance{
		grams: new(big.Int).SetUint64(grams),
		extra: map[uint32]*big.Int{},
	}
	for id, amount := range extra {
		if amount > 0 {
			out.extra[id] = new(big.Int).SetUint64(amount)
		}
	}
	return out
}

func transactionFuzzExtraAmount(balance *transactionCurrencyBalance, id uint32) uint64 {
	amount := balance.extra[id]
	if amount == nil {
		return 0
	}
	return amount.Uint64()
}

func assertTransactionFuzzMessageBalanceUnchanged(t *testing.T, got, want *transactionCurrencyBalance, version uint32, context string) {
	t.Helper()

	if got.grams.Cmp(want.grams) != 0 {
		t.Fatalf("v%d %s mutated input message grams = %s, want %s", version, context, got.grams, want.grams)
	}
	if len(got.extra) != len(want.extra) {
		t.Fatalf("v%d %s mutated input message extra = %v, want %v", version, context, got.extra, want.extra)
	}
	for id, wantAmount := range want.extra {
		gotAmount := got.extra[id]
		if gotAmount == nil || gotAmount.Cmp(wantAmount) != 0 {
			t.Fatalf("v%d %s mutated input message extra = %v, want %v", version, context, got.extra, want.extra)
		}
	}
}

func transactionFuzzSendExtraFlagsConfig(t *testing.T, version uint32) transactionConfig {
	t.Helper()

	priceCell := buildTransactionMsgForwardPricesCell(t, 0, 0)
	return transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
		tlb.ConfigParamGlobalVersion:               transactionTestGlobalVersionCell(t, version),
		tlb.ConfigParamMsgForwardPricesBasechain:   priceCell,
		tlb.ConfigParamMsgForwardPricesMasterchain: priceCell,
	})
}

func transactionFuzzWorkchainConfig(t *testing.T, version uint32, workchain int32, acceptMessages bool) transactionConfig {
	t.Helper()

	workchains := cell.NewDict(32)
	descr := transactionFuzzBasicWorkchainDescrCell(acceptMessages)
	if err := workchains.SetIntKey(big.NewInt(int64(workchain)), descr); err != nil {
		t.Fatalf("store workchain descriptor: %v", err)
	}
	workchainsCell, err := tlb.ToCell(&tlb.WorkchainsConfig{Workchains: workchains})
	if err != nil {
		t.Fatalf("build workchains config: %v", err)
	}

	return transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
		tlb.ConfigParamGlobalVersion: transactionTestGlobalVersionCell(t, version),
		tlb.ConfigParamWorkchains:    workchainsCell,
	})
}

func transactionFuzzBasicWorkchainDescrCell(acceptMessages bool) *cell.Cell {
	return cell.BeginCell().
		MustStoreUInt(0xa6, 8).
		MustStoreUInt(123, 32).
		MustStoreUInt(0, 8).
		MustStoreUInt(0, 8).
		MustStoreUInt(4, 8).
		MustStoreBoolBit(true).
		MustStoreBoolBit(true).
		MustStoreBoolBit(acceptMessages).
		MustStoreUInt(0, 13).
		MustStoreSlice(make([]byte, 32), 256).
		MustStoreSlice(make([]byte, 32), 256).
		MustStoreUInt(7, 32).
		MustStoreUInt(1, 4).
		MustStoreInt(-1, 32).
		MustStoreUInt(3, 64).
		EndCell()
}

func transactionFuzzAddressDataForBits(data []byte, bits uint) []byte {
	ln := int((bits + 7) / 8)
	out := append([]byte(nil), data[:ln]...)
	if rem := bits % 8; rem != 0 {
		out[len(out)-1] &= 0xFF << (8 - rem)
	}
	return out
}

func checkTransactionFuzzSendExtraFlagsPhase(t *testing.T, version uint32, mode uint8, res *transactionActionApplyResult, wantMessage bool, wantError bool) {
	t.Helper()

	if res.phase == nil {
		t.Fatalf("v%d missing action phase", version)
	}
	if wantError {
		if mode&2 != 0 {
			if !res.phase.Success || res.phase.ResultCode != 0 || res.phase.SkippedActions != 1 || res.phase.MessagesCreated != 0 {
				t.Fatalf("v%d skipped phase = %+v, want one skipped action", version, res.phase)
			}
			return
		}
		if res.phase.Success || res.phase.ResultCode != 45 || res.phase.SkippedActions != 0 || res.phase.MessagesCreated != 0 {
			t.Fatalf("v%d error phase = %+v, want code 45", version, res.phase)
		}
		return
	}

	wantMessages := uint16(0)
	if wantMessage {
		wantMessages = 1
	}
	if !res.phase.Success || res.phase.ResultCode != 0 || res.phase.SkippedActions != 0 || res.phase.MessagesCreated != wantMessages {
		t.Fatalf("v%d success phase = %+v, want %d message(s)", version, res.phase, wantMessages)
	}
	if got := len(res.outMsgs); got != int(wantMessages) {
		t.Fatalf("v%d outbound messages = %d, want %d", version, got, wantMessages)
	}
}

func checkTransactionFuzzSendExtraFlagsOutMsg(t *testing.T, version uint32, msgCell *cell.Cell, wantIHRFee uint64) {
	t.Helper()

	var msg tlb.Message
	if err := tlb.Parse(&msg, msgCell); err != nil {
		t.Fatalf("v%d failed to parse outbound message: %v", version, err)
	}
	got := msg.AsInternal().IHRFee.Nano().Uint64()
	if got != wantIHRFee {
		t.Fatalf("v%d outbound IHR/extra flags = %d, want %d", version, got, wantIHRFee)
	}
}

func transactionFuzzInvalidInboundExternalDst(raw byte) *address.Address {
	switch raw % 5 {
	case 0:
		return nil
	case 1:
		return address.NewAddressNone()
	case 2:
		return address.NewAddressExt(0, 16, []byte{0xAB, raw})
	case 3:
		return address.NewAddressVar(0, tonopsTestAddr.Workchain(), 256, tonopsTestAddr.Data())
	default:
		return tonopsTestAddr.WithAnycast(address.NewAnycast(1, []byte{0x80}))
	}
}

func transactionFuzzMessageTailCell(tail *cell.Cell) *cell.Cell {
	if tail == nil {
		return cell.BeginCell().EndCell()
	}
	return cell.BeginCell().MustStoreRef(tail).EndCell()
}

func transactionFuzzNestedMerkleProofs(count int, payload byte) (*cell.Cell, error) {
	root := transactionFuzzPayloadCell(payload, 8)
	var err error
	for i := 0; i < count; i++ {
		root, err = cell.CreateMerkleProof(root)
		if err != nil {
			return nil, err
		}
	}
	return root, nil
}

func transactionFuzzInboundExternalConfig(t *testing.T, version uint32, maxMsgBits, maxMsgCells uint32, maxExtMsgDepth uint16) transactionConfig {
	t.Helper()

	sizeLimits, err := tlb.ToCell(&tlb.SizeLimitsConfigV2{
		MaxMsgBits:                  maxMsgBits,
		MaxMsgCells:                 maxMsgCells,
		MaxLibraryCells:             1000,
		MaxVMDataDepth:              512,
		MaxExtMsgSize:               65535,
		MaxExtMsgDepth:              maxExtMsgDepth,
		MaxAccStateCells:            1 << 16,
		MaxMCAccStateCells:          1 << 11,
		MaxAccPublicLibraries:       256,
		DeferOutQueueSizeLimit:      256,
		MaxMsgExtraCurrencies:       2,
		MaxAccFixedPrefixLength:     8,
		AccStateCellsForStorageDict: 26,
	})
	if err != nil {
		t.Fatalf("failed to build size limits config: %v", err)
	}

	return transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
		tlb.ConfigParamGlobalVersion: transactionTestGlobalVersionCell(t, version),
		tlb.ConfigParamSizeLimits:    sizeLimits,
	})
}

func transactionFuzzStorageDictConfig(t *testing.T, version uint32, accStateCellsForStorageDict uint32) transactionConfig {
	t.Helper()

	sizeLimits, err := tlb.ToCell(&tlb.SizeLimitsConfigV2{
		MaxMsgBits:                  1 << 21,
		MaxMsgCells:                 1 << 13,
		MaxLibraryCells:             1000,
		MaxVMDataDepth:              512,
		MaxExtMsgSize:               65535,
		MaxExtMsgDepth:              512,
		MaxAccStateCells:            1 << 16,
		MaxMCAccStateCells:          1 << 11,
		MaxAccPublicLibraries:       256,
		DeferOutQueueSizeLimit:      256,
		MaxMsgExtraCurrencies:       2,
		MaxAccFixedPrefixLength:     8,
		AccStateCellsForStorageDict: accStateCellsForStorageDict,
	})
	if err != nil {
		t.Fatalf("failed to build size limits config: %v", err)
	}

	return transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
		tlb.ConfigParamGlobalVersion: transactionTestGlobalVersionCell(t, version),
		tlb.ConfigParamSizeLimits:    sizeLimits,
	})
}

func transactionFuzzGasConfig(t *testing.T, version uint32, gasCell *cell.Cell) transactionConfig {
	t.Helper()

	return transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
		tlb.ConfigParamGlobalVersion:        transactionTestGlobalVersionCell(t, version),
		tlb.ConfigParamGasPricesBasechain:   gasCell,
		tlb.ConfigParamGasPricesMasterchain: gasCell,
	})
}

func flipTransactionFuzzBit(data []byte, bit uint64) {
	data[bit/8] ^= 1 << (7 - bit%8)
}

func checkAccountSerializationVersion(t *testing.T, addr *address.Address, rawData, rewrittenData []byte, depth uint, version uint32) {
	t.Helper()

	accountCell, accountState, _, err := buildTransactionAccountCell(
		&transactionRuntimeAccount{
			addr:    addr,
			status:  tlb.AccountStatusActive,
			balance: big.NewInt(1000),
		},
		tlb.AccountStatusActive,
		big.NewInt(1000),
		nil,
		1,
		uint32(tonopsTestTime.Unix()),
		nil,
		cell.BeginCell().EndCell(),
		cell.BeginCell().EndCell(),
		nil,
		nil,
		transactionTestConfigWithGlobalVersion(t, version),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	var parsed tlb.AccountState
	if err = tlb.Parse(&parsed, accountCell); err != nil {
		t.Fatal(err)
	}

	wantData := rawData
	wantAnycast := true
	var wantDepth *uint64
	if version < 10 {
		v := uint64(depth)
		wantDepth = &v
	} else {
		wantData = rewrittenData
		wantAnycast = false
	}

	for _, account := range []*tlb.AccountState{accountState, &parsed} {
		if !bytes.Equal(account.Address.Data(), wantData) {
			t.Fatalf("v%d account data = %x, want %x", version, account.Address.Data(), wantData)
		}
		if (account.Address.Anycast() != nil) != wantAnycast {
			t.Fatalf("v%d anycast present = %t, want %t", version, account.Address.Anycast() != nil, wantAnycast)
		}
		if !transactionUint64PtrEqual(account.StateInit.Depth, wantDepth) {
			t.Fatalf("v%d state depth = %v, want %v", version, account.StateInit.Depth, wantDepth)
		}
	}
}
