package testclock

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/simonperryman/agent-stripe/internal/cli"
	"github.com/simonperryman/agent-stripe/internal/testutil"
)

func TestTestClockGet(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{"id":"clock_1","object":"test_helpers.test_clock","status":"ready","frozen_time":1700000000}`)
	}))
	defer srv.Close()

	opts := testutil.NewOpts(srv.URL)
	out := testutil.WithCapturedStdout(t, func() {
		if err := runGet(context.Background(), opts, []string{"clock_1"}); err != nil {
			t.Fatalf("runGet: %v", err)
		}
	})

	if gotPath != "/v1/test_helpers/test_clocks/clock_1" {
		t.Errorf("path = %q, want /v1/test_helpers/test_clocks/clock_1", gotPath)
	}
	// status and frozen_time are the two fields the whole command exists for —
	// they are what separates "the clock never advanced" from "the webhook did
	// not fire", so their surviving marshalling is the contract.
	for _, want := range []string{`"status"`, `"ready"`, `"frozen_time"`} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %s in output, got %s", want, out)
		}
	}
}

func TestTestClockGet_RequiresID(t *testing.T) {
	opts := testutil.NewOpts("http://unused")
	if err := runGet(context.Background(), opts, nil); err == nil {
		t.Fatal("expected error when no id")
	}
}

func TestTestClockList_Passthrough(t *testing.T) {
	var gotQuery, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery, gotPath = r.URL.RawQuery, r.URL.Path
		_, _ = io.WriteString(w, `{"object":"list","data":[],"has_more":false,"url":"/v1/test_helpers/test_clocks"}`)
	}))
	defer srv.Close()

	opts := testutil.NewOpts(srv.URL)
	testutil.CaptureStdout(t)
	if err := runList(context.Background(), opts, []string{"--limit", "5", "--starting-after", "clock_x"}); err != nil {
		t.Fatalf("runList: %v", err)
	}
	if gotPath != "/v1/test_helpers/test_clocks" {
		t.Errorf("path = %q, want /v1/test_helpers/test_clocks", gotPath)
	}
	for _, want := range []string{"limit=5", "starting_after=clock_x"} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("expected %q in query, got %q", want, gotQuery)
		}
	}
}

// Clocks are per-account: a connected account's clock is invisible from the
// platform, so the header has to reach the wire here like everywhere else.
func TestTestClockGet_ScopesToConnectedAccount(t *testing.T) {
	var gotAccount string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccount = r.Header.Get("Stripe-Account")
		_, _ = io.WriteString(w, `{"id":"clock_1","object":"test_helpers.test_clock"}`)
	}))
	defer srv.Close()

	opts := testutil.NewOptsForAccount(srv.URL, "acct_123")
	testutil.CaptureStdout(t)
	if err := runGet(context.Background(), opts, []string{"clock_1"}); err != nil {
		t.Fatalf("runGet: %v", err)
	}
	if gotAccount != "acct_123" {
		t.Errorf("Stripe-Account = %q, want acct_123", gotAccount)
	}
}

func TestTestClockRun_Dispatch(t *testing.T) {
	opts := &cli.GlobalOpts{}
	if err := Run(context.Background(), opts, nil); err != nil {
		t.Errorf("empty: %v", err)
	}
	if err := Run(context.Background(), opts, []string{"usage"}); err != nil {
		t.Errorf("usage: %v", err)
	}
	if err := Run(context.Background(), opts, []string{"help"}); err != nil {
		t.Errorf("help: %v", err)
	}
	if err := Run(context.Background(), opts, []string{"nope"}); err == nil {
		t.Error("expected error for unknown subcommand")
	}
}
