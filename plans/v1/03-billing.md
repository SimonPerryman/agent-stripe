# Phase 3 — Billing coverage

Status: complete

## Goal

Add read coverage for the recurring-revenue resources: `subscription`, `invoice`, `product`, `price`. Each package follows the template Phases 1–2 cemented — register in the dispatcher, `Run(ctx, opts, args)` dispatches subcommands, `get`/`list` route through the read-only chokepoint, results land in the standard envelope.

By this phase the per-resource work is mostly typing. The genuinely new questions are scoped and called out below — none of them require infrastructure changes, but each is worth deciding before the keyboard starts moving.

What's actually new in Phase 3:

1. **Subscription + Invoice are the first resources whose `get` response is a meaningful tree on its own.** A `Subscription` carries `items.data[]` (line items + price + product refs); an `Invoice` carries `lines.data[]` (potentially dozens of entries with descriptions). Phase 2 dispute punted on per-field truncation overrides with a tripwire ("if a second resource hits the same shape in Phase 3 — likely `invoice.lines.data[].description` — add per-field overrides in `internal/output/`"). Phase 3 has to decide whether to pull that tripwire.
2. **`product` and `price` are catalog data, not event data.** They're small, slowly-changing, and an agent will frequently want the full set rather than a paginated walk. The `list` defaults should reflect that — a higher implicit cap, or at least a clear note in `usage`.
3. **Cross-references multiply.** A subscription points at customer + default_payment_method + latest_invoice + items[].price.product. Phase 2 documented `--expand-stripe` paths per resource; Phase 3 has to be disciplined about which paths are genuinely useful vs. footgun-tier (expanding `latest_invoice.lines` on a `subscription list` blows up payload size).

No new global plumbing is expected. If something needs to be added to `GlobalOpts` or the output package, that's a signal to stop and reconsider.

## Plan

### 1. `internal/commands/subscription/`

- `get <id>` — `opts.Client.V1Subscriptions.Retrieve`, envelope.
- `list [--customer] [--status] [--price] [--created-gt] [--created-lt] [--limit] [--starting-after]`.
  - `--status` accepts the Stripe enum values (`active`, `past_due`, `canceled`, `trialing`, `incomplete`, `incomplete_expired`, `unpaid`, `paused`, `all`). Pass through verbatim; the SDK validates server-side.
  - `--price` filters subscriptions whose `items[].price.id` matches.
- `usage` — call out the common debugging flow: agent gets a complaint about billing, runs `subscription get sub_xxx --expand-stripe customer,latest_invoice,default_payment_method`, then chases `latest_invoice.id` into `invoice get`.
- Common `--expand-stripe` paths to document: `customer`, `latest_invoice`, `default_payment_method`, `items.data.price.product`, `pending_setup_intent`.

### 2. `internal/commands/invoice/`

- `get <id>` — `opts.Client.V1Invoices.Retrieve`.
- `list [--customer] [--status] [--subscription] [--created-gt] [--created-lt] [--limit] [--starting-after]`.
  - `--status` accepts `draft`, `open`, `paid`, `uncollectible`, `void`.
- `usage` — note that `lines.data[].description` is the part agents care about when reconciling a charge to a subscription period, and currently truncates by default. Recommend `--full` until per-field overrides land (see §5).
- Common `--expand-stripe` paths: `customer`, `subscription`, `payment_intent`, `charge`, `lines.data.price.product`.
- **Do not** add an `upcoming` / `preview` subcommand in v1. The old `GET /v1/invoices/upcoming` no longer exists in our pinned API version (`2026-04-22.dahlia`) — stripe-go v85 only exposes `invoice.CreatePreview` → `POST /v1/invoices/create_preview`. Semantically it's a read (the SDK docstring is explicit: "you are simply viewing a preview — the invoice has not yet been created"), but `internal/stripe/readonly.go` rejects by HTTP verb and would block it. Allowing POST-but-read endpoints is a real design question — per-endpoint allowlist vs. switching the chokepoint from verb-based to method-based — and belongs in Phase 4 once we know if other resources have the same shape (likely candidates: tax calculation, quote preview). High-value endpoint; not forgotten, just gated on the safety-model decision.

### 3. `internal/commands/product/`

- `get <id>` — `opts.Client.V1Products.Retrieve`.
- `list [--active] [--ids <comma>] [--created-gt] [--created-lt] [--limit] [--starting-after]`.
  - `--active` is a tri-state: unset = no filter, `--active=true` only active, `--active=false` only archived. Stdlib `flag.BoolVar` won't model this cleanly; use a `string` flag with `""`/`true`/`false` and reject anything else.
  - `--ids` is a Stripe-supported batch lookup (up to 100 ids). Useful when an agent has a list of price IDs and wants the parent products in one call — saves N round trips.
