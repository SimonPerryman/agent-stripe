// Package testclock implements `agent-stripe test-clock ...`, the read half of
// Stripe's /v1/test_helpers/test_clocks. Advancing a clock is a write and stays
// out of scope; reading one is what makes a stalled billing verification
// diagnosable.
package testclock

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

const Usage = `test-clock — read Stripe test clocks (test mode only)

Subcommands:
  get <id>                                  Fetch one test clock (clock_...)
  list [--limit N] [--starting-after CLOCK] List test clocks

Every subscription-billing verification runs on a test clock, and when one
appears stuck there are two very different causes that look identical from the
outside: the clock never advanced, or it advanced and the webhook did not fire.
Reading the clock is what tells them apart.

  status      ready            = the advance finished; downstream is your problem
              advancing        = still processing; objects have NOT caught up yet
              internal_failure = Stripe gave up on the advance; the clock is dead
                                 and nothing further will happen on it
  frozen_time where the clock actually is now. Compare against the
              current_period_end of the subscription you expected to renew — if
              frozen_time has not passed it, no invoice was due and nothing is
              wrong except the expectation.

status_details carries the target time while an advance is in flight, so an
'advancing' clock tells you what it is advancing toward.

Advancing is a write and this CLI is read-only — use the Stripe CLI or the
Dashboard to move a clock, then read it back here.

Test clocks exist only in test mode. Against a live-mode account Stripe returns
an error, which is correct rather than a bug: there is nothing to read.

Objects created under a clock carry a test_clock field (subscription, customer,
invoice, quote), so an object behaving oddly is worth checking for one — a
customer frozen in the past explains a great deal on its own.

Connect: clocks are per-account. A connected account's clock is invisible from
the platform, so pass --stripe-account acct_... (global) when the subscription
being verified lives on that account.

No Search API for test clocks.

Streaming: pass --stream (top-level) on list to emit NDJSON over pages until
--limit or exhausted.

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
		Msg:  fmt.Sprintf("unknown test-clock subcommand %q", args[0]),
		Hint: cli.SubcommandHint(args[0], []string{"get", "list"}),
		By:   output.FixableByAgent,
	}
}

func runGet(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: test-clock get <id>")
	}
	params := &stripeapi.TestHelpersTestClockRetrieveParams{}
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	c, err := opts.Client.V1TestHelpersTestClocks.Retrieve(ctx, args[0], params)
	if err != nil {
		return err
	}
	return cli.EmitSingle(opts, c)
}

func runList(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	fs := flag.NewFlagSet("test-clock list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	limit := fs.Int("limit", agentstripe.DefaultMaxResults, "max items to return (cap)")
	startingAfter := fs.String("starting-after", "", "cursor: clock_... id from previous page")
	if err := fs.Parse(args); err != nil {
		return err
	}

	params := &stripeapi.TestHelpersTestClockListParams{}
	params.Limit = stripeapi.Int64(int64(min(*limit, agentstripe.MaxPageSize)))
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	if *startingAfter != "" {
		params.StartingAfter = stripeapi.String(*startingAfter)
	}

	if opts.Stream {
		params.Limit = stripeapi.Int64(agentstripe.MaxPageSize)
	}
	return cli.RunListOrStream(ctx, opts, opts.Client.V1TestHelpersTestClocks.List(ctx, params), *limit, cli.LimitExplicit(fs))
}
