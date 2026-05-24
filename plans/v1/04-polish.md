# Phase 4 — Polish

Status: draft

## Goal

Take the read-only resource coverage built in Phases 1–3 and turn it into something an agent actually wants to reach for: uniform search across resources, NDJSON streaming for the lists that overflow the default cap, a schema/describe surface so agents can learn a resource without burning an API call, and the packaging (`SKILL.md`, Homebrew tap) that gets it onto a teammate's machine in one command.

What's genuinely new vs. Phases 1–3:

1. **Search is the first command shape that touches every resource at once.** Stripe Search is its own query language (eventually consistent, ~1min lag) and lives on a different SDK path (`V1Customers.Search` etc.) than `List`. Phase 4 has to pick whether `search` is one top-level command with `--resource` or a subcommand per resource (`customer search`, `charge search`, …). The PLAN.md CLI sketch already commits to the per-resource shape — confirm that's still right, then make sure the shared query-handling lives in one place so we don't write the same `--query/--limit/--starting-after` plumbing eight times.
2. **`--stream` changes the envelope contract.** Phase 3 envelopes are one JSON object per command invocation; `--stream` emits a header line then one `data` per line (per PLAN.md §Output). The output package has the envelope types but no streaming writer yet — that's net-new infrastructure and the only place in Phase 4 that touches `internal/output/`.
3. **`resource describe`** reflects over `stripe-go` structs. That's reflection over a third-party SDK we don't control; the shape we emit becomes a contract agents will pattern-match against. Pick that shape carefully — once it ships, changing it is a breaking change for skill prompts.
4. **The per-field truncation tripwire from Phase 3 finally fires** if search proves out the predicted shape. Phase 3's resolved decision deferred it explicitly to here ("pre-`search` task — search results across resources will hit the same shape").
5. **Skill packaging + brew tap are user-facing surface.** Until now everything has been internal. `SKILL.md` is what Claude Code reads to decide to invoke us at all; the brew tap is what unblocks anyone-not-Simon from running this.

No new global flags are expected besides `--stream`. If something else creeps into `GlobalOpts`, stop and reconsider — Phases 1–3 kept it tight on purpose.

## Plan

### 1. `search` — per-resource, shared plumbing

Resources that get `search` (Stripe API support): `customer`, `charge`, `payment-intent`, `refund`, `subscription`, `invoice`, `product`, `price`. (Stripe does **not** offer Search on `dispute`, `event`, `balance`, `payout`, `account` — those stay list-only and `usage` should say so explicitly to head off agent confusion.)

