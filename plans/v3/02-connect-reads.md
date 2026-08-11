# Phase 14 — Connect: the reads phase 13 left out

Status: done

## Goal

Phase 13 (`01-connect.md`) made every existing command connected-account-capable
and added the two Connect-only resources with no command. Review of that work
surfaced a further set of reads that Connect investigations keep reaching for
and not finding. This phase collects them.

Nothing here is a defect in phase 13 — those were found and fixed on the same
PR. These are gaps in coverage, and every item is a params passthrough over an
endpoint the SDK already exposes.

The one review finding that is *not* an additive read — fields that exist on
the wire but are invisible because output is marshalled through the pinned
SDK's structs — is its own phase:
[`03-api-version-raw.md`](03-api-version-raw.md). That is the correctness gap
and should land first; everything here is additive and can wait until it is
wanted.

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

### 7. Multi-account fan-out (`--all-connected`) — deferred, do not start

Portfolio-scale questions ("which of these accounts cannot accept payments")
are a shell loop plus manual correlation today.

**Deliberately not scheduled.** It is the only item here that has not been
spiked, so its sizing is guesswork, and it needs real design: a concurrency
limit, per-account error isolation (one 403 must not abort the sweep), and an
envelope shape that attributes each row to its account. Build it when something
concretely needs it, not on spec. The `stripeAccount` echo added in phase 13 is
the building block when that day comes.

## Out of scope

- Anything that writes, per the standing posture.
- Connect onboarding, `topup`, `country-spec`, v2 accounts — unchanged from
  phase 13.
- API-version overrides and raw output — see
  [`03-api-version-raw.md`](03-api-version-raw.md).

## Resolved decisions

- **`--expand-stripe` stays documented, not auto-corrected.** Phase 13 chose to
  document the `data.` prefix rule for list endpoints rather than silently
  rewrite the caller's input; Stripe's own 400 already names the correct path,
  and rewriting input hides the rule rather than teaching it. Revisit only if
  it keeps tripping people up.
- **Order of work:** phase 15 first (the correctness gap), then §1–§6 here
  batched into one PR when they are actually wanted, and §7 not at all until
  something needs it.
- **Split the version/raw work into its own phase.** It is an output-layer fix,
  not a read; keeping it at the head of this list buried the one item that
  matters behind six nice-to-haves.

## Log

- 2026-08-10 — Drafted from review findings on the phase 13 PR.
- 2026-08-10 — Reordered after establishing that three separate-looking
  findings shared one cause: output marshalled through the pinned SDK structs.
- 2026-08-10 — Spiked raw passthrough; downgraded it from "architectural" to a
  contained change, then split it out to `03-api-version-raw.md` so this file
  is purely the additive reads. No open questions remain here.
- 2026-08-10 — Implemented §1–§6 on `feat/connect-reads-phase-14`, one commit
  per item (§1 and §2 share a commit — both are the balance package, and the
  edits interleave in the same file). §7 deliberately untouched.

  Deviations and findings worth carrying forward:

  - **§1 is `balance transaction <id>`, not `balance transaction get <id>`.**
    Three-level nesting has no precedent here, and `transfer reversal` /
    `transfer reversals` already establishes singular-fetches-one,
    plural-lists. `balance transaction` / `balance transactions` is the same
    pair.
  - **§5: no expandable `coupon` on a promotion code.** On the pinned version
    (2026-04-22.dahlia) the coupon is inlined at `promotion.coupon` — there is
    no top-level field to expand. The first draft advertised
    `--expand-stripe coupon` and
    `TestExpandPathsResolveAgainstSDKStructs` rejected it, which is exactly the
    silent-null failure that test exists to catch. `promotion-code list
    --coupon <id>` still filters the other way.
  - **§5: `--active` is two flags, `--active` / `--inactive`.** Stripe's filter
    is tri-state and a single bool cannot express "unset" without the caller
    knowing to omit it; passing both is rejected locally.
  - Registry, `resource describe` entries, expand-path curation, README and
    SKILL.md all updated alongside. `main_test.go`'s command-count assertion
    and `TestEveryNetworkCommandDocumentsConnect` both caught omissions during
    the work — the guard tests are earning their keep.
