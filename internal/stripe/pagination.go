package stripe

import (
	"context"
	"encoding/json"

	stripeapi "github.com/stripe/stripe-go/v85"
)

// DefaultMaxResults is the cap on items returned by a bounded `list`.
const DefaultMaxResults = 100

// MaxPageSize is Stripe's per-request maximum for `limit` on list endpoints.
const MaxPageSize = 100

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

// toRawMap is ToRawMap plus the item's id, which the list/stream paths need
// for the pagination cursor. It must delegate rather than re-implement the
// marshal: when these were two separate JSON round-trips, a fix applied to
// one silently missed `list` and `--stream`.
func toRawMap(item any) (map[string]any, string, error) {
	m, err := ToRawMap(item)
	if err != nil {
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
	restoreOmitemptyBools(item, m)
	return m, nil
}

// restoreOmitemptyBools puts back response booleans that stripe-go tags
// `omitempty`, which deletes them from the marshalled output when false.
//
// On Account that is actively harmful: `charges_enabled`, `payouts_enabled`
// and `details_submitted` are the summary "is this account working?" fields,
// and they disappear in exactly the case someone is investigating. A broken
// account and an account whose fields Stripe never returned render
// identically. We marshal the SDK struct rather than Stripe's wire JSON
// (that is what buys us --expand-stripe and the truncation walk), so the tag
// is ours to compensate for.
//
// Only Account is affected today — the response booleans on charge,
// subscription, payout, refund, dispute and capability carry no `omitempty`.
// Keep this list minimal and driven by an actual observed loss.
func restoreOmitemptyBools(item any, m map[string]any) {
	acct, ok := item.(*stripeapi.Account)
	if !ok {
		if v, isVal := item.(stripeapi.Account); isVal {
			acct = &v
		} else {
			return
		}
	}
	if acct == nil {
		return
	}
	// Guard against an unexpanded `acct_…` reference, where the SDK parsed an
	// id string into an otherwise-zero struct. Stamping charges_enabled:false
	// on one of those would invent a signal rather than restore one; a real
	// retrieval always carries object:"account".
	if acct.Object != "account" {
		return
	}
	m["charges_enabled"] = acct.ChargesEnabled
	m["payouts_enabled"] = acct.PayoutsEnabled
	m["details_submitted"] = acct.DetailsSubmitted
}
