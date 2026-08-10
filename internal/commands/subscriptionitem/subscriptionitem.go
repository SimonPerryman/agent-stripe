// Package subscriptionitem implements `agent-stripe subscription-item ...`.
package subscriptionitem

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

const Usage = `subscription-item — read Stripe subscription items

Subcommands:
  get <id>                                  Fetch one subscription item (si_...)
  list --subscription SUB
       [--limit N] [--starting-after SI]    List items on a subscription

In most cases subscription items are already returned inline as
subscription.items.data[]. This command exists for paging through
subscriptions with more than 10 items (rare but real).

--subscription is required by Stripe's list endpoint.

No Search API for subscription items.

Streaming: pass --stream (top-level) on list to emit NDJSON over pages until
--limit or exhausted.

Recommended --expand-stripe paths:
  price.product

Connect: items belong to the account that owns the subscription, so a connected
account's items need --stripe-account acct_... (global).

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
	return fmt.Errorf("unknown subscription-item subcommand %q", args[0])
}

func runGet(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: subscription-item get <id>")
	}
	params := &stripeapi.SubscriptionItemRetrieveParams{}
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	si, err := opts.Client.V1SubscriptionItems.Retrieve(ctx, args[0], params)
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
	fs := flag.NewFlagSet("subscription-item list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	subscription := fs.String("subscription", "", "subscription id (sub_...) — required")
	limit := fs.Int("limit", agentstripe.DefaultMaxResults, "max items to return (cap)")
	startingAfter := fs.String("starting-after", "", "cursor: si_... id from previous page")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *subscription == "" {
		return errors.New("subscription-item list requires --subscription <sub_...>")
	}

	params := &stripeapi.SubscriptionItemListParams{}
	params.Subscription = stripeapi.String(*subscription)
	params.Limit = stripeapi.Int64(int64(min(*limit, agentstripe.MaxPageSize)))
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	if *startingAfter != "" {
		params.StartingAfter = stripeapi.String(*startingAfter)
	}
	if opts.Stream {
		params.Limit = stripeapi.Int64(agentstripe.MaxPageSize)
	}
	return cli.RunListOrStream(ctx, opts, opts.Client.V1SubscriptionItems.List(ctx, params), *limit, cli.LimitExplicit(fs))
}
