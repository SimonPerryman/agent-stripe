// Package transfer implements `agent-stripe transfer ...`.
package transfer

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/simonperryman/agent-stripe/internal/cli"
	agentstripe "github.com/simonperryman/agent-stripe/internal/stripe"

	stripeapi "github.com/stripe/stripe-go/v85"
)

const Usage = `transfer — read Stripe transfers (platform-account view)

Subcommands:
  get <id>                                  Fetch one transfer (tr_...)
  list [--transfer-group G] [--destination acct_...]
       [--created-gt T] [--created-lt T]
       [--limit N] [--starting-after TR]    List transfers (cursor-paginated)
  reversals <transfer-id>                   List reversals on a transfer
                                            (the first 10 are inline on the
                                            transfer object; only needed for >10)
  reversal <transfer-id> <rev-id>           Fetch one reversal (trr_...)

Note: Stripe does not offer a Search API for transfers — use list with
--transfer-group / --destination / --created-gt/lt filters. There is no
metadata search; metadata-based lookup needs client-side filter over list
results.

Streaming: pass --stream (top-level) on list or reversals to emit NDJSON
over pages until --limit or exhausted.

Scope: this command operates on the platform account only. Listing transfers
from a connected account's perspective (Stripe-Account header) is v2; no
--stripe-account flag exists today.

Cross-reference: a transfer's source_transaction is the originating ch_...
(common for destination-charge flows) and its balance_transaction is the
platform-account BT that records the funds leaving. To walk
transfer → underlying charge → fees, combine
` + "`transfer get … --expand-stripe source_transaction.balance_transaction`" + `
with ` + "`charge get`" + `.

Recommended --expand-stripe paths:
  destination, source_transaction, balance_transaction,
  source_transaction.balance_transaction, reversals

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
	case "reversals":
		return runReversals(ctx, opts, args[1:])
	case "reversal":
		return runReversal(ctx, opts, args[1:])
	case "usage", "help":
		fmt.Fprintln(os.Stderr, Usage)
		return nil
	}
	return fmt.Errorf("unknown transfer subcommand %q", args[0])
}

func runGet(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: transfer get <id>")
	}
	params := &stripeapi.TransferRetrieveParams{}
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	t, err := opts.Client.V1Transfers.Retrieve(ctx, args[0], params)
	if err != nil {
		return err
	}
	m, err := agentstripe.ToRawMap(t)
	if err != nil {
		return err
	}
	return cli.EmitSingle(opts, m)
}

func runList(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	fs := flag.NewFlagSet("transfer list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	transferGroup := fs.String("transfer-group", "", "filter by transfer_group")
	destination := fs.String("destination", "", "filter by destination connected account id")
	createdGT := fs.Int64("created-gt", 0, "filter: created > unix seconds")
	createdLT := fs.Int64("created-lt", 0, "filter: created < unix seconds")
	limit := fs.Int("limit", agentstripe.DefaultMaxResults, "max items to return (cap)")
	startingAfter := fs.String("starting-after", "", "cursor: tr_... id from previous page")
	if err := fs.Parse(args); err != nil {
		return err
	}

	params := &stripeapi.TransferListParams{}
	params.Limit = stripeapi.Int64(int64(min(*limit, 100)))
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	if *transferGroup != "" {
		params.TransferGroup = stripeapi.String(*transferGroup)
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

	if opts.Stream {
		params.Limit = stripeapi.Int64(100)
	}
	return cli.RunListOrStream(ctx, opts, opts.Client.V1Transfers.List(ctx, params), *limit, cli.LimitExplicit(fs))
}

func runReversals(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	fs := flag.NewFlagSet("transfer reversals", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	limit := fs.Int("limit", agentstripe.DefaultMaxResults, "max items to return (cap)")
	startingAfter := fs.String("starting-after", "", "cursor: trr_... id from previous page")
	// Pull the positional transfer-id out before flag parsing so flags can
	// appear before or after it (Go's flag package stops at the first non-flag).
	var transferID string
	flags := make([]string, 0, len(args))
	for _, a := range args {
		if transferID == "" && !strings.HasPrefix(a, "-") {
			transferID = a
			continue
		}
		flags = append(flags, a)
	}
	if transferID == "" {
		return errors.New("usage: transfer reversals <transfer-id> [--limit N] [--starting-after TRR]")
	}
	if err := fs.Parse(flags); err != nil {
		return err
	}

	params := &stripeapi.TransferReversalListParams{ID: stripeapi.String(transferID)}
	params.Limit = stripeapi.Int64(int64(min(*limit, 100)))
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	if *startingAfter != "" {
		params.StartingAfter = stripeapi.String(*startingAfter)
	}

	if opts.Stream {
		params.Limit = stripeapi.Int64(100)
	}
	return cli.RunListOrStream(ctx, opts, opts.Client.V1TransferReversals.List(ctx, params), *limit, cli.LimitExplicit(fs))
}

func runReversal(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	if len(args) != 2 {
		return errors.New("usage: transfer reversal <transfer-id> <rev-id>")
	}
	transferID, reversalID := args[0], args[1]
	params := &stripeapi.TransferReversalRetrieveParams{ID: stripeapi.String(transferID)}
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	r, err := opts.Client.V1TransferReversals.Retrieve(ctx, reversalID, params)
	if err != nil {
		return err
	}
	m, err := agentstripe.ToRawMap(r)
	if err != nil {
		return err
	}
	return cli.EmitSingle(opts, m)
}
