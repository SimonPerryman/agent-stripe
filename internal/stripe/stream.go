package stripe

import (
	"context"

	stripeapi "github.com/stripe/stripe-go/v85"
)

// StreamRawList walks list.All(ctx) and calls emit for each item (already
// converted to map[string]any). Stops at maxResults (0 = unlimited). Returns
// the count emitted and any iterator error. Pagination is handled by the
// underlying iterator — caller's job is to wire StartingAfter into params
// before calling.
//
// If emit returns an error (e.g. broken pipe), StreamRawList returns it
// immediately without draining further pages.
func StreamRawList[T any](ctx context.Context, list *stripeapi.V1List[T], maxResults int, emit func(map[string]any) error) (count int, err error) {
	for item, iterErr := range list.All(ctx) {
		if iterErr != nil {
			return count, iterErr
		}
		m, _, mErr := toRawMap(item)
		if mErr != nil {
			return count, mErr
		}
		if emitErr := emit(m); emitErr != nil {
			return count, emitErr
		}
		count++
		if maxResults > 0 && count >= maxResults {
			return count, nil
		}
	}
	return count, nil
}

// StreamRawSearch is the V1SearchList[T] counterpart of StreamRawList.
// Search pagination uses the opaque next_page token internally; from the
// caller's perspective the iterator just keeps going until exhausted.
func StreamRawSearch[T any](ctx context.Context, list *stripeapi.V1SearchList[T], maxResults int, emit func(map[string]any) error) (count int, err error) {
	for item, iterErr := range list.All(ctx) {
		if iterErr != nil {
			return count, iterErr
		}
		m, _, mErr := toRawMap(item)
		if mErr != nil {
			return count, mErr
		}
		if emitErr := emit(m); emitErr != nil {
			return count, emitErr
		}
		count++
		if maxResults > 0 && count >= maxResults {
			return count, nil
		}
	}
	return count, nil
}
