// Package connectedaccount implements `agent-stripe connected-account ...` —
// the Stripe Accounts API (Connect). Distinct from the `account` command,
// which manages local keychain aliases and keeps that name.
package connectedaccount

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/simonperryman/agent-stripe/internal/cli"
	"github.com/simonperryman/agent-stripe/internal/output"
	agentstripe "github.com/simonperryman/agent-stripe/internal/stripe"

	stripeapi "github.com/stripe/stripe-go/v85"
)

const Usage = `connected-account — read Stripe Connect accounts (Accounts API)

Subcommands:
  get <acct_id>                             Fetch one connected account
  list [--created-gt T] [--created-lt T]
       [--limit N] [--starting-after ACCT]  List your connected accounts
  capabilities <acct_id>                    Per-capability status (why an
                                            account can't charge or pay out)
  persons <acct_id> [--relationship-owner]
          [--relationship-director]         People on the account (KYC)
  external-accounts <acct_id>               Bank accounts/cards payouts go to

Not to be confused with ` + "`account`" + `, which manages local API-key aliases.

Verification chain — read these in order when an account "isn't working":
  charges_enabled / payouts_enabled        the summary booleans (what)
  requirements.currently_due               the outstanding fields (why)
  requirements.disabled_reason             why Stripe blocked it (why)
  capabilities <acct_id>                   the per-capability breakdown (which)

Scope: list is platform-scoped — it enumerates the accounts connected to the
key you are using, so --stripe-account is meaningless there. get,
capabilities, persons, and external-accounts all take the account id as a
positional argument and likewise need no --stripe-account. Use that flag when
reading *other* resources (charge, balance, payout…) from a connected
account's books.

Note: Stripe offers no Search API for accounts — use list with
--created-gt/lt. external-accounts has no dedicated endpoint either; it is a
get with expand[]=external_accounts under the hood.

Streaming: pass --stream (top-level) on list, capabilities, or persons to emit
NDJSON over pages until --limit or exhausted.

Recommended --expand-stripe paths:
  external_accounts, settings, requirements

Help: usage | help | -h | --help`

func Run(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, Usage)
		return nil
	}
	switch args[0] {
	case "get":
		return runGet(ctx, opts, args[1:])
	case "list":
		return runList(ctx, opts, args[1:])
	case "capabilities":
		return runCapabilities(ctx, opts, args[1:])
	case "persons":
		return runPersons(ctx, opts, args[1:])
	case "external-accounts":
		return runExternalAccounts(ctx, opts, args[1:])
	case "usage", "help":
		fmt.Fprintln(os.Stderr, Usage)
		return nil
	}
	return &output.Error{
		Msg:  fmt.Sprintf("unknown connected-account subcommand %q", args[0]),
		Hint: cli.SubcommandHint(args[0], []string{"get", "list", "capabilities", "persons", "external-accounts"}),
		By:   output.FixableByAgent,
	}
}

func runGet(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: connected-account get <acct_id>")
	}
	params := &stripeapi.AccountRetrieveParams{}
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	acct, err := opts.Client.V1Accounts.GetByID(ctx, args[0], params)
	if err != nil {
		return err
	}
	m, err := agentstripe.ToRawMap(acct)
	if err != nil {
		return err
	}
	return cli.EmitSingle(opts, m)
}

