package invoiceitem

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/simonperryman/agent-stripe/internal/testutil"
)

func TestInvoiceItemList_PendingPassthrough(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"object":"list","data":[],"has_more":false,"url":"/v1/invoiceitems"}`)
	}))
	defer srv.Close()
	opts := testutil.NewOpts(srv.URL)
	testutil.CaptureStdout(t)
	if err := runList(context.Background(), opts, []string{"--pending"}); err != nil {
		t.Fatalf("runList: %v", err)
	}
	if !strings.Contains(got, "pending=true") {
		t.Errorf("expected pending=true in query, got %q", got)
	}
}

func TestInvoiceItemList_PendingAbsent(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"object":"list","data":[],"has_more":false,"url":"/v1/invoiceitems"}`)
	}))
	defer srv.Close()
	opts := testutil.NewOpts(srv.URL)
	testutil.CaptureStdout(t)
	if err := runList(context.Background(), opts, []string{"--customer", "cus_x"}); err != nil {
		t.Fatalf("runList: %v", err)
	}
	if strings.Contains(got, "pending=") {
		t.Errorf("expected no pending key when flag absent, got %q", got)
	}
}
