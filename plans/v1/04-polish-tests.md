# Phase 4 — Plan 2: Automated tests to add

Status: done
Parent: [04-polish.md](./04-polish.md)

## Goal

Close test coverage gaps in Phase 4 work: search across all 7 resources,
streaming helpers in `internal/cli/stream.go`, and an end-to-end integration
sweep for Stripe Search.

## Order (by ROI)

1. Search unit tests for the 6 missing resources
2. `internal/cli/stream_test.go`
3. Integration: search sweep (gated on `STRIPE_TEST_KEY`)
4. Event streaming tests — deferred until [Plan 1](./04-polish-stream-related.md) lands
5. `output/json_test.go` — **already covered**, verify only

---

## 1. Search unit tests for the 6 missing resources

Template: `internal/commands/charge/charge_test.go:117`
(`TestChargeSearch_QueryAndPage` and `TestChargeSearch_MissingQueryErrors`).

Add a matching pair per package:

- `internal/commands/customer/customer_test.go`
- `internal/commands/paymentintent/paymentintent_test.go`
- `internal/commands/subscription/subscription_test.go`
- `internal/commands/invoice/invoice_test.go`
- `internal/commands/product/product_test.go`
- `internal/commands/price/price_test.go`

Per-package layout (not one shared table) — matches existing convention.

Each test should:

- Stand up an `httptest.NewServer` returning a canned `/v1/<resource>/search`
  payload with `has_more`, `next_page`, and one record.
- Assert the request carries `query=` and `limit=`, and that a second call
  with `--page` forwards `page=`.
- `MissingQueryErrors` variant: invoke without `-q` / `--query` and assert
  the dispatch returns a usage error (no HTTP call made).

## 2. `internal/cli/stream_test.go` (new file)

Three unit tests against helpers in `internal/cli/stream.go`:

- **`LimitExplicit`** — build a `flag.FlagSet`, parse with and without
  `--limit`; assert the helper inspects `fs.Visit` (presence), not the
  value. Covers `stream.go:17`.
- **`EnvelopeFor`** — call with `GlobalOpts.Account` set vs. empty;
  assert `output.Envelope.Account`, `Mode`, `APIVersion` populated
  correctly. Covers `stream.go:29`.
- **`StreamList` end-to-end** — `httptest.NewServer` returning two pages
  (page 1 `has_more:true` with `starting_after` cursor, page 2
  `has_more:false`). Wire through a real `stripeapi.V1List[T]`. Assert:
  - iterator drains both pages (record count = sum of both pages),
  - with `cap` smaller than total records, iteration stops at cap,
  - the rendered NDJSON has a header line + N record lines.

Covers `stream.go:58` and the shared `streamIter` at `stream.go:71`.

## 3. Integration: search sweep

One table-driven sub-test gated on `STRIPE_TEST_KEY`. Location: add to an
existing package's `*_integration_test.go` or new
`internal/commands/search_integration_test.go`.

Loops the 7 resources (charge, customer, payment-intent, subscription,
invoice, product, price) with a benign query and `--limit 1`. Assert:

- request returns 2xx,
- response parses as the stream envelope shape,
- 0 results is acceptable (Stripe Search is eventually consistent — we
  test request shape and response handling, not data).

## 4. Event streaming tests — deferred

Depends on Plan 1 (`event list --related` under `--stream`) landing first.

## 5. `output/json_test.go` — already covered

Existing tests already pin the cases Plan 2 flagged:

- `TestRenderExpandPathsSkipsOnlyMatchingPath` at `json_test.go:56`
  covers the "expand path only applies on matching path" invariant
  (the invoice `lines.data.description` shape).
- The Phase 2 dispute regression at `json_test.go:100` is in place.

**Action: verify the names still match, do not duplicate.**

## Out of scope

- Snapshot tests for `resource describe` reflection across every SDK
  type. Rots when the Stripe Go SDK adds fields. Current
  customer/subscription/unknown coverage in
  `internal/commands/resource/describe_test.go` is sufficient.

## Verification

- `go test ./...` green.
- `go test ./... -run Search` exercises all 7 resource search paths.
- `STRIPE_TEST_KEY=… go test ./internal/commands/... -run Integration`
  green against a real test-mode account.
