# Phase 13 — Connect: read connected-account data

Status: done

## Goal

Today the CLI is platform-scoped. Every command reads the books of whichever
account owns the API key, and `transfer usage` says so out loud
(`internal/commands/transfer/transfer.go:40`). An agent asked about a
connected account correctly reports that agent-stripe can't help. This phase
removes that limit.

Stripe does not expose a separate API for connected accounts — it is the same
endpoints, scoped by a `Stripe-Account: acct_...` request header. So the bulk
of this phase is **one global flag plumbed into one HTTP chokepoint**, after
which the 21 registered commands that actually call Stripe (all of the
registry bar `resource`, which is pure reflection) become
connected-account-capable with no per-command changes. The remaining work is
(a) the Connect-only resources that have no command today, (b) making the
response envelope and error mapping account-aware, and (c) documenting the
direct-vs-destination charge model so an agent knows *when* to reach for the
flag.

Reverses decision #2 in `plans/initial-scoping/PLAN.md:157` ("Connect
accounts: defer to v2. Not designing before someone asks"). Decided to pick
it up as part of v3.

## Plan

### 1. `--stripe-account` global flag

**File:** `internal/cli/dispatch.go`

- Add `StripeAccount string` to `GlobalOpts` (`dispatch.go:24`), next to
  `AccountAlias`. Two different "accounts" now exist in the struct — comment
  the distinction explicitly: `AccountAlias` selects *which credential*,
  `StripeAccount` selects *whose books that credential reads*.
- Register the flag in the global `FlagSet` (`dispatch.go:64-71`):
  `--stripe-account acct_...`, "read a connected account's data via the
  Stripe-Account header (Connect platforms only)".
- Env fallback `AGENT_STRIPE_STRIPE_ACCOUNT`, resolved with the same
  precedence as `resolveAccountAlias` (`dispatch.go:~200`): flag > env.
  No config-file default — see Resolved decisions.
- Validate the value before constructing the client: must match
  `^acct_[A-Za-z0-9]+$`. On mismatch, `output.Fail` with
  `fixableBy: "agent"` and a hint naming the expected prefix. Passing a
  `cus_`/`ch_` id here is the obvious agent mistake and must not become an
  opaque 403 from Stripe.

### 2. Header injection at the transport

**File:** `internal/stripe/client.go`

- Change the signature to
  `NewClient(apiKey, baseURL, stripeAccount string, timeout time.Duration)`
  and update **all three** call sites:
  - `internal/cli/dispatch.go:150` — the main path.
  - `internal/commands/account/account.go:269` — `account test <alias>`
    rebuilds its own client when given an explicit alias. Easy to miss; it
    must pass `opts.StripeAccount` through or §5's Connect probe silently
    reads the platform account instead.
  - `internal/testutil/testutil.go:47` — the shared test harness. Every
    command unit test constructs its client here, so threading the parameter
    through this one spot is what gives the whole test suite Connect
    coverage without per-package changes.
- When `stripeAccount != ""`, wrap the transport chain with a small
  `stripeAccountTransport` that sets the `Stripe-Account` header on every
  outbound request. Compose it *around* `NewReadOnlyTransport` so the
  read-only guarantee stays the outermost contract and is unaffected.
- New file `internal/stripe/connect.go` (~30 lines), mirroring the shape and
  comment style of `readonly.go`.
- The transport must **not** overwrite an existing `Stripe-Account` header —
  set it only when absent, so a future per-params override still wins.

**Why the transport and not params.** stripe-go v85 exposes the header only
as `Params.StripeAccount` (`params.go:134`, applied at `stripe.go:798`).
Using it would mean editing the params struct in every command package and
would silently break the next command someone adds. There is no
client-level option in v85. The transport is the same single-chokepoint move
already proven by `readonly.go` — one place, impossible to forget, and every
existing command inherits Connect support for free. This is the crux of why
this phase is small.

### 3. Envelope: echo the account scope

**File:** `internal/output/json.go`

- Add `StripeAccount string \`json:"stripeAccount,omitempty"\`` to `Envelope`
  (`json.go:17`), after `Account`.
- Populate it in `EnvelopeFor` (`internal/cli/stream.go:37`) so single, list,
  and `--stream` header lines (`json.go:203`) all carry it.
