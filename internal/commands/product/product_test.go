package product

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shhac/agent-stripe/internal/cli"
	"github.com/shhac/agent-stripe/internal/config"
	agentstripe "github.com/shhac/agent-stripe/internal/stripe"
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
	opts := newOpts("http://127.0.0.1:1")
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

func captureQuery(t *testing.T, args []string) string {
	t.Helper()
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"object":"list","data":[],"has_more":false,"url":"/v1/products"}`)
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
