# Phase 4 — Plan 3: CLI help/usage audit

Status: done
Parent: [04-polish.md](./04-polish.md)

## Goal

Quick sweep of every command's `Usage` string to make sure the help text
matches the surface that actually shipped in Phase 4. No new behavior, no
refactors — just bringing the prose in line with the code so an agent
reading `agent-stripe <cmd> usage` doesn't get lied to.

Time budget: ~45 minutes. Expected output: 5–10 small `Usage` const edits,
maybe one tweak to the top-level dispatcher's global-flags line, plus the
two test-comment rewrites called out in checks 6 and 7. If the audit
turns up something structural (a missing flag, wrong default,
behavior-vs-doc divergence), kick that out into its own ticket — this
plan is documentation accuracy only.

## Things to check, per command

For each of the 14 command packages under `internal/commands/`:

1. **`search` subcommand listed** where it exists. Per [04-polish.md §1](./04-polish.md)
   and the Phase 4 log, search shipped for: `customer`, `charge`,
   `payment-intent`, `subscription`, `invoice`, `product`, `price` (seven, not
   eight — `refund` was planned but `stripe-go` v85 doesn't expose
   `Refund.Search`, so refund stays list-only). The `Usage` block for each of
   those seven must show the `search --query <q> [--limit N] [--page T]` line.
2. **"No Search API" disclaimer on every list-only resource.** 04-polish.md §1
   is explicit: Stripe doesn't offer Search on `refund`, `account`, `balance`,
   `payout`, `dispute`, `event`. Each of those six `Usage` blocks must carry a
   one-line note saying so, in the same place and style — otherwise an agent
   that pattern-matched on `customer search` will burn a turn discovering
   `dispute search` doesn't exist. Template (refund-flavored):
   ```
   Note: Stripe does not offer a Search API for refunds — use list with
   --charge / --payment-intent filters.
   ```
   Adapt the trailing "use list with …" hint per resource.
3. **`--page` vs `--starting-after` distinction** called out on *both* sides:
   - Every `search` subcommand must say `--page` is Stripe's opaque
     `next_page` token, **not** interchangeable with `list`'s
     `--starting-after`. Customer's `search` block (`customer.go:25-26`) is
     the template.
   - Every `list` subcommand must say the inverse: use `--starting-after`,
     not `--page`. Both directions matter so an agent landing on either
     command learns the same lesson.
4. **Eventual-consistency note** on every `search` line (~1 min lag). Same
   template as customer.
5. **Search query syntax pointer** —
   `https://docs.stripe.com/search#search-query-language` — on every
   resource that has search. Cheap, high-value, agent-discoverable.
6. **One canonical search example** per resource that's actually useful for
   *that* resource (not a copy-paste of customer's email example). Plan §1
   suggested `customer search --query 'email:"alice@example.com"'` and
   `charge search --query 'amount>5000 AND status:"succeeded"'`; pick
   something equally on-point for the other five.
7. **`--stream` documented iff supported.** Every command that supports
   `--stream` (the `list` and `search` shapes) must mention it in its
   `Usage`, pointing back at the top-level flag rather than re-paraphrasing
   (see dispatcher section). Commands that don't support it (`get`,
   `balance` retrieve, `resource describe`) must **not** claim to. Build the
   support list from the Phase 4 `--stream` rollout and diff against what
   each `Usage` says.
8. **`--expand` path-aware semantics (Phase 4 §4).** The flag changed
   meaning: a value containing `.` (e.g. `lines.data.description`) now
   routes to `ExpandPaths`, a bare identifier (e.g. `customer_id`) stays in
   `Expand` and matches anywhere in the tree. Every `Usage` block that
   documents `--expand` must reflect the new behavior, not the
   pre-Phase-4 "field-name only" shape. This is the most likely source of
   prose-vs-code drift in the audit — the change was buried in §4 and
   wasn't called out in the per-command rollout commits. Canonical
   one-liner to converge on (lift into per-command `Usage` verbatim):
   ```
   --expand: bare names (e.g. id) match any field; dotted paths
   (e.g. lines.data.description) skip truncation on that exact path.
   ```