func runList(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	fs := flag.NewFlagSet("connected-account list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	createdGT := fs.Int64("created-gt", 0, "filter: created > unix seconds")
	createdLT := fs.Int64("created-lt", 0, "filter: created < unix seconds")
	limit := fs.Int("limit", agentstripe.DefaultMaxResults, "max items to return (cap)")
	startingAfter := fs.String("starting-after", "", "cursor: acct_... id from previous page")
	if err := fs.Parse(args); err != nil {
		return err
	}

	params := &stripeapi.AccountListParams{}
	params.Limit = stripeapi.Int64(int64(min(*limit, agentstripe.MaxPageSize)))
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	if *startingAfter != "" {
		params.StartingAfter = stripeapi.String(*startingAfter)
	}
	if rq := createdRange(*createdGT, *createdLT); rq != nil {
		params.CreatedRange = rq
	}
	if opts.Stream {
		params.Limit = stripeapi.Int64(agentstripe.MaxPageSize)
	}
	return cli.RunListOrStream(ctx, opts, opts.Client.V1Accounts.List(ctx, params), *limit, cli.LimitExplicit(fs))
}

func runCapabilities(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: connected-account capabilities <acct_id> [--limit N]")
	}
	acctID := args[0]
	fs := flag.NewFlagSet("connected-account capabilities", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	limit := fs.Int("limit", agentstripe.DefaultMaxResults, "max items to return (cap)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	params := &stripeapi.CapabilityListParams{Account: stripeapi.String(acctID)}
	params.Limit = stripeapi.Int64(int64(min(*limit, agentstripe.MaxPageSize)))
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	if opts.Stream {
		params.Limit = stripeapi.Int64(agentstripe.MaxPageSize)
	}
	return cli.RunListOrStream(ctx, opts, opts.Client.V1Capabilities.List(ctx, params), *limit, cli.LimitExplicit(fs))
}

func runPersons(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: connected-account persons <acct_id> [--relationship-owner] [--relationship-director] [--limit N] [--starting-after PERSON]")
	}
	acctID := args[0]
	fs := flag.NewFlagSet("connected-account persons", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	owner := fs.Bool("relationship-owner", false, "only people who are owners of the account's company")
	director := fs.Bool("relationship-director", false, "only people who are directors of the account's company")
	limit := fs.Int("limit", agentstripe.DefaultMaxResults, "max items to return (cap)")
	startingAfter := fs.String("starting-after", "", "cursor: person_... id from previous page")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	params := &stripeapi.PersonListParams{Account: stripeapi.String(acctID)}
	params.Limit = stripeapi.Int64(int64(min(*limit, agentstripe.MaxPageSize)))
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	if *startingAfter != "" {
		params.StartingAfter = stripeapi.String(*startingAfter)
	}
	// Only set the sub-struct when a filter was asked for — an all-false
	// relationship hash is a different (and narrower) query than omitting it.
	if *owner || *director {
		rel := &stripeapi.PersonListRelationshipParams{}
		if *owner {
			rel.Owner = stripeapi.Bool(true)
		}
		if *director {
			rel.Director = stripeapi.Bool(true)
		}
		params.Relationship = rel
	}
	if opts.Stream {
		params.Limit = stripeapi.Int64(agentstripe.MaxPageSize)
	}
	return cli.RunListOrStream(ctx, opts, opts.Client.V1Persons.List(ctx, params), *limit, cli.LimitExplicit(fs))
}

// runExternalAccounts answers "where do this account's payouts actually go".
// stripe-go/v85 has no external-accounts service: the list arrives inline on
// the account object, so this is a retrieve with an explicit expand plus a
// projection down to the nested list.
func runExternalAccounts(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: connected-account external-accounts <acct_id>")
	}
	params := &stripeapi.AccountRetrieveParams{}
	params.Expand = agentstripe.ExpandSlice(append([]string{"external_accounts"}, opts.ExpandStripe...))
	acct, err := opts.Client.V1Accounts.GetByID(ctx, args[0], params)
	if err != nil {
		return err
	}
	m, err := agentstripe.ToRawMap(acct)
	if err != nil {
		return err
	}
	items, hasMore := externalAccountItems(m)
	return cli.EmitList(opts, items, hasMore, "")
}

// externalAccountItems pulls the nested {object:list, data:[…], has_more} out
// of the raw account map. A missing or oddly-shaped field yields an empty
// list rather than an error: an account with no external account attached is
// a perfectly good answer to "where do payouts go" (nowhere, yet).
func externalAccountItems(acct map[string]any) ([]map[string]any, bool) {
	nested, ok := acct["external_accounts"].(map[string]any)
	if !ok {
		return nil, false
	}
	raw, _ := nested["data"].([]any)
	items := make([]map[string]any, 0, len(raw))
	for _, r := range raw {
		if m, ok := r.(map[string]any); ok {
			items = append(items, m)
		}
	}
	hasMore, _ := nested["has_more"].(bool)
	return items, hasMore
}

func createdRange(gt, lt int64) *stripeapi.RangeQueryParams {
	if gt <= 0 && lt <= 0 {
		return nil
	}
	rq := &stripeapi.RangeQueryParams{}
	if gt > 0 {
		rq.GreaterThan = gt
	}
	if lt > 0 {
		rq.LesserThan = lt
	}
	return rq
}
