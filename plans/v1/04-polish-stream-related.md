# Phase 4 — Plan 1: `event list --related` under `--stream`

Status: done
Parent: [04-polish.md](./04-polish.md) §2

## Why this needs its own plan

`--stream` for the other `list`/`search` commands is mechanical: drain the Stripe cursor through `cli.StreamList` / `cli.StreamSearch` and you're done. `event list --related <id>` is the odd one out — it's not a pagination-bounded walk, it's a **client-side scan** over recent events filtered by `data.object.id`, capped by `--max-scan` (default 500). The scan cap is a budget, not a pagination limit, so `--stream` cannot lift it the way it lifts `--limit`. And the response shape today carries a `scan: {scanned, matched, truncated}` sibling on the envelope — that sibling has no natural home on the streaming header (we haven't scanned anything yet) and no natural home on a record line (it's not a record). So the streaming variant needs a third line shape: a trailing summary.

Everything else under `--stream` (bare `event list`, `charge list`, etc.) is already handled by the existing `cli.StreamList` path. This plan is **only** about the `--related` branch.

## Target wire shape

Three line shapes, in order:

1. **Header** — standard `cli.EnvelopeFor(opts)` with `stream:true`. No `page`, no `scan`, no `data`. Identical to every other streamed command.
2. **Records** — one JSON object per matched event, bare (no `{data: …}` wrapper). Identical to every other streamed command. Rendered through `cli.RenderForStream` so `--full` / `--expand` / `--expand-stripe` apply.
3. **Summary** — exactly one trailing line: `{"_truncated": bool, "scanned": N, "matched": M}`.

The leading underscore on `_truncated` is the cheap signal that this line isn't an event record. Every Stripe event has a top-level `id`; the summary line doesn't. An agent that reads NDJSON and switches on "does this line have an `id`?" will skip the summary naturally. We considered wrapping it as `{"scan": {…}}` to mirror the non-stream envelope's `scan` sibling — rejected, because the non-stream `scan` is part of the envelope, not a record, and conflating the two on a record line creates a worse parsing surface (now there are *two* legitimate non-record line shapes if we ever stream another scan-style command).

## Flag semantics under `--stream`

- `--max-scan` still applies. It's a client-side budget against the user's API quota and patience; `--stream` has no reason to lift it. Default stays 500.
- `--limit` becomes a hard stop on **matched** count, but only when explicitly set (matching the convention `cli.LimitExplicit` already encodes for list/search). If not set, no cap on matched — only the scan budget bounds things.
- `--type` / `--created-gt` / `--created-lt` keep their current meanings; they go into the Stripe `EventListParams` and reduce the volume the scan walks through.

Termination conditions, in order of check inside the loop:

1. `scanned >= maxScan` → `truncated=true`, stop.
2. `limitExplicit && matched >= limit` → `truncated=true`, stop. (Treating "hit the cap" as truncation matches the non-stream behaviour at `event.go:122`.)
3. Iterator exhausted → `truncated=false`, stop.

In all three cases, emit the summary line before returning.

## Code changes

### `internal/output/json.go`

Add one method on `Streamer`:

```go
// WriteSummary emits a single trailing object — used by commands whose stream
// has a tail with shape distinct from the records (e.g. event --related's
// scan summary). Same broken-pipe semantics as Write.
func (s *Streamer) WriteSummary(summary any) error { … }
```

Implementation is identical to `Write` — it's a separate method only so the call site reads as intent, and so a future change (e.g. a trailing newline, or a sentinel marker) lands in one place. No state machine, no "summary already written" guard; the caller is responsible for calling it once.

### `internal/commands/event/event.go`

`runList` already branches `if *related != "" { return runRelated(...) }`. Add a stream branch one level deeper:

```go
if *related != "" {
    if opts.Stream {
        return runRelatedStream(ctx, opts, list, *related, *maxScan, *limit, cli.LimitExplicit(fs))
    }
    return runRelated(ctx, opts, list, *related, *maxScan, *limit)
}
```

The bare `event list` path (no `--related`) inside `runList` gets the same `if opts.Stream` treatment the other commands have — a one-liner into `cli.StreamList`. No scan logic involved.

`runRelatedStream` mirrors `runRelated` but with a `Streamer` instead of buffered `matched` slice:

```go
func runRelatedStream(ctx context.Context, opts *cli.GlobalOpts, list *stripeapi.V1List[*stripeapi.Event], related string, maxScan, limit int, limitExplicit bool) error {
    streamer, err := output.NewStreamer(os.Stdout, cli.EnvelopeFor(opts))
    if err != nil {
        if output.IsBrokenPipe(err) { return nil }
        return err
    }
    scanned, matched := 0, 0
    truncated := false
    for evt, iterErr := range list.All(ctx) {
        if iterErr != nil { return iterErr }
        scanned++
        m, err := agentstripe.ToRawMap(evt)
        if err != nil { return err }
        if eventMatchesObject(m, related) {
            rendered, rErr := cli.RenderForStream(m, opts)
            if rErr != nil { return rErr }
            if wErr := streamer.Write(rendered); wErr != nil {
                if output.IsBrokenPipe(wErr) { return nil } // no summary on broken pipe
                return wErr
            }
            matched++
            if limitExplicit && matched >= limit { truncated = true; break }
        }
        if scanned >= maxScan { truncated = true; break }
    }
    if err := streamer.WriteSummary(map[string]any{
        "_truncated": truncated, "scanned": scanned, "matched": matched,
    }); err != nil {
        if output.IsBrokenPipe(err) { return nil }
        return err
    }
    return nil
}
```

Key behaviours worth being explicit about:

- **Broken pipe mid-stream returns clean (no summary)**. The consumer is gone; trying to write a summary line to a closed pipe is pointless and would just be another broken-pipe error to swallow. This matches what `| head -5` users expect.
- **Iterator error returns the error**. Don't try to emit a partial summary — the envelope contract is "if you got the header, you'll get records and a summary or a broken-pipe-clean-exit." Adding a third failure mode (header + records + error-without-summary) is worse than letting the process exit with a non-zero status and a stderr message.
- **`agentstripe.ToRawMap` happens before the match check**, same as the non-stream path. Necessary because the match itself reads `data.object.id` off the map. We don't render unmatched events — that'd be a waste of CPU on every event in the scan budget.

### `internal/cli/stream.go`

No changes needed. `EnvelopeFor` and `RenderForStream` already do what we want; the scan loop is event-specific enough that pushing it behind a generic helper would just create a one-caller abstraction.

## Tests

In `internal/commands/event/event_test.go` (table-driven, using `httptest`-backed Stripe client fixtures already used elsewhere):

1. **`scan_budget_exhausted`** — 600 fake events, none match, `--max-scan=100`, `--stream`. Expect 1 header line, 0 records, 1 summary with `_truncated:true, scanned:100, matched:0`.
2. **`all_matched_within_budget`** — 50 events, 3 match the target id, `--max-scan=500`, `--stream`. Expect header + 3 records + summary with `_truncated:false, scanned:50, matched:3`.
3. **`limit_hard_stop_under_stream`** — 200 events, all match, `--limit=5 --stream`. Expect header + 5 records + summary with `_truncated:true, scanned:5, matched:5`. (Note `scanned==matched` here because we break out the moment matched hits limit — we don't scan further just to update the counter.) **Implementation gotcha**: this assertion only holds if the loop body is ordered `scanned++ → match → if match { write; matched++; check limit }`. Inverting to "check limit before match" would make `scanned==matched+1` at break time and silently break this test. Eyeball the loop once written.
4. **`broken_pipe_mid_stream`** — write to an `io.Pipe` whose reader closes after the header. Expect `runRelatedStream` to return nil; explicitly assert **no summary line was written**. This is the test that locks in the "broken pipe → no summary" decision so it doesn't silently regress.
5. **`stream_header_shape`** — header line has `stream:true`, no `data`, no `page`, no `scan`. Mostly already covered by `output/stream_test.go`'s `TestStreamerHeaderShape`, but worth re-asserting from the event command's perspective in case someone later threads scan state into the header.

For the broken-pipe test, the existing `output.IsBrokenPipe` plumbing means we can use `io.Pipe` and close the reader; `Streamer.Write` will surface the sentinel and we just need to assert the function returns nil and the captured output ends at the header.

## Docs

`event` usage string at `event.go:28` gets one new line under `list`:

```
                                            With --stream: emits header line,
                                            one event per line, then a final
                                            {"_truncated":bool,"scanned":N,"matched":M}
                                            line. --max-scan still applies;
                                            --limit caps matched count if set.
```

`SKILL.md` gets a 2-line note under the existing `event list --related` example calling out the trailing-summary shape — this is the one command where parsing the stream requires more than "read JSON lines until EOF." **Land in the same commit as the code change** so SKILL.md doesn't go stale in the window between code landing and a reader hitting the surprise.

## Out of scope

- Streaming any other `scan`-style command. There isn't another one today; if one shows up, the `WriteSummary` primitive is ready and the shape decision (`_`-prefixed object) becomes precedent.
- Changing the non-stream `scan: {…}` envelope shape. It stays exactly as-is.
- Lifting `--max-scan` under `--stream`. Explicitly rejected above.
- Cursor-based resumption of a partial scan. Out of scope for v1 entirely.

## Open questions resolved

- **Summary shape** — `_`-prefixed object, not `{"scan": {…}}` wrapper. Reasoning above.
- **Summary on broken pipe** — no. Reasoning above.
- **Summary on iterator error** — no, return the error. Reasoning above.
- **`--limit` semantics** — hard stop on matched, only when explicitly set. Matches `cli.LimitExplicit` convention used by every other streamed command.
