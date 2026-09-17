# Historical mainnet replay witnesses

Copied from the supplied TVM-002 through TVM-008, TVM-010, TVM-012 through
TVM-016, TVM-018 and TVM-020 conformance reports. Tests run offline through the
public transaction API, without a C++ emulator. The MC 1
and MC 94558 witnesses were captured from published `mainnet/v4` history on
2026-09-14.

- `genesis/`: MC 1 (2019-11-15 13:33:05 UTC), its zerostate execution context,
  and the five initial ShardAccounts covering all nine transactions. The test
  selects `vm.GasSchedule2019` explicitly.
- `config-94558.json`: MC 94558 (2019-11-18 16:39:12 UTC), the config account's
  initial ShardAccount, and config, previous-block tuple and libraries supplied
  for predecessor MC 94557. Both transactions require the 2019 schedule and the
  temporary `POP c3` Cell compatibility rule.
- `message-gas-630388.json`: MC 630388 with predecessor MC 630387. The internal
  message retains its original value for the initial gas limit after storage
  fees: `GasSchedule2019` plus `HistoricalMessageGas`.
- `no-prng-756356.json`: shard block `(0, e000000000000000, 991383)`, included
  in MC 756356, with execution `MasterRef` MC 756354. The original VM rejects
  `ADDRAND`: `GasSchedule2019` plus `NoPRNG`.
- `external-state-init-1688013.json`: shard block `(0, a000000000000000, 2221626)`,
  included in MC 1688013, with execution `MasterRef` MC 1688011. An already
  active account accepts an external message carrying an empty StateInit:
  `GasSchedule2019` plus `HistoricalExternalStateInit`.
- `no-blkdrop2-2221421.json`: shard block `(0, a000000000000000, 2928657)`, included
  in MC 2221421, with execution `MasterRef` MC 2221418. The original VM rejects
  `BLKDROP2`: `GasScheduleEarly2020` plus `NoBLKDROP2`.
- `message-gas-v1-2937658.json`: shard block `(0, a000000000000000, 3961312)`,
  included in MC 2937658, with execution `MasterRef` MC 2937655 and global
  version 1. Both transactions match with `HistoricalMessageGas` alone. The
  first transaction records an initial gas limit of 100000; a separate control
  preserves the modern default of 99999 and its complete transaction hash.
- `storage-fee-13503401.json`: MC 13503401 with predecessor MC 13503400 and
  global version 2. `HistoricalStorageFee` reproduces the old storage arithmetic,
  giving zero storage fees and the original transaction and Account hashes.
  A modern control retains the 69742905468-nanoton storage fee and both hashes.
- `public-library-deploy-17734191.json`: MC 17734191 with predecessor MC 17734190
  and global version 2. `HistoricalPublicLibraryDeploy` permits a masterchain
  UNINIT account to deploy using a matching StateInit with one public library.
  Exact replay yields 1119 gas, 6 VM steps and exit 0. Modern execution rejects
  the deployment; accept-only checks cover both choices.
- `external-state-init-v2-19763575.json`: shard block 24895177, included in
  MC 19763575, with execution `MasterRef` MC 19763573. An active version-2
  account accepts a mismatching external StateInit with
  `HistoricalExternalStateInit`. The modern control rejects before execution.
- `action-rollback-19950828.json`: shard block 25097435, included in MC 19950828,
  with execution `MasterRef` MC 19950825. A failed action phase rolls back an
  earlier send's deletion request, retaining the active account. This is an
  unconditional correctness fix and needs no historical option.
- `action-state-limits-20693766.json.gz`: shard block 25891474, included in
  MC 20693766, with execution `MasterRef` MC 20693761. Both transactions match
  with `HistoricalNoActionStateLimits`; the modern control rejects the first
  transaction's resulting state with action code 50. The fixture is compressed
  losslessly, and tests check the original JSON's SHA-256 after decompression.
- `malformed-library-23830662.json`: shard block 29259549, included in
  MC 23830662, with execution `MasterRef` MC 23830659. Malformed outgoing
  StateInit libraries invalidate the action list before action execution,
  producing code 34. No historical option is needed.
