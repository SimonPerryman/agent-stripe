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
	"regexp"
	"strings"
	"time"

	"github.com/simonperryman/agent-stripe/internal/config"
	"github.com/simonperryman/agent-stripe/internal/output"
	agentstripe "github.com/simonperryman/agent-stripe/internal/stripe"

	stripeapi "github.com/stripe/stripe-go/v85"
)

// GlobalOpts is the resolved set of cross-command flags + account context.
//
// Two different "accounts" live in this struct and they answer different
// questions: AccountAlias selects *which credential* the CLI authenticates
// with (a local keychain alias), while StripeAccount selects *whose books
// that credential reads* (a connected account, via the Stripe-Account header).
type GlobalOpts struct {
	AccountAlias string
	// StripeAccount is a connected account id (acct_...). Empty means the
	// platform account that owns the API key.
	StripeAccount string
	Live          bool
	Full          bool
	Expand        []string // bare field names (no dots) — leaf-name match anywhere in the tree
	ExpandPaths   []string // dotted paths (e.g. "lines.data.description") — exact path match
	ExpandStripe  []string // paths passed to Stripe API's expand[] (server-side)
	Stream        bool     // NDJSON output: header line + one record per line, paginate until cap
	RateLimit     float64  // requests/sec ceiling on --stream pacing; 0 = unlimited
	// Raw emits the response body Stripe sent instead of the marshalled SDK
	// struct. Set directly by --raw, and implied by APIVersion.
	Raw bool
	// APIVersion overrides the Stripe-Version header on every request. Empty
	// means the SDK's pinned version.
	APIVersion string
	Timeout    time.Duration

	// Account is populated after resolution; nil for commands that don't
	// require an account (e.g. `account add`, `account list`, `usage`).
	Account *config.Account

	// Client is the configured Stripe client; nil if Account is nil.
	Client *stripeapi.Client
}

// CommandRunner is the contract every command package implements.
type CommandRunner func(ctx context.Context, opts *GlobalOpts, args []string) error

// CommandSpec bundles everything the dispatcher needs to know about a top-level
// command. One entry per command — no parallel maps to drift out of sync.
type CommandSpec struct {
	Run       CommandRunner
	Usage     string
	NoAccount bool // command works without a resolved Stripe account
}

// Registry maps top-level command names to their spec.
type Registry struct {
	Commands map[string]CommandSpec
}

// globalFlags holds the parsed values of the cross-command flags. It exists so
// the definitions live in exactly one place: tests need to exercise global
// parsing without invoking Dispatch (which calls os.Exit), and the hand-kept
// mirror they used to build instead silently lacked every flag added after it.
type globalFlags struct {
	account       *string
	stripeAccount *string
	live          *bool
	full          *bool
	expand        *string
	expandStripe  *string
	stream        *bool
	rateLimit     *float64
	raw           *bool
	apiVersion    *string
	timeout       *time.Duration
}

func newGlobalFlags() (*flag.FlagSet, *globalFlags) {
	fs := flag.NewFlagSet("agent-stripe", flag.ContinueOnError)
	return fs, &globalFlags{
		account:       fs.String("account", "", "account alias (overrides AGENT_STRIPE_ACCOUNT and config default)"),
		stripeAccount: fs.String("stripe-account", "", "read a connected account's data via the Stripe-Account header (Connect platforms only; acct_...)"),
		live:          fs.Bool("live", false, "allow operations against a live-mode account"),
		full:          fs.Bool("full", false, "skip string truncation in output"),
		expand:        fs.String("expand", "", "comma-separated fields/paths to skip truncation on; a token with a dot (e.g. lines.data.description) is matched as a path, bare names match any leaf"),
		expandStripe:  fs.String("expand-stripe", "", "comma-separated Stripe API expand paths (server-side, e.g. customer,latest_charge)"),
		stream:        fs.Bool("stream", false, "emit NDJSON: one header line then one record per line; paginates Stripe until exhausted or --limit reached"),
		rateLimit:     fs.Float64("rate-limit", 15.0, "max Stripe requests/sec under --stream (0 = unlimited; Stripe's account-wide cap is 100/sec live, 25/sec test)"),
		raw:           fs.Bool("raw", false, "emit the JSON Stripe sent instead of the SDK's response struct; shows fields the pinned SDK version cannot model"),
		apiVersion:    fs.String("api-version", "", "request a different Stripe API version (e.g. 2022-11-15); implies --raw"),
		timeout:       fs.Duration("timeout", 30*time.Second, "per-request timeout"),
	}
}

