// Package account implements `agent-stripe account ...` — add, remove, list,
// test, set-default. This is the first command package and serves as the
// template for everything in internal/commands/.
package account

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/google/uuid"

	"github.com/simonperryman/agent-stripe/internal/cli"
	"github.com/simonperryman/agent-stripe/internal/config"
	"github.com/simonperryman/agent-stripe/internal/output"
	agentstripe "github.com/simonperryman/agent-stripe/internal/stripe"

	stripeapi "github.com/stripe/stripe-go/v85"
)

const Usage = `account — manage Stripe API keys (stored in the OS keychain)

Subcommands:
  add <alias> [--key SK] [--form] [--default]   Add an account
  remove <alias>                                Remove an account + its keychain entry
  list                                          List accounts (keys are never on disk)
  set-default <alias>                           Set the default account
  test [alias]                                  Hit GET /v1/account to verify the key

Keys must start with sk_test_ or sk_live_. Use --form on macOS for an OS-native
dialog (so the agent driving the CLI never sees the secret).`

// Run dispatches the account subcommand.
func Run(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, Usage)
		return nil
	}
	switch args[0] {
	case "add":
		return runAdd(args[1:])
	case "remove":
		return runRemove(args[1:])
	case "list":
		return runList()
	case "set-default":
		return runSetDefault(args[1:])
	case "test":
		return runTest(ctx, opts, args[1:])
	case "usage", "help":
		fmt.Fprintln(os.Stderr, Usage)
		return nil
	}
	return fmt.Errorf("unknown account subcommand %q", args[0])
}

func runAdd(args []string) error {
	fs := flag.NewFlagSet("account add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	keyFlag := fs.String("key", "", "API key (sk_test_... or sk_live_...)")
	formFlag := fs.Bool("form", false, "prompt for the key via OS-native dialog (macOS)")
	defaultFlag := fs.Bool("default", false, "set this account as the default")
	if err := fs.Parse(cli.ReorderFlagsFirst(args, fs)); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) != 1 {
		return errors.New("usage: account add <alias> [--key SK | --form] [--default]")
	}
	alias := rest[0]

	key, err := readKey(*keyFlag, *formFlag)
	if err != nil {
		return err
	}
	mode, err := config.DeriveMode(key)
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if _, exists := cfg.Accounts[alias]; exists {
		return fmt.Errorf("account %q already exists (use `account remove` first or pick another alias)", alias)
	}
	ref := uuid.NewString()
	if err := config.SetSecret(ref, key); err != nil {
		return fmt.Errorf("writing secret to keychain: %w", err)
	}
	cfg.Accounts[alias] = config.Account{Alias: alias, Mode: mode, KeychainRef: ref}
	if *defaultFlag || cfg.DefaultAccount == "" {
		cfg.DefaultAccount = alias
	}
	if err := config.Save(cfg); err != nil {
		_ = config.DeleteSecret(ref)
		return fmt.Errorf("saving config: %w", err)
	}
	return output.Emit(os.Stdout, output.Envelope{
		Mode:       string(mode),
		Account:    alias,
		APIVersion: agentstripe.PinnedAPIVersion,
		Data:       map[string]any{"added": alias, "mode": mode, "default": cfg.DefaultAccount == alias},
	})
}

// readKey resolves the API key from --key, --form, or stdin (only if piped).
// Never reads from a TTY — agents shouldn't be capturing the secret.
func readKey(keyFlag string, useForm bool) (string, error) {
	if keyFlag != "" {
		return strings.TrimSpace(keyFlag), nil
	}
	if useForm {
		return readFromOSDialog()
	}
	if isPipe(os.Stdin) {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)), nil
	}
	return "", errors.New("provide --key, --form, or pipe the key via stdin")
}

func isPipe(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) == 0
}

func readFromOSDialog() (string, error) {
	if runtime.GOOS != "darwin" {
		return "", fmt.Errorf("--form is only supported on macOS in v1 (run on your local machine and use --key elsewhere)")
	}
	script := `display dialog "Stripe API key" default answer "" with hidden answer with title "agent-stripe"
return text returned of result`
	cmd := exec.Command("osascript", "-e", script)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("osascript dialog failed (cancelled?): %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func runRemove(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: account remove <alias>")
	}
	alias := args[0]
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	acc, ok := cfg.Accounts[alias]
	if !ok {
		return fmt.Errorf("account %q not found", alias)
	}
	delete(cfg.Accounts, alias)
	if cfg.DefaultAccount == alias {
		cfg.DefaultAccount = ""
	}
	if err := config.Save(cfg); err != nil {
		return err
	}
	// Best-effort: keychain entry may already be gone.
	_ = config.DeleteSecret(acc.KeychainRef)
	return output.Emit(os.Stdout, output.Envelope{
		Mode:       string(acc.Mode),
		Account:    alias,
		APIVersion: agentstripe.PinnedAPIVersion,
		Data:       map[string]any{"removed": alias},
	})
}

func runList() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	data := make([]map[string]any, 0, len(cfg.Accounts))
	for _, a := range cfg.Accounts {
		data = append(data, map[string]any{
			"alias":   a.Alias,
			"mode":    a.Mode,
			"default": a.Alias == cfg.DefaultAccount,
		})
	}
	return output.Emit(os.Stdout, output.Envelope{
		APIVersion: agentstripe.PinnedAPIVersion,
		Data:       data,
	})
}

func runSetDefault(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: account set-default <alias>")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if _, ok := cfg.Accounts[args[0]]; !ok {
		return fmt.Errorf("account %q not found", args[0])
	}
	cfg.DefaultAccount = args[0]
	if err := config.Save(cfg); err != nil {
		return err
	}
	return output.Emit(os.Stdout, output.Envelope{
		APIVersion: agentstripe.PinnedAPIVersion,
		Data:       map[string]any{"defaultAccount": args[0]},
	})
}

func runTest(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	// `account test [alias]` may be called without alias-as-arg; the
	// dispatcher already resolved opts.Account because runTest is
	// account-required (see cli.needsAccount). When an arg is given, we
	// re-resolve to that alias inside this function.
	if len(args) == 1 {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		acc, ok := cfg.Accounts[args[0]]
		if !ok {
			return fmt.Errorf("account %q not found", args[0])
		}
		if acc.Mode == config.ModeLive && !opts.Live {
			return fmt.Errorf("account %q is live-mode; pass --live to confirm", args[0])
		}
		secret, err := config.GetSecret(acc.KeychainRef)
		if err != nil {
			return fmt.Errorf("reading secret: %w", err)
		}
		opts.Account = &acc
		opts.AccountAlias = acc.Alias
		opts.Client = agentstripe.NewClient(secret, "", opts.Timeout)
	}

	acct, err := opts.Client.V1Accounts.Retrieve(ctx, &stripeapi.AccountRetrieveParams{})
	if err != nil {
		return err
	}
	m, err := agentstripe.ToRawMap(acct)
	if err != nil {
		return err
	}
	// Trim to the fields agents actually need to verify a key works.
	slim := map[string]any{
		"id":              m["id"],
		"businessProfile": m["business_profile"],
		"country":         m["country"],
		"defaultCurrency": m["default_currency"],
		"email":           m["email"],
	}
	rendered, err := output.Render(slim, output.Options{Full: opts.Full, Expand: opts.Expand, ExpandPaths: opts.ExpandPaths})
	if err != nil {
		return err
	}
	return output.Emit(os.Stdout, output.Envelope{
		Mode:       string(opts.Account.Mode),
		Account:    opts.Account.Alias,
		APIVersion: agentstripe.PinnedAPIVersion,
		Data:       rendered,
	})
}