- `nan-comparison-26399968.json`: shard block 31958896, included in MC 26399968,
  with execution `MasterRef` MC 26399963. Both transactions match with
  `Historical.NaNComparison`; the modern control preserves the second
  transaction's exit 4. `nan-comparison-26399968-after.boc` independently checks
  the final Account and last-transaction commitments; it is never an execution
  input. All five witnesses above use workchain 0, shard `8000000000000000` and
  global version 2.

The JSON files are byte-for-byte copies of the supplied fixtures, with only the
large action-state-limits witness compressed for storage. All files in each
report passed its SHA-256 manifest before testing.

The block BOCs are Merkle proofs with `state_update` pruned. Tests verify their
original root commitments; their file SHA-256 is not the full block's FileHash.
The original BlockIDs are retained in the JSON. Zerostate file SHA-256 and root
are both checked, and its config and libraries are extracted directly from that
state. Other fixtures include their execution inputs but no separate proof
binding config to the referenced master state. A shard block's execution
MasterRef is checked separately from its inclusion masterchain sequence number.

Replay verifies predecessor BlockIDs, initial and final account-block hashes,
each transaction's old/new Account hash and previous-transaction chain, complete
transaction hashes, last-trans fields, gas limits/credit/fees, total fees,
outgoing message counts, VM steps and exit codes. The 26 transactions advance
using only computed accounts and storage statistics from the emulator's result.
The first MC 94558 transaction is pinned to
`3f75f750162d4c9defbea07eb840e9fe38bc60d526d5a7abfff72e17b542422e`.

Historical source references:

