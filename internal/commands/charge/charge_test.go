package charge

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/simonperryman/agent-stripe/internal/testutil"
)

func TestChargeList_CustomerFilterPassthrough(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"object":"list","data":[],"has_more":false,"url":"/v1/charges"}`)
	}))
	defer srv.Close()

	opts := testutil.NewOpts(srv.URL)
	testutil.CaptureStdout(t)
	if err := runList(context.Background(), opts, []string{"--customer", "cus_123", "--limit", "5"}); err != nil {
		t.Fatalf("runList: %v", err)
	}

	if !strings.Contains(gotQuery, "customer=cus_123") {
		t.Errorf("expected customer=cus_123 in query, got %q", gotQuery)
	}
	if !strings.Contains(gotQuery, "limit=5") {
		t.Errorf("expected limit=5 in query, got %q", gotQuery)
	}
}

func TestChargeGet_ExpandStripeQueryString(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"id":"ch_1","object":"charge"}`)
	}))
	defer srv.Close()

	opts := testutil.NewOpts(srv.URL)
	opts.ExpandStripe = []string{"customer", "balance_transaction"}
	testutil.CaptureStdout(t)
	if err := runGet(context.Background(), opts, []string{"ch_1"}); err != nil {
		t.Fatalf("runGet: %v", err)
	}

	// Stripe SDK emits expand as expand[0]=...&expand[1]=... — both indexed
	// and bare-bracket forms are accepted by the Stripe API.
	if !strings.Contains(gotQuery, "customer") || !strings.Contains(gotQuery, "balance_transaction") {
		t.Errorf("expected expand of customer + balance_transaction in query, got %q", gotQuery)
	}
	if !strings.Contains(gotQuery, "expand") {
		t.Errorf("expected expand[] key in query, got %q", gotQuery)
	}
}

func TestChargeList_StartingAfterCursor(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"object":"list","data":[],"has_more":false,"url":"/v1/charges"}`)
	}))
	defer srv.Close()

	opts := testutil.NewOpts(srv.URL)
	testutil.CaptureStdout(t)
	if err := runList(context.Background(), opts, []string{"--starting-after", "ch_prev"}); err != nil {
		t.Fatalf("runList: %v", err)
	}

	if !strings.Contains(gotQuery, "starting_after=ch_prev") {
		t.Errorf("expected starting_after=ch_prev in query, got %q", gotQuery)
	}
}

func TestChargeSearch_QueryAndPage(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"object":"search_result","data":[],"has_more":false,"url":"/v1/charges/search"}`)
	}))
	defer srv.Close()

	opts := testutil.NewOpts(srv.URL)
	testutil.CaptureStdout(t)
	if err := runSearch(context.Background(), opts, []string{"--query", `status:"succeeded"`, "--page", "tok_abc", "--limit", "5"}); err != nil {
		t.Fatalf("runSearch: %v", err)
	}

	if !strings.Contains(gotQuery, "query=status%3A%22succeeded%22") {
		t.Errorf("expected query=... in querystring, got %q", gotQuery)
	}
	if !strings.Contains(gotQuery, "page=tok_abc") {
		t.Errorf("expected page=tok_abc in querystring, got %q", gotQuery)
	}
}

func TestChargeSearch_MissingQueryErrors(t *testing.T) {
	opts := testutil.NewOpts("http://unused")
	if err := runSearch(context.Background(), opts, []string{}); err == nil {
		t.Fatal("expected error when --query is missing")
	}
}
