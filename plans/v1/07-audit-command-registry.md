# Phase 7 — Unified command registry

Status: done

## Goal

`cmd/agent-stripe/main.go:34-72` registers every top-level command three times: once in `Commands` (the runner), once in `UsageStrings` (the help text), and optionally in `NoAccountNeeded` (the auth-bypass flag). All three are `map[string]…` keyed on the same command name, with no compile-time or runtime check that they agree.

Concrete failure modes this enables:

- Add a command to `Commands` but forget `UsageStrings` → `agent-stripe usage` silently omits it. No error. Users only find out when they look for it.
- Typo a key in one of the maps (`"payment-itent"`) → that map silently doesn't apply for that command. `NoAccountNeeded["payment-itent"] = true` would leave `payment-intent` requiring an account; the bug surfaces as a confusing "no account configured" error somewhere unrelated.
- Remove a command from `Commands` but leave its `UsageStrings` entry → orphan help text that references a non-existent command.

This is exactly the kind of bug a struct prevents and a map cannot.

## Plan

### 1. New `CommandSpec` type in `internal/cli/`

Add to `internal/cli/dispatch.go`:

```go
type CommandSpec struct {
    Run         CommandRunner
    Usage       string
    NoAccount   bool // command works without a resolved Stripe account
}

type Registry struct {
    Commands map[string]CommandSpec
}
```

Old `Registry` (three parallel maps) is deleted. `Dispatch` and `printTopUsage` updated to read from the unified spec — the existing code already pulls all three values out of the registry, this is just consolidating the access pattern.

### 2. Migrate `main.go`

```go
reg := &cli.Registry{
    Commands: map[string]cli.CommandSpec{
        "account":        {Run: account.Run, Usage: account.Usage},
        "customer":       {Run: customer.Run, Usage: customer.Usage},
        "event":          {Run: event.Run, Usage: event.Usage},
        "charge":         {Run: charge.Run, Usage: charge.Usage},
        "payment-intent": {Run: paymentintent.Run, Usage: paymentintent.Usage},
        "refund":         {Run: refund.Run, Usage: refund.Usage},
        "dispute":        {Run: dispute.Run, Usage: dispute.Usage},
        "balance":        {Run: balance.Run, Usage: balance.Usage},
        "payout":         {Run: payout.Run, Usage: payout.Usage},
        "transfer":       {Run: transfer.Run, Usage: transfer.Usage},
        "subscription":   {Run: subscription.Run, Usage: subscription.Usage},
        "invoice":        {Run: invoice.Run, Usage: invoice.Usage},
        "product":        {Run: product.Run, Usage: product.Usage},
        "price":          {Run: price.Run, Usage: price.Usage},
        "resource":       {Run: resource.Run, Usage: resource.Usage, NoAccount: true},
    },
}
```

Adding a command now requires exactly one map entry. Forgetting `Usage` produces a literal empty string at help-render time, which is immediately visible the first time anyone runs `agent-stripe usage`. Forgetting `NoAccount` defaults to `false` — the safer default (require an account unless we know the command doesn't need one).

### 3. Optional: a `Register` method that validates

If you want belt-and-braces, add:

```go
func (r *Registry) Register(name string, spec CommandSpec) {
    if spec.Run == nil { panic("command "+name+" has nil Run") }
    if spec.Usage == "" { panic("command "+name+" has empty Usage") }
    r.Commands[name] = spec
}
```

Called at package init in `main.go`. Panics at startup are the right call here — this is a developer mistake, not a runtime condition. Skip this if it feels heavy; the struct alone removes most of the failure modes.

## Resolved decisions

- **Why not use `cobra` and skip all of this?** Cobra would solve this and a half-dozen other things. It's also a chunky dependency that changes how every command's flag parsing works and complicates the static binary story. The fix above is ~30 LOC and zero new deps. If we later decide cobra is worth it, this consolidation is still a prerequisite — we'd want one source of truth for command metadata before mapping it onto cobra's `Command` struct.

- **Why `NoAccount bool` and not `RequiresAccount bool`?** Defaults matter. The vast majority of commands need an account; `NoAccount` defaults to `false` which means "needs an account" — the safer default. `RequiresAccount: false` as a default would mean any forgotten field bypasses auth, which is the wrong direction.

## Out of scope

- Replacing the hand-rolled flag parser with cobra/urfave (separate decision; documented above why we're not bundling it).
- Auto-generating `agent-stripe usage` from command specs in a richer format. Today it just lists names + first line of `Usage`; that's fine.

## Checks

- `go test ./...` passes (no test references `Registry` fields directly — confirm with `grep -r 'NoAccountNeeded\|UsageStrings' internal/`).
- `agent-stripe usage` output diff: byte-identical before/after.
- Manual: `agent-stripe resource describe customer` (no account configured) still works; `agent-stripe customer list` (no account) still produces the existing "configure an account" error.