- **`EnvelopeFor` is not universal** — seven sites hand-build an
  `output.Envelope{}` literal and would silently omit the new field:
  `account.go:113,190,211,236,292`, `resource/describe.go:136`, and
  `event/event.go:145`. Two of those matter here and must be fixed in this
  phase:
  - `account.go:292` (`account test`) — the §5 Connect probe. An envelope
    that doesn't say which account it probed defeats the purpose.
  - `event.go:145` (`event list --related`) — the core debugging primitive,
    and a prime Connect path once the header exists.
  The rest are platform-only by nature (`account add/remove/list`,
  `resource describe` makes no API call) and can stay as-is, but prefer
  routing them through `EnvelopeFor` to stop this drifting again.
- Rationale, same as the existing `mode` echo: a platform charge and a
  connected-account charge are indistinguishable in the output otherwise. An
  agent must be able to verify what it just read without re-deriving it from
  the command line.
- Omitted when empty, so every existing golden/test expectation for
  platform-scoped output is unchanged.

### 4. Error mapping for Connect failures

**File:** `internal/output/errors.go`

Two new failure modes an agent cannot fix by retrying, both of which must map
to `fixableBy: "human"` rather than the default `agent`:

- **`account_invalid` / 403** — the `acct_` is not connected to this platform,
  or does not exist. Hint: "check the account is connected to this platform
  (`agent-stripe --stripe-account <id> account test`)".
- **Restricted key without Connect permission** — `rk_` keys are already
  accepted (`config.DeriveMode`, `store.go`) but need explicit Connect read
  scopes. Stripe returns a permissions error. Hint should name the missing
  scope rather than suggesting a retry.

Without this, an agent hitting a permissions wall will retry-loop on a
problem only a human with Dashboard access can resolve.

### 5. `account test` as the Connect probe

**File:** `internal/commands/account/account.go`

`account test` already hits `GET /v1/account`. With the header set it returns
the *connected* account, which makes it a free "is this account actually
reachable from my platform key?" check — the first step of any Connect
investigation, and worth saying so in `account usage`.

Two code changes are required, both easy to overlook because the command
appears to need none:

- `account.go:269` — the alias-arg path rebuilds its own client and must
  thread `opts.StripeAccount` through (§2).
- `account.go:292` — the hand-built envelope must carry `stripeAccount` (§3).

Without both, the probe reports success against the platform account while
appearing to confirm the connected one — the worst possible failure for a
command whose entire job is verifying scope.

### 6. New command: `connected-account`

**Package:** `internal/commands/connectedaccount/` — CLI name
`connected-account`.

Naming: the existing `account` command manages local keychain aliases and
keeps that name. `connected-account` is the Stripe Accounts API. Renaming
`account` would break every existing invocation and the SKILL.md contract.

- `get <acct_id>` — `V1Accounts.Retrieve`.
- `list [--created-gt T] [--created-lt T] [--limit N] [--starting-after ACCT]`
  — `V1Accounts.List`. Platform-scoped (lists *your* connected accounts), so
  it is one of the few commands where `--stripe-account` is meaningless;
  document that.
- `capabilities <acct_id>` — `V1Capabilities.List`. Params confirmed:
  `CapabilityListParams{Account *string}` (`capability.go:55`). Answers "why
  can't this account take payments / receive payouts".
- `persons <acct_id> [--relationship-owner] [--relationship-director]` —
  `V1Persons.List`. Params confirmed: `PersonListParams{Account *string,
  Relationship *PersonListRelationshipParams}` (`person.go:404`). KYC and
  verification debugging.
- `external-accounts <acct_id>` — **no dedicated service in v85.** External
  accounts arrive inline as `Account.ExternalAccounts`
  (`*AccountExternalAccountList`, `account.go:4465`). Implement as a
  `V1Accounts.Retrieve` with `expand[]=external_accounts` and emit the nested
  list. Answers "where do this account's payouts actually go".
- No `search` — Stripe's Search API doesn't cover accounts.
- Sub-resources as subcommands, per the `balance transactions` /
  `invoice lines` precedent from v1/v2.
- `usage` must state the verification chain plainly:
  `charges_enabled` / `payouts_enabled` are the summary booleans;
  `requirements.currently_due` and `requirements.disabled_reason` are the
  *why*; `capabilities` is the per-capability breakdown.

### 7. New command: `application-fee`

**Package:** `internal/commands/applicationfee/`.

- `get <fee_id>` — `V1ApplicationFees.Retrieve`.
- `list [--charge CH] [--created-gt T] [--created-lt T] [--limit N]
   [--starting-after FEE]` — params confirmed:
  `ApplicationFeeListParams{Charge, CreatedRange}` (`applicationfee.go:21`).
