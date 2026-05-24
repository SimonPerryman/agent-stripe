// Package dispute implements `agent-stripe dispute ...`.
package dispute

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

const Usage = `dispute — read Stripe disputes

Subcommands:
  get <id>                                  Fetch one dispute (dp_...)
  list [--charge CH] [--payment-intent PI] [--created-gt T] [--created-lt T]
       [--limit N] [--starting-after DP]    List disputes (cursor-paginated)

  evidence.* fields routinely exceed the default truncation cap (200 chars).
  When investigating a dispute, pass --full so the evidence narrative isn't cut.

Recommended --expand-stripe paths:
  charge, payment_intent, charge.balance_transaction`

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
	case "usage", "help":
		fmt.Fprintln(os.Stderr, Usage)
		return nil
	}
	return fmt.Errorf("unknown dispute subcommand %q", args[0])
}

func runGet(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: dispute get <id>")
	}
	params := &stripeapi.DisputeRetrieveParams{}
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	d, err := opts.Client.V1Disputes.Retrieve(ctx, args[0], params)
	if err != nil {
		return err
	}
	m, err := agentstripe.ToRawMap(d)
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
	fs := flag.NewFlagSet("dispute list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	charge := fs.String("charge", "", "filter by charge id (ch_...)")
	paymentIntent := fs.String("payment-intent", "", "filter by payment intent id (pi_...)")
	createdGT := fs.Int64("created-gt", 0, "filter: created > unix seconds")
	createdLT := fs.Int64("created-lt", 0, "filter: created < unix seconds")
	limit := fs.Int("limit", agentstripe.DefaultMaxResults, "max items to return (cap)")
	startingAfter := fs.String("starting-after", "", "cursor: dp_... id from previous page")
	if err := fs.Parse(args); err != nil {
		return err
	}

	params := &stripeapi.DisputeListParams{}
	params.Limit = stripeapi.Int64(int64(min(*limit, 100)))
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	if *charge != "" {
		params.Charge = stripeapi.String(*charge)
	}
	if *paymentIntent != "" {
		params.PaymentIntent = stripeapi.String(*paymentIntent)
	}
	if *startingAfter != "" {
		params.StartingAfter = stripeapi.String(*startingAfter)
	}
	if *createdGT > 0 || *createdLT > 0 {
		rq := &stripeapi.RangeQueryParams{}
		if *createdGT > 0 {
			rq.GreaterThan = *createdGT
		}
		if *createdLT > 0 {
			rq.LesserThan = *createdLT
		}
		params.CreatedRange = rq
	}

	if opts.Stream {
		params.Limit = stripeapi.Int64(100)
		cap := 0
		if cli.LimitExplicit(fs) {
			cap = *limit
		}
		return cli.StreamList(ctx, opts, opts.Client.V1Disputes.List(ctx, params), cap)
	}
	list := opts.Client.V1Disputes.List(ctx, params)
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
