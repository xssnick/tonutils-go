package dns

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"testing"
)

func TestDomain_GetRecords(t *testing.T) {
	records := cell.NewDict(256)

	h := sha256.New()
	h.Write([]byte("site"))

	adnlAddr := make([]byte, 32)
	rand.Read(adnlAddr)

	records.Set(cell.BeginCell().MustStoreSlice(h.Sum(nil), 256).EndCell(),
		cell.BeginCell().MustStoreRef(cell.BeginCell().
			MustStoreUInt(_CategoryADNLSite, 16).
			MustStoreSlice(adnlAddr, 256).
			EndCell()).EndCell())

	h = sha256.New()
	h.Write([]byte("wallet"))
	addr := address.MustParseAddr("EQCD39VS5jcptHL8vMjEXrzGaRcCVYto7HUn4bpAOg8xqB2N")
	records.Set(cell.BeginCell().MustStoreSlice(h.Sum(nil), 256).EndCell(),
		cell.BeginCell().MustStoreRef(cell.BeginCell().
			MustStoreUInt(_CategoryContractAddr, 16).
			MustStoreAddr(addr).
			EndCell()).EndCell())

	domain := Domain{
		Records: records,
	}

	addrRecord, inStorage := domain.GetSiteRecord()
	if inStorage {
		t.Fatal("should be not in storage")
	}

	if !bytes.Equal(addrRecord, adnlAddr) {
		t.Fatal("incorrect site address")
	}

	if domain.GetWalletRecord().String() != addr.String() {
		t.Fatal("incorrect wallet address")
	}

	t.Run("builders", func(t *testing.T) {
		randomizer = func() uint64 {
			return 777
		}

		h = sha256.New()
		h.Write([]byte("site"))

		site := cell.BeginCell().MustStoreUInt(0x4eb1f0f9, 32).
			MustStoreUInt(777, 64).
			MustStoreSlice(h.Sum(nil), 256).MustStoreRef(cell.BeginCell().
			MustStoreUInt(_CategoryADNLSite, 16).
			MustStoreSlice(adnlAddr, 256).
			MustStoreUInt(0, 8).
			EndCell()).EndCell()

		if !bytes.Equal(domain.BuildSetSiteRecordPayload(adnlAddr, false).Hash(), site.Hash()) {
			t.Fatal("incorrect set site payload")
		}

		siteStorage := cell.BeginCell().MustStoreUInt(0x4eb1f0f9, 32).
			MustStoreUInt(777, 64).
			MustStoreSlice(h.Sum(nil), 256).MustStoreRef(cell.BeginCell().
			MustStoreUInt(_CategoryStorageSite, 16).
			MustStoreSlice(adnlAddr, 256).
			EndCell()).EndCell()

		if !bytes.Equal(domain.BuildSetSiteRecordPayload(adnlAddr, true).Hash(), siteStorage.Hash()) {
			t.Fatal("incorrect set site storage payload")
		}

		h = sha256.New()
		h.Write([]byte("wallet"))

		wallet := cell.BeginCell().MustStoreUInt(0x4eb1f0f9, 32).
			MustStoreUInt(777, 64).
			MustStoreSlice(h.Sum(nil), 256).
			MustStoreUInt(_CategoryContractAddr, 16).
			MustStoreAddr(addr).
			EndCell()

		if !bytes.Equal(domain.BuildSetWalletRecordPayload(addr).Hash(), wallet.Hash()) {
			t.Fatal("incorrect set wallet payload")
		}
	})
}
