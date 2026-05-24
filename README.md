# agent-stripe

Read-only Stripe CLI for AI agents. Structured JSON output, multi-account, safe by default.

Modeled on [agent-mongo](https://github.com/shhac/agent-mongo) and [agent-dd](https://github.com/shhac/agent-dd). See [PLAN.md](PLAN.md) and [TECH_STACK.md](TECH_STACK.md) for design details.

## Status

🚧 Pre-release. Phases 1–2 (scaffolding + `account`, `customer`, `event`, and the Payments resources) are implemented; Phases 3–4 are still planned. See [plans/v1/](plans/v1/).

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

### Customers 🚧
| `customer get <id>` ✅ · `customer list` ✅ · `customer search --query` ⏳ |

### Payments 🚧
| Resource | Commands | Status |
|---|---|---|
| `charge` | `get`, `list` ✅ · `search` ⏳ | 🚧 |
| `payment-intent` | `get`, `list` ✅ · `search` ⏳ | 🚧 |
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

### Billing ⏳
| Resource | Commands | Status |
|---|---|---|
| `subscription` | `get`, `list` | ⏳ |
| `invoice` | `get`, `list` | ⏳ |
| `product` | `get`, `list` | ⏳ |
| `price` | `get`, `list` | ⏳ |

### Events 🚧
| `event list [--type] [--created-gt] [--created-lt] [--related <id>]` ✅ · `event get <id>` ⏳ |

`--related <id>` is the agent's core debugging tool: client-side filter over recent events
matching `data.object.id`. Response includes a `scan` envelope (`scanned`, `matched`, `truncated`).

The core debugging tool — lets an agent reconstruct what happened to an object over time.

### Config ⏳
| `config get/set/reset/list-keys` | ⏳ |

### Discoverability ⏳
- `agent-stripe usage` — top-level LLM-optimized docs
- `<command> usage` — per-command docs with examples
- `resource describe <name>` — prints the field/type tree for a Stripe resource (reflected from `stripe-go`), so agents can learn a shape without burning an API call

### Output ⏳
- JSON to stdout; errors as `{ "error": "...", "fixableBy": "human" | "retry" | "agent" }` to stderr
- Long strings truncated with `{field}Length` companion; `--full` or `--expand <fields>` to opt out
- `--expand-stripe <fields>` — passthrough to Stripe's `expand[]` for nested resources
- `--stream` — NDJSON for large lists
- Every response tagged with `mode: test | live`

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

⏳ Planned distribution:
```sh
brew install shhac/tap/agent-stripe
npx skills add shhac/agent-stripe   # Claude Code skill
```
