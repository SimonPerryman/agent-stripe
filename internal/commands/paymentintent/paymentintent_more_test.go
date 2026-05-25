package paymentintent

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

func TestPaymentIntentGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v1/payment_intents/pi_") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"id":"pi_1","object":"payment_intent"}`)
	}))
	defer srv.Close()
	opts := testutil.NewOpts(srv.URL)
	testutil.CaptureStdout(t)
	if err := runGet(context.Background(), opts, []string{"pi_1"}); err != nil {
		t.Fatalf("runGet: %v", err)
	}
}

func TestPaymentIntentGet_RequiresID(t *testing.T) {
	opts := testutil.NewOpts("http://unused")
	if err := runGet(context.Background(), opts, nil); err == nil {
		t.Fatal("expected error when no id")
	}
}

func TestPaymentIntentList_FiltersPassthrough(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"object":"list","data":[],"has_more":false,"url":"/v1/payment_intents"}`)
	}))
	defer srv.Close()
	opts := testutil.NewOpts(srv.URL)
	testutil.CaptureStdout(t)
	args := []string{"--customer", "cus_1", "--created-gt", "10", "--created-lt", "20", "--starting-after", "pi_x", "--limit", "5"}
	if err := runList(context.Background(), opts, args); err != nil {
		t.Fatalf("runList: %v", err)
	}
	for _, want := range []string{"customer=cus_1", "starting_after=pi_x", "created[gt]=10", "created[lt]=20", "limit=5"} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("expected %q in query, got %q", want, gotQuery)
		}
	}
}

func TestPaymentIntentSearch(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"object":"search_result","data":[],"has_more":false,"url":"/v1/payment_intents/search"}`)
	}))
	defer srv.Close()
	opts := testutil.NewOpts(srv.URL)
	testutil.CaptureStdout(t)
	if err := runSearch(context.Background(), opts, []string{"--query", `status:"requires_action"`, "--page", "tok_x", "--limit", "5"}); err != nil {
		t.Fatalf("runSearch: %v", err)
	}
	if !strings.Contains(gotQuery, "query=status") {
		t.Errorf("expected query=status... in query, got %q", gotQuery)
	}
	if !strings.Contains(gotQuery, "page=tok_x") {
		t.Errorf("expected page=tok_x in query, got %q", gotQuery)
	}
}

func TestPaymentIntentSearch_RequiresQuery(t *testing.T) {
	opts := testutil.NewOpts("http://unused")
	if err := runSearch(context.Background(), opts, nil); err == nil {
		t.Fatal("expected error when no --query")
	}
}

func TestPaymentIntentRun_Dispatch(t *testing.T) {
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
