# Tech Stack

## Language: Go

- Single static binary (~5–15MB), no runtime dependency
- First-party [`stripe-go`](https://github.com/stripe/stripe-go) SDK with pinned API versions
- Fast cold start for repeated agent invocations
- Standard library covers the surface: `net/http`, `encoding/json`, `testing` + `httptest`

## Dependencies

| Concern | Choice | Notes |
|---|---|---|
| Stripe SDK | `github.com/stripe/stripe-go/v85` | Pins Stripe API version `2026-04-22.dahlia`. Set `stripe.APIVersion` explicitly at client init even though the SDK defaults to it — version pin is part of the contract |
| CLI framework | stdlib `flag` + small dispatcher | Cobra's weight isn't justified for a two-level subcommand tree |
| Config store | stdlib `encoding/json` + `os.UserConfigDir()` | Per-OS path: macOS `~/Library/Application Support/agent-stripe/`, Linux `~/.config/agent-stripe/`, Windows `%AppData%\agent-stripe\`. Holds only `{alias, mode, keychain_ref}` |
| Credential store | [`zalando/go-keyring`](https://github.com/zalando/go-keyring) | macOS Keychain / Linux Secret Service / Windows Credential Manager. Secrets never on disk |
| Secret entry | `osascript` / `zenity` / PowerShell via `os/exec` | For `--form` flag — agent driving the CLI never sees the key |
| Tests | stdlib `testing` + `net/http/httptest` | Unit: hand-authored JSON in `testdata/`. Integration: gated by `STRIPE_TEST_KEY`, hits Stripe test mode, skipped by default |

## Build & distribution

- `go build -ldflags="-s -w"` for release binaries
- GitHub Actions matrix: darwin/arm64, darwin/amd64, linux/amd64, linux/arm64
- Homebrew tap: `shhac/tap/agent-stripe`
- Claude Code skill: `npx skills add shhac/agent-stripe`

## Project layout

```
cmd/agent-stripe/        # main entrypoint
internal/
├── cli/                 # arg parsing, dispatch, root command
├── commands/
│   ├── account/
│   ├── customer/
│   ├── charge/
│   └── ...
├── stripe/
│   ├── client.go        # SDK init from resolved account
│   ├── readonly.go      # rejects non-GET at the boundary
│   └── pagination.go    # auto-pagination + stream mode
├── config/
│   ├── store.go         # os.UserConfigDir()/agent-stripe/config.json
│   ├── keyring.go       # go-keyring wrapper for secrets
│   └── schema.go
├── output/
│   ├── json.go          # truncation, expand, pruning
│   └── errors.go
└── usage/               # LLM-friendly docs per command
```
