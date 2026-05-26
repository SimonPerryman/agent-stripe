package setupintent

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/simonperryman/agent-stripe/internal/testutil"
)

func TestSetupIntentAttempts_PassesSetupIntentFilter(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"object":"list","data":[],"has_more":false,"url":"/v1/setup_attempts"}`)
	}))
	defer srv.Close()

	opts := testutil.NewOpts(srv.URL)
	testutil.CaptureStdout(t)
	if err := runAttempts(context.Background(), opts, []string{"seti_123"}); err != nil {
		t.Fatalf("runAttempts: %v", err)
	}
	if !strings.HasSuffix(gotPath, "/v1/setup_attempts") {
		t.Errorf("expected /v1/setup_attempts, got %q", gotPath)
	}
	if !strings.Contains(gotQuery, "setup_intent=seti_123") {
		t.Errorf("expected setup_intent=seti_123, got %q", gotQuery)
	}
}

func TestSetupIntentAttempts_MissingIDErrors(t *testing.T) {
	opts := testutil.NewOpts("http://unused")
	if err := runAttempts(context.Background(), opts, []string{}); err == nil {
		t.Fatal("expected error when seti id is missing")
	}
}

func TestSetupIntentList_PaymentMethodPassthrough(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"object":"list","data":[],"has_more":false,"url":"/v1/setup_intents"}`)
	}))
	defer srv.Close()
	opts := testutil.NewOpts(srv.URL)
	testutil.CaptureStdout(t)
	if err := runList(context.Background(), opts, []string{"--payment-method", "pm_x"}); err != nil {
		t.Fatalf("runList: %v", err)
	}
	if !strings.Contains(got, "payment_method=pm_x") {
		t.Errorf("expected payment_method=pm_x, got %q", got)
	}
}
