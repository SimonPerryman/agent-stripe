# agent-stripe

Read-only Stripe CLI for AI agents. Structured JSON output, multi-account, safe by default.

Modeled on [agent-mongo](https://github.com/shhac/agent-mongo) and [agent-dd](https://github.com/shhac/agent-dd). See [PLAN.md](PLAN.md) and [TECH_STACK.md](TECH_STACK.md) for design details.

## Status

🚧 Pre-release. Phases 1–4 are implemented: scaffolding + `account`, `customer`, `event`, Payments, Billing, search across the resources Stripe supports, `--stream` NDJSON output, and `resource describe`. Packaging + Homebrew tap publish is the remaining open item. See [plans/v1/](plans/v1/).

Legend: ✅ shipped · 🚧 in progress · ⏳ planned

## Features

### Account management ✅
Store API keys locally, switch between accounts, never echo secrets back to the agent.

| Command | Status | Notes |
|---|---|---|
| `account add <alias> [--key] [--form] [--default]` | ✅ | `--form` opens an OS dialog (macOS only in v1) so the key never enters the agent transcript |
| `account update <alias> [--key] [--form]` | ⏳ | |
| `account remove <alias>` | ✅ | |
| `account list` | ✅ | Keys redacted; shows `mode: test \| live` from key prefix |
| `account test [alias]` | ✅ | `GET /v1/account` — verifies key works |
| `account set-default <alias>` | ✅ | |

### Customers ✅
| `customer get <id>` ✅ · `customer list` ✅ · `customer search --query` ✅ |

### Payments ✅
| Resource | Commands | Status |
|---|---|---|
| `charge` | `get`, `list`, `search` | ✅ |
| `payment-intent` | `get`, `list`, `search` | ✅ |
| `refund` | `get`, `list` | ✅ |
| `dispute` | `get`, `list` | ✅ |
| `balance` | `get`, `transactions` | ✅ |
| `payout` | `get`, `list` | ✅ |

**Debugging a failed charge** — typical agent flow:

```sh
agent-stripe event list --related ch_xxx          # what happened, in order
agent-stripe charge get ch_xxx --full             # outcome.seller_message + failure_message
agent-stripe balance transactions --type charge   # fees / settlement once it lands
```

**`--expand-stripe`** — Stripe's `expand[]` passthrough. Useful when one resource references another and you'd otherwise need a second round-trip:

```sh
agent-stripe payment-intent get pi_xxx --expand-stripe latest_charge,customer
agent-stripe charge get ch_xxx --expand-stripe customer,balance_transaction
```

Paths aren't validated — typos return the un-expanded shape silently. See each command's `usage` for the recommended set.

### Billing ✅
| Resource | Commands | Status |
|---|---|---|
| `subscription` | `get`, `list`, `search` | ✅ |
| `invoice` | `get`, `list`, `search` | ✅ |
| `product` | `get`, `list`, `search` | ✅ |
| `price` | `get`, `list`, `search` | ✅ |

**Search vs list** — `list` is filter-on-fields with strict consistency; `search` is Stripe's [search query language](https://docs.stripe.com/search#search-query-language) with eventual consistency (~1 min lag) and an opaque `--page` token (not interchangeable with `list`'s `--starting-after`):

```sh
agent-stripe charge search --query 'amount>5000 AND status:"succeeded"' --limit 20
agent-stripe customer search --query 'email:"alice@example.com"'
```

Reconciling a customer complaint:

```
agent-stripe customer get cus_xxx
agent-stripe subscription list --customer cus_xxx
agent-stripe invoice list --subscription sub_xxx --status paid
agent-stripe invoice get in_xxx --expand-stripe charge
```

`invoice preview` (formerly `upcoming`) is deferred — see plans/v1/03-billing.md §2.

### Events 🚧
| `event list [--type] [--created-gt] [--created-lt] [--related <id>]` ✅ · `event get <id>` ⏳ |

`--related <id>` is the agent's core debugging tool: client-side filter over recent events
matching `data.object.id`. Response includes a `scan` envelope (`scanned`, `matched`, `truncated`).

The core debugging tool — lets an agent reconstruct what happened to an object over time.

### Config ⏳
| `config get/set/reset/list-keys` | ⏳ |

### Discoverability ✅
- `agent-stripe usage` — top-level LLM-optimized docs
- `<command> usage` — per-command docs with examples
- `resource describe <name> [--depth N]` — emits the field/type tree (reflected from `stripe-go`) plus the curated `expandPaths` list. **No API call.** Useful for "what can I expand on a subscription?" or "what fields exist on a charge?" without spending a request.

### Output ✅
- JSON to stdout; errors as `{ "error": "...", "fixableBy": "human" | "retry" | "agent" }` to stderr
- Long strings truncated with `{field}Length` companion; `--full` skips truncation entirely, or `--expand <leaf|dotted.path>` opts out per-field
- `--expand-stripe <fields>` — passthrough to Stripe's `expand[]` for nested resources
- `--stream` — NDJSON for large lists/searches; one header line then one record per line, paginates Stripe until exhausted or `--limit` hit
- Every response tagged with `mode: test | live`

**Streaming** — `--stream` works with both `list` and `search`. Pipes cleanly to `head`, redirects to a file, and exits 0 on broken pipe:

```sh
agent-stripe charge list --created-gt 1735689600 --stream | head -5
agent-stripe invoice list --stream > invoices.ndjson
agent-stripe customer search --query 'created>1735689600' --stream | jq -r '.id'
```

Under `--stream`, `--limit` becomes a hard stop (default is unbounded — paginate everything); `--starting-after` (list) and `--page` (search) act as resume points.

### Safety ⏳
- **Read-only at the HTTP boundary** — no `POST`/`DELETE` paths importable from the Stripe client wrapper
- **Live-mode gating** — calls against `sk_live_` accounts require `--live` flag (or disable via `account.requireLiveFlag` config)
- **Bounded results** — `list.maxResults` default 100; use `--stream` to go further
- **Redaction** — keys never appear in `list`, errors, or logs

## Non-goals (v1)

- Write operations (charges, refunds, customer mutations, subscription edits) — explicit out-of-scope
- Webhook receiving / event tunneling (the official Stripe CLI covers this well)
- Card testing / payment simulation
- Connect onboarding flows

Phase 5 may add scoped writes (refunds, subscription cancel) behind `--confirm` if there's real demand.

## Tests

Unit tests run with `make test` and mock Stripe at the HTTP layer (no network).

Integration tests hit Stripe **test mode** with a dedicated key. Grab a test key from the [Stripe dashboard](https://dashboard.stripe.com/test/apikeys) (the one prefixed `sk_test_…`), then:

```sh
export STRIPE_TEST_KEY=sk_test_...
make integration
```

Without `STRIPE_TEST_KEY` set, the suite skips cleanly.

## Install

Homebrew (once the tap is published — release config in `.goreleaser.yml`):

```sh
brew install simonperryman/tap/agent-stripe
```

From source:

```sh
go install github.com/simonperryman/agent-stripe/cmd/agent-stripe@latest
```

Claude Code skill: see [SKILL.md](SKILL.md) at the repo root. Distribution via `npx skills add simonperryman/agent-stripe` is planned.

First-time setup (regardless of install method):

```sh
agent-stripe account add default --form --default
```

`--form` opens an OS dialog so the secret key never enters the agent transcript. The key is stored in the OS keychain (macOS Keychain in v1).
