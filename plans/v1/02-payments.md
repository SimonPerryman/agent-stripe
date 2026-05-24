# Phase 2 — Payments coverage

Status: done

## Goal

Add read coverage for the money-movement resources: `charge`, `payment-intent`, `refund`, `dispute`, `balance`, `payout`. Each new package follows the template Phase 1 established with `customer` — register in dispatcher, `Run(ctx, args)` dispatches subcommands, `get`/`list` route through the read-only chokepoint, results land in the envelope. No new infrastructure is expected here; the test of a good Phase 1 is that Phase 2 is mostly typing.

Two things genuinely *are* new and worth flagging up front:

1. **`--expand-stripe` becomes load-bearing.** Payments are where nested objects start to matter (a `PaymentIntent` without `latest_charge` or `customer` expanded is almost useless for debugging). Phase 1 deferred the flag; Phase 2 wires it through the global `FlagSet` and the per-command `list.Params`.
2. **Truncation gets stress-tested.** `Dispute.evidence` and `Charge.outcome` carry large free-text fields. Phase 1's truncator is exercised here for real — expect to tune `truncation.maxLength` or add per-field overrides.

Phase 1's deferred integration tests (`STRIPE_TEST_KEY`-gated) also land here, since Payments are easy to exercise with Stripe's test-mode fixtures.

## Plan

### 1. Global `--expand-stripe` plumbing

- Add `--expand-stripe <comma-list>` to the root `FlagSet` in `cmd/agent-stripe/main.go`.
- Thread the parsed slice into the command context (likely a new field on whatever options struct the dispatcher passes — if no struct exists yet, add one rather than growing `Dispatch`'s signature).
- Each command's `get` / `list` translates the slice into `params.Expand = stripe.StringSlice(expand)`.
- Document accepted paths per resource in each command's `usage` (the Stripe SDK doesn't validate these — typos return the un-expanded shape silently, so the docs matter).
- Test: a `charge get` request with `--expand-stripe customer,balance_transaction` produces an outbound request whose query string contains `expand[]=customer&expand[]=balance_transaction`. Use the `httptest` pattern from `stripe/client_test.go`.

### 2. `internal/commands/charge/`

- `get <id>` — `charge.Get`, envelope.
- `list [--customer] [--created] [--limit] [--starting-after]` — paginated list via `stripe/pagination.go`.
- `usage` — note that `outcome.seller_message` and `failure_message` are the agent's primary debugging fields; recommend `--full` when investigating a specific failure.
- No `search` here — that lands in Phase 4 alongside the other resources' search endpoints.

### 3. `internal/commands/paymentintent/`

- Package name: `paymentintent` (Go), CLI command: `payment-intent` (dispatcher maps).
- `get <id>` — `paymentintent.Get`. Common expands: `latest_charge`, `customer`, `payment_method`.
- `list [--customer] [--created] [--limit] [--starting-after]`.
- `usage` — explain the PI → Charge → BalanceTransaction chain so the agent knows when to follow which link.

### 4. `internal/commands/refund/`

- `get <id>` — `refund.Get`.
- `list [--charge] [--payment-intent] [--created] [--limit] [--starting-after]`.
- At most one of `--charge` / `--payment-intent` may be set; reject both with `fixableBy: "agent"`.
- `usage` — call out that refund creation is **not** here; Phase 5 may add it behind `--confirm`.

### 5. `internal/commands/dispute/`

- `get <id>` — `dispute.Get`.
- `list [--charge] [--created] [--limit] [--starting-after]`.
- **Truncation override**: `dispute.evidence.*` fields routinely exceed 200 chars; truncating them defeats the purpose of fetching a dispute. Options:
  - (a) Bump global `truncation.maxLength` default (rejected — affects everything).
  - (b) Auto-add `evidence.*` paths to the implicit expand list for `dispute get` only.
  - (c) Suggest `--full` in the dispute `usage` string and leave behaviour as-is.
  - Lean: **(c)** for v1, revisit if it bites. Cheapest, keeps truncation uniform.

### 6. `internal/commands/balance/`

- `get` — no id. `balance.Get`. Returns `available[]`, `pending[]`, `instant_available[]` arrays. Envelope as a single object.
- `transactions [--type] [--created] [--limit] [--starting-after]` — `balancetransaction.List`. Paginated. The `type` filter is high-signal for agent debugging ("show me the fee for this charge").
- `usage` — clarify that `balance get` is a snapshot (not historical) and `transactions` is the ledger.

### 7. `internal/commands/payout/`

- `get <id>` — `payout.Get`.
- `list [--status] [--created] [--limit] [--starting-after]`.
- `usage` — note that `payout.id` appears as `automatic_transfer_id` on related balance transactions; cross-reference both directions.

### 8. Dispatcher wiring

- Register all six packages in `internal/cli/dispatch.go` (or whatever the dispatcher file is called).
- Hyphenated command names: `payment-intent`. Map to the `paymentintent` package — keep the CLI name in one switch arm so renames stay local.
- Update top-level `usage` to list the new commands grouped under "Payments".

