package coupon

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

func TestCouponGet(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{"id":"LAUNCH20","object":"coupon","percent_off":20,"valid":true}`)
	}))
	defer srv.Close()

	opts := testutil.NewOpts(srv.URL)
	testutil.CaptureStdout(t)
	if err := runGet(context.Background(), opts, []string{"LAUNCH20"}); err != nil {
		t.Fatalf("runGet: %v", err)
	}
	// Coupon ids are caller-chosen strings rather than prefixed tokens, so the
	// path is the only place a mangled id would show up.
	if gotPath != "/v1/coupons/LAUNCH20" {
		t.Errorf("path = %q, want /v1/coupons/LAUNCH20", gotPath)
	}
}

func TestCouponGet_RequiresID(t *testing.T) {
	opts := testutil.NewOpts("http://unused")
	if err := runGet(context.Background(), opts, nil); err == nil {
		t.Fatal("expected error when no id")
	}
}

// Reading a coupon under --stripe-account is the whole diagnosis for
// "No such coupon" on a connected account, so the header has to reach the wire.
func TestCouponGet_ScopesToConnectedAccount(t *testing.T) {
	var gotAccount string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccount = r.Header.Get("Stripe-Account")
		_, _ = io.WriteString(w, `{"id":"LAUNCH20","object":"coupon"}`)
	}))
	defer srv.Close()

	opts := testutil.NewOptsForAccount(srv.URL, "acct_123")
	testutil.CaptureStdout(t)
	if err := runGet(context.Background(), opts, []string{"LAUNCH20"}); err != nil {
		t.Fatalf("runGet: %v", err)
	}
	if gotAccount != "acct_123" {
		t.Errorf("Stripe-Account = %q, want acct_123", gotAccount)
	}
}

func TestCouponList_FiltersPassthrough(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		if r.URL.Path != "/v1/coupons" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"object":"list","data":[],"has_more":false,"url":"/v1/coupons"}`)
	}))
	defer srv.Close()

	opts := testutil.NewOpts(srv.URL)
	testutil.CaptureStdout(t)
	args := []string{"--created-gt", "10", "--created-lt", "20", "--starting-after", "OLD", "--limit", "5"}
	if err := runList(context.Background(), opts, args); err != nil {
		t.Fatalf("runList: %v", err)
	}
	for _, want := range []string{"created[gt]=10", "created[lt]=20", "starting_after=OLD", "limit=5"} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("expected %q in query, got %q", want, gotQuery)
		}
	}
}

func TestCouponRun_Dispatch(t *testing.T) {
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
