package tlb

import (
	"fmt"
	"math/big"
)

func (c BlockchainConfig) GetWorkchainDescr(workchain int32) (*WorkchainDescr, error) {
	workchains, err := c.GetWorkchains()
	if err != nil {
		return nil, err
	}
	if workchains == nil || workchains.Workchains == nil {
		return nil, fmt.Errorf("%w: %d", ErrBlockchainConfigParamAbsent, ConfigParamWorkchains)
	}

	value, err := workchains.Workchains.LoadValueByIntKey(big.NewInt(int64(workchain)))
	if err != nil {
		return nil, fmt.Errorf("failed to load workchain descriptor %d: %w", workchain, err)
	}

	var descr WorkchainDescr
	if err = LoadFromCell(&descr, value); err != nil {
		return nil, fmt.Errorf("failed to decode workchain descriptor %d: %w", workchain, err)
	}

	return &descr, nil
}

func (d WorkchainDescr) Fields() WorkchainDescrFields {
	switch v := d.Descr.(type) {
	case WorkchainDescrV1:
		return v.WorkchainDescrFields
	case WorkchainDescrV2:
		return v.WorkchainDescrFields
	default:
		return WorkchainDescrFields{}
	}
}

func (d WorkchainDescr) AcceptMessages() bool {
	return d.Fields().AcceptMsgs
}

func (d WorkchainDescr) ValidAddressLength(addrLen uint) bool {
	switch format := d.Fields().Format.(type) {
	case WorkchainFormatBasic:
		return addrLen == 256
	case WorkchainFormatExtended:
		return format.ValidAddressLength(addrLen)
	default:
		return false
	}
}

func (format WorkchainFormatExtended) ValidAddressLength(addrLen uint) bool {
	minAddrLen := uint(format.MinAddrLen)
	maxAddrLen := uint(format.MaxAddrLen)
	addrLenStep := uint(format.AddrLenStep)

	return addrLen >= minAddrLen && addrLen <= maxAddrLen &&
		(addrLen == minAddrLen || addrLen == maxAddrLen ||
			(addrLenStep > 0 && (addrLen-minAddrLen)%addrLenStep == 0))
}
