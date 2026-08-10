# Phase 15 — API version override and raw output

Status: done

## Goal

Close the one correctness gap left by phase 13: fields that exist on the wire
but are invisible to this CLI.

Output is marshalled through the pinned SDK's response structs. Any field that
version does not model is dropped, regardless of what Stripe returns. There is
no error and no warning — the field is simply absent, indistinguishable from
"Stripe didn't send it".

Three findings from the phase 13 review turned out to be this one cause:

1. `invoice.application_fee_amount` is unreadable.
2. Advertised expand paths were accepted by Stripe and silently discarded,
   yielding `null` with no error.
3. There is no way to see what a webhook consumer on an older API version
   actually receives.

**Measured.** The same invoice, requested twice — 13 fields present at
`2022-11-15` are absent at the pinned `2026-04-22.dahlia`:

    application_fee_amount, charge, discount, paid, paid_out_of_band,
    payment_intent, quote, rendering_options, subscription,
    subscription_details, tax, total_tax_amounts, transfer_data

That is not a niche set. `charge`, `payment_intent` and `subscription` are the
fields you would use to link an invoice to a payment.

Stripe's Basil release moved Invoice's flat linkage fields under `parent` and
dropped `application_fee_amount` outright. Webhook endpoints pin their own
version independently — confirmed live, endpoints on one account sit at
`2020-08-27` and `2022-11-15` while this CLI reports `2026-04-22.dahlia`. So an
agent cannot verify a value against the payload its own consumers receive, and
"the CLI shows no application fee" is not evidence that there isn't one.

## Plan

### 1. `--raw` — emit Stripe's JSON, not the struct

The load-bearing half. `--api-version` without this fixes nothing.

A spike (2026-08-10) confirmed the existing pipeline needs no changes:
`output.Render` already has a fast path for `map[string]any`, so unmarshalling
the response body into a map instead of a struct feeds truncation, pruning,
`--expand` and the `--stream` record path unmodified. `stripe.APIResponse`
exposes `RawJSON []byte` on every response and the SDK ships a `RawRequest`
backend, so the body is reachable without new HTTP plumbing.

`--raw` is worth having on its own, independent of any version override: the
struct path drops fields *at the pinned version too*. That is what caused the
`external-accounts` payload loss and the silently-discarded expand paths in
phase 13.

### 2. `--api-version` — set the request version

Mirror `--stripe-account` exactly: a global flag, `AGENT_STRIPE_API_VERSION`
env fallback, no config-file default, injected as a `Stripe-Version` header at
the same transport chokepoint (`internal/stripe/`). Implies `--raw`, because
shipping it without raw output would look like it worked while dropping the
same fields.

Validate the value's shape locally (`YYYY-MM-DD` with an optional `.name`
suffix) so a typo fails with an agent-fixable hint rather than an opaque error.

### 3. Envelope: report the *effective* version

`apiVersion` currently echoes the pinned constant
(`agentstripe.PinnedAPIVersion`). Under an override it must report what was
actually requested, or the echo becomes a lie — the same reasoning that put
`stripeAccount` in the envelope in phase 13. Consider a `raw: true` marker too,
since raw output is a different contract (no pruning guarantees about which
fields the SDK would have modelled).

### 4. Raw sibling for the pagination helpers — ~~needed~~ not needed

**Dropped during implementation.** The premise was wrong: stripe-go's list and
search iterators re-split each page's `data` array and hand every item its own
recorded body (`maybeAddLastResponseV1`, `iter.go`). So raw mode still drains
the typed `*V1List[T]` — `has_more` comes off `list.Meta()` as before and the
cursor `id` reads the same off either map. `CollectRawList` / `StreamRawList`
took a `raw bool` and nothing else changed. There is no raw pagination path.

### 5. `resource describe` — say what it cannot do

It reflects over the pinned structs and therefore cannot describe another
version's shape. State that in its usage rather than letting the output imply
otherwise.

### 6. Tests

- Unit: `--api-version` flag/env precedence and shape validation; envelope
  reports the effective version; `--raw` emits a field the pinned struct cannot
  hold (fixture-driven, no network).
- Unit: raw pagination advances the cursor correctly off a map.
- Integration: request one object at the pinned version and at an older one,
  assert the older response carries fields the pinned one does not. This is the
  only test that actually proves the feature; it needs no special account, just
  `STRIPE_TEST_KEY`.

## Out of scope

- Writes, per the standing posture.
- Making the SDK structs version-aware. Raw output is the escape hatch; the
  typed path stays pinned.
- Auto-detecting which version to use. The caller names it.

## Resolved decisions

- **`--raw` is its own flag; `--api-version` implies it.** Raw output has
  standalone value because the struct path drops fields at the pinned version
  too.
- **No config-file default for `--api-version`**, same as `--stripe-account`: a
  saved version would silently change the shape of every response, and the
  envelope echo is what makes an ambient default tolerable.
- **Split out of `02-connect-reads.md`.** That plan collects additive reads;
  this is an output-layer correctness fix with a different shape and a
  different priority. Mixing them buried the one item that matters.

## Log

- 2026-08-10 — Split from `02-connect-reads.md` §1. Spike findings, the
  13-field measurement and the resolved decisions carried over intact.
- 2026-08-10 — Implemented. Deviations from the plan as written, all
  deliberate:

  - **§4 dropped entirely** (see above) — the SDK already records a per-item
    response body, so raw mode needs no pagination plumbing at all. This was
    the plan's "one piece of real plumbing"; it turned out not to exist.
  - **The mode choice moved off the call sites.** `ToRawMap` grew a `raw`
    parameter, but the 26 `get` commands no longer call it: they hand the SDK
    value to `cli.EmitSingle`, which decides. A new command physically cannot
    forget `--raw` — the same reasoning that put envelope construction behind
    `cli.EnvelopeFor`.
  - **Raw on a value with no recorded body is an error, not a silent
    fallback.** Returning the struct-marshalled map would hand back output
    that looks raw and is not, which is the failure this whole phase exists to
    close. The one place it would have bitten — `connected-account
    external-accounts`, whose items are projected off the parent account —
    now sources them from the parent's raw body instead. That is the
    external-accounts payload loss from phase 13, fixed rather than
    side-stepped.
  - **§5 went further than "say what it cannot do".** `resource describe`
    *rejects* `--api-version` (`cli.RejectAPIVersion`, mirroring
    `RejectStripeAccount`) rather than answering with a pinned tree under an
    envelope echoing the requested version. A silent wrong answer is worse
    than an error, and this is the same shape as the platform-scoped guard.
    Its usage now also states that the tree is narrower than the wire.
  - **Global flag definitions extracted** into `newGlobalFlags`. The tests
    had been building a hand-kept mirror of the flag set, which was already
    missing flags; adding two more would have widened the drift.

  Verified: `gofmt -l`, `go vet`, `go build`, `go test -race`,
  `golangci-lint` all clean; integration tests typecheck under
  `-tags=integration`. The live `--api-version` comparison in
  `invoice_version_integration_test.go` has not been run against a real
  account — it needs `STRIPE_TEST_KEY` and is the only test that proves the
  header reaches the wire.
