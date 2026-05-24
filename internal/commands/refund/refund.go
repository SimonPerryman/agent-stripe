// Package refund implements `agent-stripe refund ...`. Read-only — refund
// creation is intentionally absent from v1 (Phase 5 may add it behind
// --confirm).
package refund

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/shhac/agent-stripe/internal/cli"
	"github.com/shhac/agent-stripe/internal/output"
	agentstripe "github.com/shhac/agent-stripe/internal/stripe"

	stripeapi "github.com/stripe/stripe-go/v85"
)

const Usage = `refund — read Stripe refunds

Subcommands:
  get <id>                                  Fetch one refund (re_...)
  list [--charge CH | --payment-intent PI] [--created-gt T] [--created-lt T]
       [--limit N] [--starting-after RE]    List refunds (cursor-paginated)

  Note: --charge and --payment-intent are mutually exclusive — pass at most one.

Refund creation is NOT exposed here. This CLI is read-only; use the Stripe
dashboard or a dedicated tool with audit trail for refunds.

Common --expand-stripe paths:
  charge, payment_intent, balance_transaction`

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
	return fmt.Errorf("unknown refund subcommand %q", args[0])
}

func runGet(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: refund get <id>")
	}
	params := &stripeapi.RefundRetrieveParams{}
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	r, err := opts.Client.V1Refunds.Retrieve(ctx, args[0], params)
	if err != nil {
		return err
	}
	m, err := agentstripe.ToRawMap(r)
	if err != nil {
		return err
	}
	rendered, err := output.Render(m, output.Options{Full: opts.Full, Expand: opts.Expand})
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
	fs := flag.NewFlagSet("refund list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	charge := fs.String("charge", "", "filter by charge id (ch_...)")
	paymentIntent := fs.String("payment-intent", "", "filter by payment intent id (pi_...)")
	createdGT := fs.Int64("created-gt", 0, "filter: created > unix seconds")
	createdLT := fs.Int64("created-lt", 0, "filter: created < unix seconds")
	limit := fs.Int("limit", agentstripe.DefaultMaxResults, "max items to return (cap)")
	startingAfter := fs.String("starting-after", "", "cursor: re_... id from previous page")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *charge != "" && *paymentIntent != "" {
		return errors.New("--charge and --payment-intent are mutually exclusive; pass at most one")
	}

	params := &stripeapi.RefundListParams{}
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

	list := opts.Client.V1Refunds.List(ctx, params)
	items, hasMore, nextCursor, err := agentstripe.CollectRawList(ctx, list, *limit)
	if err != nil {
		return err
	}
	rendered, err := output.Render(items, output.Options{Full: opts.Full, Expand: opts.Expand})
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
