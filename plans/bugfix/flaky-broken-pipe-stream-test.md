# Deflake `TestRelatedStream_BrokenPipeNoSummary`

Status: done

## Goal

`internal/commands/event`'s broken-pipe test fails intermittently in CI and
passes everywhere else. Observed on PR #8, which touches `event.go` only to
swap one helper call for an equivalent one; a re-run of the identical commit
went green.

    --- FAIL: TestRelatedStream_BrokenPipeNoSummary (0.02s)
        event_test.go:253: expected only header captured before close, got 2 lines

200 local runs under `-race` on both `main` and the PR branch did not
reproduce it. It needs a machine where the writer wins the race — which
`GOMAXPROCS=1` guarantees:

    GOMAXPROCS=1 go test -race -count=30 \
      -run TestRelatedStream_BrokenPipeNoSummary ./internal/commands/event/
    --- FAIL: expected only header captured before close, got 26 lines

So it is deterministic, not mysterious: the default parallel run just happens
to schedule the reader first almost every time.

## Cause

The test swaps `os.Stdout` for a pipe, then reads it from a goroutine:

    buf := make([]byte, 4096)
    n, _ := pr.Read(buf)          // "read once for the header"
    _ = pr.Close()

and asserts the captured bytes are exactly one line.

Nothing serialises that read against the writer. A single `Read` into a
4096-byte buffer returns *whatever happens to be buffered*, not one line: if
the streaming loop writes the header and the first record before the reader
goroutine is scheduled, one `Read` returns both and the assertion fails. Two
lines is a legal outcome of the code under test, so the test is wrong, not the
code.

## The second problem

The name promises more than the test can deliver. It closes the reader
immediately after the header, so a summary line could never have arrived
regardless of what the code did — the assertion was a proxy that usually
passed rather than a check of summary suppression. Once the reader is closed
nothing further is readable, so "no summary" is not observable from this
vantage point at all.

What *is* observable, and what actually matters, is the contract a consumer
like `| head -1` depends on: the broken pipe is swallowed and `runList`
returns nil rather than an error. The absence of the summary follows
structurally — `runRelatedStream` returns at the failed `Write`, before
`WriteSummary` is reached.

## Plan

1. Read one delimited line with `bufio.Reader.ReadString('\n')` instead of a
   bare `Read`, making what the consumer saw deterministic regardless of
   scheduling.
2. Assert the observable contract: `runList` returns nil, and the line the
   consumer got is the stream header.
3. Rename to `TestRelatedStream_BrokenPipeExitsCleanly` and comment why the
   "no summary" property is not checkable here, so the next reader does not
   reintroduce the proxy assertion.

## Out of scope

Making summary suppression directly testable. It would mean injecting the
stream's writer rather than using `os.Stdout`, which is a change to shipped
code for a test's benefit — worth doing only if that path grows more branches.

## Log

- 2026-08-10 — Created plan after CI failed on PR #8, re-ran green on the same
  commit, and 200 local `-race` runs on `main` failed to reproduce.
- 2026-08-10 — Confirmed the diagnosis by reproducing the old failure
  deterministically on `main` under `GOMAXPROCS=1` (26 lines buffered before
  the single `Read`, not 2).
- 2026-08-10 — Fixed as planned. Verified with `go test -race -count=500`,
  and `-count=100` under `GOMAXPROCS=1` — the scheduling shape that fails
  every time before the fix.
