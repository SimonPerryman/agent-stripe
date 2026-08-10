# Phase 15 — API version override and raw output

Status: draft

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

### 4. Raw sibling for the pagination helpers

`CollectRawList` / `StreamRawList` drain a typed `*V1List[T]`. Raw mode has no
typed list, so cursor handling (`has_more`, last `id`) needs to read off the
decoded map. Contained, but it is the one piece of real plumbing here.

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
