// Package price implements `agent-stripe price ...`.
package price

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

const Usage = `price — read Stripe prices

Subcommands:
  get <id>                                  Fetch one price (price_...)
  list [--product PROD] [--active true|false] [--currency usd]
       [--type recurring|one_time] [--lookup-keys k1,k2]
       [--limit N] [--starting-after PRICE] List prices (cursor-paginated)
  search --query <q> [--limit N] [--page T] Stripe Search (eventual consistency
                                            ~1 min lag; --page is the opaque
                                            next_page token, NOT a price_ id)

Search query syntax: https://docs.stripe.com/search#search-query-language
Example: price search --query 'product:"prod_123" AND active:"true"'

Pagination: list uses --starting-after (a price_ id); search uses --page (opaque
next_page token). Not interchangeable.

Streaming: pass --stream (top-level) on list/search to emit NDJSON over pages
until --limit or exhausted.

Every price belongs to a product. When fetching a single price, pass
--expand-stripe product to see the parent in the same response.

--lookup-keys is the highest-signal filter when you're working from
human-readable identifiers (e.g. "pro_monthly_usd"). Up to 10 keys.

--active is tri-state: omit for no filter, --active=true for active only,
--active=false for archived only. --currency is normalised to lowercase
silently (Stripe is case-insensitive on input).`

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
	return fmt.Errorf("unknown price subcommand %q", args[0])
}

func runGet(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: price get <id>")
	}
	params := &stripeapi.PriceRetrieveParams{}
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	p, err := opts.Client.V1Prices.Retrieve(ctx, args[0], params)
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
	fs := flag.NewFlagSet("price list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	product := fs.String("product", "", "filter by product id (prod_...)")
	active := fs.String("active", "", `tri-state: "", "true", or "false"`)
	currency := fs.String("currency", "", "ISO 4217 currency (e.g. usd)")
	priceType := fs.String("type", "", "recurring or one_time")
	lookupKeys := fs.String("lookup-keys", "", "comma-separated lookup keys (up to 10)")
	limit := fs.Int("limit", agentstripe.DefaultMaxResults, "max items to return (cap)")
	startingAfter := fs.String("starting-after", "", "cursor: price_... id from previous page")
	if err := fs.Parse(args); err != nil {
		return err
	}

	params := &stripeapi.PriceListParams{}
	params.Limit = stripeapi.Int64(int64(min(*limit, 100)))
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	if *product != "" {
		params.Product = stripeapi.String(*product)
	}
	if *active != "" {
		b, err := parseTriBool(*active)
		if err != nil {
			return fmt.Errorf("--active: %w", err)
		}
		params.Active = stripeapi.Bool(b)
	}
	if *currency != "" {
		params.Currency = stripeapi.String(strings.ToLower(*currency))
	}
	if *priceType != "" {
		params.Type = stripeapi.String(*priceType)
	}
	if *lookupKeys != "" {
		params.LookupKeys = splitStripeStrings(*lookupKeys)
	}
	if *startingAfter != "" {
		params.StartingAfter = stripeapi.String(*startingAfter)
	}

	if opts.Stream {
		params.Limit = stripeapi.Int64(100)
	}
	return cli.RunListOrStream(ctx, opts, opts.Client.V1Prices.List(ctx, params), *limit, cli.LimitExplicit(fs))
}

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
	sf, err := cli.ParseSearchFlags("price search", args, agentstripe.DefaultMaxResults)
	if err != nil {
		return err
	}
	params := &stripeapi.PriceSearchParams{}
	params.Query = sf.Query
	params.Limit = stripeapi.Int64(int64(min(sf.Limit, 100)))
	if sf.Page != "" {
		params.Page = stripeapi.String(sf.Page)
	}
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	if opts.Stream {
		params.Limit = stripeapi.Int64(100)
	}
	return cli.RunSearchOrStream(ctx, opts, opts.Client.V1Prices.Search(ctx, params), sf.Limit, sf.LimitExplicit)
}
