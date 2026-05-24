---
name: agent-stripe
description: Read-only Stripe CLI for AI agents. Use for any read-only Stripe question — customers, charges, payment intents, refunds, disputes, balance, payouts, subscriptions, invoices, products, prices, events. This skill is read-only — it cannot create, modify, or delete anything in Stripe.
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
resource    describe <name> [--depth N]                  # shape discovery, no API call
```

Global flags: `-a <alias>` · `--live` · `--full` · `--expand <fields|paths>` · `--expand-stripe <paths>` · `--stream` · `--timeout <dur>`.

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
