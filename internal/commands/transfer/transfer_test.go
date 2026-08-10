package transfer

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

func TestTransferList_FilterPassthrough(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"object":"list","data":[],"has_more":false,"url":"/v1/transfers"}`)
	}))
	defer srv.Close()

	opts := &cli.GlobalOpts{
		Account: &config.Account{Alias: "test", Mode: config.ModeTest},
		Client:  agentstripe.NewClient("sk_test_fake", srv.URL, "", 5*time.Second),
	}
	restore := redirectStdout(t)
	if err := runList(context.Background(), opts, []string{
		"--transfer-group", "group_xyz",
		"--destination", "acct_123",
		"--created-gt", "1700000000",
		"--created-lt", "1800000000",
		"--limit", "1",
		"--starting-after", "tr_prev",
	}); err != nil {
		t.Fatalf("runList: %v", err)
	}
	restore()

	for _, want := range []string{
		"transfer_group=group_xyz",
		"destination=acct_123",
		"created[gt]=1700000000",
		"created[lt]=1800000000",
		"limit=1",
		"starting_after=tr_prev",
	} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("expected %q in query, got %q", want, gotQuery)
		}
	}
}

func TestTransferReversals_PathParam(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"object":"list","data":[],"has_more":false,"url":"/v1/transfers/tr_abc/reversals"}`)
	}))
	defer srv.Close()

	opts := &cli.GlobalOpts{
		Account: &config.Account{Alias: "test", Mode: config.ModeTest},
		Client:  agentstripe.NewClient("sk_test_fake", srv.URL, "", 5*time.Second),
	}
	restore := redirectStdout(t)
	if err := runReversals(context.Background(), opts, []string{"tr_abc", "--limit", "5"}); err != nil {
		t.Fatalf("runReversals: %v", err)
	}
	restore()

	if gotPath != "/v1/transfers/tr_abc/reversals" {
		t.Errorf("path = %q, want /v1/transfers/tr_abc/reversals", gotPath)
	}
	if strings.Contains(gotQuery, "tr_abc") {
		t.Errorf("transfer id leaked into query string: %q", gotQuery)
	}
	if !strings.Contains(gotQuery, "limit=5") {
		t.Errorf("expected limit=5 in query, got %q", gotQuery)
	}
}

func TestTransferReversal_PathParams(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{"id":"trr_x","object":"transfer_reversal","transfer":"tr_abc"}`)
	}))
	defer srv.Close()

	opts := &cli.GlobalOpts{
		Account: &config.Account{Alias: "test", Mode: config.ModeTest},
		Client:  agentstripe.NewClient("sk_test_fake", srv.URL, "", 5*time.Second),
	}
	restore := redirectStdout(t)
	if err := runReversal(context.Background(), opts, []string{"tr_abc", "trr_x"}); err != nil {
		t.Fatalf("runReversal: %v", err)
	}
	restore()

	if gotPath != "/v1/transfers/tr_abc/reversals/trr_x" {
		t.Errorf("path = %q, want /v1/transfers/tr_abc/reversals/trr_x", gotPath)
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
