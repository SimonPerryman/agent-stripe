// Package checkoutsession implements `agent-stripe checkout-session ...`.
package checkoutsession

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

const Usage = `checkout-session — read Stripe Checkout Sessions

Subcommands:
  get <id>                                  Fetch one Checkout Session (cs_...)
  list [--customer C] [--payment-intent PI] [--subscription SUB] [--status S]
       [--created-gt T] [--created-lt T]
       [--limit N] [--starting-after CS]    List sessions (cursor-paginated)

A Checkout Session is the hosted-checkout entry point. Depending on session
mode it fans out to one of payment_intent / subscription / setup_intent —
only one is populated.

Note: Stripe does not offer a Search API for Checkout Sessions — use list with
--customer / --payment-intent / --subscription / --status filters.

Streaming: pass --stream (top-level) on list to emit NDJSON over pages until
--limit or exhausted.

Recommended --expand-stripe paths:
  payment_intent, subscription, setup_intent, customer, line_items

line_items is a synthetic field: it is only present when explicitly expanded.
Fine on get; avoid on list (payload size).

Connect: a session created with the Stripe-Account header lives on the
connected account and is invisible from the platform. Pass --stripe-account
acct_... (global). Sessions that only set transfer_data/application_fee_amount
are platform sessions and need no flag.

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
	return fmt.Errorf("unknown checkout-session subcommand %q", args[0])
}

func runGet(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: checkout-session get <id>")
	}
	params := &stripeapi.CheckoutSessionRetrieveParams{}
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	s, err := opts.Client.V1CheckoutSessions.Retrieve(ctx, args[0], params)
	if err != nil {
		return err
	}
	return cli.EmitSingle(opts, s)
}

func runList(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	fs := flag.NewFlagSet("checkout-session list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	customer := fs.String("customer", "", "filter by customer id (cus_...)")
	paymentIntent := fs.String("payment-intent", "", "filter by payment intent id (pi_...)")
	subscription := fs.String("subscription", "", "filter by subscription id (sub_...)")
	status := fs.String("status", "", "filter by status (open, complete, expired)")
	createdGT := fs.Int64("created-gt", 0, "filter: created > unix seconds")
	createdLT := fs.Int64("created-lt", 0, "filter: created < unix seconds")
	limit := fs.Int("limit", agentstripe.DefaultMaxResults, "max items to return (cap)")
	startingAfter := fs.String("starting-after", "", "cursor: cs_... id from previous page")
	if err := fs.Parse(args); err != nil {
		return err
	}

	params := &stripeapi.CheckoutSessionListParams{}
	params.Limit = stripeapi.Int64(int64(min(*limit, agentstripe.MaxPageSize)))
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	if *customer != "" {
		params.Customer = stripeapi.String(*customer)
	}
	if *paymentIntent != "" {
		params.PaymentIntent = stripeapi.String(*paymentIntent)
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
		params.Limit = stripeapi.Int64(agentstripe.MaxPageSize)
	}
	return cli.RunListOrStream(ctx, opts, opts.Client.V1CheckoutSessions.List(ctx, params), *limit, cli.LimitExplicit(fs))
}
