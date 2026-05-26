# Plan: add `hint` to the error envelope

Status: done

## Goal
When an agent passes a name from a closed set (command, subcommand, account alias, resource type), the error envelope on stderr suggests the closest valid value so the agent can self-correct without a human round-trip.

Example:
```json
{"error":"unknown command \"chage\"","fixableBy":"agent","hint":"did you mean \"charge\"? run `agent-stripe usage` for the full list"}
```

## Step 1 — Extend the envelope
**File:** `internal/output/errors.go`
- Add `Hint string \`json:"hint,omitempty"\`` to `ErrorEnvelope` after `FixableBy`.
- Add `FailWithHint(msg, hint string, by FixableBy, code int)` mirroring `Fail`. Keep `Fail`/`EmitError` unchanged.
- Update `errors_test.go`: hint marshals as `hint`, omitted when empty.

## Step 2 — Suggestion helper
**New file:** `internal/output/suggest.go`
- `func Closest(input string, candidates []string) string` — Levenshtein distance ≤ `max(2, len(input)/3)`, case-insensitive. Returns `""` if nothing qualifies.
- `func ValidList(candidates []string) string` — returns `"valid: a, b, c"` for use when no close match exists and the list is short.
- ~50 lines, no deps. Tested with: exact miss → "", one-char typo → match, totally different → "".

## Step 3 — Hint policy
- **Top-level commands** (~25 entries): suggest closest match; if none, omit hint (list too long to dump).
- **Subcommands** (typically 2-4 entries per resource): suggest closest match; if none, fall back to `valid: get, list, search`.
- **Account aliases** (user-defined, usually <10): suggest closest match; if none, fall back to `valid: prod, staging, dev` plus `(or run `agent-stripe account list`)`.
- **Resource types in `resource describe`**: same as account aliases pattern.

## Step 4 — Error sentinel
**File:** `internal/output/errors.go`
- Add `type Error struct { Msg, Hint string; By FixableBy }` with `Error() string` returning `Msg`.
- Dispatch (`internal/cli/dispatch.go:125` and `:147`) does `errors.As(err, &output.Error{})` and routes to `FailWithHint` with the appropriate exit code. Falls through to existing `Fail`/`FailFromStripeError` otherwise.
- Lets command packages stay ignorant of exit codes — they just return `&output.Error{Msg, Hint, By}` instead of `fmt.Errorf`.

## Step 5 — Wire it in (one pass, all sites)
- `internal/cli/dispatch.go:96` — unknown top-level command. Inline `FailWithHint` (not the sentinel; this site already calls `Fail` directly).
- `internal/cli/dispatch.go:220` — unknown account alias inside `resolveAccountAlias`. Return `*output.Error`.
- `internal/commands/account/account.go:171,218,242` — unknown alias in `account use/show/remove`. Return `*output.Error`.
- All `unknown … subcommand %q` sites in `internal/commands/*/[name].go` (~16 files). Return `*output.Error`. Mechanical pass.
- `internal/commands/resource/describe.go` — unknown resource type. Return `*output.Error`.

## Step 6 — Tests
- One new envelope test (Step 1).
- `suggest_test.go` for `Closest` + `ValidList`.
- One integration-style test per category (top-level cmd, subcommand, account alias, resource type) hitting the dispatch path to confirm the hint reaches stderr. Add to existing test files where possible.

## Step 7 — Docs
- `README.md`: one line under the error contract area — `error envelope: {error, fixableBy, hint?, stripeCode?, httpStatus?, requestId?}`.

## Out of scope
- Hints for Stripe API errors (Stripe owns that surface).
- Hints for flag-parse errors (the `flag` package handles them).
- Keychain ref rename (separate concern).
- Hints for keychain read failures (no closed set).

## Estimated effort
~1.5 hours. Step 5 is the bulk and is mechanical once the sentinel exists.
