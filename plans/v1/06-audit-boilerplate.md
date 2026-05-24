# Phase 6 — Collapse per-resource boilerplate

Status: done

## Goal

The 14 per-resource command packages (`charge`, `customer`, `invoice`, `subscription`, `transfer`, `paymentintent`, `refund`, `payout`, `dispute`, `product`, `price`, `balance`, `event`, `account`) all reach the same handful of terminal calls: `agentstripe.ToRawMap` → `output.Render` → `output.Emit`. Phase 4 extracted streaming helpers (`cli.EnvelopeFor`, `cli.RenderForStream`, `cli.StreamList`, `cli.StreamSearch`) but stopped short of the **non-stream** emit path and the **search flag plumbing**, leaving ~5 identical lines at the tail of every `runGet`, plus a 15-line stream-or-collect branch in every `runList` and `runSearch`.

The audit measured this at roughly 150 LOC of pure duplication across the resource packages, plus a second ~25 LOC of `captureStdout`/`newOpts` duplicated across every `*_test.go`, plus a per-call triple JSON round-trip that exists only because no helper consumes the already-decoded map.

Goal: drive the three duplicated tails into shared helpers in `internal/cli/`, centralise test helpers in a new `internal/testutil` package, and eliminate one of the three JSON round-trips. No behaviour change — every existing integration test must pass byte-for-byte.

What's deliberately **not** in scope: changing the public envelope shape, touching `output.Render` semantics, or moving truncation logic. Those are observed by agents and changing them is a v2 conversation.

## Plan

### 1. Shared emit helpers in `internal/cli/`

New file `internal/cli/emit.go` (or extend `stream.go`) exposing two helpers that replace the trailing 5–7 lines of every command:

```go
// EmitSingle renders one Stripe object as a single-envelope JSON response.
func EmitSingle(opts *GlobalOpts, m map[string]any) error

// EmitList renders a paginated list as a single envelope with Page metadata.
func EmitList(opts *GlobalOpts, items []map[string]any, hasMore bool, nextCursor string) error
```

Both internally call `output.Render` then `output.Emit(os.Stdout, ...)` using `EnvelopeFor(opts)` (already exists). After this lands, the tail of `runGet` collapses from 11 lines (charge.go:75-89) to:

```go
m, err := agentstripe.ToRawMap(c)
if err != nil { return err }
return cli.EmitSingle(opts, m)
```

Mechanical refactor across all 14 packages. Tests should be untouched — they assert envelope contents, not control flow.

### 2. Shared list/search dispatch

`runList` and `runSearch` end with the same shape: "if `opts.Stream`, call `StreamList`/`StreamSearch`; else `CollectRawList`/`CollectRawSearch` + `EmitList`". The streaming branch is already one line via `cli.StreamList`. The non-stream branch should become one line too:

```go
// In internal/cli/list.go (new file):
func RunListOrStream[T any](
    ctx context.Context, opts *GlobalOpts,
    iter iterator[T], cap int, limitExplicit bool,
) error
```

Where `iter` is the typed Stripe iterator (`*stripeapi.Iter[*stripeapi.Charge]` etc.). Internally branches on `opts.Stream` and calls the existing stream helpers or the existing `CollectRawList` + new `EmitList`. Generic over `T` because the iterators are typed and we want to keep type safety at the call site.

If the generic plumbing turns out to be fighting `stripe-go`'s iterator interface (it returns concrete types, not an interface), fall back to two non-generic helpers — `RunListOrStream` and `RunSearchOrStream` — that take `iterator` as `any` and let the caller pass either. The duplication saved is the same; the type-safety story is just slightly weaker. Decide once you've tried the generic version on `charge`.

Per-resource `runList` then drops from ~45 lines (charge.go:91-152) to ~30 lines, with the entire if/else tail gone.

### 3. Skip the redundant JSON round-trip in `Render`

Today every response double-marshals: `agentstripe.ToRawMap(obj)` decodes the SDK struct to `map[string]any` (pagination.go:70-80), then `output.Render` re-marshals that map and re-unmarshals into another tree (json.go:62-68). `Render` only does the second pass to coerce arbitrary input into something it can walk; if the caller is already handing it `map[string]any` or `[]map[string]any` (which all our callers do), the marshal/unmarshal is dead work.

Add an internal fast path:

```go
func Render(data any, opts Options) (any, error) {
    switch v := data.(type) {
    case map[string]any: return walkMap(v, opts), nil
    case []map[string]any: return walkSlice(v, opts), nil
    default: // existing marshal/unmarshal path
    }
}
```

Risks: `walkMap` has to be careful about not mutating the input map (the SDK struct might be reused). Easiest fix is a shallow copy at the top of `walkMap`; the existing path already produces a new tree because `json.Unmarshal` allocates fresh. Add a focused test that asserts the input map is unchanged after `Render`.

Performance is not the motivation — clarity is. One round-trip is easier to reason about than three, and the dropped allocations are a free win.

### 4. Centralise test helpers in `internal/testutil`

`captureStdout` (redirects `os.Stdout`, reads via pipe, restores) and `newOpts` (builds a `*cli.GlobalOpts` with a fake `Account`) are duplicated in every resource test package. They can't be shared via a `_test.go` helper because each test lives in a different package.

New package `internal/testutil/` with:

```go
package testutil

func CaptureStdout(t *testing.T, fn func() error) (string, error)
func NewOpts(t *testing.T, client *stripeapi.Client) *cli.GlobalOpts
```

`CaptureStdout` should take `*testing.T` so it can `t.Helper()` and use `t.Cleanup` to restore `os.Stdout` even on panic — the current per-package copies don't, which is a latent flake risk. `NewOpts` returns a populated `GlobalOpts` with a deterministic account alias (`"test"`) and `Mode: ModeTest`.

Migration is mechanical: delete the duplicated definitions, add `import "internal/testutil"`, replace call sites. ~25 LOC × 14 files = ~350 LOC removed.

## Resolved decisions

- **Why not move `runGet`/`runList`/`runSearch` themselves into a generic dispatcher?** Tried in design. The per-resource flag sets diverge enough (charge has `--customer`, `--payment-intent`, `--created-gt`; invoice has `--customer`, `--status`, `--subscription`; subscription has `--customer`, `--status`, `--price`) that the "shared" function would either take a `map[string]any` of flag values (loses type safety) or a callback that builds the SDK params (no LOC savings vs. today). The tails are duplicated; the bodies aren't. Stop at the tails.

- **Why not keep `CollectRawList` returning the triple round-trip and just live with it?** Because every code reader new to the project asks "why are we marshaling this twice" within their first hour. The fix is small (~20 LOC in `output/json.go`) and removes the question permanently.

## Out of scope

- Changing the envelope JSON shape (an agent-visible contract).
- Replacing the hand-rolled flag parsing with cobra/urfave (separate, larger decision).
- The `pacedEmit` concurrency doc note — covered in plan 08.

## Checks

- `go test ./...` passes.
- Diff the JSON output of `agent-stripe charge get ch_xxx` before and after on a real test account — must be byte-identical.
- LOC: expect ~500 lines deleted, ~80 added across `internal/cli/emit.go`, `internal/cli/list.go`, `internal/output/json.go`, `internal/testutil/`.
