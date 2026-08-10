package stripe

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	stripeapi "github.com/stripe/stripe-go/v85"
)

// TestClientHitsAccountEndpoint verifies the client we hand to commands
// actually issues the expected GET against /v1/account and that the
// read-only transport doesn't block a normal call.
func TestClientHitsAccountEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/account" {
			t.Fatalf("expected /v1/account, got %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"id":"acct_123","country":"US","default_currency":"usd","business_profile":{"name":"Acme"}}`)
	}))
	defer srv.Close()

	client := NewClient("sk_test_fake", srv.URL, "", 5*time.Second)
	acct, err := client.V1Accounts.Retrieve(context.Background(), &stripeapi.AccountRetrieveParams{})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if acct.ID != "acct_123" {
		t.Fatalf("unexpected id %q", acct.ID)
	}

	m, err := ToRawMap(acct, false)
	if err != nil {
		t.Fatal(err)
	}
	// Spot-check the JSON round-trip preserves the wire shape.
	b, _ := json.Marshal(m)
	if !contains(b, "acct_123") {
		t.Fatalf("expected id in marshalled output: %s", string(b))
	}
}

func contains(b []byte, s string) bool {
	return len(b) >= len(s) && (string(b) == s || stringContains(string(b), s))
}

func stringContains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
