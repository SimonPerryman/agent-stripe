package price

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

func TestPriceList_LookupKeysAndCurrency(t *testing.T) {
	q := captureQuery(t, []string{"--lookup-keys", "pro_monthly,enterprise_yearly", "--currency", "usd"})
	if !strings.Contains(q, "currency=usd") {
		t.Errorf("expected currency=usd, got %q", q)
	}
	for _, k := range []string{"pro_monthly", "enterprise_yearly"} {
		if !strings.Contains(q, k) {
			t.Errorf("expected lookup key %s in query, got %q", k, q)
		}
	}
	if !strings.Contains(q, "lookup_keys") {
		t.Errorf("expected lookup_keys[] key, got %q", q)
	}
}

func TestPriceList_TypeRecurring(t *testing.T) {
	q := captureQuery(t, []string{"--type", "recurring"})
	if !strings.Contains(q, "type=recurring") {
		t.Errorf("expected type=recurring, got %q", q)
	}
}

func TestPriceList_CurrencyNormalisedLowercase(t *testing.T) {
	q := captureQuery(t, []string{"--currency", "USD"})
	if !strings.Contains(q, "currency=usd") {
		t.Errorf("expected currency=usd (normalised), got %q", q)
	}
}

func TestPriceSearch_QueryAndPage(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"object":"search_result","data":[{"id":"price_1","object":"price"}],"has_more":false,"next_page":"tok_next","url":"/v1/prices/search"}`)
	}))
	defer srv.Close()

	opts := newOpts(srv.URL)
	restore := captureStdout(t)
	if err := runSearch(context.Background(), opts, []string{"--query", `active:"true"`, "--page", "tok_abc", "--limit", "5"}); err != nil {
		t.Fatalf("runSearch: %v", err)
	}
	restore()

	if !strings.Contains(gotQuery, "query=active%3A%22true%22") {
		t.Errorf("expected query=... in querystring, got %q", gotQuery)
	}
	if !strings.Contains(gotQuery, "page=tok_abc") {
		t.Errorf("expected page=tok_abc, got %q", gotQuery)
	}
}

func TestPriceSearch_MissingQueryErrors(t *testing.T) {
	opts := newOpts("http://unused")
	if err := runSearch(context.Background(), opts, []string{}); err == nil {
		t.Fatal("expected error when --query is missing")
	}
}

func captureQuery(t *testing.T, args []string) string {
	t.Helper()
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"object":"list","data":[],"has_more":false,"url":"/v1/prices"}`)
	}))
	t.Cleanup(srv.Close)
	opts := newOpts(srv.URL)
	restore := captureStdout(t)
	if err := runList(context.Background(), opts, args); err != nil {
		t.Fatalf("runList: %v", err)
	}
	restore()
	return got
}

func newOpts(baseURL string) *cli.GlobalOpts {
	return &cli.GlobalOpts{
		Account: &config.Account{Alias: "test", Mode: config.ModeTest},
		Client:  agentstripe.NewClient("sk_test_fake", baseURL, 5*time.Second),
	}
}

func captureStdout(t *testing.T) func() {
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
