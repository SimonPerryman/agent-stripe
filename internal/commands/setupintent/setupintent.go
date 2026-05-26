// Package setupintent implements `agent-stripe setup-intent ...`.
package setupintent

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

const Usage = `setup-intent — read Stripe SetupIntents and SetupAttempts

Subcommands:
  get <id>                                  Fetch one SetupIntent (seti_...)
  list [--customer C] [--payment-method PM] [--attach-to-self]
       [--created-gt T] [--created-lt T]
       [--limit N] [--starting-after SETI]  List SetupIntents (cursor-paginated)
  attempts <seti_id>
       [--limit N] [--starting-after SETATT]  List SetupAttempts for a SetupIntent

SetupIntents are the save-card / off-session setup analogue of PaymentIntents.
The chain is SetupIntent → SetupAttempt → PaymentMethod, mirroring
PaymentIntent → Charge.

No Search API for SetupIntents or SetupAttempts.

Streaming: pass --stream (top-level) on list/attempts to emit NDJSON over pages
until --limit or exhausted.

Recommended --expand-stripe paths on get:
  customer, payment_method, latest_attempt

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
	case "attempts":
		return runAttempts(ctx, opts, args[1:])
	case "usage", "help":
		fmt.Fprintln(os.Stderr, Usage)
		return nil
	}
	return fmt.Errorf("unknown setup-intent subcommand %q", args[0])
}

func runGet(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: setup-intent get <id>")
	}
	params := &stripeapi.SetupIntentRetrieveParams{}
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	si, err := opts.Client.V1SetupIntents.Retrieve(ctx, args[0], params)
	if err != nil {
		return err
	}
	m, err := agentstripe.ToRawMap(si)
	if err != nil {
		return err
	}
	return cli.EmitSingle(opts, m)
}

func runList(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	fs := flag.NewFlagSet("setup-intent list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	customer := fs.String("customer", "", "filter by customer id (cus_...)")
	paymentMethod := fs.String("payment-method", "", "filter by payment method id (pm_...)")
	attachToSelf := fs.Bool("attach-to-self", false, "filter to setup intents attaching to the Stripe account itself")
	createdGT := fs.Int64("created-gt", 0, "filter: created > unix seconds")
	createdLT := fs.Int64("created-lt", 0, "filter: created < unix seconds")
	limit := fs.Int("limit", agentstripe.DefaultMaxResults, "max items to return (cap)")
	startingAfter := fs.String("starting-after", "", "cursor: seti_... id from previous page")
	if err := fs.Parse(args); err != nil {
		return err
	}

	params := &stripeapi.SetupIntentListParams{}
	params.Limit = stripeapi.Int64(int64(min(*limit, agentstripe.MaxPageSize)))
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	if *customer != "" {
		params.Customer = stripeapi.String(*customer)
	}
	if *paymentMethod != "" {
		params.PaymentMethod = stripeapi.String(*paymentMethod)
	}
	if *attachToSelf {
		params.AttachToSelf = stripeapi.Bool(true)
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
	return cli.RunListOrStream(ctx, opts, opts.Client.V1SetupIntents.List(ctx, params), *limit, cli.LimitExplicit(fs))
}

func runAttempts(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: setup-intent attempts <seti_id> [--limit N] [--starting-after SETATT]")
	}
	setiID := args[0]
	fs := flag.NewFlagSet("setup-intent attempts", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	limit := fs.Int("limit", agentstripe.DefaultMaxResults, "max items to return (cap)")
	startingAfter := fs.String("starting-after", "", "cursor: setatt_... id from previous page")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	params := &stripeapi.SetupAttemptListParams{}
	params.SetupIntent = stripeapi.String(setiID)
	params.Limit = stripeapi.Int64(int64(min(*limit, agentstripe.MaxPageSize)))
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	if *startingAfter != "" {
		params.StartingAfter = stripeapi.String(*startingAfter)
	}
	if opts.Stream {
		params.Limit = stripeapi.Int64(agentstripe.MaxPageSize)
	}
	return cli.RunListOrStream(ctx, opts, opts.Client.V1SetupAttempts.List(ctx, params), *limit, cli.LimitExplicit(fs))
}
