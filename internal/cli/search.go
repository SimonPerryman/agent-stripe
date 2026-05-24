package cli

import (
	"flag"
	"fmt"
	"io"
)

// SearchFlags is the parsed form of the search flag set every resource shares.
// Query is required; Limit/Page are optional. Page carries Stripe Search's
// opaque next_page token (NOT a `list`-style starting_after id — the two are
// not interchangeable).
type SearchFlags struct {
	Query         string
	Limit         int
	Page          string
	LimitExplicit bool // true if the user passed --limit (matters under --stream)
}

// ParseSearchFlags parses the standard search flag set from argv. Returns an
// error if --query is missing — searches against Stripe with an empty query
// 400 server-side, so we fail-fast with a usage-shaped error instead.
func ParseSearchFlags(name string, args []string, defaultLimit int) (*SearchFlags, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	query := fs.String("query", "", "Stripe search query (required) — see https://docs.stripe.com/search#search-query-language")
	limit := fs.Int("limit", defaultLimit, "max items to return (cap; default 100)")
	page := fs.String("page", "", "opaque page token from a previous search (Stripe's next_page; not interchangeable with --starting-after)")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if *query == "" {
		return nil, fmt.Errorf("usage: %s --query <stripe-search-query> [--limit N] [--page TOKEN]", name)
	}
	return &SearchFlags{Query: *query, Limit: *limit, Page: *page, LimitExplicit: LimitExplicit(fs)}, nil
}
