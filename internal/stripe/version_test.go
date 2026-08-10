package stripe

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	stripeapi "github.com/stripe/stripe-go/v85"
)

func TestAPIVersionTransportSetsHeader(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(APIVersionHeader)
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	c := &http.Client{Transport: NewAPIVersionTransport("2022-11-15", nil)}
	resp, err := c.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()
	if got != "2022-11-15" {
		t.Fatalf("Stripe-Version = %q, want 2022-11-15", got)
	}
}

// Unlike Stripe-Account, the SDK always sets this header itself, so the
// override must replace rather than defer. If this ever regresses to the
// "don't overwrite" rule, --api-version becomes a silent no-op.
func TestAPIVersionTransportOverwritesExistingHeader(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(APIVersionHeader)
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set(APIVersionHeader, PinnedAPIVersion)
	c := &http.Client{Transport: NewAPIVersionTransport("2020-08-27", nil)}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()
	if got != "2020-08-27" {
		t.Fatalf("Stripe-Version = %q, want 2020-08-27", got)
	}
}

func TestAPIVersionTransportIsPassthroughWhenEmpty(t *testing.T) {
	inner := http.DefaultTransport
	if got := NewAPIVersionTransport("", inner); got == nil {
		t.Fatal("expected the inner transport back")
	} else if _, wrapped := got.(*apiVersionTransport); wrapped {
		t.Fatal("empty version should not wrap the transport")
	}
}

// End-to-end through NewClient: the header Stripe actually receives.
func TestNewClientSendsOverriddenAPIVersion(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(APIVersionHeader)
		_, _ = io.WriteString(w, `{"id":"acct_123","object":"account"}`)
	}))
	defer srv.Close()

	c := NewClient("sk_test_fake", srv.URL, "", 0, WithAPIVersion("2022-11-15"))
	if _, err := c.V1Accounts.Retrieve(t.Context(), nil); err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if got != "2022-11-15" {
		t.Fatalf("Stripe-Version = %q, want 2022-11-15", got)
	}
}

// With no override the SDK's pinned version must still go out — the option is
// additive, not a replacement for the pin.
func TestNewClientSendsPinnedAPIVersionByDefault(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(APIVersionHeader)
		_, _ = io.WriteString(w, `{"id":"acct_123","object":"account"}`)
	}))
	defer srv.Close()

	c := NewClient("sk_test_fake", srv.URL, "", 0)
	if _, err := c.V1Accounts.Retrieve(t.Context(), nil); err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if got != PinnedAPIVersion {
		t.Fatalf("Stripe-Version = %q, want the pinned %q", got, PinnedAPIVersion)
	}
}

// Both headers compose, and the read-only guarantee stays outermost with a
// third transport in the chain.
func TestVersionAndConnectComposeUnderReadOnly(t *testing.T) {
	var version, account string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		version = r.Header.Get(APIVersionHeader)
		account = r.Header.Get(StripeAccountHeader)
		_, _ = io.WriteString(w, `{"id":"acct_123","object":"account"}`)
	}))
	defer srv.Close()

	c := NewClient("sk_test_fake", srv.URL, "acct_123", 0, WithAPIVersion("2022-11-15"))
	if _, err := c.V1Accounts.Retrieve(t.Context(), &stripeapi.AccountRetrieveParams{}); err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if version != "2022-11-15" || account != "acct_123" {
		t.Fatalf("headers = %q / %q, want 2022-11-15 / acct_123", version, account)
	}

	rt := NewReadOnlyTransport(NewStripeAccountTransport("acct_123", NewAPIVersionTransport("2022-11-15", nil)))
	req, _ := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader(""))
	resp, err := (&http.Client{Transport: rt}).Do(req)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if !errors.Is(err, ErrReadOnly) && (err == nil || !strings.Contains(err.Error(), ErrReadOnly.Error())) {
		t.Fatalf("expected ErrReadOnly through the full chain, got %v", err)
	}
}
