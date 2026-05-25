# Phase 10 — long-form-only flags + help discoverability

Status: done

## Goal

Two related papercuts:

1. `-a` is the only short-form global flag. Rather than add more short
   aliases, drop `-a` and commit to long-form-only as a design principle.
   No users yet — the window to do this cleanly is now.
2. `-h` / `--help` work at every subcommand level (Go stdlib registers them
   on every `FlagSet`) but they're not mentioned in any usage string, so
   users only learn `usage` / `help`. Document them.

## Rationale

This is an agent-first CLI. Short forms are a human-keystroke optimization
that actively hurts the primary user:

- `--account prod` is self-documenting in transcripts; `-a prod` requires
  the reader to know the alias.
- LLMs read and write argv as text — long-form is unambiguous and stable.
- Single-letter namespace is contested (`-e`, `-s`, `-t` all have multiple
  conventional meanings); spending them now forecloses future flags.
- One way to do things = simpler help output, docs, and tests.

Cost: slightly longer commands for humans. Acceptable.

## Scope

### Drop `-a`

- Remove the `-a` registration in the globals `FlagSet` (see
  `parseUntilSubcommand` in `internal/cli/dispatch.go:149`).
- `--account` remains the only way to set the account alias.
- Update any tests that pass `-a` to use `--account`.

### Document the principle

- Add a short note to `AGENTS.md` (or the `internal/cli` package doc)
  stating: global and subcommand flags are long-form only. Short aliases
  are intentionally not provided.

### Help / usage discoverability

- `usage` and `help` are already accepted as subcommand-name tokens at the
  top level and in every subcommand (`internal/cli/dispatch.go:89` and
  every `case "usage", "help":` in `internal/commands/*/`).
- `-h` / `--help` work for free via stdlib but are undocumented.
- Update each subcommand's `Usage` string to append `-h/--help` alongside
  `usage`/`help` so it's discoverable. ~14 files.
- Update `printTopUsage` (`internal/cli/dispatch.go:237`) usage line to
  mention `-h/--help`.

### README

- Change line 85 example from `agent-stripe -a prod --live charge list` to
  `agent-stripe --account prod --live charge list`.

## Out of scope

- Adding any short aliases (global or subcommand). Long-form only.
- Any flag-parsing library swap (cobra/pflag). Stdlib is sufficient.

## Tests

- `--account foo` resolves to `GlobalOpts.AccountAlias`.
- `-a foo` is rejected (verify it no longer parses as a known flag).
- `--help` at top level prints usage and exits 0 (verify existing coverage).

## Log

- 2026-05-25 — Implemented. Dropped `-a` from the globals FlagSet (`internal/cli/dispatch.go`), `--account` is now the only spelling. Updated `printTopUsage` to advertise long-form-only and the `usage | help | -h | --help` quartet. Added a `Help: usage | help | -h | --help` footer to every command `Usage` const (account, balance, charge, customer, dispute, event, invoice, paymentintent, payout, price, product, refund, resource, subscription, transfer). README example switched to `--account prod`. AGENTS.md updated: resolution order now references `--account`, plus a paragraph stating flags are long-form only. New `internal/cli/dispatch_test.go` covers `--account` parsing and `-a` rejection. `gofmt`, `go vet`, `go test ./...` all green.
