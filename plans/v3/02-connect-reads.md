# Phase 14 — Connect: the reads phase 13 left out

Status: draft

## Goal

Phase 13 (`01-connect.md`) made every existing command connected-account-capable
and added the two Connect-only resources with no command. Review of that work
surfaced a further set of reads that Connect investigations keep reaching for
and not finding. This phase collects them.

Nothing here is a defect in phase 13 — those were fixed in the same PR. These
are gaps in coverage.

## The version blind spot — one cause behind three symptoms

Three separate findings from the phase 13 review turned out to be the same
thing, and it is the largest structural gap in the tool:

1. `invoice.application_fee_amount` is unreadable.
2. Several advertised expand paths were accepted by Stripe but silently
   discarded, yielding `null` with no error.
3. There is no way to see what a webhook consumer on an older API version
   actually receives.

**Common cause:** output is marshalled through the pinned SDK's response
structs. Any field that version does not model is dropped, regardless of what
the wire returns. There is no error and no warning — the field is simply
absent, indistinguishable from "Stripe didn't send it".

Confirmed on `application_fee_amount` specifically. Requesting the same
test-mode invoice twice:

- with no `Stripe-Version` header (account default, pre-Basil) — **present**
- with `Stripe-Version: 2026-04-22.dahlia` (what this CLI pins) — **absent**

Stripe's Basil release moved Invoice's flat linkage fields (`subscription`,
`charge`, `payment_intent`) under `parent` and dropped `application_fee_amount`
outright. Webhook endpoints pin their own version independently, and pre-Basil
endpoints still receive the field — confirmed live: endpoints on one account
pinned to `2020-08-27` and `2022-11-15` while this CLI reports
`2026-04-22.dahlia`.

So an agent cannot verify a fee against the payload its own webhook consumers
receive, and "the CLI shows no application fee" is not evidence that there
isn't one. That makes this a correctness gap, not a convenience one, and it is
the item to do first.

### 1. `--api-version` override **and** raw-body passthrough (blocking)

**Two parts, and the flag alone is useless.** A `Stripe-Version` request header
changes what the server sends; it does not change what our marshalling keeps.
v85's `Invoice` struct has no `ApplicationFeeAmount` field, so the value would
be discarded on arrival however the request was framed. Shipping the flag on
its own would look like it worked while dropping the same fields — worse than
not having it.

**Shape:** mirror `--stripe-account` exactly. A `--api-version` global flag,
`AGENT_STRIPE_API_VERSION` env fallback, no config-file default, injected as a
`Stripe-Version` header at the same transport chokepoint. The envelope already
echoes `apiVersion`; it must report the *effective* version rather than the
pinned constant, or the echo becomes a lie.

**Raw mode is smaller than first assumed.** A spike (2026-08-10) confirmed the
existing render pipeline needs no changes: `output.Render` already has a fast
path for `map[string]any`, so unmarshalling Stripe's response body into a map
instead of a struct feeds truncation, pruning, `--expand` and the `--stream`
record path unmodified. `stripe.APIResponse` exposes `RawJSON []byte` on every
response, and the SDK has a `RawRequest` backend, so the body is already
reachable without new HTTP plumbing.

Measured blind spot, same invoice at two versions:

    fields present at 2022-11-15 but absent at 2026-04-22.dahlia (13):
      application_fee_amount, charge, discount, paid, paid_out_of_band,
      payment_intent, quote, rendering_options, subscription,
      subscription_details, tax, total_tax_amounts, transfer_data

Remaining decisions, none of them large:
- Global mode (`--raw`) or implied whenever `--api-version` is set? Implied is
  friendlier; explicit is more predictable.
- Cursor pagination reads `has_more`/`id` off the decoded map — fine for a map,
  but the typed list helpers need a raw sibling.
- `resource describe` reflects over pinned structs and cannot describe another
  version's shape. Say so rather than pretending otherwise.

## Additive reads

Independently shippable, no design needed.

### 2. `balance transaction get <txn_id>`

`balance transactions` lists; there is no single-object read. Spot-checking one
ledger row today means re-listing a whole payout and filtering client-side.
`V1BalanceTransactions.Retrieve` exists. Cheap, and the most-missed of the set.

### 3. `balance transactions --source <ch_|fee_|tr_|po_>`

Stripe supports `source` on the balance-transaction list
(`balancetransaction.go:158`); we don't expose it. It is the direct answer to
"what ledger row did this object produce" — currently a scan.

### 4. `charge list --transfer-group`

Stripe supports it (`charge.go:425`). `transfer_group` is the join key across
the legs of a split payment, and the only one that survives when a flow stops
emitting transfers. `transfer list` already has `--transfer-group`; `charge`
should match.

### 5. `payout list --arrival-date-gt/-lt`

Payouts reconcile by arrival date, not creation date. Only `--created-gt/lt`
exists today, which answers a different question.

### 6. `coupon` and `promotion-code`

Deferred once already (`plans/v2/01-more-resources.md:177`). The Connect case
for pulling them forward: a coupon created on the platform and referenced from
a connected account's subscription fails with `No such coupon`, and there is
currently no way to confirm from the CLI which account a coupon lives on —
which is the whole diagnosis.

### 7. Test-clock reads

`test_helpers/test_clocks` get/list. Advancing a clock is a write and stays out
of scope, but *reading* one is how you distinguish "the clock has not advanced"
from "the webhook did not fire" — currently indistinguishable, and every
subscription-billing verification runs on a test clock.

### 8. Multi-account fan-out (`--all-connected`)

Portfolio-scale questions ("which of these accounts cannot accept payments")
are a shell loop plus manual correlation today. Also architectural: it needs a
concurrency limit, per-account error isolation (one 403 must not abort the
sweep), and an envelope shape that attributes each row to its account. The
`stripeAccount` echo already added is the building block.

## Out of scope

- Anything that writes, per the standing posture.
- Connect onboarding, `topup`, `country-spec`, v2 accounts — unchanged from
  phase 13.

## Open questions

- Should `--expand-stripe` auto-prefix `data.` on list endpoints? Phase 13
  documented the rule instead of applying it, on the grounds that silently
  rewriting a user's input is worse than a clear 400 from Stripe (whose error
  message already names the correct path). Worth revisiting if it keeps
  tripping people up.
- §1 and §8 may deserve their own plan files rather than sitting here — split
  them out if either grows past a couple of sections.

## Log

- 2026-08-10 — Drafted from review findings on the phase 13 PR. Items 2–7 are
  additive and independently shippable; 1 and 8 need design first.
- 2026-08-10 — Reordered. `--api-version` was buried mid-list as a convenience;
  it is the blocker. Three findings that looked separate — the unreadable
  `invoice.application_fee_amount`, the silently-discarded expand paths, and
  the inability to see an older consumer's payload — all trace to output being
  marshalled through the pinned SDK's structs. Recorded that as one cause, with
  the same-invoice pinned/unpinned comparison that proves it, and noted that a
  `--api-version` flag without raw passthrough would fix none of them.
- 2026-08-10 — Spiked raw passthrough. Downgraded from "architectural, needs
  design" to a contained change: output.Render's map fast path means the whole
  downstream pipeline works unmodified, and APIResponse.RawJSON already exposes
  the body. Quantified the gap at 13 Invoice fields missing at the pinned
  version versus 2022-11-15. The two-part requirement stands — the flag without
  raw mode fixes nothing — but this is no longer the hardest item here.

