package customer

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

func TestCustomerSearch_QueryAndPage(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"object":"search_result","data":[{"id":"cus_1","object":"customer"}],"has_more":false,"next_page":"tok_next","url":"/v1/customers/search"}`)
	}))
	defer srv.Close()

	opts := newOpts(srv.URL)
	restore := captureStdout(t)
	if err := runSearch(context.Background(), opts, []string{"--query", `email:"a@b.com"`, "--page", "tok_abc", "--limit", "5"}); err != nil {
		t.Fatalf("runSearch: %v", err)
	}
	restore()

	if !strings.Contains(gotQuery, "query=email%3A%22a%40b.com%22") {
		t.Errorf("expected query=email:... in querystring, got %q", gotQuery)
	}
	if !strings.Contains(gotQuery, "page=tok_abc") {
		t.Errorf("expected page=tok_abc, got %q", gotQuery)
	}
	if !strings.Contains(gotQuery, "limit=5") {
		t.Errorf("expected limit=5, got %q", gotQuery)
	}
}

func TestCustomerSearch_MissingQueryErrors(t *testing.T) {
	opts := newOpts("http://unused")
	if err := runSearch(context.Background(), opts, []string{}); err == nil {
		t.Fatal("expected error when --query is missing")
	}
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
