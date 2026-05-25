package stripe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	stripeapi "github.com/stripe/stripe-go/v85"
)

// newPaginatedCustomerServer returns an httptest server that serves
// /v1/customers as N pages of size `pageSize`. Each "customer" has id
// `cus_<n>`. has_more flips to false on the last page. starting_after is
// echoed back so the test can assert cursor passthrough.
func newPaginatedCustomerServer(t *testing.T, total, pageSize int) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.URL.Path != "/v1/customers" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		startingAfter := r.URL.Query().Get("starting_after")
		start := 0
		if startingAfter != "" {
			// cus_<n> → n+1
			var n int
			if _, err := fmt.Sscanf(startingAfter, "cus_%d", &n); err == nil {
				start = n + 1
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
			"object":   "list",
			"data":     data,
			"has_more": hasMore,
			"url":      "/v1/customers",
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestCollectRawList_SinglePage(t *testing.T) {
	srv, _ := newPaginatedCustomerServer(t, 3, 10)
	client := NewClient("sk_test_fake", srv.URL, 5*time.Second)

	params := &stripeapi.CustomerListParams{}
	params.Limit = stripeapi.Int64(10)
	items, hasMore, cursor, err := CollectRawList(context.Background(), client.V1Customers.List(context.Background(), params), 50)
	if err != nil {
		t.Fatalf("CollectRawList: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	if hasMore {
		t.Error("expected hasMore=false on exhausted iterator")
	}
	if cursor != "cus_2" {
		t.Errorf("expected cursor cus_2, got %q", cursor)
	}
	if items[0]["id"] != "cus_0" {
		t.Errorf("expected first item id cus_0, got %v", items[0]["id"])
	}
}

func TestCollectRawList_MultiPage(t *testing.T) {
	// 25 items, page size 10 → 3 requests (10, 10, 5)
	srv, calls := newPaginatedCustomerServer(t, 25, 10)
	client := NewClient("sk_test_fake", srv.URL, 5*time.Second)

	params := &stripeapi.CustomerListParams{}
	params.Limit = stripeapi.Int64(10)
	items, hasMore, cursor, err := CollectRawList(context.Background(), client.V1Customers.List(context.Background(), params), 100)
	if err != nil {
		t.Fatalf("CollectRawList: %v", err)
	}
	if len(items) != 25 {
		t.Fatalf("expected 25 items, got %d", len(items))
	}
	if hasMore {
		t.Error("expected hasMore=false at end of stream")
	}
	if cursor != "cus_24" {
		t.Errorf("expected cursor cus_24, got %q", cursor)
	}
	if atomic.LoadInt32(calls) < 3 {
		t.Errorf("expected >=3 HTTP calls, got %d", atomic.LoadInt32(calls))
	}
}

func TestCollectRawList_CapHonouredAndHasMore(t *testing.T) {
	// 50 items, cap 7 → stop at 7, has_more should still be true.
	srv, _ := newPaginatedCustomerServer(t, 50, 10)
	client := NewClient("sk_test_fake", srv.URL, 5*time.Second)

	params := &stripeapi.CustomerListParams{}
	params.Limit = stripeapi.Int64(10)
	items, hasMore, cursor, err := CollectRawList(context.Background(), client.V1Customers.List(context.Background(), params), 7)
	if err != nil {
		t.Fatalf("CollectRawList: %v", err)
	}
	if len(items) != 7 {
		t.Fatalf("expected 7 items, got %d", len(items))
	}
	if !hasMore {
		t.Error("expected hasMore=true when cap hit before exhaustion")
	}
	if cursor != "cus_6" {
		t.Errorf("expected cursor cus_6, got %q", cursor)
	}
}

func TestCollectRawList_Empty(t *testing.T) {
	srv, _ := newPaginatedCustomerServer(t, 0, 10)
	client := NewClient("sk_test_fake", srv.URL, 5*time.Second)

	items, hasMore, cursor, err := CollectRawList(context.Background(), client.V1Customers.List(context.Background(), &stripeapi.CustomerListParams{}), 0)
	if err != nil {
		t.Fatalf("CollectRawList: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected 0 items, got %d", len(items))
	}
	if hasMore {
		t.Error("expected hasMore=false on empty result")
	}
	if cursor != "" {
		t.Errorf("expected empty cursor, got %q", cursor)
	}
}

func TestCollectRawList_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"message":"boom"}}`)
	}))
	defer srv.Close()
	client := NewClient("sk_test_fake", srv.URL, 5*time.Second)

	_, _, _, err := CollectRawList(context.Background(), client.V1Customers.List(context.Background(), &stripeapi.CustomerListParams{}), 10)
	if err == nil {
		t.Fatal("expected server error to propagate")
	}
}

func TestCollectRawList_ContextCancellation(t *testing.T) {
	// Serve a multi-page response slowly so we can cancel mid-flight.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"cus_x","object":"customer"}],"has_more":true,"url":"/v1/customers"}`)
	}))
	defer srv.Close()
	client := NewClient("sk_test_fake", srv.URL, 5*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, _, err := CollectRawList(ctx, client.V1Customers.List(ctx, &stripeapi.CustomerListParams{}), 100)
	if err == nil {
		t.Fatal("expected error after context cancel")
	}
}

func TestExpandSlice(t *testing.T) {
	if ExpandSlice(nil) != nil {
		t.Error("expected nil for empty input")
	}
	out := ExpandSlice([]string{"a", "b"})
	if len(out) != 2 || *out[0] != "a" || *out[1] != "b" {
		t.Errorf("unexpected expand slice: %+v", out)
	}
}

func TestToRawMap_RoundTrip(t *testing.T) {
	type X struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	m, err := ToRawMap(X{ID: "x_1", Name: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if m["id"] != "x_1" || m["name"] != "alice" {
		t.Errorf("unexpected map: %v", m)
	}
}

func TestCollectRawList_LimitClampedToDefault(t *testing.T) {
	// Passing maxResults<=0 falls through to DefaultMaxResults.
	// 5 items, default cap exceeds total, so we drain fully.
	srv, _ := newPaginatedCustomerServer(t, 5, 10)
	client := NewClient("sk_test_fake", srv.URL, 5*time.Second)
	items, hasMore, _, err := CollectRawList(context.Background(), client.V1Customers.List(context.Background(), &stripeapi.CustomerListParams{}), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 5 {
		t.Errorf("expected 5 items, got %d", len(items))
	}
	if hasMore {
		t.Error("expected hasMore=false")
	}
}

// Sanity: starting_after on the URL is the cursor convention, used by
// multi-page traversal under the hood.
func TestCollectRawList_StartingAfterPassedToServer(t *testing.T) {
	var seenCursors []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenCursors = append(seenCursors, r.URL.Query().Get("starting_after"))
		// Force a tiny second page.
		page := len(seenCursors)
		switch page {
		case 1:
			_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"cus_a","object":"customer"}],"has_more":true,"url":"/v1/customers"}`)
		default:
			_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"cus_b","object":"customer"}],"has_more":false,"url":"/v1/customers"}`)
		}
	}))
	defer srv.Close()
	client := NewClient("sk_test_fake", srv.URL, 5*time.Second)
	items, _, _, err := CollectRawList(context.Background(), client.V1Customers.List(context.Background(), &stripeapi.CustomerListParams{}), 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items across two pages, got %d", len(items))
	}
	if len(seenCursors) < 2 || !strings.Contains(seenCursors[1], "cus_a") {
		t.Errorf("expected starting_after=cus_a on second call, got %v", seenCursors)
	}
}
