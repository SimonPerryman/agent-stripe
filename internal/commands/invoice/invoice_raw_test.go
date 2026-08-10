package invoice

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/simonperryman/agent-stripe/internal/testutil"
)

// The invoice body the plan measured against: fields Stripe sends that the
// pinned SDK's Invoice struct has nowhere to put. application_fee_amount was
// dropped outright in Stripe's Basil release; charge, payment_intent and
// subscription moved under parent.
const rawInvoiceBody = `{"id":"in_1","object":"invoice","total":2000,` +
	`"application_fee_amount":300,"charge":"ch_1","payment_intent":"pi_1","subscription":"sub_1"}`

func rawInvoiceServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, rawInvoiceBody)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func decodeEnvelope(t *testing.T, out string) map[string]any {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("decode envelope: %v\noutput: %s", err, out)
	}
	return env
}

// Without --raw the fields are absent — no error, no warning. That is the
// behaviour --raw exists to give an escape hatch from, and pinning it here is
// what makes the raw test below meaningful rather than tautological.
func TestInvoiceGet_TypedPathDropsUnmodelledFields(t *testing.T) {
	srv := rawInvoiceServer(t)
	out := testutil.WithCapturedStdout(t, func() {
		if err := runGet(context.Background(), testutil.NewOpts(srv.URL), []string{"in_1"}); err != nil {
			t.Errorf("runGet: %v", err)
		}
	})
	env := decodeEnvelope(t, out)
	data, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("no data object in %s", out)
	}
	for _, k := range []string{"application_fee_amount", "charge", "payment_intent", "subscription"} {
		if _, present := data[k]; present {
			t.Errorf("typed path kept %q — if the SDK now models it, this test's premise is stale", k)
		}
	}
	if env["raw"] != nil {
		t.Errorf("raw marker should be absent on a default run, got %v", env["raw"])
	}
}

func TestInvoiceGet_RawKeepsUnmodelledFields(t *testing.T) {
	srv := rawInvoiceServer(t)
	out := testutil.WithCapturedStdout(t, func() {
		if err := runGet(context.Background(), testutil.NewRawOpts(srv.URL), []string{"in_1"}); err != nil {
			t.Errorf("runGet: %v", err)
		}
	})
	env := decodeEnvelope(t, out)
	if env["raw"] != true {
		t.Errorf("envelope should mark raw output, got %v", env["raw"])
	}
	data, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("no data object in %s", out)
	}
	if data["application_fee_amount"] != float64(300) {
		t.Errorf("application_fee_amount = %v, want 300 — the whole point of --raw", data["application_fee_amount"])
	}
	for k, want := range map[string]string{"charge": "ch_1", "payment_intent": "pi_1", "subscription": "sub_1"} {
		if data[k] != want {
			t.Errorf("%s = %v, want %s", k, data[k], want)
		}
	}
	// Raw output still goes through the renderer, so modelled fields and the
	// envelope are unchanged.
	if data["total"] != float64(2000) {
		t.Errorf("total = %v, want 2000", data["total"])
	}
	if env["mode"] != "test" {
		t.Errorf("mode = %v, want test", env["mode"])
	}
}

// list must honour --raw too: the iterator hands each item its own recorded
// body, so there is no separate raw pagination path to get wrong.
func TestInvoiceList_RawKeepsUnmodelledFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"object":"list","has_more":false,"url":"/v1/invoices","data":[`+rawInvoiceBody+`]}`)
	}))
	defer srv.Close()

	out := testutil.WithCapturedStdout(t, func() {
		if err := runList(context.Background(), testutil.NewRawOpts(srv.URL), nil); err != nil {
			t.Errorf("runList: %v", err)
		}
	})
	env := decodeEnvelope(t, out)
	items, ok := env["data"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("expected one item, got %s", out)
	}
	item, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("item is not an object: %v", items[0])
	}
	if item["application_fee_amount"] != float64(300) {
		t.Errorf("list item lost the raw-only field: %v", item)
	}
	page, ok := env["page"].(map[string]any)
	if !ok || page["nextCursor"] != "in_1" {
		t.Errorf("pagination metadata should be unaffected by --raw, got %v", env["page"])
	}
}
