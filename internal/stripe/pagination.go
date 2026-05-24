package stripe

import (
	"context"
	"encoding/json"

	stripeapi "github.com/stripe/stripe-go/v85"
)

// DefaultMaxResults is the cap on items returned by a bounded `list`.
const DefaultMaxResults = 100

// CollectRawList drains a V1List iterator up to maxResults, returning each
// item as a decoded map. hasMore is true if more pages remain after the cap.
// nextCursor is the id of the last collected item (Stripe's cursor convention).
func CollectRawList[T any](ctx context.Context, list *stripeapi.V1List[T], maxResults int) (items []map[string]any, hasMore bool, nextCursor string, err error) {
	if maxResults <= 0 {
		maxResults = DefaultMaxResults
	}
	items = make([]map[string]any, 0, maxResults)
	for item, iterErr := range list.All(ctx) {
		if iterErr != nil {
			return items, false, "", iterErr
		}
		m, id, mErr := toRawMap(item)
		if mErr != nil {
			return items, false, "", mErr
		}
		items = append(items, m)
		if id != "" {
			nextCursor = id
		}
		if len(items) >= maxResults {
			return items, list.Meta().HasMore, nextCursor, nil
		}
	}
	return items, false, nextCursor, nil
}

func toRawMap(item any) (map[string]any, string, error) {
	b, err := json.Marshal(item)
	if err != nil {
		return nil, "", err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, "", err
	}
	id, _ := m["id"].(string)
	return m, id, nil
}

// ExpandSlice converts a flat []string into the []*string that Stripe's SDK
// expects for params.Expand. Returns nil when the input is empty so the
// generated query string omits the expand[]= keys entirely.
func ExpandSlice(paths []string) []*string {
	if len(paths) == 0 {
		return nil
	}
	out := make([]*string, len(paths))
	for i, p := range paths {
		s := p
		out[i] = &s
	}
	return out
}

// ToRawMap converts any Stripe resource into a map[string]any via JSON
// round-trip so it can flow through the output package's renderer.
func ToRawMap(item any) (map[string]any, error) {
	b, err := json.Marshal(item)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}
