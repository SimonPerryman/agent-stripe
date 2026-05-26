package checkoutsession

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/simonperryman/agent-stripe/internal/testutil"
)

func TestCheckoutSessionList_CustomerAndStatusPassthrough(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"object":"list","data":[],"has_more":false,"url":"/v1/checkout/sessions"}`)
	}))
	defer srv.Close()

	opts := testutil.NewOpts(srv.URL)
	testutil.CaptureStdout(t)
	if err := runList(context.Background(), opts, []string{"--customer", "cus_x", "--status", "complete"}); err != nil {
		t.Fatalf("runList: %v", err)
	}
	if !strings.Contains(got, "customer=cus_x") {
		t.Errorf("expected customer=cus_x in query, got %q", got)
	}
	if !strings.Contains(got, "status=complete") {
		t.Errorf("expected status=complete in query, got %q", got)
	}
}

func TestCheckoutSessionGet_ExpandLineItems(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"id":"cs_1","object":"checkout.session"}`)
	}))
	defer srv.Close()

	opts := testutil.NewOpts(srv.URL)
	opts.ExpandStripe = []string{"line_items"}
	testutil.CaptureStdout(t)
	if err := runGet(context.Background(), opts, []string{"cs_1"}); err != nil {
		t.Fatalf("runGet: %v", err)
	}
	if !strings.Contains(got, "line_items") || !strings.Contains(got, "expand") {
		t.Errorf("expected expand[]=line_items, got %q", got)
	}
}
