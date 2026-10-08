package tlb

// CollatedDataRootState identifies a shard-state proof in collated block data.
type CollatedDataRootState struct {
	_    Magic  `tlb:"#4b2f36ec"`
	Hash []byte `tlb:"bits 256"`
}

// CollatedDataRootStorageDict identifies an account-storage dictionary proof.
type CollatedDataRootStorageDict struct {
	_    Magic  `tlb:"#796eaeb6"`
	Hash []byte `tlb:"bits 256"`
}

// CollatedDataSeparator separates metadata roots from the virtual proof roots
// in the multi-root BoC used for collated block data.
type CollatedDataSeparator struct {
	_ Magic `tlb:"#fa8b2b92"`
}
