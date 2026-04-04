# AGENTS.md

Guidance for coding agents working in `github.com/xssnick/tonutils-go`.

This document is intentionally repo-specific. Follow the existing code and package boundaries here before applying generic Go advice.

## Project Shape

`tonutils-go` is split by protocol layer and domain:

- `address`: TON address parsing, formatting, flags, bit helpers.
- `tl`: TL schema registration, loader/serialization primitives.
- `tlb`: TLB loaders/serializers built on top of cells and struct tags.
- `tvm/cell`: core cell, slice, builder, dict, proof, BoC primitives.
- `tvm`, `tvm/vm`, `tvm/op/*`, `tvm/tuple`, `tvm/vmerr`: TVM emulator and opcode implementation.
- `adnl`, `adnl/dht`, `adnl/overlay`, `adnl/rldp`: transport and overlay protocols.
- `liteclient`: liteserver connection pool, balancing, sticky contexts, config loading.
- `ton`: high-level blockchain API on top of `liteclient`, proofs, block/account/transaction access.
- `ton/wallet`, `ton/nft`, `ton/jetton`, `ton/dns`: domain clients built on `ton`.
- `toncenter`: HTTP-based client as a separate access path.
- `example`: runnable user-facing examples, kept small and practical.

When adding code, keep it at the lowest layer that owns the responsibility. Do not pull high-level TON client concerns into `tl`, `tlb`, `tvm/cell`, or transport packages.

## Architecture Rules

- Treat `tvm/cell`, `tl`, and `tlb` as foundational packages. They should stay reusable and mostly independent from higher-level TON client logic.
- Keep transport and business logic separate. `liteclient` handles connectivity, node selection, stickiness, retries/timeouts at transport level; `ton` handles proof-aware blockchain semantics on top.
- Put domain-specific smart-contract clients under `ton/*` packages, not in `liteclient` or low-level protocol packages.
- Keep compatibility wrappers when API evolution is cheap. This repo already preserves deprecated aliases, wrapper methods, and compatibility files instead of breaking callers aggressively.
- File naming usually mirrors protocol version or variant: `client-v2.go`, `client-v3.go`, `v5r1.go`, `overlay-adnl.go`, `item-editable.go`. Follow that pattern instead of inventing abstract names.

## Coding Style Seen In This Repo

- Use idiomatic Go with `gofmt`. Do not mass-reformat unrelated files just to normalize import groups or spacing.
- Keep comments and docs in English. Existing README, examples, and code comments are English-first.
- Prefer small exported constructors and wrappers with familiar names:
  - `NewX`
  - `FromX`
  - `WithX`
  - `MustX`
- Safe methods usually return `error`; panic-based variants are explicitly named `Must*`.
- Panic is acceptable for programmer errors or impossible internal states, not normal runtime failures.
- Public APIs favor straightforward structs and functional options over deep builder frameworks.
- Errors are usually wrapped with precise context using `fmt.Errorf("failed to ...: %w", err)`.
- Prefer early returns and explicit branching over clever control flow.
- Short local names are normal when context is obvious: `ctx`, `addr`, `b`, `res`, `api`, `cfg`, `st`.
- Keep wire/protocol registration near the owning package. `init()` is used here for TL registration and similar setup, so adding another localized `init()` is acceptable when it matches existing patterns.
- Preserve concurrency guarantees. Networking code relies on `context`, `sync`, `sync/atomic`, and careful request bookkeeping; avoid changes that weaken thread safety.

## API Design Patterns To Match

- If you add a convenience API, keep the lower-level path available too.
- If behavior is configurable, prefer wrapper/option style already used in the repo:
  - `WithRetry`
  - `WithTimeout`
  - `WithAPI`
  - `WithSigner`
- If a type has both ergonomic and strict forms, mirror the existing duality:
  - safe method returning `error`
  - `Must*` helper panicking on misuse
- For public-facing TON clients, preserve the current feel: practical, direct, and easy to use from examples.

## Testing Conventions

- Use the standard `testing` package only. This repo does not rely on `testify`-style assertion libraries.
- Prefer direct assertions with `t.Fatal` / `t.Fatalf` / `t.Errorf`.
- Use `t.Run` for grouped cases and table-driven tests where it improves clarity.
- Add regression tests close to the touched package, especially for:
  - serialization/deserialization
  - proof validation
  - dictionary logic
  - stack/opcode behavior
  - network failover/sticky behavior
- Keep unit tests deterministic when possible.
- Integration tests exist and often touch live TON infrastructure. Do not silently turn ordinary unit coverage into network-dependent tests.

## Examples And Docs

- `example/` programs are part of the public face of the library. Keep them concise, runnable, and focused on one task.
- When a change affects user workflow, update or add an example if the package is example-driven.
- Prefer practical comments that explain protocol nuance, invariants, or surprising behavior. Avoid boilerplate commentary.

## Change Strategy

- Make narrow changes that respect package ownership.
- Avoid introducing upward dependencies from low-level packages into high-level ones.
- Preserve backward compatibility where reasonable; this repo often keeps deprecated entry points instead of removing them immediately.
- When changing serialization, proof, or protocol code, add round-trip or regression tests in the same area.
- When changing high-level APIs in `ton/*`, verify the surrounding wrappers and examples still make sense.

## Useful Local Checks

Typical checks for this repo:

- `go build -v ./...`
- `go test -v -failfast $(go list ./... | grep -v /example/)`
- targeted package tests such as `go test ./ton/...` or `go test ./tvm/cell`

CI currently builds the whole repo and runs tests excluding `example/` packages, so mirror that unless your change is intentionally example-only.

## What Usually Fits Poorly Here

- Large cross-package refactors without a protocol or API reason.
- New abstractions that hide simple request/response flows.
- Pulling high-level TON logic into foundational packages.
- Replacing explicit error handling with magical helpers.
- Introducing third-party test/assert dependencies for ordinary package tests.
