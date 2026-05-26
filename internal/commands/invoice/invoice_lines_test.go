package invoice

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/simonperryman/agent-stripe/internal/testutil"
)

func TestInvoiceLines_PathContainsInvoiceID(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{"object":"list","data":[],"has_more":false,"url":"/v1/invoices/in_1/lines"}`)
	}))
	defer srv.Close()

	opts := testutil.NewOpts(srv.URL)
	testutil.CaptureStdout(t)
	if err := runLines(context.Background(), opts, []string{"in_1"}); err != nil {
		t.Fatalf("runLines: %v", err)
	}
	if !strings.Contains(gotPath, "/v1/invoices/in_1/lines") {
		t.Errorf("expected /v1/invoices/in_1/lines path, got %q", gotPath)
	}
}

func TestInvoiceLines_MissingIDErrors(t *testing.T) {
	opts := testutil.NewOpts("http://unused")
	if err := runLines(context.Background(), opts, []string{}); err == nil {
		t.Fatal("expected error when invoice id is missing")
	}
}