- [2019 cell-load prices and implicit continuations](https://github.com/ton-blockchain/ton/blob/e30d98eb30d35b6808d2511c93631a72e5882514/crypto/vm/continuation.cpp)
- [Early-2020 cached cell loads](https://github.com/ton-blockchain/ton/blob/77842f9b637dd2efcd684a4668e9ff4a173449f8/crypto/vm/vm.cpp)
- [Introduction of implicit continuation gas](https://github.com/ton-blockchain/ton/commit/e27fb1e09c7332d4eea0eb4f89ff0b7275257c10)
- [Temporary `exec_bless_pop_c3` hook](https://github.com/ton-blockchain/ton/blob/7f3a22a217c0998d9cadca972d2be6aa327711a1/crypto/vm/contops.cpp)
- [Config contract's explicit BLESS fix](https://github.com/ton-blockchain/ton/commit/7f3a22a217c0998d9cadca972d2be6aa327711a1)
- [Historical initial gas limit and external StateInit handling](https://github.com/ton-blockchain/ton/blob/7f3a22a217c0998d9cadca972d2be6aa327711a1/crypto/block/transaction.cpp)
- [Instruction table before PRNG](https://github.com/ton-blockchain/ton/blob/7f3a22a217c0998d9cadca972d2be6aa327711a1/crypto/vm/tonops.cpp)
- [Early-2020 stack instruction table before BLKDROP2](https://github.com/ton-blockchain/ton/blob/77842f9b637dd2efcd684a4668e9ff4a173449f8/crypto/vm/stackops.cpp)
- [Early-2020 independent initial gas limit](https://github.com/ton-blockchain/ton/blob/77842f9b637dd2efcd684a4668e9ff4a173449f8/crypto/block/transaction.cpp)
- [Historical storage-fee arithmetic and sign check](https://github.com/ton-blockchain/ton/blob/9f008b129f1fec6c72a5e67e69ddf9caca02d27f/crypto/block/transaction.cpp)
- [Storage arithmetic normalization upgrade](https://github.com/ton-blockchain/ton/commit/9f93888cf402f8421fef38406b67886be043ac58)
- [Introduction of the masterchain public-library deployment restriction](https://github.com/ton-blockchain/ton/commit/7262a66d210b843502353d6aa79406faa5eb9ccf)
- [Introduction of the external StateInit hash check](https://github.com/ton-blockchain/ton/commit/cdf96a21d02bc9eabbaab9ccff84511692d86c2d)
- [Deletion requests are committed only after successful actions](https://github.com/ton-blockchain/ton/blob/41ed354b9fab9fa7d99a499b2a57eaeb635e32db/crypto/block/transaction.cpp)
- [Introduction of action-phase state limits](https://github.com/ton-blockchain/ton/commit/d8dd75ec83224799afd3fd475e8f5506568ffc85)
- [Structural validation of outgoing actions and libraries](https://github.com/ton-blockchain/ton/blob/9f008b129f1fec6c72a5e67e69ddf9caca02d27f/crypto/block/transaction.cpp)
- [Historical integer comparisons](https://github.com/ton-blockchain/ton/blob/9f008b129f1fec6c72a5e67e69ddf9caca02d27f/crypto/vm/arithops.cpp)

These witnesses establish specific historical executions. Git commit dates do
not establish network activation heights, and global version zero alone does
not select a historical gas schedule or enable the temporary hook.

## Selecting historical rules

Leaving all historical options unset preserves modern behavior, including for
global version zero. Replay callers can explicitly set:

```go
opts := tvm.TransactionOptions{
    Historical: vm.HistoricalConfig{
        GasSchedule: vm.GasSchedule2019,
        PopC3Cell:    true, // Only when the temporary hook is required.
    },
}
result, err := machine.EmulateTransaction(block, account, message, opts)
```

`ExecutionConfig` and `MessageEmulationConfig` expose the same `Historical`
field, and child VMs inherit it. Public execution entry points reject the old
gas schedules and `PopC3Cell` outside global version zero and reject unknown
schedules. The other independent options have the version bounds listed below.

| Schedule | First / repeated cell load | Implicit RET / JMPREF |
| --- | --- | --- |
| `GasScheduleModern` | 100 / 25 | 5 / 10 |
| `GasSchedule2019` | 100 / 100 | 0 / 0 |
| `GasScheduleEarly2020` | 100 / 25 | 0 / 0 |

`PopC3Cell` is independent of the gas schedule. It changes only fixed `POP c3`
with a Cell operand, charges the actual cell load, and adds no synthetic
CTOS/BLESS instructions. No timestamps, original gas values or output hashes
are used to select rules or alter execution results.

Additional options are independent of the selected gas schedule. Transaction
options support the versions listed below without enabling version-zero VM
rules. Version ranges restrict opt-ins; they do not automatically select them:

| Field | Global versions | Historical behavior |
| --- | --- | --- |
| `Historical.NoPRNG` | 0 | Treat `RANDU256`, `RAND`, `SETRAND` and `ADDRAND` as unknown opcodes. |
| `Historical.NoBLKDROP2` | 0 | Treat `BLKDROP2` as an unknown opcode. |
| `Historical.NaNComparison` | 0–3 | Make binary integer comparisons return a finite left operand when the right operand is NaN. |
| `TransactionOptions.HistoricalMessageGas` | 0, 1 | Keep the credited message amount after storage fees and buy the initial gas limit from it independently of the account gas maximum. `ACCEPT` and `SETGASLIMIT` still use the real maximum. |
| `TransactionOptions.HistoricalExternalStateInit` | 0–4 | Accept a mismatching external StateInit for an already active account, making its libraries available to execution. Deployment and unfreezing checks stay strict. |
| `TransactionOptions.HistoricalStorageFee` | 0–3 | Reproduce the old storage-fee arithmetic, including the sign of non-normalized intermediate limbs. |
| `TransactionOptions.HistoricalPublicLibraryDeploy` | 0–4 | Permit public libraries in masterchain UNINIT deployment while preserving StateInit address and other state validation. |
| `TransactionOptions.HistoricalNoActionStateLimits` | 0–3 | Skip the final action-phase state-size and public-library limits while preserving message and StateInit admission checks. |

The opcode and comparison flags are available through all VM execution configs
and are inherited by child VMs. Unknown opcodes use the usual exception path,
charging 10 gas before the exception while leaving instruction bits and stack intact.
The transaction flags belong to the transaction layer and apply to both
full transaction emulation and external-message acceptance checks.
