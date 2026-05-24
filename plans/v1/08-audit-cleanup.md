# Phase 8 — Cleanup grab-bag

Status: done

## Goal

Four small, independent fixes the audit surfaced. None individually justifies its own plan, and bundling them avoids four trivial PRs that each need their own review cycle. Each item below is independently revertible — landing them in one branch is purely for momentum, not because they share code paths.

## Plan

### 1. `pacedEmit` concurrency contract

`internal/cli/stream.go:106-124` returns a closure that mutates a captured `next time.Time` with no synchronisation. Today it's only ever called serially from `drain` (line 79), so the lack of a mutex is fine — but there's no comment saying so, which leaves a footgun for whoever next plumbs streaming through a goroutine.

Two acceptable fixes, in order of preference:

**Option A (recommended): doc comment + structured pacer.** Promote the closure to a small struct so the constraint is visible at the type level:

```go
// pacer enforces a minimum interval between emit calls. Not goroutine-safe;
// the streaming loop in drain is single-goroutine by design.
type pacer struct {
    interval time.Duration
    next     time.Time
}

func (p *pacer) emit(record any, w io.Writer) error { /* ... */ }
```

**Option B (minimal): one-line `// not goroutine-safe; caller must serialise` above the existing closure.** Cheaper, but readers have to take the comment on faith. Prefer A.

### 2. `knownResources()` uses bubble sort

`internal/commands/resource/describe.go:122-136` hand-rolls an `O(n²)` sort over 13 strings. Replace with:

```go
import "slices"
// ...
names := make([]string, 0, len(registry))
for name := range registry { names = append(names, name) }
slices.Sort(names)
return names
```

Performance is irrelevant at n=13; this is a readability fix. Trivial review.

### 3. `DeriveMode` magic-number slicing

`internal/config/store.go:118-124`:

```go
case len(key) > 8 && key[:8] == "sk_test_":
```

becomes:

```go
case strings.HasPrefix(key, "sk_test_"):
```

The length guard is redundant — `HasPrefix` handles short strings correctly. Same change for the `sk_live_` branch. Drop the magic `8`.

### 4. `IsBrokenPipe` vs `isBrokenPipe` — disambiguate

`internal/output/json.go` exports `IsBrokenPipe` (uses `errors.Is`, the right portable check) and keeps a private `isBrokenPipe` that string-matches `"broken pipe"` on the error message. They have nearly-identical names and serve different purposes. Two issues to fix:

**a. Replace the string-match fallback with `errors.Is(err, syscall.EPIPE)`.** Both functions then have the same underlying mechanism; the private one is just a convenience wrapper. The `*os.PathError` type-assertion branch can stay as a fast path but the `strings.Contains` line goes away — string-matching OS error messages is fragile and breaks on locale changes / OS variants.

**b. Rename for clarity.** Either:

- Inline the private `isBrokenPipe` into its single caller (preferred if it's only used once — check with `grep -r 'isBrokenPipe' internal/output/`), or
- Rename the private one to something that describes what it does specifically (e.g. `isWriteEPIPE`) so the distinction from the exported `IsBrokenPipe` is obvious.

After this, the `syscall` import is doing the work, and both functions agree on the semantics of "broken pipe".

## Resolved decisions

- **Why bundle these into one plan instead of one each?** None individually justifies the overhead of a plan doc + PR description + review cycle. They share a theme ("audit cleanup") and a maintainer (anyone touching the codebase). Reviewer can look at the diff in 10 minutes total.

- **Why not skip the cleanup entirely and just live with it?** Each of these is a small drag on every future reader of the affected file. The bubble sort especially — anyone reading `describe.go` for the first time spends a minute wondering whether it's intentional. The fix is a one-liner; the question goes away forever.

## Out of scope

- Wider audit of the codebase for similar smells. The four items here are the ones the current audit surfaced; further sweeps are a separate exercise.
- Changing `IsBrokenPipe`'s exported signature — it's used by callers we can't see, so we leave it alone.

## Checks

- `go test ./...` passes.
- `agent-stripe customer list | head -5` exits 0, no error on stderr (validates the EPIPE handling still works after #4).
- `agent-stripe resource describe nonexistent` produces a sorted list of valid resource names (validates #2).
- `agent-stripe account add test --key sk_test_123` resolves to test mode; `sk_live_` to live (validates #3).
