package boc

import (
	"github.com/xssnick/tonutils-go/utils"
)

type BocFlags struct {
	hasIndex     bool
	HasCrc32c    bool
	hasCacheBits bool
}

var Magic = []byte{0xB5, 0xEE, 0x9C, 0x72}

func ParseFlags(data byte) (BocFlags, int) {
	return BocFlags{
		hasIndex:     utils.HasBit(data, 7),
		HasCrc32c:    utils.HasBit(data, 6),
		hasCacheBits: utils.HasBit(data, 5),
	}, int(data & 0b00000111)
}