9. **Stale "tracked for Phase 4" references** — two known, both need
   substantive rewrites, not phrase tweaks:
   - `internal/commands/invoice/invoice.go:45`: currently says
     `read-only chokepoint blocks POST. Tracked for Phase 4.` That decision
     was parked ("Out of scope for Phase 4" in 04-polish.md) — rewrite to
     reflect the current state ("blocked by the read-only chokepoint; v2
     work") rather than promising something this phase.
   - `internal/commands/invoice/invoice_test.go:44`: comment reads
     `If per-field path overrides land in Phase 4, this test gets revisited.`
     That feature shipped (§4 `ExpandPaths`). Before editing: run the test
     and confirm it still passes unmodified — if it does, rewrite to
     past-tense and cross-reference whichever new test covers the opt-in
     `--expand lines.data.description` case ("path-overrides shipped in §4;
     see TestX for the opt-in path. This test pins the default-truncated
     behavior."). If the test no longer passes as-is, the comment isn't the
     bug — kick out as a separate ticket per the "structural → separate
     ticket" rule.
10. **Per-command usage reads well**. Subjective pass: line up the columns,
    make sure the longest example fits in 100 cols, no half-sentences,
    consistent verb tense ("list charges", not "lists charges" in one
    place and "list" in another).

## Top-level dispatcher

`internal/cli/dispatch.go:229` already includes `--stream` in the global
flags line:

```
agent-stripe [-a ALIAS] [--live] [--full] [--expand FIELDS] [--expand-stripe PATHS] [--stream] [--timeout DUR] <command> [args]
```

Confirm:

- Order matches what we tell agents elsewhere (README, SKILL.md, AGENTS.md).
- `--stream` flag description in `dispatch.go:62` is the canonical one-liner
  — every command that documents `--stream` in its own `Usage` should point
  at the top-level help rather than re-paraphrase (single source of truth).
- Top-level command listing groups `resource` correctly (meta-command,
  separate from the per-resource block) per 04-polish.md §3.

## `resource describe` — its own check

New top-level command in Phase 4 (§3). Easy to miss in a per-resource
sweep because it lives outside the 13 resource packages. Confirm its
`Usage` (in `internal/commands/resource/`) covers:

- `--depth N` flag with the default (3) called out.
- The no-API-call guarantee — "this command does not hit Stripe; safe to
  run without `-a`". This is the headline reason `resource describe`
  exists; if `Usage` doesn't say it, agents won't know to reach for it.
- The `expandPaths` output field — what it means and that it's the
  machine-readable mirror of each resource's `--expand-stripe` hint.

## What this audit is NOT

- Not a behavior change. If a flag is missing or wrong, file it separately.
- Not a SKILL.md or README pass — those are 04-polish.md §5/§7 and already
  marked done; this audit is strictly the in-binary `<cmd> usage` text.
- Not a flag-level documentation rewrite. SKILL.md explicitly defers
  flag-level detail to per-command usage (04-polish.md §5), so the bar
  here is "accurate and parseable", not "exhaustive".
- Not a place to relitigate `--page` vs `nextCursor` naming
  (04-polish.md "Open questions") — that one was resolved, just verify the
  resolution is what shipped.

## Method

1. `grep -A 80 'const Usage' internal/commands/*/*.go` — read all 14 usage
   blocks side-by-side. (Earlier draft used `sed -n '/const Usage/,/^`/p'`,
   which is fragile around backtick termination and mixed const names —
   grep with a generous `-A` window is simpler and doesn't miss blocks.)
2. Diff against (a) the seven-resource search list, (b) the six-resource
   "no Search API" disclaimer list, (c) the `--stream` rollout from the
   Phase 4 log, (d) the §4 `--expand` path-aware semantics change.
3. Before touching `invoice_test.go:44`, run
   `go test ./internal/commands/invoice/ -run TestInvoiceGet -v` and
   confirm the default-truncation test still passes unmodified. If it
   does, the comment rewrite is safe; if not, that's a structural change
   and goes to a separate ticket.
4. Make edits inline.
5. `go test ./internal/commands/...` to make sure no test snapshots
   `Usage` strings verbatim (if any do, update those snapshots in the
   same commit).
6. One commit. Title: `docs(v1): Phase 4 CLI usage-string audit`.

## Out of scope

- Anything in [04-polish-stream-related.md](./04-polish-stream-related.md)
  (the `event list --related` streaming work — that plan owns its own
  usage edits).
- Anything in [04-polish-tests.md](./04-polish-tests.md).
- Reflowing or restyling the dispatcher's top-level command listing
  beyond confirming `resource` is grouped right.

## Log

- 2026-05-24 — Plan drafted. Single grep already surfaced one stale
  "Tracked for Phase 4" reference (invoice.go:45) — strong signal there
  are a few more small inconsistencies worth a single focused pass.
- 2026-05-24 — Implemented in 23ddbfc. Edits landed across 13 command Usage
  blocks plus invoice_test.go. No structural changes surfaced (the `--expand`
  §4 drift check was vacuous — no per-command block documented `--expand`).
  Kicked out as separate follow-up: top-level help iterates `reg.UsageStrings`
  as a Go map, so `resource` isn't deterministically grouped as a
  meta-command — per the "structural → separate ticket" rule.
- 2026-05-24 — Plan revised after review. Added: list-only "no Search
  API" disclaimer check across all six list-only resources (was
  refund-only); `--stream` documented-iff-supported check; `--expand`
  path-aware semantics check (Phase 4 §4 drift, likely the highest-yield
  find); `resource describe` its-own-check section; `list`-side
  `--page`/`--starting-after` distinction (was search-only). Promoted
  `invoice_test.go:44` from a phrase-tweak to a substantive rewrite with
  a pre-edit `go test` gate. Replaced fragile `sed` one-liner with
  `grep -A 80`. Bumped time budget 20 → 45 min.
