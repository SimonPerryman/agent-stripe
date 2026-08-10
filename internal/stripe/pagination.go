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
//
// raw selects the decoding source for each item (see ToRawMap). The cursor is
// unaffected: pagination still rides the typed iterator, and `id` reads the
// same off either map.
func CollectRawList[T any](ctx context.Context, list *stripeapi.V1List[T], maxResults int, raw bool) (items []map[string]any, hasMore bool, nextCursor string, err error) {
	if maxResults <= 0 {
		maxResults = DefaultMaxResults
	}
	items = make([]map[string]any, 0, maxResults)
	for item, iterErr := range list.All(ctx) {
		if iterErr != nil {
			return items, false, "", iterErr
		}
		m, id, mErr := toRawMap(item, raw)
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
func toRawMap(item any, raw bool) (map[string]any, string, error) {
	m, err := ToRawMap(item, raw)
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

// ToRawMap converts a Stripe resource into a map[string]any so it can flow
// through the output package's renderer. `raw` picks which of two sources
// that map is decoded from, and the two differ in what they can represent:
//
//   - raw == false (default): marshal the SDK response struct. Anything the
//     pinned API version does not model is absent, because the struct had
//     nowhere to put it — no error, no warning, indistinguishable from
//     "Stripe didn't send it". In exchange the shape is stable and the
//     omitempty repair below applies.
//   - raw == true (--raw): decode the body Stripe actually sent. Every field
//     on the wire survives, including ones this SDK version predates or
//     postdates. Nothing is repaired because nothing was lost.
func ToRawMap(item any, raw bool) (map[string]any, error) {
	if raw {
		b := RawJSONOf(item)
		if b == nil {
			return nil, errNoRawJSON(item)
		}
		return DecodeRawJSON(b)
	}
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
// identically. The default path marshals the SDK struct rather than Stripe's
// wire JSON, so the tag is ours to compensate for. --raw does not call this:
// the wire body carries the real values and stamping struct fields over them
// would be inventing data, not restoring it.
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
