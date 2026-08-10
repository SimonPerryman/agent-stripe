# Phase 12 — More read coverage: Checkout, PaymentMethods, SetupIntents, Subscription/Invoice sub-resources, Webhooks

Status: done

## Goal

Add read coverage for the next batch of resources commonly hit when debugging billing/payment flows:

1. **Checkout Sessions** (`cs_...`) — the hosted-checkout entry point; reconciling a payment frequently starts here.
2. **Payment Methods** (`pm_...`) — cards/banks attached to a customer; common follow-on from a PaymentIntent.
3. **Setup Intents** (`seti_...`) and **Setup Attempts** — save-card / off-session setup flows.
4. **Subscription Items** (`si_...`) and **Subscription Schedules** (`sub_sched_...`) — multi-item and phased subs that the existing `subscription` command can't fully describe.
5. **Invoice Items** (`ii_...`) and **Invoice Line Items** — line-level invoice debugging.
6. **Webhook Endpoints** (`we_...`) — list configured endpoints + a small tie-in to the existing `event` command so an agent can answer "which endpoints would receive this event?".

Each follows the established template (see `internal/commands/dispute/dispute.go` and `subscription/subscription.go` — they're the closest existing analogues): package under `internal/commands/<name>/`, exported `Run(ctx, opts, args)` + `Usage`, subcommands route through the SDK, results go through `agentstripe.ToRawMap` → `cli.EmitSingle` or `cli.RunListOrStream`. No new infrastructure is expected — the read-only HTTP chokepoint in `internal/stripe/readonly.go:33` already covers everything (all the new endpoints are GETs).

Two genuinely-new things worth flagging:

1. **Sub-resource shape.** Invoice line items and setup attempts are *scoped to a parent* (an invoice / a setup intent). Following the precedent set by `balance transactions` (subcommand of `balance`, not a sibling top-level command — see `plans/v1/02-payments.md:60` and the "Resolved decisions" section), these become subcommands of their parent rather than new top-level commands.
2. **Webhook ↔ event tie-in.** A new helper subcommand lets an agent ask "given event type `X` (or event id `evt_X`), which configured endpoints would receive it?" — pure local filter over the endpoints list against `enabled_events`. Read-only, no new state.

## Plan

### 1. `internal/commands/checkoutsession/` — `checkout-session`

- Package: `checkoutsession`; CLI name: `checkout-session` (dispatcher maps hyphenated → package, same pattern as `payment-intent`).
- `get <id>` — `V1CheckoutSessions.Retrieve`. Envelope as a single object.
- `list [--customer C] [--payment-intent PI] [--subscription SUB] [--status S] [--created-gt T] [--created-lt T] [--limit N] [--starting-after CS]`.
  - Stripe's `CheckoutSessionListParams` supports `Customer`, `PaymentIntent`, `Subscription`, `Status`, `CreatedRange`. Verify exact field names against `stripe-go/v85` before wiring.
- No `search` — Stripe's Search API doesn't cover Checkout Sessions.
- `usage`:
  - Note the cs → payment_intent / subscription / setup_intent fan-out (which one is populated depends on `mode`).
  - Recommend `--expand-stripe`: `payment_intent`, `subscription`, `setup_intent`, `customer`, `line_items` (line_items is a synthetic field — only present when explicitly expanded; flag this).
  - Mention `line_items` payload size — fine on `get`, avoid on `list`.

### 2. `internal/commands/paymentmethod/` — `payment-method`

- `get <id>` — `V1PaymentMethods.Retrieve`. Common ids: `pm_...`, `card_...` (legacy).
- `list --customer C [--type T] [--limit N] [--starting-after PM]`.
  - Stripe **requires** `customer` on PaymentMethod list (the un-customer-scoped list endpoint is restricted-keys-only and not what an agent should be hitting).
  - Reject `list` without `--customer` with `fixableBy: "agent"` and a usage hint.
  - `--type` passthrough: `card`, `us_bank_account`, `sepa_debit`, etc. — passthrough verbatim, document the common values.
- No `search`.
- `usage`:
  - Explain that the PaymentIntent flow surfaces `pm_...` ids inside `latest_charge.payment_method` and `payment_method` fields; this command is the lookup endpoint for those.
  - Recommended expands: `customer`.

### 3. `internal/commands/setupintent/` — `setup-intent`

- `get <id>` — `V1SetupIntents.Retrieve`.
- `list [--customer C] [--payment-method PM] [--attach-to-self] [--created-gt T] [--created-lt T] [--limit N] [--starting-after SETI]`.
- **Sub-resource: setup attempts.** `setup-intent attempts <seti_id> [--limit N] [--starting-after SETATT]` — `V1SetupAttempts.List` requires `SetupIntent` as a filter, so it's only meaningful scoped to one. Subcommand rather than sibling, mirroring `balance transactions`.
- No `search`.
- `usage`:
  - Explain the SetupIntent → SetupAttempt → PaymentMethod chain (analogous to PI → Charge but for save-card flows).
  - Recommended expands on `get`: `customer`, `payment_method`, `latest_attempt`.

### 4. `internal/commands/subscriptionitem/` — `subscription-item`

- `get <id>` — `V1SubscriptionItems.Retrieve`.
- `list --subscription SUB [--limit N] [--starting-after SI]`.
  - Stripe requires `subscription` on the list endpoint. Same "reject without it" pattern as `payment-method list --customer`.
- No `search`.
- `usage`:
  - Note that in most cases the items are already returned inline as `subscription.items.data[]`; this command exists for paging through subs with >10 items (rare but real).
  - Recommended expands: `price.product`.

### 5. `internal/commands/subscriptionschedule/` — `subscription-schedule`

- `get <id>` — `V1SubscriptionSchedules.Retrieve`.
- `list [--customer C] [--scheduled] [--created-gt T] [--created-lt T] [--canceled-at-gt T] [--canceled-at-lt T] [--completed-at-gt T] [--completed-at-lt T] [--released-at-gt T] [--released-at-lt T] [--limit N] [--starting-after SS]`.
  - SDK reality (checked in `stripe-go/v85`): only `Scheduled *bool` is a status-ish boolean. The other transition states (canceled/completed/released) are exposed only as **date-range filters** (`CanceledAtRange`, `CompletedAtRange`, `ReleasedAtRange`). There is no single status enum — the original "collapsed `--status` flag" idea doesn't map to the API.
  - Expose what's actually there: `--scheduled` boolean + the three transition date-range pairs. Document the asymmetry in the `usage` string ("filter to *not-yet-started* schedules with `--scheduled`; to find canceled/completed/released ones, use the matching `--<state>-at-gt`/`--<state>-at-lt` window").
  - Stripe's `SubscriptionScheduleListParams` does NOT expose a `Subscription` filter — drop that flag from the original plan.
- No `search`.
- `usage`:
  - Explain phased subs (`phases[].items[].price`) — the agent's primary use case is "what's coming next for this customer".
  - Recommended expands: `subscription`, `customer`, `phases.items.price.product`.

### 6. `internal/commands/invoiceitem/` — `invoice-item`

- `get <id>` — `V1InvoiceItems.Retrieve`.
- `list [--customer C] [--invoice INV] [--pending] [--created-gt T] [--created-lt T] [--limit N] [--starting-after II]`.
  - `--pending` (bool) filters to invoice items not yet attached to an invoice — useful for "what would the next invoice for this customer include?".
- No `search`.
- `usage`:
  - Distinguish from invoice **line items**: an `InvoiceItem` (`ii_...`) is a pre-invoice charge sitting on a customer; an invoice **line** (`il_...`) is the finalized line on a specific invoice. There's overlap but they're different resources — flag this in usage.

### 7. Invoice line items: subcommand of `invoice`

- Add `invoice lines <invoice_id> [--limit N] [--starting-after IL]` to the existing `internal/commands/invoice/invoice.go`.
- SDK method confirmed: `opts.Client.V1Invoices.ListLines(ctx, &stripeapi.InvoiceListLinesParams{Invoice: stripeapi.String(id)})`. Returns `*V1List[*InvoiceLineItem]`, so the existing `cli.RunListOrStream` plumbing works unchanged.
- The standalone `V1InvoiceLineItems` service exposes only `Update` (write op, blocked by the read-only transport), so we don't import it.
- `--expand-stripe` IS supported here. Recommend `price.product` in the usage string — that's the single most useful expansion for "what did this customer actually pay for".
- Mirrors `balance transactions` as a parent-scoped subcommand rather than a new top-level command.
- Update the existing `invoice` usage string to advertise the new subcommand.
- Existing `invoice get` and `invoice list` behaviour unchanged.

### 8. `internal/commands/webhookendpoint/` — `webhook-endpoint`

- `get <id>` — `V1WebhookEndpoints.Retrieve`. Ids look like `we_...`.
- `list [--limit N] [--starting-after WE]` — no filters supported by Stripe.
- `for-event <event_id_or_type>` — local filter:
  - If arg starts with `evt_`, fetch the event first (`opts.Client.V1Events.Retrieve`) and extract `event.type`.
  - Otherwise treat the arg as a literal event type (e.g. `charge.succeeded`).
  - List all endpoints (page through using `agentstripe.CollectRawList` — webhook endpoints typically number in the single digits, so a full drain is fine).
  - Filter to endpoints whose `enabled_events` contains `*` or the resolved type. Emit as a list envelope.
  - This is the only "tie-in" subcommand; it stays under `webhook-endpoint`, not under `event`, because the data type returned is endpoints.
- No `search`.
- `usage`:
  - Note that webhook **delivery attempts** are NOT exposed by the Stripe API — Stripe shows them only in the Dashboard. For delivery-failure debugging, point the agent at the existing `event` command (events carry `pending_webhooks` and `request` metadata).
  - Recommended expands: none (the endpoint shape is small and self-contained).

### 9. Dispatcher wiring

In `cmd/agent-stripe/main.go:42` (`buildRegistry`):

- Add seven new entries (Checkout, PaymentMethod, SetupIntent, SubscriptionItem, SubscriptionSchedule, InvoiceItem, WebhookEndpoint) under appropriate groupings.
- Update top-level help grouping in `internal/cli/dispatch.go` if it has a sectioned listing (verify — Phase 11's audit may have already structured this).

CLI names (hyphenated):
- `checkout-session`
- `payment-method`
- `setup-intent`
- `subscription-item`
- `subscription-schedule`
- `invoice-item`
- `webhook-endpoint`

### 10. Tests (unit)

One test file per new package, sized to match the existing dispute/subscription test files (~120 lines):

- `commands/checkoutsession/checkoutsession_test.go` — `--customer` + `--status` passthrough; `get` with `--expand-stripe line_items`.
- `commands/paymentmethod/paymentmethod_test.go` — `list` without `--customer` returns the structured "fixableBy: agent" error; `--type card` passthrough.
- `commands/setupintent/setupintent_test.go` — `attempts <seti>` calls `SetupAttempts.List` with `SetupIntent` filter; `attempts` without a parent id errors cleanly.
- `commands/subscriptionitem/subscriptionitem_test.go` — `list` without `--subscription` errors cleanly.
- `commands/subscriptionschedule/subscriptionschedule_test.go` — `--scheduled` passthrough sets `Scheduled *bool`; `--canceled-at-gt`/`--canceled-at-lt` populate `CanceledAtRange` (locks in the §5 mapping, including the absence of a synthesized `--status` enum).
- `commands/invoiceitem/invoiceitem_test.go` — `--pending` passthrough.
- `commands/invoice/invoice_lines_test.go` — `invoice lines in_xxx` calls the line-items endpoint with the invoice id; no other params.
- `commands/webhookendpoint/webhookendpoint_test.go`:
  - `list` and `get` smoke tests.
  - `for-event charge.succeeded` filters endpoints whose `enabled_events` includes the type or `*` (use a hand-rolled fixture list).
  - `for-event evt_xxx` fetches the event first, then filters by `event.type`.

### 11. Integration tests (`STRIPE_TEST_KEY`-gated, `//go:build integration`)

One per package, mirroring `internal/commands/dispute/dispute_integration_test.go:1`:

- `checkout-session list --limit 1` (skip the rest if zero in the test account).
- `payment-method list --customer <test-customer> --limit 1`.
- `setup-intent list --limit 1`, then `attempts <id>` if one returned.
- `subscription-item list --subscription <test-sub>`.
- `subscription-schedule list --limit 1`.
- `invoice-item list --limit 1`.
- `invoice lines <test-invoice>`.
- `webhook-endpoint list --limit 1`; `for-event charge.succeeded` against the same.

All gated behind `STRIPE_TEST_KEY`; skip cleanly when unset. `make integration` already wires the build tag.

### 12. README updates

- Add a "Read coverage" table row per new resource (or extend the existing table — verify what's there now).
- Add a worked example to the "Debugging" section: Checkout Session → PaymentIntent → Charge → BalanceTransaction. This is the single most common reconciliation path and shows off the new command alongside the existing ones.
- Document the `webhook-endpoint for-event` helper in the webhooks section.

### 13. `resource describe` schema reflection

- The existing `resource describe` command (see `internal/commands/resource/describe.go:1`) reflects over registered SDK types. Verify each new resource type is reachable through that reflection so an agent can introspect the shape without making an API call. If the reflection registry is hand-maintained, add the new types; if it auto-discovers, no action.

## Out of scope

- **Write ops** on any of these — same posture as the rest of the CLI, the read-only HTTP transport blocks them anyway (`internal/stripe/readonly.go:33`).
- **Webhook delivery attempts** — not exposed by the Stripe API; documented as such in §8 usage.
- **`SubscriptionSchedule release` / cancel / etc.** — write ops, not in scope.
- **Coupons, Promotion Codes, Credit Notes, Tax Rates, Payment Links** — next batch; deliberately not bundled here to keep the PR reviewable.
- **`search` on any of these resources** — Stripe's Search API only covers Charges, Customers, Invoices, PaymentIntents, Prices, Products, Subscriptions. None of the new resources qualify.

## Resolved decisions

- **Sub-resources are subcommands, not siblings.** `setup-intent attempts`, `invoice lines` (no top-level `setup-attempt` or `invoice-line-item`). Precedent: `balance transactions` (`plans/v1/02-payments.md:60`). Keeps the top-level command list short for LLM scanning.
- **Webhook tie-in lives under `webhook-endpoint`.** `webhook-endpoint for-event <evt|type>` returns endpoints, so it belongs in the endpoints package — not under `event`. The `event` command stays focused on event objects.
- **No Search for any of these.** Stripe's Search API doesn't cover them; documenting the absence in each `usage` string is cheaper than synthesizing client-side search.
- **SubscriptionSchedule status filter** (was open Q1). SDK lookup done — there is no enum, only `Scheduled *bool` plus per-transition date-range filters. §5 rewritten to expose the API honestly rather than synthesizing a fake `--status` enum that would have to lie about which states are "current" vs "historical". Also: `Subscription` is not a valid filter on this list endpoint, dropped from the flag set.
- **Invoice line items SDK method** (was open Q2). Confirmed `V1Invoices.ListLines(ctx, &InvoiceListLinesParams{Invoice: ...})`. The separate `V1InvoiceLineItems` service only exposes `Update` and isn't imported.
- **PaymentMethod list without `--customer`** (was open Q3). Hard reject. The agent path always has a customer in hand by the time it's looking up payment methods (it comes from a PI, a subscription, or a checkout session). The restricted-key escape hatch is a footgun that surfaces irrelevant cross-customer noise; no `--all-customers` flag.
- **`for-event evt_xxx` cost** (was open Q4). Accept the `evt_...` form and document the 2-call cost in usage text. Forcing the agent to call `event get` first and then pass the type string adds a step without saving any calls (same two GETs total). Implicit fetch is friendlier and the cost is small.
- **`for-event` envelope shape** (was open Q5). Return the full webhook endpoint object, matching `webhook-endpoint list`. Consistency wins; the agent can pluck `id`/`url` itself if it wants. The example in the discussion thread used a trimmed `{id, url}` shape for readability only.
- **`--expand-stripe` on `invoice lines`** (was open Q6). Yes. `price.product` expansion is the main use case ("what did this customer pay for"). Cheap to wire — `InvoiceListLinesParams` already has the `Expand` field. Folded into §7.

## Log

- 2026-05-25 — Drafted plan.
- 2026-05-25 — Resolved all six open questions. Two SDK-shape findings reshaped the plan: (a) SubscriptionSchedule has no status enum, only `Scheduled *bool` + transition date ranges; (b) invoice lines is `V1Invoices.ListLines` on the parent service. Sections 5 and 7 rewritten accordingly. Soft decisions (PaymentMethod reject, `for-event` accepts evt_ ids, full envelope, expand on `invoice lines`) recorded above.
- 2026-05-26 — Fixed stale §10 test description for `subscriptionschedule_test.go`: it referenced a `--status canceled` flag that no longer exists after the §5 rewrite. Replaced with `--scheduled` + `--canceled-at-gt`/`--canceled-at-lt` passthrough assertions.
- 2026-08-10 — Marked done. Shipped in `683d104` ("feat: add v2 read coverage for checkout, payment/setup, sub-resources, webhooks", #6); the status header had been left at `draft`.
