// Package promotioncode implements `agent-stripe promotion-code ...`.
package promotioncode

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

const Usage = `promotion-code — read Stripe promotion codes (the customer-facing string)

Subcommands:
  get <id>                                  Fetch one promotion code (promo_...)
  list [--code CODE] [--coupon ID] [--customer cus_...]
       [--active] [--inactive]
       [--created-gt T] [--created-lt T]
       [--limit N] [--starting-after PROMO]  List promotion codes

--code is a case-insensitive exact match on the redeemable string, and is the
one lookup that takes what a customer would actually type. There is no Search
API, so it is also the only server-side way to find a code by its text.

--active / --inactive filter on the active flag; passing neither returns both.
Note that a code can be active and still un-redeemable — check expires_at,
max_redemptions against times_redeemed, and the coupon's own valid flag.

A coupon is the discount definition (percent_off / amount_off, duration); a
promotion code is the string that applies it. One coupon backs many codes, so
'coupon' is where you look for "how much off", 'promotion-code' for "did this
string work".

On the pinned API version the coupon is inlined on the response at
promotion.coupon — there is no top-level 'coupon' field and nothing to expand
to reach it. --coupon on list still filters by coupon id, which is how you go
the other way: every code backed by a given coupon.

Connect: promotion codes and their coupons are per-account objects and do NOT
inherit from the platform. A code that resolves on the platform and 404s under
--stripe-account acct_... (global) — or the reverse — is the whole diagnosis
for "No such coupon" / "No such promotion code" on a connected account's
subscription. Each account needs its own.

Recommended --expand-stripe paths:
  customer

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
		Msg:  fmt.Sprintf("unknown promotion-code subcommand %q", args[0]),
		Hint: cli.SubcommandHint(args[0], []string{"get", "list"}),
		By:   output.FixableByAgent,
	}
}

func runGet(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: promotion-code get <id>")
	}
	params := &stripeapi.PromotionCodeRetrieveParams{}
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	pc, err := opts.Client.V1PromotionCodes.Retrieve(ctx, args[0], params)
	if err != nil {
		return err
	}
	return cli.EmitSingle(opts, pc)
}

func runList(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	fs := flag.NewFlagSet("promotion-code list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	code := fs.String("code", "", "filter by the redeemable string (case-insensitive exact match)")
	coupon := fs.String("coupon", "", "filter by the coupon id the code applies")
	customer := fs.String("customer", "", "filter by the customer the code is restricted to (cus_...)")
	// Two flags rather than one --active=bool: Stripe's filter is tri-state
	// (active, inactive, unset) and a bool flag cannot express "unset" without
	// the caller knowing to omit it entirely.
	active := fs.Bool("active", false, "only codes with active=true")
	inactive := fs.Bool("inactive", false, "only codes with active=false")
	createdGT := fs.Int64("created-gt", 0, "filter: created > unix seconds")
	createdLT := fs.Int64("created-lt", 0, "filter: created < unix seconds")
	limit := fs.Int("limit", agentstripe.DefaultMaxResults, "max items to return (cap)")
	startingAfter := fs.String("starting-after", "", "cursor: promo_... id from previous page")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *active && *inactive {
		return &output.Error{
			Msg:  "--active and --inactive are mutually exclusive",
			Hint: "omit both to return active and inactive codes together",
			By:   output.FixableByAgent,
		}
	}

	params := &stripeapi.PromotionCodeListParams{}
	params.Limit = stripeapi.Int64(int64(min(*limit, agentstripe.MaxPageSize)))
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	if *code != "" {
		params.Code = stripeapi.String(*code)
	}
	if *coupon != "" {
		params.Coupon = stripeapi.String(*coupon)
	}
	if *customer != "" {
		params.Customer = stripeapi.String(*customer)
	}
	if *active {
		params.Active = stripeapi.Bool(true)
	}
	if *inactive {
		params.Active = stripeapi.Bool(false)
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
	return cli.RunListOrStream(ctx, opts, opts.Client.V1PromotionCodes.List(ctx, params), *limit, cli.LimitExplicit(fs))
}
