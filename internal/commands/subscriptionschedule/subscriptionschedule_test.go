package subscriptionschedule

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/simonperryman/agent-stripe/internal/testutil"
)

func TestSubscriptionScheduleList_ScheduledPassthrough(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"object":"list","data":[],"has_more":false,"url":"/v1/subscription_schedules"}`)
	}))
	defer srv.Close()
	opts := testutil.NewOpts(srv.URL)
	testutil.CaptureStdout(t)
	if err := runList(context.Background(), opts, []string{"--scheduled"}); err != nil {
		t.Fatalf("runList: %v", err)
	}
	if !strings.Contains(got, "scheduled=true") {
		t.Errorf("expected scheduled=true in query, got %q", got)
	}
}

func TestSubscriptionScheduleList_ScheduledOmittedByDefault(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"object":"list","data":[],"has_more":false,"url":"/v1/subscription_schedules"}`)
	}))
	defer srv.Close()
	opts := testutil.NewOpts(srv.URL)
	testutil.CaptureStdout(t)
	if err := runList(context.Background(), opts, []string{}); err != nil {
		t.Fatalf("runList: %v", err)
	}
	if strings.Contains(got, "scheduled=") {
		t.Errorf("expected no scheduled key when flag absent, got %q", got)
	}
}

func TestSubscriptionScheduleList_CanceledAtRangePassthrough(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"object":"list","data":[],"has_more":false,"url":"/v1/subscription_schedules"}`)
	}))
	defer srv.Close()
	opts := testutil.NewOpts(srv.URL)
	testutil.CaptureStdout(t)
	if err := runList(context.Background(), opts, []string{"--canceled-at-gt", "1700000000", "--canceled-at-lt", "1800000000"}); err != nil {
		t.Fatalf("runList: %v", err)
	}
	if !strings.Contains(got, "canceled_at") {
		t.Errorf("expected canceled_at in query, got %q", got)
	}
	if !strings.Contains(got, "1700000000") || !strings.Contains(got, "1800000000") {
		t.Errorf("expected canceled_at bounds in query, got %q", got)
	}
}
