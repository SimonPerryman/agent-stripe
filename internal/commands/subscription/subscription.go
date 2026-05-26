// Package subscription implements `agent-stripe subscription ...`.
package subscription

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

const Usage = `subscription — read Stripe subscriptions

Subcommands:
  get <id>                                  Fetch one subscription (sub_...)
  list [--customer C] [--status S] [--price P] [--created-gt T] [--created-lt T]
       [--limit N] [--starting-after SUB]   List subscriptions (cursor-paginated)
  search --query <q> [--limit N] [--page T] Stripe Search (eventual consistency
                                            ~1 min lag; --page is the opaque
                                            next_page token, NOT a sub_ id)

Search query syntax: https://docs.stripe.com/search#search-query-language
Example: subscription search --query 'status:"active" AND created>1735689600'

Pagination: list uses --starting-after (a sub_ id); search uses --page (opaque
next_page token). Not interchangeable.

Streaming: pass --stream (top-level) on list/search to emit NDJSON over pages
until --limit or exhausted.

--status passes through verbatim to Stripe (active, past_due, canceled,
trialing, incomplete, incomplete_expired, unpaid, paused, all). Note that the
Stripe default is active-only — pass --status all to see canceled subs too.

--price filters to subscriptions whose items[].price.id matches.

Typical debugging flow when a customer complains about billing:
  subscription get sub_xxx --expand-stripe customer,latest_invoice,default_payment_method
then chase latest_invoice.id into invoice get.

Recommended --expand-stripe paths:
  customer, latest_invoice, default_payment_method, items.data.price.product,
  pending_setup_intent

Avoid expanding latest_invoice.lines on list — payload size balloons fast.

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
		Msg:  fmt.Sprintf("unknown subscription subcommand %q", args[0]),
		Hint: cli.SubcommandHint(args[0], []string{"get", "list", "search"}),
		By:   output.FixableByAgent,
	}
}

func runGet(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: subscription get <id>")
	}
	params := &stripeapi.SubscriptionRetrieveParams{}
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	s, err := opts.Client.V1Subscriptions.Retrieve(ctx, args[0], params)
	if err != nil {
		return err
	}
	m, err := agentstripe.ToRawMap(s)
	if err != nil {
		return err
	}
	return cli.EmitSingle(opts, m)
}

func runList(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	fs := flag.NewFlagSet("subscription list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	customer := fs.String("customer", "", "filter by customer id (cus_...)")
	status := fs.String("status", "", "filter by status (active, past_due, canceled, trialing, all, ...)")
	price := fs.String("price", "", "filter by recurring price id (price_...)")
	createdGT := fs.Int64("created-gt", 0, "filter: created > unix seconds")
	createdLT := fs.Int64("created-lt", 0, "filter: created < unix seconds")
	limit := fs.Int("limit", agentstripe.DefaultMaxResults, "max items to return (cap)")
	startingAfter := fs.String("starting-after", "", "cursor: sub_... id from previous page")
	if err := fs.Parse(args); err != nil {
		return err
	}

	params := &stripeapi.SubscriptionListParams{}
	params.Limit = stripeapi.Int64(int64(min(*limit, agentstripe.MaxPageSize)))
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	if *customer != "" {
		params.Customer = stripeapi.String(*customer)
	}
	if *status != "" {
		params.Status = stripeapi.String(*status)
	}
	if *price != "" {
		params.Price = stripeapi.String(*price)
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
	return cli.RunListOrStream(ctx, opts, opts.Client.V1Subscriptions.List(ctx, params), *limit, cli.LimitExplicit(fs))
}

func runSearch(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	sf, err := cli.ParseSearchFlags("subscription search", args, agentstripe.DefaultMaxResults)
	if err != nil {
		return err
	}
	params := &stripeapi.SubscriptionSearchParams{}
	params.Query = sf.Query
	params.Limit = stripeapi.Int64(int64(min(sf.Limit, agentstripe.MaxPageSize)))
	if sf.Page != "" {
		params.Page = stripeapi.String(sf.Page)
	}
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	if opts.Stream {
		params.Limit = stripeapi.Int64(agentstripe.MaxPageSize)
	}
	return cli.RunSearchOrStream(ctx, opts, opts.Client.V1Subscriptions.Search(ctx, params), sf.Limit, sf.LimitExplicit)
}
