// Package charge implements `agent-stripe charge ...`.
package charge

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

const Usage = `charge — read Stripe charges

Subcommands:
  get <id>                                  Fetch one charge by id (ch_...)
  list [--customer C] [--payment-intent PI] [--created-gt T] [--created-lt T]
       [--limit N] [--starting-after CH]    List charges (cursor-paginated)
  search --query <q> [--limit N] [--page T] Stripe Search (eventual consistency
                                            ~1 min lag; --page is the opaque
                                            next_page token, NOT a charge id)

Debugging tips:
  outcome.seller_message and failure_message are the primary signals when a
  charge fails. Use --full when investigating a specific failure so those
  fields aren't truncated.

Search query syntax: https://docs.stripe.com/search#search-query-language
Example: charge search --query 'amount>5000 AND status:"succeeded"'

Pagination: list uses --starting-after (a ch_ id); search uses --page (opaque
next_page token). Not interchangeable.

Streaming: pass --stream (top-level) on list/search to emit NDJSON over pages
until --limit or exhausted.

Common --expand-stripe paths:
  customer, balance_transaction, application_fee, transfer, invoice

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
	return fmt.Errorf("unknown charge subcommand %q", args[0])
}

func runGet(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: charge get <id>")
	}
	params := &stripeapi.ChargeRetrieveParams{}
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	c, err := opts.Client.V1Charges.Retrieve(ctx, args[0], params)
	if err != nil {
		return err
	}
	m, err := agentstripe.ToRawMap(c)
	if err != nil {
		return err
	}
	return cli.EmitSingle(opts, m)
}

func runList(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	fs := flag.NewFlagSet("charge list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	customer := fs.String("customer", "", "filter by customer id (cus_...)")
	paymentIntent := fs.String("payment-intent", "", "filter by payment intent id (pi_...)")
	createdGT := fs.Int64("created-gt", 0, "filter: created > unix seconds")
	createdLT := fs.Int64("created-lt", 0, "filter: created < unix seconds")
	limit := fs.Int("limit", agentstripe.DefaultMaxResults, "max items to return (cap)")
	startingAfter := fs.String("starting-after", "", "cursor: ch_... id from previous page")
	if err := fs.Parse(args); err != nil {
		return err
	}

	params := &stripeapi.ChargeListParams{}
	params.Limit = stripeapi.Int64(int64(min(*limit, 100)))
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	if *customer != "" {
		params.Customer = stripeapi.String(*customer)
	}
	if *paymentIntent != "" {
		params.PaymentIntent = stripeapi.String(*paymentIntent)
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
		// Under --stream, max per-page is fixed at 100 to minimise round-trips.
		params.Limit = stripeapi.Int64(100)
	}
	return cli.RunListOrStream(ctx, opts, opts.Client.V1Charges.List(ctx, params), *limit, cli.LimitExplicit(fs))
}

func runSearch(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	sf, err := cli.ParseSearchFlags("charge search", args, agentstripe.DefaultMaxResults)
	if err != nil {
		return err
	}
	params := &stripeapi.ChargeSearchParams{}
	params.Query = sf.Query
	params.Limit = stripeapi.Int64(int64(min(sf.Limit, 100)))
	if sf.Page != "" {
		params.Page = stripeapi.String(sf.Page)
	}
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	if opts.Stream {
		params.Limit = stripeapi.Int64(100)
	}
	return cli.RunSearchOrStream(ctx, opts, opts.Client.V1Charges.Search(ctx, params), sf.Limit, sf.LimitExplicit)
}
