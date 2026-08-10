package stripe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	stripeapi "github.com/stripe/stripe-go/v85"
)

func newSearchParams(q string) *stripeapi.CustomerSearchParams {
	p := &stripeapi.CustomerSearchParams{}
	p.Query = q
	return p
}

// newPaginatedSearchServer mimics Stripe Search: opaque next_page tokens
// instead of starting_after, has_more flag on the last page.
func newPaginatedSearchServer(t *testing.T, total, pageSize int) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		page := r.URL.Query().Get("page")
		start := 0
		if page != "" {
			var n int
			if _, err := fmt.Sscanf(page, "tok_%d", &n); err == nil {
				start = n
			}
		}
		end := start + pageSize
		if end > total {
			end = total
		}
		data := make([]map[string]any, 0, end-start)
		for i := start; i < end; i++ {
			data = append(data, map[string]any{"id": fmt.Sprintf("cus_%d", i), "object": "customer"})
		}
		hasMore := end < total
		resp := map[string]any{
			"object":   "search_result",
			"data":     data,
			"has_more": hasMore,
			"url":      "/v1/customers/search",
		}
		if hasMore {
			resp["next_page"] = fmt.Sprintf("tok_%d", end)
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestCollectRawSearch_SinglePage(t *testing.T) {
	srv, _ := newPaginatedSearchServer(t, 3, 10)
	client := NewClient("sk_test_fake", srv.URL, "", 5*time.Second)

	params := &stripeapi.CustomerSearchParams{}
	params.Query = "email:\"a@b.com\""
	items, hasMore, cursor, err := CollectRawSearch(context.Background(), client.V1Customers.Search(context.Background(), params), 50)
	if err != nil {
		t.Fatalf("CollectRawSearch: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	if hasMore {
		t.Error("expected hasMore=false on exhausted search")
	}
	if cursor != "" {
		t.Errorf("expected empty cursor on exhaustion, got %q", cursor)
	}
}

func TestCollectRawSearch_MultiPage(t *testing.T) {
	srv, calls := newPaginatedSearchServer(t, 25, 10)
	client := NewClient("sk_test_fake", srv.URL, "", 5*time.Second)

	params := &stripeapi.CustomerSearchParams{}
	params.Query = "email:\"a@b.com\""
	items, hasMore, _, err := CollectRawSearch(context.Background(), client.V1Customers.Search(context.Background(), params), 100)
	if err != nil {
		t.Fatalf("CollectRawSearch: %v", err)
	}
	if len(items) != 25 {
		t.Fatalf("expected 25 items, got %d", len(items))
	}
	if hasMore {
		t.Error("expected hasMore=false on exhausted search")
	}
	if atomic.LoadInt32(calls) < 3 {
		t.Errorf("expected >=3 HTTP calls, got %d", atomic.LoadInt32(calls))
	}
}

func TestCollectRawSearch_CapReturnsNextPage(t *testing.T) {
	srv, _ := newPaginatedSearchServer(t, 50, 10)
	client := NewClient("sk_test_fake", srv.URL, "", 5*time.Second)

	params := &stripeapi.CustomerSearchParams{}
	params.Query = "x"
	items, hasMore, cursor, err := CollectRawSearch(context.Background(), client.V1Customers.Search(context.Background(), params), 7)
	if err != nil {
		t.Fatalf("CollectRawSearch: %v", err)
	}
	if len(items) != 7 {
		t.Fatalf("expected 7 items, got %d", len(items))
	}
	if !hasMore {
		t.Error("expected hasMore=true when cap stops before exhaustion")
	}
	if cursor == "" {
		t.Error("expected opaque next_page cursor, got empty string")
	}
}

func TestCollectRawSearch_Empty(t *testing.T) {
	srv, _ := newPaginatedSearchServer(t, 0, 10)
	client := NewClient("sk_test_fake", srv.URL, "", 5*time.Second)
	items, hasMore, _, err := CollectRawSearch(context.Background(), client.V1Customers.Search(context.Background(), newSearchParams("x")), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Errorf("expected empty, got %d", len(items))
	}
	if hasMore {
		t.Error("expected hasMore=false on empty search")
	}
}

func TestCollectRawSearch_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"message":"boom"}}`)
	}))
	defer srv.Close()
	client := NewClient("sk_test_fake", srv.URL, "", 5*time.Second)
	_, _, _, err := CollectRawSearch(context.Background(), client.V1Customers.Search(context.Background(), newSearchParams("x")), 10)
	if err == nil {
		t.Fatal("expected error to propagate")
	}
}

func TestStreamRawSearch_EmitsIncrementally(t *testing.T) {
	srv, _ := newPaginatedSearchServer(t, 12, 5)
	client := NewClient("sk_test_fake", srv.URL, "", 5*time.Second)

	var emitted []string
	count, err := StreamRawSearch(context.Background(), client.V1Customers.Search(context.Background(), newSearchParams("x")), 0, func(m map[string]any) error {
		emitted = append(emitted, m["id"].(string))
		return nil
	})
	if err != nil {
		t.Fatalf("StreamRawSearch: %v", err)
	}
	if count != 12 || len(emitted) != 12 {
		t.Errorf("expected 12 emits, got count=%d len=%d", count, len(emitted))
	}
}

func TestStreamRawSearch_MaxResultsStops(t *testing.T) {
	srv, _ := newPaginatedSearchServer(t, 50, 10)
	client := NewClient("sk_test_fake", srv.URL, "", 5*time.Second)
	count, err := StreamRawSearch(context.Background(), client.V1Customers.Search(context.Background(), newSearchParams("x")), 3, func(m map[string]any) error { return nil })
	if err != nil {
		t.Fatalf("StreamRawSearch: %v", err)
	}
	if count != 3 {
		t.Errorf("expected count=3, got %d", count)
	}
}

func TestStreamRawSearch_EmitError(t *testing.T) {
	srv, _ := newPaginatedSearchServer(t, 5, 5)
	client := NewClient("sk_test_fake", srv.URL, "", 5*time.Second)
	sentinel := errors.New("broken pipe")
	_, err := StreamRawSearch(context.Background(), client.V1Customers.Search(context.Background(), newSearchParams("x")), 0, func(m map[string]any) error {
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel, got %v", err)
	}
}

func TestStreamRawSearch_ContextCancellation(t *testing.T) {
	srv, _ := newPaginatedSearchServer(t, 50, 10)
	client := NewClient("sk_test_fake", srv.URL, "", 5*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := StreamRawSearch(ctx, client.V1Customers.Search(ctx, newSearchParams("x")), 0, func(m map[string]any) error { return nil })
	if err == nil {
		t.Fatal("expected error after context cancel")
	}
}
