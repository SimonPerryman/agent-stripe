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

Note: Stripe does not offer a Search API for disputes — use list with
--charge / --payment-intent filters.

Streaming: pass --stream (top-level) on list to emit NDJSON over pages until
--limit or exhausted.

Recommended --expand-stripe paths:
  charge, payment_intent, charge.balance_transaction

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
	return cli.EmitSingle(opts, m)
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
	}
	return cli.RunListOrStream(ctx, opts, opts.Client.V1Disputes.List(ctx, params), *limit, cli.LimitExplicit(fs))
}
