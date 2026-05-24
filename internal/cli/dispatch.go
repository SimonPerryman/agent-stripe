// Package cli wires global flags, account resolution, and dispatch to the
// per-command packages. Each command package exposes Run(ctx, GlobalOpts, args)
// and a Usage string; the dispatcher just routes.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/simonperryman/agent-stripe/internal/config"
	"github.com/simonperryman/agent-stripe/internal/output"
	agentstripe "github.com/simonperryman/agent-stripe/internal/stripe"

	stripeapi "github.com/stripe/stripe-go/v85"
)

// GlobalOpts is the resolved set of cross-command flags + account context.
type GlobalOpts struct {
	AccountAlias string
	Live         bool
	Full         bool
	Expand       []string // bare field names (no dots) — leaf-name match anywhere in the tree
	ExpandPaths  []string // dotted paths (e.g. "lines.data.description") — exact path match
	ExpandStripe []string // paths passed to Stripe API's expand[] (server-side)
	Stream       bool     // NDJSON output: header line + one record per line, paginate until cap
	Timeout      time.Duration

	// Account is populated after resolution; nil for commands that don't
	// require an account (e.g. `account add`, `account list`, `usage`).
	Account *config.Account

	// Client is the configured Stripe client; nil if Account is nil.
	Client *stripeapi.Client
}

// CommandRunner is the contract every command package implements.
type CommandRunner func(ctx context.Context, opts *GlobalOpts, args []string) error

// Registry maps top-level command names to their runner.
type Registry struct {
	Commands        map[string]CommandRunner
	UsageStrings    map[string]string
	NoAccountNeeded map[string]bool // commands that work without a resolved account
}

// Dispatch parses global flags then dispatches to the right command. It does
// not return — calls os.Exit on completion. (Keeps main.go tiny.)
func Dispatch(ctx context.Context, reg *Registry, argv []string) {
	fs := flag.NewFlagSet("agent-stripe", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		account      = fs.String("a", "", "account alias (overrides AGENT_STRIPE_ACCOUNT and config default)")
		live         = fs.Bool("live", false, "allow operations against a live-mode account")
		full         = fs.Bool("full", false, "skip string truncation in output")
		expand       = fs.String("expand", "", "comma-separated fields/paths to skip truncation on; a token with a dot (e.g. lines.data.description) is matched as a path, bare names match any leaf")
		expandStripe = fs.String("expand-stripe", "", "comma-separated Stripe API expand paths (server-side, e.g. customer,latest_charge)")
		stream       = fs.Bool("stream", false, "emit NDJSON: one header line then one record per line; paginates Stripe until exhausted or --limit reached")
		timeout      = fs.Duration("timeout", 30*time.Second, "per-request timeout")
	)
	// Stop parsing at the first non-flag so subcommands can have their own flags.
	if err := parseUntilSubcommand(fs, argv); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printTopUsage(reg)
			os.Exit(0)
		}
		output.Fail(err.Error(), output.FixableByAgent, 2)
	}

	rest := fs.Args()
	if len(rest) == 0 {
		printTopUsage(reg)
		os.Exit(0)
	}

	cmd := rest[0]
	if cmd == "usage" || cmd == "help" || cmd == "-h" || cmd == "--help" {
		printTopUsage(reg)
		os.Exit(0)
	}

	runner, ok := reg.Commands[cmd]
	if !ok {
		output.Fail(fmt.Sprintf("unknown command %q (try `agent-stripe usage`)", cmd), output.FixableByAgent, 2)
	}

	leaves, paths := splitExpand(*expand)
	opts := &GlobalOpts{
		AccountAlias: resolveAccountAlias(*account),
		Live:         *live,
		Full:         *full,
		Expand:       leaves,
		ExpandPaths:  paths,
		ExpandStripe: splitCSV(*expandStripe),
		Stream:       *stream,
		Timeout:      *timeout,
	}

	// Account resolution + live-mode gate happens here, once, for every
	// command that needs it. Commands like `account add` opt out via
	// NoAccountNeeded so they can bootstrap the first alias.
	if !needsAccount(reg, cmd, rest[1:]) {
		if err := runner(ctx, opts, rest[1:]); err != nil {
			output.Fail(err.Error(), output.FixableByAgent, 1)
		}
		os.Exit(0)
	}

	if err := resolveAccount(opts); err != nil {
		output.Fail(err.Error(), output.FixableByHuman, 2)
	}
	if opts.Account.Mode == config.ModeLive && !opts.Live && !liveOverridden(opts.Account) {
		output.Fail(
			fmt.Sprintf("account %q is live-mode; pass --live to confirm", opts.Account.Alias),
			output.FixableByAgent,
			3,
		)
	}

	secret, err := config.GetSecret(opts.Account.KeychainRef)
	if err != nil {
		output.Fail(fmt.Sprintf("reading secret for %q from keychain: %v", opts.Account.Alias, err), output.FixableByHuman, 2)
	}
	opts.Client = agentstripe.NewClient(secret, "", opts.Timeout)

	if err := runner(ctx, opts, rest[1:]); err != nil {
		output.Fail(err.Error(), output.FixableByAgent, 1)
	}
}

