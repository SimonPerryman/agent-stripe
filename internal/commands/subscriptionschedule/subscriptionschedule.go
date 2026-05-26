// Package subscriptionschedule implements `agent-stripe subscription-schedule ...`.
package subscriptionschedule

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

const Usage = `subscription-schedule — read Stripe subscription schedules

Subcommands:
  get <id>                                  Fetch one schedule (sub_sched_...)
  list [--customer C] [--scheduled]
       [--created-gt T] [--created-lt T]
       [--canceled-at-gt T] [--canceled-at-lt T]
       [--completed-at-gt T] [--completed-at-lt T]
       [--released-at-gt T] [--released-at-lt T]
       [--limit N] [--starting-after SS]    List schedules (cursor-paginated)

Subscription schedules describe phased subscriptions (phases[].items[].price).
Typical use case for an agent: "what's coming next for this customer".

Note: Stripe does not expose a single status enum on schedules. Only
--scheduled (a bool — filter to not-yet-started schedules) plus per-transition
date-range windows are available. To find canceled / completed / released
schedules, use the matching --<state>-at-gt / --<state>-at-lt window.

The list endpoint does not accept a subscription-id filter — use 'get' if you
have the schedule id, or filter by customer and date ranges.

No Search API for subscription schedules.

Streaming: pass --stream (top-level) on list to emit NDJSON over pages until
--limit or exhausted.

Recommended --expand-stripe paths:
  subscription, customer, phases.items.price.product

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
	return fmt.Errorf("unknown subscription-schedule subcommand %q", args[0])
}

func runGet(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: subscription-schedule get <id>")
	}
	params := &stripeapi.SubscriptionScheduleRetrieveParams{}
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	s, err := opts.Client.V1SubscriptionSchedules.Retrieve(ctx, args[0], params)
	if err != nil {
		return err
	}
	m, err := agentstripe.ToRawMap(s)
	if err != nil {
		return err
	}
	return cli.EmitSingle(opts, m)
}

func runList(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	fs := flag.NewFlagSet("subscription-schedule list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	customer := fs.String("customer", "", "filter by customer id (cus_...)")
	scheduled := fs.Bool("scheduled", false, "filter to not-yet-started schedules")
	createdGT := fs.Int64("created-gt", 0, "filter: created > unix seconds")
	createdLT := fs.Int64("created-lt", 0, "filter: created < unix seconds")
	canceledGT := fs.Int64("canceled-at-gt", 0, "filter: canceled_at > unix seconds")
	canceledLT := fs.Int64("canceled-at-lt", 0, "filter: canceled_at < unix seconds")
	completedGT := fs.Int64("completed-at-gt", 0, "filter: completed_at > unix seconds")
	completedLT := fs.Int64("completed-at-lt", 0, "filter: completed_at < unix seconds")
	releasedGT := fs.Int64("released-at-gt", 0, "filter: released_at > unix seconds")
	releasedLT := fs.Int64("released-at-lt", 0, "filter: released_at < unix seconds")
	limit := fs.Int("limit", agentstripe.DefaultMaxResults, "max items to return (cap)")
	startingAfter := fs.String("starting-after", "", "cursor: sub_sched_... id from previous page")
	if err := fs.Parse(args); err != nil {
		return err
	}

	params := &stripeapi.SubscriptionScheduleListParams{}
	params.Limit = stripeapi.Int64(int64(min(*limit, agentstripe.MaxPageSize)))
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	if *customer != "" {
		params.Customer = stripeapi.String(*customer)
	}
	// Only attach Scheduled when the user explicitly passed --scheduled, so the
	// zero value (false) doesn't accidentally filter to started/completed
	// schedules (Stripe interprets scheduled=false as "started").
	if flagPassed(fs, "scheduled") {
		params.Scheduled = stripeapi.Bool(*scheduled)
	}
	if *startingAfter != "" {
		params.StartingAfter = stripeapi.String(*startingAfter)
	}
	if r := rangeOf(*createdGT, *createdLT); r != nil {
		params.CreatedRange = r
	}
	if r := rangeOf(*canceledGT, *canceledLT); r != nil {
		params.CanceledAtRange = r
	}
	if r := rangeOf(*completedGT, *completedLT); r != nil {
		params.CompletedAtRange = r
	}
	if r := rangeOf(*releasedGT, *releasedLT); r != nil {
		params.ReleasedAtRange = r
	}
	if opts.Stream {
		params.Limit = stripeapi.Int64(agentstripe.MaxPageSize)
	}
	return cli.RunListOrStream(ctx, opts, opts.Client.V1SubscriptionSchedules.List(ctx, params), *limit, cli.LimitExplicit(fs))
}

func rangeOf(gt, lt int64) *stripeapi.RangeQueryParams {
	if gt == 0 && lt == 0 {
		return nil
	}
	r := &stripeapi.RangeQueryParams{}
	if gt > 0 {
		r.GreaterThan = gt
	}
	if lt > 0 {
		r.LesserThan = lt
	}
	return r
}

func flagPassed(fs *flag.FlagSet, name string) bool {
	found := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}
