package product

import (
	"context"
	"github.com/simonperryman/agent-stripe/internal/testutil"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProductList_ActiveTrue(t *testing.T) {
	q := captureQuery(t, []string{"--active", "true"})
	if !strings.Contains(q, "active=true") {
		t.Errorf("expected active=true, got %q", q)
	}
}

func TestProductList_ActiveFalse(t *testing.T) {
	q := captureQuery(t, []string{"--active", "false"})
	if !strings.Contains(q, "active=false") {
		t.Errorf("expected active=false, got %q", q)
	}
}

func TestProductList_ActiveUnset(t *testing.T) {
	q := captureQuery(t, []string{})
	if strings.Contains(q, "active=") {
		t.Errorf("expected no active key, got %q", q)
	}
}

func TestProductList_ActiveInvalid(t *testing.T) {
	opts := testutil.NewOpts("http://127.0.0.1:1")
	err := runList(context.Background(), opts, []string{"--active", "banana"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "--active") {
		t.Errorf("expected --active error, got %v", err)
	}
}

func TestProductList_IDs(t *testing.T) {
	q := captureQuery(t, []string{"--ids", "prod_a,prod_b,prod_c"})
	// Stripe SDK encodes []*string as ids[0]=prod_a&ids[1]=prod_b... — check
	// presence of each id without pinning the indexed bracket form.
	for _, id := range []string{"prod_a", "prod_b", "prod_c"} {
		if !strings.Contains(q, id) {
			t.Errorf("expected %s in query, got %q", id, q)
		}
	}
	if !strings.Contains(q, "ids") {
		t.Errorf("expected ids[] key, got %q", q)
	}
}

func TestProductSearch_QueryAndPage(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"object":"search_result","data":[{"id":"prod_1","object":"product"}],"has_more":false,"next_page":"tok_next","url":"/v1/products/search"}`)
	}))
	defer srv.Close()

	opts := testutil.NewOpts(srv.URL)
	testutil.CaptureStdout(t)
	if err := runSearch(context.Background(), opts, []string{"--query", `active:"true"`, "--page", "tok_abc", "--limit", "5"}); err != nil {
		t.Fatalf("runSearch: %v", err)
	}

	if !strings.Contains(gotQuery, "query=active%3A%22true%22") {
		t.Errorf("expected query=... in querystring, got %q", gotQuery)
	}
	if !strings.Contains(gotQuery, "page=tok_abc") {
		t.Errorf("expected page=tok_abc, got %q", gotQuery)
	}
}

func TestProductSearch_MissingQueryErrors(t *testing.T) {
	opts := testutil.NewOpts("http://unused")
	if err := runSearch(context.Background(), opts, []string{}); err == nil {
		t.Fatal("expected error when --query is missing")
	}
}

func captureQuery(t *testing.T, args []string) string {
	t.Helper()
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"object":"list","data":[],"has_more":false,"url":"/v1/products"}`)
	}))
	t.Cleanup(srv.Close)
	opts := testutil.NewOpts(srv.URL)
	testutil.CaptureStdout(t)
	if err := runList(context.Background(), opts, args); err != nil {
		t.Fatalf("runList: %v", err)
	}
	return got
}
