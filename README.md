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
| Money flow   | `charge`, `payment-intent`, `refund`, `dispute`, `payout`, `balance` |
| Customers    | `customer`, `subscription`, `invoice` |
| Catalog      | `product`, `price` |
| Audit        | `event` (with `--related <id>` to reconstruct an object's history) |
| Meta         | `account`, `resource describe`, `usage` |

Power features:

- **`--related <id>`** on `event list` — reconstruct what happened to any object over time. The core agent-debugging primitive.
- **`--expand-stripe`** — passthrough to Stripe's `expand[]` for nested resources in one round-trip.
- **`--stream`** — NDJSON for large lists/searches; paginates Stripe until exhausted. Pipes cleanly to `head`, `jq`, files.
- **`resource describe <name>`** — emits field/type tree (reflected from `stripe-go`) without an API call. Answers "what can I expand on a subscription?".
- **`agent-stripe usage`** and **`<command> usage`** — LLM-optimized docs at every level.

## Use with Claude Code

Copy [`SKILL.md`](SKILL.md) into your project's `.claude/skills/` directory and Claude will reach for `agent-stripe` automatically on read-only Stripe questions.

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
| `stripeCode` | Stripe's machine-readable error code |
| `httpStatus` | HTTP status from Stripe |
| `requestId` | For grepping Stripe Dashboard logs |
| `error` | Human-readable message (last resort) |

### Configuration

State lives in `~/.config/agent-stripe/` (or `$XDG_CONFIG_HOME`). Secrets in the OS keychain only.

### Development

```sh
make test                                       # unit tests, no network
STRIPE_TEST_KEY=sk_test_... make integration    # hits Stripe test mode
```

## Not included (by design)

Writes (charges, refunds, mutations), webhook tunneling, card testing, Connect onboarding. Use the [official Stripe CLI](https://github.com/stripe/stripe-cli) for those.

## License

MIT