- `refunds <fee_id> [--limit N] [--starting-after FR]` — `V1FeeRefunds.List`.
  Subcommand, mirroring `transfer reversals`.
- No `search`.
- `usage`: this is the platform's revenue on direct charges. Cross-reference
  `charge.application_fee` (already a recommended expand path at
  `describe.go:59`) and `payment_intent.application_fee_amount`.

### 8. `resource describe` registration

**File:** `internal/commands/resource/describe.go`

Both maps are hand-maintained; add the new types to each.

- `resourceRegistry` (`describe.go:31`): `"connected-account":
  stripeapi.Account{}`, `"person": stripeapi.Person{}`, `"capability":
  stripeapi.Capability{}`, `"application-fee": stripeapi.ApplicationFee{}`,
  `"fee-refund": stripeapi.FeeRefund{}`.
- `expandPathsByResource` (`describe.go:57`):
  - `connected-account`: `external_accounts`, `settings`, `requirements`
  - `application-fee`: `charge`, `balance_transaction`, `refunds`,
    `originating_transaction`
  - `person`, `capability`, `fee-refund`: `{}` (small, self-contained shapes)

### 9. Documentation — the part that changes agent behaviour

The flag is useless if the agent doesn't know when to reach for it. The
single concept to land: **where does the object live?**

- *Destination charge* — lives on the **platform**, carries
  `transfer_data.destination`. No flag needed.
- *Direct charge* — lives on the **connected account**. Invisible from the
  platform entirely without the header.
- *Separate charges and transfers* — charge on the platform, a `tr_...` to
  the connected account, with `transfer_group` tying them together.

Getting this wrong is the top failure mode: an agent looks for a direct
charge from the platform, finds nothing, and reports "the charge does not
exist." Every doc surface must make the distinction load-bearing.

Fields that link the two views: `on_behalf_of`, `application_fee_amount`,
`transfer_data.destination`, `source_transaction`, `transfer_group`.

Also note: `on_behalf_of` is **not** the `Stripe-Account` header. It is a
field set at creation time that this CLI only ever reads. The old plan notes
(`plans/v1/03-billing.md:102`, `plans/v1/04-polish.md:103`) conflate the two;
for a read-only CLI the header is the entire mechanism.

Surfaces to update:

- `internal/commands/transfer/transfer.go:40` — delete the "Scope: platform
  account only… no `--stripe-account` flag exists today" note, replace with
  the connected-account view.
- `README.md:201` — "Not included: … Connect onboarding" stays (onboarding is
  a write flow), but the blanket Connect exclusion goes.
- `README.md` — new worked example: platform charge → transfer →
  connected-account balance transaction → payout, showing the flag appearing
  partway through the chain. Add `connected-account` and `application-fee`
  rows to the Resources table.
- `README.md` Safety section — document the account echo alongside the mode
  echo.
- `SKILL.md` and `AGENTS.md` — the routing contract; an agent reads these to
  decide whether agent-stripe can answer a Connect question at all.
- Command `Usage` blocks: `balance`, `payout`, `transfer`, `charge`,
  `payment-intent`, `event` — the six where the flag most changes the answer.
- Top-level usage (`printTopUsage`, `dispatch.go`) — the flag goes in the
  usage line alongside `--account`.

### 10. Tests

The transport approach is what keeps this section short — because injection
is global, there is no per-command test matrix.

**Unit:**
- `internal/stripe/connect_test.go` — header present when set, absent when
  empty, not overwritten when already present, and the read-only rejection
  still fires first for non-GET.
- `internal/cli/dispatch_test.go` — flag and env resolution precedence;
  `acct_` validation rejects `cus_123` with `fixableBy: agent` + hint.
- `internal/output/json_test.go` — `stripeAccount` marshals, omitted when
  empty (proves existing expectations are unchanged).
- `internal/cli/stream_test.go` — the NDJSON header line carries it.
- `internal/output/errors_test.go` — `account_invalid` → `fixableBy: human`.
- One command package each for `connectedaccount` and `applicationfee`,
  sized to match the existing dispute/webhookendpoint test files: subcommand
  routing, required-positional errors, filter passthrough.

