// Package invoice implements `agent-stripe invoice ...`.
package invoice

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/simonperryman/agent-stripe/internal/cli"
	agentstripe "github.com/simonperryman/agent-stripe/internal/stripe"

	stripeapi "github.com/stripe/stripe-go/v85"
)

const Usage = `invoice — read Stripe invoices

Subcommands:
  get <id>                                  Fetch one invoice (in_...)
  list [--customer C] [--subscription SUB] [--status S]
       [--created-gt T] [--created-lt T]
       [--limit N] [--starting-after IN]    List invoices (cursor-paginated)
  search --query <q> [--limit N] [--page T] Stripe Search (eventual consistency
                                            ~1 min lag; --page is the opaque
                                            next_page token, NOT an in_ id)

Search query syntax: https://docs.stripe.com/search#search-query-language
Example: invoice search --query 'total>10000 AND status:"open"'

Pagination: list uses --starting-after (an in_ id); search uses --page (opaque
next_page token). Not interchangeable.

Streaming: pass --stream (top-level) on list/search to emit NDJSON over pages
until --limit or exhausted.

--status accepts draft, open, paid, uncollectible, void.

lines.data[].description is what an agent reconciling a charge to a billing
period usually cares about. Stripe-generated descriptions are short labels
and fit within the default truncation cap, but custom invoice items can be
arbitrarily long — pass --full if descriptions look cut.

Recommended --expand-stripe paths:
  customer, subscription, payment_intent, charge, lines.data.price.product

Note: invoice preview (formerly upcoming) is not exposed in v1. The endpoint
moved to POST /v1/invoices/create_preview in the pinned API version and the
read-only chokepoint blocks POST; this is out of scope for v1 (read-only) and
is v2 work.`

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
	return fmt.Errorf("unknown invoice subcommand %q", args[0])
}

func runGet(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: invoice get <id>")
	}
	params := &stripeapi.InvoiceRetrieveParams{}
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	inv, err := opts.Client.V1Invoices.Retrieve(ctx, args[0], params)
	if err != nil {
		return err
	}
	m, err := agentstripe.ToRawMap(inv)
	if err != nil {
		return err
	}
	return cli.EmitSingle(opts, m)
}

func runList(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	fs := flag.NewFlagSet("invoice list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	customer := fs.String("customer", "", "filter by customer id (cus_...)")
	subscription := fs.String("subscription", "", "filter by subscription id (sub_...)")
	status := fs.String("status", "", "filter by status (draft, open, paid, uncollectible, void)")
	createdGT := fs.Int64("created-gt", 0, "filter: created > unix seconds")
	createdLT := fs.Int64("created-lt", 0, "filter: created < unix seconds")
	limit := fs.Int("limit", agentstripe.DefaultMaxResults, "max items to return (cap)")
	startingAfter := fs.String("starting-after", "", "cursor: in_... id from previous page")
	if err := fs.Parse(args); err != nil {
		return err
	}

	params := &stripeapi.InvoiceListParams{}
	params.Limit = stripeapi.Int64(int64(min(*limit, 100)))
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	if *customer != "" {
		params.Customer = stripeapi.String(*customer)
	}
	if *subscription != "" {
		params.Subscription = stripeapi.String(*subscription)
	}
	if *status != "" {
		params.Status = stripeapi.String(*status)
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
		params.Limit = stripeapi.Int64(100)
	}
	return cli.RunListOrStream(ctx, opts, opts.Client.V1Invoices.List(ctx, params), *limit, cli.LimitExplicit(fs))
}

func runSearch(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	sf, err := cli.ParseSearchFlags("invoice search", args, agentstripe.DefaultMaxResults)
	if err != nil {
		return err
	}
	params := &stripeapi.InvoiceSearchParams{}
	params.Query = sf.Query
	params.Limit = stripeapi.Int64(int64(min(sf.Limit, 100)))
	if sf.Page != "" {
		params.Page = stripeapi.String(sf.Page)
	}
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	if opts.Stream {
		params.Limit = stripeapi.Int64(100)
	}
	return cli.RunSearchOrStream(ctx, opts, opts.Client.V1Invoices.Search(ctx, params), sf.Limit, sf.LimitExplicit)
}
