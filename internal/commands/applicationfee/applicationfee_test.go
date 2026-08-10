package applicationfee

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/simonperryman/agent-stripe/internal/output"
	"github.com/simonperryman/agent-stripe/internal/testutil"
)

func recordingServer(t *testing.T, body string) (*httptest.Server, *http.Request) {
	t.Helper()
	var last http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		last = *r
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &last
}

func TestGet_Smoke(t *testing.T) {
	srv, last := recordingServer(t, `{"id":"fee_1","object":"application_fee","amount":250}`)
	testutil.CaptureStdout(t)
	if err := runGet(context.Background(), testutil.NewOpts(srv.URL), []string{"fee_1"}); err != nil {
		t.Fatalf("runGet: %v", err)
	}
	if last.URL.Path != "/v1/application_fees/fee_1" {
		t.Errorf("path = %q", last.URL.Path)
	}
}

func TestList_FilterPassthrough(t *testing.T) {
	srv, last := recordingServer(t, `{"object":"list","data":[{"id":"fee_1","object":"application_fee"}],"has_more":false,"url":"/v1/application_fees"}`)
	testutil.CaptureStdout(t)
	args := []string{"--charge", "ch_9", "--created-gt", "100", "--created-lt", "200", "--limit", "5"}
	if err := runList(context.Background(), testutil.NewOpts(srv.URL), args); err != nil {
		t.Fatalf("runList: %v", err)
	}
	q := last.URL.Query()
	for field, want := range map[string]string{
		"charge":      "ch_9",
		"created[gt]": "100",
		"created[lt]": "200",
		"limit":       "5",
	} {
		if got := q.Get(field); got != want {
			t.Errorf("%s = %q, want %q", field, got, want)
		}
	}
}

func TestRefunds_PathIncludesFee(t *testing.T) {
	srv, last := recordingServer(t, `{"object":"list","data":[{"id":"fr_1","object":"fee_refund"}],"has_more":false,"url":"/v1/application_fees/fee_1/refunds"}`)
	testutil.CaptureStdout(t)
	if err := runRefunds(context.Background(), testutil.NewOpts(srv.URL), []string{"fee_1", "--limit", "2"}); err != nil {
		t.Fatalf("runRefunds: %v", err)
	}
	if last.URL.Path != "/v1/application_fees/fee_1/refunds" {
		t.Errorf("path = %q", last.URL.Path)
	}
	if got := last.URL.Query().Get("limit"); got != "2" {
		t.Errorf("limit = %q, want 2", got)
	}
}

func TestRequiredPositionals(t *testing.T) {
	opts := testutil.NewOpts("http://127.0.0.1:1")
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"get", runGet(context.Background(), opts, nil)},
		{"refunds", runRefunds(context.Background(), opts, nil)},
	} {
		if tc.err == nil {
			t.Errorf("%s: expected a usage error with no positional arg", tc.name)
		} else if !strings.Contains(tc.err.Error(), "usage:") {
			t.Errorf("%s: expected usage error, got %v", tc.name, tc.err)
		}
	}
}

func TestRun_UnknownSubcommand(t *testing.T) {
	err := Run(context.Background(), testutil.NewOpts("http://127.0.0.1:1"), []string{"refund"})
	if err == nil {
		t.Fatal("expected an error for an unknown subcommand")
	}
	if !strings.Contains(err.Error(), "refund") {
		t.Errorf("error should name the bad subcommand, got %v", err)
	}
}

func TestRun_RejectsStripeAccount(t *testing.T) {
	opts := testutil.NewOptsForAccount("http://127.0.0.1:1", "acct_x")
	for _, sub := range []string{"list", "get", "refunds"} {
		err := Run(context.Background(), opts, []string{sub, "fee_1"})
		if err == nil {
			t.Errorf("%s: expected --stripe-account to be rejected", sub)
			continue
		}
		var oe *output.Error
		if !errors.As(err, &oe) || oe.By != output.FixableByAgent {
			t.Errorf("%s: want agent-fixable output.Error, got %v (%T)", sub, err, err)
		}
	}
}