// Dispatch parses global flags then dispatches to the right command. It does
// not return — calls os.Exit on completion. (Keeps main.go tiny.)
func Dispatch(ctx context.Context, reg *Registry, argv []string) {
	fs, g := newGlobalFlags()
	fs.SetOutput(os.Stderr)
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

	spec, ok := reg.Commands[cmd]
	if !ok {
		output.FailWithHint(
			fmt.Sprintf("unknown command %q (try `agent-stripe usage`)", cmd),
			topLevelHint(reg, cmd),
			output.FixableByAgent,
			2,
		)
	}

	// Centralize per-command help so all four forms (usage|help|-h|--help)
	// work uniformly without each command package handling -h/--help. This
	// also guarantees help is always reachable without credentials.
	if len(rest) > 1 && isHelpToken(rest[1]) {
		fmt.Fprintln(os.Stderr, spec.Usage)
		os.Exit(0)
	}

	leaves, paths := splitExpand(*g.expand)
	connected := resolveStripeAccount(*g.stripeAccount)
	// Validate before any client is constructed: passing a cus_/ch_ id here is
	// the obvious agent mistake, and Stripe would answer it with an opaque 403.
	if err := ValidateStripeAccount(connected); err != nil {
		var oe *output.Error
		if errors.As(err, &oe) {
			output.FailWithHint(oe.Msg, oe.Hint, oe.By, 2)
		}
		output.Fail(err.Error(), output.FixableByAgent, 2)
	}
	version := resolveAPIVersion(*g.apiVersion)
	if err := ValidateAPIVersion(version); err != nil {
		failFromCommand(err, output.FixableByAgent, 2)
	}
	opts := &GlobalOpts{
		AccountAlias:  resolveAccountAlias(*g.account),
		StripeAccount: connected,
		Live:          *g.live,
		Full:          *g.full,
		Expand:        leaves,
		ExpandPaths:   paths,
		ExpandStripe:  splitCSV(*g.expandStripe),
		Stream:        *g.stream,
		RateLimit:     *g.rateLimit,
		// An override without raw output would look like it worked while the
		// pinned structs dropped exactly the fields it was asked for, so the
		// flag implies the other rather than offering a broken combination.
		Raw:        *g.raw || version != "",
		APIVersion: version,
		Timeout:    *g.timeout,
	}

	// Account resolution + live-mode gate happens here, once, for every
	// command that needs it. Commands set NoAccount on their spec to opt out
	// so they can bootstrap the first alias.
	if !needsAccount(spec, cmd, rest[1:]) {
		if err := spec.Run(ctx, opts, rest[1:]); err != nil {
			failFromCommand(err, output.FixableByAgent, 1)
		}
		os.Exit(0)
	}

	if err := resolveAccount(opts); err != nil {
		failFromCommand(err, output.FixableByHuman, 2)
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
	opts.Client = agentstripe.NewClient(secret, "", opts.StripeAccount, opts.Timeout, agentstripe.WithAPIVersion(opts.APIVersion))

	if err := spec.Run(ctx, opts, rest[1:]); err != nil {
		var oe *output.Error
		if errors.As(err, &oe) {
			by := oe.By
			if by == "" {
				by = output.FixableByAgent
			}
			output.FailWithHint(oe.Msg, oe.Hint, by, 1)
		}
		output.FailFromStripeError(err, output.FixableByAgent, 1)
	}
}

// failFromCommand routes a command-returned error: a *output.Error sentinel
// becomes a hinted envelope; otherwise it falls through to plain Fail.
func failFromCommand(err error, defaultBy output.FixableBy, code int) {
	var oe *output.Error
	if errors.As(err, &oe) {
		by := oe.By
		if by == "" {
			by = defaultBy
		}
		output.FailWithHint(oe.Msg, oe.Hint, by, code)
	}
	output.Fail(err.Error(), defaultBy, code)
}

// SubcommandHint returns "did you mean X?" if there's a near match in valid,
// otherwise "valid: a, b, c". Subcommand sets are small (2–4 entries) so the
// fallback list fits comfortably on one line.
func SubcommandHint(input string, valid []string) string {
	if match := output.Closest(input, valid); match != "" {
		return fmt.Sprintf("did you mean %q?", match)
	}
	return output.ValidList(valid)
}

// topLevelHint suggests the closest command name from the registry.
func topLevelHint(reg *Registry, cmd string) string {
	names := make([]string, 0, len(reg.Commands))
	for name := range reg.Commands {
		names = append(names, name)
	}
	if match := output.Closest(cmd, names); match != "" {
		return fmt.Sprintf("did you mean %q? run `agent-stripe usage` for the full list", match)
	}
	return ""
}

// parseUntilSubcommand pulls recognized global flags to the front of argv so
// `agent-stripe charge list --limit 5 --stream` is equivalent to passing
// --stream before the subcommand. Unknown flags (subcommand-specific, like
// --limit here) are left in place; fs.Parse then stops at the first non-flag,
// which is the subcommand name.
func parseUntilSubcommand(fs *flag.FlagSet, argv []string) error {
	return fs.Parse(ReorderFlagsFirst(argv, fs))
}

func isHelpToken(s string) bool {
	switch s {
	case "usage", "help", "-h", "--help":
		return true
	}
	return false
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

// resolveStripeAccount mirrors resolveAccountAlias's precedence: flag > env.
//
// The env var is an ambient default, and a stale `export` can rescope a read
// just as a saved config value would — so the envelope's `stripeAccount` echo
// is what makes this safe, not the absence of persistence. It is there for
// parity with AGENT_STRIPE_ACCOUNT and the project's stated preference for
// explicit env config over implicit detection.
//
// There is deliberately no *config-file* default, which is a different thing:
// config is edited once and read forever by every future invocation, including
// ones whose author never saw the setting. An `export` at least lives and dies
// with the shell whose transcript shows it.
func resolveStripeAccount(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	return os.Getenv("AGENT_STRIPE_STRIPE_ACCOUNT")
}

// stripeAccountPattern is Stripe's connected-account id shape. Anything else
// (a cus_, ch_, or a bare alias) is an agent mistake worth catching locally.
var stripeAccountPattern = regexp.MustCompile(`^acct_[A-Za-z0-9]+$`)

// ValidateStripeAccount rejects a --stripe-account value that is not an
// acct_ id. Empty is valid — it means "the platform account".
func ValidateStripeAccount(v string) error {
	if v == "" || stripeAccountPattern.MatchString(v) {
		return nil
	}
	return &output.Error{
		Msg:  fmt.Sprintf("invalid --stripe-account %q: expected a connected account id starting with \"acct_\"", v),
		Hint: "--stripe-account takes an acct_... id (find one via `transfer list` → destination, or `connected-account list`); use --account for a local key alias",
		By:   output.FixableByAgent,
	}
}

// resolveAPIVersion mirrors resolveStripeAccount's precedence: flag > env,
// and deliberately no config-file default. The reasoning is the same, only
// sharper: a saved version would silently change the shape of *every* field
// of every response for every future invocation, including ones whose author
// never saw the setting. The envelope's apiVersion echo is what makes the env
// var tolerable.
func resolveAPIVersion(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	return os.Getenv("AGENT_STRIPE_API_VERSION")
}

// apiVersionPattern is Stripe's release-version shape: a date, optionally
// suffixed with the release name (e.g. "2026-04-22.dahlia").
var apiVersionPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}(\.[a-z0-9_]+)?$`)

// ValidateAPIVersion rejects a --api-version value that is not shaped like a
// Stripe version. Empty is valid — it means the pinned version.
//
// Stripe answers an unknown version with a 400 whose message does not say
// which parameter was wrong, so a typo like "2022-11-5" would come back as an
// opaque failure. Checking the shape locally turns that into something an
// agent can fix from the error alone. The *existence* of a version is still
// Stripe's call — we only check the shape.
func ValidateAPIVersion(v string) error {
	if v == "" || apiVersionPattern.MatchString(v) {
		return nil
	}
	return &output.Error{
		Msg:  fmt.Sprintf("invalid --api-version %q: expected a Stripe version date like \"2022-11-15\", optionally with a release suffix like \"2026-04-22.dahlia\"", v),
		Hint: "--api-version takes a dated Stripe release; see https://docs.stripe.com/upgrades#api-versions. Omit it to use the version this CLI is built against",
		By:   output.FixableByAgent,
	}
}

// needsAccount reports whether a command requires a resolved account before
// it runs. `account` subcommands like `add`, `list`, `remove`, `set-default`,
// `usage` work without one; `test` does need one.
func needsAccount(spec CommandSpec, cmd string, rest []string) bool {
	if spec.NoAccount {
		// The top-level command opts out entirely.
		return false
	}
	// Per-command help is always reachable without credentials so an agent
	// encountering the tool fresh can discover the surface area before being
	// asked to configure a Stripe account.
	if len(rest) > 0 && isHelpToken(rest[0]) {
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
		return &output.Error{
			Msg:  fmt.Sprintf("account %q not found (try `agent-stripe account list`)", alias),
			Hint: AliasHint(alias, cfg.Accounts),
			By:   output.FixableByHuman,
		}
	}
	opts.AccountAlias = alias
	opts.Account = &acc
	return nil
}

// AliasHint suggests the closest known alias, or falls back to a short list.
// Exported so account-subcommand handlers can reuse the same phrasing.
func AliasHint(alias string, accounts map[string]config.Account) string {
	names := make([]string, 0, len(accounts))
	for k := range accounts {
		names = append(names, k)
	}
	if match := output.Closest(alias, names); match != "" {
		return fmt.Sprintf("did you mean %q? run `agent-stripe account list` for all accounts", match)
	}
	if len(names) == 0 {
		return "no accounts configured — run `agent-stripe account add <alias>` to add one"
	}
	return output.ValidList(names) + " (or run `agent-stripe account list`)"
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
	b.WriteString("Usage:\n  agent-stripe [--account ALIAS] [--stripe-account acct_...] [--live] [--full] [--expand FIELDS] [--expand-stripe PATHS] [--raw] [--api-version DATE] [--stream] [--rate-limit N] [--timeout DUR] <command> [args]\n\n")
	b.WriteString("Flags are long-form only (no short aliases): use --account, not -a.\n\n")
	b.WriteString("--raw emits the JSON Stripe sent instead of the SDK's response struct. Output\nis normally marshalled through structs pinned to " + agentstripe.PinnedAPIVersion + ", which silently\ndrop any field that version does not model — no error, indistinguishable from\nStripe not sending it. Reach for --raw when a field you expect is missing.\n\n")
	b.WriteString("--api-version requests a different version (e.g. --api-version 2022-11-15) and\nimplies --raw. Use it to see what a consumer on an older version receives —\nwebhook endpoints pin their own version independently of this CLI. The\nenvelope's apiVersion reports the version actually requested.\n\n")
	b.WriteString("--expand-stripe paths are relative to the object. On a *list* command they\nneed a \"data.\" prefix — `--expand-stripe data.customer`, not `customer` —\nbecause Stripe expands relative to the list wrapper.\n\n")
	b.WriteString("--account picks which credential to use; --stripe-account picks whose books\nthat credential reads. Objects on a Connect direct charge live on the connected\naccount and are invisible without --stripe-account; destination charges live on\nthe platform and need no flag.\n\n")
	b.WriteString("Commands:\n")
	for name, spec := range reg.Commands {
		first := strings.SplitN(spec.Usage, "\n", 2)[0]
		fmt.Fprintf(&b, "  %-12s %s\n", name, first)
	}
	b.WriteString("\nHelp: `agent-stripe <command> usage` (also: help, -h, --help) for command-specific help.\n")
	fmt.Fprint(os.Stderr, b.String())
}

// RejectAPIVersion returns an error when --api-version is set on a command
// that cannot honour it.
//
// `resource describe` is the case: it reflects over the pinned SDK's structs
// and never calls Stripe, so it can only ever describe one version's shape.
// Answering a request for another version with the pinned tree — and an
// envelope echoing a version the output does not represent — would be a
// silent wrong answer to precisely the question being asked.
func RejectAPIVersion(opts *GlobalOpts, command string) error {
	if opts == nil || opts.APIVersion == "" {
		return nil
	}
	return &output.Error{
		Msg: fmt.Sprintf(
			"%s cannot describe API version %s: it reflects over the SDK structs this CLI is built against (%s) and makes no request",
			command, opts.APIVersion, agentstripe.PinnedAPIVersion),
		Hint: "drop --api-version here; to see another version's actual field set, request a real object with --api-version (which implies --raw)",
		By:   output.FixableByAgent,
	}
}

// RejectStripeAccount returns an error when --stripe-account is set on a
// command that is platform-scoped by nature.
//
// The header is injected unconditionally at the transport, so without this
// guard `application-fee list --stripe-account acct_x` quietly answers from
// the connected account's books and returns an empty list — from which an
// agent concludes the platform earns no fees, rather than that it asked the
// wrong account. A silent wrong answer is worse than an error.
func RejectStripeAccount(opts *GlobalOpts, command string) error {
	if opts == nil || opts.StripeAccount == "" {
		return nil
	}
	return &output.Error{
		Msg: fmt.Sprintf(
			"%s is platform-scoped; --stripe-account (%s) would read the connected account's books and return an empty or misleading result",
			command, opts.StripeAccount),
		Hint: "drop --stripe-account here; use it on commands that read a connected account's own objects (charge, balance, payout, event…)",
		By:   output.FixableByAgent,
	}
}
