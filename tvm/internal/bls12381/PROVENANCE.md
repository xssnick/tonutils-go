# Vendored copy of cloudflare/circl BLS12-381

The files in this directory are a **verbatim copy** of the top-level package
`github.com/cloudflare/circl/ecc/bls12381` at version **v1.6.4** (BSD-3-Clause,
see `LICENSE`):

    constants.go  doc.go  ec2.go  g1.go  g1Isog.go  g2.go  g2Isog.go  gt.go  pair.go

They still import `github.com/cloudflare/circl/ecc/bls12381/ff` and
`github.com/cloudflare/circl/expander` from the upstream module (unchanged), so
the field arithmetic and hash-to-curve expander are NOT vendored — only the
group layer is.

## Why vendor

The TON Virtual Machine's low-level BLS12-381 opcodes (`BLS_G1_ADD`,
`BLS_G1_MUL`, `BLS_PAIRING`, `BLS_AGGREGATE`, ...) intentionally accept points
that are on the curve but **outside** the prime-order r-torsion subgroup — this
matches the reference C++ node (`crypto/vm/bls.cpp`, which uses raw `blst`
deserialization that only checks on-curve membership) and is explicitly
documented as intentional in TON's `crypto/vm/tonops.cpp`.

Upstream circl's only decode entry point, `G1.SetBytes`/`G2.SetBytes`, always
runs the full `IsOnG1`/`IsOnG2` check (`isValidProjective && isOnCurve &&
isRTorsion`), i.e. it always rejects off-subgroup points. There is no exported
way to decode a point on-curve-only, and the point fields are unexported, so the
group type must live in a package we control.

## Local additions

Everything above is upstream-verbatim. Local additions are:

    oncurve.go   — G1.SetBytesOnCurve / G2.SetBytesOnCurve: a copy of the
                   upstream SetBytes decode with the final validation relaxed
                   from IsOnG1/IsOnG2 to (isValidProjective && isOnCurve),
                   i.e. the r-torsion subgroup check is dropped.

    map.go       — MapToG1 / MapToG2: TON's raw-field map operations, using the
                   vendored CIRCL SSWU, isogeny, cofactor-clearing, and
                   serialization internals directly.

The map API replaces the former `tvm/internal/blsmap` adaptation. No second copy
of CIRCL's mapping formulas or constants is retained: the old implementation's
known vectors are pinned in `map_test.go`, while `circl_parity_test.go` compares
the vendored group, serialization, hash-to-curve, and pairing behavior with the
module copy of CIRCL v1.6.4.

`oncurve_test.go` proves that `SetBytesOnCurve` is byte-for-byte equivalent to
CIRCL's own `SetBytes` for in-subgroup points, and that it additionally accepts
a known on-curve/off-subgroup point that `SetBytes` rejects.

## Re-vendoring

To bump the circl version, re-copy the 9 files above verbatim from the new
release and keep `oncurve.go` and `map.go`. Re-check that the `SetBytes` decode
body mirrored by `oncurve.go` did not change, and run the differential and known
vector tests before updating this provenance note.
