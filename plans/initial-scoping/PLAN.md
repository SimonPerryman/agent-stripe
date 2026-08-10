# agent-stripe

Read-only Stripe CLI for AI agents. Modeled on [agent-mongo](https://github.com/shhac/agent-mongo) and [agent-dd](https://github.com/shhac/agent-dd).

## Goals

- **LLM-friendly**: structured JSON to stdout, errors as `{ "error": "..." }` to stderr, non-zero exit on failure
- **Read-only by default**: no charges, refunds, customer mutations, or subscription edits — agents explore; humans act
- **Multi-account**: switch between Stripe accounts (test/live, multiple businesses) without juggling env vars
- **Safe credentials**: API keys stored in the OS keychain, never on disk in plaintext, never echoed back to the agent
- **Zero runtime deps**: single static Go binary (~5–15MB), cross-compiled per OS/arch
- **Discoverable**: `agent-stripe usage` and `<command> usage` give LLM-optimized docs

## Non-goals (v1)

- Write operations (creating charges, refunds, customers, etc.) — explicit out-of-scope for safety
- Webhook receiving / event tunneling (Stripe CLI already covers this well)
- Card testing, payment simulation
- Connect account onboarding flows

## CLI surface (initial sketch)

```text
agent-stripe [-a <account>] [--live] [--full] [--expand <fields>] [--timeout <ms>]
├── account
│   ├── add <alias> [--key <sk_...>] [--form] [--default]
│   ├── update <alias> [--key <sk_...>] [--form]
│   ├── remove <alias>
│   ├── list                              # keys redacted
│   ├── test [alias]                      # GET /v1/account
│   ├── set-default <alias>
│   └── usage
├── customer
│   ├── get <id>
│   ├── list [--email] [--created] [--limit] [--starting-after]
│   ├── search --query <stripe-search-query>
│   └── usage
├── charge
│   ├── get <id>
│   ├── list [--customer] [--created] [--limit]
│   ├── search --query <q>
│   └── usage
├── payment-intent
│   ├── get <id>
│   ├── list [--customer] [--created]
│   ├── search --query <q>
│   └── usage
├── refund
│   ├── get <id>
│   ├── list [--charge] [--payment-intent] [--created]
│   └── usage
├── subscription
│   ├── get <id>
│   ├── list [--customer] [--status] [--price]
│   └── usage
├── invoice
│   ├── get <id>
│   ├── list [--customer] [--status] [--subscription]
│   └── usage
├── dispute
│   ├── get <id>
│   ├── list [--charge] [--created]
│   └── usage
├── event
│   ├── get <id>
│   ├── list [--type] [--created] [--object-id]    # core debugging tool
│   └── usage
├── product
│   ├── get <id>
│   ├── list [--active]
│   └── usage
├── price
│   ├── get <id>
│   ├── list [--product] [--active] [--currency]
│   └── usage
├── balance
│   ├── get                                # current balance
│   ├── transactions [--type] [--created]
│   └── usage
├── payout
│   ├── get <id>
│   ├── list [--status] [--created]
│   └── usage
├── config
│   ├── get <key>
│   ├── set <key> <value>
│   ├── reset
│   ├── list-keys
│   └── usage
└── usage
```

## Safety model

- **Read-only enforcement**: route through a single `stripeGet` / `stripeList` / `stripeSearch` helper. No `POST` / `DELETE` paths exposed.
- **Live mode visibility**: account `list` shows `mode: "test" | "live"` derived from key prefix (`sk_test_` vs `sk_live_`). Output also tags every response with the mode used.
- **Live mode confirmation**: any command against a `live` account either requires `--live` flag explicitly OR `account.requireLiveFlag` config (default true). Prevents accidental prod inspection in agent transcripts that leak data.
- **Redaction**: API keys live in the OS keychain; the config file only holds `{alias, mode, keychain_ref}`. `account list` literally cannot print the key because it isn't on disk. Use `--form` for OS-dialog entry (osascript / zenity / PowerShell) so agents driving the CLI never see the secret.
- **No `$out`-equivalent**: search/list results are bounded by `list.maxResults` (default 100), with `--stream` for NDJSON when more is needed.

## Output

- All output: JSON to stdout, wrapped in a consistent envelope (see below)
- Errors: `{ "error": "...", "fixableBy": "human" | "retry" | "agent" }` to stderr, non-zero exit
- Long string truncation with `{field}Length` companion (same as agent-mongo)
- `--full` / `--expand <fields>` to opt back in
- Stripe `expand[]` parameter passthrough for nested resources (e.g. `--expand-stripe customer,latest_charge`)

### Response envelope

Every successful command response wraps the Stripe payload in a meta envelope so agents always know which account/mode produced it:

```json
{
  "mode": "test",
  "account": "myco-test",
  "apiVersion": "2026-04-22.dahlia",
  "data": { ... }            // resource object, array, or paginated list
}
```

- `data` holds the actual resource — object for `get`, array for `list`/`search`.
- `list` and `search` add a `page` sibling: `{ "hasMore": bool, "nextCursor": "obj_xxx" | null, "count": N }`.
- `event list --related <id>` adds a `scan` sibling: `{ "scanned": N, "matched": M, "truncated": bool }`.
- Under `--stream` (NDJSON), the envelope is emitted once as the first line (`{"mode":...,"stream":true}`), then one `data` object per line — keeps per-record overhead minimal while preserving the mode tag.

## Architecture (mirror agent-mongo)

See [TECH_STACK.md](TECH_STACK.md) for the full project layout. Summary:

```
cmd/agent-stripe/        # main entrypoint
internal/
├── cli/                 # arg parsing, dispatch, root command
├── commands/{account,customer,charge,...}
├── stripe/              # client.go, readonly.go, pagination.go
├── config/              # store.go, schema.go (~/.config/agent-stripe/config.json)
├── output/              # json.go (truncation/expand), errors.go
└── usage/               # LLM-friendly docs per command
```

Resolution order for active account: `-a` flag > `AGENT_STRIPE_ACCOUNT` env > config default.

## Tech choices

See [TECH_STACK.md](TECH_STACK.md) for the source of truth. Highlights:

- **Language**: Go — single static binary, fast cold start for repeated agent invocations
- **SDK**: `github.com/stripe/stripe-go/v85` — pins Stripe API version `2026-04-22.dahlia`
- **CLI**: `cobra` (or stdlib `flag` if the subcommand tree stays small)
- **Distribution**: Homebrew tap (`simonperryman/tap/agent-stripe`) + `npx skills add simonperryman/agent-stripe` for the Claude Code skill
- **Tests**: stdlib `testing` + `net/http/httptest` with mocked Stripe responses; recorded fixtures for integration

## Resolved design decisions

1. **Search vs list**: keep separate. Stripe Search has different syntax and is eventually consistent (~1 min lag); list is immediately consistent with structured filters. Document the tradeoff in `usage`.
2. **Connect accounts (`--on-behalf-of`)**: defer to v2. Not designing before someone asks. — *Superseded 2026-08-10: picked up as part of v3. See [`plans/v3/01-connect.md`](../v3/01-connect.md). Note the mechanism is the `Stripe-Account` header, not `on_behalf_of`, which is a write-time field this CLI only reads.*
3. **Event history by related object**: `event list --related <object_id>` filters the events log client-side by `data.object.id` to reconstruct what happened to an object. Pure GET, hard-capped at 500 events scanned (configurable via `list.maxEventScan`). Output includes `{ scanned, matched, truncated }` so the agent knows when to narrow the window. **Not** webhook resending — that's a write and stays out-of-scope.
4. **Schema discovery (`resource describe <name>`)**: ship it. Reflect over `stripe-go` structs and emit a simplified field/type tree so agents can learn a resource's shape without spending a live API call.
5. **Credential storage**: OS keychain via [`zalando/go-keyring`](https://github.com/zalando/go-keyring) (macOS Keychain / Linux Secret Service / Windows Credential Manager). Config file holds only `{alias, mode, keychain_ref}`. Secret never touches disk in plaintext.
6. **CLI library**: stdlib `flag` + a small dispatcher. Cobra's weight isn't justified for a two-level subcommand tree with simple positional + flag parsing.
7. **Config location**: `os.UserConfigDir()` — picks the right path per OS (`~/Library/Application Support/agent-stripe/` on macOS, `~/.config/agent-stripe/` on Linux, `%AppData%\agent-stripe\` on Windows).
8. **Stripe SDK & API version**: pin `stripe-go/v85` (currently maps to Stripe API version `2026-04-22.dahlia`) and set `stripe.APIVersion` explicitly at client init. Upgrade policy: one PR per Stripe version bump, fixtures regenerated, CHANGELOG entry. `account test` prints the pinned API version so agents see what they're talking to.
9. **`--stream` NDJSON**: one JSON object per line, flush per record, clean SIGPIPE handling (so `| head` works). Pagination uses Stripe's `starting_after`. On Ctrl-C, exit non-zero with `{"error": "interrupted"}` to stderr.
10. **Test fixtures**: two layers. (a) Unit tests use hand-authored JSON in `testdata/` covering pagination, errors, and redaction edge cases. (b) Integration tests run against Stripe **test mode** with a dedicated key, gated by `STRIPE_TEST_KEY` env var, skipped by default. No VCR/recording library — fixtures stay readable.
11. **Skill packaging**: ship a `SKILL.md` mirroring agent-mongo's structure — when to invoke, the exact command surface, and the read-only guarantee — so Claude Code can route Stripe questions to the CLI.

## Phasing

- **Phase 1** — Scaffolding: Go module setup, account/credential management (keychain-backed), `account test`, `customer get/list`, `event list`. Get the read-only pattern and JSON output rock-solid.
- **Phase 2** — Payments coverage: charge, payment-intent, refund, dispute, balance, payout.
- **Phase 3** — Billing: subscription, invoice, product, price.
- **Phase 4** — Polish: search across all resources, `--stream`, schema/describe command, skills package, brew tap.
- **Phase 5** (maybe): scoped write ops behind explicit `--confirm` flags (refunds, subscription cancellation) — only if there's real demand.
