package stripe

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStripeAccountTransportSetsHeader(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(StripeAccountHeader)
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	c := &http.Client{Transport: NewStripeAccountTransport("acct_123", nil)}
	resp, err := c.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()
	if got != "acct_123" {
		t.Fatalf("Stripe-Account = %q, want acct_123", got)
	}
}

func TestStripeAccountTransportAbsentWhenEmpty(t *testing.T) {
	var present bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, present = r.Header[http.CanonicalHeaderKey(StripeAccountHeader)]
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	c := &http.Client{Transport: NewStripeAccountTransport("", nil)}
	resp, err := c.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()
	if present {
		t.Fatal("Stripe-Account header should be absent when no account is set")
	}
}

// A header already on the request wins, so a future per-params override is not
// clobbered by the global flag.
func TestStripeAccountTransportDoesNotOverwrite(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(StripeAccountHeader)
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set(StripeAccountHeader, "acct_explicit")
	c := &http.Client{Transport: NewStripeAccountTransport("acct_global", nil)}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()
	if got != "acct_explicit" {
		t.Fatalf("Stripe-Account = %q, want acct_explicit", got)
	}
}

// The read-only contract stays outermost: a non-GET is rejected before the
// account transport ever runs, so Connect support cannot weaken it.
func TestReadOnlyStillOutermostWithConnect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("write should never reach the server, method=%s", r.Method)
	}))
	defer srv.Close()

	c := &http.Client{Transport: NewReadOnlyTransport(NewStripeAccountTransport("acct_123", nil))}
	req, _ := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader(""))
	resp, err := c.Do(req)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if !errors.Is(err, ErrReadOnly) && (err == nil || !strings.Contains(err.Error(), ErrReadOnly.Error())) {
		t.Fatalf("expected ErrReadOnly, got %v", err)
	}
}

// End-to-end through NewClient: the header the SDK actually sends.
func TestNewClientSendsStripeAccountHeader(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(StripeAccountHeader)
		_, _ = io.WriteString(w, `{"id":"acct_123","object":"account"}`)
	}))
	defer srv.Close()

	c := NewClient("sk_test_fake", srv.URL, "acct_123", 0)
	if _, err := c.V1Accounts.Retrieve(t.Context(), nil); err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if got != "acct_123" {
		t.Fatalf("Stripe-Account = %q, want acct_123", got)
	}
}
