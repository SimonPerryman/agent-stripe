// Package coupon implements `agent-stripe coupon ...`.
package coupon

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

const Usage = `coupon — read Stripe coupons (the discount definition)

Subcommands:
  get <id>                                  Fetch one coupon by id
  list [--created-gt T] [--created-lt T]
       [--limit N] [--starting-after ID]    List coupons (cursor-paginated)

Coupon ids are caller-chosen strings, not prefixed like most Stripe ids — a
coupon may be named "LAUNCH20" or a random token. There is no Search API, and
no filter by name or code: to find one you do not have the id for, list and
filter client-side, or look it up through the promotion code that references it
(` + "`promotion-code list --code LAUNCH20`" + `).

A coupon is the discount definition (percent_off / amount_off, duration); a
promotion code is the customer-facing string that applies it. One coupon can
back many promotion codes.

Connect: coupons are per-account objects and do NOT inherit from the platform.
A coupon created on the platform and referenced from a connected account's
subscription fails with "No such coupon" — the coupon simply does not exist on
that account's books. Confirming which account it lives on is the diagnosis:

  agent-stripe coupon get LAUNCH20                        # platform
  agent-stripe --stripe-account acct_... coupon get LAUNCH20   # the account

A 404 from one and a hit from the other is the answer. Coupons cannot be shared
across accounts; each account needs its own.

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
		Msg:  fmt.Sprintf("unknown coupon subcommand %q", args[0]),
		Hint: cli.SubcommandHint(args[0], []string{"get", "list"}),
		By:   output.FixableByAgent,
	}
}

func runGet(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: coupon get <id>")
	}
	params := &stripeapi.CouponRetrieveParams{}
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	c, err := opts.Client.V1Coupons.Retrieve(ctx, args[0], params)
	if err != nil {
		return err
	}
	return cli.EmitSingle(opts, c)
}

func runList(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	fs := flag.NewFlagSet("coupon list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	createdGT := fs.Int64("created-gt", 0, "filter: created > unix seconds")
	createdLT := fs.Int64("created-lt", 0, "filter: created < unix seconds")
	limit := fs.Int("limit", agentstripe.DefaultMaxResults, "max items to return (cap)")
	startingAfter := fs.String("starting-after", "", "cursor: coupon id from previous page")
	if err := fs.Parse(args); err != nil {
		return err
	}

	params := &stripeapi.CouponListParams{}
	params.Limit = stripeapi.Int64(int64(min(*limit, agentstripe.MaxPageSize)))
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
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
	return cli.RunListOrStream(ctx, opts, opts.Client.V1Coupons.List(ctx, params), *limit, cli.LimitExplicit(fs))
}
