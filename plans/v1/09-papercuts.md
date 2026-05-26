# Phase 9 — Smoke-test papercuts

Status: done

## Goal

Two rough edges surfaced by the first real binary smoke-test against Stripe
test mode. Neither breaks the read-only/JSON-out/exit-nonzero contract, but
both are the kind of thing an agent or human hits in the first 30 seconds.
Bundle into one PR; they're independent but each is too small to justify
its own review cycle.

## 1. Interleaved flag parsing

### Problem

Stdlib `flag.FlagSet.Parse` stops at the first non-flag arg. So:

```
agent-stripe account add it-smoke --key sk_test_xxx --default
→ {"error":"usage: account add <alias> [--key SK | --form] [--default]"}

agent-stripe charge list --limit 5 --stream
→ {"error":"flag provided but not defined: -stream"}
```

Both *look* correct to anyone used to modern CLIs (git, gh, kubectl all
accept interleaved flags). The first one fails because `it-smoke` ends
parsing inside `runAdd`; the second because `--stream` is a global flag
that must precede the subcommand. Error messages don't hint at either cause.

### Fix

Add a small `reorderFlagsFirst(args, fs)` helper in `internal/cli/` that
walks args once, pulls flag tokens (and their values) to the front, leaves
positionals at the back, then hands the result to `fs.Parse`. Detect bool
flags via the `interface{ IsBoolFlag() bool }` check — that's how stdlib's
own parser distinguishes `--default` (no value) from `--key X` (consumes
next token). Handles `--key=X` form trivially (single token, no lookahead).

Apply at the two parse sites:

- `internal/cli/dispatch.go` — global flag parse before subcommand dispatch.
  This unblocks `agent-stripe charge list --limit 5 --stream` by letting
  `--stream` appear anywhere.
- Per-subcommand parsers that mix positional + flag args: `runAdd`,
  `runUpdate`, `runRemove`, `runTest`, `runSetDefault` in
  `internal/commands/account/account.go`. Resource subcommands (`get <id>`,
  `list`, `search`) take a single positional or none, so they're already
  fine — but routing all parses through the helper keeps the behaviour
  uniform and avoids surprise asymmetry later.

### Edge cases to cover in tests

- Bool flag mid-positionals: `account add it-smoke --default` → both `it-smoke` and `--default` survive.
- Value flag mid-positionals: `account add it-smoke --key sk_test_X` → `--key` consumes `sk_test_X`, not `it-smoke`.
- `--flag=value` form: `account add it-smoke --key=sk_test_X` → single token, no lookahead.
- `--` terminator: everything after `--` stays positional, even if it looks like a flag. (Stripe ids never start with `-`, but agents passing raw user input might.)
- Unknown flag: still errors via `fs.Parse`, not silently swallowed.

### Out of scope

Switching to cobra/pflag. The whole point of stdlib `flag` was a 2-level
subcommand tree (decision #6 in `PLAN.md`). One helper is ~40 lines; a
library swap rewrites the dispatcher.

## 2. Stripe API error envelope

### Problem

`agent-stripe charge get ch_does_not_exist` emits two things to stderr:

```
[ERROR] Request error from Stripe (status 404): {...full JSON blob...}
{"error":"{\"code\":\"resource_missing\",\"doc_url\":\"...\",\"status\":404,\"message\":\"No such charge: 'ch_does_not_exist'\",...}","fixableBy":"agent"}
```

Two problems:

1. The `[ERROR] Request error from Stripe...` line is stripe-go's own
   logger leaking past our envelope. Breaks the "one JSON line on stderr"
   contract — an agent parsing stderr sees garbage before the JSON.
2. The Stripe error blob is stuffed wholesale into the `error` string
   field, JSON-escaped. An agent that wanted the human message has to
   parse JSON twice (once to read the envelope, again on the inner string).

### Fix

**Silence the SDK logger.** In `internal/stripe/client.go:NewClient`, set
`stripe.DefaultLeveledLogger = &stripe.LeveledLogger{Level: stripe.LevelError}`
to an io.Discard-backed sink, or override via `BackendConfig.LeveledLogger`.
The Stripe SDK logs every request error at ERROR level by default; we
already surface errors through our envelope, so the SDK log is pure noise.
Verify by re-running the 404 case — the `[ERROR]` line should disappear.

**Restructure the error envelope.** Extend `output.ErrorEnvelope` in
`internal/output/errors.go`:

```go
type ErrorEnvelope struct {
    Error      string    `json:"error"`                 // human message
    FixableBy  FixableBy `json:"fixableBy,omitempty"`
    StripeCode string    `json:"stripeCode,omitempty"`  // e.g. resource_missing
    HTTPStatus int       `json:"httpStatus,omitempty"`  // e.g. 404
    RequestID  string    `json:"requestId,omitempty"`   // for Stripe support
}
```

Add a helper `FailFromStripeError(err error)` that type-asserts
`*stripe.Error` (from `github.com/stripe/stripe-go/v85`) and populates the
new fields, falling back to the current behaviour for non-Stripe errors.
Call it from the central error-handling point — search for where the
existing `fixableBy:"agent"` envelope is produced for Stripe errors and
swap.

### Test plan

- Unit: feed a fake `*stripe.Error{Code: "resource_missing", HTTPStatusCode: 404, RequestID: "req_X", Msg: "..."}` to the new helper, assert all fields populate.
- Unit: feed a plain `errors.New("...")` to the helper, assert it falls through to the original two-field envelope.
- Integration (smoke, not automated): rerun `charge get ch_does_not_exist`, confirm no `[ERROR]` leak and the envelope has structured fields.

### Out of scope

- Mapping every Stripe error code to `FixableBy` semantically. Today's
  blanket `fixableBy:"agent"` is fine; refining (e.g. `invalid_request_error`
  → agent, `api_error` → retry, `authentication_error` → human) is a
  separate judgement-heavy task.
- Rate-limit / retry handling. Different problem.

## Acceptance

- `agent-stripe charge list --limit 5 --stream` works.
- `agent-stripe account add it-smoke --key sk_test_xxx --default` works.
- `agent-stripe charge get ch_bogus` emits exactly one line on stderr,
  parseable as JSON, with structured `stripeCode` / `httpStatus` /
  `requestId` fields.
- Existing unit + integration tests still pass.
- Re-run the binary smoke-test checklist from the v1 shakedown — no new
  papercuts.

## Rollout

Single PR off `main`. No migration concerns: envelope adds fields rather
than renaming, so any existing parser that reads `error` + `fixableBy`
keeps working.
