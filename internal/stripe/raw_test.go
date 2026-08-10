package stripe

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	stripeapi "github.com/stripe/stripe-go/v85"
)

// serve replies to every request with the given body, in order, repeating the
// last one once exhausted.
func serve(t *testing.T, bodies ...string) *httptest.Server {
	t.Helper()
	i := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := bodies[min(i, len(bodies)-1)]
		i++
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// The whole point of --raw. `application_fee_amount` was dropped from Invoice
// in Stripe's Basil release, so the pinned SDK's struct has nowhere to put it
// — the typed path drops it silently while Stripe is plainly sending it.
func TestRawKeepsFieldsThePinnedStructCannotHold(t *testing.T) {
	const body = `{"id":"in_1","object":"invoice","total":2000,` +
		`"application_fee_amount":300,"charge":"ch_1","subscription":"sub_1"}`
	srv := serve(t, body)
	client := NewClient("sk_test_fake", srv.URL, "", 5*time.Second)

	inv, err := client.V1Invoices.Retrieve(context.Background(), "in_1", nil)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}

	typed, err := ToRawMap(inv, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"application_fee_amount", "charge", "subscription"} {
		if _, ok := typed[k]; ok {
			t.Errorf("typed path unexpectedly kept %q — has the SDK started modelling it? "+
				"If so this test's premise is stale, not the feature", k)
		}
	}

	raw, err := ToRawMap(inv, true)
	if err != nil {
		t.Fatal(err)
	}
	if raw["application_fee_amount"] != float64(300) {
		t.Errorf("application_fee_amount = %v, want 300", raw["application_fee_amount"])
	}
	if raw["charge"] != "ch_1" || raw["subscription"] != "sub_1" {
		t.Errorf("raw dropped linkage fields: %v", raw)
	}
	// Fields the struct *does* model must not go missing in raw mode either.
	if raw["total"] != float64(2000) {
		t.Errorf("total = %v, want 2000", raw["total"])
	}
}

// The list iterator re-splits each page's `data` array and hands every item
// its own recorded body. That is what lets --raw work on lists without a
// second request path — if it regresses, raw list output silently degrades to
// the struct shape.
func TestRawListItemsCarryTheirOwnBody(t *testing.T) {
	const page = `{"object":"list","has_more":false,"url":"/v1/invoices","data":[` +
		`{"id":"in_1","object":"invoice","application_fee_amount":100},` +
		`{"id":"in_2","object":"invoice","application_fee_amount":200}]}`
	srv := serve(t, page)
	client := NewClient("sk_test_fake", srv.URL, "", 5*time.Second)

	items, hasMore, cursor, err := CollectRawList(context.Background(),
		client.V1Invoices.List(context.Background(), &stripeapi.InvoiceListParams{}), 50, true)
	if err != nil {
		t.Fatalf("CollectRawList: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0]["application_fee_amount"] != float64(100) || items[1]["application_fee_amount"] != float64(200) {
		t.Errorf("per-item raw bodies lost: %v", items)
	}
	// Pagination still rides the typed iterator: the cursor reads off the map
	// the same way in either mode.
	if hasMore {
		t.Error("hasMore should be false on a single exhausted page")
	}
	if cursor != "in_2" {
		t.Errorf("nextCursor = %q, want in_2", cursor)
	}
}

// Cursor bookkeeping under raw mode across a page boundary — the id comes off
// the decoded wire body rather than the struct field.
func TestRawListCursorAdvancesAcrossPages(t *testing.T) {
	srv := serve(t,
		`{"object":"list","has_more":true,"url":"/v1/invoices","data":[{"id":"in_1","object":"invoice"}]}`,
		`{"object":"list","has_more":true,"url":"/v1/invoices","data":[{"id":"in_2","object":"invoice"}]}`,
	)
	client := NewClient("sk_test_fake", srv.URL, "", 5*time.Second)

	items, hasMore, cursor, err := CollectRawList(context.Background(),
		client.V1Invoices.List(context.Background(), &stripeapi.InvoiceListParams{}), 2, true)
	if err != nil {
		t.Fatalf("CollectRawList: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if !hasMore {
		t.Error("hasMore should be true — the cap was hit with pages remaining")
	}
	if cursor != "in_2" {
		t.Errorf("nextCursor = %q, want in_2 (the last item collected)", cursor)
	}
}

func TestRawStreamEmitsWireFields(t *testing.T) {
	srv := serve(t, `{"object":"list","has_more":false,"url":"/v1/invoices","data":[`+
		`{"id":"in_1","object":"invoice","application_fee_amount":100}]}`)
	client := NewClient("sk_test_fake", srv.URL, "", 5*time.Second)

	var seen []map[string]any
	count, err := StreamRawList(context.Background(),
		client.V1Invoices.List(context.Background(), &stripeapi.InvoiceListParams{}), 0, true,
		func(m map[string]any) error {
			seen = append(seen, m)
			return nil
		})
	if err != nil {
		t.Fatalf("StreamRawList: %v", err)
	}
	if count != 1 || len(seen) != 1 {
		t.Fatalf("count = %d, seen = %d, want 1 / 1", count, len(seen))
	}
	if seen[0]["application_fee_amount"] != float64(100) {
		t.Errorf("stream dropped a raw field: %v", seen[0])
	}
}

// A value the SDK never received directly has no body to emit. Returning the
// struct-marshalled map instead would hand back output that looks raw and is
// not — the exact failure --raw exists to close — so it is an error.
func TestRawOnAProjectedValueIsAnError(t *testing.T) {
	if got := RawJSONOf(&stripeapi.BankAccount{ID: "ba_1"}); got != nil {
		t.Fatalf("expected no recorded body on a hand-built value, got %q", got)
	}
	_, err := ToRawMap(&stripeapi.BankAccount{ID: "ba_1"}, true)
	if err == nil {
		t.Fatal("expected an error rather than a silent fall back to the struct path")
	}
	if !strings.Contains(err.Error(), "--raw") {
		t.Errorf("error should name the flag that caused it, got %v", err)
	}
}

func TestRawJSONOfHandlesNilAndNonStructs(t *testing.T) {
	var inv *stripeapi.Invoice
	for _, v := range []any{nil, inv, "not a struct", 42} {
		if got := RawJSONOf(v); got != nil {
			t.Errorf("RawJSONOf(%v) = %q, want nil", v, got)
		}
	}
}
