# Historical storage fee reference

`cpp-vectors.json` contains 214 outputs from `reference.cpp`, using the unchanged
TON `BigInt256` implementation at
[`9f008b129f1fec6c72a5e67e69ddf9caca02d27f`](https://github.com/ton-blockchain/ton/tree/9f008b129f1fec6c72a5e67e69ddf9caca02d27f).
The corpus includes the MC 13503401 transaction (218 cells, 6250 bits), rounding
and limb boundaries, separate price windows, and 200 deterministic random inputs.

The reference was compiled and rerun locally with Clang on 2026-09-16; every JSON
result matched. Both source files were also fetched independently from the pinned
official repository and matched the reference source hashes:

| Source | SHA-256 |
| --- | --- |
| [`crypto/common/bigint.hpp`](https://github.com/ton-blockchain/ton/blob/9f008b129f1fec6c72a5e67e69ddf9caca02d27f/crypto/common/bigint.hpp) | `a7363b8ed09a61571298dcc97c77d459b6fad123eb4672155a7f4594d68d9fed` |
| [`crypto/common/bigint.cpp`](https://github.com/ton-blockchain/ton/blob/9f008b129f1fec6c72a5e67e69ddf9caca02d27f/crypto/common/bigint.cpp) | `e7fd7fe18e5b98af2ff2504421f9cf4867ef928ed8f5e1ac3ac1bf74ca103776` |

`reference.cpp` takes any argument to emit the full corpus; without arguments it
prints the malformed and normalized values for the original transaction. It
needs the pinned TON `crypto` and `tdutils` include directories and generated
build configuration. The standalone fatal-check sink replaces logging only.

## Behavior being preserved

Old storage calculation accumulates unnormalized signed base-2^52 limbs. Its
rounded right shift retains limb count, so the resulting top limb can be zero
even when the mathematical fee is positive. The storage phase tests that sign
before collection. A normalized sum alone cannot reproduce this result: separate
windows can preserve a different limb count from one combined window.

The immediately preceding source at
[`d6b11d9d3613963291bcf11dc284776a76be3eb8`](https://github.com/ton-blockchain/ton/blob/d6b11d9d3613963291bcf11dc284776a76be3eb8/crypto/block/transaction.cpp)
retains the same arithmetic. Commit
[`9f93888cf402f8421fef38406b67886be043ac58`](https://github.com/ton-blockchain/ton/blob/9f93888cf402f8421fef38406b67886be043ac58/crypto/block/transaction.cpp)
normalizes each partial payment and uses the normalized `td::rshift` helper.
These source snapshots establish implementation behavior, not network activation
dates. Historical replay requires explicit caller context.

The Go historical calculation supports nonnegative operands whose intermediate
limbs fit the reference signed words. It rejects unsigned inputs above
`MaxInt64`, oversized product carries, and signed limb-addition overflow instead
of guessing C++ overflow behavior. Modern calculation is unchanged. Existing
debt collection and account-status rules are outside this arithmetic option.

Run the offline Go regression with:

```sh
CGO_ENABLED=0 GOPROXY=off GOSUMDB=off go test ./tvm -run '^TestHistoricalStorageFee' -count=1
```
