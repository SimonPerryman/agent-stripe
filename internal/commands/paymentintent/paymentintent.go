// Package paymentintent implements `agent-stripe payment-intent ...`.
// CLI name is hyphenated (payment-intent); dispatcher owns the mapping.
package paymentintent

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

const Usage = `payment-intent — read Stripe PaymentIntents

Subcommands:
  get <id>                                  Fetch one PaymentIntent (pi_...)
  list [--customer C] [--created-gt T] [--created-lt T]
       [--limit N] [--starting-after PI]    List PaymentIntents (cursor-paginated)

Debugging chain:
  PaymentIntent → latest_charge → balance_transaction
  Follow latest_charge when investigating "was this paid?"; follow
  balance_transaction (on the Charge) when investigating "what did it cost?"
  or "when did it settle?".

Subcommands (cont.):
  search --query <q> [--limit N] [--page T] Stripe Search (eventual consistency
                                            ~1 min lag; --page is the opaque
                                            next_page token, NOT a pi_ id)

Search query syntax: https://docs.stripe.com/search#search-query-language
Example: payment-intent search --query 'status:"requires_action"'

Common --expand-stripe paths:
  latest_charge, customer, payment_method, latest_charge.balance_transaction`

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
	return fmt.Errorf("unknown payment-intent subcommand %q", args[0])
}

func runGet(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: payment-intent get <id>")
	}
	params := &stripeapi.PaymentIntentRetrieveParams{}
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	pi, err := opts.Client.V1PaymentIntents.Retrieve(ctx, args[0], params)
	if err != nil {
		return err
	}
	m, err := agentstripe.ToRawMap(pi)
	if err != nil {
		return err
	}
	rendered, err := output.Render(m, output.Options{Full: opts.Full, Expand: opts.Expand, ExpandPaths: opts.ExpandPaths})
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

func runList(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	fs := flag.NewFlagSet("payment-intent list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	customer := fs.String("customer", "", "filter by customer id (cus_...)")
	createdGT := fs.Int64("created-gt", 0, "filter: created > unix seconds")
	createdLT := fs.Int64("created-lt", 0, "filter: created < unix seconds")
	limit := fs.Int("limit", agentstripe.DefaultMaxResults, "max items to return (cap)")
	startingAfter := fs.String("starting-after", "", "cursor: pi_... id from previous page")
	if err := fs.Parse(args); err != nil {
		return err
	}

	params := &stripeapi.PaymentIntentListParams{}
	params.Limit = stripeapi.Int64(int64(min(*limit, 100)))
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	if *customer != "" {
		params.Customer = stripeapi.String(*customer)
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
		cap := 0
		if cli.LimitExplicit(fs) {
			cap = *limit
		}
		return cli.StreamList(ctx, opts, opts.Client.V1PaymentIntents.List(ctx, params), cap)
	}
	list := opts.Client.V1PaymentIntents.List(ctx, params)
	items, hasMore, nextCursor, err := agentstripe.CollectRawList(ctx, list, *limit)
	if err != nil {
		return err
	}
	rendered, err := output.Render(items, output.Options{Full: opts.Full, Expand: opts.Expand, ExpandPaths: opts.ExpandPaths})
	if err != nil {
		return err
	}
	return output.Emit(os.Stdout, output.Envelope{
		Mode:       string(opts.Account.Mode),
		Account:    opts.Account.Alias,
		APIVersion: agentstripe.PinnedAPIVersion,
		Data:       rendered,
		Page:       &output.Page{HasMore: hasMore, NextCursor: nextCursor, Count: len(items)},
	})
}

func runSearch(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	sf, err := cli.ParseSearchFlags("payment-intent search", args, agentstripe.DefaultMaxResults)
	if err != nil {
		return err
	}
	params := &stripeapi.PaymentIntentSearchParams{}
	params.Query = sf.Query
	params.Limit = stripeapi.Int64(int64(min(sf.Limit, 100)))
	if sf.Page != "" {
		params.Page = stripeapi.String(sf.Page)
	}
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	if opts.Stream {
		params.Limit = stripeapi.Int64(100)
		cap := 0
		if sf.LimitExplicit {
			cap = sf.Limit
		}
		return cli.StreamSearch(ctx, opts, opts.Client.V1PaymentIntents.Search(ctx, params), cap)
	}
	list := opts.Client.V1PaymentIntents.Search(ctx, params)
	items, hasMore, nextCursor, err := agentstripe.CollectRawSearch(ctx, list, sf.Limit)
	if err != nil {
		return err
	}
	rendered, err := output.Render(items, output.Options{Full: opts.Full, Expand: opts.Expand, ExpandPaths: opts.ExpandPaths})
	if err != nil {
		return err
	}
	return output.Emit(os.Stdout, output.Envelope{
		Mode:       string(opts.Account.Mode),
		Account:    opts.Account.Alias,
		APIVersion: agentstripe.PinnedAPIVersion,
		Data:       rendered,
		Page:       &output.Page{HasMore: hasMore, NextCursor: nextCursor, Count: len(items)},
	})
}
