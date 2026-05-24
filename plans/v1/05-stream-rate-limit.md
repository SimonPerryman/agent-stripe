# Phase 5 — Stream rate-limit pacing

Status: done

## Goal

Add a `--rate-limit <n>` global flag (requests/sec) that paces `--stream`
record emission so a long export can't monopolise the account's Stripe rate
budget (100 ops/sec live, account-wide — shared with production traffic).

Default 15 req/sec. `0` disables pacing.

## Why

Stripe rate-limits per **account**, not per API key. An agent running
`agent-stripe charge list --stream` over a year of history can sustain
~100 ops/sec (1 request per 100-record page) and crowd out production
traffic on the same account. Today nothing in agent-stripe paces itself;
we rely on the SDK's 429 backoff to absorb bursts. That's not good enough
when a key is shared with prod.

## Design

- Pacing is **per-record**, sized so request rate = configured rate.
  `--stream` forces page size to 100, so record-interval =
  `1s / (rate × 100)`. At default 15 req/sec → 666µs per record →
  smooth 1500 records/sec throughput, ~15 HTTP calls/sec.
- Wired into `internal/cli/stream.go::streamIter` by wrapping the `emit`
  callback. No changes needed in `internal/stripe/stream.go` — pacing
  lives one layer up, where global opts are visible.
- Flag in `internal/cli/dispatch.go`. `RateLimit float64` added to
  `GlobalOpts`. Top-level `usage` lists the flag.
- `0` disables (matches the "0 = unlimited" convention common to
  rate-limit flags). Negative values treated as 0.

## Out of scope

- True token-bucket / request-level pacing (see plan log for trade-off).
  Per-record is good enough for v1 and is honest given the fixed
  page size assumption.
- Per-endpoint pacing (Stripe's per-endpoint 25 req/sec cap). Not a
  realistic risk for a single agent.
- Surfacing pacing on non-stream commands. Single-shot `get`/`list`
  don't sustain enough rate to matter.

## Tests

- `stream_test.go`: pacedEmit honours interval (5 records at rate=1.0
  takes ≥ 30ms); `rate=0` is a no-op (no measurable delay).

## Log

- 2026-05-24 — Plan drafted. Per-record pacing chosen over per-request
  for simplicity (~10 lines vs intercepting the SDK iterator) and
  smoother output. Honest trade-off: assumes `--stream`'s fixed page
  size of 100, which is enforced in every list/search command.
- 2026-05-24 — Shipped. `pacedEmit` in `internal/cli/stream.go` wraps
  the `emit` callback; `streamPageSize = 100` constant locks the
  assumption. `--rate-limit` global flag, default 15.0, 0 = unlimited.
  Tests cover no-op (rate=0), pacing (rate=1 ⇒ ≥30ms across 5 records),
  and error propagation. Full `go test ./...` green.