**Integration** (`STRIPE_TEST_KEY`-gated, `//go:build integration`):
- Requires a test-mode connected account — note the setup step in the
  Development section of the README, and skip cleanly when absent
  (a second env var, `STRIPE_TEST_CONNECTED_ACCOUNT`).
- `connected-account list --limit 1`, then `capabilities`/`persons` on the
  returned id.
- `--stripe-account <acct> balance` — the canonical "why no payout" entry
  point.
- `--stripe-account <acct> account test` — the §5 probe.

## Out of scope

- **Connect onboarding** — `account_links`, `account_sessions`, and the
  hosted onboarding flow are all writes. Stays with the official Stripe CLI.
- **Any write op**, per the standing posture; the read-only transport blocks
  them regardless.
- **`topup` and `country-spec`** — real Connect resources, but neither is on
  a debugging path anyone has hit yet. Next batch if asked for.
- **Config-file default for `--stripe-account`** — see Resolved decisions.
- **v2-account-API (`/v2/core/accounts`)** — the newer Connect account
  surface. Out until the v1 Accounts API stops answering the questions being
  asked.

## Resolved decisions

- **Flag, not a saved alias.** One platform key can read *every* connected
  account, so alias-per-account would duplicate the same secret in the
  keychain N times — the alias system assumes one alias = one credential.
  More importantly, an agent discovers `acct_` ids mid-investigation (from
  `transfer_data.destination`) and must pivot in the very next command; a
  flag allows that with no config write, which also keeps the CLI's
  read-only posture intact. A saved default would additionally reintroduce
  silent scoping — commands reading a different account than the transcript
  implies — which is the exact failure mode the `mode` echo exists to
  prevent. Alias-pinning can be revisited if repeated single-merchant work
  proves annoying.
- **`connected-account` as the command name.** `account` keeps its current
  meaning (local credential aliases). Renaming it would break every existing
  invocation and the published SKILL.md contract for no benefit.
- **Header injected at the transport, not per-params.** See §2 rationale.
- **Envelope gains `stripeAccount`.** Cheap, `omitempty` so nothing existing
  changes, and it closes the "whose books did I just read" ambiguity that
  otherwise has no answer in the output.
- **No `--on-behalf-of` flag.** It is a write-time field, not a read scope.
  Reading it is already covered by normal object output.
- **Live-mode gating unchanged.** `--live` composes with `--stripe-account`
  as-is; a connected-account read is still a read, so no additional
  confirmation gate is warranted.

## Log

- 2026-08-10 — Drafted plan. SDK shapes verified against `stripe-go/v85`:
  `Params.StripeAccount` is per-request-only with no client-level option
  (`params.go:134`, `stripe.go:798`), which is what forces the transport
  approach in §2; `V1Accounts`, `V1Capabilities`, `V1Persons`,
  `V1ApplicationFees`, `V1FeeRefunds` all exist; there is **no** external
  accounts service, so §6 reads them via expand on the account object
  (`account.go:4465`).
- 2026-08-10 — Self-review before implementation caught three plan errors, now fixed: (a) `NewClient` has three call sites, not one — `account test` and the shared test harness were missed; (b) `EnvelopeFor` lives in `cli/stream.go:37`, not `emit.go`, and seven sites bypass it with hand-built envelope literals, two of which (`account test`, `event list --related`) must be fixed here; (c) the command count is 21 that call Stripe, not 22 (`resource` is pure reflection).
- 2026-08-10 — Implemented, all 10 sections. Three commits on top of 683d104:
  (1) the flag + transport + envelope + error mapping, (2) the
  `connected-account` and `application-fee` commands + `resource describe`
  registration, (3) documentation + integration tests. Full CI suite green
  (`gofmt -l`, `go vet` with and without the integration tag, `go build`,
  `go test -race`, `golangci-lint` 0 issues).

  Deviations from the plan, all small:
  - §2 composition reversed relative to the plan's wording: the transport is
    composed *inside* `NewReadOnlyTransport`, not around it. The plan asked
    for both "compose around" and "read-only stays outermost", which are
    contradictory; the test in §10 ("read-only rejection still fires first")
    settles it, and that test now exists.
  - §2 call sites: a fourth category turned up — 20 `//go:build integration`
    files construct clients directly and would have failed to compile under
    `make integration`. Fixed in the same commit.
  - §10 added `testutil.NewOptsForAccount`, so a command test can assert the
    header reaches the wire without hand-building a client.
  - §7 `application-fee` also gained an integration sweep (not specified, but
    it matches every other command package).