### 9. Integration tests (deferred from Phase 1)

- Add `*_integration_test.go` files with `//go:build integration` per command package.
- Gated by `STRIPE_TEST_KEY` env var; skip cleanly when unset.
- Coverage: one happy-path call per resource (`get` against a known fixture id in test mode, `list` with `limit=1`).
- Add a `make integration` target that sets the build tag and runs the suite.
- README: document how to acquire a Stripe test key and run the suite locally.

### 10. Tests (unit, alongside code)

- `commands/charge/list_test.go` — pagination cursor handling, `--customer` filter passthrough.
- `commands/paymentintent/get_test.go` — `--expand-stripe latest_charge` produces correct query string.
- `commands/refund/list_test.go` — rejects `--charge` + `--payment-intent` together.
- `commands/dispute/get_test.go` — evidence fields ARE truncated under default, NOT truncated under `--full` (locks in the resolved design decision).
- `commands/balance/get_test.go` — envelope shape with `data` as object, no `page` sibling.
- `commands/payout/list_test.go` — `--status pending` passthrough.
- `cli/expand_test.go` — `--expand-stripe a,b,c` parses and lands in the dispatcher options struct.

### 11. README updates

- Flip the Payments table rows from ⏳ to ✅ as each ships.
- Add a "Debugging a failed charge" snippet showing the typical flow: `event list --related ch_xxx` → `charge get ch_xxx --full` → `balance transactions --type charge`.
- Document `--expand-stripe` with a worked example (PaymentIntent + latest_charge).

## Out of scope for Phase 2

- `search` on any resource — Phase 4 (all resources at once, since Search's syntax is uniform).
- `subscription`, `invoice`, `product`, `price` — Phase 3 (Billing).
- `resource describe` schema reflection — Phase 4.
- Write ops (refund create, payout cancel, dispute submit-evidence) — Phase 5 at earliest.
- `--stream` for these resources beyond what Phase 1's plumbing already supports — Phase 4 polishes streaming.

## Resolved decisions

- **Package directory naming**: directory matches package name (`internal/commands/paymentintent/`). Mirrors the stripe-go SDK's own convention (`v85/paymentintent/`, `v85/balancetransaction/`). Dispatcher owns the CLI-name → package mapping (`case "payment-intent":` arm).
- **`balance transactions` shape**: subcommand of `balance`, not a sibling top-level command. Keeps the "snapshot vs ledger" mental model in one place and limits top-level command growth (the LLM scans `usage` linearly). SDK package split (`balance` + `balancetransaction`) is an internal import detail.
- **`dispute get` auto-expand**: no. Keep `--expand-stripe` explicit so response shape stays predictable; document the recommended set (`charge`, `payment_intent`, `charge.balance_transaction`) in `dispute usage`. Auto-expansion would hide cost and break the Phase 1 design principle that envelope shape is reasoned about, not surprised by.
- **Per-field truncation overrides**: punt for v1. Tripwire: §10's `commands/dispute/get_test.go` locks in the `--full` workaround. If a second resource hits the same shape in Phase 3 (likely candidate: `invoice.lines.data[].description`), add per-field overrides in `internal/output/` — never in command packages.

## Log

- 2026-05-24 — Drafted plan. Awaiting Phase 1 shakedown before implementation starts.
- 2026-05-24 — Resolved the four open questions (see "Resolved decisions" above). No code changes yet; decisions inform §3 (paymentintent dir), §6 (balance subcommand), §5 (dispute usage docs), §10 (dispute truncation test as tripwire).
- 2026-05-24 — Implemented. Added `--expand-stripe` to the root FlagSet (`cli.GlobalOpts.ExpandStripe`) with a `stripe.ExpandSlice` helper to keep per-command translation a one-liner. Shipped `charge`, `paymentintent`, `refund`, `dispute`, `balance`, `payout` packages with unit tests covering the §10 cases. Integration tests gated by `STRIPE_TEST_KEY` landed in each package; `make integration` already exists from Phase 1.

  Two drifts from §10 worth noting:
  - **Expand query form**: the Stripe Go SDK emits expand as `expand[0]=…&expand[1]=…` (indexed), not the `expand[]=…` form named in §1's test sketch. Both are accepted by the Stripe API. Tests assert key + values appear without pinning the bracket form.
  - **Pagination cursor-walk test skipped.** §10 lists "pagination cursor handling" under `commands/charge/list_test.go`; we covered `--starting-after` passthrough and `--customer` filter but not a multi-page drain. The cursor-walk logic lives in `internal/stripe/pagination.go` (`CollectRawList` + the SDK's `V1List.All`), not in any command package — putting the test under `commands/charge/` would exercise the SDK iterator through a thin pass-through and we'd want six copies (one per resource). If the behaviour is worth pinning, the right home is `internal/stripe/pagination_test.go`. Punted for v1; revisit if a real pagination bug surfaces.
