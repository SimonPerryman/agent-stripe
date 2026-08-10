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
                                            account can't charge or pay out).
                                            Not paginated — no --limit.
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

Streaming: pass --stream (top-level) on list or persons to emit NDJSON over
pages until --limit or exhausted.

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

// runCapabilities lists an account's capabilities. Unlike every other list in
// this CLI the endpoint is NOT paginated: CapabilityListParams embeds
// ListParams (so Limit compiles), but /v1/accounts/{id}/capabilities rejects
// `limit` with parameter_unknown. An account has a handful of capabilities, so
// there is nothing to page through — no --limit or --starting-after is offered.
func runCapabilities(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: connected-account capabilities <acct_id>")
	}
	params := &stripeapi.CapabilityListParams{Account: stripeapi.String(args[0])}
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	return cli.RunListOrStream(ctx, opts, opts.Client.V1Capabilities.List(ctx, params), agentstripe.DefaultMaxResults, false)
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
	items, err := externalAccountItems(acct.ExternalAccounts)
	if err != nil {
		return err
	}
	hasMore := acct.ExternalAccounts != nil && acct.ExternalAccounts.HasMore
	return cli.EmitList(opts, items, hasMore, "")
}

// externalAccountItems unwraps the polymorphic list into raw maps.
//
// It must reach through AccountExternalAccount to the concrete BankAccount or
// Card: that wrapper tags both payloads `json:"-"`, so marshalling the account
// object (or the wrapper itself) yields nothing but {id, object} — an answer
// that tells you a payout destination exists but not where it goes, which is
// the entire question this subcommand exists to answer.
//
// A nil list is not an error: an account with nothing attached is a perfectly
// good answer to "where do payouts go" (nowhere, yet).
func externalAccountItems(list *stripeapi.AccountExternalAccountList) ([]map[string]any, error) {
	if list == nil {
		return nil, nil
	}
	items := make([]map[string]any, 0, len(list.Data))
	for _, ea := range list.Data {
		if ea == nil {
			continue
		}
		var concrete any = ea // fallback: unexpanded or a type the SDK doesn't model
		switch {
		case ea.BankAccount != nil:
			concrete = ea.BankAccount
		case ea.Card != nil:
			concrete = ea.Card
		}
		m, err := agentstripe.ToRawMap(concrete)
		if err != nil {
			return nil, err
		}
		items = append(items, m)
	}
	return items, nil
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
