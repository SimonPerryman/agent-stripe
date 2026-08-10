// Package applicationfee implements `agent-stripe application-fee ...` — the
// platform's revenue on Connect direct charges.
package applicationfee

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

const Usage = `application-fee — read Stripe Connect application fees

Subcommands:
  get <fee_id>                              Fetch one application fee (fee_...)
  list [--charge CH] [--created-gt T]
       [--created-lt T] [--limit N]
       [--starting-after FEE]               List application fees
  refunds <fee_id> [--limit N]
          [--starting-after FR]             List refunds on an application fee

This is what the platform earned on a connected account's charges. Two ways
in from the payment side:
  charge.application_fee                    the fee_... on a direct charge
  payment_intent.application_fee_amount     the amount, on the PI

Scope: application fees belong to the platform, so this reads the platform
account by default and needs no --stripe-account. The underlying charge lives
on the connected account — reach for --stripe-account when you follow the
fee's charge id back to the payment.

Note: Stripe offers no Search API for application fees — use list with
--charge / --created-gt/lt filters.

Streaming: pass --stream (top-level) on list or refunds to emit NDJSON over
pages until --limit or exhausted.

Recommended --expand-stripe paths:
  charge, balance_transaction, refunds, originating_transaction

Help: usage | help | -h | --help`

func Run(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, Usage)
		return nil
	}
	// Application fees are the platform's revenue and only ever exist on the
	// platform's books; scoping to a connected account returns an empty list
	// that reads as "we earn nothing" rather than "wrong account".
	if args[0] != "usage" && args[0] != "help" {
		if err := cli.RejectStripeAccount(opts, "application-fee"); err != nil {
			return err
		}
	}
	switch args[0] {
	case "get":
		return runGet(ctx, opts, args[1:])
	case "list":
		return runList(ctx, opts, args[1:])
	case "refunds":
		return runRefunds(ctx, opts, args[1:])
	case "usage", "help":
		fmt.Fprintln(os.Stderr, Usage)
		return nil
	}
	return &output.Error{
		Msg:  fmt.Sprintf("unknown application-fee subcommand %q", args[0]),
		Hint: cli.SubcommandHint(args[0], []string{"get", "list", "refunds"}),
		By:   output.FixableByAgent,
	}
}

func runGet(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: application-fee get <fee_id>")
	}
	params := &stripeapi.ApplicationFeeRetrieveParams{}
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	fee, err := opts.Client.V1ApplicationFees.Retrieve(ctx, args[0], params)
	if err != nil {
		return err
	}
	m, err := agentstripe.ToRawMap(fee)
	if err != nil {
		return err
	}
	return cli.EmitSingle(opts, m)
}

func runList(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	fs := flag.NewFlagSet("application-fee list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	charge := fs.String("charge", "", "filter by charge id (ch_...)")
	createdGT := fs.Int64("created-gt", 0, "filter: created > unix seconds")
	createdLT := fs.Int64("created-lt", 0, "filter: created < unix seconds")
	limit := fs.Int("limit", agentstripe.DefaultMaxResults, "max items to return (cap)")
	startingAfter := fs.String("starting-after", "", "cursor: fee_... id from previous page")
	if err := fs.Parse(args); err != nil {
		return err
	}

	params := &stripeapi.ApplicationFeeListParams{}
	params.Limit = stripeapi.Int64(int64(min(*limit, agentstripe.MaxPageSize)))
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	if *charge != "" {
		params.Charge = stripeapi.String(*charge)
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
		params.Limit = stripeapi.Int64(agentstripe.MaxPageSize)
	}
	return cli.RunListOrStream(ctx, opts, opts.Client.V1ApplicationFees.List(ctx, params), *limit, cli.LimitExplicit(fs))
}

func runRefunds(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: application-fee refunds <fee_id> [--limit N] [--starting-after FR]")
	}
	feeID := args[0]
	fs := flag.NewFlagSet("application-fee refunds", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	limit := fs.Int("limit", agentstripe.DefaultMaxResults, "max items to return (cap)")
	startingAfter := fs.String("starting-after", "", "cursor: fr_... id from previous page")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	params := &stripeapi.FeeRefundListParams{ID: stripeapi.String(feeID)}
	params.Limit = stripeapi.Int64(int64(min(*limit, agentstripe.MaxPageSize)))
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	if *startingAfter != "" {
		params.StartingAfter = stripeapi.String(*startingAfter)
	}
	if opts.Stream {
		params.Limit = stripeapi.Int64(agentstripe.MaxPageSize)
	}
	return cli.RunListOrStream(ctx, opts, opts.Client.V1FeeRefunds.List(ctx, params), *limit, cli.LimitExplicit(fs))
}
