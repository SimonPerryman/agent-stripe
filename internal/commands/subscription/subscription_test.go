package subscription

import (
	"context"
	"github.com/simonperryman/agent-stripe/internal/testutil"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSubscriptionList_StatusAndPricePassthrough(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"object":"list","data":[],"has_more":false,"url":"/v1/subscriptions"}`)
	}))
	defer srv.Close()

	opts := testutil.NewOpts(srv.URL)
	testutil.CaptureStdout(t)
	if err := runList(context.Background(), opts, []string{"--status", "active", "--price", "price_xxx"}); err != nil {
		t.Fatalf("runList: %v", err)
	}

	if !strings.Contains(got, "status=active") {
		t.Errorf("expected status=active, got %q", got)
	}
	if !strings.Contains(got, "price=price_xxx") {
		t.Errorf("expected price=price_xxx, got %q", got)
	}
}

func TestSubscriptionGet_ExpandStripeQueryString(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"id":"sub_1","object":"subscription"}`)
	}))
	defer srv.Close()

	opts := testutil.NewOpts(srv.URL)
	opts.ExpandStripe = []string{"customer", "latest_invoice"}
	testutil.CaptureStdout(t)
	if err := runGet(context.Background(), opts, []string{"sub_1"}); err != nil {
		t.Fatalf("runGet: %v", err)
	}

	if !strings.Contains(got, "customer") || !strings.Contains(got, "latest_invoice") {
		t.Errorf("expected customer + latest_invoice in query, got %q", got)
	}
	if !strings.Contains(got, "expand") {
		t.Errorf("expected expand[] key, got %q", got)
	}
}

func TestSubscriptionSearch_QueryAndPage(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"object":"search_result","data":[{"id":"sub_1","object":"subscription"}],"has_more":false,"next_page":"tok_next","url":"/v1/subscriptions/search"}`)
	}))
	defer srv.Close()

	opts := testutil.NewOpts(srv.URL)
	testutil.CaptureStdout(t)
	if err := runSearch(context.Background(), opts, []string{"--query", `status:"active"`, "--page", "tok_abc", "--limit", "5"}); err != nil {
		t.Fatalf("runSearch: %v", err)
	}

	if !strings.Contains(gotQuery, "query=status%3A%22active%22") {
		t.Errorf("expected query=... in querystring, got %q", gotQuery)
	}
	if !strings.Contains(gotQuery, "page=tok_abc") {
		t.Errorf("expected page=tok_abc, got %q", gotQuery)
	}
}

func TestSubscriptionSearch_MissingQueryErrors(t *testing.T) {
	opts := testutil.NewOpts("http://unused")
	if err := runSearch(context.Background(), opts, []string{}); err == nil {
		t.Fatal("expected error when --query is missing")
	}
}
