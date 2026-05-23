# Phase 1 — Scaffolding

Status: pending

## Goal

Stand up the project spine so every later phase plugs in cheaply: Go module, dispatcher, JSON envelope, keychain-backed account management, and the first three read commands (`account test`, `customer get/list`, `event list`). After this phase, adding a new resource is "write the package, register it in the dispatcher" with zero infrastructure work.

Phase 1 is also where the **read-only chokepoint** (`internal/stripe/readonly.go`), the **response envelope** (`{mode, account, apiVersion, data}`), and the **live-mode confirmation** behaviour get locked in. Everything downstream inherits these decisions, so they get test coverage now, not later.

## Plan

### 1. Module + repo hygiene

- `go mod init github.com/shhac/agent-stripe`
- Pin Go version in `go.mod` (latest stable, whatever `go version` reports locally).
- Add `github.com/stripe/stripe-go/v85` and `github.com/zalando/go-keyring`.
- `.gitignore`: `bin/`, `*.local.*`, `coverage.out`, IDE folders, `~/.config/agent-stripe/` is user-side so doesn't need ignoring but worth a comment in README.
- `Makefile` (or just shell aliases in README) for `build`, `test`, `fmt`, `vet`.

### 2. `cmd/agent-stripe/main.go`

- Parse global flags (`-a`, `--live`, `--full`, `--expand`, `--timeout`) with stdlib `flag` via a `FlagSet` separated from subcommand flags.
- Resolve account: `-a` > `AGENT_STRIPE_ACCOUNT` > config default.
- Build a `context.Context` with the timeout, pass to dispatcher.
- Catch SIGINT/SIGTERM, cancel context, exit non-zero with `{"error":"interrupted"}` to stderr.

### 3. `internal/cli/` — dispatcher

- Single `Dispatch(ctx, args)` function that splits `<command> <subcommand> [args...]` and routes to the right package's `Run`.
- Top-level `usage` and `<command> usage` plumbing — usage strings live in each command package, dispatcher just routes.
- Unknown command → structured error to stderr, exit 2.

### 4. `internal/output/` — envelope + JSON

