# Plans

One folder per **work epoch** — a coherent batch of work with a single goal.
Folders are *not* release versions; see "Folders vs releases" below.

| Folder | Goal | Status |
|---|---|---|
| [`initial-scoping/`](initial-scoping/PLAN.md) | Scope the tool, pick the stack, record the up-front design decisions. | done |
| [`v1/`](v1/) | Establish the read-only foundation — keychain-backed credentials, the JSON envelope contract, the read-only HTTP chokepoint — and cover the core money-flow and billing resources. | done (shipped as `v1.0.0`) |
| [`v2/`](v2/) | Widen read coverage to the resources a real reconciliation traverses: Checkout Sessions, payment methods, setup intents, invoice/subscription sub-resources, webhook endpoints. | phase 1 done |
| [`v3/`](v3/) | Extend the CLI beyond the platform account — read connected-account data via the `Stripe-Account` header, and add the Connect-only resources. | phase 1 done, phase 2 drafted |

## Folders vs releases

Folder numbers track *work*; git tags track the *binary*, and they move at
different rates. The whole of `v1/` (12 phases) shipped as `v1.0.0`; `v2/`
phase 1 shipped without a new tag at all.

Releases follow semver against the CLI surface: a new command or flag is a
minor bump, a removed/renamed command or a changed envelope field is a major
bump. Connect support is purely additive, so `v3/` releases as `v1.1.0`.

## Plan file conventions

- One file per phase, numbered: `NN-short-name.md`.
- Header: `# Phase N — title`, then `Status: draft | in progress | done`.
- Body: `## Goal`, `## Plan` (numbered sections), `## Out of scope`,
  `## Resolved decisions`, `## Log` (dated entries, newest last).
- Keep `Status` current — a shipped plan still marked `draft` is a trap for
  whoever picks the work up next.
