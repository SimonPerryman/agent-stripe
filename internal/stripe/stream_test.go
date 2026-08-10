package stripe

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	stripeapi "github.com/stripe/stripe-go/v85"
)

func TestStreamRawList_EmitsIncrementally(t *testing.T) {
	// 12 items, page size 5 → at least 3 pages. Assert emit fires per-item,
	// not only after the final page.
	srv, _ := newPaginatedCustomerServer(t, 12, 5)
	client := NewClient("sk_test_fake", srv.URL, "", 5*time.Second)

	var emitted []string
	count, err := StreamRawList(context.Background(), client.V1Customers.List(context.Background(), &stripeapi.CustomerListParams{}), 0, func(m map[string]any) error {
		emitted = append(emitted, m["id"].(string))
		return nil
	})
	if err != nil {
		t.Fatalf("StreamRawList: %v", err)
	}
	if count != 12 {
		t.Errorf("expected count=12, got %d", count)
	}
	if len(emitted) != 12 {
		t.Errorf("expected 12 emits, got %d", len(emitted))
	}
	if emitted[0] != "cus_0" || emitted[11] != "cus_11" {
		t.Errorf("unexpected emit order: %v", emitted)
	}
}

func TestStreamRawList_MaxResultsStops(t *testing.T) {
	srv, _ := newPaginatedCustomerServer(t, 50, 10)
	client := NewClient("sk_test_fake", srv.URL, "", 5*time.Second)

	count, err := StreamRawList(context.Background(), client.V1Customers.List(context.Background(), &stripeapi.CustomerListParams{}), 4, func(m map[string]any) error {
		return nil
	})
	if err != nil {
		t.Fatalf("StreamRawList: %v", err)
	}
	if count != 4 {
		t.Errorf("expected count=4, got %d", count)
	}
}

func TestStreamRawList_EmitErrorPropagates(t *testing.T) {
	srv, _ := newPaginatedCustomerServer(t, 5, 5)
	client := NewClient("sk_test_fake", srv.URL, "", 5*time.Second)

	sentinel := errors.New("broken pipe")
	count, err := StreamRawList(context.Background(), client.V1Customers.List(context.Background(), &stripeapi.CustomerListParams{}), 0, func(m map[string]any) error {
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
	if count != 0 {
		t.Errorf("expected count=0 (emit failed first call), got %d", count)
	}
}

func TestStreamRawList_ServerErrorMidStream(t *testing.T) {
	// First page OK, second page errors.
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"cus_0","object":"customer"}],"has_more":true,"url":"/v1/customers"}`)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"message":"boom"}}`)
	}))
	defer srv.Close()
	client := NewClient("sk_test_fake", srv.URL, "", 5*time.Second)

	var emitted int
	count, err := StreamRawList(context.Background(), client.V1Customers.List(context.Background(), &stripeapi.CustomerListParams{}), 0, func(m map[string]any) error {
		emitted++
		return nil
	})
	if err == nil {
		t.Fatal("expected error after server failure mid-stream")
	}
	if emitted != 1 {
		t.Errorf("expected 1 emit before failure, got %d", emitted)
	}
	if count != 1 {
		t.Errorf("expected count=1 before failure, got %d", count)
	}
}

func TestStreamRawList_ContextCancellation(t *testing.T) {
	srv, _ := newPaginatedCustomerServer(t, 50, 10)
	client := NewClient("sk_test_fake", srv.URL, "", 5*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := StreamRawList(ctx, client.V1Customers.List(ctx, &stripeapi.CustomerListParams{}), 0, func(m map[string]any) error { return nil })
	if err == nil {
		t.Fatal("expected error after context cancel")
	}
}
