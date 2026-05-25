// Package product implements `agent-stripe product ...`.
package product

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

const Usage = `product — read Stripe products

Subcommands:
  get <id>                                  Fetch one product (prod_...)
  list [--active true|false] [--ids a,b,c] [--created-gt T] [--created-lt T]
       [--limit N] [--starting-after PROD]  List products (cursor-paginated)
  search --query <q> [--limit N] [--page T] Stripe Search (eventual consistency
                                            ~1 min lag; --page is the opaque
                                            next_page token, NOT a prod_ id)

Search query syntax: https://docs.stripe.com/search#search-query-language
Example: product search --query 'active:"true" AND name~"shirt"'

Pagination: list uses --starting-after (a prod_ id); search uses --page (opaque
next_page token). Not interchangeable.

Streaming: pass --stream (top-level) on list/search to emit NDJSON over pages
until --limit or exhausted.

Notes:
  --active is tri-state: omit for no filter, --active=true for active only,
  --active=false for archived only.
  --ids batches up to 100 lookups in a single call — prefer it when you already
  know the product ids (e.g. from a list of prices).

Catalogs tend to be small. The default --limit is usually enough on its own.

Recommended --expand-stripe paths:
  default_price

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
	case "search":
		return runSearch(ctx, opts, args[1:])
	case "usage", "help":
		fmt.Fprintln(os.Stderr, Usage)
		return nil
	}
	return fmt.Errorf("unknown product subcommand %q", args[0])
}

func runGet(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: product get <id>")
	}
	params := &stripeapi.ProductRetrieveParams{}
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	p, err := opts.Client.V1Products.Retrieve(ctx, args[0], params)
	if err != nil {
		return err
	}
	m, err := agentstripe.ToRawMap(p)
	if err != nil {
		return err
	}
	return cli.EmitSingle(opts, m)
}

func runList(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	fs := flag.NewFlagSet("product list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	active := fs.String("active", "", `tri-state: "", "true", or "false"`)
	ids := fs.String("ids", "", "comma-separated product ids (up to 100)")
	createdGT := fs.Int64("created-gt", 0, "filter: created > unix seconds")
	createdLT := fs.Int64("created-lt", 0, "filter: created < unix seconds")
	limit := fs.Int("limit", agentstripe.DefaultMaxResults, "max items to return (cap)")
	startingAfter := fs.String("starting-after", "", "cursor: prod_... id from previous page")
	if err := fs.Parse(args); err != nil {
		return err
	}

	params := &stripeapi.ProductListParams{}
	params.Limit = stripeapi.Int64(int64(min(*limit, 100)))
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	if *active != "" {
		b, err := parseTriBool(*active)
		if err != nil {
			return fmt.Errorf("--active: %w", err)
		}
		params.Active = stripeapi.Bool(b)
	}
	if *ids != "" {
		params.IDs = splitStripeStrings(*ids)
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
		params.Limit = stripeapi.Int64(100)
	}
	return cli.RunListOrStream(ctx, opts, opts.Client.V1Products.List(ctx, params), *limit, cli.LimitExplicit(fs))
}

// parseTriBool accepts "true" / "false" for the tri-state --active flag.
// Empty string is handled by the caller (means "unset, no filter").
func parseTriBool(s string) (bool, error) {
	switch s {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, fmt.Errorf("expected true or false, got %q", s)
}

func splitStripeStrings(s string) []*string {
	parts := strings.Split(s, ",")
	out := make([]*string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		v := p
		out = append(out, &v)
	}
	return out
}

func runSearch(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	sf, err := cli.ParseSearchFlags("product search", args, agentstripe.DefaultMaxResults)
	if err != nil {
		return err
	}
	params := &stripeapi.ProductSearchParams{}
	params.Query = sf.Query
	params.Limit = stripeapi.Int64(int64(min(sf.Limit, 100)))
	if sf.Page != "" {
		params.Page = stripeapi.String(sf.Page)
	}
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	if opts.Stream {
		params.Limit = stripeapi.Int64(100)
	}
	return cli.RunSearchOrStream(ctx, opts, opts.Client.V1Products.Search(ctx, params), sf.Limit, sf.LimitExplicit)
}
