# agent-stripe

Read-only Stripe CLI for AI agents. Structured JSON, multi-account, safe by default.

Built on [stripe-go v85](https://github.com/stripe/stripe-go) · Go 1.26+ · macOS & Linux

## Why

Agents need to *read* Stripe — debug a failed charge, reconcile an invoice, trace an event — without risk of writing. `agent-stripe` enforces read-only at the HTTP boundary (non-GET requests are rejected at the transport layer), redacts keys, and returns predictable JSON envelopes that an LLM can parse without prose-stripping.

## Install

**Homebrew** (macOS / Linux):

```sh
brew install simonperryman/tap/agent-stripe
```

**From source** (Go 1.26+):

```sh
go install github.com/simonperryman/agent-stripe/cmd/agent-stripe@latest
```

**First-time setup** — add a Stripe key:

```sh
agent-stripe account add default --form --default
```

`--form` opens an OS dialog so the secret never enters the agent's transcript. Keys live in the OS keychain.

## Use with Claude Code

Install the skill so Claude reaches for `agent-stripe` automatically on read-only Stripe questions:

```sh
npx skills add simonperryman/agent-stripe   # planned
```

Or copy [`SKILL.md`](SKILL.md) into your project's `.claude/skills/` directory.

## Examples

**Debug a failed charge** — what happened, why, and the settlement once it lands:

```sh
# timeline of events for this charge
agent-stripe event list --related ch_xxx

# outcome.seller_message + failure_message
agent-stripe charge get ch_xxx --full

# fees / settlement once available
agent-stripe balance transactions --type charge
```

**Reconcile a customer complaint** — trace customer → subscription → invoice → charge:

```sh
agent-stripe customer get cus_xxx
agent-stripe subscription list --customer cus_xxx
agent-stripe invoice list --subscription sub_xxx --status paid
agent-stripe invoice get in_xxx --expand-stripe charge
```

**Bulk export** — stream every charge since a timestamp, then process with `jq`:

```sh
agent-stripe charge list --created-gt 1735689600 --stream > charges.ndjson
jq -r 'select(.status == "failed") | .id' charges.ndjson
```

**Discover fields without an API call** — handy before writing an `--expand-stripe`:

```sh
agent-stripe resource describe subscription --depth 2
```

**Test vs live mode** — Stripe sandboxes are test mode (`sk_test_…` keys). Live mode requires an explicit flag so an agent can't accidentally hit production:

```sh
agent-stripe account add sandbox --form           # sk_test_… → mode: test, no flag needed
agent-stripe account add prod --form              # sk_live_… → mode: live, requires --live
agent-stripe -a prod --live charge list
```

Every response carries `"mode": "test" | "live"` so an agent can verify which environment it's reading.

**Errors** — go to stderr as a structured envelope. `fixableBy` tells an agent whether to retry, ask the human, or correct its own input:

```sh
$ agent-stripe charge get ch_doesnotexist
{"error":"No such charge: 'ch_doesnotexist'","fixableBy":"agent","stripeCode":"resource_missing","httpStatus":404,"requestId":"req_..."}
```

## Features

- **Accounts** — `account add | list | test | set-default | remove`. Keys stored in OS keychain, redacted in all output.
- **Resources** — `customer`, `charge`, `payment-intent`, `refund`, `dispute`, `balance`, `payout`, `subscription`, `invoice`, `product`, `price`, `event`. Each supports `get` / `list`, plus `search` where Stripe does.
- **Events with `--related <id>`** — reconstruct what happened to any object over time. The core agent debugging primitive.
- **`--expand-stripe`** — passthrough to Stripe's `expand[]` for nested resources in one round-trip.
- **`--stream`** — NDJSON for large lists/searches; paginates Stripe until exhausted. Pipes cleanly to `head`, `jq`, files.
- **`resource describe <name>`** — emits field/type tree (reflected from `stripe-go`) without an API call. Answers "what can I expand on a subscription?".
- **Discoverability** — `agent-stripe usage` and `<command> usage` for LLM-optimized docs.

### Safety guarantees

- Read-only at the HTTP boundary — no `POST`/`DELETE` paths importable from the client wrapper
- Live-mode calls require `--live` flag (configurable via `account.requireLiveFlag`)
- Long strings truncated by default with `{field}Length` companion; opt out per-field with `--expand`, or globally with `--full`
- Bounded results (`list.maxResults`, default 100); `--stream` to go further

### Not included (by design)

Writes (charges, refunds, mutations), webhook tunneling, card testing, Connect onboarding. Use the [official Stripe CLI](https://github.com/stripe/stripe-cli) for those.

## Configuration

State lives in `~/.config/agent-stripe/` (or `$XDG_CONFIG_HOME`). Secrets in the OS keychain only.

## Development

```sh
make test                              # unit tests, no network
STRIPE_TEST_KEY=sk_test_... make integration   # hits Stripe test mode
```

## License

MIT