// parseUntilSubcommand parses flags up to the first non-flag arg. Stdlib flag
// stops at the first non-flag by default with ContinueOnError, but only if we
// don't pass `-flag value` afterwards — which we won't, by convention.
func parseUntilSubcommand(fs *flag.FlagSet, argv []string) error {
	return fs.Parse(argv)
}

func resolveAccountAlias(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	if env := os.Getenv("AGENT_STRIPE_ACCOUNT"); env != "" {
		return env
	}
	return ""
}

// needsAccount reports whether a command requires a resolved account before
// it runs. `account` subcommands like `add`, `list`, `remove`, `set-default`,
// `usage` work without one; `test` does need one.
func needsAccount(reg *Registry, cmd string, rest []string) bool {
	if reg.NoAccountNeeded[cmd] {
		// The top-level command opts out entirely.
		return false
	}
	if cmd == "account" {
		sub := ""
		if len(rest) > 0 {
			sub = rest[0]
		}
		switch sub {
		case "add", "remove", "list", "set-default", "usage", "help", "":
			return false
		}
	}
	return true
}

func resolveAccount(opts *GlobalOpts) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	alias := opts.AccountAlias
	if alias == "" {
		alias = cfg.DefaultAccount
	}
	if alias == "" {
		return errors.New("no account specified and no default set (try `agent-stripe account add <alias> --default`)")
	}
	acc, ok := cfg.Accounts[alias]
	if !ok {
		return fmt.Errorf("account %q not found (try `agent-stripe account list`)", alias)
	}
	opts.AccountAlias = alias
	opts.Account = &acc
	return nil
}

func liveOverridden(acc *config.Account) bool {
	if acc.RequireLiveFlag == nil {
		return false
	}
	return !*acc.RequireLiveFlag
}

// splitExpand parses the --expand value, routing tokens containing a "." to
// ExpandPaths (exact path match) and bare tokens to Expand (leaf-name match).
// Backwards compatible: existing single-identifier values stay in Expand.
func splitExpand(s string) (leaves, paths []string) {
	for _, tok := range splitCSV(s) {
		if strings.Contains(tok, ".") {
			paths = append(paths, tok)
		} else {
			leaves = append(leaves, tok)
		}
	}
	return leaves, paths
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	return parts
}

func printTopUsage(reg *Registry) {
	var b strings.Builder
	b.WriteString("agent-stripe — read-only Stripe CLI for AI agents\n\n")
	b.WriteString("Usage:\n  agent-stripe [-a ALIAS] [--live] [--full] [--expand FIELDS] [--expand-stripe PATHS] [--stream] [--timeout DUR] <command> [args]\n\n")
	b.WriteString("Commands:\n")
	for name, u := range reg.UsageStrings {
		first := strings.SplitN(u, "\n", 2)[0]
		fmt.Fprintf(&b, "  %-12s %s\n", name, first)
	}
	b.WriteString("\nUse `agent-stripe <command> usage` for command-specific help.\n")
	fmt.Fprint(os.Stderr, b.String())
}
