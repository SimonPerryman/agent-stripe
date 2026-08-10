package stripe

import (
	"encoding/json"
	"fmt"
	"reflect"

	stripeapi "github.com/stripe/stripe-go/v85"
)

// RawJSONOf returns the response body Stripe actually sent for item, or nil
// if the SDK did not record one.
//
// Every response struct in stripe-go embeds stripeapi.APIResource, whose
// LastResponse carries the undecoded body. Crucially that is populated per
// *item* on list and search pages too, not just on single retrievals: the
// iterators re-split the page's `data` array and hand each element its own
// RawJSON (see the SDK's maybeAddLastResponseV1). That is what makes --raw a
// change of decoding source rather than a second request path.
//
// nil comes back for anything the SDK did not receive directly from a
// response — a nested sub-object projected out of a parent (an external
// account off Account, say) has no LastResponse of its own. Callers in raw
// mode must source those from the parent's body instead of quietly falling
// back to the struct.
func RawJSONOf(item any) []byte {
	v := reflect.ValueOf(item)
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}
	// FieldByName promotes through the embedded APIResource.
	f := v.FieldByName("LastResponse")
	if !f.IsValid() || !f.CanInterface() {
		return nil
	}
	resp, ok := f.Interface().(*stripeapi.APIResponse)
	if !ok || resp == nil || len(resp.RawJSON) == 0 {
		return nil
	}
	return resp.RawJSON
}

// DecodeRawJSON unmarshals a recorded response body into a map.
func DecodeRawJSON(b []byte) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("decoding Stripe's raw response body: %w", err)
	}
	return m, nil
}

// errNoRawJSON reports that --raw was asked for on a value the SDK never
// received directly. It is a bug rather than a user mistake — returning the
// struct-marshalled map instead would hand back output that looks raw and
// silently is not, which is the exact failure --raw exists to close.
func errNoRawJSON(item any) error {
	return fmt.Errorf("--raw: no response body recorded for %T; the value was projected out of another object rather than received from Stripe", item)
}
