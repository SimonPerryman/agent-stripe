// Package customer implements `agent-stripe customer ...`.
package customer

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

const Usage = `customer — read Stripe customers

Subcommands:
  get <id>                                  Fetch one customer by id (cus_...)
  list [--email E] [--created-gt T] [--created-lt T] [--limit N] [--starting-after CUS]
                                            List customers (cursor-paginated)
  search --query <q> [--limit N] [--page T] Stripe Search (eventual consistency
                                            ~1 min lag; --page is the opaque
                                            next_page token, NOT a cus_ id)

Search query syntax: https://docs.stripe.com/search#search-query-language
Example: customer search --query 'email:"alice@example.com"'

Pagination: list uses --starting-after (a cus_ id); search uses --page (opaque
next_page token). Not interchangeable.

Streaming: pass --stream (top-level) on list/search to emit NDJSON over pages
until --limit or exhausted.

Connect: customers are per-account. The platform and a connected account can
each hold a distinct cus_... for the same person, with the same email, and
neither is visible from the other. Pass --stripe-account acct_... (global) to
read the connected account's. Never assume a cus_ id crosses the boundary.

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
	case "search":
		return runSearch(ctx, opts, args[1:])
	case "usage", "help":
		fmt.Fprintln(os.Stderr, Usage)
		return nil
	}
	return &output.Error{
		Msg:  fmt.Sprintf("unknown customer subcommand %q", args[0]),
		Hint: cli.SubcommandHint(args[0], []string{"get", "list", "search"}),
		By:   output.FixableByAgent,
	}
}

func runGet(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: customer get <id>")
	}
	c, err := opts.Client.V1Customers.Retrieve(ctx, args[0], &stripeapi.CustomerRetrieveParams{})
	if err != nil {
		return err
	}
	return cli.EmitSingle(opts, c)
}

func runList(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	fs := flag.NewFlagSet("customer list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	email := fs.String("email", "", "filter by email (exact match)")
	createdGT := fs.Int64("created-gt", 0, "filter: created > unix seconds")
	createdLT := fs.Int64("created-lt", 0, "filter: created < unix seconds")
	limit := fs.Int("limit", agentstripe.DefaultMaxResults, "max items to return (cap)")
	startingAfter := fs.String("starting-after", "", "cursor: cus_... id from previous page")
	if err := fs.Parse(args); err != nil {
		return err
	}

	params := &stripeapi.CustomerListParams{}
	params.Limit = stripeapi.Int64(int64(min(*limit, agentstripe.MaxPageSize)))
	if *email != "" {
		params.Email = stripeapi.String(*email)
	}
	if *startingAfter != "" {
		params.StartingAfter = stripeapi.String(*startingAfter)
	}
	if *createdGT > 0 || *createdLT > 0 {
		r := &stripeapi.RangeQueryParams{}
		if *createdGT > 0 {
			r.GreaterThan = *createdGT
		}
		if *createdLT > 0 {
			r.LesserThan = *createdLT
		}
		params.CreatedRange = r
	}

	if opts.Stream {
		params.Limit = stripeapi.Int64(agentstripe.MaxPageSize)
	}
	return cli.RunListOrStream(ctx, opts, opts.Client.V1Customers.List(ctx, params), *limit, cli.LimitExplicit(fs))
}

func runSearch(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	sf, err := cli.ParseSearchFlags("customer search", args, agentstripe.DefaultMaxResults)
	if err != nil {
		return err
	}
	params := &stripeapi.CustomerSearchParams{}
	params.Query = sf.Query
	params.Limit = stripeapi.Int64(int64(min(sf.Limit, agentstripe.MaxPageSize)))
	if sf.Page != "" {
		params.Page = stripeapi.String(sf.Page)
	}
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	if opts.Stream {
		params.Limit = stripeapi.Int64(agentstripe.MaxPageSize)
	}
	return cli.RunSearchOrStream(ctx, opts, opts.Client.V1Customers.Search(ctx, params), sf.Limit, sf.LimitExplicit)
}
