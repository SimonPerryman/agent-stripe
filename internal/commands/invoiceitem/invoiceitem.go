// Package invoiceitem implements `agent-stripe invoice-item ...`.
package invoiceitem

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

const Usage = `invoice-item — read Stripe invoice items

Subcommands:
  get <id>                                  Fetch one invoice item (ii_...)
  list [--customer C] [--invoice INV] [--pending]
       [--created-gt T] [--created-lt T]
       [--limit N] [--starting-after II]    List invoice items (cursor-paginated)

Don't confuse with invoice line items. An InvoiceItem (ii_...) is a pre-invoice
charge sitting on a customer; an invoice line (il_...) is the finalized line on
a specific invoice. They overlap but are different resources — see
'invoice lines <invoice_id>' for finalized lines.

--pending filters to items not yet attached to an invoice ("what would the
next invoice for this customer include?").

No Search API for invoice items.

Streaming: pass --stream (top-level) on list to emit NDJSON over pages until
--limit or exhausted.

Connect: invoice items belong to the account that owns the invoice, so a
connected account's items need --stripe-account acct_... (global).

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
	return fmt.Errorf("unknown invoice-item subcommand %q", args[0])
}

func runGet(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: invoice-item get <id>")
	}
	params := &stripeapi.InvoiceItemRetrieveParams{}
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	ii, err := opts.Client.V1InvoiceItems.Retrieve(ctx, args[0], params)
	if err != nil {
		return err
	}
	m, err := agentstripe.ToRawMap(ii)
	if err != nil {
		return err
	}
	return cli.EmitSingle(opts, m)
}

func runList(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	fs := flag.NewFlagSet("invoice-item list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	customer := fs.String("customer", "", "filter by customer id (cus_...)")
	invoice := fs.String("invoice", "", "filter by invoice id (in_...)")
	pending := fs.Bool("pending", false, "filter to items not yet attached to an invoice")
	createdGT := fs.Int64("created-gt", 0, "filter: created > unix seconds")
	createdLT := fs.Int64("created-lt", 0, "filter: created < unix seconds")
	limit := fs.Int("limit", agentstripe.DefaultMaxResults, "max items to return (cap)")
	startingAfter := fs.String("starting-after", "", "cursor: ii_... id from previous page")
	if err := fs.Parse(args); err != nil {
		return err
	}

	params := &stripeapi.InvoiceItemListParams{}
	params.Limit = stripeapi.Int64(int64(min(*limit, agentstripe.MaxPageSize)))
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	if *customer != "" {
		params.Customer = stripeapi.String(*customer)
	}
	if *invoice != "" {
		params.Invoice = stripeapi.String(*invoice)
	}
	if flagPassed(fs, "pending") {
		params.Pending = stripeapi.Bool(*pending)
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
	return cli.RunListOrStream(ctx, opts, opts.Client.V1InvoiceItems.List(ctx, params), *limit, cli.LimitExplicit(fs))
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
