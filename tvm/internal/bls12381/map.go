package bls12381

import "github.com/cloudflare/circl/ecc/bls12381/ff"

// MapToG1 maps the first 48 bytes of data, interpreted as a big-endian field
// element reduced modulo p, to G1 using the RFC 9380 simplified SWU map,
// isogeny, and cofactor clearing. It returns the canonical compressed point.
//
// This is the raw-field mapping used by TON's BLS_MAP_TO_G1 opcode. It is not
// hash-to-curve; callers that need hash-to-curve should use G1.Hash.
func MapToG1(data []byte) ([]byte, error) {
	if len(data) < ff.FpSize {
		return nil, errInputLength
	}

	var u ff.Fp
	u.SetBytes(data[:ff.FpSize])

	var q isogG1Point
	q.sswu(&u)

	var p G1
	p.evalIsogG1(&q)
	p.clearCofactor()
	return p.BytesCompressed(), nil
}

// MapToG2 maps the first 96 bytes of data, interpreted as two big-endian field
// elements reduced modulo p, to G2 using the RFC 9380 simplified SWU map,
// isogeny, and cofactor clearing. It returns the canonical compressed point.
//
// This is the raw-field mapping used by TON's BLS_MAP_TO_G2 opcode. It is not
// hash-to-curve; callers that need hash-to-curve should use G2.Hash.
func MapToG2(data []byte) ([]byte, error) {
	if len(data) < ff.Fp2Size {
		return nil, errInputLength
	}

	var u ff.Fp2
	u[0].SetBytes(data[:ff.FpSize])
	u[1].SetBytes(data[ff.FpSize : 2*ff.FpSize])

	var q isogG2Point
	q.sswu(&u)

	var p G2
	p.evalIsogG2(&q)
	p.clearCofactor()
	return p.BytesCompressed(), nil
}
