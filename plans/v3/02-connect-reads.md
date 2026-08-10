# Phase 14 — Connect: the reads phase 13 left out

Status: draft

## Goal

Phase 13 (`01-connect.md`) made every existing command connected-account-capable
and added the two Connect-only resources with no command. Review of that work
surfaced a further set of reads that Connect investigations keep reaching for
and not finding. This phase collects them.

Nothing here is a defect in phase 13 — those were fixed in the same PR. These
are gaps in coverage, ranked by how often the absence forces a workaround.

## Plan

### 1. `balance transaction get <txn_id>`

`balance transactions` lists; there is no single-object read. Spot-checking one
ledger row today means re-listing a whole payout and filtering client-side.
`V1BalanceTransactions.Retrieve` exists. Cheap, and the most-missed of the set.

### 2. `balance transactions --source <ch_|fee_|tr_|po_>`

Stripe supports `source` on the balance-transaction list
(`balancetransaction.go:158`); we don't expose it. It is the direct answer to
"what ledger row did this object produce" — currently a scan.

### 3. `charge list --transfer-group`

Stripe supports it (`charge.go:425`). `transfer_group` is the join key across
the legs of a split payment, and the only one that survives when a flow stops
emitting transfers. `transfer list` already has `--transfer-group`; `charge`
should match.

### 4. `payout list --arrival-date-gt/-lt`

Payouts reconcile by arrival date, not creation date. Only `--created-gt/lt`
exists today, which answers a different question.

### 5. `coupon` and `promotion-code`

Deferred once already (`plans/v2/01-more-resources.md:177`). The Connect case
for pulling them forward: a coupon created on the platform and referenced from
a connected account's subscription fails with `No such coupon`, and there is
currently no way to confirm from the CLI which account a coupon lives on —
which is the whole diagnosis.

### 6. Test-clock reads

`test_helpers/test_clocks` get/list. Advancing a clock is a write and stays out
of scope, but *reading* one is how you distinguish "the clock has not advanced"
from "the webhook did not fire" — currently indistinguishable, and every
subscription-billing verification runs on a test clock.

### 7. `--api-version` override

**Architectural, not additive — scope this before building.** Webhook endpoints
pin their own `api_version` (confirmed live: endpoints on one account pinned to
2020-08-27 and 2022-11-15 while the CLI reports 2026-04-22.dahlia). An agent
debugging "the webhook payload disagrees with what the CLI shows" cannot
currently see what the consumer sees.

A request-header flag alone will not deliver this: output is marshalled through
the pinned SDK's structs, so fields the pinned version dropped are discarded
regardless of what the server returns — exactly the failure documented for
`invoice.application_fee_amount` and the stale expand paths. Doing this
properly means a raw-passthrough mode that skips struct marshalling, and that
interacts with truncation, pruning and `--expand`. Design it as its own piece
of work.

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
- §7 and §8 may deserve their own plan files rather than sitting here — split
  them out if either grows past a couple of sections.

## Log

- 2026-08-10 — Drafted from review findings on the phase 13 PR. Items 1–6 are
  additive and independently shippable; 7 and 8 need design first.
