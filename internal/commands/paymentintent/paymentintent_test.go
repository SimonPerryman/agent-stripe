package paymentintent

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/simonperryman/agent-stripe/internal/cli"
	"github.com/simonperryman/agent-stripe/internal/config"
	agentstripe "github.com/simonperryman/agent-stripe/internal/stripe"
)

func TestPaymentIntentGet_ExpandLatestCharge(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		if r.URL.Path != "/v1/payment_intents/pi_1" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"id":"pi_1","object":"payment_intent"}`)
	}))
	defer srv.Close()

	opts := &cli.GlobalOpts{
		Account:      &config.Account{Alias: "test", Mode: config.ModeTest},
		Client:       agentstripe.NewClient("sk_test_fake", srv.URL, 5*time.Second),
		ExpandStripe: []string{"latest_charge"},
	}
	restore := redirectStdout(t)
	if err := runGet(context.Background(), opts, []string{"pi_1"}); err != nil {
		t.Fatalf("runGet: %v", err)
	}
	restore()

	if !strings.Contains(gotQuery, "latest_charge") || !strings.Contains(gotQuery, "expand") {
		t.Errorf("expected expand of latest_charge in %q", gotQuery)
	}
}

func TestPaymentIntentSearch_QueryAndPage(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"object":"search_result","data":[{"id":"pi_1","object":"payment_intent"}],"has_more":false,"next_page":"tok_next","url":"/v1/payment_intents/search"}`)
	}))
	defer srv.Close()

	opts := &cli.GlobalOpts{
		Account: &config.Account{Alias: "test", Mode: config.ModeTest},
		Client:  agentstripe.NewClient("sk_test_fake", srv.URL, 5*time.Second),
	}
	restore := redirectStdout(t)
	if err := runSearch(context.Background(), opts, []string{"--query", `status:"succeeded"`, "--page", "tok_abc", "--limit", "5"}); err != nil {
		t.Fatalf("runSearch: %v", err)
	}
	restore()

	if !strings.Contains(gotQuery, "query=status%3A%22succeeded%22") {
		t.Errorf("expected query=... in querystring, got %q", gotQuery)
	}
	if !strings.Contains(gotQuery, "page=tok_abc") {
		t.Errorf("expected page=tok_abc, got %q", gotQuery)
	}
}

func TestPaymentIntentSearch_MissingQueryErrors(t *testing.T) {
	opts := &cli.GlobalOpts{
		Account: &config.Account{Alias: "test", Mode: config.ModeTest},
		Client:  agentstripe.NewClient("sk_test_fake", "http://unused", 5*time.Second),
	}
	if err := runSearch(context.Background(), opts, []string{}); err == nil {
		t.Fatal("expected error when --query is missing")
	}
}

func redirectStdout(t *testing.T) func() {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, r)
		close(done)
	}()
	return func() {
		_ = w.Close()
		os.Stdout = old
		<-done
	}
}
