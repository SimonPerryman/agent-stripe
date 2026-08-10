# agent-stripe

Read-only Stripe CLI for AI agents. Structured JSON, multi-account, safe by default — non-`GET` requests are rejected at the HTTP transport, so an agent literally cannot write.

[![CI](https://github.com/simonperryman/agent-stripe/actions/workflows/ci.yml/badge.svg)](https://github.com/simonperryman/agent-stripe/actions/workflows/ci.yml)
[![Homebrew](https://img.shields.io/badge/homebrew-simonperryman%2Ftap-orange)](https://github.com/simonperryman/homebrew-tap)
[![Go](https://img.shields.io/badge/go-1.26%2B-00ADD8)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

Built on [stripe-go v85](https://github.com/stripe/stripe-go) · macOS & Linux

---

- [Quickstart](#quickstart)
- [Safety](#safety)
- [Examples](#examples)
  - [Debug a failed charge](#debug-a-failed-charge)
  - [Reconcile a customer complaint](#reconcile-a-customer-complaint)
  - [Trace a Connect payment across platform and connected account](#trace-a-connect-payment-across-platform-and-connected-account)
  - [Bulk export](#bulk-export)
  - [Discover fields without an API call](#discover-fields-without-an-api-call)
- [Resources](#resources)
- [Use with Claude Code](#use-with-claude-code)
- [Reference](#reference)
- [Not included](#not-included-by-design) · [License](#license)

## Quickstart

```sh
brew install simonperryman/tap/agent-stripe
agent-stripe account add default --form --default     # OS dialog → key in keychain
agent-stripe charge get ch_xxx --full
```

`--form` opens an OS dialog so the secret never enters the agent's transcript.

<details>
<summary>Install from source</summary>

```sh
go install github.com/simonperryman/agent-stripe/cmd/agent-stripe@latest
```
Requires Go 1.26+.
</details>

## Safety

- **Read-only at the HTTP boundary** — no `POST`/`DELETE` paths are importable from the client wrapper. Compile-time, not runtime.
- **Live-mode gate** — `sk_live_…` keys require an explicit `--live` flag per invocation (configurable via `account.requireLiveFlag`).
- **Mode echo** — every response carries `"mode": "test" | "live"` so agents can verify which environment they're reading.
- **Account echo** — reads scoped to a connected account (`--stripe-account acct_…`) carry `"stripeAccount": "acct_…"` in the envelope. A platform charge and a connected-account charge are otherwise indistinguishable in the output; the field is omitted entirely when reading the platform account.
- **Key redaction** — secrets stored in the OS keychain, redacted in all output and errors.
- **Bounded output** — long strings truncated by default with a `{field}Length` companion (opt out per-field with `--expand`, or globally with `--full`). Lists capped at `list.maxResults` (default 100); use `--stream` for more.

## Examples

### Debug a failed charge

What happened, why, and the settlement once it lands:

```sh
# timeline of events for this charge
agent-stripe event list --related ch_xxx

# outcome.seller_message + failure_message
agent-stripe charge get ch_xxx --full

# fees / settlement once available
agent-stripe balance transactions --type charge
```

### Reconcile a customer complaint

Trace customer → subscription → invoice → charge:

```sh
agent-stripe customer get cus_xxx
agent-stripe subscription list --customer cus_xxx
agent-stripe invoice list --subscription sub_xxx --status paid
agent-stripe invoice get in_xxx --expand-stripe charge
```

### Trace a Checkout Session through to settlement

The single most common reconciliation path:

```sh
# session → linked payment intent / subscription / setup intent
agent-stripe checkout-session get cs_xxx --expand-stripe payment_intent,line_items

# payment intent → underlying charge
agent-stripe payment-intent get pi_xxx --expand-stripe latest_charge

# charge → settlement (fees, net, available_on)
agent-stripe charge get ch_xxx --expand-stripe balance_transaction
```

### Which webhook endpoints fire on a given event?

```sh
# By event type
agent-stripe webhook-endpoint for-event charge.succeeded

# By event id (fetches the event first to read its type)
agent-stripe webhook-endpoint for-event evt_xxx
```

### Trace a Connect payment across platform and connected account

Where an object lives depends on the charge type, and getting that wrong is the
top failure mode: an agent looks for a direct charge from the platform, finds
nothing, and concludes the charge doesn't exist.

| Flow | Charge lives on | Flag needed |
|---|---|---|
| Direct charge | connected account | `--stripe-account acct_…` |
| Destination charge | platform (`transfer_data.destination` names the account) | none |
| Separate charges and transfers | charge on platform, `tr_…` to the account, joined by `transfer_group` | none for the charge |

Following the money end to end — note the flag appearing partway through:

```sh
# 1. platform: the charge and the transfer it funded
agent-stripe charge get ch_xxx --expand-stripe transfer
agent-stripe transfer list --destination acct_xxx --limit 5

# 2. is the account even able to receive money?
agent-stripe connected-account get acct_xxx
agent-stripe connected-account capabilities acct_xxx

# 3. connected account: the funds arriving, and the payout they land in
agent-stripe --stripe-account acct_xxx balance get
agent-stripe --stripe-account acct_xxx balance transactions --type payment
agent-stripe --stripe-account acct_xxx payout list --limit 5

# 4. platform: what we earned on it
agent-stripe application-fee list --charge ch_xxx
```

Fields that link the two views: `on_behalf_of`, `application_fee_amount`,
`transfer_data.destination`, `source_transaction`, `transfer_group`.
`on_behalf_of` is a field set at creation time — not the `Stripe-Account`
header, and not something this read-only CLI can set.

### Bulk export

Stream every charge since a timestamp, then process with `jq`:

```sh
agent-stripe charge list --created-gt 1735689600 --stream > charges.ndjson
jq -r 'select(.status == "failed") | .id' charges.ndjson
```

### Discover fields without an API call

Handy before writing an `--expand-stripe`:

```sh
agent-stripe resource describe subscription --depth 2
```

## Resources

Each resource supports `get` and `list`, plus `search` where Stripe does.

| Group        | Resources |
|--------------|-----------|
| Money flow   | `charge`, `payment-intent`, `payment-method`, `setup-intent`, `refund`, `dispute`, `payout`, `balance`, `transfer` |
| Checkout     | `checkout-session` |
| Customers    | `customer`, `subscription`, `subscription-item`, `subscription-schedule`, `invoice`, `invoice-item` |
| Catalog      | `product`, `price` |
| Connect      | `connected-account` (with `capabilities`, `persons`, `external-accounts`), `application-fee` (with `refunds`) |
| Audit        | `event` (with `--related <id>` to reconstruct an object's history) |
| Webhooks     | `webhook-endpoint` (with `for-event <evt_or_type>` to match enabled_events) |
| Meta         | `account`, `resource describe`, `usage` |

Sub-resources live as subcommands of their parent: `balance transactions`,
`setup-intent attempts <seti_id>`, `invoice lines <invoice_id>`,
`connected-account capabilities <acct_id>`, `application-fee refunds <fee_id>`.

Power features:

- **`--related <id>`** on `event list` — reconstruct what happened to any object over time. The core agent-debugging primitive.
- **`--expand-stripe`** — passthrough to Stripe's `expand[]` for nested resources in one round-trip.
- **`--stripe-account acct_…`** — read a connected account's books through the Stripe-Account header. Works on every command, because the header is injected once at the transport. `--account` picks which *credential*; `--stripe-account` picks *whose books* it reads.
- **`--stream`** — NDJSON for large lists/searches; paginates Stripe until exhausted. Pipes cleanly to `head`, `jq`, files.
- **`resource describe <name>`** — emits field/type tree (reflected from `stripe-go`) without an API call. Answers "what can I expand on a subscription?".
- **`agent-stripe usage`** and **`<command> usage`** — LLM-optimized docs at every level.

## Use with Claude Code

```sh
npx skills add simonperryman/agent-stripe
```

Installs the [`SKILL.md`](SKILL.md) via [skills.sh](https://skills.sh) so Claude Code (and other supported agents) reach for `agent-stripe` automatically on read-only Stripe questions. Add `-g` for a global install instead of project-level.

## Reference

### Test vs live mode

Stripe sandboxes are test mode (`sk_test_…` keys). Live mode requires an explicit flag so an agent can't accidentally hit production:

```sh
agent-stripe account add sandbox --form        # sk_test_… → mode: test, no flag needed
agent-stripe account add prod --form           # sk_live_… → mode: live, requires --live
agent-stripe --account prod --live charge list
```

### Errors

Errors go to stderr as a structured envelope. `fixableBy` tells an agent whether to retry, ask the human, or correct its own input:

```sh
$ agent-stripe charge get ch_doesnotexist
{"error":"No such charge: 'ch_doesnotexist'","fixableBy":"agent","stripeCode":"resource_missing","httpStatus":404,"requestId":"req_..."}
```

| Field | Purpose |
|---|---|
| `fixableBy` | `agent` (retry/correct input), `human` (ask the user), or `none` |
| `hint` | Closest-match suggestion when the failing value is from a closed set (command name, subcommand, account alias, resource type). Example: `did you mean "charge"?` |
| `stripeCode` | Stripe's machine-readable error code |
| `httpStatus` | HTTP status from Stripe |
| `requestId` | For grepping Stripe Dashboard logs |
| `error` | Human-readable message (last resort) |

Envelope shape: `{error, fixableBy, hint?, stripeCode?, httpStatus?, requestId?}`.

### Configuration

State lives in `~/.config/agent-stripe/` (or `$XDG_CONFIG_HOME`). Secrets in the OS keychain only.

### Development

```sh
make test                                       # unit tests, no network
STRIPE_TEST_KEY=sk_test_... make integration    # hits Stripe test mode
```

The Connect integration tests need a test-mode connected account. Create one in
the Dashboard (test mode → Connect → Accounts) and export its id; the tests skip
cleanly when it's absent, so a plain non-Connect test key still passes:

```sh
STRIPE_TEST_KEY=sk_test_... STRIPE_TEST_CONNECTED_ACCOUNT=acct_... make integration
```

## Not included (by design)

Writes (charges, refunds, mutations), webhook tunneling, card testing, Connect *onboarding* (`account_links`, `account_sessions` — all write flows). Use the [official Stripe CLI](https://github.com/stripe/stripe-cli) for those. Connect *reads* are supported: see [`--stripe-account`](#trace-a-connect-payment-across-platform-and-connected-account).

## License

MIT
