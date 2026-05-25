package customer

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

func TestCustomerGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v1/customers/cus_") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"id":"cus_1","object":"customer"}`)
	}))
	defer srv.Close()
	opts := testutil.NewOpts(srv.URL)
	testutil.CaptureStdout(t)
	if err := runGet(context.Background(), opts, []string{"cus_1"}); err != nil {
		t.Fatalf("runGet: %v", err)
	}
}

func TestCustomerGet_RequiresID(t *testing.T) {
	opts := testutil.NewOpts("http://unused")
	if err := runGet(context.Background(), opts, nil); err == nil {
		t.Fatal("expected error when no id")
	}
}

func TestCustomerList_FiltersPassthrough(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"object":"list","data":[],"has_more":false,"url":"/v1/customers"}`)
	}))
	defer srv.Close()
	opts := testutil.NewOpts(srv.URL)
	testutil.CaptureStdout(t)
	args := []string{"--email", "a@b.com", "--created-gt", "10", "--created-lt", "20", "--starting-after", "cus_x", "--limit", "5"}
	if err := runList(context.Background(), opts, args); err != nil {
		t.Fatalf("runList: %v", err)
	}
	for _, want := range []string{"email=a%40b.com", "starting_after=cus_x", "created[gt]=10", "created[lt]=20", "limit=5"} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("expected %q in query, got %q", want, gotQuery)
		}
	}
}

func TestCustomerRun_Dispatch(t *testing.T) {
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
