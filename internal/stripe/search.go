package stripe

import (
	"context"

	stripeapi "github.com/stripe/stripe-go/v85"
)

// CollectRawSearch drains a V1SearchList iterator up to maxResults, returning
// each item as a decoded map.
//
// hasMore reports whether more pages remain after the cap. nextCursor carries
// Stripe Search's opaque `next_page` token — note that this is NOT
// interchangeable with the `list` cursor (which is an object id passed via
// `starting_after`). Search results are routed back through `--page`.
func CollectRawSearch[T any](ctx context.Context, list *stripeapi.V1SearchList[T], maxResults int, raw bool) (items []map[string]any, hasMore bool, nextCursor string, err error) {
	if maxResults <= 0 {
		maxResults = DefaultMaxResults
	}
	items = make([]map[string]any, 0, maxResults)
	for item, iterErr := range list.All(ctx) {
		if iterErr != nil {
			return items, false, "", iterErr
		}
		m, _, mErr := toRawMap(item, raw)
		if mErr != nil {
			return items, false, "", mErr
		}
		items = append(items, m)
		if len(items) >= maxResults {
			meta := list.Meta()
			hasMore = meta.HasMore
			if meta.NextPage != nil {
				nextCursor = *meta.NextPage
			}
			return items, hasMore, nextCursor, nil
		}
	}
	// Iterator exhausted before cap — no more pages.
	return items, false, "", nil
}