- New file `internal/stripe/search.go` (or extend `readonly.go`): a single `Search[T]` helper that takes the typed SDK search method, the query string, and pagination flags. Each command package's `runSearch` is a one-liner that calls into it. Goal: query/pagination/envelope code exists once, not eight times.
- Per-command surface: `<resource> search --query <q> [--limit N] [--starting-after CUR] [--page <token>]`. Stripe Search uses an opaque `page` token, **not** `starting_after` — important difference from `list`. Surface as `--page`; document the distinction in each command's `usage`.
- Envelope: same `Page` struct (`hasMore`, `nextCursor`, `count`), but `nextCursor` carries Stripe's `next_page` token. Document that `search`'s cursor is **not** interchangeable with `list`'s — agents that try to swap one for the other get a Stripe error, not a silent mis-paginate.
- `usage` per command must call out: (a) eventual consistency (~1min lag — search for an object you just created and you'll get nothing), (b) query syntax pointer to Stripe's search-query-language docs, (c) one canonical example that's useful for the resource (e.g. `customer search --query 'email:"alice@example.com"'`, `charge search --query 'amount>5000 AND status:"succeeded"'`).
- Top-level top-level `usage` groups commands by capability; `search` should appear as a column in the per-resource summary, not as its own top-level command.

### 2. `--stream` — NDJSON for `list` and `search`

Behavior per PLAN.md §Output:

- First line is the envelope minus `data`, plus `"stream": true`: `{"mode":"test","account":"…","apiVersion":"…","stream":true}`. No trailing `page`/`scan` siblings on the header — those would lie because we haven't paginated yet.
- One JSON object per record after that. **Just the record**, not wrapped in `{data: …}` — keeps per-line overhead minimal. The header's `stream:true` is enough to tell the reader the rest of the file is records.
- Flush after every record so `| head -5` works without a multi-MB buffer.
- Clean SIGPIPE handling: when the reader closes (`| head`), exit 0 quietly, not with a write error.
- Ctrl-C: exit non-zero, emit `{"error":"interrupted"}` to stderr.
- Pagination: walks Stripe's cursor (`starting_after` for `list`, `next_page` for `search`) until exhausted. No hard cap by default — `--stream` is the opt-in for "I really want all of it". `event list --related <id>` keeps its `list.maxEventScan` cap even under `--stream` because that one's a client-side filter, not a pagination limit; on stream exhaustion of the scan budget, emit a final `{"_truncated":true,"scanned":N,"matched":M}` line so the agent knows.
- Mutual exclusion with `--limit` and `--starting-after`: under `--stream` both still work — `--limit` becomes a hard stop (handy for testing the streaming path against a real account), `--starting-after` becomes a resume point. Document this.

New file: `internal/output/stream.go` — `NewStreamWriter(w io.Writer, env Envelope) *StreamWriter` with `.Write(record any) error` and `.Close(page *Page) error`. The `Close` call emits the trailing truncation/scan summary line if relevant; for normal `list`/`search` it's a no-op. Render-time truncation/expand options apply per record the same way they apply in non-stream mode — same `Render()` path, just per-record instead of once.

### 3. `resource describe <name>` — schema discovery

New top-level command. Subcommand-less by design (`agent-stripe resource describe customer`, not `agent-stripe customer describe`) — it's a *meta* command, doesn't belong inside any one resource package.

- New package `internal/commands/resource/` with `describe.go` and `resource.go` (the dispatcher).
- Strategy: registry maps resource name → zero-value SDK struct (`stripeapi.Customer{}`, `stripeapi.Charge{}`, …). Use `reflect` to walk the struct, emit a tree of `{field, type, nullable, repeated, docHint}`. `docHint` comes from the `json:` tag plus a short hand-curated note for the high-signal fields (do **not** try to scrape the SDK's Go doc comments — they're inconsistent and we'd inherit the rot).
- Output is the envelope as usual, with `data: { resource: "customer", fields: [...], expandPaths: [...] }`. `expandPaths` is the hand-curated list of paths each resource's `usage` already mentions for `--expand-stripe` — having it machine-readable here means an agent can ask "what can I expand on a subscription?" without parsing prose.
- **Critical constraint**: this command must not make a Stripe API call. The whole point is "learn the shape for free". `NoAccountNeeded` for `resource` in the dispatcher.
- Field-tree depth cap: 3 levels by default, `--depth N` to override. `Customer.Address` is one level; `Subscription.Items.Data[].Price.Product` is four and explodes the output. Default 3 keeps the common case readable.
- Out of scope for v1: emitting full JSON Schema, OpenAPI shapes, or anything that round-trips to validation. We're producing a hint tree for an LLM, not a schema for codegen.

### 4. Per-field truncation override — the tripwire

Phase 3 resolved this as deferred to Phase 4. Pull the trigger here. The shape decided in Phase 3 §5:

- `Options{ Full bool, Expand []string, ExpandPaths []string }` — `ExpandPaths` accepts dotted paths like `lines.data.description` or `outcome.seller_message`.
- The existing `--expand` flag becomes path-aware: a value with a `.` in it routes to `ExpandPaths`, a bare identifier stays in `Expand` (field-name match anywhere in the tree). Backwards compatible — every existing `--expand customer_id` invocation keeps working.
- Renderer in `internal/output/json.go` tracks current path during traversal; on a string field about to be truncated, check (a) full-tree expand-all match, (b) any `Expand` entry equal to the leaf name, (c) any `ExpandPaths` entry equal to the current dotted path. Glob matches are out of scope for v1 — exact path or leaf name only.
- Tests: pin the Phase 2 dispute regression still passes; add a new test for `invoice get --expand lines.data.description` skipping truncation only on that path. Search-result rendering rides this same code path — verifying once covers both.

This must land **before** `search`, not after. Search results are where long-form free-text fields (charge `outcome.seller_message`, dispute `evidence.*`, invoice line descriptions) actually show up at scale; shipping search first means agents see truncated garbage and learn to always pass `--full`, which defeats the whole point of the envelope's per-resource shaping.

### 5. `SKILL.md` — Claude Code skill packaging

Mirror agent-mongo's structure (see PLAN.md decision #11). Concretely:

- `SKILL.md` at repo root. Frontmatter: `name: agent-stripe`, `description:` one-liner that emphasizes **read-only** and **read-only** again (LLMs route based on this string — if "read-only" isn't in there twice, the model will hesitate on Stripe questions for fear of side effects).
- Body sections: When to invoke (concrete trigger phrases — "stripe customer", "what charges did X have", "why did this subscription not renew"); Command surface (table or compact tree); The read-only guarantee (one paragraph naming the chokepoint at `internal/stripe/readonly.go`); Common workflows (the reconciliation flow from the Phase 3 README, the "what happened to this object" event-history flow from Phase 1, an `event list --related` example).
- Do **not** list every flag in `SKILL.md`. Point at `agent-stripe <command> usage` and let the CLI's own help text be the source of truth. SKILL.md rots fast if it duplicates flag-level detail.
- Distribution: `npx skills add simonperryman/agent-stripe` per PLAN.md decision #11. Verify the npx command actually resolves against this repo's `SKILL.md` location; if it expects a subdirectory layout, fix the layout here, not in the SKILL.

### 6. Homebrew tap

- New repo: `simonperryman/homebrew-tap` (the repo name *must* be `homebrew-tap` — `brew tap simonperryman/tap` is sugar for `simonperryman/homebrew-tap`).
- Formula: `Formula/agent-stripe.rb` with a `bin/agent-stripe` install. Two paths to pick between:
  - **Source build** (`depends_on "go"`): formula compiles from a tagged release tarball. Simpler tap, slower install, requires Go on the user's machine.
  - **Pre-built binaries** (`url`/`sha256` per platform): faster install, no Go dep, but means we need to actually cut releases with binaries attached. Goreleaser is the standard tool — adds a CI step but pays for itself the first time someone non-technical tries to install.
  - Lean: pre-built binaries via Goreleaser. Audience is "agent users", which skews toward "doesn't want to install a Go toolchain just to inspect Stripe".
- `Makefile` already exists — add a `make release` target that wraps Goreleaser, or commit a `.goreleaser.yml` and document `goreleaser release` in CONTRIBUTING.
- Test plan: install fresh in a VM (or fresh shell with `HOMEBREW_NO_AUTO_UPDATE=1`), run `agent-stripe account add test --form` and `agent-stripe account test`, confirm the keychain path works on a non-dev machine.

### 7. README + AGENTS.md updates

- README: flip Phase 4 table rows from ⏳ to ✅ as each lands. Add a "Search vs list" snippet using one resource as an example (lean: `charge` — search query syntax is easier to demo with amounts than with email strings). Document `--stream` with a `| head` example and a `> file.ndjson` example. Add an "Install via Homebrew" block once §6 ships.
- AGENTS.md: add `resource describe` to the discoverability bullets; this is exactly the kind of command an agent needs to know exists without being told.

### 8. Tests

- `commands/<resource>/search_test.go` for each searchable resource: assert `query` and `page` query-string params pass through; one canonical happy-path response.
- `output/stream_test.go`: header line shape, per-record framing, flush behavior, SIGPIPE clean exit (use `io.Pipe` to simulate a closed reader).
- `commands/resource/describe_test.go`: snapshot the field tree for `customer` and `subscription` (subscription specifically exercises the depth cap). Lock the `expandPaths` list per resource.
- `output/json_test.go`: extend with the `ExpandPaths` path-aware cases — `lines.data.description` skip, sibling fields still truncated, depth-mismatch (path `foo.bar` on a tree with no `foo`) is a no-op not an error.
- Integration tests (gated as Phases 2–3): `charge_integration_test.go` gains `runSearch(..., --query 'status:"succeeded"' --limit 1)`. One integration test per searchable resource is overkill; one across all searchable resources via table-driven sub-tests is the right shape.

## Out of scope for Phase 4

- Write operations behind `--confirm` — Phase 5. Phase 4 stays read-only; pulling forward write surface dilutes the "agents explore; humans act" guarantee.
- `invoice create_preview` / `POST`-but-semantically-read endpoints — still gated on the verb-vs-method chokepoint decision. Worth revisiting once `search` is in (search's eventual consistency + preview's "not yet created" are similar agent-explanation shapes), but the chokepoint redesign itself is meatier than it looks: every test in `internal/stripe/readonly_test.go` is verb-shaped, and a per-endpoint allowlist needs a registration site that doesn't yet exist. Park unless someone explicitly asks.
- Connect (`--on-behalf-of` / `Stripe-Account` header) — still v2 per PLAN.md decision #2.
- Webhook tunneling / replay — Stripe CLI's territory, explicit non-goal.
- Globbed `--expand` paths (`lines.data.*.description`) — agents can just pass `--full`. Real-world demand can pull this forward later.

## Open questions

- **`search` cursor naming**: surface Stripe's `next_page` as `nextCursor` (uniform with `list`) or `nextPage` (truthful to the Stripe API)? Lean: `nextCursor` for envelope-shape uniformity, document in `usage` that the underlying token is opaque and Stripe-internal. The agent doesn't care; the agent cares that "pass this back as `--page` to keep going" works.
- **`resource describe` field selection**: emit every field (large output, low-signal fields like `livemode` and `object` listed too) or curate to "fields an agent would care about" (smaller, opinionated, risks omitting something an agent actually wants)? Lean: emit every field but mark `lowSignal: true` on the housekeeping ones (`object`, `livemode`, `created` when the resource has a richer time field). Curate the `expandPaths` list separately — that one is explicitly an opinion, not a reflection.
- **Goreleaser vs hand-rolled cross-compile in Makefile**: do we want the dependency? Counter-argument: the Makefile already does the work, Goreleaser adds a config file and a CI dependency. Lean: Goreleaser. The release-notes-from-commits + GitHub-release-asset-upload is the part we'd otherwise rewrite badly.
- **Tripwire ordering**: §4 says "before §1 (search)". Do we ship §4 in a standalone release first (so it's review-able on its own) or stack it onto §1's PR? Lean: standalone PR for §4 — it touches `internal/output/` which has the most regression risk, and pinning the behavior change separately makes bisecting cleaner.
- **Skill registry / `npx skills add`**: confirm the exact repo layout `npx skills add simonperryman/agent-stripe` expects. Phase 4 needs to either match it or document the discrepancy. (Lookup before writing SKILL.md.)

## Suggested phasing within Phase 4

Order matters here because §4 unblocks §1, §1 unblocks §2's most useful invocations, and §5/§6 only become testable end-to-end once the CLI surface is final.

1. §4 — Per-field truncation overrides. Lands first because it's pure infrastructure with no new commands; review can focus on the renderer.
2. §1 — `search` across all eight searchable resources. Single PR (or stack) once §4 is in.
3. §2 — `--stream` for `list` and `search`. The streaming writer is small but the SIGPIPE/interrupt handling is the kind of code that benefits from being landed alone.
4. §3 — `resource describe`. Independent of §§1–2 in code; sequenced here so it ships after the commands it documents are final.
5. §5 — `SKILL.md`. Needs §§1–3 done so it's not lying about the surface.
6. §6 — Homebrew tap + Goreleaser. Last because it consumes tagged releases of everything above.

## Log

- 2026-05-24 — Drafted plan. Phase 3 closed out cleanly; this is the natural next step. Big open items above are skill-distribution layout (§5/§7) and the Goreleaser-vs-Makefile call (§6). Tripwire ordering decision (§4 → §1) is load-bearing — flagging it here so we don't reflexively start with `search`.
- 2026-05-24 — §4 (per-field truncation overrides) landed first, as planned. `Options.ExpandPaths` is path-aware; bare tokens still match leaf names anywhere for backwards compatibility. `--expand` flag is now token-aware: tokens with a `.` route to `ExpandPaths`, bare tokens stay in `Expand`.
- 2026-05-24 — §1 (search) implemented for the seven Stripe-supported resources: `customer`, `charge`, `payment-intent`, `subscription`, `invoice`, `product`, `price`. **Plan listed `refund` but `stripe-go` v85 does not expose `Refund.Search` — refund is list-only.** Shared plumbing is `cli.ParseSearchFlags` + `agentstripe.CollectRawSearch[T]`. Surface uses `--page` (Stripe's opaque `next_page`), documented per command as NOT interchangeable with `list`'s `--starting-after`.
- 2026-05-24 — §2 (`--stream`) wired into every `list` + `search` (except `event list --related`; see below). Generic helpers `cli.StreamList[T]` / `cli.StreamSearch[T]` in `internal/cli/stream.go`, backed by `agentstripe.StreamRawList[T]` / `StreamRawSearch[T]`. Broken pipe (`| head`) is a clean exit. `--limit` is a hard stop only when the user explicitly set it; otherwise streaming paginates until exhausted. Per-page is fixed at 100 under `--stream` to minimise round-trips. **`event list --related` streaming with truncation summary deferred** — the existing scan-budget code is structurally different from cursor pagination; treating the merge as its own follow-up rather than rushing it in.
- 2026-05-24 — §3 (`resource describe`) added as a new top-level meta-command. `NoAccountNeeded`, pure reflection over `stripe-go` structs, no Stripe call. Output: `{ resource, fields, expandPaths }`. Default depth 3 (`--depth N` to override). `expandPaths` is hand-curated per resource to mirror what each command's `usage` recommends.
- 2026-05-24 — §5 (`SKILL.md`) at repo root. Frontmatter emphasises read-only twice as planned. Command surface is a compact tree; flag-level detail intentionally not duplicated (points at `<command> usage`).
- 2026-05-24 — §6 — `.goreleaser.yml` configured for darwin/linux × amd64/arm64, brew tap target is `simonperryman/homebrew-tap`. `make release` / `make release-snapshot` targets added. Tap repo + first tagged release still need to happen out-of-band.
- 2026-05-24 — §7 README + AGENTS.md flipped to reflect shipped state: customers/payments/billing now ✅ across the board, search and `--stream` documented with examples, `resource describe` called out in Discoverability and AGENTS principles. **Remaining open Phase 4 work**: cutting the first tagged release and publishing the `homebrew-tap` repo; both are out-of-repo.
