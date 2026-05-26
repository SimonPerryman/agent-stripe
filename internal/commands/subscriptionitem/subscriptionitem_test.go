package subscriptionitem

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/simonperryman/agent-stripe/internal/testutil"
)

func TestSubscriptionItemList_RequiresSubscription(t *testing.T) {
	opts := testutil.NewOpts("http://unused")
	err := runList(context.Background(), opts, []string{})
	if err == nil {
		t.Fatal("expected error when --subscription omitted")
	}
	if !strings.Contains(err.Error(), "--subscription") {
		t.Errorf("expected error to mention --subscription, got %q", err.Error())
	}
}

func TestSubscriptionItemList_Passthrough(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"object":"list","data":[],"has_more":false,"url":"/v1/subscription_items"}`)
	}))
	defer srv.Close()
	opts := testutil.NewOpts(srv.URL)
	testutil.CaptureStdout(t)
	if err := runList(context.Background(), opts, []string{"--subscription", "sub_x"}); err != nil {
		t.Fatalf("runList: %v", err)
	}
	if !strings.Contains(got, "subscription=sub_x") {
		t.Errorf("expected subscription=sub_x, got %q", got)
	}
}
