package crc16

const (
	poly    uint16 = 0x1021 // CRC-16-CCITT
	initCRC uint16 = 0x0000 // XMODEM init
	xorOut  uint16 = 0x0000
)

var table [256]uint16

func init() {
	for i := 0; i < 256; i++ {
		var c = uint16(i) << 8
		for j := 0; j < 8; j++ {
			if (c & 0x8000) != 0 {
				c = (c << 1) ^ poly
			} else {
				c <<= 1
			}
		}
		table[i] = c
	}
}

func ChecksumXMODEM(data []byte) uint16 {
	c := initCRC
	for _, b := range data {
		idx := byte((c >> 8) ^ uint16(b))
		c = (c << 8) ^ table[idx]
	}
	return c ^ xorOut
}
