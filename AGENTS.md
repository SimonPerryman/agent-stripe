# agent-stripe

Read-only Stripe CLI for AI agents. Go, compiled to a single static binary. Modeled on [agent-mongo](https://github.com/shhac/agent-mongo) and [agent-dd](https://github.com/shhac/agent-dd).

## Principles

- **Read-only by design** — the CLI exposes only `GET` / `list` / `search` against Stripe. One chokepoint (`internal/stripe/readonly.go`) rejects any non-read method. Adding a write operation is a deliberate, reviewed change. Agents explore; humans act.
- **Live mode is loud** — every response is tagged with `mode: "test" | "live"` derived from the API key prefix. Commands against a live account require an explicit `--live` flag (config: `account.requireLiveFlag`, default `true`). Prevents accidental prod inspection from leaking PII into agent transcripts.
- **Secrets never round-trip the agent** — API keys live in the OS keychain (Keychain / Secret Service / Credential Manager) via [`zalando/go-keyring`](https://github.com/zalando/go-keyring). The config file only holds `{alias, mode, keychain_ref}`. The `--form` flag prompts via OS-native dialog (osascript / zenity / PowerShell) so the agent driving the CLI never sees the secret on its command line or in its scrollback.
- **JSON or nothing** — all output is JSON to stdout, errors are `{ "error": "...", "fixableBy": "human" | "retry" | "agent" }` to stderr with non-zero exit. No human-friendly modes, no colourised tables, no interactive prompts in the data path.
- **Config over environment detection** ([12-factor](https://12factor.net/config)) — branch on explicit env vars (`AGENT_STRIPE_ACCOUNT`, `AGENT_STRIPE_TIMEOUT`), never on a `prod`/`dev` label. Same code path everywhere; only config changes.
- **LLM-discoverable** — `agent-stripe usage` and `<command> usage` print concise, agent-friendly docs; `agent-stripe resource describe <name>` reflects over the SDK structs to emit a field tree without spending an API call. Help text is part of the product, not an afterthought.

## Architecture

```text
cmd/agent-stripe/            # main entrypoint
internal/
├── cli/                     # arg parsing, dispatch, root command
├── commands/
│   ├── account/             # add, update, remove, list, test, set-default
│   ├── customer/
│   ├── charge/
│   ├── payment-intent/
│   ├── refund/
│   ├── subscription/
│   ├── invoice/
│   ├── dispute/
│   ├── event/
│   ├── product/
│   ├── price/
│   ├── balance/
│   ├── payout/
│   └── config/
├── stripe/
│   ├── client.go            # SDK init from resolved account, pins APIVersion
│   ├── readonly.go          # rejects any non-GET method (single chokepoint)
│   └── pagination.go        # auto-pagination + --stream NDJSON mode
├── config/
│   ├── store.go             # os.UserConfigDir()/agent-stripe/config.json
│   ├── keyring.go           # go-keyring wrapper for secrets
│   └── schema.go
├── output/
│   ├── json.go              # truncation, --expand, null pruning
│   └── errors.go            # structured error envelope
└── usage/                   # LLM-friendly docs per command
```

Account resolution order: `-a` flag > `AGENT_STRIPE_ACCOUNT` env > config default.

## Runtime & Tooling

- **Go** — single static binary via `go build -ldflags="-s -w"`. Pin the Go version in `go.mod`.
- **Stripe SDK** — `github.com/stripe/stripe-go/v85` (pins Stripe API version `2026-04-22.dahlia`), with `stripe.APIVersion` set explicitly at client init. Never rely on the SDK's default — version pin is part of the contract.
- **CLI** — stdlib `flag` + a small dispatcher. No Cobra.
- **Config** — stdlib `encoding/json` to `os.UserConfigDir()/agent-stripe/config.json`, file mode `0600`.
- **Credentials** — `zalando/go-keyring` for the secret; config file only carries the keychain ref.
- **Tests** — stdlib `testing` + `net/http/httptest` for mocked Stripe responses.

## Conventions — Go / general

- **Standard project layout** — `cmd/` for entrypoints, `internal/` for everything else. No exported library API.
- **Small packages, focused exports.** Prefer functions over methods unless state is genuinely involved.
- **No `init()` functions** outside `main` — explicit setup in `main.go` only.
- **Errors are values.** Wrap with `fmt.Errorf("doing X: %w", err)`. No sentinel-only errors at package boundaries; use `errors.Is` / `errors.As` where matching matters.
- **Context is the first arg** on anything that does I/O. Pass `context.Context` from `main` down; no `context.Background()` outside `main`.
- **No `panic` in shipped code.** Validate at boundaries (CLI args, config file, env vars, Stripe responses). Internal code trusts its types.
- **No globals** beyond `main`-scoped configuration. Pass dependencies in.

## Conventions — tests

- Test files live next to the source (`foo.go` + `foo_test.go`).
- Use table-driven tests for anything with more than one interesting case.
- Mock Stripe at the HTTP layer with `httptest.NewServer` + the SDK's `Backends` override — not by wrapping interfaces in app code.
- Unit-test fixtures: hand-authored JSON in `testdata/` (readable, diffable, no recording library).
- Integration tests hit Stripe **test mode** with a dedicated key, gated by `STRIPE_TEST_KEY` env var, skipped by default.

## Conventions — commands

- One package per top-level command under `internal/commands/<name>/`. Each package exports `Run(ctx, args)` and a `Usage` string.
- Subcommands are sibling files (`get.go`, `list.go`, `search.go`) routed by the package's dispatcher.
- Every command package has a `usage` subcommand — the help text is the contract with the LLM. Write it deliberately.
- Pagination defaults: `list` returns up to `list.maxResults` (default 100). `--stream` switches to NDJSON, bypasses the cap, flushes per record, handles SIGPIPE cleanly.
- Stripe's `expand[]` parameter is exposed via `--expand-stripe <fields>` so agents can request nested resources without a second round-trip.

## Conventions — output

- All output JSON to stdout. One JSON object per command (or one-per-line under `--stream`).
- **Response envelope:** every successful response is `{ mode, account, apiVersion, data }`. The Stripe payload always lives under `data` — never at the top level. `list`/`search` add a `page` sibling for pagination; `event list --related` adds a `scan` sibling.
- Under `--stream`, emit the envelope once as the first NDJSON line (with `stream: true` and no `data`), then one record per line. Keeps the mode tag without repeating it on every record.
- Long string fields (over `truncation.maxLength`, default 200) are truncated with a `…` suffix and a companion `{field}Length` key. Override per-call with `--full` or `--expand <fields>`.
- Null / empty fields are pruned by default to reduce token cost in agent context windows.

## Conventions — secrets & env

- API keys live in the OS keychain via `go-keyring`. The config file at `os.UserConfigDir()/agent-stripe/config.json` (mode `0600`) holds only `{alias, mode, keychain_ref}` — it literally cannot leak a key.
- `.env` is not used by the CLI itself — config is the source of truth. Env vars override (`AGENT_STRIPE_ACCOUNT`, `AGENT_STRIPE_TIMEOUT`) but never carry the secret.
- `--form` for credential entry: native OS dialog (osascript on macOS, zenity/kdialog on Linux, PowerShell on Windows). If no GUI session is available, exit with `fixableBy: "human"` and a hint to run on the user's local machine.

## Plans

Every non-trivial change has a plan in `plans/`. Create the plan before starting implementation.

Folder taxonomy:

- `plans/v1/` — initial build, phase by phase
- `plans/v2/` — future tiers once v1 ships
- `plans/bugfix/` — concrete bugfixes
- `plans/infra/` — CI, release, brew tap, skills package
- `plans/tech-debt/` — hardening, audits, cleanup

Rules:

- One markdown file per piece of work
- Each plan must include a **Log** section with timestamped entries (`YYYY-MM-DD HH:MM — <what changed>`)
- Update the log as work progresses — it should tell the story of what happened, not just what was planned
- Mark status `done` when complete; cut plans stay at their path with `Status: cut`

### Plan template

```markdown
# <Title>

Status: pending | in-progress | done | cut

## Goal

<What this work achieves and why>

## Plan

<Steps to implement>

## Log

- YYYY-MM-DD HH:MM — Created plan
```

## Development

- `go build ./...` — build everything
- `go build -o bin/agent-stripe ./cmd/agent-stripe` — build the CLI binary
- `go run ./cmd/agent-stripe <command>` — run the CLI from source
- `go test ./...` — unit tests (mocked Stripe responses)
- `STRIPE_TEST_KEY=sk_test_... go test -tags=integration ./...` — integration tests against Stripe test mode
- `go vet ./...` — vet
- `gofmt -w .` — format

## Before committing

- `gofmt -w . && go vet ./... && go test ./...`
- For any change in `internal/stripe/readonly.go`, `internal/stripe/client.go`, or credential handling: also run integration tests with `STRIPE_TEST_KEY` set.
- Never commit a real API key. `~/.config/agent-stripe/` and `*.local.*` are gitignored — keep it that way.
