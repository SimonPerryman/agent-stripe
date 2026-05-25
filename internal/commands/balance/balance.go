// Package balance implements `agent-stripe balance ...`. The package wraps
// both /v1/balance (snapshot) and /v1/balance_transactions (ledger) so the
// agent reasons about both under one mental model: snapshot vs ledger.
package balance

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

const Usage = `balance — read Stripe balance (snapshot) and balance transactions (ledger)

Subcommands:
  get                                       Current available / pending balance.
                                            Snapshot, not historical.
  transactions [--type T] [--payout PO] [--currency CCY]
               [--created-gt T] [--created-lt T] [--limit N]
               [--starting-after TXN]       List balance transactions (ledger).

The --type filter on transactions is high-signal: "charge", "refund",
"payout", "stripe_fee", "application_fee" are common starting points.

Note: Stripe does not offer a Search API for balance transactions — use
transactions with --type / --payout / --currency / --created-gt/lt filters.

Streaming: pass --stream (top-level) on transactions to emit NDJSON over pages
until --limit or exhausted. get is a single snapshot — not streamable.

Common --expand-stripe paths (transactions):
  source — fetches the originating object (charge, refund, payout, …)

Help: usage | help | -h | --help`

func Run(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, Usage)
		return nil
	}
	switch args[0] {
	case "get":
		return runGet(ctx, opts, args[1:])
	case "transactions":
		return runTransactions(ctx, opts, args[1:])
	case "usage", "help":
		fmt.Fprintln(os.Stderr, Usage)
		return nil
	}
	return fmt.Errorf("unknown balance subcommand %q", args[0])
}

func runGet(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	if len(args) != 0 {
		return errors.New("usage: balance get (no args)")
	}
	params := &stripeapi.BalanceRetrieveParams{}
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	b, err := opts.Client.V1Balance.Retrieve(ctx, params)
	if err != nil {
		return err
	}
	m, err := agentstripe.ToRawMap(b)
	if err != nil {
		return err
	}
	return cli.EmitSingle(opts, m)
}

func runTransactions(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	fs := flag.NewFlagSet("balance transactions", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	typ := fs.String("type", "", "filter by transaction type (charge, refund, payout, stripe_fee, ...)")
	payout := fs.String("payout", "", "filter by payout id (po_...)")
	currency := fs.String("currency", "", "filter by currency (lowercase ISO, e.g. usd)")
	createdGT := fs.Int64("created-gt", 0, "filter: created > unix seconds")
	createdLT := fs.Int64("created-lt", 0, "filter: created < unix seconds")
	limit := fs.Int("limit", agentstripe.DefaultMaxResults, "max items to return (cap)")
	startingAfter := fs.String("starting-after", "", "cursor: txn_... id from previous page")
	if err := fs.Parse(args); err != nil {
		return err
	}

	params := &stripeapi.BalanceTransactionListParams{}
	params.Limit = stripeapi.Int64(int64(min(*limit, agentstripe.MaxPageSize)))
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	if *typ != "" {
		params.Type = stripeapi.String(*typ)
	}
	if *payout != "" {
		params.Payout = stripeapi.String(*payout)
	}
	if *currency != "" {
		params.Currency = stripeapi.String(*currency)
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
	return cli.RunListOrStream(ctx, opts, opts.Client.V1BalanceTransactions.List(ctx, params), *limit, cli.LimitExplicit(fs))
}
