// Package paymentmethod implements `agent-stripe payment-method ...`.
package paymentmethod

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

const Usage = `payment-method — read Stripe payment methods

Subcommands:
  get <id>                                  Fetch one payment method (pm_..., card_...)
  list --customer C [--type T]
       [--limit N] [--starting-after PM]    List a customer's payment methods

PaymentIntent and related responses surface pm_... ids in fields like
latest_charge.payment_method and payment_method — this command is the lookup
endpoint for those.

--customer is required: Stripe's PaymentMethod list endpoint without a customer
is a restricted-key flow that surfaces cross-customer noise. By the time an
agent is looking up payment methods it always has a customer in hand (from a
PI, a subscription, or a checkout session).

--type passes through verbatim to Stripe. Common values: card, us_bank_account,
sepa_debit, link.

No Search API for payment methods.

Streaming: pass --stream (top-level) on list to emit NDJSON over pages until
--limit or exhausted.

Recommended --expand-stripe paths:
  customer

Connect: payment methods are per-account. A pm_ attached to a connected
account's customer is not readable from the platform — pass --stripe-account
acct_... (global).

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
	return fmt.Errorf("unknown payment-method subcommand %q", args[0])
}

func runGet(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: payment-method get <id>")
	}
	params := &stripeapi.PaymentMethodRetrieveParams{}
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	pm, err := opts.Client.V1PaymentMethods.Retrieve(ctx, args[0], params)
	if err != nil {
		return err
	}
	return cli.EmitSingle(opts, pm)
}

func runList(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	fs := flag.NewFlagSet("payment-method list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	customer := fs.String("customer", "", "filter by customer id (cus_...) — required")
	typ := fs.String("type", "", "filter by payment method type (card, us_bank_account, sepa_debit, link, ...)")
	limit := fs.Int("limit", agentstripe.DefaultMaxResults, "max items to return (cap)")
	startingAfter := fs.String("starting-after", "", "cursor: pm_... id from previous page")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *customer == "" {
		return errors.New("payment-method list requires --customer <cus_...> (Stripe's un-customer-scoped list is restricted-key only)")
	}

	params := &stripeapi.PaymentMethodListParams{}
	params.Limit = stripeapi.Int64(int64(min(*limit, agentstripe.MaxPageSize)))
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	params.Customer = stripeapi.String(*customer)
	if *typ != "" {
		params.Type = stripeapi.String(*typ)
	}
	if *startingAfter != "" {
		params.StartingAfter = stripeapi.String(*startingAfter)
	}
	if opts.Stream {
		params.Limit = stripeapi.Int64(agentstripe.MaxPageSize)
	}
	return cli.RunListOrStream(ctx, opts, opts.Client.V1PaymentMethods.List(ctx, params), *limit, cli.LimitExplicit(fs))
}
