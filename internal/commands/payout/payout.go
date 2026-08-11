// Package payout implements `agent-stripe payout ...`.
package payout

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

const Usage = `payout — read Stripe payouts

Subcommands:
  get <id>                                  Fetch one payout (po_...)
  list [--status S] [--destination D] [--created-gt T] [--created-lt T]
       [--arrival-date-gt T] [--arrival-date-lt T]
       [--limit N] [--starting-after PO]    List payouts (cursor-paginated)
                                            --status: pending|paid|failed|canceled

Reconciling against a bank statement? Filter on --arrival-date-gt/-lt, not
--created-gt/lt. created is when Stripe opened the payout; arrival_date is when
the money lands in the bank account, and the two routinely fall on different
days (and different weeks over a holiday). A bank line item is matched by the
latter.

Note: Stripe does not offer a Search API for payouts — use list with
--status / --destination / --created-gt/lt / --arrival-date-gt/lt filters.

Streaming: pass --stream (top-level) on list to emit NDJSON over pages until
--limit or exhausted.

Cross-reference: a payout's id appears as automatic_transfer_id on related
balance transactions, so to find what's *inside* a payout, query
` + "`balance transactions --payout po_...`" + ` rather than expanding here.

Connect: payouts to a merchant's own bank live on the connected account, so
reach for --stripe-account acct_... (global) here. The platform's own payout
list only covers money moving out of the platform account.

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
	return &output.Error{
		Msg:  fmt.Sprintf("unknown payout subcommand %q", args[0]),
		Hint: cli.SubcommandHint(args[0], []string{"get", "list"}),
		By:   output.FixableByAgent,
	}
}

func runGet(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: payout get <id>")
	}
	params := &stripeapi.PayoutRetrieveParams{}
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	p, err := opts.Client.V1Payouts.Retrieve(ctx, args[0], params)
	if err != nil {
		return err
	}
	return cli.EmitSingle(opts, p)
}

func runList(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	fs := flag.NewFlagSet("payout list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	status := fs.String("status", "", "filter by status: pending|paid|failed|canceled")
	destination := fs.String("destination", "", "filter by external account id")
	createdGT := fs.Int64("created-gt", 0, "filter: created > unix seconds")
	createdLT := fs.Int64("created-lt", 0, "filter: created < unix seconds")
	arrivalGT := fs.Int64("arrival-date-gt", 0, "filter: arrival_date > unix seconds (when the money lands, not when Stripe opened the payout)")
	arrivalLT := fs.Int64("arrival-date-lt", 0, "filter: arrival_date < unix seconds")
	limit := fs.Int("limit", agentstripe.DefaultMaxResults, "max items to return (cap)")
	startingAfter := fs.String("starting-after", "", "cursor: po_... id from previous page")
	if err := fs.Parse(args); err != nil {
		return err
	}

	params := &stripeapi.PayoutListParams{}
	params.Limit = stripeapi.Int64(int64(min(*limit, agentstripe.MaxPageSize)))
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	if *status != "" {
		params.Status = stripeapi.String(*status)
	}
	if *destination != "" {
		params.Destination = stripeapi.String(*destination)
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
	if *arrivalGT > 0 || *arrivalLT > 0 {
		rq := &stripeapi.RangeQueryParams{}
		if *arrivalGT > 0 {
			rq.GreaterThan = *arrivalGT
		}
		if *arrivalLT > 0 {
			rq.LesserThan = *arrivalLT
		}
		params.ArrivalDateRange = rq
	}

	if opts.Stream {
		params.Limit = stripeapi.Int64(agentstripe.MaxPageSize)
	}
	return cli.RunListOrStream(ctx, opts, opts.Client.V1Payouts.List(ctx, params), *limit, cli.LimitExplicit(fs))
}