- `output.Envelope{ Mode, Account, APIVersion, Data, Page, Scan }` with `json` tags; `omitempty` on `Page` / `Scan`.
- `output.Emit(w io.Writer, env Envelope)` — marshals, writes, newline.
- `output.Stream(w io.Writer)` returns a streamer that emits the header line once, then one `data` object per call. Flushes per write, handles `EPIPE` cleanly (exit 0, don't panic).
- Truncation helper: walks the marshalled tree, truncates strings over `truncation.maxLength` (default 200), adds `{field}Length`. Skipped under `--full`; skipped for fields listed in `--expand`.
- Null/empty pruning at the same pass.
- `output/errors.go`: `Fail(err, fixableBy)` writes the error envelope to stderr and exits non-zero. One function, used everywhere.

### 5. `internal/config/` — store + keyring

- `Config{ DefaultAccount string; Accounts map[string]Account }` where `Account{ Alias, Mode, KeychainRef }`. JSON to `os.UserConfigDir()/agent-stripe/config.json` at mode `0600`.
- Atomic write (`os.CreateTemp` + `os.Rename` in same dir).
- `keyring.go`: thin wrapper around `zalando/go-keyring` — `Set(ref, secret)`, `Get(ref) (string, error)`, `Delete(ref)`. Service name `"agent-stripe"`, account = the keychain ref (UUID per alias so renames are cheap).
- `Mode` derivation: `sk_test_` → `test`, `sk_live_` → `live`, anything else → reject at add-time.

### 6. `internal/stripe/` — client + readonly + pagination

- `client.New(account Account, timeout time.Duration)` returns a configured `*client.API` with `stripe.APIVersion` set explicitly to `"2026-04-22.dahlia"`. HTTP client carries the timeout.
- `readonly.go`: a custom `stripe.HTTPClient` wrapper that inspects `req.Method` and returns an error for anything that isn't `GET`. **This is the chokepoint** — exists even though no command currently calls a write, so the guarantee is enforced by code not convention. Test: a fake `POST` request through the client returns `ErrReadOnly`.
- `pagination.go`: helper that runs a Stripe `*Iter` to completion up to `list.maxResults`, or streams indefinitely under `--stream`. Returns `(items, hasMore, nextCursor)`.

### 7. `internal/commands/account/` — first command package, sets the pattern

- `Run(ctx, args)` dispatches `add | update | remove | list | test | set-default | usage`.
- `add <alias> [--key sk_...] [--form] [--default]`:
  - Reads key from `--key`, from `--form` (OS dialog via `os/exec`), or stdin if neither and stdin is a TTY → reject (no agent-visible secret).
  - Validates `sk_test_` / `sk_live_` prefix → derives mode.
  - Generates a UUID for `KeychainRef`, writes secret to keychain, writes config entry.
- `list`: prints envelope `data: [{alias, mode, default}]`. Keys never appear — they're not on disk to print.
- `test [alias]`: resolves account, hits `GET /v1/account`, returns `{mode, account, apiVersion, data: { id, businessProfile.name, country, defaultCurrency }}`. The first command that exercises the full stack end-to-end.
- `usage`: the help string. Write it like the LLM is reading it cold.

### 8. `internal/commands/customer/` — `get`, `list`

- `get <id>` → straight wrapper around `customer.Get`, envelope it.
- `list [--email] [--created] [--limit] [--starting-after]` → uses `pagination.go`, returns `data` array + `page` envelope sibling.

### 9. `internal/commands/event/` — `list` with `--related`

- `list [--type] [--created] [--object-id]` → straight list.
- `--related <object_id>` → client-side filter over up to `list.maxEventScan` (default 500) events, matches `data.object.id == <id>`. Returns `data` array + `scan: {scanned, matched, truncated}`.

### 10. Live-mode confirmation

- Single check in the dispatcher right after account resolution: if `account.mode == "live"` and `--live` not passed and `config.account.requireLiveFlag != false`, fail with `{"error":"live account requires --live flag","fixableBy":"agent"}` exit 3.
- Test: call any command against a stubbed live account without `--live` → exits 3, no Stripe HTTP call ever made.

### 11. Tests (alongside code, not at the end)

- `output/json_test.go`: truncation, pruning, expand override, envelope shape.
- `config/store_test.go`: atomic write, round-trip, schema migration placeholder.
- `stripe/readonly_test.go`: POST/DELETE/PUT all rejected; GET passes through.
- `stripe/client_test.go`: `httptest.NewServer` returning a fake `/v1/account` response, assert envelope is correct including `mode` derivation.
- `commands/account/add_test.go`: key prefix validation, keychain write (use a fake keyring backend — `go-keyring` has `MockInit`).
- `commands/event/related_test.go`: filtering logic with a paginated fixture.
- One integration test per command, gated by `STRIPE_TEST_KEY`, in `*_integration_test.go` with `//go:build integration`.

### 12. README updates

- Quickstart: install, `account add`, `account test`, first `customer get`.
- Note that v85 pins API version `2026-04-22.dahlia` and how to verify (`account test` prints it).

## Out of scope for Phase 1

- All Phase 2+ resources (charge, payment-intent, refund, dispute, balance, payout, subscription, invoice, product, price).
- `search` across any resource (Phase 4).
- `--stream` polish beyond the basic path (Phase 4).
- Schema-discovery (`resource describe`) (Phase 4).
- Brew tap + skills package (Phase 4 / infra plan).

## Open questions to resolve as we go

- Does `account add` accept the key via stdin (pipe) at all, or only `--key` / `--form`? Pipe is convenient for power users; agents shouldn't be touching it either way. Lean: allow pipe if `!isatty(stdin)`, reject otherwise.
- `KeychainRef` format: UUID vs `agent-stripe:<alias>`. UUID survives renames; named refs are debuggable. Lean: UUID, store alias in config only.
- Should the envelope's `apiVersion` come from `stripe.APIVersion` (compile-time pin) or echo the `Stripe-Version` response header (what the server actually used)? Lean: compile-time pin in the envelope, plus surface the response header in `account test` only — that's where mismatches matter.

## Log

- 2026-05-23 — Created plan
