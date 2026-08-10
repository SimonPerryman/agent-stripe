---
name: agent-stripe
description: Read-only Stripe CLI for AI agents. Use for any read-only Stripe question — customers, charges, payment intents, refunds, disputes, balance, payouts, subscriptions, invoices, products, prices, events, and Connect (connected accounts, application fees, and reading a connected account's data via --stripe-account). This skill is read-only — it cannot create, modify, or delete anything in Stripe.
---

# agent-stripe

`agent-stripe` is a read-only CLI wrapper around the Stripe API, designed for AI agents. It is **read-only**: the HTTP transport rejects non-GET requests at the chokepoint in `internal/stripe/readonly.go`, so no command can write, even by accident. Every response is a JSON envelope with `mode`, `account`, `apiVersion`, and `data` — predictable shape, no prose.

## When to invoke

Reach for `agent-stripe` whenever the question is read-only Stripe:

- "what charges did customer X have this week"
- "why didn't this subscription renew"
- "find the payment for invoice in_…"
- "what's in our test-mode events feed since yesterday"
- "what fields are on a Stripe Subscription"
- "is this dispute still open"
- "why can't this connected account take payments / receive payouts"
- "where is the charge for merchant acct_… — I can't find it"

Skip it when the user wants to *do* something to Stripe (create, refund, cancel, etc.). This skill cannot help with writes — direct them to the Stripe Dashboard or the Stripe CLI.

## Command surface

```
account     add | remove | list | test | set-default     # local key management
customer    get | list | search                          # cus_…
charge      get | list | search                          # ch_…
payment-intent  get | list | search                      # pi_…
refund      get | list                                   # re_…
dispute     get | list                                   # dp_…
balance     get | transactions                           # ledger entries
payout      get | list                                   # po_…
event       get | list [--related <id>]                  # evt_…
subscription get | list | search                         # sub_…
invoice     get | list | search                          # in_…
product     get | list | search                          # prod_…
price       get | list | search                          # price_…
connected-account get | list | capabilities | persons | external-accounts   # acct_…
application-fee get | list | refunds                     # fee_…
resource    describe <name> [--depth N]                  # shape discovery, no API call
```

Global flags: `--account <alias>` · `--stripe-account <acct_…>` · `--live` · `--full` · `--expand <fields|paths>` · `--expand-stripe <paths>` · `--raw` · `--api-version <date>` · `--stream` · `--timeout <dur>`.

`agent-stripe <command> usage` is the source of truth for flags — don't trust this file for flag-level detail; read the CLI's own help.

## The read-only guarantee

Every Stripe API call goes through `internal/stripe/readonly.go`, which wraps `http.RoundTripper` and returns `ErrReadOnly` on any non-GET request before it leaves the process. There is no `--force` to bypass it. Commands that look like they could write (`account add`, `account remove`) only touch local keychain state, never Stripe.

This guarantee is the reason you should reach for `agent-stripe` early on Stripe questions: there's no detection-evasion or oops-I-deleted-a-customer risk.

## Common workflows

### "Why did this charge fail?"

```
agent-stripe charge get ch_… --full --expand-stripe customer
```

`outcome.seller_message` and `failure_message` are the high-signal fields. `--full` skips truncation so the messages aren't cut.

### Reconcile a charge to an invoice

```
agent-stripe charge get ch_…
# → invoice id in response
agent-stripe invoice get in_… --expand-stripe customer,subscription
```

### "What happened to this object?"

`event list --related <id>` is the audit trail for any Stripe object:

```
agent-stripe event list --related sub_… --limit 50
```

Under `--stream`, `event list --related` emits a final trailing line
`{"_truncated":bool,"scanned":N,"matched":M}` after the records — it's the
only command whose stream has more than `header + records`. Skip lines without
a top-level `id` to filter it out when parsing.

### Connect: which account does the object live on?

`agent-stripe` can read connected accounts. Two different flags, easily confused:

- `--account <alias>` — which **credential** to authenticate with (a local keychain alias).
- `--stripe-account acct_…` — whose **books** that credential reads (the `Stripe-Account` header).

The distinction that matters most:

| Flow | Object lives on | Flag |
|---|---|---|
| Direct charge | connected account | `--stripe-account acct_…` |
| Destination charge | platform, with `transfer_data.destination` | none |
| Separate charges and transfers | charge on platform, `tr_…` to the account, joined by `transfer_group` | none for the charge |

**Do not conclude an object doesn't exist because the platform can't see it.**
A direct charge is invisible from the platform account entirely. If a `ch_…`,
`pi_…`, or its events come back empty, try the connected account before
reporting "not found".

```
agent-stripe connected-account list --limit 10           # find the acct_ id
agent-stripe connected-account capabilities acct_…       # why it can't charge/pay out
agent-stripe --stripe-account acct_… balance get         # why no payout
agent-stripe --stripe-account acct_… charge get ch_…     # the direct charge itself
agent-stripe --stripe-account acct_… account test        # is the account reachable at all?
```

Responses read this way echo `"stripeAccount": "acct_…"` in the envelope, so you
can verify the scope of what you just read rather than re-deriving it.

`on_behalf_of` is **not** the same thing as `--stripe-account`: it's a field set
when the object was created, which this CLI only ever reads.

`connected-account list` and `application-fee` are platform-scoped by nature —
they enumerate *your* connected accounts and *your* revenue, so `--stripe-account`
does nothing there.

### Find customers by email

```
agent-stripe customer search --query 'email:"alice@example.com"'
```

Search is eventually consistent — there's a ~1-minute lag between creating an object and being able to search for it. Use `list` when strict consistency matters.

### Bulk export to NDJSON

```
agent-stripe charge list --created-gt 1735689600 --stream > charges.ndjson
```

`--stream` emits one header line then one record per line; pagination happens automatically until exhausted or `--limit` is hit. Pipes cleanly: `agent-stripe ... --stream | head -5` won't hang.

### Discover a resource's shape without spending an API call

```
agent-stripe resource describe subscription --depth 3
```

Returns the field tree (reflected from the SDK) and the curated `expandPaths` — useful before you decide what to `--expand-stripe`.

Caveat: `describe` reflects over the SDK's structs, so it shows one version's shape and only the fields that version models. If a field you expect is missing from a real response, that is the reason — see below.

### "A field I expect isn't in the output"

Responses are normally marshalled through SDK structs pinned to one Stripe API version. Any field that version does not model is dropped silently — no error, indistinguishable from Stripe not sending it. `--raw` emits Stripe's JSON instead:

```
agent-stripe invoice get in_… --raw
```

To see what a consumer on an older version receives — webhook endpoints pin their own version, often years behind — name it. `--api-version` implies `--raw`:

```
agent-stripe webhook-endpoint list                       # read each endpoint's api_version
agent-stripe --api-version 2022-11-15 invoice get in_…   # what that endpoint's payload looks like
```

The envelope's `apiVersion` reports the version actually requested, and raw responses carry `"raw": true`, so the two modes are never confusable in a transcript.

## Installation

```bash
brew install simonperryman/tap/agent-stripe   # once the tap is published
# or
go install github.com/simonperryman/agent-stripe/cmd/agent-stripe@latest
```

First-time setup:

```
agent-stripe account add default --form --default
```

`--form` opens an OS dialog so the secret key never enters the agent transcript. The key is stored in the OS keychain (macOS Keychain in v1).