- `usage` — note that products are typically small in count; default `--limit` is fine but recommend `--ids` when the agent already has a known set.

### 4. `internal/commands/price/`

- `get <id>` — `opts.Client.V1Prices.Retrieve`.
- `list [--product] [--active] [--currency] [--type <recurring|one_time>] [--lookup-keys <comma>] [--limit] [--starting-after]`.
  - `--active` follows the same tri-state pattern as `product`.
  - `--lookup-keys` is the high-signal filter for agents working from human-readable identifiers (e.g. `pro_monthly_usd`); document prominently in `usage`.
  - `--currency` is lowercase ISO 4217 (`usd`, `eur`, ...); pass through unchanged.
- `usage` — note the relationship to `product` (every price belongs to a product) and recommend `--expand-stripe product` when fetching a single price.

### 5. Truncation tripwire — `invoice.lines.data[].description`

Phase 2 left this hanging: "If a second resource hits [the dispute shape] in Phase 3 (likely candidate: `invoice.lines.data[].description`), add per-field overrides in `internal/output/`."

Before writing the invoice command, look at a real invoice fixture (Stripe's test mode generates good ones via subscription billing). Decision tree:

- **If `description` fields routinely exceed `truncation.maxLength`**: pull the tripwire. Add per-field-path overrides to `internal/output/json.go`. Shape: `Options{ Full bool, Expand []string, ExpandPaths []string }` where `ExpandPaths` accepts dotted paths like `lines.data.description` and the renderer skips truncation when the current path matches. Update Phase 2's dispute test to assert per-field expand also works there. **Do not** auto-add paths per resource — make the agent opt in via `--expand` (the existing field-name flag, now path-aware) so the envelope shape stays predictable.
- **If `description` fields are usually short** (e.g. "Subscription update"): leave truncation as-is, document `--full` in invoice `usage`, log the decision here, and move the tripwire to Phase 4 as a pre-`search` task (search results across resources will hit the same shape).

Either way, write the decision into the log section below with the actual character counts that drove it.

### 6. Dispatcher wiring

- Register all four packages in `internal/cli/dispatch.go` via whatever registration site Phase 2 established (`cmd/agent-stripe/main.go` likely owns the `Registry` map).
- All four command names are single-word — no hyphen mapping needed (unlike `payment-intent`).
- Update top-level `usage` to list the new commands grouped under "Billing".

### 7. Tests (unit)

- `commands/subscription/list_test.go` — `--status active` and `--price price_xxx` passthrough.
- `commands/subscription/get_test.go` — `--expand-stripe customer,latest_invoice` query string.
- `commands/invoice/list_test.go` — `--status paid` + `--subscription sub_xxx` passthrough.
- `commands/invoice/get_test.go` — locks in the §5 decision: either `lines.data[].description` is truncated by default and full under `--full`, OR the per-field override resolves to untruncated for the named path. Whichever way §5 lands, write the test that pins it.
- `commands/product/list_test.go` — `--active=true` → `active=true` in query, `--active=false` → `active=false`, unset → no `active` key. Also `--ids a,b,c` round-trip.
- `commands/product/list_test.go` — `--active=banana` is rejected with `fixableBy: "agent"`.
- `commands/price/list_test.go` — `--lookup-keys a,b` and `--currency usd` passthrough; `--type recurring` passthrough.

Follow the Phase 2 convention: tests assert query-key presence and values without pinning the SDK's `expand[0]=…` indexed bracket form.

### 8. Integration tests

- One `*_integration_test.go` per package, `//go:build integration`, gated by `STRIPE_TEST_KEY`.
- Coverage: one happy-path `get` (against a fixture id seeded in test mode or skipped if absent) and one `list --limit 1`.
- For `subscription` and `invoice`: the README's existing instructions for acquiring a test key should already cover seeding — if not, add a one-paragraph "seed a test subscription" note pointing at Stripe's CLI fixtures.

### 9. README updates

- Flip Billing table rows from ⏳ to ✅ as each ships.
- Add a "Reconciling a customer complaint" snippet showing the flow: `customer get cus_xxx` → `subscription list --customer cus_xxx` → `invoice list --subscription sub_xxx --status paid` → `invoice get in_xxx --expand-stripe charge`.
- If §5 pulls the truncation override tripwire, document the `--expand` path syntax with a concrete `invoice get … --expand lines.data.description` example.

## Out of scope for Phase 3

- `search` on any resource — Phase 4 (all resources together, uniform Search syntax).
- `invoice preview` (formerly `upcoming`) — Phase 4. It's now `POST /v1/invoices/create_preview` in our pinned API version; the verb-based read-only chokepoint blocks it. Unblocking requires deciding how POST-but-semantically-read endpoints get carved out (per-endpoint allowlist vs. method-based gate). See §2.
- `subscription_item`, `subscription_schedule`, `tax_rate`, `coupon`, `promotion_code`, `credit_note`, `quote`, `usage_record` — out of v1. Track demand; add per-resource if an agent workflow actually needs them.
- Connect-account scoped reads (`--on-behalf-of`, `Stripe-Account` header) — deferred to v2 per PLAN.md decision #2.
- Write ops (subscription cancel, invoice void, price archive, product update) — Phase 5 at earliest.
- Skill packaging + brew tap — Phase 4 / infra plan.

## Open questions to resolve as we go

- **`--active` tri-state encoding**: settle on `string` flag with `""`/`true`/`false` (per §3) vs. introducing a `tribool` helper in `internal/cli/`. Lean: string flag, no helper — only two commands need it and a helper for two callers is premature.
- **`subscription list --status all`**: Stripe accepts the literal string `all` to bypass the default `active`-only filter. Pass through verbatim, or translate `--status=""` (unset) to `all` so agents don't accidentally miss canceled subs when debugging? Lean: pass through verbatim, document in `usage` that the Stripe default is `active`-only.
- **`price list --currency` casing**: reject uppercase or normalize? Lean: normalize to lowercase silently (Stripe is case-insensitive here; rejecting is hostile to agents typing `USD`).

## Resolved decisions

- **§5 truncation tripwire — NOT pulled.** `DefaultTruncateLength` is 200; Stripe-generated invoice line descriptions are short labels ("Subscription update", "Remaining time on Pro Monthly after 24 May 2026", period-style "Time on Pro Monthly (May 24 – Jun 24)") that sit well under that cap. Dispute evidence and `search` result snippets are the real long-form shapes, and both are Phase 4. `invoice` `usage` recommends `--full` for the custom-description edge case. Phase 2's dispute test remains the regression anchor; per-field path-aware `--expand` lands in Phase 4 once `search` proves it's actually load-bearing across resources.
- **`--active` tri-state encoding**: string flag accepting `""` / `"true"` / `"false"`, no `tribool` helper. Two callers (product, price) isn't enough abstraction weight; a helper for two call sites is premature.
- **`subscription list --status all`**: pass through verbatim. Stripe's docs explicitly call out that the endpoint defaults to `active`-only and `all` is the documented escape hatch — silently translating unset → `all` would be a hidden surprise that diverges from the docs the agent is reading.
- **`--currency` casing**: normalize to lowercase silently. Stripe is case-insensitive on input; rejecting `USD` would be hostile to agents typing canonical ISO 4217.
- **Integration test seeding**: none required. Phase 2's pattern (`refund_integration_test.go`) calls `runList(..., --limit 1)` and asserts no error — doesn't pin content. Empty test accounts return empty lists, still a passing happy-path call. Billing tests copy the shape.

## Log

- 2026-05-24 — Drafted plan. Awaiting Phase 2 shakedown — specifically a look at a real invoice fixture before §5 (truncation tripwire) can be resolved.
- 2026-05-24 — Confirmed `invoice upcoming` is no longer a GET in our pinned API version. stripe-go v85 ships `invoice.CreatePreview` → `POST /v1/invoices/create_preview` (no `Upcoming` method). Original deferral reasoning was "different param shape"; real reasoning is verb-based read-only chokepoint blocks POST. Updated §2 and Out-of-scope to reflect.
- 2026-05-24 — Resolved all four open questions plus §5 ahead of implementation (see Resolved decisions). §5 specifically: looked at `DefaultTruncateLength = 200` in `internal/output/json.go` against typical Stripe-generated invoice line descriptions; they're short labels, nowhere near the cap. Tripwire stays Phase 4 (search results, not Billing, are the trigger). Status flipped to ready-to-implement; pilot is `product` per earlier discussion.
- 2026-05-24 — Implemented all four packages (`product`, `price`, `subscription`, `invoice`) following the Phase 2 template. `parseTriBool` and `splitStripeStrings` duplicated in `product` and `price` per the "two callers isn't enough abstraction weight" resolved decision. §5 tripwire confirmed NOT pulled — `invoice get` tests pin the rule that `lines.data[].description` is truncated by default and full under `--full`, same as every other string field. Dispatcher wired; all unit tests pass; integration tests gated as before. README billing table flipped to ✅ with the reconciliation flow snippet.
