package paymentmethod

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/simonperryman/agent-stripe/internal/testutil"
)

func TestPaymentMethodList_RequiresCustomer(t *testing.T) {
	opts := testutil.NewOpts("http://unused")
	err := runList(context.Background(), opts, []string{"--type", "card"})
	if err == nil {
		t.Fatal("expected error when --customer omitted")
	}
	if !strings.Contains(err.Error(), "--customer") {
		t.Errorf("expected error to mention --customer, got %q", err.Error())
	}
}

func TestPaymentMethodList_TypePassthrough(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"object":"list","data":[],"has_more":false,"url":"/v1/payment_methods"}`)
	}))
	defer srv.Close()

	opts := testutil.NewOpts(srv.URL)
	testutil.CaptureStdout(t)
	if err := runList(context.Background(), opts, []string{"--customer", "cus_x", "--type", "card"}); err != nil {
		t.Fatalf("runList: %v", err)
	}
	if !strings.Contains(got, "customer=cus_x") {
		t.Errorf("expected customer=cus_x, got %q", got)
	}
	if !strings.Contains(got, "type=card") {
		t.Errorf("expected type=card, got %q", got)
	}
}
