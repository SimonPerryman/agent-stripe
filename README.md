# agent-stripe

Read-only Stripe CLI for AI agents. Structured JSON output, multi-account, safe by default.

Modeled on [agent-mongo](https://github.com/shhac/agent-mongo) and [agent-dd](https://github.com/shhac/agent-dd). See [PLAN.md](PLAN.md) and [TECH_STACK.md](TECH_STACK.md) for design details.

## Status

🚧 Pre-release. Phase 1 (scaffolding + `account`, `customer`, `event` commands) is implemented; Phases 2–4 are still planned. See [plans/v1/](plans/v1/).

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

### Payments ⏳
| Resource | Commands | Status |
|---|---|---|
| `charge` | `get`, `list`, `search` | ⏳ |
| `payment-intent` | `get`, `list`, `search` | ⏳ |
| `refund` | `get`, `list` | ⏳ |
| `dispute` | `get`, `list` | ⏳ |
| `balance` | `get`, `transactions` | ⏳ |
| `payout` | `get`, `list` | ⏳ |

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

## Install

⏳ Planned distribution:
```sh
brew install shhac/tap/agent-stripe
npx skills add shhac/agent-stripe   # Claude Code skill
```
