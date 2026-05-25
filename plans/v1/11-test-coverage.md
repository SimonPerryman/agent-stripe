# Phase 11 — Test coverage hardening

Status: todo

## Context

Post-v1 audit. Overall `go test ./... -cover` reports **42.2%**. Unit tests use a
consistent `httptest` pattern; integration tests are gated by
`//go:build integration` + `STRIPE_TEST_KEY`. Several critical packages are at or
near 0% coverage, and a handful of command packages are well under 25%.

## Goal

Lift coverage on the highest-leverage / highest-risk areas, in priority order.
Use the existing `httptest` pattern (see `internal/commands/charge/charge_test.go`)
and the existing integration tag convention — do not introduce new test
frameworks.

## Order (by ROI)

1. Pagination & streaming helpers in `internal/stripe/`
2. `internal/commands/account/` + mockable keyring in `internal/config/`
3. Low-coverage command packages (`balance`, `dispute`, `customer`, `refund`, `paymentintent`)
4. Smoke test for `cmd/agent-stripe/main.go`

---

## 1. `internal/stripe/` pagination & streaming (currently ~19%)

Zero-coverage functions, all used by every list command:

- `CollectRawList` — `internal/stripe/pagination.go`
- `StreamRawList` — `internal/stripe/stream.go`
- `CollectRawSearch` — `internal/stripe/search.go`
- `StreamRawSearch` — `internal/stripe/search.go`

Add `internal/stripe/pagination_test.go`, `stream_test.go`, `search_test.go`.

Each should:

- Stand up `httptest.NewServer` returning paginated JSON (`has_more`,
  `starting_after` / `next_page` semantics).
- Cover: single page, multi-page traversal, empty result, server error mid-stream,
  page-size honoured, context cancellation mid-stream.
- For streaming variants, assert that records are emitted incrementally rather
  than buffered.

This is the highest-leverage block — every list/search command sits on top of it.

## 2. `internal/commands/account/` (currently 0%) + keyring

Security-sensitive: credential storage, add/remove/list/set-default/test.

Blocker: `internal/config/keyring.go` directly calls the OS keychain, so it
can't be tested as-is. Steps:

- Extract a small keyring interface in `internal/config/keyring.go` (e.g.
  `type Keyring interface { Set/Get/Delete(service, key string) ... }`).
- Wire the real implementation as the default; allow tests to inject a stub.
- Add `internal/config/keyring_test.go` covering `SetSecret` / `GetSecret` /
  `DeleteSecret` against the stub.
- Add `internal/commands/account/account_test.go` covering each subcommand
  (add, remove, list, set-default, test) using the stub keyring + `httptest`
  for the Stripe `/v1/account` probe used by `test`.

## 3. Low-coverage command packages

Apply the existing `httptest` pattern (template:
`internal/commands/charge/charge_test.go`). Add list/get/describe coverage for:

- `internal/commands/balance/` (15.4%)
- `internal/commands/dispute/` (16.3%)
- `internal/commands/customer/` (19.6%)
- `internal/commands/refund/` (21.6%)
- `internal/commands/paymentintent/` (32.2%)

Per package: one test file, table-driven where the subcommands share shape.
Cover JSON envelope structure, error propagation, and any resource-specific
flags (e.g. dispute evidence, refund reasons).

## 4. `cmd/agent-stripe/main.go` smoke test (currently 0%)

One test that runs `main` (or its `run(ctx, args, stdout, stderr)` equivalent —
extract if needed) with `--help` and a no-op command. Asserts:

- Registry dispatch wires the expected commands.
- Signal handler installs cleanly and exits on `ctx` cancel.

Not exhaustive — just a wiring regression guard.

---

## Out of scope

- Replacing `httptest` with a Stripe SDK mock.
- Coverage for `internal/testutil/` (it's only exercised via other tests).
- Lifting integration test coverage — separate effort.

## Success criteria

- `go test ./... -cover` overall ≥ 65%.
- No package below 40% except `cmd/agent-stripe` (smoke-only) and `testutil`.
- `internal/stripe/` ≥ 70%.
- `internal/commands/account/` ≥ 60%.
