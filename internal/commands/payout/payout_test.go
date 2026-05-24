package payout

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

func TestPayoutList_StatusPassthrough(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"object":"list","data":[],"has_more":false,"url":"/v1/payouts"}`)
	}))
	defer srv.Close()

	opts := &cli.GlobalOpts{
		Account: &config.Account{Alias: "test", Mode: config.ModeTest},
		Client:  agentstripe.NewClient("sk_test_fake", srv.URL, 5*time.Second),
	}
	restore := redirectStdout(t)
	if err := runList(context.Background(), opts, []string{"--status", "pending"}); err != nil {
		t.Fatalf("runList: %v", err)
	}
	restore()

	if !strings.Contains(gotQuery, "status=pending") {
		t.Errorf("expected status=pending in query, got %q", gotQuery)
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
